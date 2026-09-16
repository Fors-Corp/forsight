package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBootRealBinary is the end-to-end check ROADMAP.md's "Left out" section
// asked for: it `go build`s the actual forsight binary — the same command
// the go job and a real release run — and boots it, so a break in what a
// user actually runs (wiring in main.go/cmd, the embedded dashboard build
// under internal/api/webdist/, graceful shutdown) fails a test even though
// every one of those pieces already has its own unit tests that pass in
// isolation. It was left out until now because there was nothing to wait on
// short of a timing guess: roadmap item 24 added /readyz, and the dashboard
// embed stopped being a stub once the real build landed in webdist/ (see
// internal/api/dashboard.go), so a plain `go build` already produces the
// real thing with no Node involved.
//
// Kept to one boot: building the binary and starting it dominate the run
// time, so every HTTP assertion below shares the one process rather than
// paying that cost repeatedly.
func TestBootRealBinary(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH: cannot build the binary to boot")
	}

	binPath := filepath.Join(t.TempDir(), "forsight")
	build := exec.Command(goBin, "build", "-o", binPath, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	addr := freeLoopbackAddr(t)
	baseURL := "http://" + addr

	cmd := exec.Command(binPath, "run",
		"--addr", addr,
		"--store", "memory",
		"--disable-docker",
		"--disable-statsd",
		"--disable-autoscrape",
	)
	var output syncBuffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting forsight: %v", err)
	}

	// done closes after Wait returns, carrying waitErr with it — the
	// channel close is what makes reading waitErr (written on the goroutine
	// below, read from the test goroutine after SIGINT) and cmd.ProcessState
	// safe without a mutex of their own.
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
			<-done
		}
	})

	if err := waitForReady(baseURL, 10*time.Second); err != nil {
		t.Fatalf("waiting for /readyz: %v\n--- forsight output ---\n%s", err, output.String())
	}

	t.Run("GET / serves the built dashboard, not a fallback page", func(t *testing.T) {
		body, status := getBody(t, baseURL+"/")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
		// The real build's index.html loads a content-hashed script from
		// ./assets/ (see internal/api/webdist/index.html); a fallback page
		// with nothing to embed would have no such tag.
		if !strings.Contains(body, `<script type="module"`) || !strings.Contains(body, "assets/") {
			t.Errorf("body doesn't look like the built dashboard (no assets script tag):\n%s", body)
		}
	})

	t.Run("GET /api/v1/metrics returns JSON", func(t *testing.T) {
		body, status := getBody(t, baseURL+"/api/v1/metrics")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, body)
		}
		var decoded any
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Errorf("body is not valid JSON: %v\nbody: %s", err, body)
		}
	})

	t.Run("GET /healthz is ok", func(t *testing.T) {
		body, status := getBody(t, baseURL+"/healthz")
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", status, body)
		}
	})

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("sending SIGINT: %v", err)
	}
	select {
	case <-done:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			t.Errorf("exited with code %d after SIGINT, want 0\n--- forsight output ---\n%s", exitErr.ExitCode(), output.String())
		} else if waitErr != nil {
			t.Errorf("Wait: %v\n--- forsight output ---\n%s", waitErr, output.String())
		}
	case <-time.After(7 * time.Second):
		// run's own shutdown budget (see cmd/run.go) is 5s; 7s gives it
		// margin before this test calls it a hang rather than a slow box.
		_ = cmd.Process.Kill()
		t.Fatalf("did not exit within the 5s shutdown budget after SIGINT\n--- forsight output ---\n%s", output.String())
	}
}

// freeLoopbackAddr reserves an ephemeral port on 127.0.0.1 by binding and
// immediately closing a listener, and returns its address for the
// subprocess's --addr. --addr has no ":0"-reports-back-the-real-port
// mechanism of its own (run's "forsight listening" log line prints the flag
// value, not what the kernel picked), so this is the only way to hand the
// child a free port without guessing one — accepting the small window
// between the close here and the child's own bind.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a free port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("releasing the reserved port: %v", err)
	}
	return addr
}

// waitForReady polls GET /readyz until it answers 200 or timeout elapses,
// returning the last error (or a non-200 status) it saw.
func waitForReady(baseURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/readyz")
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status == http.StatusOK {
			return nil
		}
		lastErr = fmt.Errorf("/readyz status = %d, want 200", status)
		time.Sleep(50 * time.Millisecond)
	}
	return lastErr
}

// getBody GETs url and returns its body and status code, failing the test
// on a transport error.
func getBody(t *testing.T, url string) (string, int) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body of GET %s: %v", url, err)
	}
	return string(data), resp.StatusCode
}

// syncBuffer collects the subprocess's combined stdout/stderr. It needs its
// own lock because os/exec copies into it from a goroutine of its own for as
// long as the process runs, while this test may read it (for a failure
// message) before that process has exited.
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
