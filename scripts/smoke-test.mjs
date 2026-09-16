#!/usr/bin/env node
/**
 * Verifies the actual compiled artifact in dist/ — not src/ — imports
 * cleanly and exposes the expected public API. Unit tests import from src
 * via Vite/Vitest's own transform pipeline, which can't catch a build-only
 * regression (a broken tsup config, an export that got tree-shaken away, a
 * malformed exports map, a styles.css that never got the Tailwind pass).
 * Run after `npm run build`, before anything gets published.
 */
import { createRequire } from "node:module";
import { existsSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const require = createRequire(import.meta.url);
const root = path.dirname(fileURLToPath(import.meta.url)) + "/..";
const distIndex = path.join(root, "dist/index.js");
const distCjs = path.join(root, "dist/index.cjs");
const distTypes = path.join(root, "dist/index.d.ts");
const distCts = path.join(root, "dist/index.d.cts");
const distTheme = path.join(root, "dist/theme.js");
const distThemeCjs = path.join(root, "dist/theme.cjs");
const distStyles = path.join(root, "dist/styles.css");
const distFonts = path.join(root, "dist/fonts.css");
const distTailwindCss = path.join(root, "dist/tailwind.css");
const distPreset = path.join(root, "dist/tailwind-preset.js");

let failures = 0;
function check(label, fn) {
  try {
    const result = fn();
    if (result === false) throw new Error("assertion returned false");
    console.log(`  ✓ ${label}`);
  } catch (err) {
    failures++;
    console.error(`  ✗ ${label}\n    ${err.message}`);
  }
}

console.log("Smoke-testing dist/ artifact...\n");

check("dist/index.js exists (ESM)", () => existsSync(distIndex));
check("dist/index.cjs exists (CJS)", () => existsSync(distCjs));
check("dist/index.d.ts exists", () => existsSync(distTypes));
check("dist/index.d.cts exists", () => existsSync(distCts));
check("dist/theme.js exists (server-safe entry)", () => existsSync(distTheme));
check("dist/theme.cjs exists", () => existsSync(distThemeCjs));
check("dist/theme.d.ts exists", () => existsSync(path.join(root, "dist/theme.d.ts")));
check("dist/styles.css exists", () => existsSync(distStyles));
check("dist/fonts.css exists", () => existsSync(distFonts));
check("dist/tailwind.css exists (Tailwind v4 @theme entry)", () => existsSync(distTailwindCss));
check("dist/tailwind-preset.js exists (Tailwind v3 preset entry)", () => existsSync(distPreset));
check("dist/tailwind-preset.cjs exists", () =>
  existsSync(path.join(root, "dist/tailwind-preset.cjs"))
);
check("dist/tailwind-preset.d.ts exists", () =>
  existsSync(path.join(root, "dist/tailwind-preset.d.ts"))
);

const mod = await import(path.resolve(distIndex));
const cjs = require(path.resolve(distCjs));
const theme = await import(path.resolve(distTheme));

check("CJS build exposes the same exports as ESM", () => {
  const esmKeys = Object.keys(mod).sort();
  const cjsKeys = Object.keys(cjs).sort();
  const missing = esmKeys.filter((k) => !cjsKeys.includes(k));
  if (missing.length) throw new Error(`CJS missing: ${missing.join(", ")}`);
  return true;
});

check('components entry is a "use client" module', () => {
  const head = readFileSync(distIndex, "utf8").slice(0, 200);
  if (!/^\s*["']use client["'];/.test(head))
    throw new Error("missing leading 'use client' directive");
  return true;
});
check('theme entry has NO "use client" directive', () => {
  const head = readFileSync(distTheme, "utf8").slice(0, 200);
  return !head.includes("use client");
});
check("theme entry exports the server-safe utilities", () => {
  return (
    typeof theme.applyForsightTheme === "function" &&
    typeof theme.forsightAntiFlashScript === "function" &&
    Array.isArray(theme.FORSIGHT_THEMES) &&
    typeof theme.FORSIGHT_PALETTES === "object" &&
    typeof theme.cn === "function"
  );
});
check("theme entry is NOT re-exported from the components entry", () => {
  return mod.applyForsightTheme === undefined && mod.FORSIGHT_PALETTES === undefined;
});
check("forsightAntiFlashScript() returns a non-empty string", () => {
  return (
    typeof theme.forsightAntiFlashScript() === "string" &&
    theme.forsightAntiFlashScript().length > 0
  );
});
check("FORSIGHT_THEMES contains dark and light", () => {
  return theme.FORSIGHT_THEMES.includes("dark") && theme.FORSIGHT_THEMES.includes("light");
});

const EXPECTED_COMPONENT_EXPORTS = [
  "Button",
  "Badge",
  "Input",
  "Textarea",
  "Card",
  "Alert",
  "Avatar",
  "AvatarGroup",
  "Tabs",
  "Heading",
  "Text",
  "Checkbox",
  "RadioGroup",
  "RadioGroupItem",
  "Switch",
  "Toggle",
  "ToggleGroup",
  "ToggleGroupItem",
  "Select",
  "Combobox",
  "MultiSelect",
  "Dialog",
  "AlertDialog",
  "Drawer",
  "DropdownMenu",
  "Tooltip",
  "Toaster",
  "Spinner",
  "Table",
  "Progress",
  "Popover",
  "HoverCard",
  "Accordion",
  "Slider",
  "Skeleton",
  "CopyButton",
  "CodeBlock",
  "Stepper",
  "Breadcrumb",
  "Pagination",
  "FilterBar",
  "Separator",
  "Label",
  "Kbd",
  "EmptyState",
  "JSONViewer",
  "Collapsible",
  "ScrollArea",
  "Command",
  "CommandDialog",
  "Calendar",
  "DatePicker",
  "DateRangePicker",
  "SidebarProvider",
  "Sidebar",
  "SidebarTrigger",
  "AppShell",
  "AppShellMain",
  // Data visualization
  "ChartFrame",
  "ChartLegend",
  "ChartTooltip",
  "LineChart",
  "BarChart",
  "BarList",
  "Funnel",
  "Sparkline",
  "DonutChart",
  "Heatmap",
  "Histogram",
  "BoxPlot",
  "ComboChart",
  "CalendarHeatmap",
  "Gauge",
  // Observability
  "StatCard",
  "Delta",
  "StatusDot",
  "UptimeBar",
  "AlertList",
  "ErrorBudget",
  "LogStream",
  "Timeline",
  "TraceWaterfall",
  "TimeRange",
];

for (const name of EXPECTED_COMPONENT_EXPORTS) {
  check(`exports "${name}"`, () => mod[name] !== undefined);
}

check("exports cn helper", () => {
  const skip = false;
  return typeof mod.cn === "function" && mod.cn("a", skip && "b", "c") === "a c";
});

const css = readFileSync(distStyles, "utf8");
check(
  "styles.css was actually processed by Tailwind (no literal @tailwind directives left)",
  () => !css.includes("@tailwind")
);
check("styles.css contains compiled component styles", () => css.includes("--forsight-accent"));
check("styles.css includes overlay motion from tailwindcss-animate", () =>
  css.includes("animate-in")
);
check("styles.css includes duration-fast", () => css.includes("duration-fast"));
check("styles.css makes no network calls (no webfont @import)", () => !css.includes("@import"));
check("styles.css is non-trivial in size", () => statSync(distStyles).size > 1000);

const fontsCss = readFileSync(distFonts, "utf8");
check("fonts.css loads the brand faces from Google Fonts", () =>
  /@import url\("https:\/\/fonts\.googleapis\.com\/css2\?family=Inter/.test(fontsCss)
);

// The two Tailwind integration surfaces must expose the same token names —
// a color added to one and not the other would silently diverge per consumer.
const tailwindCss = readFileSync(distTailwindCss, "utf8");
const preset = (await import(path.resolve(distPreset))).default;
check("tailwind.css is a Tailwind v4 @theme block", () => /@theme inline\s*\{/.test(tailwindCss));
check("tailwind-preset default-exports a preset with theme.extend + plugins", () => {
  return typeof preset.theme?.extend?.colors === "object" && preset.plugins?.length === 1;
});
check("tailwind.css and tailwind-preset expose the same color utilities", () => {
  const flatten = (obj, prefix = "") =>
    Object.entries(obj).flatMap(([k, v]) =>
      typeof v === "object"
        ? flatten(v, `${prefix}${k}-`)
        : [k === "DEFAULT" ? prefix.slice(0, -1) : `${prefix}${k}`]
    );
  const presetColors = flatten(preset.theme.extend.colors).sort();
  const cssColors = [...tailwindCss.matchAll(/--color-([a-z0-9-]+):/g)].map((m) => m[1]).sort();
  if (presetColors.join() !== cssColors.join())
    throw new Error(`preset: ${presetColors.join(",")}\n    css: ${cssColors.join(",")}`);
  return true;
});

// bundleNamedImports builds `import { ...names } from dist/index.js` the way
// a consumer's bundler would — react external, everything else resolved from
// node_modules so esbuild can read each package's `sideEffects` and drop what
// nothing reaches. (Marking @radix-ui/* external instead would force esbuild
// to keep every `import "@radix-ui/..."` for possible side effects, and even
// a bare Button would carry nineteen Radix specifiers.) The metafile is the
// exact record of which node_modules packages contributed bytes.
function bundleNamedImports(names) {
  const esbuild = require("esbuild");
  const { brotliCompressSync } = require("node:zlib");
  const { mkdtempSync, writeFileSync: write, rmSync } = require("node:fs");
  const { tmpdir } = require("node:os");
  const { join } = require("node:path");
  const dir = mkdtempSync(join(tmpdir(), "forsight-treeshake-"));
  try {
    const entry = join(dir, "entry.mjs");
    const list = names.join(", ");
    write(
      entry,
      `import { ${list} } from ${JSON.stringify(path.resolve(distIndex))};\nconsole.log(${list});\n`
    );
    const result = esbuild.buildSync({
      entryPoints: [entry],
      bundle: true,
      write: false,
      format: "esm",
      platform: "browser",
      external: ["react", "react-dom"],
      logLevel: "silent",
      treeShaking: true,
      minify: true,
      metafile: true,
    });
    const buf = result.outputFiles[0].contents;
    const output = Object.values(result.metafile.outputs)[0];
    const bundledPackages = [
      ...new Set(
        Object.keys(output.inputs)
          .map((p) => p.match(/node_modules\/((?:@[^/]+\/)?[^/]+)/)?.[1])
          .filter(Boolean)
      ),
    ].sort();
    return {
      text: new TextDecoder().decode(buf),
      brotli: brotliCompressSync(buf).length,
      bundledPackages,
    };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

const fullBarrelBrotli = () => {
  const { brotliCompressSync } = require("node:zlib");
  return brotliCompressSync(readFileSync(distIndex)).length;
};

// Named-import tree-shaking: a consumer `import { Button }` must not pull the
// entire barrel. Before the PURE/displayName fix this sat near the full
// dist/index.js size-limit budget; after, it must land dramatically below.
check("named-import tree-shaking keeps Button far below the full barrel", () => {
  const { text, brotli } = bundleNamedImports(["Button"]);
  const fullBrotli = fullBarrelBrotli();
  // Hard ceiling: must be well under half the full barrel and under 25 KB.
  // Pre-fix was ~75–110 KB brotli for this same import.
  if (brotli >= fullBrotli * 0.5) {
    throw new Error(`Button import brotli ${brotli} is not << full barrel ${fullBrotli}`);
  }
  if (brotli > 25_000) {
    throw new Error(`Button import brotli ${brotli} exceeds 25 KB ceiling`);
  }
  for (const leaked of ["AlertDialog", "BarChart", "CalendarHeatmap", "CommandInput"]) {
    if (text.includes(leaked)) {
      throw new Error(`tree-shaken Button bundle still contains ${leaked}`);
    }
  }
  console.log(
    `    (Button named-import brotli: ${brotli} B; full index.js brotli: ${fullBrotli} B)`
  );
  return true;
});

// The observability dashboard (forsight/web) draws with exactly these eight
// and nothing that composes a Radix primitive. Its embedded bundle is the
// artifact the tree-shaking win was measured on, so this is the path that
// must stay light: no @radix-ui package may contribute a byte, no Radix
// marker (data-radix-*, the radix- id prefix) may survive into the output,
// and the whole set stays under a ceiling with headroom over today's size.
check("the dashboard's chart path bundles no Radix", () => {
  const chartPath = [
    "ChartFrame",
    "LineChart",
    "BarChart",
    "ComboChart",
    "Sparkline",
    "StatCard",
    "StatusDot",
    "Table",
  ];
  const { text, brotli, bundledPackages } = bundleNamedImports(chartPath);
  const radix = bundledPackages.filter((name) => name.startsWith("@radix-ui/"));
  if (radix.length > 0) {
    throw new Error(`chart path bundles Radix: ${radix.join(", ")}`);
  }
  if (/radix/i.test(text)) {
    throw new Error("chart path output still contains a Radix identifier");
  }
  // 14.5 KB brotli at 4.0.1; the ceiling leaves a third of headroom.
  if (brotli > 20_000) {
    throw new Error(`chart path brotli ${brotli} exceeds 20 KB ceiling`);
  }
  console.log(
    `    (chart path brotli: ${brotli} B; bundled packages: ${bundledPackages.join(", ")})`
  );
  return true;
});

check("dist keeps named forwardRef render functions for DevTools", () => {
  const js = readFileSync(distIndex, "utf8");
  if ((js.match(/\.displayName\s*=/g) || []).length > 0) {
    throw new Error("dist/index.js still contains displayName assignments");
  }
  // esbuild renames `function Button` → `function Button2` when the outer
  // binding is also `var Button` in the bundled scope. The suffix still
  // beats Anonymous for React DevTools (it reads render.name).
  for (const name of ["Button", "Spinner", "Badge", "Card", "Input"]) {
    if (!new RegExp(`function ${name}\\d*\\(`).test(js)) {
      throw new Error(`dist/index.js missing named function ${name}`);
    }
  }
  if (!(js.match(/\/\* @__PURE__ \*\/ (?:React\d*\.)?forwardRef/g) || []).length) {
    throw new Error("dist/index.js missing @__PURE__ forwardRef annotations");
  }
  return true;
});

console.log("");
if (failures > 0) {
  console.error(`${failures} smoke-test check(s) failed.`);
  process.exit(1);
}
console.log("All smoke-test checks passed.");
