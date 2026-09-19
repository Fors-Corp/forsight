import * as React from "react";
import { cn } from "../lib/cn";
import { formatCompact } from "../lib/chart";

export interface HeatmapRow {
  /** Row name — a service, a host, a weekday. */
  label: string;
  /** One value per column. `null` renders an explicit "no data" cell. */
  values: Array<number | null>;
}

export interface HeatmapProps extends Omit<React.HTMLAttributes<HTMLTableElement>, "children"> {
  /** Accessible name / visible caption, e.g. "Errors by service and hour". */
  label: string;
  /** Column headers, pre-formatted (hours, days, buckets). */
  columns: string[];
  rows: HeatmapRow[];
  /** Formats cell values in their accessible names and the scale legend. */
  valueFormat?: (value: number) => string;
  /** Scale ceiling. Defaults to the largest value in the data. */
  max?: number;
  /** Read for a `null`/missing cell's title and accessible name. Defaults to `"no data"` — override to localize it. */
  noDataLabel?: string;
}

/**
 * Density matrix — errors by service × hour, latency by region × day, activity
 * calendars. Magnitude is encoded as one hue getting darker, never as a rainbow:
 * a reader can order five steps of one color, but not five hues.
 *
 * It renders as a real `<table>` with a row/column header for every cell, so
 * screen-reader users navigate it as the grid it is and each cell announces its
 * own value — there is no separate "data table" alternative because this *is*
 * the data table.
 */
export const Heatmap = React.forwardRef<HTMLTableElement, HeatmapProps>(
  (
    {
      className,
      label,
      columns,
      rows,
      valueFormat = formatCompact,
      max,
      noDataLabel = "no data",
      ...props
    },
    ref
  ) => {
    const ceiling =
      max ??
      Math.max(
        0,
        ...rows.flatMap((row) => row.values.map((value) => (value === null ? 0 : value)))
      );

    return (
      <div className="w-full overflow-x-auto">
        <table
          ref={ref}
          className={cn(
            "w-full min-w-max border-separate border-spacing-0.5 text-xs font-sans",
            className
          )}
          {...props}
        >
          <caption className="mb-2 text-start text-sm font-medium text-fg">{label}</caption>
          <thead>
            <tr>
              <th scope="col" className="sr-only">
                Row
              </th>
              {columns.map((column) => (
                <th
                  key={column}
                  scope="col"
                  className="px-1 pb-1 text-center font-normal text-fg-muted"
                >
                  {column}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.label}>
                <th
                  scope="row"
                  className="pe-2 text-start font-normal text-fg-secondary whitespace-nowrap"
                >
                  {row.label}
                </th>
                {row.values.map((value, index) => (
                  <td key={`${row.label}-${columns[index] ?? index}`} className="p-0">
                    <div
                      title={
                        value === null
                          ? noDataLabel
                          : `${row.label}, ${columns[index] ?? index}: ${valueFormat(value)}`
                      }
                      className="relative h-6 w-6 overflow-hidden rounded-sm bg-ink-surface-2"
                    >
                      {value === null ? null : (
                        <span
                          aria-hidden="true"
                          className={cn(
                            "absolute inset-0 bg-accent",
                            intensityClass(value, ceiling)
                          )}
                        />
                      )}
                      <span className="sr-only">
                        {value === null ? noDataLabel : valueFormat(value)}
                      </span>
                    </div>
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
        <div className="mt-2 flex items-center gap-2 text-xs font-sans text-fg-muted">
          <span>0</span>
          {INTENSITY_STEPS.map((step) => (
            <span
              key={step}
              className="relative h-3 w-6 overflow-hidden rounded-sm bg-ink-surface-2"
            >
              <span aria-hidden="true" className={cn("absolute inset-0 bg-accent", step)} />
            </span>
          ))}
          <span>{valueFormat(ceiling)}</span>
        </div>
      </div>
    );
  }
);
Heatmap.displayName = "Heatmap";

/**
 * Five ordinal steps of a single hue. Opacity lives on a fill layer behind the
 * cell rather than on the cell itself, so the step never dims the text or the
 * focus ring drawn over it.
 */
const INTENSITY_STEPS = ["opacity-15", "opacity-30", "opacity-50", "opacity-70", "opacity-100"];

function intensityClass(value: number, ceiling: number): string {
  if (ceiling <= 0 || value <= 0) return "opacity-0";
  const step = Math.ceil((value / ceiling) * INTENSITY_STEPS.length);
  return INTENSITY_STEPS[Math.min(step, INTENSITY_STEPS.length) - 1];
}
