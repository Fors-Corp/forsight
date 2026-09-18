// Package statsd implements a StatsD (with the common DogStatsD tag
// extension, "|#tag1:value1,tag2") UDP receiver — the other extremely
// common metrics wire protocol beyond OTLP/Prometheus, with a client
// library in nearly every language.
//
// Unlike this repo's other collectors, StatsD's protocol is architecturally
// push-based and continuous, not "ask and get an instant snapshot" — a
// counter's real value only exists as a running total across whatever
// packets arrive between two ticks. So this collector has two separate
// entry points instead of one: Listen runs the UDP server in the
// background (start it once, in its own goroutine, independent of the
// collector.Registry's ticker), accumulating counters/gauges/timers as
// packets arrive; Collect (the interface collector.Registry actually calls
// on its ticker) drains and resets that accumulator, the same
// "flush-and-reset" semantics the original Etsy statsd server popularized.
// Gauges are the one exception: they persist their last value across a
// flush rather than resetting to zero, matching every real StatsD
// implementation's gauge semantics — but only for gaugeTTL after the last
// packet that set them, so a client that goes away stops costing memory
// forever. UDP has no connection to close and no authentication to fail, so
// every bound in this file is the only thing standing between a datagram and
// the agent's heap; see maxSeries.
package statsd

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
)

var errNotBound = errors.New("statsd: Serve called before a successful Bind")

// The bounds below exist because this is the one ingest path in the agent
// that cannot be authenticated: StatsD is UDP, the default listen address is
// :8125 on every interface, and a source address is spoofable, so anything
// that can route a datagram here can write to these maps. OTLP and the
// Prometheus scrape both cap what one request can turn into; before these,
// this did not.
const (
	// maxSeries caps distinct series held across counters, gauges and timers
	// together. A single 65535-byte datagram carries roughly 6500 "a1:1|g"
	// lines, each of which was a permanent heap entry: sustained, that is an
	// out-of-memory kill in minutes, and short of one, every registry tick
	// emitted a model.Metric per accumulated gauge into the store and into
	// Forseer. 10000 is well above any real application's cardinality and far
	// below what hurts.
	maxSeries = 10_000

	// maxTimerSamplesPerKey caps one key's samples within a single flush
	// interval, so one name cannot absorb an unbounded slice between ticks.
	// Timers are summarized to count/sum/quantiles, and a few thousand
	// samples already pin those well inside a percentile bucket's width.
	maxTimerSamplesPerKey = 10_000

	// gaugeTTL is how long a gauge keeps reporting its last value with no
	// further packets. Long enough that an ordinary reporting cadence (or a
	// brief client restart) never loses a series, short enough that a dead
	// client's cardinality does not accumulate for the life of the process.
	gaugeTTL = 10 * time.Minute
)

// Collector accumulates StatsD packets and flushes them as forsight metrics.
type Collector struct {
	addr string

	mu       sync.Mutex
	conn     net.PacketConn
	counters map[metricKey]float64
	gauges   map[metricKey]gaugeEntry
	timers   map[metricKey][]float64
	// dropped counts lines refused for exceeding maxSeries since the last
	// flush, so a cardinality wall shows up as a number somewhere rather than
	// as series that quietly stop appearing.
	dropped int
}

// gaugeEntry is a gauge's last value and when a packet last set it. The
// timestamp is what lets Collect expire a gauge whose client is gone; a bare
// float64 could only ever grow.
type gaugeEntry struct {
	value    float64
	lastSeen time.Time
}

type metricKey struct {
	name string
	tags string // tags pre-sorted and joined, so two equivalent tag sets collapse to one key
}

// New builds a Collector that will listen on addr (e.g. ":8125", StatsD's
// conventional port) once Listen is called.
func New(addr string) *Collector {
	return &Collector{
		addr:     addr,
		counters: map[metricKey]float64{},
		gauges:   map[metricKey]gaugeEntry{},
		timers:   map[metricKey][]float64{},
	}
}

func (c *Collector) Name() string { return "statsd" }

// Bind reserves the UDP socket, synchronously. Binding is separate from
// serving so a caller learns immediately whether the address is usable —
// "port already in use" is a startup error worth failing on, not something
// to discover asynchronously after having already logged that the receiver
// is listening. It also means a caller that passed port 0 can read the real
// port back from LocalAddr before any packet is sent.
func (c *Collector) Bind() error {
	conn, err := net.ListenPacket("udp", c.addr)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	return nil
}

