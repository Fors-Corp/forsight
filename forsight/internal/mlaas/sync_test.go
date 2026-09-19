package mlaas

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Fors-Corp/forsight/forsight/internal/model"
	"github.com/Fors-Corp/forsight/forsight/internal/store"
)

// fakeMlaas is an in-memory mlaas: the routes the integration uses, with
// the same status codes and JSON shapes as internal/api/server.go, and a
// request log the tests assert on.
type fakeMlaas struct {
	t   *testing.T
	srv *httptest.Server
	key string

	mu          sync.Mutex
	datasets    map[string]*fakeDataset
	models      map[string]*fakeModel
	jobs        []wireJob
	predictions map[string]string // prediction id -> model
	labels      map[string]string // prediction id -> label
	nextID      int
	// hidden names are left out of GET /models, so POST /models answers
	// 409 for them: "created between the list and the create".
	hidden map[string]bool
	// predictStatus, when set for a model, is answered instead of a
	// prediction.
	predictStatus map[string]int
	// predictValues, when set for a model, is served as the predictions'
	// values verbatim instead of the fixed 40+i / "error" answers below —
	// enough rope for a test to hand back the wrong count, or a value that
	// does not parse as a float.
	predictValues map[string][]string
	// healthzOverride, when non-empty, is served as GET /healthz's literal
	// body (still 200) instead of {"ok":true} — a test's way to simulate an
	// upstream that answers but not with valid JSON.
	healthzOverride string
	// version, when non-empty, is served as GET /healthz's "version" field
	// — a test's way to simulate an mlaas release, and to change it
	// between passes to simulate an upgrade.
	version  string
	requests []string // "METHOD /path?query"
}

type fakeDataset struct {
	header []string
	rows   [][]string
}

type fakeModel struct {
	spec     wireSpec
	classes  []string
	champion *wireVersion
	check    *wireCheck
	served   int
}

