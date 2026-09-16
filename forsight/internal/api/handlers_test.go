package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forseer"
	"github.com/marcfs31/forsight/forsight/internal/collector"
	"github.com/marcfs31/forsight/forsight/internal/model"
	"github.com/marcfs31/forsight/forsight/internal/store"
)

func TestHandleHealthz(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
}

func TestHandleMetrics_FiltersByNameAndLabel(t *testing.T) {
	st := store.NewMemoryStore(time.Hour)
	now := time.Now()
	_ = st.WriteMetrics(context.Background(), []model.Metric{
		{Name: "host.cpu.percent", Value: 10, Timestamp: now, Labels: map[string]string{"host": "a"}},
		{Name: "host.cpu.percent", Value: 20, Timestamp: now, Labels: map[string]string{"host": "b"}},
		{Name: "host.memory.percent", Value: 30, Timestamp: now},
	})
	s := NewServer(st, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics?name=host.cpu.percent&label.host=a", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got []model.Metric
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got) != 1 || got[0].Value != 10 {
		t.Fatalf("got %+v, want exactly the host=a metric", got)
	}
}

func TestHandleMetrics_RejectsInvalidSince(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics?since=not-a-date", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleMetrics_LimitAndBefore(t *testing.T) {
	st := store.NewMemoryStore(time.Hour)
	base := time.Now().Add(-30 * time.Minute)
	_ = st.WriteMetrics(context.Background(), []model.Metric{
		{Name: "m", Value: 1, Timestamp: base},
		{Name: "m", Value: 2, Timestamp: base.Add(time.Minute)},
		{Name: "m", Value: 3, Timestamp: base.Add(2 * time.Minute)},
	})
	s := NewServer(st, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics?name=m&limit=1", nil)
	s.Handler().ServeHTTP(rec, req)
	var got []model.Metric
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got) != 1 || got[0].Value != 3 {
		t.Fatalf("limit=1 returned %+v, want only the newest point", got)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics?name=m&before="+base.Add(time.Minute).UTC().Format(time.RFC3339), nil)
	s.Handler().ServeHTTP(rec, req)
	got = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got) != 1 || got[0].Value != 1 {
		t.Fatalf("before= returned %+v, want only the point before it", got)
	}
}

func TestHandleMetrics_PerName(t *testing.T) {
	st := store.NewMemoryStore(time.Hour)
	base := time.Now().Add(-30 * time.Minute)
	_ = st.WriteMetrics(context.Background(), []model.Metric{
		{Name: "busy", Value: 1, Timestamp: base},
		{Name: "quiet", Value: 10, Timestamp: base.Add(30 * time.Second)},
		{Name: "busy", Value: 2, Timestamp: base.Add(time.Minute)},
		{Name: "busy", Value: 3, Timestamp: base.Add(2 * time.Minute)},
	})
	s := NewServer(st, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics?per_name=1", nil)
	s.Handler().ServeHTTP(rec, req)
	var got []model.Metric
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	// The newest of each name, oldest-first: quiet's only point, then busy's
	// third. A plain limit=2 would have answered busy's last two instead.
	if len(got) != 2 || got[0].Name != "quiet" || got[0].Value != 10 || got[1].Name != "busy" || got[1].Value != 3 {
		t.Fatalf("per_name=1 returned %+v, want the newest point of each name", got)
	}
}

func TestHandleMetrics_RejectsInvalidLimitAndBefore(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	for _, query := range []string{"limit=abc", "limit=0", "limit=-5", "before=not-a-date", "per_name=abc", "per_name=0", "per_name=-1"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics?"+query, nil)
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, rec.Code)
		}
	}
}

func TestHandleTraces_FiltersByService(t *testing.T) {
	st := store.NewMemoryStore(time.Hour)
	now := time.Now()
	_ = st.WriteSpans(context.Background(), []model.Span{
		{TraceID: "t1", SpanID: "s1", Service: "checkout", Start: now, Status: model.SpanStatusOK},
		{TraceID: "t2", SpanID: "s2", Service: "payments", Start: now, Status: model.SpanStatusError},
	})
	s := NewServer(st, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces?service=payments", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got []model.Span
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got) != 1 || got[0].TraceID != "t2" {
		t.Fatalf("got %+v, want exactly the payments span", got)
	}
}

func TestHandleLogs_FiltersBySourceAndSeverity(t *testing.T) {
	st := store.NewMemoryStore(time.Hour)
	now := time.Now()
	_ = st.WriteLogs(context.Background(), []model.LogEntry{
		{Timestamp: now, Severity: model.LogSeverityInfo, Source: "checkout", Message: "ok"},
		{Timestamp: now, Severity: model.LogSeverityError, Source: "payments", Message: "fail"},
		{Timestamp: now, Severity: model.LogSeverityWarn, Source: "payments", Message: "slow"},
	})
	s := NewServer(st, nil, nil, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?source=payments&severity=error", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got []model.LogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got) != 1 || got[0].Message != "fail" {
		t.Fatalf("got %+v, want exactly the payments error entry", got)
	}
}

