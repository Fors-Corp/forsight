// Package otlp implements a minimal OTLP/HTTP receiver — the wire format
// every OpenTelemetry SDK speaks by default, so any instrumented app can
// point its OTLP exporter at forsight with no forsight-specific integration.
//
// All five OTLP metric point types are handled (Gauge, Sum, Histogram,
// ExponentialHistogram, Summary), flattened onto forsight's flat
// name+value+labels model.Metric the same way Prometheus itself does for
// classic histograms: a `_count` and `_sum` series plus one `_bucket` series
// per boundary, labeled `le="<bound>"`, with cumulative counts (OTLP's own
// bucket_counts are per-bucket, not cumulative — this receiver sums them
// going up, matching what a Prometheus-literate reader expects `_bucket` to
// mean). ExponentialHistogram gets `_count`/`_sum`/`_min`/`_max` only, not a
// reconstructed per-bucket series — its buckets are base-2 exponential with
// a dynamic `scale`, and rebuilding boundary values from that isn't worth
// the complexity for what's usually an already-approximate distribution;
// `_count`/`_sum` alone is real, useful data, just not full fidelity.
//
// Traces and logs are accepted on the standard `/v1/traces` and `/v1/logs`
// paths. Log severity numbers/text map onto model.LogSeverity; the body
// becomes Message; resource `service.name` (falling back to the
// instrumentation scope / logger name) becomes Source.
//
// Both OTLP/protobuf (the default for every OTel SDK's HTTP exporter) and
// OTLP/JSON bodies are accepted, selected by the request's Content-Type.
package otlp

import (
	"context"
	"encoding/hex"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// MetricSink, SpanSink and LogSink are the write sides of store.Store —
// declared here (not imported from store) so this package doesn't need to
// know about the rest of the storage interface, only what it writes.
type MetricSink interface {
	WriteMetrics(ctx context.Context, metrics []model.Metric) error
}

type SpanSink interface {
	WriteSpans(ctx context.Context, spans []model.Span) error
}

type LogSink interface {
	WriteLogs(ctx context.Context, logs []model.LogEntry) error
}

// Handler serves the OTLP/HTTP metrics, traces and logs endpoints.
type Handler struct {
	metrics MetricSink
	spans   SpanSink
	logs    LogSink
}

func NewHandler(metrics MetricSink, spans SpanSink, logs LogSink) *Handler {
	return &Handler{metrics: metrics, spans: spans, logs: logs}
}

// Register mounts the standard OTLP/HTTP paths on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/metrics", h.handleMetrics)
	mux.HandleFunc("POST /v1/traces", h.handleTraces)
	mux.HandleFunc("POST /v1/logs", h.handleLogs)
}

