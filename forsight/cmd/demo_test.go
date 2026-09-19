package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forseer"
	"github.com/marcfs31/forsight/forsight/internal/model"
	"github.com/marcfs31/forsight/forsight/internal/store"
)

func TestDemoRetention(t *testing.T) {
	cases := []struct {
		name               string
		backfill, explicit time.Duration
		want               time.Duration
	}{
		{"explicit always wins", time.Hour, 30 * time.Minute, 30 * time.Minute},
		{"no backfill floors at two hours", 0, 0, 2 * time.Hour},
		{"small backfill still floors at two hours", 10 * time.Minute, 0, 2 * time.Hour},
		{"a long backfill drives retention past the floor", 6 * time.Hour, 0, 7 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := demoRetention(tc.backfill, tc.explicit); got != tc.want {
				t.Errorf("demoRetention(%v, %v) = %v, want %v", tc.backfill, tc.explicit, got, tc.want)
			}
		})
	}
}

func TestNewDemoGenerator_PlacesSpikeWithBaselineAheadOfIt(t *testing.T) {
	t.Run("no backfill: spike sits right after the minimum baseline", func(t *testing.T) {
		g := newDemoGenerator(1, 0)
		if g.spikeAt != demoMinBaseline {
			t.Errorf("spikeAt = %d, want %d (demoMinBaseline) when there is no backfill", g.spikeAt, demoMinBaseline)
		}
	})

	t.Run("a long backfill puts the spike near the end, with baseline ahead of it", func(t *testing.T) {
		steps := 1000
		g := newDemoGenerator(1, steps)
		if g.spikeAt <= demoMinBaseline {
			t.Errorf("spikeAt = %d, want more than the minimum baseline (%d) ahead of it", g.spikeAt, demoMinBaseline)
		}
		if g.spikeAt >= steps {
			t.Errorf("spikeAt = %d, want it to fall within the %d backfilled samples", g.spikeAt, steps)
		}
		// Near the end (90%), not buried in the middle.
		if want := steps - steps/10; g.spikeAt != want {
			t.Errorf("spikeAt = %d, want %d (90%% through a %d-step backfill)", g.spikeAt, want, steps)
		}
	})

	t.Run("a short backfill never places the spike before the baseline is full", func(t *testing.T) {
		g := newDemoGenerator(1, demoMinBaseline+2)
		if g.spikeAt < demoMinBaseline {
			t.Errorf("spikeAt = %d, want >= demoMinBaseline (%d) even for a short backfill", g.spikeAt, demoMinBaseline)
		}
	})

	t.Run("burst follows the spike and the slow trace follows the burst", func(t *testing.T) {
		g := newDemoGenerator(1, 0)
		if g.burstAt <= g.spikeAt {
			t.Errorf("burstAt = %d, want it after spikeAt = %d", g.burstAt, g.spikeAt)
		}
		if g.slowAt <= g.spikeAt {
			t.Errorf("slowAt = %d, want it after spikeAt = %d", g.slowAt, g.spikeAt)
		}
	})
}

func TestDemoGenerator_Tick_Deterministic(t *testing.T) {
	ts := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	a := newDemoGenerator(42, 200)
	b := newDemoGenerator(42, 200)

	for i := 0; i < 50; i++ {
		sa := a.tick(ts.Add(time.Duration(i)*10*time.Second), i)
		sb := b.tick(ts.Add(time.Duration(i)*10*time.Second), i)
		if len(sa.metrics) != len(sb.metrics) || len(sa.logs) != len(sb.logs) || len(sa.spans) != len(sb.spans) {
			t.Fatalf("tick %d: shapes diverged between two generators built from the same seed: %+v vs %+v", i, sa, sb)
		}
		for j := range sa.metrics {
			am, bm := sa.metrics[j], sb.metrics[j]
			if am.Name != bm.Name || am.Value != bm.Value || !am.Timestamp.Equal(bm.Timestamp) {
				t.Fatalf("tick %d metric %d diverged: %+v vs %+v", i, j, am, bm)
			}
		}
	}
}

func TestDemoGenerator_Tick_EverythingIsLabelledSynthetic(t *testing.T) {
	g := newDemoGenerator(1, 0)
	ts := time.Now()
	for i := 0; i < g.spikeAt+g.spikeLen+g.burstLen+10; i++ {
		s := g.tick(ts.Add(time.Duration(i)*10*time.Second), i)
		for _, m := range s.metrics {
			if m.Labels["host"] != demoHost {
				t.Errorf("tick %d: metric %q has host label %q, want %q", i, m.Name, m.Labels["host"], demoHost)
			}
		}
		for _, l := range s.logs {
			found := false
			for _, src := range demoLogSources {
				if l.Source == src {
					found = true
				}
			}
			if !found {
				t.Errorf("tick %d: log source %q is not one of the declared demo sources %v", i, l.Source, demoLogSources)
			}
		}
		for _, sp := range s.spans {
			if sp.Service != demoTraceService {
				t.Errorf("tick %d: span service %q, want %q", i, sp.Service, demoTraceService)
			}
		}
	}
}

