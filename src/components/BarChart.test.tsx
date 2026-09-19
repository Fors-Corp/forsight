import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "../test-utils/axe";
import * as chartLib from "../lib/chart";
import { BarChart } from "./BarChart";

// Wraps (never replaces) niceScale so every existing assertion below still
// exercises the real geometry — this only adds a call-count probe for the
// memoization test at the bottom of the file.
vi.mock("../lib/chart", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../lib/chart")>();
  return { ...actual, niceScale: vi.fn(actual.niceScale) };
});

const labels = ["2xx", "3xx", "4xx", "5xx"];
const series = [
  { name: "checkout", values: [1200, 40, 90, 12] },
  { name: "search", values: [800, 20, 60, 4] },
];

describe("BarChart", () => {
  it("draws a bar per series per category", () => {
    const { container } = render(<BarChart label="Responses" labels={labels} series={series} />);
    // 4 categories x 2 series, each a path with a non-empty `d`.
    const bars = [...container.querySelectorAll("g > path")].filter((p) => p.getAttribute("d"));
    expect(bars).toHaveLength(8);
  });

  it("stacks segments and totals them in the readout", async () => {
    const user = userEvent.setup();
    const { container } = render(
      <BarChart label="Responses" labels={labels} series={series} stacked />
    );
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    plot.focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("status")).toHaveTextContent("2xx: checkout 1.2k, search 800");
  });

  it("omits the stacked total when there is only one series", async () => {
    const user = userEvent.setup();
    const { container } = render(
      <BarChart label="Responses" labels={labels} series={[series[0]]} stacked />
    );
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    plot.focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.queryByText("Total")).not.toBeInTheDocument();
  });

  it("treats a missing sample as zero rather than a gap", () => {
    render(
      <BarChart
        label="Responses"
        labels={["2xx", "5xx"]}
        series={[{ name: "checkout", values: [10, null] }]}
      />
    );
    expect(screen.getByRole("cell", { name: "no data" })).toBeInTheDocument();
  });

  it("tracks a pointer to the band under it", () => {
    const { container } = render(<BarChart label="Responses" labels={labels} series={series} />);
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    plot.getBoundingClientRect = () =>
      ({
        left: 0,
        top: 0,
        width: 600,
        height: 220,
        right: 600,
        bottom: 220,
        x: 0,
        y: 0,
      }) as DOMRect;

    fireEvent(plot, new MouseEvent("pointermove", { clientX: 500, bubbles: true }));
    expect(screen.getByRole("status")).toHaveTextContent("5xx");

    fireEvent.pointerLeave(plot);
    expect(screen.getByRole("status")).toHaveTextContent("");
  });

  it("ignores a pointer event with no coordinates", () => {
    const { container } = render(<BarChart label="Responses" labels={labels} series={series} />);
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    // The guard under test fires when a pointer event carries no usable
    // coordinate. It cannot be reached through fireEvent's init object any
    // more: jsdom 30 implements PointerEvent properly, and `clientX` is a
    // WebIDL `long`, so both an omitted value and NaN arrive as 0 — a
    // perfectly valid coordinate the component is right to honour. Defining
    // the property on a constructed event sidesteps that coercion and aims
    // the test back at the branch it names.
    const event = new PointerEvent("pointermove", { bubbles: true });
    Object.defineProperty(event, "clientX", { value: NaN });
    fireEvent(plot, event);
    expect(screen.getByRole("status")).toHaveTextContent("");
  });

  it("renders with no data at all", () => {
    const { container } = render(<BarChart label="Nothing" labels={[]} series={[]} />);
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    fireEvent(plot, new MouseEvent("pointermove", { clientX: 10, bubbles: true }));
    expect(screen.getByRole("img", { name: "Nothing" })).toBeInTheDocument();
  });

  it("has no accessibility violations", async () => {
    const { container } = render(
      <BarChart label="Responses" description="By status class" labels={labels} series={series} />
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("draws a horizontal reference line for a value annotation", () => {
    const { container } = render(
      <BarChart
        label="Responses"
        labels={labels}
        series={series}
        annotations={[{ value: 1000, text: "Capacity: 1000", tone: "warning" }]}
      />
    );
    expect(container.querySelectorAll("line.stroke-warning")).toHaveLength(1);
    expect(screen.getByText("Capacity: 1000")).toBeInTheDocument();
  });

  it("draws a vertical marker for a label annotation matching a category", () => {
    render(
      <BarChart
        label="Responses"
        labels={labels}
        series={series}
        annotations={[{ label: "4xx", text: "Rate limit added", tone: "accent" }]}
      />
    );
    expect(screen.getByText("Rate limit added")).toBeInTheDocument();
  });

  it("silently skips a label annotation that matches no category", () => {
    render(
      <BarChart
        label="Responses"
        labels={labels}
        series={series}
        annotations={[{ label: "9xx", text: "Ghost marker" }]}
      />
    );
    expect(screen.queryByText("Ghost marker")).not.toBeInTheDocument();
  });

  it("folds annotation text into the visually hidden description", () => {
    render(
      <BarChart
        label="Responses"
        labels={labels}
        series={series}
        annotations={[{ value: 1000, text: "Capacity: 1000" }]}
      />
    );
    // Both the SVG <desc> and the sr-only table <caption> carry it.
    expect(screen.getAllByText(/Reference lines: Capacity: 1000\./)).toHaveLength(2);
  });

  it("has no accessibility violations with annotations", async () => {
    const { container } = render(
      <BarChart
        label="Responses"
        labels={labels}
        series={series}
        annotations={[
          { value: 1000, text: "Capacity: 1000", tone: "warning" },
          { label: "4xx", text: "Rate limit added", tone: "accent" },
        ]}
      />
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("does not recompute the scale on a re-render with unchanged series/labels", () => {
    const niceScale = chartLib.niceScale as unknown as ReturnType<typeof vi.fn>;
    niceScale.mockClear();

    const { rerender } = render(<BarChart label="Responses" labels={labels} series={series} />);
    const callsAfterMount = niceScale.mock.calls.length;
    expect(callsAfterMount).toBeGreaterThan(0);

    rerender(<BarChart label="Responses" labels={labels} series={series} />);
    expect(niceScale.mock.calls.length).toBe(callsAfterMount);

    rerender(
      <BarChart
        label="Responses"
        labels={labels}
        series={[{ name: "checkout", values: [5000, 40, 90, 12] }]}
      />
    );
    expect(niceScale.mock.calls.length).toBeGreaterThan(callsAfterMount);
  });
});
