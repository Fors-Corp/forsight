package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// TestMemoryStore_Conformance runs the shared Store query-semantics suite
// (also run by BadgerStore in badger_test.go) against MemoryStore.
func TestMemoryStore_Conformance(t *testing.T) {
	runStoreConformanceTests(t, func(t *testing.T) Store {
		return NewMemoryStore(time.Hour)
	})
}

func TestMemoryStore_WriteThenQuery(t *testing.T) {
	s := NewMemoryStore(time.Hour)
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
}

func TestMemoryStore_QueryFiltersByLabel(t *testing.T) {
	s := NewMemoryStore(time.Hour)
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
}

func TestMemoryStore_PrunesOldMetrics(t *testing.T) {
	s := NewMemoryStore(time.Minute)
	fixedNow := time.Now()
	s.now = func() time.Time { return fixedNow }
	ctx := context.Background()

	_ = s.WriteMetrics(ctx, []model.Metric{
		{Name: "old", Value: 1, Timestamp: fixedNow.Add(-2 * time.Minute)},
		{Name: "recent", Value: 2, Timestamp: fixedNow.Add(-10 * time.Second)},
	})

	got, err := s.QueryMetrics(ctx, MetricQuery{})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(got) != 1 || got[0].Name != "recent" {
		t.Fatalf("QueryMetrics after prune = %+v, want only the recent metric", got)
	}
}

func TestMemoryStore_QuerySinceExcludesOlderPoints(t *testing.T) {
	s := NewMemoryStore(time.Hour)
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
}

func TestMemoryStore_Spans(t *testing.T) {
	s := NewMemoryStore(time.Hour)
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
}

func TestMemoryStore_ElementCapBoundsGrowth(t *testing.T) {
	// Age pruning alone cannot bound this store, because ingested timestamps
	// come off the wire: a writer choosing a far-future timestamp is never
	// pruned. The element cap is what makes memory a function of configuration
	// rather than of what somebody sends.
	s := NewMemoryStore(time.Hour)
	s.SetMaxElements(100)

	future := time.Now().Add(24 * time.Hour)
	for i := 0; i < 10; i++ {
		batch := make([]model.Metric, 50)
		for j := range batch {
			batch[j] = model.Metric{Name: "m", Value: float64(i*50 + j), Timestamp: future}
		}
		if err := s.WriteMetrics(context.Background(), batch); err != nil {
			t.Fatalf("WriteMetrics: %v", err)
		}
	}

	got, err := s.QueryMetrics(context.Background(), MetricQuery{Name: "m"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("stored %d metrics, want the cap of 100 — 500 future-dated points were written", len(got))
	}
	// The newest are the ones kept: the last write was values 450..499.
	if got[len(got)-1].Value != 499 {
		t.Errorf("newest retained value = %v, want 499 (the cap must drop the oldest, not the newest)", got[len(got)-1].Value)
	}
}

// TestMemoryStore_NoExpiredElementsKeepsAll exercises pruneMetricsLocked's
// fast path: when the oldest retained element is still within the
// retention window, the O(n) age scan is skipped entirely (an optimization
// so a write doesn't rescan up to maxElements entries when nothing has
// expired). That skip must not lose data relative to running the full
// scan — every unexpired point must still come back.
func TestMemoryStore_NoExpiredElementsKeepsAll(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	ctx := context.Background()
	now := time.Now()

	batch := make([]model.Metric, 50)
	for i := range batch {
		batch[i] = model.Metric{Name: "m", Value: float64(i), Timestamp: now}
	}
	if err := s.WriteMetrics(ctx, batch); err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}

	got, err := s.QueryMetrics(ctx, MetricQuery{Name: "m"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("got %d metrics, want all 50 kept (fast path must not drop unexpired data)", len(got))
	}
}

func TestMemoryStore_SetMaxElementsRejectsUnbounded(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	s.SetMaxElements(0) // must fall back to the default, never mean "no limit"
	batch := make([]model.Metric, 10)
	for i := range batch {
		batch[i] = model.Metric{Name: "m", Timestamp: time.Now()}
	}
	if err := s.WriteMetrics(context.Background(), batch); err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}
	got, _ := s.QueryMetrics(context.Background(), MetricQuery{Name: "m"})
	if len(got) != 10 {
		t.Fatalf("got %d, want 10 — a zero cap must restore the default, not discard data", len(got))
	}
}

