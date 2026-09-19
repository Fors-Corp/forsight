import {
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type KeyboardEvent,
  type Ref,
  type SetStateAction,
} from "react";
import {
  Heading,
  Text,
  StatCard,
  TimeRange,
  type TimeRangeOption,
  LineChart,
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  StatusDot,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  EmptyState,
  LogStream,
  AlertList,
  Timeline,
  BarList,
  ErrorBudget,
  FilterBar,
  Heatmap,
  TraceWaterfall,
  Input,
  Button,
  UptimeBar,
  Badge,
  type LogEntry as StreamLogEntry,
  type AlertListItem,
  type AlertSeverity,
  type TimelineItem,
  type TimelineTone,
  type ServiceStatus,
  type FilterBarFacet,
  type FilterBarOption,
  type TraceSpan,
  type UptimeSegment,
  type BadgeProps,
} from "@marcfs31/forsight";
import {
  connectionState,
  type ConnectionState,
  useMetrics,
  useLogs,
  useInsights,
  useClusters,
  useSummary,
  useTraces,
  useBudget,
  useTimeline,
  queryForseer,
  historyFor,
  latestValue,
  containerRows,
  processRows,
  type Metric,
  type LogEntry,
  type ForseerInsight,
  type ForseerEvent,
  type ForseerBudget,
  type Span,
} from "../api";

const timeLabelFormat = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

const ONE_DAY_MS = 24 * 60 * 60_000;

// Same precision as timeLabelFormat, plus the day — "14:02:10" alone is
// ambiguous once a probe strip's segments span more than one day.
const dayTimeLabelFormat = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

// Neutral helper text for the "Ask Forseer" box — spells out the grammar
// ParseQuery actually understands (forseer/query.go) so users aren't
// guessing at a hidden vocabulary. "critical" is called out explicitly
// since it's the word AlertList/Timeline elsewhere on this dashboard train
// users to type.
const QUERY_HINT =
  'Understands error/fail/fatal/critical/severe, warn/warning, debug, and "from <source>" — e.g. "critical from checkout-api"';
const QUERY_NOT_UNDERSTOOD =
  'Didn\'t recognize that phrase — try error/warn/debug/critical, optionally "from <source>"';

const percentFormat = (value: number) => `${Math.round(value * 100)}%`;

// The ranges the Overview can show. The agent's default retention is one
// hour, which is why 1h is the default here.
const TIME_RANGES: Array<TimeRangeOption & { ms: number }> = [
  { value: "15m", label: "15m", description: "Last 15 minutes", ms: 15 * 60_000 },
  { value: "1h", label: "1h", description: "Last 1 hour", ms: 60 * 60_000 },
  { value: "6h", label: "6h", description: "Last 6 hours", ms: 6 * 60 * 60_000 },
  { value: "24h", label: "24h", description: "Last 24 hours", ms: 24 * 60 * 60_000 },
  { value: "7d", label: "7d", description: "Last 7 days", ms: 7 * 24 * 60 * 60_000 },
];
const DEFAULT_TIME_RANGE = "1h";

// The three host stats CPU already got a chart for (item 9). Memory and disk
// get the same treatment here: a Sparkline on the StatCard, and a click that
// repoints the one big LineChart at that metric's history instead of CPU's.
type ChartMetric = "cpu" | "memory" | "disk";
const CHART_METRICS: Array<{ key: ChartMetric; metricName: string; label: string }> = [
  { key: "cpu", metricName: "host.cpu.percent", label: "Host CPU" },
  { key: "memory", metricName: "host.memory.percent", label: "Host memory" },
  { key: "disk", metricName: "host.disk.percent", label: "Host disk" },
];

/** In range, or undated — a record with no parseable timestamp is kept
 *  rather than silently dropped by a filter it cannot be judged against. */
function sinceOrUndated(timestamp: string, since: number): boolean {
  const t = Date.parse(timestamp);
  return Number.isNaN(t) || t >= since;
}

/**
 * What offeredTimeRanges should be told the store holds, now that nothing
 * reads the whole retained history to measure that directly: the age of the
 * oldest timestamp among the ranged reads that actually landed (the chart
 * histories, plus logs — logs are read newest-`LOG_READ_LIMIT`-first rather
 * than time-bounded, so they can reach further back than a metric history
 * scoped to the current range).
 *
 * When that oldest point reaches the start of the current range — at or
 * within 5% of `rangeMs` short of `since` — the read that was scoped to the
 * current range came back full, which says nothing about whether more
 * exists beyond it. Rather than report the range's own span (which would
 * make offeredTimeRanges stop offering anything wider), this reports
 * `rangeMs + 1`, one tick past it, so the next wider option is offered too
 * and the user can ask. If that wider read then turns out not to be full —
 * the store genuinely didn't hold that much — the next poll's oldest
 * timestamp reports the real, shorter span, and the range the user just
 * picked stays offered for as long as it remains the first option that
 * covers that span: nothing yanks a selection out from under the user
 * between one poll and the next.
 *
 * Null with no usable timestamp at all, so offeredTimeRanges falls back to
 * its own default rather than being told the span is zero.
 */
