// Package store defines the Store interface forsight's API queries and its
// collectors/receivers write to, plus two implementations: MemoryStore (the
// default — in-process, resets on restart) and BadgerStore (opt-in via
// `forsight run --store badger`, persists to disk). Both satisfy identical
// query semantics — time-range + exact-label match, retention-based
// pruning — so either can back the same Store-typed caller.
package store

import (
	"context"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
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
