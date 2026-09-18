package forseer

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"
)

// The burn-forecast model: the error budget already says how much is gone.
// This says when it will all be gone.
//
// Those are different questions and only the second one is actionable. "You
// have used 60% of the budget" is a fact about the past; whether it is a
// problem depends entirely on whether that 60% arrived over a month or over
// the last twenty minutes. An on-call who can see "gone in about half an
// hour" can decide to act, and one looking at a percentage cannot.
//
// Holt's linear method: two numbers, a level and a trend, each an
// exponentially weighted update, and the projection is level + h·trend.
// Recent observations count for more than old ones, which is the behaviour
// wanted here — a burn that was flat all week and turned upward ten minutes
// ago should be projected from the turn, not from the week.
//
// It is graded head to head against the forecast it has to beat: assuming
// the burn stays exactly where it is. That naive persistence forecast is a
// genuinely hard baseline on slow-moving series, which is the point of
// scoring against it rather than against nothing. Every observation grades
// the previous step's two predictions before it updates either, so the win
// rate on the card is earned on data neither had seen.
const (
	// Level and trend smoothing. Level is responsive because a burn can
	// turn quickly; trend is slow because a jumpy trend produces projections
	// that swing between "fine" and "gone in a minute" on noise, which is
	// worse than no projection at all.
	holtAlpha = 0.3
	holtBeta  = 0.1

	// Observations before the model will project. Holt needs a few points
	// for the trend term to mean anything, and a projection from two is
	// a straight line through noise.
	forecastMinObservations = 20

	// The head-to-head window, in observations.
	forecastGradeWindow = 200

	// What gates a projection is not the head-to-head win rate. That was
	// tried and it is the wrong test: one-step accuracy on a flat series is
	// a tie that persistence wins on the tie-break, so the gate closed
	// exactly when a burn turned upward and would have needed a hundred
	// observations of sustained trend to open again — useless, since the
	// turn is the whole thing the forecast is for.
	//
	// The right test comes from what the model actually replaces, which is
	// no projection at all rather than persistence: persistence is flat by
	// construction and never reaches 100%, so it never produces an
	// exhaustion time. Any credible projection beats nothing; one fitted to
	// noise does not. So a projection is published only when the trend
	// survives its own error band — see exhaustedLocked, where the test
	// falls out of the band that is already being computed.
	//
	// The win rate stays on the card as an honest measure of one-step
	// skill. It is information, not a gate. (There is deliberately no
	// forecastMinGraded constant here — that was the gate this comment
	// describes rejecting.)

	// The projection is reported as a range, widened by the uncertainty in
	// the trend. A single number would claim a precision that a
	// two-parameter model fitted online does not have.
	//
	// The width is in standard errors of the trend, which is not the same
	// as the per-step prediction error and was originally confused with it.
	// Per-step noise does not shrink however long the model runs; the
	// estimate of the trend does, because smoothing averages that noise
	// away. Using the former made the band far too wide and withheld
	// projections from series with a perfectly clear trend.
	forecastBandWidth = 2.0

	// Projections beyond this are reported as "not on course" rather than
	// as a number. A trend of +0.0001% per tick technically exhausts the
	// budget eventually, and saying "in 340 days" invites someone to treat
	// it as a finding.
	forecastHorizon = 48 * time.Hour
)

type burnForecast struct {
	mu sync.Mutex

	level   float64
	trend   float64
	n       int
	last    float64
	lastAt  time.Time
	tick    float64 // mean seconds between observations
	absErr  float64 // running mean absolute one-step error, Holt
	naiveAE float64 // ... and for persistence

	// The one-step-ahead predictions made last time, waiting to be graded.
	predicted      float64
	naivePredicted float64
	havePrediction bool

	grades   []bool // true when Holt beat persistence on that step
	gradePos int
	graded   int
	wins     int
}

func newBurnForecast() *burnForecast {
	return &burnForecast{grades: make([]bool, forecastGradeWindow)}
}

