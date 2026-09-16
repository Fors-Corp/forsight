package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/marcfs31/forsight/forseer"
	"github.com/marcfs31/forsight/forsight/internal/api"
	"github.com/marcfs31/forsight/forsight/internal/collector/filelog"
	"github.com/marcfs31/forsight/forsight/internal/model"
	"github.com/marcfs31/forsight/forsight/internal/store"
)

// demoHost is the label every synthetic point carries, so nothing this
// command writes can be mistaken for a real host, process, or service —
// see the "Why" in ROADMAP.md item 20.
const demoHost = "forsight-demo"

// demoMinBaseline is how many quiet ticks a series needs before the spike:
// a bit above forseer's own minSamples (12, unexported — see
// forseer/detector.go) so the CPU-anomaly and culprit-ranking detectors
// have a settled baseline to compare the spike against, not just enough to
// stop returning the cold-start sigma pair.
const demoMinBaseline = 15

type demoOptions struct {
	addr      string
	authToken string
	backfill  time.Duration
	tick      time.Duration
	retention time.Duration
	seed      uint64
}

func newDemoCmd() *cobra.Command {
	opts := &demoOptions{}
	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Serve the dashboard against a bounded synthetic stream, so it is never empty",
		Long: "demo runs the same API/dashboard server as \"forsight run\", but instead of real " +
			"collectors it feeds a bounded, clearly-labelled synthetic stream — host and process " +
			"metrics with a daily cycle and one CPU spike with a named culprit, a handful of log " +
			"templates including a burst and a mix of declared and inferred severities, and a " +
			"couple of traces with one slow child span — through the same Engine.Observe* and " +
			"store paths a real deployment uses. With --backfill it replays that history before " +
			"serving, so the time-range picker and Forseer's own models have a trend from the " +
			"first request rather than a blank chart.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDemo(cmd.Context(), opts, slog.New(slog.NewTextHandler(cmd.OutOrStdout(), nil)))
		},
	}

	cmd.Flags().StringVar(&opts.addr, "addr", ":8080", "address to serve the API and dashboard on")
	cmd.Flags().StringVar(&opts.authToken, "auth-token", "",
		"require Authorization: Bearer <token> on every route except GET /healthz; "+
			"also read from FORSIGHT_AUTH_TOKEN when the flag is empty (auth is off by default)")
	cmd.Flags().DurationVar(&opts.backfill, "backfill", 6*time.Hour,
		"how much synthetic history to generate before serving, so the dashboard opens already "+
			"populated; 0 disables backfill and demo starts from an empty stream")
	cmd.Flags().DurationVar(&opts.tick, "tick", 10*time.Second,
		"spacing between synthetic samples, both backfilled and live")
	cmd.Flags().DurationVar(&opts.retention, "retention", 0,
		"how long the store keeps synthetic data; 0 (the default) picks --backfill plus a "+
			"two-hour margin, so a backfilled point is never pruned before it is ever served")
	cmd.Flags().Uint64Var(&opts.seed, "seed", 1,
		"seed for the synthetic generator; the same seed always produces the same demo data")

	return cmd
}

// runDemo wires a MemoryStore and a live forseer.Engine exactly as "forsight
// run" does (see observingStore in run.go), replays --backfill worth of
// synthetic history into them, then serves the dashboard while a background
// generator keeps appending live samples at --tick — the same shape as a
// real deployment's registry tick, just synthetic on both ends.
func runDemo(ctx context.Context, opts *demoOptions, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.tick <= 0 {
		return fmt.Errorf("--tick must be positive, got %s", opts.tick)
	}

	retention := demoRetention(opts.backfill, opts.retention)
	eng := forseer.NewEngine().WithSeverityFallback(func(message string) string {
		return string(filelog.FallbackSeverity(message))
	})
	st := observingStore{Store: store.NewMemoryStore(retention), eng: eng}

	backfillSteps := 0
	if opts.backfill > 0 {
		backfillSteps = int(opts.backfill / opts.tick)
	}
	gen := newDemoGenerator(opts.seed, backfillSteps)

	if backfillSteps > 0 {
		logger.Info("backfilling synthetic history", "span", opts.backfill, "samples", backfillSteps, "retention", retention)
		if err := demoBackfill(ctx, st, gen, opts.backfill, opts.tick, backfillSteps); err != nil {
			return err
		}
	}

	server := api.NewServer(st, nil, api.DashboardHandler(), logger).WithForseer(eng)
	authToken := resolveAuthToken(opts.authToken)
	if authToken == "" && !isLoopbackListenAddr(opts.addr) {
		logger.Warn("listening on a non-loopback address with no authentication configured; set --auth-token or FORSIGHT_AUTH_TOKEN")
	}
	httpServer := newHTTPServer(opts.addr, api.BearerAuth(authToken, server.Handler()))

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("forsight demo listening", "addr", opts.addr, "tick", opts.tick)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	liveDone := make(chan struct{})
	go func() {
		defer close(liveDone)
		runDemoLive(ctx, st, gen, opts.tick, backfillSteps, logger)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := httpServer.Shutdown(shutdownCtx)
		<-liveDone
		return err
	case err := <-serveErr:
		// Cancels ctx too (signal.NotifyContext's stop, called above via
		// defer, is exactly this — calling it here just does it early), so
		// runDemoLive's ticker loop unwinds instead of writing into a store
		// nothing is serving any more.
		stop()
		<-liveDone
		return err
	}
}

