package forseer

import (
	"bytes"
	"encoding/json"
	"testing"
)

// trainEngineModels puts real learned state into every one of the engine's
// seven persisted models, so a round-trip test proves something about all
// of them rather than just the ones NewEngine happens to touch on its own.
func trainEngineModels(t *testing.T, e *Engine) {
	t.Helper()
	trainRealistic(e.severity, 30)

	sent, recv := 1_000_000.0, 800_000.0
	for i := 0; i < 200; i++ {
		cpu := 50 + float64(i%5)
		jitter := []float64{0, 0.01, -0.01, 0.02, -0.02}[i%5]
		mem := cpu + jitter
		disk := 30 + float64(i%3)*0.5
		sent += 1000 + float64(i%4)*20
		recv += 800 + float64((i+1)%4)*15
		e.ObserveMetrics([]Point{
			{Name: "host.cpu.percent", Value: cpu},
			{Name: "host.memory.percent", Value: mem},
			{Name: "host.disk.percent", Value: disk},
			{Name: "host.net.bytes_sent", Value: sent},
			{Name: "host.net.bytes_recv", Value: recv},
		})
	}
	// One clearly anomalous batch, so the threshold model has both a
	// warning and a critical hit to have learned from and the culprit
	// ranker has a host anomaly to rank a process against.
	e.ObserveMetrics([]Point{
		{Name: "host.cpu.percent", Value: 99},
		{Name: "process.cpu.percent", Value: 90, Labels: map[string]string{"pid": "7", "name": "rogue"}},
	})

	lines := make([]LogLine, burstMinCount)
	for i := range lines {
		lines[i] = LogLine{Source: "api", Severity: "error", Message: "boom"}
	}
	e.ObserveLogs(lines)

	e.ObserveSpans([]SpanSample{
		{Service: "api", Name: "GET /checkout", DurationMs: 18},
		{Service: "api", Name: "GET /checkout", DurationMs: 20},
		{Service: "api", Name: "GET /checkout", DurationMs: 19},
		{Service: "api", Name: "GET /checkout", DurationMs: 21},
		{Service: "api", Name: "GET /checkout", DurationMs: 22},
		{Service: "api", Name: "GET /checkout", DurationMs: 20},
		{Service: "api", Name: "GET /checkout", DurationMs: 19},
		{Service: "api", Name: "GET /checkout", DurationMs: 21},
		{Service: "api", Name: "GET /checkout", DurationMs: 18},
		{Service: "api", Name: "GET /checkout", DurationMs: 22},
		{Service: "api", Name: "GET /checkout", DurationMs: 20},
		{Service: "api", Name: "GET /checkout", DurationMs: 19},
	})

	if e.severity.Card().Trained == 0 {
		t.Fatal("test setup: severity model never trained")
	}
	if e.paging.trained == 0 && len(e.paging.pending) == 0 {
		t.Fatal("test setup: paging model saw no burst at all")
	}
}

// TestEngine_SnapshotRestoreRoundTrip is roadmap item 25's engine-level
// proof: every model's payload lands under its own key, and every model
// with something to restore reports back as restored, at its own version.
func TestEngine_SnapshotRestoreRoundTrip(t *testing.T) {
	e := NewEngine()
	trainEngineModels(t, e)

	data, err := e.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := NewEngine()
	reports, err := restored.Restore(data)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	wantKeys := map[string]bool{
		"severity": true, "thresholds": true, "forecast": false, // forecast never observed a reading
		"paging": true, "spans": true, "culprit": true, "outlier": true,
	}
	// Each model versions its own payload independently — that is the point
	// of a per-model schema version — so this asserts each report carries
	// the version that model actually writes, not one shared number.
	wantVersion := map[string]int{
		"severity":   severitySnapshotVersion,
		"thresholds": thresholdSnapshotVersion,
		"forecast":   forecastSnapshotVersion,
		"paging":     pagingSnapshotVersion,
		"spans":      spanSnapshotVersion,
		"culprit":    culpritSnapshotVersion,
		"outlier":    hostOutlierSnapshotVersion,
	}
	seen := map[string]bool{}
	for _, r := range reports {
		seen[r.Name] = true
		if !r.Restored {
			t.Errorf("model %q did not restore: %+v", r.Name, r)
			continue
		}
		want, known := wantVersion[r.Name]
		if !known {
			t.Errorf("model %q is in the restore report but not in this test's version map", r.Name)
			continue
		}
		if r.Version != want {
			t.Errorf("model %q restored at version %d, want %d", r.Name, r.Version, want)
		}
	}
	for key, want := range wantKeys {
		if want && !seen[key] {
			t.Errorf("model %q never appeared in the restore report", key)
		}
	}

	if got, want := restored.severity.Card().Trained, e.severity.Card().Trained; got != want {
		t.Errorf("restored severity Trained = %d, want %d", got, want)
	}
	if got, want := len(restored.Models()), len(e.Models()); got != want {
		t.Errorf("restored engine reports %d models, want %d", got, want)
	}
}

// TestEngine_RestoreLeavesAMissingModelKeyCold proves an older snapshot,
// taken before a model existed, restores everything it does have and
// leaves the new model cold rather than erroring the whole document out.
func TestEngine_RestoreLeavesAMissingModelKeyCold(t *testing.T) {
	e := NewEngine()
	trainEngineModels(t, e)
	data, err := e.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var models map[string]json.RawMessage
	if err := json.Unmarshal(doc["models"], &models); err != nil {
		t.Fatalf("unmarshal models: %v", err)
	}
	delete(models, "outlier")
	remarshalledModels, err := json.Marshal(models)
	if err != nil {
		t.Fatalf("marshal models: %v", err)
	}
	doc["models"] = remarshalledModels
	data, err = json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	restored := NewEngine()
	reports, err := restored.Restore(data)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	for _, r := range reports {
		if r.Name == "outlier" {
			t.Fatalf("outlier appeared in the restore report despite having no entry: %+v", r)
		}
	}
	if got := restored.severity.Card().Trained; got == 0 {
		t.Error("the rest of the document was not restored just because one key was missing")
	}
}

// TestEngine_RestoreDiscardsAnEnvelopeVersionMismatch proves the whole
// document is discarded — not misread — when the envelope's own version
// (not any one model's) does not match.
func TestEngine_RestoreDiscardsAnEnvelopeVersionMismatch(t *testing.T) {
	e := NewEngine()
	trainEngineModels(t, e)
	data, err := e.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// json.Marshal writes struct fields in declaration order, so the
	// envelope's own "version" is the first occurrence in the document —
	// count=1 touches only that one, not any model payload's own version.
	if !bytes.HasPrefix(data, []byte(`{"version":1,`)) {
		t.Fatalf("test assumption broken: envelope does not start %q: %s", `{"version":1,`, data[:40])
	}
	data = bytes.Replace(data, []byte(`"version":1`), []byte(`"version":2`), 1)

	restored := NewEngine()
	if _, err := restored.Restore(data); err == nil {
		t.Fatal("Restore accepted an envelope with the wrong version")
	}
	if restored.severity.Card().Trained != 0 {
		t.Fatal("a discarded envelope left a model partially restored")
	}
}

func TestEngine_RestoreDiscardsCorruptJSON(t *testing.T) {
	restored := NewEngine()
	if _, err := restored.Restore([]byte("{not json")); err == nil {
		t.Fatal("Restore accepted corrupt JSON")
	}
	if restored.severity.Card().Trained != 0 {
		t.Fatal("a discarded envelope left a model partially restored")
	}
}
