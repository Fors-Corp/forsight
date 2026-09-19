import * as React from "react";
import { cn } from "../lib/cn";
import {
  ANNOTATION_TONE_CLASSES,
  barPath,
  clamp,
  formatActiveReading,
  formatCompact,
  niceScale,
  project,
  seriesFill,
  seriesLegendItems,
  type ChartAnnotation,
} from "../lib/chart";
import { useChartCursor } from "../lib/chart-hooks";
import { ChartFrame } from "./ChartFrame";
import { ChartLegend } from "./ChartLegend";
import { ChartTooltip } from "./ChartTooltip";
import { pickLabelIndices, type ChartSeries } from "./LineChart";

export interface BarChartProps extends Omit<React.HTMLAttributes<HTMLDivElement>, "children"> {
  /** Accessible name of the plot, e.g. "Requests by status class, last hour". */
  label: string;
  /** Longer summary read after the name. */
  description?: string;
  /** X-axis categories, pre-formatted. */
  labels: string[];
  /** Up to 8 series; past that, fold the tail into an "Other" series. */
  series: ChartSeries[];
  /** Stack series into one bar per category (part-to-whole) instead of grouping them side by side. */
  stacked?: boolean;
  /** Plot height in CSS pixels. Width is measured from the container. */
  height?: number;
  /** Formats values in the axis, tooltip and data table. Defaults to compact notation. */
  valueFormat?: (value: number) => string;
  /** Reference lines — an SLO threshold (`value`) or a deploy marker (`label`). */
  annotations?: ChartAnnotation[];
  /** Read for a `null`/missing sample in the cursor readout and data table. Defaults to `"no data"` — override to localize it. */
  noDataLabel?: string;
}

const PAD_LEFT = 44;
const PAD_RIGHT = 10;
const PAD_TOP = 10;
const PAD_BOTTOM = 22;
/** Surface gap between adjacent bars and between stacked segments. */
const MARK_GAP = 2;

/**
 * Categorical bar chart — counts per status class, events per bucket, spend per
 * service. Bars are always measured from a zero baseline, which is why this
 * (unlike `LineChart`) is the wrong form for a metric that hovers in a narrow
 * band far from zero.
 *
 * `stacked` turns the groups into one bar per category: use it when the
 * segments genuinely sum to a meaningful total, and keep it grouped when the
 * reader needs to compare series against each other rather than read a total.
 *
 * The plot has the same cursor as `LineChart` — hover, or focus it and use
 * Arrow/Home/End (Escape clears) — and always renders its data table.
 *
 * `annotations` draws SLO-threshold (`value`) or deploy-marker (`label`)
 * reference lines over the plot — their text is always folded into the
 * visually hidden description too, never sighted-only.
 */
