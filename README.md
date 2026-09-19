# Forsight

[![CI](https://github.com/Fors-Corp/forsight/actions/workflows/ci.yml/badge.svg)](https://github.com/Fors-Corp/forsight/actions/workflows/ci.yml)
[![Storybook](https://img.shields.io/badge/Storybook-live-16C7B0)](https://fors-corp.github.io/forsight/)

One repo, two independently versioned artifacts:

| Artifact                          | What it is                                                               | Built by        | Versioned as          |
| --------------------------------- | ------------------------------------------------------------------------ | --------------- | --------------------- |
| [`@fors-corp/forsight`](#install) | Observability design system (npm, GitHub Packages)                       | `npm run build` | Changesets → `vX.Y.Z` |
| [`forsight/`](forsight/README.md) | One Go binary: collectors, in-memory store, HTTP API, embedded dashboard | `make build`    | `forsight-vX.Y.Z`     |

**Forseer** (`forseer/`) is the AI/ML module compiled into the agent. It scores the stream; the dashboard renders those scores with the design-system components in [Storybook → Forseer](https://fors-corp.github.io/forsight/?path=/docs/forsight-forseer--docs). See [`forseer/MODELS.md`](forseer/MODELS.md) for what a model is and how each one is scored.

**[Browse the component library →](https://fors-corp.github.io/forsight/)** (Storybook, deployed from `main`)

The published version tracks [`CHANGELOG.md`](CHANGELOG.md) and the [npm package page](https://github.com/Fors-Corp/forsight/pkgs/npm/forsight) on GitHub Packages rather than a number pinned in this file — nothing stamps a version into the README at release time, so a hardcoded number here goes stale the moment a Changesets release runs. The package build is Tailwind **v4**; a Tailwind v3 `tailwind-preset` is still exported for older consumers. React 18 and 19 · Next.js App Router, [RSC-ready](#using-with-nextjs) · ESM + CJS.

This package is the observability continuation of [`@marcfs31/fors-design-system`](https://github.com/Fors-Corp/fors-design-system): same tokens and base components, plus [data visualization](#data-visualization) (plain SVG, no charting library) and [observability](#observability) primitives. Import paths are `@fors-corp/forsight`.

**Brand.** _Fors_ is Swedish/Norwegian for rapids — force, flow, clarity — and foresight is what an observability tool is for. Dark-first: near-black ink, Rapids Teal accent, Spark Amber secondary, Inter for UI, Space Grotesk for headings. Light theme included — see [Theming](#theming).

## Install

The package was published as `@marcfs31/forsight` through 4.2.0 and is `@fors-corp/forsight` from 5.0.0 onward, following the repository's move to the Fors-Corp organization. The old name is frozen at 4.2.0 and receives no further releases.

Published to GitHub Packages under the `@fors-corp` scope. Add to the consuming repo's `.npmrc`:

```
@fors-corp:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${NPM_TOKEN}
```

`NPM_TOKEN` is a classic GitHub personal access token with `read:packages` scope — set it as an environment variable locally and in your deployment platform (e.g. a Vercel project environment variable). Then:

```bash
npm install @fors-corp/forsight
```

## Usage

```tsx
import { Button, Card, CardHeader, CardTitle, CardContent } from "@fors-corp/forsight";
import "@fors-corp/forsight/styles.css";
import "@fors-corp/forsight/fonts.css"; // optional — see Fonts below

function Example() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Deploy your project</CardTitle>
      </CardHeader>
      <CardContent>
        <Button>Deploy</Button>
      </CardContent>
    </Card>
  );
}
```

Package entries:

| Import                                | What it is                                                                                                                                                                                                                                              |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `@fors-corp/forsight`                 | The components. Every one is interactive (hooks / Radix), so the bundle ships a `"use client"` directive — usable directly inside a React Server Component tree with no wrapper.                                                                        |
| `@fors-corp/forsight/theme`           | Server-safe utilities (`applyForsightTheme`, `forsightAntiFlashScript`, the raw palettes, `cn`). No `"use client"` — call these from a Server Component (e.g. a Next.js root layout).                                                                   |
| `@fors-corp/forsight/chart`           | The pure chart maths and number formatters that back the chart components (`niceScale`, `linePath`, `arcPath`, `formatCompact`, and friends). No `"use client"` — call these from a Server Component to precompute marks or format a value server-side. |
| `@fors-corp/forsight/styles.css`      | **Required.** Design tokens + compiled component styles. Tailwind's component/utility layers only (no Preflight reset, no network calls) — safe alongside an app that runs its own Tailwind base and its own CSP.                                       |
| `@fors-corp/forsight/tailwind.css`    | Optional, Tailwind **v4** apps: `@theme` mapping so your own markup can use the token utilities (`bg-accent`, `text-fg-muted`, `rounded-md`, …). See [Tailwind](#tailwind).                                                                             |
| `@fors-corp/forsight/tailwind-preset` | Optional, Tailwind **v3** apps: the same mapping as a preset for `tailwind.config`. See [Tailwind](#tailwind).                                                                                                                                          |
| `@fors-corp/forsight/fonts.css`       | Optional: loads the brand faces from Google Fonts. See [Fonts](#fonts).                                                                                                                                                                                 |

Runtime dependencies (Radix primitives, `cmdk`, `react-day-picker`, `class-variance-authority`, `tailwind-merge`) are regular `dependencies` of the package and install with it; only `react` / `react-dom` (18 or 19) are peers you provide. Tailwind is **not** required to use the components — `styles.css` is precompiled.

### Tailwind

The components are already styled by `styles.css`; you don't need Tailwind to use them. If your app _does_ use Tailwind and you want the Forsight token vocabulary available in your own markup, add the matching integration:

**Tailwind v4** — in your global stylesheet:

```css
@import "tailwindcss";
@import "@fors-corp/forsight/tailwind.css";
```

**Tailwind v3** — in `tailwind.config.ts`:

```ts
import forsightPreset from "@fors-corp/forsight/tailwind-preset";

export default {
  presets: [forsightPreset],
  content: ["./app/**/*.{ts,tsx}", "./components/**/*.{ts,tsx}"],
};
```

Either way you then get `bg-ink-surface`, `text-fg-secondary`, `border-ink-border`, `bg-accent` / `text-accent-fg`, `bg-danger-subtle`, `rounded-md`, `shadow-md`, `font-heading`, `duration-fast` / `duration-base`, and so on — every value resolves through the `--forsight-*` custom properties, so it follows the [theme switch](#theming) at runtime.

### Fonts

`styles.css` references the brand families (`Inter`, `Space Grotesk`) by name but doesn't load them — nothing in the shipped CSS makes a network call. Provide the faces yourself:

- **Quickest**: `import "@fors-corp/forsight/fonts.css"` — loads both from Google Fonts.
- **Next.js / production**: use `next/font` and point the tokens at it (see [Using with Next.js](#using-with-nextjs)).
- **Self-hosted**: set `--forsight-font-sans` / `--forsight-font-heading` on `:root` to your own stack — every component reads the font through those two CSS variables.

Without any of the above, text falls back to `system-ui` — never invisible, just not on-brand.

## Theming

Dark is the default — nothing to configure. For an app that also needs light mode, import the theme utilities from the **server-safe entry** and own the switcher/persistence yourself:

```tsx
import { applyForsightTheme, forsightAntiFlashScript } from "@fors-corp/forsight/theme";

// In your root layout's <head>, before hydration:
<script dangerouslySetInnerHTML={{ __html: forsightAntiFlashScript() }} />;

// Wherever the user toggles theme:
applyForsightTheme("light");
```

## Using with Next.js

App Router, Server Components, `next/font` — all supported.

```tsx
// app/layout.tsx (Server Component — no "use client" needed here)
import "@fors-corp/forsight/styles.css";
import { forsightAntiFlashScript } from "@fors-corp/forsight/theme";
import { Inter, Space_Grotesk } from "next/font/google";

const inter = Inter({ subsets: ["latin"], variable: "--font-inter" });
const spaceGrotesk = Space_Grotesk({ subsets: ["latin"], variable: "--font-space-grotesk" });

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${inter.variable} ${spaceGrotesk.variable}`}
      style={
        {
          "--forsight-font-sans": "var(--font-inter)",
          "--forsight-font-heading": "var(--font-space-grotesk)",
        } as React.CSSProperties
      }
    >
      <head>
        {/* Applies a stored theme before paint — no flash of the wrong theme. */}
        <script dangerouslySetInnerHTML={{ __html: forsightAntiFlashScript() }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
```

Then use components anywhere — in a Server Component tree directly, or in your own `"use client"` files:

```tsx
import { Button } from "@fors-corp/forsight";

export default function Page() {
  return <Button>Deploy</Button>; // no "use client" needed in this file
}
```

Don't add `import "@fors-corp/forsight/fonts.css"` alongside `next/font` — that would load the same families twice, once render-blocking from Google and once self-hosted.

## Agent

The Go agent lives in [`forsight/`](forsight/README.md). A bare `forsight run` collects host, process, and Docker metrics, scrapes well-known local Prometheus exporters, listens for StatsD on `:8125`, and receives OTLP metrics/traces/logs. Storage is in-process (`MemoryStore`, default 1h retention). Optional bearer auth (`--auth-token` / `FORSIGHT_AUTH_TOKEN`). The dashboard is a small React app on this design system, embedded at `forsight/internal/api/webdist/`.

```bash
cd forsight
make test
make build-go    # no Node
```

## Development

```bash
npm install
npm run storybook       # component playground at localhost:6006 — Forseer section is Forsight/Forseer
npm test                # unit + accessibility + token-contrast tests
npm run typecheck
npm run build            # dist/{index,theme,tailwind-preset}.{js,cjs,d.ts,d.cts} + dist/{styles,tailwind,fonts}.css
npm run build-storybook  # static Storybook build
```

Package-level checks (all run in CI):

```bash
npm run smoke          # imports the built dist/ and checks the public API + shipped CSS
npm run test:package   # publint + are-the-types-wrong on the packed tarball (ESM/CJS/types resolution)
npm run test:consumer  # packs the tarball, installs it into fixtures/next-consumer (Next.js 16 App Router,
                       # React 19, Tailwind v4) and runs `next build` — proves the RSC boundary, the
                       # stylesheet surviving the bundler, and the tailwind.css @theme mapping
npm run size           # bundle-size budget
```

## Releasing

This repo uses [Changesets](https://github.com/changesets/changesets):

```bash
npm run changeset          # after a change worth releasing — prompts for a bump + summary
npm run version-packages   # bumps package.json + writes CHANGELOG.md
npm run release            # builds and publishes to GitHub Packages
git push --follow-tags
```

## Component set

### Base

Typography: `Heading`, `Text`, `Label`. Forms: `Button` (built-in `loading`), `Input`, `Textarea`, `Checkbox`, `RadioGroup`, `Switch`, `Toggle`/`ToggleGroup`, `Select`, `Combobox`, `MultiSelect`, `Slider`, `Calendar`/`DatePicker`. Overlays: `Dialog`, `AlertDialog`, `Drawer`, `DropdownMenu`, `Popover`, `HoverCard`, `Tooltip`, `Toast`/`Toaster`, `Command`/`CommandDialog`. Feedback & data: `Alert`, `Badge`, `Avatar`/`AvatarGroup`, `Spinner`, `Progress`, `Skeleton`, `Stepper`, `Card`, `Table`, `Tabs`, `Accordion`, `Collapsible`, `Separator`, `EmptyState`, `JSONViewer`, `CodeBlock`, `CopyButton`, `Kbd`, `ScrollArea`. Navigation: `Breadcrumb`, `Pagination`, `FilterBar`, `Sidebar`/`AppShell`. Every component supports `dir="rtl"`, with one documented exception: `Toaster`'s swipe-to-dismiss gesture and slide animation are a physical direction, not a text-direction concern, so they stay fixed regardless of `dir` (see the comment atop `Toast.tsx`).

Overlay/select components sit on [Radix UI](https://www.radix-ui.com/) for focus and keyboard behavior, then this repo's token classes. Overlay motion is `tailwindcss-animate` on Radix `data-state`/`data-side`, and collapses under `prefers-reduced-motion`.

### Data visualization

`LineChart` · `BarChart` · `ComboChart` · `Sparkline` · `BarList` · `DonutChart` · `Funnel` · `Heatmap` · `CalendarHeatmap` · `Histogram` · `BoxPlot` · `Gauge` · `ChartFrame` / `ChartLegend` / `ChartTooltip`.

Charts are SVG from this repo's geometry helpers (`niceScale`, `linePath`, `areaPath`, `barPath`, `arcPath`, `formatCompact`, … — all exported). No charting library in the tree.

Three rules:

- **Series colors are eight fixed slots**, `--forsight-viz-1` … `--forsight-viz-8`, never cycled. Adjacent slots are the CVD-safe pairs a stack or legend puts side by side. A ninth series is drawn neutral — fold the tail into "Other" or use small multiples.
- **A chart is a picture and a table.** Every plot also renders a visually hidden `<table>` (or, for `Heatmap` and `TraceWaterfall`, _is_ a real table). `Sparkline` summarizes instead — count, low, high, latest.
- **The cursor is keyboard-reachable.** Hover and Arrow/Home/End read a point; Escape dismisses. Identity always has a text carrier beside the color.

### Observability

`StatCard` · `Delta` · `StatusDot` · `UptimeBar` · `AlertList` · `ErrorBudget` · `LogStream` · `Timeline` · `TraceWaterfall` · `TimeRange` · `FilterBar`.

### Forseer (AI / ML)

Forseer does not add components. It scores the agent's stream and lands each finding on a component that already exists. The Storybook **Forseer** section is that mapping, including a full incident composition.

| Kind           | Detector                                             | Component               |
| -------------- | ---------------------------------------------------- | ----------------------- |
| `anomaly`      | Welford rolling z-score (3σ / 5σ)                    | `AlertList`             |
| `changepoint`  | CUSUM on the same z-scores                           | `Timeline`              |
| `log_burst`    | Drain-lite templates + short-window volume           | `BarList` + `LogStream` |
| `slow_span`    | Per `(service, span name)` duration z-score          | `TraceWaterfall`        |
| `culprit`      | Join a `host.cpu` anomaly with `process.cpu.percent` | `Table`                 |
| Grok narrative | SpaceXAI `grok-4.5` when `XAI_API_KEY` is set        | `Card` + `Text`         |
| NL filter      | Phrase → facets (`error logs from checkout`)         | `FilterBar`             |
| SLO burn       | Error-log rate vs a 1% SLO                           | `ErrorBudget`           |

Statistical detectors need no API key. Details: [`forseer/README.md`](forseer/README.md).

Every component has a Storybook story (autodocs from its TypeScript types) and a co-located test covering behavior and accessibility (`vitest-axe`).