func TestDemoGenerator_SpikeAndCulprit(t *testing.T) {
	g := newDemoGenerator(1, 0)
	ts := time.Now()

	// Before the spike: cpu should never read as pegged, and the named
	// culprit process must not appear yet.
	for i := 0; i < g.spikeAt; i++ {
		s := g.tick(ts.Add(time.Duration(i)*10*time.Second), i)
		for _, m := range s.metrics {
			if m.Name == "host.cpu.percent" && m.Value > 60 {
				t.Errorf("tick %d (before the spike): host.cpu.percent = %.1f, want a normal baseline", i, m.Value)
			}
			if m.Labels["pid"] == demoCulpritPID {
				t.Errorf("tick %d (before the spike): the named culprit process already present", i)
			}
		}
	}

	// During the spike: cpu is pegged high and the named culprit shows up
	// with a pid distinct from the steady-state processes, above the 20%
	// threshold engine.trackProcesses ranks culprits against (see
	// forseer/engine.go).
	for i := g.spikeAt; i < g.spikeAt+g.spikeLen; i++ {
		s := g.tick(ts.Add(time.Duration(i)*10*time.Second), i)
		var sawHotHost, sawCulprit bool
		for _, m := range s.metrics {
			if m.Name == "host.cpu.percent" && m.Value >= 60 {
				sawHotHost = true
			}
			if m.Name == "process.cpu.percent" && m.Labels["pid"] == demoCulpritPID {
				sawCulprit = true
				if m.Labels["name"] != demoCulpritName {
					t.Errorf("tick %d: culprit process name = %q, want %q", i, m.Labels["name"], demoCulpritName)
				}
				if m.Value < 20 {
					t.Errorf("tick %d: culprit process cpu = %.1f, want >= 20 (forseer's culprit-ranking floor)", i, m.Value)
				}
			}
		}
		if !sawHotHost {
			t.Errorf("tick %d (during the spike): no host.cpu.percent reading above 60", i)
		}
		if !sawCulprit {
			t.Errorf("tick %d (during the spike): named culprit process metric missing", i)
		}
	}

	// After the spike ends, the culprit process metric stops appearing.
	after := g.tick(ts.Add(time.Duration(g.spikeAt+g.spikeLen)*10*time.Second), g.spikeAt+g.spikeLen)
	for _, m := range after.metrics {
		if m.Labels["pid"] == demoCulpritPID {
			t.Errorf("tick %d (after the spike): culprit process metric still present", g.spikeAt+g.spikeLen)
		}
	}
}

func TestDemoGenerator_LogBurst(t *testing.T) {
	g := newDemoGenerator(1, 0)
	ts := time.Now()

	before := g.tick(ts, g.burstAt-1)
	errCount := func(entries []model.LogEntry) int {
		n := 0
		for _, e := range entries {
			if e.Severity == model.LogSeverityError {
				n++
			}
		}
		return n
	}
	if n := errCount(before.logs); n > 0 {
		t.Errorf("tick %d (before the burst): %d error logs, want 0", g.burstAt-1, n)
	}

	for i := g.burstAt; i < g.burstAt+g.burstLen; i++ {
		s := g.tick(ts.Add(time.Duration(i)*10*time.Second), i)
		if n := errCount(s.logs); n < 3 {
			t.Errorf("tick %d (in the burst): %d error logs, want at least 3", i, n)
		}
	}

	after := g.tick(ts, g.burstAt+g.burstLen)
	if n := errCount(after.logs); n > 0 {
		t.Errorf("tick %d (after the burst): %d error logs, want 0", g.burstAt+g.burstLen, n)
	}
}

// TestDemoGenerator_DeclaredAndInferredSeverity guards the "a few log
// templates including ... declared-severity lines" half of item 20's What:
// the severity model only learns from lines where SeverityInferred is
// false (see forseer.Engine.ObserveLogs), so a demo with only inferred
// lines would train nothing.
func TestDemoGenerator_DeclaredAndInferredSeverity(t *testing.T) {
	g := newDemoGenerator(1, 0)
	ts := time.Now()

	var declared, inferred int
	for i := 0; i < 200; i++ {
		s := g.tick(ts.Add(time.Duration(i)*10*time.Second), i)
		for _, l := range s.logs {
			if l.SeverityInferred {
				inferred++
				if l.Severity == "" {
					t.Errorf("tick %d: inferred log has no severity — model.FallbackSeverity must always return one", i)
				}
			} else {
				declared++
				if l.Severity == "" {
					t.Errorf("tick %d: declared log has an empty severity", i)
				}
			}
		}
	}
	if declared == 0 {
		t.Error("no declared-severity log lines across 200 ticks")
	}
	if inferred == 0 {
		t.Error("no inferred-severity log lines across 200 ticks")
	}
}

