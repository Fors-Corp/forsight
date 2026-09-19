package forseer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// feed pushes n z-scores drawn from draw through the model and reports how
// often the threshold in force at the time was crossed.
func feed(m *thresholdModel, key string, n int, draw func() float64) (warnRate float64, warn, critical float64) {
	hits := 0
	for i := 0; i < n; i++ {
		z := math.Abs(draw())
		w, c, _ := m.Observe(key, z)
		if z >= w {
			hits++
		}
		warn, critical = w, c
	}
	return float64(hits) / float64(n), warn, critical
}

func TestThresholds_ColdSeriesKeepsTheFixedSigmas(t *testing.T) {
	m := newThresholdModel()

	warn, critical, ready := m.Observe("host.cpu", 1.2)

	if ready {
		t.Error("a series with one point reported itself calibrated")
	}
	if warn != warningSigma || critical != criticalSigma {
		t.Errorf("got %.2f/%.2f, want the constants %d/%d", warn, critical, warningSigma, criticalSigma)
	}
}

// TestThresholds_EachThresholdWaitsForItsOwnTail: readiness is per
// threshold, because the two thresholds are asking questions that differ in
// difficulty by the factor their budgets differ by. A one-in-a-thousand
// quantile is estimable from a sample that contains a few points beyond it;
// a one-in-ten-thousand quantile of the same series is not, and a model that
// declared both ready at the same count would be reporting a number it could
// not have learned.
func TestThresholds_EachThresholdWaitsForItsOwnTail(t *testing.T) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(1))
	draw := func() float64 { return math.Abs(rng.NormFloat64()) }

	var warn, critical float64
	var ready bool
	for i := 0; i < thresholdWarnMinSamples; i++ {
		warn, critical, ready = m.Observe("host.cpu", draw())
		if warn != warningSigma || critical != criticalSigma || ready {
			t.Fatalf("point %d: got %.2f/%.2f ready=%v, want the constants and not ready", i+1, warn, critical, ready)
		}
	}
	for i := thresholdWarnMinSamples; i < thresholdCriticalMinSamples; i++ {
		warn, critical, ready = m.Observe("host.cpu", draw())
		if warn == warningSigma {
			t.Fatalf("point %d: the warning threshold is still the shared constant", i+1)
		}
		if critical != criticalSigma {
			t.Fatalf("point %d: the page threshold is %.2f, but it cannot have been learned yet", i+1, critical)
		}
		if ready {
			t.Fatalf("point %d: reported fully calibrated while the page threshold is still the constant", i+1)
		}
	}
	warn, critical, ready = m.Observe("host.cpu", draw())
	if !ready || critical == criticalSigma {
		t.Fatalf("at %d points the page threshold is %.2f ready=%v, want a learned one",
			thresholdCriticalMinSamples, critical, ready)
	}
	if critical < warn {
		t.Fatalf("page threshold %.2f is easier to reach than the warning %.2f", critical, warn)
	}
}

// The point of the whole model: a series that is quiet should need less to
// alert than a series that is wild, rather than both needing exactly 3σ.
func TestThresholds_DivergeBetweenAQuietAndANoisySeries(t *testing.T) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(7))

	// Quiet: tight, well-behaved. Its own 3σ is rarely reached, so the
	// budget pulls the threshold down toward where its tail actually is.
	_, quietWarn, _ := feed(m, "quiet", 20000, func() float64 { return rng.NormFloat64() })

	// Noisy: heavy-tailed, so 3σ is crossed far more often than one in a
	// thousand and the budget pushes the threshold up.
	_, noisyWarn, _ := feed(m, "noisy", 20000, func() float64 {
		if rng.Float64() < 0.08 {
			return rng.NormFloat64() * 6
		}
		return rng.NormFloat64()
	})

	if noisyWarn <= quietWarn {
		t.Errorf("noisy series settled at %.2f and quiet at %.2f; the thresholds did not separate", noisyWarn, quietWarn)
	}
}

