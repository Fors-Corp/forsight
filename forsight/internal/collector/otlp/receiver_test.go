package otlp

import (
	"bytes"
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

type fakeSink struct {
	metrics []model.Metric
	spans   []model.Span
	logs    []model.LogEntry
}

func (f *fakeSink) WriteMetrics(_ context.Context, m []model.Metric) error {
	f.metrics = append(f.metrics, m...)
	return nil
}

func (f *fakeSink) WriteSpans(_ context.Context, s []model.Span) error {
	f.spans = append(f.spans, s...)
	return nil
}

func (f *fakeSink) WriteLogs(_ context.Context, logs []model.LogEntry) error {
	f.logs = append(f.logs, logs...)
	return nil
}

func stringAttr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}},
	}
}

func TestHandleMetrics_GaugeAndSum(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	req := &collectormetrics.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{stringAttr("service.name", "checkout-api")},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Metrics: []*metricspb.Metric{
							{
								Name: "requests.count",
								Data: &metricspb.Metric_Sum{
									Sum: &metricspb.Sum{
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: 1_700_000_000_000_000_000,
												Value:        &metricspb.NumberDataPoint_AsInt{AsInt: 42},
											},
										},
									},
								},
							},
							{
								Name: "cpu.percent",
								Data: &metricspb.Metric_Gauge{
									Gauge: &metricspb.Gauge{
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: 1_700_000_000_000_000_000,
												Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 12.5},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body))
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(sink.metrics) != 2 {
		t.Fatalf("got %d metrics, want 2: %+v", len(sink.metrics), sink.metrics)
	}

	byName := map[string]model.Metric{}
	for _, m := range sink.metrics {
		byName[m.Name] = m
	}
	if m, ok := byName["requests.count"]; !ok || m.Value != 42 {
		t.Errorf("requests.count = %+v, want Value=42", m)
	}
	if m, ok := byName["cpu.percent"]; !ok || m.Value != 12.5 {
		t.Errorf("cpu.percent = %+v, want Value=12.5", m)
	}
	if got := byName["requests.count"].Labels["service.name"]; got != "checkout-api" {
		t.Errorf("resource label service.name = %q, want checkout-api", got)
	}
}

func TestHandleTraces_MapsSpanFields(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	traceID := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	req := &collectortrace.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{stringAttr("service.name", "payments")},
				},
				ScopeSpans: []*tracepb.ScopeSpans{
					{
						Spans: []*tracepb.Span{
							{
								TraceId:           traceID,
								SpanId:            spanID,
								Name:              "charge",
								StartTimeUnixNano: 1_700_000_000_000_000_000,
								EndTimeUnixNano:   1_700_000_000_500_000_000,
								Status:            &tracepb.Status{Code: tracepb.Status_STATUS_CODE_ERROR},
							},
						},
					},
				},
			},
		},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(sink.spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(sink.spans))
	}
	got := sink.spans[0]
	if got.Name != "charge" || got.Service != "payments" || got.Status != model.SpanStatusError {
		t.Errorf("span = %+v, want name=charge service=payments status=error", got)
	}
	if got.Duration.Milliseconds() != 500 {
		t.Errorf("span duration = %v, want 500ms", got.Duration)
	}
}

func TestHandleMetrics_RejectsInvalidBody(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader([]byte("not protobuf")))
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func float64Ptr(v float64) *float64 { return &v }

