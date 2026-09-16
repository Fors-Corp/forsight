package proc

import (
	"context"
	"testing"

	"github.com/marcfs31/forsight/forsight/internal/model"
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
