---
"@marcfs31/forsight": patch
---

`BarList`, `BreadcrumbLink` and `SidebarNavItem` now refuse to render an
unsafe `href` as a clickable link. All three accept an `href` documented as
a drill-down or nav destination — data that in practice traces back to a
route, service name, tag or error code echoed from request input, so a
consumer that renders one of these components with attacker-influenced data
was one `javascript:` or `data:text/html` URI away from running script in an
on-call operator's authenticated session when they clicked what looked like
a normal row or link.

A new `isSafeHref` helper (not exported from the package root; internal to
these three components) parses the href against the current page's origin
and allows only relative URLs and absolute `http:`/`https:` URLs — a
protocol-relative URL (`//host/path`) is allowed too, since it resolves to
whichever of those two the current page is served over. Anything else
(`javascript:`, `data:`, `vbscript:`, an unparseable string) falls back to
the same non-link rendering each component already uses when no `href` is
given at all: `BarList` renders its existing plain `<div>` row instead of an
`<a>`, and `BreadcrumbLink`/`SidebarNavItem` render an `<a>` with no `href`
attribute (which, per the HTML spec, has no `link` role and isn't focusable
— not a link, the same as `BarList`'s fallback).
