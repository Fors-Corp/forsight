package statsd

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/model"
	"math"
	"strings"
)

func TestIngestLine_CounterGaugeTimer(t *testing.T) {
	c := New(":0")
	c.ingestLine("requests:1|c")
	c.ingestLine("requests:1|c")
	c.ingestLine("connections:5|g")
	c.ingestLine("latency:100|ms")
	c.ingestLine("latency:200|ms")

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := map[string]float64{}
	for _, m := range metrics {
		key := m.Name
		if q, ok := m.Labels["quantile"]; ok {
			key += "{quantile=" + q + "}"
		}
		byName[key] = m.Value
	}

	if byName["requests"] != 2 {
		t.Errorf("requests = %v, want 2 (two increments of 1)", byName["requests"])
	}
	if byName["connections"] != 5 {
		t.Errorf("connections = %v, want 5", byName["connections"])
	}
	if byName["latency_count"] != 2 {
		t.Errorf("latency_count = %v, want 2", byName["latency_count"])
	}
	if byName["latency_sum"] != 300 {
		t.Errorf("latency_sum = %v, want 300", byName["latency_sum"])
	}
}

func TestCollect_ResetsCountersAndTimersButNotGauges(t *testing.T) {
	c := New(":0")
	c.ingestLine("requests:5|c")
	c.ingestLine("connections:10|g")
	c.ingestLine("latency:50|ms")

	first, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect (first): %v", err)
	}
	if len(first) == 0 {
		t.Fatal("first flush produced no metrics")
	}

	second, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect (second): %v", err)
	}

	byName := map[string]float64{}
	for _, m := range second {
		byName[m.Name] = m.Value
	}
	if _, ok := byName["requests"]; ok {
		t.Error("counter 'requests' survived a flush with no new packets — counters should reset to zero (absent) after flushing")
	}
	if _, ok := byName["latency_count"]; ok {
		t.Error("timer 'latency' survived a flush with no new packets — timers should reset after flushing")
	}
	if got, ok := byName["connections"]; !ok || got != 10 {
		t.Errorf("gauge 'connections' = %v (present=%v), want 10 (present) — gauges must persist across a flush with no new packets", got, ok)
	}
}

func TestIngestLine_SampleRateScalesCounters(t *testing.T) {
	c := New(":0")
	// A client reporting at 1-in-10 sampling says "this one event actually
	// represents 10" — the stored counter must scale back up.
	c.ingestLine("requests:1|c|@0.1")

	metrics, _ := c.Collect(context.Background())
	if len(metrics) != 1 || metrics[0].Value != 10 {
		t.Fatalf("got %+v, want a single requests=10 metric (1 / 0.1)", metrics)
	}
}

func TestIngestLine_DogStatsDTags(t *testing.T) {
	c := New(":0")
	c.ingestLine("requests:1|c|#route:checkout,env:prod")
	c.ingestLine("requests:1|c|#env:prod,route:checkout") // same tags, different order

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// Both lines should collapse into ONE series (tag order shouldn't
	// fragment an otherwise-identical tag set) with a combined value of 2.
	if len(metrics) != 1 {
		t.Fatalf("got %d metrics, want 1 (tag-order-independent aggregation): %+v", len(metrics), metrics)
	}
	if metrics[0].Value != 2 {
		t.Errorf("requests = %v, want 2", metrics[0].Value)
	}
	if metrics[0].Labels["route"] != "checkout" || metrics[0].Labels["env"] != "prod" {
		t.Errorf("labels = %+v, want route=checkout, env=prod", metrics[0].Labels)
	}
}

func TestIngestLine_MalformedLinesAreIgnored(t *testing.T) {
	c := New(":0")
	c.ingestLine("not a valid line")
	c.ingestLine("missing-type:5")
	c.ingestLine("bad-value:abc|c")
	c.ingestLine("") // blank line from a trailing newline

	metrics, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(metrics) != 0 {
		t.Fatalf("got %+v, want no metrics from garbage input", metrics)
	}
}

