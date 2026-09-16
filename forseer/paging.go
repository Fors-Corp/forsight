package forseer

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"
)

// The paging model answers one question the volume rule cannot: is a burst
// of this log template worth paging for. `log_burst` fires on volume alone,
// so a chatty debug template tripling and an exception template tripling
// are the same insight. This model ranks them by whether bursts of this
// template have, in this deployment, been followed by something that
// mattered.
//
// Labels are self-supervised from the agent's own insight stream: a burst is
// "worth paging" if a critical insight from a different detector — a metric
// anomaly, a slow span, a culprit process — opened within pagingLabelWindow
// after it. Log-burst insights are excluded from both the label and the
// co-occurrence feature, otherwise the model would learn the volume rule back
// from itself.
//
// Method: online logistic regression over three declared features, one
// weight vector per cluster. A cluster's vector starts as a copy of a shared
// vector that trains on every example, so a template's first burst is scored
// by what this deployment has learned about bursts in general, and the
// cluster specialises from there. Predict-then-train, with the twist that the
// label arrives minutes later: a burst registers a pending example with its
// prediction frozen, and grading happens when the window closes or a
// qualifying critical arrives, whichever is first.
const (
	// pagingLabelWindow is how long after a burst a critical insight from
	// another detector still counts as following it.
	pagingLabelWindow = 5 * time.Minute
	// pagingGradeWindow is the number of most recent graded bursts the two
	// accuracies are computed over.
	pagingGradeWindow = 256
	// pagingMinGraded is the number of graded bursts before readiness is
	// even considered; below it the comparison is noise.
	pagingMinGraded = 20
	// pagingWinMargin is how far ahead of the volume rule the model must be
	// to be used, so readiness does not flap on a single burst.
	pagingWinMargin = 0.01
	// pagingLearningRate is the SGD step. The features are all in [0, 1], so
	// one rate serves them all.
	pagingLearningRate = 0.3
	// pagingMaxWeight bounds every weight, so a run of identical labels
	// cannot push a logit past the point where one contrary example matters.
	pagingMaxWeight = 8.0
	// pagingFeatures is bias plus the three declared inputs.
	pagingFeatures = 4
	// pagingShapeCeiling is the burst ratio at which the shape feature
	// saturates: a template that is 32× its previous minute is as bursty as
	// the feature can say.
	pagingShapeCeiling = 32.0
)

type pagingWeights [pagingFeatures]float64

type pagingCluster struct {
	w       pagingWeights
	trained int
	last    time.Time
}

// pagingExample is one burst awaiting its label. At most one per cluster:
// the miner re-fires the burst on every line while the template is bursting,
// and those are the same episode, not new evidence.
type pagingExample struct {
	cluster   string
	features  pagingWeights
	predicted bool
	fallback  bool
	opened    time.Time
}

// criticalMark is a critical insight from a detector other than the log
// miner, kept just long enough to label the bursts around it.
type criticalMark struct {
	id string
	at time.Time
}

// pagingModel is safe for concurrent use: the miner calls Burst on the log
// ingest path while the engine notes insights from the metrics path. It
// never calls back into the miner, so the two locks never nest the other way.
type pagingModel struct {
	mu       sync.Mutex
	shared   pagingWeights
	clusters map[string]*pagingCluster
	pending  map[string]*pagingExample
	marks    []criticalMark
	trained  int

	grades        []bool
	fallbackGrade []bool
	gradePos      int
	graded        int
	hits          int
	fallbackHits  int

	now func() time.Time
}

func newPagingModel() *pagingModel {
	return &pagingModel{
		clusters:      make(map[string]*pagingCluster),
		pending:       make(map[string]*pagingExample),
		grades:        make([]bool, pagingGradeWindow),
		fallbackGrade: make([]bool, pagingGradeWindow),
		now:           time.Now,
	}
}

