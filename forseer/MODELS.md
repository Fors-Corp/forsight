# Models

Forseer's tools are models. Each one does a single job, learns that job from
this deployment's own stream, and reads only the inputs it declares.

`GET /api/v1/forseer/models` returns a card per model: its job, its inputs,
whether it is ready, and how it is scoring.

## What a model is here

The contract is `Model` in [`learn.go`](learn.go), and it is deliberately
small. Every model must have three things:

**A declared input list.** `Reads` is the whole list, not a summary. The
severity model reads log message text. Not the source, not the host, not the
time of day, not the trace it belongs to — just the text. A model that looks
at something it did not declare is a bug, and the list is what makes that
checkable.

**A readiness gate and a named fallback.** A model is used only while it is
beating the thing it replaces, and the fallback is scored on exactly the same
examples so that is a measurement rather than a hope. A cold model never
degrades the product, a model that falls behind stands itself down with
nobody watching, and an operator who reads the card always knows which answer
they are getting. A model with no fallback cannot be gated, so naming one is
part of the contract.

**A measured score.** Where the stream supplies its own labels, the model
predicts each example *before* training on it and keeps the running hit rate.
The accuracy on the card is therefore always measured on data the model had
not yet seen. Nothing here reports a number it was told rather than earned;
a job with no labels reports `Unmeasured`, not a flattering zero.

## Why trained here, not shipped trained

No weights in this repo, and none in the binary. A model that has never run
knows nothing.

That is not a limitation, it is the point. A model trained on everyone's logs
carries a great deal of information about everyone's logs, and almost all of
it is wrong here: someone else's vocabulary, someone else's traffic shape,
someone else's idea of a slow endpoint. A model trained on one job against
one stream carries almost none of it, which is why a 64KB table beats a large
general model at this particular question.

It also keeps the agent what it claims to be: one static binary, no sidecar,
no API key, nothing leaving the host.

Practical consequences, and they are constraints not preferences:

- **Stdlib only.** The `forseer` module has no dependencies and must keep
  none. That rules out a tensor library and rules in counting, hashing, and
  closed-form fits.
- **Bounded state.** The severity model hashes tokens into a fixed 4096
  buckets, so its size does not depend on how large the vocabulary grows.
  Every model needs an equivalent bound.
- **Online.** Training happens per record on the ingest path, so a
  deployment that starts logging a new phrase today is understood today.
- **Python stays in [`python/`](python/).** A trainer that needs numpy reads
  `/api/v1/*` and posts results back. It never enters the binary.

## Shipped

### Log severity

Also the first to carry the fallback comparison: the substring rule is scored
on the same stream, in the same window, and the model is used only while it
is at least a point ahead.

**Job.** Give a log line that arrived without a level the level this
deployment would have given it.

A tailed file carries no level, so the agent has to work one out. The rule
that did it is four substrings, and it is wrong in both directions: it reads
"no errors reported" and "error_rate 0" as errors because the word is there,
and it reads "panic: nil map write" as info because the word is not. No
number of extra substrings fixes that, because which words matter depends on
the system doing the logging.

**Labels are free.** Logs arriving over OTLP carry a real level set by the
application. Those are labelled examples of exactly this deployment's
vocabulary. The model learns from the labelled half of the stream and is
applied to the unlabelled half — and never trains on an inferred level,
which would only teach it the substring rule back.

**Method.** Multinomial naive Bayes over hashed tokens, trained online.
Numbers are dropped: a request id or a duration is unique per line, so it
teaches nothing and would fill every bucket with noise.

**Measured on a demo stream**, 390 labelled lines: 98.2% prequential accuracy
against the substring rule's 22.9% on the same 389 examples. That gap is
flattering — the demo corpus is built from the rule's blind spots on purpose,
so treat it as a demonstration that the comparison works, not as a number any
real deployment will see. What matters is that the agent measures it rather
than claims it. On the same five lines:

