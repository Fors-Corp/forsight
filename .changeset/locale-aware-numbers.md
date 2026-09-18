---
"@marcfs31/forsight": minor
---

Number formatting follows the viewer's locale.

`formatCompact`, `formatDuration` and `formatPercent` — the defaults behind
Gauge, BarList, Heatmap, CalendarHeatmap, Delta and every chart axis and
tooltip — wrote their numbers with a hardcoded ASCII decimal point, so a
German or French reader saw `1.2k`, `940ms` and `5.5%` where their locale
wants `1,2k`, `940ms` and `5,5 %`. They now delegate to `Intl.NumberFormat`,
matching the `Intl.DateTimeFormat` the date code already used.

English output is unchanged, deliberately: the unit suffixes stay the
library's own rather than `Intl`'s `notation: "compact"`, whose single
`maximumFractionDigits` cannot keep `1240` short and `0.0123` precise at the
same time, whose suffixes are locale data (German renders no `k` at all), and
which renders `1.2K` in English.

`CalendarHeatmap`'s weekday row headers were a hardcoded English array one
line above a correct `Intl.DateTimeFormat` for its month labels; they are now
computed too.

`formatPercent`'s doc comment claimed `99.982 → "99.982%"`, which was never
true of its default `decimals = 1` — that returns `"100%"`. The behaviour is
unchanged; the comment now says so and points at `UptimeBar`, which passes
`2` for exactly this reason.
