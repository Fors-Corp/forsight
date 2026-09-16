package forseer

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxClusters   = 256
	burstWindow   = time.Minute
	burstMinCount = 8
	burstRatio    = 3
)

var (
	uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	ipv4Re = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	hexRe  = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	numRe  = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
)

type logMiner struct {
	mu       sync.Mutex
	clusters map[string]*liveCluster
	open     map[string]Insight
	now      func() time.Time
	total    int
	errors   int
	// paging, when set, decides a burst's severity once it is beating the
	// volume rule, and scores every cluster for the dashboard. Nil keeps the
	// volume rule alone.
	paging *pagingModel
}

type liveCluster struct {
	Cluster
	times []time.Time
}

func newLogMiner() *logMiner {
	return &logMiner{
		clusters: make(map[string]*liveCluster),
		open:     make(map[string]Insight),
		now:      time.Now,
	}
}

func (m *logMiner) Observe(lines []LogLine) {
	if len(lines) == 0 {
		return
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(now)
	for _, line := range lines {
		m.observeOneLocked(line, now)
	}
}

func (m *logMiner) observeOneLocked(line LogLine, now time.Time) {
	tmpl := templateOf(line.Message)
	if tmpl == "" {
		return
	}
	id := line.Source + "|" + tmpl
	c := m.clusters[id]
	if c == nil {
		if len(m.clusters) >= maxClusters {
			m.evictOldestLocked()
		}
		c = &liveCluster{Cluster: Cluster{ID: id, Template: tmpl, Source: line.Source}}
		m.clusters[id] = c
	}
	c.Count++
	m.total++
	if line.Severity == "error" || line.Severity == "fatal" {
		c.ErrorCount++
		m.errors++
	}
	c.LastSeen = line.Timestamp
	if c.LastSeen.IsZero() {
		c.LastSeen = now
	}
	c.Sample = line.Message
	c.times = append(c.times, c.LastSeen)
	cutoff := c.LastSeen.Add(-2 * burstWindow)
	i := 0
	for i < len(c.times) && c.times[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		c.times = append([]time.Time{}, c.times[i:]...)
	}

	recent, previous := c.window(c.LastSeen)
	if recent >= burstMinCount && (previous == 0 || recent >= previous*burstRatio) {
		// The volume rule. It stays the answer until the paging model is
		// beating it on this deployment's own bursts.
		critical := c.ErrorCount > 0 || recent >= burstMinCount*2
		if m.paging != nil {
			if page, ok := m.paging.Burst(id, c.errorShare(), burstRatioOf(recent, previous), critical, now); ok {
				critical = page
			}
		}
		sev := SeverityWarning
		if critical {
			sev = SeverityCritical
		}
		m.open[id] = Insight{
			ID:          "log:" + id,
			Kind:        KindLogBurst,
			Severity:    sev,
			Title:       fmt.Sprintf("log template burst from %s", emptySource(line.Source)),
			Description: fmt.Sprintf("%d lines in the last minute matching %q", recent, tmpl),
			Source:      emptySource(line.Source),
			Value:       float64(recent),
			Time:        now,
			Related:     []string{tmpl},
		}
	}
}

func (m *logMiner) expireLocked(now time.Time) {
	for k, ins := range m.open {
		if now.Sub(ins.Time) > insightTTL {
			delete(m.open, k)
		}
	}
}

func (m *logMiner) evictOldestLocked() {
	var oldestID string
	var oldest time.Time
	first := true
	for id, c := range m.clusters {
		if first || c.LastSeen.Before(oldest) {
			oldestID, oldest, first = id, c.LastSeen, false
		}
	}
	delete(m.clusters, oldestID)
	delete(m.open, oldestID)
}

func (m *logMiner) Insights() []Insight {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(m.now())
	out := make([]Insight, 0, len(m.open))
	for _, ins := range m.open {
		out = append(out, ins)
	}
	sortInsights(out)
	return out
}

func (m *logMiner) counts() (errors, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.errors, m.total
}

func (m *logMiner) Clusters() []Cluster {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	out := make([]Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		cluster := c.Cluster
		if m.paging != nil {
			recent, previous := c.window(now)
			if score, ok := m.paging.Score(c.ID, c.errorShare(), burstRatioOf(recent, previous), now); ok {
				cluster.PagingScore = &score
			}
		}
		out = append(out, cluster)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Template < out[j].Template
	})
	return out
}

// window counts the cluster's lines in the burstWindow ending at `at` and
// in the window before it.
func (c *liveCluster) window(at time.Time) (recent, previous int) {
	split := at.Add(-burstWindow)
	floor := split.Add(-burstWindow)
	for _, ts := range c.times {
		switch {
		case ts.After(at):
		case !ts.Before(split):
			recent++
		case !ts.Before(floor):
			previous++
		}
	}
	return recent, previous
}

// errorShare is the cluster's error lines over all its lines: the paging
// model's first declared input.
func (c *liveCluster) errorShare() float64 {
	if c.Count == 0 {
		return 0
	}
	return float64(c.ErrorCount) / float64(c.Count)
}

// burstRatioOf is the paging model's second declared input, lines this
// window over the window before. A quiet previous window reads as the
// recent count itself, so a template that went from nothing to eight lines
// is as bursty as one that went from one to eight.
func burstRatioOf(recent, previous int) float64 {
	if previous == 0 {
		return float64(recent)
	}
	return float64(recent) / float64(previous)
}

func templateOf(msg string) string {
	s := strings.TrimSpace(msg)
	if s == "" {
		return ""
	}
	s = uuidRe.ReplaceAllString(s, "<*>")
	s = ipv4Re.ReplaceAllString(s, "<*>")
	s = hexRe.ReplaceAllString(s, "<*>")
	s = numRe.ReplaceAllString(s, "<*>")
	return strings.Join(strings.Fields(s), " ")
}

func emptySource(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
