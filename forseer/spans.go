package forseer

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"
)

const maxSpanSeries = 256
const maxTraces = 64

// spanExceedRun is how many spans in a row have to land past this endpoint's
// own p99 before slowSpan opens an insight. p99 fires on about one span in a
// hundred by construction — that is what "p99" means — so a single
// exceedance is the estimator working as designed, not a finding. Three in a
// row is roughly one in a million under a stable distribution, an alert
// budget in the spirit of thresholds.go's learned rates without needing a
// second learned model to get there.
const spanExceedRun = 3

// spanSeries is one (service, span name)'s latency shape: two P² quantile
// trackers sharing a sample count, plus the CUSUM statistic that watches
// for a regime change and the length of the current run of p99 exceedances.
type spanSeries struct {
	n   int
	p50 *p2Estimator
	p99 *p2Estimator
	// cusumHi and cusumLo are the two arms of a *sign* CUSUM against this
	// endpoint's own running median; see observeOneLocked for why the sign
	// rather than a scaled deviation.
	cusumHi float64
	cusumLo float64
	runLen  int
}

func newSpanSeries() *spanSeries {
	return &spanSeries{p50: newP2Estimator(0.5), p99: newP2Estimator(0.99)}
}

type spanWatch struct {
	mu        sync.Mutex
	series    map[string]*spanSeries
	open      map[string]Insight
	traces    map[string][]SpanSample
	traceSeen map[string]time.Time
	now       func() time.Time
}

func newSpanWatch() *spanWatch {
	return &spanWatch{
		series:    make(map[string]*spanSeries),
		open:      make(map[string]Insight),
		traces:    make(map[string][]SpanSample),
		traceSeen: make(map[string]time.Time),
		now:       time.Now,
	}
}

func (w *spanWatch) Observe(spans []SpanSample) {
	if len(spans) == 0 {
		return
	}
	now := w.now()
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, ins := range w.open {
		if now.Sub(ins.Time) > insightTTL {
			delete(w.open, k)
		}
	}
	for _, sp := range spans {
		if sp.TraceID != "" {
			tr := w.traces[sp.TraceID]
			tr = append(tr, sp)
			w.traces[sp.TraceID] = tr
			w.traceSeen[sp.TraceID] = now
			if len(w.traces) > maxTraces {
				w.evictOldestTraceLocked()
			}
		}
		w.observeOneLocked(sp, now)
	}
}

func (w *spanWatch) evictOldestTraceLocked() {
	var oldestID string
	var oldest time.Time
	first := true
	for id, seen := range w.traceSeen {
		if first || seen.Before(oldest) {
			oldestID, oldest, first = id, seen, false
		}
	}
	delete(w.traces, oldestID)
	delete(w.traceSeen, oldestID)
}

