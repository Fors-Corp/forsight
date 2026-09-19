import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "../lib/cn";
import {
  areaPath,
  barPath,
  formatCompact,
  linePath,
  niceScale,
  project,
  type Point,
} from "../lib/chart";
import { useMeasuredWidth } from "../lib/chart-hooks";

const sparklineVariants = cva("", {
  variants: {
    tone: {
      accent: "stroke-accent fill-accent",
      success: "stroke-success fill-success",
      warning: "stroke-warning fill-warning",
      danger: "stroke-danger fill-danger",
      neutral: "stroke-fg-muted fill-fg-muted",
    },
  },
  defaultVariants: {
    tone: "accent",
  },
});

export interface SparklineProps
  extends
    Omit<React.HTMLAttributes<HTMLDivElement>, "children">,
    VariantProps<typeof sparklineVariants> {
  /** Samples in chronological order. Fewer than two renders nothing but the label. */
  values: number[];
  /** What the trend is of, e.g. "Error rate, last 24 hours". */
  label: string;
  /** `line` for a rate or level; `bar` for counts per bucket. */
  variant?: "line" | "bar";
  /** Height in CSS pixels. Width fills the container. */
  height?: number;
  /** Formats the min/max/latest numbers in the accessible summary. */
  valueFormat?: (value: number) => string;
}

/**
 * Axis-less trend line, sized to sit inside a `StatCard`, a table cell or a
 * list row. It answers "which way is this going", not "what is the value at
 * 14:20" — when the reader needs to read points, use `LineChart`.
 *
 * Unlike the plotted charts it has no data table: at this size the shape is the
 * message, so the accessible name carries the summary a reader actually needs
 * (sample count, min, max, latest) instead of forty unreadable cells.
 */
export const Sparkline = React.memo(
  React.forwardRef<HTMLDivElement, SparklineProps>(
    (
      {
        className,
        values,
        label,
        tone,
        variant = "line",
        height = 36,
        valueFormat = formatCompact,
        ...props
      },
      ref
    ) => {
      const [measureRef, width] = useMeasuredWidth<HTMLDivElement>(120);
      const titleId = React.useId();

      // Keyed on the props that actually change these — a re-render this
      // instance didn't ask for (an unrelated sibling in a table/list of many
      // sparklines re-rendering its own row) must not redo the scale, the
      // summary string or the point projection.
      const { summary, points, barPaths } = React.useMemo(() => {
        const min = values.length > 0 ? Math.min(...values) : 0;
        const max = values.length > 0 ? Math.max(...values) : 0;
        const scale = niceScale(variant === "bar" ? Math.min(0, min) : min, max, 2);
        const summary =
          values.length === 0
            ? `${label}: no data`
            : `${label}: ${values.length} samples, low ${valueFormat(min)}, high ${valueFormat(
                max
              )}, latest ${valueFormat(values[values.length - 1])}`;

        const yAt = (value: number) =>
          height - 2 - project(value, scale.min, scale.max, height - 4);
        const xAt = (index: number) =>
          values.length < 2 ? width / 2 : (index / (values.length - 1)) * width;
        const points: Point[] = values.map((value, index) => [xAt(index), yAt(value)]);
        const barPaths = values.map((value, index) => {
          const barWidth = Math.max(1, width / values.length - 2);
          const barLength = Math.max(0, height - 2 - yAt(value));
          return barPath((index * width) / values.length, yAt(value), barWidth, barLength, 2);
        });

        return { summary, points, barPaths };
      }, [values, variant, height, valueFormat, label, width]);

      return (
        <div ref={ref} className={cn("w-full min-w-0", className)} {...props}>
          <div ref={measureRef} className="w-full">
            <svg
              role="img"
              aria-labelledby={titleId}
              width={width}
              height={height}
              viewBox={`0 0 ${width} ${height}`}
              className="block max-w-full"
            >
              <title id={titleId}>{summary}</title>
              {variant === "bar" ? (
                barPaths.map((d, index) => (
                  <path
                    key={index}
                    d={d}
                    className={cn(sparklineVariants({ tone }), "stroke-none")}
                  />
                ))
              ) : (
                <>
                  <path
                    d={areaPath(points, height)}
                    className={cn(sparklineVariants({ tone }), "stroke-none opacity-15")}
                  />
                  <path
                    d={linePath(points)}
                    fill="none"
                    strokeWidth={2}
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    className={sparklineVariants({ tone })}
                  />
                </>
              )}
            </svg>
          </div>
        </div>
      );
    }
  )
);
Sparkline.displayName = "Sparkline";
