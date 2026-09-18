package forseer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
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
// checking. It is a cycle, not the whole warm-up: warmSpanBaseline repeats
// it until the series has the spanP99MinSamples spans its p99 marker needs
// before it is allowed to judge anything.
var spanBaseline = []float64{18, 19, 20, 21, 22, 20, 19, 21, 18, 22, 20, 19}

// warmSpanBaseline feeds exactly spanP99MinSamples spans, so the series
// becomes eligible for slow-span evaluation right after the last one lands.
// It used to feed minSamples, which is what the old gate asked for and is
// nowhere near enough for a p99 — see spanP99MinSamples.
func warmSpanBaseline(t *testing.T, w *spanWatch, service, name string) {
	t.Helper()
	warmSpanSeries(t, w, service, name, spanP99MinSamples)
}

// warmSpanSeries adds n more spans from the jitter cycle and insists the
// series actually kept them: the cycle is stationary, so a CUSUM reset here
// would mean the test is measuring the changepoint statistic by accident.
func warmSpanSeries(t *testing.T, w *spanWatch, service, name string, n int) {
	t.Helper()
	key := service + "|" + name
	before := 0
	if s := w.series[key]; s != nil {
		before = s.n
	}
	for i := 0; i < n; i++ {
		w.Observe([]SpanSample{{Service: service, Name: name, DurationMs: spanBaseline[i%len(spanBaseline)]}})
	}
	s := w.series[key]
	if s == nil || s.n != before+n {
		t.Fatalf("test setup: %d spans of stationary jitter took n from %d to %v; it must not trip a CUSUM reset",
			n, before, s)
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
// blended into a lifetime baseline that never catches up.
//
// A *sustained* shift, not a single point: the sign CUSUM needs a run of
// spans on one side of the median to accumulate past cusumH, which is the
// point of it. TestSpanWatch_OneSlowSpanDoesNotResetTheBaseline below is the
// other half of that contract.
func TestSpanWatch_CUSUMResetsEstimatorOnRegimeShift(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")
	key := "api|GET /checkout"
	warm := w.series[key]
	if warm == nil || warm.n < minSamples {
		t.Fatalf("setup: series not warmed up: %+v", warm)
	}
	warmedN := warm.n

	// Every span above the old median: +1 each, less cusumK, so cusumH is
	// reached after ceil(cusumH/(1-cusumK)) of them.
	need := int(math.Ceil(cusumH/(1-cusumK))) + 1
	for i := 0; i < need; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 500}})
	}

	s := w.series[key]
	if s.n >= warmedN {
		t.Fatalf("expected the sustained shift to reset the series (n back down from %d), got n=%d", warmedN, s.n)
	}
	if s.cusumHi != 0 || s.cusumLo != 0 {
		t.Fatalf("expected both CUSUM arms cleared right after a reset, got hi=%v lo=%v", s.cusumHi, s.cusumLo)
	}
}

// TestSpanWatch_OneSlowSpanDoesNotResetTheBaseline pins the half of the
// contract the previous formulation got wrong. It divided |duration-median|
// by the interquartile spread, so a single span far outside a tight baseline
// cleared cusumH on its own and threw away p50/p99 — losing the very
// baseline needed to judge the next span, on evidence of exactly one sample.
// One slow span is a slow span; spanExceedRun exceedances in a row are what
// opens an insight about it, and neither is a regime change.
func TestSpanWatch_OneSlowSpanDoesNotResetTheBaseline(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")
	key := "api|GET /checkout"
	warmedN := w.series[key].n
	wantP50, ok := w.series[key].p50.value()
	if !ok {
		t.Fatal("setup: no p50 after warm-up")
	}

	w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 5000}})

	s := w.series[key]
	if s.n < warmedN {
		t.Fatalf("one outlier reset the series: n went %d -> %d", warmedN, s.n)
	}
	// A tolerance rather than exact equality: with the warm-up now
	// spanP99MinSamples long, the P² median marker is free to take its normal
	// one-position step on any observation, outlier or not. What must not
	// happen is the baseline being thrown away or dragged towards 5000ms.
	if got, ok := s.p50.value(); !ok || math.Abs(got-wantP50)/wantP50 > 0.01 {
		t.Fatalf("one outlier moved the median baseline: p50 %v -> %v (ok=%v)", wantP50, got, ok)
	}
}

