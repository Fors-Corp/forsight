import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { cn } from "../lib/cn";

const SIDEBAR_ICON_BUTTON_CLASS = cn(
  "inline-flex min-h-10 min-w-10 items-center justify-center rounded-md text-fg-muted transition-colors duration-base",
  "hover:bg-ink-surface-2 hover:text-fg",
  "focus-visible:outline-none focus-visible:shadow-focus-ring"
);

const MENU_ICON = (
  <svg width="18" height="18" viewBox="0 0 18 18" fill="none" aria-hidden="true">
    <path
      d="M2.5 5h13M2.5 9h13M2.5 13h13"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
    />
  </svg>
);

const CLOSE_ICON = (
  <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
    <path d="M1 1L13 13M13 1L1 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
  </svg>
);

export interface SidebarContextValue {
  /** Desktop icon-rail state — irrelevant below the `md` breakpoint. */
  collapsed: boolean;
  setCollapsed: (value: boolean) => void;
  toggleCollapsed: () => void;
  /** Mobile drawer state — irrelevant at/above the `md` breakpoint. */
  mobileOpen: boolean;
  setMobileOpen: (value: boolean) => void;
  toggleMobileOpen: () => void;
  /**
   * The mobile trigger button, so `Sidebar`'s drawer can return focus to it
   * on close. `SidebarTrigger` isn't a descendant of `Sidebar`'s own
   * `DialogPrimitive.Root` (it can live anywhere in the tree), so Radix's
   * usual `DialogPrimitive.Trigger` focus-return (which needs that
   * ancestry) can't apply here — this ref is the substitute.
   */
  mobileTriggerRef: React.RefObject<HTMLButtonElement | null>;
}

const SidebarContext = React.createContext<SidebarContextValue | null>(null);

/** Reads the nearest `SidebarProvider`'s state — throws outside one, since every other Sidebar part depends on it. */
export function useSidebar(): SidebarContextValue {
  const context = React.useContext(SidebarContext);
  if (!context) {
    throw new Error("useSidebar must be used within a SidebarProvider.");
  }
  return context;
}

export interface SidebarProviderProps {
  children: React.ReactNode;
  /** Initial desktop collapsed state. Defaults to expanded. */
  defaultCollapsed?: boolean;
}

/**
 * Holds the collapsed/mobile-open state shared by `Sidebar` and
 * `SidebarTrigger` — wrap the whole `AppShell` in one, the same shape as
 * `TooltipProvider` wrapping a subtree.
 */
export function SidebarProvider({ children, defaultCollapsed = false }: SidebarProviderProps) {
  const [collapsed, setCollapsed] = React.useState(defaultCollapsed);
  const [mobileOpen, setMobileOpen] = React.useState(false);
  const mobileTriggerRef = React.useRef<HTMLButtonElement>(null);

  const value = React.useMemo<SidebarContextValue>(
    () => ({
      collapsed,
      setCollapsed,
      toggleCollapsed: () => setCollapsed((v) => !v),
      mobileOpen,
      setMobileOpen,
      toggleMobileOpen: () => setMobileOpen((v) => !v),
      mobileTriggerRef,
    }),
    [collapsed, mobileOpen]
  );

  return <SidebarContext.Provider value={value}>{children}</SidebarContext.Provider>;
}

export interface SidebarProps {
  children: React.ReactNode;
  /** Accessible name for the navigation landmark (both the desktop `<nav>` and the mobile drawer). */
  label: string;
  className?: string;
}

/**
 * Renders its `children` twice — once into a static desktop `<nav>` (hidden
 * below `md`), once into a mobile slide-in drawer (hidden at/above `md`) —
 * so there's exactly one composition for consumers to write, matching the
 * responsive pattern already used throughout this repo (CSS-driven
 * show/hide, not a JS media-query mount/unmount). The mobile drawer is built
 * directly on `@radix-ui/react-dialog` (not this repo's `DialogContent`,
 * whose centered-modal shape doesn't fit an edge-anchored drawer) so it
 * still gets Radix's real focus-trap/dismiss/Escape behavior for free.
 */
export function Sidebar({ children, label, className }: SidebarProps) {
  const { collapsed, mobileOpen, setMobileOpen, mobileTriggerRef } = useSidebar();

  return (
    <>
      <nav
        aria-label={label}
        className={cn(
          "hidden shrink-0 flex-col border-e border-ink-border bg-ink-surface transition-[width] duration-base md:flex",
          collapsed ? "w-16" : "w-64",
          className
        )}
      >
        {children}
      </nav>
      <DialogPrimitive.Root open={mobileOpen} onOpenChange={setMobileOpen}>
        <DialogPrimitive.Portal>
          <DialogPrimitive.Overlay
            className={cn(
              "fixed inset-0 z-50 bg-black/60 md:hidden",
              "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-base"
            )}
          />
          <DialogPrimitive.Content
            aria-label={label}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              mobileTriggerRef.current?.focus();
            }}
            className={cn(
              "fixed inset-y-0 start-0 z-50 flex w-64 max-w-[calc(100vw-3rem)] flex-col border-e border-ink-border bg-ink-surface md:hidden",
              "focus-visible:outline-none",
              "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-base",
              // The drawer anchors to the logical start side (left in LTR,
              // right in RTL, matching the desktop rail's flex-row position,
              // which flips automatically) — so which physical edge it
              // slides to/from has to flip explicitly too; `start`/`end`
              // have no slide-animation equivalent in tailwindcss-animate.
              "ltr:data-[state=closed]:slide-out-to-left-full ltr:data-[state=open]:slide-in-from-left-full",
              "rtl:data-[state=closed]:slide-out-to-right-full rtl:data-[state=open]:slide-in-from-right-full",
              className
            )}
          >
            {children}
            <DialogPrimitive.Close
              aria-label="Close navigation"
              className={cn(SIDEBAR_ICON_BUTTON_CLASS, "absolute end-2 top-2")}
            >
              {CLOSE_ICON}
            </DialogPrimitive.Close>
          </DialogPrimitive.Content>
        </DialogPrimitive.Portal>
      </DialogPrimitive.Root>
    </>
  );
}