func TestHandleMetrics_HistogramCumulativeBuckets(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	req := &collectormetrics.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{
					Name: "request.duration",
					Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
						DataPoints: []*metricspb.HistogramDataPoint{{
							TimeUnixNano: 1_700_000_000_000_000_000,
							Count:        10,
							Sum:          float64Ptr(42.5),
							// per-bucket (not cumulative) counts: 2, 3, 5 across
							// bounds [1, 5] plus an implicit +Inf tail bucket.
							ExplicitBounds: []float64{1, 5},
							BucketCounts:   []uint64{2, 3, 5},
						}},
					}},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	byNameAndLe := map[string]model.Metric{}
	for _, m := range sink.metrics {
		key := m.Name
		if le, ok := m.Labels["le"]; ok {
			key += "{le=" + le + "}"
		}
		byNameAndLe[key] = m
	}

	if got := byNameAndLe["request.duration_count"]; got.Value != 10 {
		t.Errorf("request.duration_count = %v, want 10", got.Value)
	}
	if got := byNameAndLe["request.duration_sum"]; got.Value != 42.5 {
		t.Errorf("request.duration_sum = %v, want 42.5", got.Value)
	}
	// Cumulative, not per-bucket: 2, then 2+3=5, the implicit +Inf tail
	// (count 5) is dropped since it has no boundary to label with.
	if got := byNameAndLe["request.duration_bucket{le=1}"]; got.Value != 2 {
		t.Errorf("request.duration_bucket{le=1} = %v, want 2 (the raw per-bucket count, unchanged for the first bucket)", got.Value)
	}
	if got := byNameAndLe["request.duration_bucket{le=5}"]; got.Value != 5 {
		t.Errorf("request.duration_bucket{le=5} = %v, want 5 (cumulative: 2+3)", got.Value)
	}
	if _, ok := byNameAndLe["request.duration_bucket{le=+Inf}"]; ok {
		t.Error("an le=+Inf bucket was emitted, but the implicit tail bucket has no boundary and should be dropped")
	}
}

