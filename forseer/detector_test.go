package forseer

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDetector_OpensWarningOnThreeSigmaSpike(t *testing.T) {
	d := NewDetector()
	fixed := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	d.now = func() time.Time { return fixed }

	for i := 0; i < minSamples; i++ {
		d.Observe([]Point{{Name: "host.cpu.percent", Value: 10}})
	}
	if got := d.Insights(); len(got) != 0 {
		t.Fatalf("baseline produced insights: %+v", got)
	}

	d.Observe([]Point{{Name: "host.cpu.percent", Value: 80}})
	got := d.Insights()
	if len(got) == 0 {
		t.Fatal("expected at least one insight on the spike")
	}
	found := false
	for _, ins := range got {
		if ins.Kind == KindAnomaly && ins.Metric == "host.cpu.percent" {
			found = true
			if ins.Severity != SeverityWarning && ins.Severity != SeverityCritical {
				t.Errorf("severity = %q, want warning or critical", ins.Severity)
			}
			if ins.Source != "forseer" {
				t.Errorf("source = %q, want forseer", ins.Source)
			}
		}
	}
	if !found {
		t.Fatalf("no anomaly insight: %+v", got)
	}
}

func TestDetector_ClearsWhenValueReturnsToBaseline(t *testing.T) {
	d := NewDetector()
	for i := 0; i < minSamples; i++ {
		d.Observe([]Point{{Name: "host.memory.percent", Value: 20}})
	}
	d.Observe([]Point{{Name: "host.memory.percent", Value: 90}})
	if len(d.Insights()) == 0 {
		t.Fatal("expected an insight on the spike")
	}
	for i := 0; i < minSamples; i++ {
		d.Observe([]Point{{Name: "host.memory.percent", Value: 20}})
	}
	for _, ins := range d.Insights() {
		if ins.Kind == KindAnomaly {
			t.Fatalf("anomaly still open after return to baseline: %+v", ins)
		}
	}
}

func TestDetector_EmitsChangepointOnSustainedShift(t *testing.T) {
	d := NewDetector()
	for i := 0; i < minSamples; i++ {
		d.Observe([]Point{{Name: "host.disk.percent", Value: 10}})
	}
	for i := 0; i < 20; i++ {
		d.Observe([]Point{{Name: "host.disk.percent", Value: 80}})
	}
	found := false
	for _, ins := range d.Insights() {
		if ins.Kind == KindChangepoint {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a CUSUM changepoint after a sustained shift")
	}
}

// TestDetector_NoChangepointOnStationarySeries is the assertion the
// changepoint test above never made, and its absence hid a guaranteed
// false-alarm loop for the life of the detector.
//
// CUSUM is defined on a statistic whose in-control mean is zero: subtracting
// the slack k then gives it negative drift, and max(0, ...) pins it at zero
// until something real happens. This fed it |z|, which is half-normal —
// in-control mean E|Z| = sqrt(2/pi) = 0.798, above cusumK = 0.5. The sum
// drifted up by ~0.3 per sample on a series that never changed, crossed
// cusumH = 5 after ~17 samples, fired "changed regime", reset, and did it
// again forever: 579 changepoints per 10000 stationary samples, on every
// series, at every collector tick.
//
// The values below are a deterministic stand-in for stationary noise: they
// hold a steady mean and never shift regime, so every changepoint this
// produces is false by construction.
func TestDetector_NoChangepointOnStationarySeries(t *testing.T) {
	d := NewDetector()
	wobble := []float64{49, 51, 50, 52, 48, 50, 51, 49, 50, 53, 47, 50, 50, 52, 48}
	for i := 0; i < 400; i++ {
		d.Observe([]Point{{Name: "host.cpu.percent", Value: wobble[i%len(wobble)]}})
	}
	for _, ins := range d.Insights() {
		if ins.Kind == KindChangepoint {
			t.Fatalf("changepoint on a series that never changed regime: %+v", ins)
		}
	}
}

// TestDetector_ChangepointNamesItsDirection: a regime change that went down
// is not the same operational event as one that went up, and a one-armed
// CUSUM over |z| could not tell them apart even in principle.
func TestDetector_ChangepointNamesItsDirection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		from, to   float64
		wantInDesc string
	}{
		{"upward shift", 10, 80, "up"},
		{"downward shift", 80, 10, "down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDetector()
			for i := 0; i < minSamples; i++ {
				d.Observe([]Point{{Name: "host.disk.percent", Value: tc.from}})
			}
			for i := 0; i < 20; i++ {
				d.Observe([]Point{{Name: "host.disk.percent", Value: tc.to}})
			}
			var cp *Insight
			for i := range d.Insights() {
				if ins := d.Insights()[i]; ins.Kind == KindChangepoint {
					cp = &ins
					break
				}
			}
			if cp == nil {
				t.Fatal("expected a CUSUM changepoint after a sustained shift")
			}
			if !strings.Contains(cp.Description, tc.wantInDesc) {
				t.Errorf("description %q does not name the direction %q", cp.Description, tc.wantInDesc)
			}
		})
	}
}

