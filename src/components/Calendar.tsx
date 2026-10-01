import * as React from "react";
import {
  DayPicker,
  UI,
  DayFlag,
  SelectionState,
  type DateRange,
  type DayButtonProps,
  type DayPickerProps,
} from "react-day-picker";
import { cn } from "../lib/cn";
import { Popover, PopoverTrigger, PopoverContent } from "./Popover";

export type CalendarProps = DayPickerProps;

const navButtonClass = cn(
  "inline-flex h-8 w-8 items-center justify-center rounded-md text-fg-muted transition-colors duration-base",
  "hover:bg-ink-surface-2 hover:text-fg",
  "focus-visible:outline-none focus-visible:shadow-focus-ring",
  "disabled:pointer-events-none disabled:opacity-40"
);

const defaultClassNames: Partial<Record<UI | DayFlag | SelectionState, string>> = {
  // `UI.Nav`'s `absolute inset-x-0` positions relative to the nearest
  // positioned ancestor, which by default (no `navLayout` prop) is `Months`
  // — `Nav` renders as `Months`'s direct sibling to `Month`, not nested
  // inside it. `relative` has to live here, not on `Month`/`MonthCaption`,
  // or the buttons escape to the viewport edges instead of the card.
  [UI.Months]: "relative flex flex-col gap-4 sm:flex-row",
  [UI.Month]: "flex flex-col gap-3",
  [UI.MonthCaption]: "flex h-9 items-center justify-center",
  [UI.CaptionLabel]: "font-sans text-sm font-medium text-fg",
  [UI.Nav]: "absolute inset-x-0 flex items-center justify-between",
  [UI.PreviousMonthButton]: navButtonClass,
  [UI.NextMonthButton]: navButtonClass,
  [UI.Chevron]: "h-4 w-4 fill-current",
  [UI.MonthGrid]: "w-full border-collapse",
  [UI.Weekdays]: "flex",
  [UI.Weekday]: "w-9 text-center text-xs font-medium text-fg-muted",
  [UI.Weeks]: "flex flex-col gap-1",
  [UI.Week]: "flex gap-1",
  [UI.Day]: "p-0 text-center",
};

/**
 * `DayButton` swapped in wholesale (per react-day-picker's own recommended
 * customization surface) rather than styled via `classNames[UI.DayButton]` —
 * v10 puts `data-selected`/`data-today`/etc. on the non-interactive day cell
 * (the `td`), not on the button itself, so the button's own selected/today/
 * disabled look has to be computed from the `modifiers` prop react-day-picker
 * passes to this component. The focus effect mirrors the library's own
 * default `DayButton` — react-day-picker moves keyboard-grid focus by
 * flagging `modifiers.focused`, and skipping this would silently break
 * arrow-key navigation.
 */
function CalendarDayButton({ className, day: _day, modifiers, ...props }: DayButtonProps) {
  const ref = React.useRef<HTMLButtonElement>(null);
  React.useEffect(() => {
    if (modifiers.focused) ref.current?.focus();
  }, [modifiers.focused]);
  return (
    <button
      ref={ref}
      type="button"
      className={cn(
        "inline-flex h-9 w-9 items-center justify-center rounded-md text-sm font-sans text-fg outline-none transition-colors duration-base",
        "hover:bg-ink-surface-2",
        "focus-visible:outline-none focus-visible:shadow-focus-ring",
        modifiers.outside && "text-fg-muted opacity-50",
        modifiers.today &&
          !modifiers.selected &&
          !modifiers.range_middle &&
          "font-semibold text-accent",
        // range_middle's softer fill must win over the plain `selected`
        // check below — react-day-picker's range mode sets `selected` true
        // for every day in the range, not just the two endpoints.
        modifiers.range_middle && "bg-accent-subtle text-accent",
        (modifiers.selected || modifiers.range_start || modifiers.range_end) &&
          !modifiers.range_middle &&
          "bg-accent text-accent-fg hover:bg-accent-hover",
        modifiers.disabled && "pointer-events-none text-fg-muted opacity-30",
        className
      )}
      {...props}
    />
  );
}

/**
 * Date grid built on `react-day-picker` — reach for `DatePicker`/
 * `DateRangePicker` instead when you need a compact form field rather than
 * an always-visible grid. Supports `mode="single"`/`"multiple"`/`"range"`;
 * in range mode, the days between the two picked ends get a softer fill
 * (`range_middle`) distinct from the two endpoints. react-day-picker
 * implements the full WAI-ARIA date-grid keyboard model
 * (arrow keys, Home/End/PageUp/PageDown) and month-nav accessible names
 * itself — this wrapper only restyles it with this repo's tokens. No `ref`
 * prop: `DayPicker` is a plain function component with no ref-forwarding
 * support of its own (no `forwardRef`, no `rootRef` prop), so there's
 * nothing real to forward here.
 */
export function Calendar({ classNames, components, ...props }: CalendarProps) {
  return (
    <DayPicker
      classNames={{ ...defaultClassNames, ...classNames }}
      components={{ DayButton: CalendarDayButton, ...components }}
      {...props}
    />
  );
}

export interface DatePickerProps {
  /** The selected date, or `undefined` for none. */
  value?: Date;
  /** Fires when a date is picked; the popover closes immediately after. */
  onValueChange?: (date: Date | undefined) => void;
  /** Shown (and used as the field's accessible name) when nothing is selected. */
  placeholder?: string;
  /** Matches `Calendar`'s `disabled` matcher — dates it accepts can't be picked. */
  disabled?: DayPickerProps["disabled"];
  className?: string;
  /** Forwarded to the trigger button, e.g. to pair it with a `<label htmlFor>`. */
  id?: string;
  /**
   * Month the grid opens on when nothing is selected. With a `value` the grid
   * opens on that date's month, so this is only needed for the empty state
   * (and to keep stories/screenshots independent of the current date).
   */
  defaultMonth?: Date;
}

