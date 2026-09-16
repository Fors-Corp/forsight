package forseer

import (
	"math"
	"math/rand"
	"testing"
)

// TestP2Estimator_NotReadyBeforeFiveObservations pins the "no buffered
// sample" boundary: the estimator has nothing to report until the fifth
// point seeds its five markers.
func TestP2Estimator_NotReadyBeforeFiveObservations(t *testing.T) {
	e := newP2Estimator(0.5)
	for i := 0; i < 4; i++ {
		e.observe(float64(i))
		if _, ok := e.value(); ok {
			t.Fatalf("value ready after %d observations, want 5", i+1)
		}
	}
	e.observe(4)
	if _, ok := e.value(); !ok {
		t.Fatal("value still not ready after the fifth observation")
	}
}

// TestP2Estimator_TracksKnownQuantiles feeds a shuffled 1..2000 stream — a
// known distribution, not a synthetic one built to flatter the estimator —
// and checks the streaming p50/p99 land close to the true order statistics,
// using only O(1) state per point rather than a sorted buffer of all 2000.
func TestP2Estimator_TracksKnownQuantiles(t *testing.T) {
	const n = 2000
	vals := make([]float64, n)
	for i := range vals {
		vals[i] = float64(i + 1)
	}
	r := rand.New(rand.NewSource(1))
	r.Shuffle(len(vals), func(i, j int) { vals[i], vals[j] = vals[j], vals[i] })

	p50, p99 := newP2Estimator(0.5), newP2Estimator(0.99)
	for _, v := range vals {
		p50.observe(v)
		p99.observe(v)
	}

	wantP50, wantP99 := 1000.5, 1980.0
	gotP50, ok := p50.value()
	if !ok {
		t.Fatal("p50 not ready after 2000 observations")
	}
	gotP99, ok := p99.value()
	if !ok {
		t.Fatal("p99 not ready after 2000 observations")
	}

	// 2% of the true value: the P² algorithm is an approximation by design,
	// not a sorted-buffer exact quantile, so the bar is "close", not exact.
	if tol := 0.02 * wantP50; math.Abs(gotP50-wantP50) > tol {
		t.Errorf("p50 = %.1f, want within %.1f of %.1f", gotP50, tol, wantP50)
	}
	if tol := 0.02 * wantP99; math.Abs(gotP99-wantP99) > tol {
		t.Errorf("p99 = %.1f, want within %.1f of %.1f", gotP99, tol, wantP99)
	}
}

// TestP2Estimator_MarkersMapToBoxPlotBox checks the property MODELS.md's
// roadmap entry leans on: a median-targeted (p=0.5) P² estimator's five
// markers ARE min, q1, median, q3, max, in that order, with no extra state
// needed for a later BoxPlotBox view.
func TestP2Estimator_MarkersMapToBoxPlotBox(t *testing.T) {
	e := newP2Estimator(0.5)
	vals := make([]float64, 500)
	r := rand.New(rand.NewSource(2))
	for i := range vals {
		vals[i] = r.Float64() * 100
	}
	for _, v := range vals {
		e.observe(v)
	}
	if e.height[0] > e.height[1] || e.height[1] > e.height[2] ||
		e.height[2] > e.height[3] || e.height[3] > e.height[4] {
		t.Fatalf("markers not monotonic: %v, want min<=q1<=median<=q3<=max", e.height)
	}
	median, ok := e.value()
	if !ok || median != e.height[2] {
		t.Fatalf("value() = %v, %v; want the 3rd marker (median)", median, ok)
	}
}

// TestP2Estimator_ResetDiscardsHistory checks the CUSUM-changepoint path:
// reset must throw away every observation, not blend the old baseline into
// the new one, so the estimator needs five fresh points before it has a
// value again.
func TestP2Estimator_ResetDiscardsHistory(t *testing.T) {
	e := newP2Estimator(0.99)
	for i := 0; i < 20; i++ {
		e.observe(float64(i))
	}
	if _, ok := e.value(); !ok {
		t.Fatal("setup: expected a value before reset")
	}
	e.reset()
	if _, ok := e.value(); ok {
		t.Fatal("value still ready immediately after reset")
	}
	for i := 0; i < 4; i++ {
		e.observe(1000)
		if _, ok := e.value(); ok {
			t.Fatalf("value ready after only %d post-reset observations", i+1)
		}
	}
	e.observe(1000)
	got, ok := e.value()
	if !ok {
		t.Fatal("value not ready after five post-reset observations")
	}
	if got != 1000 {
		t.Fatalf("value = %v after reset and five identical points, want 1000 (no trace of the discarded history)", got)
	}
}