// LocalAddr is the address the socket is actually bound to, or nil before
// Bind succeeds.
func (c *Collector) LocalAddr() net.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// Serve reads packets until ctx is cancelled. Call it in its own goroutine
// (see cmd/run.go) — it blocks for the process's lifetime. Bind must have
// succeeded first.
func (c *Collector) Serve(ctx context.Context) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return errNotBound
	}

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	buf := make([]byte, 65535) // the practical max UDP payload; oversized packets are truncated by the kernel before we see them
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue // a single malformed/truncated read shouldn't kill the listener
		}
		c.ingest(buf[:n])
	}
}

// Listen is Bind followed by Serve, for a caller that needs neither the
// resolved address nor a synchronous bind error.
func (c *Collector) Listen(ctx context.Context) error {
	if err := c.Bind(); err != nil {
		return err
	}
	return c.Serve(ctx)
}

// ingest handles one UDP datagram, which may batch several newline-separated
// stat lines — a common StatsD client optimization to reduce packet count.
func (c *Collector) ingest(data []byte) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		c.ingestLine(line)
	}
}

// ingestLine parses one "name:value|type[|@sampleRate][|#tag1:v1,tag2]" line.
func (c *Collector) ingestLine(line string) {
	nameAndRest, ok := splitOnce(line, ':')
	if !ok {
		return
	}
	name := nameAndRest[0]
	fields := strings.Split(nameAndRest[1], "|")
	if len(fields) < 2 {
		return
	}

	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return
	}
	// ParseFloat accepts "NaN", "Inf", "+Inf" and "-Inf" (in any case), so
	// without this an unauthenticated datagram can put a non-finite value in
	// the store. json.Marshal then fails on the whole metrics response, and
	// the detector's Welford state for that series is poisoned permanently.
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return
	}
	metricType := fields[1]

	sampleRate := 1.0
	var tags string
	for _, f := range fields[2:] {
		switch {
		case strings.HasPrefix(f, "@"):
			if r, err := strconv.ParseFloat(f[1:], 64); err == nil && r > 0 {
				sampleRate = r
			}
		case strings.HasPrefix(f, "#"):
			tags = normalizeTags(f[1:])
		}
	}
	key := metricKey{name: name, tags: tags}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Membership before length: once the cap is reached a series already
	// being tracked must keep updating, or the accumulator freezes at
	// whatever arrived first and every established metric goes stale while
	// the flood is refused. Only a genuinely new key is turned away.
	if !c.knownLocked(key) && c.seriesLocked() >= maxSeries {
		c.dropped++
		return
	}

	switch metricType {
	case "c":
		// A sampled counter under-reports by definition — scale back up
		// to what the client says the real count would have been.
		c.counters[key] += value / sampleRate
	case "g":
		now := time.Now()
		if strings.HasPrefix(fields[0], "+") || strings.HasPrefix(fields[0], "-") {
			// DogStatsD's relative-gauge-adjustment convention.
			c.gauges[key] = gaugeEntry{value: c.gauges[key].value + value, lastSeen: now}
		} else {
			c.gauges[key] = gaugeEntry{value: value, lastSeen: now}
		}
	case "ms", "h", "d":
		if len(c.timers[key]) >= maxTimerSamplesPerKey {
			c.dropped++
			return
		}
		c.timers[key] = append(c.timers[key], value)
	case "s":
		// Sets (unique-value counting) need a different accumulator shape
		// than a plain float — not implemented; the line is dropped rather
		// than silently mis-recorded as a bogus numeric value.
	}
}

// knownLocked reports whether key is already accumulating somewhere. Callers
// hold c.mu.
func (c *Collector) knownLocked(key metricKey) bool {
	if _, ok := c.counters[key]; ok {
		return true
	}
	if _, ok := c.gauges[key]; ok {
		return true
	}
	_, ok := c.timers[key]
	return ok
}

// seriesLocked is how many distinct series are held right now. A key present
// in two accumulators counts twice, which is the conservative direction: the
// cap is about bounding memory, and two entries cost two entries. Callers
// hold c.mu.
func (c *Collector) seriesLocked() int {
	return len(c.counters) + len(c.gauges) + len(c.timers)
}