func TestSeasonalKey_HourSplitsHostNotProcess(t *testing.T) {
	day := seasonalKey("host.cpu.percent", nil, 15)
	night := seasonalKey("host.cpu.percent", nil, 3)
	if day == night {
		t.Fatal("hour of day should split host series")
	}
	a := seasonalKey("process.cpu.percent", map[string]string{"pid": "7"}, 3)
	b := seasonalKey("process.cpu.percent", map[string]string{"pid": "7"}, 15)
	if a != b {
		t.Fatal("process series should not be hour-split")
	}
}

func TestDetector_SeriesBaselineColdUntilMinSamples(t *testing.T) {
	d := NewDetector()
	labels := map[string]string{"pid": "7", "name": "rogue"}
	// A little jitter, not a single repeated value — zero variance is its
	// own case (TestDetector_SeriesBaselineZeroVarianceIsNotReady), not what
	// this test is about.
	jitter := []float64{1, 2, 3, 2, 1, 3, 2, 1, 3, 2, 1, 2}
	if len(jitter) != minSamples {
		t.Fatalf("test setup: need exactly minSamples=%d points", minSamples)
	}

	if _, _, ready := d.SeriesBaseline("process.cpu.percent", labels); ready {
		t.Fatal("an unseen series reported a baseline")
	}

	for _, v := range jitter[:len(jitter)-1] {
		d.Observe([]Point{{Name: "process.cpu.percent", Value: v, Labels: labels}})
	}
	if _, _, ready := d.SeriesBaseline("process.cpu.percent", labels); ready {
		t.Fatalf("reported a baseline before minSamples=%d", minSamples)
	}

	d.Observe([]Point{{Name: "process.cpu.percent", Value: jitter[len(jitter)-1], Labels: labels}})
	mean, stddev, ready := d.SeriesBaseline("process.cpu.percent", labels)
	if !ready {
		t.Fatal("still cold at exactly minSamples")
	}
	if mean <= 0 {
		t.Errorf("mean = %v, want roughly the jitter's average (~1.9)", mean)
	}
	if stddev <= 0 {
		t.Errorf("stddev = %v, want a positive spread (the jitter has one)", stddev)
	}
}

func TestDetector_SeriesBaselineZeroVarianceIsNotReady(t *testing.T) {
	d := NewDetector()
	for i := 0; i < minSamples+5; i++ {
		d.Observe([]Point{{Name: "process.cpu.percent", Value: 5, Labels: map[string]string{"pid": "1"}}})
	}
	// Every point identical: no spread to measure a deviation against, so
	// this must not claim readiness — the same treatment observeOneLocked
	// gives a zero-variance series.
	if _, _, ready := d.SeriesBaseline("process.cpu.percent", map[string]string{"pid": "1"}); ready {
		t.Fatal("a zero-variance series reported itself ready")
	}
}

func TestDetector_SeriesBaselineTracksEachLabelSetSeparately(t *testing.T) {
	d := NewDetector()
	jitter := []float64{18, 19, 20, 21, 22, 20, 19, 21, 18, 22, 20, 19}
	for _, v := range jitter {
		d.Observe([]Point{{Name: "process.cpu.percent", Value: v, Labels: map[string]string{"pid": "1"}}})
	}
	if _, _, ready := d.SeriesBaseline("process.cpu.percent", map[string]string{"pid": "2"}); ready {
		t.Fatal("a different pid's series reported a baseline it never saw")
	}
	if _, _, ready := d.SeriesBaseline("process.cpu.percent", map[string]string{"pid": "1"}); !ready {
		t.Fatal("pid 1's own series should be ready")
	}
}

