package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// isPublicPath reports whether a request can be served with no bearer token
// at all: GET/HEAD on the probes (/healthz, and /readyz, which a kubelet hits
// with no headers at all, so requiring a token there would make every pod
// permanently unready the moment --auth-token is set), on the browser's own
// /favicon.ico request, and on the dashboard's static shell ("/" and
// everything under "/assets/"). The shell is a public build — no agent data
// lives in index.html or its bundled JS/CSS, only the React app that then
// calls the protected /api/v1/* routes with the token the user types into it
// (see fetchWithAuth in forsight/web/src/api.ts). Without this exemption,
// --auth-token also locks the browser out of the one page that could ever
// prompt for the token.
func isPublicPath(method, path string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	switch path {
	case "/healthz", "/readyz", "/favicon.ico", "/":
		return true
	}
	return strings.HasPrefix(path, "/assets/")
}

// anonymousKey marks a request that reached a public path on a server with
// a token configured, without presenting it. Public paths are exempt from
// the 401 — a kubelet sends no headers, and the dashboard shell has to load
// before there is anywhere to type a token — but exempt from the check is
// not the same as trusted. /readyz reads this to decide how much to say.
//
// The absence of the mark means "trusted", not "unknown": a server with no
// token at all serves next unchanged, and the operator who turned auth off
// has decided every caller is equal. The only way a request is marked is
// the hardened middleware explicitly saying so.
type ctxKey int

const anonymousKey ctxKey = iota

// isAnonymous reports whether BearerAuth let this request through a public
// path without a valid token.
func isAnonymous(ctx context.Context) bool {
	v, _ := ctx.Value(anonymousKey).(bool)
	return v
}

// BearerAuth wraps next so every route except the public paths above
// requires Authorization: Bearer <token>. The header is never compared
// against the expected value directly: ConstantTimeCompare returns 0
// immediately when the two slices' lengths differ, so a raw comparison
// still leaks the token's length through timing — a header a byte too
// short or too long comes back faster than one of the right length. Both
// sides are hashed to a fixed 32-byte SHA-256 digest first, and it is the
// digests that go through ConstantTimeCompare, so every comparison costs
// the same regardless of what the caller sent. An empty token disables
// auth entirely and returns next unchanged. A public path is served
// either way, but a correct bearer on one still counts: the comparison
// happens first so /readyz can tell an operator with the token from
// anything that can reach the port.
func BearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		presented := subtle.ConstantTimeCompare(got[:], want[:]) == 1
		if isPublicPath(r.Method, r.URL.Path) {
			if !presented {
				r = r.WithContext(context.WithValue(r.Context(), anonymousKey, true))
			}
			next.ServeHTTP(w, r)
			return
		}
		if !presented {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
