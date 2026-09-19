// Package store defines the Store interface forsight's API queries and its
// collectors/receivers write to, plus two implementations: MemoryStore (the
// default — in-process, resets on restart) and BadgerStore (opt-in via
// `forsight run --store badger`, persists to disk). Both satisfy identical
// query semantics — time-range + exact-label match, retention-based
// pruning — so either can back the same Store-typed caller.
package store

import (
	"context"
	"sort"
	"time"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

// MetricQuery filters a metrics read. A zero value matches everything within
// the store's retention window — there is no query language in v1 (see the
// README's scope note), just time range + exact label match.
type MetricQuery struct {
	Name   string            // exact match; empty matches any name
	Labels map[string]string // every pair must match (AND); nil matches any
	Since  time.Time         // zero value means "from the oldest retained point"
	// Before excludes records at or after this time, so [Since, Before) is
	// the window. Zero means no upper bound.
	Before time.Time
	// Limit caps the result at the newest Limit records in the window; zero
	// means no cap. The window is handed back oldest-first either way, so a
	// consumer that never sets Limit sees no change in shape.
	Limit int
	// PerName caps each metric name at its newest PerName records in the
	// window; zero means no cap. It is the bound an unscoped read needs: a
	// plain Limit on a read that mixes every name cuts a busy name's history
	// short before it reaches a quiet name's newest point, while PerName
	// keeps the newest N of every name. With Name set it is the same cap as
	// Limit. When both are set, PerName bounds the set and Limit keeps the
	// newest Limit of it by timestamp, the one order both backends agree on.
	// Within a name the result is oldest-first, as always; across names the
	// order is otherwise the store's own (a keyed store groups by name).
	PerName int
}

// newestByTime keeps the newest limit metrics of xs by timestamp, oldest-
// first. It is how Limit composes with PerName: the per-name cap has
// already bounded xs, and this picks the newest of it in the order both
// backends share. Stable, so equal timestamps keep their store order. Under
// the cap, xs comes back as it is, in the store's own order.
func newestByTime(xs []model.Metric, limit int) []model.Metric {
	if limit <= 0 || len(xs) <= limit {
		return xs
	}
	sorted := make([]model.Metric, len(xs))
	copy(sorted, xs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp.Before(sorted[j].Timestamp) })
	return sorted[len(sorted)-limit:]
}

// SpanQuery filters a spans read, analogous to MetricQuery.
type SpanQuery struct {
	Service string
	TraceID string
	Since   time.Time
	Before  time.Time // as MetricQuery.Before
	Limit   int       // as MetricQuery.Limit
}

// LogQuery filters a logs read, analogous to SpanQuery.
type LogQuery struct {
	Since    time.Time
	Before   time.Time         // as MetricQuery.Before
	Limit    int               // as MetricQuery.Limit
	Severity model.LogSeverity // empty matches any severity
	Source   string            // exact match; empty matches any source
}

// Store is the read/write surface the API and collectors/receivers use —
// metrics, spans, and log entries from the OTLP receiver and built-in
// collectors.
type Store interface {
	WriteMetrics(ctx context.Context, metrics []model.Metric) error
	QueryMetrics(ctx context.Context, q MetricQuery) ([]model.Metric, error)

	WriteSpans(ctx context.Context, spans []model.Span) error
	QuerySpans(ctx context.Context, q SpanQuery) ([]model.Span, error)

	WriteLogs(ctx context.Context, logs []model.LogEntry) error
	QueryLogs(ctx context.Context, q LogQuery) ([]model.LogEntry, error)

	// Ping reports whether the store can currently serve requests. It exists
	// for /readyz (see internal/api's handleReadyz): a query method can
	// succeed against an empty window even when the underlying resource
	// (a closed Badger database, say) is failing every real read, so
	// readiness needs its own cheap, always-attempted check rather than
	// inferring health from whatever the last query happened to return.
	Ping(ctx context.Context) error
}
