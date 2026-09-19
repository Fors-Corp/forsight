# @marcfs31/fors-observability-design-system

## 4.3.0

### Minor Changes

- 1b1a70b: `Tabs.Root`/`List`/`Trigger`/`Panel` and `SidebarHeader`/`SidebarContent`/
  `SidebarFooter`/`SidebarNav`/`AppShell`/`AppShellMain` are now built with
  `React.forwardRef`, matching every other compound component in the library
  (`Card`, `Dialog`, `AlertDialog`, `Command`, `Table`, `Drawer`,
  `DropdownMenu`, `Breadcrumb`, `RadioGroup`, `ToggleGroup`) — they were the
  last two that weren't. A consumer passing `ref={ref}` to any of them
  previously failed to typecheck (`ref` was not part of the declared prop
  type), so a TypeScript app could not measure a tab list, scroll a panel into
  view, or focus the sidebar's `<nav>` from outside. Each part also now sets
  `displayName`, so React DevTools and test-runner error messages name it
  instead of showing `Anonymous`.

  New named, exported prop types, closing gaps where a type existed in source
  but wasn't part of the public API (or, for `DialogContent`/`AlertDialogAction`/
  `AlertDialogCancel`/`DrawerContent`, existed only as an inline, unnamed
  intersection): `TabsRootProps`, `TabsTriggerProps`, `TabsPanelProps`,
  `TableHeadProps`, `SliderProps`, `DialogContentProps`,
  `AlertDialogActionProps`, `AlertDialogCancelProps`, `DrawerContentProps`. A
  new test (`src/__tests__/prop-exports.test.ts`) asserts every exported
  `*Props` type defined in `src/components/*.tsx` is re-exported from
  `src/index.ts` unless explicitly allow-listed, so a future gap like this is
  a decision instead of an oversight.

### Patch Changes

