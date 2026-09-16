package forseer

import (
	"strings"
	"testing"
	"time"
)

var incidentBase = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)

func TestGroupIncidents_RelatedInsightsGroup(t *testing.T) {
	a := Insight{ID: "a", Kind: KindAnomaly, Severity: SeverityWarning, Title: "host.cpu.percent is 4.1σ from its baseline", Source: "forseer", Time: incidentBase, Related: []string{"payment-service"}}
	b := Insight{ID: "b", Kind: KindSlowSpan, Severity: SeverityCritical, Title: "checkout is slower than its own p99", Source: "envoy", Time: incidentBase.Add(2 * time.Minute), Related: []string{"payment-service"}}

	out := groupIncidents([]Insight{a, b})
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1 (a and b share Related \"payment-service\" within the window): %+v", len(out), out)
	}
	ev := out[0]
	if !strings.Contains(ev.Title, "2") || !strings.Contains(ev.Title, a.Title) {
		t.Fatalf("Title = %q, want the count and the earliest cause (%q)", ev.Title, a.Title)
	}
	if ev.Tone != "danger" {
		t.Fatalf("Tone = %q, want %q (b is critical, the most severe member)", ev.Tone, "danger")
	}
	// newest-last: a (earlier) before b (later) in the description.
	if i, j := strings.Index(ev.Description, a.Title), strings.Index(ev.Description, b.Title); i < 0 || j < 0 || i > j {
		t.Fatalf("Description = %q, want %q before %q (oldest first, newest last)", ev.Description, a.Title, b.Title)
	}
	if ev.Time != b.Time {
		t.Fatalf("Time = %v, want the latest member's time %v", ev.Time, b.Time)
	}
}

func TestGroupIncidents_UnrelatedInsightsInTheSameWindowDoNotGroup(t *testing.T) {
	c := Insight{ID: "c", Kind: KindAnomaly, Severity: SeverityWarning, Title: "host.memory.percent is 3.5σ from its baseline", Source: "forseer", Time: incidentBase}
	d := Insight{ID: "d", Kind: KindLogBurst, Severity: SeverityWarning, Title: "template X burst", Source: "checkout", Time: incidentBase.Add(time.Minute), Related: []string{"template-x"}}

	out := groupIncidents([]Insight{c, d})
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (different Source, no shared Related): %+v", len(out), out)
	}
	ids := map[string]bool{out[0].ID: true, out[1].ID: true}
	if !ids["evt-c"] || !ids["evt-d"] {
		t.Fatalf("event ids = %v, want evt-c and evt-d unchanged", ids)
	}
}

// The Detector stamps every metric finding with the literal Source
// "forseer"; two of them in the same window about different series are not
// one incident unless a Related value says so.
func TestGroupIncidents_TheDetectorFamilySourceIsNotALink(t *testing.T) {
	cpu := Insight{ID: "cpu", Kind: KindAnomaly, Severity: SeverityWarning, Title: "host.cpu.percent is 4.1σ from its baseline", Source: "forseer", Time: incidentBase, Related: []string{"host.cpu.percent"}}
	disk := Insight{ID: "disk", Kind: KindChangepoint, Severity: SeverityWarning, Title: "host.disk.percent shifted", Source: "forseer", Time: incidentBase.Add(time.Minute), Related: []string{"host.disk.percent"}}
	events := groupIncidents([]Insight{cpu, disk})
	if len(events) != 2 {
		t.Fatalf("two unrelated detector findings grouped into %d event(s): %+v", len(events), events)
	}
	// A specific source still links: two findings from the same service.
	x := Insight{ID: "x", Kind: KindSlowSpan, Severity: SeverityWarning, Title: "X", Source: "checkout", Time: incidentBase}
	y := Insight{ID: "y", Kind: KindLogBurst, Severity: SeverityWarning, Title: "Y", Source: "checkout", Time: incidentBase.Add(time.Minute)}
	if events := groupIncidents([]Insight{x, y}); len(events) != 1 {
		t.Fatalf("two findings from the same service did not group: %+v", events)
	}
}

func TestGroupIncidents_OutsideTheWindowDoesNotGroupDespiteSharedRelated(t *testing.T) {
	a := Insight{ID: "a", Kind: KindAnomaly, Severity: SeverityWarning, Title: "A", Source: "forseer", Time: incidentBase, Related: []string{"payment-service"}}
	b := Insight{ID: "b", Kind: KindSlowSpan, Severity: SeverityCritical, Title: "B", Source: "envoy", Time: incidentBase.Add(11 * time.Minute), Related: []string{"payment-service"}}

	out := groupIncidents([]Insight{a, b})
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 (11 minutes apart is outside incidentWindow): %+v", len(out), out)
	}
}

