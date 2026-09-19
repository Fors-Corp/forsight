import * as React from "react";
import { cn } from "../lib/cn";

export interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  /** Renders the field in its error state with a danger-colored border. */
  invalid?: boolean;
  /** Compact helper/error text shown below the field. */
  hint?: string;
}

/**
 * Single-line text field. Set `invalid` and pass an error message via
 * `hint` for validation states; `hint` alone (no `invalid`) renders as
 * neutral helper text. When `hint` becomes an error message after `invalid`
 * turns on (e.g. after a failed submit), the hint is announced to screen
 * readers via `aria-live="polite"` — it stays silent the rest of the time.
 */
export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, invalid, hint, id, ...props }, ref) => {
    const generatedId = React.useId();
    const inputId = id ?? generatedId;
    const hintId = React.useId();
    return (
      <div className="flex flex-col gap-1.5">
        <input
          ref={ref}
          id={inputId}
          className={cn(
            "h-10 w-full rounded-md border bg-ink-surface px-3 text-sm font-sans text-fg placeholder:text-fg-muted transition-colors duration-base",
            "focus-visible:outline-none focus-visible:shadow-focus-ring",
            invalid
              ? "border-danger focus-visible:border-danger"
              : "border-ink-border focus-visible:border-accent",
            "disabled:opacity-50 disabled:cursor-not-allowed",
            className
          )}
          aria-invalid={invalid || undefined}
          aria-describedby={hint ? hintId : undefined}
          {...props}
        />
        {hint && (
          <span
            id={hintId}
            // Live only while invalid: a hint that turns into an error after
            // submit (the common validation pattern) needs to be announced —
            // WCAG 4.1.3 — but an always-live hint would announce every
            // keystroke-driven hint change too (e.g. a character counter),
            // which is noise rather than a status change worth interrupting
            // for.
            aria-live={invalid ? "polite" : undefined}
            className={cn("text-xs font-sans", invalid ? "text-danger" : "text-fg-muted")}
          >
            {hint}
          </span>
        )}
      </div>
    );
  }
);
Input.displayName = "Input";
