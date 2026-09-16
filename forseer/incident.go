package forseer

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// incidentWindow is how close in time two insights have to land to be
// considered part of the same incident.
const incidentWindow = 5 * time.Minute

// groupIncidents folds insights that land within incidentWindow of each
// other AND share at least one Related value or the same Source into a
// single incident Event, for Story. This is deliberately NOT a Model (see
// learn.go and MODELS.md's Rules): there is no label for "these findings
// were really one incident", so nothing here can be scored against a
// fallback the way every Model in this package must be. ROADMAP.md's "Left
// out" section is why — a learned version never merges with no label
// source, so this does the deterministic version instead: same input,
// same output, no state carried across calls, no Card, not listed on the
// Models page.
//
// Grouping is transitive through clusterInsights: three insights chained
// A-B-C by a Related value each shares only with its neighbour still land
// in one incident, not two. A group of exactly one insight passes through
// as the same Event Story has always produced, so a single finding looks
// exactly as it did before this existed.
func groupIncidents(insights []Insight) []Event {
	groups := clusterInsights(insights)
	out := make([]Event, 0, len(groups))
	for _, g := range groups {
		if len(g) == 1 {
			out = append(out, storyEvent(g[0]))
			continue
		}
		out = append(out, incidentEvent(g))
	}
	return out
}

// clusterInsights unions insights into connected components with a
// union-find: two insights are linked when linked(a, b) is true, and
// linking is transitive by construction — the result does not depend on
// the order insights are passed in, only on which pairs are linked, so
// re-running Story on the same insight set is stable.
func clusterInsights(insights []Insight) [][]Insight {
	n := len(insights)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(i, j int) {
		ri, rj := find(i), find(j)
		if ri != rj {
			parent[ri] = rj
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if linked(insights[i], insights[j]) {
				union(i, j)
			}
		}
	}

	byRoot := make(map[int][]Insight, n)
	var roots []int
	for i := 0; i < n; i++ {
		r := find(i)
		if _, seen := byRoot[r]; !seen {
			roots = append(roots, r)
		}
		byRoot[r] = append(byRoot[r], insights[i])
	}
	// roots are stable per call (find() over a freshly built parent slice
	// indexed by input position), but sorting them is what keeps distinct
	// incidents in the same relative order regardless of how the union-find
	// pass happened to walk the pairs.
	sort.Ints(roots)
	out := make([][]Insight, 0, len(roots))
	for _, r := range roots {
		out = append(out, byRoot[r])
	}
	return out
}

// linked is the pairwise test groupIncidents' spec names: within
// incidentWindow of each other, and sharing a Related value or a Source.
// Only a specific Source counts — a service, a host, a log source. The
// Detector stamps every metric anomaly, changepoint and host outlier with
// the literal "forseer", and emptySource falls back to "unknown", so either
// of those (or a blank) would fold every finding of that kind inside the
// window into one incident regardless of what it was about; those are the
// findings whose Related values carry the real link.
func linked(a, b Insight) bool {
	diff := a.Time.Sub(b.Time)
	if diff < 0 {
		diff = -diff
	}
	if diff > incidentWindow {
		return false
	}
	if specificSource(a.Source) && a.Source == b.Source {
		return true
	}
	for _, ra := range a.Related {
		for _, rb := range b.Related {
			if ra == rb {
				return true
			}
		}
	}
	return false
}

// specificSource reports whether a Source names one thing rather than the
// detector family or the "unknown" fallback.
func specificSource(source string) bool {
	switch source {
	case "", "forseer", "unknown":
		return false
	}
	return true
}

// incidentEvent builds one Timeline event from a connected component of two
// or more insights: title names the count and the earliest member (the
// cause), description lists every member oldest-first — newest last, the
// way a running log reads — and tone is the most severe member's, on the
// same critical/warning/else ranking sortInsights uses.
func incidentEvent(members []Insight) Event {
	sorted := append([]Insight(nil), members...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Before(sorted[j].Time) })

	earliest := sorted[0]
	latest := sorted[len(sorted)-1]

	lines := make([]string, len(sorted))
	rank, tone := 3, "accent"
	for i, ins := range sorted {
		lines[i] = ins.Title
		if r := severityRank(ins.Severity); r < rank {
			rank, tone = r, eventTone(ins.Severity)
		}
	}

	return Event{
		ID:          "evt-incident-" + earliest.ID,
		Time:        latest.Time,
		Title:       fmt.Sprintf("%d related findings, starting with %s", len(sorted), earliest.Title),
		Description: strings.Join(lines, " · "),
		Tone:        tone,
	}
}

// severityRank orders severities most-severe-first, matching
// sortInsights (detector.go) so an incident's tone and Story's own ordering
// agree on what "most severe" means.
func severityRank(sev string) int {
	switch sev {
	case SeverityCritical:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}
