package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/marcfs31/forsight/forseer"
	"github.com/marcfs31/forsight/forsight/internal/collector"
	"github.com/marcfs31/forsight/forsight/internal/model"
	"github.com/marcfs31/forsight/forsight/internal/store"
)

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyzResponse is handleReadyz's body. Collectors is omitted entirely
// (rather than an empty array) when no Registry was attached — cmd/demo has
// no real collectors, so there is nothing meaningful to list.
type readyzResponse struct {
	Status     string                      `json:"status"`
	StoreError string                      `json:"storeError,omitempty"`
	Collectors []collector.CollectorStatus `json:"collectors,omitempty"`
}

// handleReadyz serves GET /readyz. Unlike /healthz — an unconditional "the
// process is up", meant for a liveness probe that should never crash-loop
// the pod over something a restart can't fix — this is the readiness
// signal: it pings the store and fails (503) only when that ping fails,
// since nothing this agent serves is meaningful without it. Each
// collector's last error is reported alongside, for visibility, but never
// fails the probe by itself: a Docker collector with no socket to talk to,
// or a transient host-metrics hiccup, is not a reason to pull an otherwise
// healthy pod out of service (see ROADMAP.md item 24's Why).
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	resp := readyzResponse{Status: "ready"}
	if err := s.store.Ping(r.Context()); err != nil {
		resp.Status = "unavailable"
		resp.StoreError = err.Error()
	}
	if s.registry != nil {
		resp.Collectors = s.registry.Statuses()
	}

	status := http.StatusOK
	if resp.Status != "ready" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, resp)
}