// Observe records one reading of the consumed percentage and updates the
// level and trend. Call it on whatever cadence the burn is read at; the
// model learns that cadence itself, so the projection comes out in real
// time rather than in ticks.
func (f *burnForecast) Observe(consumed float64, at time.Time) {
	if math.IsNaN(consumed) || math.IsInf(consumed, 0) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	// Grade the predictions made last time, before anything learns from
	// this observation.
	if f.havePrediction {
		holtErr := math.Abs(consumed - f.predicted)
		naiveErr := math.Abs(consumed - f.naivePredicted)
		f.gradeLocked(holtErr < naiveErr)
		f.absErr = ewma(f.absErr, holtErr, f.n)
		f.naiveAE = ewma(f.naiveAE, naiveErr, f.n)
	}

	switch f.n {
	case 0:
		f.level, f.trend = consumed, 0
	case 1:
		f.trend = consumed - f.level
		f.level = consumed
	default:
		level := holtAlpha*consumed + (1-holtAlpha)*(f.level+f.trend)
		f.trend = holtBeta*(level-f.level) + (1-holtBeta)*f.trend
		f.level = level
	}

	if !f.lastAt.IsZero() {
		gap := at.Sub(f.lastAt).Seconds()
		// A clock that went backwards, or two readings in the same instant,
		// would poison the cadence estimate and with it every projection.
		if gap > 0 {
			f.tick = ewma(f.tick, gap, f.n)
		}
	}
	f.lastAt = at
	f.last = consumed
	f.n++

	f.predicted = f.level + f.trend
	f.naivePredicted = consumed
	f.havePrediction = true
}

// Exhausted projects when the budget reaches 100%. The two durations bound
// the answer; ok is false when the model cannot say — too few observations,
// a burn that is not rising, or an exhaustion date so far out that reporting
// it would be noise.
func (f *burnForecast) Exhausted() (soonest, latest time.Duration, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exhaustedLocked()
}

func (f *burnForecast) exhaustedLocked() (soonest, latest time.Duration, ok bool) {
	if f.n < forecastMinObservations {
		return 0, 0, false
	}
	// A spent budget is an observation, not a projection, so it is reported
	// whether or not the model is currently earning its place.
	if f.last >= 100 {
		return 0, 0, true
	}
	if !f.readyLocked() {
		return 0, 0, false
	}
	remaining := 100 - f.level
	if remaining <= 0 {
		return 0, 0, true
	}

	// Requiring the slow edge to still be rising is the significance test:
	// a trend smaller than the uncertainty in its own estimate is
	// indistinguishable from noise, and projecting from it would put a
	// number on the dashboard that the next few readings would contradict.
	band := forecastBandWidth * f.trendStdErrLocked()
	fast := f.trend + band
	slow := f.trend - band
	if slow <= 0 {
		return 0, 0, false
	}

	// Compare in seconds, as floats, BEFORE converting to a Duration.
	//
	// Converting first cannot work: the Go spec says a float-to-integer
	// conversion whose value the result type cannot represent "succeeds but
	// the result value is implementation-dependent", and the two
	// architectures this runs on disagree in the worst possible way. A trend
	// that is nearly flat — which is to say a budget that is emphatically not
	// being spent — makes remaining/fast astronomically large. arm64
	// saturates to MaxInt64, so `soonest > forecastHorizon` holds and the
	// answer is "not on course to exhaust the budget", which is right. amd64
	// yields MinInt64, which is less than forecastHorizon, so the guard lets
	// it through as a large negative duration and FormatProjection's
	// `soonest <= 0 && latest <= 0` branch reports "already spent" — the
	// exact opposite, from the same binary logic, on the architecture the
	// agent actually ships on.
	//
	// In float seconds there is no representable-range cliff: an overflow is
	// +Inf, and every comparison below behaves.
	horizonSec := forecastHorizon.Seconds()
	soonestSec := remaining / fast * f.tick
	latestSec := remaining / slow * f.tick
	if math.IsNaN(soonestSec) || math.IsInf(soonestSec, 0) || soonestSec > horizonSec {
		return 0, 0, false
	}
	if soonestSec < 0 {
		soonestSec = 0
	}
	soonest = time.Duration(soonestSec * float64(time.Second))
	if math.IsNaN(latestSec) || math.IsInf(latestSec, 0) || latestSec > horizonSec {
		latest = 0
	} else {
		if latestSec < 0 {
			latestSec = 0
		}
		latest = time.Duration(latestSec * float64(time.Second))
	}
	return soonest, latest, true
}

