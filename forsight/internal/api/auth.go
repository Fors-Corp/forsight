package api

import (
	"crypto/subtle"
	"net/http"
)

// BearerAuth wraps next so every route except GET /healthz and GET /readyz
// requires Authorization: Bearer <token>, compared with ConstantTimeCompare.
// An empty token disables auth entirely and returns next unchanged. /readyz
// is exempt for the same reason /healthz is: a kubelet probe hits it with no
// headers at all, so requiring a bearer token here would make every pod
// permanently unready the moment --auth-token is set.
func BearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") {
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