export function coveredSpanMs(
  now: number,
  since: number,
  rangeMs: number,
  timestamps: Iterable<string>
): number | null {
  let oldest = Infinity;
  for (const ts of timestamps) {
    const t = Date.parse(ts);
    if (!Number.isNaN(t) && t < oldest) oldest = t;
  }
  if (oldest === Infinity) return null;
  if (oldest <= since + rangeMs * 0.05) return rangeMs + 1;
  return Math.max(0, now - oldest);
}

/** The ranges worth offering: every option shorter than what the store
 *  holds, plus the first one that covers all of it. A week-long option on
 *  an hour of data is a choice that changes nothing. Until anything has
 *  arrived, the span is taken as the agent's default retention. */
export function offeredTimeRanges(spanMs: number | null): Array<TimeRangeOption & { ms: number }> {
  const span = spanMs ?? TIME_RANGES.find((r) => r.value === DEFAULT_TIME_RANGE)?.ms ?? 0;
  const out: Array<TimeRangeOption & { ms: number }> = [];
  for (const range of TIME_RANGES) {
    out.push(range);
    if (range.ms >= span) break;
  }
  return out;
}

/** One entry per distinct HTTP/TLS probe target — what the "Probes" card
 *  renders. Built from the agent's probe.* metrics
 *  (forsight/internal/collector/probe/probe.go), grouped by labels.name,
 *  falling back to labels.url for a sample that somehow lacks a name. */
export interface ProbeUptime {
  name: string;
  url: string;
  /** oldest first, one per bucket */
  segments: UptimeSegment[];
  /** Newest probe.tls.days_remaining sample in the window, if the target is
   *  probed over https and at least one such sample landed inside it. */
  tlsDaysRemaining?: number;
  /** Newest probe.tls.valid sample in the window, as a boolean. */
  tlsValid?: boolean;
}

/**
 * Tiles [since, now] into `buckets` equal-width, oldest-first buckets and
 * summarizes each target's probe.http.up samples into one UptimeSegment per
 * bucket: no sample is "unknown" ("no checks"), all up is "operational",
 * none up is "outage", and a mix is "degraded" — the latter two carry a
 * "N of M checks failed" detail. probe.tls.days_remaining/.valid are read
 * independently of probe.http.up (per the collector's own doc comment: a
 * probe failure and a bad certificate are different signals) and take the
 * newest sample in the window, if any.
 *
 * Every probe.* metric across the whole `metrics` array — not just samples
 * inside the window — is considered when finding the set of targets, so a
 * target with no checks landing in the current range still shows up as an
 * all-"unknown" strip rather than disappearing.
 */
export function probeUptime(
  metrics: Metric[],
  since: number,
  now: number,
  buckets = 60
): ProbeUptime[] {
  const targetKeyOf = (labels: Record<string, string> | undefined): string | undefined => {
    if (!labels) return undefined;
    return labels.name || labels.url || undefined;
  };

  const targets = new Map<string, { name: string; url: string }>();
  for (const m of metrics) {
    if (!m.name.startsWith("probe.")) continue;
    const key = targetKeyOf(m.labels);
    if (!key || targets.has(key)) continue;
    targets.set(key, { name: key, url: m.labels?.url ?? "" });
  }

  const span = Math.max(now - since, 0);
  const bucketMs = buckets > 0 ? span / buckets : 0;
  const labelFormat = span > ONE_DAY_MS ? dayTimeLabelFormat : timeLabelFormat;

  const bucketIndexOf = (t: number): number => {
    if (bucketMs <= 0) return 0;
    return Math.min(Math.max(Math.floor((t - since) / bucketMs), 0), buckets - 1);
  };

  /** Parsed timestamp, or null when unparseable or outside [since, now] —
   *  such a sample belongs to no bucket and can't be "newest in the window". */
  const inWindow = (timestamp: string): number | null => {
    const t = Date.parse(timestamp);
    return Number.isNaN(t) || t < since || t > now ? null : t;
  };

  const out: ProbeUptime[] = [];
  for (const { name, url } of targets.values()) {
    const perBucket = Array.from({ length: buckets }, () => ({ up: 0, total: 0 }));
    let newestDays: { t: number; value: number } | undefined;
    let newestValid: { t: number; value: number } | undefined;

    for (const m of metrics) {
      if (targetKeyOf(m.labels) !== name) continue;
      const t = inWindow(m.timestamp);
      if (t === null) continue;
      if (m.name === "probe.http.up") {
        const bucket = perBucket[bucketIndexOf(t)];
        bucket.total += 1;
        if (m.value === 1) bucket.up += 1;
      } else if (m.name === "probe.tls.days_remaining") {
        if (!newestDays || t > newestDays.t) newestDays = { t, value: m.value };
      } else if (m.name === "probe.tls.valid") {
        if (!newestValid || t > newestValid.t) newestValid = { t, value: m.value };
      }
    }

    const segments: UptimeSegment[] = perBucket.map((bucket, i) => {
      const label = labelFormat.format(new Date(since + i * bucketMs));
      if (bucket.total === 0) return { label, status: "unknown", detail: "no checks" };
      const down = bucket.total - bucket.up;
      if (down === 0) return { label, status: "operational" };
      return {
        label,
        status: bucket.up === 0 ? "outage" : "degraded",
        detail: `${down} of ${bucket.total} checks failed`,
      };
    });

    out.push({
      name,
      url,
      segments,
      ...(newestDays ? { tlsDaysRemaining: newestDays.value } : {}),
      ...(newestValid ? { tlsValid: newestValid.value === 1 } : {}),
    });
  }

  return out.sort((a, b) => a.name.localeCompare(b.name));
}

