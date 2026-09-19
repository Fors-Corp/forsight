import { describe, expect, it } from "vitest";
import {
  arcPath,
  areaPath,
  barPath,
  clamp,
  formatActiveReading,
  formatCompact,
  formatDuration,
  formatPercent,
  linePath,
  niceScale,
  polar,
  project,
  seriesBg,
  seriesFill,
  seriesLegendItems,
  seriesStroke,
  SERIES_SLOTS,
  splitAtGaps,
  splitAtProjection,
  type Point,
} from "../chart";

describe("series colors", () => {
  it("assigns one distinct class per slot", () => {
    const fills = Array.from({ length: SERIES_SLOTS }, (_, i) => seriesFill(i));
    expect(new Set(fills).size).toBe(SERIES_SLOTS);
    expect(fills[0]).toBe("fill-viz-1");
    expect(seriesStroke(7)).toBe("stroke-viz-8");
    expect(seriesBg(2)).toBe("bg-viz-3");
  });

  it("falls back to a neutral 'Other' mark past the last slot", () => {
    expect(seriesFill(SERIES_SLOTS)).toBe("fill-fg-muted");
    expect(seriesStroke(99)).toBe("stroke-fg-muted");
    expect(seriesBg(SERIES_SLOTS)).toBe("bg-fg-muted");
  });
});

describe("seriesLegendItems", () => {
  it("returns null for zero or one series — nothing to key", () => {
    expect(seriesLegendItems([])).toBeNull();
    expect(seriesLegendItems([{ name: "us-east" }])).toBeNull();
  });

  it("maps each series to its label and slot index once there's more than one", () => {
    expect(
      seriesLegendItems([{ name: "us-east" }, { name: "eu-west" }, { name: "ap-south" }])
    ).toEqual([
      { label: "us-east", seriesIndex: 0 },
      { label: "eu-west", seriesIndex: 1 },
      { label: "ap-south", seriesIndex: 2 },
    ]);
  });
});

describe("clamp", () => {
  it("bounds a value on both sides", () => {
    expect(clamp(5, 0, 10)).toBe(5);
    expect(clamp(-1, 0, 10)).toBe(0);
    expect(clamp(11, 0, 10)).toBe(10);
  });
});

describe("niceScale", () => {
  it("rounds the domain out to round tick values", () => {
    const scale = niceScale(0, 87);
    expect(scale.min).toBe(0);
    expect(scale.max).toBeGreaterThanOrEqual(87);
    expect(scale.ticks[0]).toBe(scale.min);
    expect(scale.ticks[scale.ticks.length - 1]).toBe(scale.max);
  });

  it("gives flat positive data a zero baseline instead of a zero-height domain", () => {
    const scale = niceScale(40, 40);
    expect(scale.min).toBe(0);
    expect(scale.max).toBeGreaterThanOrEqual(40);
    expect(scale.max).toBeGreaterThan(scale.min);
  });

  it("gives flat negative data a zero ceiling", () => {
    const scale = niceScale(-12, -12);
    expect(scale.max).toBe(0);
    expect(scale.min).toBeLessThan(-12);
  });

  it("gives an all-zero series a usable 0-1 domain", () => {
    const scale = niceScale(0, 0);
    expect(scale.min).toBe(0);
    expect(scale.max).toBe(1);
  });

  it("treats a missing extent (empty data) as zero", () => {
    const scale = niceScale(Infinity, -Infinity);
    expect(scale.min).toBe(0);
    expect(scale.max).toBe(1);
  });

  it("keeps sub-unit ticks free of float noise", () => {
    const scale = niceScale(0, 1);
    for (const tick of scale.ticks) {
      expect(String(tick).length).toBeLessThanOrEqual(5);
    }
  });

  it("accepts a reversed extent", () => {
    expect(niceScale(90, 10)).toEqual(niceScale(10, 90));
  });
});

describe("project", () => {
  it("maps a value into the pixel range", () => {
    expect(project(50, 0, 100, 200)).toBe(100);
    expect(project(0, 0, 100, 200)).toBe(0);
  });

  it("returns 0 for a collapsed domain rather than dividing by zero", () => {
    expect(project(5, 5, 5, 200)).toBe(0);
  });
});

