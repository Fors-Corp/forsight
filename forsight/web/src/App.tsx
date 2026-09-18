import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  AppShell,
  AppShellMain,
  Button,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarNav,
  SidebarNavItem,
  SidebarProvider,
  SidebarTrigger,
  Switch,
  Text,
  useSidebar,
} from "@marcfs31/forsight";
import { ROUTE_LABELS, useHashRoute, type Route } from "./route";
import { submitAuthToken, useAuthPrompt } from "./api";
import { useTheme } from "./theme";
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
 * The sidebar's one appearance control: a Switch (it reads as a settings
 * row, not a toolbar action, so Switch over Toggle) that flips between the
 * design system's two themes and remembers the choice per browser via
 * useTheme. "Light theme" is the accessible name regardless of which way
 * it's currently set, same as any other on/off setting.
 */
function ThemeToggle() {
  const [theme, setTheme] = useTheme();

  return (
    <div className="flex items-center justify-between gap-2">
      <Text as="span" size="sm">
        Light theme
      </Text>
      <Switch
        aria-label="Light theme"
        checked={theme === "light"}
        onCheckedChange={(checked) => setTheme(checked ? "light" : "dark")}
      />
    </div>
  );
}

/**
 * Blocks the dashboard behind a bearer-token prompt whenever the agent's
 * data routes 401 (see forsight/internal/api/auth.go's BearerAuth — every
 * route but the static shell requires one once --auth-token is set). The
 * shell itself always loads with no token, so this is the only gate the
 * page has; it isn't dismissable (no close button, Escape and an outside
 * click are both swallowed) because the pages behind it have nothing to
 * show without a good token anyway. `useAuthPrompt` opens it the moment any
 * fetchWithAuth call gets a 401 and `submitAuthToken` closes it again — a
 * wrong guess just reopens it (this time with `rejected: true`) once the
 * next poll 401s.
 */
function AuthTokenDialog() {
  const { open, rejected } = useAuthPrompt();
  const [value, setValue] = useState("");

  // Never carry a rejected guess (or the last one typed) into the next time
  // the dialog opens.
  useEffect(() => {
    if (open) setValue("");
  }, [open]);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const token = value.trim();
    if (!token) return;
    // Never touches a URL or a log — sessionStorage and the Authorization
    // header only (see submitAuthToken/fetchWithAuth in api.ts).
    submitAuthToken(token);
  }

  return (
    <Dialog open={open} onOpenChange={() => {}}>
      <DialogContent
        hideClose
        onEscapeKeyDown={(event) => event.preventDefault()}
        onPointerDownOutside={(event) => event.preventDefault()}
        onInteractOutside={(event) => event.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>Access token required</DialogTitle>
          <DialogDescription>
            {rejected
              ? "That token was rejected. Enter the value forsight was started with (--auth-token or FORSIGHT_AUTH_TOKEN)."
              : "This agent requires a bearer token to show its data. Enter the value forsight was started with (--auth-token or FORSIGHT_AUTH_TOKEN)."}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <Input
            type="password"
            autoFocus
            autoComplete="off"
            spellCheck={false}
            aria-label="Access token"
            placeholder="Bearer token"
            invalid={rejected}
            hint={rejected ? "Incorrect token." : undefined}
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
          <DialogFooter>
            <Button type="submit" disabled={!value.trim()}>
              Continue
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
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

  // Client-navigation focus per the ARIA APG hash-routing pattern: move
  // focus to the new page's <h1> so a screen-reader user learns the page
  // changed instead of staying parked on the nav link they just activated.
  // `hasNavigated` skips the very first render — focusing on initial load
  // would steal focus from the top of the document, which is its own bug.
  const headingRef = useRef<HTMLHeadingElement | null>(null);
  const hasNavigated = useRef(false);
  useEffect(() => {
    if (!hasNavigated.current) {
      hasNavigated.current = true;
      return;
    }
    headingRef.current?.focus();
  }, [route]);

  return (
    <SidebarProvider>
      <AuthTokenDialog />
      <AppShell>
        <Sidebar label="Main navigation">
          <SidebarHeader>
            <Text weight="semibold">forsight</Text>
          </SidebarHeader>
          <SidebarContent>
            <SidebarNavLinks route={route} />
          </SidebarContent>
          <SidebarFooter>
            <ThemeToggle />
          </SidebarFooter>
        </Sidebar>
        <AppShellMain>
          <div className="flex h-14 items-center gap-2 border-b border-ink-border px-3">
            <SidebarTrigger />
            <Text as="span" size="sm" tone="secondary">
              {ROUTE_LABELS[route]}
            </Text>
          </div>
          {route === "models" ? (
            <Models headingRef={headingRef} />
          ) : (
            <Overview headingRef={headingRef} />
          )}
        </AppShellMain>
      </AppShell>
    </SidebarProvider>
  );
}
