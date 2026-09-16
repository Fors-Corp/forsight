import { useEffect, useRef, useState, useSyncExternalStore } from "react";

// ---------------------------------------------------------------------------
// Auth: forsight/internal/api/auth.go rejects every route but the
// dashboard's own static shell once --auth-token is set, so every fetch in
// this file goes through fetchWithAuth below instead of calling the global
// fetch directly. The token lives only in sessionStorage (cleared when the
// tab closes — never localStorage, which would outlive it) and the
// Authorization header; it is never put in a URL, a query string, or a log
// line, and this module never lets a caller print it either.
// ---------------------------------------------------------------------------

const AUTH_TOKEN_KEY = "forsight.authToken";

/** What App renders the token dialog from. `rejected` distinguishes "no
 *  token has been entered yet" (first load under --auth-token) from "the
 *  one that was just tried came back 401" (a wrong guess), so the dialog
 *  can show an error only in the second case. */
export interface AuthPromptState {
  open: boolean;
  rejected: boolean;
}

const NO_PROMPT: AuthPromptState = { open: false, rejected: false };

// Fired whenever submitAuthToken runs, so every usePoll loop on the page
// (Overview and Models each mount several) retries immediately instead of
// leaving stale "unauthorized" data on screen until its own next scheduled
// tick — which, for the slower pollers, is tens of seconds away.
const AUTH_TOKEN_SUBMITTED_EVENT = "forsight:auth-token-submitted";

let authPrompt: AuthPromptState = NO_PROMPT;
const authListeners = new Set<() => void>();

function setAuthPrompt(next: AuthPromptState) {
  authPrompt = next;
  for (const listener of authListeners) listener();
}

function subscribeAuthPrompt(listener: () => void): () => void {
  authListeners.add(listener);
  return () => {
    authListeners.delete(listener);
  };
}

/** The token dialog's state, live: opens the moment any fetchWithAuth call
 *  gets a 401, closes the moment submitAuthToken runs. */
export function useAuthPrompt(): AuthPromptState {
  return useSyncExternalStore(
    subscribeAuthPrompt,
    () => authPrompt,
    () => NO_PROMPT
  );
}

function readStoredToken(): string {
  try {
    return sessionStorage.getItem(AUTH_TOKEN_KEY) ?? "";
  } catch {
    // Private browsing, or sessionStorage disabled entirely: fall back to
    // sending no token, same as if the user hadn't typed one in yet.
    return "";
  }
}

function storeToken(token: string) {
  try {
    if (token) sessionStorage.setItem(AUTH_TOKEN_KEY, token);
    else sessionStorage.removeItem(AUTH_TOKEN_KEY);
  } catch {
    // Nothing to persist to, but fetchWithAuth still reads the in-memory
    // `token` argument's effect for the rest of this tab's life — it just
    // won't survive a reload.
  }
}

/** The token dialog's submit handler calls this. Closes the prompt
 *  optimistically, and wakes every mounted usePoll loop so the page's data
 *  arrives right away instead of waiting for each poller's own next tick.
 *  A wrong guess reopens the prompt (this time with `rejected: true`) as
 *  soon as the retried request 401s. */
export function submitAuthToken(token: string): void {
  storeToken(token);
  setAuthPrompt(NO_PROMPT);
  window.dispatchEvent(new Event(AUTH_TOKEN_SUBMITTED_EVENT));
}

/**
 * `fetch`, but with the stored bearer token attached whenever one is on
 * hand, and the token dialog opened the moment a response comes back 401 —
 * every call in this file uses this instead of the global `fetch` so no
 * poll or action can silently skip the header. A 401 also clears the
 * stored token, since it's now known bad (either never set, or rejected):
 * leaving it in place would just 401 again on the next poll with no way for
 * the dialog to tell "still waiting for the first token" from "reopened
 * after a rejection".
 */
