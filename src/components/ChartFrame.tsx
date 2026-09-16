import * as React from "react";
import { cn } from "../lib/cn";
import { useMeasuredWidth } from "../lib/chart-hooks";

export interface ChartTableRow {
  /** Row header — the category, timestamp or series name this row describes. */
  header: string;
  /** Already-formatted cell values, one per column. */
  cells: string[];
}

export interface ChartFrameProps extends Omit<
  React.HTMLAttributes<HTMLDivElement>,
  "children" | "role"
> {
  /** Accessible name of the plot, e.g. "Request rate, last 24 hours". */
  label: string;
  /** Optional longer summary — the trend, the range, the units. */
  description?: string;
  /** Plot height in CSS pixels. Width is measured from the container. */
  height?: number;
  /** Column headers of the screen-reader data table (excluding the row header column). */
  columns: string[];
  /** Data rows behind the plot, rendered as a visually hidden table. */
  rows: ChartTableRow[];
  /** Draws the marks. Receives the measured plot box in real pixels. */
  children: (size: { width: number; height: number }) => React.ReactNode;
}

/**
 * The shared shell every chart in this library renders into: a measured,
 * responsive `<svg>` plus the same plot's data as a visually hidden `<table>`.
 *
 * Use it directly when you need a chart form this package doesn't ship — it
 * gives a custom plot the accessibility contract the built-in charts have.
 * A chart is a picture to a sighted reader and a table to everyone else, so the
 * table is not optional: it is the non-visual equivalent required by WCAG 1.1.1,
 * and it is what makes the sub-3:1 light-theme series colors permissible.
 *
 * The plot is drawn at measured pixel width rather than a scaled viewBox, so
 * axis text stays at its real size down to a 320px viewport. Time flows
 * left-to-right in both text directions (the numeric-axis convention); the
 * surrounding chrome follows the document direction.
 */
export const ChartFrame = React.forwardRef<HTMLDivElement, ChartFrameProps>(
  ({ className, label, description, height = 200, columns, rows, children, ...props }, ref) => {
    const [measureRef, width] = useMeasuredWidth<HTMLDivElement>(640);
    const titleId = React.useId();
    const descId = React.useId();

    return (
      <div ref={ref} className={cn("w-full min-w-0", className)} {...props}>
        <div ref={measureRef} className="w-full">
          <svg
            role="img"
            aria-labelledby={titleId}
            aria-describedby={description ? descId : undefined}
            width={width}
            height={height}
            viewBox={`0 0 ${width} ${height}`}
            // SVG resolves `text-anchor="start"/"end"` against the inherited
            // CSS `direction`, not a fixed physical side — so under
            // `dir="rtl"` every axis-value and annotation label drawn with
            // those anchors (LineChart, BarChart) flips onto the wrong edge
            // and lands on top of the plot instead of in its margin. Pinning
            // the svg's own direction keeps that resolution physical, which
            // is what "time flows left-to-right in both text directions"
            // above already assumes; nothing outside the svg is affected.
            style={{ direction: "ltr" }}
            className="block max-w-full"
          >
            <title id={titleId}>{label}</title>
            {description ? <desc id={descId}>{description}</desc> : null}
            {children({ width, height })}
          </svg>
        </div>
        <table className="sr-only">
          <caption>{description ? `${label}. ${description}` : label}</caption>
          <thead>
            <tr>
              <th scope="col">Category</th>
              {columns.map((column) => (
                <th key={column} scope="col">
                  {column}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.header}>
                <th scope="row">{row.header}</th>
                {row.cells.map((cell, i) => (
                  <td key={`${row.header}-${columns[i] ?? i}`}>{cell}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }
);
ChartFrame.displayName = "ChartFrame";
