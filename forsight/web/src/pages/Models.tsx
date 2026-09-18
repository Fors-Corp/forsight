import { useMemo, useState, type Ref } from "react";
import {
  Alert,
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  Heading,
  Input,
  LineChart,
  Progress,
  StatusDot,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Text,
  type BadgeProps,
  type ChartAnnotation,
  type ServiceStatus,
} from "@marcfs31/forsight";
import {
  classifyForseer,
  historyFor,
  predictMlaas,
  trainMlaas,
  tuneMlaas,
  useForseerModels,
  useMetrics,
  useMlaasStatus,
  type ClassifyResult,
  type ForseerCard,
  type Metric,
  type MlaasForecast,
  type MlaasJob,
  type MlaasModel,
  type MlaasPrediction,
  type MlaasStatus,
  type PredictResult,
} from "../api";

const timeFormat = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});

const minuteFormat = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
});

// How many one-minute buckets of history sit under a forecast. An hour of
// observed values against an hour of projection reads as one continuous
// line; more history than that squashes the forecast into the right edge.
const OBSERVED_BUCKETS = 60;

// The exact flags the CLI understands (forsight/cmd/run.go) — shown
// verbatim in the not-configured state so an operator can copy them.
const CONFIGURE_HINT =
  "forsight run --mlaas-url http://127.0.0.1:8090 --mlaas-api-key-file /path/to/mlaas/data/api_key";

function formatTime(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return timeFormat.format(d);
}

function formatMinute(ms: number): string {
  return minuteFormat.format(new Date(ms));
}

/** Three significant digits — enough to tell 0.92 from 0.93 without
 * turning an rmse of 1.23456 into noise. */
function formatScore(v: number | undefined): string {
  if (v === undefined || v === null || Number.isNaN(v)) return "—";
  return v.toPrecision(3);
}

function truncate(s: string, max: number): string {
  return s.length > max ? s.slice(0, max - 1) + "…" : s;
}

function stateVariant(state: string): BadgeProps["variant"] {
  switch (state) {
    case "ready":
      return "success";
    case "training":
      return "accent";
    case "no-champion":
      return "danger";
    default:
      // waiting-for-data, missing, and anything a newer agent adds
      return "neutral";
  }
}

/** Warning at half of mlaas's own retrain.drift_threshold, critical at or
 * above it — the same value mlaas retrains on, in Insight.Severity's
 * vocabulary, not a threshold this dashboard invents. A threshold of 0
 * (an older agent, or a classifier mlaas has not defaulted yet) never
 * reads as critical from a stray positive PSI. */
function driftVariant(max: number, threshold: number): BadgeProps["variant"] {
  if (threshold <= 0) return "neutral";
  if (max >= threshold) return "danger";
  if (max >= threshold / 2) return "warning";
  return "success";
}

function jobVariant(status: string): BadgeProps["variant"] {
  switch (status) {
    // mlaas's own terminal success status is "done" — "succeeded" is kept
    // as a harmless alias in case an older/other producer ever sends it.
    case "done":
    case "succeeded":
      return "success";
    case "failed":
      return "danger";
    case "running":
      return "accent";
    default:
      // queued, cancelled
      return "neutral";
  }
}

function connectionStatus(status: MlaasStatus): { status: ServiceStatus; label: string } {
  if (!status.configured) return { status: "unknown", label: "Not configured" };
  if (!status.reachable) {
    return { status: "outage", label: `Unreachable: ${status.lastError || "no response"}` };
  }
  return { status: "operational", label: `Connected to ${status.url ?? "mlaas"}` };
}

/** Score text for a Forseer card: the model against its fallback over the
 * graded window, so 82% means something ("vs rule 60%"). */
function forseerScoreText(card: ForseerCard): string {
  const model = `${Math.round(card.accuracy * 100)}%`;
  const rule = card.fallbackAccuracy >= 0 ? ` vs rule ${Math.round(card.fallbackAccuracy * 100)}%` : "";
  return `${model}${rule} on ${card.graded} graded`;
}

