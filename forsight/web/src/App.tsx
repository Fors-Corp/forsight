import { useEffect, useRef } from "react";
import {
  AppShell,
  AppShellMain,
  Sidebar,
  SidebarContent,
  SidebarHeader,
  SidebarNav,
  SidebarNavItem,
  SidebarProvider,
  SidebarTrigger,
  Text,
  useSidebar,
} from "@marcfs31/forsight";
import { useHashRoute, type Route } from "./route";
import Overview from "./pages/Overview";
import Models from "./pages/Models";

// Decorative nav icons, same shape as the design system's Sidebar story.
// SidebarNavItem applies aria-hidden to `icon` itself, so these carry no
// accessible name of their own — the link text does.
const OVERVIEW_ICON = (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
    <path
      d="M2 7L8 2l6 5v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V7Z"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinejoin="round"
    />
  </svg>
);

const MODELS_ICON = (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
    <path
      d="M2 12l3.5-4 3 2.5L13.5 4"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
    <circle cx="13.5" cy="4" r="1.5" stroke="currentColor" strokeWidth="1.4" />
  </svg>
);

/**
 * The sidebar's nav links, as a child of `SidebarProvider` (via `Sidebar`,
 * whose `children` it is part of) so it can read/close the mobile drawer.
 * `Sidebar` renders `children` twice — once into the always-mounted desktop
 * `<nav>`, once into the mobile drawer's dialog content, which Radix only
 * mounts while the drawer is open — so this mounts once per visible copy
 * and its effect fires in whichever copy is actually on screen.
 *
 * Without this, the mobile drawer stayed open after a nav tap: `<a href>`
 * navigation changes `location.hash` but never touches `mobileOpen`, so the
 * drawer only closed via its own explicit close button or an outside click.
 * Closing on every route change — tap, back/forward, or a typed hash —
 * fixes that, and returns focus to the trigger via `Sidebar`'s own
 * `onCloseAutoFocus`.
 *
 * The mobile copy only exists in the DOM at all while the drawer is open
 * (Radix unmounts the dialog's content on close), so its very first effect
 * run *is* the render that just opened it — an unguarded `setMobileOpen(false)`
 * here would close the drawer the instant it opened. `previousRoute` tells
 * that "just mounted, already on this route" apart from "route actually
 * changed under a still-mounted copy", so only a real change closes it.
 */
function SidebarNavLinks({ route }: { route: Route }) {
  const { setMobileOpen } = useSidebar();
  const previousRoute = useRef(route);

  useEffect(() => {
    if (previousRoute.current !== route) {
      setMobileOpen(false);
    }
    previousRoute.current = route;
  }, [route, setMobileOpen]);

  return (
    <SidebarNav>
      <SidebarNavItem href="#/" icon={OVERVIEW_ICON} active={route === "overview"}>
        Overview
      </SidebarNavItem>
      <SidebarNavItem href="#/models" icon={MODELS_ICON} active={route === "models"}>
        Models
      </SidebarNavItem>
    </SidebarNav>
  );
}

/**
 * The dashboard shell: a sidebar with one link per page and a `<main>`
 * that shows whichever page the location hash names. Routing is a hash so
 * the embedded build keeps working from any mount point without a server
 * rewrite (see route.ts). The page bodies live under pages/ so this file
 * stays a layout and nothing else.
 */
export default function App() {
  const route = useHashRoute();

  return (
    <SidebarProvider>
      <AppShell>
        <Sidebar label="Main navigation">
          <SidebarHeader>
            <Text weight="semibold">forsight</Text>
          </SidebarHeader>
          <SidebarContent>
            <SidebarNavLinks route={route} />
          </SidebarContent>
        </Sidebar>
        <AppShellMain>
          <div className="flex h-14 items-center gap-2 border-b border-ink-border px-3">
            <SidebarTrigger />
            <Text as="span" size="sm" tone="secondary">
              {route === "models" ? "Models" : "Overview"}
            </Text>
          </div>
          {route === "models" ? <Models /> : <Overview />}
        </AppShellMain>
      </AppShell>
    </SidebarProvider>
  );
}
