package mlaas

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorded is one request as the fake server saw it.
type recorded struct {
	mu                                    sync.Mutex
	method, path, query, key, contentType string
	body                                  []byte
	form                                  map[string]string // multipart fields
	fileName                              string
}

// record wraps a handler that answers with a fixed status and body and
// keeps what it was sent, so a test pins the wire format in both
// directions. The handler runs on the server's goroutine, so the record is
// written and read under its own lock.
func record(t *testing.T, status int, body string) (*Client, *recorded) {
	t.Helper()
	got := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.mu.Lock()
		defer got.mu.Unlock()
		got.method, got.path, got.query = r.Method, r.URL.Path, r.URL.RawQuery
		got.key = r.Header.Get("X-API-Key")
		got.contentType = r.Header.Get("Content-Type")
		if strings.HasPrefix(got.contentType, "multipart/form-data") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			got.form = map[string]string{}
			for k := range r.MultipartForm.Value {
				got.form[k] = r.FormValue(k)
			}
			f, hdr, err := r.FormFile("file")
			if err == nil {
				got.fileName = hdr.Filename
				got.body, _ = io.ReadAll(f)
				_ = f.Close()
			}
		} else {
			got.body, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/", "secret"), got
}

func TestClient_HealthzSendsNoKey(t *testing.T) {
	c, got := record(t, 200, `{"ok":true,"version":"v1.8.0"}`)
	version, err := c.Healthz(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/healthz" || got.key != "" {
		t.Errorf("healthz went to %s with key %q; want /healthz and no key", got.path, got.key)
	}
	if version != "v1.8.0" {
		t.Errorf("Healthz version = %q, want v1.8.0", version)
	}
	c, _ = record(t, 503, `{"ok":false,"failed":"store"}`)
	_, err = c.Healthz(context.Background())
	var ue *UpstreamError
	if !errors.As(err, &ue) || ue.Status != 503 {
		t.Errorf("unhealthy probe gave %v, want an UpstreamError 503", err)
	}
}

func TestClient_SendsKeyAndDecodesErrors(t *testing.T) {
	c, got := record(t, 409, `{"error":"model already exists"}`)
	_, err := c.CreateModel(context.Background(), managed[0].spec("forsight"))
	if got.key != "secret" {
		t.Errorf("X-API-Key = %q, want secret", got.key)
	}
	if got.method != http.MethodPost || got.path != "/models" || got.contentType != "application/json" {
		t.Errorf("create went as %s %s (%s)", got.method, got.path, got.contentType)
	}
	var ue *UpstreamError
	if !errors.As(err, &ue) || ue.Status != 409 || ue.Message != "model already exists" {
		t.Fatalf("err = %v, want UpstreamError{409, model already exists}", err)
	}
	if upstreamStatus(err) != 409 {
		t.Error("upstreamStatus did not see the 409")
	}
	// A proxy's HTML page is not mlaas's message: fall back to the status
	// text rather than putting markup on the dashboard.
	c, _ = record(t, 502, `<html>nginx</html>`)
	_, err = c.ListModels(context.Background())
	if !errors.As(err, &ue) || ue.Status != 502 || ue.Message != "Bad Gateway" {
		t.Errorf("err = %v, want UpstreamError{502, Bad Gateway}", err)
	}
	if upstreamStatus(errors.New("dial tcp: refused")) != 0 {
		t.Error("a transport error reported an upstream status")
	}
}

func TestClient_TruncatesLongErrorMessages(t *testing.T) {
	long := strings.Repeat("x", 8<<10)
	body, err := json.Marshal(map[string]string{"error": long})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := record(t, 400, string(body))
	_, err = c.ListModels(context.Background())
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v, want an UpstreamError", err)
	}
	if len(ue.Message) > 520 {
		t.Errorf("message length = %d, want <= 520 (512 plus a 3-byte ellipsis)", len(ue.Message))
	}
	if !strings.HasSuffix(ue.Message, "…") {
		t.Errorf("message = %q, want it to end with an ellipsis marking the cut", ue.Message)
	}
}

func TestClient_DoesNotFollowRedirectsWithTheKey(t *testing.T) {
	hit := false
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(second.Close)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, second.URL+"/models", http.StatusFound)
	}))
	t.Cleanup(first.Close)

	c := NewClient(first.URL, "secret")
	_, err := c.ListModels(context.Background())
	var ue *UpstreamError
	if !errors.As(err, &ue) || ue.Status != http.StatusFound {
		t.Fatalf("err = %v, want an UpstreamError 302", err)
	}
	if hit {
		t.Error("the redirect target received a request — the key must never follow a redirect")
	}
}

