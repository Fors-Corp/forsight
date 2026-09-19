// Package collector defines the Collector interface every data source
// (host, docker, otlp, ...) implements, and a Registry that runs each one on
// its own interval and forwards results to a sink (the store).
package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

// Collector gathers metrics from one source. Implementations must be safe to
// call repeatedly and must return quickly (the registry calls Collect on a
// fixed interval; a slow or blocked collector delays only its own next tick,
// never the others — see Registry.Run).
type Collector interface {
	// Name identifies the collector in logs and the /healthz response.
	Name() string
	// Collect returns the current metrics. An error is logged and skipped —
	// it never stops the collector's future ticks (a transient Docker-socket
	// hiccup shouldn't take down host-metric collection too).
	Collect(ctx context.Context) ([]model.Metric, error)
}

// Sink receives metrics as they're collected. *store.Store satisfies this
// without collector needing to import store — the dependency points one way.
type Sink interface {
	WriteMetrics(ctx context.Context, metrics []model.Metric) error
}

// CollectorStatus is one collector's most recent outcome: whether its last
// tick (Collect, or the sink write that followed it) succeeded, and when
// that tick ran. /readyz (see internal/api's handleReadyz) reports these
// alongside the store's own health, which is what lets a missing Docker
// socket show up in the probe's response without ever failing the probe —
// see Registry.Statuses.
type CollectorStatus struct {
	Name string `json:"name"`
	// LastError is empty when the collector's last tick succeeded, or it
	// hasn't run one yet.
	LastError string `json:"lastError,omitempty"`
	// LastRunAt is the zero time until the collector's first tick completes.
	LastRunAt time.Time `json:"lastRunAt"`
}

// Registry runs a fixed set of collectors on independent tickers.
type Registry struct {
	collectors []Collector
	interval   time.Duration
	sink       Sink
	logger     *slog.Logger

	mu       sync.Mutex
	statuses map[string]CollectorStatus
}

// NewRegistry builds a Registry. interval applies to every collector; a
// per-collector interval isn't needed yet (host/docker are cheap to poll
// every few seconds) — the collectors slice is what varies, not the timing.
func NewRegistry(sink Sink, interval time.Duration, logger *slog.Logger, collectors ...Collector) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	statuses := make(map[string]CollectorStatus, len(collectors))
	for _, c := range collectors {
		statuses[c.Name()] = CollectorStatus{Name: c.Name()}
	}
	return &Registry{collectors: collectors, interval: interval, sink: sink, logger: logger, statuses: statuses}
}

// Statuses returns every collector's most recent outcome, in the same order
// the collectors were registered. Safe to call concurrently with Run — it is
// meant to be called from an HTTP handler goroutine while Run's own
// goroutines keep ticking.
func (r *Registry) Statuses() []CollectorStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CollectorStatus, 0, len(r.collectors))
	for _, c := range r.collectors {
		out = append(out, r.statuses[c.Name()])
	}
	return out
}

// setStatus records the outcome of a collector's tick for Statuses to
// report. err is nil on success.
func (r *Registry) setStatus(name string, err error) {
	st := CollectorStatus{Name: name, LastRunAt: time.Now()}
	if err != nil {
		st.LastError = err.Error()
	}
	r.mu.Lock()
	r.statuses[name] = st
	r.mu.Unlock()
}

// Run starts every collector on its own goroutine/ticker and blocks until ctx
// is cancelled. Each collector's failures are isolated (logged, not fatal).
func (r *Registry) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, c := range r.collectors {
		wg.Add(1)
		go func(c Collector) {
			defer wg.Done()
			r.runOne(ctx, c)
		}(c)
	}
	wg.Wait()
}

func (r *Registry) runOne(ctx context.Context, c Collector) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.collectOnce(ctx, c) // first tick immediately, not after the first interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.collectOnce(ctx, c)
		}
	}
}

func (r *Registry) collectOnce(ctx context.Context, c Collector) {
	metrics, err := c.Collect(ctx)
	if err != nil {
		r.logger.Warn("collector failed", "collector", c.Name(), "error", err)
		r.setStatus(c.Name(), err)
		return
	}
	if len(metrics) == 0 {
		r.setStatus(c.Name(), nil)
		return
	}
	if err := r.sink.WriteMetrics(ctx, metrics); err != nil {
		r.logger.Warn("failed to write collected metrics", "collector", c.Name(), "error", err)
		r.setStatus(c.Name(), err)
		return
	}
	r.setStatus(c.Name(), nil)
}
