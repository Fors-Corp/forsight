package promscrape

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// realExposition is genuine Prometheus text-exposition-format output (the
// same shape node_exporter or any Prometheus client library would produce),
// covering all four metric types this collector flattens.
const realExposition = `# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="get",code="200"} 1027

# HELP process_cpu_seconds Process CPU time
# TYPE process_cpu_seconds gauge
process_cpu_seconds 0.42

# HELP request_duration_seconds Request latency
# TYPE request_duration_seconds histogram
request_duration_seconds_bucket{le="0.1"} 5
request_duration_seconds_bucket{le="0.5"} 12
request_duration_seconds_bucket{le="+Inf"} 15
request_duration_seconds_sum 3.2
request_duration_seconds_count 15

# HELP rpc_duration_seconds RPC latency
# TYPE rpc_duration_seconds summary
rpc_duration_seconds{quantile="0.5"} 0.05
rpc_duration_seconds{quantile="0.99"} 0.31
rpc_duration_seconds_sum 12.3
rpc_duration_seconds_count 240
`

func TestCollect_ParsesRealExpositionFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(realExposition))
	}))
	defer srv.Close()

	c := New([]Target{{URL: srv.URL, Labels: map[string]string{"job": "test-target"}}})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byKey := map[string]float64{}
	for _, m := range metrics {
		key := m.Name
		if le, ok := m.Labels["le"]; ok {
			key += "{le=" + le + "}"
		}
		if q, ok := m.Labels["quantile"]; ok {
			key += "{quantile=" + q + "}"
		}
		byKey[key] = m.Value
		if m.Labels["job"] != "test-target" {
			t.Errorf("metric %q missing the target's own 'job' label: %+v", m.Name, m.Labels)
		}
	}

	tests := []struct {
		key  string
		want float64
	}{
		{"http_requests_total", 1027},
		{"process_cpu_seconds", 0.42},
		{"request_duration_seconds_bucket{le=0.1}", 5},
		{"request_duration_seconds_bucket{le=+Inf}", 15},
		{"request_duration_seconds_sum", 3.2},
		{"request_duration_seconds_count", 15},
		{"rpc_duration_seconds{quantile=0.5}", 0.05},
		{"rpc_duration_seconds{quantile=0.99}", 0.31},
		{"rpc_duration_seconds_count", 240},
	}
	for _, tt := range tests {
		got, ok := byKey[tt.key]
		if !ok {
			t.Errorf("missing metric %q; got keys: %v", tt.key, keys(byKey))
			continue
		}
		if got != tt.want {
			t.Errorf("%s = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestCollect_OneDownTargetDoesNotDropOthers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("up 1\n"))
	}))
	defer srv.Close()

	c := New([]Target{
		{URL: "http://127.0.0.1:1/nonexistent"}, // nothing listens here
		{URL: srv.URL},
	})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Name != "up" {
		t.Fatalf("got %+v, want exactly the reachable target's one metric", metrics)
	}
}

// TestCollect_ScrapeBodyIsBounded is a regression test: scrapeOne used to
// hand the parser resp.Body directly, with no limit. The exposition text
// format has no end-of-message marker but EOF, so a target that never stops
// sending (an unbounded or malicious response, transparently gzip-inflated
// by the default transport — a "gzip bomb") made the parse call block, and
// grow memory, forever. A LimitReader is what makes it return instead.
func TestCollect_ScrapeBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		line := []byte("# a filler comment line, repeated forever\n")
		for {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := w.Write(line); err != nil {
				return // the client stopped reading — exactly what this test wants
			}
		}
	}))
	defer srv.Close()

	c := New([]Target{{URL: srv.URL}})
	done := make(chan struct{})
	go func() {
		_, _ = c.Collect(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Collect did not return within 5s — an unbounded scrape body was not capped")
	}
}

// TestCollect_PerTargetMetricCapSkipsRatherThanTruncates is a regression
// test: scrapeOne used to flatten every family into one target's output
// with no cap, the same unbounded-fan-out shape the OTLP receiver already
// guards against (a small body can still expand past its byte size, e.g.
// via histogram buckets). Storing a truncated prefix would silently drop
// data, so a target over the cap must be skipped entirely instead — the
// same contract Collect already gives an unreachable target.
func TestCollect_PerTargetMetricCapSkipsRatherThanTruncates(t *testing.T) {
	var body strings.Builder
	body.WriteString("# TYPE flood gauge\n")
	for i := 0; i < maxScrapeMetricsPerTarget+1; i++ {
		fmt.Fprintf(&body, "flood{n=\"%d\"} 1\n", i)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(body.String()))
	}))
	defer srv.Close()

	c := New([]Target{{URL: srv.URL}})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(metrics) != 0 {
		t.Fatalf("got %d metrics from a target over the per-target cap, want 0 (a partial write is the thing this prevents)", len(metrics))
	}
}

// TestCollect_DoesNotFollowRedirects is a regression test: the scrape
// client had no CheckRedirect override, so the default net/http behaviour
// (follow up to 10 redirects) applied. A scrape target has no reason to
// answer with a redirect; auto-discovery (DiscoverLocal) adds targets the
// operator never explicitly listed, so following one sends this agent's
// request to a host nobody configured.
func TestCollect_DoesNotFollowRedirects(t *testing.T) {
	var redirectTargetHit bool
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectTargetHit = true
		_, _ = w.Write([]byte("up 1\n"))
	}))
	defer redirectTarget.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer srv.Close()

	c := New([]Target{{URL: srv.URL}})
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if redirectTargetHit {
		t.Error("the redirect was followed; a scrape target must not be able to send this agent's request elsewhere")
	}
	if len(metrics) != 0 {
		t.Errorf("got %d metrics from a redirect response, want 0", len(metrics))
	}
}

func TestCollect_NoTargetsReturnsNothing(t *testing.T) {
	c := New(nil)
	metrics, err := c.Collect(context.Background())
	if err != nil || metrics != nil {
		t.Fatalf("Collect() = %v, %v; want nil, nil", metrics, err)
	}
}

func TestName(t *testing.T) {
	if got := New(nil).Name(); got != "promscrape" {
		t.Errorf("Name() = %q, want promscrape", got)
	}
}

func keys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
