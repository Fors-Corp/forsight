/**
 * Whether `href` is safe to render as a clickable link.
 *
 * Every `href` a consumer hands to this design system's link-rendering
 * components (`BarList`'s row `href`, `BreadcrumbLink`, `SidebarNavItem`) is
 * documented as a drill-down built from request-derived data — a route, a
 * service name, a tag, an error code echoed back from a trace — so it has to
 * be treated as untrusted input, not a value the caller already sanitized.
 * Without a check, an attacker who can influence that upstream data can hand
 * an on-call operator's authenticated session a `javascript:` or
 * `data:text/html` URI that runs when they click a "drill down" link.
 *
 * This allows only what a navigation link needs: a relative URL (resolved
 * against the current page), an absolute `http:`/`https:` URL, or a
 * protocol-relative URL (`//host/path`, which inherits the current page's
 * scheme). Every other scheme — `javascript:`, `data:`, `vbscript:`, `file:`,
 * and anything else — is rejected, along with any string `new URL` can't
 * parse at all.
 *
 * Resolving a relative href needs a base, and the current page's origin is
 * the honest one — but every component that calls this sits inside the
 * package's `"use client"` boundary, which Next.js still renders on the
 * server, where there is no `location` to read. Reading it unguarded turned
 * the reference into a `ReferenceError` that this `catch` swallowed, so
 * during SSR *every* href came back unsafe, safe ones included: a `BarList`
 * row served as a `<div>` and rehydrated into an `<a>`, a breadcrumb and a
 * nav item served with no `href` at all. The verdict never actually depends
 * on which origin is used — only the resolved scheme is inspected, and a
 * protocol-relative href resolves to `http:` or `https:` against either — so
 * a fixed `https:` base stands in wherever there is no document, and server
 * and client agree on every input.
 */
const SSR_BASE = "https://forsight.invalid";

export function isSafeHref(href: string): boolean {
  try {
    const url = new URL(href, typeof location === "undefined" ? SSR_BASE : location.origin);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}
