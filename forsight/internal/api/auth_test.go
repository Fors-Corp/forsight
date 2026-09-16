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
	// The browser asks for this on its own. There is no favicon to serve,
	// so the answer is the mux's 404 — the point is that it is not the auth
	// layer's 401, which was only console noise.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("GET /favicon.ico status = 401; the auth layer should let it through")
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

// The dashboard's static shell — "/" and everything under "/assets/" — must
// stay reachable with no token at all, or the browser can never load the
// page that would let a person type one in.
func TestBearerAuth_ShellOpenWithoutToken(t *testing.T) {
	inner := NewServer(store.NewMemoryStore(time.Hour), nil, DashboardHandler(), nil).Handler()
	handler := BearerAuth("secret", inner)

	for _, path := range []string{"/", "/assets/index-CAuISbhf.js", "/assets/does-not-exist.js"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("GET %s: got 401 with no token, want the dashboard handler's own response", path)
		}
	}

	// "/" itself must actually serve index.html, not just skip the 401.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rec.Code)
	}
}

// A data route must stay protected even once the dashboard is mounted at
// "/" alongside it — the shell exemption must not widen past the shell.
func TestBearerAuth_DataRouteStillProtectedWithDashboardMounted(t *testing.T) {
	inner := NewServer(store.NewMemoryStore(time.Hour), nil, DashboardHandler(), nil).Handler()
	handler := BearerAuth("secret", inner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil))
	assertUnauthorized(t, rec)
}

// The exemption is for reads of the static shell only: a non-GET/HEAD
// request to "/" (nothing the dashboard build itself ever sends) still
// requires the token.
func TestBearerAuth_NonGetRootRequiresBearer(t *testing.T) {
	inner := NewServer(store.NewMemoryStore(time.Hour), nil, DashboardHandler(), nil).Handler()
	handler := BearerAuth("secret", inner)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	assertUnauthorized(t, rec)
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