/**
 * Toggle control for `Sidebar` — renders two buttons, CSS-switched by
 * breakpoint like `Sidebar` itself: one opens the mobile drawer (below
 * `md`), the other toggles the desktop icon-rail (at/above `md`). The
 * hidden one is `display:none`, so only the relevant control ever reaches
 * the accessibility tree.
 */
export function SidebarTrigger({ className }: { className?: string }) {
  const { toggleMobileOpen, toggleCollapsed, mobileTriggerRef } = useSidebar();
  return (
    <>
      <button
        ref={mobileTriggerRef}
        type="button"
        aria-label="Open navigation"
        onClick={toggleMobileOpen}
        className={cn(SIDEBAR_ICON_BUTTON_CLASS, "md:hidden", className)}
      >
        {MENU_ICON}
      </button>
      <button
        type="button"
        aria-label="Toggle sidebar"
        onClick={toggleCollapsed}
        className={cn(SIDEBAR_ICON_BUTTON_CLASS, "hidden md:inline-flex", className)}
      >
        {MENU_ICON}
      </button>
    </>
  );
}

export const SidebarHeader = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        "flex h-14 shrink-0 items-center gap-2 border-b border-ink-border px-3",
        className
      )}
      {...props}
    />
  )
);
SidebarHeader.displayName = "SidebarHeader";

export const SidebarContent = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div ref={ref} className={cn("flex-1 overflow-y-auto p-2", className)} {...props} />
));
SidebarContent.displayName = "SidebarContent";

export const SidebarFooter = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div
      ref={ref}
      className={cn("shrink-0 border-t border-ink-border p-3", className)}
      {...props}
    />
  )
);
SidebarFooter.displayName = "SidebarFooter";

export const SidebarNav = React.forwardRef<
  HTMLUListElement,
  React.HTMLAttributes<HTMLUListElement>
>(({ className, ...props }, ref) => (
  <ul ref={ref} className={cn("flex flex-col gap-1", className)} {...props} />
));
SidebarNav.displayName = "SidebarNav";

export interface SidebarNavItemProps extends React.ComponentPropsWithoutRef<"a"> {
  /** Marks this as the current page (`aria-current="page"`) and applies the active look. */
  active?: boolean;
  /** Decorative leading icon — `aria-hidden` is applied automatically. */
  icon?: React.ReactNode;
}

/**
 * A plain nav link — normal tab order, no roving-tabindex/ARIA-menu pattern,
 * since this is a simple link list, not a widget (applying the `menu`
 * pattern here would fight native assistive-tech expectations). When the
 * sidebar is collapsed to its desktop icon rail, the label stays in the
 * accessibility tree via `sr-only` rather than `hidden` — hiding it with
 * `display:none` would leave the link with no accessible name at all once
 * its icon (correctly `aria-hidden`) is the only visible content.
 */
export const SidebarNavItem = React.forwardRef<HTMLAnchorElement, SidebarNavItemProps>(
  ({ className, active, icon, children, ...props }, ref) => {
    const { collapsed } = useSidebar();
    return (
      <li>
        <a
          ref={ref}
          aria-current={active ? "page" : undefined}
          className={cn(
            "flex min-h-10 items-center gap-3 rounded-md px-3 text-sm font-sans transition-colors duration-base",
            active
              ? "bg-ink-surface-2 text-accent"
              : "text-fg-secondary hover:bg-ink-surface-2 hover:text-fg",
            "focus-visible:outline-none focus-visible:shadow-focus-ring",
            className
          )}
          {...props}
        >
          {icon && (
            <span aria-hidden="true" className="shrink-0">
              {icon}
            </span>
          )}
          <span className={cn("truncate", collapsed && "md:sr-only")}>{children}</span>
        </a>
      </li>
    );
  }
);
SidebarNavItem.displayName = "SidebarNavItem";

export const AppShell = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div
      ref={ref}
      className={cn("flex h-screen overflow-hidden bg-ink-bg", className)}
      {...props}
    />
  )
);
AppShell.displayName = "AppShell";

/** The page's single `<main>` landmark — `min-w-0` so it can actually shrink in the flex row instead of pushing `Sidebar` off-screen. */
export const AppShellMain = React.forwardRef<HTMLElement, React.HTMLAttributes<HTMLElement>>(
  ({ className, ...props }, ref) => (
    <main ref={ref} className={cn("min-w-0 flex-1 overflow-y-auto", className)} {...props} />
  )
);
AppShellMain.displayName = "AppShellMain";
