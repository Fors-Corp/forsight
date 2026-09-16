package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcfs31/forsight/forsight/internal/store"
)

func TestBearerAuth_NoTokenLeavesRoutesOpen(t *testing.T) {
	inner := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).Handler()
	handler := BearerAuth("", inner)

	for _, path := range []string{"/healthz", "/readyz", "/api/v1/metrics", "/api/v1/logs"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("%s: got 401 with empty token, want the inner handler's response", path)
		}
	}
}

func TestBearerAuth_HealthzOpenWithoutHeader(t *testing.T) {
	inner := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).Handler()
	handler := BearerAuth("secret", inner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", rec.Code)
	}
}

func TestBearerAuth_ReadyzOpenWithoutHeader(t *testing.T) {
	inner := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).Handler()
	handler := BearerAuth("secret", inner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /readyz status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestBearerAuth_MetricsRequiresBearer(t *testing.T) {
	inner := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).Handler()
	handler := BearerAuth("secret", inner)

	t.Run("missing header", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil))
		assertUnauthorized(t, rec)
	})

	t.Run("wrong token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		handler.ServeHTTP(rec, req)
		assertUnauthorized(t, rec)
	})

	t.Run("correct token", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
		req.Header.Set("Authorization", "Bearer secret")
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("WWW-Authenticate = %q on 200, want empty", got)
		}
	})
}

func TestBearerAuth_LogsRequiresBearer(t *testing.T) {
	inner := NewServer(store.NewMemoryStore(time.Hour), nil, nil, nil).Handler()
	handler := BearerAuth("secret", inner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/logs", nil))
	assertUnauthorized(t, rec)

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/logs", nil)
	req.Header.Set("Authorization", "Bearer secret")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
}

func assertUnauthorized(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
}