/** Tone for the TLS badge under a probe's UptimeBar: an invalid chain is
 *  always danger regardless of days remaining; otherwise danger at 0 or
 *  below, warning under two weeks, and the muted default beyond that. */
function tlsBadgeTone(
  tlsValid: boolean | undefined,
  daysRemaining: number
): NonNullable<BadgeProps["variant"]> {
  if (tlsValid === false) return "danger";
  if (daysRemaining <= 0) return "danger";
  if (daysRemaining < 14) return "warning";
  return "neutral";
}

function formatPercent(v: number | undefined): string {
  return v === undefined ? "—" : `${v.toFixed(1)}`;
}

function formatRss(bytes: number | undefined): string {
  if (bytes === undefined) return "—";
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatSLO(slo: number | undefined): string | undefined {
  if (slo === undefined) return undefined;
  return `${Math.round(slo * 10000) / 100}% SLO`;
}

function formatInsightTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return timeLabelFormat.format(d);
}

function toStreamEntries(logs: LogEntry[]): StreamLogEntry[] {
  return [...logs]
    .sort((a, b) => a.timestamp.localeCompare(b.timestamp))
    .map((entry, i) => ({
      id: `${entry.timestamp}-${entry.source}-${i}`,
      timestamp: timeLabelFormat.format(new Date(entry.timestamp)),
      level: entry.severity,
      source: entry.source,
      message: entry.message,
    }));
}

function asAlertSeverity(value: string): AlertSeverity {
  if (value === "critical" || value === "warning" || value === "info") return value;
  return "info";
}

function toAlertItems(insights: ForseerInsight[]): AlertListItem[] {
  return insights.map((ins) => ({
    id: ins.id,
    severity: asAlertSeverity(ins.severity),
    title: ins.title,
    description: ins.description,
    time: formatInsightTime(ins.time),
    source: ins.kind ?? ins.source,
  }));
}

function toneFor(severity: string): TimelineTone {
  if (severity === "critical") return "danger";
  if (severity === "warning") return "warning";
  return "accent";
}

function toTimelineItems(events: ForseerEvent[]): TimelineItem[] {
  return events.map((ev) => ({
    id: ev.id,
    time: formatInsightTime(ev.time),
    title: ev.title,
    description: ev.description,
    tone: (ev.tone as TimelineTone) || toneFor("info"),
  }));
}

// Merges newly-parsed query facets into the existing filter set rather than
// replacing it wholesale: any previous chip whose key the new parse didn't
// touch survives; a key the parse did produce is replaced by its new value
// (so re-querying "critical" after "critical from checkout-api" doesn't
// leave two conflicting status chips FilterBar's AND semantics can never
// both satisfy).
function mergeQueryFacets(prev: FilterBarFacet[], parsed: FilterBarFacet[]): FilterBarFacet[] {
  const parsedKeys = new Set(parsed.map((f) => f.key));
  return [...prev.filter((f) => !parsedKeys.has(f.key)), ...parsed];
}

function matchesFilters(entry: LogEntry, filters: FilterBarFacet[]): boolean {
  return filters.every((f) => {
    if (f.key === "status") return entry.severity === f.value;
    if (f.key === "source") return entry.source.toLowerCase().includes(f.value.toLowerCase());
    return true;
  });
}

function errorHeatmap(logs: LogEntry[]): {
  columns: string[];
  rows: { label: string; values: Array<number | null> }[];
} {
  const columns = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, "0"));
  const sources = [...new Set(logs.map((l) => l.source || "unknown"))].sort();
  const rows = sources.map((source) => {
    const values = columns.map((hour) => {
      const n = logs.filter((l) => {
        if ((l.source || "unknown") !== source || l.severity !== "error") return false;
        const d = new Date(l.timestamp);
        return !Number.isNaN(d.getTime()) && String(d.getHours()).padStart(2, "0") === hour;
      }).length;
      return n === 0 ? null : n;
    });
    return { label: source, values };
  });
  return { columns, rows: rows.filter((row) => row.values.some((v) => v !== null)) };
}

function toWaterfall(spans: Span[]): TraceSpan[] {
  if (spans.length === 0) return [];
  const starts = spans.map((s) => new Date(s.start).getTime());
  const origin = Math.min(...starts);
  const byParent = new Map<string, number>();
  const depthOf = (span: Span, seen: Set<string>): number => {
    if (!span.parentId) return 0;
    if (seen.has(span.spanId)) return 0;
    seen.add(span.spanId);
    const parent = spans.find((s) => s.spanId === span.parentId);
    if (!parent) return 1;
    const cached = byParent.get(span.spanId);
    if (cached !== undefined) return cached;
    const d = 1 + depthOf(parent, seen);
    byParent.set(span.spanId, d);
    return d;
  };
  return spans.map((s, i) => ({
    id: s.spanId || String(i),
    name: s.name,
    service: s.service,
    start: Math.max(0, new Date(s.start).getTime() - origin),
    duration: s.duration > 1e6 ? s.duration / 1e6 : s.duration,
    depth: depthOf(s, new Set()),
    status: s.status === "error" ? "error" : undefined,
  }));
}