func TestThresholds_ConvergeTowardTheBudgetOnANoisySeries(t *testing.T) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(11))
	draw := func() float64 {
		if rng.Float64() < 0.08 {
			return rng.NormFloat64() * 6
		}
		return rng.NormFloat64()
	}

	// Warm up, then measure only the settled period, counting what the
	// fixed threshold would have done over the same points.
	feed(m, "noisy", 20000, draw)
	fixed := 0
	hits := 0
	const n = 20000
	for i := 0; i < n; i++ {
		z := math.Abs(draw())
		warn, _, _ := m.Observe("noisy", z)
		if z >= warn {
			hits++
		}
		if z >= warningSigma {
			fixed++
		}
	}
	rate := float64(hits) / n
	fixedRate := float64(fixed) / n

	// The claim worth making is not that the budget is hit exactly — the
	// ceiling deliberately stops a wild series becoming unalertable, and
	// this distribution pushes right up against it. The claim is that a
	// series whose fixed threshold pages every few minutes is brought back
	// to something an operator can live with.
	if rate > 5*targetWarnRate {
		t.Errorf("settled alert rate %.4f is more than five times the %.4f budget", rate, targetWarnRate)
	}
	if rate > fixedRate/5 {
		t.Errorf("learned rate %.4f is not a large improvement on the fixed threshold's %.4f", rate, fixedRate)
	}
}

func TestThresholds_StayWithinTheirBounds(t *testing.T) {
	m := newThresholdModel()

	// A series that never moves: the budget pushes the threshold down, and
	// the floor is what stops it alerting on ordinary variation.
	_, quietWarn, _ := feed(m, "flat", 50000, func() float64 { return 0 })
	if quietWarn < thresholdFloor {
		t.Errorf("threshold fell to %.2f, below the %.2f floor", quietWarn, thresholdFloor)
	}

	// A series that is always extreme: the ceiling stops it from becoming
	// unalertable.
	_, wildWarn, _ := feed(m, "wild", 50000, func() float64 { return 100 })
	if wildWarn > thresholdCeiling {
		t.Errorf("threshold rose to %.2f, above the %.2f ceiling", wildWarn, thresholdCeiling)
	}
}

func TestThresholds_CriticalIsNeverEasierThanWarning(t *testing.T) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(3))

	for i := 0; i < 30000; i++ {
		warn, critical, _ := m.Observe("mixed", math.Abs(rng.NormFloat64()*3))
		if critical < warn {
			t.Fatalf("critical %.2f is below warning %.2f at point %d", critical, warn, i)
		}
	}
}

func TestThresholds_BoundTheNumberOfSeriesTracked(t *testing.T) {
	m := newThresholdModel()

	// The bound is in this map's own unit — seasonalKey, of which one real
	// series is up to 24 — not in the Detector's base-series unit.
	for i := 0; i < maxThresholdKeys*2; i++ {
		m.Observe(fmt.Sprintf("series-%d", i), 1)
	}

	m.mu.Lock()
	tracked := len(m.series)
	m.mu.Unlock()
	if tracked > maxThresholdKeys {
		t.Errorf("tracking %d keys, cap is %d", tracked, maxThresholdKeys)
	}
}

func TestThresholds_CardReportsTheBudgetItIsHitting(t *testing.T) {
	m := newThresholdModel()

	cold := m.Card()
	if cold.Ready {
		t.Error("card reported ready with no series")
	}
	if cold.Accuracy != Unmeasured || cold.FallbackAccuracy != Unmeasured {
		t.Error("a calibration has no labels, so it must not claim an accuracy")
	}
	if cold.Name == "" || cold.Job == "" || len(cold.Reads) == 0 || cold.Fallback == "" {
		t.Error("card does not fully describe itself")
	}

	rng := rand.New(rand.NewSource(5))
	feed(m, "host.cpu", thresholdWarnMinSamples*3, func() float64 { return rng.NormFloat64() })

	warm := m.Card()
	if !warm.Ready {
		t.Error("card still not ready after a well-populated series")
	}
	if warm.Detail == cold.Detail {
		t.Error("detail did not change once the model had something to say")
	}
	// The two thresholds are ready at different times, so one boolean cannot
	// describe the model: the card has to say how many series have learned
	// each of them, or an operator reading "ready" would believe a page
	// threshold had been calibrated weeks before it can be.
	if !strings.Contains(warm.Detail, "warning threshold") || !strings.Contains(warm.Detail, "page threshold") {
		t.Errorf("detail %q does not report both thresholds' readiness separately", warm.Detail)
	}
}

