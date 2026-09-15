package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
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
		"require Authorization: Bearer <token> on every route except GET /healthz; "+
			"also read from FORSIGHT_AUTH_TOKEN when the flag is empty (auth is off by default)")
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

	// The severity model competes against the tailer's substring rule on the
	// same stream, and is used only while it is winning.
	eng := forseer.NewEngine().WithSeverityFallback(func(message string) string {
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

	server := api.NewServer(st, otlpHandler, api.DashboardHandler(), logger).WithForseer(eng)

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
	httpServer := &http.Server{Addr: opts.addr, Handler: api.BearerAuth(authToken, server.Handler())}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("forsight listening", "addr", opts.addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
		return errors.Join(shutdownErr, closeBadgerStore(badgerStore, logger))
	case err := <-serveErr:
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

// resolveAuthToken prefers the --auth-token flag; when that is empty it falls
// back to FORSIGHT_AUTH_TOKEN. An empty result means auth stays off.
func resolveAuthToken(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("FORSIGHT_AUTH_TOKEN")
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
