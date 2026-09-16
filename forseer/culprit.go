package forseer

import (
	"fmt"
	"sync"
)

// The culprit ranker answers "which process" once the host itself is
// anomalous. It used to sort candidates by raw `process.cpu.percent`, keep
// the top three, and drop anything under a fixed 20% floor — which drops a
// process that jumped from 2% to 18% during the very spike being explained,
// in favour of one that always idles at 22% and is doing nothing unusual at
// all. Raw usage cannot tell "always like this" from "just like this".
//
// Change can: the Detector already keeps a rolling Welford mean/variance for
// every process.cpu.percent and process.memory.rss_bytes series, as a side
// effect of running anomaly detection on them, so how far a process has
// risen above its own baseline is free to ask for. Once a process's own
// series has minSamples of history, that answer replaces the raw floor for
// it outright — a process sitting exactly at its own normal is not a
// culprit merely for having a high normal — and the floor stays only as the
// named fallback for a process too new to have a baseline yet.
//
// This is the deterministic half of the contract. The label the follow-up
// needs — "this process's own series fell back toward normal as the host
// anomaly closed" — is self-supervised and has to be watched for a while
// before a logistic weight over these same features can be trained and
// graded against this rule; a jump that merely preceded the host recovering
// is a proxy for causation, not causation, and that card will need to say
// so. Until then there is no label, so Accuracy stays Unmeasured rather than
// borrowing a number from a different job.
const (
	// cpuCulpritFloor is the raw CPU percentage a process must clear to be
	// named a culprit while its own series is still cold — the rule this
	// ranker always used, kept as the named fallback.
	cpuCulpritFloor = 20.0
	// culpritZFloor is how far above its own baseline a warmed-up series
	// has to rise to be named a culprit — comfortably under the 3σ the
	// Detector itself uses to open a warning, since this ranks a handful of
	// candidates against each other rather than deciding whether to alert
	// at all.
	culpritZFloor = 1.5
)

// culpritModel implements Model. It has no weights: `rank` is a pure
// function of one process's snapshot, and the counters below exist only so
// the card can say honestly how often each rule actually fired.
type culpritModel struct {
	mu      sync.Mutex
	ranked  int // times a process's own baseline had enough history to score it
	floored int // times a process fell back to the raw floor because it did not
}

func newCulpritModel() *culpritModel {
	return &culpritModel{}
}

// rank scores one process's contribution to an open host-CPU anomaly, and
// decides outright whether it is a candidate at all. It prefers whichever
// of cpu or rss has risen further above that process's own baseline,
// requiring culpritZFloor of it; only when neither series has minSamples of
// history yet does it fall back to the raw floor this ranker always used.
// ok is false whenever neither rule finds anything: a cold process under
// the floor, or a warmed one sitting at or below its own normal — which is
// excluded outright, never re-scored by its raw value the way it used to be.
func (m *culpritModel) rank(snap procSnap) (score float64, changeBased bool, metric string, raw float64, ok bool) {
	best, bestReady := snap.cpuZ, snap.cpuReady
	bestMetric, bestRaw := "process.cpu.percent", snap.cpu
	if snap.rssReady && (!bestReady || snap.rssZ > best) {
		best, bestReady = snap.rssZ, true
		bestMetric, bestRaw = "process.memory.rss_bytes", snap.rss
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case bestReady && best >= culpritZFloor:
		// At least one series has a baseline and this process has risen
		// clearly above it.
		m.ranked++
		return best, true, bestMetric, bestRaw, true
	case bestReady:
		// The best baseline reading available (cpu, or rss if it moved
		// further) is at or not meaningfully above its own normal. That is
		// evidence of nothing, whatever the raw values are, so this process
		// is not a candidate — not even via the floor.
		return 0, false, "", 0, false
	case snap.cpu >= cpuCulpritFloor:
		// Neither series has minSamples of history yet: the only honest
		// answer left is the floor this ranker always used.
		m.floored++
		return snap.cpu, false, "process.cpu.percent", snap.cpu, true
	default:
		return 0, false, "", 0, false
	}
}

// Card implements Model.
func (m *culpritModel) Card() Card {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := m.ranked + m.floored
	detail := "no host CPU anomaly has opened yet"
	if total > 0 {
		detail = fmt.Sprintf(
			"%d of %d culprit rankings used a process's own baseline; the rest fell back to the raw floor because that process's series was still cold",
			m.ranked, total)
	}

	return Card{
		Name: "culprit ranking",
		Job:  "Rank the processes most likely contributing to an open host CPU anomaly.",
		Reads: []string{
			"process cpu (percent)",
			"process rss (bytes)",
			"each one's deviation from that process's own rolling baseline",
		},
		Fallback: "raw CPU usage, top three over a fixed 20% floor",
		Ready:    m.ranked > 0,
		Trained:  0,
		// There is no label yet for "this process actually caused the
		// spike" — see the comment above — so there is nothing to grade,
		// and Unmeasured says that honestly rather than a flattering zero.
		Accuracy:         Unmeasured,
		FallbackAccuracy: Unmeasured,
		Graded:           0,
		Detail:           detail,
	}
}