// readyLocked reports whether the model is in a position to have an opinion
// at all. Whether that opinion is a projection is a separate question, and
// the answer lives in exhaustedLocked: a flat budget is a model working
// correctly and saying there is nothing coming.
func (f *burnForecast) readyLocked() bool {
	return f.n >= forecastMinObservations && f.tick > 0
}

// trendStdErrLocked estimates how uncertain the trend component is, from
// the one-step error and the trend smoothing factor. For exponential
// smoothing the trend's variance settles at beta/(2-beta) of the
// observation variance, so a slow beta buys a trend that is much steadier
// than the series it is fitted to — which is the whole reason beta is slow.
func (f *burnForecast) trendStdErrLocked() float64 {
	return f.absErr * math.Sqrt(holtBeta/(2-holtBeta))
}

func (f *burnForecast) gradeLocked(won bool) {
	if f.graded == len(f.grades) {
		if f.grades[f.gradePos] {
			f.wins--
		}
	} else {
		f.graded++
	}
	f.grades[f.gradePos] = won
	if won {
		f.wins++
	}
	f.gradePos = (f.gradePos + 1) % len(f.grades)
}

// Card implements Model.
func (f *burnForecast) Card() Card {
	f.mu.Lock()
	defer f.mu.Unlock()

	accuracy, fallback := Unmeasured, Unmeasured
	if f.graded > 0 {
		accuracy = float64(f.wins) / float64(f.graded)
		fallback = 1 - accuracy
	}

	detail := "not enough readings to project yet"
	if soonest, latest, ok := f.exhaustedLocked(); ok {
		if phrase := FormatProjection(soonest, latest); phrase == "already spent" {
			detail = "the budget is already spent"
		} else {
			detail = "on course to exhaust the budget " + phrase
		}
	} else if f.readyLocked() {
		detail = "no trend that stands out from the noise — not on course to exhaust the budget"
	}

	return Card{
		Name:     "error-budget forecast",
		Job:      "Say when the error budget will be gone, not just how much of it already is.",
		Reads:    []string{"the consumed share of the error budget, over time"},
		Fallback: "assuming the burn stays exactly where it is",
		Ready:    f.readyLocked(),
		Trained:  f.n,
		// Head to head: the share of one-step-ahead predictions where this
		// model was closer than persistence. Two forecasts, one set of
		// observations, and the numbers sum to one.
		Accuracy:         accuracy,
		FallbackAccuracy: fallback,
		Graded:           f.graded,
		Detail:           detail,
	}
}

// forecastSnapshotVersion is this model's own schema version — see
// severitySnapshotVersion's comment for what that guards against.
const forecastSnapshotVersion = 1

// forecastSnapshot is Snapshot's JSON payload: Holt's level and trend, the
// observation count and cadence, and the two running error terms — the
// entire fitted model. The head-to-head grading window (grades, gradePos,
// graded, wins) is deliberately absent — see Restore.
type forecastSnapshot struct {
	Version        int       `json:"version"`
	Level          float64   `json:"level"`
	Trend          float64   `json:"trend"`
	N              int       `json:"n"`
	Last           float64   `json:"last"`
	LastAt         time.Time `json:"lastAt"`
	Tick           float64   `json:"tick"`
	AbsErr         float64   `json:"absErr"`
	NaiveAE        float64   `json:"naiveAE"`
	Predicted      float64   `json:"predicted"`
	NaivePredicted float64   `json:"naivePredicted"`
	HavePrediction bool      `json:"havePrediction"`
}

// Snapshot implements Model.
func (f *burnForecast) Snapshot() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := forecastSnapshot{
		Version:        forecastSnapshotVersion,
		Level:          f.level,
		Trend:          f.trend,
		N:              f.n,
		Last:           f.last,
		LastAt:         f.lastAt,
		Tick:           f.tick,
		AbsErr:         f.absErr,
		NaiveAE:        f.naiveAE,
		Predicted:      f.predicted,
		NaivePredicted: f.naivePredicted,
		HavePrediction: f.havePrediction,
	}
	return json.Marshal(snap)
}

