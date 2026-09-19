package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Fors-Corp/forsight/forsight/internal/mlaas"
	"github.com/Fors-Corp/forsight/forsight/internal/store"
)

// fakeMlaasService is a hand-rolled MlaasService: each field is nil unless
// a test needs it, in which case a zero JobRef/PredictResult/Status with a
// nil error is a reasonable default so tests that only care about one call
// don't have to stub every method.
type fakeMlaasService struct {
	statusFn  func(ctx context.Context) mlaas.Status
	trainFn   func(ctx context.Context, name string) (mlaas.JobRef, error)
	tuneFn    func(ctx context.Context, name string) (mlaas.JobRef, error)
	predictFn func(ctx context.Context, name string, rows []map[string]any) (mlaas.PredictResult, error)
}

func (f *fakeMlaasService) Status(ctx context.Context) mlaas.Status {
	if f.statusFn != nil {
		return f.statusFn(ctx)
	}
	return mlaas.Status{}
}

func (f *fakeMlaasService) Train(ctx context.Context, name string) (mlaas.JobRef, error) {
	if f.trainFn != nil {
		return f.trainFn(ctx, name)
	}
	return mlaas.JobRef{}, nil
}

func (f *fakeMlaasService) Tune(ctx context.Context, name string) (mlaas.JobRef, error) {
	if f.tuneFn != nil {
		return f.tuneFn(ctx, name)
	}
	return mlaas.JobRef{}, nil
}

func (f *fakeMlaasService) Predict(ctx context.Context, name string, rows []map[string]any) (mlaas.PredictResult, error) {
	if f.predictFn != nil {
		return f.predictFn(ctx, name, rows)
	}
	return mlaas.PredictResult{}, nil
}

