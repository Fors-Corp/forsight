package forseer

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

// The alert-threshold model: given a series, learn how far out of line it
// has to go before a human should hear about it.
//
// Every series shares 3σ and 5σ today. Those are the right shape of answer
// and the wrong number for almost every series, because sigma only means
// "rare" if the series is normally distributed, and metrics are not. A CPU
// percentage bounded at 100 is skewed. A request rate has a daily cycle. A
// queue depth is mostly zero with occasional spikes, so its standard
// deviation is dominated by the spikes it is supposed to be detecting. The
// result an operator actually sees is one noisy series paging every few
// minutes and a smooth one that never fires at all, and the only recourse
// they have is to stop trusting alerts.
//
// So learn the threshold instead of choosing it, per series, from the
// series' own history — and state the budget in the unit an operator can
// reason about. "Alert on roughly one point in a thousand" is a sentence
// somebody can hold an opinion about. "Three sigma" is not, unless they
// already know the distribution, which is the thing nobody knows.
//
// A budget stated as a rate is a quantile: alert on one point in a thousand
// is "alert above this series' 99.9th percentile of |z|", and page is the
// 99.99th. So each threshold is one running quantile estimate, and the
// estimator is the P² algorithm already in this package (p2.go) — five
// markers, O(1) per point, no stored sample and no assumption about the
// shape of the distribution.
//
// It replaces a Robbins-Monro stochastic approximation,
// `t ← t · (1 + step · (exceeded − target))`, which is the textbook way to
// chase a quantile and settles in the right place — eventually. Eventually
// is the problem, and it is not a detail: with the constant step this model
// used (0.02), every non-exceeding point pulls a threshold down by only
// step·target of itself, which is 2e-6 for the page threshold. Starting at
// criticalSigma, reaching the tail of an ordinary |N(0,1)| series takes on
// the order of 10^5 points of pure downward drift, and measured on that data
// the realised page rate was exactly zero at 2 000 points, 0.00003% at
// 10 000 against a 0.01% budget. seasonalKey gives each hour of the day its
// own key, so an hourly key collects about 360 points a day: the page
// threshold would have converged some time in the following year. The P²
// estimator converges in the number of points the tail itself requires and
// no more — and that number, not a constant somebody picked, is what
// readiness is gated on below.
const (
	// One alert per thousand points, and one page per ten thousand. At a
	// ten-second collect interval that is about nine warnings and one
	// critical per series per day — a budget, not a guess about what the
	// data looks like.
	targetWarnRate     = 1.0 / 1000
	targetCriticalRate = 1.0 / 10000

	// The threshold cannot wander anywhere. Below the floor it would alert
	// on ordinary variation whatever the budget says; above the ceiling a
	// series that genuinely went haywire would never be reported. Both are
	// generous: the floor is well inside today's 3σ and the ceiling well
	// outside today's 5σ.
	thresholdFloor   = 2.0
	thresholdCeiling = 12.0

	// How many points in the tail a threshold needs before it is used
	// instead of its constant. A quantile at tail probability q is estimated
	// from the points that land beyond it, and a series has seen about n·q
	// of those: no method — P², Robbins-Monro or an exact sort — can know
	// where the one-in-ten-thousand point of a distribution is from a sample
	// that contains none.
	//
	// Two expected exceedances is where the measurement says the estimate
	// becomes worth more than the constant it replaces. On |N(0,1)|, the P²
	// estimate at that point is 3.25 against a true 0.999 quantile of 3.29
	// and 3.86 against a true 0.9999 of 3.89, which realises 1.15x the
	// budgeted alert rate — and tightens from there: within 3% of budget by
	// 50 000 points. Anything earlier is measurably worse: at one expected
	// exceedance the estimate is 3.06/3.68 and realises 2.2x the budget.
	thresholdReadyExceedances = 2

	// Which, per threshold, is the sample count each of them waits for. They
	// differ by the factor their budgets differ by, and that is the honest
	// shape of the answer: a warning threshold is ready ten times sooner
	// than a page threshold because it is asking a question ten times
	// easier. At a ten-second interval and seasonalKey's per-hour buckets
	// (about 360 points a day per key) that is roughly six days for the
	// warning and eight weeks for the page; on a series that is not
	// hour-split, sixteen hours and a week. Snapshot/Restore carries both
	// across restarts so the wait is paid once, not once per restart.
	thresholdWarnMinSamples     = int(thresholdReadyExceedances / targetWarnRate)
	thresholdCriticalMinSamples = int(thresholdReadyExceedances / targetCriticalRate)
)

