# Roadmap

Twenty-five items, in the order they are worth doing. Every one names the
file and line it starts from, what gets built, and what it waits on.

How this list was made: sixty-five proposals were drafted against the tree,
each scored by three judges who checked the evidence themselves, and the
composite is the weighted judge average. This document does not take the top
twenty-five by score. It balances the areas, drops what breaks a rule, merges
proposals that are one piece of work, and orders by value against effort with
dependencies respected. A second pass then re-read every span the first draft
cited. Three mechanisms were wrong as written and are corrected below —
Badger's scan walks oldest-first, not newest; mlaas's `drift_max` is never
absent on the wire, so a nullable copy of it would never be null; and the
`make build-web` workaround the tree still documents is for a credential gap
that has closed — and three gaps the draft had missed are in: the dashboard's
design-system pin is a major behind, the agent's own README contradicts the
tree, and `forseer/README.md`'s Next table has one row nobody had decided on.
Where a proposal's evidence turned out to be stale or wrong — the DaemonSet's
`hostPID` claim, a `test:coverage` script that does not exist, a forecast band
with nothing to draw, a tree-shaking gate CI already runs — the item is
reframed or left out, and the reason is written down at the bottom.

Every item keeps the rules in [`forseer/MODELS.md`](forseer/MODELS.md): no
model weights and no API keys in this repo; `forseer` stays stdlib-only;
every model bounds its state, names a fallback and gates on readiness; every
model lands on a component the design system already has; the agent stays one
static binary. Anything that would edit `.github/workflows/**`, `CLAUDE.md` or
branch protection is proposed for Marc to apply, never done under the
automation mandate.

Phases: **this session** is the mlaas integration and the Models page that
this branch is for. **Next** is what follows, in order. **Later** is either
large, waiting on a design-system release, or half-owned by a workflow
change. Effort: S is a day or less, M is a few days, L is a week or more.

## This session

### 1. mlaas integration: export the stream, train there, proxy predictions, feed outcomes back

- **Area** mlaas · **Effort** L · **Score** fixed by the brief, not judged · **Shipped** in #83 (2026-09-15)