func splitOnce(s string, sep byte) ([2]string, bool) {
	i := strings.IndexByte(s, sep)
	if i < 0 {
		return [2]string{}, false
	}
	return [2]string{s[:i], s[i+1:]}, true
}

// normalizeTags sorts a raw "tag1:v1,tag2" fragment so semantically
// identical tag sets sent in a different order collapse to the same
// metricKey instead of fragmenting the same series.
func normalizeTags(raw string) string {
	tags := strings.Split(raw, ",")
	sort.Strings(tags)
	return strings.Join(tags, ",")
}

func tagsToLabels(tags string) map[string]string {
	if tags == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	out := make(map[string]string, len(parts))
	for _, t := range parts {
		if k, v, ok := strings.Cut(t, ":"); ok {
			out[k] = v
		} else {
			out[t] = ""
		}
	}
	return out
}

// Collect drains the current accumulator. Counters and timers reset to zero
// after a flush (classic StatsD semantics: each flush reports "since the
// last flush"); gauges persist their last value, since a gauge represents
// "the current value of something," not an interval total.
func (c *Collector) Collect(_ context.Context) ([]model.Metric, error) {
	now := time.Now()

	c.mu.Lock()
	counters, timers := c.counters, c.timers
	c.counters = map[metricKey]float64{}
	c.timers = map[metricKey][]float64{}

	// Gauges are not swapped out — they persist by design — so they must be
	// COPIED here rather than aliased. Handing the live map out and ranging
	// over it after the unlock is a concurrent map iteration and write the
	// moment one more packet arrives, which the Go runtime turns into an
	// unrecoverable fatal error: a remote, unauthenticated crash of the whole
	// agent. Expiry happens in the same critical section, so a gone client's
	// keys leave both the snapshot and the live map together.
	for key, g := range c.gauges {
		if now.Sub(g.lastSeen) > gaugeTTL {
			delete(c.gauges, key)
		}
	}
	gauges := make(map[metricKey]float64, len(c.gauges))
	for key, g := range c.gauges {
		gauges[key] = g.value
	}
	dropped := c.dropped
	c.dropped = 0
	c.mu.Unlock()

	var out []model.Metric
	if dropped > 0 {
		slog.Default().Warn("statsd dropped lines over the series cap",
			"dropped", dropped, "max_series", maxSeries)
	}
	for key, v := range counters {
		out = append(out, model.Metric{Name: key.name, Value: v, Timestamp: now, Labels: tagsToLabels(key.tags)})
	}
	for key, v := range gauges {
		out = append(out, model.Metric{Name: key.name, Value: v, Timestamp: now, Labels: tagsToLabels(key.tags)})
	}
	for key, samples := range timers {
		out = append(out, timerMetrics(key, samples, now)...)
	}
	return out, nil
}

// timerMetrics summarizes one flush interval's timer/histogram samples the
// same way this repo's other two ingestion paths already do — _count,
// _sum, and {quantile="..."} — so a dashboard query doesn't need to care
// whether a given series arrived via StatsD, OTLP, or a Prometheus scrape
// (see internal/collector/otlp/receiver.go and
// internal/collector/promscrape/promscrape.go for the same shape).
func timerMetrics(key metricKey, samples []float64, ts time.Time) []model.Metric {
	if len(samples) == 0 {
		return nil
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)

	labels := tagsToLabels(key.tags)
	var sum float64
	for _, s := range sorted {
		sum += s
	}

	out := []model.Metric{
		{Name: key.name + "_count", Value: float64(len(sorted)), Timestamp: ts, Labels: labels},
		{Name: key.name + "_sum", Value: sum, Timestamp: ts, Labels: labels},
	}
	for _, q := range []float64{0.5, 0.9, 0.99} {
		qLabels := map[string]string{"quantile": strconv.FormatFloat(q, 'g', -1, 64)}
		for k, v := range labels {
			qLabels[k] = v
		}
		out = append(out, model.Metric{Name: key.name, Value: percentile(sorted, q), Timestamp: ts, Labels: qLabels})
	}
	return out
}

// percentile assumes sorted is already sorted ascending.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}