function pickTrace(spans: Span[], insights: ForseerInsight[]): Span[] {
  const related = insights.find((ins) => ins.kind === "slow_span")?.related ?? [];
  const traceId = related.find((r) => spans.some((s) => s.traceId === r));
  if (traceId) return spans.filter((s) => s.traceId === traceId);
  const byTrace = new Map<string, Span[]>();
  for (const s of spans) {
    const list = byTrace.get(s.traceId) ?? [];
    list.push(s);
    byTrace.set(s.traceId, list);
  }
  let best: Span[] = [];
  for (const group of byTrace.values()) {
    const hasError = group.some((s) => s.status === "error");
    const dur = group.reduce((n, s) => n + s.duration, 0);
    const bestDur = best.reduce((n, s) => n + s.duration, 0);
    const bestErr = best.some((s) => s.status === "error");
    if ((hasError && !bestErr) || (hasError === bestErr && dur > bestDur)) best = group;
  }
  return best;
}

function statusFromInsights(
  connection: ConnectionState["state"],
  insights: ForseerInsight[]
): ServiceStatus {
  if (connection === "waiting") return "unknown";
  if (connection === "stale") return "outage";
  if (insights.some((ins) => ins.severity === "critical")) return "outage";
  if (insights.some((ins) => ins.severity === "warning")) return "degraded";
  return "operational";
}

/**
 * The count/score bars behind the "Log templates" card, plus whether every
 * cluster currently carries a paging score. Wraps useClusters so Overview
 * has one poll to wire into connectionState (via the returned `poll`) and
 * LogsPanel has ready-to-render bars — the same shape as every other
 * data-fetching hook in api.ts, just page-specific rather than generic.
 */
function useLogClusters(intervalMs: number) {
  const poll = useClusters(intervalMs);
  const clusters = poll.data;
  // Once Forseer's paging model is ready every cluster carries a score, and
  // the bar becomes that score: what a burst of this template is worth,
  // rather than how loud it is. Until then, the count.
  const clustersScored = useMemo(
    () => clusters.length > 0 && clusters.every((c) => c.pagingScore !== undefined),
    [clusters]
  );
  const clusterBars = useMemo(
    () =>
      (clustersScored
        ? [...clusters].sort((a, b) => (b.pagingScore ?? 0) - (a.pagingScore ?? 0))
        : clusters
      )
        .slice(0, 8)
        .map((c) => ({
          label: c.template || c.id,
          value: clustersScored ? (c.pagingScore ?? 0) : c.count,
        })),
    [clusters, clustersScored]
  );
  return { poll, clustersScored, clusterBars };
}

/**
 * The "Forseer" card: the AI narrative, the error-log budget, the "Ask
 * Forseer" natural-language query box that turns a phrase into filter chips
 * (queryForseer, mergeQueryFacets), and the resulting AlertList/Timeline
 * feed. `filters` is owned by Overview and shared with LogsPanel — this
 * panel both edits it (the query form and FilterBar) and reads it back to
 * render the current chips, so `onFiltersChange` is the raw state setter
 * (not a plain callback) and the query form updates it functionally
 * (`onFiltersChange((prev) => ...)`), same as the pre-split code, so a
 * filter change elsewhere can never be clobbered by a query response that
 * resolves against a stale snapshot.
 */