func TestMemoryStore_SpanElementCap(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	s.SetMaxElements(20)
	future := time.Now().Add(24 * time.Hour)
	spans := make([]model.Span, 100)
	for i := range spans {
		spans[i] = model.Span{TraceID: "t", SpanID: "s", Service: "svc", Start: future}
	}
	if err := s.WriteSpans(context.Background(), spans); err != nil {
		t.Fatalf("WriteSpans: %v", err)
	}
	got, err := s.QuerySpans(context.Background(), SpanQuery{Service: "svc"})
	if err != nil {
		t.Fatalf("QuerySpans: %v", err)
	}
	if len(got) != 20 {
		t.Fatalf("stored %d spans, want the cap of 20", len(got))
	}
}

func TestMemoryStore_Logs(t *testing.T) {
	s := NewMemoryStore(time.Hour)
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
}

func TestMemoryStore_PrunesOldLogs(t *testing.T) {
	s := NewMemoryStore(time.Minute)
	fixedNow := time.Now()
	s.now = func() time.Time { return fixedNow }
	ctx := context.Background()

	_ = s.WriteLogs(ctx, []model.LogEntry{
		{Timestamp: fixedNow.Add(-2 * time.Minute), Severity: model.LogSeverityInfo, Source: "a", Message: "old"},
		{Timestamp: fixedNow.Add(-10 * time.Second), Severity: model.LogSeverityInfo, Source: "a", Message: "recent"},
	})

	got, err := s.QueryLogs(ctx, LogQuery{})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(got) != 1 || got[0].Message != "recent" {
		t.Fatalf("QueryLogs after prune = %+v, want only the recent entry", got)
	}
}

func TestMemoryStore_QueryLogsSinceExcludesOlder(t *testing.T) {
	s := NewMemoryStore(time.Hour)
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
}

// TestMemoryStore_QueryDoesNotSizeResultToWholeStore is a regression test
// for an allocation bug: QueryMetrics/QuerySpans/QueryLogs used to build
// their result with make(..., 0, len(store)) before filtering and applying
// Limit, so a query naming a name/service/source nothing matches, with a
// small Limit, still allocated a buffer sized to the WHOLE store —
// `?name=nonexistent&limit=1` against a store near its element cap
// allocates on the order of the store's full memory footprint just to
// answer "no results, take the newest one anyway". len() was always small
// and never caught this; only cap() — the backing array's allocated size —
// gives the bug away, so that is what this test asserts.
func TestMemoryStore_QueryDoesNotSizeResultToWholeStore(t *testing.T) {
	const storeSize = 50_000
	const smallLimit = 1
	const maxAllowedCap = 64 // must stay a small constant, never O(storeSize)

	s := NewMemoryStore(time.Hour)
	s.SetMaxElements(storeSize + 10)
	ctx := context.Background()
	now := time.Now()

	metrics := make([]model.Metric, storeSize)
	spans := make([]model.Span, storeSize)
	logs := make([]model.LogEntry, storeSize)
	for i := range metrics {
		metrics[i] = model.Metric{Name: "present", Value: float64(i), Timestamp: now}
		spans[i] = model.Span{TraceID: "t", SpanID: "s", Service: "present", Start: now}
		logs[i] = model.LogEntry{Timestamp: now, Source: "present", Message: "m"}
	}
	if err := s.WriteMetrics(ctx, metrics); err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}
	if err := s.WriteSpans(ctx, spans); err != nil {
		t.Fatalf("WriteSpans: %v", err)
	}
	if err := s.WriteLogs(ctx, logs); err != nil {
		t.Fatalf("WriteLogs: %v", err)
	}

	gotMetrics, err := s.QueryMetrics(ctx, MetricQuery{Name: "nonexistent", Limit: smallLimit})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if cap(gotMetrics) > maxAllowedCap {
		t.Errorf("QueryMetrics(name=nonexistent, limit=1) result cap = %d, want <= %d (a %d-element store must not size the buffer to itself)", cap(gotMetrics), maxAllowedCap, storeSize)
	}

	gotSpans, err := s.QuerySpans(ctx, SpanQuery{Service: "nonexistent", Limit: smallLimit})
	if err != nil {
		t.Fatalf("QuerySpans: %v", err)
	}
	if cap(gotSpans) > maxAllowedCap {
		t.Errorf("QuerySpans(service=nonexistent, limit=1) result cap = %d, want <= %d", cap(gotSpans), maxAllowedCap)
	}

	gotLogs, err := s.QueryLogs(ctx, LogQuery{Source: "nonexistent", Limit: smallLimit})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if cap(gotLogs) > maxAllowedCap {
		t.Errorf("QueryLogs(source=nonexistent, limit=1) result cap = %d, want <= %d", cap(gotLogs), maxAllowedCap)
	}

	// The single-name PerName case must take the same bounded path as a
	// plain Limit (the fold borrowed from BadgerStore's QueryMetrics),
	// rather than newestPerName's whole-store grouping walk.
	gotPerName, err := s.QueryMetrics(ctx, MetricQuery{Name: "nonexistent", PerName: smallLimit})
	if err != nil {
		t.Fatalf("QueryMetrics per_name: %v", err)
	}
	if cap(gotPerName) > maxAllowedCap {
		t.Errorf("QueryMetrics(name=nonexistent, per_name=1) result cap = %d, want <= %d", cap(gotPerName), maxAllowedCap)
	}
}

