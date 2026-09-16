package forseer

import (
	"strings"
	"testing"
	"time"
)

func TestTemplateOf_StripsIDsAndNumbers(t *testing.T) {
	got := templateOf("request 550e8400-e29b-41d4-a716-446655440000 from 10.0.0.4 took 12.4ms")
	if !strings.Contains(got, "<*>") {
		t.Fatalf("expected placeholders, got %q", got)
	}
	if strings.Contains(got, "10.0.0.4") || strings.Contains(got, "550e8400") {
		t.Fatalf("identifiers leaked into template: %q", got)
	}
}

func TestLogMiner_ClustersSameTemplate(t *testing.T) {
	m := newLogMiner()
	m.Observe([]LogLine{
		{Message: "user 1 login failed", Source: "api", Severity: "error"},
		{Message: "user 99 login failed", Source: "api", Severity: "error"},
		{Message: "user 7 login failed", Source: "api", Severity: "warn"},
	})
	got := m.Clusters()
	if len(got) != 1 {
		t.Fatalf("got %d clusters, want 1: %+v", len(got), got)
	}
	if got[0].Count != 3 {
		t.Errorf("count = %d, want 3", got[0].Count)
	}
	if got[0].ErrorCount != 2 {
		t.Errorf("errorCount = %d, want 2", got[0].ErrorCount)
	}
}

func TestLogMiner_BurstOpensInsight(t *testing.T) {
	m := newLogMiner()
	fixed := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	lines := make([]LogLine, 0, burstMinCount)
	for i := 0; i < burstMinCount; i++ {
		lines = append(lines, LogLine{
			Timestamp: fixed,
			Source:    "api",
			Severity:  "error",
			Message:   "timeout talking to payments",
		})
	}
	m.Observe(lines)
	got := m.Insights()
	if len(got) != 1 {
		t.Fatalf("got %d insights, want 1: %+v", len(got), got)
	}
	if got[0].Kind != KindLogBurst {
		t.Errorf("kind = %q, want %s", got[0].Kind, KindLogBurst)
	}
}

// A miner with a ready paging model lets the model decide a burst's
// severity; before that, and without one, the volume rule decides.
func TestLogMiner_BurstSeverityFollowsThePagingModelOnceReady(t *testing.T) {
	clock := &pagingClock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	m := newLogMiner()
	m.now = clock.now
	paging := newPagingModel()
	paging.now = clock.now
	m.paging = paging

	// Sixteen debug lines a minute: the volume rule calls it critical.
	burst := func() {
		lines := make([]LogLine, 0, burstMinCount*2)
		for i := 0; i < burstMinCount*2; i++ {
			lines = append(lines, LogLine{Timestamp: clock.now(), Source: "web", Severity: "debug", Message: "request served"})
		}
		m.Observe(lines)
	}
	burst()
	if got := m.Insights(); len(got) != 1 || got[0].Severity != SeverityCritical {
		t.Fatalf("cold model: want the volume rule's critical, got %+v", got)
	}
	if clusters := m.Clusters(); clusters[0].PagingScore != nil {
		t.Fatalf("cold model scored a cluster: %v", *clusters[0].PagingScore)
	}

	// Teach the model that this deployment's debug bursts are never followed
	// by anything, and that error bursts always are. The label is time
	// based — a critical credits every burst inside the window before it —
	// so the two must not share a window, or the debug bursts would earn the
	// exception bursts' credit. That is the semantics, not a flaw: a
	// template that bursts during incidents is worth paging.
	for i := 0; i < 40; i++ {
		clock.advance(3 * burstWindow)
		burst()
		clock.advance(pagingLabelWindow + burstWindow)
		paging.NoteInsights(nil)
		runEpisode(paging, clock, "api|timeout talking to <*>", 0.6, true, true)
	}
	if !paging.Card().Ready {
		t.Fatalf("model not ready: %+v", paging.Card())
	}
	clock.advance(3 * burstWindow)
	burst()
	got := m.Insights()
	if len(got) != 1 || got[0].Severity != SeverityWarning {
		t.Fatalf("ready model: want the debug burst downgraded to warning, got %+v", got)
	}
	clusters := m.Clusters()
	if clusters[0].PagingScore == nil || *clusters[0].PagingScore > 0.2 {
		t.Errorf("debug cluster score = %v, want <= 0.2", clusters[0].PagingScore)
	}
	card := paging.Card()
	t.Logf("through the miner: model %.3f vs volume rule %.3f over %d bursts; debug template scores %.2f",
		card.Accuracy, card.FallbackAccuracy, card.Graded, *clusters[0].PagingScore)
}
