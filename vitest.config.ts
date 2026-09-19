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
      // perFile + glob-keyed thresholds, not a single aggregate number: the
      // old blended 98/98/85/85 passed in total while individual files sat
      // far under it (Slider.tsx had 40% branch coverage, Card.tsx 50%, with
      // no test ever pressing an arrow key or an `interactive` prop) — the
      // aggregate hid exactly the files most likely to have a real, operable
      // bug. Every file must now clear 98/98/85/85 *on its own*.
      //
      // Two buckets, not a plain top-level floor: Vitest's `perFile` also
      // runs an implicit "global" threshold set against literally every
      // matched file whenever statements/branches/functions/lines is set at
      // the top level, and that check cannot be relaxed by a glob elsewhere
      // — both the top-level and the glob thresholds run, and either one
      // failing fails the file. So a lone top-level number would either
      // re-flatten this back into the old aggregate-only gate (if set low
      // enough for the worst file) or permanently fail every file below the
      // real target (if set to 98/98/85/85). Omitting the top-level keys
      // skips that implicit global set entirely; the glob below is then the
      // *only* check, and it covers every file exactly once.
      thresholds: {
        perFile: true,
        // The default: every file except the seven named below. New files
        // land here automatically and must clear the real bar from day one.
        "src/**/!(BoxPlot|Calendar|Combobox|Dialog|Drawer|JSONViewer|Sidebar).{ts,tsx}": {
          statements: 98,
          lines: 98,
          branches: 85,
          functions: 85,
        },
        // Pre-existing gaps this PR did not touch (out of scope: this PR's
        // brief was Slider/Progress/Card/Toast/Select/Tabs/ScrollArea) —
        // each gated at its own measured baseline as of this change, the
        // same "a bit below the real number" convention as the default
        // above, so it's still a real regression gate and not a wall nobody
        // has verified passes. Raise (or delete, once a file clears the
        // default) an entry here as its tests improve; never lower one.
        //   BoxPlot.tsx      97.91 / 90.9  / 100   / 100   — stmts just under 98
        //   Calendar.tsx     76.19 / 87.17 / 71.42 / 78.94 — well under on 3/4
        //   Combobox.tsx     100   / 84.61 / 100   / 100   — branches just under 85
        //   Dialog.tsx       92.85 / 100   / 80    / 92.85 — overlay body, see the
        //   Drawer.tsx       92.85 / 100   / 80    / 92.85 — HoverCard-style exclude
        //                    note above; not extended to these without a decision
        //   JSONViewer.tsx   97.87 / 95.12 / 100   / 100   — stmts just under 98
        //   Sidebar.tsx      94.11 / 100   / 94.11 / 93.75
        "src/components/BoxPlot.tsx": {
          statements: 97,
          branches: 90,
          functions: 100,
          lines: 100,
        },
        "src/components/Calendar.tsx": {
          statements: 76,
          branches: 87,
          functions: 71,
          lines: 78,
        },
        "src/components/Combobox.tsx": {
          statements: 100,
          branches: 84,
          functions: 100,
          lines: 100,
        },
        "src/components/Dialog.tsx": {
          statements: 92,
          branches: 100,
          functions: 80,
          lines: 92,
        },
        "src/components/Drawer.tsx": {
          statements: 92,
          branches: 100,
          functions: 80,
          lines: 92,
        },
        "src/components/JSONViewer.tsx": {
          statements: 97,
          branches: 95,
          functions: 100,
          lines: 100,
        },
        "src/components/Sidebar.tsx": {
          statements: 94,
          branches: 100,
          functions: 94,
          lines: 93,
        },
      },
    },
  },
});