func TestClient_UploadDatasetIsMultipart(t *testing.T) {
	c, got := record(t, 201, `{"name":"forsight-host-cpu","path":"data/datasets/forsight-host-cpu.csv","columns":["at","value"],"rows":2,"created_at":"2026-09-15T10:00:00Z"}`)
	csvBytes := []byte("at,value\n2026-09-15T10:00:00Z,1\n2026-09-15T10:01:00Z,2\n")
	d, err := c.UploadDataset(context.Background(), "forsight-host-cpu", csvBytes)
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/datasets" {
		t.Errorf("upload went as %s %s", got.method, got.path)
	}
	if got.form["name"] != "forsight-host-cpu" || got.fileName != "forsight-host-cpu.csv" || string(got.body) != string(csvBytes) {
		t.Errorf("multipart form = %v file %q body %q", got.form, got.fileName, got.body)
	}
	if d.Rows != 2 || !reflect.DeepEqual(d.Columns, []string{"at", "value"}) {
		t.Errorf("decoded dataset = %+v", d)
	}
}

func TestClient_ListModelsDecodesChampionAndHoldout(t *testing.T) {
	c, _ := record(t, 200, `[{"name":"forsight-cpu-forecast","spec":{"name":"forsight-cpu-forecast","dataset":"forsight-host-cpu","plugin":"holtwinters","task":"forecast","target":"value","timestamp":"at","features":["at"],"params":{},"metric":"rmse","retrain":{"min_new_labels":50,"cooldown_minutes":10,"schedule_minutes":30,"tune_every_days":30,"metric_drop":0.05,"drift_threshold":0.2,"window":200,"min_window":20},"promotion":{"min_improvement":0,"min_confidence":0.8,"min_relative_gain":0.002}},"champion_version":7,"last_retrain_at":null,"last_check":null,"created_at":"2026-09-15T10:00:00Z","champion":{"id":7,"model":"forsight-cpu-forecast","number":3,"artifact_dir":"x","status":"champion","metrics":{"holdout":{"rmse":1.25,"mae":0.9},"train_rows":100,"holdout_rows":20,"labeled_rows":0},"reason":"","trained_at":"2026-09-15T10:05:00Z"}},{"name":"forsight-log-severity","classes":["error","info"],"spec":{"name":"forsight-log-severity","dataset":"forsight-logs","plugin":"bayes","task":"classification","target":"severity","features":["message"],"params":{},"metric":"accuracy","retrain":{}},"champion_version":null,"last_retrain_at":null,"last_check":null,"created_at":"2026-09-15T10:00:00Z"}]`)
	ms, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("got %d models", len(ms))
	}
	cpu := ms[0]
	if cpu.Champion == nil || cpu.Champion.Number != 3 || cpu.ChampionVersion == nil || *cpu.ChampionVersion != 7 {
		t.Errorf("champion not decoded: %+v", cpu)
	}
	if h := cpu.Champion.holdout("rmse"); h == nil || *h != 1.25 {
		t.Errorf("holdout rmse = %v, want 1.25", h)
	}
	if h := cpu.Champion.holdout("accuracy"); h != nil {
		t.Errorf("holdout of an absent metric = %v, want nil", *h)
	}
	if cpu.Spec.Retrain.ScheduleMinutes != 30 || cpu.Spec.Task != "forecast" {
		t.Errorf("spec not decoded: %+v", cpu.Spec)
	}
	if cpu.Spec.Retrain.DriftThreshold != 0.2 {
		t.Errorf("drift threshold = %v, want 0.2 (the value mlaas retrains on)", cpu.Spec.Retrain.DriftThreshold)
	}
	sev := ms[1]
	if sev.Champion != nil || sev.ChampionVersion != nil || !reflect.DeepEqual(sev.Classes, []string{"error", "info"}) {
		t.Errorf("classifier decoded as %+v", sev)
	}
	if (*wireVersion)(nil).holdout("rmse") != nil {
		t.Error("nil version reported a holdout")
	}
}

