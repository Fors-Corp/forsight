package forseer

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	minSamples    = 12
	warningSigma  = 3
	criticalSigma = 5
	insightTTL    = 10 * time.Minute
	maxSeries     = 512
	cusumK        = 0.5
	cusumH        = 5
)

// Detector watches a stream of metric points and opens/closes insights
// using Welford's online mean/variance plus a CUSUM changepoint. No model
// file, no API key.
type Detector struct {
	mu     sync.Mutex
	series map[string]*rolling
	open   map[string]Insight
	// thresholds learns, per series, how far out of line is far enough.
	// Until a series has enough history it hands back the fixed sigma pair,
	// so a cold detector behaves exactly as it always did.
	thresholds *thresholdModel
	// outlier scores the joint host vector; see outlier.go. It only ever
	// sees a batch that carries all five of its inputs, which in practice
	// means one host-collector tick, never a Docker/OTLP/scrape batch.
	outlier *hostOutlierModel
	now     func() time.Time
}

type rolling struct {
	n     int
	mean  float64
	m2    float64
	cusum float64
}

// NewDetector builds an empty detector.
func NewDetector() *Detector {
	return &Detector{
		series:     make(map[string]*rolling),
		open:       make(map[string]Insight),
		thresholds: newThresholdModel(),
		outlier:    newHostOutlierModel(),
		now:        time.Now,
	}
}

// Observe records points and updates the active insight set.
func (d *Detector) Observe(points []Point) {
	if len(points) == 0 {
		return
	}
	now := d.now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expireLocked(now)
	for _, p := range points {
		d.observeOneLocked(p, now)
	}
	d.observeHostVectorLocked(points, now)
}

func (d *Detector) observeOneLocked(p Point, now time.Time) {
	key := seasonalKey(p.Name, p.Labels, now.Hour())
	s := d.series[key]
	if s == nil {
		if len(d.series) >= maxSeries {
			return
		}
		s = &rolling{}
		d.series[key] = s
	}
	s.n++
	delta := p.Value - s.mean
	s.mean += delta / float64(s.n)
	s.m2 += delta * (p.Value - s.mean)
	if s.n < minSamples {
		return
	}
	variance := s.m2 / float64(s.n-1)
	if variance <= 0 {
		delete(d.open, key)
		s.cusum = 0
		return
	}
	sigma := math.Sqrt(variance)
	if sigma == 0 {
		delete(d.open, key)
		s.cusum = 0
		return
	}
	z := math.Abs(p.Value-s.mean) / sigma
	warn, critical, _ := d.thresholds.Observe(key, z)
	switch {
	case z >= critical:
		d.open[key] = insight(key, KindAnomaly, p, z, SeverityCritical, now)
	case z >= warn:
		d.open[key] = insight(key, KindAnomaly, p, z, SeverityWarning, now)
	default:
		if existing, ok := d.open[key]; ok && existing.Kind == KindAnomaly {
			delete(d.open, key)
		}
	}

	s.cusum = math.Max(0, s.cusum+z-cusumK)
	cpKey := key + "|cusum"
	if s.cusum >= cusumH {
		d.open[cpKey] = Insight{
			ID:          cpKey,
			Kind:        KindChangepoint,
			Severity:    SeverityWarning,
			Title:       fmt.Sprintf("%s changed regime", p.Name),
			Description: fmt.Sprintf("CUSUM reached %.1f (value %.4g vs rolling mean %.4g)", s.cusum, p.Value, s.mean),
			Source:      "forseer",
			Metric:      p.Name,
			Value:       p.Value,
			Time:        now,
		}
		s.cusum = 0
	}
}

func insight(key, kind string, p Point, z float64, sev string, now time.Time) Insight {
	return Insight{
		ID:       key,
		Kind:     kind,
		Severity: sev,
		Title:    fmt.Sprintf("%s is %.1fσ from its baseline", p.Name, z),
		Description: fmt.Sprintf(
			"value %.4g vs rolling mean; %s",
			p.Value, sev,
		),
		Source: "forseer",
		Metric: p.Name,
		Value:  p.Value,
		Time:   now,
	}
}

// hostOutlierKey is the single Insight key host_outlier ever opens under —
// one model, one score, unlike the per-series map keyed by seasonalKey.
const hostOutlierKey = "host_outlier"

