---
"@marcfs31/forsight": patch
---

Fix the keyboard-cursor tooltip's side under `dir="rtl"` in `ComboChart`,
`BoxPlot` and `Histogram`, so it no longer covers the slot it is reading.

The three charts parked the readout on the side opposite the active slot
with a logical `start-2`/`end-2` class, but a slot's x position is a
physical pixel — the plot never mirrors under RTL (see `ChartFrame`) — so
under `dir="rtl"` the class resolved to the same side as the mark it was
meant to avoid. Now the physical `left-2`/`right-2`, the same fix
`LineChart` and `BarChart` got from the `DashboardRTL` coverage; these
three were flagged there as the follow-up because `Dashboard` does not
compose them, so no story exercised them under RTL. Each now has an "RTL"
story that renders the chart under both directions, drives the cursor to
each end of the axis, and checks the tooltip's rendered position against
the active mark (`getBoundingClientRect`, not `toHaveClass`), per
`Switch.stories.tsx`'s "RTL" story pattern.
