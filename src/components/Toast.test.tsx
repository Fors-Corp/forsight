import { afterEach, describe, expect, it } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { act } from "react";
import { Toaster, dismissToast, toast } from "./Toast";
import { axe } from "../test-utils/axe";

describe("Toast", () => {
  // `toast`/`dismissToast` share one module-level queue across every test in
  // this file (see the `toastState` module state in Toast.tsx) — RTL's own
  // `afterEach(cleanup)` (vitest.setup.ts) only unmounts the DOM, it doesn't
  // touch that queue, so a toast enqueued by one test would otherwise still
  // be in it for the next test's `<Toaster />`. Track every id this file
  // creates and dismiss it once its test finishes (a redundant dismiss, e.g.
  // for a toast a test already closed itself, is a harmless no-op filter).
  const enqueuedIds: string[] = [];

  afterEach(() => {
    while (enqueuedIds.length > 0) {
      dismissToast(enqueuedIds.pop() as string);
    }
  });

  function enqueue(item: Parameters<typeof toast>[0]) {
    let id = "";
    act(() => {
      id = toast(item);
    });
    enqueuedIds.push(id);
    return id;
  }

  it("renders a toast enqueued via toast()", async () => {
    render(<Toaster />);
    enqueue({ title: "Deployed", description: "v14 is live." });
    await waitFor(() => expect(screen.getByText("Deployed")).toBeInTheDocument());
    expect(screen.getByText("v14 is live.")).toBeInTheDocument();
  });

  it("applies responsive viewport width for small screens", () => {
    const { container } = render(<Toaster />);
    const viewport = container.querySelector('ol[class*="max-w-\\[calc"]');
    expect(viewport).toHaveClass("max-w-[calc(100vw-2rem)]");
    expect(viewport).toHaveClass("sm:max-w-sm");
  });

  it("applies the close button's size utility classes (min-h-9 min-w-9)", async () => {
    // Class-name assertion only — jsdom has no layout engine. The real
    // touch-target-size assertion is a Storybook play test (see
    // Toast.stories.tsx), which runs in a real browser via
    // test:storybook:ci.
    const { container } = render(<Toaster />);
    enqueue({ title: "Test", description: "Test toast" });
    await waitFor(() => expect(screen.getByText("Test")).toBeInTheDocument());
    const closeButton = container.querySelector('button[aria-label="Dismiss"]');
    expect(closeButton).toHaveClass("min-h-9");
    expect(closeButton).toHaveClass("min-w-9");
  });

  // The viewport renders in the tree, not a portal, so this covers the
  // enqueued toast itself: its role, its accessible name, its close button.
  it("has no axe violations with a toast enqueued", async () => {
    const { container } = render(<Toaster />);
    enqueue({ title: "Rollback complete", description: "v13 is live again." });
    await waitFor(() => expect(screen.getByText("Rollback complete")).toBeInTheDocument());
    expect(await axe(container)).toHaveNoViolations();
  });

  it("removes a toast from the queue when its close button is clicked", async () => {
    render(<Toaster />);
    enqueue({ title: "Ephemeral", description: "Dismiss me." });
    await waitFor(() => expect(screen.getByText("Ephemeral")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "Dismiss" }));

    await waitFor(() => expect(screen.queryByText("Ephemeral")).not.toBeInTheDocument());
  });

  it("renders a title-only toast without a description block", async () => {
    render(<Toaster />);
    enqueue({ title: "Deploy started" });
    await waitFor(() => expect(screen.getByText("Deploy started")).toBeInTheDocument());
    // Only the title landed in the queue item — nothing else to find.
    expect(screen.queryByText("v14 is live.")).not.toBeInTheDocument();
  });

  it("renders a description-only toast without a title element", async () => {
    const { container } = render(<Toaster />);
    enqueue({ description: "Build #204 finished in 42s." });
    await waitFor(() =>
      expect(screen.getByText("Build #204 finished in 42s.")).toBeInTheDocument()
    );
    // ToastTitle is the only element styled with font-semibold — assert
    // none mounted, since no `title` was enqueued.
    expect(container.querySelector(".font-semibold")).not.toBeInTheDocument();
  });
});
