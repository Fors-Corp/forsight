import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import { AlertList, type AlertListItem } from "./AlertList";

const items: AlertListItem[] = [
  {
    id: "1",
    severity: "critical",
    title: "Elevated 5xx rate",
    description: "Error rate above 5%.",
    source: "checkout-api",
    time: "2 min ago",
  },
  {
    id: "2",
    severity: "info",
    title: "Disk usage above 80%",
    source: "worker-3",
    time: "20 min ago",
    resolved: true,
  },
];

describe("AlertList", () => {
  it("renders one item per alert with its severity as a word", () => {
    render(<AlertList label="Active alerts" items={items} />);
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByText("Critical")).toBeInTheDocument();
    expect(screen.getByText("Info")).toBeInTheDocument();
  });

  it("exposes the list's accessible name", () => {
    render(<AlertList label="Active alerts" items={items} />);
    expect(screen.getByRole("list", { name: "Active alerts" })).toBeInTheDocument();
  });

  it("marks a resolved alert with a Resolved badge", () => {
    render(<AlertList label="Active alerts" items={items} />);
    expect(screen.getByText("Resolved")).toBeInTheDocument();
  });

  it("renders source and description when present", () => {
    render(<AlertList label="Active alerts" items={items} />);
    expect(screen.getByText("checkout-api")).toBeInTheDocument();
    expect(screen.getByText("Error rate above 5%.")).toBeInTheDocument();
  });

  it("keeps the list itself mounted and shows the empty message inside it", () => {
    // Was: "renders an EmptyState instead of a list when there are no items",
    // asserting `queryByRole("list")` was absent when empty. That's the bug
    // (H10) — swapping the live region out for an EmptyState on 0 items means
    // going 0 -> 1 mounts the live region for the first time with content
    // already on it, which most AT/browser pairs never announce. The list
    // must stay mounted at every count, so this now asserts the opposite.
    render(<AlertList label="Active alerts" items={[]} emptyMessage="No active alerts." />);
    expect(screen.getByRole("list", { name: "Active alerts" })).toBeInTheDocument();
    expect(screen.getByText("No active alerts.")).toBeInTheDocument();
  });

  it("keeps the live region as the same DOM node when the first alert arrives", () => {
    // Pins H10: the live region has to be the one element that's already
    // mounted when an alert lands, or AT never picks up the mutation. Node
    // identity (not just presence) is what actually proves that — a fresh
    // node with the same role and name would still be silently missed.
    const { rerender } = render(<AlertList label="Active alerts" items={[]} />);
    const liveRegion = screen.getByRole("list", { name: "Active alerts" });

    rerender(<AlertList label="Active alerts" items={items} />);

    expect(screen.getByRole("list", { name: "Active alerts" })).toBe(liveRegion);
  });

  it("defaults to a polite live region, opt-out via announce", () => {
    const { rerender } = render(<AlertList label="Active alerts" items={items} />);
    expect(screen.getByRole("list")).toHaveAttribute("aria-live", "polite");

    rerender(<AlertList label="Active alerts" items={items} announce={false} />);
    expect(screen.getByRole("list")).toHaveAttribute("aria-live", "off");
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<AlertList label="Active alerts" items={items} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("has no accessibility violations when empty", async () => {
    const { container } = render(<AlertList label="Active alerts" items={[]} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
