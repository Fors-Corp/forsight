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
 */
export function isSafeHref(href: string): boolean {
  try {
    const url = new URL(href, location.origin);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}
