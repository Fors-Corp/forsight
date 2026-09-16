import { useCallback, useEffect, useState } from "react";
import { applyForsightTheme, FORSIGHT_THEMES, type ForsightTheme } from "@marcfs31/forsight/theme";

// Same key the vite.config.ts anti-flash script writes/reads before first
// paint (forsightAntiFlashScript({ storageKey: THEME_STORAGE_KEY })), so a
// stored choice actually round-trips instead of silently living under two
// different keys.
export const THEME_STORAGE_KEY = "forsight-theme";

/** Reads the stored theme choice. Falls back to "dark" — the design
 *  system's own bare-:root default — for a first visit, a value that isn't
 *  one of FORSIGHT_THEMES, or storage that throws. Mirrors
 *  readStoredToken/storeToken in api.ts: localStorage access always goes
 *  through try/catch and degrades to the default rather than throwing. */
export function readStoredTheme(): ForsightTheme {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY);
    return (FORSIGHT_THEMES as readonly string[]).includes(stored ?? "")
      ? (stored as ForsightTheme)
      : "dark";
  } catch {
    // Private browsing, or localStorage disabled entirely: behave as if
    // nothing were stored yet.
    return "dark";
  }
}

function storeTheme(theme: ForsightTheme) {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // Nothing to persist to — the in-memory state below still drives the
    // page for the rest of this tab's life, it just won't survive a reload.
  }
}

/**
 * The dashboard's one theme switch. Starts from whatever the anti-flash
 * script already applied to <html> before React mounted (readStoredTheme
 * agrees with it), applies `applyForsightTheme` again on every change so
 * `data-theme` always matches state, and persists the choice the same way
 * every other reload-and-remember value in this app does.
 */
export function useTheme(): [ForsightTheme, (theme: ForsightTheme) => void] {
  const [theme, setThemeState] = useState<ForsightTheme>(() => readStoredTheme());

  useEffect(() => {
    applyForsightTheme(theme);
  }, [theme]);

  const setTheme = useCallback((next: ForsightTheme) => {
    storeTheme(next);
    setThemeState(next);
  }, []);

  return [theme, setTheme];
}
