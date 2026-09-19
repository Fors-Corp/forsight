package forseer

import (
	"math/rand"
	"testing"
	"time"
)

// What these tests are for.
//
// Exhausted returns a range, and a range is a claim: the budget runs out
// somewhere between these two times. A claim like that is only worth
// printing if it is true about as often as it says it is. Too narrow and
// the dashboard is confidently wrong; too wide and the significance gate in
// exhaustedLocked closes and there is no projection at all. Neither failure
// shows up in a test that checks one series against one expected number,
// which is why these run thousands of series and count.
//
// The yardstick is deliberately the process Holt is a model OF: a local
// linear trend where a single shock moves both the level and the trend, so
// the trend itself wanders. That is the process the interval is a statement
// about, and it is also the honest one for an error budget, whose burn rate
// changes — a deploy goes out, a dependency degrades, traffic shifts. A
// ramp with a FIXED slope plus noise is an easier world than that: the true
// exhaustion time barely moves, so almost any band covers it and coverage
// stops discriminating. That case is still worth a test, but for whether
// the model still speaks at all (see the ramp test below), not for whether
// the band is the right size.

// linearTrendSeries is the process above, generated one observation at a
// time so a test can keep drawing past the point the model stopped
// watching and find out when the budget really did run out.
//
//	y_t = l_{t-1} + b_{t-1} + e_t
//	l_t = l_{t-1} + b_{t-1} + alpha·e_t
//	b_t = b_{t-1} + alpha·beta·e_t
type linearTrendSeries struct {
	rng          *rand.Rand
	level, trend float64
	sigma        float64
}

func (s *linearTrendSeries) next() float64 {
	e := s.rng.NormFloat64() * s.sigma
	y := s.level + s.trend + e
	s.level += s.trend + holtAlpha*e
	s.trend += holtAlpha * holtBeta * e
	return y
}

// observeAhead feeds n observations into f and then keeps drawing, up to
// ahead more, to find when the series first reaches 100%. crossed is false
// when it never did, which is not a failure — that draw simply has no true
// answer to compare a projection against.
func observeAhead(f *burnForecast, s *linearTrendSeries, n, ahead int, step time.Duration) (actual time.Duration, crossed bool) {
	at := time.Unix(0, 0)
	for i := 0; i < n; i++ {
		f.Observe(s.next(), at)
		at = at.Add(step)
	}
	for j := 1; j <= ahead; j++ {
		if s.next() >= 100 {
			return time.Duration(j) * step, true
		}
	}
	return 0, false
}

// TestForecast_IntervalIsCalibratedAgainstTheProcessItModels is the one that
// pins the width of the band.
//
// The model claims a two-standard-deviation interval, which for normal
// errors is about 95%: roughly nineteen times in twenty the budget should
// run out inside the range that was printed. The assertion is a band around
// that rather than a point, because 95% is itself an approximation — the
// errors are not exactly normal, the smoothing constants are fixed rather
// than fitted, and the crossing time is a nonlinear function of a forecast
// interval on the value.
//
// Each row is a different distance to project over, from a few dozen
// observations to several hundred, because that is the axis the old band
// got wrong: it was a constant, so it could be right at one distance and
// nowhere else.
func TestForecast_IntervalIsCalibratedAgainstTheProcessItModels(t *testing.T) {
	const (
		draws = 2000
		step  = 10 * time.Second
		// Two standard deviations, the width the model advertises.
		nominal   = 0.95
		tolerance = 0.05
	)

	for _, tc := range []struct {
		name         string
		trend, sigma float64
		observations int
	}{
		{"a fast burn, a few dozen observations out", 0.8, 0.6, 60},
		{"a moderate burn", 0.4, 0.3, 100},
		{"a slow burn, a couple of hundred out", 0.2, 0.15, 80},
		{"a very slow burn, most of a thousand out", 0.1, 0.05, 150},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var projected, covered, sooner, later, comparable int
			for seed := 0; seed < draws; seed++ {
				f := newBurnForecast()
				s := &linearTrendSeries{
					rng:   rand.New(rand.NewSource(int64(seed))),
					trend: tc.trend,
					sigma: tc.sigma,
				}
				actual, crossed := observeAhead(f, s, tc.observations, 5000, step)
				if !crossed {
					continue
				}
				comparable++
				soonest, latest, ok := f.Exhausted()
				if !ok {
					continue
				}
				projected++
				switch {
				case actual < soonest:
					sooner++
				// A zero upper bound means "past the reporting horizon",
				// which is an open-ended claim and cannot be contradicted
				// from above.
				case latest != 0 && actual > latest:
					later++
				default:
					covered++
				}
			}

			if projected == 0 {
				t.Fatal("the model never projected at all; there is no interval to judge")
			}
			// A band nobody ever sees is not calibrated, it is silent. The
			// gate is allowed to close on the unlucky draws, not on most.
			if rate := float64(projected) / float64(comparable); rate < 0.8 {
				t.Errorf("projected on %.1f%% of draws; the significance gate is closing on series with a real trend", 100*rate)
			}

			coverage := float64(covered) / float64(projected)
			if coverage < nominal-tolerance || coverage > nominal+tolerance {
				t.Errorf("the interval contained the real exhaustion %.1f%% of the time, want %.0f%% ± %.0f "+
					"(%d of %d draws; %d ran out sooner than the range, %d later)",
					100*coverage, 100*nominal, 100*tolerance, covered, projected, sooner, later)
			}
		})
	}
}