// pagingFeatureVector is the whole declared input list. errorShare is the
// cluster's error lines over all its lines; ratio is lines in the last
// burstWindow over the window before it; cooccur is whether a critical from
// another detector opened within burstWindow before this moment.
func pagingFeatureVector(errorShare, ratio float64, cooccur bool) pagingWeights {
	shape := 0.0
	if ratio > 0 {
		shape = math.Log1p(ratio) / math.Log1p(pagingShapeCeiling)
	}
	return pagingWeights{
		1,
		clamp01(errorShare),
		clamp01(shape),
		indicator(cooccur),
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func sigmoid(z float64) float64 {
	return 1 / (1 + math.Exp(-z))
}

func (w *pagingWeights) dot(x pagingWeights) float64 {
	var z float64
	for i := range w {
		z += w[i] * x[i]
	}
	return z
}

func (w *pagingWeights) learn(x pagingWeights, label bool) {
	err := indicator(label) - sigmoid(w.dot(x))
	for i := range w {
		w[i] += pagingLearningRate * err * x[i]
		if w[i] > pagingMaxWeight {
			w[i] = pagingMaxWeight
		} else if w[i] < -pagingMaxWeight {
			w[i] = -pagingMaxWeight
		}
	}
}

// NoteInsights records the critical insights other detectors currently hold
// open, then labels any pending burst one of them followed. Log-burst
// insights are ignored on purpose: a burst may never label another burst.
func (m *pagingModel) NoteInsights(insights []Insight) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for _, ins := range insights {
		if ins.Severity != SeverityCritical || ins.Kind == KindLogBurst {
			continue
		}
		if m.hasMarkLocked(ins.ID, ins.Time) {
			continue
		}
		m.marks = append(m.marks, criticalMark{id: ins.ID, at: ins.Time})
	}
	m.expireMarksLocked(now)
	m.resolveLocked(now)
}

func (m *pagingModel) hasMarkLocked(id string, at time.Time) bool {
	for _, mark := range m.marks {
		if mark.id == id && mark.at.Equal(at) {
			return true
		}
	}
	return false
}

// expireMarksLocked drops marks too old to label anything still pending.
func (m *pagingModel) expireMarksLocked(now time.Time) {
	cutoff := now.Add(-(pagingLabelWindow + burstWindow))
	kept := m.marks[:0]
	for _, mark := range m.marks {
		if !mark.at.Before(cutoff) {
			kept = append(kept, mark)
		}
	}
	m.marks = kept
}

// criticalWithinLocked reports whether a critical from another detector
// opened in (from, to].
func (m *pagingModel) criticalWithinLocked(from, to time.Time) bool {
	for _, mark := range m.marks {
		if mark.at.After(from) && !mark.at.After(to) {
			return true
		}
	}
	return false
}

// resolveLocked grades and trains every pending burst whose label is now
// known: true if a critical from another detector followed it inside the
// window, false once the window has closed with none.
func (m *pagingModel) resolveLocked(now time.Time) {
	for id, ex := range m.pending {
		deadline := ex.opened.Add(pagingLabelWindow)
		followed := m.criticalWithinLocked(ex.opened, deadline)
		if !followed && !now.After(deadline) {
			continue
		}
		delete(m.pending, id)
		m.gradeLocked(ex.predicted == followed, ex.fallback == followed)
		m.shared.learn(ex.features, followed)
		if c := m.clusters[id]; c != nil {
			c.w.learn(ex.features, followed)
			c.trained++
			c.last = now
		}
		m.trained++
	}
}

// clusterLocked returns the cluster's weights, creating them from the shared
// vector on first sight. The map is bounded by maxClusters, the same bound
// the miner keeps, evicting the least recently trained.
func (m *pagingModel) clusterLocked(id string, now time.Time) *pagingCluster {
	c := m.clusters[id]
	if c != nil {
		return c
	}
	if len(m.clusters) >= maxClusters {
		var oldestID string
		var oldest time.Time
		first := true
		for cid, cc := range m.clusters {
			if first || cc.last.Before(oldest) {
				oldestID, oldest, first = cid, cc.last, false
			}
		}
		delete(m.clusters, oldestID)
		delete(m.pending, oldestID)
	}
	c = &pagingCluster{w: m.shared, last: now}
	m.clusters[id] = c
	return c
}

// Burst is called by the miner when a template bursts. It returns the
// model's answer and whether that answer should be used: false while the
// model is not yet beating the volume rule, in which case the caller keeps
// fallbackCritical. Either way the burst is registered for grading, one
// example per cluster per episode. The miner re-fires on every line while
// the template keeps bursting and both verdicts move as it does — the volume
// rule says warning at eight lines and critical at sixteen — so the example
// carries the latest verdict of each, which is what the operator saw, not
// the first.
func (m *pagingModel) Burst(cluster string, errorShare, ratio float64, fallbackCritical bool, at time.Time) (critical, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolveLocked(at)
	x := pagingFeatureVector(errorShare, ratio, m.criticalWithinLocked(at.Add(-burstWindow), at))
	c := m.clusterLocked(cluster, at)
	p := sigmoid(c.w.dot(x))
	predicted := p >= 0.5
	if ex := m.pending[cluster]; ex != nil {
		ex.features, ex.predicted, ex.fallback = x, predicted, fallbackCritical
	} else {
		m.pending[cluster] = &pagingExample{
			cluster:   cluster,
			features:  x,
			predicted: predicted,
			fallback:  fallbackCritical,
			opened:    at,
		}
	}
	return predicted, m.readyLocked()
}

// Score is the model's probability that a burst of this template right now
// is worth paging for, or false while the model is not ready.
func (m *pagingModel) Score(cluster string, errorShare, ratio float64, at time.Time) (float64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.readyLocked() {
		return 0, false
	}
	x := pagingFeatureVector(errorShare, ratio, m.criticalWithinLocked(at.Add(-burstWindow), at))
	w := m.shared
	if c := m.clusters[cluster]; c != nil {
		w = c.w
	}
	return sigmoid(w.dot(x)), true
}

func (m *pagingModel) gradeLocked(hit, fallbackHit bool) {
	if m.graded == len(m.grades) {
		if m.grades[m.gradePos] {
			m.hits--
		}
		if m.fallbackGrade[m.gradePos] {
			m.fallbackHits--
		}
	} else {
		m.graded++
	}
	m.grades[m.gradePos] = hit
	if hit {
		m.hits++
	}
	m.fallbackGrade[m.gradePos] = fallbackHit
	if fallbackHit {
		m.fallbackHits++
	}
	m.gradePos = (m.gradePos + 1) % len(m.grades)
}

func (m *pagingModel) accuracyLocked() (model, fallback float64) {
	if m.graded == 0 {
		return Unmeasured, Unmeasured
	}
	return float64(m.hits) / float64(m.graded), float64(m.fallbackHits) / float64(m.graded)
}

// readyLocked: enough graded bursts, and ahead of the volume rule on them.
func (m *pagingModel) readyLocked() bool {
	if m.graded < pagingMinGraded {
		return false
	}
	model, fallback := m.accuracyLocked()
	return model >= fallback+pagingWinMargin
}

// Card implements Model.
func (m *pagingModel) Card() Card {
	m.mu.Lock()
	defer m.mu.Unlock()
	accuracy, fallbackAccuracy := m.accuracyLocked()
	return Card{
		Name: "log burst paging",
		Job:  "Decide whether a burst of this log template is worth paging for.",
		Reads: []string{
			"a template's severity mix (error lines over all its lines)",
			"its burst shape (lines this minute over the minute before)",
			"whether a critical insight from another detector opened within the last minute",
		},
		Fallback:         "the volume rule: critical when the template has any error line or reaches 16 lines a minute",
		Ready:            m.readyLocked(),
		Trained:          m.trained,
		Accuracy:         accuracy,
		FallbackAccuracy: fallbackAccuracy,
		Graded:           m.graded,
		Detail: fmt.Sprintf("%d bursts awaiting their label, %d templates with their own weights",
			len(m.pending), len(m.clusters)),
	}
}

// pagingSnapshotVersion is this model's own schema version — see
// severitySnapshotVersion's comment for what that guards against.
const pagingSnapshotVersion = 1

// pagingSnapshot is Snapshot's JSON payload: the shared weight vector, every
// cluster's own specialised weights, and the trained count — the logistic
// model itself. The prequential grading window and the in-flight
// pending/marks bookkeeping are deliberately absent — see Restore.
type pagingSnapshot struct {
	Version  int                              `json:"version"`
	Shared   pagingWeights                    `json:"shared"`
	Clusters map[string]pagingClusterSnapshot `json:"clusters"`
	Trained  int                              `json:"trained"`
}

type pagingClusterSnapshot struct {
	W       pagingWeights `json:"w"`
	Trained int           `json:"trained"`
	Last    time.Time     `json:"last"`
}

// Snapshot implements Model.
func (m *pagingModel) Snapshot() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := pagingSnapshot{
		Version:  pagingSnapshotVersion,
		Shared:   m.shared,
		Clusters: make(map[string]pagingClusterSnapshot, len(m.clusters)),
		Trained:  m.trained,
	}
	for id, c := range m.clusters {
		snap.Clusters[id] = pagingClusterSnapshot{W: c.w, Trained: c.trained, Last: c.last}
	}
	return json.Marshal(snap)
}

