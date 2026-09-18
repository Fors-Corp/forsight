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

// TestEngine_CulpritRanksByChangeNotRawCPU is the scenario ROADMAP item 22
// exists for: "a process that went from 2% to 18% during the spike is
// dropped for one that always sits at 22%." Once each process has enough
// history for its own baseline, the mover — a genuine jump above its own
// normal — must be named a culprit even though its raw CPU is lower, and
// the steady one — sitting exactly at its own normal — must not be, even
// though its raw CPU clears the old 20% floor.
func TestEngine_CulpritRanksByChangeNotRawCPU(t *testing.T) {
	e := NewEngine()

	for i := 0; i < minSamples; i++ {
		e.ObserveMetrics([]Point{{Name: "host.cpu.percent", Value: 8}})
	}

	// Warm up both processes' own baselines: "mover" idles low with a
	// little jitter, "steady" idles high with a little jitter. Neither
	// baseline is degenerate (zero variance), which is what real readings
	// from a real process look like and what SeriesBaseline requires to be
	// ready at all.
	mover := []float64{1, 2, 3, 2, 1, 3, 2, 1, 3, 2, 1, 2}
	steady := []float64{21, 23, 21, 23, 22, 23, 21, 22, 23, 21, 22, 23}
	if len(mover) != minSamples || len(steady) != minSamples {
		t.Fatalf("test setup: need exactly minSamples=%d points to warm each baseline", minSamples)
	}
	for i := range mover {
		e.ObserveMetrics([]Point{
			{Name: "process.cpu.percent", Value: mover[i], Labels: map[string]string{"pid": "7", "name": "mover"}},
			{Name: "process.cpu.percent", Value: steady[i], Labels: map[string]string{"pid": "9", "name": "steady"}},
		})
	}

	// The host goes anomalous. "mover" jumps to 18% — a real jump above its
	// own baseline (~2%) — while "steady" holds at 22%, squarely inside its
	// own baseline (~22%). Raw CPU alone would rank steady over mover.
	e.ObserveMetrics([]Point{
		{Name: "host.cpu.percent", Value: 95},
		{Name: "process.cpu.percent", Value: 18, Labels: map[string]string{"pid": "7", "name": "mover"}},
		{Name: "process.cpu.percent", Value: 22, Labels: map[string]string{"pid": "9", "name": "steady"}},
	})

	var sawMover, sawSteady bool
	for _, ins := range e.Insights() {
		if ins.Kind != KindCulprit {
			continue
		}
		switch ins.Source {
		case "mover":
			sawMover = true
		case "steady":
			sawSteady = true
		}
	}
	if !sawMover {
		t.Fatalf("mover (2%%->18%%, a real jump off its own baseline) was not ranked a culprit: %+v", e.Insights())
	}
	if sawSteady {
		t.Fatalf("steady (sitting at its own normal 22%%) was ranked a culprit merely for a high raw CPU: %+v", e.Insights())
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
	// Repeated to spanP99MinSamples, not minSamples: a p99 marker needs far
	// more than a median does before it is worth judging against.
	baseline := []float64{18, 19, 20, 21, 22, 20, 19, 21, 18, 22, 20, 19}
	for i := 0; i < spanP99MinSamples; i++ {
		e.ObserveSpans([]SpanSample{{Name: "GET /checkout", Service: "api", DurationMs: baseline[i%len(baseline)]}})
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
