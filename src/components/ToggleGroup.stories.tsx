import type { Meta, StoryObj } from "@storybook/react";
import { expect, userEvent, within } from "storybook/test";
import { ToggleGroup, ToggleGroupItem } from "./ToggleGroup";

const meta: Meta<typeof ToggleGroup> = {
  title: "Forsight/Forms/ToggleGroup",
  component: ToggleGroup,
};
export default meta;
type Story = StoryObj<typeof ToggleGroup>;

export const SingleSelect: Story = {
  render: () => (
    <ToggleGroup type="single" defaultValue="chart" aria-label="View mode">
      <ToggleGroupItem value="table">Table</ToggleGroupItem>
      <ToggleGroupItem value="chart">Chart</ToggleGroupItem>
      <ToggleGroupItem value="raw">Raw</ToggleGroupItem>
    </ToggleGroup>
  ),
};

export const MultiSelect: Story = {
  render: () => (
    <ToggleGroup type="multiple" defaultValue={["errors"]} aria-label="Log levels shown">
      <ToggleGroupItem value="info">Info</ToggleGroupItem>
      <ToggleGroupItem value="warn">Warn</ToggleGroupItem>
      <ToggleGroupItem value="errors">Errors</ToggleGroupItem>
    </ToggleGroup>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="flex flex-col gap-4">
      <ToggleGroup type="single" defaultValue="1h" aria-label="Range (small)">
        <ToggleGroupItem size="sm" value="1h">
          1h
        </ToggleGroupItem>
        <ToggleGroupItem size="sm" value="24h">
          24h
        </ToggleGroupItem>
      </ToggleGroup>
      <ToggleGroup type="single" defaultValue="1h" aria-label="Range (medium)">
        <ToggleGroupItem size="md" value="1h">
          1h
        </ToggleGroupItem>
        <ToggleGroupItem size="md" value="24h">
          24h
        </ToggleGroupItem>
      </ToggleGroup>
    </div>
  ),
};

/**
 * Real-browser interaction: single-select group is a radiogroup with a
 * roving tabindex — Arrow keys move focus between items, and Space/Enter
 * (or a click) selects the focused one. Unlike `RadioGroup`, arrow keys
 * alone don't change the selection.
 */
export const KeyboardNavigation: Story = {
  render: () => (
    <ToggleGroup type="single" defaultValue="table" aria-label="View mode">
      <ToggleGroupItem value="table">Table</ToggleGroupItem>
      <ToggleGroupItem value="chart">Chart</ToggleGroupItem>
    </ToggleGroup>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const table = canvas.getByRole("radio", { name: "Table" });
    const chart = canvas.getByRole("radio", { name: "Chart" });

    expect(table).toHaveAttribute("aria-checked", "true");
    table.focus();
    await userEvent.keyboard("{ArrowRight}");
    expect(chart).toHaveFocus();
    expect(table).toHaveAttribute("aria-checked", "true");

    await userEvent.keyboard("{Enter}");
    expect(chart).toHaveAttribute("aria-checked", "true");
    expect(table).toHaveAttribute("aria-checked", "false");
  },
};

export const MinimumTouchTarget: Story = {
  render: () => (
    <ToggleGroup type="single" defaultValue="table" aria-label="View mode">
      <ToggleGroupItem value="table">Table</ToggleGroupItem>
    </ToggleGroup>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const item = canvas.getByRole("radio", { name: "Table" });
    const rect = item.getBoundingClientRect();
    // WCAG 2.5.8: minimum 24x24 CSS px touch target at the default (sm) size.
    await expect(rect.width).toBeGreaterThanOrEqual(24);
    await expect(rect.height).toBeGreaterThanOrEqual(24);
  },
};