interface Bucket {
  ms: number;
  value: number;
}

/** Mean per UTC minute, oldest first, newest `limit` buckets — the same
 * bucketing the agent's exporter feeds mlaas, so the observed line and the
 * projection share an x-axis. */
function minuteBuckets(metrics: Metric[], name: string, limit: number): Bucket[] {
  const sums = new Map<number, { sum: number; n: number }>();
  for (const m of historyFor(metrics, name)) {
    const t = new Date(m.timestamp).getTime();
    if (Number.isNaN(t)) continue;
    const ms = Math.floor(t / 60000) * 60000;
    const acc = sums.get(ms) ?? { sum: 0, n: 0 };
    acc.sum += m.value;
    acc.n += 1;
    sums.set(ms, acc);
  }
  return [...sums.entries()]
    .sort((a, b) => a[0] - b[0])
    .slice(-limit)
    .map(([ms, acc]) => ({ ms, value: acc.sum / acc.n }));
}

interface ForecastChart {
  labels: string[];
  observed: Array<number | null>;
  forecast: Array<number | null>;
  annotations: ChartAnnotation[];
  /** Index of the first forecast minute — the Forecast series' `dashedFrom`,
   * so the projection is drawn dashed past the last real measurement (a
   * shape, not only a colour, so it survives grayscale and colorblind
   * vision) and the chart's own description names the minute it is
   * projected from. Undefined when the forecast has no points. */
  forecastFrom: number | undefined;
}

/** The x-axis is the sorted union of every minute timestamp either series
 * has a value for. Observed keeps going past the forecast origin (the
 * newest 60 minutes of history, not clipped to it) so an operator can read
 * the real values right alongside the projection; once observed history
 * extends past the origin the two sets of minutes interleave, so a simple
 * "observed then forecast" concatenation (the previous approach) produced a
 * non-monotonic axis. Each series is null everywhere it has no value for a
 * given minute, so the two lines still meet only at shared minutes.
 *
 * Exported for a direct unit test of the axis-merging logic — the bug this
 * fixes (a non-monotonic axis once observed history runs past the forecast
 * origin) is about the shape of the merged data, not about anything a
 * rendered chart's accessible table conveniently exposes. */
export function forecastChart(forecast: MlaasForecast, metrics: Metric[]): ForecastChart {
  const observed = minuteBuckets(metrics, forecast.metric, OBSERVED_BUCKETS);
  const observedByMs = new Map(observed.map((b) => [b.ms, b.value]));
  const forecastByMs = new Map(
    forecast.points.map((p) => [new Date(p.at).getTime(), p.value] as const)
  );

  const allMs = [...new Set([...observedByMs.keys(), ...forecastByMs.keys()])].sort((a, b) => a - b);
  const labels = allMs.map((ms) => formatMinute(ms));

  const originMs = new Date(forecast.origin).getTime();
  const originLabel = Number.isNaN(originMs) ? undefined : formatMinute(originMs);

  const forecastValues = allMs.map((ms) => forecastByMs.get(ms) ?? null);
  const firstForecast = forecastValues.findIndex((v) => v !== null);

  return {
    labels,
    observed: allMs.map((ms) => observedByMs.get(ms) ?? null),
    forecast: forecastValues,
    forecastFrom: firstForecast === -1 ? undefined : firstForecast,
    annotations:
      originLabel && labels.includes(originLabel)
        ? [{ label: originLabel, text: "forecast starts", tone: "accent" }]
        : [],
  };
}

