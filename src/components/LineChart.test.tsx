import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "../test-utils/axe";
import * as chartLib from "../lib/chart";
import { LineChart, pickLabelIndices } from "./LineChart";

// Wraps (never replaces) niceScale so every existing assertion below still
// exercises the real geometry — this only adds a call-count probe for the
// memoization test at the bottom of the file.
vi.mock("../lib/chart", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../lib/chart")>();
  return { ...actual, niceScale: vi.fn(actual.niceScale) };
});

const labels = ["12:00", "13:00", "14:00", "15:00"];
const series = [
  { name: "us-east", values: [120, 180, 140, 200] },
  { name: "eu-west", values: [90, 110, null, 130] },
];

/**
 * jsdom implements no PointerEvent, so `fireEvent.pointerMove` produces an
 * event with no coordinates. A MouseEvent dispatched under the pointermove
 * type reaches React's onPointerMove with a real clientX.
 */
function pointerMoveAt(element: HTMLElement, clientX: number) {
  fireEvent(element, new MouseEvent("pointermove", { clientX, bubbles: true }));
}

function renderChart(props: Partial<React.ComponentProps<typeof LineChart>> = {}) {
  return render(
    <LineChart label="Requests per second" labels={labels} series={series} {...props} />
  );
}

describe("LineChart", () => {
  it("draws one path per series and skips a null sample", () => {
    const { container } = renderChart();
    // us-east is unbroken (1 path); eu-west's null splits it into 2.
    expect(container.querySelectorAll("path")).toHaveLength(3);
  });

  it("adds a fill under the line when asked", () => {
    const { container } = render(
      <LineChart label="Requests" labels={labels} series={[series[0]]} area />
    );
    expect(container.querySelectorAll("path")).toHaveLength(2);
  });

  it("shows a legend only when there is more than one series", () => {
    const { rerender } = renderChart();
    // The legend is the only list in the tree; the data table is a table.
    expect(screen.getByRole("list")).toHaveTextContent("eu-west");
    rerender(<LineChart label="Requests" labels={labels} series={[series[0]]} />);
    expect(screen.queryByRole("list")).not.toBeInTheDocument();
  });

  it("publishes every point in the data table, marking gaps", () => {
    renderChart();
    expect(screen.getByRole("rowheader", { name: "eu-west" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "no data" })).toBeInTheDocument();
  });

  it("lets a consumer translate the no-data label", async () => {
    const user = userEvent.setup();
    const { container } = renderChart({ noDataLabel: "sem dados" });

    // Data table: the gap cell reads the override, not the English literal.
    expect(screen.getByRole("cell", { name: "sem dados" })).toBeInTheDocument();
    expect(screen.queryByRole("cell", { name: "no data" })).not.toBeInTheDocument();

    // Cursor readout: focusing the gapped point (index 2, "14:00") announces
    // the override too — three ArrowRights from an unfocused cursor land on
    // it (index 0, then 1, then 2).
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    plot.focus();
    await user.keyboard("{ArrowRight}{ArrowRight}{ArrowRight}");
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("14:00");
    expect(status).toHaveTextContent("eu-west sem dados");
  });

  it("moves the cursor with the keyboard and announces the reading", async () => {
    const user = userEvent.setup();
    const { container } = renderChart();
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;

    plot.focus();
    await user.keyboard("{ArrowRight}");
    const status = screen.getByRole("status");
    expect(status).toHaveTextContent("12:00");

    await user.keyboard("{End}");
    expect(status).toHaveTextContent("15:00");
    expect(status).toHaveTextContent("eu-west 130");

    await user.keyboard("{Home}");
    expect(status).toHaveTextContent("12:00");

    await user.keyboard("{ArrowLeft}");
    expect(status).toHaveTextContent("12:00");
  });

  it("dismisses the readout on Escape and on blur", async () => {
    const user = userEvent.setup();
    const { container } = renderChart();
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;

    plot.focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("status")).toHaveTextContent("12:00");

    await user.keyboard("{Escape}");
    expect(screen.getByRole("status")).toHaveTextContent("");

    await user.keyboard("{ArrowRight}");
    fireEvent.blur(plot);
    expect(screen.getByRole("status")).toHaveTextContent("");
  });

  it("ignores keys it doesn't own", async () => {
    const user = userEvent.setup();
    const { container } = renderChart();
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    plot.focus();
    await user.keyboard("a");
    expect(screen.getByRole("status")).toHaveTextContent("");
  });

  it("tracks a pointer over the plot and clears on leave", () => {
    const { container } = renderChart();
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    // jsdom has no layout, so the plot's box is stubbed to make the
    // pointer-to-index maths meaningful.
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

    pointerMoveAt(plot, 344);
    expect(screen.getByRole("status")).toHaveTextContent("14:00");

    fireEvent.pointerLeave(plot);
    expect(screen.getByRole("status")).toHaveTextContent("");
  });

  it("ignores a pointer event that carries no coordinates", () => {
    const { container } = renderChart();
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

  it("renders an empty series set without crashing", () => {
    const { container } = render(<LineChart label="Nothing yet" labels={[]} series={[]} />);
    const plot = container.querySelector("[tabindex='0']") as HTMLElement;
    pointerMoveAt(plot, 10);
    fireEvent.keyDown(plot, { key: "ArrowRight" });
    expect(screen.getByRole("img", { name: "Nothing yet" })).toBeInTheDocument();
  });

  it("centers a single-point series instead of dividing by zero", () => {
    const { container } = render(
      <LineChart label="One sample" labels={["12:00"]} series={[{ name: "a", values: [5] }]} />
    );
    expect(container.querySelector("path")?.getAttribute("d")).toMatch(/^M/);
  });

  it("has no accessibility violations", async () => {
    const { container } = renderChart({ description: "Two regions" });
    expect(await axe(container)).toHaveNoViolations();
  });

  it("draws a horizontal reference line for a value annotation", () => {
    const { container } = renderChart({
      annotations: [{ value: 150, text: "SLO: 150ms", tone: "danger" }],
    });
    const lines = container.querySelectorAll("line.stroke-danger");
    expect(lines).toHaveLength(1);
    expect(screen.getByText("SLO: 150ms")).toBeInTheDocument();
  });

  it("draws a vertical marker for a label annotation matching a category", () => {
    renderChart({
      annotations: [{ label: "13:00", text: "Deploy v2.4.1", tone: "accent" }],
    });
    expect(screen.getByText("Deploy v2.4.1")).toBeInTheDocument();
  });

  it("silently skips a label annotation that matches no category", () => {
    renderChart({
      annotations: [{ label: "not-a-real-time", text: "Ghost marker" }],
    });
    expect(screen.queryByText("Ghost marker")).not.toBeInTheDocument();
  });

  it("folds annotation text into the visually hidden description", () => {
    renderChart({ annotations: [{ value: 150, text: "SLO: 150ms" }] });
    // Both the SVG <desc> and the sr-only table <caption> carry it.
    expect(screen.getAllByText(/Reference lines: SLO: 150ms\./)).toHaveLength(2);
  });

  it("has no accessibility violations with annotations", async () => {
    const { container } = renderChart({
      annotations: [
        { value: 150, text: "SLO: 150ms", tone: "danger" },
        { label: "13:00", text: "Deploy v2.4.1", tone: "accent" },
      ],
    });
    expect(await axe(container)).toHaveNoViolations();
  });

  it("draws a projected series dashed past dashedFrom, solid before it", () => {
    const { container } = renderChart({
      series: [{ ...series[0], dashedFrom: 2 }],
    });
    // One un-dashed path for the leading run, one dashed path for the tail —
    // shape, not color, is what marks the projection (CONTRIBUTING.md).
    const paths = container.querySelectorAll("path");
    expect(paths).toHaveLength(2);
    expect(container.querySelectorAll("path[stroke-dasharray]")).toHaveLength(1);
    expect(container.querySelector("path:not([stroke-dasharray])")).not.toHaveAttribute(
      "stroke-dasharray"
    );
  });

  it("renders every point solid when a series has no dashedFrom", () => {
    const { container } = renderChart();
    expect(container.querySelectorAll("path[stroke-dasharray]")).toHaveLength(0);
  });

  it("folds a series' projection point into the visually hidden description", () => {
    renderChart({ series: [{ ...series[0], dashedFrom: 2 }] });
    // Both the SVG <desc> and the sr-only table <caption> carry it.
    expect(screen.getAllByText(/us-east is projected from 14:00\./)).toHaveLength(2);
  });

  it("has no accessibility violations with a projected series", async () => {
    const { container } = renderChart({ series: [{ ...series[0], dashedFrom: 2 }] });
    expect(await axe(container)).toHaveNoViolations();
  });

  it("fills the observed run solid and the projected run with a lighter, hatched treatment", () => {
    const { container } = render(
      <LineChart label="Requests" labels={labels} series={[{ ...series[0], dashedFrom: 2 }]} area />
    );
    // The observed run (indices 0-2) fills at the ordinary area opacity.
    const solidArea = container.querySelector("path.opacity-20");
    expect(solidArea).toBeInTheDocument();
    // The projected run (indices 2-3) fills lighter still...
    const lighterArea = container.querySelector("path.opacity-10");
    expect(lighterArea).toBeInTheDocument();
    // ...and shape, not just opacity, carries the distinction: a hatch
    // pattern is defined once and referenced by a second fill path over the
    // same projected run (CONTRIBUTING.md's data-viz rule — color, and by
    // extension opacity alone, is never the only carrier).
    const pattern = container.querySelector("pattern");
    expect(pattern).toBeInTheDocument();
    const hatchedArea = container.querySelector(`path[fill="url(#${pattern?.id})"]`);
    expect(hatchedArea).toBeInTheDocument();
    // Same run, drawn twice: the lighter fill and the hatch trace the same path.
    expect(hatchedArea?.getAttribute("d")).toBe(lighterArea?.getAttribute("d"));
    expect(container.querySelectorAll("pattern")).toHaveLength(1);
  });

  it("fills the whole run solid, with no hatch, when a series has no dashedFrom", () => {
    const { container } = render(
      <LineChart label="Requests" labels={labels} series={[series[0]]} area />
    );
    expect(container.querySelectorAll("path")).toHaveLength(2);
    expect(container.querySelector("pattern")).not.toBeInTheDocument();
    expect(container.querySelector('path[fill^="url("]')).not.toBeInTheDocument();
  });

  it("has no accessibility violations with a projected area series", async () => {
    const { container } = renderChart({
      series: [{ ...series[0], dashedFrom: 2 }],
      area: true,
    });
    expect(await axe(container)).toHaveNoViolations();
  });

  it("does not recompute the scale on a re-render with unchanged series/labels", () => {
    const niceScale = chartLib.niceScale as unknown as ReturnType<typeof vi.fn>;
    niceScale.mockClear();

    const { rerender } = renderChart();
    const callsAfterMount = niceScale.mock.calls.length;
    expect(callsAfterMount).toBeGreaterThan(0);

    // Same `label`/`labels`/`series` references as the mount above — this is
    // the shape of a parent re-rendering (a dashboard's 5s poll, a sibling's
    // state change) without the chart's own data changing. The extent scan
    // and niceScale it feeds must not run again.
    rerender(
      <LineChart label="Requests per second" labels={labels} series={series} />
    );
    expect(niceScale.mock.calls.length).toBe(callsAfterMount);

    // A real data change (a new `series` reference/value) must still recompute.
    rerender(
      <LineChart
        label="Requests per second"
        labels={labels}
        series={[{ name: "us-east", values: [500, 600, 700, 800] }]}
      />
    );
    expect(niceScale.mock.calls.length).toBeGreaterThan(callsAfterMount);
  });
});

describe("pickLabelIndices", () => {
  it("labels every category when they fit", () => {
    expect(pickLabelIndices(4, 600)).toEqual([0, 1, 2, 3]);
  });

  it("thins evenly spaced labels when they don't", () => {
    const picked = pickLabelIndices(50, 600);
    expect(picked[0]).toBe(0);
    expect(picked[picked.length - 1]).toBe(49);
    expect(picked.length).toBeLessThanOrEqual(6);
  });

  it("keeps at least two labels on a narrow plot", () => {
    expect(pickLabelIndices(50, 40).length).toBe(2);
  });

  it("returns nothing for no categories", () => {
    expect(pickLabelIndices(0, 600)).toEqual([]);
  });
});
