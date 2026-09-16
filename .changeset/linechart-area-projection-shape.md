---
"@marcfs31/forsight": patch
---

Make `LineChart`'s area fill respect `dashedFrom` too.

Previously a series with both `area` and `dashedFrom` filled the whole run
the same way, so the projected tail read as a second solid measurement
under its own dashed stroke. The fill now splits at the same boundary as
the stroke: the observed run keeps the plain area tint, and the projected
run's fill drops to half that opacity and is overlaid with a diagonal hatch
— shape, not color or opacity alone, carries the distinction, as
CONTRIBUTING.md's data-viz rule requires. The hatch is one `<pattern>`
per chart, referenced by every series' projected fill, not redeclared per
series or per segment.
