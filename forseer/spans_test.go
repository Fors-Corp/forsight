package forseer

import (
	"fmt"
	"testing"
	"time"
)

// TestSpanWatch_EvictsOldestTraceByTime inserts maxTraces distinct traces in
// a known order, then adds one more to push the trace map over maxTraces.
// evictOldestTraceLocked must remove the specific trace that was inserted
// first (oldest by last-updated time), not whatever key Go's map iteration
// happens to yield. The old implementation kept the map bounded too, so a
// weaker assertion ("len never exceeds maxTraces") would not have caught the
// bug — this test pins down which trace survives.
func TestSpanWatch_EvictsOldestTraceByTime(t *testing.T) {
	w := newSpanWatch()
	tick := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	w.now = func() time.Time {
		got := tick
		tick = tick.Add(time.Second)
		return got
	}

	for i := 0; i < maxTraces; i++ {
		id := fmt.Sprintf("trace-%02d", i)
		w.Observe([]SpanSample{{TraceID: id, SpanID: "s", Name: "op", DurationMs: 1}})
	}
	if got := len(w.traces); got != maxTraces {
		t.Fatalf("after filling to capacity: got %d traces, want %d", got, maxTraces)
	}

	// This insert pushes the map to maxTraces+1, forcing an eviction. Under
	// the old code, eviction picks an arbitrary map key — which could even
	// be "trace-new", the one just inserted on this very call.
	w.Observe([]SpanSample{{TraceID: "trace-new", SpanID: "s", Name: "op", DurationMs: 1}})

	if got := len(w.traces); got != maxTraces {
		t.Fatalf("after eviction: got %d traces, want %d", got, maxTraces)
	}
	if _, ok := w.traces["trace-00"]; ok {
		t.Fatalf("expected the oldest trace (trace-00) to be evicted, but it is still present")
	}
	if _, ok := w.traceSeen["trace-00"]; ok {
		t.Fatalf("expected traceSeen bookkeeping for trace-00 to be cleared on eviction")
	}
	if _, ok := w.traces["trace-new"]; !ok {
		t.Fatalf("expected the just-inserted trace-new to survive eviction, it did not")
	}
	for i := 1; i < maxTraces; i++ {
		id := fmt.Sprintf("trace-%02d", i)
		if _, ok := w.traces[id]; !ok {
			t.Fatalf("expected trace %s to survive eviction, it did not", id)
		}
	}
}

// spanBaseline is a little jitter, not one repeated value: a zero-spread
// series makes the very first outlier redefine the interquartile spread
// too, which is a degenerate case of its own and not what these tests are
// checking. It has exactly minSamples points so the series becomes eligible
// for slow-span evaluation right after the last one lands.
var spanBaseline = []float64{18, 19, 20, 21, 22, 20, 19, 21, 18, 22, 20, 19}

func warmSpanBaseline(t *testing.T, w *spanWatch, service, name string) {
	t.Helper()
	if len(spanBaseline) != minSamples {
		t.Fatalf("test setup: spanBaseline has %d points, want minSamples=%d", len(spanBaseline), minSamples)
	}
	for _, d := range spanBaseline {
		w.Observe([]SpanSample{{Service: service, Name: name, DurationMs: d}})
	}
}

// TestSpanWatch_RunOfExceedancesGatesOpening checks the alert budget: p99
// fires on about one span in a hundred by construction, so a single
// exceedance must not open an insight, and only a run of spanExceedRun in a
// row does.
func TestSpanWatch_RunOfExceedancesGatesOpening(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")

	for i := 0; i < spanExceedRun-1; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 23}})
		if len(w.Insights()) != 0 {
			t.Fatalf("slow_span opened after only %d of %d exceedances", i+1, spanExceedRun)
		}
	}

	w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 23}})
	insights := w.Insights()
	if len(insights) != 1 {
		t.Fatalf("expected exactly one slow_span after a run of %d exceedances, got %+v", spanExceedRun, insights)
	}
	if insights[0].Kind != KindSlowSpan {
		t.Fatalf("kind = %q, want %q", insights[0].Kind, KindSlowSpan)
	}
	if insights[0].Severity != SeverityWarning {
		t.Fatalf("severity = %q, want warning (no error status)", insights[0].Severity)
	}
}

// TestSpanWatch_ClosesAsSoonAsASpanIsBackInLine checks that closing is not
// gated the way opening is: the run-length budget only guards a false
// positive on the way in, so a single span back at or under p99 clears the
// insight immediately.
func TestSpanWatch_ClosesAsSoonAsASpanIsBackInLine(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")
	for i := 0; i < spanExceedRun; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 23}})
	}
	if len(w.Insights()) != 1 {
		t.Fatalf("setup: expected the run to open an insight, got %+v", w.Insights())
	}

	w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 20}})
	if got := w.Insights(); len(got) != 0 {
		t.Fatalf("expected the insight to close on the first in-line span, still open: %+v", got)
	}
}