export const BarChart = React.forwardRef<HTMLDivElement, BarChartProps>(
  (
    {
      className,
      label,
      description,
      labels,
      series,
      stacked = false,
      height = 220,
      valueFormat = formatCompact,
      annotations = [],
      noDataLabel = "no data",
      ...props
    },
    ref
  ) => {
    const cursor = useChartCursor(labels.length);
    const plotRef = React.useRef<HTMLDivElement>(null);

    const valueAt = (s: ChartSeries, index: number) => s.values[index] ?? 0;
    const categoryTotals = labels.map((_, index) =>
      series.reduce((sum, s) => sum + valueAt(s, index), 0)
    );
    const highest = stacked
      ? Math.max(0, ...categoryTotals)
      : Math.max(0, ...series.flatMap((s) => s.values.map((v) => v ?? 0)));
    const scale = niceScale(0, highest);

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
    const legendItems = seriesLegendItems(series);

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
              annotations.length > 0
                ? `Reference lines: ${annotations.map((a) => a.text).join(", ")}.`
                : null,
              "Use arrow keys to read individual bars.",
            ]
              .filter(Boolean)
              .join(" ")}
            height={height}
            columns={labels}
            rows={series.map((s) => ({
              header: s.name,
              cells: s.values.map((v) => (v === null ? noDataLabel : valueFormat(v))),
            }))}
          >
            {({ width }) => {
              const plotWidth = Math.max(1, width - PAD_LEFT - PAD_RIGHT);
              const plotHeight = Math.max(1, height - PAD_TOP - PAD_BOTTOM);
              const baselineY = PAD_TOP + plotHeight;
              const band = plotWidth / Math.max(1, labels.length);
              const groupWidth = band * 0.72;
              const barWidth = stacked
                ? groupWidth
                : Math.max(1, (groupWidth - MARK_GAP * (series.length - 1)) / series.length);
              const bandStart = (index: number) =>
                PAD_LEFT + index * band + (band - groupWidth) / 2;
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

                  {labels.map((categoryLabel, index) => {
                    let stackedTop = baselineY;
                    return (
                      <g
                        key={categoryLabel}
                        opacity={active === null || active === index ? 1 : 0.45}
                      >
                        {series.map((s, seriesIndex) => {
                          const value = valueAt(s, index);
                          const barLength = lengthOf(value);
                          if (stacked) {
                            const top = stackedTop - barLength;
                            // The gap is taken out of the segment, not added
                            // between them, so the stack still totals the axis.
                            const visible = Math.max(0, barLength - MARK_GAP);
                            stackedTop = top;
                            return (
                              <path
                                key={s.name}
                                d={barPath(bandStart(index), top, barWidth, visible)}
                                className={seriesFill(seriesIndex)}
                              />
                            );
                          }
                          return (
                            <path
                              key={s.name}
                              d={barPath(
                                bandStart(index) + seriesIndex * (barWidth + MARK_GAP),
                                baselineY - barLength,
                                barWidth,
                                barLength
                              )}
                              className={seriesFill(seriesIndex)}
                            />
                          );
                        })}
                      </g>
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

                  {annotations.map((annotation, i) => {
                    const tone = ANNOTATION_TONE_CLASSES[annotation.tone ?? "neutral"];
                    if (annotation.value !== undefined) {
                      const y = baselineY - lengthOf(annotation.value);
                      return (
                        <g key={i}>
                          <line
                            x1={PAD_LEFT}
                            x2={PAD_LEFT + plotWidth}
                            y1={y}
                            y2={y}
                            className={tone.stroke}
                            strokeWidth={1.5}
                            strokeDasharray="4 3"
                          />
                          <text
                            x={PAD_LEFT + plotWidth}
                            y={y - 4}
                            textAnchor="end"
                            className={cn(tone.text, "text-xs font-sans")}
                          >
                            {annotation.text}
                          </text>
                        </g>
                      );
                    }
                    const index = annotation.label ? labels.indexOf(annotation.label) : -1;
                    if (index === -1) return null;
                    const x = bandStart(index) + groupWidth / 2;
                    return (
                      <g key={i}>
                        <line
                          x1={x}
                          x2={x}
                          y1={PAD_TOP}
                          y2={baselineY}
                          className={tone.stroke}
                          strokeWidth={1.5}
                          strokeDasharray="4 3"
                        />
                        <text
                          x={x + 4}
                          y={PAD_TOP + 10}
                          className={cn(tone.text, "text-xs font-sans")}
                        >
                          {annotation.text}
                        </text>
                      </g>
                    );
                  })}

                  {pickLabelIndices(labels.length, plotWidth).map((index) => (
                    <text
                      key={labels[index]}
                      x={bandStart(index) + groupWidth / 2}
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
            // Physical left/right, not logical start/end — see the matching
            // comment on LineChart's tooltip: the cursor's x position is a
            // physical pixel because the plot never mirrors under RTL, so a
            // logical class here would land the tooltip on the same side as
            // the bar it is supposed to avoid.
            <div
              className={cn(
                "absolute top-2 z-10",
                active > (labels.length - 1) / 2 ? "left-2" : "right-2"
              )}
            >
              <ChartTooltip
                title={labels[active]}
                rows={[
                  ...series.map((s, seriesIndex) => ({
                    label: s.name,
                    seriesIndex,
                    value: valueFormat(valueAt(s, active)),
                  })),
                  ...(stacked && series.length > 1
                    ? [{ label: "Total", value: valueFormat(categoryTotals[active]) }]
                    : []),
                ]}
              />
            </div>
          )}
        </div>

        <div role="status" className="sr-only">
          {formatActiveReading(labels, series, active, () => valueFormat, noDataLabel)}
        </div>

        {legendItems ? <ChartLegend items={legendItems} /> : null}
      </div>
    );
  }
);
BarChart.displayName = "BarChart";
