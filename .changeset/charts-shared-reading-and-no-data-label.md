---
"@fors-corp/forsight": minor
---

Share the chart cursor's screen-reader summary and legend guard, cover
`splitAtGaps` with unit tests, and let consumers localize "no data".

`LineChart`, `BarChart` and `ComboChart` each built their own copy of the
`role="status"` sentence the cursor announces on focus/hover, and each
repeated the same `series.length > 1 ? <ChartLegend /> : null` guard. Those
are now `formatActiveReading` and `seriesLegendItems` in `src/lib/chart.ts`,
called from all three — `BarChart`'s copy also silently formatted a `null`
sample as `0` instead of announcing it as missing, unlike its own data table
and unlike the other two charts' announcements; it now matches them.

`splitAtGaps` — the null-gap splitter `ComboChart`'s line series has used
since it shipped — had no unit tests of its own, unlike `splitAtProjection`.

New `noDataLabel?: string` prop on `LineChart`, `BarChart`, `ComboChart` and
`Heatmap` (default `"no data"`, threaded the way `valueFormat` already is)
lets a consumer translate the literal that appears in the cursor readout,
tooltip, data table and (for `Heatmap`) each cell's title and accessible
name.