func TestMemoryStore_LogElementCap(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	s.SetMaxElements(20)
	future := time.Now().Add(24 * time.Hour)
	logs := make([]model.LogEntry, 100)
	for i := range logs {
		logs[i] = model.LogEntry{
			Timestamp: future,
			Severity:  model.LogSeverityInfo,
			Source:    "svc",
			Message:   "m",
		}
	}
	if err := s.WriteLogs(context.Background(), logs); err != nil {
		t.Fatalf("WriteLogs: %v", err)
	}
	got, err := s.QueryLogs(context.Background(), LogQuery{Source: "svc"})
	if err != nil {
		t.Fatalf("QueryLogs: %v", err)
	}
	if len(got) != 20 {
		t.Fatalf("stored %d logs, want the cap of 20", len(got))
	}
}

// TestMemoryStore_ConcurrentWriteAndQueryAcrossCollections drives one writer
// and one reader per collection (metrics, spans, logs) plus a concurrent
// SetMaxElements — the one method that touches all three — at once, the
// same shape production traffic takes: ingest writes while the dashboard's
// pollers read, on all three collections simultaneously. Each
// metrics/spans/logs pair guards a disjoint field with its own RWMutex,
// and SetMaxElements is the only method that ever needs more than one, so
// this is also a check that its fixed three-lock acquisition order is
// enough on its own. Run with -race: a shared single mutex would also pass
// this test functionally, so what -race actually verifies is that the
// three-lock split introduced no race on maxElements or on a field crossing
// its own lock.
func TestMemoryStore_ConcurrentWriteAndQueryAcrossCollections(t *testing.T) {
	s := NewMemoryStore(time.Hour)
	ctx := context.Background()
	now := time.Now()

	const iterations = 200
	var wg sync.WaitGroup

	writeRead := func(write func(), read func()) {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			write()
			read()
		}
	}

	wg.Add(3)
	go writeRead(
		func() {
			_ = s.WriteMetrics(ctx, []model.Metric{{Name: "cpu", Value: 1, Timestamp: now}})
		},
		func() { _, _ = s.QueryMetrics(ctx, MetricQuery{Name: "cpu"}) },
	)
	go writeRead(
		func() {
			_ = s.WriteSpans(ctx, []model.Span{{TraceID: "t", SpanID: "s", Name: "op", Service: "svc", Start: now}})
		},
		func() { _, _ = s.QuerySpans(ctx, SpanQuery{Service: "svc"}) },
	)
	go writeRead(
		func() {
			_ = s.WriteLogs(ctx, []model.LogEntry{{Timestamp: now, Severity: model.LogSeverityInfo, Source: "svc", Message: "m"}})
		},
		func() { _, _ = s.QueryLogs(ctx, LogQuery{Source: "svc"}) },
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations/10; i++ {
			s.SetMaxElements(1000)
		}
	}()

	wg.Wait()
}
