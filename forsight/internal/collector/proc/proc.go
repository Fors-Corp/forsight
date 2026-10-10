// Package proc collects per-process CPU, memory, and open file descriptors
// so a default `forsight run` sees what is actually running on the box, not
// just host-level totals.
package proc

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

const defaultLimit = 40

// selfFDWarnThreshold is how many open descriptors forsight may hold before
// it warns about itself. A healthy agent holds a few hundred (badger's
// tables, listeners, the collectors' sockets); the 2026-10-10 probe leak
// reached 16k and exhausted the host's ephemeral ports (16,384 on macOS).
// Crossing this leaves most of that headroom still free.
const selfFDWarnThreshold = 4096

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
// process.fd.count for the busiest processes (by CPU, then RSS), plus
// forsight's own process whether or not it ranks. The first tick for a pid
// has no CPU percent — gopsutil needs two samples to diff, the same shape as
// the Docker collector.
type Collector struct {
	list  lister
	limit int
	// self is forsight's own pid, reported past the limit and watched for
	// a descriptor leak; 0 means none (tests that do not exercise it).
	self   int32
	logger *slog.Logger

	mu       sync.Mutex
	seen     map[int32]struct{}
	fdWarned bool
}

// New watches the live process table.
func New() *Collector {
	return &Collector{list: listLive, limit: defaultLimit, self: int32(os.Getpid()), seen: map[int32]struct{}{}}
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
	self, selfFound := c.findSelf(samples)
	if c.limit > 0 && len(samples) > c.limit {
		kept := samples[:c.limit:c.limit]
		if selfFound && !containsPID(kept, c.self) {
			kept = append(kept, self)
		}
		samples = kept
	}
	if selfFound {
		c.checkOwnFDs(self.FDCount)
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

func (c *Collector) findSelf(samples []sample) (sample, bool) {
	if c.self == 0 {
		return sample{}, false
	}
	for _, s := range samples {
		if s.PID == c.self {
			return s, true
		}
	}
	return sample{}, false
}

func containsPID(samples []sample, pid int32) bool {
	for _, s := range samples {
		if s.PID == pid {
			return true
		}
	}
	return false
}

// checkOwnFDs warns once when forsight's own descriptor count crosses
// selfFDWarnThreshold, and again only after it has dropped back below — a
// leak is one incident, not a line every tick.
func (c *Collector) checkOwnFDs(fds int32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fds <= selfFDWarnThreshold {
		c.fdWarned = false
		return
	}
	if c.fdWarned {
		return
	}
	c.fdWarned = true
	logger := c.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("forsight holds an unusual number of open file descriptors; a connection or file leak is likely",
		"open_fds", fds, "threshold", selfFDWarnThreshold)
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
