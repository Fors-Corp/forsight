# forsight is a Go module inside the forsight repo —
# run `make` from this directory (forsight/), not the repo root.
#
# Building just the agent needs only Go (`make build-go`); building the real
# dashboard also needs Node, since it's a small React app on
# @marcfs31/forsight (see web/README.md and
# internal/api/dashboard.go's doc comment on why webdist/ is checked in).

VERSION ?= dev
LDFLAGS := -X github.com/marcfs31/forsight/forsight/internal/version.Version=$(VERSION)

.PHONY: build build-web build-go release dev demo test test-install lint vet clean

# OS/arch pairs cross-compiled for a release — matches install.sh's
# "forsight_<os>_<arch>.tar.gz" naming exactly.
RELEASE_TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# Full release build: real dashboard + the agent binary.
build: build-web build-go

# Builds forsight/web against the pinned published @marcfs31/forsight from
# GitHub Packages, then copies the output into internal/api/webdist/ for
# go:embed. Run this after any change under forsight/web/ or a bump of the
# design-system pin — the diff it produces in webdist/ is expected, not a
# mistake. Needs packages:read auth for npm.pkg.github.com (see
# forsight/web/.npmrc for the registry mapping): this target sources a token
# from NODE_AUTH_TOKEN (set by CI) or, locally, `gh auth token`, and writes
# it to a throwaway npm user config for the duration of `npm ci` — never
# into the tree, never printed, removed on exit whether or not the build
# succeeds.
build-web:
	NPM_TOKEN="$${NODE_AUTH_TOKEN:-$$(gh auth token 2>/dev/null)}"; \
	if [ -z "$$NPM_TOKEN" ]; then \
		echo "build-web: no npm.pkg.github.com credential — set NODE_AUTH_TOKEN (CI) or 'gh auth login' with packages:read locally" >&2; \
		exit 1; \
	fi; \
	NPM_CONFIG_USERCONFIG=$$(mktemp) || exit 1; \
	trap 'rm -f "$$NPM_CONFIG_USERCONFIG"' EXIT; \
	echo "//npm.pkg.github.com/:_authToken=$$NPM_TOKEN" > "$$NPM_CONFIG_USERCONFIG"; \
	export NPM_CONFIG_USERCONFIG; \
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

# Runs against a bounded, clearly-labelled synthetic stream instead of real
# collectors — a populated dashboard with no infrastructure to point it at.
# See cmd/demo.go and README.md's CLI section for the flags (--backfill,
# --tick, --seed, ...).
demo:
	go run . demo

test:
	go test ./...

# install.sh's own tests (checksum verification, the systemd unit and its
# never-overwritten env file) — bats-core, not go test, since install.sh is
# POSIX sh with no Go runtime involved. Not part of the Go module's `test`
# target or forsight-ci.yml's `go` job; run it directly when touching
# install.sh. brew install bats-core (or apt-get install bats).
test-install:
	bats install_test.bats

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin dist

# `make release VERSION=v1.0.1` — cross-compiles the real (build-web) binary
# for every target in RELEASE_TARGETS, tars each one up as
# dist/forsight_<os>_<arch>.tar.gz, and writes dist/sha256sums.txt over all
# of them — install.sh downloads and verifies against this file before it
# extracts anything, so it must ship alongside the archives:
#   gh release create forsight-$(VERSION) dist/*.tar.gz dist/sha256sums.txt
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
	@cd dist && \
	if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum forsight_*.tar.gz > sha256sums.txt; \
	else \
		shasum -a 256 forsight_*.tar.gz > sha256sums.txt; \
	fi
	@ls -la dist/