| Line | Substring rule | Model |
| --- | --- | --- |
| `no errors reported during the sweep` | error | **info** |
| `recovered from the earlier failure and resumed` | error | **info** |
| `panic: nil map write in handler` | info | **error** |
| `connection refused by the payment service` | info | **error** |
| `health probe ok` | info | info |

**Component.** **LogStream**, and every severity filter above it.

### Alert thresholds

**Job.** Decide how far out of line one series has to go before a human
should hear about it.

Every series shared 3σ and 5σ. That is the right shape of answer and the
wrong number for almost every series, because sigma only means "rare" if the
series is normally distributed, and metrics are not: a percentage bounded at
100 is skewed, a request rate has a daily cycle, and a queue depth that is
mostly zero has its standard deviation set by the very spikes it is supposed
to detect. What the operator sees is one series paging every few minutes and
another that never fires.

So the threshold is learned per series from that series' own history, and the
budget is stated in a unit somebody can hold an opinion about: alert on about
one point in a thousand, page on one in ten thousand.

**Reads.** The z-score of one series, as the Detector computes it — and it
computes it against the baseline that excludes the point being scored. That
is not a detail of another model: an in-sample z is bounded by Samuelson's
inequality to (n-1)/√n, which is 3.18 at n=12 and does not reach 5 until
n=29, so a threshold learned from in-sample z would be learning the shape of
that ceiling as much as the shape of the series.

**Method.** One running quantile per threshold. A budget stated as a rate is
a quantile — "alert on one point in a thousand" is "alert above this series'
99.9th percentile of |z|" — so each threshold is the P² estimator already in
this package ([`p2.go`](p2.go)): five markers, O(1) per point, no stored
sample and no assumption about the shape of the distribution.

It replaces a Robbins-Monro stochastic approximation,
`t ← t · (1 + step · (exceeded − target))`, which chases the same quantile
and settles in the same place — eventually. Eventually was the problem, and
it was not a detail. Every non-exceeding point pulled a threshold down by
`step · target` of itself: 2e-5 for the warning, 2e-6 for the page. From
`criticalSigma`, reaching the tail of an ordinary |N(0,1)| series took on the
order of 10⁵ points of uninterrupted downward drift. Measured on that data,
the realised page rate was exactly zero at 2 000 points and 0.00003% at
10 000, against a 0.01% budget — and `seasonalKey` gives each hour of the day
its own key, so an hourly key collects about 360 points a day and the page
threshold would have arrived some time the following year.

**Readiness means converged, and each threshold earns it separately.** A
quantile at tail probability q is estimated from the points that land beyond
it, of which a series has seen about n·q: no method can know where the
one-in-ten-thousand point of a distribution is from a sample that contains
none of them. So a threshold is used instead of its constant once the series
has two expected exceedances beyond it — 2 000 points for the warning,
20 000 for the page — which on |N(0,1)| is where the P² estimate becomes
worth more than the constant it replaces:

| Points seen | P² warning estimate (truth 3.29) | P² page estimate (truth 3.89) |
| --- | --- | --- |
| 1 000 | 3.06 | 3.06 |
| 2 000 | **3.25** | 3.25 |
| 10 000 | 3.28 | 3.68 |
| 20 000 | 3.28 | **3.86** |
| 100 000 | 3.29 | 3.89 |

The two columns being identical below 10 000 points is the same fact from
the other side: with less than one expected exceedance beyond the page
quantile, the sample holds nothing that distinguishes it from the warning
quantile, and any number claiming otherwise would be extrapolation. Two
expected exceedances realises 1.15× the budgeted rate at the moment of
readiness and
tightens from there; at one expected exceedance it would be 2.2×, which is
why the wait is what it is. The two thresholds are therefore ready at
different times, and the card says how many series have earned each rather
than hiding both behind one boolean. `Snapshot`/`Restore` carries the markers
across restarts, so the wait is paid once rather than once per restart.

**Measured**, realised alert rate over the 100 000 points following n points
of history, on |N(0,1)| across 60 series — the budget is 0.1% and 0.01%:

| n | Warning, before | Warning, now | Page, before | Page, now |
| --- | --- | --- | --- | --- |
| 1 000 | 0.084% | 0.105% | 0.00058% | 0.00895% |
| 2 000 | 0.083% | 0.104% | 0.00062% | 0.00908% |
| 10 000 | 0.081% | 0.102% | 0.00075% | 0.00995% |

**Measured** over 40 000 points per series after a 20 000-point warm-up, the
comparison against the fixed pair it replaces:

| Series | Fixed 3σ alerts on | Learned alerts on | Learned threshold |
| --- | --- | --- | --- |
| well-behaved | 0.25% of points | 0.10% | 3.32 |
| heavy-tailed | 5.10% of points | 0.33% | 12.00 |

The heavy-tailed row is the one that matters: at a ten-second interval, 5.1%
is a page every three minutes, which is how an alert channel becomes something
people mute. It does not reach the 0.1% budget because it is pressed against
the ceiling — deliberate, since a series that genuinely goes haywire must stay
alertable.

**Component.** **AlertList**, unchanged — the severities are the same, there
are just far fewer of them that nobody asked for.

### Error-budget forecast

**Job.** Say when the error budget will be gone, not just how much of it
already is.

Those are different questions and only the second is actionable. "You have
used 60% of the budget" is a fact about the past, and whether it is a problem
depends entirely on whether that 60% arrived over a month or over the last
twenty minutes.

**Method.** Holt's linear method — a level and a trend, each exponentially
smoothed — projected forward to 100%. The projection is a range, not a line,
widened by the uncertainty in the trend.

**Two things this got wrong first, both caught by running it rather than by
reading it.**

The gate was originally the head-to-head win rate against naive persistence,
the way the severity model gates against its substring rule. That is the wrong
test here: one-step accuracy on a flat series is a tie that persistence wins,
so the gate closed exactly when a burn turned upward and would have needed a
hundred observations of sustained trend to reopen — useless, since the turn is
the entire thing a forecast is for. What the model actually replaces is *no
projection at all*: persistence is flat by construction and never reaches
100%, so it never produces an exhaustion time. The gate is now whether the
trend survives its own error band, which is a significance test that falls out
of the band already being computed.

The band was then sized with the per-step prediction error, which is a
different quantity from the uncertainty in the trend. Per-step noise does not
shrink however long the model runs; the trend estimate does, because smoothing
averages it away. Confusing the two made the band far too wide and withheld
projections from series with a perfectly clear trend.

**Measured** against a quiet period followed by a rising burn, read once per
second:

| Consumed | Projection |
| --- | --- |
| 2.2% (quiet) | none — no trend that stands out from the noise |
| 5.0% | exhausted in 5m to 2h |
| 6.5% | exhausted in 5m to 40m |
| 9.9% | exhausted in 5m to 15m |
| 13.7% | exhausted in 1m to 10m |

The range narrowing as evidence accumulates is the behaviour being aimed at.
The win rate against persistence stays on the card as an honest measure of
one-step skill — information, not a gate.

**Component.** **ErrorBudget**, whose caption now carries the projection, so a
dashboard that knows nothing about the new field still shows it.

### Log burst paging

**Job.** Decide whether a burst of this log template is worth paging for.

`log_burst` fires on volume, so a chatty debug template tripling and an
exception template tripling were the same insight, and the rule that picked
critical over warning — any error line, or sixteen lines a minute — could
not tell them apart either.

**Labels are self-supervised.** A burst is worth paging if a critical from
a different detector — a metric anomaly, a slow span, a culprit process —
opened within five minutes after it. Log-burst insights are excluded from
both the label and the co-occurrence feature; otherwise the model would
learn the volume rule back from itself, which is the one thing the rules
below forbid. The label is time based on purpose: every template that
burst in the five minutes before an incident earns the credit, because a
template that bursts during incidents is worth paging.