- 4496b43: `BarList`, `BreadcrumbLink` and `SidebarNavItem` now refuse to render an
  unsafe `href` as a clickable link. All three accept an `href` documented as
  a drill-down or nav destination — data that in practice traces back to a
  route, service name, tag or error code echoed from request input, so a
  consumer that renders one of these components with attacker-influenced data
  was one `javascript:` or `data:text/html` URI away from running script in an
  on-call operator's authenticated session when they clicked what looked like
  a normal row or link.

  A new `isSafeHref` helper (not exported from the package root; internal to
  these three components) parses the href against the current page's origin —
  or, where there is no document to read one from, against a fixed `https:`
  base, so a server render reaches the same verdict as the browser instead of
  refusing every href and rehydrating a row from `<div>` to `<a>` — and allows
  only relative URLs and absolute `http:`/`https:` URLs — a
  protocol-relative URL (`//host/path`) is allowed too, since it resolves to
  whichever of those two the current page is served over. Anything else
  (`javascript:`, `data:`, `vbscript:`, an unparseable string) falls back to
  the same non-link rendering each component already uses when no `href` is
  given at all: `BarList` renders its existing plain `<div>` row instead of an
  `<a>`, and `BreadcrumbLink`/`SidebarNavItem` render an `<a>` with no `href`
  attribute (which, per the HTML spec, has no `link` role and isn't focusable
  — not a link, the same as `BarList`'s fallback).

## 4.2.0

### Minor Changes

- b5ca542: New server-safe entry `@marcfs31/forsight/chart`: the pure chart maths and
  number formatters (`arcPath`, `areaPath`, `barPath`, `clamp`, `formatCompact`,
  `formatDuration`, `formatPercent`, `linePath`, `niceScale`, `polar`,
  `project`, `seriesBg`, `seriesFill`, `seriesStroke`, `SERIES_SLOTS`,
  `splitAtGaps`, `splitAtProjection`, `ANNOTATION_TONE_CLASSES`) that back the
  chart components.

  Previously this maths was reachable only through the components entry, and
  that entire entry ships a leading `"use client"` directive — so a React
  Server Component that imported `clamp` or `formatCompact` to precompute a
  mark or format a value server-side failed to build. The README already
  called the package "RSC-ready", which this contradicted for anything that
  touched chart maths outside a component.

  The new entry mirrors `@marcfs31/forsight/theme`: no client directive, and
  `publint`/`arethetypeswrong` validate it clean on every resolution mode,
  including legacy `node10` (via a root `chart/package.json` stub, same as
  `theme/`). Nothing moved — `clamp`, `niceScale`, and the rest are still
  exported from the components entry too, for chart components and consumers
  that already import them from there.

- 45f7506: Number formatting follows the viewer's locale.

  `formatCompact`, `formatDuration` and `formatPercent` — the defaults behind
  Gauge, BarList, Heatmap, CalendarHeatmap, Delta and every chart axis and
  tooltip — wrote their numbers with a hardcoded ASCII decimal point, so a
  German or French reader saw `1.2k`, `940ms` and `5.5%` where their locale
  wants `1,2k`, `940ms` and `5,5 %`. They now delegate to `Intl.NumberFormat`,
  matching the `Intl.DateTimeFormat` the date code already used.

  English output is unchanged, deliberately: the unit suffixes stay the
  library's own rather than `Intl`'s `notation: "compact"`, whose single
  `maximumFractionDigits` cannot keep `1240` short and `0.0123` precise at the
  same time, whose suffixes are locale data (German renders no `k` at all), and
  which renders `1.2K` in English.

  `CalendarHeatmap`'s weekday row headers were a hardcoded English array one
  line above a correct `Intl.DateTimeFormat` for its month labels; they are now
  computed too.

  `formatPercent`'s doc comment claimed `99.982 → "99.982%"`, which was never
  true of its default `decimals = 1` — that returns `"100%"`. The behaviour is
  unchanged; the comment now says so and points at `UptimeBar`, which passes
  `2` for exactly this reason.

- f4266cb: Make the focus ring meet WCAG 1.4.11, so keyboard focus is actually visible.

  `--forsight-focus-ring` was the brand teal at 45% alpha in both themes. A
  `box-shadow` is painted outside the control, against the page behind it, so
  that alpha composited straight onto the ink surfaces and left the ring at
  2.67:1 on dark `bg`/`surface` and 2.58:1 on `surface-2`, and 2.03/2.07/1.99:1
  in light — all under the 3:1 floor a non-text indicator has to clear. Roughly
  thirty components carry `focus-visible:shadow-focus-ring` as their only focus
  affordance, so on every one of them the sole marker of keyboard focus was too
  faint to see.

  The ring is now the solid accent — `#16c7b0` dark, `#0b6c5e` light, both
  already on the brand ramp — which measures 9.00/8.36/7.52:1 and
  5.99/6.32/5.62:1 against `bg`/`surface`/`surface-2`. `shadow-focus-ring` keeps
  its 3px footprint but spends the innermost pixel on a hairline of
  `--forsight-ink-bg`, so a solid teal ring around an accent-filled control
  (primary `Button`, a checked `Switch` or `Checkbox`) is still separated from
  the fill instead of merging into it.

  `ForsightPalette` gained `focusRing` and `focusRingAlpha`, and
  `contrast.test.ts` now blends the ring at its declared alpha and asserts 3:1
  against all three surfaces in both themes — the check that could not see this
  token before.

### Patch Changes

- 5813d58: Fix `AlertList` so the first alert after an empty state is announced.

  Previously `AlertList` rendered `EmptyState` in place of its `<ul
aria-live="polite">` whenever `items` was empty, so the live region did
  not exist yet at 0 items. Going from 0 to 1 alert mounted that `<ul>` for
  the first time with its content already on it — a live region announces
  mutations to content already inside it, not its own arrival, so most
  AT/browser pairs never spoke the alert a responder needed to hear first.

  The `<ul>` now stays mounted at every item count, the same way
  `LogStream`'s `role="log"` region does, with the empty message rendered
  as a child `<li>` (wrapping `EmptyState`, with its own `role="status"`
  turned off so it doesn't compete with the list's own live region for the
  same announcement).

## 4.1.1

### Patch Changes

- adc6953: Fix the keyboard-cursor tooltip's side under `dir="rtl"` in `ComboChart`,
  `BoxPlot` and `Histogram`, so it no longer covers the slot it is reading.

  The three charts parked the readout on the side opposite the active slot
  with a logical `start-2`/`end-2` class, but a slot's x position is a
  physical pixel — the plot never mirrors under RTL (see `ChartFrame`) — so
  under `dir="rtl"` the class resolved to the same side as the mark it was
  meant to avoid. Now the physical `left-2`/`right-2`, the same fix
  `LineChart` and `BarChart` got from the `DashboardRTL` coverage; these
  three were flagged there as the follow-up because `Dashboard` does not
  compose them, so no story exercised them under RTL. Each now has an "RTL"
  story that renders the chart under both directions, drives the cursor to
  each end of the axis, and checks the tooltip's rendered position against
  the active mark (`getBoundingClientRect`, not `toHaveClass`), per
  `Switch.stories.tsx`'s "RTL" story pattern.

- 08f7db5: Fix the chart family's RTL rendering: axis-value labels no longer land on
  top of the plot, the keyboard-cursor tooltip no longer covers the point it
  is reading, and `TraceWaterfall` no longer reads a trace backwards.

  SVG resolves `text-anchor="start"/"end"` against the inherited CSS
  `direction`, not a fixed physical side, so under `dir="rtl"` every
  `LineChart`/`BarChart` axis-value and annotation label flipped onto the
  wrong edge — `ChartFrame`'s `<svg>` now pins its own direction to `ltr`,
  matching the "time flows left-to-right in both text directions" contract
  its own comment already stated. The two charts' keyboard-cursor tooltip
  used a logical `start-2`/`end-2` class to park on the side opposite the
  active point, which is a physical question (the plot never mirrors under
  RTL) — now `left-2`/`right-2`. `TraceWaterfall`'s span bars used the
  logical `insetInlineStart`, which mirrored the whole waterfall under RTL —
  now the physical `left`, so a span's position always reads its wall-clock
  offset left-to-right.

  Caught by folding the whole chart family — `LineChart` (x2), `BarChart`,
  `DonutChart`, `BarList`, `Heatmap`, `TraceWaterfall`, and `Sparkline` (via
  `StatCard`) — into the `Forsight/Overview` → `DashboardRTL` story, which
  already composed `Dashboard`'s exact tree under `dir="rtl"`; the gap was
  that nothing there checked rendered geometry. `DashboardRTL`'s `play` now
  does, verified by real `getBoundingClientRect` position per
  `Switch.stories.tsx`'s "RTL" story pattern, not `toHaveClass`.

- d68be72: Make `LineChart`'s area fill respect `dashedFrom` too.

  Previously a series with both `area` and `dashedFrom` filled the whole run
  the same way, so the projected tail read as a second solid measurement
  under its own dashed stroke. The fill now splits at the same boundary as
  the stroke: the observed run keeps the plain area tint, and the projected
  run's fill drops to half that opacity and is overlaid with a diagonal hatch
  — shape, not color or opacity alone, carries the distinction, as
  CONTRIBUTING.md's data-viz rule requires. The hatch is one `<pattern>`
  per chart, referenced by every series' projected fill, not redeclared per
  series or per segment.

## 4.1.0

### Minor Changes

- 33b6d59: Give `LineChart` a way to draw a projection as a projection.

  A series' new `dashedFrom` index marks the last point drawn solid — the
  observed/forecast boundary. The stroke from that point onward switches to
  dashed instead of a second color, so a forecast reads the same in grayscale
  and to colorblind viewers, as CONTRIBUTING's data-viz rule requires. The
  boundary point is shared by both runs, so the line has no gap where it
  switches, and both runs still plot on the same y-axis and domain — a
  projection is more of the same series, not a second one. The distinction is
  never sighted-only: a series with `dashedFrom` set adds "`<name>` is
  projected from `<label>`." to `ChartFrame`'s hidden description.

## 4.0.1

### Patch Changes

- 414d6ad: **Security fix:** the tree-shaking build plugin (`scripts/forsight-treeshake-plugin.ts`, added in the 4.0.0 named-import tree-shaking change) rewrote each source file with plain `.replace()` regexes over raw, untokenized text — with no awareness of comments or string/template literals — before handing the result to esbuild.

  A source file containing a comment shaped like a `forwardRef`/`memo`/`cva` call could have `/* @__PURE__ */` inserted _inside_ that comment. Because the annotation itself contains `*/`, the insertion closed the real comment early, turning the remainder of it into live, executing top-level JavaScript in the built `dist/` bundle shipped to npm. The `displayName`-stripping pass had the same root cause: it could delete a line that merely looked like a `displayName` assignment from inside a multi-line template literal, corrupting that string's runtime value.

  Both passes (plus the `forwardRef`-declaration scan used to rename render functions for DevTools) now run against a comment/string-aware "code mask" instead of raw text, so a match can never start inside a comment, string, or template literal. No public API changed; this only affects how `dist/` is built.

  A regression test (`scripts/forsight-treeshake-plugin.test.ts`) reproduces the exact exploit shape end-to-end through the real esbuild toolchain and asserts the malicious payload never executes — confirmed to fail against the pre-fix code (the payload really did run) and pass against the fix.

  No source file in this repository currently contains a comment or string that triggers this, so the dist bundle actually published in past releases (4.0.0 and earlier since 1084bdc) was not observed to already contain injected code — but the vulnerable build tooling itself was live and publishable from the moment it landed.

## 4.0.0

### Major Changes

- 3184b1e: Compile the design system with Tailwind CSS v4.

  This package's own build (`dist/styles.css`, Storybook) now runs Tailwind 4.3
  via `@tailwindcss/cli` / `@tailwindcss/postcss`. Lightning CSS in that
  pipeline replaces Autoprefixer. The shipped stylesheet still omits Preflight
  and still makes no network calls.

  **If you consume `@marcfs31/forsight/styles.css` (the precompiled
  stylesheet):** keep importing it. No Tailwind upgrade is required to use the
  components. The generated CSS now uses v4 features (`@property`,
  `color-mix()`), so it targets Safari 16.4+, Chrome 111+, and Firefox 128+.

  **If you consume `@marcfs31/forsight/tailwind-preset` on Tailwind v3:** that
  export is unchanged — it is still a v3 JS preset (`presets: [forsightPreset]`
  in `tailwind.config`). Stay on Tailwind 3.4. The optional `tailwindcss` peer
  range remains `>=3.4`. Token class names (`bg-ink-*`, `text-fg*`,
  `bg-accent*`, `rounded-*`, `shadow-*`, `duration-fast` / `duration-base`) are
  the same.

  **If you consume `@marcfs31/forsight/tailwind.css` on Tailwind v4:** keep
  `@import "@marcfs31/forsight/tailwind.css"` after `@import "tailwindcss"`. The
  `@theme` mapping now also emits accordion/collapsible keyframes and
  `duration-fast` / `duration-base` `@utility` rules, matching the v3 preset.

  **If you compile this repo from source:** install Tailwind 4 together with
  `@tailwindcss/cli` and `@tailwindcss/postcss`. `tailwindcss` v4 no longer
  ships a CLI binary or a PostCSS plugin.

### Patch Changes

- 0388e8b: Give the `Combobox`, `MultiSelect` and `FilterBar` popovers an accessible name.

  All three compose a Radix popover internally, which renders as `role="dialog"`.
  None named it, so screen readers announced an unnamed dialog when the control
  opened — axe's `aria-dialog-name`. The inner `Command` listbox was already
  named in each, which is why this went unnoticed: the interactive part read
  correctly, the wrapper around it did not.

  The dialog now mirrors whatever names the trigger. For `Combobox` and
  `MultiSelect` that is `aria-labelledby` when the consumer passes one (so the
  name tracks the referenced element's text), otherwise the trigger's
  `aria-label`, otherwise the `placeholder`, which always has a value. Nothing
  changes for consumers who already pass `aria-label` — the dialog simply
  inherits that name instead of having none. `FilterBar`'s popover is internal
  and single-purpose, so it takes the trigger's own visible name, "Add filter".

  This was caught by moving the Storybook accessibility gate to
  `@storybook/addon-vitest`. The previous test runner scoped axe to
  `#storybook-root`, and Radix renders these popovers into a portal on
  `document.body` — outside that root — so no overlay content was ever checked.

- 904f3d1: Widen `SidebarContextValue.mobileTriggerRef` to `React.RefObject<HTMLButtonElement | null>`.

  The dev toolchain moves to React 19, whose `useRef<T>(null)` is typed
  `RefObject<T | null>`, so the context's declared type had to match. This is a
  type-only widening: nothing changes at runtime, and nothing breaks for
  consumers on React 18, where `RefObject<T>.current` was already `T | null`.

  The library's `react`/`react-dom` peer range is unchanged (`>=18`) — React 18
  and 19 are both still supported.

- 1084bdc: Enable named-import tree-shaking of the component barrel via a tsup esbuild plugin that marks `forwardRef`/`memo`/`cva` as `@__PURE__`, strips `displayName` assignments from the compiled output (source keeps them for DevTools during development), and names the render function passed to `forwardRef`.

## 3.0.0

### Major Changes

- c224c2d: **Breaking: the public API now uses the `forsight` name throughout.** The package rename in the previous major only moved the import path; this completes it in the API itself.

  CSS custom properties — all 48 of them — lose the `--fors-` prefix for `--forsight-`:

  ```diff
  -  --fors-accent: …;
  -  --fors-ink-surface: …;
  +  --forsight-accent: …;
  +  --forsight-ink-surface: …;
  ```

  Anything that reads a token directly (`var(--fors-accent)`, a `style` override of `--fors-font-sans` / `--fors-font-heading`, a custom theme that sets the palette) must be updated. Apps that only use the shipped components, `styles.css`, `tailwind.css` or the Tailwind preset need no change — the token utilities (`bg-accent`, `text-fg-muted`, …) keep their names.

  The `@marcfs31/forsight/theme` entry renames its exports:

  | Before                 | After                      |
  | ---------------------- | -------------------------- |
  | `applyForsTheme`       | `applyForsightTheme`       |
  | `forsAntiFlashScript`  | `forsightAntiFlashScript`  |
  | `ForsTheme`            | `ForsightTheme`            |
  | `ForsAntiFlashOptions` | `ForsightAntiFlashOptions` |
  | `FORS_THEMES`          | `FORSIGHT_THEMES`          |
  | `FORS_PALETTES`        | `FORSIGHT_PALETTES`        |
  | `ForsPalette`          | `ForsightPalette`          |

  The Tailwind v3 preset's default export is now `forsightPreset` (it was `forsPreset`); the import path is unchanged.

  **The stored theme preference resets once.** `applyForsightTheme` and `forsightAntiFlashScript` read and write `localStorage["forsight-theme"]` instead of `localStorage["fors-theme"]`, so a returning visitor falls back to the default (dark) on their first load after the upgrade and their next toggle sticks. No migration shim is shipped: the value is a UI preference, not data.

  Storybook story titles move from `Fors/…` to `Forsight/…`, and the demo content in stories uses the new brand. No component API, token _value_, or rendered behavior changes.

- 25d3229: **Breaking: the package is renamed from `@marcfs31/fors-observability-design-system` to `@marcfs31/forsight`.** Every import path changes accordingly:

  ```diff
  -import { Button } from "@marcfs31/fors-observability-design-system";
  -import "@marcfs31/fors-observability-design-system/styles.css";
  +import { Button } from "@marcfs31/forsight";
  +import "@marcfs31/forsight/styles.css";
  ```

  The same applies to the `/theme`, `/tailwind-preset`, `/tailwind.css`, and `/fonts.css` sub-paths. No component API, token, or behavior changes — this is a rename only. The GitHub repository is also renamed (`marcfs31/fors-observability-design-system` → `marcfs31/forsight`); GitHub redirects the old URL, but update any bookmarked links or CI references pointing at the old repo path.

### Minor Changes

- ac00222: Add `CodeBlock` — a monospace code/config snippet display (a curl example, a terminal command, a config file) with an optional label and a built-in `CopyButton`. Plain text only, no syntax highlighting; reach for `JSONViewer` when the content is structured data a reader might want to collapse rather than copy whole.
- ac00222: Add `CopyButton` — an icon button that copies a value to the clipboard and briefly confirms it. The accessible name swaps to `copiedLabel` and a polite live region announces it too, so the confirmation reaches assistive tech even though nothing moves focus. Used by the upcoming `CodeBlock`; also useful standalone next to an API key or webhook URL.

  Also adds the `"success (as text) on bg"` pairing to the token contrast test suite, covering the copied-state icon color.

- ac00222: Add `DateRangePicker` — `DatePicker`'s two-ended sibling for picking a custom start/end window, complementing `TimeRange`'s fixed presets ("1h", "24h") for whenever a reader needs an arbitrary range. `Calendar` now also supports `mode="range"`, styling the days between the two picked ends with a softer fill distinct from the two endpoints.
- ac00222: Add `Funnel` — a conversion/pipeline stage visualization (bars narrowing stage by stage, e.g. signups → activated → paid). Each bar scales against the first stage so the narrowing reads as overall conversion, while the percentage printed on every stage after the first is the step conversion from the stage right before it — the number a reader actually wants when hunting for where the funnel leaks.
- ac00222: Add `HoverCard` — a rich preview shown on hover or focus (`@radix-ui/react-hover-card`) for something the trigger only partly represents (a user's profile behind their avatar, a service's health summary behind its name). Unlike `Tooltip` it can hold arbitrary content; unlike `Popover` it never traps focus and never becomes the only place to reach information — the trigger must still make sense on its own.
- ac00222: Add `MultiSelect` — the tag-picker sibling to `Combobox`'s single-select, composed from the same `Popover` + `Command` pairing. Selected options show as plain (non-interactive) chips inside the trigger — deliberately no per-chip remove button, since that would nest an interactive control inside the trigger `<button>`; removal happens by reopening the popover and unchecking. Picking an option leaves the popover open so multiple picks don't need reopening.
- ac00222: Add `ScrollArea` — a custom-styled scrollbar for a fixed-size panel (a side nav, a long dropdown, a code pane), built on `@radix-ui/react-scroll-area`. Keeps real native scrolling (wheel, touch, keyboard, momentum) and only re-skins the visual thumb/track; the scrollable viewport is keyboard-focusable so arrow-key scrolling works without a mouse.
- ac00222: Add `Stepper` — a multi-step wizard/setup-progress indicator (done/current/upcoming). A real `<ol>` so a screen reader announces the step count and position, with the current step marked via `aria-current="step"`. For a bounded progress percentage instead of discrete named steps, use `Progress`.
- ac00222: Add `sortDirection`/`onSort` to `TableHead` for sortable column headers: sets `aria-sort` and renders the header as a button with a direction indicator. Matches `Table`'s existing scope — it only renders and announces the sort state, it never sorts rows itself; compose it with your own state (or a headless table library) to actually reorder rows, as shown in the new `Sortable` story.
- ac00222: Add `Toggle` — a single pressed/unpressed control (`@radix-ui/react-toggle`) for a standalone toolbar action ("show grid lines", "pin sidebar"), styled as a button rather than `Switch`'s track-and-thumb. For a set of related toggles, use `ToggleGroup` instead.

## 2.0.0

### Major Changes

- 10c1ee9: Fork of `@marcfs31/fors-design-system`, published under the new name `@marcfs31/fors-observability-design-system`. Every import path changes accordingly (`@marcfs31/fors-observability-design-system`, `/theme`, `/styles.css`, `/tailwind.css`, `/tailwind-preset`, `/fonts.css`); the base component API and the `--fors-*` token namespace are unchanged, so an app can swap the dependency and keep its markup.

### Minor Changes

- 414a3be: Add `AlertDialog` — a confirmation dialog for destructive or otherwise consequential actions (`@radix-ui/react-alert-dialog`). Unlike `Dialog`, it doesn't dismiss on an outside click and always returns focus to `AlertDialogCancel` on open, so a keyboard or screen-reader user lands on the safe choice first. `AlertDialogAction` defaults to the `danger` `Button` variant and accepts `variant`/`size` overrides for non-destructive confirmations.
- 414a3be: Add `AlertList` — "what's firing right now": active and recently resolved alerts ranked by recency, complementing `Timeline` (full history) and `StatusDot` (a single point-in-time health read). Severity is always printed as a word via `Badge`, never color alone; a resolved alert dims and gets a "Resolved" badge without hiding what it was. Renders `EmptyState` when there are no alerts, and defaults to a polite live region (opt out via `announce={false}` for a firehose feed).

  Also raises the JS bundle size-limit budget from 92 KB to 100 KB for this component-library-expansion batch (two new Radix deps — `alert-dialog`, `toggle-group` — plus several components composed from existing primitives).

- 414a3be: Add `BoxPlot` — a five-number summary (min/Q1/median/Q3/max) per category, for the latency/duration percentile spread `LineChart`/`BarChart` can't show. Like `LineChart` and unlike `BarChart`, its axis fits the data range rather than a zero baseline. Every box shares the neutral accent color (like `Histogram`/`Sparkline`) since it compares one metric's spread across categories, not several metrics against each other.
- 414a3be: Add `CalendarHeatmap` — a GitHub-contributions-style year-in-review activity calendar for incident or deploy frequency by day. Unlike `Heatmap` (a density matrix you've already arranged into rows/columns), it takes a flat, unordered list of `{ date, value }` days and buckets them into weeks (columns) and weekdays (rows) itself, then hands that grid to `Heatmap` — so it gets the same real `<table>`, five-step intensity scale, and legend. A date with no entry renders as an untracked (empty) cell, distinct from a tracked day whose count is zero.
- 414a3be: Add an `annotations` prop to `LineChart` and `BarChart` — SLO-threshold (`value`) or deploy-marker (`label`) reference lines drawn over the plot, in a `neutral`/`accent`/`warning`/`danger` tone. Every annotation's text is folded into the plot's visually hidden description, so a threshold or marker is never sighted-only information. The shared `ChartAnnotation` type and its tone-to-class mapping (`ANNOTATION_TONE_CLASSES`) are exported for building a custom plot on `ChartFrame` with the same annotation styling.

  (Note: this landed on `LineChart`/`BarChart` rather than `ChartFrame` itself — `ChartFrame` has no scale or category knowledge to place a reference line against, so the annotation geometry has to live where the scale does.)

  Also adds the `"warning (as text) on bg"` and `"danger (as text) on bg"` pairings to the token contrast test suite, covering the annotation labels' text color drawn directly on the plot background.

- 414a3be: Add `ComboChart` — bars and a line sharing one x-axis on independent scales (request volume + p99 latency, the APM-dashboard pattern), built on the same `ChartFrame`/`niceScale`/`barPath`/`linePath` geometry as `BarChart`/`LineChart`. Bar series always share a zero-baselined left axis; line series always share a right axis fit to their own data range; the visually hidden description calls out which series reads on which axis.

  Also exports `splitAtGaps` (the `null`-gap-splitting helper `LineChart` already used internally, now shared with `ComboChart`'s line series) alongside the rest of the chart math — no behavior change to `LineChart` itself.

- 414a3be: Add `Combobox` — a searchable, filterable single-select for lists too long to scan un-filtered, composed from the existing `Popover` + `Command` primitives (no new dependency). Pass `options`, `value`/`onValueChange`, and an `aria-label` for the trigger's accessible name (it replaces the visible placeholder/value text for assistive tech, so word it as the field's purpose rather than a repeat of the placeholder).
- 414a3be: Add `Drawer` — an edge-anchored panel for row detail, filters, or focused editing next to the content it relates to, built on the same `@radix-ui/react-dialog` primitive as `Dialog`. `DrawerContent` takes a logical `side` (`"start"` | `"end"`, default `"end"`) that flips the physical edge automatically under `dir="rtl"`, the same technique already used by `Sidebar`'s mobile drawer.
- 414a3be: Add `EmptyState` — placeholder content for a table, list, or search result with nothing to show (icon, title, description, action slot). Renders `role="status"` by default so a screen reader user is told the section resolved to empty; override via the `role` prop for a static (non-dynamic) empty section.
- 414a3be: Add `ErrorBudget` — an SLO burn-down bar showing how much of an error budget is spent, read as "how much is left." Unlike `Gauge` (a bounded metric with a caller-judged `tone`), the status here (`healthy`/`at-risk`/`critical`) derives automatically from `warningAt`/`dangerAt` thresholds, since "72% of your error budget is gone" means roughly the same thing in any context. The status word is always printed via `Badge`, never carried by color alone, and the whole thing is exposed as an ARIA `meter`.
- 414a3be: Add `FilterBar` — a faceted filter row for an observability dashboard header (service/env/status/whatever facets the data supports). Applied facets render as removable chips; "Add filter" opens a searchable, grouped list of everything not already applied, built on the same `Popover` + `Command` pairing `Combobox` uses. `filters` is controlled — the component owns only the chip/add-menu chrome, and the caller decides what applying a filter actually does.
- 414a3be: Add `Histogram` — distribution of one metric across ordered ranges (request-duration buckets, payload-size buckets), built on the same `ChartFrame`/`niceScale`/`barPath` geometry as `BarChart`. Bars sit edge to edge (a hairline gap) since ranges are continuous, unlike `BarChart`'s discrete, gapped categories, and it uses the neutral accent color (like `Sparkline`/`Gauge`) since there's only ever one metric to show.
- 414a3be: Add `JSONViewer` — a collapsible tree for structured data (log fields, trace/span attributes, request/response payloads). Every visible node is real text content, and collapsing a node removes its children from the DOM rather than hiding them, so assistive tech never lands on content the sighted view has hidden. It's a set of nested disclosure buttons (Tab + Enter/Space), not a full WAI-ARIA `tree` widget.

  Also adds the `"accent (as text) on bg"` pairing to the token contrast test suite, covering the node-toggle hover state's background.

- 414a3be: Add `Kbd` — a single keyboard key/shortcut token rendered as a native `<kbd>`, for use inside `CommandItem`, `Tooltip`, or anywhere a shortcut hint is shown. Compose several side by side for a multi-key shortcut (e.g. `⌘` + `K`).
- 414a3be: Add `ToggleGroup` / `ToggleGroupItem` — a segmented set of pressed-button options (view-mode switches, chart-type toggles, density controls), built on `@radix-ui/react-toggle-group`. `type="single"` exposes `role="radiogroup"`/`role="radio"` (same roles as `RadioGroup`, styled as a connected button row); `type="multiple"` exposes `role="toolbar"` with independently `aria-pressed` buttons. Arrow/Home/End move a roving focus between items; Space/Enter or a click selects.
- 55a5909: Two new component families for dashboards, and the tokens behind them.

  **Data visualization** — `LineChart` (lines or a single area, with gaps for missing samples), `BarChart` (grouped or stacked, zero-baselined), `Sparkline`, `BarList`, `DonutChart`, `Heatmap`, `Gauge`, plus `ChartFrame` / `ChartLegend` / `ChartTooltip` for building a plot this package doesn't ship. Everything is drawn as SVG from exported geometry helpers (`niceScale`, `linePath`, `areaPath`, `barPath`, `arcPath`, `project`, `polar`, `formatCompact`, `formatDuration`, `formatPercent`, `seriesFill`/`seriesStroke`/`seriesBg`) — no charting dependency is added.

  **Observability** — `StatCard`, `Delta`, `StatusDot`, `UptimeBar`, `LogStream`, `Timeline`, `TraceWaterfall`, `TimeRange`.

  **Tokens** — eight categorical chart-series slots, `--fors-viz-1` … `--fors-viz-8`, per theme, mapped into both Tailwind entries as `fill-viz-*` / `stroke-viz-*` / `bg-viz-*`. The slot order is the colorblind-safety mechanism (adjacent pairs clear the CVD and normal-vision separation floors in both themes) and is pinned, along with the 3:1 non-text contrast bar, by `src/tokens/__tests__/contrast.test.ts`.

  Accessibility notes worth knowing before using these: every plot also renders its data as a visually hidden table (`Heatmap` and `TraceWaterfall` _are_ real tables); the hover readout is reachable by keyboard (Arrow/Home/End, Escape to dismiss) and announced through a polite live region; and identity is never carried by color alone — legends, status words, delta arrows and printed values do that work.

- 10c1ee9: Ready the package for use as a dependency in Tailwind apps:

  - New `@marcfs31/fors-observability-design-system/tailwind.css` export — a Tailwind v4 `@theme` mapping so a consumer's own markup can use the token utilities (`bg-accent`, `text-fg-muted`, `rounded-md`, …).
  - New `@marcfs31/fors-observability-design-system/tailwind-preset` export — the same mapping as a Tailwind v3 preset (this repo's own config now consumes it). `tailwindcss` is declared as an optional peer.
  - `sideEffects` now lists the shipped `.css` files instead of `false`, so bundlers that honor the flag literally never tree-shake `import "@marcfs31/fors-observability-design-system/styles.css"`.
  - `./package.json` is exported for tooling that reads package metadata.
  - New `npm run test:consumer` (also a CI job): packs the tarball, installs it into a Next.js 16 / React 19 / Tailwind v4 fixture app and builds it, verifying the RSC `"use client"` boundary, the stylesheet surviving the bundler, and the `tailwind.css` mapping.

> Forked from [`@marcfs31/fors-design-system`](https://github.com/marcfs31/fors-design-system) at
> v1.4.1. Entries below that release are inherited history and name the old package; every version
> from 2.0.0 on is this package. The fork adds the data-visualization and observability component
> families and the `--fors-viz-*` series tokens — see the pending changesets.

## 1.4.1

### Patch Changes

- 4a54e50: RTL (`dir="rtl"`) support: hardcoded physical-direction classes (`text-left`, `ml-*`, `pl-*`/`pr-*`, `right-*`, `border-r`, `-ml-*`) across `Accordion`, `Table`, `Select`, `Dialog`, `Avatar`, and `Sidebar` are now logical-property equivalents (`text-start`, `ms-*`, `ps-*`/`pe-*`, `end-*`, `border-e`, `-ms-*`), and `Switch`'s thumb travel and `Sidebar`'s mobile-drawer slide animation now use explicit `rtl:`/`ltr:` variants. No API changes — purely internal styling.

  `Toast` is a deliberate, documented exception: its swipe-to-dismiss gesture and resting position are tied together by a hardcoded physical `swipeDirection`, and making that RTL-aware needs a runtime direction-detection mechanism this repo doesn't have yet. `DropdownMenu`/`Popover`/`Tooltip` needed no changes — their positioning already reads Radix's own viewport-relative `data-side`, not text direction.

  Storybook gains a `dir` toolbar (mirroring the existing theme toggle) and a `KitchenSinkRTL` story for whole-system verification.

## 1.4.0

### Minor Changes

- f979d2d: Add `Calendar`/`DatePicker` — a date grid built on `react-day-picker`, plus a compact popover-based date field for forms. Scoped to single-date selection for now; range selection is a documented follow-up.

  **Bundle budget raised from 70 KB to 85 KB** (brotli, `dist/index.js`): `react-day-picker` pulls in `date-fns`/`@date-fns/tz`, adding a real, measured ~18 KB even after tree-shaking. This is a deliberate trade-off for shipping a genuine date picker rather than a hand-rolled one — real usage after this release is ~80 KB, leaving headroom for incidental growth.

- 65591ff: Add `Sidebar`/`AppShell` — a page-shell layout primitive: `SidebarProvider`/`useSidebar` for shared collapsed/mobile-open state, `Sidebar` (a desktop icon-rail that collapses, or a mobile slide-in drawer built on Radix Dialog below the `md` breakpoint), `SidebarHeader`/`SidebarContent`/`SidebarFooter` layout slots, `SidebarNav`/`SidebarNavItem` for plain nav links, `SidebarTrigger`, and `AppShell`/`AppShellMain` for the page's flex container and its single `<main>` landmark. No new dependency — reuses `@radix-ui/react-dialog`, already in this package.
- 18e8d56: Add `Command`/`CommandDialog` — a searchable, filterable command palette built on `cmdk`, for use in place of `Select` when a list is long, filterable, or mixes action types. `CommandDialog` opens `Command` as a modal palette on top of this repo's own `Dialog`.

  `DialogContent` also gains an optional `hideClose` prop (default `false`, fully backward-compatible) for chrome-free modal surfaces like `CommandDialog` that rely on Escape to close instead of a visible close button.

## 1.3.0

### Minor Changes

- 25d6b8a: Three new components, filling gaps this repo's own usage kept surfacing:

  - **`Separator`** — a divider between sections of content, decorative by default (`decorative={false}` for the rare case it's the only signal of a semantic boundary).
  - **`Label`** — an accessible field label (wraps `@radix-ui/react-label`) so `htmlFor`/`id` correctly associates with any control, native or Radix-based — replaces every hand-rolled `<label className="...">` this design system's own stories had been using.
  - **`Collapsible`** (+ `CollapsibleTrigger`, `CollapsibleContent`) — a single show/hide section (a "Show more" toggle, an optional advanced-settings block); `Accordion` is built on the same Radix primitive but manages a whole group, this is the standalone version.

### Patch Changes

- 0fad059: Bump `tailwind-merge` to 3.6.0 (a runtime dependency of the `cn()` helper). Verified against the full test suite (unit + DOM snapshots + Storybook test runner) with no output changes — the class-merging behavior for every component is identical.

## 1.2.0

### Minor Changes

- ce3f210: Next.js App Router / React Server Components compatibility, and confirmed React 19 support.

  - The components entry (`@marcfs31/fors-design-system`) now ships a leading `"use client"` directive — every component is interactive (hooks / Radix), so the whole bundle is a client module usable directly inside an RSC tree, no wrapper `"use client"` file required.
  - New server-safe entry **`@marcfs31/fors-design-system/theme`**: `applyForsTheme`, `forsAntiFlashScript`, `FORS_THEMES`, `FORS_PALETTES`/`DARK_PALETTE`/`LIGHT_PALETTE`, and `cn` — no client directive, callable from a Server Component (e.g. a Next.js root layout). `publint`/`arethetypeswrong` validate it clean on every resolution mode, including legacy `node10` (via a root `theme/package.json` stub).
  - Type families are now CSS variables — `--fors-font-sans`, `--fors-font-heading`, `--fors-font-mono` — so a consumer can point them at `next/font` (or any self-hosted face) instead of the bundled Google Fonts import.
  - The shipped `styles.css` no longer makes a network call: the Google Fonts `@import` moved to a new optional **`@marcfs31/fors-design-system/fonts.css`** entry. Without either, text falls back to `system-ui` — never invisible, just not on-brand until fonts are wired up.
  - React 19: confirmed compatible — peer range (`>=18`) and the resolved Radix versions (`^19.0` in their own peer ranges) both support it; no removed API (`forwardRef`, etc.) is used.

  README gained "Using with Next.js" and "Fonts" sections.

## 1.1.0

### Minor Changes

- fab1ae2: Full accessibility (WCAG 2.1 AA) and responsiveness hardening pass across every component.

  Accessibility:

  - `Tabs` reworked to the complete WAI-ARIA Tabs pattern: roving `tabIndex`, Arrow/Home/End keyboard navigation with disabled-trigger skipping and wrapping, `id`↔`aria-controls`/`aria-labelledby` wiring between each trigger and panel, focusable panels, and an `orientation` prop.
  - `Input` / `Textarea`: `hint` text is now linked to the control via `aria-describedby` so screen readers announce it.
  - `Alert`: new `assertive` prop switching the live region to `role="alert"`; defaults to `true` when `variant="danger"`.
  - `Avatar`: keeps its accessible name (via `role="img"` + `aria-label`) when the image fails and it falls back to initials; `AvatarGroup`'s "+N" bubble is now labelled ("N more").
  - `Checkbox` / `RadioGroup` controls enlarged to a 24×24px minimum touch target (WCAG 2.5.8); `Dialog` and `Toast` close buttons padded to a real hit area.
  - `Breadcrumb` current-page marker simplified to a plain `aria-current="page"` span (no more `role="link"` + `aria-disabled`).
  - `Table` column headers now carry `scope="col"`.

  Responsiveness — every component is usable at 320/768/1280px with no forced horizontal page scroll:

  - `Dialog` content capped to `w-[calc(100vw-2rem)]` with internal scroll for tall content; `DropdownMenu`, `Popover`, and `Toast` viewports capped to the viewport width with a gutter.
  - `Table` renders inside a horizontal scroll container instead of overflowing the page.
  - `Tabs` list scrolls when it overflows; `Pagination` wraps.

- a9e461a: Ship a CommonJS build alongside ESM. `require("@marcfs31/fors-design-system")` now resolves to `dist/index.cjs` with its own `dist/index.d.cts` types; ESM consumers are unchanged (`dist/index.js`). `publint` and `@arethetypeswrong/cli` both pass clean across `node10`, `node16` (CJS and ESM), and bundler resolution. Non-breaking — nothing was removed or renamed.

### Patch Changes

- a9e461a: Fix `--fors-fg-muted` (dark theme) failing WCAG AA contrast on `ink-surface` and `ink-surface-2` — it only cleared 4.5:1 against the base `ink-bg`. Nudged `#75818d` → `#8a95a1` (now ≥ 5.2:1 on every ink surface). Caught by the new full-browser axe pass on the Table header, which renders `text-fg-muted` on `ink-surface-2`. The token-contrast test now checks `fg-muted` and `fg-secondary` against all three ink surfaces so this can't regress. `ToastDescription` also switched from `opacity-90` (which tipped `text-danger` under AA in the danger toast) to solid `text-fg-secondary`.

## 1.0.0

### Major Changes

- First stable release of the Fors design system.

  - Dark (default) and light theming via `[data-theme]`, with WCAG AA contrast enforced by an automated test against the real token values — no `ThemeProvider` required; consumers get `applyForsTheme`/`forsAntiFlashScript` instead.
  - 30 components: typography (`Heading`, `Text`), forms (`Button` with a built-in `loading` state, `Input`, `Textarea`, `Checkbox`, `RadioGroup`, `Switch`, `Select`, `Slider`), overlays (`Dialog`, `DropdownMenu`, `Popover`, `Tooltip`, `Toast`/`Toaster`/`useToast`), feedback and data (`Alert`, `Badge`, `Avatar`/`AvatarGroup`, `Spinner`, `Progress`, `Skeleton`, `Card`, `Table`, `Tabs`, `Accordion`), and navigation (`Breadcrumb`, `Pagination`).
  - Overlay and select components are built on Radix UI primitives for correct focus/keyboard/ARIA behavior, restyled entirely through this package's own Tailwind token vocabulary.
  - Real open/close motion on every overlay via `tailwindcss-animate`, driven by Radix's own `data-state`/`data-side` attributes, and automatically neutralized under `prefers-reduced-motion`.
  - Every component ships a Storybook story (with autodocs generating a props table from its TypeScript types) and a co-located test covering behavior and accessibility (`vitest-axe`).
  - Production build via `tsup` (ESM, typed) plus a separately compiled `styles.css` (no Preflight reset, safe alongside a consumer's own Tailwind base).
  - CI (typecheck, lint, test, build, build-storybook) and a Changesets-driven release process publishing to GitHub Packages.
