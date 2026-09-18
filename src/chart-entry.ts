// Server-safe entry: "@marcfs31/forsight/chart". No "use client" — this is
// the pure chart maths and number formatting that backs every chart
// component (geometry, scales, path builders, `formatCompact` and friends),
// callable from a React Server Component to precompute marks or format a
// value server-side. The component bundle ("@marcfs31/forsight") is a
// separate client entry.

export {
  ANNOTATION_TONE_CLASSES,
  arcPath,
  areaPath,
  barPath,
  clamp,
  formatCompact,
  formatDuration,
  formatPercent,
  linePath,
  niceScale,
  polar,
  project,
  seriesBg,
  seriesFill,
  seriesStroke,
  SERIES_SLOTS,
  splitAtGaps,
  splitAtProjection,
  type ChartAnnotation,
  type NiceScale,
  type Point,
} from "./lib/chart";
