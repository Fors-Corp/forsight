package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/marcfs31/forsight/forsight/internal/mlaas"
	"github.com/marcfs31/forsight/forsight/internal/model"
)

// mlaasMaxBodyBytes caps a predict request body the same way the OTLP
// ingest handlers cap theirs: a client that sends something huge gets a
// decode error, not a server that reads it all into memory first.
const mlaasMaxBodyBytes = 64 << 10

// mlaasMaxPredictRows mirrors the cap the Syncer itself enforces
// (see the design doc's "Actions (proxied)" section); checking it here too
// means a client gets a plain 400 before anything reaches mlaas, rather
// than depending on the service to say no first.
const mlaasMaxPredictRows = 10

// classifyMaxMessageBytes bounds GET .../forseer/classify?message= — a
// query parameter, not a body, so there is no MaxBytesReader to lean on.
const classifyMaxMessageBytes = 4096

// MlaasService is what the API layer needs from the mlaas integration: the
// cached status snapshot and the three actions the dashboard can trigger.
// It is satisfied by *mlaas.Syncer in production and a fake in tests, and
// declared here — rather than depending on mlaas.Syncer directly — so this
// package only ever imports mlaas for its plain JSON types.
type MlaasService interface {
	Status(ctx context.Context) mlaas.Status
	Train(ctx context.Context, name string) (mlaas.JobRef, error)
	Tune(ctx context.Context, name string) (mlaas.JobRef, error)
	Predict(ctx context.Context, name string, rows []map[string]any) (mlaas.PredictResult, error)
}

// WithMlaas attaches the mlaas integration. Leaving it unset (a nil svc) is
// valid: /api/v1/mlaas/* then reports not-configured instead of failing,
// the same way an unset forseer engine leaves /api/v1/forseer/* empty.
func (s *Server) WithMlaas(svc MlaasService) *Server {
	s.mlaas = svc
	return s
}

// handleMlaasStatus serves GET /api/v1/mlaas/status. With no service
// attached it still answers 200 with an explicitly empty snapshot — the
// dashboard's Models page renders the same "not configured" card either
// way, and never has to special-case a missing route.
func (s *Server) handleMlaasStatus(w http.ResponseWriter, r *http.Request) {
	if s.mlaas == nil {
		writeJSON(w, http.StatusOK, mlaas.Status{
			Models:      []mlaas.ModelStatus{},
			Forecasts:   []mlaas.Forecast{},
			Predictions: []mlaas.Prediction{},
			Jobs:        []mlaas.Job{},
		})
		return
	}
	writeJSON(w, http.StatusOK, s.mlaas.Status(r.Context()))
}

// handleMlaasTrain serves POST /api/v1/mlaas/models/{name}/train.
func (s *Server) handleMlaasTrain(w http.ResponseWriter, r *http.Request) {
	s.handleMlaasJob(w, r, func(ctx context.Context, name string) (mlaas.JobRef, error) {
		return s.mlaas.Train(ctx, name)
	})
}

// handleMlaasTune serves POST /api/v1/mlaas/models/{name}/tune.
func (s *Server) handleMlaasTune(w http.ResponseWriter, r *http.Request) {
	s.handleMlaasJob(w, r, func(ctx context.Context, name string) (mlaas.JobRef, error) {
		return s.mlaas.Tune(ctx, name)
	})
}

// handleMlaasJob is the train/tune handlers' shared body: both are "check
// the service exists, run the one action that differs, map the error".
// action is only ever called once s.mlaas is known non-nil.
func (s *Server) handleMlaasJob(w http.ResponseWriter, r *http.Request, action func(context.Context, string) (mlaas.JobRef, error)) {
	if s.mlaas == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": mlaas.ErrNotConfigured.Error()})
		return
	}
	ref, err := action(r.Context(), r.PathValue("name"))
	if err != nil {
		writeMlaasError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, ref)
}

// handleMlaasPredict serves POST /api/v1/mlaas/models/{name}/predict with
// body {"rows":[{...}, ...]}. Row-count and per-cell-size limits are also
// enforced by the Syncer, but checking the row count here means a bad
// request never has to make a round trip to mlaas to be told no.
func (s *Server) handleMlaasPredict(w http.ResponseWriter, r *http.Request) {
	if s.mlaas == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": mlaas.ErrNotConfigured.Error()})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, mlaasMaxBodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "failed to read request body"})
		return
	}
	var req struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	if len(req.Rows) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "rows must not be empty"})
		return
	}
	if len(req.Rows) > mlaasMaxPredictRows {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("at most %d rows per request", mlaasMaxPredictRows)})
		return
	}

	result, err := s.mlaas.Predict(r.Context(), r.PathValue("name"), req.Rows)
	if err != nil {
		writeMlaasError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// writeMlaasError maps a Syncer error onto the status code the dashboard
// needs to tell "this isn't one of ours" (404) apart from "mlaas itself
// said no" (its own status) apart from "mlaas didn't answer at all" (502).
func writeMlaasError(w http.ResponseWriter, err error) {
	var upstream *mlaas.UpstreamError
	switch {
	case errors.Is(err, mlaas.ErrUnknownModel):
		writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	case errors.Is(err, mlaas.ErrNotConfigured):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error()})
	case errors.As(err, &upstream):
		writeJSON(w, upstream.Status, map[string]any{"error": upstream.Message})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
	}
}

// handleForseerClassify serves GET /api/v1/forseer/classify?message=. It
// answers from the in-binary Forseer model when it is trained enough to
// have an opinion, and otherwise from the same fixed rule filelog falls
// back to on ingest — so the same log line classified through this route
// or through ingest gets the same answer whenever the model doesn't have
// one yet. The "Try the severity models" card on the Models page uses this
// to show what Forseer says next to what mlaas's classifier says.
func (s *Server) handleForseerClassify(w http.ResponseWriter, r *http.Request) {
	message := r.URL.Query().Get("message")
	if message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "message is required"})
		return
	}
	if len(message) > classifyMaxMessageBytes {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "message exceeds 4096 bytes"})
		return
	}

	if s.forseer != nil {
		if severity, ok := s.forseer.ClassifySeverity(message); ok {
			writeJSON(w, http.StatusOK, map[string]any{"severity": severity, "source": "model", "ready": true})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"severity": string(model.FallbackSeverity(message)),
		"source":   "rule",
		"ready":    false,
	})
}
