package store

import (
	"context"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// runStoreConformanceTests exercises the query semantics every Store
// implementation must share — write/query round trips and exact
// name/service/source, label, TraceID, and Since (time-range) filtering —
// purely through the public Store interface. Both MemoryStore
// (memory_test.go) and BadgerStore (badger_test.go) run it, so the two
// can't silently drift on what a query is supposed to match.
//
// Retention/pruning mechanics are deliberately NOT covered here: MemoryStore
// prunes by comparing an injectable clock against each record's own
// Timestamp field, while BadgerStore expires by Badger's wall-clock TTL
// anchored to write time — different enough in mechanism (and in how a test
// can control time) that each gets its own test instead (see
// TestMemoryStore_PrunesOldMetrics/PrunesOldLogs and
// TestBadgerStore_RetentionExpiresEntries).
func runStoreConformanceTests(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()

	t.Run("metrics write-then-query round trip", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		now := time.Now()

		err := s.WriteMetrics(ctx, []model.Metric{
			{Name: "host.cpu.percent", Value: 42, Timestamp: now, Labels: map[string]string{"host": "a"}},
			{Name: "host.cpu.percent", Value: 7, Timestamp: now, Labels: map[string]string{"host": "b"}},
		})
		if err != nil {
			t.Fatalf("WriteMetrics: %v", err)
		}

		got, err := s.QueryMetrics(ctx, MetricQuery{Name: "host.cpu.percent"})
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("QueryMetrics returned %d metrics, want 2", len(got))
		}
	})

	t.Run("metrics query filters by label", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		now := time.Now()

		_ = s.WriteMetrics(ctx, []model.Metric{
			{Name: "m", Value: 1, Timestamp: now, Labels: map[string]string{"host": "a"}},
			{Name: "m", Value: 2, Timestamp: now, Labels: map[string]string{"host": "b"}},
		})

		got, err := s.QueryMetrics(ctx, MetricQuery{Labels: map[string]string{"host": "a"}})
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 1 || got[0].Value != 1 {
			t.Fatalf("QueryMetrics with label filter = %+v, want exactly the host=a metric", got)
		}
	})

	t.Run("metrics query since excludes older points", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		now := time.Now()

		_ = s.WriteMetrics(ctx, []model.Metric{
			{Name: "m", Value: 1, Timestamp: now.Add(-30 * time.Minute)},
			{Name: "m", Value: 2, Timestamp: now.Add(-5 * time.Minute)},
		})

		got, err := s.QueryMetrics(ctx, MetricQuery{Since: now.Add(-10 * time.Minute)})
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 1 || got[0].Value != 2 {
			t.Fatalf("QueryMetrics with Since = %+v, want only the recent point", got)
		}
	})

	t.Run("metrics query with no name filter scans every name", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		now := time.Now()

		_ = s.WriteMetrics(ctx, []model.Metric{
			{Name: "a", Value: 1, Timestamp: now},
			{Name: "b", Value: 2, Timestamp: now},
		})

		got, err := s.QueryMetrics(ctx, MetricQuery{})
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("QueryMetrics with no filter = %+v, want both metrics", got)
		}
	})

	t.Run("spans write-then-query filters by service", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		now := time.Now()

		err := s.WriteSpans(ctx, []model.Span{
			{TraceID: "t1", SpanID: "s1", Service: "checkout", Start: now, Status: model.SpanStatusOK},
			{TraceID: "t2", SpanID: "s2", Service: "payments", Start: now, Status: model.SpanStatusError},
		})
		if err != nil {
			t.Fatalf("WriteSpans: %v", err)
		}

		got, err := s.QuerySpans(ctx, SpanQuery{Service: "payments"})
		if err != nil {
			t.Fatalf("QuerySpans: %v", err)
		}
		if len(got) != 1 || got[0].TraceID != "t2" {
			t.Fatalf("QuerySpans with service filter = %+v, want exactly the payments span", got)
		}
	})

	t.Run("spans query filters by trace ID across services", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		now := time.Now()

		_ = s.WriteSpans(ctx, []model.Span{
			{TraceID: "t1", SpanID: "s1", Service: "checkout", Start: now},
			{TraceID: "t1", SpanID: "s2", Service: "payments", Start: now},
			{TraceID: "t2", SpanID: "s3", Service: "checkout", Start: now},
		})

		got, err := s.QuerySpans(ctx, SpanQuery{TraceID: "t1"})
		if err != nil {
			t.Fatalf("QuerySpans: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("QuerySpans with TraceID filter = %+v, want the 2 spans of trace t1 across services", got)
		}
	})

	t.Run("logs write-then-query filters by source and severity", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		now := time.Now()

		err := s.WriteLogs(ctx, []model.LogEntry{
			{Timestamp: now, Severity: model.LogSeverityInfo, Source: "checkout", Message: "ok"},
			{Timestamp: now, Severity: model.LogSeverityError, Source: "payments", Message: "fail"},
		})
		if err != nil {
			t.Fatalf("WriteLogs: %v", err)
		}

		got, err := s.QueryLogs(ctx, LogQuery{Source: "payments"})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if len(got) != 1 || got[0].Message != "fail" {
			t.Fatalf("QueryLogs with source filter = %+v, want exactly the payments entry", got)
		}

		got, err = s.QueryLogs(ctx, LogQuery{Severity: model.LogSeverityError})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if len(got) != 1 || got[0].Source != "payments" {
			t.Fatalf("QueryLogs with severity filter = %+v, want exactly the error entry", got)
		}
	})

	t.Run("logs query since excludes older entries", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		now := time.Now()

		_ = s.WriteLogs(ctx, []model.LogEntry{
			{Timestamp: now.Add(-30 * time.Minute), Severity: model.LogSeverityInfo, Source: "a", Message: "old"},
			{Timestamp: now.Add(-5 * time.Minute), Severity: model.LogSeverityInfo, Source: "a", Message: "recent"},
		})

		got, err := s.QueryLogs(ctx, LogQuery{Since: now.Add(-10 * time.Minute)})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if len(got) != 1 || got[0].Message != "recent" {
			t.Fatalf("QueryLogs with Since = %+v, want only the recent entry", got)
		}
	})

	// Limit and Before, on every record type. Ten points a minute apart,
	// oldest written first, as the collectors write them.
	t.Run("metrics query limit returns the newest N, oldest-first", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		base := time.Now().Add(-30 * time.Minute)
		points := make([]model.Metric, 0, 10)
		for i := 0; i < 10; i++ {
			points = append(points, model.Metric{Name: "m", Value: float64(i), Timestamp: base.Add(time.Duration(i) * time.Minute)})
		}
		_ = s.WriteMetrics(ctx, points)

		got, err := s.QueryMetrics(ctx, MetricQuery{Name: "m", Limit: 3})
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 3 || got[0].Value != 7 || got[1].Value != 8 || got[2].Value != 9 {
			t.Fatalf("QueryMetrics with Limit 3 = %+v, want values 7, 8, 9 in that order", got)
		}
		// The unscoped scan takes a different path in a keyed store.
		got, err = s.QueryMetrics(ctx, MetricQuery{Limit: 2})
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 2 || got[0].Value != 8 || got[1].Value != 9 {
			t.Fatalf("unscoped QueryMetrics with Limit 2 = %+v, want values 8, 9", got)
		}
	})

	t.Run("metrics query before excludes points at or after it", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		base := time.Now().Add(-30 * time.Minute)
		_ = s.WriteMetrics(ctx, []model.Metric{
			{Name: "m", Value: 1, Timestamp: base},
			{Name: "m", Value: 2, Timestamp: base.Add(time.Minute)},
			{Name: "m", Value: 3, Timestamp: base.Add(2 * time.Minute)},
		})

		got, err := s.QueryMetrics(ctx, MetricQuery{Name: "m", Before: base.Add(time.Minute)})
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 1 || got[0].Value != 1 {
			t.Fatalf("QueryMetrics with Before = %+v, want only the point before it", got)
		}
	})

	t.Run("metrics query since, before and limit compose as a window with a cap", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		base := time.Now().Add(-30 * time.Minute)
		points := make([]model.Metric, 0, 10)
		for i := 0; i < 10; i++ {
			points = append(points, model.Metric{Name: "m", Value: float64(i), Timestamp: base.Add(time.Duration(i) * time.Minute)})
		}
		_ = s.WriteMetrics(ctx, points)

		// Window [2, 8): values 2..7; the newest two of those are 6 and 7.
		got, err := s.QueryMetrics(ctx, MetricQuery{
			Name:   "m",
			Since:  base.Add(2 * time.Minute),
			Before: base.Add(8 * time.Minute),
			Limit:  2,
		})
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 2 || got[0].Value != 6 || got[1].Value != 7 {
			t.Fatalf("QueryMetrics with Since+Before+Limit = %+v, want values 6, 7", got)
		}
	})

	t.Run("spans query limit returns the newest N, oldest-first", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		base := time.Now().Add(-30 * time.Minute)
		spans := make([]model.Span, 0, 5)
		for i := 0; i < 5; i++ {
			spans = append(spans, model.Span{TraceID: "t", SpanID: string(rune('a' + i)), Service: "svc", Name: "op", Start: base.Add(time.Duration(i) * time.Minute), Duration: time.Second})
		}
		_ = s.WriteSpans(ctx, spans)

		got, err := s.QuerySpans(ctx, SpanQuery{Service: "svc", Limit: 2, Before: base.Add(4 * time.Minute)})
		if err != nil {
			t.Fatalf("QuerySpans: %v", err)
		}
		if len(got) != 2 || got[0].SpanID != "c" || got[1].SpanID != "d" {
			t.Fatalf("QuerySpans with Limit 2 and Before = %+v, want spans c, d", got)
		}
	})

	t.Run("logs query limit returns the newest N, oldest-first", func(t *testing.T) {
		s := newStore(t)
		ctx := context.Background()
		base := time.Now().Add(-30 * time.Minute)
		entries := make([]model.LogEntry, 0, 5)
		for i := 0; i < 5; i++ {
			entries = append(entries, model.LogEntry{Timestamp: base.Add(time.Duration(i) * time.Minute), Severity: model.LogSeverityInfo, Source: "a", Message: string(rune('a' + i))})
		}
		_ = s.WriteLogs(ctx, entries)

		got, err := s.QueryLogs(ctx, LogQuery{Limit: 2})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if len(got) != 2 || got[0].Message != "d" || got[1].Message != "e" {
			t.Fatalf("QueryLogs with Limit 2 = %+v, want entries d, e", got)
		}
		got, err = s.QueryLogs(ctx, LogQuery{Source: "a", Before: base.Add(time.Minute), Limit: 5})
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if len(got) != 1 || got[0].Message != "a" {
			t.Fatalf("QueryLogs with Before = %+v, want only entry a", got)
		}
	})
}
