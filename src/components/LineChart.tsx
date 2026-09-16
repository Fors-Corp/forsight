import * as React from "react";
import { cn } from "../lib/cn";
import {
  ANNOTATION_TONE_CLASSES,
  areaPath,
  clamp,
  formatCompact,
  linePath,
  niceScale,
  project,
  seriesFill,
  seriesStroke,
  splitAtGaps,
  splitAtProjection,
  type ChartAnnotation,
} from "../lib/chart";
import { useChartCursor } from "../lib/chart-hooks";
import { ChartFrame } from "./ChartFrame";
import { ChartLegend } from "./ChartLegend";
import { ChartTooltip } from "./ChartTooltip";

export interface ChartSeries {
  /** Series name — shown in the legend, the tooltip and the data table. */
  name: string;
  /** One value per `labels` entry. `null` leaves a gap instead of interpolating. */
  values: Array<number | null>;
  /**
   * Index into `values` of the last point to draw solid — the
   * observed/forecast boundary. The stroke from that point onward (the
   * point itself is shared by both runs, so the line has no gap where it
   * switches) is dashed instead of a second color, distinguishing a
   * projection by shape rather than color (CONTRIBUTING.md's data-viz
   * rule). The text equivalent ("<name> is projected from <label>.") is
   * folded into `ChartFrame`'s hidden description, so the distinction is
   * never sighted-only.
   */
  dashedFrom?: number;
}

export interface LineChartProps extends Omit<React.HTMLAttributes<HTMLDivElement>, "children"> {
  /** Accessible name of the plot, e.g. "p95 latency, last 6 hours". */
  label: string;
  /** Longer summary read after the name — the window, the unit, the takeaway. */
  description?: string;
  /** X-axis categories, pre-formatted (timestamps, buckets). */
  labels: string[];
  /** Up to 8 series; past that, fold the tail into an "Other" series. */
  series: ChartSeries[];
  /** Fill the space under each line. Use for a single series only — stacked-looking overlaps mislead. */
  area?: boolean;
  /** Plot height in CSS pixels. Width is measured from the container. */
  height?: number;
  /** Formats values in the axis, tooltip and data table. Defaults to compact notation. */
  valueFormat?: (value: number) => string;
  /** Reference lines — an SLO threshold (`value`) or a deploy marker (`label`). */
  annotations?: ChartAnnotation[];
}

const PAD_LEFT = 44;
const PAD_RIGHT = 10;
const PAD_TOP = 10;
const PAD_BOTTOM = 22;

/**
 * Time-series line (or area) chart for one to eight series — request rates,
 * latency percentiles, error counts, saturation over a window.
 *
 * Use `area` only with a single series: overlapping translucent fills read as
 * a stacked total that isn't one. For part-to-whole over time, prefer a stacked
 * `BarChart`; for a single headline number, prefer `StatCard`.
 *
 * The plot has a cursor: hovering, or focusing it and pressing Arrow keys
 * (Home/End to jump, Escape to clear), reads out every series at that point.
 * The same numbers are always available as the frame's data table.
 *
 * `annotations` draws SLO-threshold (`value`) or deploy-marker (`label`)
 * reference lines over the plot — their text is always folded into the
 * visually hidden description too, never sighted-only.
 *
 * A series' `dashedFrom` index marks a forecast or projection: the stroke
 * past that point switches to dashed instead of a second color, so the
 * distinction survives grayscale printing and colorblind vision. Both runs
 * share one y-axis and domain — a projection is drawn as more of the same
 * series, not a second one.
 */
