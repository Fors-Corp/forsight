import * as React from "react";
import { cn } from "../lib/cn";
import {
  barPath,
  clamp,
  formatActiveReading,
  formatCompact,
  linePath,
  niceScale,
  project,
  seriesFill,
  seriesLegendItems,
  seriesStroke,
  splitAtGaps,
  type Point,
} from "../lib/chart";
import { useChartCursor } from "../lib/chart-hooks";
import { ChartFrame } from "./ChartFrame";
import { ChartLegend } from "./ChartLegend";
import { ChartTooltip } from "./ChartTooltip";
import { pickLabelIndices } from "./LineChart";

export interface ComboChartBarSeries {
  name: string;
  type: "bar";
  values: Array<number | null>;
}

export interface ComboChartLineSeries {
  name: string;
  type: "line";
  values: Array<number | null>;
}

export type ComboChartSeries = ComboChartBarSeries | ComboChartLineSeries;

export interface ComboChartProps extends Omit<React.HTMLAttributes<HTMLDivElement>, "children"> {
  /** Accessible name of the plot, e.g. "Request volume and p99 latency, last hour". */
  label: string;
  /** Longer summary read after the name. */
  description?: string;
  /** X-axis categories, pre-formatted. */
  labels: string[];
  /** Mix of `"bar"` and `"line"` series, up to 8 total. */
  series: ComboChartSeries[];
  /** Plot height in CSS pixels. Width is measured from the container. */
  height?: number;
  /** Formats the left axis (bar series), and their tooltip/table values. Defaults to compact notation. */
  valueFormat?: (value: number) => string;
  /** Formats the right axis (line series), and their tooltip/table values. Defaults to `valueFormat`. */
  secondaryValueFormat?: (value: number) => string;
  /** Read for a `null`/missing sample in the cursor readout, tooltip and data table. Defaults to `"no data"` — override to localize it. */
  noDataLabel?: string;
}

const PAD_LEFT = 44;
const PAD_RIGHT = 44;
const PAD_TOP = 10;
const PAD_BOTTOM = 22;
const MARK_GAP = 2;

/**
 * Two metrics on unlike scales sharing one x-axis — request volume as bars
 * against p99 latency as a line, the APM-dashboard pattern `BarChart` and
 * `LineChart` can't cover alone since forcing both onto one scale would
 * make whichever has the smaller range unreadable. Bar series always share
 * a zero-baselined left axis (same rule as `BarChart`); line series always
 * share a right axis fit to their own data range (same rule as
 * `LineChart`) — the visually hidden description calls out which series
 * reads on which axis, since the data table's numbers alone don't carry
 * that. Reach for plain `BarChart`/`LineChart` once every series is the
 * same kind of thing on the same scale.
 *
 * The plot has the same cursor as the other charts — hover, or focus it and
 * use Arrow/Home/End (Escape clears) — and always renders its data table.
 */
