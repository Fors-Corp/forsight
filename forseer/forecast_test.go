package forseer

import (
	"bytes"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
)

// burn drives the model with a series of consumed percentages, ten seconds
// apart, and returns it ready to be asked.
func burn(values []float64) *burnForecast {
	f := newBurnForecast()
	at := time.Unix(0, 0)
	for _, v := range values {
		f.Observe(v, at)
		at = at.Add(10 * time.Second)
	}
	return f
}

func ramp(from, step float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = from + step*float64(i)
	}
	return out
}

func TestForecast_SaysNothingUntilItHasEnoughReadings(t *testing.T) {
	f := burn(ramp(0, 1, forecastMinObservations-1))

	if _, _, ok := f.Exhausted(); ok {
		t.Error("projected from fewer readings than the minimum")
	}
	if f.Card().Ready {
		t.Error("card reported ready before the minimum")
	}
}

func TestForecast_ProjectsAConstantBurnToTheRightTime(t *testing.T) {
	// 1% per reading, ten seconds apart, starting from zero. At reading 40
	// the level is about 39 and 61 points remain, so exhaustion is about
	// 61 readings away — a little over ten minutes.
	f := burn(ramp(0, 1, 40))

	soonest, latest, ok := f.Exhausted()
	if !ok {
		t.Fatal("no projection from a clean linear burn")
	}
	if soonest < 5*time.Minute || soonest > 12*time.Minute {
		t.Errorf("soonest exhaustion %s is outside the plausible 5-12 minute range", soonest)
	}
	if latest != 0 && latest < soonest {
		t.Errorf("latest %s is sooner than soonest %s", latest, soonest)
	}
}

func TestForecast_ReportsARangeRatherThanAPoint(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	values := make([]float64, 60)
	for i := range values {
		values[i] = float64(i)*0.8 + rng.NormFloat64()*0.6
	}
	f := burn(values)

	soonest, latest, ok := f.Exhausted()
	if !ok {
		t.Fatal("no projection from a noisy but clearly rising burn")
	}
	if latest != 0 && latest == soonest {
		t.Error("the range collapsed to a point despite the model having real error")
	}
}

func TestForecast_SaysNotOnCourseWhenTheBurnIsFlat(t *testing.T) {
	f := burn(ramp(40, 0, 60))

	if _, _, ok := f.Exhausted(); ok {
		t.Error("projected exhaustion from a flat burn")
	}
	// A flat budget is the model working, not failing: it has an opinion
	// and the opinion is "nothing is coming".
	if !f.Card().Ready {
		t.Error("model has plenty of readings but reports no opinion at all")
	}
	if detail := f.Card().Detail; detail == "" {
		t.Error("card said nothing about why there is no projection")
	}
}

func TestForecast_ReportsASpentBudgetWithNoTrendToProject(t *testing.T) {
	// 100% and flat: there is no trend, so nothing to project — but "the
	// budget is gone" is something that was measured, not forecast, and
	// must still be reported.
	f := burn(ramp(100, 0, 60))

	soonest, _, ok := f.Exhausted()
	if !ok || soonest != 0 {
		t.Errorf("got (%s, %v) for a spent budget, want (0, true)", soonest, ok)
	}
}

func TestForecast_SaysNotOnCourseWhenTheBurnIsFalling(t *testing.T) {
	f := burn(ramp(80, -0.5, 60))

	if _, _, ok := f.Exhausted(); ok {
		t.Error("projected exhaustion from a falling burn")
	}
}

func TestForecast_WithholdsAProjectionBeyondTheHorizon(t *testing.T) {
	// Rising, but so slowly that exhaustion is weeks away. Reporting a
	// number there invites someone to treat it as a finding.
	f := burn(ramp(0, 0.0005, 60))

	if _, _, ok := f.Exhausted(); ok {
		t.Error("projected an exhaustion beyond the horizon instead of withholding it")
	}
}

func TestForecast_ReportsAnAlreadySpentBudget(t *testing.T) {
	f := burn(ramp(100, 0, 40))

	soonest, _, ok := f.Exhausted()
	if !ok || soonest != 0 {
		t.Errorf("got (%s, %v) for a fully spent budget, want (0, true)", soonest, ok)
	}
	if detail := f.Card().Detail; detail != "the budget is already spent" {
		t.Errorf("detail %q does not say the budget is spent", detail)
	}
}