func TestHandleMetrics_ExponentialHistogramSkipsBucketReconstruction(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	req := &collectormetrics.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{
					Name: "latency",
					Data: &metricspb.Metric_ExponentialHistogram{ExponentialHistogram: &metricspb.ExponentialHistogram{
						DataPoints: []*metricspb.ExponentialHistogramDataPoint{{
							TimeUnixNano: 1_700_000_000_000_000_000,
							Count:        7,
							Sum:          float64Ptr(100),
							Min:          float64Ptr(1),
							Max:          float64Ptr(50),
						}},
					}},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(sink.metrics) != 4 {
		t.Fatalf("got %d metrics, want exactly 4 (count/sum/min/max, no bucket series): %+v", len(sink.metrics), sink.metrics)
	}
}

func TestHandleMetrics_SummaryQuantiles(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	req := &collectormetrics.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{
					Name: "gc.pause",
					Data: &metricspb.Metric_Summary{Summary: &metricspb.Summary{
						DataPoints: []*metricspb.SummaryDataPoint{{
							TimeUnixNano: 1_700_000_000_000_000_000,
							Count:        100,
							Sum:          500,
							QuantileValues: []*metricspb.SummaryDataPoint_ValueAtQuantile{
								{Quantile: 0.5, Value: 3.2},
								{Quantile: 0.99, Value: 9.8},
							},
						}},
					}},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	byNameAndQuantile := map[string]float64{}
	for _, m := range sink.metrics {
		key := m.Name
		if q, ok := m.Labels["quantile"]; ok {
			key += "{quantile=" + q + "}"
		}
		byNameAndQuantile[key] = m.Value
	}
	if byNameAndQuantile["gc.pause_count"] != 100 {
		t.Errorf("gc.pause_count = %v, want 100", byNameAndQuantile["gc.pause_count"])
	}
	if byNameAndQuantile["gc.pause{quantile=0.5}"] != 3.2 {
		t.Errorf("gc.pause{quantile=0.5} = %v, want 3.2", byNameAndQuantile["gc.pause{quantile=0.5}"])
	}
	if byNameAndQuantile["gc.pause{quantile=0.99}"] != 9.8 {
		t.Errorf("gc.pause{quantile=0.99} = %v, want 9.8", byNameAndQuantile["gc.pause{quantile=0.99}"])
	}
}

func TestHandleMetrics_AcceptsOTLPJSON(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	// OTLP/JSON uses the same message shape, just proto3 JSON encoding —
	// field names are camelCase and byte fields (trace/span IDs) are base64.
	jsonBody := []byte(`{
		"resourceMetrics": [{
			"resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "json-client"}}]},
			"scopeMetrics": [{
				"metrics": [{
					"name": "requests",
					"gauge": {"dataPoints": [{"timeUnixNano": "1700000000000000000", "asInt": "7"}]}
				}]
			}]
		}]
	}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("response Content-Type = %q, want application/json (should match the request's own codec)", ct)
	}
	if len(sink.metrics) != 1 || sink.metrics[0].Value != 7 {
		t.Fatalf("got %+v, want exactly one metric with value 7", sink.metrics)
	}
	if got := sink.metrics[0].Labels["service.name"]; got != "json-client" {
		t.Errorf("service.name label = %q, want json-client", got)
	}
}

func TestHandleMetrics_RejectsInvalidJSONBody(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// histogramRequest builds one OTLP request carrying a single histogram data
// point with the given number of explicit bounds — the shape that made a
// small body expand into millions of metrics.
func histogramRequest(buckets int) *collectormetrics.ExportMetricsServiceRequest {
	bounds := make([]float64, buckets)
	counts := make([]uint64, buckets+1)
	for i := range bounds {
		bounds[i] = float64(i)
		counts[i] = 1
	}
	return &collectormetrics.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{
					Name: "http_duration",
					Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{
						DataPoints: []*metricspb.HistogramDataPoint{{
							TimeUnixNano:   uint64(time.Now().UnixNano()),
							Count:          uint64(buckets),
							ExplicitBounds: bounds,
							BucketCounts:   counts,
						}},
					}},
				}},
			}},
		}},
	}
}

func TestHistogramBucketFanOutIsBounded(t *testing.T) {
	// 200,000 bounds is a ~3MB body, well inside the 32MiB read limit, and
	// used to flatten to 200,001 metrics each carrying its own label map.
	metrics, truncated := metricsFromOTLP(histogramRequest(200_000))
	if truncated {
		t.Fatalf("one data point should not trip the per-request cap; the per-point cap should have contained it first")
	}
	// _count plus the capped buckets. Sum/min/max are absent on this fixture.
	if want := 1 + maxBucketsPerPoint; len(metrics) != want {
		t.Fatalf("flattened to %d metrics, want %d (1 count + %d capped buckets)", len(metrics), want, maxBucketsPerPoint)
	}
}

func TestPerRequestMetricCapRejectsRatherThanTruncates(t *testing.T) {
	// Many data points, each individually under the per-point cap, still add
	// up. Storing a truncated prefix would silently corrupt somebody's data,
	// so the handler must refuse the request instead.
	points := make([]*metricspb.HistogramDataPoint, 0, 400)
	bounds := make([]float64, maxBucketsPerPoint)
	counts := make([]uint64, maxBucketsPerPoint+1)
	for i := range bounds {
		bounds[i] = float64(i)
		counts[i] = 1
	}
	for i := 0; i < 400; i++ {
		points = append(points, &metricspb.HistogramDataPoint{
			TimeUnixNano:   uint64(time.Now().UnixNano()),
			ExplicitBounds: bounds,
			BucketCounts:   counts,
		})
	}
	req := &collectormetrics.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{
					Name: "http_duration",
					Data: &metricspb.Metric_Histogram{Histogram: &metricspb.Histogram{DataPoints: points}},
				}},
			}},
		}},
	}
	metrics, truncated := metricsFromOTLP(req)
	if !truncated {
		t.Fatalf("flattened %d metrics without reporting truncation", len(metrics))
	}
	if len(metrics) != maxMetricsPerRequest {
		t.Errorf("returned %d metrics, want exactly the cap %d", len(metrics), maxMetricsPerRequest)
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sink := &fakeSink{}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/metrics", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	NewHandler(sink, sink, sink).handleMetrics(rec, httpReq)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if len(sink.metrics) != 0 {
		t.Errorf("stored %d metrics from a rejected request, want 0 — a partial write is the thing this prevents", len(sink.metrics))
	}
}

func TestFutureTimestampsAreClamped(t *testing.T) {
	// A timestamp past the retention horizon would never be pruned, making the
	// point immortal and pinning the dashboard's latest value to it.
	now := time.Now()
	far := uint64(now.Add(72 * time.Hour).UnixNano())
	if got := pointTime(far, now); got.After(now.Add(maxFutureSkew)) {
		t.Errorf("pointTime kept a far-future timestamp %v; want it clamped to about %v", got, now)
	}
	// A uint64 too large for int64 must not wrap to a negative time.
	if got := pointTime(math.MaxUint64, now); got.Before(now.Add(-time.Second)) {
		t.Errorf("pointTime(MaxUint64) = %v, want ~now rather than a wrapped negative", got)
	}
	// A normal timestamp is untouched.
	past := now.Add(-30 * time.Second)
	if got := pointTime(uint64(past.UnixNano()), now); !got.Equal(past) {
		t.Errorf("pointTime altered an ordinary timestamp: %v != %v", got, past)
	}
}

// TestNumberDataPointsClampsFarFutureTimestamp is a regression test:
// numberDataPoints (the Gauge/Sum path) used to convert TimeUnixNano
// straight to a time.Time with no clamp, unlike every other OTLP point
// type. A far-future Gauge would then survive every retention pass forever
// (memory.go prunes by timestamp) and pin the dashboard's "latest value" to
// it permanently.
func TestNumberDataPointsClampsFarFutureTimestamp(t *testing.T) {
	now := time.Now()
	far := uint64(now.Add(72 * time.Hour).UnixNano())
	points := []*metricspb.NumberDataPoint{
		{TimeUnixNano: far, Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 12.5}},
	}

	out := numberDataPoints("cpu.percent", points, nil)
	if len(out) != 1 {
		t.Fatalf("got %d metrics, want 1", len(out))
	}
	if out[0].Timestamp.After(now.Add(maxFutureSkew)) {
		t.Errorf("Gauge/Sum timestamp = %v, want clamped to about %v", out[0].Timestamp, now)
	}
}

// TestSpanFromOTLPClampsFarFutureStartAndDuration is a regression test:
// spanFromOTLP used to convert both timestamps straight from the wire, with
// no clamp. A far-future Start would be immortal the same way a far-future
// Gauge is; a wire value above MaxInt64 wrapped to a time near the Unix
// epoch on conversion, so Duration (end.Sub(start)) came out as a huge
// negative number instead of an error — garbage a consumer would have had
// no reason to guard against.
func TestSpanFromOTLPClampsFarFutureStartAndDuration(t *testing.T) {
	now := time.Now()

	// A far-future Start must be clamped, the same as any other timestamp.
	far := uint64(now.Add(72 * time.Hour).UnixNano())
	s := &tracepb.Span{StartTimeUnixNano: far, EndTimeUnixNano: far + uint64(time.Second)}
	got := spanFromOTLP(s, "svc")
	if got.Start.After(now.Add(maxFutureSkew)) {
		t.Errorf("span Start = %v, want clamped to about %v", got.Start, now)
	}
	if got.Duration < 0 {
		t.Errorf("span Duration = %v, want >= 0", got.Duration)
	}

	// EndTimeUnixNano above MaxInt64 used to wrap to ~epoch on a raw
	// int64 conversion, making end.Sub(start) a huge negative Duration.
	overflow := &tracepb.Span{
		StartTimeUnixNano: uint64(now.Add(-time.Second).UnixNano()),
		EndTimeUnixNano:   math.MaxUint64,
	}
	gotOverflow := spanFromOTLP(overflow, "svc")
	if gotOverflow.Duration < 0 {
		t.Errorf("span Duration = %v, want >= 0 (an end above MaxInt64 must not wrap into a huge negative duration)", gotOverflow.Duration)
	}

	// Even with no overflow involved, an end that precedes start (both
	// otherwise ordinary timestamps) must not produce a negative Duration.
	inverted := &tracepb.Span{
		StartTimeUnixNano: uint64(now.Add(4 * time.Minute).UnixNano()),
		EndTimeUnixNano:   uint64(now.Add(1 * time.Minute).UnixNano()),
	}
	gotInverted := spanFromOTLP(inverted, "svc")
	if gotInverted.Duration != 0 {
		t.Errorf("span Duration = %v, want 0 (end before start must clamp, not go negative)", gotInverted.Duration)
	}
}

// TestPerRequestSpanCapRejectsRatherThanTruncates is a regression test:
// unlike /v1/metrics and /v1/logs, /v1/traces had no per-request cap at
// all — a minimal Span message is small enough that the 32MiB body-read
// limit every /v1/* endpoint already applies decodes to on the order of
// ten million spans. Same contract as the metrics/logs caps: reject the
// whole request rather than silently store a truncated prefix.
func TestPerRequestSpanCapRejectsRatherThanTruncates(t *testing.T) {
	spans := make([]*tracepb.Span, 0, maxSpansPerRequest+1)
	for i := 0; i < maxSpansPerRequest+1; i++ {
		spans = append(spans, &tracepb.Span{
			StartTimeUnixNano: uint64(time.Now().UnixNano()),
		})
	}
	req := &collectortrace.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}},
		}},
	}
	got, truncated := spansFromOTLP(req)
	if !truncated {
		t.Fatalf("flattened %d spans without reporting truncation", len(got))
	}
	if len(got) != maxSpansPerRequest {
		t.Errorf("returned %d spans, want exactly the cap %d", len(got), maxSpansPerRequest)
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sink := &fakeSink{}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	NewHandler(sink, sink, sink).handleTraces(rec, httpReq)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if len(sink.spans) != 0 {
		t.Errorf("stored %d spans from a rejected request, want 0 — a partial write is the thing this prevents", len(sink.spans))
	}
}

func TestHandleLogs_MapsLogFields(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	req := &collectorlogs.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{stringAttr("service.name", "checkout-api")},
				},
				ScopeLogs: []*logspb.ScopeLogs{
					{
						LogRecords: []*logspb.LogRecord{
							{
								TimeUnixNano:   1_700_000_000_000_000_000,
								SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_ERROR,
								SeverityText:   "ERROR",
								Body:           &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "payment failed"}},
								Attributes:     []*commonpb.KeyValue{stringAttr("order.id", "42")},
							},
						},
					},
				},
			},
		},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(sink.logs) != 1 {
		t.Fatalf("got %d logs, want 1", len(sink.logs))
	}
	got := sink.logs[0]
	if got.Message != "payment failed" || got.Source != "checkout-api" || got.Severity != model.LogSeverityError {
		t.Errorf("log = %+v, want message=payment failed source=checkout-api severity=error", got)
	}
	if got.Labels["order.id"] != "42" {
		t.Errorf("labels = %+v, want order.id=42", got.Labels)
	}
	wantTS := time.Unix(0, 1_700_000_000_000_000_000)
	if !got.Timestamp.Equal(wantTS) {
		t.Errorf("timestamp = %v, want %v", got.Timestamp, wantTS)
	}
}

func TestHandleLogs_FallsBackToLoggerName(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	req := &collectorlogs.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			ScopeLogs: []*logspb.ScopeLogs{{
				Scope: &commonpb.InstrumentationScope{Name: "app.logger"},
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano:   1_700_000_000_000_000_000,
					SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_WARN,
					Body:           &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "slow query"}},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(sink.logs) != 1 {
		t.Fatalf("got %d logs, want 1", len(sink.logs))
	}
	got := sink.logs[0]
	if got.Source != "app.logger" || got.Severity != model.LogSeverityWarn {
		t.Errorf("log = %+v, want source=app.logger severity=warn", got)
	}
}

func TestHandleLogs_AcceptsOTLPJSON(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	jsonBody := []byte(`{
		"resourceLogs": [{
			"resource": {"attributes": [{"key": "service.name", "value": {"stringValue": "json-logger"}}]},
			"scopeLogs": [{
				"logRecords": [{
					"timeUnixNano": "1700000000000000000",
					"severityNumber": 9,
					"severityText": "INFO",
					"body": {"stringValue": "hello from json"}
				}]
			}]
		}]
	}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("response Content-Type = %q, want application/json", ct)
	}
	if len(sink.logs) != 1 {
		t.Fatalf("got %d logs, want 1", len(sink.logs))
	}
	got := sink.logs[0]
	if got.Message != "hello from json" || got.Source != "json-logger" || got.Severity != model.LogSeverityInfo {
		t.Errorf("log = %+v, want message=hello from json source=json-logger severity=info", got)
	}
}

func TestHandleLogs_RejectsInvalidBody(t *testing.T) {
	sink := &fakeSink{}
	h := NewHandler(sink, sink, sink)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader([]byte("not protobuf")))
	mux.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestPerRequestLogCapRejectsRatherThanTruncates(t *testing.T) {
	records := make([]*logspb.LogRecord, 0, maxLogsPerRequest+1)
	for i := 0; i < maxLogsPerRequest+1; i++ {
		records = append(records, &logspb.LogRecord{
			TimeUnixNano: uint64(time.Now().UnixNano()),
			Body:         &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "x"}},
		})
	}
	req := &collectorlogs.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{
			ScopeLogs: []*logspb.ScopeLogs{{LogRecords: records}},
		}},
	}
	logs, truncated := logsFromOTLP(req)
	if !truncated {
		t.Fatalf("flattened %d logs without reporting truncation", len(logs))
	}
	if len(logs) != maxLogsPerRequest {
		t.Errorf("returned %d logs, want exactly the cap %d", len(logs), maxLogsPerRequest)
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sink := &fakeSink{}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	NewHandler(sink, sink, sink).handleLogs(rec, httpReq)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if len(sink.logs) != 0 {
		t.Errorf("stored %d logs from a rejected request, want 0", len(sink.logs))
	}
}