func TestHandleLogs_RejectsInvalidSince(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs?since=not-a-date", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_MountsOTLPAndDashboard(t *testing.T) {
	otlpMounted := false
	fakeOTLP := otlpRegisterFunc(func(mux *http.ServeMux) {
		otlpMounted = true
		mux.HandleFunc("POST /v1/metrics", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})
	dashboard := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot) // distinctive, unambiguous marker
	})

	s := NewServer(store.NewMemoryStore(time.Hour), fakeOTLP, dashboard, nil)
	handler := s.Handler()

	if !otlpMounted {
		t.Fatal("Handler() did not call otlp.Register")
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("fallback route status = %d, want the dashboard handler's 418", rec.Code)
	}
}

func TestHandleForseerInsights_EmptyWithoutDetector(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/insights", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got []any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want []", got)
	}
}

func TestHandleForseerSummary_DisabledWithoutKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/summary", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["enabled"] != false {
		t.Errorf("enabled = %v, want false", body["enabled"])
	}
}

func TestHandleForseerQuery_ParsesPhrase(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/query?q=error+logs+from+checkout", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Facets  []map[string]string `json:"facets"`
		Matched bool                `json:"matched"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Matched {
		t.Errorf("matched = false, want true for %+v", got.Facets)
	}
	if len(got.Facets) != 2 {
		t.Fatalf("got %+v", got.Facets)
	}
}

// A phrase this grammar doesn't recognize must round-trip as
// matched:false with an empty (non-null) facets array — the dashboard
// uses "matched" to decide whether to show "didn't understand that"
// feedback instead of silently doing nothing.
func TestHandleForseerQuery_UnrecognizedPhraseIsUnmatched(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/query?q=what+is+happening", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Facets  []map[string]string `json:"facets"`
		Matched bool                `json:"matched"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Matched {
		t.Errorf("matched = true, want false for %+v", got.Facets)
	}
	if len(got.Facets) != 0 {
		t.Errorf("got %+v, want no facets", got.Facets)
	}
}

