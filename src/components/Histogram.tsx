import * as React from "react";
import { cn } from "../lib/cn";
import { barPath, clamp, formatCompact, niceScale, project } from "../lib/chart";
import { useChartCursor } from "../lib/chart-hooks";
import { ChartFrame } from "./ChartFrame";
import { ChartTooltip } from "./ChartTooltip";
import { pickLabelIndices } from "./LineChart";

export interface HistogramBucket {
  /** Bucket label, e.g. "0–50ms". */
  label: string;
  count: number;
}

export interface HistogramProps extends Omit<React.HTMLAttributes<HTMLDivElement>, "children"> {
  /** Accessible name of the plot, e.g. "Request duration distribution, last hour". */
  label: string;
  /** Longer summary read after the name. */
  description?: string;
  /** Ordered buckets, lowest range first. */
  buckets: HistogramBucket[];
  /** Plot height in CSS pixels. Width is measured from the container. */
  height?: number;
  /** Formats the count axis, tooltip and data table. Defaults to compact notation. */
  valueFormat?: (value: number) => string;
}

const PAD_LEFT = 44;
const PAD_RIGHT = 10;
const PAD_TOP = 10;
const PAD_BOTTOM = 22;
/** Hairline gap so adjacent bars still read as distinct — a histogram's ranges are continuous, unlike `BarChart`'s discrete, gapped categories. */
const MARK_GAP = 1;

/**
 * Distribution of one metric across ordered ranges — request-duration
 * buckets, payload-size buckets. Bars sit edge to edge (only a hairline
 * gap) because the ranges are continuous, which is what visually tells
 * this apart from `BarChart`'s discrete, gapped categories; reach for
 * `BarChart` instead once the x-axis is categorical rather than a
 * range. Single-metric, so it uses the neutral accent color (like
 * `Sparkline`/`Gauge`) rather than a series slot — there's nothing here to
 * compare against another series.
 *
 * The plot has the same cursor as the other charts — hover, or focus it and
 * use Arrow/Home/End (Escape clears) — and always renders its data table.
 */
export const Histogram = React.forwardRef<HTMLDivElement, HistogramProps>(
  (
    { className, label, description, buckets, height = 220, valueFormat = formatCompact, ...props },
    ref
  ) => {
    const cursor = useChartCursor(buckets.length);
    const plotRef = React.useRef<HTMLDivElement>(null);

    const highest = Math.max(0, ...buckets.map((b) => b.count));
    const scale = niceScale(0, highest);

    const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
      const box = plotRef.current?.getBoundingClientRect();
      if (!box || buckets.length === 0) return;
      const plotWidth = Math.max(1, box.width - PAD_LEFT - PAD_RIGHT);
      const band = plotWidth / buckets.length;
      const index = Math.floor((event.clientX - box.left - PAD_LEFT) / band);
      if (!Number.isFinite(index)) return;
      cursor.setActive(clamp(index, 0, buckets.length - 1));
    };

    const active = cursor.active;
    const labels = buckets.map((b) => b.label);

    return (
      <div ref={ref} className={cn("flex w-full min-w-0 flex-col gap-3", className)} {...props}>
        <div
          ref={plotRef}
          tabIndex={0}
          onKeyDown={cursor.onKeyDown}
          onBlur={cursor.onBlur}
          onPointerMove={handlePointerMove}
          onPointerLeave={() => cursor.setActive(null)}
          className="relative rounded-md focus-visible:outline-none focus-visible:shadow-focus-ring"
        >
          <ChartFrame
            label={label}
            description={[description, "Use arrow keys to read individual buckets."]
              .filter(Boolean)
              .join(" ")}
            height={height}
            columns={labels}
            rows={[{ header: "Count", cells: buckets.map((b) => valueFormat(b.count)) }]}
          >
            {({ width }) => {
              const plotWidth = Math.max(1, width - PAD_LEFT - PAD_RIGHT);
              const plotHeight = Math.max(1, height - PAD_TOP - PAD_BOTTOM);
              const baselineY = PAD_TOP + plotHeight;
              const band = plotWidth / Math.max(1, buckets.length);
              const barWidth = Math.max(1, band - MARK_GAP);
              const lengthOf = (value: number) => project(value, scale.min, scale.max, plotHeight);

              return (
                <>
                  {scale.ticks.map((tick) => {
                    const y = baselineY - lengthOf(tick);
                    return (
                      <g key={tick}>
                        <line
                          x1={PAD_LEFT}
                          x2={PAD_LEFT + plotWidth}
                          y1={y}
                          y2={y}
                          className="stroke-ink-border-subtle"
                          strokeWidth={1}
                        />
                        <text
                          x={PAD_LEFT - 8}
                          y={y}
                          textAnchor="end"
                          dominantBaseline="middle"
                          className="fill-fg-muted text-xs font-sans"
                        >
                          {valueFormat(tick)}
                        </text>
                      </g>
                    );
                  })}

                  {buckets.map((bucket, index) => {
                    const barLength = lengthOf(bucket.count);
                    return (
                      <path
                        key={bucket.label}
                        d={barPath(
                          PAD_LEFT + index * band + MARK_GAP / 2,
                          baselineY - barLength,
                          barWidth,
                          barLength,
                          2
                        )}
                        className={cn(
                          "fill-accent",
                          active !== null && active !== index && "opacity-45"
                        )}
                      />
                    );
                  })}

                  <line
                    x1={PAD_LEFT}
                    x2={PAD_LEFT + plotWidth}
                    y1={baselineY}
                    y2={baselineY}
                    className="stroke-ink-border"
                    strokeWidth={1}
                  />

                  {pickLabelIndices(labels.length, plotWidth).map((index) => (
                    <text
                      key={labels[index]}
                      x={PAD_LEFT + index * band + band / 2}
                      y={height - 6}
                      textAnchor="middle"
                      className="fill-fg-muted text-xs font-sans"
                    >
                      {labels[index]}
                    </text>
                  ))}
                </>
              );
            }}
          </ChartFrame>

          {active === null ? null : (
            // Parked on the side opposite the cursor so the readout never
            // covers the bar being read. Physical left/right, not logical
            // start/end: the cursor's x position is a physical pixel (the
            // plot never mirrors under RTL — see ChartFrame), so which side
            // avoids it is a physical question too. A logical class would
            // resolve to the same side as the bar under `dir="rtl"`.
            <div
              className={cn(
                "absolute top-2 z-10",
                active > (buckets.length - 1) / 2 ? "left-2" : "right-2"
              )}
            >
              <ChartTooltip
                title={labels[active]}
                rows={[{ label: "Count", value: valueFormat(buckets[active].count) }]}
              />
            </div>
          )}
        </div>

        <div role="status" className="sr-only">
          {active === null ? "" : `${labels[active]}: ${valueFormat(buckets[active].count)}`}
        </div>
      </div>
    );
  }
);
Histogram.displayName = "Histogram";
