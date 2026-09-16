package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcfs31/forsight/forseer"
	"github.com/marcfs31/forsight/forsight/internal/api"
	"github.com/marcfs31/forsight/forsight/internal/collector"
	dockercollector "github.com/marcfs31/forsight/forsight/internal/collector/docker"
	"github.com/marcfs31/forsight/forsight/internal/collector/filelog"
	hostcollector "github.com/marcfs31/forsight/forsight/internal/collector/host"
	"github.com/marcfs31/forsight/forsight/internal/collector/otlp"
	proccollector "github.com/marcfs31/forsight/forsight/internal/collector/proc"
	"github.com/marcfs31/forsight/forsight/internal/collector/promscrape"
	"github.com/marcfs31/forsight/forsight/internal/collector/statsd"
	"github.com/marcfs31/forsight/forsight/internal/mlaas"
	"github.com/marcfs31/forsight/forsight/internal/model"
	"github.com/marcfs31/forsight/forsight/internal/store"
)

type runOptions struct {
	addr              string
	authToken         string
	tlsCertFile       string
	tlsKeyFile        string
	tlsClientCAFile   string
	retention         time.Duration
	collectInterval   time.Duration
	disableDocker     bool
	disableOTLP       bool
	disableProc       bool
	disableStatsd     bool
	disableAutoscrape bool
	scrapeTargets     []string
	statsdAddr        string
	logFiles          []string
	errorSLO          float64
	storeBackend      string
	dataDir           string
	mlaasURL          string
	mlaasAPIKeyFile   string
	mlaasSyncInterval time.Duration
	mlaasPrefix       string
}

func newRunCmd() *cobra.Command {
	opts := &runOptions{}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the forsight agent: collectors, storage, and the API/dashboard server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), opts, slog.New(slog.NewTextHandler(cmd.OutOrStdout(), nil)))
		},
	}

	cmd.Flags().StringVar(&opts.addr, "addr", ":8080", "address to serve the API and dashboard on")
	cmd.Flags().StringVar(&opts.authToken, "auth-token", "",
		"require Authorization: Bearer <token> on every route except the dashboard's "+
			"static shell and GET /healthz; "+
			"also read from FORSIGHT_AUTH_TOKEN when the flag is empty (auth is off by default)")
	cmd.Flags().StringVar(&opts.tlsCertFile, "tls-cert", "",
		"path to a PEM certificate; with --tls-key, serves HTTPS instead of plaintext HTTP "+
			"(off by default); also read from FORSIGHT_TLS_CERT when the flag is empty")
	cmd.Flags().StringVar(&opts.tlsKeyFile, "tls-key", "",
		"path to the PEM private key matching --tls-cert; "+
			"also read from FORSIGHT_TLS_KEY when the flag is empty")
	cmd.Flags().StringVar(&opts.tlsClientCAFile, "tls-client-ca", "",
		"path to a PEM CA bundle; with --tls-cert/--tls-key, requires and verifies a client "+
			"certificate signed by it on every connection (mTLS); also read from "+
			"FORSIGHT_TLS_CLIENT_CA when the flag is empty")
	cmd.Flags().DurationVar(&opts.retention, "retention", time.Hour, "how long the store retains data, memory or Badger alike")
	cmd.Flags().DurationVar(&opts.collectInterval, "collect-interval", 10*time.Second, "how often the host/Docker collectors poll")
	cmd.Flags().BoolVar(&opts.disableDocker, "disable-docker", false, "skip the Docker collector even if a daemon is reachable")
	cmd.Flags().BoolVar(&opts.disableOTLP, "disable-otlp", false, "don't mount the OTLP ingest endpoints")
	cmd.Flags().BoolVar(&opts.disableProc, "disable-proc", false, "skip per-process CPU/memory collection")
	cmd.Flags().BoolVar(&opts.disableStatsd, "disable-statsd", false, "don't listen for StatsD/DogStatsD")
	cmd.Flags().BoolVar(&opts.disableAutoscrape, "disable-autoscrape", false, "don't probe well-known local Prometheus exporters (node_exporter :9100, …)")
	cmd.Flags().StringArrayVar(&opts.scrapeTargets, "scrape", nil,
		"Prometheus exposition endpoint to scrape on --collect-interval; repeatable. "+
			"Optionally prefix a job name: --scrape node=http://localhost:9100/metrics")
	cmd.Flags().StringVar(&opts.statsdAddr, "statsd-addr", ":8125",
		"listen for StatsD/DogStatsD metrics over UDP (default :8125 so a bare install receives them; --disable-statsd turns it off)")
	cmd.Flags().StringArrayVar(&opts.logFiles, "log-file", nil,
		"path of a log file to tail into the store (repeatable); severity is inferred from the line")
	cmd.Flags().Float64Var(&opts.errorSLO, "error-slo", 0,
		"target error-log rate for Forseer's error budget, e.g. 0.01 for 1% (default 1%); "+
			"also read from FORSIGHT_ERROR_SLO when unset")
	cmd.Flags().StringVar(&opts.storeBackend, "store", "memory",
		`storage backend: "memory" (default; fast, resets on every restart) or `+
			`"badger" (persists to --data-dir, survives a restart)`)
	cmd.Flags().StringVar(&opts.dataDir, "data-dir", "./forsight-data",
		`directory for the Badger database when --store=badger (ignored otherwise); `+
			"created if it doesn't exist")
	cmd.Flags().StringVar(&opts.mlaasURL, "mlaas-url", "",
		"base URL of an mlaas server (github.com/marcfs31/mlaas) that trains and serves models from this agent's own stream, "+
			"e.g. http://127.0.0.1:8090; also read from MLAAS_URL when the flag is empty (off by default)")
	cmd.Flags().StringVar(&opts.mlaasAPIKeyFile, "mlaas-api-key-file", "",
		"file holding the mlaas API key (mlaas writes it to <data>/api_key); also read from MLAAS_API_KEY_FILE, "+
			"or the key itself from MLAAS_API_KEY. There is deliberately no flag for the key, so it never shows up in ps")
	cmd.Flags().DurationVar(&opts.mlaasSyncInterval, "mlaas-sync-interval", 5*time.Minute,
		"how often the agent exports its datasets to mlaas and runs the feedback loops")
	cmd.Flags().StringVar(&opts.mlaasPrefix, "mlaas-prefix", "forsight",
		"prefix for every dataset and model this agent creates in mlaas, so several agents can share one server")

	return cmd
}