// TestSpanWatch_ErrorStatusEscalatesToCritical checks that an error status
// on the span completing the run still escalates severity the way the old
// z-score check did, even though the run-length gate is new.
func TestSpanWatch_ErrorStatusEscalatesToCritical(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")
	for i := 0; i < spanExceedRun-1; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 23}})
	}
	w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 23, Status: "error"}})

	insights := w.Insights()
	if len(insights) != 1 || insights[0].Severity != SeverityCritical {
		t.Fatalf("expected one critical slow_span, got %+v", insights)
	}
}

// TestSpanWatch_CUSUMResetsEstimatorOnRegimeShift checks that a sharp,
// sustained jump — the CUSUM statistic on the series' own robust z crossing
// cusumH — throws the P² markers away rather than letting one shift get
// blended into a lifetime baseline that never catches up. A tight baseline
// makes the interquartile spread small, so a jump far outside it produces a
// z large enough to cross cusumH on a single point.
func TestSpanWatch_CUSUMResetsEstimatorOnRegimeShift(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")
	key := "api|GET /checkout"
	warm := w.series[key]
	if warm == nil || warm.n < minSamples {
		t.Fatalf("setup: series not warmed up: %+v", warm)
	}
	warmedN := warm.n

	w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 500}})

	s := w.series[key]
	if s.n >= warmedN {
		t.Fatalf("expected the regime-shift point to reset the series (n back down from %d), got n=%d", warmedN, s.n)
	}
	if s.cusum != 0 {
		t.Fatalf("expected cusum cleared right after a reset, got %v", s.cusum)
	}
}

// TestSpanWatch_AdaptsToNewBaselineAfterReset is the behavioural half of the
// CUSUM test above: once the estimator resets and re-warms on the new
// regime, spans at the new normal must stop opening insights — proving the
// reset actually re-learned a baseline rather than just clearing counters.
func TestSpanWatch_RegimeShiftClosesTheOpenInsight(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")
	key := "api|GET /checkout"
	p99, ok := w.series[key].p99.value()
	if !ok {
		t.Fatal("setup: no p99 after warm-up")
	}
	// Just past p99, enough times in a row to open, but nowhere near a
	// CUSUM reset.
	for i := 0; i < spanExceedRun; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: p99 * 1.05}})
	}
	if got := w.Insights(); len(got) != 1 {
		t.Fatalf("setup: expected one open slow_span, got %+v", got)
	}

	// A regime shift resets the series, and the insight judged against the
	// old baseline goes with it.
	w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 500}})
	if w.series[key].n != 1 {
		t.Fatalf("setup: expected the jump to reset the series, n=%d", w.series[key].n)
	}
	if got := w.Insights(); len(got) != 0 {
		t.Fatalf("expected the open insight closed by the reset, got %+v", got)
	}
}

func TestSpanWatch_AdaptsToNewBaselineAfterReset(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")

	// The jump itself resets the series (see the test above); feed enough
	// points at the new level to re-warm past minSamples.
	shifted := []float64{98, 99, 100, 101, 102, 100, 99, 101, 98, 102, 100, 99, 100, 101, 99}
	for _, d := range shifted {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: d}})
	}

	// More spans at the same new level must not read as exceedances of a
	// stale, pre-shift p99.
	for i := 0; i < spanExceedRun+2; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 100}})
	}
	if got := w.Insights(); len(got) != 0 {
		t.Fatalf("expected no slow_span once the series re-warmed on its new baseline, got %+v", got)
	}
}

// TestSpanWatch_Card checks the Model contract: a cold watch reports itself
// not ready with no invented accuracy, and a warmed-up series moves Ready
// and Trained without ever claiming a measured Accuracy — there is no label
// for "was this span actually slow" to grade against.
func TestSpanWatch_Card(t *testing.T) {
	w := newSpanWatch()
	card := w.Card()
	if card.Ready {
		t.Fatal("a cold span watch reports Ready")
	}
	if card.Accuracy != Unmeasured || card.FallbackAccuracy != Unmeasured {
		t.Fatalf("expected Unmeasured accuracy (no label exists), got %+v", card)
	}
	if card.Name == "" || card.Job == "" || len(card.Reads) == 0 || card.Fallback == "" {
		t.Fatalf("card does not fully describe itself: %+v", card)
	}

	warmSpanBaseline(t, w, "api", "GET /checkout")
	card = w.Card()
	if !card.Ready {
		t.Fatalf("expected Ready once a series passed minSamples: %+v", card)
	}
	if card.Trained != minSamples {
		t.Fatalf("Trained = %d, want %d", card.Trained, minSamples)
	}
}
