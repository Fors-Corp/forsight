import { useEffect, useState } from "react";

export interface Metric {
  name: string;
  value: number;
  timestamp: string;
  labels?: Record<string, string>;
}

export type LogSeverity = "debug" | "info" | "warn" | "error";

export interface LogEntry {
  timestamp: string;
  severity: LogSeverity;
  source: string;
  message: string;
  labels?: Record<string, string>;
}

export interface UseMetricsOptions {
  /** When set, each poll asks for only the last `sinceMinutes` of history
   * (`?since=<RFC3339>`, recomputed at every poll so the window keeps
   * sliding) instead of the server's whole retained window — for a caller
   * that only ever charts a bounded recent slice, so it isn't re-fetching
   * and re-serializing metrics it will never use. */
  sinceMinutes?: number;
}

/** Polls /api/v1/metrics every `intervalMs` — the server's own MemoryStore
 * already retains the whole window, so with no `sinceMinutes` one fetch
 * returns full history for every metric name, not just the latest point. */
export function useMetrics(intervalMs: number, options?: UseMetricsOptions): Metric[] {
  const [metrics, setMetrics] = useState<Metric[]>([]);
  const sinceMinutes = options?.sinceMinutes;

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const url =
          sinceMinutes === undefined
            ? "/api/v1/metrics"
            : `/api/v1/metrics?since=${encodeURIComponent(
                new Date(Date.now() - sinceMinutes * 60000).toISOString()
              )}`;
        const res = await fetch(url);
        if (!res.ok) return;
        const data: Metric[] = await res.json();
        if (!cancelled) setMetrics(data);
      } catch {
        // Transient fetch failure — keep showing the last good snapshot
        // rather than clearing the dashboard to empty.
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs, sinceMinutes]);

  return metrics;
}

