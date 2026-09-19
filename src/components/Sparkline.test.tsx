import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import * as chartLib from "../lib/chart";
import { Sparkline } from "./Sparkline";

// Wraps (never replaces) niceScale so every existing assertion below still
// exercises the real geometry — this only adds a call-count probe for the
// memoization test at the bottom of the file.
vi.mock("../lib/chart", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../lib/chart")>();
  return { ...actual, niceScale: vi.fn(actual.niceScale) };
});

const values = [4, 9, 6, 12, 8];

describe("Sparkline", () => {
  it("summarizes the trend in its accessible name", () => {
    render(<Sparkline label="Error rate" values={values} />);
    expect(
      screen.getByRole("img", {
        name: "Error rate: 5 samples, low 4, high 12, latest 8",
      })
    ).toBeInTheDocument();
  });

  it("says so when there is no data", () => {
    render(<Sparkline label="Error rate" values={[]} />);
    expect(screen.getByRole("img", { name: "Error rate: no data" })).toBeInTheDocument();
  });

  it("draws a fill and a line by default", () => {
    const { container } = render(<Sparkline label="Rate" values={values} />);
    expect(container.querySelectorAll("path")).toHaveLength(2);
  });

  it("draws one mark per sample as bars", () => {
    const { container } = render(<Sparkline label="Rate" values={values} variant="bar" />);
    expect(container.querySelectorAll("path")).toHaveLength(values.length);
  });

  it("carries the tone through to the mark", () => {
    const { container } = render(<Sparkline label="Rate" values={values} tone="danger" />);
    expect(container.querySelector("path")?.getAttribute("class")).toContain("fill-danger");
  });

  it("centers a single sample instead of dividing by zero", () => {
    const { container } = render(<Sparkline label="Rate" values={[7]} />);
    expect(container.querySelector("path")?.getAttribute("d")).not.toContain("NaN");
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<Sparkline label="Error rate" values={values} />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("does not recompute the scale on a re-render with unchanged values", () => {
    const niceScale = chartLib.niceScale as unknown as ReturnType<typeof vi.fn>;
    niceScale.mockClear();

    const { rerender } = render(<Sparkline label="Rate" values={values} />);
    const callsAfterMount = niceScale.mock.calls.length;
    expect(callsAfterMount).toBeGreaterThan(0);

    rerender(<Sparkline label="Rate" values={values} />);
    expect(niceScale.mock.calls.length).toBe(callsAfterMount);

    rerender(<Sparkline label="Rate" values={[1, 2, 3]} />);
    expect(niceScale.mock.calls.length).toBeGreaterThan(callsAfterMount);
  });
});