// demoRetention picks how long the demo store keeps data. An explicit
// --retention always wins; otherwise it is --backfill plus a one-hour
// margin so nothing backfilled is pruned before it can ever be queried,
// floored at two hours so a --backfill=0 demo still has a sane live window.
func demoRetention(backfill, explicit time.Duration) time.Duration {
	if explicit > 0 {
		return explicit
	}
	const floor = 2 * time.Hour
	if margin := backfill + time.Hour; margin > floor {
		return margin
	}
	return floor
}

// demoBackfill replays `steps` synthetic samples spaced `tick` apart, oldest
// first, ending at "now". It runs as fast as it can — no real sleeping —
// since every metric/log/span already carries its true historical
// Timestamp; only forseer.Engine's own clock (always wall-clock, the same
// as a real deployment's — see forseer/engine.go's now field) is compressed
// to the moment this call runs, exactly as it would be if a real agent
// ingested a batch of old data all at once.
func demoBackfill(ctx context.Context, st store.Store, gen *demoGenerator, span, tick time.Duration, steps int) error {
	start := time.Now().Add(-span)
	for i := 0; i < steps; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		ts := start.Add(time.Duration(i) * tick)
		if err := emitDemoTick(ctx, st, gen, ts, i); err != nil {
			return fmt.Errorf("backfilling sample %d/%d: %w", i+1, steps, err)
		}
	}
	return nil
}

// runDemoLive appends one synthetic sample every tick until ctx is done —
// the live counterpart to demoBackfill, continuing the same index sequence
// so a spike or burst placed near the end of the backfill window is not
// repeated once live ticking picks up.
func runDemoLive(ctx context.Context, st store.Store, gen *demoGenerator, tick time.Duration, startIndex int, logger *slog.Logger) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	index := startIndex
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := emitDemoTick(ctx, st, gen, now, index); err != nil {
				logger.Error("demo generator", "error", err)
			}
			index++
		}
	}
}

// emitDemoTick generates one sample and writes it through st — the same
// WriteMetrics/WriteLogs/WriteSpans path collector.Registry and the OTLP
// receiver use, which is what feeds forseer.Engine on the observingStore
// wrapper (see run.go).
func emitDemoTick(ctx context.Context, st store.Store, gen *demoGenerator, ts time.Time, index int) error {
	sample := gen.tick(ts, index)
	if len(sample.metrics) > 0 {
		if err := st.WriteMetrics(ctx, sample.metrics); err != nil {
			return fmt.Errorf("write synthetic metrics: %w", err)
		}
	}
	if len(sample.logs) > 0 {
		if err := st.WriteLogs(ctx, sample.logs); err != nil {
			return fmt.Errorf("write synthetic logs: %w", err)
		}
	}
	if len(sample.spans) > 0 {
		if err := st.WriteSpans(ctx, sample.spans); err != nil {
			return fmt.Errorf("write synthetic spans: %w", err)
		}
	}
	return nil
}

// demoSample is what one generator tick produces; any field may be empty —
// not every tick carries a trace, for instance.
type demoSample struct {
	metrics []model.Metric
	logs    []model.LogEntry
	spans   []model.Span
}

// demoGenerator produces the bounded, deterministic synthetic stream: given
// the same seed and the same backfill length, two runs of `forsight demo`
// produce byte-identical data. It schedules exactly one CPU spike (with a
// named culprit process), one correlated error-log burst, and one
// deliberately slow trace — placed near the end of the backfill window when
// there is one (visible the moment the dashboard opens, with a full
// baseline behind it for Forseer to judge it against), or a short way into
// the live stream otherwise.
type demoGenerator struct {
	rng *rand.Rand

	spikeAt  int // first tick of the CPU spike / culprit process
	spikeLen int
	burstAt  int // first tick of the error-log burst
	burstLen int
	slowAt   int // tick whose trace gets a slow child span

	netSent, netRecv float64 // monotonic counters, accumulated across ticks
}

