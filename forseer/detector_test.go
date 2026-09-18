package forseer

import (
	"strings"
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
