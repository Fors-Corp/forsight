package forseer

import (
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
	n      int
	p50    *p2Estimator
	p99    *p2Estimator
	cusum  float64
	runLen int
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

	// CUSUM on this span's deviation from the endpoint's own median, scaled
	// by its interquartile spread (q3-q1, read straight off the p50
	// tracker's own markers) — the same changepoint test the Detector runs
	// on its metric series (cusumK/cusumH), but wired to reset these two
	// estimators rather than open a changepoint insight: spans never reach
	// the Detector, and a marker set that keeps its pre-shift shape forever
	// would keep comparing tonight's traffic to a baseline that stopped
	// being true the moment the deploy went out.
	iqr := s.p50.height[3] - s.p50.height[1]
	if iqr <= 0 {
		return
	}
	z := math.Abs(sp.DurationMs-median) / iqr
	s.cusum = math.Max(0, s.cusum+z-cusumK)
	if s.cusum >= cusumH {
		s.p50.reset()
		s.p99.reset()
		s.n = 0
		s.runLen = 0
		s.cusum = 0
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
