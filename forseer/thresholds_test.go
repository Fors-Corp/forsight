package forseer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
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

func TestThresholds_BecomeReadyOnlyWithEnoughHistory(t *testing.T) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(1))

	for i := 0; i < thresholdMinSamples-1; i++ {
		if _, _, ready := m.Observe("host.cpu", math.Abs(rng.NormFloat64())); ready {
			t.Fatalf("reported calibrated after %d points, minimum is %d", i+1, thresholdMinSamples)
		}
	}
	if _, _, ready := m.Observe("host.cpu", math.Abs(rng.NormFloat64())); !ready {
		t.Fatal("still not calibrated at the minimum sample count")
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

	for i := 0; i < maxSeries*2; i++ {
		m.Observe(string(rune('a'+i%26))+string(rune(i)), 1)
	}

	m.mu.Lock()
	tracked := len(m.series)
	m.mu.Unlock()
	if tracked > maxSeries {
		t.Errorf("tracking %d series, cap is %d", tracked, maxSeries)
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
	feed(m, "host.cpu", thresholdMinSamples*3, func() float64 { return rng.NormFloat64() })

	warm := m.Card()
	if !warm.Ready {
		t.Error("card still not ready after a well-populated series")
	}
	if warm.Detail == cold.Detail {
		t.Error("detail did not change once the model had something to say")
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
	if s.n < thresholdMinSamples {
		t.Fatalf("only %d points reached the model", s.n)
	}
	if s.warn <= warningSigma {
		t.Errorf("warning threshold settled at %.2f, no higher than the fixed %d it replaced", s.warn, warningSigma)
	}
}

// TestThresholds_SnapshotRestoreRoundTrip is roadmap item 25's proof for
// this model. Unlike severity's, there is no grading window to reset here:
// a calibration is restored whole, n included, so a series that was already
// calibrated stays calibrated across a restart.
func TestThresholds_SnapshotRestoreRoundTrip(t *testing.T) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(21))
	feed(m, "host.cpu", thresholdMinSamples*2, func() float64 { return rng.NormFloat64() })

	m.mu.Lock()
	want := *m.series["host.cpu"]
	m.mu.Unlock()

	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := newThresholdModel()
	if err := restored.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	restored.mu.Lock()
	got, ok := restored.series["host.cpu"]
	restored.mu.Unlock()
	if !ok {
		t.Fatal("restored model has no host.cpu series")
	}
	if *got != want {
		t.Fatalf("restored series = %+v, want %+v", *got, want)
	}
	if !restored.Card().Ready {
		t.Fatal("restored model is not ready even though its series already had thresholdMinSamples")
	}
}

func TestThresholds_RestoreDiscardsAVersionMismatch(t *testing.T) {
	m := newThresholdModel()
	feed(m, "host.cpu", thresholdMinSamples, func() float64 { return 1 })
	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1)

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
	snap := thresholdSnapshot{
		Version: thresholdSnapshotVersion,
		Series:  make(map[string]seriesThresholdSnapshot, total),
	}
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("series-%04d", i)
		snap.Series[key] = seriesThresholdSnapshot{
			Warn:     3 + float64(i)*0.001,
			Critical: 5 + float64(i)*0.001,
			N:        thresholdMinSamples + i,
			WarnHits: i,
			CritHits: i / 2,
		}
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal test snapshot: %v", err)
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
		if got.warn != want.Warn || got.critical != want.Critical || got.n != want.N ||
			got.warnHits != want.WarnHits || got.critHits != want.CritHits {
			t.Errorf("series %q = %+v, want %+v", key, *got, want)
		}
	}
}
