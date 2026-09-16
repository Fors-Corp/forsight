package api

import (
	"embed"
	"io/fs"
	"net/http"
)

// webdist holds the dashboard's static build output — a small React app
// (forsight/web) built on @marcfs31/forsight, the same design system this
// repo publishes. `go:embed` can't reach outside its own package directory,
// so `make build-web` copies web/dist's contents here (and `make build` runs
// it first; see the Makefile).
//
// What's checked into git is a snapshot of that real build — index.html plus
// its content-hashed assets/ — not a placeholder. It is refreshed
// deliberately, never on the fly: a change under forsight/web/ ships its
// rebuilt webdist/ on the same PR, and CI's `web` job fails if a fresh build
// differs from what is embedded, so the two cannot silently drift apart.
// Because the snapshot is already in the tree, a bare `go build`/`go test`
// needs no Node at all; the web toolchain is only needed to refresh it.
//
//go:embed all:webdist
var webdist embed.FS

// DashboardHandler serves the embedded dashboard build at "/".
func DashboardHandler() http.Handler {
	sub, err := fs.Sub(webdist, "webdist")
	if err != nil {
		// Only possible if the embed directive above and this path drift
		// apart, which a build would already have caught.
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