func TestLogSeverityMapping(t *testing.T) {
	cases := []struct {
		num  logspb.SeverityNumber
		text string
		want model.LogSeverity
	}{
		{logspb.SeverityNumber_SEVERITY_NUMBER_DEBUG, "", model.LogSeverityDebug},
		{logspb.SeverityNumber_SEVERITY_NUMBER_INFO, "", model.LogSeverityInfo},
		{logspb.SeverityNumber_SEVERITY_NUMBER_WARN, "", model.LogSeverityWarn},
		{logspb.SeverityNumber_SEVERITY_NUMBER_ERROR, "", model.LogSeverityError},
		{logspb.SeverityNumber_SEVERITY_NUMBER_FATAL, "", model.LogSeverityError},
		{0, "warning", model.LogSeverityWarn},
		{0, "FATAL", model.LogSeverityError},
		{0, "", model.LogSeverityInfo},
	}
	for _, tc := range cases {
		if got := logSeverity(tc.num, tc.text); got != tc.want {
			t.Errorf("logSeverity(%v, %q) = %q, want %q", tc.num, tc.text, got, tc.want)
		}
	}
}

// TestNumberDataPointValue_DropsNonFinite. A double on the OTLP wire can be
// NaN or +/-Inf, and nothing downstream survives one: json.Marshal fails on
// the whole metrics response, the Badger batch cannot encode the record, and
// Forseer's Welford state for that series is poisoned permanently. Dropping
// the point is the only reading that is not a lie — reporting 0 would invent
// a measurement nobody took.
func TestNumberDataPointValue_DropsNonFinite(t *testing.T) {
	for _, tc := range []struct {
		name   string
		dp     *metricspb.NumberDataPoint
		want   float64
		wantOK bool
	}{
		{"NaN", &metricspb.NumberDataPoint{Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: math.NaN()}}, 0, false},
		{"+Inf", &metricspb.NumberDataPoint{Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: math.Inf(1)}}, 0, false},
		{"-Inf", &metricspb.NumberDataPoint{Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: math.Inf(-1)}}, 0, false},
		{"ordinary double", &metricspb.NumberDataPoint{Value: &metricspb.NumberDataPoint_AsDouble{AsDouble: 12.5}}, 12.5, true},
		{"int is finite by construction", &metricspb.NumberDataPoint{Value: &metricspb.NumberDataPoint_AsInt{AsInt: 7}}, 7, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := numberDataPointValue(tc.dp)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Errorf("numberDataPointValue = (%v, %v), want (%v, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