describe("path builders", () => {
  const points: Point[] = [
    [0, 10],
    [10, 0],
  ];

  it("draws a polyline", () => {
    expect(linePath(points)).toBe("M0 10 L10 0");
  });

  it("closes an area down to the baseline", () => {
    expect(areaPath(points, 20)).toBe("M0 10 L10 0 L10 20 L0 20 Z");
  });

  it("returns an empty path for no points", () => {
    expect(linePath([])).toBe("");
    expect(areaPath([], 20)).toBe("");
  });

  it("rounds the data end of a bar and squares its baseline end", () => {
    const path = barPath(0, 0, 20, 50, 4);
    expect(path.startsWith("M0 50")).toBe(true);
    expect(path).toContain("Q");
  });

  it("draws nothing for a zero-value or zero-width bar", () => {
    expect(barPath(0, 0, 20, 0)).toBe("");
    expect(barPath(0, 0, 0, 20)).toBe("");
  });

  it("caps the corner radius on a bar shorter than the radius", () => {
    expect(barPath(0, 0, 20, 2, 4)).toContain("Q");
  });

  it("splits a full ring into two arcs so start and end never coincide", () => {
    const full = arcPath(50, 50, 40, 20, 0, 360);
    expect(full.match(/M/g)).toHaveLength(2);
  });

  it("flags the large-arc case past a half turn", () => {
    expect(arcPath(50, 50, 40, 20, 0, 200)).toContain("0 1 1");
    expect(arcPath(50, 50, 40, 20, 0, 90)).toContain("0 0 1");
  });

  it("puts angle 0 at twelve o'clock", () => {
    const [x, y] = polar(0, 0, 10, 0);
    expect(x).toBeCloseTo(0);
    expect(y).toBeCloseTo(-10);
  });
});

describe("splitAtProjection", () => {
  const toPoint = (index: number, value: number): Point => [index, value];

  it("keeps everything solid when there is no projection", () => {
    const { solid, dashed } = splitAtProjection([10, 20, 30], undefined, toPoint);
    expect(solid).toEqual([
      [
        [0, 10],
        [1, 20],
        [2, 30],
      ],
    ]);
    expect(dashed).toEqual([]);
  });

  it("splits solid from dashed at the given index, sharing the boundary point", () => {
    const { solid, dashed } = splitAtProjection([10, 20, 30, 40], 2, toPoint);
    expect(solid).toEqual([
      [
        [0, 10],
        [1, 20],
        [2, 30],
      ],
    ]);
    expect(dashed).toEqual([
      [
        [2, 30],
        [3, 40],
      ],
    ]);
  });

  it("dashes the whole run when dashedFrom is 0", () => {
    const { solid, dashed } = splitAtProjection([10, 20], 0, toPoint);
    expect(solid).toEqual([]);
    expect(dashed).toEqual([
      [
        [0, 10],
        [1, 20],
      ],
    ]);
  });

  it("leaves everything solid when dashedFrom is past the last index", () => {
    const { solid, dashed } = splitAtProjection([10, 20], 5, toPoint);
    expect(solid).toEqual([
      [
        [0, 10],
        [1, 20],
      ],
    ]);
    expect(dashed).toEqual([]);
  });

  it("still breaks on a null gap that straddles the projection boundary", () => {
    const { solid, dashed } = splitAtProjection([10, null, 30, 40], 2, toPoint);
    expect(solid).toEqual([[[0, 10]]]);
    expect(dashed).toEqual([
      [
        [2, 30],
        [3, 40],
      ],
    ]);
  });
});

describe("splitAtGaps", () => {
  const toPoint = (index: number, value: number): Point => [index, value];

  it("returns one run when there are no gaps", () => {
    expect(splitAtGaps([10, 20, 30], toPoint)).toEqual([
      [
        [0, 10],
        [1, 20],
        [2, 30],
      ],
    ]);
  });

  it("splits into two runs around a gap in the middle", () => {
    expect(splitAtGaps([10, null, 30], toPoint)).toEqual([[[0, 10]], [[2, 30]]]);
  });

  it("starts with no leading run when the first sample is a gap", () => {
    expect(splitAtGaps([null, 20, 30], toPoint)).toEqual([
      [
        [1, 20],
        [2, 30],
      ],
    ]);
  });

  it("drops a trailing gap without an empty trailing run", () => {
    expect(splitAtGaps([10, 20, null], toPoint)).toEqual([
      [
        [0, 10],
        [1, 20],
      ],
    ]);
  });

  it("returns no runs for an empty array", () => {
    expect(splitAtGaps([], toPoint)).toEqual([]);
  });
});

describe("formatters", () => {
  it("abbreviates large numbers", () => {
    expect(formatCompact(999)).toBe("999");
    expect(formatCompact(1240)).toBe("1.2k");
    expect(formatCompact(120_000)).toBe("120k");
    expect(formatCompact(4_500_000)).toBe("4.5M");
    expect(formatCompact(-2300)).toBe("-2.3k");
  });

  it("keeps sub-unit values readable instead of rounding them to zero", () => {
    expect(formatCompact(0.042)).toBe("0.042");
    expect(formatCompact(0)).toBe("0");
  });

  it("marks a non-finite value rather than printing NaN", () => {
    expect(formatCompact(Number.NaN)).toBe("–");
    expect(formatDuration(Number.POSITIVE_INFINITY)).toBe("–");
    expect(formatPercent(Number.NaN)).toBe("–");
  });

  it("scales durations by magnitude", () => {
    expect(formatDuration(0.5)).toBe("0.5ms");
    expect(formatDuration(940)).toBe("940ms");
    expect(formatDuration(1250)).toBe("1.25s");
    expect(formatDuration(90_000)).toBe("1.5min");
  });

  it("keeps uptime precision", () => {
    expect(formatPercent(99.982, 2)).toBe("99.98%");
    expect(formatPercent(50)).toBe("50%");
  });
});

