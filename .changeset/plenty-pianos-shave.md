---
"@marcfs31/fors-observability-design-system": minor
---

Make the focus ring meet WCAG 1.4.11, so keyboard focus is actually visible.

`--forsight-focus-ring` was the brand teal at 45% alpha in both themes. A
`box-shadow` is painted outside the control, against the page behind it, so
that alpha composited straight onto the ink surfaces and left the ring at
2.67:1 on dark `bg`/`surface` and 2.58:1 on `surface-2`, and 2.03/2.07/1.99:1
in light — all under the 3:1 floor a non-text indicator has to clear. Roughly
thirty components carry `focus-visible:shadow-focus-ring` as their only focus
affordance, so on every one of them the sole marker of keyboard focus was too
faint to see.

The ring is now the solid accent — `#16c7b0` dark, `#0b6c5e` light, both
already on the brand ramp — which measures 9.00/8.36/7.52:1 and
5.99/6.32/5.62:1 against `bg`/`surface`/`surface-2`. `shadow-focus-ring` keeps
its 3px footprint but spends the innermost pixel on a hairline of
`--forsight-ink-bg`, so a solid teal ring around an accent-filled control
(primary `Button`, a checked `Switch` or `Checkbox`) is still separated from
the fill instead of merging into it.

`ForsightPalette` gained `focusRing` and `focusRingAlpha`, and
`contrast.test.ts` now blends the ring at its declared alpha and asserts 3:1
against all three surfaces in both themes — the check that could not see this
token before.
