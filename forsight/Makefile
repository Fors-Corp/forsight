# forsight is a Go module inside the forsight repo —
# run `make` from this directory (forsight/), not the repo root.
#
# Building just the agent needs only Go (`make build-go`); building the real
# dashboard also needs Node, since it's a small React app on
# @marcfs31/forsight (see web/README.md and
# internal/api/dashboard.go's doc comment on why webdist/ is checked in).

VERSION ?= dev
LDFLAGS := -X github.com/marcfs31/forsight/forsight/internal/version.Version=$(VERSION)

.PHONY: build build-web build-go release dev test lint vet clean

# OS/arch pairs cross-compiled for a release — matches install.sh's
# "forsight_<os>_<arch>.tar.gz" naming exactly.
RELEASE_TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# Full release build: real dashboard + the agent binary.
build: build-web build-go

# Builds forsight/web against the pinned published @marcfs31/forsight from
# GitHub Packages, then copies the output into internal/api/webdist/ for
# go:embed. Run this after any change under forsight/web/ or a bump of the
# design-system pin — the diff it produces in webdist/ is expected, not a
# mistake. Needs packages:read auth for npm (see forsight/web/.npmrc).
build-web:
	cd web && npm ci && npm run build
	rm -rf internal/api/webdist/assets
	cp -r web/dist/. internal/api/webdist/

build-go:
	mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/forsight .

# Runs the agent directly against whatever's currently embedded in
# internal/api/webdist/ (the fallback page unless build-web has been run).
dev:
	go run . run

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin dist

# `make release VERSION=v1.0.1` — cross-compiles the real (build-web) binary
# for every target in RELEASE_TARGETS and tars each one up as
# dist/forsight_<os>_<arch>.tar.gz. Upload the result with:
#   gh release create forsight-$(VERSION) dist/*.tar.gz
# `.github/workflows/forsight-release.yml` runs exactly this on every
# forsight-vX.Y.Z tag push — see forsight/README.md, Releasing.
release: build-web
	@if [ "$(VERSION)" = "dev" ]; then \
		echo "release: pass an explicit VERSION, e.g. make release VERSION=v1.0.1" >&2; \
		exit 1; \
	fi
	mkdir -p dist
	@for target in $(RELEASE_TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "building $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o dist/forsight . || exit 1; \
		tar -czf dist/forsight_$${os}_$${arch}.tar.gz -C dist forsight; \
		rm dist/forsight; \
	done
	@ls -la dist/
