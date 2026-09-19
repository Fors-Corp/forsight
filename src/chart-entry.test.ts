import { describe, expect, it } from "vitest";
import * as ChartEntry from "./chart-entry";

// Every chart component's own test imports straight from "./lib/chart", so
// nothing ever exercises this file's own re-export statement — it sat at 0%
// statement coverage even though the underlying implementations are well
// tested. This asserts the public "@marcfs31/forsight/chart" surface (the
// server-safe entry a consumer actually imports) still carries everything
// lib/chart.ts documents, so a re-export silently dropped from the list here
// would fail a test instead of just shrinking the published API.
describe("chart-entry", () => {
  it("re-exports every documented chart utility", () => {
    expect(typeof ChartEntry.formatCompact).toBe("function");
    expect(typeof ChartEntry.formatDuration).toBe("function");
    expect(typeof ChartEntry.formatPercent).toBe("function");
    expect(typeof ChartEntry.clamp).toBe("function");
    expect(typeof ChartEntry.arcPath).toBe("function");
    expect(typeof ChartEntry.areaPath).toBe("function");
    expect(typeof ChartEntry.barPath).toBe("function");
    expect(typeof ChartEntry.linePath).toBe("function");
    expect(typeof ChartEntry.niceScale).toBe("function");
    expect(typeof ChartEntry.polar).toBe("function");
    expect(typeof ChartEntry.project).toBe("function");
    expect(typeof ChartEntry.seriesBg).toBe("function");
    expect(typeof ChartEntry.seriesFill).toBe("function");
    expect(typeof ChartEntry.seriesStroke).toBe("function");
    expect(typeof ChartEntry.splitAtGaps).toBe("function");
    expect(typeof ChartEntry.splitAtProjection).toBe("function");
    expect(ChartEntry.SERIES_SLOTS).toBe(8);
    expect(typeof ChartEntry.ANNOTATION_TONE_CLASSES).toBe("object");
  });

  it("re-exported clamp behaves like the real implementation", () => {
    expect(ChartEntry.clamp(150, 0, 100)).toBe(100);
    expect(ChartEntry.clamp(-5, 0, 100)).toBe(0);
    expect(ChartEntry.clamp(42, 0, 100)).toBe(42);
  });
});
