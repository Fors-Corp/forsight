# Changelog

All notable changes to the `forsight` agent (the Go binary in this
directory, embedding `forseer/` and the dashboard) are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This changelog tracks `forsight-vX.Y.Z` release tags — a separate version
line from `@marcfs31/forsight`'s own `vX.Y.Z` tags for the design system;
see `forsight/README.md`'s Releasing section for how the two relate and how
this file fits into cutting a release.

## [Unreleased]

## [1.2.1] - 2026-10-10

Fixes the probe collector's socket leak, and releases the fixes merged
since 1.2.0.

### Fixed

- The probe collector no longer leaks a socket per probe. It built a new
  HTTP transport for every request, and each one kept its connection open
  in an idle pool that never expired, so `--probe` held one ESTABLISHED
  socket per `--collect-interval`. After 3.5 days on 2026-10-10 that was
  16,314 sockets to one target, every ephemeral port on a macOS host, and
  every other program on the machine failed to connect. Probes now share
  one client that opens a fresh connection per probe and closes it, so
  `probe.tls.*` still reports the certificate served now. A handshake that
  fails outright no longer reports `probe.tls.*` for that tick
- The mlaas client has a transport of its own with a per-host connection
  cap, instead of sharing `http.DefaultTransport`
- `process.fd.count` always includes forsight's own process, which an idle
  agent never ranked into the busiest 40, and forsight logs one warning
  when its own descriptor count passes 4096 (again only after recovering)
