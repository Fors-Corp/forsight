package forseer

import (
	"fmt"
	"math/rand"
	"testing"
)

// BenchmarkThresholdModel_ObserveWarmingUp measures the update path — one
// P² marker update per threshold plus the hit-count bookkeeping — while a
// series is still answering with the fixed sigma constants and building the
// quantile estimates that will replace them.
func BenchmarkThresholdModel_ObserveWarmingUp(b *testing.B) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(1))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Observe("host.cpu", absNormal(rng))
	}
}

// BenchmarkThresholdModel_ObserveCalibrated measures the same path once both
// thresholds are learned, the steady state an agent spends almost all its
// time in.
func BenchmarkThresholdModel_ObserveCalibrated(b *testing.B) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < thresholdCriticalMinSamples; i++ {
		m.Observe("host.cpu", absNormal(rng))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Observe("host.cpu", absNormal(rng))
	}
}

// BenchmarkThresholdModel_ObserveAtSeriesCap fills the model to maxSeries —
// the same cardinality bound the detector shares its series map with — and
// measures one Observe call per series in rotation, the realistic cost of
// an agent watching every series it is allowed to.
func BenchmarkThresholdModel_ObserveAtSeriesCap(b *testing.B) {
	m := newThresholdModel()
	rng := rand.New(rand.NewSource(3))
	keys := make([]string, maxSeries)
	for i := range keys {
		keys[i] = fmt.Sprintf("metric.%d", i)
		m.Observe(keys[i], absNormal(rng))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Observe(keys[i%len(keys)], absNormal(rng))
	}
}

func absNormal(rng *rand.Rand) float64 {
	z := rng.NormFloat64()
	if z < 0 {
		z = -z
	}
	return z
}