func TestHandleMlaasStatus_NotConfiguredWithoutService(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/mlaas/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	// The exact literal, not just a decode-and-check: the dashboard's empty
	// state depends on every slice being [] rather than a missing/null key.
	want := `{"configured":false,"reachable":false,"models":[],"forecasts":[],"predictions":[],"jobs":[]}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestHandleMlaasStatus_ReturnsServiceSnapshot(t *testing.T) {
	svc := &fakeMlaasService{
		statusFn: func(context.Context) mlaas.Status {
			return mlaas.Status{
				Configured: true, Reachable: true, URL: "http://127.0.0.1:8090",
				Models:      []mlaas.ModelStatus{{Name: "forsight-cpu-forecast", State: mlaas.StateReady}},
				Forecasts:   []mlaas.Forecast{},
				Predictions: []mlaas.Prediction{},
				Jobs:        []mlaas.Job{},
			}
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/mlaas/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var got mlaas.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Configured || !got.Reachable || len(got.Models) != 1 || got.Models[0].Name != "forsight-cpu-forecast" {
		t.Fatalf("got %+v, want the service's snapshot passed straight through", got)
	}
}

func TestHandleMlaasTrain_NotConfiguredIs503(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-cpu-forecast/train", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleMlaasTrain_QueuesAJob(t *testing.T) {
	var gotName string
	svc := &fakeMlaasService{
		trainFn: func(_ context.Context, name string) (mlaas.JobRef, error) {
			gotName = name
			return mlaas.JobRef{JobID: 12}, nil
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-cpu-forecast/train", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	if gotName != "forsight-cpu-forecast" {
		t.Errorf("service saw name %q, want the {name} path segment", gotName)
	}
	var ref mlaas.JobRef
	if err := json.Unmarshal(rec.Body.Bytes(), &ref); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ref.JobID != 12 {
		t.Errorf("jobId = %d, want 12", ref.JobID)
	}
}

func TestHandleMlaasTune_QueuesAJob(t *testing.T) {
	var gotName string
	svc := &fakeMlaasService{
		tuneFn: func(_ context.Context, name string) (mlaas.JobRef, error) {
			gotName = name
			return mlaas.JobRef{JobID: 7, AlreadyQueued: true}, nil
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-log-severity/tune", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %s)", rec.Code, rec.Body.String())
	}
	if gotName != "forsight-log-severity" {
		t.Errorf("service saw name %q", gotName)
	}
}

func TestHandleMlaasTrain_UnknownModelIs404(t *testing.T) {
	svc := &fakeMlaasService{
		trainFn: func(_ context.Context, _ string) (mlaas.JobRef, error) {
			return mlaas.JobRef{}, mlaas.ErrUnknownModel
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/someone-elses-model/train", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleMlaasTrain_UpstreamErrorPassesThroughStatusAndMessage(t *testing.T) {
	svc := &fakeMlaasService{
		trainFn: func(_ context.Context, _ string) (mlaas.JobRef, error) {
			return mlaas.JobRef{}, &mlaas.UpstreamError{Status: http.StatusConflict, Message: "model has no trained version yet"}
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-cpu-forecast/train", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "model has no trained version yet" {
		t.Errorf("error = %q, want the upstream message verbatim, not the wrapped Error() string", body["error"])
	}
}

func TestHandleMlaasTrain_OtherErrorIs502(t *testing.T) {
	svc := &fakeMlaasService{
		trainFn: func(_ context.Context, _ string) (mlaas.JobRef, error) {
			return mlaas.JobRef{}, errors.New("dial tcp 127.0.0.1:8090: connection refused")
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-cpu-forecast/train", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleMlaasPredict_NotConfiguredIs503(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-log-severity/predict",
		bytes.NewBufferString(`{"rows":[{"message":"boom"}]}`))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleMlaasPredict_Success(t *testing.T) {
	var gotName string
	var gotRows []map[string]any
	svc := &fakeMlaasService{
		predictFn: func(_ context.Context, name string, rows []map[string]any) (mlaas.PredictResult, error) {
			gotName, gotRows = name, rows
			return mlaas.PredictResult{Model: name, Version: 3, Predictions: []mlaas.PredictedRow{{Value: "error"}}}, nil
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-log-severity/predict",
		bytes.NewBufferString(`{"rows":[{"message":"disk full"}]}`))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if gotName != "forsight-log-severity" || len(gotRows) != 1 || gotRows[0]["message"] != "disk full" {
		t.Fatalf("service saw name=%q rows=%+v", gotName, gotRows)
	}
	var result mlaas.PredictResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Model != "forsight-log-severity" || len(result.Predictions) != 1 || result.Predictions[0].Value != "error" {
		t.Fatalf("got %+v", result)
	}
}

func TestHandleMlaasPredict_RejectsBadJSON(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(&fakeMlaasService{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-log-severity/predict",
		bytes.NewBufferString("not json"))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleMlaasPredict_RejectsEmptyRows(t *testing.T) {
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(&fakeMlaasService{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-log-severity/predict",
		bytes.NewBufferString(`{"rows":[]}`))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleMlaasPredict_RejectsMoreThanTenRows(t *testing.T) {
	svc := &fakeMlaasService{
		predictFn: func(_ context.Context, _ string, _ []map[string]any) (mlaas.PredictResult, error) {
			t.Fatal("service must not be called once the row cap is already violated")
			return mlaas.PredictResult{}, nil
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rows := make([]map[string]any, 11)
	for i := range rows {
		rows[i] = map[string]any{"message": "line"}
	}
	payload, err := json.Marshal(map[string]any{"rows": rows})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-log-severity/predict", bytes.NewReader(payload))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleMlaasPredict_RejectsOversizedBody(t *testing.T) {
	svc := &fakeMlaasService{
		predictFn: func(_ context.Context, _ string, _ []map[string]any) (mlaas.PredictResult, error) {
			t.Fatal("service must not be called for a body over the size limit")
			return mlaas.PredictResult{}, nil
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	// One long string cell pushes the whole body well past mlaasMaxBodyBytes;
	// LimitReader cuts it mid-string, so what's left never parses as JSON.
	long := strings.Repeat("a", mlaasMaxBodyBytes+1024)
	payload, err := json.Marshal(map[string]any{"rows": []map[string]any{{"message": long}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) <= mlaasMaxBodyBytes {
		t.Fatalf("payload is %d bytes, want more than the %d-byte cap", len(payload), mlaasMaxBodyBytes)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-log-severity/predict", bytes.NewReader(payload))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleMlaasPredict_UpstreamErrorPassesThroughStatus(t *testing.T) {
	svc := &fakeMlaasService{
		predictFn: func(_ context.Context, _ string, _ []map[string]any) (mlaas.PredictResult, error) {
			return mlaas.PredictResult{}, &mlaas.UpstreamError{Status: http.StatusBadRequest, Message: "unknown feature: bogus"}
		},
	}
	s := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).WithMlaas(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mlaas/models/forsight-log-severity/predict",
		bytes.NewBufferString(`{"rows":[{"message":"x"}]}`))
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "unknown feature: bogus" {
		t.Errorf("error = %q, want the upstream message", body["error"])
	}
}
