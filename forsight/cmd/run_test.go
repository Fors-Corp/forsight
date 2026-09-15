package cmd

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
	"github.com/marcfs31/forsight/forsight/internal/store"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestNewBackingStore(t *testing.T) {
	t.Run("defaults to memory", func(t *testing.T) {
		backing, badgerStore, err := newBackingStore(&runOptions{storeBackend: "", retention: time.Hour})
		if err != nil {
			t.Fatalf("newBackingStore: %v", err)
		}
		if badgerStore != nil {
			t.Errorf("badgerStore = %v, want nil for an unset --store", badgerStore)
		}
		if _, ok := backing.(*store.MemoryStore); !ok {
			t.Errorf("backing = %T, want *store.MemoryStore", backing)
		}
	})

	t.Run(`"memory" is explicit too`, func(t *testing.T) {
		backing, badgerStore, err := newBackingStore(&runOptions{storeBackend: "memory", retention: time.Hour})
		if err != nil {
			t.Fatalf("newBackingStore: %v", err)
		}
		if badgerStore != nil {
			t.Errorf("badgerStore = %v, want nil for --store=memory", badgerStore)
		}
		if _, ok := backing.(*store.MemoryStore); !ok {
			t.Errorf("backing = %T, want *store.MemoryStore", backing)
		}
	})

	t.Run(`"badger" opens a database at --data-dir and returns it for the shutdown path too`, func(t *testing.T) {
		dir := t.TempDir()
		backing, badgerStore, err := newBackingStore(&runOptions{storeBackend: "badger", dataDir: dir, retention: time.Hour})
		if err != nil {
			t.Fatalf("newBackingStore: %v", err)
		}
		t.Cleanup(func() {
			if err := closeBadgerStore(badgerStore, discardLogger()); err != nil {
				t.Errorf("closeBadgerStore: %v", err)
			}
		})
		if badgerStore == nil {
			t.Fatal("badgerStore = nil, want the opened *store.BadgerStore for --store=badger")
		}
		if backing != store.Store(badgerStore) {
			t.Errorf("backing and badgerStore must be the same value: backing=%v badgerStore=%v", backing, badgerStore)
		}
	})

	t.Run("rejects an unknown backend", func(t *testing.T) {
		_, _, err := newBackingStore(&runOptions{storeBackend: "postgres", retention: time.Hour})
		if err == nil {
			t.Fatal(`newBackingStore(storeBackend: "postgres") = nil error, want one`)
		}
	})
}

func TestCloseBadgerStore_NilIsNoop(t *testing.T) {
	if err := closeBadgerStore(nil, discardLogger()); err != nil {
		t.Errorf("closeBadgerStore(nil, ...) = %v, want nil", err)
	}
}

func TestResolveAuthToken(t *testing.T) {
	t.Setenv("FORSIGHT_AUTH_TOKEN", "from-env")

	if got := resolveAuthToken("from-flag"); got != "from-flag" {
		t.Errorf("flag should override env: got %q", got)
	}
	if got := resolveAuthToken(""); got != "from-env" {
		t.Errorf("empty flag should fall back to env: got %q", got)
	}

	t.Setenv("FORSIGHT_AUTH_TOKEN", "")
	if got := resolveAuthToken(""); got != "" {
		t.Errorf("empty flag and empty env: got %q, want empty", got)
	}
}

func TestResolveErrorSLO(t *testing.T) {
	t.Setenv("FORSIGHT_ERROR_SLO", "0.02")

	if got := resolveErrorSLO(0.05); got != 0.05 {
		t.Errorf("flag should override env: got %v", got)
	}
	if got := resolveErrorSLO(0); got != 0.02 {
		t.Errorf("unset flag should fall back to env: got %v", got)
	}

	t.Setenv("FORSIGHT_ERROR_SLO", "not-a-number")
	if got := resolveErrorSLO(0); got != 0 {
		t.Errorf("unparsable env should be ignored: got %v, want 0", got)
	}

	t.Setenv("FORSIGHT_ERROR_SLO", "")
	if got := resolveErrorSLO(0); got != 0 {
		t.Errorf("unset flag and empty env: got %v, want 0", got)
	}
	if got := resolveErrorSLO(-1); got != 0 {
		t.Errorf("non-positive flag and empty env: got %v, want 0", got)
	}
}