func TestClient_GetModelDecodesModelOnly(t *testing.T) {
	c, got := record(t, 200, `{"model":{"name":"forsight-log-severity","classes":["error","info"],"spec":{"name":"forsight-log-severity","dataset":"forsight-logs","plugin":"bayes","task":"classification","target":"severity","features":["message"],"params":{},"metric":"accuracy","retrain":{}},"champion_version":null,"last_retrain_at":null,"last_check":null,"created_at":"2026-09-15T10:00:00Z"},"versions":[{"id":1,"model":"forsight-log-severity","number":1,"status":"retired"}]}`)
	m, err := c.GetModel(context.Background(), "forsight-log-severity")
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodGet || got.path != "/models/forsight-log-severity" {
		t.Errorf("GetModel went as %s %s, want GET /models/forsight-log-severity", got.method, got.path)
	}
	if m.Name != "forsight-log-severity" || !reflect.DeepEqual(m.Classes, []string{"error", "info"}) {
		t.Errorf("decoded model = %+v, want classes carried through", m)
	}
}

func TestClient_GetHealthDecodesLastCheck(t *testing.T) {
	c, got := record(t, 200, `{"model":"forsight-log-severity","metric":"accuracy","last_check":{"checked_at":"2026-09-15T10:10:00Z","champion":2,"new_labels":12,"window_n":40,"window_metric":0.85,"holdout_metric":0.9,"drift_n":40,"drift":{"message":0.01},"drift_feature":"message","drift_max":0.01,"note":"ok"},"last_retrain_at":"2026-09-15T09:00:00Z","champion":{"id":5,"model":"forsight-log-severity","number":2,"artifact_dir":"x","status":"champion","metrics":{"holdout":{"accuracy":0.9}},"reason":"","trained_at":"2026-09-15T09:00:00Z"},"active_job":true,"predictions_logged":123}`)
	h, err := c.GetHealth(context.Background(), "forsight-log-severity")
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/models/forsight-log-severity/health" {
		t.Errorf("path = %s", got.path)
	}
	chk := h.check()
	if chk.Champion != 2 || chk.NewLabels != 12 || chk.WindowN != 40 || chk.WindowMetric == nil || *chk.WindowMetric != 0.85 || chk.HoldoutMetric == nil || *chk.HoldoutMetric != 0.9 || chk.Note != "ok" {
		t.Errorf("last_check decoded as %+v", chk)
	}
	// Drift is measured here (drift is non-nil), so DriftMax/DriftFeature
	// are trustworthy — the "0 means unmeasured" trap this item exists to
	// avoid is exercised below, on a check with no drift key at all.
	if chk.Drift == nil || chk.Drift["message"] != 0.01 || chk.DriftFeature != "message" || chk.DriftMax != 0.01 {
		t.Errorf("drift decoded as %+v", chk)
	}
	if !h.ActiveJob || h.PredictionsLogged != 123 || h.Champion == nil || h.Champion.Number != 2 || h.LastRetrainAt == nil {
		t.Errorf("health decoded as %+v", h)
	}
	if empty := (&wireHealth{LastCheck: json.RawMessage("null")}).check(); empty.Champion != 0 || empty.NewLabels != 0 || empty.Drift != nil || empty.DriftMax != 0 || empty.Note != "" {
		t.Errorf("a null last_check did not decode to the zero check, got %+v", empty)
	}
}

// TestClient_GetHealthDriftAbsentIsNilNotZero: a check with no "drift" key
// at all (too few recent predictions for mlaas to compare, loop.go's
// MinWindow gate) must decode to a nil Drift even though drift_max is
// still the wire's ever-present 0 — the exact "flattering zero" MODELS.md
// says a card must never show, so callers must gate on Drift, not DriftMax.
func TestClient_GetHealthDriftAbsentIsNilNotZero(t *testing.T) {
	c, _ := record(t, 200, `{"model":"forsight-log-severity","metric":"accuracy","last_check":{"checked_at":"2026-09-15T10:10:00Z","champion":2,"new_labels":3,"window_n":0,"drift_n":2,"drift_max":0},"last_retrain_at":null,"active_job":false,"predictions_logged":5}`)
	h, err := c.GetHealth(context.Background(), "forsight-log-severity")
	if err != nil {
		t.Fatal(err)
	}
	chk := h.check()
	if chk.Drift != nil || chk.DriftFeature != "" || chk.DriftMax != 0 {
		t.Errorf("no-drift check decoded as %+v, want a nil Drift", chk)
	}
}

