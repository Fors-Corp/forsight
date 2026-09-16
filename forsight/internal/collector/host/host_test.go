package host

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/net"
)

func TestCollect_ReturnsPlausibleHostMetrics(t *testing.T) {
	c := New()
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect returned an error: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatal("Collect returned no metrics")
	}

	byName := make(map[string]float64)
	for _, m := range metrics {
		byName[m.Name] = m.Value
		if m.Timestamp.IsZero() {
			t.Errorf("metric %q has a zero Timestamp", m.Name)
		}
	}

	if v, ok := byName["host.cpu.percent"]; ok && (v < 0 || v > 100) {
		t.Errorf("host.cpu.percent = %v, want within [0, 100]", v)
	}
	if v, ok := byName["host.memory.percent"]; ok && (v < 0 || v > 100) {
		t.Errorf("host.memory.percent = %v, want within [0, 100]", v)
	}
	if v, ok := byName["host.memory.used_bytes"]; ok && v <= 0 {
		t.Errorf("host.memory.used_bytes = %v, want > 0", v)
	}
}

func TestName(t *testing.T) {
	if got := New().Name(); got != "host" {
		t.Errorf("Name() = %q, want %q", got, "host")
	}
}

func TestReadFileNr(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sys/fs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sys/fs/file-nr"), []byte("1024\t0\t9223372036854775807\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	used, max, ok := readFileNr(dir)
	if !ok {
		t.Fatal("readFileNr reported not ok for a well-formed file")
	}
	if used != 1024 {
		t.Errorf("used = %v, want 1024 (the first field)", used)
	}
	if max != 9223372036854775807 {
		t.Errorf("max = %v, want the third field, not the middle (free) one", max)
	}
}

func TestReadFileNr_MissingOrMalformed(t *testing.T) {
	if _, _, ok := readFileNr(t.TempDir()); ok {
		t.Error("readFileNr should report not ok when file-nr does not exist")
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sys/fs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sys/fs/file-nr"), []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := readFileNr(dir); ok {
		t.Error("readFileNr should report not ok for unparseable content")
	}
}

func TestCollect_EmitsHostFDMetricsFromHostProc(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sys/fs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sys/fs/file-nr"), []byte("256 0 4096\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := New()
	c.HostProc = dir
	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect returned an error: %v", err)
	}

	byName := make(map[string]float64)
	for _, m := range metrics {
		byName[m.Name] = m.Value
	}
	if byName["host.fd.used"] != 256 {
		t.Errorf("host.fd.used = %v, want 256", byName["host.fd.used"])
	}
	if byName["host.fd.max"] != 4096 {
		t.Errorf("host.fd.max = %v, want 4096", byName["host.fd.max"])
	}
}

func TestCollect_GroupsConnectionCountsByState(t *testing.T) {
	c := New()
	c.HostProc = t.TempDir() // no file-nr here — isolates this test to conn_count
	c.connections = func(context.Context) ([]net.ConnectionStat, error) {
		return []net.ConnectionStat{
			{Status: "ESTABLISHED"},
			{Status: "ESTABLISHED"},
			{Status: "LISTEN"},
		}, nil
	}

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect returned an error: %v", err)
	}

	byState := make(map[string]float64)
	for _, m := range metrics {
		if m.Name == "host.net.conn_count" {
			byState[m.Labels["state"]] = m.Value
		}
	}
	if byState["ESTABLISHED"] != 2 {
		t.Errorf("ESTABLISHED count = %v, want 2: %+v", byState["ESTABLISHED"], byState)
	}
	if byState["LISTEN"] != 1 {
		t.Errorf("LISTEN count = %v, want 1: %+v", byState["LISTEN"], byState)
	}
}

func TestCollect_ConnectionErrorIsBestEffort(t *testing.T) {
	c := New()
	c.HostProc = t.TempDir()
	c.connections = func(context.Context) ([]net.ConnectionStat, error) {
		return nil, context.DeadlineExceeded
	}

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect should stay best-effort when connection enumeration fails: %v", err)
	}
	for _, m := range metrics {
		if m.Name == "host.net.conn_count" {
			t.Errorf("expected no host.net.conn_count metrics when the lister errors, got %+v", m)
		}
	}
}
