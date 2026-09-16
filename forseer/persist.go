package forseer

import (
	"encoding/json"
	"fmt"
)

// engineSnapshotVersion versions the envelope Snapshot writes and Restore
// reads below — the shape of {"version","models":{...}} itself, not any one
// model's payload. Each model versions its own payload independently (see
// each model's own Restore), so a change to what one model persists does
// not need to bump this.
const engineSnapshotVersion = 1

// engineSnapshotDoc is the one JSON document Snapshot produces and Restore
// consumes. Models is keyed by the stable name persistedModels gives each
// model, not by Card().Name (a display string free to reword without
// invalidating an old snapshot), and every value is exactly the byte slice
// that model's own Snapshot returned — this file never looks inside one.
type engineSnapshotDoc struct {
	Version int                        `json:"version"`
	Models  map[string]json.RawMessage `json:"models"`
}

// persistedModel pairs one engine model with the stable key its snapshot is
// stored and restored under.
type persistedModel struct {
	key   string
	model Model
}

// persistedModels lists every model in the engine against its persistence
// key, in the same order Models() reports them. Adding a model here (and to
// Models() in engine.go) is the whole change needed to make a new model
// participate in both the API and Engine.Snapshot/Restore.
func (e *Engine) persistedModels() []persistedModel {
	return []persistedModel{
		{"severity", e.severity},
		{"thresholds", e.det.thresholds},
		{"forecast", e.forecast},
		{"paging", e.paging},
		{"spans", e.spans},
		{"culprit", e.culprit},
		{"outlier", e.det.outlier},
	}
}

// Snapshot serializes every model in the engine into one JSON document: a
// top-level version for the envelope itself, and a map of each model's
// persistence key to its own versioned payload. Nothing in this package
// touches a filesystem — forsight/cmd/run.go is what writes the result to
// one file beside the Badger directory under --data-dir, on shutdown.
func (e *Engine) Snapshot() ([]byte, error) {
	doc := engineSnapshotDoc{Version: engineSnapshotVersion, Models: make(map[string]json.RawMessage)}
	for _, pm := range e.persistedModels() {
		payload, err := pm.model.Snapshot()
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", pm.key, err)
		}
		doc.Models[pm.key] = payload
	}
	return json.Marshal(doc)
}

// RestoreReport is one model's outcome from Restore, for a caller that wants
// to log what actually came back (forsight/cmd/run.go's
// restoreForseerSnapshot logs one INFO line naming every model that did).
type RestoreReport struct {
	// Name is the model's stable persistence key (see persistedModels), not
	// its Card().Name.
	Name string
	// Version is the schema version found inside that model's own payload;
	// 0 when the payload didn't parse enough to have one.
	Version int
	// Restored is true when the model accepted the payload. False means it
	// was discarded — a version mismatch, or a payload that doesn't parse
	// as this model's shape — and the model was left exactly as NewEngine
	// built it, cold.
	Restored bool
}

// Restore reads a document Snapshot produced and restores, in place, every
// model whose key it finds — it does not build a new Engine. It is meant to
// be called once, right after NewEngine and before anything else touches
// the engine (forsight/cmd/run.go calls it before WithSeverityFallback):
// every model's own Restore overwrites rather than merges, so restoring
// onto a model that has already observed live data would discard that
// observation.
//
// A model with no entry in the document (an older snapshot taken before
// that model existed) is left cold, silently — there was never anything to
// restore, which is not the same as a discard. A model whose entry does not
// parse as its own shape, or carries a schema version this build does not
// recognize, is also left cold: that model's own Restore returns an error,
// which this method turns into a RestoreReport with Restored=false rather
// than propagating, so one unreadable model's payload never keeps the rest
// of the engine from coming back warm.
//
// Only the envelope itself failing to parse — not valid JSON, or not this
// version of the {"version","models"} shape — is returned as an error: at
// that point nothing in the document can be trusted to mean what its keys
// claim, so the caller's right answer is the same as a missing file, a cold
// engine, with every model reporting as such.
func (e *Engine) Restore(data []byte) ([]RestoreReport, error) {
	var doc engineSnapshotDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse forseer snapshot: %w", err)
	}
	if doc.Version != engineSnapshotVersion {
		return nil, fmt.Errorf("forseer snapshot version %d, want %d", doc.Version, engineSnapshotVersion)
	}
	pms := e.persistedModels()
	reports := make([]RestoreReport, 0, len(pms))
	for _, pm := range pms {
		payload, ok := doc.Models[pm.key]
		if !ok {
			continue
		}
		report := RestoreReport{Name: pm.key, Version: snapshotPayloadVersion(payload)}
		if err := pm.model.Restore(payload); err == nil {
			report.Restored = true
		}
		reports = append(reports, report)
	}
	return reports, nil
}

// snapshotPayloadVersion peeks at a model payload's own "version" field
// without knowing that model's full schema, for RestoreReport. It reports 0
// when the payload doesn't even parse enough to have one — the same value a
// genuinely absent field decodes to, which is harmless: a caller only ever
// reports the version of a model that actually restored.
func snapshotPayloadVersion(payload json.RawMessage) int {
	var v struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(payload, &v)
	return v.Version
}