export const ComboChart = React.memo(
  React.forwardRef<HTMLDivElement, ComboChartProps>(
    (
      {
        className,
        label,
        description,
        labels,
        series,
        height = 240,
        valueFormat = formatCompact,
        secondaryValueFormat = valueFormat,
        noDataLabel = "no data",
        ...props
      },
      ref
    ) => {
      const cursor = useChartCursor(labels.length);
      const plotRef = React.useRef<HTMLDivElement>(null);

      // Keyed on `series` alone — the cursor re-renders this component on
      // every hover/key event without the series lists or either domain
      // actually changing.
      const { barSeries, lineSeries, barScale, lineScale } = React.useMemo(() => {
        const bars = series.filter((s): s is ComboChartBarSeries => s.type === "bar");
        const lines = series.filter((s): s is ComboChartLineSeries => s.type === "line");

        const barHighest = Math.max(0, ...bars.flatMap((s) => s.values.map((v) => v ?? 0)));

        let lineMin = Infinity;
        let lineMax = -Infinity;
        for (const s of lines) {
          for (const value of s.values) {
            if (value === null) continue;
            if (value < lineMin) lineMin = value;
            if (value > lineMax) lineMax = value;
          }
        }

        return {
          barSeries: bars,
          lineSeries: lines,
          barScale: niceScale(0, barHighest),
          lineScale: niceScale(lineMin, lineMax),
        };
      }, [series]);

      const formatFor = (s: ComboChartSeries) =>
        s.type === "bar" ? valueFormat : secondaryValueFormat;

      // Which points are gapped vs. connected, split into runs in
      // value-space (x = index, y = value) — independent of plot width, so
      // it doesn't need to be redone on the width-dependent draw below.
      const projectedLineSeries = React.useMemo(
        () => lineSeries.map((s) => splitAtGaps(s.values, (index, value): Point => [index, value])),
        [lineSeries]
      );

      const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
        const box = plotRef.current?.getBoundingClientRect();
        if (!box || labels.length === 0) return;
        const plotWidth = Math.max(1, box.width - PAD_LEFT - PAD_RIGHT);
        const band = plotWidth / labels.length;
        const index = Math.floor((event.clientX - box.left - PAD_LEFT) / band);
        if (!Number.isFinite(index)) return;
        cursor.setActive(clamp(index, 0, labels.length - 1));
      };

      const active = cursor.active;
      const legendItems = React.useMemo(() => seriesLegendItems(series), [series]);

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
              description={[
                description,
                barSeries.length > 0 && lineSeries.length > 0
                  ? `${barSeries.map((s) => s.name).join(", ")} use the left axis; ${lineSeries
                      .map((s) => s.name)
                      .join(", ")} use the right axis.`
                  : null,
                "Use arrow keys to read individual points.",
              ]
                .filter(Boolean)
                .join(" ")}
              height={height}
              columns={labels}
              rows={series.map((s) => ({
                header: s.name,
                cells: s.values.map((v) => (v === null ? noDataLabel : formatFor(s)(v))),
              }))}
            >
              {({ width }) => {
                const plotWidth = Math.max(1, width - PAD_LEFT - PAD_RIGHT);
                const plotHeight = Math.max(1, height - PAD_TOP - PAD_BOTTOM);
                const baselineY = PAD_TOP + plotHeight;
                const band = plotWidth / Math.max(1, labels.length);
                const groupWidth = band * 0.6;
                const barWidth =
                  barSeries.length > 0
                    ? Math.max(
                        1,
                        (groupWidth - MARK_GAP * (barSeries.length - 1)) / barSeries.length
                      )
                    : 0;
                const bandStart = (index: number) =>
                  PAD_LEFT + index * band + (band - groupWidth) / 2;
                const centerOf = (index: number) => PAD_LEFT + index * band + band / 2;
                const barLengthOf = (value: number) =>
                  project(value, barScale.min, barScale.max, plotHeight);
                const lineYAt = (value: number) =>
                  baselineY - project(value, lineScale.min, lineScale.max, plotHeight);

                return (
                  <>
                    {barSeries.length > 0 &&
                      barScale.ticks.map((tick) => {
                        const y = baselineY - barLengthOf(tick);
                        return (
                          <g key={`left-${tick}`}>
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

                    {lineSeries.length > 0 &&
                      lineScale.ticks.map((tick) => (
                        <text
                          key={`right-${tick}`}
                          x={PAD_LEFT + plotWidth + 8}
                          y={lineYAt(tick)}
                          textAnchor="start"
                          dominantBaseline="middle"
                          className="fill-fg-muted text-xs font-sans"
                        >
                          {secondaryValueFormat(tick)}
                        </text>
                      ))}

                    {labels.map((categoryLabel, index) => (
                      <g
                        key={categoryLabel}
                        opacity={active === null || active === index ? 1 : 0.45}
                      >
                        {barSeries.map((s, i) => {
                          const value = s.values[index] ?? 0;
                          const barLength = barLengthOf(value);
                          const seriesIndex = series.indexOf(s);
                          return (
                            <path
                              key={s.name}
                              d={barPath(
                                bandStart(index) + i * (barWidth + MARK_GAP),
                                baselineY - barLength,
                                barWidth,
                                barLength
                              )}
                              className={seriesFill(seriesIndex)}
                            />
                          );
                        })}
                      </g>
                    ))}

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
                        x={centerOf(index)}
                        y={height - 6}
                        textAnchor="middle"
                        className="fill-fg-muted text-xs font-sans"
                      >
                        {labels[index]}
                      </text>
                    ))}

                    {lineSeries.map((s, i) => {
                      const seriesIndex = series.indexOf(s);
                      // The gap-splitting itself already happened, keyed on
                      // `lineSeries` alone, above — this only projects the
                      // cached value-space points into pixel space.
                      const segments = projectedLineSeries[i].map((segment) =>
                        segment.map(([index, value]): Point => [centerOf(index), lineYAt(value)])
                      );
                      return (
                        <g key={s.name}>
                          {segments.map((segment, i) => (
                            <path
                              key={i}
                              d={linePath(segment)}
                              fill="none"
                              strokeWidth={2}
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              className={seriesStroke(seriesIndex)}
                            />
                          ))}
                          {active !== null && s.values[active] !== null && (
                            <circle
                              cx={centerOf(active)}
                              cy={lineYAt(s.values[active] as number)}
                              r={4}
                              strokeWidth={2}
                              className={cn(seriesFill(seriesIndex), "stroke-ink-bg")}
                            />
                          )}
                        </g>
                      );
                    })}
                  </>
                );
              }}
            </ChartFrame>

            {active === null ? null : (
              // Parked on the side opposite the cursor so the readout never
              // covers the category being read. Physical left/right, not logical
              // start/end: the cursor's x position is a physical pixel (the
              // plot never mirrors under RTL — see ChartFrame), so which side
              // avoids it is a physical question too. A logical class would
              // resolve to the same side as the category under `dir="rtl"`.
              <div
                className={cn(
                  "absolute top-2 z-10",
                  active > (labels.length - 1) / 2 ? "left-2" : "right-2"
                )}
              >
                <ChartTooltip
                  title={labels[active]}
                  rows={series.map((s, seriesIndex) => ({
                    label: s.name,
                    seriesIndex,
                    value:
                      s.values[active] === null || s.values[active] === undefined
                        ? noDataLabel
                        : formatFor(s)(s.values[active] as number),
                  }))}
                />
              </div>
            )}
          </div>

          <div role="status" className="sr-only">
            {formatActiveReading(labels, series, active, formatFor, noDataLabel)}
          </div>

          {legendItems ? <ChartLegend items={legendItems} /> : null}
        </div>
      );
    }
  )
);
ComboChart.displayName = "ComboChart";