function AlertsPanel({
  summary,
  budget,
  insights,
  story,
  logs,
  filters,
  onFiltersChange,
}: {
  summary: { enabled: boolean; summary: string };
  budget: ForseerBudget;
  insights: ForseerInsight[];
  story: ForseerEvent[];
  logs: LogEntry[];
  filters: FilterBarFacet[];
  onFiltersChange: Dispatch<SetStateAction<FilterBarFacet[]>>;
}) {
  // null = neutral (show QUERY_HINT); a string = the last submitted phrase
  // wasn't understood (show it as an inline error instead).
  const [query, setQuery] = useState("");
  const [queryError, setQueryError] = useState<string | null>(null);

  const alertItems = useMemo(() => toAlertItems(insights), [insights]);
  const timelineItems = useMemo(() => toTimelineItems(story), [story]);
  const filterOptions: FilterBarOption[] = useMemo(() => {
    const sources = [...new Set(logs.map((l) => l.source).filter(Boolean))];
    const opts: FilterBarOption[] = [
      { facetKey: "status", facetLabel: "Status", value: "error", label: "error" },
      { facetKey: "status", facetLabel: "Status", value: "warn", label: "warn" },
      { facetKey: "status", facetLabel: "Status", value: "info", label: "info" },
    ];
    for (const src of sources) {
      opts.push({ facetKey: "source", facetLabel: "Source", value: src, label: src });
    }
    return opts;
  }, [logs]);

  const sloLabel = formatSLO(budget.slo);
  const budgetLabel = sloLabel
    ? `${budget.label || "Error-log budget"} · ${sloLabel}`
    : budget.label || "Error-log budget";

  return (
    <Card>
      <CardHeader>
        <CardTitle>Forseer</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {summary.enabled ? (
          summary.summary ? (
            <Text>{summary.summary}</Text>
          ) : null
        ) : (
          <Text tone="muted" size="sm">
            AI narrative disabled — set XAI_API_KEY to enable
          </Text>
        )}
        <ErrorBudget
          label={budgetLabel}
          consumed={budget.consumed}
          caption={budget.caption}
          warningAt={budget.warningAt}
          dangerAt={budget.dangerAt}
        />
        <form
          className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-end"
          onSubmit={(event) => {
            event.preventDefault();
            const phrase = query.trim();
            if (!phrase) return;
            void queryForseer(phrase).then(({ facets, matched }) => {
              if (!matched) {
                setQueryError(QUERY_NOT_UNDERSTOOD);
                return;
              }
              setQueryError(null);
              onFiltersChange((prev) => mergeQueryFacets(prev, facets));
            });
          }}
        >
          <Input
            className="min-w-0 flex-1"
            aria-label="Ask Forseer"
            invalid={queryError != null}
            hint={queryError ?? QUERY_HINT}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              if (queryError) setQueryError(null);
            }}
          />
          <Button type="submit">Apply</Button>
        </form>
        <FilterBar
          label="Log filters"
          filters={filters}
          onFiltersChange={onFiltersChange}
          options={filterOptions}
        />
        <AlertList
          label="Forseer insights"
          items={alertItems}
          emptyMessage="Forseer watches every metric, log template, and span against its own baseline. Spikes, regime shifts, log bursts, and slow traces show up here."
        />
        {timelineItems.length > 0 ? (
          <Timeline items={timelineItems} />
        ) : (
          <EmptyState
            title="No timeline events yet"
            description="Forseer insights are stitched into a timeline here as they occur."
          />
        )}
      </CardContent>
    </Card>
  );
}

/**
 * The log-centric cards: "Log templates" (via useLogClusters' bars), "Logs"
 * (the filtered stream), and "Error logs by hour" (the heatmap). `filters`
 * comes from Overview (shared with AlertsPanel, which is where the query box
 * and FilterBar chips actually render); the heatmap deliberately reads
 * `logs`/`since` rather than the filtered set — unchanged from the
 * pre-split page, where errorHeatmap always ran over rangedLogs, not
 * filteredLogs.
 */
