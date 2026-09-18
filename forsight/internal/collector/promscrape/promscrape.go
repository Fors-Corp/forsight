// Package promscrape pulls metrics from any Prometheus-instrumented HTTP
// endpoint — the exposition format node_exporter, cAdvisor, and a huge
// fraction of existing infrastructure software already speak natively, so
// forsight can ingest all of it with no instrumentation of its own. Parsing
// uses the official github.com/prometheus/common/expfmt library rather than
// a hand-rolled line parser: label-value escaping (`\"`, `\\`, `\n`) and the
// HELP/TYPE comment grammar have real edge cases that library already
// handles correctly.
package promscrape

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	fsmodel "github.com/marcfs31/forsight/forsight/internal/model"
)

const (
	// maxScrapeBodyBytes bounds how much of a target's response body the
	// parser is handed, the same way every other HTTP response this repo
	// reads is bounded (probe.go's maxProbeBodyBytes, the OTLP receiver's
	// 32MiB request cap, mlaas/client.go's maxResponseBytes). resp.Body is
	// already the DECOMPRESSED stream — Go's transport applies
	// Accept-Encoding: gzip and inflates it transparently when no Transport
	// override disables that, as here — so this is what actually bounds a
	// gzip bomb from a compromised or malicious exporter, not the size of
	// the bytes that came in over the wire.
	maxScrapeBodyBytes = 8 << 20
	// maxScrapeMetricsPerTarget bounds one target's flattened output, for
	// the same reason the OTLP receiver bounds its own (see its package
	// doc): a histogram or summary's buckets/quantiles fan out one metric
	// each, so a compact body can still expand well past what its byte size
	// suggests. Same order of magnitude as the OTLP receiver's per-request
	// caps.
	maxScrapeMetricsPerTarget = 200_000
)

// Target is one endpoint to scrape on the collector's shared interval.
type Target struct {
	// URL of the target's exposition endpoint, e.g. "http://localhost:9100/metrics".
	URL string
	// Labels attached to every metric scraped from this target (e.g. "job": "node").
	Labels map[string]string
}

// Collector scrapes a fixed set of Prometheus exposition-format targets.
type Collector struct {
	targets []Target
	client  *http.Client
}

// New builds a Collector for the given targets. An empty target list is
// valid — Collect then simply returns nothing every tick, the same graceful
// "nothing configured" shape as forsight's other optional collectors.
func New(targets []Target) *Collector {
	return &Collector{targets: targets, client: &http.Client{
		Timeout: 10 * time.Second,
		// A scrape target has no reason to answer with a redirect; treating
		// one as the final response (mlaas/client.go's noRedirectClient does
		// the same, for the same reason) avoids following it — and replaying
		// the request — to a host the operator never configured.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *Collector) Name() string { return "promscrape" }

func (c *Collector) Collect(ctx context.Context) ([]fsmodel.Metric, error) {
	var out []fsmodel.Metric
	for _, t := range c.targets {
		metrics, err := c.scrapeOne(ctx, t)
		if err != nil {
			// One unreachable target (a restarting exporter, a typo'd URL)
			// shouldn't drop metrics from every other configured target.
			continue
		}
		out = append(out, metrics...)
	}
	return out, nil
}

func (c *Collector) scrapeOne(ctx context.Context, t Target) ([]fsmodel.Metric, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("promscrape: %s returned %d", t.URL, resp.StatusCode)
	}

	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(io.LimitReader(resp.Body, maxScrapeBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("promscrape: parsing %s: %w", t.URL, err)
	}

	now := time.Now()
	var out []fsmodel.Metric
	for name, mf := range families {
		for _, m := range mf.GetMetric() {
			out = append(out, metricsFromFamily(name, m, t.Labels, now)...)
			if len(out) > maxScrapeMetricsPerTarget {
				// Half of one target's metrics is worse than none this
				// tick: Collect treats any error here the same as an
				// unreachable target and simply skips it, the way the OTLP
				// receiver refuses a request rather than store a truncated
				// prefix.
				return nil, fmt.Errorf("promscrape: %s exceeds the per-target metric limit of %d", t.URL, maxScrapeMetricsPerTarget)
			}
		}
	}
	return out, nil
}

// metricsFromFamily flattens one Prometheus metric point to forsight's flat
// model — Counter/Gauge/Untyped map to a single point; Histogram and
// Summary use the exact same _count/_sum/_bucket{le=...}/{quantile=...}
// naming this repo's OTLP receiver already produces for its own
// Histogram/Summary types (internal/collector/otlp/receiver.go), so a
// dashboard query doesn't need to care which protocol a given series came
// in on. Prometheus's own bucket counts are already cumulative (unlike
// OTLP's), so they're passed straight through with no running-sum step.
func metricsFromFamily(name string, m *dto.Metric, extraLabels map[string]string, now time.Time) []fsmodel.Metric {
	ts := now
	if m.GetTimestampMs() != 0 {
		ts = time.UnixMilli(m.GetTimestampMs())
	}
	labels := mergeLabels(extraLabels, labelPairsToMap(m.GetLabel()))

	switch {
	case m.Counter != nil:
		return []fsmodel.Metric{{Name: name, Value: m.GetCounter().GetValue(), Timestamp: ts, Labels: labels}}
	case m.Gauge != nil:
		return []fsmodel.Metric{{Name: name, Value: m.GetGauge().GetValue(), Timestamp: ts, Labels: labels}}
	case m.Untyped != nil:
		return []fsmodel.Metric{{Name: name, Value: m.GetUntyped().GetValue(), Timestamp: ts, Labels: labels}}
	case m.Summary != nil:
		return summaryMetrics(name, m.GetSummary(), labels, ts)
	case m.Histogram != nil:
		return histogramMetrics(name, m.GetHistogram(), labels, ts)
	default:
		return nil
	}
}

func summaryMetrics(name string, s *dto.Summary, labels map[string]string, ts time.Time) []fsmodel.Metric {
	out := []fsmodel.Metric{
		{Name: name + "_count", Value: float64(s.GetSampleCount()), Timestamp: ts, Labels: labels},
		{Name: name + "_sum", Value: s.GetSampleSum(), Timestamp: ts, Labels: labels},
	}
	for _, q := range s.GetQuantile() {
		qLabels := mergeLabels(labels, map[string]string{"quantile": formatFloat(q.GetQuantile())})
		out = append(out, fsmodel.Metric{Name: name, Value: q.GetValue(), Timestamp: ts, Labels: qLabels})
	}
	return out
}

func histogramMetrics(name string, h *dto.Histogram, labels map[string]string, ts time.Time) []fsmodel.Metric {
	out := []fsmodel.Metric{
		{Name: name + "_count", Value: float64(h.GetSampleCount()), Timestamp: ts, Labels: labels},
		{Name: name + "_sum", Value: h.GetSampleSum(), Timestamp: ts, Labels: labels},
	}
	for _, b := range h.GetBucket() {
		bucketLabels := mergeLabels(labels, map[string]string{"le": formatBound(b.GetUpperBound())})
		out = append(out, fsmodel.Metric{Name: name + "_bucket", Value: float64(b.GetCumulativeCount()), Timestamp: ts, Labels: bucketLabels})
	}
	return out
}

func labelPairsToMap(pairs []*dto.LabelPair) map[string]string {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		out[p.GetName()] = p.GetValue()
	}
	return out
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
		out[k] = v // the target's own labels win over any same-named scraped label
	}
	return out
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

func formatBound(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	return formatFloat(v)
}
