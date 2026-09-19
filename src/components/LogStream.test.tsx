import { describe, expect, it } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import { LogStream, LOG_LEVELS, type LogEntry } from "./LogStream";

const entries = [
  {
    id: "1",
    timestamp: "14:02:11",
    level: "info" as const,
    message: "deploy started",
    source: "ci",
  },
  { id: "2", timestamp: "14:02:44", level: "error" as const, message: "connection refused" },
];

/** `count` entries, each with a distinct, greppable message. */
function manyEntries(count: number): LogEntry[] {
  return Array.from({ length: count }, (_, i) => ({
    id: String(i),
    timestamp: `14:${String(i).padStart(4, "0")}`,
    level: "info" as const,
    message: `line ${i}`,
  }));
}

describe("LogStream", () => {
  it("is an ARIA log region named after the stream", () => {
    render(<LogStream label="checkout-api logs" entries={entries} />);
    expect(screen.getByRole("log", { name: "checkout-api logs" })).toBeInTheDocument();
  });

  it("prints the level as a word next to each line", () => {
    render(<LogStream label="logs" entries={entries} />);
    expect(screen.getByText("info")).toBeInTheDocument();
    expect(screen.getByText("error")).toBeInTheDocument();
    expect(screen.getByText("connection refused")).toBeInTheDocument();
    expect(screen.getByText("ci")).toBeInTheDocument();
  });

  it("stays silent unless announcing is explicitly asked for", () => {
    const { rerender } = render(<LogStream label="logs" entries={entries} />);
    expect(screen.getByRole("log")).toHaveAttribute("aria-live", "off");
    rerender(<LogStream label="logs" entries={entries} announce />);
    expect(screen.getByRole("log")).toHaveAttribute("aria-live", "polite");
  });

  it("says when the window is empty", () => {
    render(<LogStream label="logs" entries={[]} />);
    expect(screen.getByText("No log lines in this window.")).toBeInTheDocument();
  });

  it("scrolls inside its own box, and that box is reachable by keyboard", () => {
    render(<LogStream label="logs" entries={entries} maxHeight={200} />);
    const region = screen.getByRole("log");
    expect(region).toHaveStyle({ maxHeight: "200px" });
    expect(region.getAttribute("class")).toContain("overflow-y-auto");
    expect(region).toHaveAttribute("tabindex", "0");
  });

  it("exports the levels in severity order for filter controls", () => {
    expect(LOG_LEVELS).toEqual(["debug", "info", "warn", "error", "fatal"]);
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<LogStream label="checkout-api logs" entries={entries} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  describe("windowing a large feed", () => {
    it("renders every row at or under the threshold, unwindowed", () => {
      const atThreshold = manyEntries(200);
      const { container } = render(<LogStream label="logs" entries={atThreshold} />);
      // One <time> per real row (spacer rows carry none) — every entry mounted.
      expect(container.querySelectorAll("time")).toHaveLength(200);
      // No aria-setsize/-posinset below the threshold: DOM stays identical
      // to a caller who never asked for a large feed.
      expect(container.querySelector("li[aria-setsize]")).not.toBeInTheDocument();
    });

    it("mounts far fewer DOM rows than entries once past the threshold", () => {
      const lots = manyEntries(2000);
      const { container } = render(<LogStream label="logs" entries={lots} maxHeight={320} />);
      const rendered = container.querySelectorAll("time").length;
      expect(rendered).toBeGreaterThan(0);
      expect(rendered).toBeLessThan(200);
    });

    it("keeps the true count on each mounted row once windowed", () => {
      const lots = manyEntries(2000);
      const { container } = render(<LogStream label="logs" entries={lots} />);
      const rows = container.querySelectorAll("li[aria-setsize]");
      expect(rows.length).toBeGreaterThan(0);
      rows.forEach((row) => {
        expect(row).toHaveAttribute("aria-setsize", "2000");
        expect(row).toHaveAttribute("aria-posinset");
      });
    });

    it("scrolling reveals later rows that weren't mounted at the top", () => {
      const lots = manyEntries(2000);
      render(<LogStream label="logs" entries={lots} maxHeight={320} />);
      const region = screen.getByRole("log");

      expect(screen.getByText("line 0")).toBeInTheDocument();
      expect(screen.queryByText("line 1900")).not.toBeInTheDocument();

      fireEvent.scroll(region, { target: { scrollTop: 1900 * 32 } });

      expect(screen.getByText("line 1900")).toBeInTheDocument();
      expect(screen.queryByText("line 0")).not.toBeInTheDocument();
    });

    it("still exposes the count of entries with no message", () => {
      render(<LogStream label="logs" entries={[]} />);
      expect(screen.getByText("No log lines in this window.")).toBeInTheDocument();
    });

    it("keeps the region focusable and its own scroll box once windowed", () => {
      const lots = manyEntries(2000);
      render(<LogStream label="logs" entries={lots} maxHeight={200} />);
      const region = screen.getByRole("log");
      expect(region).toHaveStyle({ maxHeight: "200px" });
      expect(region.getAttribute("class")).toContain("overflow-y-auto");
      expect(region).toHaveAttribute("tabindex", "0");
    });

    it("preserves aria-live/aria-relevant once windowed", () => {
      const lots = manyEntries(2000);
      render(<LogStream label="logs" entries={lots} announce />);
      const region = screen.getByRole("log");
      expect(region).toHaveAttribute("aria-live", "polite");
      expect(region).toHaveAttribute("aria-relevant", "additions");
    });

    it("has no accessibility violations once windowed", async () => {
      const lots = manyEntries(2000);
      const { container } = render(<LogStream label="checkout-api logs" entries={lots} />);
      expect(await axe(container)).toHaveNoViolations();
    });
  });
});
