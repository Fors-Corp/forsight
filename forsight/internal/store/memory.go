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
type MemoryStore struct {
	retention   time.Duration
	maxElements int
	now         func() time.Time

	mu      sync.RWMutex
	metrics []model.Metric
	spans   []model.Span
	logs    []model.LogEntry
}

// NewMemoryStore builds a MemoryStore retaining data for the given window
// (e.g. 1h for the live dashboard).
func NewMemoryStore(retention time.Duration) *MemoryStore {
	return &MemoryStore{retention: retention, maxElements: DefaultMaxElements, now: time.Now}
}

// SetMaxElements overrides the per-collection element cap. A value <= 0
// restores the default rather than disabling the cap: an unbounded in-memory
// store is not an option this type offers.
func (s *MemoryStore) SetMaxElements(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		n = DefaultMaxElements
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics = append(s.metrics, metrics...)
	s.pruneMetricsLocked()
	return nil
}

func (s *MemoryStore) QueryMetrics(_ context.Context, q MetricQuery) ([]model.Metric, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Metric, 0, len(s.metrics))
	for _, m := range s.metrics {
		if !matchesMetric(m, q) {
			continue
		}
		out = append(out, m)
	}
	if q.PerName > 0 {
		return newestByTime(newestPerName(out, q.PerName), q.Limit), nil
	}
	return newest(out, q.Limit), nil
}

func (s *MemoryStore) WriteSpans(_ context.Context, spans []model.Span) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spans = append(s.spans, spans...)
	s.pruneSpansLocked()
	return nil
}

func (s *MemoryStore) QuerySpans(_ context.Context, q SpanQuery) ([]model.Span, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.Span, 0, len(s.spans))
	for _, sp := range s.spans {
		if !matchesSpan(sp, q) {
			continue
		}
		out = append(out, sp)
	}
	return newest(out, q.Limit), nil
}

func (s *MemoryStore) WriteLogs(_ context.Context, logs []model.LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, logs...)
	s.pruneLogsLocked()
	return nil
}

func (s *MemoryStore) QueryLogs(_ context.Context, q LogQuery) ([]model.LogEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]model.LogEntry, 0, len(s.logs))
	for _, entry := range s.logs {
		if !matchesLog(entry, q) {
			continue
		}
		out = append(out, entry)
	}
	return newest(out, q.Limit), nil
}

// pruneMetricsLocked drops points older than the retention window, then
// enforces the element cap. Callers must hold s.mu for writing.
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
// case where nothing has expired yet — see its comment.
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
// case where nothing has expired yet — see its comment.
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

// newest keeps the last limit records of a slice that is in write order,
// which for this store is the order records arrived — the collectors and
// receivers write in time order, so the tail is the newest window. Zero
// means everything.
func newest[T any](xs []T, limit int) []T {
	if limit > 0 && len(xs) > limit {
		return xs[len(xs)-limit:]
	}
	return xs
}

// newestPerName keeps the newest perName records of each metric name in xs,
// in xs's own (write) order, which is the order the collectors wrote them.
// A walk from the end counts each name up to the cap, so the kept set is the
// tail of every name's run at once.
func newestPerName(xs []model.Metric, perName int) []model.Metric {
	if perName <= 0 {
		return xs
	}
	counts := make(map[string]int)
	kept := make([]model.Metric, 0, len(xs))
	for i := len(xs) - 1; i >= 0; i-- {
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
