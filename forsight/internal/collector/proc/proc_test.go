package proc

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
)

func TestCollect_EmitsRSSAlwaysAndCPUOnSecondSighting(t *testing.T) {
	c := &Collector{
		limit: 10,
		seen:  map[int32]struct{}{},
		list: func(context.Context) ([]sample, error) {
			return []sample{
				{PID: 7, Name: "forsight", CPUPercent: 12.5, RSS: 40 << 20},
				{PID: 9, Name: "idle", CPUPercent: 0.1, RSS: 1 << 20},
			}, nil
		},
	}

	first, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countByName(first, "process.memory.rss_bytes") != 2 {
		t.Fatalf("first tick rss = %d, want 2: %+v", countByName(first, "process.memory.rss_bytes"), first)
	}
	if countByName(first, "process.cpu.percent") != 0 {
		t.Fatalf("first tick should skip cpu.percent (no prior sample), got %+v", first)
	}

	second, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countByName(second, "process.cpu.percent") != 2 {
		t.Fatalf("second tick cpu = %d, want 2: %+v", countByName(second, "process.cpu.percent"), second)
	}
	var cpu model.Metric
	for _, m := range second {
		if m.Name == "process.cpu.percent" && m.Labels["name"] == "forsight" {
			cpu = m
		}
	}
	if cpu.Value != 12.5 {
		t.Errorf("forsight cpu = %v, want 12.5", cpu.Value)
	}
}

func TestCollect_EmitsFDCountAlways(t *testing.T) {
	c := &Collector{
		limit: 10,
		seen:  map[int32]struct{}{},
		list: func(context.Context) ([]sample, error) {
			return []sample{
				{PID: 7, Name: "forsight", CPUPercent: 12.5, RSS: 40 << 20, FDCount: 42},
				{PID: 9, Name: "idle", CPUPercent: 0.1, RSS: 1 << 20, FDCount: 3},
			}, nil
		},
	}

	got, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countByName(got, "process.fd.count") != 2 {
		t.Fatalf("fd.count = %d, want 2 (emitted like rss, no prior sample needed): %+v",
			countByName(got, "process.fd.count"), got)
	}
	var fd model.Metric
	for _, m := range got {
		if m.Name == "process.fd.count" && m.Labels["name"] == "forsight" {
			fd = m
		}
	}
	if fd.Value != 42 {
		t.Errorf("forsight fd.count = %v, want 42", fd.Value)
	}
}

func TestCollect_RespectsLimit(t *testing.T) {
	c := &Collector{
		limit: 1,
		seen:  map[int32]struct{}{},
		list: func(context.Context) ([]sample, error) {
			return []sample{
				{PID: 1, Name: "busy", CPUPercent: 90, RSS: 10},
				{PID: 2, Name: "quiet", CPUPercent: 1, RSS: 99},
			}, nil
		},
	}
	got, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countByName(got, "process.memory.rss_bytes") != 1 {
		t.Fatalf("limit not applied: %+v", got)
	}
	if got[0].Labels["name"] != "busy" {
		t.Errorf("kept %q, want the busier process", got[0].Labels["name"])
	}
}

func TestName(t *testing.T) {
	if New().Name() != "proc" {
		t.Errorf("Name() = %q, want proc", New().Name())
	}
}

func countByName(metrics []model.Metric, name string) int {
	n := 0
	for _, m := range metrics {
		if m.Name == name {
			n++
		}
	}
	return n
}

// The 2026-10-10 port-exhaustion incident: forsight held 16k leaked
// sockets for days, and none of it was visible, because an idle agent never
// ranks in the top N by CPU or RSS. Its own row is always reported.
func TestCollect_AlwaysReportsItselfPastTheLimit(t *testing.T) {
	c := &Collector{
		limit: 1,
		self:  7,
		seen:  map[int32]struct{}{},
		list: func(context.Context) ([]sample, error) {
			return []sample{
				{PID: 1, Name: "busy", CPUPercent: 90, RSS: 10, FDCount: 5},
				{PID: 7, Name: "forsight", CPUPercent: 0.1, RSS: 1, FDCount: 16589},
			}, nil
		},
	}
	got, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if countByName(got, "process.fd.count") != 2 {
		t.Fatalf("fd.count = %d, want the top-1 process plus forsight itself: %+v", countByName(got, "process.fd.count"), got)
	}
	var self model.Metric
	for _, m := range got {
		if m.Name == "process.fd.count" && m.Labels["pid"] == "7" {
			self = m
		}
	}
	if self.Value != 16589 {
		t.Errorf("own fd.count = %v, want 16589", self.Value)
	}
}

func TestCollect_WarnsOnceWhileOwnFDsStayAboveTheThreshold(t *testing.T) {
	fds := int32(selfFDWarnThreshold + 1)
	var logged bytes.Buffer
	c := &Collector{
		limit:  10,
		self:   7,
		seen:   map[int32]struct{}{},
		logger: slog.New(slog.NewTextHandler(&logged, nil)),
		list: func(context.Context) ([]sample, error) {
			return []sample{{PID: 7, Name: "forsight", FDCount: fds}}, nil
		},
	}
	warnings := func() int { return strings.Count(logged.String(), "forsight holds an unusual number of open file descriptors") }

	for i := 0; i < 3; i++ {
		if _, err := c.Collect(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if warnings() != 1 {
		t.Fatalf("got %d warnings over 3 ticks above the threshold, want 1:\n%s", warnings(), logged.String())
	}

	fds = 100 // back to normal, then over again: a fresh incident warns again
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	fds = selfFDWarnThreshold + 1
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if warnings() != 2 {
		t.Fatalf("got %d warnings, want a second one after recovering and crossing again:\n%s", warnings(), logged.String())
	}
}

func TestCollect_NoWarningBelowTheThreshold(t *testing.T) {
	var logged bytes.Buffer
	c := &Collector{
		limit:  10,
		self:   7,
		seen:   map[int32]struct{}{},
		logger: slog.New(slog.NewTextHandler(&logged, nil)),
		list: func(context.Context) ([]sample, error) {
			return []sample{{PID: 7, Name: "forsight", FDCount: selfFDWarnThreshold}}, nil
		},
	}
	if _, err := c.Collect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if logged.Len() != 0 {
		t.Fatalf("logged at exactly the threshold, want nothing:\n%s", logged.String())
	}
}
