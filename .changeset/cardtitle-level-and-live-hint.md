---
"@marcfs31/forsight": minor
---

`CardTitle` can now pick its heading level, and `Input`'s hint announces itself once it becomes an error.

`CardTitle` always rendered an `<h3>`, so any page where it needed to be the
top (or second) heading — the dashboard's every section does this — skipped
straight from `h1` to `h3`. It now takes an `as` prop (`"h1"`–`"h6"`, same
convention as `Heading`'s), defaulting to `h3` — nothing changes unless you
pass it.

`Input`'s hint was linked to the field only through `aria-describedby`, so a
hint that turns into an error message after submit — the common validation
pattern, used by the dashboard's Ask Forseer flow and `AuthTokenDialog` —
was never announced to a screen-reader user who had already tabbed past the
field (WCAG 4.1.3). The hint now carries `aria-live="polite"` while
`invalid` is set, and is silent otherwise so it doesn't announce every
keystroke-driven hint change (e.g. a character counter).