func TestDetector_UsesLearnedThresholdsOnceCalibrated(t *testing.T) {
	d := NewDetector()

	// A series with a fat tail: 3σ would fire constantly, so once the model
	// is calibrated the threshold must have moved above it.
	rng := rand.New(rand.NewSource(13))
	for i := 0; i < 6000; i++ {
		value := rng.NormFloat64()
		if rng.Float64() < 0.1 {
			value *= 8
		}
		d.Observe([]Point{{Name: "app.latency", Value: value}})
	}

	d.mu.Lock()
	key := seasonalKey("app.latency", nil, d.now().Hour())
	s := d.thresholds.series[key]
	d.mu.Unlock()

	if s == nil {
		t.Fatal("the detector never fed this series to the threshold model")
	}
	if s.n < thresholdWarnMinSamples {
		t.Fatalf("only %d points reached the model", s.n)
	}
	warn, _, _ := s.thresholds()
	if warn <= warningSigma {
		t.Errorf("warning threshold settled at %.2f, no higher than the fixed %d it replaced", warn, warningSigma)
	}
}

// TestThresholds_SnapshotRestoreRoundTrip is roadmap item 25's proof for
// this model. Unlike severity's, there is no grading window to reset here:
// a calibration is restored whole, n included, so a series that was already
// calibrated stays calibrated across a restart.
//
// What "whole" means is now the two quantile estimators' markers, not a pair
// of scalars, and the assertion is the one that matters to an operator: the
// restored model answers the next point with exactly the thresholds the
// original would have, and keeps moving from there rather than from a
// frozen number.
func TestThresholds_SnapshotRestoreRoundTrip(t *testing.T) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(21))
	feed(m, "host.cpu", thresholdCriticalMinSamples+500, func() float64 { return rng.NormFloat64() })

	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := newThresholdModel()
	if err := restored.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Feed both the same continuation: a restored calibration that had lost
	// any part of its state would diverge on the very next point.
	next := rand.New(rand.NewSource(99))
	for i := 0; i < 500; i++ {
		z := math.Abs(next.NormFloat64())
		wantWarn, wantCrit, wantReady := m.Observe("host.cpu", z)
		gotWarn, gotCrit, gotReady := restored.Observe("host.cpu", z)
		if gotWarn != wantWarn || gotCrit != wantCrit || gotReady != wantReady {
			t.Fatalf("point %d after restore: got %.6f/%.6f ready=%v, want %.6f/%.6f ready=%v",
				i+1, gotWarn, gotCrit, gotReady, wantWarn, wantCrit, wantReady)
		}
	}
	if !restored.Card().Ready {
		t.Fatal("restored model is not ready even though its series was already calibrated")
	}
}

// TestThresholds_RestoreRejectsAnImpossibleEstimator: the markers are not
// free-form numbers, they are a state machine's state. Marker positions that
// are equal or out of order make the P² update divide by zero, and the NaN
// that follows compares false against every z forever — a series that
// silently never alerts again. A snapshot that says so is refused, and the
// series re-earns its calibration instead.
func TestThresholds_RestoreRejectsAnImpossibleEstimator(t *testing.T) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(23))
	feed(m, "good", thresholdWarnMinSamples, func() float64 { return rng.NormFloat64() })
	feed(m, "corrupt", thresholdWarnMinSamples, func() float64 { return rng.NormFloat64() })

	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var snap thresholdSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	broken := snap.Series["corrupt"]
	broken.Warn.Pos = [5]int{1, 2, 2, 4, broken.Warn.N}
	snap.Series["corrupt"] = broken
	data, err = json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal edited snapshot: %v", err)
	}

	restored := newThresholdModel()
	if err := restored.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restored.mu.Lock()
	_, keptCorrupt := restored.series["corrupt"]
	_, keptGood := restored.series["good"]
	restored.mu.Unlock()
	if keptCorrupt {
		t.Error("restored a series whose markers P² could not have produced")
	}
	if !keptGood {
		t.Error("one corrupt series cost an intact one its calibration")
	}

	// And the dropped series still works: it starts over on the constants
	// rather than answering NaN.
	warn, critical, ready := restored.Observe("corrupt", 4)
	if warn != warningSigma || critical != criticalSigma || ready {
		t.Errorf("a dropped series answered %.2f/%.2f ready=%v, want the constants", warn, critical, ready)
	}
}

