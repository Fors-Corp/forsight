import { useEffect, useState } from "react";

/** The pages the dashboard has. Anything the hash doesn't name falls back
 * to the overview, so a stale bookmark never lands on a blank screen. */
export type Route = "overview" | "models";

/** Maps a location hash to a route. Exported so the mapping has a unit
 * test of its own and App's routing test can reason about it. */
export function routeFromHash(hash: string): Route {
  const path = hash.replace(/^#/, "");
  if (path === "/models") return "models";
  return "overview";
}

/**
 * Hash routing with no router library: the dashboard is embedded in the Go
 * binary and served from a single index.html at an arbitrary mount point
 * (see vite.config.ts's `base: "./"`), so `#/models` keeps working with no
 * server-side rewrite and no knowledge of where the page lives.
 */
export function useHashRoute(): Route {
  const [route, setRoute] = useState<Route>(() => routeFromHash(window.location.hash));

  useEffect(() => {
    const onChange = () => setRoute(routeFromHash(window.location.hash));
    // The initial state above already reflects the hash at mount; re-read
    // it here anyway so a hash set between render and effect isn't lost.
    onChange();
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);

  return route;
}