// Restore implements Model. The shared weight vector, every cluster's own
// specialised weights, and the trained count all come back — that is the
// logistic model itself. Bounded the same way clusterLocked keeps it
// bounded live: a snapshot with more than maxClusters entries is truncated
// on the way in, never grown past the cap the running model itself never
// exceeds.
//
// What does not come back:
//   - The prequential grading window (grades, fallbackGrade, gradePos,
//     graded, hits, fallbackHits) that gates readiness — readyLocked needs
//     pagingMinGraded graded bursts still ahead of the volume rule.
//     Resetting it means a restarted agent starts back at "not yet ready"
//     and has to beat the volume rule again on live bursts before the model
//     is trusted, the same fallback contract a freshly started model has to
//     earn.
//   - pending and marks: a burst awaiting its label, and the recent
//     critical insights that would label it, are both keyed to real
//     wall-clock time against a live, continuous insight stream. A gap of
//     unknown length sits between the shutdown that wrote this snapshot and
//     the startup that reads it, so neither can be resumed honestly; they
//     start empty, and a burst genuinely still in flight when the agent
//     stopped is not carried over half-labelled.
func (m *pagingModel) Restore(data []byte) error {
	var snap pagingSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("paging snapshot: %w", err)
	}
	if snap.Version != pagingSnapshotVersion {
		return fmt.Errorf("paging snapshot version %d, want %d", snap.Version, pagingSnapshotVersion)
	}
	clusters := make(map[string]*pagingCluster, len(snap.Clusters))
	for id, c := range snap.Clusters {
		if len(clusters) >= maxClusters {
			break
		}
		clusters[id] = &pagingCluster{w: c.W, trained: c.Trained, last: c.Last}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shared = snap.Shared
	m.clusters = clusters
	m.trained = snap.Trained
	return nil
}
