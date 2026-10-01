import * as React from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { expect, screen, userEvent, waitFor, within } from "storybook/test";
import { Calendar, DatePicker, DateRangePicker, type DateRangePickerProps } from "./Calendar";

const meta: Meta = {
  title: "Forsight/Forms/Calendar",
  parameters: { layout: "fullscreen" },
};
export default meta;
type Story = StoryObj;

// A fixed reference month keeps stories/screenshots deterministic regardless
// of when Storybook is opened.
const REFERENCE_MONTH = new Date(2026, 8, 1);

export const Default: Story = {
  render: () => (
    <div className="flex justify-center pt-12">
      <Calendar
        mode="single"
        defaultMonth={REFERENCE_MONTH}
        selected={new Date(2026, 8, 8)}
        className="rounded-md border border-ink-border bg-ink-surface p-4"
      />
    </div>
  ),
};

export const DisabledDates: Story = {
  render: () => (
    <div className="flex justify-center pt-12">
      <Calendar
        mode="single"
        defaultMonth={REFERENCE_MONTH}
        disabled={{ dayOfWeek: [0, 6] }}
        className="rounded-md border border-ink-border bg-ink-surface p-4"
      />
    </div>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const saturday = canvas.getByRole("button", { name: /Saturday, September 12/i });
    await expect(saturday).toBeDisabled();
  },
};

/**
 * Real-browser interaction: arrow keys move focus across the date grid
 * (including wrapping to the next/previous week), Enter selects.
 */
export const KeyboardNavigation: Story = {
  render: () => (
    <div className="flex justify-center pt-12">
      <Calendar
        mode="single"
        defaultMonth={REFERENCE_MONTH}
        selected={new Date(2026, 8, 8)}
        className="rounded-md border border-ink-border bg-ink-surface p-4"
      />
    </div>
  ),
  play: async () => {
    const selectedDay = await screen.findByRole("button", { name: /September 8.*selected/i });
    selectedDay.focus();
    await userEvent.keyboard("{ArrowRight}");
    await expect(screen.getByRole("button", { name: /September 9/i })).toHaveFocus();
    await userEvent.keyboard("{ArrowDown}");
    await expect(screen.getByRole("button", { name: /September 16/i })).toHaveFocus();
  },
};

export const RangeSelection: Story = {
  render: () => (
    <div className="flex justify-center pt-12">
      <Calendar
        mode="range"
        defaultMonth={REFERENCE_MONTH}
        selected={{ from: new Date(2026, 8, 8), to: new Date(2026, 8, 11) }}
        className="rounded-md border border-ink-border bg-ink-surface p-4"
      />
    </div>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    // The two endpoints and the days between them are all flagged
    // "selected" by react-day-picker; `CalendarDayButton` gives the
    // in-between days a softer fill, but all four remain in the accessible
    // tree as selected.
    await expect(
      canvas.getByRole("button", { name: /September 8.*selected/i })
    ).toBeInTheDocument();
    await expect(
      canvas.getByRole("button", { name: /September 9.*selected/i })
    ).toBeInTheDocument();
    await expect(
      canvas.getByRole("button", { name: /September 11.*selected/i })
    ).toBeInTheDocument();
  },
};

function DatePickerExample() {
  const [value, setValue] = React.useState<Date | undefined>(new Date(2026, 8, 8));
  return (
    <div className="flex max-w-xs flex-col gap-2 pt-12">
      <DatePicker value={value} onValueChange={setValue} />
    </div>
  );
}

/**
 * Real-browser interaction: opens the popover, moves through the grid with
 * arrow keys, Enter selects and closes the popover, and the trigger's
 * accessible name updates to the new selection.
 */
export const DatePickerExampleStory: Story = {
  name: "DatePicker",
  render: () => <DatePickerExample />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    // The trigger's static "Selected date:"/"Change date." wording is this
    // component's own hardcoded copy (locale-independent), but the date
    // itself is formatted via `Intl.DateTimeFormat(undefined, ...)` — the
    // *viewer's* locale, unlike the Calendar grid's day labels below, which
    // react-day-picker always formats in English via its date-fns default.
    // Match only the stable prefix/digit, not a full English sentence.
    const trigger = canvas.getByRole("button", { name: /^Selected date:/i });
    await expect(trigger).toHaveAccessibleName(/8/);
    await userEvent.click(trigger);
    const grid = await screen.findByRole("grid");
    await expect(grid).toBeInTheDocument();
    const target = screen.getByRole("button", { name: /September 15/i });
    target.focus();
    await userEvent.keyboard("{Enter}");
    // Radix keeps the popover mounted through its CSS close animation
    // (Presence), so the grid disappears asynchronously, not the instant
    // `open` flips to false — same pattern as Popover's `OpensOnClick` story.
    // `waitFor` (not `waitForElementToBeRemoved`) because under the test
    // runner's reduced-motion emulation the unmount can already have
    // happened by the time this line runs, and the latter throws on that.
    await waitFor(() => expect(screen.queryByRole("grid")).not.toBeInTheDocument());
    await expect(canvas.getByRole("button", { name: /^Selected date:/i })).toHaveAccessibleName(
      /15/
    );
  },
};

function DateRangePickerExample() {
  const [value, setValue] = React.useState<DateRangePickerProps["value"]>();
  return (
    <div className="flex max-w-xs flex-col gap-2 pt-12">
      <DateRangePicker value={value} onValueChange={setValue} defaultMonth={REFERENCE_MONTH} />
    </div>
  );
}

/**
 * Real-browser interaction: picking the first day of a range keeps the
 * popover open (only `from` is known yet) and updates the trigger to show
 * the "… " partial state; picking the second day closes the popover and the
 * trigger shows the complete range.
 */
export const DateRangePickerExampleStory: Story = {
  name: "DateRangePicker",
  render: () => <DateRangePickerExample />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const trigger = canvas.getByRole("button", { name: "Pick a date range" });
    await userEvent.click(trigger);
    const grid = await screen.findByRole("grid");
    await expect(grid).toBeInTheDocument();

    const start = screen.getByRole("button", { name: /September 8/i });
    start.focus();
    await userEvent.keyboard("{Enter}");
    // Only the range's start is known — the popover must stay open so the
    // end can be picked next.
    await expect(screen.getByRole("grid")).toBeInTheDocument();
    await expect(canvas.getByRole("button", { name: /^Selected range:/i })).toHaveAccessibleName(
      /8.*–.*…/
    );

    const end = screen.getByRole("button", { name: /September 15/i });
    end.focus();
    await userEvent.keyboard("{Enter}");
    await waitFor(() => expect(screen.queryByRole("grid")).not.toBeInTheDocument());
    await expect(canvas.getByRole("button", { name: /^Selected range:/i })).toHaveAccessibleName(
      /8.*–.*15/
    );
  },
};