// demoLogSources rotate across the synthetic services so the "Error logs by
// hour" heatmap and the log stream's source filter have more than one row.
var demoLogSources = []string{"demo.web-1", "demo.checkout-service", "demo.payments-worker"}

func newDemoGenerator(seed uint64, backfillSteps int) *demoGenerator {
	spikeAt := demoMinBaseline
	if backfillSteps > demoMinBaseline {
		// 90% of the way through the backfill: visible without hunting for
		// it, with the whole rest of the window as quiet baseline before it.
		if at := backfillSteps - backfillSteps/10; at > demoMinBaseline {
			spikeAt = at
		}
	}
	return &demoGenerator{
		rng:      rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		spikeAt:  spikeAt,
		spikeLen: 3,
		burstAt:  spikeAt + 1,
		burstLen: 4,
		slowAt:   spikeAt + 2,
	}
}

// tick produces the sample for one time step. index is the position in the
// overall backfill+live sequence (0-based, monotonic), used only to place
// the scheduled spike/burst/slow-trace and to pick which rotating template
// applies — never to compute wall time, which comes from ts.
func (g *demoGenerator) tick(ts time.Time, index int) demoSample {
	var s demoSample
	s.metrics = g.hostMetrics(ts, index)
	s.metrics = append(s.metrics, g.processMetrics(index)...)
	s.logs = g.logLines(ts, index)
	if span := g.trace(ts, index); span != nil {
		s.spans = span
	}
	return s
}

// dayFraction is how far ts is into its UTC day, in [0, 1) — the daily
// cycle host metrics ride on. UTC, not Local: it must be identical
// regardless of where "forsight demo" happens to run.
func dayFraction(ts time.Time) float64 {
	u := ts.UTC()
	secs := u.Hour()*3600 + u.Minute()*60 + u.Second()
	return float64(secs) / 86400
}

func (g *demoGenerator) inSpike(index int) bool {
	return index >= g.spikeAt && index < g.spikeAt+g.spikeLen
}

func (g *demoGenerator) inBurst(index int) bool {
	return index >= g.burstAt && index < g.burstAt+g.burstLen
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

func (g *demoGenerator) noise(spread float64) float64 {
	return (g.rng.Float64() - 0.5) * spread
}

func (g *demoGenerator) hostMetrics(ts time.Time, index int) []model.Metric {
	frac := dayFraction(ts)
	labels := map[string]string{"host": demoHost}

	cpu := clamp(26+12*math.Sin(2*math.Pi*frac)+g.noise(6), 2, 97)
	if g.inSpike(index) {
		cpu = clamp(90+g.noise(8), 60, 99)
	}
	mem := clamp(42+9*math.Sin(2*math.Pi*frac+1)+g.noise(4), 5, 95)
	disk := clamp(55+float64(index)*0.001+g.noise(1), 5, 97)

	g.netSent += 40_000 + g.rng.Float64()*20_000
	g.netRecv += 90_000 + g.rng.Float64()*40_000

	return []model.Metric{
		{Name: "host.cpu.percent", Value: cpu, Timestamp: ts, Labels: labels},
		{Name: "host.memory.percent", Value: mem, Timestamp: ts, Labels: labels},
		{Name: "host.disk.percent", Value: disk, Timestamp: ts, Labels: mergeLabels(labels, "path", "/")},
		{Name: "host.net.bytes_sent", Value: g.netSent, Timestamp: ts, Labels: labels},
		{Name: "host.net.bytes_recv", Value: g.netRecv, Timestamp: ts, Labels: labels},
		{Name: "host.uptime_seconds", Value: 3600 + float64(index), Timestamp: ts, Labels: mergeLabels(labels, "hostname", demoHost)},
	}
}

// demoProcesses are the always-on synthetic workload; demoCulprit only
// appears while inSpike is true.
var demoProcesses = []struct {
	pid, name string
	cpu, cpu2 float64 // baseline CPU range
	rss, rss2 float64 // baseline RSS range, bytes
}{
	{pid: "101", name: "demo-api", cpu: 4, cpu2: 16, rss: 150_000_000, rss2: 210_000_000},
	{pid: "102", name: "demo-worker", cpu: 3, cpu2: 12, rss: 120_000_000, rss2: 170_000_000},
}

const demoCulpritPID = "4242"
const demoCulpritName = "demo-reindex-job"

func (g *demoGenerator) processMetrics(index int) []model.Metric {
	var out []model.Metric
	for _, p := range demoProcesses {
		labels := map[string]string{"host": demoHost, "pid": p.pid, "name": p.name}
		cpu := p.cpu + g.rng.Float64()*(p.cpu2-p.cpu)
		rss := p.rss + g.rng.Float64()*(p.rss2-p.rss)
		out = append(out,
			model.Metric{Name: "process.cpu.percent", Value: cpu, Labels: labels},
			model.Metric{Name: "process.memory.rss_bytes", Value: rss, Labels: labels},
		)
	}
	if g.inSpike(index) {
		labels := map[string]string{"host": demoHost, "pid": demoCulpritPID, "name": demoCulpritName}
		out = append(out,
			model.Metric{Name: "process.cpu.percent", Value: clamp(58+g.noise(14), 40, 90), Labels: labels},
			model.Metric{Name: "process.memory.rss_bytes", Value: 950_000_000 + g.rng.Float64()*150_000_000, Labels: labels},
		)
	}
	return out
}

func mergeLabels(base map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(base)+1)
	for bk, bv := range base {
		out[bk] = bv
	}
	out[k] = v
	return out
}