// seriesThreshold is one series' pair of running quantile estimates, plus
// what its thresholds have actually been doing — which is the only honest
// way to report a calibration that has no labels to be scored against.
type seriesThreshold struct {
	warn     *p2Estimator
	critical *p2Estimator
	n        int
	warnHits int
	critHits int
}

func newSeriesThreshold() *seriesThreshold {
	return &seriesThreshold{
		warn:     newP2Estimator(1 - targetWarnRate),
		critical: newP2Estimator(1 - targetCriticalRate),
	}
}

// thresholds reports the pair to use for the next point: each estimate once
// its own estimator has the samples that quantile needs, and the fixed sigma
// constant until then. ready means both are learned — a caller that only
// wants to know whether this series still leans on the constants.
func (s *seriesThreshold) thresholds() (warn, critical float64, ready bool) {
	warn, critical = float64(warningSigma), float64(criticalSigma)
	if s.n >= thresholdWarnMinSamples {
		if v, ok := s.warn.value(); ok {
			warn = clampThreshold(v)
		}
	}
	if s.n >= thresholdCriticalMinSamples {
		if v, ok := s.critical.value(); ok {
			critical = clampThreshold(v)
		}
	}
	// A page that is easier to reach than a warning is nonsense, and a
	// mid-warm-up series produces one honestly: a wild series' learned
	// warning can pass the fixed 5σ page it has not yet replaced.
	if critical < warn {
		critical = warn
	}
	return warn, critical, s.n >= thresholdCriticalMinSamples
}

type thresholdModel struct {
	mu     sync.Mutex
	series map[string]*seriesThreshold
}

func newThresholdModel() *thresholdModel {
	return &thresholdModel{series: make(map[string]*seriesThreshold)}
}

// Observe records one z-score for a series and folds it into that series'
// quantile estimates. It returns the pair to use for this point, and false
// while either of them is still the constant.
// maxThresholdKeys is this map's backstop, in the unit this map is actually
// keyed by: seasonalKey, so one real series is up to 24 of them. It is
// maxSeries real series' worth, which is the most the Detector can ever ask
// about — the Detector is what bounds real cardinality, and it calls Forget
// for every key of a series it evicts, so in practice this ceiling is never
// approached. Expressing it as a bare maxSeries, as it was, made this map run
// out at about 21 real series while reading as though it held 512.
const maxThresholdKeys = maxSeries * 24

func (m *thresholdModel) Observe(key string, z float64) (warn, critical float64, ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.series[key]
	if s == nil {
		if len(m.series) >= maxThresholdKeys {
			return warningSigma, criticalSigma, false
		}
		s = newSeriesThreshold()
		m.series[key] = s
	}

	// Grade against the thresholds that were in force when the point
	// arrived, before it moves them, so the counts describe what the
	// operator was actually alerted on.
	warn, critical, ready = s.thresholds()
	if z >= warn {
		s.warnHits++
	}
	if z >= critical {
		s.critHits++
	}
	s.n++
	s.warn.observe(z)
	s.critical.observe(z)
	return warn, critical, ready
}

