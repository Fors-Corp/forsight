import * as React from "react";
import { cn } from "../lib/cn";
import { formatCompact, seriesBg } from "../lib/chart";
import { isSafeHref } from "../lib/safe-href";

export interface BarListItem {
  /** Row name — an endpoint, service, region, error code. */
  label: string;
  value: number;
  /**
   * Makes the row a link to the drill-down for that dimension. Since this is
   * typically built from route/service/tag/error-code data that traces back
   * to request input, it is treated as untrusted: only relative URLs and
   * absolute `http:`/`https:` URLs are linked (`isSafeHref`, `../lib/safe-href`)
   * — anything else (a `javascript:` or `data:` URI, say) falls back to the
   * same non-link row rendered when `href` is omitted.
   */
  href?: string;
  /** Categorical slot for the bar. Omit to draw every row in the accent color. */
  seriesIndex?: number;
}

export interface BarListProps extends Omit<React.HTMLAttributes<HTMLElement>, "children"> {
  items: BarListItem[];
  /** Formats the value shown at the end of each row. Defaults to compact notation. */
  valueFormat?: (value: number) => string;
  /** Bar scale ceiling. Defaults to the largest value, so the top row is always full width. */
  max?: number;
}

/**
 * Ranked "top N" list — slowest endpoints, noisiest alerts, biggest spenders.
 * The bar is a horizontal magnitude read behind the label, so long names stay
 * readable at any width where a rotated-label bar chart would not.
 *
 * Every row prints its value as text next to the bar, so the ranking never
 * depends on comparing bar lengths (or colors) by eye. Pass `href` to make rows
 * drill down; without it they are static text, not fake buttons.
 */
export const BarList = React.forwardRef<HTMLOListElement, BarListProps>(
  ({ className, items, valueFormat = formatCompact, max, ...props }, ref) => {
    const ceiling = max ?? Math.max(...items.map((item) => item.value), 0);

    return (
      <ol ref={ref} className={cn("flex w-full min-w-0 flex-col gap-1", className)} {...props}>
        {items.map((item) => {
          const share = ceiling > 0 ? Math.max(0, (item.value / ceiling) * 100) : 0;
          const content = (
            <>
              <span
                aria-hidden="true"
                className={cn(
                  "absolute inset-y-0 start-0 rounded-sm opacity-25",
                  item.seriesIndex === undefined ? "bg-accent" : seriesBg(item.seriesIndex)
                )}
                style={{ width: `${share}%` }}
              />
              <span className="relative min-w-0 flex-1 truncate text-fg">{item.label}</span>
              <span className="relative shrink-0 font-mono text-fg-secondary">
                {valueFormat(item.value)}
              </span>
            </>
          );

          return (
            <li key={item.label} className="min-w-0">
              {item.href && isSafeHref(item.href) ? (
                <a
                  href={item.href}
                  className="relative flex min-h-9 w-full items-center gap-3 overflow-hidden rounded-sm px-2 text-sm font-sans transition-colors duration-fast hover:bg-ink-surface-2 focus-visible:outline-none focus-visible:shadow-focus-ring"
                >
                  {content}
                </a>
              ) : (
                <div className="relative flex min-h-9 w-full items-center gap-3 overflow-hidden rounded-sm px-2 text-sm font-sans">
                  {content}
                </div>
              )}
            </li>
          );
        })}
      </ol>
    );
  }
);
BarList.displayName = "BarList";