// observeHostVectorLocked feeds the host outlier model with this batch's
// cpu/memory/disk percentages and net-counter deltas, and opens or closes
// its insight in d.open the same way a per-series anomaly does. It is a
// no-op unless a single batch carries all three percentages plus both net
// counters — exactly what one host-collector tick emits (host.go), and
// never true of a Docker, OTLP or scrape batch, so this only ever scores
// this host's own samples, never a container's or another process's.
func (d *Detector) observeHostVectorLocked(points []Point, now time.Time) {
	var cpu, mem, disk, sent, recv float64
	var haveCPU, haveMem, haveDisk, haveSent, haveRecv bool
	for _, p := range points {
		switch p.Name {
		case "host.cpu.percent":
			cpu, haveCPU = p.Value, true
		case "host.memory.percent":
			mem, haveMem = p.Value, true
		case "host.disk.percent":
			if !haveDisk {
				// Today's host collector reports one disk path (DiskPath,
				// "/" by default); a future multi-disk collector would need
				// a choice here, but there is only ever one value to pick
				// from right now.
				disk, haveDisk = p.Value, true
			}
		case "host.net.bytes_sent":
			sent, haveSent = p.Value, true
		case "host.net.bytes_recv":
			recv, haveRecv = p.Value, true
		}
	}
	if !haveCPU || !haveMem || !haveDisk || !haveSent || !haveRecv {
		return
	}

	d2, metric, ready := d.outlier.Observe(cpu, mem, disk, sent, recv)
	if !ready {
		return
	}
	switch {
	case d2 >= hostOutlierCriticalChi2:
		d.openHostOutlierLocked(metric, d2, SeverityCritical, now)
	case d2 >= hostOutlierWarnChi2:
		d.openHostOutlierLocked(metric, d2, SeverityWarning, now)
	default:
		delete(d.open, hostOutlierKey)
	}
}

// openHostOutlierLocked opens (or re-scores) the single host_outlier
// insight. On a genuine open — a transition from not-open to open, never a
// re-score of one already open — it tells the model whether any per-series
// anomaly was open on one of the five inputs at that same moment, which is
// the exit criterion outlier.go's Card reports.
func (d *Detector) openHostOutlierLocked(metric string, d2 float64, sev string, now time.Time) {
	if _, already := d.open[hostOutlierKey]; !already {
		d.outlier.NoteOpened(!d.anyPerSeriesOpenLocked())
	}
	d.open[hostOutlierKey] = Insight{
		ID:       hostOutlierKey,
		Kind:     KindHostOutlier,
		Severity: sev,
		Title:    fmt.Sprintf("host looks unusual across cpu, memory, disk and network together (%.1f)", d2),
		Description: fmt.Sprintf(
			"squared Mahalanobis distance %.1f over the host vector; %s stands out most", d2, metric),
		Source:  "forseer",
		Metric:  metric,
		Value:   d2,
		Time:    now,
		Related: []string{metric},
	}
}

// anyPerSeriesOpenLocked reports whether a per-series anomaly is currently
// open on any of the five inputs host outlier reads (see hostOutlierMetrics
// in outlier.go).
func (d *Detector) anyPerSeriesOpenLocked() bool {
	for _, ins := range d.open {
		if ins.Kind != KindAnomaly {
			continue
		}
		for _, name := range hostOutlierMetrics {
			if ins.Metric == name {
				return true
			}
		}
	}
	return false
}

func (d *Detector) expireLocked(now time.Time) {
	for k, ins := range d.open {
		if now.Sub(ins.Time) > insightTTL {
			delete(d.open, k)
		}
	}
}

// SeriesBaseline returns the rolling mean and standard deviation Observe has
// built for one exact series (name plus labels) — the same Welford
// statistics the anomaly check itself scores z against — and whether it has
// minSamples of history to trust them. ready is false for a series that is
// too new, or one whose variance is not yet positive, exactly as
// observeOneLocked treats those cases: a caller ranking by "how far from
// baseline" gets no answer rather than a division by zero.
func (d *Detector) SeriesBaseline(name string, labels map[string]string) (mean, stddev float64, ready bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := seasonalKey(name, labels, d.now().Hour())
	s := d.series[key]
	if s == nil || s.n < minSamples {
		return 0, 0, false
	}
	variance := s.m2 / float64(s.n-1)
	if variance <= 0 {
		return 0, 0, false
	}
	return s.mean, math.Sqrt(variance), true
}

// Insights returns a copy of the currently open findings, critical first.
func (d *Detector) Insights() []Insight {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Insight, 0, len(d.open))
	now := d.now()
	d.expireLocked(now)
	for _, ins := range d.open {
		out = append(out, ins)
	}
	sortInsights(out)
	return out
}

func sortInsights(out []Insight) {
	sort.Slice(out, func(i, j int) bool {
		rank := func(s string) int {
			switch s {
			case SeverityCritical:
				return 0
			case SeverityWarning:
				return 1
			default:
				return 2
			}
		}
		ri, rj := rank(out[i].Severity), rank(out[j].Severity)
		if ri != rj {
			return ri < rj
		}
		return out[i].Time.After(out[j].Time)
	})
}

// seasonalKey keeps a separate rolling baseline per hour of day for low-
// cardinality series (host.*, docker.*, scraped jobs). Process series stay
// global — hour-suffixing them would blow the 512-series cap.
func seasonalKey(name string, labels map[string]string, hour int) string {
	base := seriesKey(name, labels)
	if strings.HasPrefix(name, "process.") {
		return base
	}
	return fmt.Sprintf("%s|h=%02d", base, hour)
}

func seriesKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, k := range keys {
		b.WriteByte(',')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}
