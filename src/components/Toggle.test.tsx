import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "../test-utils/axe";
import { Toggle } from "./Toggle";

describe("Toggle", () => {
  it("renders an aria-pressed button, off by default", () => {
    render(<Toggle aria-label="Grid">Grid</Toggle>);
    expect(screen.getByRole("button", { name: "Grid" })).toHaveAttribute("aria-pressed", "false");
  });

  it("toggles aria-pressed on click", async () => {
    render(<Toggle aria-label="Grid">Grid</Toggle>);
    const toggle = screen.getByRole("button", { name: "Grid" });
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "true");
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "false");
  });

  it("respects defaultPressed", () => {
    render(
      <Toggle aria-label="Grid" defaultPressed>
        Grid
      </Toggle>
    );
    expect(screen.getByRole("button", { name: "Grid" })).toHaveAttribute("aria-pressed", "true");
  });

  it("calls onPressedChange with the new state", async () => {
    const onPressedChange = vi.fn();
    render(
      <Toggle aria-label="Grid" onPressedChange={onPressedChange}>
        Grid
      </Toggle>
    );
    await userEvent.click(screen.getByRole("button", { name: "Grid" }));
    expect(onPressedChange).toHaveBeenCalledWith(true);
  });

  it("supports being operated by keyboard", async () => {
    render(<Toggle aria-label="Grid">Grid</Toggle>);
    const toggle = screen.getByRole("button", { name: "Grid" });
    toggle.focus();
    await userEvent.keyboard("{Enter}");
    expect(toggle).toHaveAttribute("aria-pressed", "true");
  });

  it("does not toggle when disabled", async () => {
    render(
      <Toggle aria-label="Grid" disabled>
        Grid
      </Toggle>
    );
    const toggle = screen.getByRole("button", { name: "Grid" });
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "false");
  });

  it("applies the default (sm) size utility class", () => {
    // This only asserts the Tailwind class name is applied, not the
    // rendered box size — jsdom has no layout engine, so it can't measure
    // getBoundingClientRect(). The real touch-target-size assertion is a
    // Storybook play test (see Toggle.stories.tsx), which runs in a real
    // browser via test:storybook:ci.
    render(<Toggle aria-label="Grid">Grid</Toggle>);
    expect(screen.getByRole("button", { name: "Grid" })).toHaveClass("h-8");
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<Toggle aria-label="Grid">Grid</Toggle>);
    expect(await axe(container)).toHaveNoViolations();
  });
});
