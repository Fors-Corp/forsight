import * as React from "react";
import { cn } from "../lib/cn";

export type LogLevel = "debug" | "info" | "warn" | "error" | "fatal";

export interface LogEntry {
  id: string;
  /** Pre-formatted timestamp — the caller owns the timezone and precision. */
  timestamp: string;
  level: LogLevel;
  message: string;
  /** Emitting service, pod or logger name. */
  source?: string;
}

export interface LogStreamProps extends Omit<React.HTMLAttributes<HTMLDivElement>, "children"> {
  /** What is being streamed, e.g. "checkout-api production logs". */
  label: string;
  entries: LogEntry[];
  /** Scroll ceiling in CSS pixels. */
  maxHeight?: number;
  /**
   * Announce new lines as they arrive. Off by default: a busy stream read aloud
   * continuously is unusable — turn it on only for a low-volume, high-value feed
   * such as a deploy log the user is waiting on.
   */
  announce?: boolean;
}

const LEVEL_TONES: Record<LogLevel, string> = {
  debug: "bg-ink-surface-2 text-fg-muted",
  info: "bg-ink-surface-2 text-fg-secondary",
  warn: "bg-warning-subtle text-warning",
  error: "bg-danger-subtle text-danger",
  fatal: "bg-danger text-danger-fg",
};

const LEVEL_ORDER: LogLevel[] = ["debug", "info", "warn", "error", "fatal"];

/**
 * Past this many entries, rows are windowed to a scroll-position slice
 * instead of every one mounting at once — see the component doc comment.
 * At or under it, every row renders exactly as before.
 */
const WINDOW_THRESHOLD = 200;
/**
 * Estimated pixel height of one row — this component's rows are a single
 * text-xs line plus `py-2` padding, so this is a fixed guess, not a
 * measurement. It only has to decide which rows are near the scrolled
 * viewport; a message that wraps to two or three lines just means the
 * window's estimate undershoots slightly for that one row, which `OVERSCAN`
 * exists to absorb.
 */
const ROW_HEIGHT_ESTIMATE = 32;
/**
 * Extra rows rendered on each side of the estimated visible slice, so a
 * taller-than-estimated (wrapped) row, or scrolling faster than a render
 * can follow, doesn't uncover blank space before the next scroll event
 * recomputes the window.
 */
const OVERSCAN = 15;

/**
 * Scrollable log viewer with a level chip per line — the "what just happened"
 * pane under a chart, or the body of a log-search result.
 *
 * The level is printed as a word, never as color alone, and lines wrap rather
 * than truncate so a long message is readable without a horizontal scrub. The
 * region is exposed as an ARIA `log`; whether it *announces* is opt-in via
 * `announce`, because a live region attached to a busy stream drowns out
 * everything else on the page.
 *
 * Past `entries.length === 200`, rows are windowed: only the slice near the
 * current scroll position (plus a small overscan buffer) is actually
 * mounted, with a pair of empty spacer rows standing in for the rest so the
 * box's scrollbar still represents the true total. This is a fixed-estimate
 * scroll window, not a full virtualizer — it trades pixel-perfect spacer
 * sizing (a wrapped multi-line message is under-estimated) for no added
 * dependency, which is enough to keep a 2000-row feed reconciling every few
 * seconds from mounting 2000 DOM nodes on every tick. Screen-reader users
 * don't lose the true count: each mounted row carries `aria-setsize`/
 * `aria-posinset` (the ARIA-specified technique for a list where "not all
 * items in the set are present in the DOM"), and the region's own keyboard
 * and `aria-live`/`aria-relevant` semantics are unchanged either way — a
 * focused, scrollable region is natively reachable by keyboard regardless of
 * how much of it is mounted. A caller relying on `announce` for entries
 * appended below the current window won't hear them until they scroll near
 * that point, the same as a message that would have been below the fold in
 * an unwindowed box.
 */
export const LogStream = React.forwardRef<HTMLDivElement, LogStreamProps>(
  ({ className, label, entries, maxHeight = 320, announce = false, ...props }, ref) => {
    const windowed = entries.length > WINDOW_THRESHOLD;
    const [scrollTop, setScrollTop] = React.useState(0);

    const handleScroll = React.useCallback((event: React.UIEvent<HTMLDivElement>) => {
      setScrollTop(event.currentTarget.scrollTop);
    }, []);

    const { visibleEntries, startIndex, topSpacerHeight, bottomSpacerHeight } =
      React.useMemo(() => {
        if (!windowed) {
          return {
            visibleEntries: entries,
            startIndex: 0,
            topSpacerHeight: 0,
            bottomSpacerHeight: 0,
          };
        }
        const visibleRows = Math.ceil(maxHeight / ROW_HEIGHT_ESTIMATE);
        const centerIndex = Math.floor(scrollTop / ROW_HEIGHT_ESTIMATE);
        const start = Math.max(0, centerIndex - OVERSCAN);
        const end = Math.min(entries.length, centerIndex + visibleRows + OVERSCAN);
        return {
          visibleEntries: entries.slice(start, end),
          startIndex: start,
          topSpacerHeight: start * ROW_HEIGHT_ESTIMATE,
          bottomSpacerHeight: (entries.length - end) * ROW_HEIGHT_ESTIMATE,
        };
      }, [entries, windowed, scrollTop, maxHeight]);

    return (
      <div
        ref={ref}
        role="log"
        aria-label={label}
        // A scrollable box that can't be reached by keyboard traps its content
        // for anyone not using a mouse (WCAG 2.1.1, axe's
        // scrollable-region-focusable): the region itself is the scroll control.
        tabIndex={0}
        aria-live={announce ? "polite" : "off"}
        aria-relevant="additions"
        onScroll={windowed ? handleScroll : undefined}
        style={{ maxHeight }}
        className={cn(
          "w-full min-w-0 overflow-y-auto rounded-md border border-ink-border bg-ink-bg font-mono text-xs focus-visible:outline-none focus-visible:shadow-focus-ring",
          className
        )}
        {...props}
      >
        {entries.length === 0 ? (
          <p className="p-3 text-fg-muted">No log lines in this window.</p>
        ) : (
          <ol className="divide-y divide-ink-border-subtle">
            {topSpacerHeight > 0 ? (
              <li aria-hidden="true" style={{ height: topSpacerHeight }} />
            ) : null}
            {visibleEntries.map((entry, i) => (
              <li
                key={entry.id}
                aria-setsize={windowed ? entries.length : undefined}
                aria-posinset={windowed ? startIndex + i + 1 : undefined}
                className="flex flex-wrap items-start gap-x-3 gap-y-1 px-3 py-2 hover:bg-ink-surface"
              >
                <time className="shrink-0 text-fg-muted">{entry.timestamp}</time>
                <span
                  className={cn(
                    "shrink-0 rounded-sm px-1.5 py-0.5 text-[0.6875rem] font-medium uppercase",
                    LEVEL_TONES[entry.level]
                  )}
                >
                  {entry.level}
                </span>
                {entry.source ? (
                  <span className="shrink-0 text-fg-secondary">{entry.source}</span>
                ) : null}
                <span className="min-w-0 flex-1 break-words text-fg">{entry.message}</span>
              </li>
            ))}
            {bottomSpacerHeight > 0 ? (
              <li aria-hidden="true" style={{ height: bottomSpacerHeight }} />
            ) : null}
          </ol>
        )}
      </div>
    );
  }
);
LogStream.displayName = "LogStream";

/** Levels in severity order — for building a filter control over a stream. */
export const LOG_LEVELS = LEVEL_ORDER;