// Restore implements Model. Holt's level and trend, the observation count,
// the cadence, and the two running error terms all come back — that is the
// entire fitted model, and readyLocked (n >= forecastMinObservations and a
// positive tick) is gated on exactly those, not on a comparison window, so
// a restored model can project again the moment it restarts rather than
// waiting through forecastMinObservations fresh readings.
//
// What resets: the head-to-head grading window (grades, gradePos, graded,
// wins) that Card's Accuracy is earned from. It gates nothing — unlike
// severity's or paging's, readiness here does not depend on it — but it is
// still a report of how often Holt has *recently* beaten persistence, and
// the same rule holds as everywhere else in this package: a number that
// claims to be recent is wrong the moment it survives a restart unchanged.
func (f *burnForecast) Restore(data []byte) error {
	var snap forecastSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("forecast snapshot: %w", err)
	}
	if snap.Version != forecastSnapshotVersion {
		return fmt.Errorf("forecast snapshot version %d, want %d", snap.Version, forecastSnapshotVersion)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.level, f.trend, f.n = snap.Level, snap.Trend, snap.N
	f.last, f.lastAt, f.tick = snap.Last, snap.LastAt, snap.Tick
	f.absErr, f.naiveAE = snap.AbsErr, snap.NaiveAE
	f.predicted, f.naivePredicted, f.havePrediction = snap.Predicted, snap.NaivePredicted, snap.HavePrediction
	return nil
}

// ewma is the running mean used for the error terms and the cadence: a
// plain average while there is little data, decaying to an exponential one
// so a series that changes shape is not held back by its own history.
func ewma(current, sample float64, n int) float64 {
	if n <= 1 {
		return sample
	}
	weight := math.Max(0.05, 1/float64(n))
	return current*(1-weight) + sample*weight
}

// FormatProjection turns the pair Exhausted returns into the phrase a human
// reads.
//
// It exists because the obvious formatting kept producing sentences that
// read as bugs even when the numbers behind them were right: "in 10s to 0s"
// once both ends of a steep projection rounded into the same few seconds,
// then "in 0s to 1h0m0s" once the rounding buckets were widened and a
// twenty-second lower bound rounded away to nothing. A projection is only
// useful if the sentence is one somebody can act on, so the rule is that a
// lower bound too small to state becomes "within", and a range whose ends
// meet becomes the single value it has become.
func FormatProjection(soonest, latest time.Duration) string {
	if soonest <= 0 && latest <= 0 {
		return "already spent"
	}
	// Under a minute at both ends there is nothing to act on but the fact
	// that it is imminent.
	if soonest < time.Minute && (latest == 0 || latest < time.Minute) {
		return "in under a minute"
	}
	// A lower bound below the precision worth stating turns the range into
	// a deadline, which is the more useful sentence anyway.
	if soonest < time.Minute {
		return "within " + humanDuration(roundDuration(latest))
	}
	lo := roundDuration(soonest)
	if latest == 0 {
		return "in about " + humanDuration(lo)
	}
	hi := roundDuration(latest)
	if hi <= lo {
		return "in about " + humanDuration(lo)
	}
	return "in " + humanDuration(lo) + " to " + humanDuration(hi)
}

// roundDuration trims a projection to a precision it can actually support.
// "in 42 minutes" is a claim; "in about 40 minutes" is the same claim
// without the false precision.
func roundDuration(d time.Duration) time.Duration {
	switch {
	case d < time.Hour:
		return d.Round(5 * time.Minute)
	default:
		return d.Round(30 * time.Minute)
	}
}

// humanDuration writes a rounded projection the way a person says it: "45m",
// "2h", "2h30m". Go's own String gives "2h30m0s", which puts a precision on
// the end that the rounding just took off.
func humanDuration(d time.Duration) string {
	minutes := int(d.Minutes())
	if minutes < 60 {
		if minutes < 1 {
			minutes = 1
		}
		return strconv.Itoa(minutes) + "m"
	}
	hours, rest := minutes/60, minutes%60
	if rest == 0 {
		return strconv.Itoa(hours) + "h"
	}
	return strconv.Itoa(hours) + "h" + strconv.Itoa(rest) + "m"
}