func run(ctx context.Context, opts *runOptions, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Scrape targets are pull-based like host/Docker, so the registry's own
	// ticker drives them — one collector for all targets, not one each.
	// Validated up front, before the store (possibly a Badger database) is
	// opened, so a bad --scrape value fails fast with nothing to clean up.
	targets, err := parseScrapeTargets(opts.scrapeTargets)
	if err != nil {
		return err
	}

	// TLS is opt-in and, like --scrape, validated up front: a missing key, an
	// unreadable cert, or a bad --tls-client-ca bundle must fail at startup
	// with nothing to clean up yet, never fall back to serving plaintext.
	tlsConfig, tlsEnabled, err := resolveTLSConfig(opts)
	if err != nil {
		return err
	}

	// Restored before anything else touches the engine, per roadmap item 25:
	// every model's own Restore overwrites rather than merges, so restoring
	// after the fallback (or after any live data) would either be a no-op
	// racing live traffic or discard what that traffic had already taught
	// it. snapshotPath is "" for a memory-store run, which makes restore and
	// (later) write both silent no-ops — a memory deployment has no data
	// dir, and Forseer's learned state has nowhere of its own to live
	// either.
	snapshotPath := forseerSnapshotPath(opts)
	eng := forseer.NewEngine()
	restoreForseerSnapshot(eng, snapshotPath, logger)
	// The severity model competes against the tailer's substring rule on the
	// same stream, and is used only while it is winning.
	eng = eng.WithSeverityFallback(func(message string) string {
		return string(filelog.FallbackSeverity(message))
	})
	if slo := resolveErrorSLO(opts.errorSLO); slo > 0 {
		eng.SetErrorSLO(slo)
	}
	backingStore, badgerStore, err := newBackingStore(opts)
	if err != nil {
		return err
	}
	if badgerStore != nil {
		logger.Info("using the Badger persistent store", "dir", opts.dataDir, "retention", opts.retention)
	}
	st := observingStore{Store: backingStore, eng: eng}

	collectors := []collector.Collector{hostcollector.New()}
	if !opts.disableProc {
		collectors = append(collectors, proccollector.New())
	}
	if !opts.disableDocker {
		if dockerCollector, err := dockercollector.New(ctx); err != nil {
			logger.Info("Docker collector disabled: no daemon reachable", "detail", err)
		} else {
			collectors = append(collectors, dockerCollector)
		}
	}

	if !opts.disableAutoscrape {
		found := promscrape.DiscoverLocal(ctx, targets)
		for _, t := range found {
			logger.Info("auto-discovered Prometheus exporter", "url", t.URL, "job", t.Labels["job"])
		}
		targets = append(targets, found...)
	}
	if len(targets) > 0 {
		collectors = append(collectors, promscrape.New(targets))
		for _, t := range targets {
			logger.Info("scraping Prometheus target", "url", t.URL, "job", t.Labels["job"])
		}
	}

	// StatsD is push-based: Listen accumulates packets continuously in its own
	// goroutine while Collect drains and resets that accumulator on the
	// registry's tick. That is why it is started separately from being
	// registered — see the package doc. Default listen is :8125 so a
	// curl|sh install receives StatsD with no flags; a bind failure is
	// non-fatal (the port may already be taken).
	if !opts.disableStatsd && opts.statsdAddr != "" {
		statsdCollector := statsd.New(opts.statsdAddr)
		if err := statsdCollector.Bind(); err != nil {
			logger.Warn("StatsD receiver not started", "addr", opts.statsdAddr, "error", err)
		} else {
			collectors = append(collectors, statsdCollector)
			go func() {
				if err := statsdCollector.Serve(ctx); err != nil && ctx.Err() == nil {
					logger.Error("StatsD receiver stopped", "addr", opts.statsdAddr, "error", err)
				}
			}()
			logger.Info("StatsD receiver listening", "addr", statsdCollector.LocalAddr())
		}
	}

	registry := collector.NewRegistry(st, opts.collectInterval, logger, collectors...)
	go registry.Run(ctx)

	for _, path := range opts.logFiles {
		logPath := path
		go tailWithRetry(ctx, logPath, st, eng, logger)
		logger.Info("tailing log file", "path", logPath)
	}

	var otlpHandler api.OTLPHandler
	if !opts.disableOTLP {
		otlpHandler = otlp.NewHandler(st, st, st)
	}

	server := api.NewServer(st, otlpHandler, api.DashboardHandler(), logger).WithForseer(eng).WithRegistry(registry)

	// mlaas is opt-in: with a URL, the agent exports its own stream there,
	// trains the managed models, and proxies what it learned to the
	// dashboard's Models page. The syncer reads the backing store directly
	// (reads only), and the API key it carries never leaves this process.
	if cfg, ok, err := resolveMlaas(opts); err != nil {
		return err
	} else if ok {
		syncer, err := mlaas.New(cfg, backingStore, eng.ClassifySeverity, logger)
		if err != nil {
			return err
		}
		go syncer.Run(ctx)
		server = server.WithMlaas(syncer)
		logger.Info("mlaas integration on", "url", syncer.DisplayURL(), "prefix", cfg.Prefix, "sync-interval", cfg.SyncInterval)
	}
	authToken := resolveAuthToken(opts.authToken)
	if authToken == "" && !isLoopbackListenAddr(opts.addr) {
		logger.Warn("listening on a non-loopback address with no authentication configured; set --auth-token or FORSIGHT_AUTH_TOKEN")
	}
	if authToken != "" && !tlsEnabled && !isLoopbackListenAddr(opts.addr) {
		logger.Warn("--auth-token is set on a non-loopback address with no TLS configured; " +
			"the bearer token and every payload cross the network in cleartext. Set --tls-cert/--tls-key.")
	}
	httpServer := newHTTPServer(opts.addr, api.BearerAuth(authToken, server.Handler()))
	if tlsEnabled {
		httpServer.TLSConfig = tlsConfig
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("forsight listening", "addr", opts.addr, "tls", tlsEnabled)
		// TLSConfig already carries the loaded certificate (and, for mTLS, the
		// client CA pool), so ListenAndServeTLS needs no filenames of its own —
		// net/http only reads its two string args when TLSConfig.Certificates
		// is empty.
		serve := httpServer.ListenAndServe
		if tlsEnabled {
			serve = func() error { return httpServer.ListenAndServeTLS("", "") }
		}
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := httpServer.Shutdown(shutdownCtx)
		writeForseerSnapshot(eng, snapshotPath, logger)
		return errors.Join(shutdownErr, closeBadgerStore(badgerStore, logger))
	case err := <-serveErr:
		writeForseerSnapshot(eng, snapshotPath, logger)
		return errors.Join(err, closeBadgerStore(badgerStore, logger))
	}
}

