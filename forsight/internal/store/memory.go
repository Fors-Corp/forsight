package store

import (
	"context"
	"sync"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// DefaultMaxElements bounds how many metrics (and, separately, spans and
// logs) a MemoryStore will hold. Age alone is not a bound: retention prunes
// by timestamp, and timestamps on ingested data come off the wire, so a
// remote writer choosing a far-future timestamp is otherwise never pruned at
// all. This cap is what makes the store's memory a function of the agent's
// configuration rather than of what somebody sends it.
//
// 2,000,000 metrics is roughly 200 MB at this struct's size and comfortably
// above what the built-in collectors produce in an hour, so a normal agent
// never reaches it.
const DefaultMaxElements = 2_000_000

// MemoryStore is an in-process, retention-bounded store — it powers the live
// dashboard on its own with no setup, and backs every write when no
// persistent store is configured. Bounded twice over: by age, so a burst of
// writes can't starve older-but-still-relevant points within the window, and
// by element count, so neither a burst nor a hostile timestamp can grow it
// without limit.
//
// Metrics, spans and logs each get their own RWMutex rather than sharing
// one. No method ever needs more than one collection at a time — a metrics
// write only ever touches s.metrics, a logs query only ever touches s.logs —
// so one lock per collection lets a metrics write and a logs query (the
// dashboard runs several read-heavy pollers against continuous ingest, one
// per collection) proceed concurrently instead of one exclusive lock
// serialising work on disjoint data. SetMaxElements is the one exception: it
// touches all three, and takes all three locks in the fixed order below to
// do it.
type MemoryStore struct {
	retention time.Duration
	now       func() time.Time

	// maxElements is guarded by all three locks below, not one of its own:
	// SetMaxElements writes it only while holding metricsMu, spansMu and
	// logsMu all exclusively (see the fixed acquisition order there), and
	// every reader (each pruneXLocked, called only under its own collection's
	// lock) already holds at least one of the three. Holding all three to
	// write is what makes holding just one enough to read: a writer can't be
	// in the middle of the write while any prune*Locked call — which needs
	// at least one of the three — is running.
	maxElements int

	metricsMu sync.RWMutex
	metrics   []model.Metric

	spansMu sync.RWMutex
	spans   []model.Span

	logsMu sync.RWMutex
	logs   []model.LogEntry
}

// NewMemoryStore builds a MemoryStore retaining data for the given window
// (e.g. 1h for the live dashboard).
func NewMemoryStore(retention time.Duration) *MemoryStore {
	return &MemoryStore{retention: retention, maxElements: DefaultMaxElements, now: time.Now}
}

// SetMaxElements overrides the per-collection element cap. A value <= 0
// restores the default rather than disabling the cap: an unbounded in-memory
// store is not an option this type offers.
//
// Acquires all three collection locks, in a fixed order (metrics, spans,
// logs — the same order every place in this file that ever needs more than
// one lock at once must use, to rule out a lock-ordering deadlock), so the
// write to maxElements and the three prunes it triggers happen atomically
// with respect to every other method here, each of which touches at most
// one collection under at most one of these locks.
func (s *MemoryStore) SetMaxElements(n int) {
	if n <= 0 {
		n = DefaultMaxElements
	}
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.spansMu.Lock()
	defer s.spansMu.Unlock()
	s.logsMu.Lock()
	defer s.logsMu.Unlock()
	s.maxElements = n
	s.pruneMetricsLocked()
	s.pruneSpansLocked()
	s.pruneLogsLocked()
}

// Ping always succeeds: a MemoryStore is a mutex-protected slice living in
// this process's own memory, so it has no external resource (a disk, a
// connection) that Ping could find unavailable while the process itself is
// still up. It exists only so MemoryStore satisfies Store the same way
// BadgerStore does, whose Ping can fail.
func (s *MemoryStore) Ping(_ context.Context) error {
	return nil
}

func (s *MemoryStore) WriteMetrics(_ context.Context, metrics []model.Metric) error {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()
	s.metrics = append(s.metrics, metrics...)
	s.pruneMetricsLocked()
	return nil
}

func (s *MemoryStore) QueryMetrics(_ context.Context, q MetricQuery) ([]model.Metric, error) {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	limit, perName := q.Limit, q.PerName
	if q.Name != "" && perName > 0 && (limit == 0 || perName < limit) {
		// One name: the per-name cap is the cap. Mirrors BadgerStore's own
		// QueryMetrics fold (see its comment there) so the dashboard's
		// common single-name-with-per-name-cap read takes the same bounded
		// path on both backends — a walk that stops at limit, rather than
		// newestPerName's whole-store grouping walk, which this scoped case
		// does not need.
		limit, perName = perName, 0
	}
	match := func(m model.Metric) bool { return matchesMetric(m, q) }
	if perName > 0 {
		return newestByTime(newestPerName(s.metrics, perName, match), limit), nil
	}
	return matchNewest(s.metrics, limit, match), nil
}

func (s *MemoryStore) WriteSpans(_ context.Context, spans []model.Span) error {
	s.spansMu.Lock()
	defer s.spansMu.Unlock()
	s.spans = append(s.spans, spans...)
	s.pruneSpansLocked()
	return nil
}

func (s *MemoryStore) QuerySpans(_ context.Context, q SpanQuery) ([]model.Span, error) {
	s.spansMu.RLock()
	defer s.spansMu.RUnlock()

	return matchNewest(s.spans, q.Limit, func(sp model.Span) bool { return matchesSpan(sp, q) }), nil
}

func (s *MemoryStore) WriteLogs(_ context.Context, logs []model.LogEntry) error {
	s.logsMu.Lock()
	defer s.logsMu.Unlock()
	s.logs = append(s.logs, logs...)
	s.pruneLogsLocked()
	return nil
}

func (s *MemoryStore) QueryLogs(_ context.Context, q LogQuery) ([]model.LogEntry, error) {
	s.logsMu.RLock()
	defer s.logsMu.RUnlock()

	return matchNewest(s.logs, q.Limit, func(entry model.LogEntry) bool { return matchesLog(entry, q) }), nil
}

// pruneMetricsLocked drops points older than the retention window, then
// enforces the element cap. Callers must hold s.metricsMu for writing.
//
// The age scan is skipped when the oldest retained element — data arrives
// in roughly chronological order, the same assumption capOldest's own
// comment documents — is still within the retention window: nothing has
// expired, so there is no reason for every write to re-scan up to
// maxElements (2,000,000 by default) entries just to learn that. The
// element cap is still enforced unconditionally either way; it is the hard
// bound, and this only skips the O(n) age pass in the common case where it
// would keep everything anyway.
func (s *MemoryStore) pruneMetricsLocked() {
	if len(s.metrics) == 0 {
		return
	}
	cutoff := s.now().Add(-s.retention)
	if s.metrics[0].Timestamp.After(cutoff) {
		s.metrics = capOldest(s.metrics, s.maxElements)
		return
	}
	kept := s.metrics[:0]
	for _, m := range s.metrics {
		if m.Timestamp.After(cutoff) {
			kept = append(kept, m)
		}
	}
	s.metrics = capOldest(kept, s.maxElements)
}

// capOldest keeps the newest max elements, dropping from the front. Writes
// arrive in roughly chronological order, so the front is the oldest data —
// and when it is not (a hostile or clock-skewed writer), dropping the front
// is still the right call, because that is the data the caller inserted
// earliest and the alternative is unbounded growth.
func capOldest[T any](xs []T, max int) []T {
	if max <= 0 || len(xs) <= max {
		return xs
	}
	return append(xs[:0], xs[len(xs)-max:]...)
}

// pruneSpansLocked mirrors pruneMetricsLocked's fast path for the common
// case where nothing has expired yet — see its comment. Callers must hold
// s.spansMu for writing.
func (s *MemoryStore) pruneSpansLocked() {
	if len(s.spans) == 0 {
		return
	}
	cutoff := s.now().Add(-s.retention)
	if s.spans[0].Start.After(cutoff) {
		s.spans = capOldest(s.spans, s.maxElements)
		return
	}
	kept := s.spans[:0]
	for _, sp := range s.spans {
		if sp.Start.After(cutoff) {
			kept = append(kept, sp)
		}
	}
	s.spans = capOldest(kept, s.maxElements)
}

// pruneLogsLocked mirrors pruneMetricsLocked's fast path for the common
// case where nothing has expired yet — see its comment. Callers must hold
// s.logsMu for writing.
func (s *MemoryStore) pruneLogsLocked() {
	if len(s.logs) == 0 {
		return
	}
	cutoff := s.now().Add(-s.retention)
	if s.logs[0].Timestamp.After(cutoff) {
		s.logs = capOldest(s.logs, s.maxElements)
		return
	}
	kept := s.logs[:0]
	for _, entry := range s.logs {
		if entry.Timestamp.After(cutoff) {
			kept = append(kept, entry)
		}
	}
	s.logs = capOldest(kept, s.maxElements)
}

// matchNewest walks xs — a slice in write order, which for this store is
// the order records arrived — from the tail, keeping every element that
// satisfies pred until it has limit of them (or, when limit <= 0, every
// match), then restores write order. This computes exactly what filtering
// the whole slice and then keeping the newest limit would, but a positive
// limit stops the walk and sizes the result buffer to limit, not to len(xs):
// a query for the newest 1 of a store holding two million records no longer
// allocates a buffer sized to all two million to answer it (see
// DefaultMaxElements' comment on why that bound exists in the first place).
func matchNewest[T any](xs []T, limit int, pred func(T) bool) []T {
	if limit <= 0 {
		kept := make([]T, 0)
		for _, x := range xs {
			if pred(x) {
				kept = append(kept, x)
			}
		}
		return kept
	}
	kept := make([]T, 0, limit)
	for i := len(xs) - 1; i >= 0 && len(kept) < limit; i-- {
		if pred(xs[i]) {
			kept = append(kept, xs[i])
		}
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept
}

// newestPerName keeps the newest perName records of each metric name in xs
// that satisfies match, in xs's own (write) order, which is the order the
// collectors wrote them. A walk from the end counts each name up to the
// cap, so the kept set is the tail of every name's run at once. match is
// applied inline rather than pre-filtering xs into a separate slice first,
// so the result buffer is sized to what is actually kept (bounded by the
// number of distinct names times perName), never to the whole store — the
// same reasoning as matchNewest, for the one caller (an unscoped per-name
// read) that can't use matchNewest's early stop, because it doesn't know
// how many distinct names it will meet until it has walked every record.
func newestPerName(xs []model.Metric, perName int, match func(model.Metric) bool) []model.Metric {
	if perName <= 0 {
		return xs
	}
	counts := make(map[string]int)
	kept := make([]model.Metric, 0)
	for i := len(xs) - 1; i >= 0; i-- {
		if !match(xs[i]) {
			continue
		}
		if counts[xs[i].Name] < perName {
			counts[xs[i].Name]++
			kept = append(kept, xs[i])
		}
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept
}

func matchesMetric(m model.Metric, q MetricQuery) bool {
	if q.Name != "" && m.Name != q.Name {
		return false
	}
	if !q.Since.IsZero() && m.Timestamp.Before(q.Since) {
		return false
	}
	if !q.Before.IsZero() && !m.Timestamp.Before(q.Before) {
		return false
	}
	for k, v := range q.Labels {
		if m.Labels[k] != v {
			return false
		}
	}
	return true
}

func matchesSpan(sp model.Span, q SpanQuery) bool {
	if q.Service != "" && sp.Service != q.Service {
		return false
	}
	if q.TraceID != "" && sp.TraceID != q.TraceID {
		return false
	}
	if !q.Since.IsZero() && sp.Start.Before(q.Since) {
		return false
	}
	if !q.Before.IsZero() && !sp.Start.Before(q.Before) {
		return false
	}
	return true
}

func matchesLog(entry model.LogEntry, q LogQuery) bool {
	if q.Severity != "" && entry.Severity != q.Severity {
		return false
	}
	if q.Source != "" && entry.Source != q.Source {
		return false
	}
	if !q.Since.IsZero() && entry.Timestamp.Before(q.Since) {
		return false
	}
	if !q.Before.IsZero() && !entry.Timestamp.Before(q.Before) {
		return false
	}
	return true
}