- A shutdown that runs out of its time budget is no longer reported as a
  failed stop (#218)
- The agent waits for its collectors before closing the store, and the
  store's locks are split (#167)
- Every remaining unbounded read on the ingest paths is bounded (#162);
  non-finite values are rejected, and a 200 that carries no body is no
  longer accepted (#149); the statsd accumulator is bounded and `Collect`
  no longer receives its live map (#143)
- `/readyz` tells an anonymous caller only what it is for (#163)
- Log clustering: a token-length leak is closed, an old log line no longer
  rewinds a cluster, and the severity fallback moved out of the file-log
  collector (#166)
- forseer detectors: CUSUM runs on a zero-mean statistic (#139); real
  series are capped and dead ones evicted, instead of capping hour buckets
  (#150); host-outlier variance is floored per input (#148); a p99 needs
  more than twelve samples (#153); the burn projection is compared in
  seconds before it can overflow (#151); points are scored against the
  estimate that excludes them, and threshold readiness means converged
  (#170); the trend interval is sized from Holt's actual errors (#168)
- mlaas: only the first key is read from the API key file (#138), and the
  version mlaas reports on `/healthz` is recorded and logged (#141)
- Dashboard: it survives a render error and no longer blames the user for
  an outage (#178); the heading outline is fixed, the mlaas status is
  announced, and the dashboard runs under axe in its tests (#183);
  hash-route navigation sets the page title and moves focus (#152)

### Changed

- The Go module path is `github.com/Fors-Corp/forsight/forsight`, following
  the repository's move to the Fors-Corp organisation (#181). Nothing
  imports it as a library
- The dashboard builds against `@marcfs31/forsight` 4.2.0 (#189, #192), and
  Go and dashboard dependencies are updated (#206 and others)

## [1.2.0] - 2026-09-17

Probes, a lighter dashboard, and the last of the roadmap follow-ups.

### Added

- An HTTP and TLS-expiry probe collector: `--probe [name=]<url>` (repeatable)
  GETs a URL on `--collect-interval` and reports `probe.http.up`, `.status`
  and `.duration_ms`, plus `probe.tls.days_remaining` and `.valid` from the
  leaf certificate of an `https://` target, read independently so a
  certificate nearing expiry never makes a reachable site look down (#120)
- The Overview page shows an uptime strip per probe target over the
  selected range, with the certificate's expiry as a badge, only once a
  target is configured (#131)
- A light/dark theme toggle in the dashboard's sidebar footer, remembered
  per browser and applied before first paint (#130)
- Forecasts on the Models page are drawn as a dashed projection of the
  observed series, and the chart's description names the minute the
  projection starts; the dashboard builds on design system 4.1.0 (#126)
- `GET /api/v1/metrics?per_name=<n>` keeps the newest N points of every
  metric name, the bound an unscoped read needs; on the Badger store a
  ranged unscoped read now costs the window rather than the history (#128)
- The dashboard reads a two-minute latest window plus per-name ranged
  history for its charts and probe strips, instead of the whole retained
  window every five seconds (#133)
- Deterministic incident grouping on the Timeline: insights within five
  minutes of each other that share a Related value or a Source fold into
  one incident event (#118)
- An end-to-end boot test builds the real binary and boots it against
  `--store memory`, asserting the embedded dashboard, the metrics API,
  `/healthz` and a clean SIGINT shutdown (#117)
- This changelog, and a self-generating `make help` (#119)

### Changed

- The dashboard builds on Vite 8, plugin-react 6, Vitest 5 and TypeScript 7
  (#111, #114, #112)
- The embedded dashboard is documented as the checked-in snapshot it is,
  not a fallback page (#124)

### Fixed

- Restoring an oversized paging snapshot keeps the most recently trained
  clusters, the same ones the live model's eviction would keep, instead of
  an arbitrary subset; every persisted model's restore-time bound is now
  tested (#129)
- The dashboard's access-token dialog no longer flips back to its first-load
  wording after a rejected token: a 401 now counts as evidence only about the
  token that request carried, so the token-less poll that follows a rejection
  (or a stale one that lands after a new token is entered) leaves the prompt
  and the stored token alone (#132)

## [1.1.0] - 2026-09-16

The first release cut end to end by `.github/workflows/forsight-release.yml`:
archives, `sha256sums.txt` and the `ghcr.io/marcfs31/forsight` image.

### Added

- Forseer's models now persist what they've learned to
  `<data-dir>/forseer.json` and restore it on restart, so an agent that
  restarts often doesn't retrain from cold every time (roadmap item 25)
  (#110)
- Added a stdlib multivariate host outlier check — an online
  mean/covariance Mahalanobis distance across CPU, memory, disk and network
  — replacing the planned Isolation Forest row (#107)
- mlaas model drift is now shown as a Badge against its retrain threshold,
  with an explicit "not enough data yet" state instead of a flattering zero
  (#103)
- `forsight` ships a `Dockerfile` and working liveness/readiness probes for
  the Kubernetes DaemonSet (#102)
- Forseer's slow-span detector now tracks P2 quantiles (median and p99) per
  span instead of a rolling z-score, so long-tailed latency regressions are
  caught correctly (#100)
- Added a `forsight demo` subcommand that runs a deterministic synthetic
  stream so the dashboard is never empty (#99)
- The agent listener supports opt-in TLS (`--tls-cert`/`--tls-key`, with
  `FORSIGHT_TLS_CERT`/`FORSIGHT_TLS_KEY` fallbacks) and mutual TLS via
  `--tls-client-ca` (#97)
- Added file-descriptor (`process.fd.count`, `host.fd.used`/`host.fd.max`)
  and connection-state (`host.net.conn_count{state=...}`) metrics (#96)
- The Overview's memory and disk stat cards now get the same sparkline
  history and chart selection CPU already had (#98)
- Every store query now accepts `Before` and `Limit`, walking newest-first,
  and the API handlers and dashboard use it to bound reads (#94)
- Added a time range control (15m/1h/6h/24h/7d) to the Overview, offered
  only as far as the store's retention actually reaches (#92)
- Forseer's `pagingModel` decides whether a log burst is worth paging for,
  via online logistic regression graded against the existing volume rule
  (#91)
- Added a shared `usePoll` hook and a real connection state
  (waiting/live/stale) on the Overview (#88)
- The agent can train models in mlaas, and the dashboard gained a Models
  page with forecasts, drift, champion/holdout, and manual
  classify/train/tune (#83)
- Forseer's error budget now includes a Holt's-linear-method forecast for
  when it will be exhausted, not just how much has been spent (#74)

### Changed

- The release workflow now attaches `dist/sha256sums.txt`, builds against
  the verified lockfile, and publishes the `forsight` image to `ghcr.io`
  (#115)
- The dashboard now builds against `@marcfs31/forsight` 4.0.1 instead of
  3.0.0, cutting the embedded JS bundle by 27% (#86)

### Fixed

- The release workflow's image build no longer copies a `forseer/go.sum` that
  does not exist, and a tag with a suffix (`-rc1`) is created as a
  pre-release so it is never marked Latest, which is what `install.sh`
  installs by default (#116)
- `build-web` now sources its own `npm.pkg.github.com` credential from
  `NODE_AUTH_TOKEN` or `gh auth token` instead of needing a manual `file:`
  link workaround (#106)
- The dashboard no longer locks itself out of its own shell when
  `--auth-token` is set; it now prompts for and supplies its own bearer
  token (#105)
- `install.sh` now verifies its download's checksum before extracting, and
  no longer resets configured flags (`--store`, `--auth-token`,
  `--mlaas-url`) back to defaults on every upgrade (#101)
- Culprit ranking now sorts processes by change from their own baseline
  instead of raw CPU, so a process that idles high all the time no longer
  outranks the one that actually spiked (#104)
- The agent's `http.Server` now sets read/write/idle timeouts and a max
  header size, closing a slow-client resource leak (#85)

### Security

- Values sourced from outside the agent (mlaas error text, model/dataset
  names from the API, the access log's request line) are now sanitized to a
  single line before being logged, closing 14 CodeQL go/log-injection
  alerts (#93)
- Bumped `forsight/web`'s `vitest` to 4.1.11, fixing `@vitest/mocker`'s
  path-traversal advisory (GHSA-82fw-gwwq-j7x9) (#76)

## [1.0.0] - 2026-09-11

First production release. From the `forsight-v1.0.0` tag's own release
notes:

### Added

- A self-contained Go observability agent with the dashboard embedded in
  one binary
- Collection: host, process, Docker containers, Prometheus scrape (with
  local auto-discovery), StatsD/DogStatsD, OTLP metrics + traces + logs,
  log-file tailing
- Storage: in-memory (default) or a Badger-backed persistent store
  (`--store badger`)
- Forseer: online anomaly/changepoint detection, Drain-lite log templating,
  error-log SLO budgets, incident timeline stitching, trace critical-path
  analysis, natural-language query, and the first trained model on top of
  the statistical baseline
- Optional bearer-token auth and bounded ingest against hostile OTLP
  payloads
- Ships as a Kubernetes DaemonSet or a standalone binary via `install.sh`
