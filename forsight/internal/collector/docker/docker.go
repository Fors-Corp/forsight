// Package docker collects per-container CPU/memory stats from the local
// Docker (or Docker-API-compatible) daemon. Availability is checked once, at
// construction (New pings the daemon) — a laptop with no Docker running
// simply doesn't get this collector registered at all, rather than this
// package silently failing on every tick forever.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

// cpuSample is the pair of counters CPU-percent math needs a delta between.
type cpuSample struct {
	totalUsage  uint64
	systemUsage uint64
	onlineCPUs  uint32
}

// Collector gathers per-container resource stats via the Docker API.
//
// It deliberately does NOT ask Docker for ContainerStatsOptions.
// IncludePreviousSample: that option makes the daemon block for a full
// second *per container* to gather two samples itself. With even a handful
// of containers running, that serializes into several seconds per tick —
// long enough to blow through a typical collection interval. Instead, this
// collector takes one instant sample per tick and diffs it against the
// sample it saved from the *previous* tick, which is exactly what Docker's
// own two-sample approach computes, just spread across ticks instead of
// paid for synchronously on every one.
type Collector struct {
	cli *client.Client

	mu   sync.Mutex
	prev map[string]cpuSample // by container ID, rebuilt each tick
}

// New connects to the local Docker daemon (respecting DOCKER_HOST etc., see
// client.FromEnv) and pings it. A non-nil error here means "no Docker
// available" — the caller should skip registering this collector rather than
// retry-collecting against a socket that isn't there.
func New(ctx context.Context) (*Collector, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker collector: building client: %w", err)
	}
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		return nil, fmt.Errorf("docker collector: daemon unreachable: %w", err)
	}
	return &Collector{cli: cli, prev: map[string]cpuSample{}}, nil
}

func (c *Collector) Name() string { return "docker" }

func (c *Collector) Collect(ctx context.Context) ([]model.Metric, error) {
	list, err := c.cli.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker collector: listing containers: %w", err)
	}

	now := time.Now()
	nextPrev := make(map[string]cpuSample, len(list.Items))

	c.mu.Lock()
	prev := c.prev
	c.mu.Unlock()

	var metrics []model.Metric
	for _, item := range list.Items {
		stats, err := c.containerStats(ctx, item.ID)
		if err != nil {
			// One unreachable container (e.g. it stopped between List and
			// Stats) shouldn't drop stats for every other container.
			continue
		}

		current := cpuSample{
			totalUsage:  stats.CPUStats.CPUUsage.TotalUsage,
			systemUsage: stats.CPUStats.SystemUsage,
			onlineCPUs:  onlineCPUs(stats),
		}
		nextPrev[item.ID] = current

		labels := map[string]string{
			"container_id":   shortID(item.ID),
			"container_name": containerName(item),
			"image":          item.Image,
		}
		metrics = append(metrics,
			model.Metric{Name: "docker.memory.percent", Value: memoryPercent(stats), Timestamp: now, Labels: labels},
			model.Metric{Name: "docker.memory.used_bytes", Value: float64(stats.MemoryStats.Usage), Timestamp: now, Labels: labels},
		)
		// A container seen for the first time has no prior sample to diff
		// against — skip cpu.percent for just this one tick rather than
		// reporting a meaningless 0 or a huge since-container-start number.
		if last, ok := prev[item.ID]; ok {
			metrics = append(metrics, model.Metric{
				Name: "docker.cpu.percent", Value: cpuPercent(current, last), Timestamp: now, Labels: labels,
			})
		}
	}

	c.mu.Lock()
	c.prev = nextPrev
	c.mu.Unlock()

	return metrics, nil
}

func (c *Collector) containerStats(ctx context.Context, id string) (container.StatsResponse, error) {
	result, err := c.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{})
	if err != nil {
		return container.StatsResponse{}, err
	}
	defer func() { _ = result.Body.Close() }()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		return container.StatsResponse{}, err
	}
	var stats container.StatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return container.StatsResponse{}, err
	}
	return stats, nil
}

// cpuPercent is the standard Docker CLI formula: the container's share of
// total CPU time consumed between two samples, scaled by online CPUs.
func cpuPercent(current, previous cpuSample) float64 {
	cpuDelta := float64(current.totalUsage) - float64(previous.totalUsage)
	systemDelta := float64(current.systemUsage) - float64(previous.systemUsage)
	if systemDelta <= 0 || cpuDelta < 0 {
		return 0
	}
	online := float64(current.onlineCPUs)
	if online == 0 {
		online = 1
	}
	return (cpuDelta / systemDelta) * online * 100
}

func onlineCPUs(stats container.StatsResponse) uint32 {
	if stats.CPUStats.OnlineCPUs > 0 {
		return stats.CPUStats.OnlineCPUs
	}
	return uint32(len(stats.CPUStats.CPUUsage.PercpuUsage))
}

func memoryPercent(stats container.StatsResponse) float64 {
	if stats.MemoryStats.Limit == 0 {
		return 0
	}
	return float64(stats.MemoryStats.Usage) / float64(stats.MemoryStats.Limit) * 100
}

func containerName(item container.Summary) string {
	if len(item.Names) > 0 {
		// Docker's own names carry a leading slash ("/web-1").
		name := item.Names[0]
		if len(name) > 0 && name[0] == '/' {
			return name[1:]
		}
		return name
	}
	return shortID(item.ID)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