func TestSeriesKey_IncludesSortedLabels(t *testing.T) {
	a := seriesKey("docker.cpu.percent", map[string]string{"container_name": "web", "container_id": "abc"})
	b := seriesKey("docker.cpu.percent", map[string]string{"container_id": "abc", "container_name": "web"})
	if a != b {
		t.Fatalf("label order changed the key:\n%s\n%s", a, b)
	}
	c := seriesKey("docker.cpu.percent", map[string]string{"container_id": "def", "container_name": "web"})
	if a == c {
		t.Fatal("different container_id produced the same key")
	}
}

// TestDetector_ADayOfOrdinaryTrafficIsNotRefused is the test whose absence
// let the Detector go deaf after fourteen hours.
//
// seasonalKey suffixes every non-process series with |h=NN, so one series
// occupies 24 keys once the agent has been up a day. The cap counted keys, so
// an ordinary single host — the host collector's twelve names, five
// containers, two probes, ten processes: 67 real series — needed 918 keys
// against a cap of 512, and past it observeOneLocked returned immediately for
// any series/hour pair not already seen. No baseline, no anomaly check, no
// changepoint, no log line. Which series went dark, and at which hours,
// depended on the order the cap happened to fill in.
func TestDetector_ADayOfOrdinaryTrafficIsNotRefused(t *testing.T) {
	d := NewDetector()
	hour := 0
	d.now = func() time.Time { return time.Date(2026, 9, 18, hour, 0, 0, 0, time.UTC) }

	var batch []Point
	for _, name := range []string{
		"host.cpu.percent", "host.memory.percent", "host.disk.percent",
		"host.disk.used_bytes", "host.fd.max", "host.fd.used",
		"host.memory.total_bytes", "host.memory.used_bytes",
		"host.net.bytes_recv", "host.net.bytes_sent",
		"host.net.conn_count", "host.uptime_seconds",
	} {
		batch = append(batch, Point{Name: name, Value: 10})
	}
	for c := 0; c < 5; c++ {
		for _, name := range []string{"docker.cpu.percent", "docker.memory.percent", "docker.memory.used_bytes"} {
			batch = append(batch, Point{Name: name, Value: 10, Labels: map[string]string{"container": fmt.Sprintf("c%d", c)}})
		}
	}
	for pr := 0; pr < 2; pr++ {
		for _, name := range []string{
			"probe.http.up", "probe.http.status", "probe.http.duration_ms",
			"probe.tls.days_remaining", "probe.tls.valid",
		} {
			batch = append(batch, Point{Name: name, Value: 10, Labels: map[string]string{"name": fmt.Sprintf("p%d", pr)}})
		}
	}

	// A full day, every hour bucket. The values have to move: a constant
	// series has zero variance and SeriesBaseline reports not-ready for that
	// reason alone, which would make this test pass or fail for nothing to do
	// with the cap.
	for hour = 0; hour < 24; hour++ {
		for i := 0; i < minSamples+2; i++ {
			jittered := make([]Point, len(batch))
			for j, p := range batch {
				p.Value = 10 + float64((i+j)%5)
				jittered[j] = p
			}
			d.Observe(jittered)
		}
	}

	// Every series must still have a baseline in the CURRENT hour bucket.
	hour = 23
	for _, p := range batch {
		if _, _, ready := d.SeriesBaseline(p.Name, p.Labels); !ready {
			t.Fatalf("no baseline for %s%v after a full day: the cap refused it", p.Name, p.Labels)
		}
	}

	watching, evicted := d.SeriesCount()
	if evicted != 0 {
		t.Errorf("evicted %d series on a host with only %d, which is well inside maxSeries=%d",
			evicted, watching, maxSeries)
	}
	if watching != len(batch) {
		t.Errorf("watching %d series, want %d", watching, len(batch))
	}
}