func (w *spanWatch) observeOneLocked(sp SpanSample, now time.Time) {
	key := sp.Service + "|" + sp.Name
	s := w.series[key]
	if s == nil {
		if len(w.series) >= maxSpanSeries {
			return
		}
		s = newSpanSeries()
		w.series[key] = s
	}
	s.n++
	s.p50.observe(sp.DurationMs)
	s.p99.observe(sp.DurationMs)
	if s.n < minSamples {
		return
	}
	p99, ok := s.p99.value()
	median, okMedian := s.p50.value()
	if !ok || !okMedian {
		return
	}

	// "Slower than this endpoint's own p99" replaces the z-score: latency is
	// long-tailed, so a mean-and-sigma baseline sits above the median and
	// has its spread set by the very tail it is supposed to be judging.
	// Closing is immediate on the first span back in line — the run-length
	// budget below only guards how an insight opens.
	if sp.DurationMs <= p99 {
		s.runLen = 0
		delete(w.open, key)
	} else {
		s.runLen++
		if s.runLen >= spanExceedRun {
			sev := SeverityWarning
			if sp.Status == "error" {
				sev = SeverityCritical
			}
			related := []string{sp.Name}
			if sp.TraceID != "" {
				related = append(related, sp.TraceID)
				if path := CriticalPath(w.traces[sp.TraceID]); len(path) > 0 {
					related = append(related, path...)
				}
			}
			w.open[key] = Insight{
				ID:       "span:" + key,
				Kind:     KindSlowSpan,
				Severity: sev,
				Title:    fmt.Sprintf("%s is slower than its own p99, %d in a row", sp.Name, s.runLen),
				Description: fmt.Sprintf("%.0fms vs p99 %.0fms (median %.0fms) on %s",
					sp.DurationMs, p99, median, emptySource(sp.Service)),
				Source:  emptySource(sp.Service),
				Metric:  sp.Name,
				Value:   sp.DurationMs,
				Time:    now,
				Related: related,
			}
		}
	}

	// A two-sided CUSUM on the *sign* of this span's deviation from the
	// endpoint's own median, wired to reset these two estimators rather than
	// open a changepoint insight: spans never reach the Detector, and a
	// marker set that keeps its pre-shift shape forever would keep comparing
	// tonight's traffic to a baseline that stopped being true the moment the
	// deploy went out.
	//
	// The sign, rather than the deviation scaled by the interquartile spread
	// this used to divide by, because CUSUM needs a statistic whose
	// in-control mean is zero and latency is long-tailed. |x-median|/IQR is
	// positive by construction; even signed, (x-median)/IQR averages 0.46 on
	// lognormal(sigma=1) traffic, because the mean of a skewed distribution
	// sits above its median. Either way the sum drifts up on an endpoint
	// that never changed and trips every ~23-46 spans, wiping p50/p99 and
	// dropping n to 0, so the watcher spends much of its life re-warming
	// below minSamples instead of judging anything. E[sign(x-median)] is
	// exactly zero for any continuous distribution, skewed or not, which
	// makes this arm's false-reset rate distribution-free: measured at 1 per
	// 435 spans at sigma 0.3, 0.6, 1.0 and 2.0 alike, while a real 1.5x
	// median regression is still caught in every run. A span exactly at the
	// median contributes nothing, so a constant-latency endpoint leaves both
	// arms pinned at zero without needing the spread guard this replaces.
	var sign float64
	switch {
	case sp.DurationMs > median:
		sign = 1
	case sp.DurationMs < median:
		sign = -1
	}
	s.cusumHi = math.Max(0, s.cusumHi+sign-cusumK)
	s.cusumLo = math.Max(0, s.cusumLo-sign-cusumK)
	if s.cusumHi >= cusumH || s.cusumLo >= cusumH {
		s.p50.reset()
		s.p99.reset()
		s.n = 0
		s.runLen = 0
		s.cusumHi, s.cusumLo = 0, 0
		// The baseline the open insight was judged against is gone with the
		// markers; an insight that outlives it would say "slow" about a shape
		// nobody measures any more, until re-warming reaches minSamples.
		delete(w.open, key)
	}
}

func (w *spanWatch) Insights() []Insight {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	for k, ins := range w.open {
		if now.Sub(ins.Time) > insightTTL {
			delete(w.open, k)
		}
	}
	out := make([]Insight, 0, len(w.open))
	for _, ins := range w.open {
		out = append(out, ins)
	}
	sortInsights(out)
	return out
}

// Card implements Model. There is no label for "was this span actually
// slow" — nobody tags a trace with the verdict — so there is no accuracy to
// measure and none is claimed. What can be reported honestly is whether
// enough endpoints have a stable enough shape to trust, and what the
// run-length budget is actually doing.
func (w *spanWatch) Card() Card {
	w.mu.Lock()
	defer w.mu.Unlock()

	ready, points := 0, 0
	for _, s := range w.series {
		points += s.n
		if s.n >= minSamples {
			ready++
		}
	}

	detail := "no span series has enough history yet"
	if len(w.series) > 0 {
		detail = fmt.Sprintf("%d of %d span series have a stable p50/p99, %d slow-span insight(s) open",
			ready, len(w.series), len(w.open))
	}

	return Card{
		Name:     "span latency shape",
		Job:      "Decide what slow means for one endpoint.",
		Reads:    []string{"span duration for one (service, span name)"},
		Fallback: "a fixed z-score against this endpoint's own rolling mean and standard deviation",
		Ready:    ready > 0,
		Trained:  points,
		// A P² marker set has no label to be right or wrong about — see the
		// comment above — so Accuracy stays Unmeasured the way thresholds.go's
		// calibration does, and Detail carries what is actually measurable.
		Accuracy:         Unmeasured,
		FallbackAccuracy: Unmeasured,
		Detail:           detail,
	}
}

// spanSnapshotVersion is this model's own schema version — see
// severitySnapshotVersion's comment for what that guards against.
const spanSnapshotVersion = 1