// TestSpanWatch_SteadyTrafficNeverResetsTheBaseline is the false-alarm gate.
// Latency is long-tailed, and the old statistic was positive by
// construction, so its CUSUM drifted upward on an endpoint that never
// changed and wiped p50/p99 every few dozen spans — leaving the watcher
// below minSamples, and so judging nothing, for much of its life. Steady
// traffic must reset nothing, however long it runs.
func TestSpanWatch_SteadyTrafficNeverResetsTheBaseline(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")
	key := "api|GET /checkout"
	warmedN := w.series[key].n

	// A long-tailed but stationary stream: mostly fast, a heavy tail every
	// tenth span. Deterministic, so a failure here is reproducible.
	//
	// A bound rather than zero: a sign CUSUM is a hypothesis test, so a
	// stationary stream still trips it occasionally — measured at 1 per 435
	// spans, and unchanged whether the tail is light or heavy, because
	// E[sign(x-median)] is zero for any continuous distribution. The old
	// |duration-median|/IQR statistic tripped every 23-46 spans instead,
	// which over this run is a dozen resets or more. Anything near that is
	// the drift coming back.
	const spans, maxResets = 600, 2
	tail := []float64{8, 9, 10, 11, 9, 10, 12, 9, 10, 240}
	resets, prevN := 0, warmedN
	for i := 0; i < spans; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: tail[i%len(tail)]}})
		n := w.series[key].n
		if n < prevN {
			resets++
		}
		prevN = n
	}
	if resets > maxResets {
		t.Fatalf("steady traffic reset the baseline %d times in %d spans (want <= %d); the old drifting statistic did this every 23-46 spans",
			resets, spans, maxResets)
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
	// old baseline goes with it. Sustained, not a single jump: the sign
	// CUSUM needs a run on one side of the median, and the exceedances above
	// are already part of that run.
	sawReset, prevN := false, w.series[key].n
	for i := 0; i < int(math.Ceil(cusumH/(1-cusumK)))+1; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 500}})
		if n := w.series[key].n; n < prevN {
			sawReset = true
			break
		} else {
			prevN = n
		}
	}
	if !sawReset {
		t.Fatalf("setup: expected the sustained shift to reset the series, n=%d", w.series[key].n)
	}
	if got := w.Insights(); len(got) != 0 {
		t.Fatalf("expected the open insight closed by the reset, got %+v", got)
	}
}

