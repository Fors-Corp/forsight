package forseer

import (
	"fmt"
	"math/rand"
	"testing"
)

// BenchmarkDetector_ObserveSingleSeries measures the steady-state cost of
// the per-record path once a series is past minSamples and warn/critical are
// being computed on every point: Welford's update, the threshold model
// lookup, and the CUSUM changepoint check.
func BenchmarkDetector_ObserveSingleSeries(b *testing.B) {
	d := NewDetector()
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < minSamples; i++ {
		d.Observe([]Point{{Name: "host.cpu.percent", Value: 10 + rng.NormFloat64()}})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Observe([]Point{{Name: "host.cpu.percent", Value: 10 + rng.NormFloat64()}})
	}
}

// BenchmarkDetector_ObserveAtSeriesCap fills the detector to maxSeries — the
// bound its own cardinality guard enforces — and measures one Observe call
// touching all of them, the realistic worst case for a single ingest batch
// on an agent watching every series it is allowed to.
func BenchmarkDetector_ObserveAtSeriesCap(b *testing.B) {
	d := NewDetector()
	rng := rand.New(rand.NewSource(2))
	points := make([]Point, maxSeries)
	for i := range points {
		points[i] = Point{Name: fmt.Sprintf("metric.%d", i), Value: 10}
	}
	// Warm every series past minSamples so the benchmark measures the
	// threshold + CUSUM path, not the cold no-op branch.
	for i := 0; i < minSamples; i++ {
		batch := make([]Point, len(points))
		for j, p := range points {
			batch[j] = Point{Name: p.Name, Value: 10 + rng.NormFloat64()}
		}
		d.Observe(batch)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batch := make([]Point, len(points))
		for j, p := range points {
			batch[j] = Point{Name: p.Name, Value: 10 + rng.NormFloat64()}
		}
		d.Observe(batch)
	}
}