func indicator(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func clampThreshold(t float64) float64 {
	return math.Min(thresholdCeiling, math.Max(thresholdFloor, t))
}

// Card implements Model.
func (m *thresholdModel) Card() Card {
	m.mu.Lock()
	defer m.mu.Unlock()

	warnReady, critReady, points, warnHits, critHits := 0, 0, 0, 0, 0
	for _, s := range m.series {
		points += s.n
		warnHits += s.warnHits
		critHits += s.critHits
		if s.n >= thresholdWarnMinSamples {
			warnReady++
		}
		if s.n >= thresholdCriticalMinSamples {
			critReady++
		}
	}

	detail := "no series has enough history yet"
	if points > 0 {
		detail = fmt.Sprintf(
			"warning on %.3f%% of points against a %.3f%% budget and paging on %.4f%% against %.4f%%; "+
				"of %d series, %d have a learned warning threshold and %d a learned page threshold",
			100*float64(warnHits)/float64(points), 100*targetWarnRate,
			100*float64(critHits)/float64(points), 100*targetCriticalRate,
			len(m.series), warnReady, critReady)
	}

	return Card{
		Name:     "alert thresholds",
		Job:      "Decide how far out of line one series has to go before a human should hear about it.",
		Reads:    []string{"the z-score of one series"},
		Fallback: "a fixed 3σ warning and 5σ critical, shared by every series",
		// A series is doing better than the fallback as soon as its warning
		// threshold is its own rather than everyone's; the page threshold
		// takes ten times as long, and the count for it is in Detail rather
		// than hidden behind one boolean.
		Ready:   warnReady > 0,
		Trained: points,
		// A calibration has no labels to be right or wrong about, so there
		// is no accuracy to report. What it does have is a budget, and
		// whether it is hitting it — which is in Detail.
		Accuracy:         Unmeasured,
		FallbackAccuracy: Unmeasured,
		Detail:           detail,
	}
}

// thresholdSnapshotVersion is this model's own schema version — see
// severitySnapshotVersion's comment for what that guards against. Version 2
// carries the P² markers behind each threshold; a version 1 payload held a
// Robbins-Monro warn/critical pair, which describes no state this model
// keeps any more, so it is discarded rather than half-read.
const thresholdSnapshotVersion = 2

// thresholdSnapshot is Snapshot's JSON payload: every series' two quantile
// estimators and the counts behind them.
type thresholdSnapshot struct {
	Version int                                `json:"version"`
	Series  map[string]seriesThresholdSnapshot `json:"series"`
}

type seriesThresholdSnapshot struct {
	Warn     p2EstimatorSnapshot `json:"warn"`
	Critical p2EstimatorSnapshot `json:"critical"`
	N        int                 `json:"n"`
	WarnHits int                 `json:"warnHits"`
	CritHits int                 `json:"critHits"`
}

// Forget drops the learned thresholds for a set of keys. The Detector passes
// every hour bucket of a series it is evicting, so threshold state cannot
// outlive the series it describes and slowly fill this map on its own.
func (m *thresholdModel) Forget(keys ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range keys {
		delete(m.series, key)
	}
}

// Snapshot implements Model.
func (m *thresholdModel) Snapshot() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := thresholdSnapshot{Version: thresholdSnapshotVersion, Series: make(map[string]seriesThresholdSnapshot, len(m.series))}
	for key, s := range m.series {
		snap.Series[key] = seriesThresholdSnapshot{
			Warn:     snapshotP2(s.warn),
			Critical: snapshotP2(s.critical),
			N:        s.n,
			WarnHits: s.warnHits,
			CritHits: s.critHits,
		}
	}
	return json.Marshal(snap)
}

// Restore implements Model. Unlike severity's or paging's, there is no
// prequential grading window here to reset: a calibration has no labels to
// grade against (see Card), so every field this model holds is learned
// state, and every field comes back — n included. That is deliberate, and it
// is what makes readiness affordable at all: a page threshold needs
// thresholdCriticalMinSamples points, which is weeks of a real series, and
// re-earning them from zero on every restart would mean an agent that is
// restarted weekly never learns one. Bounded the same way Observe bounds it
// live: an oversized snapshot is truncated on the way in.
//
// A series whose estimators do not come back as something P² could have
// produced is dropped, not repaired: see restoreP2Checked for why a
// plausible-looking repair is worse than starting that one series over.
func (m *thresholdModel) Restore(data []byte) error {
	var snap thresholdSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("thresholds snapshot: %w", err)
	}
	if snap.Version != thresholdSnapshotVersion {
		return fmt.Errorf("thresholds snapshot version %d, want %d", snap.Version, thresholdSnapshotVersion)
	}
	series := make(map[string]*seriesThreshold, len(snap.Series))
	for key, s := range snap.Series {
		if len(series) >= maxSeries {
			break
		}
		warn, ok := restoreP2Checked(s.Warn, 1-targetWarnRate)
		if !ok {
			continue
		}
		critical, ok := restoreP2Checked(s.Critical, 1-targetCriticalRate)
		if !ok {
			continue
		}
		if s.N < 0 || s.WarnHits < 0 || s.CritHits < 0 {
			continue
		}
		series[key] = &seriesThreshold{warn: warn, critical: critical, n: s.N, warnHits: s.WarnHits, critHits: s.CritHits}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.series = series
	return nil
}