// TestForecast_BandWidensWithTheDistanceProjected is the same defect seen
// without any simulation: a projection further away must carry a wider band
// than a near one drawn from exactly the same evidence.
//
// Both models here are fed the identical series, offset by a constant. Holt
// is shift-invariant — the level absorbs the offset and the trend and every
// one-step error are untouched — so the two differ in one thing only: how
// far they have to project. The near one is about sixty observations from
// 100%, the far one about four times that.
//
// The old band was a fixed multiple of the one-step error with no h in it
// at all, which made the ratio of the two edges (trend+band)/(trend−band)
// identical for both models — the same relative uncertainty whether the
// budget ran out in ten minutes or in three hours.
func TestForecast_BandWidensWithTheDistanceProjected(t *testing.T) {
	const (
		observations = 120
		trend        = 0.2
		sigma        = 0.25
		step         = 10 * time.Second
	)
	climb := trend * float64(observations-1)

	// Chosen so each model ends up the stated number of observations short
	// of 100%, which is the only difference between them.
	near := newBurnForecast()
	far := newBurnForecast()
	nearStart := 100 - trend*60 - climb
	farStart := 100 - trend*240 - climb

	rng := rand.New(rand.NewSource(7))
	at := time.Unix(0, 0)
	for i := 0; i < observations; i++ {
		v := trend*float64(i) + rng.NormFloat64()*sigma
		near.Observe(nearStart+v, at)
		far.Observe(farStart+v, at)
		at = at.Add(step)
	}

	spread := func(t *testing.T, name string, f *burnForecast) float64 {
		t.Helper()
		soonest, latest, ok := f.Exhausted()
		if !ok {
			t.Fatalf("%s projection was withheld; there is no band to measure", name)
		}
		if latest == 0 || soonest <= 0 {
			t.Fatalf("%s projection is open-ended (%v, %v); there is no band to measure", name, soonest, latest)
		}
		return float64(latest) / float64(soonest)
	}

	nearSpread := spread(t, "near", near)
	farSpread := spread(t, "far", far)
	if farSpread <= nearSpread {
		t.Fatalf("a projection four times further out was no less certain: near %.2fx, far %.2fx", nearSpread, farSpread)
	}
	// Not just different, but materially so: the uncertainty in a Holt
	// trend accumulates with every step projected, so four times the
	// distance is a band well over half again as wide.
	if farSpread < 1.5*nearSpread {
		t.Errorf("the band barely grew over four times the distance: near %.2fx, far %.2fx", nearSpread, farSpread)
	}
}

// TestForecast_CoversAndStillProjectsOnANoisyRamp is the easier world named
// at the top of this file: a fixed slope with noise on top, where the true
// exhaustion time hardly moves from draw to draw. Coverage cannot fail here
// for any band that is not absurdly narrow, so what this pins is the other
// direction — that the band stays small enough for the model to say
// anything at all, on a series whose trend is obvious to the eye.
func TestForecast_CoversAndStillProjectsOnANoisyRamp(t *testing.T) {
	const (
		draws        = 500
		observations = 100
		trend        = 0.4
		sigma        = 0.3
		step         = 10 * time.Second
	)
	// Where the underlying ramp, noise aside, reaches 100%.
	actual := time.Duration((100/trend - float64(observations-1)) * float64(step))

	var projected, covered int
	for seed := 0; seed < draws; seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		f := newBurnForecast()
		at := time.Unix(0, 0)
		for i := 0; i < observations; i++ {
			f.Observe(trend*float64(i)+rng.NormFloat64()*sigma, at)
			at = at.Add(step)
		}
		soonest, latest, ok := f.Exhausted()
		if !ok {
			continue
		}
		projected++
		if actual >= soonest && (latest == 0 || actual <= latest) {
			covered++
		}
	}

	if projected < draws*9/10 {
		t.Errorf("projected on only %d of %d draws from a clearly rising budget", projected, draws)
	}
	if covered < projected*9/10 {
		t.Errorf("the interval missed the real exhaustion on %d of %d projections", projected-covered, projected)
	}
}

// TestForecast_SnapshotCarriesTheErrorTermTheBandIsSizedFrom guards the half
// of the round trip that a clean ramp cannot see. Restoring level, trend,
// count and cadence but not the accumulated error would reproduce the
// projection exactly on a noiseless series — every band there is zero — and
// come back on a real one with no uncertainty at all, publishing a single
// time as if it were certain.
func TestForecast_SnapshotCarriesTheErrorTermTheBandIsSizedFrom(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	f := newBurnForecast()
	at := time.Unix(0, 0)
	for i := 0; i < 80; i++ {
		f.Observe(float64(i)*0.5+rng.NormFloat64()*0.4, at)
		at = at.Add(10 * time.Second)
	}

	wantSoonest, wantLatest, ok := f.Exhausted()
	if !ok {
		t.Fatal("no projection from a noisy but rising burn; nothing to round-trip")
	}
	if wantLatest <= wantSoonest {
		t.Fatalf("the warm model reported no band at all (%v, %v) on a series with real error", wantSoonest, wantLatest)
	}

	data, err := f.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	restored := newBurnForecast()
	if err := restored.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	gotSoonest, gotLatest, gotOK := restored.Exhausted()
	if !gotOK || gotSoonest != wantSoonest || gotLatest != wantLatest {
		t.Fatalf("restored Exhausted() = (%v, %v, %v), want (%v, %v, true)",
			gotSoonest, gotLatest, gotOK, wantSoonest, wantLatest)
	}
}
