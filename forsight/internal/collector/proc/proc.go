// Package proc collects per-process CPU, memory, and open file descriptors
// so a default `forsight run` sees what is actually running on the box, not
// just host-level totals.
package proc

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

const defaultLimit = 40

// sample is one process as of this tick.
type sample struct {
	PID        int32
	Name       string
	CPUPercent float64
	RSS        uint64
	FDCount    int32
}

// lister is injectable so tests do not need a live process table.
type lister func(ctx context.Context) ([]sample, error)

// Collector emits process.cpu.percent, process.memory.rss_bytes, and
// process.fd.count for the busiest processes (by CPU, then RSS). The first
// tick for a pid has no CPU percent — gopsutil needs two samples to diff,
// the same shape as the Docker collector.
type Collector struct {
	list  lister
	limit int

	mu   sync.Mutex
	seen map[int32]struct{}
}

// New watches the live process table.
func New() *Collector {
	return &Collector{list: listLive, limit: defaultLimit, seen: map[int32]struct{}{}}
}

func (c *Collector) Name() string { return "proc" }

func (c *Collector) Collect(ctx context.Context) ([]model.Metric, error) {
	samples, err := c.list(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].CPUPercent != samples[j].CPUPercent {
			return samples[i].CPUPercent > samples[j].CPUPercent
		}
		return samples[i].RSS > samples[j].RSS
	})
	if c.limit > 0 && len(samples) > c.limit {
		samples = samples[:c.limit]
	}

	now := time.Now()
	c.mu.Lock()
	prev := c.seen
	next := make(map[int32]struct{}, len(samples))
	c.mu.Unlock()

	var metrics []model.Metric
	for _, s := range samples {
		next[s.PID] = struct{}{}
		labels := map[string]string{
			"pid":  strconv.Itoa(int(s.PID)),
			"name": s.Name,
		}
		metrics = append(metrics, model.Metric{
			Name: "process.memory.rss_bytes", Value: float64(s.RSS), Timestamp: now, Labels: labels,
		})
		metrics = append(metrics, model.Metric{
			Name: "process.fd.count", Value: float64(s.FDCount), Timestamp: now, Labels: labels,
		})
		if _, ok := prev[s.PID]; ok {
			metrics = append(metrics, model.Metric{
				Name: "process.cpu.percent", Value: s.CPUPercent, Timestamp: now, Labels: labels,
			})
		}
	}

	c.mu.Lock()
	c.seen = next
	c.mu.Unlock()
	return metrics, nil
}

func listLive(ctx context.Context) ([]sample, error) {
	pids, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]sample, 0, len(pids))
	for _, p := range pids {
		name, err := p.NameWithContext(ctx)
		if err != nil || name == "" {
			continue
		}
		cpu, _ := p.CPUPercentWithContext(ctx)
		mem, err := p.MemoryInfoWithContext(ctx)
		var rss uint64
		if err == nil && mem != nil {
			rss = mem.RSS
		}
		fds, _ := p.NumFDsWithContext(ctx)
		out = append(out, sample{PID: p.Pid, Name: name, CPUPercent: cpu, RSS: rss, FDCount: fds})
	}
	return out, nil
}