/** Polls /api/v1/logs every `intervalMs` — same retention window as metrics. */
export function useLogs(intervalMs: number): LogEntry[] {
  const [logs, setLogs] = useState<LogEntry[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/logs");
        if (!res.ok) return;
        const data: LogEntry[] = await res.json();
        if (!cancelled) setLogs(data);
      } catch {
        // Keep the last good snapshot on transient failure.
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return logs;
}

/** Every point for one metric name, oldest first (LineChart expects that order). */
export function historyFor(metrics: Metric[], name: string): Metric[] {
  return metrics
    .filter((m) => m.name === name)
    .sort((a, b) => a.timestamp.localeCompare(b.timestamp));
}

/** The most recent value for a metric name, or undefined if none yet. */
export function latestValue(metrics: Metric[], name: string): number | undefined {
  const points = historyFor(metrics, name);
  return points.length > 0 ? points[points.length - 1].value : undefined;
}

export interface ContainerRow {
  id: string;
  name: string;
  image: string;
  cpuPercent?: number;
  memoryPercent?: number;
}

/** Groups the docker.* metrics by container into one row per container. */
export function containerRows(metrics: Metric[]): ContainerRow[] {
  const byId = new Map<string, ContainerRow>();
  for (const m of metrics) {
    if (!m.labels?.container_id) continue;
    const id = m.labels.container_id;
    const row = byId.get(id) ?? {
      id,
      name: m.labels.container_name ?? id,
      image: m.labels.image ?? "",
    };
    if (m.name === "docker.cpu.percent") row.cpuPercent = m.value;
    if (m.name === "docker.memory.percent") row.memoryPercent = m.value;
    byId.set(id, row);
  }
  return [...byId.values()].sort((a, b) => a.name.localeCompare(b.name));
}

export interface ProcessRow {
  pid: string;
  name: string;
  cpuPercent?: number;
  rssBytes?: number;
}

/** Groups process.* metrics by pid into one row per process. */
export function processRows(metrics: Metric[]): ProcessRow[] {
  const byPid = new Map<string, ProcessRow>();
  for (const m of metrics) {
    if (!m.labels?.pid) continue;
    const pid = m.labels.pid;
    const row = byPid.get(pid) ?? { pid, name: m.labels.name ?? pid };
    if (m.name === "process.cpu.percent") row.cpuPercent = m.value;
    if (m.name === "process.memory.rss_bytes") row.rssBytes = m.value;
    byPid.set(pid, row);
  }
  return [...byPid.values()].sort((a, b) => (b.cpuPercent ?? 0) - (a.cpuPercent ?? 0));
}

export interface ForseerInsight {
  id: string;
  kind?: string;
  severity: string;
  title: string;
  description?: string;
  source?: string;
  metric?: string;
  value?: number;
  time: string;
  related?: string[];
}

export interface ForseerCluster {
  id: string;
  template: string;
  source: string;
  count: number;
  errorCount: number;
  lastSeen: string;
  sample: string;
}

export function useInsights(intervalMs: number): ForseerInsight[] {
  const [items, setItems] = useState<ForseerInsight[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/forseer/insights");
        if (!res.ok) return;
        const data: ForseerInsight[] = await res.json();
        if (!cancelled) setItems(Array.isArray(data) ? data : []);
      } catch {
        // keep last snapshot
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return items;
}

export function useClusters(intervalMs: number): ForseerCluster[] {
  const [items, setItems] = useState<ForseerCluster[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/forseer/clusters");
        if (!res.ok) return;
        const data: ForseerCluster[] = await res.json();
        if (!cancelled) setItems(Array.isArray(data) ? data : []);
      } catch {
        // keep last snapshot
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return items;
}

export interface Span {
  traceId: string;
  spanId: string;
  parentId?: string;
  name: string;
  service: string;
  start: string;
  duration: number;
  status: string;
}

export function useTraces(intervalMs: number): Span[] {
  const [items, setItems] = useState<Span[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/traces");
        if (!res.ok) return;
        const data: Span[] = await res.json();
        if (!cancelled) setItems(Array.isArray(data) ? data : []);
      } catch {
        // keep last snapshot
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return items;
}

export interface ForseerBudget {
  label: string;
  consumed: number;
  caption?: string;
  /** When the budget is projected to exhaust, e.g. "exhausted in 40 minutes
   *  to 2 hours" — already folded into `caption`, present here too so a
   *  consumer that wants just the projection doesn't have to parse it back
   *  out of the caption string. */
  forecast?: string;
  errors?: number;
  total?: number;
  /** Target error-log rate the budget burns against, e.g. 0.01 for 1%. */
  slo?: number;
  warningAt?: number;
  dangerAt?: number;
}

export function useBudget(intervalMs: number): ForseerBudget {
  const [state, setState] = useState<ForseerBudget>({ label: "Error-log budget", consumed: 0 });

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/forseer/budget");
        if (!res.ok) return;
        const data = (await res.json()) as ForseerBudget;
        if (!cancelled && data) setState(data);
      } catch {
        // keep last snapshot
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return state;
}

export interface ForseerEvent {
  id: string;
  time: string;
  title: string;
  description?: string;
  tone?: string;
}

export function useTimeline(intervalMs: number): ForseerEvent[] {
  const [items, setItems] = useState<ForseerEvent[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/forseer/timeline");
        if (!res.ok) return;
        const data: ForseerEvent[] = await res.json();
        if (!cancelled) setItems(Array.isArray(data) ? data : []);
      } catch {
        // keep last snapshot
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return items;
}

export interface ForseerQueryFacet {
  key: string;
  label: string;
  value: string;
}

export interface ForseerQueryResult {
  facets: ForseerQueryFacet[];
  /** Whether the phrase was recognized at all. False for both "nothing
   * typed" and "typed something this grammar doesn't understand" — callers
   * that need to tell those apart should check the query text themselves
   * before calling this. */
  matched: boolean;
}

export async function queryForseer(q: string): Promise<ForseerQueryResult> {
  const res = await fetch("/api/v1/forseer/query?q=" + encodeURIComponent(q));
  if (!res.ok) return { facets: [], matched: false };
  const data = (await res.json()) as Partial<ForseerQueryResult>;
  return {
    facets: Array.isArray(data.facets) ? data.facets : [],
    matched: Boolean(data.matched),
  };
}

export function useSummary(intervalMs: number): { enabled: boolean; summary: string } {
  const [state, setState] = useState({ enabled: false, summary: "" });

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/forseer/summary");
        if (!res.ok) return;
        const data = (await res.json()) as { enabled?: boolean; summary?: string };
        if (!cancelled) {
          setState({ enabled: Boolean(data.enabled), summary: data.summary ?? "" });
        }
      } catch {
        // keep last snapshot
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return state;
}

// ---------------------------------------------------------------------------
// Models page: Forseer's in-binary cards and the mlaas integration. The
// types below mirror forseer.Card (forseer/learn.go) and the JSON of
// forsight/internal/mlaas/types.go field for field — the Go side is the
// contract, so change it there first.
// ---------------------------------------------------------------------------

/** One in-binary model, as GET /api/v1/forseer/models describes it. */
export interface ForseerCard {
  name: string;
  job: string;
  reads: string[];
  fallback: string;
  ready: boolean;
  trained: number;
  /** Share of recent predictions that were right; -1 means the job has no
   * labels to grade against (forseer.Unmeasured), not that the model is bad. */
  accuracy: number;
  /** The fallback's score over the same window; -1 when there is nothing to compare. */
  fallbackAccuracy: number;
  graded: number;
  detail?: string;
}

export type MlaasState = "waiting-for-data" | "missing" | "training" | "no-champion" | "ready";

export interface MlaasModel {
  name: string;
  job: string;
  task: string;
  plugin: string;
  dataset: string;
  datasetRows: number;
  reads: string[];
  state: MlaasState | string;
  /** Serving version number, 0 while there is none. */
  champion: number;
  metric: string;
  holdout?: number;
  live?: number;
  liveWindow: number;
  newLabels: number;
  driftMax: number;
  activeJob: boolean;
  predictionsLogged: number;
  lastRetrainAt?: string;
  note?: string;
}

export interface MlaasForecastPoint {
  at: string;
  value: number;
}

export interface MlaasForecast {
  model: string;
  /** The agent's metric name the series comes from, e.g. "host.cpu.percent". */
  metric: string;
  origin: string;
  points: MlaasForecastPoint[];
}

export interface MlaasPrediction {
  at: string;
  message: string;
  declared: string;
  /** Empty when the in-binary model was not ready to answer. */
  forseer?: string;
  mlaas: string;
}

export interface MlaasJob {
  id: number;
  model: string;
  kind: string;
  trigger: string;
  status: string;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
}

export interface MlaasStatus {
  configured: boolean;
  url?: string;
  reachable: boolean;
  checkedAt?: string;
  lastSync?: string;
  lastError?: string;
  models: MlaasModel[];
  forecasts: MlaasForecast[];
  predictions: MlaasPrediction[];
  jobs: MlaasJob[];
}

/** What POST train/tune answer: the job queued, or the one already waiting. */
export interface MlaasJobRef {
  jobId: number;
  alreadyQueued: boolean;
}

export interface PredictedRow {
  value: string;
  /** Per-feature "this input looked unlike the training data" notes. */
  unusual?: Record<string, string>;
}

export interface PredictResult {
  model: string;
  version: number;
  predictions: PredictedRow[];
}

export interface ClassifyResult {
  severity: string;
  /** "model" when the in-binary model answered, "rule" when the fallback did. */
  source: "model" | "rule";
  ready: boolean;
}

const EMPTY_MLAAS_STATUS: Omit<MlaasStatus, "configured" | "reachable"> = {
  models: [],
  forecasts: [],
  predictions: [],
  jobs: [],
};

/** Polls /api/v1/forseer/models — same shape as the other hooks: a failed
 * poll keeps the last snapshot rather than blanking the page. */
export function useForseerModels(intervalMs: number): ForseerCard[] {
  const [items, setItems] = useState<ForseerCard[]>([]);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/forseer/models");
        if (!res.ok) return;
        const data: ForseerCard[] = await res.json();
        if (!cancelled) setItems(Array.isArray(data) ? data : []);
      } catch {
        // keep last snapshot
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return items;
}

/** Polls /api/v1/mlaas/status. Returns `null` until the first successful
 * response lands — before that, the agent hasn't actually said whether
 * mlaas is configured, so a caller must not render "not configured" from a
 * guess. After the first 2xx, a later failure keeps the last snapshot
 * (same as every other hook here) rather than reverting to `null`. The
 * slices are normalized to arrays so the page can map over them without
 * null checks whatever the server sent. */
export function useMlaasStatus(intervalMs: number): MlaasStatus | null {
  const [state, setState] = useState<MlaasStatus | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function poll() {
      try {
        const res = await fetch("/api/v1/mlaas/status");
        if (!res.ok) return;
        const data = (await res.json()) as Partial<MlaasStatus> | null;
        if (!cancelled && data) {
          setState({
            ...EMPTY_MLAAS_STATUS,
            ...data,
            configured: Boolean(data.configured),
            reachable: Boolean(data.reachable),
            models: Array.isArray(data.models) ? data.models : [],
            forecasts: Array.isArray(data.forecasts) ? data.forecasts : [],
            predictions: Array.isArray(data.predictions) ? data.predictions : [],
            jobs: Array.isArray(data.jobs) ? data.jobs : [],
          });
        }
      } catch {
        // keep last snapshot
      }
    }

    poll();
    const id = setInterval(poll, intervalMs);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [intervalMs]);

  return state;
}

/** A failed call, with the server's `{"error": ...}` message when it sent
 * one, or a generic line when the network itself failed. */
export type ApiFailure = { ok: false; error: string };

/** Reads an error body the way the agent writes them (`writeJSON` with an
 * `error` key) and falls back to the status line, so the page always has
 * a sentence to show rather than "undefined". */
async function errorFrom(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: unknown };
    if (body && typeof body.error === "string" && body.error) return body.error;
  } catch {
    // not JSON — fall through to the status line
  }
  return res.statusText ? `${res.status} ${res.statusText}` : `HTTP ${res.status}`;
}

async function postJob(path: string): Promise<({ ok: true } & MlaasJobRef) | ApiFailure> {
  try {
    const res = await fetch(path, { method: "POST" });
    if (!res.ok) return { ok: false, error: await errorFrom(res) };
    const data = (await res.json()) as Partial<MlaasJobRef>;
    return { ok: true, jobId: Number(data.jobId ?? 0), alreadyQueued: Boolean(data.alreadyQueued) };
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : "request failed" };
  }
}

/** POST /api/v1/mlaas/models/{name}/train. Never throws. */
export function trainMlaas(name: string): Promise<({ ok: true } & MlaasJobRef) | ApiFailure> {
  return postJob(`/api/v1/mlaas/models/${encodeURIComponent(name)}/train`);
}

/** POST /api/v1/mlaas/models/{name}/tune. Never throws. */
export function tuneMlaas(name: string): Promise<({ ok: true } & MlaasJobRef) | ApiFailure> {
  return postJob(`/api/v1/mlaas/models/${encodeURIComponent(name)}/tune`);
}

/** POST /api/v1/mlaas/models/{name}/predict with `{"rows": rows}`. Never throws. */
export async function predictMlaas(
  name: string,
  rows: Array<Record<string, unknown>>
): Promise<({ ok: true } & PredictResult) | ApiFailure> {
  try {
    const res = await fetch(`/api/v1/mlaas/models/${encodeURIComponent(name)}/predict`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ rows }),
    });
    if (!res.ok) return { ok: false, error: await errorFrom(res) };
    const data = (await res.json()) as Partial<PredictResult>;
    return {
      ok: true,
      model: data.model ?? name,
      version: Number(data.version ?? 0),
      predictions: Array.isArray(data.predictions) ? data.predictions : [],
    };
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : "request failed" };
  }
}

/** GET /api/v1/forseer/classify?message= — the in-binary answer, or the
 * rule's when the model is not ready. Never throws. */
export async function classifyForseer(
  message: string
): Promise<({ ok: true } & ClassifyResult) | ApiFailure> {
  try {
    const res = await fetch("/api/v1/forseer/classify?message=" + encodeURIComponent(message));
    if (!res.ok) return { ok: false, error: await errorFrom(res) };
    const data = (await res.json()) as Partial<ClassifyResult>;
    return {
      ok: true,
      severity: data.severity ?? "",
      source: data.source === "model" ? "model" : "rule",
      ready: Boolean(data.ready),
    };
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : "request failed" };
  }
}