// newBackingStore builds the Store the agent writes to and queries,
// selected by --store. It also returns the concrete *store.BadgerStore when
// that backend was chosen (nil otherwise), so run's shutdown path can call
// Close on it directly — Store itself has no Close method, since MemoryStore
// has nothing to close.
func newBackingStore(opts *runOptions) (backing store.Store, badgerStore *store.BadgerStore, err error) {
	switch opts.storeBackend {
	case "", "memory":
		return store.NewMemoryStore(opts.retention), nil, nil
	case "badger":
		bs, err := store.NewBadgerStore(opts.dataDir, opts.retention)
		if err != nil {
			return nil, nil, fmt.Errorf("open Badger store: %w", err)
		}
		return bs, bs, nil
	default:
		return nil, nil, fmt.Errorf(`--store %q: want "memory" or "badger"`, opts.storeBackend)
	}
}

// closeBadgerStore closes bs if it's non-nil (opts.storeBackend != "badger"
// leaves it nil, so this is always safe to call unconditionally on both
// shutdown paths below). Called from two places rather than a bare
// top-level defer so a hard failure of the HTTP server's own shutdown
// doesn't skip it, and so the same explicit path handles both the graceful
// (ctx.Done) and the listener-failed (serveErr) cases identically.
func closeBadgerStore(bs *store.BadgerStore, logger *slog.Logger) error {
	if bs == nil {
		return nil
	}
	if err := bs.Close(); err != nil {
		logger.Error("closing Badger store", "error", err)
		return fmt.Errorf("close Badger store: %w", err)
	}
	return nil
}

