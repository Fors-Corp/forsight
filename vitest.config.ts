import { configDefaults, defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
    globals: false,
    // .claude/worktrees holds throwaway git worktrees (full repo copies with
    // their own node_modules + tests) created by Claude Code sessions.
    // forsight/ and forseer/ are separate Go modules with their own toolchain
    // (forsight/web is its own npm project, with its own vitest config and
    // its own React/testing-library versions) — not part of this project's
    // test run. Same exclusion eslint.config.js and .prettierignore already
    // make; vitest's default `include` glob has no directory scoping of its
    // own, so without this a `*.test.tsx` added under forsight/web/src gets
    // picked up and run against THIS package's react copy instead of its own.
    exclude: [
      ...configDefaults.exclude,
      ".claude/worktrees/**",
      "fixtures/**",
      "forsight/**",
      "forseer/**",
    ],
    // Radix overlay tests still open a portal + run focus-scope/floating-ui
    // logic under jsdom (no layout engine), which is slow-ish on a loaded CI
    // runner. The pathological case — `axe` on an *open* overlay, which ran
    // for minutes — has been moved to the Storybook test runner (real
    // Chromium); what's left here is structural and comfortably under 60s.
    testTimeout: 60000,
    coverage: {
      provider: "v8",
      reporter: ["text", "html", "json-summary"],
      include: ["src/**/*.{ts,tsx}"],
      exclude: [
        "src/**/*.stories.tsx",
        "src/**/*.test.tsx",
        "src/**/__tests__/**",
        "src/test-types.d.ts",
        "src/test-utils/**",
        "src/index.ts",
        "src/theme-entry.ts",
        // Floating (Popper) overlays can't be rendered open under jsdom at a
        // usable speed, so their render bodies have no jsdom test to cover
        // them. They ARE fully exercised (render + open/close + axe) by the
        // Storybook test runner in real Chromium — a coverage tool v8 can't
        // see. Excluded here so the 95% bar stays meaningful for the ~24
        // components jsdom covers properly.
        "src/components/HoverCard.tsx",
        "src/components/Popover.tsx",
        "src/components/DropdownMenu.tsx",
        "src/components/Tooltip.tsx",
      ],
      // Set a bit below the actual measured numbers (~99.5/86.5/89/99.5 as of
      // the Separator/Label/Collapsible addition) so this is a real
      // regression gate — catching a wholesale untested addition or a broken
      // branch — not a wall nobody's verified passes. Ratcheted up from the
      // v1.0.0 baseline (95/95/85/80) as coverage genuinely improved; bump up
      // again the same way, never down.
      thresholds: {
        statements: 98,
        lines: 98,
        branches: 85,
        functions: 85,
      },
    },
  },
});