func TestThresholds_RestoreDiscardsAVersionMismatch(t *testing.T) {
	m := newThresholdModel()
	feed(m, "host.cpu", thresholdWarnMinSamples, func() float64 { return 1 })
	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data = bytes.Replace(data,
		[]byte(fmt.Sprintf(`"version":%d`, thresholdSnapshotVersion)),
		[]byte(fmt.Sprintf(`"version":%d`, thresholdSnapshotVersion+1)), 1)

	fresh := newThresholdModel()
	if err := fresh.Restore(data); err == nil {
		t.Fatal("Restore accepted a payload with the wrong schema version")
	}
	if len(fresh.series) != 0 {
		t.Fatalf("a discarded restore left %d series, want 0", len(fresh.series))
	}
}

func TestThresholds_RestoreDiscardsCorruptJSON(t *testing.T) {
	m := newThresholdModel()
	if err := m.Restore([]byte("{not json")); err == nil {
		t.Fatal("Restore accepted corrupt JSON")
	}
	if len(m.series) != 0 {
		t.Fatalf("a discarded restore left %d series, want 0", len(m.series))
	}
}

// TestThresholds_RestoreBoundsAnOversizedSnapshot builds a snapshot payload
// (the same thresholdSnapshot shape Snapshot itself produces, not something
// Observe could ever create live — Observe refuses a new series outright
// once len(m.series) reaches maxSeries) with far more than maxSeries
// entries, the way a hand-edited or stale forseer.json could arrive on
// boot. Live, Observe never evicts an already-tracked series once
// established; it only refuses a *new* one once the cap is full, so unlike
// paging's LRU cache there is no recency order for Restore to reproduce.
// The invariant Restore must still uphold is the cap itself, plus that
// whatever it keeps is exactly what the snapshot said for that key, never
// corrupted by the truncation.
func TestThresholds_RestoreBoundsAnOversizedSnapshot(t *testing.T) {
	const extra = 50
	const total = maxSeries + extra

	// Built by feeding real points, because the markers are a state machine's
	// state: a hand-written pair of numbers is not a state P² could have
	// produced, and Restore refuses those on purpose (see
	// TestThresholds_RestoreRejectsAnImpossibleEstimator). Only the map is
	// oversized, which is what this test is about.
	source := newThresholdModel()
	rng := rand.New(rand.NewSource(31))
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("series-%04d", i)
		for j := 0; j < 10; j++ {
			source.Observe(key, math.Abs(rng.NormFloat64()))
		}
	}
	data, err := source.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var snap thresholdSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	if len(snap.Series) != total {
		t.Fatalf("test setup: snapshot holds %d series, want %d", len(snap.Series), total)
	}

	m := newThresholdModel()
	if err := m.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if len(m.series) != maxSeries {
		t.Fatalf("restored %d series, want exactly the cap %d", len(m.series), maxSeries)
	}
	for key, got := range m.series {
		want, ok := snap.Series[key]
		if !ok {
			t.Fatalf("restored series %q was never in the snapshot", key)
		}
		if got.n != want.N || got.warnHits != want.WarnHits || got.critHits != want.CritHits {
			t.Errorf("series %q counts = n:%d warn:%d crit:%d, want n:%d warn:%d crit:%d",
				key, got.n, got.warnHits, got.critHits, want.N, want.WarnHits, want.CritHits)
		}
		if !reflect.DeepEqual(snapshotP2(got.warn), want.Warn) || !reflect.DeepEqual(snapshotP2(got.critical), want.Critical) {
			t.Errorf("series %q came back with different markers than the snapshot held", key)
		}
	}
}

