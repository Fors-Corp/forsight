import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import { CalendarHeatmap } from "./CalendarHeatmap";

describe("CalendarHeatmap", () => {
  it("buckets days into weekday rows and week columns", () => {
    // 2026-03-02 is a Monday; the whole week (Sun 3/1 - Sat 3/7) should
    // appear as one column, with each day on its matching weekday row.
    render(
      <CalendarHeatmap
        label="Deploys this week"
        days={[
          { date: "2026-03-02", value: 2 },
          { date: "2026-03-04", value: 5 },
        ]}
      />
    );
    expect(screen.getByRole("rowheader", { name: "Mon" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "2" })).toBeInTheDocument();
    expect(screen.getByRole("cell", { name: "5" })).toBeInTheDocument();
    // A full Sun-Sat week is 7 cells, even though only 2 days were given.
    expect(screen.getAllByRole("cell")).toHaveLength(7);
  });

  it("renders a week-start date as each column's header", () => {
    render(<CalendarHeatmap label="Deploys" days={[{ date: "2026-03-02", value: 1 }]} />);
    // The Sunday on/before 2026-03-02 is 2026-03-01.
    expect(screen.getByRole("columnheader", { name: "Mar 1" })).toBeInTheDocument();
  });

  it("treats an untracked day as an empty cell, distinct from a tracked zero", () => {
    render(
      <CalendarHeatmap
        label="Deploys"
        days={[
          { date: "2026-03-01", value: 0 },
          { date: "2026-03-03", value: 4 },
        ]}
      />
    );
    // 2026-03-01 was explicitly given as 0 -> a real value, not "no data".
    expect(screen.getByRole("cell", { name: "0" })).toBeInTheDocument();
    // The other 5 days of that week were never given -> untracked.
    expect(screen.getAllByRole("cell", { name: "no data" })).toHaveLength(5);
  });

  it("spans multiple week columns for a range crossing a week boundary", () => {
    render(
      <CalendarHeatmap
        label="Deploys"
        days={[
          { date: "2026-03-02", value: 1 }, // Mon, week of Mar 1
          { date: "2026-03-09", value: 1 }, // Mon, week of Mar 8
        ]}
      />
    );
    expect(screen.getByRole("columnheader", { name: "Mar 1" })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Mar 8" })).toBeInTheDocument();
  });

  it("renders an empty grid when given no days", () => {
    const { container } = render(<CalendarHeatmap label="Deploys" days={[]} />);
    expect(screen.queryAllByRole("cell")).toHaveLength(0);
    expect(container.querySelector("table")).toBeInTheDocument();
  });

  it("has no accessibility violations", async () => {
    const { container } = render(
      <CalendarHeatmap
        label="Deploys this week"
        days={[
          { date: "2026-03-02", value: 2 },
          { date: "2026-03-04", value: 5 },
        ]}
      />
    );
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe("weekday labels", () => {
  it("come from Intl, not a hardcoded English array", () => {
    // Asserts delegation, not literal strings, so it holds in any locale —
    // and it failed before, when a German reader got German month labels one
    // line above English weekday labels in the same grid.
    const expected = new Intl.DateTimeFormat(undefined, {
      weekday: "short",
      timeZone: "UTC",
    });
    const firstSunday = Date.UTC(2026, 0, 4);
    const wanted = Array.from({ length: 7 }, (_, day) =>
      expected.format(new Date(firstSunday + day * 86_400_000))
    );

    render(<CalendarHeatmap days={[{ date: "2026-01-04", value: 1 }]} label="Deploys" />);
    for (const name of wanted) {
      expect(screen.getByRole("rowheader", { name })).toBeInTheDocument();
    }
  });
});
