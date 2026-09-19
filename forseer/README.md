# Forseer

All AI and ML that sits on top of Forsight lives here. The observability
agent (`forsight/`) collects; Forseer *interprets*.

No extra process: `forsight run` is enough. Statistical detection needs no
API key. Optional Grok narrative uses SpaceXAI (`XAI_API_KEY`).

| Path | What |
| --- | --- |
| `.` (this Go module) | Detectors and trained models compiled into the agent |
| [`MODELS.md`](MODELS.md) | What a Forseer model is, the one that ships, and the roadmap |
| [`python/`](python/) | Training, notebooks, local models. They talk to the agent over `/api/v1/*`. |

## What the agent serves

| Path | Needs key | What |
| --- | --- | --- |
| `GET /api/v1/forseer/insights` | no | Open findings, critical first |
| `GET /api/v1/forseer/clusters` | no | Drain-style log templates |
| `GET /api/v1/forseer/budget` | no | Error-log burn vs a 1% SLO |
| `GET /api/v1/forseer/timeline` | no | Stitched incident events |
| `GET /api/v1/forseer/query?q=` | no | Phrase → FilterBar facets |
| `GET /api/v1/forseer/models` | no | What each trained model reads, whether it is ready, how it scores |
| `GET /api/v1/forseer/summary` | `XAI_API_KEY` | Grok paragraph; otherwise `{"enabled":false}` |
| `GET /api/v1/forseer/classify?message=` | no | Classify one log line: the in-binary severity model if it's ready, else the substring fallback |
| `GET /api/v1/mlaas/status` | no | Snapshot of the mlaas integration: model state, forecasts, recent predictions, jobs |
| `POST /api/v1/mlaas/models/{name}/train` | no | Queue a training job for one of the four models `forsight` manages in mlaas |
| `POST /api/v1/mlaas/models/{name}/tune` | no | Queue a tuning job for the same |
| `POST /api/v1/mlaas/models/{name}/predict` | no | Proxy up to 10 rows to a managed mlaas model and return its predictions |