func TestGroupIncidents_ChainGroupsByTransitiveRelated(t *testing.T) {
	e := Insight{ID: "e", Kind: KindAnomaly, Severity: SeverityInfo, Title: "E", Source: "forseer", Time: incidentBase, Related: []string{"x"}}
	f := Insight{ID: "f", Kind: KindChangepoint, Severity: SeverityWarning, Title: "F", Source: "forseer2", Time: incidentBase.Add(1 * time.Minute), Related: []string{"x", "y"}}
	g := Insight{ID: "g", Kind: KindCulprit, Severity: SeverityInfo, Title: "G", Source: "worker-3", Time: incidentBase.Add(2 * time.Minute), Related: []string{"y"}}

	// e and g share nothing directly (e has "x", g has "y") and have
	// different Sources, but f links to both, so all three land in one
	// incident.
	out := groupIncidents([]Insight{e, f, g})
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1 (e-f-g chained by Related): %+v", len(out), out)
	}
	ev := out[0]
	if !strings.Contains(ev.Title, "3") {
		t.Fatalf("Title = %q, want it to name the count 3", ev.Title)
	}
	for _, title := range []string{"E", "F", "G"} {
		if !strings.Contains(ev.Description, title) {
			t.Fatalf("Description = %q, missing member %q", ev.Description, title)
		}
	}
}

func TestGroupIncidents_OrderingIsStable(t *testing.T) {
	a := Insight{ID: "a", Kind: KindAnomaly, Severity: SeverityWarning, Title: "A", Source: "forseer", Time: incidentBase, Related: []string{"svc"}}
	b := Insight{ID: "b", Kind: KindSlowSpan, Severity: SeverityCritical, Title: "B", Source: "envoy", Time: incidentBase.Add(1 * time.Minute), Related: []string{"svc"}}
	c := Insight{ID: "c", Kind: KindCulprit, Severity: SeverityWarning, Title: "C", Source: "worker", Time: incidentBase.Add(2 * time.Minute), Related: []string{"svc"}}

	forward := groupIncidents([]Insight{a, b, c})
	reversed := groupIncidents([]Insight{c, b, a})
	shuffled := groupIncidents([]Insight{b, a, c})

	for _, out := range [][]Event{forward, reversed, shuffled} {
		if len(out) != 1 {
			t.Fatalf("len(out) = %d, want 1 regardless of input order: %+v", len(out), out)
		}
		if out[0] != forward[0] {
			t.Fatalf("got %+v, want the same event as the forward order %+v regardless of input order", out[0], forward[0])
		}
	}
}

func TestGroupIncidents_UngroupedInsightPassesThroughUnchanged(t *testing.T) {
	ins := Insight{ID: "solo", Kind: KindHostOutlier, Severity: SeverityCritical, Title: "host looks unusual", Source: "forseer", Time: incidentBase, Related: []string{"host.cpu.percent"}}

	out := groupIncidents([]Insight{ins})
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	want := storyEvent(ins)
	if out[0] != want {
		t.Fatalf("got %+v, want the unchanged storyEvent rendering %+v", out[0], want)
	}
}

// TestEngine_StoryGroupsRelatedCulprits wires groupIncidents through
// Engine.Story end to end: two culprit insights on different processes
// that both name the same host metric in Related land in one incident.
func TestEngine_StoryGroupsRelatedCulprits(t *testing.T) {
	e := NewEngine()
	now := incidentBase
	e.now = func() time.Time { return now }
	e.mu.Lock()
	e.culprits["1"] = Insight{
		ID: "culprit:1", Kind: KindCulprit, Severity: SeverityWarning,
		Title:  "api's cpu rose 2.0σ above its own baseline while the host is anomalous",
		Source: "api", Time: now, Related: []string{"api", "pid=1"},
	}
	e.culprits["2"] = Insight{
		ID: "culprit:2", Kind: KindCulprit, Severity: SeverityCritical,
		Title:  "worker's cpu rose 3.0σ above its own baseline while the host is anomalous",
		Source: "worker", Time: now.Add(time.Minute), Related: []string{"worker", "pid=1"},
	}
	e.mu.Unlock()

	story := e.Story()
	if len(story) != 1 {
		t.Fatalf("len(story) = %d, want 1 (both culprits share Related pid=1): %+v", len(story), story)
	}
	if story[0].Tone != "danger" {
		t.Fatalf("Tone = %q, want %q", story[0].Tone, "danger")
	}
}
