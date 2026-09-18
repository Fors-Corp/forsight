import * as React from "react";
import { cn } from "../lib/cn";
import { formatPercent } from "../lib/chart";

export type DeltaDirection = "up" | "down" | "flat";

export interface DeltaProps extends React.HTMLAttributes<HTMLSpanElement> {
  /** Signed change. `-8.2` renders as a decrease of 8.2%. */
  value: number;
  /**
   * Which direction is the good one for this metric — throughput is `up`,
   * latency and error rate are `down`, a queue depth you just want stable is
   * `none`. This is what decides the color; the sign alone can't.
   */
  goodDirection?: "up" | "down" | "none";
  /** Formats the magnitude. Defaults to a one-decimal percentage. */
  format?: (value: number) => string;
}

/**
 * Period-over-period change chip for a metric — the "+12.4% vs. last week" that
 * sits under a `StatCard` value.
 *
 * Direction is carried by an arrow *and* a word ("increase"/"decrease", read by
 * screen readers), never by the color alone, and the color means "good" or
 * "bad" rather than "up" or "down" — a 30% jump in p99 latency is red even
 * though it points up.
 */
export const Delta = React.forwardRef<HTMLSpanElement, DeltaProps>(
  ({ className, value, goodDirection = "up", format = defaultFormat, ...props }, ref) => {
    const direction: DeltaDirection = value > 0 ? "up" : value < 0 ? "down" : "flat";
    const tone =
      direction === "flat" || goodDirection === "none"
        ? "text-fg-secondary"
        : direction === goodDirection
          ? "text-success"
          : "text-danger";

    return (
      <span
        ref={ref}
        className={cn(
          "inline-flex items-center gap-1 text-sm font-medium font-sans tabular-nums",
          tone,
          className
        )}
        {...props}
      >
        <svg aria-hidden="true" viewBox="0 0 12 12" className="h-3 w-3 fill-current">
          {direction === "flat" ? (
            <rect x="1" y="5" width="10" height="2" rx="1" />
          ) : (
            <path
              d={direction === "up" ? "M6 1.5 11 8.5H1z" : "M6 10.5 1 3.5h10z"}
              // The triangle is the redundant encoding for the color; it is
              // never the only one, since the value's sign is printed too.
            />
          )}
        </svg>
        <span>{`${value > 0 ? "+" : value < 0 ? "−" : ""}${format(Math.abs(value))}`}</span>
        <span className="sr-only">
          {direction === "up" ? "increase" : direction === "down" ? "decrease" : "no change"}
        </span>
      </span>
    );
  }
);
Delta.displayName = "Delta";

// Delegates rather than repeating the percent format, so a Delta reads the
// same as every other percentage in the library and picks up the locale's
// decimal separator with it. This used to be its own template literal, which
// always wrote "5.5%" regardless of who was reading.
function defaultFormat(value: number): string {
  return formatPercent(value);
}