The `mlaas` routes proxy a separate service (see
[`forsight/README.md`](../forsight/README.md#mlaas-integration)) and never
expose its API key to the caller. `status` always answers 200, even when
mlaas isn't configured (`configured:false` and empty slices, so the Models
page never has to special-case a missing route); `train`, `tune`, and
`predict` are actions, so they 503 instead when there's nothing to act on.

## Two kinds of model

The dashboard's Models page shows both side by side on purpose. The models
below train inside this module: online, on this deployment's own stream,
gated on a named fallback, carrying no weights. The models `forsight` hands
to mlaas train in a separate service instead, with a real held-out score and
their own retrain loop. The severity classifier ships both ways — same job,
same input, scored the same way — so the difference between "trained here"
and "trained by mlaas" is something an operator can read off the page, not
something they have to take on faith.

## Detectors that ship, and the component they drive

Every detector is chosen because a Forsight design-system component already
exists for that shape of answer. Forseer never invents a new widget.

| Kind | Method | Why it is ML/stats, not a threshold | Dashboard component |
| --- | --- | --- | --- |
| `log severity` | Multinomial naive Bayes over hashed tokens, trained online on the levels OTLP declares | A tailed line carries no level; the four-substring rule it replaces reads "no errors reported" as an error and "panic:" as info. See [MODELS.md](MODELS.md) | **LogStream** |
| `alert thresholds` | A P² running quantile per threshold, at 1 − the budgeted alert rate, each used once the series has two expected exceedances beyond it | One shared 3σ pages constantly on a noisy series and never fires on a smooth one, because sigma only means "rare" for a distribution metrics do not have. See [MODELS.md](MODELS.md) | **AlertList** |
| `log burst paging` | Online logistic regression per template over error share, burst shape and co-occurring criticals, labelled by whether a critical from another detector followed within five minutes | The volume rule cannot tell a debug template tripling from an exception template tripling; this deployment's own incident history can. See [MODELS.md](MODELS.md) | **BarList** (ranked by what a burst is worth) + the burst's severity on **AlertList** |
| `budget forecast` | Holt's linear method, projected to 100% with a band from the trend's own uncertainty | "60% consumed" is a fact about the past; when it runs out is the actionable question | **ErrorBudget** |
| `anomaly` | Welford online mean/variance, 3σ / 5σ, each point scored against the baseline of every point but itself | The baseline is the series itself, not a hardcoded CPU%; and scoring a point against statistics it is part of caps the score at Samuelson's (n-1)/√n, which cannot reach 5σ before the 29th point of a series. See [MODELS.md](MODELS.md) | **AlertList** (severity vocabulary is identical) |
| `changepoint` | CUSUM on the same z-scores | Catches a *shift* that a single spike detector misses (disk filling, leak) | **Timeline** |
| `log_burst` | Drain-lite templates (UUID/IP/number → `<*>`) + short-window volume | Turns a firehose into "this pattern just exploded" | **BarList** (ranked templates) + **LogStream** (raw lines) |
| `slow_span` | Per `(service, span name)` P² p50/p99, opened on a run past p99 | "This endpoint is slow *for itself*", not vs a global 200ms SLO, and not a mean/sigma test on a long-tailed shape. See [MODELS.md](MODELS.md) | **TraceWaterfall** (related trace id) |
| `culprit` | Rank processes open during a `host.cpu` anomaly by how far each has risen above its own rolling `process.cpu`/`process.memory.rss` baseline (free from the same Welford stats the Detector runs on them), raw CPU over a 20% floor only while a process's own series is still cold | Raw usage cannot tell a process that jumped from 2% to 18% from one that always idles at 22%; its own baseline can. See [MODELS.md](MODELS.md) | **Table** (process rows) |
| `host_outlier` | Online mean vector + 5x5 covariance (Welford's multivariate form) over cpu%, memory%, disk% and the per-interval delta of each net counter, scored by squared Mahalanobis distance against the Detector's own 3σ/5σ tail probabilities | Three host percentages moving together can hide a story none of them tells alone; a per-series check never looks at more than one series at a time. See [MODELS.md](MODELS.md) | **AlertList** |
| Grok narrative | SpaceXAI `grok-4.5` | Stitches the above into four sentences an on-call can read | **Card** + **Text** |
| error-log budget | error/total vs 1% SLO | **ErrorBudget** |
| incident stitch | insights within a 5-minute window that share a Related value or a Source are folded into one incident event, newest member last; deterministic, not a Model — no label exists for "these were one incident" (`forseer/incident.go`, roadmap's "Left out") | **Timeline** |
| NL filter | phrase → facets (`error logs from checkout`) | **FilterBar** |
| hour-of-day baseline | separate z-score per hour for host/docker series | **AlertList** (same findings, less night/day false fire) |
| critical path | walk error leaf to root | **TraceWaterfall** via `related` |

`Insight.severity` is `critical` / `warning` / `info` on purpose: those are
`AlertListItem.severity` values. `Insight.kind` is how the dashboard picks
Timeline vs AlertList vs BarList.

## Why these, not a chatbot

A generic "ask the dashboard" box would sit on **Combobox** / **FilterBar**
and ignore the rest of the library. The useful work is *scoring the stream
and landing each score on the component that already knows how to show it*:

- Alerts that page belong on **AlertList**, not a paragraph.
- Regime changes belong on **Timeline**, next to deploys later.
- Repeated logs belong on **BarList**, with the raw feed still on **LogStream**.
- Slow traces belong on **TraceWaterfall**, not a table of durations.
- A burned SLO belongs on **ErrorBudget**, not a chatbot that restates the
  burn in prose.

## Shipped since the table below was first written

Seasonal (hour-of-day) baselines, the NL-filter → FilterBar facets, incident
stitching onto Timeline, and trace critical-path → TraceWaterfall are all
built — see the detector table above (`seasonal baseline`, `NL filter`,
`incident stitch`, `critical path` rows). `Engine.Budget()` reads the current
error-log burn against a 1% SLO for **ErrorBudget**, and the forecast that
projects that burn forward is built too — see [MODELS.md](MODELS.md).

## Next (stay in this folder)

Nothing is queued here right now. The one row this table used to carry —
multivariate outlier detection, an Isolation Forest in `python/` with scores
POSTed back — is **built**, but not that way: mlaas has no IsolationForest
(`plugins/sklearn/plugin.py`) and the agent had no route to ingest a score
computed elsewhere, so it shipped as a stdlib Mahalanobis check over an
online mean/covariance instead, the same contract as everything else in this
module. See `host_outlier` in the detector table above and
[MODELS.md](MODELS.md#host-outlier) for why, and for the exit criterion that
decides whether it earns its keep.

The error-budget forecast is **built** — see [MODELS.md](MODELS.md). So are
per-series alert thresholds and the log-severity classifier; the rest of the
model roadmap lives there too.

Do not put model weights or API keys in this repo. Python jobs that need
numpy/a trainer stay under `python/` and read `/api/v1/*`; they do not
belong in the static `forsight` binary.