// TestDetector_EvictsWholeSeriesAndLeavesNoOrphanInsight: past the cap,
// something must give, and what gives must go completely. An insight left
// behind after its series is evicted can never be closed — closing happens in
// observeOneLocked, and no further point will arrive under that key — so the
// dashboard would show a permanent finding about a series nothing measures.
func TestDetector_EvictsWholeSeriesAndLeavesNoOrphanInsight(t *testing.T) {
	d := NewDetector()
	// Eviction is by recency, so the clock must advance for "oldest" to mean
	// anything — with a frozen clock every series ties and the victim is
	// whichever key Go's randomized map iteration happens to yield. Staying
	// inside one hour keeps seasonalKey from splitting the series.
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	tick := 0
	d.now = func() time.Time { return base.Add(time.Duration(tick) * time.Millisecond) }

	// One series with an open anomaly, seen first so it is the eviction victim.
	for i := 0; i < minSamples; i++ {
		tick++
		d.Observe([]Point{{Name: "doomed.series", Value: 10 + float64(i%3)}})
	}
	tick++
	d.Observe([]Point{{Name: "doomed.series", Value: 10_000}})
	if len(d.Insights()) == 0 {
		t.Fatal("setup: expected an open insight on the series about to be evicted")
	}

	for i := 0; i < maxSeries+5; i++ {
		tick++
		d.Observe([]Point{{Name: fmt.Sprintf("flood.%d", i), Value: 1}})
	}

	watching, evicted := d.SeriesCount()
	if watching > maxSeries {
		t.Errorf("watching %d series, cap is %d", watching, maxSeries)
	}
	if evicted == 0 {
		t.Error("nothing was evicted, so the flood was refused instead — a new series must be able to displace a dead one")
	}
	for _, ins := range d.Insights() {
		if ins.Metric == "doomed.series" {
			t.Errorf("an insight outlived its evicted series and can never be closed: %+v", ins)
		}
	}
	d.mu.Lock()
	leftover := 0
	for key := range d.series {
		if strings.HasPrefix(key, "doomed.series") {
			leftover++
		}
	}
	d.mu.Unlock()
	if leftover != 0 {
		t.Errorf("%d hour buckets of the evicted series are still held", leftover)
	}
}

// TestDetector_NonFiniteValueDoesNotPoisonTheSeries.
//
// Welford has no way back from a NaN: once mean and m2 are NaN they stay NaN,
// and every comparison against NaN is false in Go, so neither the
// variance <= 0 guard nor the sigma == 0 guard fires, z is NaN, and both
// z >= warn and z >= critical are false forever. Anomaly and changepoint
// detection for that series go permanently dead with no error and no log
// line. One value is enough, and it need not be trusted: strconv.ParseFloat
// accepts "NaN" and "Inf" from an unauthenticated StatsD datagram.
func TestDetector_NonFiniteValueDoesNotPoisonTheSeries(t *testing.T) {
	for _, tc := range []struct {
		name string
		bad  float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDetector()
			for i := 0; i < minSamples; i++ {
				d.Observe([]Point{{Name: "host.cpu.percent", Value: 10}})
			}
			d.Observe([]Point{{Name: "host.cpu.percent", Value: tc.bad}})

			// The series must still be able to see a real spike afterwards.
			d.Observe([]Point{{Name: "host.cpu.percent", Value: 10_000}})

			var sawAnomaly bool
			for _, ins := range d.Insights() {
				if ins.Kind == KindAnomaly {
					sawAnomaly = true
				}
			}
			if !sawAnomaly {
				t.Fatal("a huge spike after the non-finite value opened nothing: the series baseline is poisoned")
			}
			mean, stddev, ready := d.SeriesBaseline("host.cpu.percent", nil)
			if !ready || math.IsNaN(mean) || math.IsNaN(stddev) {
				t.Fatalf("baseline is mean=%v stddev=%v ready=%v", mean, stddev, ready)
			}
		})
	}
}