const CALENDAR_ICON = (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    aria-hidden="true"
    className="shrink-0"
  >
    <rect x="2" y="3" width="12" height="11" rx="1.5" stroke="currentColor" strokeWidth="1.3" />
    <path
      d="M2 6.5h12M5 1.5v3M11 1.5v3"
      stroke="currentColor"
      strokeWidth="1.3"
      strokeLinecap="round"
    />
  </svg>
);

/**
 * Compact form field pairing a text trigger with a `Calendar` in a
 * `Popover` — single-date selection only (see `Calendar`'s own scope note).
 * The trigger's accessible name always reflects the current value instead of
 * a generic "Open calendar", per this repo's `Popover` naming requirement.
 */
export function DatePicker({
  value,
  onValueChange,
  placeholder = "Pick a date",
  disabled,
  className,
  id,
  defaultMonth,
}: DatePickerProps) {
  const [open, setOpen] = React.useState(false);
  const label = value
    ? new Intl.DateTimeFormat(undefined, { dateStyle: "long" }).format(value)
    : placeholder;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          id={id}
          type="button"
          aria-label={value ? `Selected date: ${label}. Change date.` : placeholder}
          className={cn(
            "flex h-10 w-full items-center gap-2 rounded-md border border-ink-border bg-ink-surface px-3 text-sm font-sans transition-colors duration-base",
            "focus-visible:outline-none focus-visible:shadow-focus-ring",
            value ? "text-fg" : "text-fg-muted",
            className
          )}
        >
          {CALENDAR_ICON}
          {label}
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" aria-label="Choose a date" className="w-auto p-2">
        <Calendar
          mode="single"
          selected={value}
          // Without this the grid opens on the current month even when a date
          // from another month is selected, so the selection isn't on screen.
          defaultMonth={value ?? defaultMonth}
          onSelect={(date) => {
            onValueChange?.(date);
            setOpen(false);
          }}
          disabled={disabled}
          autoFocus
        />
      </PopoverContent>
    </Popover>
  );
}

export interface DateRangePickerProps {
  /** The selected range. `to` may be `undefined` while only the start has been picked. */
  value?: DateRange;
  /** Fires as each end is picked — once with only `from` set, again with both set. The popover stays open until both ends are picked. */
  onValueChange?: (range: DateRange | undefined) => void;
  /** Shown (and used as the field's accessible name) when nothing is selected. */
  placeholder?: string;
  /** Matches `Calendar`'s `disabled` matcher. */
  disabled?: DayPickerProps["disabled"];
  className?: string;
  /** Forwarded to the trigger button, e.g. to pair it with a `<label htmlFor>`. */
  id?: string;
  /**
   * Month the grid opens on when no range has been started. With a `value` the
   * grid opens on `value.from`'s month, so this is only needed for the empty
   * state (and to keep stories/screenshots independent of the current date).
   */
  defaultMonth?: Date;
}

const rangeDateFormat = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });

/**
 * `DatePicker`'s two-ended sibling — a compact form field for a custom
 * start/end window, complementing `TimeRange`'s fixed presets ("1h",
 * "24h") for whenever a reader needs an arbitrary range instead. The
 * popover stays open after the first click (only the range's start is
 * known yet) and closes once both ends are picked; picking a new start
 * after a complete range restarts it. The underlying `Calendar` is given
 * `min={1}` so the two ends must be distinct days — without it,
 * react-day-picker's own range algorithm treats a single click as a
 * complete one-day range and the popover would close before a second day
 * could ever be picked. One consequence: clicking the start day again
 * before picking an end clears the selection rather than producing a
 * one-day range — use `DatePicker` for a single specific day.
 */
export function DateRangePicker({
  value,
  onValueChange,
  placeholder = "Pick a date range",
  disabled,
  className,
  id,
  defaultMonth,
}: DateRangePickerProps) {
  const [open, setOpen] = React.useState(false);
  const label =
    value?.from && value?.to
      ? `${rangeDateFormat.format(value.from)} – ${rangeDateFormat.format(value.to)}`
      : value?.from
        ? `${rangeDateFormat.format(value.from)} – …`
        : placeholder;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          id={id}
          type="button"
          aria-label={value?.from ? `Selected range: ${label}. Change range.` : placeholder}
          className={cn(
            "flex h-10 w-full items-center gap-2 rounded-md border border-ink-border bg-ink-surface px-3 text-sm font-sans transition-colors duration-base",
            "focus-visible:outline-none focus-visible:shadow-focus-ring",
            value?.from ? "text-fg" : "text-fg-muted",
            className
          )}
        >
          {CALENDAR_ICON}
          {label}
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" aria-label="Choose a date range" className="w-auto p-2">
        <Calendar
          mode="range"
          min={1}
          selected={value}
          // As in DatePicker: open on the range's start, not on today, so an
          // existing selection is visible when the popover opens.
          defaultMonth={value?.from ?? defaultMonth}
          onSelect={(range) => {
            onValueChange?.(range);
            if (range?.from && range?.to) setOpen(false);
          }}
          disabled={disabled}
          autoFocus
        />
      </PopoverContent>
    </Popover>
  );
}
