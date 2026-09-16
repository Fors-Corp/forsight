package forseer

import (
	"bytes"
	"testing"
	"time"
)

// pagingClock is a settable clock shared by a model under test.
type pagingClock struct{ t time.Time }

func (c *pagingClock) now() time.Time          { return c.t }
func (c *pagingClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestPagingModel() (*pagingModel, *pagingClock) {
	clock := &pagingClock{t: time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)}
	m := newPagingModel()
	m.now = clock.now
	return m, clock
}

func criticalAnomaly(id string, at time.Time) Insight {
	return Insight{ID: id, Kind: KindAnomaly, Severity: SeverityCritical, Time: at}
}

// runEpisode bursts one cluster, optionally has a critical from another
// detector follow a minute later, then closes the label window.
func runEpisode(m *pagingModel, clock *pagingClock, cluster string, errorShare float64, fallbackCritical, followed bool) {
	m.Burst(cluster, errorShare, 4, fallbackCritical, clock.now())
	clock.advance(time.Minute)
	if followed {
		m.NoteInsights([]Insight{criticalAnomaly("anomaly:"+cluster+clock.now().String(), clock.now())})
	}
	clock.advance(pagingLabelWindow)
	m.NoteInsights(nil)
}

func TestPagingModel_LearnsWhichTemplatesMatterAndBeatsTheVolumeRule(t *testing.T) {
	m, clock := newTestPagingModel()

	// Two templates the volume rule cannot tell apart: both cross 16 lines a
	// minute, so it calls both critical. Only the exception template is ever
	// followed by a critical from another detector.
	for i := 0; i < 40; i++ {
		runEpisode(m, clock, "api|timeout talking to <*>", 0.6, true, true)
		runEpisode(m, clock, "web|debug request <*> served", 0, true, false)
	}

	card := m.Card()
	if !card.Ready {
		t.Fatalf("model not ready after 80 graded bursts: %+v", card)
	}
	if card.Graded != pagingGradeWindow && card.Graded != 80 {
		t.Errorf("graded = %d, want 80", card.Graded)
	}
	if card.Accuracy <= card.FallbackAccuracy {
		t.Errorf("model accuracy %.2f is not ahead of the volume rule %.2f", card.Accuracy, card.FallbackAccuracy)
	}
	// The volume rule is right on every exception burst and wrong on every
	// debug burst: exactly one half.
	if card.FallbackAccuracy < 0.45 || card.FallbackAccuracy > 0.55 {
		t.Errorf("fallback accuracy = %.2f, want about 0.5", card.FallbackAccuracy)
	}
	if card.Accuracy < 0.85 {
		t.Errorf("model accuracy = %.2f, want at least 0.85", card.Accuracy)
	}

	now := clock.now()
	page, ok := m.Score("api|timeout talking to <*>", 0.6, 4, now)
	if !ok || page < 0.8 {
		t.Errorf("exception template scores %.2f (ok=%v), want >= 0.8", page, ok)
	}
	noise, ok := m.Score("web|debug request <*> served", 0, 4, now)
	if !ok || noise > 0.2 {
		t.Errorf("debug template scores %.2f (ok=%v), want <= 0.2", noise, ok)
	}
	if critical, ok := m.Burst("web|debug request <*> served", 0, 4, true, now); !ok || critical {
		t.Errorf("a ready model still calls the debug burst critical (critical=%v ok=%v)", critical, ok)
	}
	t.Logf("model %.3f vs volume rule %.3f over %d bursts; exception %.2f, debug %.2f",
		card.Accuracy, card.FallbackAccuracy, card.Graded, page, noise)
}