// "critical" is the word AlertList/Timeline train the user to type
// (Insight.Severity's vocabulary); the query grammar must map it onto the
// error-class status facet rather than leaving it unmatched.
func TestHandleForseerQuery_CriticalMapsToErrorStatus(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/query?q=critical", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got struct {
		Facets  []map[string]string `json:"facets"`
		Matched bool                `json:"matched"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Matched {
		t.Fatalf("matched = false, want true for %+v", got.Facets)
	}
	if len(got.Facets) != 1 || got.Facets[0]["key"] != "status" || got.Facets[0]["value"] != "error" {
		t.Fatalf("got %+v, want [status=error]", got.Facets)
	}
}

func TestHandleForseerClusters_EmptyWithoutEngine(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/clusters", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var got []any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %+v, want []", got)
	}
}

func TestHandleForseerClassify_NilEngineFallsBackToTheRule(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/classify?message=connection+refused+error", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["source"] != "rule" {
		t.Errorf("source = %v, want rule with no engine attached", body["source"])
	}
	if body["ready"] != false {
		t.Errorf("ready = %v, want false with no engine attached", body["ready"])
	}
	if body["severity"] == "" {
		t.Error("severity is empty, want the fallback rule's answer")
	}
}

func TestHandleForseerClassify_ColdEngineFallsBackToTheRule(t *testing.T) {
	eng := forseer.NewEngine()
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithForseer(eng)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/classify?message=something+went+wrong", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// A freshly built engine has seen nothing, so it can't have an opinion
	// yet — the handler must fall back rather than claim "model".
	if body["source"] != "rule" {
		t.Errorf("source = %v, want rule for a cold engine", body["source"])
	}
	if body["ready"] != false {
		t.Errorf("ready = %v, want false for a cold engine", body["ready"])
	}
}

func TestHandleForseerClassify_RejectsAnEmptyMessage(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/classify?message=", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleForseerClassify_RejectsAnOversizedMessage(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/forseer/classify?message="+strings.Repeat("a", 4097), nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleForseerClassify_AcceptsAMessageAtTheFourKilobyteBoundary(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/forseer/classify?message="+strings.Repeat("a", 4096), nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 at exactly the 4096-byte limit (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleForseerClassify_WarmEngineAnswersFromTheModel(t *testing.T) {
	eng := forseer.NewEngine()
	// Same shape as forseer/engine_test.go's warm-up: two declared levels,
	// enough repetitions that the severity model is ready to answer.
	for i := 0; i < 60; i++ {
		eng.ObserveLogs([]forseer.LogLine{
			{Message: "request completed cleanly", Severity: "info", Source: "api"},
			{Message: "no errors reported during the sweep", Severity: "info", Source: "api"},
			{Message: "panic nil map write in handler", Severity: "error", Source: "api"},
			{Message: "could not reach the database cluster", Severity: "error", Source: "api"},
		})
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithForseer(eng)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/forseer/classify?message=panic+nil+map+write+in+the+checkout+handler", nil)
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["source"] != "model" {
		t.Errorf("source = %v, want model for a warm engine", body["source"])
	}
	if body["ready"] != true {
		t.Errorf("ready = %v, want true for a warm engine", body["ready"])
	}
	if body["severity"] != "error" {
		t.Errorf("severity = %v, want error", body["severity"])
	}
}

type otlpRegisterFunc func(mux *http.ServeMux)

func (f otlpRegisterFunc) Register(mux *http.ServeMux) { f(mux) }

func TestForseerModels_DescribesEachTrainedModel(t *testing.T) {
	eng := forseer.NewEngine()
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithForseer(eng)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/models", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	var cards []forseer.Card
	if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(cards) == 0 {
		t.Fatal("no model cards returned")
	}
	for _, card := range cards {
		if card.Name == "" || card.Job == "" {
			t.Errorf("card %+v does not name its job", card)
		}
		if len(card.Reads) == 0 {
			t.Errorf("model %q declares no inputs", card.Name)
		}
		if card.Fallback == "" {
			t.Errorf("model %q names no fallback", card.Name)
		}
	}
}

func TestForseerModels_ReportsAColdModelAsNotReady(t *testing.T) {
	eng := forseer.NewEngine()
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithForseer(eng)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/models", nil))

	var cards []forseer.Card
	if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, card := range cards {
		if card.Ready {
			t.Errorf("model %q claims to be ready having learned nothing", card.Name)
		}
		if card.Accuracy != forseer.Unmeasured {
			t.Errorf("model %q reports accuracy %.2f before grading anything", card.Name, card.Accuracy)
		}
	}
}

func TestForseerModels_EmptyWithoutAnEngine(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/forseer/models", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body %q, want []", body)
	}
}

// pingFailingStore wraps a real Store so handleReadyz's 503 path can be
// exercised without needing a real failure mode of a concrete store — only
// Ping's result differs from the store it wraps (BadgerStore's own genuine
// failure, a closed database, is covered by
// internal/store's TestBadgerStore_PingFailsAfterClose).
type pingFailingStore struct {
	store.Store
	err error
}

func (s pingFailingStore) Ping(context.Context) error { return s.err }

// fakeCollector is a minimal collector.Collector for exercising
// handleReadyz's registry-attached path without pulling in a real one.
type fakeCollector struct{ name string }

func (c fakeCollector) Name() string { return c.name }
func (c fakeCollector) Collect(context.Context) ([]model.Metric, error) {
	return nil, nil
}

func TestHandleReadyz_ReadyWhenStoreIsHealthy(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "ready" {
		t.Errorf("status = %q, want ready", body.Status)
	}
	if body.StoreError != "" {
		t.Errorf("storeError = %q, want empty", body.StoreError)
	}
}

func TestHandleReadyz_UnavailableWhenStorePingFails(t *testing.T) {
	failing := pingFailingStore{Store: store.NewMemoryStore(time.Hour), err: errors.New("disk full")}
	s := NewServer(failing, nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body: %s)", rec.Code, rec.Body.String())
	}
	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "unavailable" {
		t.Errorf("status = %q, want unavailable", body.Status)
	}
	if body.StoreError != "disk full" {
		t.Errorf("storeError = %q, want %q", body.StoreError, "disk full")
	}
}

// TestHandleReadyz_CollectorErrorDoesNotFailTheProbe is the regression test
// for the Why in ROADMAP.md item 24: a collector's own error (a missing
// Docker socket, say) must never turn /readyz's 200 into a 503 — only the
// store's own Ping does that. Reporting it is still expected, just not as a
// failure.
func TestHandleReadyz_CollectorErrorDoesNotFailTheProbe(t *testing.T) {
	st := store.NewMemoryStore(time.Hour)
	registry := collector.NewRegistry(st, time.Hour, nil, fakeCollector{name: "docker"})
	s := NewServer(st, nil, nil, nil).WithRegistry(registry)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body readyzResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(body.Collectors) != 1 || body.Collectors[0].Name != "docker" {
		t.Errorf("collectors = %+v, want one entry named docker", body.Collectors)
	}
}

// TestHandleReadyz_OmitsCollectorsWithoutARegistry covers cmd/demo's
// wiring: no Registry means nothing meaningful to report, so the field is
// left out of the body entirely rather than an empty array.
func TestHandleReadyz_OmitsCollectorsWithoutARegistry(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if _, ok := raw["collectors"]; ok {
		t.Errorf("body has a collectors field with no registry attached: %s", rec.Body.String())
	}
}
