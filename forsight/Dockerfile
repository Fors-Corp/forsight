# Builds the forsight agent as a single static binary with no shell and no
# package manager in the final image — the DaemonSet manifest
# (deploy/k8s/daemonset.yaml) runs this on every node in a cluster, so the
# smaller and less exploitable the image, the smaller the fleet-wide blast
# radius of a container escape.
#
# The dashboard needs no build step here: forsight/internal/api/webdist/ is
# a checked-in snapshot of `forsight/web`'s build output, embedded via
# go:embed (see that package's doc comment and CLAUDE.md's "two independently
# versioned artifacts") — exactly what makes `go build` alone produce the
# real dashboard, with no Node stage in this Dockerfile.
#
# Build from the REPO ROOT, not from forsight/: go.mod replaces
# github.com/marcfs31/forsight/forseer with ../forseer (a sibling module),
# so the build context has to contain both directories.
#
#   docker build -f forsight/Dockerfile -t forsight .
#
# .github/workflows/forsight-release.yml builds and pushes this on every
# forsight-vX.Y.Z tag, as ghcr.io/marcfs31/forsight:<version> and :latest.

FROM golang:1.27-alpine AS builder
WORKDIR /src

# Dependencies first, so an edit to application code doesn't invalidate the
# module cache layer.
COPY forseer/go.mod forseer/go.sum forseer/
COPY forsight/go.mod forsight/go.sum forsight/
WORKDIR /src/forsight
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

WORKDIR /src
COPY forseer/ forseer/
COPY forsight/ forsight/
WORKDIR /src/forsight

# CGO_ENABLED=0 is what makes the binary static: every dependency here
# (gopsutil, Badger, the Moby/Docker client) is pure Go, so nothing is lost
# by disabling cgo, and a static binary is what lets the final stage be
# `scratch` with no libc for it to dynamically link against.
# -trimpath and -s -w drop local filesystem paths and debug symbols the
# running agent never needs, shrinking the image without changing behavior.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/forsight .

# scratch, not distroless: forsight has no shell dependency of its own (no
# subprocess, no filesystem tooling), and the DaemonSet manifest's own
# mounts (HOST_PROC/HOST_SYS/HOST_ETC, the Docker socket) are what the
# manifest needs, not the image — so there is nothing here for even a
# minimal distro image to provide beyond CA certificates and the binary.
FROM scratch

# The mlaas integration and Grok summaries (XAI_API_KEY) make outbound HTTPS
# calls that need to verify certificates; scratch has no certificate store
# of its own.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/forsight /forsight

# A fixed, unprivileged, non-root UID:GID rather than a named user — scratch
# has no /etc/passwd for a name to resolve against. Kubernetes' hostPath
# mounts of /proc, /sys, and /etc (read-only, per the DaemonSet manifest)
# don't need root to read.
USER 65532:65532

EXPOSE 8080
ENTRYPOINT ["/forsight"]
CMD ["run"]