**Method.** Online logistic regression over three declared inputs — the
template's error share, its burst shape (this minute over the last, log
scaled), and whether a critical from another detector opened within the
minute before — with one weight vector per template. A template's vector
starts as a copy of a shared vector that trains on every example, so a
first burst is scored by what this deployment has learned about bursts in
general and the template specialises from there. Predict-then-train, with
the label arriving minutes later: a burst registers one pending example per
episode, carrying the latest verdict of both the model and the rule (the
rule's own answer moves from warning to critical as a burst grows, and the
operator sees the last one), and is graded when the window closes or a
qualifying critical arrives. State is bounded at 257 vectors of four
floats, the same 256-template cap the miner keeps plus the shared one.

**Measured on a synthetic stream** of eighty bursts, half from an exception
template always followed by a metric critical and half from a debug
template never followed by anything, both past sixteen lines a minute so the
volume rule calls every one critical: 97.5% prequential accuracy against
the rule's 50%, with the debug template scoring 0.05 (fed to the model
directly, in the same suite, the exception template scores 0.95 and the
debug one 0.07). The gate then lets the model set the burst's severity, so
the debug burst opens as a warning. As with the severity model, the corpus
is built from the rule's blind spot; what matters is that the comparison is
measured on this deployment's own bursts and the model is used only while
it is ahead.

**Component.** **BarList**, which ranks templates by count until the model
is ready and by this score after — the card's title says which — with the
burst's own severity on **AlertList**.

### Per-endpoint latency shape

**Job.** Decide what slow means for one endpoint.

`slow_span` used to compare an endpoint to itself with a mean/three-sigma
z-score, which assumes a shape latency does not have: it is long-tailed, so
the mean sits above the median and the standard deviation is set by the very
tail the check is supposed to be judging. "Slower than this endpoint's own
p99" is a sentence an on-call can act on; "5.2 sigma out" is not, unless they
already know the distribution — which for latency, nobody does.

**Method.** Two P² ("piecewise-parabolic") quantile estimators — Jain &
Chlamtac, 1985 — per `(service, span name)`: five markers each, updated in
O(1) time and space with no buffered sample and no assumption about the
shape of the distribution. One targets the median, whose five markers are
min/q1/median/q3/max — a box plot, for free, for a later per-endpoint spread
view. The other targets p99, and a span past it is what opens `slow_span`
now, replacing the sigma test.

A *calibrated* p99 fires on about one span in a hundred *by construction*, so
opening still waits for a run of `spanExceedRun` (3) consecutive exceedances
— an alert budget in the spirit of the learned thresholds above, sized
without standing up a second model to learn it. Closing is immediate on the
first span back in line; the budget only guards the false-positive cost of
opening.

"Calibrated" is the load-bearing word, and `spanP99MinSamples` (200) is what
earns it. A P² marker converges on a long tail from below, so a series judged
on twelve samples reports a "p99" that is really nearer a p79: measured on
stationary lognormal latency, a series with 12-25 samples lands past its own
marker on 13.5% of spans and opens an insight once per 323, against a budget
of one per million. From 200 samples on it is 1.11% and one per 734 000. Two
hundred rather than a thousand because a CUSUM reset drops the count to zero
and the gate is not free: the share of spans judged at all falls from 75.6%
at 200 to 23.5% at 1000, for another 0.06 percentage points of calibration.

A CUSUM on the span's deviation from the median — scaled by the p50
tracker's own interquartile spread, so it is a shape-free "how many spreads
out" rather than a sigma — watches for a sustained shift and resets both
estimators when it crosses the same `cusumK`/`cusumH` the Detector runs on
its metric series. Spans never reach the Detector, so this is the same test
wired up locally rather than shared; without it a deploy that doubles an
endpoint's latency would get a p99 that inches toward the new normal one
marker-move at a time and, in the meantime, an insight that never closes
because it is being compared against a baseline that stopped being true.

**Measured.** Nobody tags a trace with "yes, this really was slow", so there
is no label to grade `slow_span` against — the Card reports `Unmeasured` and
gates `Ready` on `spanP99MinSamples`, because what it claims is a stable
p50/p99 and a twelve-sample series has only the first of those.