func newFakeMlaas(t *testing.T) *fakeMlaas {
	t.Helper()
	f := &fakeMlaas{
		t: t, key: "k",
		datasets: map[string]*fakeDataset{}, models: map[string]*fakeModel{},
		predictions: map[string]string{}, labels: map[string]string{},
		hidden: map[string]bool{}, predictStatus: map[string]int{},
		predictValues: map[string][]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", f.healthz)
	mux.HandleFunc("GET /datasets", f.listDatasets)
	mux.HandleFunc("POST /datasets", f.uploadDataset)
	mux.HandleFunc("GET /models", f.listModels)
	mux.HandleFunc("POST /models", f.createModel)
	mux.HandleFunc("GET /models/{name}", f.getModel)
	mux.HandleFunc("GET /models/{name}/health", f.health)
	mux.HandleFunc("POST /models/{name}/train", func(w http.ResponseWriter, r *http.Request) { f.enqueue(w, r, "train") })
	mux.HandleFunc("POST /models/{name}/tune", func(w http.ResponseWriter, r *http.Request) { f.enqueue(w, r, "tune") })
	mux.HandleFunc("POST /models/{name}/predict", f.predict)
	mux.HandleFunc("GET /models/{name}/actual", f.actual)
	mux.HandleFunc("POST /feedback", f.feedback)
	mux.HandleFunc("GET /jobs", f.listJobs)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		if r.URL.Path != "/healthz" && r.Header.Get("X-API-Key") != f.key {
			f.json(w, 401, map[string]any{"error": "missing or wrong X-API-Key"})
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeMlaas) healthz(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	override := f.healthzOverride
	version := f.version
	f.mu.Unlock()
	if override != "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, override)
		return
	}
	body := map[string]any{"ok": true}
	if version != "" {
		body["version"] = version
	}
	f.json(w, 200, body)
}

func (f *fakeMlaas) json(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeMlaas) dataset(name string) wireDataset {
	d := f.datasets[name]
	return wireDataset{Name: name, Path: "data/datasets/" + name + ".csv", Columns: d.header, Rows: len(d.rows)}
}

func (f *fakeMlaas) listDatasets(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []wireDataset{}
	for name := range f.datasets {
		out = append(out, f.dataset(name))
	}
	f.json(w, 200, out)
}

func (f *fakeMlaas) uploadDataset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		f.json(w, 400, map[string]any{"error": err.Error()})
		return
	}
	name := r.FormValue("name")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		f.json(w, 400, map[string]any{"error": "missing file field"})
		return
	}
	defer func() { _ = file.Close() }()
	if hdr.Filename != name+".csv" {
		f.t.Errorf("upload filename = %q, want %s.csv", hdr.Filename, name)
	}
	raw, _ := io.ReadAll(file)
	recs, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil || len(recs) == 0 {
		f.json(w, 400, map[string]any{"error": "bad csv"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.datasets[name] = &fakeDataset{header: recs[0], rows: recs[1:]}
	f.json(w, 201, f.dataset(name))
}

func (f *fakeMlaas) listed(name string, m *fakeModel) map[string]any {
	out := map[string]any{"name": name, "spec": m.spec, "champion_version": nil, "last_retrain_at": nil, "last_check": m.check, "created_at": base}
	if m.classes != nil {
		out["classes"] = m.classes
	}
	if m.champion != nil {
		out["champion_version"] = m.champion.ID
		out["champion"] = m.champion
	}
	return out
}

func (f *fakeMlaas) listModels(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []map[string]any{}
	for name, m := range f.models {
		if f.hidden[name] {
			continue
		}
		out = append(out, f.listed(name, m))
	}
	f.json(w, 200, out)
}

func (f *fakeMlaas) createModel(w http.ResponseWriter, r *http.Request) {
	var spec wireSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		f.json(w, 400, map[string]any{"error": "bad JSON body"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.datasets[spec.Dataset]
	if !ok {
		f.json(w, 400, map[string]any{"error": fmt.Sprintf("dataset %q does not exist", spec.Dataset)})
		return
	}
	if !slices.Contains(d.header, spec.Target) {
		f.json(w, 400, map[string]any{"error": fmt.Sprintf("target %q is not a column of the dataset", spec.Target)})
		return
	}
	if spec.Task == "forecast" && !slices.Contains(d.header, spec.Timestamp) {
		f.json(w, 400, map[string]any{"error": "forecast task requires a timestamp column"})
		return
	}
	if _, exists := f.models[spec.Name]; exists {
		f.json(w, 409, map[string]any{"error": "model already exists"})
		return
	}
	// The agent never sets drift_threshold (wireRetrain's omitempty), the
	// same way a real POST /models leaves it out; mlaas fills its own
	// default in, same as store.go's RetrainPolicy does.
	if spec.Retrain.DriftThreshold == 0 {
		spec.Retrain.DriftThreshold = 0.2
	}
	m := &fakeModel{spec: spec}
	if spec.Task == "classification" {
		ti := slices.Index(d.header, spec.Target)
		seen := map[string]bool{}
		for _, row := range d.rows {
			if !seen[row[ti]] {
				seen[row[ti]] = true
				m.classes = append(m.classes, row[ti])
			}
		}
	}
	f.models[spec.Name] = m
	f.json(w, 201, f.listed(spec.Name, m))
}

// getModel is GET /models/{name}: the shape a real mlaas answers with is
// {"model": {...}, "versions": [...]}; the fake's versions list is always
// empty since nothing here reads it.
func (f *fakeMlaas) getModel(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.PathValue("name")
	m, ok := f.models[name]
	if !ok {
		f.json(w, 404, map[string]any{"error": "not found"})
		return
	}
	f.json(w, 200, map[string]any{"model": f.listed(name, m), "versions": []any{}})
}

func (f *fakeMlaas) activeLocked(name string) bool {
	for _, j := range f.jobs {
		if j.Model == name && (j.Status == "queued" || j.Status == "running") {
			return true
		}
	}
	return false
}

func (f *fakeMlaas) health(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.PathValue("name")
	m, ok := f.models[name]
	if !ok {
		f.json(w, 404, map[string]any{"error": "not found"})
		return
	}
	out := map[string]any{"model": name, "metric": m.spec.Metric, "last_check": m.check, "last_retrain_at": nil,
		"active_job": f.activeLocked(name), "predictions_logged": m.served}
	if m.champion != nil {
		out["champion"] = m.champion
	}
	f.json(w, 200, out)
}

func (f *fakeMlaas) enqueue(w http.ResponseWriter, r *http.Request, kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.PathValue("name")
	if _, ok := f.models[name]; !ok {
		f.json(w, 404, map[string]any{"error": "not found"})
		return
	}
	for _, j := range f.jobs {
		if j.Model == name && j.Status == "queued" && j.Kind == kind {
			f.json(w, 202, map[string]any{"job_id": j.ID, "status": "queued", "already_queued": true})
			return
		}
	}
	f.nextID++
	f.jobs = append(f.jobs, wireJob{ID: int64(f.nextID), Model: name, Kind: kind, Trigger: "manual", Status: "queued", CreatedAt: base})
	f.json(w, 202, map[string]any{"job_id": f.nextID, "status": "queued", "already_queued": false})
}

func (f *fakeMlaas) predict(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name := r.PathValue("name")
	m, ok := f.models[name]
	if !ok {
		f.json(w, 404, map[string]any{"error": "not found"})
		return
	}
	if code := f.predictStatus[name]; code != 0 {
		f.json(w, code, map[string]any{"error": "predict failed on purpose"})
		return
	}
	if m.champion == nil {
		f.json(w, 409, map[string]any{"error": "model has no trained version yet; POST /models/{name}/train first"})
		return
	}
	var req struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Rows) == 0 {
		f.json(w, 400, map[string]any{"error": `body must be {"rows":[{...}]}`})
		return
	}
	// An overridden value list ignores the request rows entirely: a test's
	// way to answer the wrong count, or a value that will not parse as a
	// float, without needing the client to send a matching row count.
	if vals, ok := f.predictValues[name]; ok {
		out := make([]map[string]any, len(vals))
		for i, v := range vals {
			f.nextID++
			id := "p" + strconv.Itoa(f.nextID)
			f.predictions[id] = name
			m.served++
			out[i] = map[string]any{"prediction_id": id, "value": v}
		}
		f.json(w, 200, map[string]any{"model": name, "version": m.champion.Number, "predictions": out})
		return
	}
	features := m.spec.Features
	if m.spec.Task == "forecast" {
		features = []string{m.spec.Timestamp}
	}
	out := make([]map[string]any, 0, len(req.Rows))
	for i, row := range req.Rows {
		for _, feat := range features {
			if _, ok := row[feat]; !ok {
				f.json(w, 400, map[string]any{"error": fmt.Sprintf("row %d is missing feature %q", i, feat)})
				return
			}
		}
		f.nextID++
		id := "p" + strconv.Itoa(f.nextID)
		f.predictions[id] = name
		m.served++
		// Forecasts answer 40, 41, 42, 43 for the four horizons; the
		// classifier always says "error", so a disagreement with an "info"
		// line is visible in the table.
		val := "error"
		if m.spec.Task == "forecast" {
			val = strconv.FormatFloat(40+float64(i), 'g', -1, 64)
		}
		out = append(out, map[string]any{"prediction_id": id, "value": val})
	}
	f.json(w, 200, map[string]any{"model": name, "version": m.champion.Number, "predictions": out})
}

func (f *fakeMlaas) actual(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.models[r.PathValue("name")]
	if !ok {
		f.json(w, 404, map[string]any{"error": "not found"})
		return
	}
	if m.spec.Task != "forecast" {
		f.json(w, 400, map[string]any{"error": "actual values exist for forecast models only"})
		return
	}
	if _, err := time.Parse(time.RFC3339, r.URL.Query().Get("at")); err != nil {
		f.json(w, 400, map[string]any{"error": "?at= must be a date or time"})
		return
	}
	f.json(w, 200, map[string]any{"model": r.PathValue("name"), "at": r.URL.Query().Get("at"), "found": false, "live": false, "refreshed": false, "labeled": 0})
}

func (f *fakeMlaas) feedback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Labels []wireLabel `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Labels) == 0 {
		f.json(w, 400, map[string]any{"error": "body must be {labels:[...]}"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	unknown := []string{}
	for _, l := range req.Labels {
		modelName, ok := f.predictions[l.PredictionID]
		if !ok {
			unknown = append(unknown, l.PredictionID)
			continue
		}
		if m := f.models[modelName]; len(m.classes) > 0 && !slices.Contains(m.classes, l.Label) {
			f.json(w, 400, map[string]any{"error": fmt.Sprintf("label %q is not a class of model %s", l.Label, modelName)})
			return
		}
	}
	for _, l := range req.Labels {
		if _, ok := f.predictions[l.PredictionID]; ok {
			f.labels[l.PredictionID] = l.Label
		}
	}
	f.json(w, 200, map[string]any{"accepted": len(req.Labels) - len(unknown), "unknown_prediction_ids": unknown})
}

func (f *fakeMlaas) listJobs(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out := []wireJob{}
	for i := len(f.jobs) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, f.jobs[i])
	}
	f.json(w, 200, out)
}

// promote gives a model a champion with a holdout score and marks its
// queued jobs done, the way a finished training job would.
func (f *fakeMlaas) promote(name string, number int, metric string, score float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.models[name]
	if m == nil {
		f.t.Fatalf("promote: no model %s", name)
	}
	metrics, _ := json.Marshal(map[string]any{"holdout": map[string]float64{metric: score}})
	f.nextID++
	m.champion = &wireVersion{ID: int64(f.nextID), Model: name, Number: number, Status: "champion", Metrics: metrics, TrainedAt: base}
	for i := range f.jobs {
		if f.jobs[i].Model == name && f.jobs[i].Status == "queued" {
			f.jobs[i].Status = "done"
		}
	}
}

// count is how many logged requests start with a prefix; countExact how
// many are exactly that line.
func (f *fakeMlaas) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeMlaas) countExact(line string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r == line {
			n++
		}
	}
	return n
}

func (f *fakeMlaas) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
}

// ---- fixtures ----

// minute is the bucket base falls in; fixtures are laid out from it so a
// sample never straddles a bucket boundary by accident.
var minute = base.Truncate(time.Minute)

// seedStore fills a store with enough of everything: 40 one-minute buckets
// of each host series, the newest two minutes before base's bucket, and 60
// declared log lines (plus a few inferred ones that must never be
// exported).
func seedStore(t *testing.T) *store.MemoryStore {
	t.Helper()
	// Retention is measured from the real clock; base is fixed, so give
	// the store a window nothing in these tests can fall out of.
	st := store.NewMemoryStore(20 * 365 * 24 * time.Hour)
	var metrics []model.Metric
	for _, name := range []string{"host.cpu.percent", "host.memory.percent", "host.disk.percent"} {
		for i := 0; i < 40; i++ {
			at := minute.Add(-time.Duration(41-i) * time.Minute)
			metrics = append(metrics,
				model.Metric{Name: name, Value: float64(i), Timestamp: at.Add(5 * time.Second)},
				model.Metric{Name: name, Value: float64(i) + 2, Timestamp: at.Add(35 * time.Second)},
			)
		}
	}
	if err := st.WriteMetrics(context.Background(), metrics); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteLogs(context.Background(), seedLogs(0, 60)); err != nil {
		t.Fatal(err)
	}
	return st
}

// hostMetrics reproduces seedStore's per-series bucket layout for buckets
// i in [from, to): two samples per minute, at minute.Add(-(41-i)*time.Minute).
// A test that wants to move the window itself — rather than wait on the
// store's own real-clock retention — calls this with a shifted range.
func hostMetrics(name string, from, to int) []model.Metric {
	var out []model.Metric
	for i := from; i < to; i++ {
		at := minute.Add(-time.Duration(41-i) * time.Minute)
		out = append(out,
			model.Metric{Name: name, Value: float64(i), Timestamp: at.Add(5 * time.Second)},
			model.Metric{Name: name, Value: float64(i) + 2, Timestamp: at.Add(35 * time.Second)},
		)
	}
	return out
}

// slidingMetricsStore answers QueryMetrics from a swappable, name-keyed set
// of points instead of the wrapped store's own — everything else (logs,
// writes) delegates through. It exists so a test can simulate a retention
// window that has slid (new points arrived, old ones aged out, the exported
// row count unchanged) on demand, rather than depend on real wall-clock
// pruning, which MemoryStore ties to time.Now rather than to a Syncer's
// (fake, overridable) clock.
type slidingMetricsStore struct {
	store.Store
	mu      sync.Mutex
	metrics map[string][]model.Metric
}

func (s *slidingMetricsStore) QueryMetrics(_ context.Context, q store.MetricQuery) ([]model.Metric, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.Metric(nil), s.metrics[q.Name]...), nil
}

func (s *slidingMetricsStore) setMetrics(name string, m []model.Metric) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.metrics == nil {
		s.metrics = map[string][]model.Metric{}
	}
	s.metrics[name] = m
}

// seedLogs makes declared lines i in [from, to): even ones "info", odd
// ones "error", one every 30 seconds ending an hour before base, plus an
// inferred line after each fifth.
func seedLogs(from, to int) []model.LogEntry {
	var logs []model.LogEntry
	for i := from; i < to; i++ {
		sev := model.LogSeverityInfo
		if i%2 == 1 {
			sev = model.LogSeverityError
		}
		at := base.Add(-time.Hour + time.Duration(i)*30*time.Second)
		logs = append(logs, model.LogEntry{Timestamp: at, Severity: sev, Source: "app", Message: fmt.Sprintf("line %03d happened", i)})
		if i%5 == 4 {
			logs = append(logs, model.LogEntry{Timestamp: at.Add(time.Second), Severity: "warn", Source: "file", Message: "guessed", SeverityInferred: true})
		}
	}
	return logs
}

func newSyncer(t *testing.T, f *fakeMlaas, st store.Store) *Syncer {
	t.Helper()
	classify := func(msg string) (string, bool) {
		if strings.Contains(msg, "line 05") {
			return "", false // "not ready" for a few lines
		}
		return "info", true
	}
	s, err := New(Config{URL: f.srv.URL, APIKey: f.key}, st, classify, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return base }
	return s
}

func modelByName(t *testing.T, st Status, name string) ModelStatus {
	t.Helper()
	for _, m := range st.Models {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no model %s in %+v", name, st.Models)
	return ModelStatus{}
}

// ---- tests ----

func TestNew_ValidatesConfig(t *testing.T) {
	for _, bad := range []string{"localhost:8090", "8090", "ftp://x", "http://"} {
		if _, err := New(Config{URL: bad}, store.NewMemoryStore(time.Hour), nil, nil); err == nil {
			t.Errorf("URL %q accepted", bad)
		}
	}
	if _, err := New(Config{URL: "http://127.0.0.1:8090"}, nil, nil, nil); err == nil {
		t.Error("a configured syncer without a store was accepted")
	}
	s, err := New(Config{URL: "http://127.0.0.1:8090"}, store.NewMemoryStore(time.Hour), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.cfg.Prefix != "forsight" || s.cfg.SyncInterval != defaultSyncInterval {
		t.Errorf("defaults not applied: %+v", s.cfg)
	}
}

func TestNew_ValidatesPrefix(t *testing.T) {
	// "memory-forecast" is the longest suffix, so a 60-byte prefix is the
	// shortest one that overruns mlaas's 64-byte whole-name limit
	// (60 + "-" + 15 = 76) without needing an implausibly long input.
	for _, bad := range []string{"forsight team", strings.Repeat("a", 60)} {
		if _, err := New(Config{Prefix: bad}, store.NewMemoryStore(time.Hour), nil, nil); err == nil {
			t.Errorf("prefix %q accepted", bad)
		}
	}
	for _, good := range []string{"forsight", "agent-a"} {
		if _, err := New(Config{Prefix: good}, store.NewMemoryStore(time.Hour), nil, nil); err != nil {
			t.Errorf("prefix %q rejected: %v", good, err)
		}
	}
}

func TestNew_RedactsURLCredentials(t *testing.T) {
	s, err := New(Config{URL: "http://user:pass@127.0.0.1:1/"}, store.NewMemoryStore(time.Hour), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://user:xxxxx@127.0.0.1:1/"; s.DisplayURL() != want {
		t.Errorf("DisplayURL() = %q, want %q", s.DisplayURL(), want)
	}
	st := s.Status(context.Background())
	if want := "http://user:xxxxx@127.0.0.1:1/"; st.URL != want {
		t.Errorf("Status().URL = %q, want %q (the password must never appear)", st.URL, want)
	}
}

func TestNotConfigured(t *testing.T) {
	s, err := New(Config{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Status(context.Background())
	if st.Configured || st.Reachable || st.Models == nil || st.Forecasts == nil || st.Predictions == nil || st.Jobs == nil || len(st.Models) != 0 {
		t.Errorf("unconfigured status = %+v, want Configured=false and empty non-nil slices", st)
	}
	raw, _ := json.Marshal(st)
	if !strings.Contains(string(raw), `"models":[]`) {
		t.Errorf("unconfigured status serialises as %s", raw)
	}
	if _, err := s.Train(context.Background(), "forsight-cpu-forecast"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Train = %v, want ErrNotConfigured", err)
	}
	if _, err := s.Predict(context.Background(), "forsight-log-severity", []map[string]any{{"message": "x"}}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Predict = %v, want ErrNotConfigured", err)
	}
	done := make(chan struct{})
	go func() { s.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Run did not return at once when not configured")
	}
}

func TestFirstPass_UploadsCreatesAndTrains(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())

	f.mu.Lock()
	for _, ds := range []string{"forsight-host-cpu", "forsight-host-memory", "forsight-host-disk"} {
		d := f.datasets[ds]
		if d == nil || len(d.rows) != 40 || d.header[0] != "at" || d.header[1] != "value" {
			t.Errorf("dataset %s = %+v, want 40 at,value rows", ds, d)
			continue
		}
		// Bucket 0 held samples 0 and 2: mean 1.
		if d.rows[0][1] != "1" || d.rows[0][0] != minute.Add(-41*time.Minute).Format(timeLayout) {
			t.Errorf("%s first row = %v", ds, d.rows[0])
		}
	}
	if d := f.datasets["forsight-logs"]; d == nil || len(d.rows) != 60 || d.header[0] != "message" || d.header[1] != "severity" {
		t.Errorf("logs dataset = %+v, want 60 message,severity rows", d)
	} else {
		for _, r := range d.rows {
			if r[0] == "guessed" {
				t.Error("an inferred line was exported")
			}
		}
	}
	for _, name := range Names("forsight") {
		m := f.models[name]
		if m == nil {
			t.Errorf("model %s not created", name)
			continue
		}
		if m.spec.Tags[0] != "forsight" || m.spec.Params == nil {
			t.Errorf("model %s spec = %+v", name, m.spec)
		}
		if !f.activeLocked(name) {
			t.Errorf("model %s has no training job queued", name)
		}
	}
	if sev := f.models["forsight-log-severity"]; sev != nil && len(sev.classes) != 2 {
		t.Errorf("classifier classes = %v", sev.classes)
	}
	f.mu.Unlock()

	st := s.Status(context.Background())
	if !st.Reachable || st.LastSync.IsZero() || st.LastError != "" {
		t.Errorf("status = reachable %v lastSync %v lastError %q", st.Reachable, st.LastSync, st.LastError)
	}
	if got := Names("forsight"); len(st.Models) != 4 || st.Models[0].Name != got[0] || st.Models[3].Name != got[3] {
		t.Errorf("models listed as %+v, want table order", st.Models)
	}
	for _, m := range st.Models {
		if m.State != StateTraining || !m.ActiveJob || m.Champion != 0 {
			t.Errorf("%s state = %s (activeJob %v), want training", m.Name, m.State, m.ActiveJob)
		}
		if m.DatasetRows == 0 || m.Job == "" || len(m.Reads) == 0 {
			t.Errorf("%s = %+v, missing static or dataset fields", m.Name, m)
		}
	}
	if len(st.Jobs) != 4 || st.Jobs[0].Kind != "train" || st.Jobs[0].Status != "queued" {
		t.Errorf("jobs = %+v, want the four queued trainings", st.Jobs)
	}
	if len(st.Forecasts) != 0 || len(st.Predictions) != 0 {
		t.Errorf("forecasts/predictions before any champion: %+v %+v", st.Forecasts, st.Predictions)
	}
}

// TestSyncModel_CreatesUploadsAndTrains exercises syncModel directly on a
// single managed model — the block pass used to inline before the split —
// rather than through a whole pass, so a regression here points straight at
// syncModel instead of somewhere in pass's other three model iterations. It
// checks both syncModel's own side effects on mlaas (dataset uploaded, model
// created, training queued) and the maps pass depends on afterward
// (exports, byName, rowsByDataset), which is exactly what pass's later
// forecast/feedback loops and second visit to a model read.
func TestSyncModel_CreatesUploadsAndTrains(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	m := managed[0] // cpu-forecast: plain create-and-train, no 409 to recover from
	name, ds := m.name(s.cfg.Prefix), m.dataset(s.cfg.Prefix)

	byName := map[string]wireModel{}
	rowsByDataset := map[string]int{}
	exports := map[string][]row{}
	loadLogs := func() ([]model.LogEntry, error) {
		t.Fatal("a forecast model's syncModel must not read logs")
		return nil, nil
	}
	var errs []error
	note := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	s.syncModel(context.Background(), m, byName, rowsByDataset, exports, loadLogs, note)

	if len(errs) != 0 {
		t.Fatalf("syncModel noted errors: %v", errs)
	}
	if rows, ok := exports[name]; !ok || len(rows) != 40 {
		t.Errorf("exports[%s] = %v, want 40 rows", name, rows)
	}
	if got := rowsByDataset[ds]; got != 40 {
		t.Errorf("rowsByDataset[%s] = %d, want 40 (updated after upload)", ds, got)
	}
	wm, known := byName[name]
	if !known {
		t.Fatalf("byName[%s] not set after syncModel created it", name)
	}
	if wm.Champion != nil {
		t.Errorf("Champion = %+v, want nil before any training completes", wm.Champion)
	}

	f.mu.Lock()
	fm := f.models[name]
	trained := f.activeLocked(name)
	d := f.datasets[ds]
	f.mu.Unlock()
	if fm == nil {
		t.Fatalf("model %s not created upstream", name)
	}
	if !trained {
		t.Errorf("model %s has no training job queued", name)
	}
	if d == nil || len(d.rows) != 40 {
		t.Errorf("dataset %s = %+v, want 40 rows uploaded", ds, d)
	}

	s.mu.Lock()
	enough := s.exported[name]
	s.mu.Unlock()
	if !enough {
		t.Errorf("s.exported[%s] = false, want true", name)
	}
}

// TestSyncModel_NotEnoughDataSkipsUploadAndCreate checks syncModel's early
// return: with nothing in the store, the export is too small to register,
// so mlaas must never see a create or an upload, and exports/byName must
// stay untouched for pass's later loops to read as "not exported this pass".
func TestSyncModel_NotEnoughDataSkipsUploadAndCreate(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, store.NewMemoryStore(time.Hour)) // empty: no metrics, no logs
	m := managed[0]                                       // cpu-forecast
	name := m.name(s.cfg.Prefix)

	byName := map[string]wireModel{}
	rowsByDataset := map[string]int{}
	exports := map[string][]row{}
	loadLogs := func() ([]model.LogEntry, error) { return nil, nil }
	var errs []error
	note := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	s.syncModel(context.Background(), m, byName, rowsByDataset, exports, loadLogs, note)

	if len(errs) != 0 {
		t.Fatalf("syncModel noted errors on the not-enough-data path: %v", errs)
	}
	if _, ok := exports[name]; ok {
		t.Errorf("exports[%s] set despite not enough rows", name)
	}
	if _, ok := byName[name]; ok {
		t.Errorf("byName[%s] set despite not enough rows: model must not be created", name)
	}
	f.mu.Lock()
	created := f.models[name] != nil
	f.mu.Unlock()
	if created {
		t.Errorf("model %s created upstream despite not enough data", name)
	}
	s.mu.Lock()
	enough := s.exported[name]
	s.mu.Unlock()
	if enough {
		t.Errorf("s.exported[%s] = true, want false", name)
	}
}

// TestPass_RecordsMlaasVersionOnChange checks that the version mlaas
// reports on GET /healthz is recorded, and that an upgrade between two
// sync passes — mlaas swapped out underneath a running agent — is
// observed. It asserts on Syncer's own recorded state (mlaasVersion)
// rather than on log text: that field only ever changes through
// noteVersion, so if version-tracking were removed or never wired into a
// pass, mlaasVersion would stay "" and this test would fail.
func TestPass_RecordsMlaasVersionOnChange(t *testing.T) {
	f := newFakeMlaas(t)
	f.version = "v1.8.0"
	s := newSyncer(t, f, seedStore(t))

	s.pass(context.Background())
	s.mu.Lock()
	got := s.mlaasVersion
	s.mu.Unlock()
	if got != "v1.8.0" {
		t.Fatalf("mlaasVersion after first pass = %q, want v1.8.0", got)
	}

	// A pass with the same version reported is the routine case: still
	// recorded, nothing to observe as a change.
	s.pass(context.Background())
	s.mu.Lock()
	got = s.mlaasVersion
	s.mu.Unlock()
	if got != "v1.8.0" {
		t.Fatalf("mlaasVersion after an unchanged pass = %q, want v1.8.0", got)
	}

	// mlaas is upgraded underneath the running agent.
	f.mu.Lock()
	f.version = "v1.9.0"
	f.mu.Unlock()

	s.pass(context.Background())
	s.mu.Lock()
	got = s.mlaasVersion
	s.mu.Unlock()
	if got != "v1.9.0" {
		t.Fatalf("mlaasVersion after an upgrade = %q, want v1.9.0", got)
	}
}

// TestPass_OnlyModelsWithEnoughDataAreUploadedCreatedAndTrained checks the
// per-model minimums independently: enough rows (cpu, 30 buckets) goes all
// the way to a queued training; one short (memory, 29 buckets) or absent
// (disk, no rows) never reaches mlaas at all; enough rows but one class
// (the classifier, all "info") is the same — enough() gates on distinct
// labels too, not just the count.
func TestPass_OnlyModelsWithEnoughDataAreUploadedCreatedAndTrained(t *testing.T) {
	f := newFakeMlaas(t)
	st := store.NewMemoryStore(20 * 365 * 24 * time.Hour)
	var metrics []model.Metric
	for i := 0; i < minForecastRows; i++ { // cpu: exactly enough
		at := minute.Add(-time.Duration(minForecastRows-i) * time.Minute)
		metrics = append(metrics, model.Metric{Name: "host.cpu.percent", Value: float64(i), Timestamp: at.Add(5 * time.Second)})
	}
	for i := 0; i < minForecastRows-1; i++ { // memory: one short
		at := minute.Add(-time.Duration(minForecastRows-1-i) * time.Minute)
		metrics = append(metrics, model.Metric{Name: "host.memory.percent", Value: float64(i), Timestamp: at.Add(5 * time.Second)})
	}
	// disk: no data at all.
	if err := st.WriteMetrics(context.Background(), metrics); err != nil {
		t.Fatal(err)
	}
	var logs []model.LogEntry
	for i := 0; i < 60; i++ { // enough rows, but a single declared class.
		logs = append(logs, model.LogEntry{
			Timestamp: base.Add(-time.Hour + time.Duration(i)*30*time.Second),
			Severity:  model.LogSeverityInfo, Source: "app", Message: fmt.Sprintf("line %03d happened", i),
		})
	}
	if err := st.WriteLogs(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	s := newSyncer(t, f, st)
	s.pass(context.Background())

	if n := f.countExact("POST /models"); n != 1 {
		t.Errorf("%d models created, want exactly 1 (cpu)", n)
	}
	f.mu.Lock()
	_, cpuUploaded := f.datasets["forsight-host-cpu"]
	_, memUploaded := f.datasets["forsight-host-memory"]
	_, diskUploaded := f.datasets["forsight-host-disk"]
	_, logsUploaded := f.datasets["forsight-logs"]
	f.mu.Unlock()
	if !cpuUploaded {
		t.Error("cpu forecast (enough rows) was not uploaded")
	}
	if memUploaded {
		t.Error("memory forecast (one row short) was uploaded")
	}
	if diskUploaded {
		t.Error("disk forecast (no rows) was uploaded")
	}
	if logsUploaded {
		t.Error("the classifier (a single declared class) was uploaded")
	}

	status := s.Status(context.Background())
	cpu := modelByName(t, status, "forsight-cpu-forecast")
	if cpu.State != StateTraining {
		t.Errorf("cpu state = %s, want training", cpu.State)
	}
	for _, name := range []string{"forsight-memory-forecast", "forsight-disk-forecast", "forsight-log-severity"} {
		if m := modelByName(t, status, name); m.State != StateWaitingForData {
			t.Errorf("%s state = %s, want waiting-for-data", name, m.State)
		}
	}
}

func TestSecondPass_UnchangedRowsNoReupload(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	f.reset()
	s.pass(context.Background())
	if n := f.count("POST /datasets"); n != 0 {
		t.Errorf("second pass uploaded %d datasets with unchanged rows", n)
	}
	if n := f.count("POST /models"); n != 0 {
		t.Errorf("second pass re-created %d models", n)
	}
	if n := f.count("POST /models/forsight-cpu-forecast/train"); n != 0 {
		t.Error("second pass queued another training while one was already queued")
	}
	// A new minute of data changes the row count: that dataset, and only
	// that one, is re-uploaded.
	st := s.store.(*store.MemoryStore)
	_ = st.WriteMetrics(context.Background(), []model.Metric{{Name: "host.cpu.percent", Value: 5, Timestamp: base.Add(-time.Minute)}})
	f.reset()
	s.pass(context.Background())
	if n := f.count("POST /datasets"); n != 1 {
		t.Errorf("a grown series caused %d uploads, want 1", n)
	}
	f.mu.Lock()
	if rows := len(f.datasets["forsight-host-cpu"].rows); rows != 41 {
		t.Errorf("cpu dataset has %d rows after the upload, want 41", rows)
	}
	f.mu.Unlock()
}

// TestPass_ForecastReuploadsWhenTheWindowSlides pins the retention-window
// bug: once a forecast export hits its cap the row count stops changing,
// but the series keeps moving — a bucket ages out for every one that
// arrives — and the count-only rule above would then never re-upload
// again. It also re-checks the base case (an unchanged window re-uploads
// nothing) so the fix does not turn every pass into a re-upload.
func TestPass_ForecastReuploadsWhenTheWindowSlides(t *testing.T) {
	f := newFakeMlaas(t)
	sw := &slidingMetricsStore{Store: seedStore(t)}
	for _, name := range []string{"host.cpu.percent", "host.memory.percent", "host.disk.percent"} {
		sw.setMetrics(name, hostMetrics(name, 0, 40))
	}
	s := newSyncer(t, f, sw)
	s.pass(context.Background())
	f.promote("forsight-cpu-forecast", 2, "rmse", 1.0)
	oldOrigin := minute.Add(-2 * time.Minute)

	// Unchanged window: same 40 rows, same newest bucket. No re-upload.
	f.reset()
	s.pass(context.Background())
	if n := f.count("POST /datasets"); n != 0 {
		t.Errorf("an unchanged window uploaded %d datasets, want 0", n)
	}
	if n := f.countExact("GET /models/forsight-cpu-forecast/actual?at=" + url.QueryEscape(oldOrigin.Format(timeLayout))); n != 1 {
		t.Errorf("actual not called at the unchanged origin; requests: %v", f.requests)
	}

	// The window slides forward 5 minutes: the oldest 5 buckets age out and
	// 5 new ones arrive (real time passing along with them, so the newest
	// bucket is still one that has finished filling). The exported row
	// count is unchanged at 40, but the newest bucket is 5 minutes later —
	// exactly the case the count-only rule cannot see.
	sw.setMetrics("host.cpu.percent", hostMetrics("host.cpu.percent", 5, 45))
	s.now = func() time.Time { return base.Add(5 * time.Minute) }
	f.reset()
	s.pass(context.Background())
	if n := f.count("POST /datasets"); n != 1 {
		t.Errorf("a slid window uploaded %d datasets, want exactly 1", n)
	}
	f.mu.Lock()
	rows := len(f.datasets["forsight-host-cpu"].rows)
	f.mu.Unlock()
	if rows != 40 {
		t.Errorf("cpu dataset has %d rows after the slide, want 40 (an unchanged count)", rows)
	}
	newOrigin := oldOrigin.Add(5 * time.Minute)
	if n := f.countExact("GET /models/forsight-cpu-forecast/actual?at=" + url.QueryEscape(newOrigin.Format(timeLayout))); n != 1 {
		t.Errorf("actual not called at the new origin; requests: %v", f.requests)
	}
}

func TestPass_Tolerates409OnCreate(t *testing.T) {
	f := newFakeMlaas(t)
	st := seedStore(t)
	s := newSyncer(t, f, st)
	s.pass(context.Background())
	f.promote("forsight-log-severity", 1, "accuracy", 0.9)
	// Both models exist but the listing does not show them: the next
	// create answers 409 for each, and the pass carries on to recover the
	// real model via GetModel, then the health check and train.
	f.mu.Lock()
	f.hidden["forsight-cpu-forecast"] = true
	f.hidden["forsight-log-severity"] = true
	f.jobs = nil
	f.mu.Unlock()
	f.reset()
	// A line with a level the classifier has never seen (seedStore's logs
	// only ever declare info/error) arrives alongside the usual ones.
	if err := st.WriteLogs(context.Background(), []model.LogEntry{
		{Timestamp: base.Add(-time.Minute), Severity: "debug", Source: "app", Message: "line 999 happened"},
	}); err != nil {
		t.Fatal(err)
	}
	s.pass(context.Background())
	if n := f.countExact("POST /models"); n != 2 {
		t.Errorf("%d creates attempted for the hidden models, want 2", n)
	}
	if n := f.countExact("GET /models/forsight-cpu-forecast"); n != 1 {
		t.Errorf("the cpu model was not re-read via GetModel after the 409 (%d GETs)", n)
	}
	if n := f.countExact("GET /models/forsight-log-severity"); n != 1 {
		t.Errorf("the classifier was not re-read via GetModel after the 409 (%d GETs)", n)
	}
	if n := f.count("GET /models/forsight-cpu-forecast/health"); n == 0 {
		t.Error("the model was not re-read after the 409")
	}
	if n := f.count("POST /models/forsight-cpu-forecast/train"); n != 1 {
		t.Errorf("train called %d times after the 409, want 1", n)
	}
	if st := s.Status(context.Background()); st.LastError != "" {
		t.Errorf("a 409 on create surfaced as an error: %q", st.LastError)
	}

	// The classifier's classes were recovered via GetModel rather than left
	// on a bare wireModel{Name: name}, so the feedback loop below — which
	// runs this same pass, since the champion promoted above is now known —
	// filters the "debug" line's label out of the batch instead of sending
	// mlaas a class it has never seen.
	if n := f.count("POST /models/forsight-log-severity/predict"); n != 1 {
		t.Fatalf("predict called %d times, want one batch", n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, label := range f.labels {
		if label == "debug" {
			t.Errorf("an unknown class reached mlaas as a label: %s = %q", id, label)
		}
	}
}

func TestPass_ForecastsCachedAndActualCalled(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	f.promote("forsight-cpu-forecast", 2, "rmse", 1.5)
	f.reset()
	s.pass(context.Background())

	origin := minute.Add(-2 * time.Minute) // the newest complete bucket
	if n := f.countExact("GET /models/forsight-cpu-forecast/actual?at=" + url.QueryEscape(origin.Format(timeLayout))); n != 1 {
		t.Errorf("actual called %d times at the origin; requests: %v", n, f.requests)
	}
	if n := f.count("GET /models/forsight-memory-forecast/actual"); n != 0 {
		t.Error("actual called for a model without a champion")
	}
	st := s.Status(context.Background())
	if len(st.Forecasts) != 1 {
		t.Fatalf("forecasts = %+v, want one for the cpu model", st.Forecasts)
	}
	fc := st.Forecasts[0]
	if fc.Model != "forsight-cpu-forecast" || fc.Metric != "host.cpu.percent" || !fc.Origin.Equal(origin) {
		t.Errorf("forecast = %+v", fc)
	}
	if len(fc.Points) != 4 {
		t.Fatalf("points = %+v, want 4", fc.Points)
	}
	for i, h := range []time.Duration{5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 60 * time.Minute} {
		if !fc.Points[i].At.Equal(origin.Add(h)) || fc.Points[i].Value != 40+float64(i) {
			t.Errorf("point %d = %+v, want %v / %v", i, fc.Points[i], origin.Add(h), 40+i)
		}
	}
	cpu := modelByName(t, st, "forsight-cpu-forecast")
	if cpu.State != StateReady || cpu.Champion != 2 || cpu.Holdout == nil || *cpu.Holdout != 1.5 || cpu.PredictionsLogged != 4 {
		t.Errorf("cpu model = %+v, want ready, v2, holdout 1.5, 4 predictions logged", cpu)
	}

	// A failing predict drops the cached forecast rather than leaving a
	// stale one on the chart.
	f.mu.Lock()
	f.predictStatus["forsight-cpu-forecast"] = 500
	f.mu.Unlock()
	s.pass(context.Background())
	st = s.Status(context.Background())
	if len(st.Forecasts) != 0 {
		t.Errorf("forecast kept after a predict failure: %+v", st.Forecasts)
	}
	if st.LastError == "" {
		t.Error("the predict failure was not reported")
	}
}

// TestPass_ForecastPredictMismatchOrBadValueDropsTheForecast covers the two
// ways a predict answer can be malformed without mlaas itself erroring:
// the wrong number of predictions for the rows asked about, and a value
// that does not parse as a float. Either drops the cached forecast (a
// stale line on the chart is worse than none) and is reported.
func TestPass_ForecastPredictMismatchOrBadValueDropsTheForecast(t *testing.T) {
	for _, tc := range []struct {
		name string
		vals []string
	}{
		{"three values for four rows", []string{"1", "2", "3"}},
		{"a non-numeric value", []string{"1", "2", "3", "not-a-number"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeMlaas(t)
			s := newSyncer(t, f, seedStore(t))
			s.pass(context.Background())
			f.promote("forsight-cpu-forecast", 2, "rmse", 1.0)
			f.mu.Lock()
			f.predictValues["forsight-cpu-forecast"] = tc.vals
			f.mu.Unlock()
			f.reset()
			s.pass(context.Background())
			status := s.Status(context.Background())
			if len(status.Forecasts) != 0 {
				t.Errorf("forecast kept despite a bad predict answer: %+v", status.Forecasts)
			}
			if status.LastError == "" {
				t.Error("the bad predict answer was not reported")
			}
		})
	}
}

func TestPass_FeedsDeclaredLinesAndRecordsBothAnswers(t *testing.T) {
	f := newFakeMlaas(t)
	st := seedStore(t)
	s := newSyncer(t, f, st)
	s.pass(context.Background())
	f.promote("forsight-log-severity", 1, "accuracy", 0.9)
	f.reset()
	s.pass(context.Background())

	if n := f.count("POST /models/forsight-log-severity/predict"); n != 1 {
		t.Fatalf("predict called %d times, want one batch", n)
	}
	if n := f.count("POST /feedback"); n != 1 {
		t.Fatalf("feedback called %d times, want one batch", n)
	}
	f.mu.Lock()
	if len(f.labels) != feedbackBatch {
		t.Errorf("%d labels posted, want %d", len(f.labels), feedbackBatch)
	}
	f.mu.Unlock()

	status := s.Status(context.Background())
	if len(status.Predictions) != feedbackBatch {
		t.Fatalf("%d predictions recorded, want %d", len(status.Predictions), feedbackBatch)
	}
	// Newest first, and the batch is the newest 20 declared lines: 59 down
	// to 40.
	if status.Predictions[0].Message != "line 059 happened" || status.Predictions[19].Message != "line 040 happened" {
		t.Errorf("predictions ordered %q .. %q", status.Predictions[0].Message, status.Predictions[19].Message)
	}
	for _, p := range status.Predictions {
		// seedLogs: odd lines are "error", even ones "info".
		n, err := strconv.Atoi(p.Message[5:8])
		if err != nil {
			t.Fatalf("unexpected message %q", p.Message)
		}
		want := "info"
		if n%2 == 1 {
			want = "error"
		}
		if p.Declared != want {
			t.Errorf("%q declared %q, want %q", p.Message, p.Declared, want)
		}
		if p.Mlaas != "error" {
			t.Errorf("%q mlaas answer %q, want the fake's fixed answer", p.Message, p.Mlaas)
		}
		wantForseer := "info"
		if strings.Contains(p.Message, "line 05") {
			wantForseer = "" // classify said not ready
		}
		if p.Forseer != wantForseer {
			t.Errorf("%q forseer answer %q, want %q", p.Message, p.Forseer, wantForseer)
		}
		if p.At.IsZero() {
			t.Errorf("%q has no time", p.Message)
		}
	}
	// The labels are the declared levels, keyed by mlaas's prediction ids.
	f.mu.Lock()
	for id, label := range f.labels {
		if label != "info" && label != "error" {
			t.Errorf("label for %s = %q", id, label)
		}
	}
	f.mu.Unlock()

	// Nothing new: no predict, no feedback.
	f.reset()
	s.pass(context.Background())
	if n := f.count("POST /models/forsight-log-severity/predict"); n != 0 {
		t.Error("predict called again with no new lines")
	}

	// Three new declared lines, one with a level the model never saw: all
	// three go through predict and land in the table, two are labelled.
	newLines := seedLogs(60, 62)
	newLines = append(newLines, model.LogEntry{Timestamp: base.Add(-time.Minute), Severity: "debug", Source: "app", Message: "line 062 happened"})
	_ = st.WriteLogs(context.Background(), newLines)
	f.reset()
	s.pass(context.Background())
	if n := f.count("POST /models/forsight-log-severity/predict"); n != 1 {
		t.Fatalf("predict called %d times for new lines", n)
	}
	f.mu.Lock()
	labels := len(f.labels)
	f.mu.Unlock()
	if labels != feedbackBatch+2 {
		t.Errorf("%d labels after the new lines, want %d (the debug line is not a class)", labels, feedbackBatch+2)
	}
	status = s.Status(context.Background())
	if len(status.Predictions) != feedbackBatch+3 || status.Predictions[0].Message != "line 062 happened" || status.Predictions[0].Declared != "debug" {
		t.Errorf("predictions after new lines: %d, newest %+v", len(status.Predictions), status.Predictions[0])
	}
}

// TestPass_FeedbackPredictFailureDoesNotAdvanceTheCursor: a 500 from the
// classifier's predict must not move fedThrough — otherwise the lines it
// failed to send would never be retried. The next pass, once predict works
// again, must send exactly the same batch.
func TestPass_FeedbackPredictFailureDoesNotAdvanceTheCursor(t *testing.T) {
	f := newFakeMlaas(t)
	st := seedStore(t)
	s := newSyncer(t, f, st)
	s.pass(context.Background())
	f.promote("forsight-log-severity", 1, "accuracy", 0.9)

	f.mu.Lock()
	f.predictStatus["forsight-log-severity"] = 500
	f.mu.Unlock()
	f.reset()
	s.pass(context.Background())
	if n := f.count("POST /feedback"); n != 0 {
		t.Error("feedback posted despite a failed predict")
	}
	if status := s.Status(context.Background()); status.LastError == "" {
		t.Error("a failed predict was not reported")
	}
	s.mu.Lock()
	primed := s.fedPrimed
	s.mu.Unlock()
	if primed {
		t.Error("a failed predict advanced (primed) the feedback cursor")
	}

	// Predict works again: the cursor never moved, so the same batch — the
	// newest 20 declared lines — is retried rather than skipped.
	f.mu.Lock()
	delete(f.predictStatus, "forsight-log-severity")
	f.mu.Unlock()
	f.reset()
	s.pass(context.Background())
	if n := f.count("POST /models/forsight-log-severity/predict"); n != 1 {
		t.Fatalf("predict called %d times on the retry, want 1", n)
	}
	status := s.Status(context.Background())
	if len(status.Predictions) != feedbackBatch ||
		status.Predictions[0].Message != "line 059 happened" ||
		status.Predictions[feedbackBatch-1].Message != "line 040 happened" {
		t.Errorf("retried batch = %d predictions, newest %q; want the same 20 lines as the first attempt", len(status.Predictions), status.Predictions[0].Message)
	}
}

func TestPass_PredictionsCappedAtFifty(t *testing.T) {
	f := newFakeMlaas(t)
	st := seedStore(t)
	s := newSyncer(t, f, st)
	s.pass(context.Background())
	f.promote("forsight-log-severity", 1, "accuracy", 0.9)
	for i := 0; i < 3; i++ {
		s.pass(context.Background())
		_ = st.WriteLogs(context.Background(), seedLogs(60+i*20, 80+i*20))
	}
	s.pass(context.Background())
	status := s.Status(context.Background())
	if len(status.Predictions) != maxPredictions {
		t.Errorf("%d predictions kept, want the cap %d", len(status.Predictions), maxPredictions)
	}
}

func TestStatus_RefreshesOnTTLAndKeepsSnapshotWhenUnreachable(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	f.promote("forsight-cpu-forecast", 3, "rmse", 2)
	clock := base
	s.now = func() time.Time { return clock }

	f.reset()
	_ = s.Status(context.Background())
	if n := f.count("GET /healthz"); n != 0 {
		t.Errorf("a status right after the pass hit mlaas %d times", n)
	}
	clock = clock.Add(statusTTL - time.Second)
	st := s.Status(context.Background())
	if n := f.count("GET /healthz"); n != 0 {
		t.Errorf("a status inside the TTL hit mlaas %d times", n)
	}
	if cpu := modelByName(t, st, "forsight-cpu-forecast"); cpu.State != StateTraining {
		t.Errorf("inside the TTL the cpu model is %s, want the cached training state", cpu.State)
	}
	clock = clock.Add(2 * time.Second)
	st = s.Status(context.Background())
	if n := f.count("GET /healthz"); n != 1 {
		t.Errorf("a status past the TTL hit mlaas %d times, want 1", n)
	}
	if n := f.count("GET /models/"); n != 4 {
		t.Errorf("refresh made %d health calls, want one per managed model", n)
	}
	if n := f.count("GET /jobs?limit=200"); n != 1 {
		t.Errorf("refresh listed jobs %d times", n)
	}
	cpu := modelByName(t, st, "forsight-cpu-forecast")
	if cpu.State != StateReady || cpu.Champion != 3 || cpu.Holdout == nil || *cpu.Holdout != 2 || !st.CheckedAt.Equal(clock) {
		t.Errorf("after the refresh the cpu model is %+v (checked %v)", cpu, st.CheckedAt)
	}

	// Server gone: reachable false, the error shown, the models kept.
	f.srv.Close()
	clock = clock.Add(statusTTL + time.Second)
	st = s.Status(context.Background())
	if st.Reachable || st.LastError == "" || !st.CheckedAt.Equal(clock) {
		t.Errorf("unreachable status = reachable %v lastError %q checked %v", st.Reachable, st.LastError, st.CheckedAt)
	}
	if len(st.Models) != 4 || modelByName(t, st, "forsight-cpu-forecast").State != StateReady {
		t.Errorf("models after losing the server: %+v, want the last known values", st.Models)
	}
	// A pass while unreachable records the failure and touches nothing else.
	s.pass(context.Background())
	st = s.Status(context.Background())
	if st.Reachable || !st.LastSync.Equal(clock) || st.LastError == "" {
		t.Errorf("pass while unreachable: %+v", st)
	}
}

// setCheck installs a fake model's last_check, the way a running mlaas
// would after CheckModel — the tests below drive Status through it rather
// than calling buildModelsLocked directly, so they exercise the same
// decode-then-derive path a real refresh does.
func (f *fakeMlaas) setCheck(t *testing.T, name string, c *wireCheck) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.models[name]
	if m == nil {
		t.Fatalf("setCheck: no model %s", name)
	}
	m.check = c
}

// TestStatus_DriftUnmeasuredIsNilNotZero: mlaas's own wire always carries
// drift_max as a plain float64 (0 when there is nothing to report yet,
// client.go's wireCheck comment) — the flattering zero MODELS.md says a
// card must never show. A last_check with no "drift" key at all (too few
// recent predictions, loop.go's MinWindow gate) must leave ModelStatus's
// DriftMax nil, never a *float64 pointing at 0, so the dashboard can tell
// "measured, and it's zero" from "not measured".
func TestStatus_DriftUnmeasuredIsNilNotZero(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	f.promote("forsight-cpu-forecast", 3, "rmse", 1.1)
	f.setCheck(t, "forsight-cpu-forecast", &wireCheck{CheckedAt: base, NewLabels: 4, DriftMax: 0})

	s.now = func() time.Time { return base.Add(statusTTL + time.Second) }
	st := s.Status(context.Background())
	cpu := modelByName(t, st, "forsight-cpu-forecast")
	if cpu.DriftMax != nil {
		t.Errorf("driftMax = %v, want nil (unmeasured, not a flattering 0)", *cpu.DriftMax)
	}
	if cpu.DriftFeature != "" {
		t.Errorf("driftFeature = %q, want empty when drift was not measured", cpu.DriftFeature)
	}
	if cpu.DriftThreshold != 0.2 {
		t.Errorf("driftThreshold = %v, want 0.2 (mlaas's default) even while unmeasured", cpu.DriftThreshold)
	}
}

// TestStatus_DriftMeasuredReportsFeatureAndThreshold: once mlaas has
// enough recent predictions to compare against the training profile, the
// Badge needs three things out of one check: the measured max, which
// feature it came from, and the threshold mlaas itself retrains on
// (retrain.drift_threshold) — not a value this agent invents.
func TestStatus_DriftMeasuredReportsFeatureAndThreshold(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	f.promote("forsight-cpu-forecast", 3, "rmse", 1.1)
	f.setCheck(t, "forsight-cpu-forecast", &wireCheck{
		CheckedAt:    base,
		NewLabels:    4,
		Drift:        map[string]float64{"at": 0.24, "value": 0.05},
		DriftFeature: "at",
		DriftMax:     0.24,
	})

	s.now = func() time.Time { return base.Add(statusTTL + time.Second) }
	st := s.Status(context.Background())
	cpu := modelByName(t, st, "forsight-cpu-forecast")
	if cpu.DriftMax == nil || *cpu.DriftMax != 0.24 {
		t.Errorf("driftMax = %v, want 0.24", cpu.DriftMax)
	}
	if cpu.DriftFeature != "at" {
		t.Errorf("driftFeature = %q, want %q", cpu.DriftFeature, "at")
	}
	if cpu.DriftThreshold != 0.2 {
		t.Errorf("driftThreshold = %v, want 0.2", cpu.DriftThreshold)
	}
}

// TestStatus_RefreshMalformedBodyMarksUnreachableButKeepsModels: a 200 that
// does not decode is not the same bug as a network failure, but it must be
// handled the same way — Reachable=false, the error explains why (it names
// decoding, not just "unhealthy"), and whatever the last good refresh
// learned stays on the page.
func TestStatus_RefreshMalformedBodyMarksUnreachableButKeepsModels(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	f.promote("forsight-cpu-forecast", 2, "rmse", 1.5)
	s.now = func() time.Time { return base.Add(statusTTL + time.Second) }
	before := s.Status(context.Background())
	if !before.Reachable {
		t.Fatalf("setup refresh did not succeed: %+v", before)
	}
	cpuBefore := modelByName(t, before, "forsight-cpu-forecast")

	f.mu.Lock()
	f.healthzOverride = "{not valid json"
	f.mu.Unlock()
	s.now = func() time.Time { return base.Add(2*statusTTL + 2*time.Second) }
	after := s.Status(context.Background())
	if after.Reachable {
		t.Error("a malformed healthz body was treated as reachable")
	}
	if !strings.Contains(strings.ToLower(after.LastError), "decod") {
		t.Errorf("LastError = %q, want it to mention decoding", after.LastError)
	}
	cpuAfter := modelByName(t, after, "forsight-cpu-forecast")
	if cpuAfter.Champion != cpuBefore.Champion || cpuAfter.State != cpuBefore.State || cpuAfter.Holdout == nil || *cpuAfter.Holdout != *cpuBefore.Holdout {
		t.Errorf("models changed after a failed refresh: before %+v after %+v", cpuBefore, cpuAfter)
	}
}

// TestRefresh_IgnoresCallerCancellation: the ctx a dashboard request hands
// Status is often cancelled the moment that request ends — a browser tab
// that navigates away, ten polling tabs racing one shared refresh under
// refreshMu. That must not cancel the refresh itself: every upstream call
// has its own timeout, so there is no other reason for it to hang.
func TestRefresh_IgnoresCallerCancellation(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	f.promote("forsight-cpu-forecast", 2, "rmse", 1.5)
	deadline := base.Add(statusTTL + time.Second)
	s.now = func() time.Time { return deadline }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st := s.Status(ctx)
	if !st.Reachable || st.LastError != "" {
		t.Errorf("a cancelled caller ctx broke the refresh: reachable=%v lastError=%q", st.Reachable, st.LastError)
	}
	if !st.CheckedAt.Equal(deadline) {
		t.Errorf("CheckedAt = %v, want %v — the refresh must still have completed", st.CheckedAt, deadline)
	}
	cpu := modelByName(t, st, "forsight-cpu-forecast")
	if cpu.State != StateReady || cpu.Champion != 2 {
		t.Errorf("refresh did not complete with a cancelled caller ctx: cpu = %+v", cpu)
	}
}

// TestStatus_JobsSurviveOtherTenantsSharingTheServer: mlaas can be shared,
// so the job list a refresh reads is not only this agent's. A page just
// wide enough for the managed models' own jobs lets a noisy neighbour push
// them off the end; jobsLimit exists so that does not happen in practice.
func TestStatus_JobsSurviveOtherTenantsSharingTheServer(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())

	f.mu.Lock()
	for i := 0; i < 30; i++ {
		f.nextID++
		f.jobs = append(f.jobs, wireJob{ID: int64(f.nextID), Model: "other-tenant-model", Kind: "train", Trigger: "manual", Status: "done", CreatedAt: base})
	}
	f.mu.Unlock()
	s.now = func() time.Time { return base.Add(statusTTL + time.Second) }
	f.reset()
	st := s.Status(context.Background())
	if n := f.countExact("GET /jobs?limit=200"); n != 1 {
		t.Errorf("refresh asked for %d /jobs?limit=200 calls, want 1", n)
	}
	if len(st.Jobs) != 4 {
		t.Errorf("%d jobs returned, want the 4 managed ones out of 34 total", len(st.Jobs))
	}
	for _, j := range st.Jobs {
		if j.Model == "other-tenant-model" {
			t.Errorf("an unmanaged tenant's job leaked into the status: %+v", j)
		}
	}
}

// TestStatus_JobsAreCappedAtTheNewest: the refresh asks for 200 jobs so a
// shared server cannot crowd ours out, but the page is a recent-activity
// view — the snapshot keeps the newest jobsShown of the managed models'
// jobs, in mlaas's own newest-first order.
func TestStatus_JobsAreCappedAtTheNewest(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())

	f.mu.Lock()
	for i := 0; i < 30; i++ {
		f.nextID++
		f.jobs = append(f.jobs, wireJob{ID: int64(f.nextID), Model: "forsight-cpu-forecast", Kind: "retrain", Trigger: "schedule", Status: "done", CreatedAt: base})
	}
	f.mu.Unlock()
	s.now = func() time.Time { return base.Add(statusTTL + time.Second) }
	f.reset()
	st := s.Status(context.Background())
	if len(st.Jobs) != jobsShown {
		t.Fatalf("%d jobs returned, want the newest %d", len(st.Jobs), jobsShown)
	}
	for i := 1; i < len(st.Jobs); i++ {
		if st.Jobs[i].ID > st.Jobs[i-1].ID {
			t.Fatalf("jobs are not newest first: %d after %d", st.Jobs[i].ID, st.Jobs[i-1].ID)
		}
	}
}

// TestStatus_ConcurrentCallsShareOneRefresh: ten dashboard tabs polling at
// once past the TTL should cost mlaas one round of calls, not ten —
// refreshMu serialises the real work, and every caller that queued behind
// it must see the freshly refreshed snapshot as "fresh" rather than repeat
// it. Run under -race: it is also a check that s.mu and refreshMu are
// enough on their own.
func TestStatus_ConcurrentCallsShareOneRefresh(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	// A clock fixed past the TTL: every goroutine agrees the snapshot is
	// stale before the refresh, and agrees it is fresh again immediately
	// after — there is no real time passing to race against.
	s.now = func() time.Time { return base.Add(statusTTL + time.Second) }
	f.reset()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Status(context.Background())
		}()
	}
	wg.Wait()

	if n := f.count("GET /healthz"); n != 1 {
		t.Errorf("10 concurrent Status calls past the TTL made %d healthz calls, want exactly 1", n)
	}
}

func TestStatus_BeforeFirstContactListsEveryModel(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	s, err := New(Config{URL: srv.URL, Prefix: "agent2"}, store.NewMemoryStore(time.Hour), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Status(context.Background())
	if st.Reachable || st.LastError == "" || !st.Configured || st.URL != srv.URL {
		t.Errorf("status = %+v", st)
	}
	if len(st.Models) != 4 {
		t.Fatalf("models = %+v", st.Models)
	}
	for i, m := range st.Models {
		if m.Name != Names("agent2")[i] || m.State != StateWaitingForData || m.Dataset == "" || m.Job == "" {
			t.Errorf("model %d = %+v", i, m)
		}
	}
	if st.Forecasts == nil || st.Predictions == nil || st.Jobs == nil {
		t.Error("nil slices in the status")
	}
}

func TestDeriveState(t *testing.T) {
	cases := []struct {
		exported, dsKnown, known, champion, active bool
		want                                       string
	}{
		{false, false, false, false, false, StateWaitingForData},
		{true, false, false, false, false, StateMissing},
		{false, true, false, false, false, StateMissing},
		{true, true, true, false, true, StateTraining},
		{true, true, true, false, false, StateNoChampion},
		{true, true, true, true, true, StateReady},
		{false, false, true, true, false, StateReady},
	}
	for _, c := range cases {
		if got := deriveState(c.exported, c.dsKnown, c.known, c.champion, c.active); got != c.want {
			t.Errorf("deriveState(%v) = %s, want %s", c, got, c.want)
		}
	}
}

func TestActions_RefuseUnmanagedNamesAndOversizedRows(t *testing.T) {
	f := newFakeMlaas(t)
	s := newSyncer(t, f, seedStore(t))
	s.pass(context.Background())
	ctx := context.Background()

	for _, name := range []string{"other-cpu-forecast", "forsight-cpu-forecast/../x", "", "forsight-cpu-forecas"} {
		if _, err := s.Train(ctx, name); !errors.Is(err, ErrUnknownModel) {
			t.Errorf("Train(%q) = %v, want ErrUnknownModel", name, err)
		}
		if _, err := s.Tune(ctx, name); !errors.Is(err, ErrUnknownModel) {
			t.Errorf("Tune(%q) = %v, want ErrUnknownModel", name, err)
		}
		if _, err := s.Predict(ctx, name, []map[string]any{{"message": "x"}}); !errors.Is(err, ErrUnknownModel) {
			t.Errorf("Predict(%q) = %v, want ErrUnknownModel", name, err)
		}
	}
	if n := f.count("POST /models/other-cpu-forecast"); n != 0 {
		t.Error("an unmanaged name reached mlaas")
	}

	var ue *UpstreamError
	tooMany := make([]map[string]any, maxPredictRows+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{"message": "x"}
	}
	if _, err := s.Predict(ctx, "forsight-log-severity", tooMany); !errors.As(err, &ue) || ue.Status != 400 {
		t.Errorf("11 rows gave %v, want a 400", err)
	}
	if _, err := s.Predict(ctx, "forsight-log-severity", []map[string]any{{"message": strings.Repeat("x", maxMessageBytes+1)}}); !errors.As(err, &ue) || ue.Status != 400 {
		t.Errorf("a 4097-byte cell gave %v, want a 400", err)
	}
	if _, err := s.Predict(ctx, "forsight-log-severity", nil); !errors.As(err, &ue) || ue.Status != 400 {
		t.Errorf("no rows gave %v, want a 400", err)
	}
	if n := f.count("POST /models/forsight-log-severity/predict"); n != 0 {
		t.Error("a refused predict reached mlaas")
	}

	// No champion yet: mlaas's 409 passes through as-is.
	if _, err := s.Predict(ctx, "forsight-log-severity", []map[string]any{{"message": "x"}}); !errors.As(err, &ue) || ue.Status != 409 {
		t.Errorf("predict without a champion gave %v, want mlaas's 409", err)
	}

	f.promote("forsight-log-severity", 4, "accuracy", 0.8)
	res, err := s.Predict(ctx, "forsight-log-severity", []map[string]any{{"message": "disk full"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "forsight-log-severity" || res.Version != 4 || len(res.Predictions) != 1 || res.Predictions[0].Value != "error" {
		t.Errorf("predict result = %+v", res)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), "prediction_id") {
		t.Errorf("prediction id leaked into the proxied result: %s", raw)
	}

	ref, err := s.Tune(ctx, "forsight-log-severity")
	if err != nil || ref.JobID == 0 || ref.AlreadyQueued {
		t.Errorf("Tune = %+v, %v", ref, err)
	}
	again, err := s.Tune(ctx, "forsight-log-severity")
	if err != nil || again.JobID != ref.JobID || !again.AlreadyQueued {
		t.Errorf("second Tune = %+v, %v, want the same job flagged already queued", again, err)
	}
	// The queued job shows on the very next status, TTL or not.
	f.reset()
	st := s.Status(ctx)
	if f.count("GET /healthz") != 1 {
		t.Error("an action did not invalidate the snapshot")
	}
	found := false
	for _, j := range st.Jobs {
		if j.ID == ref.JobID && j.Kind == "tune" {
			found = true
		}
	}
	if !found {
		t.Errorf("tune job %d not in %+v", ref.JobID, st.Jobs)
	}
	if !modelByName(t, st, "forsight-log-severity").ActiveJob {
		t.Error("model not flagged with an active job")
	}
}

func TestRun_StopsOnCancel(t *testing.T) {
	f := newFakeMlaas(t)
	st := seedStore(t)
	s, err := New(Config{URL: f.srv.URL, APIKey: f.key, SyncInterval: 20 * time.Millisecond}, st, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for f.count("GET /healthz") < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if f.count("GET /healthz") < 2 {
		t.Fatal("Run did not pass at least twice")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	// The cancel may have cut a pass short (its calls then fail on the
	// dead context, which is the point); what must hold is that the passes
	// before it completed and left a synced snapshot behind.
	status := s.Status(context.Background())
	if status.LastSync.IsZero() || len(status.Models) != 4 {
		t.Errorf("status after the run = %+v, want a completed sync", status)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.models) != 4 {
		t.Errorf("%d models created by the run, want 4", len(f.models))
	}
}