// TestDetector_ConcurrentObserveAndRead: production runs Observe from the
// ingest path (one call per collector tick) while Insights/SeriesCount/
// SeriesBaseline are read from API handler goroutines at the same time, and
// no test exercised that shape — every other test in this file drives a
// Detector from a single goroutine. d.mu is the only thing standing between
// this and a torn read of series/lastSeen/open, so this test is a check
// that it is enough on its own; run with -race.
func TestDetector_ConcurrentObserveAndRead(t *testing.T) {
	d := NewDetector()
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			name := "host.cpu.percent"
			if i%7 == 0 {
				// Occasionally a different series, to exercise the
				// eviction/lastSeen bookkeeping alongside the steady one.
				name = "proc.cpu.percent." + strconv.Itoa(i%5)
			}
			d.Observe([]Point{{Name: name, Value: float64(i % 100)}})
		}
	}()

	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = d.Insights()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _ = d.SeriesCount()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _, _ = d.SeriesBaseline("host.cpu.percent", nil)
		}
	}()

	wg.Wait()
}

// wobble returns n points of a 10±1 baseline: a real series' worth of
// jitter, deliberately not a constant, so the series has an honest standard
// deviation to be far from.
func wobble(n int) []float64 {
	pattern := []float64{9, 10, 11, 10}
	out := make([]float64, n)
	for i := range out {
		out[i] = pattern[i%len(pattern)]
	}
	return out
}

func severityOfAnomaly(t *testing.T, d *Detector, metric string) string {
	t.Helper()
	for _, ins := range d.Insights() {
		if ins.Kind == KindAnomaly && ins.Metric == metric {
			return ins.Severity
		}
	}
	return ""
}

// TestDetector_SeverityTracksTheSizeOfTheSpike is the in-sample-z
// regression.
//
// observeOneLocked used to fold the arriving point into the Welford
// statistics and then score it against them. Samuelson's inequality bounds
// every member of a sample to within (n-1)/sqrt(n) sample standard
// deviations of that sample's own mean, so the z it computed could not
// exceed 3.18 at n=12 whatever the value was — a spike to a billion on a
// baseline of ten scored exactly what a spike to thirteen scored, and both
// came out "warning". criticalSigma is 5, which n=12 cannot reach at all.
func TestDetector_SeverityTracksTheSizeOfTheSpike(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spike float64
		want  string
	}{
		{"a few sigma out is a warning", 13, SeverityWarning},
		{"orders of magnitude out is a page", 1e9, SeverityCritical},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDetector()
			fixed := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
			d.now = func() time.Time { return fixed }

			for _, v := range wobble(minSamples - 1) {
				d.Observe([]Point{{Name: "host.cpu.percent", Value: v}})
			}
			d.Observe([]Point{{Name: "host.cpu.percent", Value: tc.spike}})

			if got := severityOfAnomaly(t, d, "host.cpu.percent"); got != tc.want {
				t.Errorf("a spike to %g on a 10±1 baseline was graded %q, want %q",
					tc.spike, got, tc.want)
			}
		})
	}
}

// TestDetector_SpikeSeverityDoesNotDependOnHowLongTheSeriesHasRun: the same
// spike on the same baseline is the same event whether the series has
// eleven points of history or thirty-nine, and an operator reading the
// alert has no way to know which.
//
// Under in-sample scoring it was not: the Samuelson ceiling is 3.18 at n=12
// and 6.17 at n=40, so criticalSigma (5) was unreachable below n=29 and
// reachable above it, and the identical spike arrived as a warning in the
// morning and a page in the afternoon. seasonalKey makes that worse than a
// warm-up: every non-process series starts a fresh baseline for each hour
// of the day, so the sub-29 window is re-entered on 24 keys a day for as
// long as the agent runs.
func TestDetector_SpikeSeverityDoesNotDependOnHowLongTheSeriesHasRun(t *testing.T) {
	for _, history := range []int{minSamples - 1, 39} {
		t.Run(fmt.Sprintf("%d points of history", history), func(t *testing.T) {
			d := NewDetector()
			fixed := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
			d.now = func() time.Time { return fixed }

			for _, v := range wobble(history) {
				d.Observe([]Point{{Name: "host.memory.percent", Value: v}})
			}
			d.Observe([]Point{{Name: "host.memory.percent", Value: 1e9}})

			if got := severityOfAnomaly(t, d, "host.memory.percent"); got != SeverityCritical {
				t.Errorf("spike graded %q after %d points of history, want %q",
					got, history, SeverityCritical)
			}
		})
	}
}
