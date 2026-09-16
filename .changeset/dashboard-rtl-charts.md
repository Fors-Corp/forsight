---
"@marcfs31/forsight": patch
---

Fix the chart family's RTL rendering: axis-value labels no longer land on
top of the plot, the keyboard-cursor tooltip no longer covers the point it
is reading, and `TraceWaterfall` no longer reads a trace backwards.

SVG resolves `text-anchor="start"/"end"` against the inherited CSS
`direction`, not a fixed physical side, so under `dir="rtl"` every
`LineChart`/`BarChart` axis-value and annotation label flipped onto the
wrong edge — `ChartFrame`'s `<svg>` now pins its own direction to `ltr`,
matching the "time flows left-to-right in both text directions" contract
its own comment already stated. The two charts' keyboard-cursor tooltip
used a logical `start-2`/`end-2` class to park on the side opposite the
active point, which is a physical question (the plot never mirrors under
RTL) — now `left-2`/`right-2`. `TraceWaterfall`'s span bars used the
logical `insetInlineStart`, which mirrored the whole waterfall under RTL —
now the physical `left`, so a span's position always reads its wall-clock
offset left-to-right.

Caught by folding the whole chart family — `LineChart` (x2), `BarChart`,
`DonutChart`, `BarList`, `Heatmap`, `TraceWaterfall`, and `Sparkline` (via
`StatCard`) — into the `Forsight/Overview` → `DashboardRTL` story, which
already composed `Dashboard`'s exact tree under `dir="rtl"`; the gap was
that nothing there checked rendered geometry. `DashboardRTL`'s `play` now
does, verified by real `getBoundingClientRect` position per
`Switch.stories.tsx`'s "RTL" story pattern, not `toHaveClass`.
