import { useMemo, useState } from "react";
import {
  Heading,
  Text,
  StatCard,
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
  type LogEntry as StreamLogEntry,
  type AlertListItem,
  type AlertSeverity,
  type TimelineItem,
  type TimelineTone,
  type ServiceStatus,
  type FilterBarFacet,
  type FilterBarOption,
  type TraceSpan,
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
  type LogEntry,
  type ForseerInsight,
  type ForseerEvent,
  type Span,
} from "../api";

const timeLabelFormat = new Intl.DateTimeFormat(undefined, {
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

function errorHeatmap(logs: LogEntry[]): { columns: string[]; rows: { label: string; values: Array<number | null> }[] } {
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

export default function Overview() {
  const metricsPoll = useMetrics(5000);
  const logsPoll = useLogs(5000);
  const tracesPoll = useTraces(5000);
  const insightsPoll = useInsights(5000);
  const clustersPoll = useClusters(5000);
  const summaryPoll = useSummary(30000);
  const budgetPoll = useBudget(5000);
  const storyPoll = useTimeline(5000);
  const metrics = metricsPoll.data;
  const logs = logsPoll.data;
  const traces = tracesPoll.data;
  const insights = insightsPoll.data;
  const clusters = clustersPoll.data;
  const summary = summaryPoll.data;
  const budget = budgetPoll.data;
  const story = storyPoll.data;
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<FilterBarFacet[]>([]);
  // null = neutral (show QUERY_HINT); a string = the last submitted phrase
  // wasn't understood (show it as an inline error instead).
  const [queryError, setQueryError] = useState<string | null>(null);

  const cpuHistory = historyFor(metrics, "host.cpu.percent");
  const cpuLabels = cpuHistory.map((m) => timeLabelFormat.format(new Date(m.timestamp)));

  const cpu = latestValue(metrics, "host.cpu.percent");
  const memory = latestValue(metrics, "host.memory.percent");
  const disk = latestValue(metrics, "host.disk.percent");
  const containers = containerRows(metrics);
  const processes = processRows(metrics).slice(0, 15);
  const filteredLogs = useMemo(() => logs.filter((l) => matchesFilters(l, filters)), [logs, filters]);
  const streamEntries = useMemo(() => toStreamEntries(filteredLogs), [filteredLogs]);
  const errorCount = useMemo(
    () => filteredLogs.filter((entry) => entry.severity === "error").length,
    [filteredLogs]
  );
  const alertItems = useMemo(() => toAlertItems(insights), [insights]);
  const timelineItems = useMemo(() => toTimelineItems(story), [story]);
  const heatmap = useMemo(() => errorHeatmap(logs), [logs]);
  const waterfall = useMemo(() => toWaterfall(pickTrace(traces, insights)), [traces, insights]);
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
  const clusterBars = useMemo(
    () =>
      clusters.slice(0, 8).map((c) => ({
        label: c.template || c.id,
        value: c.count,
      })),
    [clusters]
  );

  // The summary poller runs six times slower and is left out on purpose: a
  // success from it could only make a dead agent look alive for longer.
  const connection = connectionState(
    [metricsPoll, logsPoll, tracesPoll, insightsPoll, clustersPoll, budgetPoll, storyPoll],
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
  const sloLabel = formatSLO(budget.slo);
  const budgetLabel = sloLabel ? `${budget.label || "Error-log budget"} · ${sloLabel}` : budget.label || "Error-log budget";

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <Heading as="h1" size="xl">
            forsight
          </Heading>
          <Text tone="secondary">
            Collects host, process, Docker, OTLP, StatsD, and local Prometheus — Forseer
            watches the stream
          </Text>
        </div>
        {/* Its own live region, so a flip to stale is read out. AlertList's
            region only announces additions to its list, so a status change
            is announced once, here. */}
        <div role="status" aria-live="polite" aria-label="Agent connection" className="shrink-0">
          <StatusDot status={status} label={connectionLabel} pulse={connection.state === "live"} />
        </div>
      </header>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard label="Host CPU" value={formatPercent(cpu)} unit="%" status="operational" />
        <StatCard label="Host memory" value={formatPercent(memory)} unit="%" status="operational" />
        <StatCard label="Host disk" value={formatPercent(disk)} unit="%" status="operational" />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Host CPU over time</CardTitle>
        </CardHeader>
        <CardContent>
          {cpuHistory.length > 1 ? (
            <LineChart
              label="Host CPU percent over time"
              labels={cpuLabels}
              series={[{ name: "CPU %", values: cpuHistory.map((m) => m.value) }]}
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

      <Card>
        <CardHeader>
          <CardTitle>Forseer</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {summary.enabled ? (
            summary.summary ? <Text>{summary.summary}</Text> : null
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
                setFilters((prev) => mergeQueryFacets(prev, facets));
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
            onFiltersChange={setFilters}
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

      <Card>
        <CardHeader>
          <CardTitle>Log templates</CardTitle>
        </CardHeader>
        <CardContent>
          {clusterBars.length === 0 ? (
            <EmptyState
              title="No log templates yet"
              description="OTLP logs are clustered into Drain-style templates. Bursts become Forseer insights."
            />
          ) : (
            <BarList items={clusterBars} />
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
            <EmptyState title="No error logs" description="Sources show up here once error lines land." />
          ) : (
            <Heatmap label="Error logs by source and hour" columns={heatmap.columns} rows={heatmap.rows} />
          )}
        </CardContent>
      </Card>

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
    </div>
  );
}
