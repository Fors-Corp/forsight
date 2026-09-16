package forseer

import (
	"strconv"
	"testing"
	"time"
)

func TestEngine_MergesMetricAndLogInsights(t *testing.T) {
	e := NewEngine()
	fixed := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	e.now = func() time.Time { return fixed }

	for i := 0; i < minSamples; i++ {
		e.ObserveMetrics([]Point{{Name: "host.cpu.percent", Value: 10}})
	}
	e.ObserveMetrics([]Point{{Name: "host.cpu.percent", Value: 90}})

	lines := make([]LogLine, burstMinCount)
	for i := range lines {
		lines[i] = LogLine{Timestamp: fixed, Source: "api", Severity: "error", Message: "boom"}
	}
	e.ObserveLogs(lines)

	got := e.Insights()
	kinds := map[string]bool{}
	for _, ins := range got {
		kinds[ins.Kind] = true
	}
	if !kinds[KindAnomaly] {
		t.Fatalf("missing anomaly: %+v", got)
	}
	if !kinds[KindLogBurst] {
		t.Fatalf("missing log burst: %+v", got)
	}
}

func TestEngine_CulpritRanksHotProcess(t *testing.T) {
	e := NewEngine()
	for i := 0; i < minSamples; i++ {
		e.ObserveMetrics([]Point{{Name: "host.cpu.percent", Value: 8}})
	}
	e.ObserveMetrics([]Point{
		{Name: "host.cpu.percent", Value: 95},
		{Name: "process.cpu.percent", Value: 80, Labels: map[string]string{"pid": "7", "name": "rogue"}},
		{Name: "process.cpu.percent", Value: 1, Labels: map[string]string{"pid": "1", "name": "idle"}},
	})
	found := false
	for _, ins := range e.Insights() {
		if ins.Kind == KindCulprit && ins.Source == "rogue" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected culprit insight for rogue: %+v", e.Insights())
	}
}

// TestEngine_PrunesStaleProcesses guards against the same unbounded-growth
// bug PR #44 fixed on the OTLP ingest path, recurring here: unlike
// e.culprits, e.processes was never evicted, and pid resource attributes
// reach it verbatim off an unauthenticated OTLP POST. A burst of distinct
// pids must grow the map (it's supposed to track what it sees), but once
// insightTTL has passed with no further sighting, those entries must be
// swept — the same TTL discipline already applied to culprits, on the same
// tick.
func TestEngine_PrunesStaleProcesses(t *testing.T) {
	e := NewEngine()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	e.now = func() time.Time { return now }

	const burst = 5000
	for i := 0; i < burst; i++ {
		e.ObserveMetrics([]Point{{
			Name:   "process.cpu.percent",
			Value:  1,
			Labels: map[string]string{"pid": strconv.Itoa(i), "name": "p"},
		}})
	}
	e.mu.Lock()
	got := len(e.processes)
	e.mu.Unlock()
	if got != burst {
		t.Fatalf("processes = %d, want %d right after the burst", got, burst)
	}

	// Advance the clock past insightTTL and feed one more point. Every pid
	// from the burst is now stale and must be swept in this same tick,
	// leaving only the one just observed — bounded, not the unbounded
	// burst size.
	now = now.Add(insightTTL + time.Minute)
	e.ObserveMetrics([]Point{{
		Name:   "process.cpu.percent",
		Value:  1,
		Labels: map[string]string{"pid": "fresh", "name": "p"},
	}})

	e.mu.Lock()
	got = len(e.processes)
	e.mu.Unlock()
	if got != 1 {
		t.Fatalf("processes after TTL sweep = %d, want 1 (bounded, stale pids evicted)", got)
	}
}