**Component.** **TraceWaterfall**, unchanged.

### Culprit ranking

**Job.** Rank the processes most likely contributing to an open host CPU
anomaly.

`culprit` used to sort candidates by raw `process.cpu.percent`, keep the top
three, and drop anything under a fixed 20% floor. That drops a process that
jumped from 2% to 18% during the very spike it is supposed to explain, in
favour of one that always idles at 22% and is doing nothing unusual at all —
raw usage cannot tell "always like this" from "just like this".

**Reads are free.** The Detector already keeps a rolling Welford
mean/variance for every `process.cpu.percent` and `process.memory.rss_bytes`
series, as a side effect of running its own anomaly check on them
(`Detector.SeriesBaseline`). Once a process's own series has `minSamples` of
history, how far it has risen above its own baseline — in that series' own
standard deviations, signed — is already sitting there to ask for.

**Method.** For each process, take whichever of cpu or rss has risen further
above its own baseline. A rise of at least 1.5σ — comfortably under the 3σ
the Detector itself uses to open a warning, since this ranks a handful of
candidates against each other rather than deciding whether to alert at all —
makes it a candidate, ranked by that σ. A process whose own baseline says it
is at or below normal is excluded outright, never re-scored by its raw
value: that is precisely the "always sits at 22%" case the raw-CPU rule used
to promote over the one that actually moved. Only a process too new for
either series to have `minSamples` of history falls back to the original
rule — raw CPU over a 20% floor — which is why that rule is still in the
code, named as `Fallback` rather than replaced.

**Measured.** There is no label yet for "this process actually caused the
spike" — see the next paragraph — so the Card reports `Unmeasured`, and
`Ready` just means at least one ranking has used a real baseline rather than
the floor.

**The learned score is the follow-up, not this.** A jump that merely
preceded the host anomaly is a proxy for causation, not causation itself:
the process that jumps because it is causing the spike and the process that
jumps because the spike is starving it of CPU look identical from here. The
self-supervised label this needs — "this process's own series fell back
toward normal as the host anomaly closed" — has to be watched for a while
before a logistic weight over the same features (cpu deviation, rss
deviation) can be trained and graded against the rule above, the same way
the paging model is graded against the volume rule. Until that label exists,
the rule above is the whole model, and the Card says so rather than
borrowing a number from a different job.

**Component.** **Table** (process rows), unchanged.

### Host outlier

**Job.** Say whether the host's cpu, memory, disk and network look unusual
*together*, even when no single one of them does on its own.

`Next` used to list this as an Isolation Forest trained in `python/`, scored
POSTed back over `/api/v1/*`. That was blocked twice over: mlaas's sklearn
plugin has no IsolationForest anywhere (`plugins/sklearn/plugin.py`), and the
agent has no ingest route for a score computed elsewhere — every
`/api/v1/forseer` route is `GET`. Neither gap is worth opening for this: the
judges' original doubt was that three host percentages carry little joint
signal beyond what per-series z already tells an operator, and a stdlib
Mahalanobis check answers that question directly, on the same stream, with
the same contract every other model here keeps — no producer/consumer
problem to solve in the first place.

**Reads are free, almost.** `Detector.Observe` already runs a per-series
check on `host.cpu.percent`, `host.memory.percent` and `host.disk.percent`;
the two net inputs are the only new work, and it is arithmetic rather than a
new collector: `host.net.bytes_sent` and `host.net.bytes_recv` are
ever-growing counters (`host.go`), so the model reads each batch's delta
against the previous one rather than the raw counter, which has no
stationary distribution for a covariance to describe.