// forseerSnapshotFile is the name of the file Forseer's own learned state
// restores from and is written to, beside the Badger directory under
// --data-dir. See forseerSnapshotPath for when there is one at all.
const forseerSnapshotFile = "forseer.json"

// forseerSnapshotPath returns where this run's Forseer snapshot lives, or
// "" when there is none to have. Only --store=badger has a --data-dir that
// belongs to this deployment; a memory store's data — and Forseer's learned
// state alongside it — disappears with the process by design, so there is
// nowhere durable to put a snapshot and restoreForseerSnapshot/
// writeForseerSnapshot both treat "" as a silent no-op.
func forseerSnapshotPath(opts *runOptions) string {
	if opts.storeBackend != "badger" || opts.dataDir == "" {
		return ""
	}
	return filepath.Join(opts.dataDir, forseerSnapshotFile)
}

// restoreForseerSnapshot reads path (a no-op for "") and restores eng from
// it. Callers must call this right after forseer.NewEngine and before
// anything else touches the engine — every model's own Restore overwrites
// rather than merges, so restoring onto an engine that has already wired up
// live data would discard that observation.
//
// A missing file is silent: it is not an error, it is what a brand new
// deployment or data dir looks like, on every run, not just the first — so
// logging it every restart would be noise rather than signal. Anything else
// that keeps the snapshot from being read or from parsing as this engine's
// own envelope is logged and otherwise ignored: the engine is left exactly
// as NewEngine built it, cold, which is always a safe place to start from.
// A model that did restore is logged once at INFO, by name and by the
// schema version its own payload carried — the roadmap item's own
// observability requirement, and the one line an operator needs to confirm
// a restart actually came back warm.
func restoreForseerSnapshot(eng *forseer.Engine, path string, logger *slog.Logger) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("reading forseer snapshot, starting cold", "path", path, "error", err)
		}
		return
	}
	reports, err := eng.Restore(data)
	if err != nil {
		logger.Warn("forseer snapshot discarded, starting cold", "path", path, "error", err)
		return
	}
	restored := make([]string, 0, len(reports))
	for _, r := range reports {
		if r.Restored {
			restored = append(restored, fmt.Sprintf("%s(v%d)", r.Name, r.Version))
		}
	}
	if len(restored) == 0 {
		return
	}
	logger.Info("forseer models restored", "path", path, "models", restored)
}

