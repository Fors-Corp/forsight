package store

import (
	"context"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

func newTestBadgerStore(t *testing.T, retention time.Duration) *BadgerStore {
	t.Helper()
	s, err := NewBadgerStore(t.TempDir(), retention)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

// TestBadgerStore_Conformance runs the shared Store query-semantics suite
// (also run by MemoryStore in memory_test.go) against BadgerStore, so the
// on-disk implementation can't silently drift from what a query is supposed
// to match.
func TestBadgerStore_Conformance(t *testing.T) {
	runStoreConformanceTests(t, func(t *testing.T) Store {
		return newTestBadgerStore(t, time.Hour)
	})
}

// TestBadgerStore_NameScopedQueryDoesNotLeakAcrossNames is a regression test
// for the key-encoding design explained in badger.go's doc comment: names
// are length-prefixed, not delimiter-separated, specifically so a name that
// is a byte-prefix of another ("cpu" / "cpu2") or that itself contains the
// byte a delimiter scheme would have used (a literal NUL) can never leak
// into a differently-scoped query.
func TestBadgerStore_NameScopedQueryDoesNotLeakAcrossNames(t *testing.T) {
	s := newTestBadgerStore(t, time.Hour)
	ctx := context.Background()
	now := time.Now()

	err := s.WriteMetrics(ctx, []model.Metric{
		{Name: "cpu", Value: 1, Timestamp: now},
		{Name: "cpu2", Value: 2, Timestamp: now},
		{Name: "cp\x00u", Value: 3, Timestamp: now},
	})
	if err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}

	got, err := s.QueryMetrics(ctx, MetricQuery{Name: "cpu"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(got) != 1 || got[0].Value != 1 {
		t.Fatalf(`QueryMetrics(Name: "cpu") = %+v, want exactly the one cpu metric, no leakage from "cpu2" or the NUL-byte name`, got)
	}

	got, err = s.QueryMetrics(ctx, MetricQuery{Name: "cp\x00u"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(got) != 1 || got[0].Value != 3 {
		t.Fatalf("QueryMetrics with an embedded-NUL name = %+v, want exactly that one metric", got)
	}

	got, err = s.QueryMetrics(ctx, MetricQuery{Name: "cpu2"})
	if err != nil {
		t.Fatalf("QueryMetrics: %v", err)
	}
	if len(got) != 1 || got[0].Value != 2 {
		t.Fatalf(`QueryMetrics(Name: "cpu2") = %+v, want exactly the one cpu2 metric`, got)
	}
}

// TestBadgerStore_SetsExpiryFromRetention checks the write path actually
// wires the store's retention into each entry's Badger TTL, by reading the
// TTL Badger itself recorded (Item.ExpiresAt) rather than waiting for real
// expiry — fast and precise, complementary to the slower end-to-end
// TestBadgerStore_RetentionExpiresEntries below.
func TestBadgerStore_SetsExpiryFromRetention(t *testing.T) {
	const retention = 5 * time.Minute
	s := newTestBadgerStore(t, retention)

	before := time.Now()
	if err := s.WriteMetrics(context.Background(), []model.Metric{
		{Name: "m", Value: 1, Timestamp: before},
	}); err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}
	after := time.Now()

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		it.Rewind()
		if !it.Valid() {
			t.Fatal("expected one key in the database, found none")
		}
		expiresAt := time.Unix(int64(it.Item().ExpiresAt()), 0)
		// Badger's TTL is second-granular (ExpiresAt is a Unix-seconds
		// value), so allow a couple of seconds of slack around
		// [write time, write time] + retention.
		wantMin := before.Add(retention).Add(-2 * time.Second)
		wantMax := after.Add(retention).Add(2 * time.Second)
		if expiresAt.Before(wantMin) || expiresAt.After(wantMax) {
			t.Errorf("entry ExpiresAt = %v, want within [%v, %v] (write time + retention)", expiresAt, wantMin, wantMax)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("iterating badger db: %v", err)
	}
}

// TestBadgerStore_RetentionExpiresEntries proves retention-based pruning
// end-to-end through the public Store API: once the configured retention
// has actually elapsed, expired metrics/spans/logs stop coming back from a
// query, with no sweep loop of this store's own involved — this is Badger's
// native TTL doing the work (see badger.go's doc comment).
func TestBadgerStore_RetentionExpiresEntries(t *testing.T) {
	// Badger's TTL is second-granular: Entry.WithTTL stores ExpiresAt as
	// time.Now().Add(dur).Unix(), truncating to whole seconds. For an
	// integer-second retention r, that makes ExpiresAt exactly (the write's
	// whole second) + r regardless of where in the second the write lands —
	// so real time-to-expiry from the write instant is somewhere in
	// (r-1, r] seconds, never less. A sub-second retention doesn't have
	// that guarantee (it can round down to "already expired"), which is
	// why this uses a whole 2 seconds rather than something smaller: the
	// immediate "still there" check below is guaranteed at least 1s of
	// headroom, and 2.5s of sleep is guaranteed past the ≤2s worst case.
	const retention = 2 * time.Second
	s := newTestBadgerStore(t, retention)
	ctx := context.Background()
	now := time.Now()

	if err := s.WriteMetrics(ctx, []model.Metric{{Name: "m", Value: 1, Timestamp: now}}); err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}
	if err := s.WriteSpans(ctx, []model.Span{{TraceID: "t", SpanID: "s", Service: "svc", Start: now}}); err != nil {
		t.Fatalf("WriteSpans: %v", err)
	}
	if err := s.WriteLogs(ctx, []model.LogEntry{
		{Timestamp: now, Severity: model.LogSeverityInfo, Source: "svc", Message: "hi"},
	}); err != nil {
		t.Fatalf("WriteLogs: %v", err)
	}

	// Sanity check: freshly written data is queryable before it expires.
	if got, err := s.QueryMetrics(ctx, MetricQuery{}); err != nil || len(got) != 1 {
		t.Fatalf("QueryMetrics before expiry = %v, err=%v; want exactly 1 metric", got, err)
	}

	time.Sleep(2500 * time.Millisecond)

	if got, err := s.QueryMetrics(ctx, MetricQuery{}); err != nil || len(got) != 0 {
		t.Errorf("QueryMetrics after retention lapsed = %v, err=%v; want no metrics", got, err)
	}
	if got, err := s.QuerySpans(ctx, SpanQuery{}); err != nil || len(got) != 0 {
		t.Errorf("QuerySpans after retention lapsed = %v, err=%v; want no spans", got, err)
	}
	if got, err := s.QueryLogs(ctx, LogQuery{}); err != nil || len(got) != 0 {
		t.Errorf("QueryLogs after retention lapsed = %v, err=%v; want no logs", got, err)
	}
}

// TestBadgerStore_PersistsAcrossReopen is the unit-test-level proof of the
// entire point of this store: data written before Close is still there
// after re-opening the same directory in a fresh BadgerStore — i.e. it
// survives a process restart, which MemoryStore by definition cannot.
func TestBadgerStore_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	now := time.Now()

	s1, err := NewBadgerStore(dir, time.Hour)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	if err := s1.WriteMetrics(ctx, []model.Metric{
		{Name: "durable", Value: 99, Timestamp: now, Labels: map[string]string{"host": "a"}},
	}); err != nil {
		t.Fatalf("WriteMetrics: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := NewBadgerStore(dir, time.Hour)
	if err != nil {
		t.Fatalf("re-opening NewBadgerStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s2.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	got, err := s2.QueryMetrics(ctx, MetricQuery{Name: "durable"})
	if err != nil {
		t.Fatalf("QueryMetrics after reopen: %v", err)
	}
	if len(got) != 1 || got[0].Value != 99 || got[0].Labels["host"] != "a" {
		t.Fatalf("QueryMetrics after reopen = %+v, want the metric written before Close", got)
	}
}

// TestBadgerStore_WriteMetricsEmptyBatchIsNoop guards WriteMetrics/
// WriteSpans/WriteLogs' early return for an empty slice — a WriteBatch that
// is opened and immediately Flushed with nothing set is a wasted
// transaction, not a correctness bug, but the early return is easy to
// accidentally drop in a refactor, and there's no other test that would
// catch its absence.
func TestBadgerStore_WriteEmptyBatchIsNoop(t *testing.T) {
	s := newTestBadgerStore(t, time.Hour)
	ctx := context.Background()

	if err := s.WriteMetrics(ctx, nil); err != nil {
		t.Errorf("WriteMetrics(nil): %v", err)
	}
	if err := s.WriteSpans(ctx, nil); err != nil {
		t.Errorf("WriteSpans(nil): %v", err)
	}
	if err := s.WriteLogs(ctx, nil); err != nil {
		t.Errorf("WriteLogs(nil): %v", err)
	}
}

// TestBadgerStore_PingFailsAfterClose is BadgerStore's half of Ping's
// contract: the conformance suite (store_conformance_test.go) already
// proves Ping succeeds on a healthy store; a closed database is the one
// failure mode BadgerStore actually has, and it's what /readyz (see
// internal/api's handleReadyz) exists to catch.
func TestBadgerStore_PingFailsAfterClose(t *testing.T) {
	s, err := NewBadgerStore(t.TempDir(), time.Hour)
	if err != nil {
		t.Fatalf("NewBadgerStore: %v", err)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("Ping before Close: %v, want nil", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Ping(context.Background()); err == nil {
		t.Error("Ping after Close = nil, want an error")
	}
}