**Method.** An online mean vector and 5x5 covariance matrix — Welford's
multivariate form, the same running update the per-series check uses,
generalised from a scalar to a matrix — scored by squared Mahalanobis
distance. One mean and one covariance for the whole model: `forsight run` is
one binary per host (see `host.go`'s package comment — a DaemonSet mounts one
node's `/proc`/`/sys` into one pod), so every point a single `Engine` ever
observes already comes from that one host. Gated at `hostOutlierMinSamples`
(50), stricter than the per-series `minSamples` (12): a 5x5 covariance has
fifteen free entries to estimate, not one variance, and needs more history
before it is safe to invert. Below that, or whenever the covariance is
singular (a run of identical values in one input, most likely early on), the
fallback is exactly what already runs regardless: the per-series z-score
check on each of the five inputs.

Warning and critical are the chi-squared quantiles at 5 degrees of freedom
matching the same two-tailed-normal tail probability `warningSigma` (3σ) and
`criticalSigma` (5σ) use, so the host vector as a whole is called out only as
rarely as a single series is. Related names the input contributing most to
the distance — the largest term in the same decomposition that computes it —
the way `culprit` names the process that moved most.

**Measured, and the exit criterion.** Nobody labels "this combination of
host metrics was really unusual", so the Card reports `Unmeasured`, the same
as `culprit` and `slow_span`. What it reports instead, in Detail, is the
number the judges' doubt actually turns on: how many times this model opened
an insight while no per-series anomaly was open on any of the five inputs.
If that count stays at zero over a watched period, this model is finding
nothing the cheaper per-series checks were not already finding, and should
be retired — the roadmap item's own exit criterion, kept on the card rather
than in a document nobody rereads.

**Component.** **AlertList**, the same severity vocabulary as `anomaly`.

### Models page

The cards were already served over the API; what shipped is a page for them.
`#/models` in the dashboard puts every model on one screen: a **Table** of
what trains inside this module, with **Progress** showing each measured
accuracy against its named fallback where there is one, and the model's own
detail line where there isn't yet a label to score against; alongside it,
what `forsight` hands to a separate mlaas service — state, champion version,
held-out and live score, drift, and the buttons to train or tune it — plus
the forecast charts and the most recent severity predictions next to what
this module's own classifier said about the same line. "The agent learned
something" used to be a claim; on this page it's a number, next to the
margin it is a margin of. See [`README.md`](README.md#two-kinds-of-model) for
why the two sources sit side by side rather than merged into one table, and
[`forsight/README.md`](../forsight/README.md#mlaas-integration) for the
mlaas half.

**Component.** **Table**, **Card**, and **Progress**, all already in the
design system, plus **LineChart** for the mlaas forecasts.

### Persisted state

Every model above trains cold on every restart, which on a frequently
restarted agent means `severityMinTrained`, `thresholdCriticalMinSamples`, and every
other model's own warm-up never finish being paid for. `Snapshot() ([]byte,
error)` and `Restore([]byte) error` on `Model` (`learn.go`), alongside
`Card()`, fix that: `forsight/cmd/run.go` restores right after
`forseer.NewEngine`, before `WithSeverityFallback` or any live data touches
the engine, and writes a fresh snapshot on shutdown, next to the Badger
close.

**What persists, model by model.** Every payload is its own JSON document
with its own schema version — a version this build does not recognize, or a
payload that does not parse as that model's own shape, is a discard (a
logged, non-fatal event), never a misread. What comes back is the learned
state that gates readiness on sample count alone: naive-Bayes counts and the
trained count (`log severity`), each series' two quantile estimators and how
many points each has seen (`alert thresholds` — the one model whose entire
state is exactly what a restart used to throw away, since a calibration has
no comparison window to re-earn, and the model for which this matters most:
a page threshold needs 20 000 points, which is weeks of a real series, so an
agent restarted weekly would otherwise never learn one), Holt's level and
trend (`error-budget
forecast`), the logistic weights (`log burst paging`), each series' P²
markers (`per-endpoint latency shape`), the mean vector and covariance
(`host outlier`), and the two reporting counters (`culprit ranking`).

What does **not** come back, on every model that grades itself against a
named fallback (`log severity`, `log burst paging`): the prequential window
those two accuracies are measured over. Readiness there means "beating the
thing I replace," not "have seen enough," so restoring a stale comparison
would let a restart open already trusted on a result from a previous run
rather than one this run has actually earned — the same reasoning
`error-budget forecast`'s head-to-head win rate resets for, even though
nothing gates on it. Also reset: bookkeeping tied to a live, continuous
stream rather than to what a model has learned — `log burst paging`'s
in-flight bursts awaiting a label and the recent criticals that would label
them, and `per-endpoint latency shape`'s currently-open insights and
recent-trace buffer. A restart's own gap is of unknown length, so neither
can be resumed honestly; both start empty and rebuild from the first spans
or bursts that actually arrive. Every restore is bounded by the same caps
`Observe` already enforces — the 512-series cap, the 256-cluster/256-span-series
caps, the hashed 4096 buckets — so a snapshot can never grow a model past
where live traffic already keeps it, whether that snapshot came from this
build's own `Snapshot` or was hand-edited to carry more than any of those
caps allow. Where the live cap evicts by recency rather than just refusing
new entries — `log burst paging`'s per-cluster LRU — restoring an oversized
snapshot keeps the same most-recently-trained entries a live cache would,
not whatever subset Go's randomized map iteration happens to hand back.

**Where it lives.** One file, `<data-dir>/forseer.json`, beside the Badger
database — a top-level version for the envelope itself plus a map of each
model's stable name to its own payload, so restoring one model correctly
never depends on any other model's shape. `--store memory` deployments have
no data dir and stay cold, silently: there is nowhere durable for the
snapshot, the same reason there is nowhere durable for the metrics/logs/
spans MemoryStore holds either. The data directory belongs to the operator;
nothing here lands in this repo. See
[`forsight/README.md`](../forsight/README.md#persistent-storage---store-badger)
for the flag.

## Served by mlaas

Four more models answer to the same contract as the ones above, but they
don't live here: `forsight` trains and serves them through a separate
service, mlaas, and only proxies the result onto the Models page
(`forsight/internal/mlaas`; see
[`forsight/README.md`](../forsight/README.md#mlaas-integration) for the
flags and what happens when it's unreachable).

| Model | Job | Reads | Fallback | Score |
| --- | --- | --- | --- | --- |
| `cpu-forecast` | Say where host CPU is heading over the next hour. | `host.cpu.percent`, one-minute means | none — there is no existing forecast for it to beat | mlaas's held-out RMSE, plus RMSE over the live window of predictions scored since |
| `memory-forecast` | Say where host memory is heading over the next hour. | `host.memory.percent`, one-minute means | none | same |
| `disk-forecast` | Say where disk usage is heading, and so when it fills. | `host.disk.percent`, one-minute means | none | same |
| `log-severity` | Give a log line the level this deployment would have given it. | log lines whose source declared a level (never an inferred one) | this module's own log-severity model, above | mlaas's held-out accuracy, plus accuracy over the live window of predictions scored since |

The contract at the top of this file still holds for all four: a declared
input list, a named fallback or none, and a measured score rather than a
claimed one. What differs is where they run — a real held-out split, their
own retrain schedule, a training corpus that keeps growing — none of which
this module could do without breaking the rules it was built around.

That boundary is deliberate and it doesn't move: no model weights and no API
keys land in this repo, `forseer` stays a stdlib-only Go module, and
`forseer` never imports or calls mlaas. The integration is entirely
`forsight`'s own package; this module has no idea mlaas exists. `log-severity`
is the one row where the two meet — same job, same input, scored against
each other in the open on the Models page, not merged into one answer.

## Next

Nothing queued. Persisted state (above) was the last row on this list —
every proposal this file tracked has shipped.

## Rules

- No model weights and no API keys in this repo. Ever.
- The `forseer` module stays stdlib-only.
- Every model bounds its own state.
- Every model names a fallback and gates on readiness.
- Every model lands on a component the design system already has. Forseer
  does not invent widgets.
- A model never trains on its own output.
