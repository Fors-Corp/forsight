import * as React from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "../test-utils/axe";
import {
  SidebarProvider,
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarTrigger,
  SidebarNav,
  SidebarNavItem,
  AppShell,
  AppShellMain,
  useSidebar,
} from "./Sidebar";

// The mobile drawer is built on @radix-ui/react-dialog (a portalled overlay),
// so — per CONTRIBUTING.md's jsdom boundary note — only its closed/default
// state is exercised here; real open/close/focus-trap behavior belongs in
// the Storybook test runner. The desktop static <nav> isn't portal-based,
// so it's tested fully, including axe().

function ExampleShell() {
  return (
    <SidebarProvider>
      <AppShell>
        <Sidebar label="Main navigation">
          <SidebarHeader>Forsight</SidebarHeader>
          <SidebarContent>
            <SidebarNav>
              <SidebarNavItem href="#overview" active icon={<svg aria-hidden="true" />}>
                Overview
              </SidebarNavItem>
              <SidebarNavItem href="#settings">Settings</SidebarNavItem>
            </SidebarNav>
          </SidebarContent>
          <SidebarFooter>v1.0</SidebarFooter>
        </Sidebar>
        <AppShellMain>
          <SidebarTrigger />
          <h1>Page content</h1>
        </AppShellMain>
      </AppShell>
    </SidebarProvider>
  );
}

describe("Sidebar", () => {
  it("renders a labelled navigation landmark with its nav items", () => {
    render(<ExampleShell />);
    const nav = screen.getByRole("navigation", { name: "Main navigation" });
    expect(nav).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Settings" })).not.toHaveAttribute("aria-current");
  });

  it("renders exactly one <main> landmark via AppShellMain", () => {
    render(<ExampleShell />);
    expect(screen.getAllByRole("main")).toHaveLength(1);
  });

  it("collapses via useSidebar's toggleCollapsed without hiding the nav item's accessible name", async () => {
    function Collapser() {
      const { toggleCollapsed } = useSidebar();
      return (
        <button type="button" onClick={toggleCollapsed}>
          Collapse
        </button>
      );
    }
    render(
      <SidebarProvider>
        <Collapser />
        <Sidebar label="Main navigation">
          <SidebarNav>
            <SidebarNavItem href="#overview">Overview</SidebarNavItem>
          </SidebarNav>
        </Sidebar>
      </SidebarProvider>
    );
    // sr-only, not display:none, so the link keeps a real accessible name
    // even once collapsed — see SidebarNavItem's own doc comment for why.
    expect(screen.getByRole("link", { name: "Overview" })).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Collapse" }));
    expect(screen.getByRole("link", { name: "Overview" })).toBeInTheDocument();
  });

  it("throws a clear error when a Sidebar part is used outside SidebarProvider", () => {
    // Swallow the expected React error-boundary console.error noise for this one assertion.
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(() => render(<Sidebar label="Main navigation">content</Sidebar>)).toThrow(
      "useSidebar must be used within a SidebarProvider."
    );
    spy.mockRestore();
  });

  it("has no accessibility violations in its default desktop state", async () => {
    const { container } = render(<ExampleShell />);
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe("SidebarNavItem", () => {
  function renderItem(href: string) {
    return render(
      <SidebarProvider>
        <SidebarNav>
          <SidebarNavItem href={href}>Overview</SidebarNavItem>
        </SidebarNav>
      </SidebarProvider>
    );
  }

  it("renders a real link for a safe href", () => {
    renderItem("/overview");
    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute("href", "/overview");
  });

  it("renders its non-link branch for a javascript: href", () => {
    renderItem("javascript:alert(document.cookie)");
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("Overview")).toBeInTheDocument();
  });
});

describe("SidebarTrigger", () => {
  it("exposes exactly one accessible control per breakpoint, both operable", async () => {
    const onToggle = vi.fn();
    function Wrapper() {
      const { collapsed, toggleCollapsed, mobileOpen, toggleMobileOpen } = useSidebar();
      onToggle.mockImplementation(() => ({ collapsed, mobileOpen }));
      return (
        <>
          <SidebarTrigger />
          <output data-testid="collapsed">{String(collapsed)}</output>
          <output data-testid="mobile-open">{String(mobileOpen)}</output>
          <button onClick={toggleCollapsed} type="button" hidden />
          <button onClick={toggleMobileOpen} type="button" hidden />
        </>
      );
    }
    render(
      <SidebarProvider>
        <Wrapper />
      </SidebarProvider>
    );
    await userEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    expect(screen.getByTestId("mobile-open")).toHaveTextContent("true");
    await userEvent.click(screen.getByRole("button", { name: "Toggle sidebar" }));
    expect(screen.getByTestId("collapsed")).toHaveTextContent("true");
  });
});

describe("ref forwarding", () => {
  it("forwards a ref to SidebarHeader's DOM node", () => {
    const ref = React.createRef<HTMLDivElement>();
    render(<SidebarHeader ref={ref}>Forsight</SidebarHeader>);
    expect(ref.current).toBe(screen.getByText("Forsight"));
  });

  it("forwards a ref to SidebarContent's DOM node", () => {
    const ref = React.createRef<HTMLDivElement>();
    render(<SidebarContent ref={ref}>content</SidebarContent>);
    expect(ref.current).toBe(screen.getByText("content"));
  });

  it("forwards a ref to SidebarFooter's DOM node", () => {
    const ref = React.createRef<HTMLDivElement>();
    render(<SidebarFooter ref={ref}>v1.0</SidebarFooter>);
    expect(ref.current).toBe(screen.getByText("v1.0"));
  });

  it("forwards a ref to SidebarNav's DOM node", () => {
    const ref = React.createRef<HTMLUListElement>();
    render(
      <SidebarProvider>
        <SidebarNav ref={ref}>
          <SidebarNavItem href="#overview">Overview</SidebarNavItem>
        </SidebarNav>
      </SidebarProvider>
    );
    expect(ref.current?.tagName).toBe("UL");
    expect(ref.current).toContainElement(screen.getByRole("link", { name: "Overview" }));
  });

  it("forwards a ref to AppShell's DOM node", () => {
    const ref = React.createRef<HTMLDivElement>();
    render(<AppShell ref={ref}>shell</AppShell>);
    expect(ref.current).toBe(screen.getByText("shell"));
  });

  it("forwards a ref to AppShellMain's DOM node", () => {
    const ref = React.createRef<HTMLElement>();
    render(<AppShellMain ref={ref}>main content</AppShellMain>);
    expect(ref.current).toBe(screen.getByRole("main"));
  });
});
