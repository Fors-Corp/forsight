import * as React from "react";
import { cn } from "../lib/cn";
import { clamp, formatCompact, niceScale, project } from "../lib/chart";
import { useChartCursor } from "../lib/chart-hooks";
import { ChartFrame } from "./ChartFrame";
import { ChartTooltip } from "./ChartTooltip";
import { pickLabelIndices } from "./LineChart";

export interface BoxPlotBox {
  label: string;
  min: number;
  q1: number;
  median: number;
  q3: number;
  max: number;
}

export interface BoxPlotProps extends Omit<React.HTMLAttributes<HTMLDivElement>, "children"> {
  /** Accessible name of the plot, e.g. "p95 latency spread by service, last hour". */
  label: string;
  /** Longer summary read after the name. */
  description?: string;
  /** One box-and-whisker per category. */
  boxes: BoxPlotBox[];
  /** Plot height in CSS pixels. Width is measured from the container. */
  height?: number;
  /** Formats the axis, tooltip and data table. Defaults to compact notation. */
  valueFormat?: (value: number) => string;
}

const PAD_LEFT = 44;
const PAD_RIGHT = 10;
const PAD_TOP = 10;
const PAD_BOTTOM = 22;

/**
 * Five-number summary (min / Q1 / median / Q3 / max) per category — the
 * shape a latency or duration percentile spread needs and `LineChart`/
 * `BarChart` can't show: how wide the middle 50% of samples is, not just
 * one summary number per category. Like `LineChart` and unlike `BarChart`,
 * it is not pinned to a zero baseline — a spread's shape matters, not its
 * distance from zero. Every box is the same accent color (like `Histogram`/
 * `Sparkline`): a box plot compares one metric's spread *across*
 * categories, not several different metrics against each other, so there's
 * no series identity to carry.
 *
 * The plot has the same cursor as the other charts — hover, or focus it and
 * use Arrow/Home/End (Escape clears) — and always renders its data table.
 */
export const BoxPlot = React.forwardRef<HTMLDivElement, BoxPlotProps>(
  (
    { className, label, description, boxes, height = 240, valueFormat = formatCompact, ...props },
    ref
  ) => {
    const cursor = useChartCursor(boxes.length);
    const plotRef = React.useRef<HTMLDivElement>(null);

    let dataMin = Infinity;
    let dataMax = -Infinity;
    for (const box of boxes) {
      if (box.min < dataMin) dataMin = box.min;
      if (box.max > dataMax) dataMax = box.max;
    }
    const scale = niceScale(dataMin, dataMax);

    const handlePointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
      const box = plotRef.current?.getBoundingClientRect();
      if (!box || boxes.length === 0) return;
      const plotWidth = Math.max(1, box.width - PAD_LEFT - PAD_RIGHT);
      const band = plotWidth / boxes.length;
      const index = Math.floor((event.clientX - box.left - PAD_LEFT) / band);
      if (!Number.isFinite(index)) return;
      cursor.setActive(clamp(index, 0, boxes.length - 1));
    };

    const active = cursor.active;
    const labels = boxes.map((b) => b.label);

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
            description={[description, "Use arrow keys to read individual boxes."]
              .filter(Boolean)
              .join(" ")}
            height={height}
            columns={["Min", "Q1", "Median", "Q3", "Max"]}
            rows={boxes.map((b) => ({
              header: b.label,
              cells: [b.min, b.q1, b.median, b.q3, b.max].map(valueFormat),
            }))}
          >
            {({ width }) => {
              const plotWidth = Math.max(1, width - PAD_LEFT - PAD_RIGHT);
              const plotHeight = Math.max(1, height - PAD_TOP - PAD_BOTTOM);
              const baselineY = PAD_TOP + plotHeight;
              const band = plotWidth / Math.max(1, boxes.length);
              const boxWidth = Math.max(4, band * 0.4);
              const capWidth = boxWidth * 0.5;
              const centerOf = (index: number) => PAD_LEFT + index * band + band / 2;
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

                  {boxes.map((box, index) => {
                    const cx = centerOf(index);
                    const isDimmed = active !== null && active !== index;
                    return (
                      <g key={box.label} opacity={isDimmed ? 0.45 : 1}>
                        <line
                          x1={cx}
                          x2={cx}
                          y1={yAt(box.max)}
                          y2={yAt(box.min)}
                          className="stroke-accent"
                          strokeWidth={1.5}
                        />
                        <line
                          x1={cx - capWidth / 2}
                          x2={cx + capWidth / 2}
                          y1={yAt(box.max)}
                          y2={yAt(box.max)}
                          className="stroke-accent"
                          strokeWidth={1.5}
                        />
                        <line
                          x1={cx - capWidth / 2}
                          x2={cx + capWidth / 2}
                          y1={yAt(box.min)}
                          y2={yAt(box.min)}
                          className="stroke-accent"
                          strokeWidth={1.5}
                        />
                        <rect
                          x={cx - boxWidth / 2}
                          y={yAt(box.q3)}
                          width={boxWidth}
                          height={Math.max(1, yAt(box.q1) - yAt(box.q3))}
                          className="fill-accent-subtle stroke-accent"
                          strokeWidth={1.5}
                        />
                        <line
                          x1={cx - boxWidth / 2}
                          x2={cx + boxWidth / 2}
                          y1={yAt(box.median)}
                          y2={yAt(box.median)}
                          className="stroke-accent"
                          strokeWidth={2}
                        />
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
                </>
              );
            }}
          </ChartFrame>

          {active === null ? null : (
            // Parked on the side opposite the cursor so the readout never
            // covers the box being read. Physical left/right, not logical
            // start/end: the cursor's x position is a physical pixel (the
            // plot never mirrors under RTL — see ChartFrame), so which side
            // avoids it is a physical question too. A logical class would
            // resolve to the same side as the box under `dir="rtl"`.
            <div
              className={cn(
                "absolute top-2 z-10",
                active > (boxes.length - 1) / 2 ? "left-2" : "right-2"
              )}
            >
              <ChartTooltip
                title={labels[active]}
                rows={[
                  { label: "Max", value: valueFormat(boxes[active].max) },
                  { label: "Q3", value: valueFormat(boxes[active].q3) },
                  { label: "Median", value: valueFormat(boxes[active].median) },
                  { label: "Q1", value: valueFormat(boxes[active].q1) },
                  { label: "Min", value: valueFormat(boxes[active].min) },
                ]}
              />
            </div>
          )}
        </div>

        <div role="status" className="sr-only">
          {active === null
            ? ""
            : `${labels[active]}: median ${valueFormat(boxes[active].median)}, Q1 ${valueFormat(
                boxes[active].q1
              )}, Q3 ${valueFormat(boxes[active].q3)}, min ${valueFormat(
                boxes[active].min
              )}, max ${valueFormat(boxes[active].max)}`}
        </div>
      </div>
    );
  }
);
BoxPlot.displayName = "BoxPlot";
