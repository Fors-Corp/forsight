package collector

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

// countingCollector reports how many times it was called, and can be made to
// always fail (to prove a failing collector doesn't block the others).
type countingCollector struct {
	name string
	fail bool

	mu    sync.Mutex
	calls int
}

func (c *countingCollector) Name() string { return c.name }

func (c *countingCollector) Collect(_ context.Context) ([]model.Metric, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if c.fail {
		return nil, errors.New("boom")
	}
	return []model.Metric{{Name: c.name, Value: 1, Timestamp: time.Now()}}, nil
}

func (c *countingCollector) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

type recordingSink struct {
	mu      sync.Mutex
	written []model.Metric
}

func (s *recordingSink) WriteMetrics(_ context.Context, metrics []model.Metric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, metrics...)
	return nil
}

func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.written)
}

func TestRegistry_CollectsImmediatelyOnFirstTick(t *testing.T) {
	sink := &recordingSink{}
	good := &countingCollector{name: "good"}
	// A long interval — if the registry waited for a full tick before the
	// first collection, this test's short deadline would see zero calls.
	registry := NewRegistry(sink, time.Hour, slog.New(slog.DiscardHandler), good)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	registry.Run(ctx)

	if good.callCount() < 1 {
		t.Fatal("collector was never called before the registry's context was cancelled")
	}
	if sink.count() < 1 {
		t.Fatal("sink never received the collector's metrics")
	}
}

func TestRegistry_FailingCollectorDoesNotBlockOthers(t *testing.T) {
	sink := &recordingSink{}
	failing := &countingCollector{name: "failing", fail: true}
	good := &countingCollector{name: "good"}
	registry := NewRegistry(sink, 20*time.Millisecond, slog.New(slog.DiscardHandler), failing, good)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	registry.Run(ctx)

	if failing.callCount() < 1 {
		t.Error("failing collector was never called")
	}
	if good.callCount() < 2 {
		t.Errorf("good collector was called %d times, want at least 2 (proves it kept ticking independently)", good.callCount())
	}
	// Only the good collector's metrics ever reach the sink.
	if sink.count() != good.callCount() {
		t.Errorf("sink has %d metrics, want exactly one per good-collector call (%d)", sink.count(), good.callCount())
	}
}

// TestRegistry_StatusesReportsCollectorsBeforeAnyTick proves Statuses is
// populated from NewRegistry's collector list, not lazily on first tick —
// /readyz (see internal/api's handleReadyz) must be able to name every
// registered collector even if it's called before Run has ticked once.
func TestRegistry_StatusesReportsCollectorsBeforeAnyTick(t *testing.T) {
	registry := NewRegistry(&recordingSink{}, time.Hour, slog.New(slog.DiscardHandler),
		&countingCollector{name: "host"}, &countingCollector{name: "docker"})

	statuses := registry.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("Statuses() returned %d entries, want 2", len(statuses))
	}
	if statuses[0].Name != "host" || statuses[1].Name != "docker" {
		t.Errorf("Statuses() = %+v, want registration order (host, docker)", statuses)
	}
	for _, s := range statuses {
		if s.LastError != "" {
			t.Errorf("%s: LastError = %q before any tick, want empty", s.Name, s.LastError)
		}
		if !s.LastRunAt.IsZero() {
			t.Errorf("%s: LastRunAt = %v before any tick, want zero", s.Name, s.LastRunAt)
		}
	}
}

// TestRegistry_StatusesReportsCollectorsLastError is the regression test for
// what /readyz actually depends on: a collector that fails keeps its error
// visible in Statuses (rather than the map entry never being set, or a later
// success on another collector clobbering it), and a collector that
// succeeds reports no error.
func TestRegistry_StatusesReportsCollectorsLastError(t *testing.T) {
	failing := &countingCollector{name: "failing", fail: true}
	good := &countingCollector{name: "good"}
	registry := NewRegistry(&recordingSink{}, 20*time.Millisecond, slog.New(slog.DiscardHandler), failing, good)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	registry.Run(ctx)

	statuses := registry.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("Statuses() returned %d entries, want 2", len(statuses))
	}
	if statuses[0].LastError == "" {
		t.Error("failing collector's LastError is empty, want the Collect error")
	}
	if statuses[0].LastRunAt.IsZero() {
		t.Error("failing collector's LastRunAt is zero after ticking, want a real time")
	}
	if statuses[1].LastError != "" {
		t.Errorf("good collector's LastError = %q, want empty", statuses[1].LastError)
	}
	if statuses[1].LastRunAt.IsZero() {
		t.Error("good collector's LastRunAt is zero after ticking, want a real time")
	}
}
