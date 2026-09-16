// Package host collects system-level metrics via gopsutil — CPU, memory,
// disk, network, uptime. gopsutil itself honors the HOST_PROC/HOST_SYS/
// HOST_ETC env-var overrides (the same convention node_exporter uses), so
// this collector needs no Kubernetes-specific code: a DaemonSet that mounts
// the host's /proc and /sys into the pod and sets those env vars gets real
// node-level metrics from this same binary. See forsight/deploy/k8s/.
package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

// maxTrackedConnections bounds how many connections a single Collect call
// will enumerate, so a large conntrack table degrades only the
// host.net.conn_count series instead of stalling the whole collector.
const maxTrackedConnections = 4096

// connLister is injectable so tests do not need a live connection table.
type connLister func(ctx context.Context) ([]net.ConnectionStat, error)

// Collector gathers host-level metrics. DiskPath defaults to "/" — set it to
// something else for a container image whose root isn't the volume worth
// tracking.
type Collector struct {
	DiskPath string
	// HostProc overrides the root used to read the host-level /proc data
	// gopsutil has no call for (currently just file-descriptor counts).
	// Empty means honor the HOST_PROC env var, gopsutil's own convention,
	// falling back to "/proc".
	HostProc string
	// sampleWindow is how long cpu.Percent blocks measuring — a real
	// sample, not an instantaneous (and noisier) snapshot. Kept short so
	// it doesn't dominate the registry's own collection interval.
	sampleWindow time.Duration
	connections  connLister
}

// New builds a host Collector with sensible defaults.
func New() *Collector {
	return &Collector{
		DiskPath:     "/",
		sampleWindow: 200 * time.Millisecond,
		connections: func(ctx context.Context) ([]net.ConnectionStat, error) {
			return net.ConnectionsMaxWithContext(ctx, "inet", maxTrackedConnections)
		},
	}
}

func (c *Collector) Name() string { return "host" }

func (c *Collector) Collect(ctx context.Context) ([]model.Metric, error) {
	now := time.Now()
	var metrics []model.Metric

	if pct, err := cpu.PercentWithContext(ctx, c.sampleWindow, false); err == nil && len(pct) > 0 {
		metrics = append(metrics, model.Metric{Name: "host.cpu.percent", Value: pct[0], Timestamp: now})
	}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		metrics = append(metrics,
			model.Metric{Name: "host.memory.percent", Value: vm.UsedPercent, Timestamp: now},
			model.Metric{Name: "host.memory.used_bytes", Value: float64(vm.Used), Timestamp: now},
			model.Metric{Name: "host.memory.total_bytes", Value: float64(vm.Total), Timestamp: now},
		)
	}

	if du, err := disk.UsageWithContext(ctx, c.diskPath()); err == nil {
		metrics = append(metrics,
			model.Metric{
				Name: "host.disk.percent", Value: du.UsedPercent, Timestamp: now,
				Labels: map[string]string{"path": du.Path},
			},
			model.Metric{
				Name: "host.disk.used_bytes", Value: float64(du.Used), Timestamp: now,
				Labels: map[string]string{"path": du.Path},
			},
		)
	}

	if counters, err := net.IOCountersWithContext(ctx, false); err == nil && len(counters) > 0 {
		total := counters[0]
		metrics = append(metrics,
			model.Metric{Name: "host.net.bytes_sent", Value: float64(total.BytesSent), Timestamp: now},
			model.Metric{Name: "host.net.bytes_recv", Value: float64(total.BytesRecv), Timestamp: now},
		)
	}

	if info, err := host.InfoWithContext(ctx); err == nil {
		metrics = append(metrics,
			model.Metric{
				Name: "host.uptime_seconds", Value: float64(info.Uptime), Timestamp: now,
				Labels: map[string]string{"hostname": info.Hostname},
			},
		)
	}

	if usedFDs, maxFDs, ok := readFileNr(c.hostProcPath()); ok {
		metrics = append(metrics,
			model.Metric{Name: "host.fd.used", Value: usedFDs, Timestamp: now},
			model.Metric{Name: "host.fd.max", Value: maxFDs, Timestamp: now},
		)
	}

	if conns, err := c.connections(ctx); err == nil {
		counts := map[string]int{}
		for _, cn := range conns {
			counts[cn.Status]++
		}
		for state, count := range counts {
			metrics = append(metrics, model.Metric{
				Name: "host.net.conn_count", Value: float64(count), Timestamp: now,
				Labels: map[string]string{"state": state},
			})
		}
	}

	if len(metrics) == 0 {
		return nil, fmt.Errorf("host collector: every metric source failed")
	}
	return metrics, nil
}

func (c *Collector) diskPath() string {
	if c.DiskPath == "" {
		return "/"
	}
	return c.DiskPath
}

func (c *Collector) hostProcPath() string {
	if c.HostProc != "" {
		return c.HostProc
	}
	if v := os.Getenv("HOST_PROC"); v != "" {
		return v
	}
	return "/proc"
}

// readFileNr parses $HOST_PROC/sys/fs/file-nr — gopsutil has no host-level
// fd call, so this reads the same file node_exporter's filefd collector
// does. The format is three whitespace-separated numbers: allocated file
// handles, free allocated handles, and the max; node_exporter (and this)
// report the first as "used" and the third as "max", ignoring the middle
// field. ok is false on any read or parse failure so the caller can skip
// the metric rather than emit a zero.
func readFileNr(hostProc string) (used, max float64, ok bool) {
	data, err := os.ReadFile(filepath.Join(hostProc, "sys/fs/file-nr"))
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, false
	}
	usedFDs, errUsed := strconv.ParseFloat(fields[0], 64)
	maxFDs, errMax := strconv.ParseFloat(fields[2], 64)
	if errUsed != nil || errMax != nil {
		return 0, 0, false
	}
	return usedFDs, maxFDs, true
}
