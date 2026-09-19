---
"@fors-corp/forsight": patch
---

Memoize the plotted charts' geometry so the cursor doesn't recompute it on every move.

`LineChart`, `BarChart`, `ComboChart`, `DonutChart` and `Sparkline` recomputed
their scale (an extent scan plus `niceScale`), and `LineChart`/`ComboChart`
their per-series solid/dashed or gapped point runs, on every render — including
the renders their own hover/keyboard cursor triggers many times a second,
where none of that geometry had actually changed. Each component is now
wrapped in `React.memo` (so an unrelated parent re-render with unchanged props
is skipped entirely), and the geometry itself is computed once via `useMemo`,
keyed on the props that actually determine it (`series`/`labels`/`stacked` for
the scale, `series` alone for the per-series runs) rather than on the cursor's
`active` index. No rendered output changes — every existing DOM assertion and
snapshot passes unchanged.