function LogsPanel({
  logs,
  since,
  filters,
  clustersScored,
  clusterBars,
}: {
  logs: LogEntry[];
  since: number;
  filters: FilterBarFacet[];
  clustersScored: boolean;
  clusterBars: Array<{ label: string; value: number }>;
}) {
  const rangedLogs = useMemo(
    () => logs.filter((l) => sinceOrUndated(l.timestamp, since)),
    [logs, since]
  );
  const filteredLogs = useMemo(
    () => rangedLogs.filter((l) => matchesFilters(l, filters)),
    [rangedLogs, filters]
  );
  const streamEntries = useMemo(() => toStreamEntries(filteredLogs), [filteredLogs]);
  const errorCount = useMemo(
    () => filteredLogs.filter((entry) => entry.severity === "error").length,
    [filteredLogs]
  );
  const heatmap = useMemo(() => errorHeatmap(rangedLogs), [rangedLogs]);

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>
            Log templates{clustersScored ? " · worth paging" : " · by volume"}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {clusterBars.length === 0 ? (
            <EmptyState
              title="No log templates yet"
              description="OTLP logs are clustered into Drain-style templates. Bursts become Forseer insights."
            />
          ) : (
            <BarList
              items={clusterBars}
              max={clustersScored ? 1 : undefined}
              valueFormat={clustersScored ? percentFormat : undefined}
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Logs</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <p className="sr-only" aria-live="polite" aria-atomic="true">
            {errorCount === 0
              ? "No error logs in the current window."
              : `${errorCount} error log${errorCount === 1 ? "" : "s"} in the current window.`}
          </p>
          {streamEntries.length === 0 ? (
            <EmptyState
              title="No logs yet"
              description="POST OTLP logs to /v1/logs and they will appear here."
            />
          ) : (
            <LogStream label="Ingested logs" entries={streamEntries} maxHeight={360} />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Error logs by hour</CardTitle>
        </CardHeader>
        <CardContent>
          {heatmap.rows.length === 0 ? (
            <EmptyState
              title="No error logs"
              description="Sources show up here once error lines land."
            />
          ) : (
            <Heatmap
              label="Error logs by source and hour"
              columns={heatmap.columns}
              rows={heatmap.rows}
            />
          )}
        </CardContent>
      </Card>
    </>
  );
}

/** The "Slowest trace" card: picks the trace worth showing (an insight's
 *  related slow span, or else the longest/most-erroring one) and renders it
 *  as a waterfall. */
function TracesPanel({ traces, insights }: { traces: Span[]; insights: ForseerInsight[] }) {
  const waterfall = useMemo(() => toWaterfall(pickTrace(traces, insights)), [traces, insights]);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Slowest trace</CardTitle>
      </CardHeader>
      <CardContent>
        {waterfall.length === 0 ? (
          <EmptyState
            title="No traces yet"
            description="POST OTLP traces to /v1/traces. Forseer marks the critical path on slow or error spans."
          />
        ) : (
          <TraceWaterfall label="Related trace" spans={waterfall} />
        )}
      </CardContent>
    </Card>
  );
}

interface OverviewProps {
  /** Forwarded to the page's <h1> so App can move focus onto it after a
   * route change (skipping the very first mount) per the ARIA APG
   * client-navigation pattern — see App.tsx. Optional so the page's own
   * tests can still render it standalone. */
  headingRef?: Ref<HTMLHeadingElement>;
}

export default function Overview({ headingRef }: OverviewProps = {}) {
  // Every series' newest points inside a short, fixed window — not the
  // store's whole retained history. Feeds the StatCards' latest values,
  // the container/process rows, and the probe gate below. Two minutes
  // covers several collect intervals; a per-name cap (the kind item 10 gave
  // logs and traces) is the wrong bound here because containers and
  // processes share a metric name and differ only by label, so capping by
  // name would still return one series' worth of every container mixed
  // together rather than every container's own newest point.
  const latestPoll = useMetrics(5000, { sinceMinutes: 2 });
  const logsPoll = useLogs(5000);
  const tracesPoll = useTraces(5000);
  const insightsPoll = useInsights(5000);
  const logClusters = useLogClusters(5000);
  const summaryPoll = useSummary(30000);
  const budgetPoll = useBudget(5000);
  const storyPoll = useTimeline(5000);
  const latest = latestPoll.data;
  const logs = logsPoll.data;
  const traces = tracesPoll.data;
  const insights = insightsPoll.data;
  const summary = summaryPoll.data;
  const budget = budgetPoll.data;
  const story = storyPoll.data;
  const [filters, setFilters] = useState<FilterBarFacet[]>([]);

  const [rangeValue, setRangeValue] = useState(DEFAULT_TIME_RANGE);
  // `now` is read once per render, and every poll re-renders, so every
  // window below slides with the data.
  const now = Date.now();
  // What the user's selection (or the default, before any click) asks the
  // ranged reads below for — independent of what ends up "offered" a few
  // lines down, since that comes FROM these reads rather than the other way
  // around.
  const requestedRangeMs =
    TIME_RANGES.find((r) => r.value === rangeValue)?.ms ??
    TIME_RANGES.find((r) => r.value === DEFAULT_TIME_RANGE)?.ms ??
    0;
  const requestedSince = now - requestedRangeMs;

  // One ranged, name-scoped read per stat, keyed the same way as
  // CHART_METRICS so the StatCard row and the chart below can both index
  // into it by key. historyFor's filter+sort still runs on each result: in
  // production the server already scoped the response to this one name, so
  // the filter is a no-op there, but it keeps this correct against a test
  // double (or a future store) that doesn't, and the sort is load-bearing
  // either way since LineChart needs oldest-first.
  const metricHistories: Record<ChartMetric, Metric[]> = {
    cpu: historyFor(
      useMetrics(5000, { name: "host.cpu.percent", sinceMs: requestedRangeMs }).data,
      "host.cpu.percent"
    ),
    memory: historyFor(
      useMetrics(5000, { name: "host.memory.percent", sinceMs: requestedRangeMs }).data,
      "host.memory.percent"
    ),
    disk: historyFor(
      useMetrics(5000, { name: "host.disk.percent", sinceMs: requestedRangeMs }).data,
      "host.disk.percent"
    ),
  };

  // What the reads above actually cover (plus logs, read newest-first
  // rather than time-bounded, so they can reach further back) decides what
  // offeredTimeRanges offers — there is no more unbounded read to measure
  // the store's whole held span against directly. A selection the data
  // doesn't reach falls back to the widest still on offer, same as before.
  const coveredSpan = coveredSpanMs(now, requestedSince, requestedRangeMs, [
    ...metricHistories.cpu.map((m) => m.timestamp),
    ...metricHistories.memory.map((m) => m.timestamp),
    ...metricHistories.disk.map((m) => m.timestamp),
    ...logs.map((l) => l.timestamp),
  ]);
  const offeredRanges = offeredTimeRanges(coveredSpan);
  const range =
    offeredRanges.find((r) => r.value === rangeValue) ?? offeredRanges[offeredRanges.length - 1];
  const since = now - range.ms;

  const [chartMetric, setChartMetric] = useState<ChartMetric>("cpu");
  // Roving tab stop across the three StatCards, same model as TimeRange:
  // one stop for the group, arrows move (and select) inside it.
  const chartMetricRefs = useRef<Array<HTMLDivElement | null>>([]);
  const moveChartMetric = (index: number) => {
    const next = CHART_METRICS[(index + CHART_METRICS.length) % CHART_METRICS.length];
    setChartMetric(next.key);
    chartMetricRefs.current[CHART_METRICS.indexOf(next)]?.focus();
  };
  const handleChartMetricKeyDown = (event: KeyboardEvent<HTMLDivElement>, index: number) => {
    switch (event.key) {
      case "ArrowRight":
      case "ArrowDown":
        event.preventDefault();
        return moveChartMetric(index + 1);
      case "ArrowLeft":
      case "ArrowUp":
        event.preventDefault();
        return moveChartMetric(index - 1);
      case "Home":
        event.preventDefault();
        return moveChartMetric(0);
      case "End":
        event.preventDefault();
        return moveChartMetric(CHART_METRICS.length - 1);
      default:
        return;
    }
  };

  const chartHistory = metricHistories[chartMetric];
  const chartLabels = chartHistory.map((m) => timeLabelFormat.format(new Date(m.timestamp)));
  const chartMetricLabel = CHART_METRICS.find((m) => m.key === chartMetric)?.label ?? "Host CPU";
  // Memoized so LineChart's own internal memoization isn't defeated by a
  // fresh array-of-objects literal on every render (chartHistory/chartMetric
  // only actually change on a poll tick or a metric switch).
  const chartSeries = useMemo(
    () => [{ name: `${chartMetricLabel} %`, values: chartHistory.map((m) => m.value) }],
    [chartMetricLabel, chartHistory]
  );

  const cpu = latestValue(latest, "host.cpu.percent");
  const memory = latestValue(latest, "host.memory.percent");
  const disk = latestValue(latest, "host.disk.percent");
  const statValues: Record<ChartMetric, number | undefined> = { cpu, memory, disk };
  const containers = containerRows(latest);
  const processes = processRows(latest).slice(0, 15);
  // False on a deployment with no --probe target, so the card below it never
  // renders and this page looks exactly as it did before item #128, and its
  // three extra polls (ProbesSection) never run either.
  const hasProbeMetrics = latest.some((m) => m.name.startsWith("probe."));
  const rangeStartLabel = (range.ms > ONE_DAY_MS ? dayTimeLabelFormat : timeLabelFormat).format(
    new Date(since)
  );

  // The summary poller runs six times slower and is left out on purpose: a
  // success from it could only make a dead agent look alive for longer.
  const connection = connectionState(
    [latestPoll, logsPoll, tracesPoll, insightsPoll, logClusters.poll, budgetPoll, storyPoll],
    5000
  );
  const status = statusFromInsights(connection.state, insights);
  const connectionLabel =
    connection.state === "waiting"
      ? "Waiting for data…"
      : connection.state === "stale"
        ? `No data for ${Math.round((connection.silentForMs ?? 0) / 1000)}s — agent unreachable`
        : status === "outage"
          ? "Forseer: critical"
          : status === "degraded"
            ? "Forseer: warning"
            : "Receiving data";

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <Heading as="h1" size="xl" ref={headingRef} tabIndex={-1}>
            forsight
          </Heading>
          <Text tone="secondary">
            Collects host, process, Docker, OTLP, StatsD, and local Prometheus — Forseer watches the
            stream
          </Text>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <TimeRange
            label="Overview time range"
            options={offeredRanges}
            value={range.value}
            onValueChange={setRangeValue}
          />
          {/* Its own live region, so a flip to stale is read out. AlertList's
              region only announces additions to its list, so a status change
              is announced once, here. */}
          <div role="status" aria-live="polite" aria-label="Agent connection" className="shrink-0">
            <StatusDot
              status={status}
              label={connectionLabel}
              pulse={connection.state === "live"}
            />
          </div>
        </div>
      </header>

      {/* A real ARIA radio group, same model as TimeRange above: exactly one
          stat is charted, Arrow/Home/End move between them (and select as
          they go — a StatCard is a div, not a button, so there is no native
          Enter/Space activation to lean on), only the selected card is in
          the tab order, and a plain click selects too. */}
      <div
        role="radiogroup"
        aria-label="Chart metric"
        className="grid grid-cols-1 gap-4 sm:grid-cols-3"
      >
        {CHART_METRICS.map(({ key, label }, index) => {
          const selected = chartMetric === key;
          return (
            <StatCard
              key={key}
              ref={(node) => {
                chartMetricRefs.current[index] = node;
              }}
              label={label}
              value={formatPercent(statValues[key])}
              unit="%"
              status="operational"
              trend={metricHistories[key].map((m) => m.value)}
              trendTone={selected ? "accent" : "neutral"}
              role="radio"
              aria-checked={selected}
              tabIndex={selected ? 0 : -1}
              className={`cursor-pointer transition-colors focus-visible:outline-none focus-visible:shadow-focus-ring ${
                selected ? "border-accent" : ""
              }`}
              onClick={() => setChartMetric(key)}
              onKeyDown={(event) => handleChartMetricKeyDown(event, index)}
            />
          );
        })}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{chartMetricLabel} over time</CardTitle>
        </CardHeader>
        <CardContent>
          {chartHistory.length > 1 ? (
            <LineChart
              label={`${chartMetricLabel} percent over time`}
              labels={chartLabels}
              series={chartSeries}
              area
            />
          ) : (
            <EmptyState
              title="Collecting data"
              description="The chart fills in once a few collection ticks have landed."
            />
          )}
        </CardContent>
      </Card>

      {/* Only on a deployment that passed at least one --probe target — see
          probeUptime's own doc comment. Placed right after the host stat
          cards and chart, before Forseer/processes/containers/logs, because
          "is this endpoint up" is a health-at-a-glance question like the
          StatusDot above, not one of the diagnostic-detail sections below
          it. ProbesSection itself is only mounted once `latest` has proven a
          probe target exists, so its three polls never start on a
          deployment with none. */}
      {hasProbeMetrics ? (
        <Card>
          <CardHeader>
            <CardTitle>Probes</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-6">
            <ProbesSection
              since={since}
              now={now}
              rangeMs={range.ms}
              rangeLabel={range.label}
              rangeStartLabel={rangeStartLabel}
            />
          </CardContent>
        </Card>
      ) : null}

      <AlertsPanel
        summary={summary}
        budget={budget}
        insights={insights}
        story={story}
        logs={logs}
        filters={filters}
        onFiltersChange={setFilters}
      />

      <Card>
        <CardHeader>
          <CardTitle>Processes</CardTitle>
        </CardHeader>
        <CardContent>
          {processes.length === 0 ? (
            <EmptyState
              title="Collecting processes"
              description="Per-process CPU and RSS show up after the first couple of ticks."
            />
          ) : (
            <div className="w-full overflow-x-auto">
              <Table>
                <caption className="sr-only">Busiest processes by CPU, with RSS</caption>
                <TableHeader>
                  <TableRow>
                    <TableHead>PID</TableHead>
                    <TableHead>Name</TableHead>
                    <TableHead>CPU %</TableHead>
                    <TableHead>RSS</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {processes.map((p) => (
                    <TableRow key={p.pid}>
                      <TableCell>{p.pid}</TableCell>
                      <TableCell>{p.name}</TableCell>
                      <TableCell>{formatPercent(p.cpuPercent)}</TableCell>
                      <TableCell>{formatRss(p.rssBytes)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Containers</CardTitle>
        </CardHeader>
        <CardContent>
          {containers.length === 0 ? (
            <EmptyState
              title="No containers"
              description="No Docker daemon was reachable when forsight started, or nothing is running."
            />
          ) : (
            <div className="w-full overflow-x-auto">
              <Table>
                <caption className="sr-only">Running containers with CPU and memory usage</caption>
                <TableHeader>
                  <TableRow>
                    <TableHead>Container</TableHead>
                    <TableHead>Image</TableHead>
                    <TableHead>CPU %</TableHead>
                    <TableHead>Memory %</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {containers.map((c) => (
                    <TableRow key={c.id}>
                      <TableCell>{c.name}</TableCell>
                      <TableCell className="text-fg-secondary">{c.image}</TableCell>
                      <TableCell>{formatPercent(c.cpuPercent)}</TableCell>
                      <TableCell>{formatPercent(c.memoryPercent)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>

      <LogsPanel
        logs={logs}
        since={since}
        filters={filters}
        clustersScored={logClusters.clustersScored}
        clusterBars={logClusters.clusterBars}
      />

      <TracesPanel traces={traces} insights={insights} />
    </div>
  );
}

/**
 * Mounted only once `latest` has shown at least one probe.* sample, so its
 * three polls — the only ones on this page that ever ask for probe.* — never
 * run at all on a deployment with no --probe target. Same pattern as
 * Models.tsx's ForecastsSection: a narrow child component whose sole job is
 * to hold the extra hooks a conditional render would otherwise pull into the
 * page unconditionally.
 *
 * Each read is named and ranged to the selected time range, same as the
 * CHART_METRICS reads above, and their results are merged back into one
 * array for the existing probeUptime — unchanged by this component's
 * existence. One consequence of scoping these reads to the window: a target
 * with literally no probe.* sample anywhere in the selected range (rather
 * than merely no probe.http.up sample, which still buckets as "unknown" —
 * see probeUptime's own doc comment) no longer surfaces, since it would
 * never appear in any of the three arrays below to be discovered from. That
 * traded a genuinely unbounded read for a case that only bites a target
 * whose checks have been absent for the entire visible window.
 */
function ProbesSection({
  since,
  now,
  rangeMs,
  rangeLabel,
  rangeStartLabel,
}: {
  since: number;
  now: number;
  rangeMs: number;
  rangeLabel: string;
  rangeStartLabel: string;
}) {
  const up = useMetrics(5000, { name: "probe.http.up", sinceMs: rangeMs }).data;
  const tlsDaysRemaining = useMetrics(5000, {
    name: "probe.tls.days_remaining",
    sinceMs: rangeMs,
  }).data;
  const tlsValid = useMetrics(5000, { name: "probe.tls.valid", sinceMs: rangeMs }).data;
  const probes = probeUptime([...up, ...tlsDaysRemaining, ...tlsValid], since, now);

  return (
    <>
      {probes.map((probe) => (
        <div key={probe.name} className="flex flex-col gap-2">
          <UptimeBar
            label={`${probe.name}, last ${rangeLabel}`}
            segments={probe.segments}
            startCaption={rangeStartLabel}
            endCaption="Now"
          />
          {probe.tlsDaysRemaining !== undefined ? (
            <Badge variant={tlsBadgeTone(probe.tlsValid, probe.tlsDaysRemaining)}>
              {probe.tlsValid === false
                ? "TLS certificate invalid"
                : `TLS expires in ${Math.round(probe.tlsDaysRemaining)} days`}
            </Badge>
          ) : null}
        </div>
      ))}
    </>
  );
}