export const LineChart = React.forwardRef<HTMLDivElement, LineChartProps>(
  (
    {
      className,
      label,
      description,
      labels,
      series,
      area = false,
      height = 220,
      valueFormat = formatCompact,
      annotations = [],
      ...props
    },
    ref
  ) => {
    const cursor = useChartCursor(labels.length);
    const plotRef = React.useRef<HTMLDivElement>(null);

    // Extent of the actual data — a line chart reads change, so it is not
    // pinned to a zero baseline the way a bar chart must be.
    let dataMin = Infinity;
    let dataMax = -Infinity;
    for (const s of series) {
      for (const value of s.values) {
        if (value === null) continue;
        if (value < dataMin) dataMin = value;
        if (value > dataMax) dataMax = value;
      }
    }
    const scale = niceScale(dataMin, dataMax);

    // Text equivalent of the dashed-stroke projection, folded into the
    // hidden description so the distinction is never sighted-only.
    const projectionNotes = series
      .filter((s) => s.dashedFrom !== undefined && labels[s.dashedFrom] !== undefined)
      .map((s) => `${s.name} is projected from ${labels[s.dashedFrom as number]}.`);

    const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
      const box = plotRef.current?.getBoundingClientRect();
      if (!box || labels.length === 0) return;
      const plotWidth = Math.max(1, box.width - PAD_LEFT - PAD_RIGHT);
      const ratio = (event.clientX - box.left - PAD_LEFT) / plotWidth;
      const index = Math.round(ratio * (labels.length - 1));
      // A pointer event without coordinates (synthetic, or a device that
      // reports none) must not push a NaN index into the geometry.
      if (!Number.isFinite(index)) return;
      cursor.setActive(clamp(index, 0, labels.length - 1));
    };

    const active = cursor.active;
    const activeReading =
      active === null
        ? ""
        : `${labels[active]}: ${series
            .map((s) => {
              const value = s.values[active];
              return `${s.name} ${value === null || value === undefined ? "no data" : valueFormat(value)}`;
            })
            .join(", ")}`;

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
              projectionNotes.length > 0 ? projectionNotes.join(" ") : null,
              "Use arrow keys to read individual points.",
            ]
              .filter(Boolean)
              .join(" ")}
            height={height}
            columns={labels}
            rows={series.map((s) => ({
              header: s.name,
              cells: s.values.map((v) => (v === null ? "no data" : valueFormat(v))),
            }))}
          >
            {({ width }) => {
              const plotWidth = Math.max(1, width - PAD_LEFT - PAD_RIGHT);
              const plotHeight = Math.max(1, height - PAD_TOP - PAD_BOTTOM);
              const baselineY = PAD_TOP + plotHeight;
              const xAt = (index: number) =>
                PAD_LEFT +
                (labels.length < 2 ? plotWidth / 2 : (index / (labels.length - 1)) * plotWidth);
              const yAt = (value: number) =>
                baselineY - project(value, scale.min, scale.max, plotHeight);

              return (
                <>
                  {scale.ticks.map((tick) => (
                    <g key={tick}>
                      <line
                        x1={PAD_LEFT}
                        x2={PAD_LEFT + plotWidth}
                        y1={yAt(tick)}
                        y2={yAt(tick)}
                        className="stroke-ink-border-subtle"
                        strokeWidth={1}
                      />
                      <text
                        x={PAD_LEFT - 8}
                        y={yAt(tick)}
                        textAnchor="end"
                        dominantBaseline="middle"
                        className="fill-fg-muted text-xs font-sans"
                      >
                        {valueFormat(tick)}
                      </text>
                    </g>
                  ))}

                  {pickLabelIndices(labels.length, plotWidth).map((index) => (
                    <text
                      key={labels[index]}
                      x={xAt(index)}
                      y={height - 6}
                      textAnchor={
                        index === 0 ? "start" : index === labels.length - 1 ? "end" : "middle"
                      }
                      className="fill-fg-muted text-xs font-sans"
                    >
                      {labels[index]}
                    </text>
                  ))}

                  {series.map((s, seriesIndex) => {
                    const toPoint = (index: number, value: number): [number, number] => [
                      xAt(index),
                      yAt(value),
                    ];
                    const { solid, dashed } = splitAtProjection(s.values, s.dashedFrom, toPoint);
                    return (
                      <g key={s.name}>
                        {area
                          ? splitAtGaps(s.values, toPoint).map((segment, i) => (
                              <path
                                key={`area-${i}`}
                                d={areaPath(segment, baselineY)}
                                className={cn(seriesFill(seriesIndex), "opacity-20")}
                              />
                            ))
                          : null}
                        {solid.map((segment, i) => (
                          <path
                            key={`line-solid-${i}`}
                            d={linePath(segment)}
                            fill="none"
                            strokeWidth={2}
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            className={seriesStroke(seriesIndex)}
                          />
                        ))}
                        {dashed.map((segment, i) => (
                          <path
                            key={`line-dashed-${i}`}
                            d={linePath(segment)}
                            fill="none"
                            strokeWidth={2}
                            strokeLinecap="round"
                            strokeLinejoin="round"
                            strokeDasharray="6 4"
                            className={seriesStroke(seriesIndex)}
                          />
                        ))}
                      </g>
                    );
                  })}

                  {annotations.map((annotation, i) => {
                    const tone = ANNOTATION_TONE_CLASSES[annotation.tone ?? "neutral"];
                    if (annotation.value !== undefined) {
                      const y = yAt(annotation.value);
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
                    const x = xAt(index);
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

                  {active === null ? null : (
                    <g>
                      <line
                        x1={xAt(active)}
                        x2={xAt(active)}
                        y1={PAD_TOP}
                        y2={baselineY}
                        className="stroke-fg-muted"
                        strokeWidth={1}
                        strokeDasharray="3 3"
                      />
                      {series.map((s, seriesIndex) => {
                        const value = s.values[active];
                        if (value === null || value === undefined) return null;
                        return (
                          <circle
                            key={s.name}
                            cx={xAt(active)}
                            cy={yAt(value)}
                            r={4}
                            strokeWidth={2}
                            className={cn(seriesFill(seriesIndex), "stroke-ink-bg")}
                          />
                        );
                      })}
                    </g>
                  )}
                </>
              );
            }}
          </ChartFrame>

          {active === null ? null : (
            // Parked on the side opposite the cursor so the readout never
            // covers the point being read.
            <div
              className={cn(
                "absolute top-2 z-10",
                active > (labels.length - 1) / 2 ? "start-2" : "end-2"
              )}
            >
              <ChartTooltip
                title={labels[active]}
                rows={series.map((s, seriesIndex) => ({
                  label: s.name,
                  seriesIndex,
                  value:
                    s.values[active] === null || s.values[active] === undefined
                      ? "no data"
                      : valueFormat(s.values[active] as number),
                }))}
              />
            </div>
          )}
        </div>

        <div role="status" className="sr-only">
          {activeReading}
        </div>

        {series.length > 1 ? (
          <ChartLegend items={series.map((s, seriesIndex) => ({ label: s.name, seriesIndex }))} />
        ) : null}
      </div>
    );
  }
);
LineChart.displayName = "LineChart";

/** Evenly spaced x-label indices that fit the plot width without colliding. */
export function pickLabelIndices(count: number, plotWidth: number): number[] {
  if (count === 0) return [];
  const maxLabels = clamp(Math.floor(plotWidth / 72), 2, 6);
  if (count <= maxLabels) return Array.from({ length: count }, (_, i) => i);
  const step = (count - 1) / (maxLabels - 1);
  return Array.from({ length: maxLabels }, (_, i) => Math.round(i * step));
}