func TestBindServe_RealUDPPacket(t *testing.T) {
	// Port 0 plus Bind's synchronous contract: the socket is bound before
	// this line returns, and LocalAddr reports the port the kernel chose, so
	// there is no window in which a packet can be sent to an unbound address
	// (which on Linux earns an ICMP port-unreachable and makes the *next*
	// write on a connected UDP socket fail with ECONNREFUSED).
	c := New("127.0.0.1:0")
	if err := c.Bind(); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	addr := c.LocalAddr()
	if addr == nil {
		t.Fatal("LocalAddr is nil after a successful Bind")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- c.Serve(ctx) }()

	conn, err := net.Dial("udp", addr.String())
	if err != nil {
		t.Fatalf("net.Dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte("real.metric:42|c\n")); err != nil {
		t.Fatalf("conn.Write: %v", err)
	}

	// The write is synchronous but delivery to the reader goroutine is not,
	// so poll for the value rather than sleeping a guessed interval.
	deadline := time.Now().Add(5 * time.Second)
	var value float64
	for time.Now().Before(deadline) {
		got, err := c.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if len(got) > 0 {
			value = got[0].Value
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if value != 42 {
		t.Fatalf("no metric received over a real UDP socket within the deadline (got %v)", value)
	}

	cancel()
	select {
	case err := <-serveErrCh:
		if err != nil {
			t.Errorf("Serve returned an error after ctx cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return within 2s of ctx cancellation")
	}
}

func TestServeBeforeBind(t *testing.T) {
	if err := New("127.0.0.1:0").Serve(context.Background()); err == nil {
		t.Error("Serve() before Bind() = nil error, want one")
	}
}

func TestBindRejectsAnUnusableAddress(t *testing.T) {
	if err := New("127.0.0.1:notaport").Bind(); err == nil {
		t.Error("Bind() on a malformed address = nil error, want one")
	}
}

func TestName(t *testing.T) {
	if got := New(":0").Name(); got != "statsd" {
		t.Errorf("Name() = %q, want statsd", got)
	}
}

// TestCollector_SeriesCapBoundsIngest pins the cardinality bound at the point
// that matters: after ingest, not after a flush. Nothing about this listener
// can be authenticated — it is UDP on every interface by default — so before
// the cap, one 65535-byte datagram of distinct gauge lines was ~6500
// permanent heap entries, and a stream of them was an out-of-memory kill.
func TestCollector_SeriesCapBoundsIngest(t *testing.T) {
	c := New(":0")
	for i := 0; i < maxSeries*2; i++ {
		c.ingestLine("flood" + strconv.Itoa(i) + ":1|g")
	}
	c.mu.Lock()
	held, dropped := c.seriesLocked(), c.dropped
	c.mu.Unlock()
	if held > maxSeries {
		t.Fatalf("held %d series, want at most maxSeries=%d", held, maxSeries)
	}
	if dropped == 0 {
		t.Error("nothing recorded as dropped, so a cardinality wall would be invisible")
	}
}

// TestCollector_CapKeepsUpdatingKnownSeries is the other half of the cap: at
// the wall, a series already being tracked must keep accumulating. Checking
// the length before membership would freeze every established metric at
// whatever arrived first and hand an attacker a way to silence real data.
func TestCollector_CapKeepsUpdatingKnownSeries(t *testing.T) {
	c := New(":0")
	c.ingestLine("real.requests:1|c")
	for i := 0; i < maxSeries*2; i++ {
		c.ingestLine("flood" + strconv.Itoa(i) + ":1|g")
	}
	c.ingestLine("real.requests:41|c")

	got := collectOne(t, c)
	for _, m := range got {
		if m.Name == "real.requests" {
			if m.Value != 42 {
				t.Fatalf("real.requests = %v, want 42 — the cap stopped an established series from updating", m.Value)
			}
			return
		}
	}
	t.Fatal("real.requests missing entirely after the flood")
}

// TestCollector_GaugesExpireAfterTTL: gauges persist across a flush by
// design, which without a TTL means forever — every gauge any client ever
// sent, re-emitted into the store and into Forseer on every tick for the life
// of the process.
func TestCollector_GaugesExpireAfterTTL(t *testing.T) {
	c := New(":0")
	c.ingestLine("stale.gauge:7|g")
	c.ingestLine("fresh.gauge:9|g")

	c.mu.Lock()
	c.gauges[metricKey{name: "stale.gauge"}] = gaugeEntry{
		value: 7, lastSeen: time.Now().Add(-gaugeTTL - time.Minute),
	}
	c.mu.Unlock()

	names := map[string]bool{}
	for _, m := range collectOne(t, c) {
		names[m.Name] = true
	}
	if names["stale.gauge"] {
		t.Error("a gauge untouched for longer than gaugeTTL is still being reported")
	}
	if !names["fresh.gauge"] {
		t.Error("expiry took a gauge that was still being set")
	}

	c.mu.Lock()
	_, stillHeld := c.gauges[metricKey{name: "stale.gauge"}]
	c.mu.Unlock()
	if stillHeld {
		t.Error("the expired gauge left its entry in the live map, so the memory is not actually reclaimed")
	}
}

// TestCollector_CollectDoesNotRaceIngest fails under -race on the version
// that handed Collect the live gauge map and ranged over it after unlocking.
// That is not a benign race: concurrent map iteration and write is a Go
// runtime fatal error, which recover() cannot catch — so any host able to
// route a datagram to :8125 could crash the agent outright.
func TestCollector_CollectDoesNotRaceIngest(t *testing.T) {
	c := New(":0")
	for i := 0; i < 200; i++ {
		c.ingestLine("g" + strconv.Itoa(i) + ":1|g")
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			c.ingestLine("g" + strconv.Itoa(i%500) + ":1|g")
		}
	}()
	for i := 0; i < 200; i++ {
		if _, err := c.Collect(context.Background()); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}

// TestCollector_TimerSamplesBounded: one name must not absorb an unbounded
// slice between two ticks.
func TestCollector_TimerSamplesBounded(t *testing.T) {
	c := New(":0")
	for i := 0; i < maxTimerSamplesPerKey*2; i++ {
		c.ingestLine("slow.op:1|ms")
	}
	c.mu.Lock()
	n := len(c.timers[metricKey{name: "slow.op"}])
	c.mu.Unlock()
	if n > maxTimerSamplesPerKey {
		t.Fatalf("held %d timer samples for one key, want at most %d", n, maxTimerSamplesPerKey)
	}
}

func collectOne(t *testing.T, c *Collector) []model.Metric {
	t.Helper()
	got, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return got
}

// TestCollector_RejectsNonFiniteValues: ParseFloat accepts these literals in
// any case, and this listener is unauthenticated UDP on every interface by
// default — so without the filter, one datagram puts a value in the store
// that json.Marshal cannot encode and that poisons Forseer's baseline for
// that series permanently.
func TestCollector_RejectsNonFiniteValues(t *testing.T) {
	c := New(":0")
	for _, line := range []string{
		"evil.gauge:NaN|g", "evil.gauge2:nan|g",
		"evil.counter:Inf|c", "evil.counter2:+Inf|c", "evil.counter3:-inf|c",
		"evil.timer:NaN|ms",
	} {
		c.ingestLine(line)
	}
	c.ingestLine("good.gauge:1.5|g")

	for _, m := range collectOne(t, c) {
		if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
			t.Errorf("%s came through with a non-finite value %v", m.Name, m.Value)
		}
		if strings.HasPrefix(m.Name, "evil.") {
			t.Errorf("%s was accumulated at all", m.Name)
		}
	}
}
