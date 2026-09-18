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
// The method is Robbins-Monro stochastic approximation, which is the whole
// of the update rule:
//
//	t ← t · (1 + step · (exceeded ? 1 : 0 − target))
//
// A point above the threshold pushes it up by roughly step·t; every point
// below nudges it down by step·t·target. Those balance exactly when the
// threshold sits at the target quantile, so that is where it settles, with
// four floats of state and no assumption about the shape of the
// distribution.
//
// The update is multiplicative rather than additive, and the step does not
// decay. Both were arrived at by watching it fail. An additive step has to
// be chosen relative to a scale nobody knows in advance, and a decaying one
// dies long before a threshold starting at 3 can walk out to the one-in-ten-
// thousand tail — measured at 2.8% of points still alerting after forty
// thousand samples, against a 0.1% budget. Moving by a fraction of the
// current threshold converges at any magnitude, and a constant step means a
// series that changes shape next month is re-learned rather than frozen.
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

	// Fraction of the current threshold each update moves it by. Small
	// enough that the settled threshold jitters by a fraction of a sigma,
	// large enough to walk from 3 to the far tail in a couple of thousand
	// points.
	thresholdStep = 0.02

	// Below this many points a series keeps the fixed sigma thresholds. A
	// budget of one in a thousand cannot be estimated from a hundred
	// points, and claiming otherwise would be worse than the constant. It
	// is also roughly where the update above has settled, so "ready" means
	// converged rather than merely started.
	thresholdMinSamples = 2000
)

// seriesThreshold is one series' learned pair, plus what it has actually
// been doing — which is the only honest way to report a calibration that
// has no labels to be scored against.
type seriesThreshold struct {
	warn     float64
	critical float64
	n        int
	warnHits int
	critHits int
}

type thresholdModel struct {
	mu     sync.Mutex
	series map[string]*seriesThreshold
}

func newThresholdModel() *thresholdModel {
	return &thresholdModel{series: make(map[string]*seriesThreshold)}
}

// Observe records one z-score for a series and moves its thresholds toward
// the budget. It returns the pair to use for this point, and false when the
// series has not been seen enough for them to beat the constants.
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
		// Start where the constants are, so the first thousand points
		// behave exactly as they did before and the model only ever moves
		// away from that deliberately.
		s = &seriesThreshold{warn: warningSigma, critical: criticalSigma}
		m.series[key] = s
	}

	// Grade before moving, so the counts describe the threshold that was
	// actually in force when the point arrived.
	if z >= s.warn {
		s.warnHits++
	}
	if z >= s.critical {
		s.critHits++
	}
	s.n++

	s.warn = clampThreshold(s.warn * (1 + thresholdStep*(indicator(z >= s.warn)-targetWarnRate)))
	s.critical = clampThreshold(s.critical * (1 + thresholdStep*(indicator(z >= s.critical)-targetCriticalRate)))
	// A critical alert that is easier to reach than a warning is nonsense,
	// and finite samples can briefly produce one.
	if s.critical < s.warn {
		s.critical = s.warn
	}

	if s.n < thresholdMinSamples {
		return warningSigma, criticalSigma, false
	}
	return s.warn, s.critical, true
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

	ready, points, warnHits := 0, 0, 0
	for _, s := range m.series {
		points += s.n
		warnHits += s.warnHits
		if s.n >= thresholdMinSamples {
			ready++
		}
	}

	detail := "no series has enough history yet"
	if points > 0 {
		detail = fmt.Sprintf("warning on %.3f%% of points against a %.3f%% budget, %d of %d series calibrated",
			100*float64(warnHits)/float64(points), 100*targetWarnRate, ready, len(m.series))
	}

	return Card{
		Name:     "alert thresholds",
		Job:      "Decide how far out of line one series has to go before a human should hear about it.",
		Reads:    []string{"the z-score of one series"},
		Fallback: "a fixed 3σ warning and 5σ critical, shared by every series",
		Ready:    ready > 0,
		Trained:  points,
		// A calibration has no labels to be right or wrong about, so there
		// is no accuracy to report. What it does have is a budget, and
		// whether it is hitting it — which is in Detail.
		Accuracy:         Unmeasured,
		FallbackAccuracy: Unmeasured,
		Detail:           detail,
	}
}

// thresholdSnapshotVersion is this model's own schema version — see
// severitySnapshotVersion's comment for what that guards against.
const thresholdSnapshotVersion = 1

// thresholdSnapshot is Snapshot's JSON payload: every series' learned
// warn/critical pair and the counts behind it.
type thresholdSnapshot struct {
	Version int                                `json:"version"`
	Series  map[string]seriesThresholdSnapshot `json:"series"`
}

type seriesThresholdSnapshot struct {
	Warn     float64 `json:"warn"`
	Critical float64 `json:"critical"`
	N        int     `json:"n"`
	WarnHits int     `json:"warnHits"`
	CritHits int     `json:"critHits"`
}

// Snapshot implements Model.
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

func (m *thresholdModel) Snapshot() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := thresholdSnapshot{Version: thresholdSnapshotVersion, Series: make(map[string]seriesThresholdSnapshot, len(m.series))}
	for key, s := range m.series {
		snap.Series[key] = seriesThresholdSnapshot{Warn: s.warn, Critical: s.critical, N: s.n, WarnHits: s.warnHits, CritHits: s.critHits}
	}
	return json.Marshal(snap)
}

// Restore implements Model. Unlike severity's or paging's, there is no
// prequential grading window here to reset: a calibration has no labels to
// grade against (see Card), so every field this model holds is learned
// state, and every field comes back — n included. That is deliberate, and
// it is the specific problem this roadmap item names for this model:
// thresholdMinSamples (2000) gates readiness purely on how many points a
// series has seen, with nothing to re-earn against, so restoring n lets an
// already-calibrated series stay calibrated across a restart instead of
// re-walking the 2000-point Robbins-Monro climb from warningSigma/
// criticalSigma. Bounded the same way Observe bounds it live: a snapshot
// with more than maxSeries entries is truncated on the way in.
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
		series[key] = &seriesThreshold{warn: s.Warn, critical: s.Critical, n: s.N, warnHits: s.WarnHits, critHits: s.CritHits}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.series = series
	return nil
}