export async function fetchWithAuth(
  input: RequestInfo | URL,
  init: RequestInit = {}
): Promise<Response> {
  const token = readStoredToken();
  const headers = new Headers(init.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(input, { ...init, headers });
  if (res.status === 401) {
    storeToken("");
    setAuthPrompt({ open: true, rejected: token !== "" });
  }
  return res;
}

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

export interface PollState<T> {
  /** The last successful response, or the hook's initial value until one lands. */
  data: T;
  /** Wall-clock time (ms) of the last 2xx response; null until the first. */
  lastSuccessAt: number | null;
  /** Wall-clock time (ms) of the last failed poll — a non-2xx status, a
   *  network error, a body the hook could not parse, or a request that
   *  outlived two intervals and was abandoned. Null until one happens, and
   *  not cleared by a later success, so `lastErrorAt > lastSuccessAt` reads
   *  as "failing right now". */
  lastErrorAt: number | null;
}

/**
 * Polls `path` every `intervalMs` and keeps the last good snapshot across
 * failures. Every hook below is one line over this.
 *
 * - The first poll is immediate. Later ones fire on the interval's wall-clock
 *   boundary (`Date.now() % intervalMs`), so every poller on the page with
 *   the same interval fires together instead of drifting apart.
 * - Nothing is fetched while the tab is hidden; a poll fires the moment it
 *   becomes visible again.
 * - One request in flight at a time. A request that has outlived two full
 *   intervals is aborted and counted as a failure, so a hung agent still
 *   advances `lastErrorAt` — the page's connection state depends on that
 *   re-render. Unmount aborts whatever is in flight.
 * - `path` may be a function, for a URL that carries the time of the
 *   request (`?since=`). It is read fresh on every poll, so a caller may
 *   change it without remounting the hook.
 * - `parse` normalizes the JSON body; throwing from it counts as a failure
 *   and keeps the previous snapshot.
 */
export function usePoll<T>(
  path: string | (() => string),
  intervalMs: number,
  initial: T,
  parse: (raw: unknown) => T = (raw) => raw as T
): PollState<T> {
  const [state, setState] = useState<PollState<T>>({
    data: initial,
    lastSuccessAt: null,
    lastErrorAt: null,
  });
  const pathRef = useRef(path);
  pathRef.current = path;
  const parseRef = useRef(parse);
  parseRef.current = parse;
  // A function path never restarts the loop; a string path does when it changes.
  const pathKey = typeof path === "string" ? path : null;

  useEffect(() => {
    let disposed = false;
    let inFlight: { controller: AbortController; startedAt: number } | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;

    async function poll() {
      if (disposed || inFlight) return;
      const controller = new AbortController();
      const request = { controller, startedAt: Date.now() };
      inFlight = request;
      try {
        const current = pathRef.current;
        const url = typeof current === "function" ? current() : current;
        const res = await fetchWithAuth(url, { signal: controller.signal });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const data = parseRef.current(await res.json());
        if (!disposed) {
          setState((prev) => ({ data, lastSuccessAt: Date.now(), lastErrorAt: prev.lastErrorAt }));
        }
      } catch {
        if (!disposed) setState((prev) => ({ ...prev, lastErrorAt: Date.now() }));
      } finally {
        if (inFlight === request) inFlight = null;
      }
    }

    function schedule() {
      timer = setTimeout(tick, intervalMs - (Date.now() % intervalMs));
    }

    function tick() {
      if (inFlight && Date.now() - inFlight.startedAt >= 2 * intervalMs) {
        inFlight.controller.abort();
        inFlight = null;
      }
      if (!document.hidden) void poll();
      schedule();
    }

    function onVisibility() {
      if (!document.hidden) void poll();
    }

    // A submitted token means the last request that just 401'd is worth
    // retrying now rather than on this poller's own next scheduled tick,
    // which for a 30s/60s poller would otherwise leave a stale
    // "unauthorized" snapshot on screen well after the dialog closes.
    function onAuthTokenSubmitted() {
      if (!document.hidden) void poll();
    }

    if (!document.hidden) void poll();
    schedule();
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener(AUTH_TOKEN_SUBMITTED_EVENT, onAuthTokenSubmitted);
    return () => {
      disposed = true;
      clearTimeout(timer);
      inFlight?.controller.abort();
      document.removeEventListener("visibilitychange", onVisibility);
      window.removeEventListener(AUTH_TOKEN_SUBMITTED_EVENT, onAuthTokenSubmitted);
    };
  }, [pathKey, intervalMs]);

  return state;
}

export interface ConnectionState {
  /** `waiting` until any poller has succeeded once; `live` while the newest
   *  success is within three intervals; `stale` after that, however many
   *  snapshots are still on screen. */
  state: "waiting" | "live" | "stale";
  /** Milliseconds since the newest success; null while waiting. */
  silentForMs: number | null;
}

/** Derives a page's connection to the agent from the pollers it mounts. Pass
 * the ones that share `intervalMs`; a slower poller would only make the
 * answer look fresher than it is. */
export function connectionState(
  polls: ReadonlyArray<PollState<unknown>>,
  intervalMs: number,
  now: number = Date.now()
): ConnectionState {
  let newest: number | null = null;
  for (const poll of polls) {
    if (poll.lastSuccessAt !== null && (newest === null || poll.lastSuccessAt > newest)) {
      newest = poll.lastSuccessAt;
    }
  }
  if (newest === null) return { state: "waiting", silentForMs: null };
  const silentForMs = now - newest;
  return { state: silentForMs > 3 * intervalMs ? "stale" : "live", silentForMs };
}

function asArray<T>(raw: unknown): T[] {
  return Array.isArray(raw) ? (raw as T[]) : [];
}

/** Polls /api/v1/metrics every `intervalMs` — the server's own MemoryStore
 * already retains the whole window, so with no `sinceMinutes` one fetch
 * returns full history for every metric name, not just the latest point. */
export function useMetrics(intervalMs: number, options?: UseMetricsOptions): PollState<Metric[]> {
  const sinceMinutes = options?.sinceMinutes;
  return usePoll<Metric[]>(
    sinceMinutes === undefined
      ? "/api/v1/metrics"
      : () =>
          `/api/v1/metrics?since=${encodeURIComponent(
            new Date(Date.now() - sinceMinutes * 60000).toISOString()
          )}`,
    intervalMs,
    [],
    asArray
  );
}

// What the Overview draws from logs and traces is bounded — a stream of
// the newest lines, a heatmap over them, one trace in the waterfall — so the
// reads are too. The store hands back the newest `limit` in the window,
// oldest-first, so under a week of retention the page stays the size it is
// under an hour. Metrics are not capped here: that endpoint mixes every
// metric name, and a global cap would truncate the CPU chart's history
// before the stat cards' newest points; sizing it needs a per-name read.
export const LOG_READ_LIMIT = 2000;
export const TRACE_READ_LIMIT = 2000;

/** Polls /api/v1/logs every `intervalMs` — the newest LOG_READ_LIMIT lines. */
export function useLogs(intervalMs: number): PollState<LogEntry[]> {
  return usePoll<LogEntry[]>(`/api/v1/logs?limit=${LOG_READ_LIMIT}`, intervalMs, [], asArray);
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
  /** The paging model's probability that a burst of this template is worth
   *  paging for. Present only while that model is ready; absent means rank
   *  by count. */
  pagingScore?: number;
}

export function useInsights(intervalMs: number): PollState<ForseerInsight[]> {
  return usePoll<ForseerInsight[]>("/api/v1/forseer/insights", intervalMs, [], asArray);
}

export function useClusters(intervalMs: number): PollState<ForseerCluster[]> {
  return usePoll<ForseerCluster[]>("/api/v1/forseer/clusters", intervalMs, [], asArray);
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

/** Polls /api/v1/traces every `intervalMs` — the newest TRACE_READ_LIMIT spans. */
export function useTraces(intervalMs: number): PollState<Span[]> {
  return usePoll<Span[]>(`/api/v1/traces?limit=${TRACE_READ_LIMIT}`, intervalMs, [], asArray);
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

const EMPTY_BUDGET: ForseerBudget = { label: "Error-log budget", consumed: 0 };

export function useBudget(intervalMs: number): PollState<ForseerBudget> {
  return usePoll<ForseerBudget>("/api/v1/forseer/budget", intervalMs, EMPTY_BUDGET, (raw) => {
    if (!raw) throw new Error("empty budget body");
    return raw as ForseerBudget;
  });
}

export interface ForseerEvent {
  id: string;
  time: string;
  title: string;
  description?: string;
  tone?: string;
}

export function useTimeline(intervalMs: number): PollState<ForseerEvent[]> {
  return usePoll<ForseerEvent[]>("/api/v1/forseer/timeline", intervalMs, [], asArray);
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
  const res = await fetchWithAuth("/api/v1/forseer/query?q=" + encodeURIComponent(q));
  if (!res.ok) return { facets: [], matched: false };
  const data = (await res.json()) as Partial<ForseerQueryResult>;
  return {
    facets: Array.isArray(data.facets) ? data.facets : [],
    matched: Boolean(data.matched),
  };
}

export function useSummary(intervalMs: number): PollState<{ enabled: boolean; summary: string }> {
  return usePoll("/api/v1/forseer/summary", intervalMs, { enabled: false, summary: "" }, (raw) => {
    const data = raw as { enabled?: boolean; summary?: string } | null;
    return { enabled: Boolean(data?.enabled), summary: data?.summary ?? "" };
  });
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
export function useForseerModels(intervalMs: number): PollState<ForseerCard[]> {
  return usePoll<ForseerCard[]>("/api/v1/forseer/models", intervalMs, [], asArray);
}

/** Polls /api/v1/mlaas/status. Returns `null` until the first successful
 * response lands — before that, the agent hasn't actually said whether
 * mlaas is configured, so a caller must not render "not configured" from a
 * guess. After the first 2xx, a later failure keeps the last snapshot
 * (same as every other hook here) rather than reverting to `null`. The
 * slices are normalized to arrays so the page can map over them without
 * null checks whatever the server sent. */
export function useMlaasStatus(intervalMs: number): PollState<MlaasStatus | null> {
  return usePoll<MlaasStatus | null>("/api/v1/mlaas/status", intervalMs, null, (raw) => {
    const data = raw as Partial<MlaasStatus> | null;
    if (!data) throw new Error("empty mlaas status body");
    return {
      ...EMPTY_MLAAS_STATUS,
      ...data,
      configured: Boolean(data.configured),
      reachable: Boolean(data.reachable),
      models: Array.isArray(data.models) ? data.models : [],
      forecasts: Array.isArray(data.forecasts) ? data.forecasts : [],
      predictions: Array.isArray(data.predictions) ? data.predictions : [],
      jobs: Array.isArray(data.jobs) ? data.jobs : [],
    };
  });
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
    const res = await fetchWithAuth(path, { method: "POST" });
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
    const res = await fetchWithAuth(`/api/v1/mlaas/models/${encodeURIComponent(name)}/predict`, {
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
    const res = await fetchWithAuth(
      "/api/v1/forseer/classify?message=" + encodeURIComponent(message)
    );
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