// budgetRates is the measurement the whole model exists to pass: feed one
// series `warm` points of |N(0,1)|, then over the next `window` points count
// how often the thresholds actually in force were crossed. That fraction is
// what an operator experiences as "how often this pages me", and the budget
// is what they were promised.
//
// Independent series, averaged, because a one-in-ten-thousand rate cannot be
// measured on one short stream: 2 000 points of a correctly calibrated
// series contain 0.2 expected pages, so a single series reports either 0% or
// 0.05% and neither number means anything.
func budgetRates(seed int64, series, warm, window int) (warnRate, critRate float64) {
	m := newThresholdModel()
	warnHits, critHits := 0, 0
	for s := 0; s < series; s++ {
		key := fmt.Sprintf("series-%d", s)
		rng := rand.New(rand.NewSource(seed + int64(s)))
		for i := 0; i < warm; i++ {
			m.Observe(key, math.Abs(rng.NormFloat64()))
		}
		for i := 0; i < window; i++ {
			z := math.Abs(rng.NormFloat64())
			warn, critical, _ := m.Observe(key, z)
			if z >= warn {
				warnHits++
			}
			if z >= critical {
				critHits++
			}
		}
	}
	total := float64(series * window)
	return float64(warnHits) / total, float64(critHits) / total
}

func assertWithinBudget(t *testing.T, label string, got, budget, tolerance float64) {
	t.Helper()
	if got < budget*(1-tolerance) || got > budget*(1+tolerance) {
		t.Errorf("%s: alerting on %.5f%% of points against a %.5f%% budget — outside the stated ±%.0f%%",
			label, 100*got, 100*budget, 100*tolerance)
	}
}

// exceedancesNeeded is how many points beyond a threshold a series has to
// have seen before that threshold can be called learned rather than guessed.
// It is stated here in the tests' own terms — a budget and a count of
// exceedances — rather than borrowed from the model, so that the two tests
// below measure the promise and not the implementation of it.
const exceedancesNeeded = 2

// TestThresholds_MeetTheWarningBudgetOnceItsTailIsEstimable and its paging
// sibling below are the readiness regression.
//
// "Ready" used to mean 2 000 points for both thresholds, a constant with no
// relation to either budget. For the warning threshold that is about right
// by accident. For the page threshold it was not: with a multiplicative
// Robbins-Monro step of 0.02, every non-exceeding point moved a threshold by
// step x target = 2e-6 of itself, so from criticalSigma it needed of the
// order of 10^5 points of uninterrupted downward drift to reach the tail of
// an ordinary series. Measured on |N(0,1)|: a realised page rate of exactly
// zero at 2 000 points and 0.00003% at 10 000 against a 0.01% budget, with
// the model reporting itself calibrated for all of it. seasonalKey gives
// each hour of the day its own key at about 360 points a day, so a promise
// of one page per ten thousand points was being kept as no pages at all for
// the best part of a year — and critical is the label pagingModel learns
// from.
//
// Both tests measure the same way: warm a series up to where its tail holds
// the couple of exceedances any estimator needs, then measure what the
// thresholds in force actually do over a window long enough for the rate to
// mean something.
func TestThresholds_MeetTheWarningBudgetOnceItsTailIsEstimable(t *testing.T) {
	warm := int(exceedancesNeeded / targetWarnRate)
	warnRate, _ := budgetRates(2026, 40, warm, 10000)
	assertWithinBudget(t, "warning", warnRate, targetWarnRate, 0.25)
}

func TestThresholds_MeetThePagingBudgetOnceItsTailIsEstimable(t *testing.T) {
	warm := int(exceedancesNeeded / targetCriticalRate)
	warnRate, critRate := budgetRates(4051, 20, warm, 100000)
	assertWithinBudget(t, "paging", critRate, targetCriticalRate, 0.40)
	// By here the warning threshold has ten times the history it needs, so
	// it should be tighter than the test above requires.
	assertWithinBudget(t, "warning", warnRate, targetWarnRate, 0.15)
}