// writeForseerSnapshot snapshots eng and writes it to path (a no-op for
// ""), called from the shutdown path next to closeBadgerStore. A failure to
// snapshot or to write it is logged, never returned as a shutdown-blocking
// error: losing a snapshot only means the next start re-earns readiness the
// way every start did before this roadmap item, never a failed shutdown.
func writeForseerSnapshot(eng *forseer.Engine, path string, logger *slog.Logger) {
	if path == "" {
		return
	}
	data, err := eng.Snapshot()
	if err != nil {
		logger.Error("snapshotting forseer models", "path", path, "error", err)
		return
	}
	// Written to a sibling temp file and renamed into place, so a crash or
	// power loss mid-write leaves the previous snapshot intact rather than a
	// truncated one. Restore would discard a truncated file safely, but
	// "cold after a crash" is a worse outcome than "one snapshot behind".
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		logger.Error("writing forseer snapshot", "path", path, "error", err)
		return
	}
	if _, err := tmp.Write(data); err != nil {
		logger.Error("writing forseer snapshot", "path", path, "error", err)
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return
	}
	if err := tmp.Close(); err != nil {
		logger.Error("writing forseer snapshot", "path", path, "error", err)
		_ = os.Remove(tmp.Name())
		return
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		logger.Error("writing forseer snapshot", "path", path, "error", err)
		_ = os.Remove(tmp.Name())
		return
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		logger.Error("writing forseer snapshot", "path", path, "error", err)
		_ = os.Remove(tmp.Name())
	}
}

// tailMinBackoff and tailMaxBackoff bound the delay tailWithRetry waits
// between restarts of a failed filelog.Tail.
const (
	tailMinBackoff = time.Second
	tailMaxBackoff = 30 * time.Second
)

// tailWithRetry runs filelog.Tail for path, restarting it with a capped
// exponential backoff whenever it returns an error other than ctx
// cancellation. Tail itself now recovers from the failures this codebase
// has actually hit (an oversized line, a rotated/truncated file) instead of
// returning fatally, but this keeps a log file from going permanently
// unwatched — "log tailer stopped" used to mean stopped forever — on
// whatever else a filesystem can throw at it (e.g. a transient permission
// or I/O error), consistent with every other collector goroutine's
// ctx-scoped restart discipline in this file.
func tailWithRetry(ctx context.Context, path string, sink filelog.Sink, classifier filelog.Classifier, logger *slog.Logger) {
	retryTail(ctx, path, sink, classifier, logger, tailMinBackoff, tailMaxBackoff)
}

