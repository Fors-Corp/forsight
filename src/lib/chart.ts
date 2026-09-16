/**
 * Geometry, scales and formatting shared by every chart component.
 *
 * Everything here is pure — no React, no DOM — so the maths that decides where
 * a mark lands is unit-testable on its own, and the components stay thin
 * renderers over it.
 */

/** Number of categorical series slots the token palette defines. */
export const SERIES_SLOTS = 8;

// Static class strings (never interpolated) so Tailwind's content scanner can
// see every utility this library can emit.
const SERIES_FILL = [
  "fill-viz-1",
  "fill-viz-2",
  "fill-viz-3",
  "fill-viz-4",
  "fill-viz-5",
  "fill-viz-6",
  "fill-viz-7",
  "fill-viz-8",
] as const;

const SERIES_STROKE = [
  "stroke-viz-1",
  "stroke-viz-2",
  "stroke-viz-3",
  "stroke-viz-4",
  "stroke-viz-5",
  "stroke-viz-6",
  "stroke-viz-7",
  "stroke-viz-8",
] as const;

const SERIES_BG = [
  "bg-viz-1",
  "bg-viz-2",
  "bg-viz-3",
  "bg-viz-4",
  "bg-viz-5",
  "bg-viz-6",
  "bg-viz-7",
  "bg-viz-8",
] as const;

/**
 * Series colors are assigned in fixed slot order and never cycled: the slot
 * ordering is what keeps adjacent series distinguishable for colorblind
 * readers, and a repeated hue would make two series look like one. Past the
 * eighth slot the mark goes neutral — the "Other" bucket — which is the
 * signal to the caller that the remaining series should be aggregated or
 * split into small multiples.
 */
export function seriesFill(index: number): string {
  return SERIES_FILL[index] ?? "fill-fg-muted";
}

export function seriesStroke(index: number): string {
  return SERIES_STROKE[index] ?? "stroke-fg-muted";
}

