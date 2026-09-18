import * as React from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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

  describe("ref forwarding", () => {
    it("forwards a ref to Tabs.Root's DOM node", () => {
      const ref = React.createRef<HTMLDivElement>();
      render(
        <Tabs.Root ref={ref} defaultValue="a">
          <Tabs.Panel value="a">A</Tabs.Panel>
        </Tabs.Root>
      );
      expect(ref.current).toBeInstanceOf(HTMLDivElement);
    });

    it("forwards a ref to Tabs.List's DOM node", () => {
      const ref = React.createRef<HTMLDivElement>();
      render(
        <Tabs.Root defaultValue="a">
          <Tabs.List ref={ref}>
            <Tabs.Trigger value="a">A</Tabs.Trigger>
          </Tabs.List>
          <Tabs.Panel value="a">A</Tabs.Panel>
        </Tabs.Root>
      );
      expect(ref.current).toBe(screen.getByRole("tablist"));
    });

    it("forwards a ref to Tabs.Trigger's DOM node", () => {
      const ref = React.createRef<HTMLButtonElement>();
      render(
        <Tabs.Root defaultValue="a">
          <Tabs.List>
            <Tabs.Trigger ref={ref} value="a">
              A
            </Tabs.Trigger>
          </Tabs.List>
          <Tabs.Panel value="a">A</Tabs.Panel>
        </Tabs.Root>
      );
      expect(ref.current).toBe(screen.getByRole("tab", { name: "A" }));
    });

    it("forwards a ref to Tabs.Panel's DOM node", () => {
      const ref = React.createRef<HTMLDivElement>();
      render(
        <Tabs.Root defaultValue="a">
          <Tabs.Panel ref={ref} value="a">
            A
          </Tabs.Panel>
        </Tabs.Root>
      );
      expect(ref.current).toBe(screen.getByRole("tabpanel"));
    });
  });
});