**Why.** Forseer's models are stdlib-only, online and bounded, which is why
they gate on a fallback and score prequentially — and why they can never have
a real holdout, a retrain schedule, or a corpus that grows
(`forseer/MODELS.md`, \"Why trained here, not shipped trained\"). mlaas is a
separate service that does exactly those things: train, tune, serve, retrain a
challenger and promote it only if it beats the champion on the same holdout
(mlaas `README.md`; the route table at mlaas `internal/api/server.go:94-128`,
`X-API-Key` at `:180`, `/healthz` exempt from auth at `:132-152`). Before this
branch the agent had no way to hand it anything, and the dashboard was one
flat page.

**What.** `forsight/internal/mlaas` — `client.go`, `export.go`, `managed.go`,
`sync.go`, `types.go` and their tests, eight files on the branch. Opt-in via
`--mlaas-url`; the key from `MLAAS_API_KEY` or `--mlaas-api-key-file`, never a
flag value (`forsight/cmd/run.go:93-102`, `resolveMlaas` at `:353-383`).
`Syncer.pass` (`sync.go:185-366`) probes `GET /healthz`, exports one-minute
buckets of `host.{cpu,memory,disk}.percent` and declared-severity log lines as
CSV (`POST /datasets`), creates and trains the four managed models
(`POST /models`, `POST /models/{name}/train`), asks each forecast champion for
the next hour and reconciles it against what then arrived, and posts a sample
of freshly-declared lines back as feedback (`POST /feedback`). The agent
proxies `/api/v1/mlaas/status`, `…/train`, `…/tune` and `…/predict`
(`forsight/internal/api/server.go:64-67`, `api/mlaas.go`) so the key never
reaches the browser. The dashboard gains a hash router (`route.ts`, `App.tsx`)
and `pages/Models.tsx`: `MlaasModelsCard` (`:275-432`, Train and Tune),
`ForecastsCard` (`:434-472`), `PredictionsCard` (`:482-533`), `TryModelsCard`
(`:548-663`), `JobsCard` (`:665-713`). Unreachable is a StatusDot outage that
carries the error and keeps the last-known state (`Models.tsx:119-125`,
`sync_test.go:833`). To close the session: CI green on the branch, the
refreshed embed (`webdist/assets/index-D0AG6eS8.js` is untracked and the old
bundle deleted), and the docs, already written in
`forsight/README.md:121-199` and `forseer/MODELS.md` \"Served by mlaas\".

**Where.** `forsight/internal/mlaas/`, `forsight/internal/api/mlaas.go`,
`forsight/internal/api/server.go`, `forsight/cmd/run.go`,
`forsight/web/src/pages/Models.tsx`, `forsight/web/src/route.ts`,
`forsight/web/src/App.tsx`, `forsight/web/src/api.ts`,
`forsight/internal/api/webdist/`, `forsight/README.md`, `forseer/MODELS.md`.

**Depends on.** Nothing.

### 2. Forseer's model cards on the same Models page

- **Area** dashboard · **Effort** M · **Score** fixed by the brief, not judged · **Shipped** in #83 (2026-09-15)

**Why.** `forseer/MODELS.md`'s Next list carried \"a models view in the
dashboard\": `GET /api/v1/forseer/models` served a card per model
(`server.go:61`) but nothing rendered it, so \"gated on a fallback\" and
\"measured prequentially\" were claims an operator could not check. Putting the
in-binary cards next to the mlaas cards is what makes `log-severity` a
comparison rather than a replacement (`forseer/README.md:36`, \"Two kinds of
model\").

**What.** `ForseerModelsCard` (`Models.tsx:190-267`): a **Table** row per
card — job, Reads, Ready, Trained, and Score as **Progress** against the named
fallback, the fallback printed as text, never colour alone;
`useForseerModels` (`api.ts:526`). `TryModelsCard` sends one line through
`GET /api/v1/forseer/classify` (`server.go:62`, `api/mlaas.go:160-182`) and
through mlaas's `log-severity`, so the two answers sit side by side. Lands on
Table, Card and Progress; no new widget.

**Still to do before this ships.** `Models.test.tsx` has seven cases (`:143`
not configured, `:156` connected, `:184` train, `:204` train refused, `:221`
forecast chart, `:258` prediction disagree, `:286` try both), and the only
`reachable: false` (`:45`) is inside the `notConfigured` fixture. Nothing
asserts the `Unreachable: <error>` StatusDot label for a configured-but-down
mlaas with the last-known rows kept (`Models.tsx:121-123`), and nothing
asserts the \"No Forseer models yet\" EmptyState (`Models.tsx:198-201`). Both
cases go in with item 1.

**Where.** `forsight/web/src/pages/Models.tsx`,
`forsight/web/src/pages/Models.test.tsx`, `forsight/web/src/api.ts`,
`forsight/internal/api/mlaas.go`, `forseer/MODELS.md`.

**Depends on.** Item 1.

## Next

### 3. Timeouts on the agent's http.Server

- **Area** agent · **Effort** S · **Score** 4.47 · **Shipped** 2026-09-16, `newHTTPServer` in `forsight/cmd/run.go`

**Why.** `forsight/cmd/run.go:219` builds `&http.Server{Addr, Handler}` and
nothing else — no `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`,
`IdleTimeout` or `MaxHeaderBytes`. A client that opens a connection and never
finishes its headers holds a goroutine for as long as it likes. The OTLP
receiver caps bodies at 32 MiB but nothing caps time. The statsd receiver
(`statsd.go:105-112`) reads 65535-byte packets with no per-source guard, a
smaller cousin of the same gap.

**What.** Set the five fields in `run.go`, sized so a full 32 MiB OTLP body on
a slow link still fits: `ReadHeaderTimeout` around 10s, `IdleTimeout` around
120s, read and write timeouts in minutes, `MaxHeaderBytes` 1 MiB; a
`run_test.go` case asserting they are set. Leave statsd per-source rate for a
later, separate change.

**Where.** `forsight/cmd/run.go`, `forsight/cmd/run_test.go`.

**Depends on.** Nothing.

### 4. The dashboard's design-system pin catches up to 4.0.1, and Dependabot gets the credential to keep it there

- **Area** design system · **Effort** S · **Score** not in the judged pool; placed by the verification pass · Marc supplies the secret · **Shipped** 2026-09-16 (pin `^4.0.1` and the embed in #86, the chart-path smoke check in #87); the Dependabot secret was set on 2026-09-16, so nothing is open

**Why.** This is `CLAUDE.md`'s stated end state, and the first draft did not
carry it. Root `package.json:3` is `4.0.1` and tags run to `v4.0.1`, but
`forsight/web/package.json:13` pins `^3.0.0` and `package-lock.json:1101`
resolves `3.0.0` — a caret never crosses a major, so nothing moves it.
`gh api repos/marcfs31/forsight/dependabot/secrets` returns `total_count: 0`,
so the `registries:` block merged in #77 is inert and Dependabot cannot see
the package at all (the memory note of 2026-09-13: still blocking, needs
Marc). What that costs: the PURE-annotation tree-shaking landed in 4.0.0
(`CHANGELOG.md`; the 4.0.1 entry names it), so the checked-in embed —
`webdist/assets/index-D0AG6eS8.js`, 562 KB of raw JS from three barrel
imports in `forsight/web/src` — is built from a pre-tree-shaking release;
4.0.0's `aria-dialog-name` fix for FilterBar (imported at `Overview.tsx:24`)
never reaches the dashboard either; and item 23's \"the dashboard picks it up
on the next pin bump\" assumed a mechanism that is dead.

**What.** Bump the pin to `^4.0.1`, `make build-web`, commit `webdist/` on
the same PR. 4.0.0's one breaking change is the Tailwind v4 compile, and
`forsight/web/src/index.css` already imports `tailwindcss`,
`@marcfs31/forsight/tailwind.css` and `styles.css` with `tailwindcss ^4` in
devDependencies, so expect a small stylesheet diff — read it anyway. Measure
the bundle before and after; the audit's 87% is the ceiling, not a promise.
Then the half of design-system-10 that was right, as a small root-package PR:
`scripts/smoke-test.mjs:243-286` already bundles `import { Button }` and
fails if it is not far below the barrel; add the same check for what the
dashboard draws — `{ ChartFrame, LineChart, BarChart, ComboChart, Sparkline,
StatCard, StatusDot, Table }` — asserting no Radix identifier leaks, so the
chart path stays as light as the win depends on. For Marc, web-UI only: a
fine-grained PAT with `read:packages` stored as the Dependabot secret
`DEPENDABOT_NPM_PACKAGES_TOKEN`; from then on each design-system release
arrives as an ordinary Dependabot PR that rebuilds the embed. Land after item
1 merges so the two `webdist/` refreshes do not collide.

**Where.** `forsight/web/package.json`, `forsight/web/package-lock.json`,
`forsight/internal/api/webdist/`, `scripts/smoke-test.mjs`, the Dependabot
secret (Marc).

**Depends on.** Item 1.

### 5. One `usePoll` helper with a real connection state

- **Area** dashboard · **Effort** M · **Score** 4.12 · merges dashboard-ux-9, dashboard-ux-10 · **Shipped** 2026-09-16, `usePoll` and `connectionState` in `forsight/web/src/api.ts`

**Why.** `Overview.tsx:286` computes
`connected = metrics.length > 0 || logs.length > 0`, and every hook in
`api.ts` keeps the last snapshot in its `catch` block (`:35`, `:65`, `:176`,
`:204`, `:243`, `:288`, `:324`, `:379`, `:538`, `:577`) with no error exposed.
Once any fetch has succeeded, the StatusDot says operational for the rest of
the session, agent dead or not. The eight hooks (`Overview.tsx:233-240`) plus
the Models page's are copy-paste: unsynchronised timers, no `AbortController`,
no pause while the tab is hidden. Three small gaps in the same file ride
along: the header (`:291-294`) has no wrap rule while the stat grid and the
query form both stack at narrow widths; the Containers **Table**
(`:460-465`) lacks the `w-full overflow-x-auto` wrapper Processes has
(`:425`); and the only `aria-live` in the file is the error-log count
(`:509`), so a status flip to outage is never read out.

**What.** `usePoll<T>(path, intervalMs)` in `api.ts` returning
`{data, lastSuccessAt, lastErrorAt}`, with an `AbortController` and a
`document.hidden` pause; each existing hook becomes a one-liner over it.
`connected` becomes three states derived from the newest `lastSuccessAt` —
waiting for first data, receiving, stale after three missed intervals —
surfaced through StatusDot's label, pulse only while live, the render wrapped
in `role=\"status\" aria-live=\"polite\"` with a screen-reader spot check that
it does not double-announce against AlertList's own `announce` prop.
`flex-col sm:flex-row` on the header and the overflow wrapper around
Containers in the same PR. No server change; a combined endpoint is not
needed.

**Where.** `forsight/web/src/api.ts`, `forsight/web/src/pages/Overview.tsx`,
`forsight/web/src/pages/Models.tsx`, `forsight/web/src/App.test.tsx`, the
refreshed `webdist/`.

**Depends on.** Nothing.

### 6. Make CONTRIBUTING.md and the agent's own docs agree with the tree

- **Area** quality · **Effort** S · **Score** 4.07 · merges design-system-7, design-system-9 · **Shipped** 2026-09-16 (the route table had already caught up in #83; Toast passed `axe()` in about a second, so it got the test, not an exemption)

**Why.** `CONTRIBUTING.md`'s sections — Setup, Before opening a PR, Testing
policy, Regression policy, Adding a component, Versioning — are all design
system; there is no section for `forsight/` or `forseer/`, so the Go gate and
the model contract live only in `CLAUDE.md` and `MODELS.md`. Line 130 says
\"Never a second y-axis\" with no exception while `ComboChart.tsx:94-107`
computes an independent bar and line scale on purpose. Line 135 names the
`axe()` exemption as Dialog, DropdownMenu, Popover, Tooltip and Select, but
`Drawer.test.tsx:16` exempts itself by \"same rationale as Dialog\" and
`Toast.test.tsx` has neither an `axe()` call nor a reason.

The agent side is worse, and it is what an operator reads first.
`forsight/README.md:331-341` \"Deliberately not built yet\" lists log-file
tailing and seasonal baselines plus the error-budget forecast as not built,
while `--log-file` ships (`cmd/run.go:82`,
`internal/collector/filelog/tail.go`, `README.md:94` and `:240`) and
`forseer/README.md:86-104` says both Forseer features are built. The HTTP
surface table at `forsight/README.md:201-218` omits
`GET /api/v1/forseer/models`, `GET /api/v1/forseer/classify` and all four
`/api/v1/mlaas/*` routes `server.go:61-67` mounts (`forseer/README.md:17-30`
has them). `forsight/Makefile:56` still says \"(No automated release workflow
yet — see forsight/README.md's roadmap note.)\" while `forsight-release.yml`
exists and `README.md:277-305` documents it.

**What.** One docs PR. A \"Contributing to the agent (`forsight/`,
`forseer/`)\" section that points at the Go gate (vet, golangci-lint, `go test`
in both modules; `make build-web` plus a clean `webdist/` when `forsight/web`
changes) and at `MODELS.md`'s contract, without restating them. One sentence
naming ComboChart the sole guarded exception with its three guardrails: bar
axis zero-based, line axis its own range, the mapping stated in text. The
exemption list extended to the overlays that actually claim it, and an
`axe()` call on an enqueued Toast in `Toast.test.tsx` — if it times out under
jsdom, that is the finding, and the list grows by one with a reason. On the
agent side: delete the two stale \"not built\" bullets, add the eight missing
routes to the surface table, fix the Makefile comment. Still S: prose only.

**Where.** `CONTRIBUTING.md`, `src/components/Toast.test.tsx`,
`forsight/README.md`, `forsight/Makefile`.

**Depends on.** Nothing.

### 7. CodeQL scans the Go, then the results check becomes required

- **Area** ops · **Effort** S · **Score** 4.07 · Marc applies

**Why.** `.github/workflows/codeql.yml:42-46` analyses `actions` and
`javascript-typescript` only. The Go under `forsight/` parses untrusted OTLP
wire data and terminates network connections — the code most worth scanning —
and is never analysed. Separately, the two required \"Analyze (…)\" checks prove
a scan ran, not that it was clean; \"Code scanning results / CodeQL\" is not
required (`CLAUDE.md`, \"The gap that remains\").

**What.** Proposed, for Marc to apply — workflows are outside the mandate's
scope. A `language: go, build-mode: autobuild` matrix entry. Budget a first
batch of Go findings, fixed at source and never suppressed. Once the Go
analysis is stable, add \"Code scanning results / CodeQL\" to
`required_status_checks`, after checking it does not deadlock PRs whose checks
re-run by dispatch (it reports only from `pull_request` analyses).

**Where.** `.github/workflows/codeql.yml`, branch protection on `main`.

**Depends on.** Nothing.

### 8. Is this log burst worth paging

- **Area** forseer · **Effort** M · **Score** 4.00 · **Shipped** 2026-09-16, `pagingModel` in `forseer/paging.go`

**Why.** `forseer/logs.go:105` opens `log_burst` on
`recent >= burstMinCount && (previous == 0 || recent >= previous*burstRatio)`;
severity bumps to critical only when `ErrorCount > 0` or `recent >= 16`
(`:107`). A debug template tripling and an exception template tripling are the
same insight. `MODELS.md` Next #1, and not one of the four mlaas-managed
models (`forsight/README.md:137-148`), so it is open either way.

**What.** `pagingModel` in `forseer`: online logistic regression over three
declared per-cluster features — severity mix (`ErrorCount/Count`), burst shape
(`recent/previous`), and whether a critical insight from another source opened
within `burstWindow`. The label is self-supervised and graded before training,
predict-then-train as `severity.go` does: did a critical insight elsewhere
follow within N minutes — excluding the burst's own escalation, or the model
learns its own labelling back. Fallback: the volume rule. State: one weight
vector per cluster, capped by `maxClusters=256` (`logs.go:13`). A Card in
`Engine.Models()`; the score becomes the bar on the clusters **BarList**. Do
not route this through mlaas: a network call on the log ingest path is the
wrong place for it.

**Where.** `forseer/logs.go`, `forseer/learn.go`, `forseer/engine.go`,
`forseer/MODELS.md`, `forsight/web/src/pages/Overview.tsx`.

**Depends on.** Nothing.

### 9. Time range on the Overview, via the unused TimeRange

- **Area** dashboard · **Effort** S · **Score** 3.98 · **Shipped** 2026-09-16, `offeredTimeRanges` in `forsight/web/src/pages/Overview.tsx`

**Why.** **TimeRange** is exported by the design system and never imported in
`forsight/web`. `useMetrics`/`useLogs` return the whole retention window on
every poll and the Host CPU **LineChart** (`Overview.tsx:326-336`) plots all of
`cpuHistory`. With `--store badger --retention 168h` — what
`forsight/README.md:189-192` tells operators to run for the forecasts — that is
a week of ten-second points in one chart.

**What.** A TimeRange in the Overview header bound to a `rangeMs` state;
`cpuHistory`, `filteredLogs` and the heatmap filtered by
`timestamp >= now - rangeMs` before charting. Client-side only; the data is
already in memory. Cap the longest option at what the store actually holds —
the oldest point in the current metrics is the proxy until the API reports
retention.

**Where.** `forsight/web/src/pages/Overview.tsx`,
`forsight/web/src/App.test.tsx`, the refreshed `webdist/`.

**Depends on.** Nothing.

### 10. Limit and Before on every store query, walking newest-first

- **Area** agent · **Effort** M · **Score** 3.93 · corrected · **Shipped** 2026-09-16, `scanNewestFirst` in `forsight/internal/store/badger.go`; metrics reads stay uncapped until there is a per-name history read

**Why.** `forsight/internal/store/store.go:19-37`: `MetricQuery`, `SpanQuery`
and `LogQuery` carry Name, Labels and Since and nothing that bounds the
result. The handlers write the whole slice; the dashboard fetches full history
every five seconds (`api.ts:31`, `:61`, `:239`); the mlaas exporter reads the
entire log table each pass (`sync.go`, `loadLogs`). At 168h retention this
decides whether the dashboard stays usable.

The first draft said Badger's iterator already walks newest-first. It does
not: `badger.go:248`, `:297` and `:346` take `badger.DefaultIteratorOptions`
— forward — with `it.Seek(seek)` and no `Reverse`, and the comment at `:34`
says the big-endian timestamp encoding makes byte order match chronological
order, so the walk is oldest-first; `memory.go:74` ranges the slice in append
order, also oldest-first. \"Stop at Limit\" on either returns the oldest N
points, the wrong end for a dashboard.

**What.** `Limit int` and `Before time.Time` on the three query types;
`?limit=` and `?before=` in `handlers.go`. Badger: `iopts.Reverse = true` and
a `Seek` at the prefix's upper bound — the prefix plus Before's big-endian
timestamp when set, the prefix plus `0xFF` bytes otherwise — walking
newest-first while the key's timestamp is at or after Since and stopping at
Limit, in all three query paths. MemoryStore: walk the slice from the end.
Both hand the window back oldest-first, as every consumer already expects, so
nothing above the store changes shape. `store_conformance_test.go` gains
cases for Limit returning the newest N, for Before, and for their interaction
with Since, before either backend is trusted; `api.ts` passes a limit sized to
what it draws. Prerequisite for probes, rollups and anything cross-host.

**Where.** `forsight/internal/store/store.go`, `memory.go`, `badger.go`,
`store_conformance_test.go`, `forsight/internal/api/handlers.go`,
`forsight/web/src/api.ts`.

**Depends on.** Nothing.

### 11. Memory and disk get the chart CPU has

- **Area** dashboard · **Effort** S · **Score** 3.90 · **Shipped** 2026-09-16, `Overview.tsx`'s `CHART_METRICS` radio group

**Why.** `Overview.tsx:315-322` renders three StatCards; only CPU is charted
(`:326-336`). Memory growth and disk fill are the two trends an operator asks
about first, and `historyFor(metrics, name)` already works for any name.

**What.** A **Sparkline** in each StatCard (its default height of 36 is the
StatCard size) and a click that swaps the LineChart's series to that metric,
reusing `historyFor` and `formatPercent` and the range from item 9. No new API
call.

**Where.** `forsight/web/src/pages/Overview.tsx`,
`forsight/web/src/App.test.tsx`.

**Depends on.** Item 9.

### 12. Per-endpoint latency shape: a P2 quantile instead of a z-score

- **Area** forseer · **Effort** M · **Score** 3.87 · corrected · **Shipped**
  2026-09-16, `forseer/p2.go` (`p2Estimator`), `spanWatch` in `forseer/spans.go`

**Why.** `forseer/spans.go:71-99` keeps Welford mean and variance per
`(service, span name)` and opens `slow_span` on
`z = (dur - mean)/sigma >= warningSigma`. Latency is long-tailed: the mean
sits above the median and sigma is set by the tail, so \"three sigma slow\" is
wrong for exactly the endpoints that matter. `MODELS.md` Next #2.

**What.** Replace `rolling` in `spanWatch` with a P2 estimator — five markers
per series, O(1) memory, `math` only — tracking p50 and p99; \"slower than this
endpoint's own p99\" replaces N sigma. Run the CUSUM the Detector uses
(`cusumK`/`cusumH`, `detector.go:18-19`) on the span series' own z inside
`spanWatch.observeOneLocked` and reset the estimator when it fires, so it
follows a regime change instead of drifting for a lifetime — not by wiring to
the Detector: spans never reach it (`engine.go:107-109` calls only
`e.spans.Observe`), and the `rolling` struct `spanWatch` shares with it
(`detector.go:36-41`) has a `cusum` field `spans.go:71-99` never updates. An
alert budget in the `thresholds.go` sense, since p99 fires on one span in a
hundred by construction: open on a run of exceedances, not one. Fallback:
today's z-score. State: five markers plus counts per series under
`maxSpanSeries`. No label exists, so the Card gates on marker stability past
`minSamples` and reports `Unmeasured` rather than an invented accuracy. The
five markers map onto `BoxPlotBox{min,q1,median,q3,max}` for a later
per-endpoint spread view. Lands on **TraceWaterfall**.

**Where.** `forseer/spans.go`, `forseer/engine.go`, `forseer/MODELS.md`.

**Depends on.** Nothing.

### 13. Multivariate outlier over the host vector, stdlib, and a decision on the README's last Next row

- **Area** forseer · **Effort** M · **Score** 2.72 · replaces the Isolation Forest row

**Why.** `forseer/README.md:96-100`, \"Next (stay in this folder)\", has
exactly one row: Multivariate outlier, Isolation Forest in `python/`, scores
POSTed back, landing on AlertList. This roadmap covers every `MODELS.md` Next
row (items 8, 12, 25) and had nothing to say about this one, and it needs a
decision because as written it is blocked twice: mlaas's sklearn plugin offers
forests, boosting, kNN, SVMs and linear models (mlaas
`plugins/sklearn/plugin.py:30-45`) and has no IsolationForest anywhere, and
the agent has no ingest route for externally computed insights —
`server.go:49-67` is GET-only under `/api/v1/forseer`. The judges' doubt
stands: three host percentages carry little joint signal beyond per-series z.
The row still cannot sit there promising a method with no producer and no
consumer.

**What.** A stdlib `hostOutlier` model: an online mean vector and covariance
(Welford's multivariate form) over five inputs — `host.cpu.percent`,
`host.memory.percent`, `host.disk.percent` and the per-interval deltas of
`host.net.bytes_sent` and `host.net.bytes_recv` (`host.go:46-74`; the net
series are cumulative counters, so the delta is the signal) — scored by
Mahalanobis distance. One mean and one 5x5 covariance per host: bounded by
construction, outside the 512-series cap. Gated at fifty samples, stricter
than `minSamples=12`, because a covariance that size needs it; fallback is
today's per-series z in the Detector; the Card in `Engine.Models()` declares
Reads, reports `Unmeasured` — there is no label — and carries a Detail line
counting outlier insights opened while no per-series insight was open on any
of the five, which is the exit criterion: if that count stays zero over a
watched period, retire the model. Lands on **AlertList** in
`Insight.Severity`'s vocabulary, Related naming the largest-contributing input
the way culprit does. Rewrite the README row either way. Ordered here because
it shares `MODELS.md` and the Detector code item 12 reworks.

**Where.** `forseer/outlier.go` (new), `forseer/detector.go`,
`forseer/learn.go`, `forseer/engine.go`, `forseer/MODELS.md`,
`forseer/README.md`.

**Depends on.** Nothing.

### 14. File-descriptor and connection-state metrics

- **Area** agent · **Effort** S · **Score** 3.82 · **Shipped** 2026-09-16, `internal/collector/host/host.go`

**Why.** `host.go:46-81` emits cpu, memory, disk, net bytes and uptime;
`proc.go:21-26`'s sample is PID, Name, CPUPercent, RSS. Nothing counts open
file descriptors or socket states, so an EMFILE crash or ephemeral-port
exhaustion has no series to look at — and the threshold model would learn
those series for free.

**What.** `process.fd.count` per tracked process (gopsutil `NumFDs`);
`host.fd.used` and `host.fd.max` read from `$HOST_PROC/sys/fs/file-nr` —
gopsutil has no host-level fd call, and `host.go:4` already documents the
`HOST_PROC` convention; `host.net.conn_count{state=…}` from
`net.ConnectionsWithContext` grouped by Status. Each in the same best-effort
`if err == nil` block the file uses, the connection enumeration bounded so a
large conntrack table degrades only that one metric.

**Where.** `forsight/internal/collector/host/host.go`,
`forsight/internal/collector/proc/proc.go`, `forsight/README.md`.

**Depends on.** Nothing.

### 15. install.sh verifies what it runs, and stops resetting the unit

- **Area** ops · **Effort** M · **Score** 3.78 · merges the install.sh half of agent-collection-8 · **Shipped** 2026-09-16, install.sh

**Why.** `.github/workflows/forsight-release.yml:136-139` uploads
`dist/*.tar.gz` and nothing else; `forsight/install.sh:45-46` curls and untars
it unverified, then `:67` writes `ExecStart=${INSTALL_DIR}/forsight run` with
no flags on every run — so the documented `curl | sh` upgrade silently drops
`--store badger`, `--auth-token` and `--mlaas-url`.

**What.** `make release` writes `dist/sha256sums.txt`; `install.sh` downloads
it and verifies before extracting. The unit gains
`EnvironmentFile=-/etc/default/forsight` and `ExecStart=… run $FORSIGHT_ARGS`;
`install.sh` never overwrites an existing `/etc/default/forsight`; the README
documents it. Uploading the checksum file is a one-line change to
`forsight-release.yml` — proposed for Marc. A build-provenance attestation
needs `id-token: write` and is a separate ask. No config file: flags plus env
are enough, and a third source of truth is not.

**Where.** `forsight/Makefile`, `forsight/install.sh`, `forsight/README.md`,
`.github/workflows/forsight-release.yml` (Marc).

**Depends on.** Nothing.

### 16. Fuzz and benchmark Forseer's ingest boundary

- **Area** forseer · **Effort** S · **Score** 3.77 · merges quality-dx-3 · **Shipped** 2026-09-16, forseer/logs_fuzz_test.go

**Why.** `grep` for `func Fuzz` and `func Benchmark` across `forseer` and
`forsight` finds nothing. `templateOf` (`logs.go:179-188`, a four-regex chain)
and `severityTokens` (`severity.go:307-347`, `message[:8192]` cut by byte
before ranging runes) are the first code to touch every raw log line. Every
point, line and span also runs `Detector.Observe`,
`severityModel.Learn/Classify`, the threshold update and
`burnForecast.Observe` on the ingest path, and items 8, 12, 13 and 22 add
more work to it.

**What.** `FuzzTemplateOf` and `FuzzSeverityTokens` with Go's native fuzzer,
seeded from the table tests plus inputs that straddle the 8192 cut mid-rune,
asserting only safety — no panic, bounds respected, idempotent. A mid-rune cut
yields `RuneError`, not a panic, so expect these to pass; they stay as a
tripwire. `testing.B` benchmarks for the four per-record paths at realistic
cardinality up to the bound constants. Stdlib only. A short `-fuzztime` CI
step is a workflow edit, so it is proposed; the benchmarks stay informational.

**Where.** `forseer/logs_fuzz_test.go`, `forseer/severity_fuzz_test.go`,
`forseer/{detector,severity,thresholds,forecast}_bench_test.go` (all new).

**Depends on.** Nothing.

### 17. TLS on the agent listener, opt-in

- **Area** agent · **Effort** M · **Score** 3.62 · **Shipped** 2026-09-16, `forsight/cmd/run.go`'s `resolveTLSConfig`

**Why.** `run.go:219-224` is a plain `http.Server` and `ListenAndServe`;
there is no TLS path in the binary. With `--auth-token` set on a non-loopback
`--addr` — the DaemonSet's case — the bearer token and every payload cross the
network in cleartext. `forsight/README.md:183-186` already tells operators to
put mlaas behind HTTPS for the same reason.

**What.** `--tls-cert` and `--tls-key` (with env fallbacks, as the token has)
switch to `ListenAndServeTLS`; an optional `--tls-client-ca` sets
`ClientAuth: RequireAndVerifyClientCert` for mTLS. A bad path fails at startup
and never falls back to plaintext. `crypto/tls` only, so still one static
binary. README flags table, and a comment in `daemonset.yaml` on mounting a
cert-manager secret.

**Where.** `forsight/cmd/run.go`, `forsight/cmd/run_test.go`,
`forsight/README.md`, `forsight/deploy/k8s/daemonset.yaml`.

**Depends on.** Nothing.

### 18. The dashboard can supply the bearer token — and can load at all under `--auth-token`

- **Area** dashboard · **Effort** M · **Score** 3.57 · reframed · **Shipped** 2026-09-16, `BearerAuth` in `forsight/internal/api/auth.go`

**Why.** `forsight/internal/api/auth.go:11-29` exempts only `GET /healthz`,
and `run.go:219` wraps `server.Handler()` — which mounts the dashboard at `/`
(`server.go:73`) — so with `--auth-token` set the browser cannot fetch
`index.html`, let alone show a prompt. `api.ts` sets no `Authorization` header
anywhere. Setting the flag locks the dashboard out along with everyone else.
The pool's proposal could not work as written for this reason.

**What.** `BearerAuth` exempts `GET` on the static shell (`/` and `/assets/`)
— it is a public build; every data route stays protected. A token prompt on
the first 401, built from the design system's **Input** and **Dialog**, kept
in `sessionStorage` (never `localStorage`, never a query string), and a
`fetchWithAuth` used by every `usePoll` call (item 5) that clears the token
and re-prompts on 401. `auth_test.go` gains cases for the shell and for the
data routes.

**Where.** `forsight/internal/api/auth.go`, `forsight/internal/api/auth_test.go`,
`forsight/web/src/api.ts`, `forsight/web/src/App.tsx`.

**Depends on.** Item 5.

### 19. `build-web` supplies its own registry credential — no more `file:` link workaround

- **Area** ops · **Effort** S · **Score** 3.43 · reframed · **Shipped**
  2026-09-16, `build-web` in `forsight/Makefile`

**Why.** `forsight/Makefile:25` says `build-web` \"Needs packages:read auth
for npm (see forsight/web/.npmrc)\" and neither file says how to supply it;
`CLAUDE.md`'s answer is a `file:`-link plus React-symlink workaround,
rediscovered by hand. That workaround is for a gap that has closed:
`gh auth status` shows the local token carries `read:packages`;
`forsight/web/package-lock.json:1102` resolves `@marcfs31/forsight` from
`https://npm.pkg.github.com` (#76 regenerated it; no `file:../..` anywhere);
`forsight/web/README.md:9-12` already says a packages:read token is needed;
`forsight-ci.yml:116-137` does it with setup-node's `registry-url` and
`NODE_AUTH_TOKEN`. The only missing piece is the three lines that hand npm the
token locally.

**What.** `build-web` writes
`//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN:-$(gh auth token)}` into
a `mktemp` file, exports it as `NPM_CONFIG_USERCONFIG` for `npm ci`, and
removes it on exit through a trap — no `package.json` or lockfile mutation, no
two-Reacts trap, nothing to restore, never in the tree; a clear failure when
neither the variable nor `gh` can supply a token. One sentence in
`forsight/web/README.md` and item 6's section. For Marc: the `CLAUDE.md`
rough-edge paragraph can go, and `forsight-release.yml:92-105` still carries
the stale \"lockfile resolves `file:../..`\" comment and runs
`rm -f package-lock.json; npm install` before `make release`, so release
builds are not reproducible against the lockfile — `forsight-ci.yml` switched
to `npm ci` in #76 and the release workflow should follow.

**Where.** `forsight/Makefile`, `forsight/web/README.md`,
`.github/workflows/forsight-release.yml` (Marc).

**Depends on.** Nothing.

### 20. `forsight demo`: a synthetic stream so the dashboard is never empty

- **Area** agent · **Effort** M · **Score** 3.40 · **Shipped** 2026-09-16, `forsight/cmd/demo.go`

**Why.** `Overview.tsx` renders **EmptyState** in eight panels (`:336`, `:406`,
`:420`, `:458`, `:494`, `:515`, `:531`, `:544`), the Models page needs enough
rows before mlaas will train (`forsight/README.md:189-192`), and there is no
demo or seed target in the Makefile or `cmd/`. The only populated dashboard
anyone has seen ran on real infrastructure.

**What.** A `forsight demo` subcommand: a bounded synthetic stream — host
metrics with a daily cycle and one CPU spike with a named culprit, a few log
templates including one burst and declared-severity lines, a couple of traces
with a slow child span — fed through the same `Engine.Observe*` and store
paths a real deployment uses, with hostnames and lines visibly labelled
synthetic, and an optional backfill of N hours so the forecasts have a trend
to learn. `make demo` runs it against the memory store.

**Where.** `forsight/cmd/demo.go` (new), `forsight/Makefile`,
`forsight/README.md`.

**Depends on.** Nothing.

### 21. Drift as a Badge with a not-measured state

- **Area** mlaas · **Effort** S · **Score** 3.33 · corrected · **Shipped** 2026-09-16, `buildModelsLocked` in `forsight/internal/mlaas/sync.go`

**Why.** `Models.tsx:388` renders `model.driftMax.toFixed(2)`;
`types.go:144`'s `DriftMax` is a plain `float64` copied at `sync.go:685` from
`wireCheck` (`client.go:145-154`), which parses `drift_max` only. On mlaas's
wire (`internal/train/loop.go:208-221`) `drift` is `map[string]float64` with
`omitempty` and `drift_max` a plain `float64` that is always present and 0
when unmeasured — which is why the first draft's \"make `DriftMax` a
`*float64`\" could never have gone nil. A model whose live window has not
filled shows `0.00`, the \"flattering zero\" `MODELS.md` says a card must never
show; Forseer's own cards say `Unmeasured`.

**What.** `wireCheck` gains `Drift map[string]float64` (`json:\"drift\"`) and
`DriftFeature string` (`json:\"drift_feature\"`); `ModelStatus.DriftMax`
becomes `*float64` (plus `DriftFeature`), set in `buildModelsLocked` only when
`Drift != nil` — the test mlaas itself applies on its `/metrics`
(`server.go:1101`) and in its retrain trigger (`loop.go:299`). `wireRetrain`
(`client.go:75-79`) gains `DriftThreshold` so the **Badge** compares against
the value mlaas retrains on (`retrain.drift_threshold`, default 0.2, mlaas
`store.go:124`, `:259-260`): warning above half of it, critical at or above
it, in `Insight.Severity`'s vocabulary, the threshold and the drifting feature
stated in the cell, and \"not enough data yet\" when nil. `client_test.go`
gains a check with `drift` and one without. Still S.

**Where.** `forsight/internal/mlaas/client.go`, `types.go`, `sync.go`,
`client_test.go`, `sync_test.go`, `forsight/web/src/api.ts`,
`forsight/web/src/pages/Models.tsx`.

**Depends on.** Item 1.

## Later

### 22. Culprits ranked by change, not raw CPU, with a card

- **Area** forseer · **Effort** M · **Score** 2.98 · reframed · **Shipped**
  2026-09-16, `culpritModel` in `forseer/culprit.go`

**Why.** `forseer/engine.go:176-181` sorts processes by raw CPU, keeps three,
and drops any under 20%. A process that went from 2% to 18% during the spike
is dropped for one that always sits at 22%. And culprit is not in
`Engine.Models()` (`:97`), so it has no card and nobody can see how often it
was right.

**What.** Rank by each process's change since the host anomaly opened — the
Detector already keeps a rolling baseline per `process.cpu` series, so the z
or delta is free — with the 20% floor kept as the named fallback rule. A Card
declaring Reads (process cpu, rss, their deltas) that reports `Unmeasured`
until there is a label. The learned score — logistic over the same features,
self-supervised on \"the process's own series fell as the host anomaly closed\"
— is the follow-up once that label has been watched for a while; it is a
proxy for causation, not causation, and the card must say so.

**Where.** `forseer/engine.go`, `forseer/learn.go`, `forseer/MODELS.md`.

**Depends on.** Nothing.

### 23. LineChart draws a projection as a projection

- **Area** design system · **Effort** M · **Score** 3.50 · scoped down · **Shipped** 2026-09-16, LineChart

**Why.** `src/components/LineChart.tsx:28-46` has `series` and `annotations`
only. `Models.tsx:170-188` draws each mlaas forecast as a second solid series
meeting the observed one at a \"forecast starts\" annotation — a reader tells
projection from measurement only by the legend. The band half of the pool
item has no producer: mlaas's `ForecastPoint` (`types.go:169`) carries a value
and no bounds, and Forseer's Holt band lives in the ErrorBudget caption, not
as points.

**What.** A per-series `dashedFrom` index (or a `projection` prop) that
switches the stroke to dashed past that point and adds \"projected from
<label>\" to ChartFrame's hidden description — shape, not colour, per
CONTRIBUTING. `bandPath(upper, lower)` in `chart.ts` only when a producer
sends bounds. One y-axis, same domain. Story, test, changeset; the dashboard
picks it up on the design-system release after item 4's pin bump — as a
Dependabot PR once the secret exists, by hand otherwise — which is why this is
later.

**Where.** `src/components/LineChart.tsx`, `src/lib/chart.ts`,
`src/components/LineChart.stories.tsx`, `src/components/LineChart.test.tsx`,
then `forsight/web/src/pages/Models.tsx`.

**Depends on.** Items 1 and 4.

### 24. A DaemonSet that can actually run: image, probes, `/readyz`

- **Area** ops · **Effort** M · **Score** 3.52 · merges agent-collection-9 · **Shipped** 2026-09-16, `handleReadyz` in `forsight/internal/api/handlers.go`

**Why.** `forsight/deploy/k8s/daemonset.yaml:53` pins
`ghcr.io/marcfs31/forsight:latest`; there is no Dockerfile in the repo and no
publish step in `forsight-release.yml`, so `kubectl apply` fails to pull. The
container has no `livenessProbe` or `readinessProbe`, and
`handlers.go:15-17`'s `handleHealthz` is an unconditional `{\"status\":\"ok\"}` —
a probe on it would report healthy with the store gone. The pool's `hostPID`
claim was wrong: gopsutil enumerates via `HOST_PROC`, which the manifest
already mounts (`:56-58`).

**What.** A scratch or distroless Dockerfile for the static binary; a ghcr
publish step on the `forsight-vX.Y.Z` tag — needs `packages: write`, so Marc
applies that half, which is why this is later. `livenessProbe` on `/healthz`,
left cheap and unconditional; `readinessProbe` on a new `/readyz` that pings
the store and reports each collector's last error from the Registry —
readiness, not liveness, so a missing Docker socket cannot crash-loop the pod.
`HOST_ETC` alongside `HOST_PROC` and `HOST_SYS`, as `host.go:4` documents the
trio.

**Where.** `forsight/Dockerfile` (new), `forsight/deploy/k8s/daemonset.yaml`,
`forsight/internal/api/handlers.go`, `forsight/internal/collector/collector.go`,
`.github/workflows/forsight-release.yml` (Marc).

**Depends on.** Nothing.

### 25. Persist what has been learned

- **Area** forseer · **Effort** L · **Score** 3.47

**Why.** `forseer/engine.go:37-54` constructs every model cold and restores
nothing, so `severityMinTrained` and `thresholdMinSamples` are re-earned on
every restart; on an agent that restarts often, the models are never ready.
`MODELS.md` Next #3. L because it touches every model and the shutdown path.

**What.** `Snapshot() []byte` and `Restore([]byte) error` on the `Model`
interface (`learn.go:27`), JSON with a per-model schema version. Written on
shutdown and read in `NewEngine` before the fallbacks wire, as a file beside
the Badger directory under `--data-dir` — memory-store deployments have no
data dir and stay cold. A mismatched or unparsable version is discarded, never
misread. Trained counts restore; the grading window resets, so readiness is
re-earned on live data and the fallback contract holds. The data directory
belongs to the operator; nothing lands in the repo.

**Where.** `forseer/learn.go`, `forseer/severity.go`, `forseer/thresholds.go`,
`forseer/forecast.go`, `forseer/engine.go`, `forsight/cmd/run.go`.

**Depends on.** Nothing.

## Left out, and why

Strong proposals that did not make the twenty-five, with the reason. Scores
are the judges' composites.

- **Chart-only size-limit budget** (design-system-10, 3.92). The why was
  wrong: `.github/workflows/ci.yml:46` runs `npm run smoke` in the required
  verify job, and `scripts/smoke-test.mjs:243-286` already fails the build if
  `import { Button }` is not far below the barrel. Only the chart path is
  unbudgeted, and it cannot matter until the embed is built from a
  tree-shaking release. The chart-path check is the follow-up inside item 4.
- **Responsive header, Containers overflow, status live region**
  (dashboard-ux-10, 3.83). Three one-liners in `Overview.tsx` that belong in
  the same PR as the connection state they announce; inside item 5.
- **A script for the `file:`-link workaround** (ops-security-8 as proposed,
  3.43). The link is gone from the lockfile and the local token has
  `read:packages`; reframed as item 19, three lines of npm auth instead of a
  mutation-and-restore script.
- **Theme toggle** (dashboard-ux-6, 3.72). Cheap, and `applyForsightTheme`
  plus the anti-flash script already exist for it — but no operator decision
  changes. Next round.
- **Fold seven chart components into the DashboardRTL story**
  (design-system-8, 3.62). Strengthens a design-system gate; invisible in the
  agent. Belongs on the design system's own list.
- **Dense Sparkline size** (design-system-4, 3.65). Earns its place only once
  sparklines go into table rows; item 11 puts them in StatCards, where the
  default fits.
- **Learned CUSUM `h` per series** (forseer-models-7, 3.07). The statistic
  already sums sigma-normalised z, so the constant is scale-free, and a
  changepoints-per-week budget converges too slowly for Robbins-Monro — the
  trap `thresholds.go` documents. Not proven enough to schedule.
- **End-to-end boot test** (quality-dx-2, 3.22). The go CI job never builds
  the real dashboard, so it could only assert the fallback page. Revisit after
  item 24's `/readyz` gives it something to wait on.
- **TLS-expiry and HTTP probe collectors** (agent-collection-2 and -3, 3.40
  and 3.03). One probe collector is the right shape and UptimeBar is waiting
  for it, but reconstructing segments from raw points needs item 10 first.
- **Downsampling tier** (agent-collection-7, 2.73). L; the concrete consumer
  is the mlaas exporter re-bucketing fourteen days each pass. After item 10.
- **`forsight/CHANGELOG.md`** (quality-dx-8, 3.25). Worth starting at the
  second `forsight-v*` tag; the release-workflow gate is Marc's.
- **`make help`** (quality-dx-10, 3.40). Trivial; fold into whichever
  Makefile PR lands first (item 15 or 19).
- **A `--config` file** (the other half of agent-collection-8). A third
  source of truth next to flags and env. The install.sh half is item 15.
- **Champion severity classifier, the feedback loop, capacity forecasts, the
  routing shell** (mlaas-depth-3, -5, -4, dashboard-ux-1). Stale:
  `sync.go`'s `feedback()` and `forecast()`, `ForecastsCard`,
  `PredictionsCard` and the hash router are already on the branch. That is
  items 1 and 2.
- **mlaas health as an AlertList insight** (mlaas-depth-8, 2.72). A `forseer`
  Kind for an external signal breaks \"forseer has no idea mlaas exists\". The
  shipped StatusDot outage on the Models page is the right surface.
- **Tuning leaderboard** (mlaas-depth-7, 2.55). The Tune button ships; the
  leaderboard is mlaas's own console, and copying it turns the comparison page
  into an mlaas admin page.
- **Client-side acknowledge** (dashboard-ux-5, 2.82). Insight IDs expire and
  reopen, so a localStorage ack hides every recurrence forever. Needs a
  backend endpoint.
- **Log detail drawer** (dashboard-ux-4, 2.85). LogStream has no per-row
  callback; a design-system change first.
- **gRPC receiver** (agent-collection-4, 2.62). L, grpc-go in a minimal static
  binary, a second port with no auth story.
- **Per-IP rate limiting** (ops-security-2, 2.67). The 32 MiB body cap and the
  fixed OTLP memory bounds cover the damaging case; adds `x/time/rate` for a
  threat with no evidence yet.
- **Shared secret type** (ops-security-5, 3.05). Hygiene; the \"proxied error
  contains the key\" test guards a leak that cannot happen, and the type would
  have to live in `forseer`.
- **`forsight/web` coverage gate** (quality-dx-6, 2.95). Evidence partly wrong
  — there is no `test:coverage` script or coverage dependency in
  `forsight/web` — and the CI step is a workflow edit.
- **Exemplars** (agent-collection-5, 2.47). `trace_id` labels on bucket
  metrics make every point its own series, the cardinality amplification the
  OTLP bounds work just closed.
- **Incident grouping model** (forseer-models-9, 2.77). No label source, so
  the \"model\" is never ready and never merges. Do it as a deterministic window
  plus shared-`Related` grouping when the Timeline needs it.
- **Weekly seasonality** (forseer-models-4, 2.92). Weeks to fill buckets
  against the 512-series cap. (The Mahalanobis host anomaly that used to share
  this line, forseer-models-6, is now item 13 with an exit criterion, because
  `forseer/README.md`'s own Next row needed a decision either way.)
- **OpenAPI codegen, DOM snapshot, command palette, ModelCard, KpiRow,
  DiffViewer, NotificationCenter, federation, backup awareness, catalog smoke
  test** (quality-dx-1, quality-dx-9, dashboard-ux-7, design-system-2, -3, -5,
  -6, agent-collection-10, mlaas-depth-9, -10; all at or below 3.18). Low
  value, wrong shape, or no consumer. The judges' notes stand.