/** Swatch color for legends and HTML (non-SVG) marks like BarList rows. */
export function seriesBg(index: number): string {
  return SERIES_BG[index] ?? "bg-fg-muted";
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

export interface NiceScale {
  min: number;
  max: number;
  ticks: number[];
}

/**
 * A reference line drawn over `LineChart`/`BarChart` — an SLO threshold, a
 * deploy marker. Pass exactly one of `value` (a horizontal line at that
 * value on the value axis) or `label` (a vertical line at that x-axis
 * category, which must match one of the chart's own `labels` — an
 * unmatched label is silently skipped). `text` is always shown next to the
 * line and folded into the chart's visually hidden description, so the
 * threshold/marker is never sighted-only information.
 */
export interface ChartAnnotation {
  value?: number;
  label?: string;
  text: string;
  /** Defaults to `"neutral"`. */
  tone?: "neutral" | "accent" | "warning" | "danger";
}

export const ANNOTATION_TONE_CLASSES: Record<
  NonNullable<ChartAnnotation["tone"]>,
  { stroke: string; text: string }
> = {
  neutral: { stroke: "stroke-fg-muted", text: "fill-fg-muted" },
  accent: { stroke: "stroke-accent", text: "fill-accent" },
  warning: { stroke: "stroke-warning", text: "fill-warning" },
  danger: { stroke: "stroke-danger", text: "fill-danger" },
};

/** Rounds `raw` up to the nearest 1/2/5 × 10ⁿ — the steps that read as "round". */
function niceStep(raw: number): number {
  const magnitude = 10 ** Math.floor(Math.log10(raw));
  const normalized = raw / magnitude;
  const step = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
  return step * magnitude;
}

/**
 * Axis domain rounded out to human-readable tick values.
 *
 * `min`/`max` are the data extent; the returned domain is widened to the
 * enclosing round numbers so gridlines land on values a reader can name. A
 * flat series (every value identical) still gets a band around it rather than
 * a zero-height domain that would divide by zero downstream.
 */
export function niceScale(min: number, max: number, tickCount = 4): NiceScale {
  const safeMin = Number.isFinite(min) ? min : 0;
  const safeMax = Number.isFinite(max) ? max : 0;
  let lo = Math.min(safeMin, safeMax);
  let hi = Math.max(safeMin, safeMax);

  if (lo === hi) {
    // Flat data: show it against a plausible range instead of a single line
    // pinned to the top of an empty plot.
    if (lo === 0) {
      hi = 1;
    } else if (lo > 0) {
      lo = 0;
    } else {
      hi = 0;
    }
  }

  const step = niceStep((hi - lo) / Math.max(1, tickCount));
  const niceMin = Math.floor(lo / step) * step;
  const niceMax = Math.ceil(hi / step) * step;

  const ticks: number[] = [];
  // Accumulate in step units, not by repeated addition, so float drift can't
  // push the last tick past niceMax and drop it.
  const stepCount = Math.round((niceMax - niceMin) / step);
  for (let i = 0; i <= stepCount; i++) {
    ticks.push(roundToStep(niceMin + i * step, step));
  }

  return { min: niceMin, max: niceMax, ticks };
}

/** Kills binary-float noise (0.30000000000000004) at the tick's own precision. */
function roundToStep(value: number, step: number): number {
  const decimals = Math.max(0, -Math.floor(Math.log10(step)));
  return Number(value.toFixed(Math.min(decimals + 1, 10)));
}

/** Maps a data value onto a pixel position within `[start, start + size]`. */
export function project(value: number, domainMin: number, domainMax: number, size: number): number {
  const span = domainMax - domainMin;
  if (span === 0) return 0;
  return ((value - domainMin) / span) * size;
}

export type Point = readonly [x: number, y: number];

/** `M`/`L` polyline through the points; `""` for an empty set. */
export function linePath(points: readonly Point[]): string {
  if (points.length === 0) return "";
  return points.map(([x, y], i) => `${i === 0 ? "M" : "L"}${round(x)} ${round(y)}`).join(" ");
}

/**
 * Splits a `null`-gapped value series into the runs of consecutive
 * non-null points to draw — a missing sample should leave a gap, not be
 * interpolated through or treated as zero. Shared by `LineChart` and
 * `ComboChart`'s line series.
 */
export function splitAtGaps(
  values: ReadonlyArray<number | null>,
  toPoint: (index: number, value: number) => Point
): Point[][] {
  const segments: Point[][] = [];
  let current: Point[] = [];
  values.forEach((value, index) => {
    if (value === null) {
      if (current.length > 0) segments.push(current);
      current = [];
      return;
    }
    current.push(toPoint(index, value));
  });
  if (current.length > 0) segments.push(current);
  return segments;
}

/**
 * Splits a value series into the solid run(s) up to and including
 * `dashedFrom` and the dashed run(s) from that same point onward — the
 * shape (not color) that marks a projection past the last real
 * measurement. The boundary point is shared by both runs so the stroke has
 * no gap where it switches. Null-gap handling is the same as
 * `splitAtGaps`: a missing sample still breaks the line rather than being
 * interpolated through. `dashedFrom` is optional — omitted, every point
 * comes back solid and `dashed` is empty, so a series with no projection
 * renders exactly as `splitAtGaps` would draw it.
 */
export function splitAtProjection(
  values: ReadonlyArray<number | null>,
  dashedFrom: number | undefined,
  toPoint: (index: number, value: number) => Point
): { solid: Point[][]; dashed: Point[][] } {
  const solid: Point[][] = [];
  const dashed: Point[][] = [];
  let current: Point[] = [];
  let currentDashed = false;

  const flush = () => {
    if (current.length === 0) return;
    (currentDashed ? dashed : solid).push(current);
  };

  values.forEach((value, index) => {
    if (value === null) {
      flush();
      current = [];
      currentDashed = false;
      return;
    }
    const isDashed = dashedFrom !== undefined && index >= dashedFrom;
    if (current.length > 0 && isDashed !== currentDashed) {
      // Crossing from solid to dashed mid-run: carry the boundary point onto
      // both runs so the stroke stays continuous where it switches.
      current.push(toPoint(index, value));
      flush();
      current = [toPoint(index, value)];
    } else {
      current.push(toPoint(index, value));
    }
    currentDashed = isDashed;
  });
  flush();

  return { solid, dashed };
}

/** The same polyline closed down to `baselineY`, for an area fill. */
export function areaPath(points: readonly Point[], baselineY: number): string {
  if (points.length === 0) return "";
  const first = points[0];
  const last = points[points.length - 1];
  return `${linePath(points)} L${round(last[0])} ${round(baselineY)} L${round(first[0])} ${round(
    baselineY
  )} Z`;
}

/**
 * Closed band between an `upper` and `lower` bound sharing the same x
 * positions — `upper` left to right, then `lower` right to left back to the
 * start. For a confidence interval around a projection (a Holt band, an
 * mlaas forecast with error bounds); only meaningful once a producer sends
 * bounds alongside its points, which none does as of this writing — mlaas's
 * `ForecastPoint` carries a value and no bounds, and Forseer's Holt band
 * lives in the `ErrorBudget` caption, not as plottable points. Kept here,
 * tested on its own, for whichever one does. Mismatched-length inputs are
 * zipped to the shorter one rather than throwing.
 */
export function bandPath(upper: readonly Point[], lower: readonly Point[]): string {
  if (upper.length === 0 || lower.length === 0) return "";
  const n = Math.min(upper.length, lower.length);
  const top = upper.slice(0, n);
  const bottom = lower.slice(0, n);
  const down = top.map(([x, y], i) => `${i === 0 ? "M" : "L"}${round(x)} ${round(y)}`).join(" ");
  const back = [...bottom]
    .reverse()
    .map(([x, y]) => `L${round(x)} ${round(y)}`)
    .join(" ");
  return `${down} ${back} Z`;
}

/**
 * Bar with rounded corners at the data end only — the baseline end stays square
 * so bars sit flush on the axis and a stacked segment meets its neighbour flat.
 * `height` may be 0 (a zero-value bar draws nothing).
 */
export function barPath(x: number, y: number, width: number, height: number, radius = 4): string {
  if (height <= 0 || width <= 0) return "";
  const r = Math.min(radius, width / 2, height);
  return [
    `M${round(x)} ${round(y + height)}`,
    `L${round(x)} ${round(y + r)}`,
    `Q${round(x)} ${round(y)} ${round(x + r)} ${round(y)}`,
    `L${round(x + width - r)} ${round(y)}`,
    `Q${round(x + width)} ${round(y)} ${round(x + width)} ${round(y + r)}`,
    `L${round(x + width)} ${round(y + height)}`,
    "Z",
  ].join(" ");
}

/** Arc segment of a ring, used by DonutChart and Gauge. Angles in degrees, 0 = 12 o'clock. */
export function arcPath(
  cx: number,
  cy: number,
  outerRadius: number,
  innerRadius: number,
  startAngle: number,
  endAngle: number
): string {
  const sweep = endAngle - startAngle;
  // A full ring can't be drawn as one arc (start and end coincide), so it is
  // split into two half sweeps.
  if (sweep >= 360) {
    return `${arcPath(cx, cy, outerRadius, innerRadius, startAngle, startAngle + 180)} ${arcPath(
      cx,
      cy,
      outerRadius,
      innerRadius,
      startAngle + 180,
      startAngle + 360
    )}`;
  }
  const largeArc = sweep > 180 ? 1 : 0;
  const [ox1, oy1] = polar(cx, cy, outerRadius, startAngle);
  const [ox2, oy2] = polar(cx, cy, outerRadius, endAngle);
  const [ix1, iy1] = polar(cx, cy, innerRadius, endAngle);
  const [ix2, iy2] = polar(cx, cy, innerRadius, startAngle);
  return [
    `M${round(ox1)} ${round(oy1)}`,
    `A${round(outerRadius)} ${round(outerRadius)} 0 ${largeArc} 1 ${round(ox2)} ${round(oy2)}`,
    `L${round(ix1)} ${round(iy1)}`,
    `A${round(innerRadius)} ${round(innerRadius)} 0 ${largeArc} 0 ${round(ix2)} ${round(iy2)}`,
    "Z",
  ].join(" ");
}

export function polar(cx: number, cy: number, radius: number, angle: number): Point {
  const radians = ((angle - 90) * Math.PI) / 180;
  return [cx + radius * Math.cos(radians), cy + radius * Math.sin(radians)];
}

function round(n: number): number {
  return Math.round(n * 100) / 100;
}

const COMPACT_UNITS = ["", "k", "M", "B", "T"] as const;

/**
 * Axis-and-tile number format: 1_240 → "1.2k". Keeps two significant decimals
 * below 1 so sub-unit metrics (error rates, seconds) don't collapse to "0".
 */
export function formatCompact(value: number): string {
  if (!Number.isFinite(value)) return "–";
  const sign = value < 0 ? "-" : "";
  let n = Math.abs(value);
  if (n < 1 && n > 0) return `${sign}${Number(n.toPrecision(2))}`;
  let unit = 0;
  while (n >= 1000 && unit < COMPACT_UNITS.length - 1) {
    n /= 1000;
    unit++;
  }
  const rounded = n >= 100 || unit === 0 ? Math.round(n) : Number(n.toFixed(1));
  return `${sign}${rounded}${COMPACT_UNITS[unit]}`;
}

/** Span format for traces: 940 → "940ms", 1_250 → "1.25s". */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms)) return "–";
  if (ms < 1) return `${Number(ms.toFixed(2))}ms`;
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60_000) return `${Number((ms / 1000).toFixed(2))}s`;
  return `${Number((ms / 60_000).toFixed(1))}min`;
}

/** Percentage with the precision uptime numbers actually need: 99.982 → "99.982%". */
export function formatPercent(value: number, decimals = 1): string {
  if (!Number.isFinite(value)) return "–";
  return `${Number(value.toFixed(decimals))}%`;
}
