---
"@marcfs31/forsight": minor
---

New server-safe entry `@marcfs31/forsight/chart`: the pure chart maths and
number formatters (`arcPath`, `areaPath`, `barPath`, `clamp`, `formatCompact`,
`formatDuration`, `formatPercent`, `linePath`, `niceScale`, `polar`,
`project`, `seriesBg`, `seriesFill`, `seriesStroke`, `SERIES_SLOTS`,
`splitAtGaps`, `splitAtProjection`, `ANNOTATION_TONE_CLASSES`) that back the
chart components.

Previously this maths was reachable only through the components entry, and
that entire entry ships a leading `"use client"` directive — so a React
Server Component that imported `clamp` or `formatCompact` to precompute a
mark or format a value server-side failed to build. The README already
called the package "RSC-ready", which this contradicted for anything that
touched chart maths outside a component.

The new entry mirrors `@marcfs31/forsight/theme`: no client directive, and
`publint`/`arethetypeswrong` validate it clean on every resolution mode,
including legacy `node10` (via a root `chart/package.json` stub, same as
`theme/`). Nothing moved — `clamp`, `niceScale`, and the rest are still
exported from the components entry too, for chart components and consumers
that already import them from there.
