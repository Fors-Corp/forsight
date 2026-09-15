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

**Method.** Robbins-Monro stochastic approximation, one line:
`t ← t · (1 + step · (exceeded − target))`. A point above the threshold pushes
it up, every point below nudges it down, and those balance exactly at the
target quantile — with four floats per series and no assumption about the
shape of the distribution.

The update is multiplicative and the step does not decay. Both were arrived at
by watching the obvious version fail: an additive step has to be chosen
against a scale nobody knows in advance, and a decaying one died long before a
threshold starting at 3 could walk out to the one-in-ten-thousand tail — 2.8%
of points still alerting after forty thousand samples, against a 0.1% budget.

**Measured** over 40,000 points per series, after a 20,000-point warm-up:

| Series | Fixed 3σ alerts on | Learned alerts on | Learned threshold |
| --- | --- | --- | --- |
| well-behaved | 0.25% of points | 0.08% | 3.41 |
| heavy-tailed | 5.10% of points | 0.33% | 11.94 |

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

Ordered by what each one is worth against what it costs. Every row keeps the
rules above: declared inputs, a readiness gate, a named fallback, and an
existing Forsight component to land on.

### 1. Is this log cluster worth paging

**Job.** Rank Drain-lite clusters by whether a burst of this template has
ever coincided with something that mattered.

`log_burst` fires on volume, so a chatty debug template that triples is
indistinguishable from an exception that triples. Cluster features
(severity mix, whether an insight opened within the window, whether the
error budget moved) are enough for a small logistic regression, and the
labels come from the agent's own insight stream.

*Reads: cluster severity mix, burst shape, co-occurring insights. Fallback:
the current volume ratio. Component: **BarList**.*

### 2. Per-endpoint latency shape

**Job.** Decide what slow means for one endpoint.

`slow_span` already compares an endpoint to itself, but with a z-score, which
assumes a shape latency does not have — it is long-tailed, so the mean sits
above the median and the tail is wide by nature. A streaming quantile per
`(service, span name)` describes it properly, and "slower than this endpoint's
own p99" is a sentence an on-call can act on.

*Reads: durations for one (service, span name). Fallback: the current
z-score. Component: **TraceWaterfall**.*

### 3. Persist what has been learned

A model that resets on restart has to re-earn its readiness every deploy,
which on a frequently-restarted agent means it is never ready. The state is
small and JSON-serialisable; it belongs next to the Badger database under
`--data-dir`, written on shutdown and restored on start.

Two things this must get right: the snapshot is versioned, so a model whose
shape changed discards an old snapshot rather than misreading it; and the
data directory belongs to the operator, never to this repo.

*Applies to every model.*

## Rules

- No model weights and no API keys in this repo. Ever.
- The `forseer` module stays stdlib-only.
- Every model bounds its own state.
- Every model names a fallback and gates on readiness.
- Every model lands on a component the design system already has. Forseer
  does not invent widgets.
- A model never trains on its own output.