func TestPagingModel_NotReadyUntilGradedAndAhead(t *testing.T) {
	m, clock := newTestPagingModel()
	if _, ok := m.Burst("api|x", 0.5, 4, true, clock.now()); ok {
		t.Fatal("a cold model reported ready")
	}
	if _, ok := m.Score("api|x", 0.5, 4, clock.now()); ok {
		t.Fatal("a cold model scored")
	}
	card := m.Card()
	if card.Accuracy != Unmeasured || card.FallbackAccuracy != Unmeasured {
		t.Errorf("cold card reports accuracy %.2f / %.2f, want Unmeasured", card.Accuracy, card.FallbackAccuracy)
	}
	// Bursts the volume rule gets right every time: the model can tie, never
	// win, so it must never stand in for the rule.
	for i := 0; i < pagingMinGraded+5; i++ {
		runEpisode(m, clock, "api|x", 0.5, true, true)
	}
	if m.Card().Ready {
		t.Error("model ready while merely tying the volume rule")
	}
}

func TestPagingModel_ALogBurstNeverLabelsAnotherBurst(t *testing.T) {
	m, clock := newTestPagingModel()
	m.Burst("api|x", 0.5, 4, true, clock.now())
	clock.advance(time.Minute)
	// Its own escalation, and another template's burst, both critical.
	m.NoteInsights([]Insight{
		{ID: "log:api|x", Kind: KindLogBurst, Severity: SeverityCritical, Time: clock.now()},
		{ID: "log:web|y", Kind: KindLogBurst, Severity: SeverityCritical, Time: clock.now()},
	})
	if len(m.pending) != 1 {
		t.Fatalf("a log burst labelled the pending burst: pending=%d", len(m.pending))
	}
	clock.advance(pagingLabelWindow)
	m.NoteInsights(nil)
	if len(m.pending) != 0 || m.graded != 1 {
		t.Fatalf("window close did not grade: pending=%d graded=%d", len(m.pending), m.graded)
	}
	// Graded as "not followed": the volume rule said critical, so it missed.
	if m.fallbackHits != 0 {
		t.Errorf("fallback scored a hit on an unfollowed burst")
	}
}

func TestPagingModel_OneExamplePerEpisode(t *testing.T) {
	m, clock := newTestPagingModel()
	for i := 0; i < 50; i++ {
		m.Burst("api|x", 0.5, float64(4+i), true, clock.now())
		clock.advance(time.Second)
	}
	if len(m.pending) != 1 {
		t.Fatalf("re-fired burst registered %d examples, want 1", len(m.pending))
	}
	clock.advance(pagingLabelWindow)
	m.NoteInsights(nil)
	if m.graded != 1 {
		t.Errorf("graded = %d, want 1", m.graded)
	}
	// A new episode after resolution is new evidence.
	m.Burst("api|x", 0.5, 4, true, clock.now())
	if len(m.pending) != 1 {
		t.Errorf("new episode not registered")
	}
}

func TestPagingModel_BoundsItsState(t *testing.T) {
	m, clock := newTestPagingModel()
	for i := 0; i < maxClusters+50; i++ {
		m.Burst("src|template "+time.Duration(i).String(), 0.1, 4, false, clock.now())
		clock.advance(time.Second)
	}
	if len(m.clusters) > maxClusters {
		t.Errorf("clusters = %d, want <= %d", len(m.clusters), maxClusters)
	}
	if len(m.pending) > maxClusters {
		t.Errorf("pending = %d, want <= %d", len(m.pending), maxClusters)
	}
	clock.advance(pagingLabelWindow + burstWindow + time.Second)
	m.NoteInsights([]Insight{criticalAnomaly("old", clock.now().Add(-time.Hour))})
	if len(m.marks) != 0 {
		t.Errorf("an hour-old critical was kept: %d marks", len(m.marks))
	}
}