func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20)) // 32MiB cap
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var req collectormetrics.ExportMetricsServiceRequest
	if err := unmarshalOTLP(r, body, &req); err != nil {
		http.Error(w, "invalid OTLP metrics payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	metrics, truncated := metricsFromOTLP(&req)
	if truncated {
		http.Error(w, "payload flattens to more than the per-request metric limit", http.StatusRequestEntityTooLarge)
		return
	}
	if err := h.metrics.WriteMetrics(r.Context(), metrics); err != nil {
		http.Error(w, "failed to store metrics", http.StatusInternalServerError)
		return
	}
	writeEmptyResponse(w, r)
}

func (h *Handler) handleTraces(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var req collectortrace.ExportTraceServiceRequest
	if err := unmarshalOTLP(r, body, &req); err != nil {
		http.Error(w, "invalid OTLP traces payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	spans, truncated := spansFromOTLP(&req)
	if truncated {
		http.Error(w, "payload contains more than the per-request span limit", http.StatusRequestEntityTooLarge)
		return
	}
	if err := h.spans.WriteSpans(r.Context(), spans); err != nil {
		http.Error(w, "failed to store spans", http.StatusInternalServerError)
		return
	}
	writeEmptyResponse(w, r)
}

func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var req collectorlogs.ExportLogsServiceRequest
	if err := unmarshalOTLP(r, body, &req); err != nil {
		http.Error(w, "invalid OTLP logs payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	logs, truncated := logsFromOTLP(&req)
	if truncated {
		http.Error(w, "payload contains more than the per-request log limit", http.StatusRequestEntityTooLarge)
		return
	}
	if err := h.logs.WriteLogs(r.Context(), logs); err != nil {
		http.Error(w, "failed to store logs", http.StatusInternalServerError)
		return
	}
	writeEmptyResponse(w, r)
}

// isJSONRequest is the only signal OTLP/HTTP defines for which codec a body
// uses — there's no magic byte to sniff, protobuf's wire format is not
// self-describing. A request with no Content-Type at all (some minimal
// clients omit it) falls back to protobuf, the default every real OTel SDK
// exporter uses.
func isJSONRequest(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Content-Type"), "json")
}

func unmarshalOTLP(r *http.Request, body []byte, msg proto.Message) error {
	if isJSONRequest(r) {
		return protojson.Unmarshal(body, msg)
	}
	return proto.Unmarshal(body, msg)
}

// writeEmptyResponse satisfies OTLP/HTTP clients, which expect a (possibly
// empty) ExportServiceResponse body on success — an empty body with a 200
// status is a valid empty message in both codecs, so this just sets the
// content type and status without constructing one. Matches the request's
// own codec, since a JSON client checking Content-Type on the response would
// otherwise see protobuf's mime type on an empty body it never asked for.
func writeEmptyResponse(w http.ResponseWriter, r *http.Request) {
	if isJSONRequest(r) {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "application/x-protobuf")
	}
	w.WriteHeader(http.StatusOK)
}

// metricsFromOTLP flattens a request, stopping at maxMetricsPerRequest. The
// bool reports whether it stopped early, so the handler can reject the
// request outright rather than silently storing a truncated prefix — half of
// somebody's histogram is worse than a clear 413.
func metricsFromOTLP(req *collectormetrics.ExportMetricsServiceRequest) ([]model.Metric, bool) {
	var out []model.Metric
	for _, rm := range req.GetResourceMetrics() {
		resourceLabels := resourceAttributes(rm.GetResource())
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				out = append(out, dataPointsFromMetric(m, resourceLabels)...)
				if len(out) > maxMetricsPerRequest {
					return out[:maxMetricsPerRequest], true
				}
			}
		}
	}
	return out, false
}

func dataPointsFromMetric(m *metricspb.Metric, resourceLabels map[string]string) []model.Metric {
	switch data := m.GetData().(type) {
	case *metricspb.Metric_Gauge:
		return numberDataPoints(m.GetName(), data.Gauge.GetDataPoints(), resourceLabels)
	case *metricspb.Metric_Sum:
		return numberDataPoints(m.GetName(), data.Sum.GetDataPoints(), resourceLabels)
	case *metricspb.Metric_Histogram:
		return histogramDataPoints(m.GetName(), data.Histogram.GetDataPoints(), resourceLabels)
	case *metricspb.Metric_ExponentialHistogram:
		return expHistogramDataPoints(m.GetName(), data.ExponentialHistogram.GetDataPoints(), resourceLabels)
	case *metricspb.Metric_Summary:
		return summaryDataPoints(m.GetName(), data.Summary.GetDataPoints(), resourceLabels)
	default:
		return nil
	}
}

func numberDataPoints(name string, points []*metricspb.NumberDataPoint, resourceLabels map[string]string) []model.Metric {
	now := time.Now()
	out := make([]model.Metric, 0, len(points))
	for _, dp := range points {
		value, ok := numberDataPointValue(dp)
		if !ok {
			continue
		}
		labels := mergeLabels(resourceLabels, attributesToLabels(dp.GetAttributes()))
		out = append(out, model.Metric{
			Name:      name,
			Value:     value,
			Timestamp: pointTime(dp.GetTimeUnixNano(), now),
			Labels:    labels,
		})
	}
	return out
}

// histogramDataPoints flattens a classic (explicit-bounds) histogram the way
// Prometheus itself would expose it: <name>_count, <name>_sum, and one
// <name>_bucket{le="<bound>"} per boundary carrying the CUMULATIVE count up
// to and including that bound — OTLP's own bucket_counts are per-bucket
// (the count strictly between the previous and current bound), so this
// running-sum conversion is what makes `le` mean what a Prometheus-literate
// reader already expects it to mean.
func histogramDataPoints(name string, points []*metricspb.HistogramDataPoint, resourceLabels map[string]string) []model.Metric {
	var out []model.Metric
	for _, dp := range points {
		ts := pointTime(dp.GetTimeUnixNano(), time.Now())
		labels := mergeLabels(resourceLabels, attributesToLabels(dp.GetAttributes()))

		out = append(out, model.Metric{Name: name + "_count", Value: float64(dp.GetCount()), Timestamp: ts, Labels: labels})
		if dp.Sum != nil {
			out = append(out, model.Metric{Name: name + "_sum", Value: dp.GetSum(), Timestamp: ts, Labels: labels})
		}
		if dp.Min != nil {
			out = append(out, model.Metric{Name: name + "_min", Value: dp.GetMin(), Timestamp: ts, Labels: labels})
		}
		if dp.Max != nil {
			out = append(out, model.Metric{Name: name + "_max", Value: dp.GetMax(), Timestamp: ts, Labels: labels})
		}

		bounds := dp.GetExplicitBounds()
		var cumulative float64
		for i, count := range dp.GetBucketCounts() {
			cumulative += float64(count)
			if i >= len(bounds) {
				break // the final, implicit (+Inf) bucket carries no boundary to label with
			}
			if i >= maxBucketsPerPoint {
				break // see maxBucketsPerPoint: the fan-out is the attack surface
			}
			bucketLabels := mergeLabels(labels, map[string]string{"le": formatBound(bounds[i])})
			out = append(out, model.Metric{Name: name + "_bucket", Value: cumulative, Timestamp: ts, Labels: bucketLabels})
		}
	}
	return out
}

// expHistogramDataPoints deliberately does not reconstruct per-bucket
// series — see the package doc for why — count/sum/min/max are still real,
// useful data even without full distribution fidelity.
func expHistogramDataPoints(name string, points []*metricspb.ExponentialHistogramDataPoint, resourceLabels map[string]string) []model.Metric {
	var out []model.Metric
	for _, dp := range points {
		ts := pointTime(dp.GetTimeUnixNano(), time.Now())
		labels := mergeLabels(resourceLabels, attributesToLabels(dp.GetAttributes()))

		out = append(out, model.Metric{Name: name + "_count", Value: float64(dp.GetCount()), Timestamp: ts, Labels: labels})
		if dp.Sum != nil {
			out = append(out, model.Metric{Name: name + "_sum", Value: dp.GetSum(), Timestamp: ts, Labels: labels})
		}
		if dp.Min != nil {
			out = append(out, model.Metric{Name: name + "_min", Value: dp.GetMin(), Timestamp: ts, Labels: labels})
		}
		if dp.Max != nil {
			out = append(out, model.Metric{Name: name + "_max", Value: dp.GetMax(), Timestamp: ts, Labels: labels})
		}
	}
	return out
}

// summaryDataPoints flattens to <name>_count, <name>_sum, and one
// <name>{quantile="<q>"} per reported quantile — the same shape Prometheus
// client libraries already use for their own summary type.
func summaryDataPoints(name string, points []*metricspb.SummaryDataPoint, resourceLabels map[string]string) []model.Metric {
	var out []model.Metric
	for _, dp := range points {
		ts := pointTime(dp.GetTimeUnixNano(), time.Now())
		labels := mergeLabels(resourceLabels, attributesToLabels(dp.GetAttributes()))

		out = append(out, model.Metric{Name: name + "_count", Value: float64(dp.GetCount()), Timestamp: ts, Labels: labels})
		out = append(out, model.Metric{Name: name + "_sum", Value: dp.GetSum(), Timestamp: ts, Labels: labels})
		for i, q := range dp.GetQuantileValues() {
			if i >= maxQuantilesPerPoint {
				break // same fan-out concern as histogram buckets
			}
			qLabels := mergeLabels(labels, map[string]string{"quantile": formatBound(q.GetQuantile())})
			out = append(out, model.Metric{Name: name, Value: q.GetValue(), Timestamp: ts, Labels: qLabels})
		}
	}
	return out
}

// Ingest bounds. These exist because every number below is chosen by whoever
// sends the request: an OTLP histogram declares its own bucket count, and the
// receiver flattens one metric per bucket. Without a cap, a single request
// inside the body-size limit expands into millions of metrics — the body is
// compact because bucket bounds are small, while each produced metric carries
// a freshly copied label map. Capping the body is not a fix for that; capping
// what the body is allowed to *produce* is.
const (
	// maxBucketsPerPoint bounds explicit histogram buckets flattened per data
	// point. Prometheus histograms in practice have tens of buckets; 1024 is
	// far above any real instrument and far below anything dangerous.
	maxBucketsPerPoint = 1024
	// maxQuantilesPerPoint bounds a summary's reported quantiles per data
	// point, which flatten the same way.
	maxQuantilesPerPoint = 128
	// maxMetricsPerRequest bounds the total flattened output of one request.
	maxMetricsPerRequest = 200_000
	// maxLogsPerRequest bounds how many log records one request may carry —
	// same order as the metrics cap, so a single POST cannot pin unbounded
	// memory before the store's own element cap runs.
	maxLogsPerRequest = 200_000
	// maxSpansPerRequest bounds how many spans one request may carry — same
	// reasoning and order as maxMetricsPerRequest/maxLogsPerRequest. Unlike
	// those two, spans had no per-request cap at all: a minimal Span message
	// is small enough that 32MiB (the body-read limit every /v1/* endpoint
	// already applies) decodes to on the order of ten million of them.
	maxSpansPerRequest = 200_000
	// maxFutureSkew is how far ahead of now an ingested timestamp may be
	// before it is clamped. A timestamp past the retention horizon would
	// otherwise never be pruned, making a point immortal and pinning the
	// dashboard's "latest value" to it forever.
	maxFutureSkew = 5 * time.Minute
)

// pointTime converts a wire timestamp, defending against both a uint64 that
// does not fit in an int64 and a timestamp chosen to outlive retention.
func pointTime(nanos uint64, now time.Time) time.Time {
	if nanos > math.MaxInt64 {
		return now
	}
	ts := time.Unix(0, int64(nanos))
	if ts.After(now.Add(maxFutureSkew)) {
		return now
	}
	return ts
}

func formatBound(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// numberDataPointValue reads a data point's value, reporting false for one
// that is not finite. A double on the wire can be NaN or +/-Inf, and nothing
// downstream survives one: json.Marshal fails on the entire metrics response
// (see writeJSON), the Badger batch cannot encode the record, and the
// detector's Welford state for that series is poisoned permanently (see
// observeOneLocked). Dropping the point is the only reading that is not a
// lie; an int value is finite by construction.
func numberDataPointValue(dp *metricspb.NumberDataPoint) (float64, bool) {
	switch v := dp.GetValue().(type) {
	case *metricspb.NumberDataPoint_AsDouble:
		if math.IsNaN(v.AsDouble) || math.IsInf(v.AsDouble, 0) {
			return 0, false
		}
		return v.AsDouble, true
	case *metricspb.NumberDataPoint_AsInt:
		return float64(v.AsInt), true
	default:
		return 0, false
	}
}

// spansFromOTLP flattens a request, stopping at maxSpansPerRequest. The
// bool reports whether it stopped early, so the handler can reject the
// request outright rather than silently storing a truncated prefix — same
// contract as metricsFromOTLP/logsFromOTLP, and for the same reason: half
// of somebody's trace is worse than a clear 413.
func spansFromOTLP(req *collectortrace.ExportTraceServiceRequest) ([]model.Span, bool) {
	var out []model.Span
	for _, rs := range req.GetResourceSpans() {
		service := serviceName(rs.GetResource())
		for _, ss := range rs.GetScopeSpans() {
			for _, s := range ss.GetSpans() {
				out = append(out, spanFromOTLP(s, service))
				if len(out) > maxSpansPerRequest {
					return out[:maxSpansPerRequest], true
				}
			}
		}
	}
	return out, false
}

// logsFromOTLP flattens a request, stopping at maxLogsPerRequest. The bool
// reports whether it stopped early so the handler can reject rather than
// silently store a truncated prefix — same contract as metricsFromOTLP.
func logsFromOTLP(req *collectorlogs.ExportLogsServiceRequest) ([]model.LogEntry, bool) {
	now := time.Now()
	var out []model.LogEntry
	for _, rl := range req.GetResourceLogs() {
		service := serviceName(rl.GetResource())
		for _, sl := range rl.GetScopeLogs() {
			// Prefer resource service.name; fall back to the instrumentation
			// scope name (the logger) when the resource has no service.
			source := service
			if service == "unknown_service" {
				if scope := sl.GetScope(); scope != nil && scope.GetName() != "" {
					source = scope.GetName()
				}
			}
			for _, lr := range sl.GetLogRecords() {
				out = append(out, logFromOTLP(lr, source, now))
				if len(out) > maxLogsPerRequest {
					return out[:maxLogsPerRequest], true
				}
			}
		}
	}
	return out, false
}

func logFromOTLP(lr *logspb.LogRecord, source string, now time.Time) model.LogEntry {
	nanos := lr.GetTimeUnixNano()
	if nanos == 0 {
		nanos = lr.GetObservedTimeUnixNano()
	}
	return model.LogEntry{
		Timestamp: pointTime(nanos, now),
		Severity:  logSeverity(lr.GetSeverityNumber(), lr.GetSeverityText()),
		Source:    source,
		Message:   logBodyMessage(lr.GetBody()),
		Labels:    attributesToLabels(lr.GetAttributes()),
	}
}

func logBodyMessage(body *commonpb.AnyValue) string {
	if body == nil {
		return ""
	}
	if s, ok := stringifyAnyValue(body); ok {
		return s
	}
	return ""
}

// logSeverity maps OTLP severity number/text onto forsight's coarse four
// levels. Number wins when set; text is a case-insensitive fallback; missing
// both defaults to info (the least surprising level for an untagged line).
func logSeverity(num logspb.SeverityNumber, text string) model.LogSeverity {
	switch {
	case num >= logspb.SeverityNumber_SEVERITY_NUMBER_FATAL:
		return model.LogSeverityError
	case num >= logspb.SeverityNumber_SEVERITY_NUMBER_ERROR:
		return model.LogSeverityError
	case num >= logspb.SeverityNumber_SEVERITY_NUMBER_WARN:
		return model.LogSeverityWarn
	case num >= logspb.SeverityNumber_SEVERITY_NUMBER_INFO:
		return model.LogSeverityInfo
	case num >= logspb.SeverityNumber_SEVERITY_NUMBER_TRACE:
		return model.LogSeverityDebug
	}

	switch strings.ToLower(strings.TrimSpace(text)) {
	case "trace", "debug":
		return model.LogSeverityDebug
	case "info", "information":
		return model.LogSeverityInfo
	case "warn", "warning":
		return model.LogSeverityWarn
	case "error", "fatal", "critical", "panic":
		return model.LogSeverityError
	default:
		return model.LogSeverityInfo
	}
}

func spanFromOTLP(s *tracepb.Span, service string) model.Span {
	now := time.Now()
	start := pointTime(s.GetStartTimeUnixNano(), now)
	end := pointTime(s.GetEndTimeUnixNano(), now)
	// end before start is possible both from a clamp (only one side of a
	// pathological span gets pulled back to now) and from a malformed
	// payload with no clamping involved at all; either way a negative
	// Duration is nonsense a consumer must not have to guard against.
	duration := end.Sub(start)
	if duration < 0 {
		duration = 0
	}
	return model.Span{
		TraceID:    hex.EncodeToString(s.GetTraceId()),
		SpanID:     hex.EncodeToString(s.GetSpanId()),
		ParentID:   hex.EncodeToString(s.GetParentSpanId()),
		Name:       s.GetName(),
		Service:    service,
		Start:      start,
		Duration:   duration,
		Status:     spanStatus(s.GetStatus()),
		Attributes: attributesToLabels(s.GetAttributes()),
	}
}

func spanStatus(status *tracepb.Status) model.SpanStatus {
	switch status.GetCode() {
	case tracepb.Status_STATUS_CODE_OK:
		return model.SpanStatusOK
	case tracepb.Status_STATUS_CODE_ERROR:
		return model.SpanStatusError
	default:
		return model.SpanStatusUnset
	}
}

// serviceName reads the "service.name" resource attribute OTel's semantic
// conventions require every SDK to set — falls back to "unknown_service" if
// absent, matching the OpenTelemetry spec's own documented default.
func serviceName(r *resourcepb.Resource) string {
	labels := resourceAttributes(r)
	if name, ok := labels["service.name"]; ok {
		return name
	}
	return "unknown_service"
}

func resourceAttributes(r *resourcepb.Resource) map[string]string {
	return attributesToLabels(r.GetAttributes())
}

// attributesToLabels flattens OTLP attributes to strings. Non-string values
// (bool/int/double) are formatted; nested array/kvlist attributes are
// skipped — forsight's label model is flat, matching model.Metric.Labels.
func attributesToLabels(attrs []*commonpb.KeyValue) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for _, a := range attrs {
		if v, ok := stringifyAnyValue(a.GetValue()); ok {
			out[a.GetKey()] = v
		}
	}
	return out
}

func stringifyAnyValue(v *commonpb.AnyValue) (string, bool) {
	switch val := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return val.StringValue, true
	case *commonpb.AnyValue_BoolValue:
		if val.BoolValue {
			return "true", true
		}
		return "false", true
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(val.IntValue, 10), true
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(val.DoubleValue, 'g', -1, 64), true
	default:
		return "", false
	}
}

func mergeLabels(a, b map[string]string) map[string]string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v // point-level attributes win over resource-level ones
	}
	return out
}