func TestResolveMlaas(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		t.Setenv("MLAAS_URL", "")
		t.Setenv("MLAAS_API_KEY", "")
		t.Setenv("MLAAS_API_KEY_FILE", "")
		_, ok, err := resolveMlaas(&runOptions{})
		if err != nil || ok {
			t.Fatalf("resolveMlaas with nothing set = ok %v, err %v; want off and no error", ok, err)
		}
	})

	t.Run("key file, trimmed", func(t *testing.T) {
		t.Setenv("MLAAS_API_KEY", "")
		path := filepath.Join(t.TempDir(), "api_key")
		if err := os.WriteFile(path, []byte("  secret-token\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, ok, err := resolveMlaas(&runOptions{
			mlaasURL: "http://127.0.0.1:8090", mlaasAPIKeyFile: path,
			mlaasPrefix: "agent-a", mlaasSyncInterval: time.Minute,
		})
		if err != nil || !ok {
			t.Fatalf("resolveMlaas = ok %v, err %v; want on", ok, err)
		}
		if cfg.APIKey != "secret-token" {
			t.Errorf("APIKey = %q, want the file's contents trimmed", cfg.APIKey)
		}
		if cfg.URL != "http://127.0.0.1:8090" || cfg.Prefix != "agent-a" || cfg.SyncInterval != time.Minute {
			t.Errorf("cfg = %+v, want the flags carried through", cfg)
		}
	})

	t.Run("env wins over the file and fills in the URL", func(t *testing.T) {
		t.Setenv("MLAAS_URL", "http://mlaas.internal:8090")
		t.Setenv("MLAAS_API_KEY", "from-env")
		cfg, ok, err := resolveMlaas(&runOptions{mlaasAPIKeyFile: filepath.Join(t.TempDir(), "missing")})
		if err != nil || !ok {
			t.Fatalf("resolveMlaas = ok %v, err %v; want on", ok, err)
		}
		if cfg.URL != "http://mlaas.internal:8090" || cfg.APIKey != "from-env" {
			t.Errorf("cfg = %+v, want URL and key from the environment", cfg)
		}
	})

	t.Run("a URL without a key is an error up front", func(t *testing.T) {
		t.Setenv("MLAAS_API_KEY", "")
		t.Setenv("MLAAS_API_KEY_FILE", "")
		if _, _, err := resolveMlaas(&runOptions{mlaasURL: "http://127.0.0.1:8090"}); err == nil {
			t.Fatal("resolveMlaas with a URL and no key: want an error, got nil")
		}
		missing := filepath.Join(t.TempDir(), "nope")
		if _, _, err := resolveMlaas(&runOptions{mlaasURL: "http://127.0.0.1:8090", mlaasAPIKeyFile: missing}); err == nil {
			t.Fatal("resolveMlaas with an unreadable key file: want an error, got nil")
		}
	})
}

func TestIsLoopbackListenAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:8080", true},
		{"[::1]:8080", true},
		{":8080", false},
		{"0.0.0.0:8080", false},
		{"192.168.1.1:8080", false},
	}
	for _, tc := range cases {
		if got := isLoopbackListenAddr(tc.addr); got != tc.want {
			t.Errorf("isLoopbackListenAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestParseScrapeTargets(t *testing.T) {
	t.Run("bare URL labels the job with the host", func(t *testing.T) {
		got, err := parseScrapeTargets([]string{"http://localhost:9100/metrics"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 target, got %d", len(got))
		}
		if got[0].URL != "http://localhost:9100/metrics" {
			t.Errorf("URL = %q", got[0].URL)
		}
		if got[0].Labels["job"] != "localhost:9100" {
			t.Errorf(`job = %q, want "localhost:9100"`, got[0].Labels["job"])
		}
	})

	t.Run("job= prefix wins over the host default", func(t *testing.T) {
		got, err := parseScrapeTargets([]string{"node=http://10.0.0.4:9100/metrics"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[0].Labels["job"] != "node" {
			t.Errorf(`job = %q, want "node"`, got[0].Labels["job"])
		}
		if got[0].URL != "http://10.0.0.4:9100/metrics" {
			t.Errorf("URL = %q — the prefix must not stay in the URL", got[0].URL)
		}
	})

	t.Run("a URL containing = is not mistaken for a job prefix", func(t *testing.T) {
		got, err := parseScrapeTargets([]string{"http://host:9100/metrics?format=text"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got[0].URL != "http://host:9100/metrics?format=text" {
			t.Errorf("URL = %q", got[0].URL)
		}
		if got[0].Labels["job"] != "host:9100" {
			t.Errorf(`job = %q`, got[0].Labels["job"])
		}
	})

	t.Run("rejects a value that is not an absolute URL", func(t *testing.T) {
		for _, bad := range []string{"localhost:9100/metrics", "job=", "/metrics"} {
			if _, err := parseScrapeTargets([]string{bad}); err == nil {
				t.Errorf("parseScrapeTargets(%q) = nil error, want one", bad)
			}
		}
	})

	t.Run("no targets is not an error", func(t *testing.T) {
		got, err := parseScrapeTargets(nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v", got, err)
		}
	})
}

// errSink always fails WriteLogs, forcing filelog.Tail to return an error
// so retryTail's restart path runs.
type errSink struct{ calls atomic.Int32 }

func (s *errSink) WriteLogs(_ context.Context, _ []model.LogEntry) error {
	s.calls.Add(1)
	return errors.New("boom")
}

// TestRetryTail_RestartsAfterFailureAndStopsOnCancel guards against the
// finding that a filelog.Tail failure used to kill the collector for a
// path permanently ("log tailer stopped" was the last anyone heard of it,
// logged once by cmd/run.go's goroutine before it exited for good). A
// failing tailer must now be restarted with backoff — and still exit
// promptly once the context is cancelled, rather than retrying forever.
func TestRetryTail_RestartsAfterFailureAndStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sink := &errSink{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		retryTail(ctx, path, sink, nil, logger, time.Millisecond, 5*time.Millisecond)
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for sink.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := sink.calls.Load(); got < 3 {
		t.Fatalf("want at least 3 restarts (WriteLogs calls), got %d — retryTail should keep restarting a failing tailer", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retryTail did not stop promptly after ctx cancellation")
	}
}
