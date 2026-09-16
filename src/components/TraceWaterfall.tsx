import * as React from "react";
import { cn } from "../lib/cn";
import { clamp, formatDuration } from "../lib/chart";

export interface TraceSpan {
  id: string;
  /** Operation name, e.g. "SELECT orders" or "POST /checkout". */
  name: string;
  /** Emitting service, shown next to the name. */
  service?: string;
  /** Offset from the start of the trace, in milliseconds. */
  start: number;
  /** Span duration in milliseconds. */
  duration: number;
  /** Nesting level; 0 is the root span. */
  depth?: number;
  /** A failed span is marked with a word and a red bar, not red alone. */
  status?: "ok" | "error";
}

export interface TraceWaterfallProps extends Omit<
  React.HTMLAttributes<HTMLTableElement>,
  "children"
> {
  /** What the trace is of, e.g. "POST /checkout — trace 9f2c…". */
  label: string;
  /** Spans in display order (usually start time, depth-first). */
  spans: TraceSpan[];
  /**
   * Trace wall time in milliseconds, used as the bar scale. Defaults to the
   * furthest span end — pass it explicitly when the root span's duration is the
   * real total and some child reports past it.
   */
  total?: number;
}

/**
 * Distributed-trace span waterfall: where a request's time actually went.
 *
 * Bars are positioned and scaled against the trace's wall time, so a reader can
 * see serial versus parallel work at a glance — which is the question a
 * waterfall answers and a list of durations does not. Every row also prints its
 * own duration, so nothing depends on measuring a bar by eye.
 *
 * It is a `<table>`: each span is a row with a real header cell, which is how a
 * screen-reader user reads the same structure the bars show.
 */
export const TraceWaterfall = React.forwardRef<HTMLTableElement, TraceWaterfallProps>(
  ({ className, label, spans, total, ...props }, ref) => {
    const wallTime = total ?? Math.max(1, ...spans.map((span) => span.start + span.duration));

    return (
      <div className="w-full overflow-x-auto">
        <table
          ref={ref}
          className={cn("w-full min-w-[32rem] text-sm font-sans", className)}
          {...props}
        >
          <caption className="mb-2 text-start text-sm font-medium text-fg">
            {`${label} — ${formatDuration(wallTime)} total`}
          </caption>
          <thead>
            <tr className="text-xs text-fg-muted">
              <th scope="col" className="w-1/3 pb-2 text-start font-normal">
                Span
              </th>
              <th scope="col" className="pb-2 text-start font-normal">
                Timeline
              </th>
              <th scope="col" className="pb-2 text-end font-normal">
                Duration
              </th>
            </tr>
          </thead>
          <tbody>
            {spans.map((span) => {
              const offset = clamp((span.start / wallTime) * 100, 0, 100);
              const width = clamp((span.duration / wallTime) * 100, 0, 100 - offset);
              return (
                <tr key={span.id} className="align-middle hover:bg-ink-surface">
                  <th scope="row" className="py-1.5 pe-3 text-start font-normal">
                    <span
                      className="flex min-w-0 items-baseline gap-2"
                      style={{ paddingInlineStart: `${(span.depth ?? 0) * 12}px` }}
                    >
                      <span className="min-w-0 truncate text-fg">{span.name}</span>
                      {span.service ? (
                        <span className="shrink-0 truncate text-xs text-fg-muted">
                          {span.service}
                        </span>
                      ) : null}
                    </span>
                  </th>
                  <td className="py-1.5">
                    <span aria-hidden="true" className="relative block h-3 w-full">
                      <span
                        className={cn(
                          "absolute inset-y-0 rounded-sm",
                          span.status === "error" ? "bg-danger" : "bg-accent"
                        )}
                        style={{
                          // Physical `left`, not the logical `insetInlineStart`:
                          // a span's offset is wall-clock time, which (like
                          // LineChart/BarChart's x-axis — see ChartFrame) reads
                          // left-to-right in both text directions. The logical
                          // property would mirror the whole waterfall under
                          // `dir="rtl"`, so a span late in the trace would
                          // render nearer the timeline's start than one early.
                          left: `${offset}%`,
                          width: `${Math.max(width, 0.5)}%`,
                        }}
                      />
                    </span>
                  </td>
                  <td className="py-1.5 ps-3 text-end font-mono text-xs text-fg-secondary">
                    {formatDuration(span.duration)}
                    {span.status === "error" ? (
                      <span className="ms-2 font-sans text-danger">error</span>
                    ) : null}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    );
  }
);
TraceWaterfall.displayName = "TraceWaterfall";