// retryTail holds tailWithRetry's loop with the backoff bounds as
// parameters so a test can drive it with millisecond backoffs instead of
// tailMinBackoff/tailMaxBackoff's real-world values.
func retryTail(ctx context.Context, path string, sink filelog.Sink, classifier filelog.Classifier, logger *slog.Logger, minBackoff, maxBackoff time.Duration) {
	backoff := minBackoff
	for {
		err := filelog.TailWith(ctx, path, sink, classifier)
		if ctx.Err() != nil {
			return
		}
		logger.Error("log tailer stopped, restarting", "path", path, "error", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// resolveFlagOrEnv prefers flagValue; when that is empty it falls back to
// envVar. Every flag in this file with an environment fallback (the auth
// token, and the TLS paths below) shares this precedence.
func resolveFlagOrEnv(flagValue, envVar string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv(envVar)
}

// resolveAuthToken prefers the --auth-token flag; when that is empty it falls
// back to FORSIGHT_AUTH_TOKEN. An empty result means auth stays off.
func resolveAuthToken(flagValue string) string {
	return resolveFlagOrEnv(flagValue, "FORSIGHT_AUTH_TOKEN")
}

// resolveTLSConfig builds the *tls.Config for --tls-cert/--tls-key (env
// fallbacks FORSIGHT_TLS_CERT/FORSIGHT_TLS_KEY) plus the optional
// --tls-client-ca (FORSIGHT_TLS_CLIENT_CA) for mTLS. ok is false only when
// neither --tls-cert nor --tls-key was set anywhere — TLS is opt-in, the same
// shape as --auth-token and --mlaas-url. Setting just one of --tls-cert/
// --tls-key, an unreadable or mismatched cert/key pair, or a --tls-client-ca
// file with no PEM certificate in it, is an error returned up front: run's
// caller treats any error here as fatal before the listener starts, so a bad
// path can never fall back to plaintext.
func resolveTLSConfig(opts *runOptions) (*tls.Config, bool, error) {
	certFile := resolveFlagOrEnv(opts.tlsCertFile, "FORSIGHT_TLS_CERT")
	keyFile := resolveFlagOrEnv(opts.tlsKeyFile, "FORSIGHT_TLS_KEY")
	if certFile == "" && keyFile == "" {
		return nil, false, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, false, errors.New("--tls-cert and --tls-key (or their FORSIGHT_TLS_CERT/FORSIGHT_TLS_KEY " +
			"equivalents) must both be set, or both left empty, to enable TLS")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, false, fmt.Errorf("loading --tls-cert/--tls-key: %w", err)
	}
	// TLS 1.2 is the floor, stated rather than inherited from whatever the
	// runtime's default happens to be in the Go version that built this.
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}

	if caFile := resolveFlagOrEnv(opts.tlsClientCAFile, "FORSIGHT_TLS_CLIENT_CA"); caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, false, fmt.Errorf("reading --tls-client-ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, false, fmt.Errorf("--tls-client-ca %q: no PEM certificate found", caFile)
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return cfg, true, nil
}

// resolveErrorSLO prefers the --error-slo flag; when that is unset (the flag
// defaults to 0, since 0% error tolerance is not a meaningful SLO) it falls
// back to FORSIGHT_ERROR_SLO. A non-positive result means the engine keeps
// its own built-in default (see forseer.defaultErrorSLO).
func resolveErrorSLO(flagValue float64) float64 {
	if flagValue > 0 {
		return flagValue
	}
	if raw := os.Getenv("FORSIGHT_ERROR_SLO"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil {
			return v
		}
	}
	return 0
}

// resolveMlaas turns the --mlaas-* flags and their MLAAS_* environment
// fallbacks into an mlaas.Config. ok is false when no URL was given anywhere,
// which is the default: the integration is opt-in. The key comes from
// MLAAS_API_KEY, else from the file named by --mlaas-api-key-file or
// MLAAS_API_KEY_FILE. A URL with no key is an error up front rather than a
// silent stream of 401s five minutes into the run.
func resolveMlaas(opts *runOptions) (mlaas.Config, bool, error) {
	base := opts.mlaasURL
	if base == "" {
		base = os.Getenv("MLAAS_URL")
	}
	if base == "" {
		return mlaas.Config{}, false, nil
	}
	key := os.Getenv("MLAAS_API_KEY")
	if key == "" {
		path := opts.mlaasAPIKeyFile
		if path == "" {
			path = os.Getenv("MLAAS_API_KEY_FILE")
		}
		if path != "" {
			raw, err := os.ReadFile(path)
			if err != nil {
				return mlaas.Config{}, false, fmt.Errorf("reading the mlaas API key: %w", err)
			}
			key = strings.TrimSpace(string(raw))
		}
	}
	if key == "" {
		return mlaas.Config{}, false, errors.New("--mlaas-url is set but no API key was given: " +
			"set MLAAS_API_KEY, or point --mlaas-api-key-file (MLAAS_API_KEY_FILE) at the file mlaas writes to <data>/api_key")
	}
	return mlaas.Config{
		URL:          base,
		APIKey:       key,
		Prefix:       opts.mlaasPrefix,
		SyncInterval: opts.mlaasSyncInterval,
	}, true, nil
}

// isLoopbackListenAddr reports whether addr is explicitly bound to a loopback
// interface. Bare ":PORT" and "0.0.0.0:PORT" are not — they listen on all
// interfaces.
func isLoopbackListenAddr(addr string) bool {
	return strings.HasPrefix(addr, "127.0.0.1:") ||
		strings.HasPrefix(addr, "localhost:") ||
		strings.HasPrefix(addr, "[::1]:")
}

// newHTTPServer builds the agent's listener with every timeout set. A bare
// &http.Server{} has none, so a client that opens a connection and never
// finishes its headers holds a goroutine for as long as it likes; the OTLP
// receiver caps bodies at 32 MiB but nothing caps time. The values are sized
// so a full 32 MiB OTLP body on a slow link still fits:
//
//   - ReadHeaderTimeout: headers are a few hundred bytes; 10s is generous for
//     any real client and is what frees the goroutine a stalled one holds.
//   - ReadTimeout: the whole request including the body. 32 MiB at ~1 Mbit/s
//     is about four and a half minutes.
//   - WriteTimeout: Go resets it once the header is read, so for HTTP/1.x it
//     spans reading the body plus writing the response. It must be at least
//     ReadTimeout or a large upload is cut off mid-body. Nothing here streams
//     (no SSE, no hijack) and the mlaas proxy bounds its own calls at 60s.
//   - IdleTimeout: a keep-alive connection between requests.
//   - MaxHeaderBytes: 1 MiB, the same as Go's default, made explicit.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// parseScrapeTargets turns --scrape values into promscrape targets. Each value
// is a URL, optionally prefixed with "job=" to label everything scraped from
// it — the same "job" convention Prometheus itself uses, so a dashboard can
// tell two node_exporters apart. Without a prefix the job label is the URL's
// host, which is a more useful default than no label at all.
func parseScrapeTargets(values []string) ([]promscrape.Target, error) {
	targets := make([]promscrape.Target, 0, len(values))
	for _, v := range values {
		job, raw := "", v
		if name, rest, found := strings.Cut(v, "="); found && !strings.Contains(name, "/") {
			job, raw = name, rest
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("--scrape %q: want an absolute URL like http://host:9100/metrics, optionally prefixed job=", v)
		}
		if job == "" {
			job = u.Host
		}
		targets = append(targets, promscrape.Target{URL: raw, Labels: map[string]string{"job": job}})
	}
	return targets, nil
}

// observingStore writes through to MemoryStore and feeds Forseer on every
// ingest path (host collectors and OTLP), so insights see the same stream
// the dashboard queries.
type observingStore struct {
	store.Store
	eng *forseer.Engine
}

func (s observingStore) WriteMetrics(ctx context.Context, metrics []model.Metric) error {
	if s.eng != nil && len(metrics) > 0 {
		pts := make([]forseer.Point, len(metrics))
		for i, m := range metrics {
			pts[i] = forseer.Point{Name: m.Name, Value: m.Value, Labels: m.Labels}
		}
		s.eng.ObserveMetrics(pts)
	}
	return s.Store.WriteMetrics(ctx, metrics)
}

func (s observingStore) WriteLogs(ctx context.Context, logs []model.LogEntry) error {
	if s.eng != nil && len(logs) > 0 {
		lines := make([]forseer.LogLine, len(logs))
		for i, l := range logs {
			lines[i] = forseer.LogLine{
				Timestamp:        l.Timestamp,
				Severity:         string(l.Severity),
				Source:           l.Source,
				Message:          l.Message,
				SeverityInferred: l.SeverityInferred,
			}
		}
		s.eng.ObserveLogs(lines)
	}
	return s.Store.WriteLogs(ctx, logs)
}

func (s observingStore) WriteSpans(ctx context.Context, spans []model.Span) error {
	if s.eng != nil && len(spans) > 0 {
		samples := make([]forseer.SpanSample, len(spans))
		for i, sp := range spans {
			samples[i] = forseer.SpanSample{
				Name:       sp.Name,
				Service:    sp.Service,
				DurationMs: float64(sp.Duration) / float64(time.Millisecond),
				Status:     string(sp.Status),
				TraceID:    sp.TraceID,
				SpanID:     sp.SpanID,
				ParentID:   sp.ParentID,
			}
		}
		s.eng.ObserveSpans(samples)
	}
	return s.Store.WriteSpans(ctx, spans)
}