// The claim the card makes: this beats assuming the burn stays put. On a
// series with a real trend it should, because persistence never anticipates
// the next step of a ramp.
func TestForecast_BeatsPersistenceOnATrendingBurn(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	values := make([]float64, 300)
	for i := range values {
		values[i] = math.Min(100, float64(i)*0.3+rng.NormFloat64()*0.5)
	}
	f := burn(values)

	card := f.Card()
	if card.Graded == 0 {
		t.Fatal("nothing was graded; the head-to-head is not running")
	}
	if card.Accuracy <= 0.5 {
		t.Errorf("won %.2f of steps against persistence on a clearly trending series", card.Accuracy)
	}
	if math.Abs(card.Accuracy+card.FallbackAccuracy-1) > 1e-9 {
		t.Errorf("the two win rates are %.3f and %.3f; head to head they must sum to 1",
			card.Accuracy, card.FallbackAccuracy)
	}
}

func TestForecast_GradeWindowStaysBounded(t *testing.T) {
	f := burn(ramp(0, 0.05, forecastGradeWindow*3))

	if graded := f.Card().Graded; graded > forecastGradeWindow {
		t.Errorf("graded %d steps, window is %d", graded, forecastGradeWindow)
	}
}

func TestForecast_IgnoresUnusableReadings(t *testing.T) {
	f := newBurnForecast()
	at := time.Unix(0, 0)
	f.Observe(math.NaN(), at)
	f.Observe(math.Inf(1), at.Add(time.Second))

	if trained := f.Card().Trained; trained != 0 {
		t.Errorf("learned from %d unusable readings, want 0", trained)
	}
}

func TestForecast_SurvivesAClockThatDoesNotAdvance(t *testing.T) {
	f := newBurnForecast()
	at := time.Unix(0, 0)
	// Every reading at the same instant: the cadence is unknowable, so the
	// model must decline rather than divide by zero.
	for i := 0; i < 60; i++ {
		f.Observe(float64(i), at)
	}

	if _, _, ok := f.Exhausted(); ok {
		t.Error("projected a time from readings that carry no time")
	}
}

func TestForecast_CardDescribesItself(t *testing.T) {
	card := newBurnForecast().Card()

	if card.Name == "" || card.Job == "" || len(card.Reads) == 0 || card.Fallback == "" {
		t.Errorf("card does not fully describe itself: %+v", card)
	}
	if card.Accuracy != Unmeasured || card.FallbackAccuracy != Unmeasured {
		t.Error("an ungraded forecast reported a win rate")
	}
}

func TestFormatProjection_ReadsAsASentenceAtEveryScale(t *testing.T) {
	cases := []struct {
		soonest, latest time.Duration
		want            string
	}{
		{0, 0, "already spent"},
		// A steep burn used to print "in 10s to 0s": a range backwards,
		// because both ends rounded into the same few seconds.
		{10 * time.Second, 4 * time.Second, "in under a minute"},
		{0, 10 * time.Second, "in under a minute"},
		{30 * time.Second, 0, "in under a minute"},
		// A lower bound too small to state becomes a deadline instead of a
		// range starting at zero.
		{20 * time.Second, 55 * time.Minute, "within 55m"},
		{8 * time.Minute, 0, "in about 10m"},
		{8 * time.Minute, 42 * time.Minute, "in 10m to 40m"},
		// Ends that round together are one value, not a range of one.
		{31 * time.Minute, 32 * time.Minute, "in about 30m"},
		{3 * time.Hour, 5 * time.Hour, "in 3h to 5h"},
		{90 * time.Minute, 0, "in about 1h30m"},
	}

	for _, tc := range cases {
		if got := FormatProjection(tc.soonest, tc.latest); got != tc.want {
			t.Errorf("FormatProjection(%s, %s) = %q, want %q", tc.soonest, tc.latest, got, tc.want)
		}
	}
}

func TestFormatProjection_NeverPrintsABackwardsRange(t *testing.T) {
	for soonest := time.Duration(0); soonest < 4*time.Hour; soonest += 7 * time.Minute {
		for _, latest := range []time.Duration{0, soonest / 2, soonest, soonest * 2} {
			got := FormatProjection(soonest, latest)
			if strings.Contains(got, "0m") && strings.HasPrefix(got, "in 0m") {
				t.Errorf("FormatProjection(%s, %s) = %q, which states a bound of nothing", soonest, latest, got)
			}
			if strings.Contains(got, " to ") {
				parts := strings.SplitN(strings.TrimPrefix(got, "in "), " to ", 2)
				lo, err1 := time.ParseDuration(parts[0])
				hi, err2 := time.ParseDuration(parts[1])
				if err1 != nil || err2 != nil {
					t.Fatalf("unparseable projection %q", got)
				}
				if hi <= lo {
					t.Errorf("FormatProjection(%s, %s) = %q, which reads backwards", soonest, latest, got)
				}
			}
		}
	}
}

