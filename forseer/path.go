package forseer

// CriticalPath walks from the error (or slowest) span up its parents to the
// root and returns span names root-first. Used as Insight.Related so
// TraceWaterfall can highlight the same path the detector blamed.
func CriticalPath(spans []SpanSample) []string {
	if len(spans) == 0 {
		return nil
	}
	byID := make(map[string]SpanSample, len(spans))
	for _, sp := range spans {
		if sp.SpanID != "" {
			byID[sp.SpanID] = sp
		}
	}
	hasChild := map[string]bool{}
	for _, sp := range spans {
		if sp.ParentID != "" {
			hasChild[sp.ParentID] = true
		}
	}
	leaf := spans[0]
	score := func(sp SpanSample) (err, terminal, dur int) {
		if sp.Status == "error" {
			err = 1
		}
		if sp.SpanID == "" || !hasChild[sp.SpanID] {
			terminal = 1
		}
		dur = int(sp.DurationMs)
		return
	}
	bestErr, bestTerm, bestDur := score(leaf)
	for _, sp := range spans[1:] {
		err, term, dur := score(sp)
		if err > bestErr || (err == bestErr && term > bestTerm) || (err == bestErr && term == bestTerm && dur > bestDur) {
			leaf, bestErr, bestTerm, bestDur = sp, err, term, dur
		}
	}
	var names []string
	seen := map[string]bool{}
	cur := leaf
	for cur.SpanID == "" || !seen[cur.SpanID] {
		if cur.SpanID != "" {
			seen[cur.SpanID] = true
		}
		names = append([]string{cur.Name}, names...)
		if cur.ParentID == "" {
			break
		}
		parent, ok := byID[cur.ParentID]
		if !ok {
			break
		}
		cur = parent
	}
	return names
}