// These assert DELEGATION to Intl rather than literal strings, so they hold
// whatever locale the runtime is in — which is the whole point. A literal
// "1.2k" expectation would pass on an en-US CI runner and pass again if the
// delegation were reverted.
describe("locale-aware number formatting", () => {
  const num = (v: number, maximumFractionDigits: number) =>
    new Intl.NumberFormat(undefined, { maximumFractionDigits, useGrouping: false }).format(v);

  it("formatCompact writes its number in the viewer's locale, keeping our unit", () => {
    expect(formatCompact(1_240)).toBe(`${num(1.24, 1)}k`);
    expect(formatCompact(1_240_000)).toBe(`${num(1.24, 1)}M`);
    expect(formatCompact(-1_240)).toBe(`-${num(1.24, 1)}k`);
  });

  it("formatCompact keeps two significant decimals below 1", () => {
    expect(formatCompact(0.0123)).toBe(num(0.012, 3));
  });

  it("keeps a precision Intl's own compact notation cannot give it", () => {
    // notation:"compact" applies one maximumFractionDigits AFTER compacting,
    // so the value that keeps 1240 reading as "1.2k" rather than "1.24k" is
    // the same value that flattens 0.0123 to "0". This library needs both,
    // which is why it compacts itself and delegates only the number.
    const compact = new Intl.NumberFormat(undefined, {
      notation: "compact",
      maximumFractionDigits: 1,
    });
    expect(compact.format(0.0123)).toBe(num(0, 0));
    expect(formatCompact(0.0123)).not.toBe(compact.format(0.0123));
  });

  it("formatDuration writes its number in the viewer's locale", () => {
    expect(formatDuration(1_250)).toBe(`${num(1.25, 2)}s`);
    expect(formatDuration(940)).toBe(`${num(940, 0)}ms`);
    expect(formatDuration(210_000)).toBe(`${num(3.5, 1)}min`);
  });

  it("formatPercent delegates the whole value, so the locale's spacing applies too", () => {
    const pct = (v: number, maximumFractionDigits: number) =>
      new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits }).format(v / 100);
    expect(formatPercent(5.5)).toBe(pct(5.5, 1));
    expect(formatPercent(99.982, 3)).toBe(pct(99.982, 3));
    expect(formatPercent(50, 0)).toBe(pct(50, 0));
  });

  it("formatPercent's default rounds an SLO figure, as its doc now says", () => {
    // Pinned because the previous doc comment claimed 99.982 -> "99.982%" for
    // the default, which was never true: decimals defaults to 1.
    expect(formatPercent(99.982)).toBe(formatPercent(100));
    expect(formatPercent(99.982, 2)).not.toBe(formatPercent(100, 2));
  });

  it("no formatter emits a hardcoded ASCII decimal point", () => {
    // The separator must come from Intl, so it must match Intl's for the same
    // number — a hardcoded "." passes in en-US and fails everywhere else.
    const sep = num(1.1, 1).replace(/1/g, "");
    expect(formatCompact(1_100)).toBe(`1${sep}1k`);
    expect(formatDuration(1_100)).toBe(`1${sep}1s`);
  });
});

describe("formatActiveReading", () => {
  const labels = ["12:00", "13:00"];
  const series = [
    { name: "us-east", values: [120, 180] },
    { name: "eu-west", values: [90, null] },
  ];

  it("returns an empty string when nothing is active", () => {
    expect(formatActiveReading(labels, series, null, () => formatCompact)).toBe("");
  });

  it("names the category, then every series' reading, in order", () => {
    expect(formatActiveReading(labels, series, 0, () => formatCompact)).toBe(
      "12:00: us-east 120, eu-west 90"
    );
  });

  it("speaks a null or undefined sample as the no-data label instead of formatting it", () => {
    expect(formatActiveReading(labels, series, 1, () => formatCompact)).toBe(
      "13:00: us-east 180, eu-west no data"
    );
  });

  it("lets the no-data label be overridden", () => {
    expect(formatActiveReading(labels, series, 1, () => formatCompact, "sem dados")).toBe(
      "13:00: us-east 180, eu-west sem dados"
    );
  });

  it("picks the formatter per series, for a chart mixing two value kinds", () => {
    const mixed = [
      { name: "Requests", values: [1200] },
      { name: "p99 latency", values: [340] },
    ];
    const formatFor = (s: (typeof mixed)[number]) =>
      s.name === "Requests" ? formatCompact : (v: number) => `${v}ms`;
    expect(formatActiveReading(["now"], mixed, 0, formatFor)).toBe(
      "now: Requests 1.2k, p99 latency 340ms"
    );
  });
});