// TestForecast_SnapshotRestoreRoundTrip is roadmap item 25's proof for this
// model: Holt's level and trend survive a restart, so a restored model
// projects the same exhaustion a warm one would.
func TestForecast_SnapshotRestoreRoundTrip(t *testing.T) {
	f := burn(ramp(0, 1, 40))
	wantSoonest, wantLatest, wantOK := f.Exhausted()
	if !wantOK {
		t.Fatal("no projection from a clean linear burn; nothing to prove a round trip on")
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
		t.Fatalf("restored Exhausted() = (%s, %s, %v), want (%s, %s, %v)",
			gotSoonest, gotLatest, gotOK, wantSoonest, wantLatest, wantOK)
	}
	if !restored.Card().Ready {
		t.Fatal("restored model is not ready even though n/tick both came back")
	}

	// The head-to-head grading window is a report of recent skill, not a
	// gate, but it must still reset: it is a claim about "recently", and it
	// would be wrong the instant it survived a restart unchanged.
	if restored.graded != 0 || restored.wins != 0 {
		t.Fatalf("restored model carries a graded window (graded=%d wins=%d), want zero", restored.graded, restored.wins)
	}
}

func TestForecast_RestoreDiscardsAVersionMismatch(t *testing.T) {
	f := burn(ramp(0, 1, 40))
	data, err := f.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Written from the constant rather than a literal, so bumping the schema
	// version cannot quietly turn this test into one that feeds Restore a
	// payload it is supposed to accept.
	current := []byte(`"version":` + strconv.Itoa(forecastSnapshotVersion))
	if !bytes.Contains(data, current) {
		t.Fatalf("snapshot %s does not carry %s", data, current)
	}
	data = bytes.Replace(data, current, []byte(`"version":`+strconv.Itoa(forecastSnapshotVersion+1)), 1)

	fresh := newBurnForecast()
	if err := fresh.Restore(data); err == nil {
		t.Fatal("Restore accepted a payload with the wrong schema version")
	}
	if fresh.n != 0 {
		t.Fatalf("a discarded restore left n=%d, want 0", fresh.n)
	}
}

func TestForecast_RestoreDiscardsCorruptJSON(t *testing.T) {
	f := newBurnForecast()
	if err := f.Restore([]byte("{not json")); err == nil {
		t.Fatal("Restore accepted corrupt JSON")
	}
	if f.n != 0 {
		t.Fatalf("a discarded restore left n=%d, want 0", f.n)
	}
}

// TestBurnForecast_NearlyFlatTrendIsNotOnCourse pins a bug that gave opposite
// answers on different CPUs.
//
// burnForecast used to convert remaining/fast into a time.Duration before
// checking it against forecastHorizon. The Go spec says a float-to-integer
// conversion the result type cannot represent "succeeds but the result value
// is implementation-dependent": arm64 saturates to MaxInt64, so the horizon
// guard held and the answer was "not on course"; amd64 yields MinInt64, which
// is below the horizon, so the guard passed it through as a large negative
// duration and FormatProjection reported "already spent". A budget being
// consumed so slowly it will outlive the hardware was announced as already
// gone — on linux/amd64, which is what the agent ships on.
//
// This test therefore passes on arm64 for the wrong reason on the old code and
// fails on amd64, which is what CI runs.
func TestBurnForecast_NearlyFlatTrendIsNotOnCourse(t *testing.T) {
	f := newBurnForecast()
	at := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	// A budget creeping up by a billionth of a percent per minute: it will
	// not be exhausted inside any horizon anyone cares about.
	consumed := 1.0
	for i := 0; i < 200; i++ {
		f.Observe(consumed, at)
		consumed += 1e-9
		at = at.Add(time.Minute)
	}

	soonest, latest, ok := f.Exhausted()
	if ok {
		t.Fatalf("a nearly flat trend was reported as on course: soonest=%v latest=%v (%s)",
			soonest, latest, FormatProjection(soonest, latest))
	}
	if soonest < 0 || latest < 0 {
		t.Errorf("negative durations escaped: soonest=%v latest=%v", soonest, latest)
	}
}

// TestBurnForecast_RealBurnStillProjects is the other half: the overflow guard
// must not swallow a budget that genuinely is on course.
func TestBurnForecast_RealBurnStillProjects(t *testing.T) {
	f := newBurnForecast()
	at := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	consumed := 10.0
	for i := 0; i < 200; i++ {
		f.Observe(consumed, at)
		consumed += 0.4 // ~100% within a few hours
		at = at.Add(time.Minute)
	}

	soonest, _, ok := f.Exhausted()
	if !ok {
		t.Fatal("a budget burning at 0.4%/min projected nothing")
	}
	if soonest <= 0 || soonest > forecastHorizon {
		t.Errorf("soonest = %v, want a positive duration inside the %v horizon", soonest, forecastHorizon)
	}
}
