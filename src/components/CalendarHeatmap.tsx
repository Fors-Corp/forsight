import * as React from "react";
import { formatCompact } from "../lib/chart";
import { Heatmap, type HeatmapRow } from "./Heatmap";

export interface CalendarHeatmapDay {
  /** ISO date, `"YYYY-MM-DD"`. */
  date: string;
  /** Count for that day — incidents, deploys, whatever the calendar tracks. */
  value: number;
}

export interface CalendarHeatmapProps extends Omit<
  React.HTMLAttributes<HTMLTableElement>,
  "children"
> {
  /** Accessible name / visible caption, e.g. "Incidents per day, last 6 months". */
  label: string;
  /** One entry per tracked day, any order. A date with no entry renders as an untracked (empty) cell, not a zero. */
  days: CalendarHeatmapDay[];
  /** Formats cell values in their accessible names and the scale legend. */
  valueFormat?: (value: number) => string;
  /** Scale ceiling. Defaults to the largest value in `days`. */
  max?: number;
}

// Computed, not written out. These are the visible <th scope="row"> of the
// grid and part of every cell's title, and they sat one line above a correct
// Intl.DateTimeFormat for the month labels — so a German reader got German
// months beside English weekdays. Any Sunday works as the anchor; 2026-01-04
// is one, in UTC to match weekLabelFormat and the UTC bucketing below.
const WEEKDAY_LABELS = (() => {
  const format = new Intl.DateTimeFormat(undefined, { weekday: "short", timeZone: "UTC" });
  const firstSunday = Date.UTC(2026, 0, 4);
  return Array.from({ length: 7 }, (_, day) =>
    format.format(new Date(firstSunday + day * 86_400_000))
  );
})();

const weekLabelFormat = new Intl.DateTimeFormat(undefined, {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});

/**
 * Year-in-review activity calendar — incident or deploy frequency by day,
 * GitHub-contributions-style. Unlike `Heatmap` (a density *matrix* you
 * already have arranged into rows/columns), this takes a flat, unordered
 * list of `{ date, value }` days and does the calendar bucketing itself:
 * weeks as columns (Sunday-start), weekdays as rows. It renders by handing
 * that grid to `Heatmap`, so it gets the exact same real `<table>`,
 * five-step intensity scale, and legend — this component's only job is the
 * date math. A day with no entry in `days` is untracked (an empty cell),
 * distinct from a tracked day whose count is zero.
 */
export const CalendarHeatmap = React.forwardRef<HTMLTableElement, CalendarHeatmapProps>(
  ({ label, days, valueFormat = formatCompact, max, ...props }, ref) => {
    const { weekLabels, rows } = React.useMemo(() => buildWeeks(days), [days]);

    return (
      <Heatmap
        ref={ref}
        label={label}
        columns={weekLabels}
        rows={rows}
        valueFormat={valueFormat}
        max={max}
        {...props}
      />
    );
  }
);
CalendarHeatmap.displayName = "CalendarHeatmap";

function buildWeeks(days: CalendarHeatmapDay[]): { weekLabels: string[]; rows: HeatmapRow[] } {
  if (days.length === 0)
    return { weekLabels: [], rows: WEEKDAY_LABELS.map((label) => ({ label, values: [] })) };

  const values = new Map(days.map((d) => [d.date, d.value]));
  const sortedDates = [...values.keys()].sort();
  const start = new Date(`${sortedDates[0]}T00:00:00Z`);
  const end = new Date(`${sortedDates[sortedDates.length - 1]}T00:00:00Z`);

  const gridStart = new Date(start);
  gridStart.setUTCDate(gridStart.getUTCDate() - gridStart.getUTCDay());
  const gridEnd = new Date(end);
  gridEnd.setUTCDate(gridEnd.getUTCDate() + (6 - gridEnd.getUTCDay()));

  const weekStarts: Date[] = [];
  for (
    const cursor = new Date(gridStart);
    cursor <= gridEnd;
    cursor.setUTCDate(cursor.getUTCDate() + 7)
  ) {
    weekStarts.push(new Date(cursor));
  }

  const rows: HeatmapRow[] = WEEKDAY_LABELS.map((label, weekday) => ({
    label,
    values: weekStarts.map((weekStart) => {
      const cellDate = new Date(weekStart);
      cellDate.setUTCDate(cellDate.getUTCDate() + weekday);
      const iso = cellDate.toISOString().slice(0, 10);
      return values.has(iso) ? (values.get(iso) as number) : null;
    }),
  }));

  return { weekLabels: weekStarts.map((d) => weekLabelFormat.format(d)), rows };
}
