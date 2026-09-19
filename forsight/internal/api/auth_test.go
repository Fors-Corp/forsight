package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"encoding/json"
	"errors"
	"github.com/marcfs31/forsight/forsight/internal/collector"
	"github.com/marcfs31/forsight/forsight/internal/store"
	"strings"
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

// readyzInternalsServer is a server whose /readyz has something to leak:
// a store whose ping error names a path, and a registry with a collector
// in it. The status itself is meant to be public; these two fields are not.
func readyzInternalsServer(t *testing.T) http.Handler {
	t.Helper()
	failing := pingFailingStore{
		Store: store.NewMemoryStore(time.Hour),
		err:   errors.New("badger: /var/lib/forsight/data: disk full"),
	}
	registry := collector.NewRegistry(failing, time.Hour, nil, fakeCollector{name: "docker"})
	return NewServer(failing, nil, nil, nil).WithRegistry(registry).Handler()
}

func readyzRaw(t *testing.T, h http.Handler, authorization string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding /readyz body %q: %v", rec.Body.String(), err)
	}
	return rec.Code, raw
}

// TestBearerAuth_ReadyzHidesInternalsFromAnAnonymousCaller: the readiness
// probe stays reachable without a token — that is the point of the
// exemption — but on a server that HAS a token, a caller without it gets
// the status and nothing else. Before this, the same request returned the
// store's on-disk path in storeError and the whole collector inventory.
func TestBearerAuth_ReadyzHidesInternalsFromAnAnonymousCaller(t *testing.T) {
	code, raw := readyzRaw(t, BearerAuth("secret", readyzInternalsServer(t)), "")

	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: the status code itself must stay public", code)
	}
	if raw["status"] != "unavailable" {
		t.Errorf("status = %v, want unavailable: the readiness answer must stay public", raw["status"])
	}
	if v, ok := raw["storeError"]; ok {
		t.Errorf("storeError leaked to an anonymous caller: %v", v)
	}
	if v, ok := raw["collectors"]; ok {
		t.Errorf("collector inventory leaked to an anonymous caller: %v", v)
	}
}

// TestBearerAuth_ReadyzShowsInternalsToTheBearer: presenting the token on
// the public path is still worth something.
func TestBearerAuth_ReadyzShowsInternalsToTheBearer(t *testing.T) {
	code, raw := readyzRaw(t, BearerAuth("secret", readyzInternalsServer(t)), "Bearer secret")

	if code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", code)
	}
	if se, _ := raw["storeError"].(string); !strings.Contains(se, "/var/lib/forsight") {
		t.Errorf("storeError = %q, want the store's own error text for an authenticated operator", se)
	}
	cs, _ := raw["collectors"].([]any)
	if len(cs) != 1 {
		t.Errorf("collectors = %v, want the one registered collector", raw["collectors"])
	}
}

// TestBearerAuth_ReadyzWrongBearerIsPublicButBlind: a bad token on a public
// path must not turn a health probe into a 401 — but it earns nothing.
func TestBearerAuth_ReadyzWrongBearerIsPublicButBlind(t *testing.T) {
	code, raw := readyzRaw(t, BearerAuth("secret", readyzInternalsServer(t)), "Bearer wrong")

	if code == http.StatusUnauthorized {
		t.Fatal("a public path answered 401 to a wrong bearer; the probe must still be served")
	}
	if _, ok := raw["collectors"]; ok {
		t.Error("a wrong bearer was treated as authenticated")
	}
}

// TestBearerAuth_ReadyzShowsInternalsWhenAuthIsOff pins the default: no
// token configured means no distinction between callers, so the operator
// who runs without auth still gets the full probe body. Hiding is something
// only the hardened middleware asks for.
func TestBearerAuth_ReadyzShowsInternalsWhenAuthIsOff(t *testing.T) {
	_, raw := readyzRaw(t, BearerAuth("", readyzInternalsServer(t)), "")

	if _, ok := raw["storeError"]; !ok {
		t.Error("storeError hidden with auth disabled")
	}
	if _, ok := raw["collectors"]; !ok {
		t.Error("collectors hidden with auth disabled")
	}
}