// demoLogLine is one rotating log template. declared is false for the one
// template meant to exercise the severity-fallback path (see
// filelog.FallbackSeverity) rather than train the severity model on it.
type demoLogLine struct {
	message  string
	severity model.LogSeverity
	declared bool
	everyN   int // emitted when index%everyN == offset
	offset   int
}

var demoLogTemplates = []demoLogLine{
	{message: "[demo] request completed", severity: model.LogSeverityInfo, declared: true, everyN: 1, offset: 0},
	{message: "[demo] refreshing connection pool", severity: model.LogSeverityDebug, declared: true, everyN: 7, offset: 3},
	{message: "[demo] retrying upstream call (attempt 2)", severity: model.LogSeverityWarn, declared: true, everyN: 11, offset: 5},
	{message: "[demo] request failed downstream, falling back", severity: "", declared: false, everyN: 23, offset: 9},
	{message: "[demo] payment provider timeout, scheduling retry", severity: model.LogSeverityError, declared: true, everyN: 47, offset: 17},
}

func (g *demoGenerator) logLines(ts time.Time, index int) []model.LogEntry {
	var out []model.LogEntry
	for _, tmpl := range demoLogTemplates {
		if index%tmpl.everyN != tmpl.offset%tmpl.everyN {
			continue
		}
		source := demoLogSources[index%len(demoLogSources)]
		entry := model.LogEntry{Timestamp: ts, Source: source, Message: tmpl.message}
		if tmpl.declared {
			entry.Severity = tmpl.severity
		} else {
			entry.Severity = filelog.FallbackSeverity(tmpl.message)
			entry.SeverityInferred = true
		}
		out = append(out, entry)
	}
	if g.inBurst(index) {
		for i := 0; i < 3; i++ {
			source := demoLogSources[(index+i)%len(demoLogSources)]
			out = append(out, model.LogEntry{
				Timestamp: ts,
				Source:    source,
				Severity:  model.LogSeverityError,
				Message:   "[demo] connection reset by peer",
			})
		}
	}
	return out
}

const demoTraceService = "demo-service"

// trace emits one root+child span pair every 6th tick, plus one extra pair
// right at slowAt regardless of that alignment — otherwise the deliberately
// slow trace the "What" promises could fall on a tick trace() never fires
// on. The tick at slowAt gets the slow child; every other trace stays fast.
func (g *demoGenerator) trace(ts time.Time, index int) []model.Span {
	if index%6 != 0 && index != g.slowAt {
		return nil
	}
	traceID := fmt.Sprintf("demo-trace-%06d", index)
	rootID := traceID + "-root"
	childID := traceID + "-child"

	childMs := 8 + g.rng.Float64()*30
	if index == g.slowAt {
		childMs = 3_200 + g.rng.Float64()*400
	}
	rootMs := childMs + 15 + g.rng.Float64()*20

	root := model.Span{
		TraceID: traceID, SpanID: rootID, Name: "GET /orders", Service: demoTraceService,
		Start: ts, Duration: time.Duration(rootMs * float64(time.Millisecond)), Status: model.SpanStatusOK,
		Attributes: map[string]string{"host": demoHost},
	}
	child := model.Span{
		TraceID: traceID, SpanID: childID, ParentID: rootID, Name: "db.query", Service: demoTraceService,
		Start: ts, Duration: time.Duration(childMs * float64(time.Millisecond)), Status: model.SpanStatusOK,
		Attributes: map[string]string{"host": demoHost},
	}
	return []model.Span{root, child}
}