function ForseerModelsCard({ cards }: { cards: ForseerCard[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Trained inside the agent (Forseer)</CardTitle>
      </CardHeader>
      <CardContent>
        {cards.length === 0 ? (
          <EmptyState
            title="No Forseer models yet"
            description="Forseer's models register as the first metrics and logs arrive."
          />
        ) : (
          <div className="w-full overflow-x-auto">
            <Table>
              <caption className="sr-only">
                Models trained inside the agent: job, inputs, readiness and score against the fallback
              </caption>
              <TableHeader>
                <TableRow>
                  <TableHead>Model</TableHead>
                  <TableHead>Job</TableHead>
                  <TableHead>Reads</TableHead>
                  <TableHead>Ready</TableHead>
                  <TableHead>Trained</TableHead>
                  <TableHead>Score</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {cards.map((card) => (
                  <TableRow key={card.name}>
                    <TableCell>{card.name}</TableCell>
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <span>{card.job}</span>
                        {card.fallback ? (
                          <Text as="span" size="xs" tone="muted">
                            Fallback: {card.fallback}
                          </Text>
                        ) : null}
                      </div>
                    </TableCell>
                    <TableCell className="text-fg-secondary">{card.reads.join(", ")}</TableCell>
                    <TableCell>
                      {card.ready ? (
                        <Badge variant="success">Ready</Badge>
                      ) : (
                        <Badge variant="neutral">Warming up</Badge>
                      )}
                    </TableCell>
                    <TableCell>{card.trained}</TableCell>
                    <TableCell>
                      {card.accuracy >= 0 ? (
                        <div className="flex min-w-40 flex-col gap-1">
                          <Progress
                            value={Math.round(card.accuracy * 100)}
                            aria-label={`${card.name} accuracy`}
                          />
                          <Text as="span" size="xs" tone="secondary">
                            {forseerScoreText(card)}
                          </Text>
                        </div>
                      ) : (
                        <Text as="span" size="sm" tone="muted">
                          {card.detail || "unmeasured — no labels for this job"}
                        </Text>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

interface JobFeedback {
  model: string;
  text: string;
  error: boolean;
}

function MlaasModelsCard({ status }: { status: MlaasStatus | null }) {
  // One feedback entry per model rather than a single slot: queuing an
  // action for a second model used to blow away the first model's message
  // the instant its own request settled. A later result for the same model
  // still replaces its own (earlier) entry.
  const [feedback, setFeedback] = useState<JobFeedback[]>([]);
  // Which kind of action (if any) is in flight for a given model, keyed by
  // model name. Both Train and Tune are disabled for a row whenever either
  // one is in flight — previously a single `busy` string meant clicking
  // Tune while Train was still in flight silently cleared Train's loading
  // state (and vice versa) since only one action's key could be remembered
  // at a time.
  const [busyModels, setBusyModels] = useState<Map<string, "train" | "tune">>(new Map());
  const connection = status === null ? { status: "unknown" as ServiceStatus, label: "Checking mlaas…" } : connectionStatus(status);

  async function queue(kind: "train" | "tune", model: MlaasModel) {
    setBusyModels((prev) => new Map(prev).set(model.name, kind));
    const result = kind === "train" ? await trainMlaas(model.name) : await tuneMlaas(model.name);
    setBusyModels((prev) => {
      const next = new Map(prev);
      next.delete(model.name);
      return next;
    });
    const entry: JobFeedback = result.ok
      ? {
          model: model.name,
          text: result.alreadyQueued ? `Job #${result.jobId} already queued` : `Job #${result.jobId} queued`,
          error: false,
        }
      : { model: model.name, text: result.error, error: true };
    setFeedback((prev) => [...prev.filter((f) => f.model !== model.name), entry]);
  }

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
        <CardTitle>Served by mlaas</CardTitle>
        <StatusDot status={connection.status} label={connection.label} />
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {status === null ? (
          <Text tone="muted" size="sm">
            Waiting for the first response from mlaas…
          </Text>
        ) : !status.configured ? (
          <EmptyState
            title="mlaas is not configured"
            description={
              <span className="flex flex-col gap-2">
                <span>Point the agent at a running mlaas to train these models:</span>
                <code className="break-all font-mono text-xs text-fg-secondary">{CONFIGURE_HINT}</code>
                <span>
                  or set <code className="font-mono">MLAAS_URL</code> and{" "}
                  <code className="font-mono">MLAAS_API_KEY</code>.
                </span>
              </span>
            }
          />
        ) : (
          <>
            <div className="w-full overflow-x-auto">
              <Table>
                <caption className="sr-only">
                  Models served by mlaas: state, champion version, holdout and live scores, drift, and
                  train or tune actions
                </caption>
                <TableHeader>
                  <TableRow>
                    <TableHead>Model</TableHead>
                    <TableHead>Job</TableHead>
                    <TableHead>State</TableHead>
                    <TableHead>Champion</TableHead>
                    <TableHead>Holdout</TableHead>
                    <TableHead>Live</TableHead>
                    <TableHead>New labels</TableHead>
                    <TableHead>Drift</TableHead>
                    <TableHead>Predictions</TableHead>
                    <TableHead>Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {status.models.map((model) => {
                    const inFlight = busyModels.get(model.name);
                    const cannotAct =
                      model.activeJob ||
                      model.state === "waiting-for-data" ||
                      model.state === "missing" ||
                      inFlight !== undefined;
                    return (
                      <TableRow key={model.name}>
                        <TableCell>
                          <div className="flex flex-col gap-1">
                            <span>{model.name}</span>
                            <Text as="span" size="xs" tone="muted">
                              {model.plugin} · {model.dataset} ({model.datasetRows} rows)
                            </Text>
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-col gap-1">
                            <span>{model.job}</span>
                            {model.reads.length > 0 ? (
                              <Text as="span" size="xs" tone="muted">
                                Reads: {model.reads.join(", ")}
                              </Text>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-col gap-1">
                            <Badge variant={stateVariant(model.state)}>{model.state}</Badge>
                            {model.note ? (
                              <Text as="span" size="xs" tone="muted">
                                {model.note}
                              </Text>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell>{model.champion > 0 ? `v${model.champion}` : "—"}</TableCell>
                        <TableCell>
                          {model.holdout === undefined || model.holdout === null
                            ? "—"
                            : `${model.metric} ${formatScore(model.holdout)}`}
                        </TableCell>
                        <TableCell>
                          {model.live === undefined || model.live === null
                            ? "—"
                            : `${formatScore(model.live)} over ${model.liveWindow} labels`}
                        </TableCell>
                        <TableCell>{model.newLabels}</TableCell>
                        <TableCell>
                          {model.driftMax === undefined || model.driftMax === null ? (
                            <Text as="span" size="xs" tone="muted">
                              not enough data yet
                            </Text>
                          ) : (
                            <div className="flex flex-col gap-1">
                              <Badge variant={driftVariant(model.driftMax, model.driftThreshold)}>
                                {model.driftMax.toFixed(2)} of {model.driftThreshold.toFixed(2)}
                              </Badge>
                              {model.driftFeature ? (
                                <Text as="span" size="xs" tone="muted">
                                  {model.driftFeature}
                                </Text>
                              ) : null}
                            </div>
                          )}
                        </TableCell>
                        <TableCell>{model.predictionsLogged}</TableCell>
                        <TableCell>
                          <div className="flex flex-wrap gap-2">
                            <Button
                              size="sm"
                              variant="secondary"
                              disabled={cannotAct}
                              loading={inFlight === "train"}
                              onClick={() => void queue("train", model)}
                            >
                              Train <span className="sr-only">{model.name}</span>
                            </Button>
                            <Button
                              size="sm"
                              variant="secondary"
                              disabled={cannotAct}
                              loading={inFlight === "tune"}
                              onClick={() => void queue("tune", model)}
                            >
                              Tune <span className="sr-only">{model.name}</span>
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
            {feedback.length > 0 ? (
              <div className="flex flex-col gap-2">
                {feedback.map((f) => (
                  <Alert key={f.model} variant={f.error ? "danger" : "success"} title={f.model}>
                    {f.text}
                  </Alert>
                ))}
              </div>
            ) : null}
            <Text size="sm" tone="muted">
              Last sync: {formatTime(status.lastSync)}
              {status.lastError ? ` · Last error: ${status.lastError}` : ""}
            </Text>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function ForecastsCard({ forecasts, metrics }: { forecasts: MlaasForecast[]; metrics: Metric[] }) {
  const charts = useMemo(
    () => forecasts.map((f) => ({ forecast: f, chart: forecastChart(f, metrics) })),
    [forecasts, metrics]
  );
  return (
    <Card>
      <CardHeader>
        <CardTitle>Forecasts</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        {charts.length === 0 ? (
          <EmptyState
            title="No forecasts yet"
            description="Forecasts appear once a forecast model has a champion"
          />
        ) : (
          charts.map(({ forecast, chart }) => (
            <div key={forecast.model} className="flex flex-col gap-2">
              <Text weight="medium">{forecast.model}</Text>
              <Text size="sm" tone="muted">
                {forecast.metric} · projection from {formatTime(forecast.origin)}
              </Text>
              <LineChart
                label={`${forecast.metric}: observed one-minute means and the forecast from ${forecast.model}`}
                labels={chart.labels}
                series={[
                  { name: "Observed", values: chart.observed },
                  { name: "Forecast", values: chart.forecast, dashedFrom: chart.forecastFrom },
                ]}
                annotations={chart.annotations}
              />
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}

/** Only mounted when there's at least one forecast to chart, so its
 * `useMetrics` poll — the one hook on this page that actually needs metric
 * history — never runs (and the page never hits /api/v1/metrics at all) on
 * a deployment with no forecast model yet. Scoped to the last hour: that's
 * all `forecastChart` ever charts (`OBSERVED_BUCKETS` one-minute buckets),
 * so there's no reason to pull the server's entire retained window every
 * poll just to throw most of it away. */
function ForecastsSection({ forecasts }: { forecasts: MlaasForecast[] }) {
  const metrics = useMetrics(10000, { sinceMinutes: 60 }).data;
  return <ForecastsCard forecasts={forecasts} metrics={metrics} />;
}

/** A model's answer next to the declared level — a Badge when it disagreed,
 * plain text when it matched, "—" when it had nothing to say. The Badge's
 * color is not the only signal of disagreement: a visually hidden span
 * spells it out for anyone who can't rely on color (or on the Badge's shape
 * alone) to tell it apart from an agreeing plain-text answer. */
function AnswerCell({ answer, declared }: { answer: string | undefined; declared: string }) {
  if (!answer) return <>—</>;
  if (answer !== declared) {
    return (
      <Badge variant="danger">
        {answer}
        <span className="sr-only"> (disagrees with declared {declared})</span>
      </Badge>
    );
  }
  return <>{answer}</>;
}

function PredictionsCard({ predictions }: { predictions: MlaasPrediction[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent predictions</CardTitle>
      </CardHeader>
      <CardContent>
        {predictions.length === 0 ? (
          <EmptyState
            title="No predictions yet"
            description="Log lines that arrive with a declared level are sent through both severity models; the answers show up here."
          />
        ) : (
          <div className="w-full overflow-x-auto">
            <Table>
              <caption className="sr-only">
                Recent log lines with the level the source declared and what each severity model
                answered; a marked answer disagreed with the declared level
              </caption>
              <TableHeader>
                <TableRow>
                  <TableHead>Time</TableHead>
                  <TableHead>Message</TableHead>
                  <TableHead>Declared</TableHead>
                  <TableHead>Forseer</TableHead>
                  <TableHead>mlaas</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {predictions.map((p, i) => (
                  <TableRow key={`${p.at}-${i}`}>
                    <TableCell>{formatTime(p.at)}</TableCell>
                    <TableCell title={p.message.length > 120 ? p.message : undefined}>
                      {truncate(p.message, 120)}
                    </TableCell>
                    <TableCell>{p.declared}</TableCell>
                    <TableCell>
                      <AnswerCell answer={p.forseer} declared={p.declared} />
                    </TableCell>
                    <TableCell>
                      <AnswerCell answer={p.mlaas} declared={p.declared} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

interface ClassifyAnswers {
  forseer: ClassifyResult | null;
  forseerError: string | null;
  mlaas: PredictResult | null;
  mlaasError: string | null;
  /** True when the mlaas half was skipped because no severity model is ready. */
  mlaasSkipped: boolean;
}

function forseerAnswerDetail(r: ClassifyResult): string {
  return r.source === "model" ? "model" : r.ready ? "rule" : "rule — model not ready";
}

function TryModelsCard({ status }: { status: MlaasStatus | null }) {
  const [line, setLine] = useState("");
  const [busy, setBusy] = useState(false);
  const [answers, setAnswers] = useState<ClassifyAnswers | null>(null);
  // Still waiting on the first /api/v1/mlaas/status response: there's no
  // severity model to check yet, distinct from "checked, and mlaas has
  // none" — the copy below and the skipped-row reason tell them apart.
  const pending = status === null;
  const severityModel = status?.models.find((m) => m.task === "classification");
  const mlaasReady = !pending && severityModel !== undefined && severityModel.state === "ready";

  async function submit() {
    const message = line.trim();
    if (!message) return;
    setBusy(true);
    const [forseer, mlaas] = await Promise.all([
      classifyForseer(message),
      mlaasReady && severityModel ? predictMlaas(severityModel.name, [{ message }]) : Promise.resolve(null),
    ]);
    setBusy(false);
    setAnswers({
      forseer: forseer.ok ? forseer : null,
      forseerError: forseer.ok ? null : forseer.error,
      mlaas: mlaas && mlaas.ok ? mlaas : null,
      mlaasError: mlaas && !mlaas.ok ? mlaas.error : null,
      mlaasSkipped: mlaas === null,
    });
  }

  const mlaasRow = answers?.mlaas?.predictions[0];
  const unusual = mlaasRow?.unusual
    ? Object.entries(mlaasRow.unusual)
        .map(([k, v]) => `${k}: ${v}`)
        .join("; ")
    : "";

  return (
    <Card>
      <CardHeader>
        <CardTitle>Try the severity models</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Text size="sm" tone="secondary">
          Paste a log line and see the level each model would give it.
          {pending
            ? " Checking mlaas — only Forseer answers until it responds."
            : !mlaasReady
              ? severityModel
                ? ` The mlaas model is ${severityModel.state}; only Forseer answers until it has a champion.`
                : " mlaas has no severity model yet; only Forseer answers."
              : ""}
        </Text>
        <form
          className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-end"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <Input
            className="min-w-0 flex-1"
            aria-label="Log line"
            value={line}
            onChange={(event) => setLine(event.target.value)}
            placeholder="connection refused to db-primary:5432"
          />
          <Button type="submit" loading={busy}>
            Classify
          </Button>
        </form>
        {answers?.forseerError ? (
          <Alert variant="danger" title="Forseer">
            {answers.forseerError}
          </Alert>
        ) : null}
        {answers?.mlaasError ? (
          <Alert variant="danger" title="mlaas">
            {answers.mlaasError}
          </Alert>
        ) : null}
        {answers && (answers.forseer || answers.mlaas || answers.mlaasSkipped) ? (
          <div className="w-full overflow-x-auto">
            <Table>
              <caption className="sr-only">The level each severity model gave the submitted log line</caption>
              <TableHeader>
                <TableRow>
                  <TableHead>Model</TableHead>
                  <TableHead>Severity</TableHead>
                  <TableHead>How</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {answers.forseer ? (
                  <TableRow>
                    <TableCell>Forseer</TableCell>
                    <TableCell>{answers.forseer.severity}</TableCell>
                    <TableCell className="text-fg-secondary">{forseerAnswerDetail(answers.forseer)}</TableCell>
                  </TableRow>
                ) : null}
                {answers.mlaas ? (
                  <TableRow>
                    <TableCell>mlaas</TableCell>
                    <TableCell>{mlaasRow?.value ?? "—"}</TableCell>
                    <TableCell className="text-fg-secondary">
                      v{answers.mlaas.version}
                      {unusual ? ` · unusual — ${unusual}` : ""}
                    </TableCell>
                  </TableRow>
                ) : answers.mlaasSkipped ? (
                  <TableRow>
                    <TableCell>mlaas</TableCell>
                    <TableCell>—</TableCell>
                    <TableCell className="text-fg-secondary">{pending ? "checking" : "not ready"}</TableCell>
                  </TableRow>
                ) : null}
              </TableBody>
            </Table>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function JobsCard({ jobs }: { jobs: MlaasJob[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent jobs</CardTitle>
      </CardHeader>
      <CardContent>
        {jobs.length === 0 ? (
          <EmptyState
            title="No jobs yet"
            description="Training and tuning jobs mlaas ran for these models are listed here."
          />
        ) : (
          <div className="w-full overflow-x-auto">
            <Table>
              <caption className="sr-only">Recent mlaas training and tuning jobs for the managed models</caption>
              <TableHeader>
                <TableRow>
                  <TableHead>Id</TableHead>
                  <TableHead>Model</TableHead>
                  <TableHead>Kind</TableHead>
                  <TableHead>Trigger</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Started</TableHead>
                  <TableHead>Finished</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {jobs.map((job) => (
                  <TableRow key={job.id}>
                    <TableCell>{job.id}</TableCell>
                    <TableCell>{job.model}</TableCell>
                    <TableCell>{job.kind}</TableCell>
                    <TableCell>{job.trigger}</TableCell>
                    <TableCell>
                      <Badge variant={jobVariant(job.status)}>{job.status}</Badge>
                    </TableCell>
                    <TableCell>{formatTime(job.startedAt)}</TableCell>
                    <TableCell>{formatTime(job.finishedAt)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

interface ModelsProps {
  /** Forwarded to the page's <h1> so App can move focus onto it after a
   * route change (skipping the very first mount) per the ARIA APG
   * client-navigation pattern — see App.tsx. Optional so the page's own
   * tests can still render it standalone. */
  headingRef?: Ref<HTMLHeadingElement>;
}

/**
 * The Models page: what the agent has learned, from both places it learns.
 * Forseer's models train inside the binary and gate on a fallback; mlaas's
 * train in a separate service with a held-out score and retrain themselves.
 * Both kinds sit on one page so an operator can compare them on the same
 * job (log severity) and see the forecasts the agent cannot make on its own.
 */
export default function Models({ headingRef }: ModelsProps = {}) {
  const cards = useForseerModels(10000).data;
  const status = useMlaasStatus(10000).data;
  const forecasts = status?.forecasts ?? [];

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-2">
        <Heading as="h1" size="xl" ref={headingRef} tabIndex={-1}>
          Models
        </Heading>
        <Text tone="secondary">
          Two kinds of model serve this agent. Forseer&apos;s train inside the agent on the ingest
          path and gate on a fallback until they have seen enough of this deployment. mlaas&apos;s
          train in a separate service on the agent&apos;s exported data, carry a held-out score, and
          retrain themselves as new labels arrive.
        </Text>
      </header>

      <ForseerModelsCard cards={cards} />
      <MlaasModelsCard status={status} />
      {forecasts.length > 0 ? (
        <ForecastsSection forecasts={forecasts} />
      ) : (
        <ForecastsCard forecasts={[]} metrics={[]} />
      )}
      <PredictionsCard predictions={status?.predictions ?? []} />
      <TryModelsCard status={status} />
      <JobsCard jobs={status?.jobs ?? []} />
    </div>
  );
}