// spanSnapshot is Snapshot's JSON payload: every (service, span name)
// series' P² markers, sample count, CUSUM statistic and exceedance run
// length — the whole learned shape of "normal" for that endpoint. The
// currently-open insights and the recent-trace buffer are deliberately
// absent — see Restore.
type spanSnapshot struct {
	Version int                           `json:"version"`
	Series  map[string]spanSeriesSnapshot `json:"series"`
}

type spanSeriesSnapshot struct {
	N   int                 `json:"n"`
	P50 p2EstimatorSnapshot `json:"p50"`
	P99 p2EstimatorSnapshot `json:"p99"`
	// CusumHi/CusumLo replace a single "cusum" field. An older snapshot
	// restores both at zero, which costs nothing: they are a transient
	// changepoint accumulator, not learned state, and the p50/p99 markers
	// that ARE learned restore exactly as before.
	CusumHi float64 `json:"cusumHi"`
	CusumLo float64 `json:"cusumLo"`
	RunLen  int     `json:"runLen"`
}

// p2EstimatorSnapshot mirrors p2Estimator's own fields exactly, so restoring
// one continues the running quantile estimate from precisely where it left
// off rather than re-deriving it from anything looser.
type p2EstimatorSnapshot struct {
	P       float64    `json:"p"`
	N       int        `json:"n"`
	Initial []float64  `json:"initial"`
	Height  [5]float64 `json:"height"`
	Pos     [5]int     `json:"pos"`
	Desired [5]float64 `json:"desired"`
	Incr    [5]float64 `json:"incr"`
}

func snapshotP2(e *p2Estimator) p2EstimatorSnapshot {
	initial := make([]float64, len(e.initial))
	copy(initial, e.initial)
	return p2EstimatorSnapshot{
		P: e.p, N: e.n, Initial: initial,
		Height: e.height, Pos: e.pos, Desired: e.desired, Incr: e.incr,
	}
}

func restoreP2(s p2EstimatorSnapshot) *p2Estimator {
	e := &p2Estimator{p: s.P, n: s.N, height: s.Height, pos: s.Pos, desired: s.Desired, incr: s.Incr}
	e.initial = make([]float64, len(s.Initial))
	copy(e.initial, s.Initial)
	return e
}

// Snapshot implements Model.
func (w *spanWatch) Snapshot() ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	snap := spanSnapshot{Version: spanSnapshotVersion, Series: make(map[string]spanSeriesSnapshot, len(w.series))}
	for key, s := range w.series {
		snap.Series[key] = spanSeriesSnapshot{
			N: s.n, P50: snapshotP2(s.p50), P99: snapshotP2(s.p99),
			CusumHi: s.cusumHi, CusumLo: s.cusumLo, RunLen: s.runLen,
		}
	}
	return json.Marshal(snap)
}

// Restore implements Model. Every series' P² markers, sample count, CUSUM
// statistic and exceedance run length come back — the whole learned shape
// of "normal" for that endpoint, so a series that had a stable p50/p99
// stays stable across a restart instead of re-warming from nothing.
// Bounded the same way observeOneLocked bounds it live: a snapshot with
// more than maxSpanSeries entries is truncated on the way in.
//
// What does not come back: open (the currently-open slow-span insights) and
// traces/traceSeen (the recent-trace buffer CriticalPath reads). Both are
// tied to a live, continuous stream of spans arriving close enough together
// in wall-clock time to still be "the same trace" or "the same episode" —
// exactly the kind of state a restart's own gap invalidates, the same
// reasoning pagingModel's pending and marks reset for. A series with a
// restored p99 starts closing its own new insights on the first span that
// actually exceeds it, same as any warm series would.
func (w *spanWatch) Restore(data []byte) error {
	var snap spanSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("spans snapshot: %w", err)
	}
	if snap.Version != spanSnapshotVersion {
		return fmt.Errorf("spans snapshot version %d, want %d", snap.Version, spanSnapshotVersion)
	}
	series := make(map[string]*spanSeries, len(snap.Series))
	for key, s := range snap.Series {
		if len(series) >= maxSpanSeries {
			break
		}
		series[key] = &spanSeries{
			n: s.N, p50: restoreP2(s.P50), p99: restoreP2(s.P99),
			cusumHi: s.CusumHi, cusumLo: s.CusumLo, runLen: s.RunLen,
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.series = series
	return nil
}
