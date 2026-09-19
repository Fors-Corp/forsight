# forsight

A self-contained observability agent: one binary that collects, stores, and
serves — no Docker, no Prometheus, no Grafana required to get a working
observability platform.

- **Host metrics** — CPU, memory, disk, network, uptime, via
  [gopsutil](https://github.com/shirou/gopsutil). Works on any OS Go
  supports, with zero configuration.
- **Process metrics** — per-process CPU and RSS for the busiest processes,
  on by default (`--disable-proc` to skip).
- **Docker container metrics** — per-container CPU/memory, auto-discovered
  from the local Docker daemon if one is reachable; simply doesn't register
  if not (a laptop with no Docker running still works fine).
- **OpenTelemetry ingestion** — a standard OTLP/HTTP receiver
  (`/v1/metrics`, `/v1/traces`, `/v1/logs`) so any app instrumented with an
  OTel SDK can point its exporter at forsight with no forsight-specific
  integration. Gauge, Sum, Histogram, ExponentialHistogram and Summary, plus
  log records, over protobuf or JSON bodies.
- **Prometheus scraping** — `--scrape` any exposition endpoint, **and** a
  default probe of well-known local exporters (`node_exporter` :9100,
  Prometheus :9090, windows_exporter :9182, process-exporter :9273). A miss
  is silence. `--disable-autoscrape` turns the probe off.
- **HTTP and TLS-expiry probes** — `--probe <url>` (repeatable) GETs a URL on
  `--collect-interval`, reporting `probe.http.up`/`.status`/`.duration_ms`
  and, for `https://` targets, `probe.tls.days_remaining` and `.valid` from
  the leaf certificate — read independently, so a certificate nearing (or
  past) expiry never makes an otherwise-reachable site look down. The
  Overview page shows an uptime strip per target, plus its certificate's
  days to expiry, once at least one `--probe` is configured.
- **StatsD / DogStatsD** — listens on `:8125` by default so a bare install
  receives them. `--disable-statsd` turns it off; a bind failure is a
  warning, not a crash.
- **Log-file tailing** — `--log-file <path>` (repeatable) follows a file on
  disk into the same log store OTLP log records land in, each line's
  severity classified the way `/api/v1/forseer/classify` classifies it. A
  tail that fails restarts with backoff rather than taking the agent down.
- **Forseer** — AI/ML lives in the sibling [`forseer/`](../forseer/) folder.
  Statistical detectors (z-score, CUSUM, log templates, slow spans, process
  culprits) are always on. Optional Grok narrative when `XAI_API_KEY` is set.
- **mlaas** — an optional bridge (`internal/mlaas/`) to a separate
  ML-as-a-Service for the handful of models that want a real holdout and
  their own retrain loop instead of Forseer's online, stdlib-only ones. Off
  by default; set `--mlaas-url` to turn it on. See "mlaas integration"
  below.
- **Kubernetes** — the [DaemonSet manifest](deploy/k8s/daemonset.yaml) runs
  this exact binary on every node, reusing the same host/Docker collectors
  (see the manifest's own comments for how and why).
- **Storage and dashboard, built in** — an embedded, retention-bounded store
  and a real dashboard (built on
  [`@marcfs31/forsight`](..), the design system this
  repo also publishes) served from the same process. No separate database,
  no separate frontend server. In-memory by default; add `--store badger` for
  a store that survives a restart (see below). A light/dark theme toggle
  lives in the sidebar footer and is remembered per browser.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Fors-Corp/forsight/main/forsight/install.sh | sh
forsight run
```

Downloads the right binary for your OS/architecture, verifies it against the
release's `sha256sums.txt` before extracting anything, and — on Linux, run as
root — registers and starts a systemd service. Everywhere else, `forsight run`
starts it directly. See [install.sh](install.sh) for the env vars that
control the install location and version.

**Flags survive an upgrade.** On Linux as root, the unit's
`ExecStart` reads `$FORSIGHT_ARGS` from `/etc/default/forsight`
(`EnvironmentFile=-/etc/default/forsight`) rather than hard-coding flags into
the unit file. install.sh writes that file once, commented out, the first
time it installs, and never touches it again — so `--store badger`,
`--auth-token` and `--mlaas-url` set there survive the next `curl | sh`
re-run instead of getting silently dropped back to a bare `forsight run`:

```bash
# /etc/default/forsight
FORSIGHT_ARGS="--store badger --auth-token <token> --mlaas-url http://localhost:9000"
```

```bash
sudo systemctl restart forsight   # after editing the file above
```

Then open `http://localhost:8080` for the dashboard.

**No infrastructure handy?** `forsight demo` (or `make demo`) serves the same
dashboard against a bounded, clearly-labelled synthetic stream instead of
real collectors — see "forsight demo" below.

## Why Go

Every product this agent is modeled on is written in Go: Prometheus and
`node_exporter`, Grafana Alloy, the OpenTelemetry Collector, Loki, the
Datadog Agent — and Kubernetes itself, which is why `client-go`-adjacent
tooling and single-static-binary distribution are such a natural fit here.
Go produces one dependency-free binary per OS/arch (no runtime, no libc
concerns), which is what makes `curl | sh` installation actually work, and
its standard library plus `gopsutil`/`go.opentelemetry.io/proto/otlp`/
`github.com/moby/moby` cover this project's whole surface without exotic
dependencies. Rust (Datadog's own Vector) or C++ (Dynatrace OneAgent) would
both beat Go on raw memory footprint, but at real cost to how fast a project
covering four different target environments can be built correctly — see the
project's planning notes for the full comparison.

## CLI

```
forsight run [flags]
    --addr string                address to serve on (default ":8080")
    --retention duration         how long the store retains data, memory or Badger alike (default 1h)
    --store string                storage backend: "memory" (default, resets on restart)
                                 or "badger" (persists to --data-dir, survives a restart)
    --data-dir string             directory for the Badger database when --store=badger
                                 (default "./forsight-data"); ignored otherwise
    --collect-interval duration  how often pull-based collectors poll (default 10s)
    --disable-docker             skip the Docker collector even if a daemon is reachable
    --disable-otlp               don't mount the OTLP ingest endpoints
    --disable-proc               skip per-process CPU/memory collection
    --disable-statsd             don't listen for StatsD/DogStatsD
    --disable-autoscrape         don't probe well-known local Prometheus exporters
    --scrape string              Prometheus exposition endpoint to scrape, repeatable;
                                 optionally prefixed with a job name
                                 (--scrape node=http://localhost:9100/metrics)
    --probe string                HTTP(S) URL to GET on --collect-interval, repeatable;
                                 optionally prefixed with a name
                                 (--probe checkout=https://example.com/healthz)
    --statsd-addr string         StatsD/DogStatsD UDP listen address (default :8125)
    --log-file string            log file to tail into the store (repeatable)
    --auth-token string          require Authorization: Bearer <token> on every route except
                                 the dashboard's static shell and GET /healthz; also read from
                                 FORSIGHT_AUTH_TOKEN (off by default) — the dashboard itself
                                 prompts for the token on first use and holds it in
                                 sessionStorage, so this doesn't lock the browser out
    --tls-cert string            PEM certificate; with --tls-key, serves HTTPS instead of
                                 plaintext HTTP (off by default); also read from FORSIGHT_TLS_CERT
    --tls-key string             PEM private key matching --tls-cert;
                                 also read from FORSIGHT_TLS_KEY
    --tls-client-ca string       PEM CA bundle; with --tls-cert/--tls-key, requires and verifies
                                 a client certificate signed by it on every connection (mTLS);
                                 also read from FORSIGHT_TLS_CLIENT_CA
    --mlaas-url string           base URL of a running mlaas ML service; empty (the
                                 default) disables the integration; also read from MLAAS_URL
    --mlaas-api-key-file string  path to a file holding the mlaas API key;
                                 also read from MLAAS_API_KEY_FILE, or the key itself from
                                 MLAAS_API_KEY — never a flag value, so it doesn't show up in `ps`
    --mlaas-sync-interval duration how often datasets are exported and
                                 forecasts/feedback are refreshed (default 5m)
    --mlaas-prefix string        names everything this agent creates in mlaas,
                                 so several agents can share one server (default "forsight")

forsight demo [flags]
    --addr string                address to serve the API and dashboard on (default ":8080")
    --auth-token string           require Authorization: Bearer <token> on every route except
                                 the dashboard's static shell and GET /healthz; also read from
                                 FORSIGHT_AUTH_TOKEN
    --backfill duration           how much synthetic history to generate before serving, so the
                                 dashboard opens already populated (default 6h); 0 disables it
    --tick duration               spacing between synthetic samples, backfilled and live (default 10s)
    --retention duration          how long the store keeps synthetic data; 0 (the default)
                                 picks --backfill plus a two-hour margin
    --seed uint                   seed for the synthetic generator; the same seed always
                                 produces the same demo data (default 1)

forsight version
```

### `forsight demo`

Runs the same API/dashboard server as `forsight run`, but instead of real
collectors it feeds a bounded, clearly-labelled synthetic stream through the
same `Engine.Observe*` and store paths a real deployment uses: host and
process metrics with a daily cycle and one CPU spike attributed to a named
culprit process, a handful of log templates (a mix of declared and inferred
severities, plus one correlated error-log burst), and a couple of traces
with one deliberately slow child span. Every point carries a `host` label
(or, for logs, a `demo.*` source) of `forsight-demo` or similar, so nothing
it writes can be mistaken for a real host, process, or service. With the
default `--backfill 6h` the time-range picker and Forseer's own models
(`/api/v1/forseer/models`) have a trend to show from the first request
instead of a blank chart; `--backfill 0` starts from an empty stream and
lets the spike, burst, and slow trace arrive live instead. There is no
Docker collector or OTLP ingest in demo mode — the Containers panel stays
empty, as it would with no Docker daemon reachable.

### Persistent storage (`--store badger`)

By default `forsight run` stores everything in memory: fast, zero setup, and
gone on restart. Pass `--store badger --data-dir /path/to/dir` to persist
metrics, spans, and logs to an embedded [Badger](https://github.com/dgraph-io/badger)
key-value database instead — same query semantics (time-range + exact-label
match, retention-based pruning), same `Store` interface, so it's a drop-in
swap and nothing else about `run` changes. `--data-dir` defaults to
`./forsight-data` and is created if it doesn't exist; `--retention` governs
Badger's per-entry TTL the same way it governs MemoryStore's pruning.
`internal/store/badger.go`'s doc comment covers the key encoding and why it's
shaped the way it is.

`--store badger` also gives Forseer itself somewhere to persist what it has
learned: `<data-dir>/forseer.json`, one JSON document written on shutdown and
read back at startup, before the severity model's fallback wires up. It
carries each model's own trained state (naive-Bayes counts, per-series
thresholds, Holt's forecast, and so on) so a restart doesn't re-earn
`severityMinTrained`/`thresholdCriticalMinSamples`/every other model's own warm-up
from zero; each model's prequential grading window still resets, so
readiness against a fallback is always re-earned on live data. `--store
memory` has no data dir, so Forseer stays cold on every restart, the same as
its metrics/logs/spans. See
[`forseer/MODELS.md`](../forseer/MODELS.md#persisted-state) for what
persists model by model and what doesn't.

### The dashboard under `--auth-token`

`--auth-token` protects every route except the dashboard's static shell
(`GET`/`HEAD` on `/` and `/assets/*`) and `GET /healthz` — the shell carries
no agent data, only the React app that then has to call the protected
`/api/v1/*` routes itself. So the browser can always load the page; what it
can't do without the token is see any data. The first request that comes
back `401` opens a modal prompt (built from the design system's `Dialog` and
`Input`) asking for the token forsight was started with; a correct one is
kept in `sessionStorage` — never `localStorage`, and never put in a URL or a
log line — for the rest of that tab's life, and every poll and action in
`forsight/web/src/api.ts` attaches it via `fetchWithAuth`. A wrong guess (or
a token that later stops matching, e.g. the agent restarted with a new one)
clears it and reopens the same prompt with an "incorrect token" state.

### TLS (`--tls-cert`, `--tls-key`)

By default `forsight run` serves plain HTTP. On a loopback `--addr` that's
fine; on any other address — a DaemonSet's, most often — every payload and
the `--auth-token` bearer token itself cross the network in cleartext. Set
`--tls-cert` and `--tls-key` (PEM, env fallbacks `FORSIGHT_TLS_CERT` and
`FORSIGHT_TLS_KEY`) to switch the listener to HTTPS; a missing file, an
unreadable one, or a certificate that doesn't match the key fails at
startup and never falls back to plaintext. Add `--tls-client-ca` (a PEM CA
bundle, env `FORSIGHT_TLS_CLIENT_CA`) to also require and verify a client
certificate signed by it on every connection (mTLS) — useful when the agent
sits behind nothing but the network itself, as in the DaemonSet. All three
flags use `crypto/tls` and `crypto/x509` from the standard library, so
`forsight` stays one static binary. `--auth-token` set without TLS on a
non-loopback address logs a warning at startup; running one without the
other is still allowed, since a reverse proxy or service mesh may already
be terminating TLS in front of the agent.

## mlaas integration

`forsight` can hand a handful of models to
[mlaas](https://github.com/Fors-Corp/mlaas) — Marc's separate ML-as-a-Service
— instead of training everything in-binary. The split is deliberate:
Forseer's detectors (see [`forseer/`](../forseer/)) are stdlib-only, train
online on the ingest path, and carry no weights, so each one gates on a
named fallback and reports a prequential score. A model handed to mlaas
trades that for a real holdout, a champion/challenger loop, and its own
retrain schedule, at the cost of running a second process. Both kinds show
up together on the dashboard's Models page so an operator can compare them
rather than have to know which is which.

The integration is off by default: an empty `--mlaas-url` disables it
entirely and nothing is exported.

**What's trained.** `internal/mlaas` manages exactly four models, named
`<prefix>-<suffix>` (prefix default `forsight`, so more than one agent can
share one mlaas):

| Model             | Reads                                                        | Job                                                                                                                   |
| ------------------ | ------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `cpu-forecast`     | `host.cpu.percent`, one-minute means                          | Say where host CPU is heading over the next hour.                                                                       |
| `memory-forecast`  | `host.memory.percent`, one-minute means                       | Say where host memory is heading over the next hour.                                                                    |
| `disk-forecast`    | `host.disk.percent`, one-minute means                         | Say where disk usage is heading, and so when it fills.                                                                  |
| `log-severity`     | log lines whose source declared a level (never an inferred one) | Give a log line the level this deployment would have given it — the same job as Forseer's in-binary model, served with a real holdout score. |

The three forecasts run mlaas's `holtwinters` plugin; the severity model
runs `bayes` — the same job Forseer's own log-severity model already does,
which is why the Models page puts the two side by side rather than picking
one.

On a timer (`--mlaas-sync-interval`), the agent exports one-minute buckets
of each host series and declared-severity log lines as CSV, uploads
whichever changed, creates and trains any model mlaas doesn't have yet, and
— once a model has a champion — asks it for the next hour's forecast, or
feeds it a sample of freshly-declared log lines and posts the outcome back
as feedback. That loop is what makes mlaas's live accuracy and retrain
triggers measure real, continuing data instead of just the original
training snapshot.

**The Models page** (in the dashboard) shows both kinds of model: a card for
what trains inside the agent, gated on its fallback, and a card for what
mlaas serves — state, champion version, holdout and live score, new labels,
drift, and Train/Tune buttons — plus the forecast charts and the most
recent severity predictions next to what Forseer said about the same line.
See [`forseer/MODELS.md`](../forseer/MODELS.md) for the Forseer half of the
contract.

**Flags:**

- `--mlaas-url` (env `MLAAS_URL`) — base URL of a running mlaas, e.g.
  `http://127.0.0.1:8090`. Empty, the default, disables the integration.
- `--mlaas-api-key-file` (env `MLAAS_API_KEY_FILE`), or the key itself via
  env `MLAAS_API_KEY` — never a flag value, so it doesn't show up in `ps`.
  mlaas writes its own key to `<data>/api_key`.
- `--mlaas-sync-interval` — how often datasets are exported and
  forecasts/feedback are refreshed (default 5m).
- `--mlaas-prefix` — names everything this agent creates in mlaas (default
  `forsight`).

**The key never reaches the browser.** The dashboard calls the agent's own
`/api/v1/mlaas/*` routes (behind `--auth-token` when one is set); the agent
is the only thing that holds the mlaas API key, and the only thing that
calls mlaas directly. Run mlaas on loopback, or put it behind HTTPS if it
has to be reached over a network the agent doesn't share with it — the key
goes over the wire as a plain `X-API-Key` header.

**Retention, for forecasts worth having.** The forecast exporter buckets up
to 14 days of history; the default `--store memory --retention 1h` doesn't
hold enough of a series for mlaas to learn a trend from. Run with
`--store badger --retention 168h` (or more) when the forecasts matter.

**When mlaas is down.** The sync pass probes `GET /healthz` first; if that
fails, the pass records that mlaas is unreachable, along with the error,
and skips the rest of the pass — nothing else about the agent is affected.
The Models page keeps showing the last known state for each model (or
`waiting-for-data`/`missing` before the first successful contact) and marks
the mlaas card unreachable rather than hiding it.

## HTTP surface

| Path                                    | Method | What                                                    |
| ---------------------------------------- | ------ | -------------------------------------------------------- |
| `/`                                      | GET    | The dashboard                                             |
| `/healthz`                               | GET    | `{"status":"ok"}` unconditionally — for a liveness probe    |
| `/readyz`                                | GET    | Pings the store and reports each collector's last error; 503 only if the store ping fails — for a readiness probe |
| `/api/v1/metrics?name=&since=&before=&limit=&per_name=&label.<k>=<v>` | GET | Query stored metrics (all filters optional; `limit` keeps the newest N, `per_name` the newest N of every metric name) |
| `/api/v1/traces?service=&traceId=&since=`    | GET | Query stored spans                                        |
| `/api/v1/logs?since=&source=&severity=`       | GET | Query stored log entries                                  |
| `/api/v1/forseer/insights`                   | GET | Current Forseer findings (always on, no API key)          |
| `/api/v1/forseer/clusters`                   | GET | Drain-style log templates                                 |
| `/api/v1/forseer/budget`                     | GET | Error-log burn against a 1% SLO (ErrorBudget)             |
| `/api/v1/forseer/timeline`                   | GET | Stitched incident events (Timeline)                       |
| `/api/v1/forseer/query?q=`                   | GET | Phrase → FilterBar facets                                 |
| `/api/v1/forseer/models`                     | GET | What each trained model reads, whether it is ready, how it scores |
| `/api/v1/forseer/summary`                    | GET | Grok paragraph when `XAI_API_KEY` is set; else disabled   |
| `/api/v1/forseer/classify?message=`          | GET | Classify one log line: the in-binary severity model if it's ready, else the substring fallback |
| `/api/v1/mlaas/status`                       | GET | Snapshot of the mlaas integration: model state, forecasts, recent predictions, jobs |
| `/api/v1/mlaas/models/{name}/train`          | POST | Queue a training job for one of the four models `forsight` manages in mlaas |
| `/api/v1/mlaas/models/{name}/tune`           | POST | Queue a tuning job for the same                           |
| `/api/v1/mlaas/models/{name}/predict`        | POST | Proxy up to 10 rows to a managed mlaas model and return its predictions |
| `/v1/metrics`                            | POST   | OTLP/HTTP metrics ingest (protobuf or JSON body)          |
| `/v1/traces`                             | POST   | OTLP/HTTP traces ingest (protobuf or JSON body)           |
| `/v1/logs`                               | POST   | OTLP/HTTP logs ingest (protobuf or JSON body)             |

Point any OpenTelemetry SDK's OTLP/HTTP exporter at `http://<host>:8080` and
it works unmodified — those are the standard OTLP paths every SDK already
uses by default.

## Architecture

```
forseer/                   AI/ML module imported by the agent (detectors + Grok)
forsight/
  main.go, cmd/            CLI (cobra): `run`, `version`
  internal/
    model/                 shared data shapes: Metric, Span, LogEntry
    collector/              the Collector interface + a scheduling Registry
      host/                 gopsutil — CPU/memory/disk/network/uptime/fds/conns
      proc/                  per-process CPU, RSS, and fd count
      docker/                Docker API — per-container CPU/memory
      otlp/                  OTLP/HTTP receiver — metrics (all five types),
                             traces, and logs (protobuf or JSON)
      promscrape/            Prometheus exposition-format scraper + local discover
      statsd/                StatsD/DogStatsD UDP receiver
      filelog/               tail --log-file paths into LogEntry
    store/                  Store interface + MemoryStore (default) and BadgerStore
                             (--store badger) implementations, same query semantics
    api/                    HTTP server: query API, OTLP mount, embedded dashboard
  web/                      the dashboard — a small React app on the design system
  deploy/k8s/               DaemonSet manifest for cluster-wide deployment
  Dockerfile                 static-binary image the DaemonSet runs
  install.sh                 curl|sh installer — verifies sha256sums.txt before extracting
  install_test.bats          install.sh's own tests (bats-core)
  Makefile                   build-web, build-go, build, release, dev, test, test-install, lint
```

**The "one host collector, four environments" trick**: rather than writing a
Kubernetes-specific collector, the [DaemonSet manifest](deploy/k8s/daemonset.yaml)
mounts the host's `/proc`/`/sys`/`/etc` into the pod and sets
`HOST_PROC`/`HOST_SYS`/`HOST_ETC` — the exact convention `node_exporter`
itself uses — so the *same* host collector code gets real node-level metrics
with zero forsight-specific Kubernetes code. See that manifest's comments for
the Docker-socket caveat on non-Docker-runtime clusters.

**Running in Kubernetes.** Build the image from the repo root —
`docker build -f forsight/Dockerfile -t forsight .` (see the
[Dockerfile](Dockerfile)'s own comment for why the root, not `forsight/`, is
the build context) — push it wherever the manifest's `image:` points, then
`kubectl apply -f forsight/deploy/k8s/daemonset.yaml`. The container's
`livenessProbe` hits `/healthz` (unconditional, so it can never crash-loop a
pod over something a restart won't fix); its `readinessProbe` hits `/readyz`,
which pings the store and reports each collector's last error but only fails
the probe if the store itself is unreachable — a missing Docker socket is
reported, not a reason to pull the pod from service.

## Building from source

```bash
brew install go golangci-lint   # or your platform's equivalent
make               # or `make help` — lists every target below with its description
make build-go     # agent only — no Node needed, embeds the webdist/ snapshot already in git
make build-web    # dashboard only — builds web/ against the published design-system pin, then embeds it
make build        # both
make test         # go test ./...
make test-install # bats install_test.bats — install.sh's checksum/systemd-unit behavior
make lint         # golangci-lint run ./...
make dev          # go run . run, against whatever's currently embedded
```

`internal/api/webdist/` (the dashboard's build output) is checked into git
deliberately, so a bare `go build`/`go test` always works without Node
installed at all — only refreshing the *real* dashboard after a change to
`web/` needs the JS toolchain. CI enforces that the checked-in copy actually
matches what `web/` currently builds (`.github/workflows/forsight-ci.yml`'s
`web` job) — bump it with `make build-web` and commit the diff.

## Releasing

A release starts by updating `forsight/CHANGELOG.md`: move the entries
under `## [Unreleased]` to a new `## [X.Y.Z] - YYYY-MM-DD` heading matching
the tag you're about to cut, and commit that before tagging — the tag
should never point at a commit whose changelog still says the version is
unreleased.

Then push a `forsight-vX.Y.Z` tag and `.github/workflows/forsight-release.yml`
does the rest: it checks that tag out, cross-compiles every
`RELEASE_TARGETS` entry via `make release`, and publishes a GitHub Release
with the resulting `dist/*.tar.gz` archives attached.

`make release` also writes `dist/sha256sums.txt` over those archives, and
`install.sh` now downloads and verifies against it before extracting
anything — so that file has to be attached to the release alongside the
archives, not just built alongside them.

```bash
git tag forsight-v1.0.0
git push --tags
```

A stuck or partially-failed run can be re-driven without pushing a new tag —
dispatch the workflow with the existing tag as input (`gh workflow run
forsight-release.yml -f tag=forsight-v1.0.0`, or from the Actions UI); it
re-runs the build and uploads over the existing release's assets.

`install.sh` expects that exact tag prefix (`forsight-vX.Y.Z`, distinct from
the npm package's own `vX.Y.Z` tags — this repo now ships two independently
versioned artifacts) and asset naming (`forsight_<os>_<arch>.tar.gz`).

**Local/fallback path.** The workflow runs nothing that isn't available by
hand — to cut a release without pushing a tag (e.g. the workflow itself is
broken), do exactly what it does:

```bash
make release VERSION=v1.0.0
gh release create forsight-v1.0.0 dist/*.tar.gz dist/sha256sums.txt --title "forsight v1.0.0"
```

`.github/workflows/forsight-release.yml`'s "Create or update the GitHub
Release" step attaches `dist/sha256sums.txt` alongside the archives (since
#115), so a release cut through the workflow verifies the same way the
local path above does. That same workflow also builds `forsight/Dockerfile`
from the repo root and pushes `ghcr.io/fors-corp/forsight`, tagged with the
release version and as `latest` — the image `deploy/k8s/daemonset.yaml`
pulls. (No image exists at that path until the repo's `ghcr.io` rename PR
merges and the next `forsight-vX.Y.Z` tag is cut; until then the last image
published is `ghcr.io/marcfs31/forsight`.)

## Scope: what's real vs. what's roadmap

**Built and verified working:** everything listed at the top of this file —
host and process metrics, Docker container metrics, OTLP metrics+traces+logs
ingestion, Kubernetes via the DaemonSet manifest, embedded in-memory storage,
persistent Badger-backed storage (`--store badger`), a real dashboard, and
Forseer statistical detectors. Verified by hand: ran the binary, watched real
host and Docker metrics flow through `/api/v1/metrics`, sent a real OTLP
protobuf payload and queried it back out, opened the dashboard in a browser
to confirm the chart, stat tiles, and container table render live data, and
— for the Badger store — ran with `--store badger --data-dir <dir>`, sent a
real OTLP metrics payload and let the host collector tick, queried the data
back out, killed the process, restarted it against the same `--data-dir`,
and confirmed every metric written before the restart (the OTLP payload and
every prior host-collector tick) was still there — not just that the code
compiles.
Also **automated releases**: `.github/workflows/forsight-release.yml` cuts
and publishes a GitHub Release on every `forsight-vX.Y.Z` tag push — see
Releasing above. Verified by tracing it step-for-step against
`forsight/Makefile`'s `release` target (same cross-compile targets, same
`forsight_<os>_<arch>.tar.gz` naming) and by running that target's
cross-compile+archive loop locally for all four `RELEASE_TARGETS`, including
extracting and running the resulting binary. `dist/sha256sums.txt` and
`install.sh`'s verification of it against a real release are covered by
`install_test.bats` (`make test-install`); the release workflow itself
attaches that file too, since #115.

**Deliberately not built yet, flagged rather than silently skipped:**

- **A real query language.** The API takes simple time-range + exact-label
  filters, not anything PromQL-equivalent.
