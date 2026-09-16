package forseer

import "testing"

func TestCulpritModel_ColdProcessFallsBackToTheFloor(t *testing.T) {
	m := newCulpritModel()
	snap := procSnap{name: "rogue", cpu: 80}

	score, changeBased, metric, raw, ok := m.rank(snap)
	if !ok {
		t.Fatal("a cold process with no baseline should still be scorable via the floor")
	}
	if changeBased {
		t.Error("a cold process (no baseline) was scored as change-based")
	}
	if metric != "process.cpu.percent" || raw != 80 || score != 80 {
		t.Errorf("got metric=%q raw=%v score=%v, want the raw cpu reading (80) all three ways", metric, raw, score)
	}
}

func TestCulpritModel_WarmedProcessThatRoseIsChangeBased(t *testing.T) {
	m := newCulpritModel()
	snap := procSnap{name: "mover", cpu: 18, cpuZ: 3.2, cpuReady: true}

	score, changeBased, metric, raw, ok := m.rank(snap)
	if !ok || !changeBased {
		t.Fatalf("a warmed process that rose above its own baseline should be change-based: ok=%v changeBased=%v", ok, changeBased)
	}
	if score != 3.2 {
		t.Errorf("score = %v, want the cpu z-score 3.2", score)
	}
	if metric != "process.cpu.percent" || raw != 18 {
		t.Errorf("metric=%q raw=%v, want process.cpu.percent/18", metric, raw)
	}
}

// The whole point of the item: a warmed process sitting at or below its own
// normal is excluded outright, never re-scored by its raw value — that is
// exactly the "always sits at 22%" case the raw-CPU rule used to promote
// over a process that actually moved.
func TestCulpritModel_WarmedProcessAtItsOwnBaselineIsExcluded(t *testing.T) {
	m := newCulpritModel()
	snap := procSnap{name: "steady", cpu: 22, cpuZ: 0.02, cpuReady: true}

	_, _, _, _, ok := m.rank(snap)
	if ok {
		t.Fatal("a process at its own normal was still returned as a candidate")
	}
}

func TestCulpritModel_RssCanOutrankCpu(t *testing.T) {
	m := newCulpritModel()
	snap := procSnap{
		name: "leaker", cpu: 5, cpuZ: 0.5, cpuReady: true,
		rss: 9e9, rssZ: 6.0, rssReady: true,
	}

	score, changeBased, metric, raw, ok := m.rank(snap)
	if !ok || !changeBased {
		t.Fatalf("expected a change-based candidate, got ok=%v changeBased=%v", ok, changeBased)
	}
	if metric != "process.memory.rss_bytes" || raw != 9e9 || score != 6.0 {
		t.Errorf("got metric=%q raw=%v score=%v, want rss to win (its z is larger)", metric, raw, score)
	}
}

func TestCulpritModel_Card(t *testing.T) {
	m := newCulpritModel()

	card := m.Card()
	if card.Ready {
		t.Error("a model that has never ranked anything reported itself ready")
	}
	if card.Accuracy != Unmeasured || card.FallbackAccuracy != Unmeasured {
		t.Errorf("Accuracy/FallbackAccuracy = %v/%v, want Unmeasured (no label exists yet)", card.Accuracy, card.FallbackAccuracy)
	}
	if card.Fallback == "" || len(card.Reads) == 0 || card.Job == "" || card.Name == "" {
		t.Errorf("card does not fully describe itself: %+v", card)
	}

	m.rank(procSnap{name: "mover", cpu: 18, cpuZ: 3.2, cpuReady: true})
	if card = m.Card(); !card.Ready {
		t.Error("a model that scored a process via its own baseline should report itself ready")
	}
}
