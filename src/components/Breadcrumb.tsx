import * as React from "react";
import { cn } from "../lib/cn";
import { isSafeHref } from "../lib/safe-href";

export function Breadcrumb({ className, ...props }: React.ComponentPropsWithoutRef<"nav">) {
  return <nav aria-label="Breadcrumb" className={cn(className)} {...props} />;
}

export const BreadcrumbList = React.forwardRef<
  HTMLOListElement,
  React.OlHTMLAttributes<HTMLOListElement>
>(({ className, ...props }, ref) => (
  <ol
    ref={ref}
    className={cn(
      "flex flex-wrap items-center gap-1.5 font-sans text-sm text-fg-secondary",
      className
    )}
    {...props}
  />
));
BreadcrumbList.displayName = "BreadcrumbList";

export const BreadcrumbItem = React.forwardRef<
  HTMLLIElement,
  React.LiHTMLAttributes<HTMLLIElement>
>(({ className, ...props }, ref) => (
  <li ref={ref} className={cn("flex items-center gap-1.5", className)} {...props} />
));
BreadcrumbItem.displayName = "BreadcrumbItem";

/**
 * A crumb that links back up the hierarchy. `href` is typically built from
 * route/dimension data that traces back to request input, so it is treated
 * as untrusted: an unsafe scheme (`javascript:`, `data:`, ...) is never set
 * on the rendered element (`isSafeHref`, `../lib/safe-href`) — an `<a>`
 * without an `href` has no `link` role and isn't focusable, the same
 * non-link fallback `BarList` and `SidebarNavItem` use for the same case.
 */
export const BreadcrumbLink = React.forwardRef<
  HTMLAnchorElement,
  React.AnchorHTMLAttributes<HTMLAnchorElement>
>(({ className, href, ...props }, ref) => (
  <a
    ref={ref}
    className={cn("transition-colors duration-base hover:text-accent", className)}
    href={href !== undefined && isSafeHref(href) ? href : undefined}
    {...props}
  />
));
BreadcrumbLink.displayName = "BreadcrumbLink";

/** The final, non-clickable crumb — the current page. */
export const BreadcrumbPage = React.forwardRef<
  HTMLSpanElement,
  React.HTMLAttributes<HTMLSpanElement>
>(({ className, ...props }, ref) => (
  <span ref={ref} aria-current="page" className={cn("font-medium text-fg", className)} {...props} />
));
BreadcrumbPage.displayName = "BreadcrumbPage";

export function BreadcrumbSeparator({
  className,
  children,
  ...props
}: React.HTMLAttributes<HTMLLIElement>) {
  return (
    <li
      role="presentation"
      aria-hidden="true"
      className={cn("text-fg-muted", className)}
      {...props}
    >
      {children ?? "/"}
    </li>
  );
}
