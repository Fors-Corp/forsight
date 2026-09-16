---
"@marcfs31/forsight": minor
---

Give `LineChart` a way to draw a projection as a projection.

A series' new `dashedFrom` index marks the last point drawn solid — the
observed/forecast boundary. The stroke from that point onward switches to
dashed instead of a second color, so a forecast reads the same in grayscale
and to colorblind viewers, as CONTRIBUTING's data-viz rule requires. The
boundary point is shared by both runs, so the line has no gap where it
switches, and both runs still plot on the same y-axis and domain — a
projection is more of the same series, not a second one. The distinction is
never sighted-only: a series with `dashedFrom` set adds "`<name>` is
projected from `<label>`." to `ChartFrame`'s hidden description.

`chart.ts` also gains `bandPath(upper, lower)`, a closed path between an
upper and lower bound, for whenever a forecast producer starts sending
bounds alongside its points — neither mlaas's `ForecastPoint` nor Forseer's
Holt band does yet, so nothing in this package draws one today.
