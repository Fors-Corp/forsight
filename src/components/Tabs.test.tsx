import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { KeyboardEvent } from "react";
import { axe } from "../test-utils/axe";
import { Tabs } from "./Tabs";

function ExampleTabs() {
  return (
    <Tabs.Root defaultValue="overview">
      <Tabs.List>
        <Tabs.Trigger value="overview">Overview</Tabs.Trigger>
        <Tabs.Trigger value="settings">Settings</Tabs.Trigger>
        <Tabs.Trigger value="danger" disabled>
          Danger
        </Tabs.Trigger>
      </Tabs.List>
      <Tabs.Panel value="overview">Overview content</Tabs.Panel>
      <Tabs.Panel value="settings">Settings content</Tabs.Panel>
      <Tabs.Panel value="danger">Danger content</Tabs.Panel>
    </Tabs.Root>
  );
}

describe("Tabs", () => {
  it("shows only the active panel", () => {
    render(<ExampleTabs />);
    expect(screen.getByText("Overview content")).toBeInTheDocument();
    expect(screen.queryByText("Settings content")).not.toBeInTheDocument();
  });

  it("switches panels when a trigger is clicked", async () => {
    render(<ExampleTabs />);
    await userEvent.click(screen.getByRole("tab", { name: "Settings" }));
    expect(screen.getByText("Settings content")).toBeInTheDocument();
    expect(screen.queryByText("Overview content")).not.toBeInTheDocument();
  });

  it("links each panel to its trigger and exposes selection state", () => {
    render(<ExampleTabs />);
    const overviewTab = screen.getByRole("tab", { name: "Overview" });
    const panel = screen.getByRole("tabpanel");
    expect(overviewTab).toHaveAttribute("aria-selected", "true");
    expect(panel).toHaveAttribute("aria-labelledby", overviewTab.id);
    expect(overviewTab).toHaveAttribute("aria-controls", panel.id);
  });

  it("uses a roving tabindex — only the active trigger is in the tab order", () => {
    render(<ExampleTabs />);
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute("tabindex", "0");
    expect(screen.getByRole("tab", { name: "Settings" })).toHaveAttribute("tabindex", "-1");
  });

  it("moves and activates with Arrow keys, wrapping and skipping disabled triggers", async () => {
    render(<ExampleTabs />);
    const user = userEvent.setup();
    await user.tab(); // focus the active (Overview) trigger
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();

    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Settings" })).toHaveFocus();
    expect(screen.getByText("Settings content")).toBeInTheDocument();

    // ArrowRight again skips the disabled "Danger" tab and wraps to "Overview"
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();

    await user.keyboard("{End}");
    expect(screen.getByRole("tab", { name: "Settings" })).toHaveFocus();
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<ExampleTabs />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("moves and activates backward with ArrowLeft, wrapping past the first trigger", async () => {
    render(<ExampleTabs />);
    const user = userEvent.setup();
    await user.tab(); // focus the active (Overview) trigger
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();

    // From the first trigger, ArrowLeft wraps backward to the last
    // *enabled* one — skipping the disabled "Danger" trigger — and
    // activates it, the same as ArrowRight does going forward.
    await user.keyboard("{ArrowLeft}");
    expect(screen.getByRole("tab", { name: "Settings" })).toHaveFocus();
    expect(screen.getByText("Settings content")).toBeInTheDocument();
  });

  it("ignores keys outside its navigation set", async () => {
    render(<ExampleTabs />);
    const user = userEvent.setup();
    await user.tab();
    const overviewTab = screen.getByRole("tab", { name: "Overview" });
    expect(overviewTab).toHaveFocus();

    await user.keyboard("a");
    expect(overviewTab).toHaveFocus();
    expect(screen.getByText("Overview content")).toBeInTheDocument();
  });

  it("jumps to the first trigger with Home", async () => {
    render(<ExampleTabs />);
    const user = userEvent.setup();
    await user.tab();
    await user.keyboard("{ArrowRight}"); // move focus off the first trigger
    expect(screen.getByRole("tab", { name: "Settings" })).toHaveFocus();

    await user.keyboard("{Home}");
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();
    expect(screen.getByText("Overview content")).toBeInTheDocument();
  });

  it("switches the arrow-key axis to ArrowDown/ArrowUp when orientation is vertical", async () => {
    render(
      <Tabs.Root defaultValue="overview" orientation="vertical">
        <Tabs.List>
          <Tabs.Trigger value="overview">Overview</Tabs.Trigger>
          <Tabs.Trigger value="settings">Settings</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Panel value="overview">Overview content</Tabs.Panel>
        <Tabs.Panel value="settings">Settings content</Tabs.Panel>
      </Tabs.Root>
    );
    expect(screen.getByRole("tablist")).toHaveAttribute("aria-orientation", "vertical");

    const user = userEvent.setup();
    await user.tab();
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();

    // ArrowRight/Left do nothing on a vertical tablist — only Down/Up do.
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();

    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("tab", { name: "Settings" })).toHaveFocus();
    expect(screen.getByText("Settings content")).toBeInTheDocument();

    await user.keyboard("{ArrowUp}");
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();
  });

  it("defers to a caller's onKeyDown when it already called preventDefault", async () => {
    const onKeyDown = vi.fn((event: KeyboardEvent<HTMLDivElement>) => event.preventDefault());
    render(
      <Tabs.Root defaultValue="overview">
        <Tabs.List onKeyDown={onKeyDown}>
          <Tabs.Trigger value="overview">Overview</Tabs.Trigger>
          <Tabs.Trigger value="settings">Settings</Tabs.Trigger>
        </Tabs.List>
        <Tabs.Panel value="overview">Overview content</Tabs.Panel>
        <Tabs.Panel value="settings">Settings content</Tabs.Panel>
      </Tabs.Root>
    );
    const user = userEvent.setup();
    await user.tab();
    await user.keyboard("{ArrowRight}");

    expect(onKeyDown).toHaveBeenCalled();
    // Our own navigation bailed out because the caller's handler already
    // consumed the key — selection never moved off the first trigger.
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveFocus();
    expect(screen.getByText("Overview content")).toBeInTheDocument();
  });

  it("does nothing when the tablist has no triggers to navigate between", () => {
    render(
      <Tabs.Root defaultValue="overview">
        <Tabs.List />
        <Tabs.Panel value="overview">Overview content</Tabs.Panel>
      </Tabs.Root>
    );
    const tablist = screen.getByRole("tablist");
    expect(() => fireEvent.keyDown(tablist, { key: "ArrowRight" })).not.toThrow();
  });

  it("throws a clear error when a Tabs part is used outside Tabs.Root", () => {
    // Swallow the expected React error-boundary console.error noise for this one assertion.
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => render(<Tabs.Trigger value="overview">Overview</Tabs.Trigger>)).toThrow(
      "Tabs.* components must be rendered inside <Tabs.Root>"
    );
    spy.mockRestore();
  });
});