func TestDemoGenerator_TraceHasSlowChildAtSlowAt(t *testing.T) {
	g := newDemoGenerator(1, 0)
	ts := time.Now()

	slow := g.trace(ts, g.slowAt)
	if len(slow) != 2 {
		t.Fatalf("trace at slowAt (%d) has %d spans, want 2 (root + child)", g.slowAt, len(slow))
	}
	root, child := slow[0], slow[1]
	if child.ParentID != root.SpanID {
		t.Errorf("child.ParentID = %q, want root's SpanID %q", child.ParentID, root.SpanID)
	}
	if child.Duration < 3*time.Second {
		t.Errorf("slow child duration = %v, want >= 3s", child.Duration)
	}
	if root.Duration < child.Duration {
		t.Errorf("root duration %v is shorter than its own child %v", root.Duration, child.Duration)
	}

	// A trace on an ordinary tick stays fast.
	fast := g.trace(ts, 0)
	if len(fast) != 2 {
		t.Fatalf("trace at tick 0 has %d spans, want 2", len(fast))
	}
	if fast[1].Duration >= time.Second {
		t.Errorf("ordinary trace's child duration = %v, want well under 1s", fast[1].Duration)
	}
}

func TestEmitDemoTick_WritesThroughTheObservingStorePath(t *testing.T) {
	eng := forseer.NewEngine()
	st := observingStore{Store: store.NewMemoryStore(time.Hour), eng: eng}
	gen := newDemoGenerator(1, 0)
	ts := time.Now()

	// Drive it through the spike so every kind of record (metrics, logs,
	// spans) is written at least once.
	for i := 0; i <= gen.spikeAt+gen.spikeLen; i++ {
		if err := emitDemoTick(context.Background(), st, gen, ts.Add(time.Duration(i)*10*time.Second), i); err != nil {
			t.Fatalf("emitDemoTick(%d): %v", i, err)
		}
	}

	metrics, err := st.QueryMetrics(context.Background(), store.MetricQuery{Name: "host.cpu.percent"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(metrics) == 0 {
		t.Error("no host.cpu.percent metrics landed in the store")
	}

	logs, err := st.QueryLogs(context.Background(), store.LogQuery{})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(logs) == 0 {
		t.Error("no logs landed in the store")
	}

	// The observingStore wrapper (see run.go) is what feeds forseer.Engine
	// on every write; Models must reflect that something was observed.
	if models := eng.Models(); len(models) == 0 {
		t.Error("Engine.Models() is empty after ticks were written through the observing store")
	}
}

func TestDemoBackfill_SpansRoughlyTheRequestedWindow(t *testing.T) {
	span := 2 * time.Minute
	tick := time.Second
	steps := int(span / tick)

	eng := forseer.NewEngine()
	st := observingStore{Store: store.NewMemoryStore(span + time.Hour), eng: eng}
	gen := newDemoGenerator(1, steps)

	before := time.Now()
	if err := demoBackfill(context.Background(), st, gen, span, tick, steps); err != nil {
		t.Fatalf("demoBackfill: %v", err)
	}

	metrics, err := st.QueryMetrics(context.Background(), store.MetricQuery{Name: "host.cpu.percent"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(metrics) != steps {
		t.Fatalf("got %d host.cpu.percent points, want %d (one per backfilled step)", len(metrics), steps)
	}
	oldest := metrics[0].Timestamp
	wantOldest := before.Add(-span)
	if diff := oldest.Sub(wantOldest); diff < -2*tick || diff > 2*tick {
		t.Errorf("oldest backfilled timestamp %v is %v away from the requested start %v, want within %v", oldest, diff, wantOldest, 2*tick)
	}
}

func TestDemoBackfill_StopsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	st := observingStore{Store: store.NewMemoryStore(time.Hour), eng: forseer.NewEngine()}
	gen := newDemoGenerator(1, 1000)

	if err := demoBackfill(ctx, st, gen, time.Hour, time.Second, 3600); err == nil {
		t.Fatal("demoBackfill with an already-cancelled context: want an error, got nil")
	}
}

func TestRunDemoLive_StopsPromptlyOnCancel(t *testing.T) {
	st := observingStore{Store: store.NewMemoryStore(time.Minute), eng: forseer.NewEngine()}
	gen := newDemoGenerator(1, 0)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		runDemoLive(ctx, st, gen, 5*time.Millisecond, 0, discardLogger())
		close(done)
	}()

	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runDemoLive did not stop promptly after ctx cancellation")
	}

	metrics, err := st.QueryMetrics(context.Background(), store.MetricQuery{Name: "host.cpu.percent"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(metrics) == 0 {
		t.Error("runDemoLive produced no metrics before it was cancelled")
	}
}

func TestNewDemoCmd_Flags(t *testing.T) {
	cmd := newDemoCmd()
	if cmd.Use != "demo" {
		t.Errorf("Use = %q, want %q", cmd.Use, "demo")
	}
	for _, name := range []string{"addr", "auth-token", "backfill", "tick", "retention", "seed"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q is not registered", name)
		}
	}
}

func TestDemoProcessMetrics_CarryTheTickTimestamp(t *testing.T) {
	g := newDemoGenerator(1, 0)
	ts := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	for _, m := range g.processMetrics(ts, 0) {
		if !m.Timestamp.Equal(ts) {
			t.Fatalf("%s carries timestamp %v, want the tick's %v", m.Name, m.Timestamp, ts)
		}
	}
}