func TestClient_TrainAndTuneMapJobRef(t *testing.T) {
	c, got := record(t, 202, `{"job_id":7,"status":"queued","already_queued":true}`)
	ref, err := c.Train(context.Background(), "forsight-cpu-forecast")
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/models/forsight-cpu-forecast/train" {
		t.Errorf("train went as %s %s", got.method, got.path)
	}
	if ref != (JobRef{JobID: 7, AlreadyQueued: true}) {
		t.Errorf("ref = %+v", ref)
	}
	c, got = record(t, 202, `{"job_id":8,"status":"queued","already_queued":false}`)
	if _, err := c.Tune(context.Background(), "forsight-cpu-forecast"); err != nil {
		t.Fatal(err)
	}
	if got.path != "/models/forsight-cpu-forecast/tune" {
		t.Errorf("tune went to %s", got.path)
	}
}

func TestClient_PredictAndFeedbackBodies(t *testing.T) {
	c, got := record(t, 200, `{"model":"forsight-log-severity","version":2,"predictions":[{"prediction_id":"abc","value":"error","unusual":{"message":"never seen"}},{"prediction_id":"def","value":"info"}]}`)
	resp, err := c.Predict(context.Background(), "forsight-log-severity", []map[string]any{{"message": "disk full"}, {"message": "started"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/models/forsight-log-severity/predict" {
		t.Errorf("predict went to %s", got.path)
	}
	var body map[string][]map[string]any
	if err := json.Unmarshal(got.body, &body); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body, map[string][]map[string]any{"rows": {{"message": "disk full"}, {"message": "started"}}}) {
		t.Errorf("predict body = %s", got.body)
	}
	if resp.Version != 2 || len(resp.Predictions) != 2 || resp.Predictions[0].PredictionID != "abc" || resp.Predictions[0].Value != "error" || resp.Predictions[0].Unusual["message"] != "never seen" || resp.Predictions[1].Unusual != nil {
		t.Errorf("predict response decoded as %+v", resp)
	}

	c, got = record(t, 200, `{"accepted":1,"unknown_prediction_ids":["zzz"]}`)
	fb, err := c.Feedback(context.Background(), []wireLabel{{PredictionID: "abc", Label: "error"}, {PredictionID: "zzz", Label: "info"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/feedback" {
		t.Errorf("feedback went as %s %s", got.method, got.path)
	}
	if want := `{"labels":[{"prediction_id":"abc","label":"error"},{"prediction_id":"zzz","label":"info"}]}`; string(got.body) != want {
		t.Errorf("feedback body = %s, want %s", got.body, want)
	}
	if fb.Accepted != 1 || !reflect.DeepEqual(fb.UnknownPredictionIDs, []string{"zzz"}) {
		t.Errorf("feedback response decoded as %+v", fb)
	}
}

func TestClient_ActualAndJobs(t *testing.T) {
	c, got := record(t, 200, `{"model":"forsight-cpu-forecast","at":"2026-09-15T10:29:00Z","found":true,"value":"12.5","live":false,"refreshed":false,"labeled":3}`)
	at := time.Date(2026, 9, 15, 12, 29, 0, 0, time.FixedZone("plus2", 2*3600))
	a, err := c.Actual(context.Background(), "forsight-cpu-forecast", at)
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/models/forsight-cpu-forecast/actual" || got.query != "at=2026-09-15T10%3A29%3A00Z" {
		t.Errorf("actual went to %s?%s, want the UTC instant", got.path, got.query)
	}
	if !a.Found || a.Value != "12.5" || a.Labeled != 3 {
		t.Errorf("actual decoded as %+v", a)
	}

	c, got = record(t, 200, `[{"id":9,"model":"forsight-cpu-forecast","kind":"retrain","trigger":"schedule","status":"done","log":"...","created_at":"2026-09-15T10:00:00Z","started_at":"2026-09-15T10:00:01Z","finished_at":"2026-09-15T10:00:30Z"},{"id":8,"model":"x","kind":"train","trigger":"manual","status":"queued","log":"","created_at":"2026-09-15T09:00:00Z","started_at":null,"finished_at":null}]`)
	jobs, err := c.ListJobs(context.Background(), 20)
	if err != nil {
		t.Fatal(err)
	}
	if got.path != "/jobs" || got.query != "limit=20" {
		t.Errorf("jobs went to %s?%s", got.path, got.query)
	}
	if len(jobs) != 2 || jobs[0].ID != 9 || jobs[0].FinishedAt == nil || jobs[1].StartedAt != nil {
		t.Errorf("jobs decoded as %+v", jobs)
	}
}

func TestClient_TimeoutIsBounded(t *testing.T) {
	c, _ := record(t, 200, `{"ok":true}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Healthz(ctx); err == nil {
		t.Error("a cancelled context still completed the call")
	}
}
