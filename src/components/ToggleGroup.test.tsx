import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "../test-utils/axe";
import { ToggleGroup, ToggleGroupItem } from "./ToggleGroup";

describe("ToggleGroup", () => {
  it("type='single' only allows one item pressed, exposed as radiogroup/radio", async () => {
    render(
      <ToggleGroup type="single" defaultValue="table" aria-label="View mode">
        <ToggleGroupItem value="table">Table</ToggleGroupItem>
        <ToggleGroupItem value="chart">Chart</ToggleGroupItem>
      </ToggleGroup>
    );
    expect(screen.getByRole("radiogroup", { name: "View mode" })).toBeInTheDocument();
    const table = screen.getByRole("radio", { name: "Table" });
    const chart = screen.getByRole("radio", { name: "Chart" });
    expect(table).toHaveAttribute("aria-checked", "true");

    await userEvent.click(chart);
    expect(chart).toHaveAttribute("aria-checked", "true");
    expect(table).toHaveAttribute("aria-checked", "false");
  });

  it("type='multiple' allows independent pressed state, exposed as toolbar/aria-pressed", async () => {
    render(
      <ToggleGroup type="multiple" defaultValue={["errors"]} aria-label="Log levels shown">
        <ToggleGroupItem value="info">Info</ToggleGroupItem>
        <ToggleGroupItem value="errors">Errors</ToggleGroupItem>
      </ToggleGroup>
    );
    expect(screen.getByRole("toolbar", { name: "Log levels shown" })).toBeInTheDocument();
    const info = screen.getByRole("button", { name: "Info" });
    const errors = screen.getByRole("button", { name: "Errors" });
    expect(errors).toHaveAttribute("aria-pressed", "true");
    expect(info).toHaveAttribute("aria-pressed", "false");

    await userEvent.click(info);
    expect(info).toHaveAttribute("aria-pressed", "true");
    expect(errors).toHaveAttribute("aria-pressed", "true");
  });

  it("calls onValueChange when the selection changes", async () => {
    const onValueChange = vi.fn();
    render(
      <ToggleGroup
        type="single"
        defaultValue="table"
        onValueChange={onValueChange}
        aria-label="View mode"
      >
        <ToggleGroupItem value="table">Table</ToggleGroupItem>
        <ToggleGroupItem value="chart">Chart</ToggleGroupItem>
      </ToggleGroup>
    );
    await userEvent.click(screen.getByRole("radio", { name: "Chart" }));
    expect(onValueChange).toHaveBeenCalledWith("chart");
  });

  it("moves focus with arrow keys (roving tabindex), then selects on Space/Enter", async () => {
    render(
      <ToggleGroup type="single" defaultValue="table" aria-label="View mode">
        <ToggleGroupItem value="table">Table</ToggleGroupItem>
        <ToggleGroupItem value="chart">Chart</ToggleGroupItem>
      </ToggleGroup>
    );
    const table = screen.getByRole("radio", { name: "Table" });
    const chart = screen.getByRole("radio", { name: "Chart" });
    table.focus();
    await userEvent.keyboard("{ArrowRight}");
    // Arrow keys only move the roving focus — selection is unchanged until
    // an explicit activation key, matching Radix's toggle-group primitive
    // (unlike RadioGroup, which selects as focus moves).
    expect(chart).toHaveFocus();
    expect(table).toHaveAttribute("aria-checked", "true");

    await userEvent.keyboard("{Enter}");
    expect(chart).toHaveAttribute("aria-checked", "true");
    expect(table).toHaveAttribute("aria-checked", "false");
  });

  it("applies the default (sm) size utility class", () => {
    // Class-name assertion only — jsdom has no layout engine. The real
    // touch-target-size assertion is a Storybook play test (see
    // ToggleGroup.stories.tsx), which runs in a real browser via
    // test:storybook:ci.
    render(
      <ToggleGroup type="single" defaultValue="table" aria-label="View mode">
        <ToggleGroupItem value="table">Table</ToggleGroupItem>
      </ToggleGroup>
    );
    expect(screen.getByRole("radio", { name: "Table" })).toHaveClass("h-8");
  });

  it("has no accessibility violations (single-select)", async () => {
    const { container } = render(
      <ToggleGroup type="single" defaultValue="table" aria-label="View mode">
        <ToggleGroupItem value="table">Table</ToggleGroupItem>
        <ToggleGroupItem value="chart">Chart</ToggleGroupItem>
      </ToggleGroup>
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("has no accessibility violations (multi-select)", async () => {
    const { container } = render(
      <ToggleGroup type="multiple" defaultValue={["errors"]} aria-label="Log levels shown">
        <ToggleGroupItem value="info">Info</ToggleGroupItem>
        <ToggleGroupItem value="errors">Errors</ToggleGroupItem>
      </ToggleGroup>
    );
    expect(await axe(container)).toHaveNoViolations();
  });
});
