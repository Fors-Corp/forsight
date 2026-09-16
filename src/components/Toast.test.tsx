import { describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { act } from "react";
import { Toaster, toast } from "./Toast";
import { axe } from "../test-utils/axe";

describe("Toast", () => {
  it("renders a toast enqueued via toast()", async () => {
    render(<Toaster />);
    act(() => {
      toast({ title: "Deployed", description: "v14 is live." });
    });
    await waitFor(() => expect(screen.getByText("Deployed")).toBeInTheDocument());
    expect(screen.getByText("v14 is live.")).toBeInTheDocument();
  });

  it("applies responsive viewport width for small screens", () => {
    const { container } = render(<Toaster />);
    const viewport = container.querySelector('ol[class*="max-w-\\[calc"]');
    expect(viewport).toHaveClass("max-w-[calc(100vw-2rem)]");
    expect(viewport).toHaveClass("sm:max-w-sm");
  });

  it("close button has adequate touch target size", async () => {
    const { container } = render(<Toaster />);
    act(() => {
      toast({ title: "Test", description: "Test toast" });
    });
    await waitFor(() => expect(screen.getByText("Test")).toBeInTheDocument());
    const closeButton = container.querySelector('button[aria-label="Dismiss"]');
    expect(closeButton).toHaveClass("min-h-9");
    expect(closeButton).toHaveClass("min-w-9");
  });

  // The viewport renders in the tree, not a portal, so this covers the
  // enqueued toast itself: its role, its accessible name, its close button.
  it("has no axe violations with a toast enqueued", async () => {
    const { container } = render(<Toaster />);
    act(() => {
      toast({ title: "Rollback complete", description: "v13 is live again." });
    });
    await waitFor(() => expect(screen.getByText("Rollback complete")).toBeInTheDocument());
    expect(await axe(container)).toHaveNoViolations();
  });
});