// handleMetrics serves GET /api/v1/metrics?name=&since=<RFC3339>&before=<RFC3339>&limit=<n>&per_name=<n>&label.<key>=<value>
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := store.MetricQuery{Name: q.Get("name"), Labels: labelsFromQuery(q)}

	if err := parseWindow(q, &query.Since, &query.Before, &query.Limit); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// per_name is the metrics read's own cap (see store.MetricQuery.PerName):
	// the newest N of every name, so an unscoped read stays bounded without
	// a busy name starving a quiet one.
	if err := parsePositive(q.Get("per_name"), errInvalidPerName, &query.PerName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	metrics, err := s.store.QueryMetrics(r.Context(), query)
	if err != nil {
		http.Error(w, "failed to query metrics", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

// handleTraces serves GET /api/v1/traces?service=&traceId=&since=<RFC3339>&before=<RFC3339>&limit=<n>
func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := store.SpanQuery{Service: q.Get("service"), TraceID: q.Get("traceId")}

	if err := parseWindow(q, &query.Since, &query.Before, &query.Limit); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	spans, err := s.store.QuerySpans(r.Context(), query)
	if err != nil {
		http.Error(w, "failed to query traces", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, spans)
}

// handleLogs serves GET /api/v1/logs?since=<RFC3339>&before=<RFC3339>&limit=<n>&source=&severity=
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := store.LogQuery{
		Source:   q.Get("source"),
		Severity: model.LogSeverity(q.Get("severity")),
	}

	if err := parseWindow(q, &query.Since, &query.Before, &query.Limit); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logs, err := s.store.QueryLogs(r.Context(), query)
	if err != nil {
		http.Error(w, "failed to query logs", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

// parseWindow reads the three parameters every store query shares:
// since and before (RFC3339, each optional) bound the window, limit (a
// positive integer, optional) caps it at the newest N records.
func parseWindow(q url.Values, since, before *time.Time, limit *int) error {
	var err error
	if *since, err = parseTime(q.Get("since"), errInvalidSince); err != nil {
		return err
	}
	if *before, err = parseTime(q.Get("before"), errInvalidBefore); err != nil {
		return err
	}
	return parsePositive(q.Get("limit"), errInvalidLimit, limit)
}

// parsePositive stores raw as a positive integer in dst, leaves dst alone
// when raw is empty, and answers invalid for anything else.
func parsePositive(raw string, invalid error, dst *int) error {
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return invalid
	}
	*dst = n
	return nil
}

func parseTime(raw string, invalid error) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, invalid
	}
	return t, nil
}

var (
	errInvalidSince   = &queryError{"invalid since: expected RFC3339, e.g. 2026-01-02T15:04:05Z"}
	errInvalidBefore  = &queryError{"invalid before: expected RFC3339, e.g. 2026-01-02T15:04:05Z"}
	errInvalidLimit   = &queryError{"invalid limit: expected a positive integer"}
	errInvalidPerName = &queryError{"invalid per_name: expected a positive integer"}
)

type queryError struct{ msg string }

func (e *queryError) Error() string { return e.msg }

// labelsFromQuery extracts label.<key>=<value> query parameters into a map;
// returns nil (matching an unset MetricQuery.Labels) when there are none.
func labelsFromQuery(q url.Values) map[string]string {
	var labels map[string]string
	for key, values := range q {
		name, ok := strings.CutPrefix(key, "label.")
		if !ok || len(values) == 0 {
			continue
		}
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[name] = values[0]
	}
	return labels
}

func (s *Server) handleForseerInsights(w http.ResponseWriter, _ *http.Request) {
	if s.forseer == nil {
		writeJSON(w, http.StatusOK, []forseer.Insight{})
		return
	}
	writeJSON(w, http.StatusOK, s.forseer.Insights())
}

func (s *Server) handleForseerClusters(w http.ResponseWriter, _ *http.Request) {
	if s.forseer == nil {
		writeJSON(w, http.StatusOK, []forseer.Cluster{})
		return
	}
	writeJSON(w, http.StatusOK, s.forseer.Clusters())
}

func (s *Server) handleForseerBudget(w http.ResponseWriter, _ *http.Request) {
	if s.forseer == nil {
		writeJSON(w, http.StatusOK, forseer.Budget{Label: "Error-log budget", SLO: 0.01, WarningAt: 70, DangerAt: 90})
		return
	}
	writeJSON(w, http.StatusOK, s.forseer.Budget())
}

// handleForseerModels serves GET /api/v1/forseer/models: one card per
// trained model, saying what job it does, which inputs it reads, whether it
// has seen enough of this deployment to be trusted, and how it is scoring.
//
// It is the agent's only claim about what it has learned, and it is
// deliberately readable rather than impressive: a model that is not ready
// says so, and one whose job supplies no labels reports no accuracy instead
// of a flattering number.
func (s *Server) handleForseerModels(w http.ResponseWriter, _ *http.Request) {
	if s.forseer == nil {
		writeJSON(w, http.StatusOK, []forseer.Card{})
		return
	}
	writeJSON(w, http.StatusOK, s.forseer.Models())
}

func (s *Server) handleForseerTimeline(w http.ResponseWriter, _ *http.Request) {
	if s.forseer == nil {
		writeJSON(w, http.StatusOK, []forseer.Event{})
		return
	}
	writeJSON(w, http.StatusOK, s.forseer.Story())
}

// handleForseerQuery serves GET /api/v1/forseer/query?q=. The response
// carries "matched" alongside "facets" so the dashboard can tell "the phrase
// was empty" apart from "the phrase wasn't understood" — both parse to no
// facets, but only the latter is worth surfacing to the user as feedback.
func (s *Server) handleForseerQuery(w http.ResponseWriter, r *http.Request) {
	facets, matched := forseer.ParseQuery(r.URL.Query().Get("q"))
	if facets == nil {
		facets = []forseer.Facet{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"facets": facets, "matched": matched})
}

func (s *Server) handleForseerSummary(w http.ResponseWriter, r *http.Request) {
	key := forseer.APIKeyFromEnv()
	if key == "" {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "summary": ""})
		return
	}
	var insights []forseer.Insight
	var clusters []forseer.Cluster
	if s.forseer != nil {
		insights = s.forseer.Insights()
		clusters = s.forseer.Clusters()
	}
	summary, err := forseer.Summarize(r.Context(), insights, clusters, key)
	if err != nil {
		http.Error(w, "forseer summary failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "summary": summary})
}

// writeJSON marshals first and commits the status afterwards.
//
// The obvious shape — WriteHeader then Encode — cannot report a failure,
// because by the time Encode runs the status line is already on the wire.
// Encode marshals into an internal buffer before writing, so a failure there
// writes zero body bytes: the client gets a 200 with an empty body, res.ok is
// true, res.json() throws, and the dashboard reports the agent unreachable
// while the agent logs nothing. json.Marshal fails outright on a non-finite
// float, and handleMetrics passes the entire matching slice, so one NaN
// anywhere in the retention window did that to every metrics read. The
// non-finite filters at the ingest boundaries are what stop it arriving; this
// is what stops it being silent if anything ever does.
//
// slog.Default() rather than s.logger: writeJSON is a free function shared by
// handlers.go and mlaas.go with no receiver, and NewServer falls back to the
// same default.
func writeJSON(w http.ResponseWriter, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		slog.Default().Error("encoding a JSON response", "status", status, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}