func TestSpanWatch_AdaptsToNewBaselineAfterReset(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")

	// A sustained run at the new level resets the series (see the test
	// above); the points after it re-warm past minSamples on the new regime.
	for i := 0; i < int(math.Ceil(cusumH/(1-cusumK)))+1; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: 500}})
	}
	// Re-warm all the way past the gate, so the assertion below is about a
	// live p99 rather than about a series that is simply still too young to
	// judge anything.
	// Feed until the series is past the gate rather than a fixed count: the
	// tail of 500ms spans left over after the reset seeds the fresh markers
	// high, so the first shifted spans read as a second downward regime
	// change and reset it once more. That is the statistic working; the point
	// here is where it settles.
	shifted := []float64{98, 99, 100, 101, 102, 100, 99, 101, 98, 102, 100, 99}
	for i := 0; i < 4*spanP99MinSamples; i++ {
		if s := w.series["api|GET /checkout"]; s != nil && s.n >= spanP99MinSamples {
			break
		}
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: shifted[i%len(shifted)]}})
	}
	if s := w.series["api|GET /checkout"]; s == nil || s.n < spanP99MinSamples {
		t.Fatalf("setup: the series did not re-warm past the gate: %+v", s)
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

// lognormalSpans is stationary long-tailed latency: median 40ms, sigma 0.6,
// so the true p99 is 40*exp(0.6*z99) = 161.5ms. Latency is lognormal-ish in
// practice and this is the shape the p99 marker is meant to summarise.
func lognormalSpans(r *rand.Rand) float64 { return 40 * math.Exp(0.6*r.NormFloat64()) }

const trueLognormalP99 = 161.5

// TestSpanWatch_P99IsNotUsableAtMinSamples is the measurement that justifies
// spanP99MinSamples, kept as a test so the constant cannot quietly be lowered
// back towards minSamples to suit a fixture. A P² marker converges on a long
// tail from below, because early on it has not yet seen the tail it is
// supposed to be summarising: at minSamples the value this code reports as
// "p99" — and prints in every KindSlowSpan Description — sits nearer the true
// 79th percentile, and the old gate let it judge there anyway. It asserts
// both ends: badly off at minSamples, within 15% by the gate.
func TestSpanWatch_P99IsNotUsableAtMinSamples(t *testing.T) {
	r := rand.New(rand.NewSource(20260918))
	w := newSpanWatch()
	for i := 0; i < spanP99MinSamples; i++ {
		w.Observe([]SpanSample{{Service: "api", Name: "GET /checkout", DurationMs: lognormalSpans(r)}})
		s := w.series["api|GET /checkout"]
		if s.n != i+1 {
			t.Fatalf("setup: stationary traffic reset the series at span %d", i+1)
		}
		p99, ok := s.p99.value()
		if !ok {
			continue
		}
		off := math.Abs(p99-trueLognormalP99) / trueLognormalP99

		if s.n == minSamples && off < 0.25 {
			t.Fatalf("test is not measuring what it claims: at minSamples=%d the marker reported %.1fms, "+
				"within %.0f%% of the true p99 %.1fms", minSamples, p99, 100*off, trueLognormalP99)
		}
		if s.n >= spanP99MinSamples && off > 0.15 {
			t.Fatalf("at the gate (n=%d) the marker reported %.1fms, %.0f%% off the true p99 %.1fms",
				s.n, p99, 100*off, trueLognormalP99)
		}
	}
}

// TestSpanWatch_ColdSeriesDoNotOpenOnOrdinaryTraffic is the alert-budget
// half. spanExceedRun's comment claims a run of three exceedances is about
// one span in a million; that is a property of the marker actually sitting
// at the p99, and it is simply false while the marker is still warming up.
// Measured over four million spans of this traffic, a series judged with
// between 12 and 25 samples exceeds its own "p99" on 13.5% of spans and
// opens an insight once per 323 — three and a half orders of magnitude past
// the budget. Nothing in this stream ever changes, so every open is false.
//
// Against the old gate (minSamples) this test fails with 13 opens.
func TestSpanWatch_ColdSeriesDoNotOpenOnOrdinaryTraffic(t *testing.T) {
	const endpoints, spansEach = 250, 250
	if endpoints > maxSpanSeries {
		t.Fatalf("test setup: %d endpoints exceeds maxSpanSeries=%d", endpoints, maxSpanSeries)
	}
	r := rand.New(rand.NewSource(20260918))
	w := newSpanWatch()

	opens, prev := 0, 0
	for e := 0; e < endpoints; e++ {
		name := fmt.Sprintf("GET /op-%03d", e)
		for i := 0; i < spansEach; i++ {
			w.Observe([]SpanSample{{Service: "api", Name: name, DurationMs: lognormalSpans(r)}})
			w.mu.Lock()
			n := len(w.open)
			w.mu.Unlock()
			if n > prev {
				opens++
			}
			prev = n
		}
	}

	if opens != 0 {
		t.Fatalf("stationary traffic opened %d slow_span insight(s) across %d endpoints x %d spans; "+
			"nothing in this stream ever changed, so every one of them is false",
			opens, endpoints, spansEach)
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

	// A series with a placed median but a p99 marker still warming up is not
	// a series with "a stable p50/p99", which is what Card claims. It used to
	// report Ready here.
	warmSpanSeries(t, w, "api", "GET /checkout", minSamples)
	if card = w.Card(); card.Ready {
		t.Fatalf("reported Ready on %d samples, far short of the %d its p99 needs: %+v",
			minSamples, spanP99MinSamples, card)
	}

	warmSpanSeries(t, w, "api", "GET /checkout", spanP99MinSamples-minSamples)
	card = w.Card()
	if !card.Ready {
		t.Fatalf("expected Ready once a series passed spanP99MinSamples: %+v", card)
	}
	if card.Trained != spanP99MinSamples {
		t.Fatalf("Trained = %d, want %d", card.Trained, spanP99MinSamples)
	}
}

// TestSpanWatch_SnapshotRestoreRoundTrip is roadmap item 25's proof for this
// model: a series' P² markers survive a restart exactly, but the currently
// open insight and the recent-trace buffer do not.
func TestSpanWatch_SnapshotRestoreRoundTrip(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")

	w.mu.Lock()
	s := w.series["api|GET /checkout"]
	wantN, wantHi, wantLo, wantRunLen := s.n, s.cusumHi, s.cusumLo, s.runLen
	wantP50, ok50 := s.p50.value()
	wantP99, ok99 := s.p99.value()
	w.mu.Unlock()
	if !ok50 || !ok99 {
		t.Fatal("test setup: baseline series has no p50/p99 value yet")
	}

	data, err := w.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := newSpanWatch()
	if err := restored.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restored.mu.Lock()
	rs, ok := restored.series["api|GET /checkout"]
	restored.mu.Unlock()
	if !ok {
		t.Fatal("restored watch has no api|GET /checkout series")
	}
	if rs.n != wantN || rs.cusumHi != wantHi || rs.cusumLo != wantLo || rs.runLen != wantRunLen {
		t.Fatalf("restored series = {n:%d hi:%g lo:%g runLen:%d}, want {n:%d hi:%g lo:%g runLen:%d}",
			rs.n, rs.cusumHi, rs.cusumLo, rs.runLen, wantN, wantHi, wantLo, wantRunLen)
	}
	gotP50, gotOK50 := rs.p50.value()
	gotP99, gotOK99 := rs.p99.value()
	if !gotOK50 || !gotOK99 || gotP50 != wantP50 || gotP99 != wantP99 {
		t.Fatalf("restored p50/p99 = (%v ok=%v)/(%v ok=%v), want %v/%v", gotP50, gotOK50, gotP99, gotOK99, wantP50, wantP99)
	}

	// The currently-open insight and the recent-trace buffer are both tied
	// to a live, continuous stream and reset — see spanWatch.Restore.
	if len(restored.open) != 0 || len(restored.traces) != 0 || len(restored.traceSeen) != 0 {
		t.Fatalf("restored watch carries live state: open=%d traces=%d traceSeen=%d",
			len(restored.open), len(restored.traces), len(restored.traceSeen))
	}
	if !restored.Card().Ready {
		t.Fatal("restored series lost its warm baseline")
	}
}

func TestSpanWatch_RestoreDiscardsAVersionMismatch(t *testing.T) {
	w := newSpanWatch()
	warmSpanBaseline(t, w, "api", "GET /checkout")
	data, err := w.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1)

	fresh := newSpanWatch()
	if err := fresh.Restore(data); err == nil {
		t.Fatal("Restore accepted a payload with the wrong schema version")
	}
	if len(fresh.series) != 0 {
		t.Fatalf("a discarded restore left %d series, want 0", len(fresh.series))
	}
}

func TestSpanWatch_RestoreDiscardsCorruptJSON(t *testing.T) {
	w := newSpanWatch()
	if err := w.Restore([]byte("{not json")); err == nil {
		t.Fatal("Restore accepted corrupt JSON")
	}
	if len(w.series) != 0 {
		t.Fatalf("a discarded restore left %d series, want 0", len(w.series))
	}
}

// TestSpanWatch_RestoreBoundsAnOversizedSnapshot builds a snapshot payload
// (the same spanSnapshot shape Snapshot itself produces, not something
// observeOneLocked could ever create live — it refuses a new series outright
// once len(w.series) reaches maxSpanSeries) with far more than maxSpanSeries
// entries, the way a hand-edited or stale forseer.json could arrive on boot.
// Live, observeOneLocked never evicts an already-tracked series once
// established; it only refuses a *new* one once the cap is full, the same
// policy thresholds.go's Observe uses for its own series map, so — unlike
// paging's LRU cache — there is no recency order for Restore to reproduce.
// The invariant Restore must still uphold is the cap itself, plus that
// whatever it keeps is exactly what the snapshot said for that key,
// including a P² estimator that still answers the same quantile.
func TestSpanWatch_RestoreBoundsAnOversizedSnapshot(t *testing.T) {
	p50, p99 := newP2Estimator(0.5), newP2Estimator(0.99)
	for i := 1; i <= 20; i++ {
		p50.observe(float64(i))
		p99.observe(float64(i))
	}
	p50Snap, p99Snap := snapshotP2(p50), snapshotP2(p99)
	wantP99, ok := p99.value()
	if !ok {
		t.Fatal("test setup: p99 estimator never produced a value")
	}

	const extra = 50
	const total = maxSpanSeries + extra
	snap := spanSnapshot{Version: spanSnapshotVersion, Series: make(map[string]spanSeriesSnapshot, total)}
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("svc|op-%04d", i)
		snap.Series[key] = spanSeriesSnapshot{
			N: minSamples + i, P50: p50Snap, P99: p99Snap, RunLen: i % spanExceedRun,
		}
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal test snapshot: %v", err)
	}

	w := newSpanWatch()
	if err := w.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(w.series) != maxSpanSeries {
		t.Fatalf("restored %d span series, want exactly the cap %d", len(w.series), maxSpanSeries)
	}
	for key, got := range w.series {
		want, ok := snap.Series[key]
		if !ok {
			t.Fatalf("restored series %q was never in the snapshot", key)
		}
		if got.n != want.N || got.runLen != want.RunLen {
			t.Errorf("series %q n=%d runLen=%d, want n=%d runLen=%d", key, got.n, got.runLen, want.N, want.RunLen)
		}
		gotP99, ok := got.p99.value()
		if !ok || gotP99 != wantP99 {
			t.Errorf("series %q restored p99 = %v (ok=%v), want %v", key, gotP99, ok, wantP99)
		}
	}
}
