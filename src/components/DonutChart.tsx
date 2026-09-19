import * as React from "react";
import { cn } from "../lib/cn";
import { arcPath, formatCompact, formatPercent, seriesFill } from "../lib/chart";
import { useChartCursor } from "../lib/chart-hooks";
import { ChartFrame } from "./ChartFrame";
import { ChartLegend } from "./ChartLegend";

export interface DonutSlice {
  name: string;
  value: number;
}

export interface DonutChartProps extends Omit<React.HTMLAttributes<HTMLDivElement>, "children"> {
  /** Accessible name, e.g. "Traffic by region". */
  label: string;
  /** Longer summary read after the name. */
  description?: string;
  /** Up to 8 slices; aggregate the tail into an explicit "Other" slice. */
  data: DonutSlice[];
  /** Ring diameter in CSS pixels. */
  size?: number;
  /** Big number in the middle. Defaults to the total of every slice. */
  centerValue?: string;
  /** Caption under the center number, e.g. "requests". */
  centerLabel?: string;
  /** Formats slice values in the legend, tooltip and data table. */
  valueFormat?: (value: number) => string;
}

/**
 * Part-to-whole ring — traffic by region, spend by service, error classes as a
 * share of total. Only use it when the slices genuinely sum to a meaningful
 * whole and there are few of them; for a ranking, `BarList` is easier to read,
 * and for change over time a stacked `BarChart` is.
 *
 * The middle is not decoration: it carries the total, which is the number most
 * readers actually want. Every slice's share is printed in the legend, so no
 * one has to judge angles.
 */
export const DonutChart = React.memo(
  React.forwardRef<HTMLDivElement, DonutChartProps>(
    (
      {
        className,
        label,
        description,
        data,
        size = 200,
        centerValue,
        centerLabel,
        valueFormat = formatCompact,
        ...props
      },
      ref
    ) => {
      const cursor = useChartCursor(data.length);
      // Keyed on `data` alone — hovering a slice re-renders this component
      // without the totals actually changing.
      const total = React.useMemo(() => data.reduce((sum, slice) => sum + slice.value, 0), [data]);
      const shareOf = (value: number) => (total > 0 ? (value / total) * 100 : 0);
      const active = cursor.active;

      return (
        <div
          ref={ref}
          className={cn("flex w-full min-w-0 flex-col items-center gap-3", className)}
          {...props}
        >
          <div
            tabIndex={0}
            onKeyDown={cursor.onKeyDown}
            onBlur={cursor.onBlur}
            className="w-full rounded-md focus-visible:outline-none focus-visible:shadow-focus-ring"
          >
            <ChartFrame
              label={label}
              description={[description, "Use arrow keys to read individual slices."]
                .filter(Boolean)
                .join(" ")}
              height={size}
              columns={["Value", "Share"]}
              rows={data.map((slice) => ({
                header: slice.name,
                cells: [valueFormat(slice.value), formatPercent(shareOf(slice.value))],
              }))}
            >
              {({ width }) => {
                const cx = width / 2;
                const cy = size / 2;
                const outer = Math.max(8, Math.min(width, size) / 2 - 2);
                const inner = outer * 0.62;
                let angle = 0;

                return (
                  <>
                    {total === 0 ? (
                      <circle
                        cx={cx}
                        cy={cy}
                        r={(outer + inner) / 2}
                        fill="none"
                        strokeWidth={outer - inner}
                        className="stroke-ink-surface-2"
                      />
                    ) : (
                      data.map((slice, index) => {
                        const sweep = (slice.value / total) * 360;
                        const path = arcPath(cx, cy, outer, inner, angle, angle + sweep);
                        angle += sweep;
                        return (
                          <path
                            key={slice.name}
                            d={path}
                            strokeWidth={2}
                            onPointerEnter={() => cursor.setActive(index)}
                            onPointerLeave={() => cursor.setActive(null)}
                            className={cn(
                              seriesFill(index),
                              "stroke-ink-bg",
                              active !== null && active !== index && "opacity-45"
                            )}
                          />
                        );
                      })
                    )}
                    <text
                      x={cx}
                      y={cy}
                      textAnchor="middle"
                      dominantBaseline="middle"
                      className="fill-fg text-xl font-semibold font-heading"
                    >
                      {active === null
                        ? (centerValue ?? valueFormat(total))
                        : formatPercent(shareOf(data[active].value))}
                    </text>
                    {centerLabel === undefined && active === null ? null : (
                      <text
                        x={cx}
                        y={cy + 20}
                        textAnchor="middle"
                        dominantBaseline="middle"
                        className="fill-fg-muted text-xs font-sans"
                      >
                        {active === null ? centerLabel : data[active].name}
                      </text>
                    )}
                  </>
                );
              }}
            </ChartFrame>
          </div>

          <div role="status" className="sr-only">
            {active === null
              ? ""
              : `${data[active].name}: ${valueFormat(data[active].value)}, ${formatPercent(
                  shareOf(data[active].value)
                )} of total`}
          </div>

          <ChartLegend
            className="justify-center"
            items={data.map((slice, index) => ({
              label: slice.name,
              seriesIndex: index,
              value: formatPercent(shareOf(slice.value)),
            }))}
          />
        </div>
      );
    }
  )
);
DonutChart.displayName = "DonutChart";
