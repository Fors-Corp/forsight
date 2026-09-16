import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import { TraceWaterfall } from "./TraceWaterfall";
import type { TraceSpan } from "./TraceWaterfall";

const spans: TraceSpan[] = [
  { id: "a", name: "POST /checkout", service: "edge", start: 0, duration: 400 },
  { id: "b", name: "SELECT orders", service: "db", start: 40, duration: 120, depth: 1 },
  {
    id: "c",
    name: "charge",
    service: "payments",
    start: 200,
    duration: 180,
    depth: 1,
    status: "error",
  },
];

describe("TraceWaterfall", () => {
  it("renders each span as a table row with its own duration", () => {
    render(<TraceWaterfall label="trace 9f2c" spans={spans} />);
    expect(screen.getByRole("rowheader", { name: /POST \/checkout/ })).toBeInTheDocument();
    expect(screen.getByText("120ms")).toBeInTheDocument();
    expect(screen.getByText("180ms")).toBeInTheDocument();
  });

  it("scales bars against the trace's wall time", () => {
    const { container } = render(<TraceWaterfall label="trace" spans={spans} />);
    const bars = [...container.querySelectorAll("td span span")] as HTMLElement[];
    expect(bars[0].style.width).toBe("100%");
    expect(bars[1].style.left).toBe("10%");
    expect(bars[1].style.width).toBe("30%");
  });

  it("takes an explicit total when the root span owns the truth", () => {
    const { container } = render(<TraceWaterfall label="trace" spans={spans} total={800} />);
    const bars = [...container.querySelectorAll("td span span")] as HTMLElement[];
    expect(bars[0].style.width).toBe("50%");
    expect(screen.getByText(/800ms total/)).toBeInTheDocument();
  });

  it("marks a failed span with a word, not just a red bar", () => {
    render(<TraceWaterfall label="trace" spans={spans} />);
    expect(screen.getByText("error")).toBeInTheDocument();
  });

  it("indents by depth so nesting is visible", () => {
    const { container } = render(<TraceWaterfall label="trace" spans={spans} />);
    const cells = container.querySelectorAll("th[scope='row'] > span");
    expect((cells[0] as HTMLElement).style.paddingInlineStart).toBe("0px");
    expect((cells[1] as HTMLElement).style.paddingInlineStart).toBe("12px");
  });

  it("keeps a zero-duration span visible", () => {
    const { container } = render(
      <TraceWaterfall label="trace" spans={[{ id: "z", name: "noop", start: 0, duration: 0 }]} />
    );
    const bar = container.querySelector("td span span") as HTMLElement;
    expect(bar.style.width).toBe("0.5%");
  });

  it("scrolls sideways rather than pushing the page wide", () => {
    const { container } = render(<TraceWaterfall label="trace" spans={spans} />);
    expect(container.firstElementChild?.getAttribute("class")).toContain("overflow-x-auto");
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<TraceWaterfall label="trace 9f2c" spans={spans} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
