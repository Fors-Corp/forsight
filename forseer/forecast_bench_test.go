package forseer

import (
	"testing"
	"time"
)

// BenchmarkBurnForecast_ObserveWarmingUp measures the per-record path before
// forecastMinObservations: the Holt level/trend update and cadence
// estimate, with no grading yet because there is no prior prediction.
func BenchmarkBurnForecast_ObserveWarmingUp(b *testing.B) {
	f := newBurnForecast()
	at := time.Unix(0, 0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N && i < forecastMinObservations; i++ {
		f.Observe(float64(i%100), at)
		at = at.Add(10 * time.Second)
	}
}

// BenchmarkBurnForecast_ObserveSteady measures the steady-state cost once
// the model is past forecastMinObservations and every call also grades the
// previous step's Holt and persistence predictions — the realistic per-tick
// cost of a burn being watched continuously.
func BenchmarkBurnForecast_ObserveSteady(b *testing.B) {
	f := newBurnForecast()
	at := time.Unix(0, 0)
	for i := 0; i < forecastMinObservations; i++ {
		f.Observe(float64(i)*0.1, at)
		at = at.Add(10 * time.Second)
	}

	b.ReportAllocs()
	b.ResetTimer()
	consumed := float64(forecastMinObservations) * 0.1
	for i := 0; i < b.N; i++ {
		consumed += 0.01
		if consumed > 100 {
			consumed = 0
		}
		f.Observe(consumed, at)
		at = at.Add(10 * time.Second)
	}
}

// BenchmarkBurnForecast_Exhausted measures the read path: computing the
// significance band and, when the trend clears it, the two projected
// durations.
func BenchmarkBurnForecast_Exhausted(b *testing.B) {
	f := burn(ramp(0, 1, forecastGradeWindow))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Exhausted()
	}
}
