package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// isPublicPath reports whether a request can be served with no bearer token
// at all: GET/HEAD /healthz, and GET/HEAD on the dashboard's static shell
// ("/" and everything under "/assets/"). The shell is a public build — no
// agent data lives in index.html or its bundled JS/CSS, only the React app
// that then calls the protected /api/v1/* routes with the token the user
// types into it (see fetchWithAuth in forsight/web/src/api.ts). Without this
// exemption, --auth-token also locks the browser out of the one page that
// could ever prompt for the token.
func isPublicPath(method, path string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	if path == "/healthz" || path == "/" {
		return true
	}
	return strings.HasPrefix(path, "/assets/")
}

// BearerAuth wraps next so every route except the public paths above
// requires Authorization: Bearer <token>, compared with
// ConstantTimeCompare. An empty token disables auth entirely and returns
// next unchanged.
func BearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicPath(r.Method, r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
