// Package api wires forsight's HTTP surface: the query API the dashboard (and
// any other client) reads from, the OTLP ingest endpoints, and the embedded
// dashboard itself.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/marcfs31/forsight/forseer"
	"github.com/marcfs31/forsight/forsight/internal/logsafe"
	"github.com/marcfs31/forsight/forsight/internal/store"
)

// OTLPHandler is satisfied by *otlp.Handler; declared here rather than
// imported so this package doesn't need to know about OTLP's wire format,
// only that something can register routes on a mux.
type OTLPHandler interface {
	Register(mux *http.ServeMux)
}

// Server wires the query API, OTLP ingest, and dashboard into one handler.
type Server struct {
	store     store.Store
	otlp      OTLPHandler  // nil is valid: OTLP ingest simply isn't mounted
	dashboard http.Handler // nil is valid: falls back to a plain 404 at "/"
	logger    *slog.Logger
	forseer   *forseer.Engine // nil: /api/v1/forseer/* returns empty/disabled
	mlaas     MlaasService    // nil: /api/v1/mlaas/* reports not-configured
}

// NewServer builds a Server. dashboard may be nil (see dashboard.go for the
// embedded-build version); otlpHandler may be nil to run without an ingest
// endpoint.
func NewServer(st store.Store, otlpHandler OTLPHandler, dashboard http.Handler, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{store: st, otlp: otlpHandler, dashboard: dashboard, logger: logger}
}

// WithForseer attaches the AI/ML engine (statistical detectors + optional Grok).
func (s *Server) WithForseer(e *forseer.Engine) *Server {
	s.forseer = e
	return s
}

// Handler builds the full routed http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /api/v1/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/v1/traces", s.handleTraces)
	mux.HandleFunc("GET /api/v1/logs", s.handleLogs)
	mux.HandleFunc("GET /api/v1/forseer/insights", s.handleForseerInsights)
	mux.HandleFunc("GET /api/v1/forseer/clusters", s.handleForseerClusters)
	mux.HandleFunc("GET /api/v1/forseer/budget", s.handleForseerBudget)
	mux.HandleFunc("GET /api/v1/forseer/timeline", s.handleForseerTimeline)
	mux.HandleFunc("GET /api/v1/forseer/query", s.handleForseerQuery)
	mux.HandleFunc("GET /api/v1/forseer/summary", s.handleForseerSummary)
	mux.HandleFunc("GET /api/v1/forseer/models", s.handleForseerModels)
	mux.HandleFunc("GET /api/v1/forseer/classify", s.handleForseerClassify)

	mux.HandleFunc("GET /api/v1/mlaas/status", s.handleMlaasStatus)
	mux.HandleFunc("POST /api/v1/mlaas/models/{name}/train", s.handleMlaasTrain)
	mux.HandleFunc("POST /api/v1/mlaas/models/{name}/tune", s.handleMlaasTune)
	mux.HandleFunc("POST /api/v1/mlaas/models/{name}/predict", s.handleMlaasPredict)

	if s.otlp != nil {
		s.otlp.Register(mux)
	}
	if s.dashboard != nil {
		mux.Handle("/", s.dashboard)
	}

	return s.withLogging(mux)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		s.logger.Debug("http request",
			"method", logsafe.String(r.Method), "path", logsafe.String(r.URL.Path), "status", rw.status, "duration", time.Since(start))
	})
}

// statusRecorder captures the status code a handler wrote, since
// http.ResponseWriter itself doesn't expose it after the fact.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