func TestEngine_SlowSpan(t *testing.T) {
	e := NewEngine()
	// A little jitter, not a single repeated value: a zero-spread baseline
	// makes the very first outlier redefine the interquartile spread too,
	// which is a degenerate case of its own and not what this test is for.
	baseline := []float64{18, 19, 20, 21, 22, 20, 19, 21, 18, 22, 20, 19}
	if len(baseline) != minSamples {
		t.Fatalf("test setup: baseline has %d points, want minSamples=%d", len(baseline), minSamples)
	}
	for _, d := range baseline {
		e.ObserveSpans([]SpanSample{{Name: "GET /checkout", Service: "api", DurationMs: d}})
	}

	// p99 fires on about one span in a hundred by construction, so a single
	// exceedance must not open anything — it takes a run.
	for i := 0; i < spanExceedRun-1; i++ {
		e.ObserveSpans([]SpanSample{{Name: "GET /checkout", Service: "api", DurationMs: 23, TraceID: "abc"}})
	}
	for _, ins := range e.Insights() {
		if ins.Kind == KindSlowSpan {
			t.Fatalf("slow_span opened after only %d of %d exceedances: %+v", spanExceedRun-1, spanExceedRun, ins)
		}
	}

	// The span that completes the run opens it, and an error status on that
	// span escalates it straight to critical.
	e.ObserveSpans([]SpanSample{{Name: "GET /checkout", Service: "api", DurationMs: 23, TraceID: "abc", Status: "error"}})
	found := false
	for _, ins := range e.Insights() {
		if ins.Kind == KindSlowSpan {
			found = true
			if ins.Severity != SeverityCritical {
				t.Errorf("severity = %q, want critical (error status on the span that completed the run)", ins.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("expected slow_span after a run of %d exceedances: %+v", spanExceedRun, e.Insights())
	}
}

func TestEngine_TrainsSeverityOnlyOnDeclaredLevels(t *testing.T) {
	e := NewEngine()

	// A tailed file: the agent guessed the level, so this teaches nothing.
	e.ObserveLogs([]LogLine{{
		Message:          "no errors reported during the sweep",
		Severity:         "error",
		Source:           "/var/log/app.log",
		SeverityInferred: true,
	}})

	if trained := e.Models()[0].Trained; trained != 0 {
		t.Fatalf("engine trained on %d inferred severities; it would learn the rule it replaces", trained)
	}

	// OTLP: the application declared the level, so this is a real example.
	e.ObserveLogs([]LogLine{{
		Message:  "could not reach the database",
		Severity: "error",
		Source:   "checkout",
	}})

	if trained := e.Models()[0].Trained; trained != 1 {
		t.Fatalf("engine trained on %d declared severities, want 1", trained)
	}
}

func TestEngine_ClassifySeverityDefersUntilTheModelIsReady(t *testing.T) {
	e := NewEngine()

	if _, ok := e.ClassifySeverity("panic: nil map write"); ok {
		t.Fatal("a cold engine answered; the caller must keep its fallback")
	}

	for i := 0; i < 60; i++ {
		e.ObserveLogs([]LogLine{
			{Message: "request completed cleanly", Severity: "info", Source: "api"},
			{Message: "no errors reported during the sweep", Severity: "info", Source: "api"},
			{Message: "panic nil map write in handler", Severity: "error", Source: "api"},
			{Message: "could not reach the database cluster", Severity: "error", Source: "api"},
		})
	}

	got, ok := e.ClassifySeverity("panic nil map write in the checkout handler")
	if !ok {
		t.Fatal("a warm engine still declined to classify a clearly learned line")
	}
	if got != "error" {
		t.Errorf("got %q, want error", got)
	}
}

func TestEngine_ModelsDescribeThemselves(t *testing.T) {
	cards := NewEngine().Models()

	if len(cards) == 0 {
		t.Fatal("engine reports no models")
	}
	for _, card := range cards {
		if card.Name == "" || card.Job == "" || len(card.Reads) == 0 || card.Fallback == "" {
			t.Errorf("model %q does not fully describe itself: %+v", card.Name, card)
		}
	}
}