func TestPagingModel_CooccurrenceIsAFeatureOnlyBeforeTheBurst(t *testing.T) {
	m, clock := newTestPagingModel()
	m.NoteInsights([]Insight{criticalAnomaly("a", clock.now().Add(-30*time.Second))})
	x := pagingFeatureVector(0, 4, m.criticalWithinLocked(clock.now().Add(-burstWindow), clock.now()))
	if x[3] != 1 {
		t.Errorf("critical 30s before the burst not seen as co-occurring: %v", x)
	}
	m.NoteInsights([]Insight{criticalAnomaly("b", clock.now().Add(-2*time.Minute))})
	x = pagingFeatureVector(0, 4, m.criticalWithinLocked(clock.now().Add(-burstWindow), clock.now()))
	if x[3] != 1 {
		t.Errorf("the 30s-old critical should still count: %v", x)
	}
	if x[2] <= 0 || x[2] > 1 || x[1] != 0 {
		t.Errorf("shape/severity features out of range: %v", x)
	}
	if far := pagingFeatureVector(2, 1000, false); far[1] != 1 || far[2] != 1 {
		t.Errorf("features not clamped to [0,1]: %v", far)
	}
}

// TestPagingModel_SnapshotRestoreRoundTrip is roadmap item 25's proof for
// this model: the shared and per-cluster weights come back exactly, but the
// grading window and the in-flight pending/marks bookkeeping do not.
func TestPagingModel_SnapshotRestoreRoundTrip(t *testing.T) {
	m, clock := newTestPagingModel()
	for i := 0; i < 40; i++ {
		runEpisode(m, clock, "api|timeout talking to <*>", 0.6, true, true)
		runEpisode(m, clock, "web|debug request <*> served", 0, true, false)
	}
	wantTrained := m.trained
	wantShared := m.shared
	wantClusterW := make(map[string]pagingWeights, len(m.clusters))
	for id, c := range m.clusters {
		wantClusterW[id] = c.w
	}

	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := newPagingModel()
	if err := restored.Restore(data); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.trained != wantTrained {
		t.Fatalf("restored trained = %d, want %d", restored.trained, wantTrained)
	}
	if restored.shared != wantShared {
		t.Fatalf("restored shared weights = %v, want %v", restored.shared, wantShared)
	}
	if len(restored.clusters) != len(wantClusterW) {
		t.Fatalf("restored %d clusters, want %d", len(restored.clusters), len(wantClusterW))
	}
	for id, w := range wantClusterW {
		c, ok := restored.clusters[id]
		if !ok || c.w != w {
			t.Errorf("cluster %q weights = %+v (ok=%v), want %v", id, c, ok, w)
		}
	}

	// The prequential grading window gates readiness, and the in-flight
	// pending/marks bookkeeping is tied to real wall-clock time against a
	// live stream — both reset, so a restarted agent re-earns readiness on
	// live bursts rather than opening already trusted.
	if restored.graded != 0 || restored.hits != 0 || len(restored.pending) != 0 || len(restored.marks) != 0 {
		t.Fatalf("restored model carries live state: graded=%d hits=%d pending=%d marks=%d",
			restored.graded, restored.hits, len(restored.pending), len(restored.marks))
	}
	if restored.Card().Ready {
		t.Fatal("restored model reports ready before earning it on live bursts")
	}
}

func TestPagingModel_RestoreDiscardsAVersionMismatch(t *testing.T) {
	m, clock := newTestPagingModel()
	runEpisode(m, clock, "api|x", 0.5, true, true)
	data, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1)

	fresh := newPagingModel()
	if err := fresh.Restore(data); err == nil {
		t.Fatal("Restore accepted a payload with the wrong schema version")
	}
	if fresh.trained != 0 || len(fresh.clusters) != 0 {
		t.Fatalf("a discarded restore left trained=%d clusters=%d, want 0/0", fresh.trained, len(fresh.clusters))
	}
}

func TestPagingModel_RestoreDiscardsCorruptJSON(t *testing.T) {
	m := newPagingModel()
	if err := m.Restore([]byte("{not json")); err == nil {
		t.Fatal("Restore accepted corrupt JSON")
	}
	if m.trained != 0 || len(m.clusters) != 0 {
		t.Fatalf("a discarded restore left trained=%d clusters=%d, want 0/0", m.trained, len(m.clusters))
	}
}
