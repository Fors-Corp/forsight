package forseer

import (
	"math"
	"sort"
)

// p2Estimator is the P² ("piecewise-parabolic") algorithm — Jain & Chlamtac,
// "The P² Algorithm for Dynamic Calculation of Quantiles and Histograms
// Without Storing Observations", CACM 1985. It tracks one quantile of a
// stream with exactly five markers, updated in O(1) time and space per
// observation: no buffered sample, no sort, no assumption about the shape
// of the distribution.
//
// Marker i (0-indexed) approximates the value at cumulative probability:
//
//	0: 0        the minimum
//	1: p/2
//	2: p        <- the quantile this estimator reports
//	3: (1+p)/2
//	4: 1        the maximum
//
// For p=0.5 those five probabilities are exactly 0, 0.25, 0.5, 0.75, 1 —
// a box plot's five numbers — which is why spanWatch's p50 tracker doubles
// as the source for a later BoxPlotBox view (min, q1, median, q3, max)
// without carrying any extra state.
type p2Estimator struct {
	p float64 // target quantile, 0 < p < 1

	n       int       // observations seen (including the first five)
	initial []float64 // buffered raw values until the fifth observation

	height  [5]float64 // marker heights — height[2] is the estimate
	pos     [5]int     // marker positions
	desired [5]float64 // desired (possibly fractional) positions
	incr    [5]float64 // how far each desired position moves per observation
}

func newP2Estimator(p float64) *p2Estimator {
	return &p2Estimator{p: p, initial: make([]float64, 0, 5)}
}

// value reports the current quantile estimate, and whether the estimator
// has seen the five observations it needs before it has one at all.
func (e *p2Estimator) value() (float64, bool) {
	if e.n < 5 {
		return 0, false
	}
	return e.height[2], true
}

// reset discards every observation. Used when a CUSUM changepoint says the
// series just moved to a new regime: blending the old baseline into the new
// one marker at a time would take as long to forget as it took to learn, so
// instead the estimator starts over and re-earns its baseline from scratch.
func (e *p2Estimator) reset() {
	p := e.p
	*e = p2Estimator{p: p, initial: make([]float64, 0, 5)}
}

// restoreP2Checked rebuilds an estimator from the snapshot shape spans.go
// already defines (p2EstimatorSnapshot, snapshotP2, restoreP2), reporting
// false for any state this algorithm could not have produced — including one
// tracking a different quantile than the caller expects.
//
// The check is not defensive politeness. A snapshot is a file on disk that
// an operator can edit and a truncated write can corrupt, and the P² update
// divides by differences between marker positions: positions that are equal
// or out of order produce ±Inf and then NaN heights. A NaN threshold
// compares false against every z forever, so the series would never alert
// again — silently, with no error and nothing on the card to read. Rejecting
// the state costs that series its learned calibration and it re-earns it;
// accepting it costs that series every future alert.
func restoreP2Checked(s p2EstimatorSnapshot, wantP float64) (*p2Estimator, bool) {
	if s.P != wantP || s.N < 0 || len(s.Initial) > 5 {
		return nil, false
	}
	for _, v := range s.Initial {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, false
		}
	}
	if s.N < 5 {
		// Still buffering the first five observations: the markers are not
		// in use yet, so the buffer is the whole state.
		if len(s.Initial) != s.N {
			return nil, false
		}
		return restoreP2(s), true
	}
	if len(s.Initial) != 5 || s.Pos[0] != 1 || s.Pos[4] != s.N {
		return nil, false
	}
	for i := 0; i < 5; i++ {
		if math.IsNaN(s.Height[i]) || math.IsInf(s.Height[i], 0) ||
			math.IsNaN(s.Desired[i]) || math.IsInf(s.Desired[i], 0) ||
			math.IsNaN(s.Incr[i]) || math.IsInf(s.Incr[i], 0) {
			return nil, false
		}
		if i > 0 && (s.Pos[i] <= s.Pos[i-1] || s.Height[i] < s.Height[i-1]) {
			return nil, false
		}
	}
	return restoreP2(s), true
}

// observe feeds one value into the estimator.
func (e *p2Estimator) observe(x float64) {
	e.n++
	if len(e.initial) < 5 {
		e.initial = append(e.initial, x)
		if len(e.initial) < 5 {
			return
		}
		sort.Float64s(e.initial)
		for i, v := range e.initial {
			e.height[i] = v
			e.pos[i] = i + 1
		}
		p := e.p
		e.desired = [5]float64{1, 1 + 2*p, 1 + 4*p, 3 + 2*p, 5}
		e.incr = [5]float64{0, p / 2, p, (1 + p) / 2, 1}
		return
	}

	// Which of the four cells x falls in, clamping the extremes into the
	// outer markers so height[0] and height[4] always track the running
	// min and max exactly.
	k := 3
	switch {
	case x < e.height[0]:
		e.height[0] = x
		k = 0
	case x >= e.height[4]:
		e.height[4] = x
		k = 3
	default:
		for i := 1; i <= 3; i++ {
			if x < e.height[i] {
				k = i - 1
				break
			}
		}
	}

	for i := k + 1; i < 5; i++ {
		e.pos[i]++
	}
	for i := range e.desired {
		e.desired[i] += e.incr[i]
	}

	for i := 1; i <= 3; i++ {
		d := e.desired[i] - float64(e.pos[i])
		if (d >= 1 && e.pos[i+1]-e.pos[i] > 1) || (d <= -1 && e.pos[i-1]-e.pos[i] < -1) {
			sign := 1
			if d < 0 {
				sign = -1
			}
			if q := e.parabolic(i, sign); e.height[i-1] < q && q < e.height[i+1] {
				e.height[i] = q
			} else {
				e.height[i] = e.linear(i, sign)
			}
			e.pos[i] += sign
		}
	}
}

// parabolic is the P² algorithm's parabolic-interpolation formula for
// moving marker i by d (±1) positions.
func (e *p2Estimator) parabolic(i, d int) float64 {
	df := float64(d)
	n0, n1, n2 := float64(e.pos[i-1]), float64(e.pos[i]), float64(e.pos[i+1])
	q0, q1, q2 := e.height[i-1], e.height[i], e.height[i+1]
	return q1 + df/(n2-n0)*((n1-n0+df)*(q2-q1)/(n2-n1)+
		(n2-n1-df)*(q1-q0)/(n1-n0))
}

// linear is the fallback formula when the parabolic estimate would not stay
// strictly between the neighbouring markers.
func (e *p2Estimator) linear(i, d int) float64 {
	j := i + d
	return e.height[i] + float64(d)*(e.height[j]-e.height[i])/float64(e.pos[j]-e.pos[i])
}
