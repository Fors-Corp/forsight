#!/usr/bin/env node
/**
 * End-to-end consumer check: packs this package exactly as `npm publish`
 * would, installs the tarball into fixtures/next-consumer (a real Next.js
 * App Router + React 19 + Tailwind v4 app), runs `next build`, and asserts
 * on the output. This is the only layer that exercises the package the way
 * an app does — through a bundler, an RSC boundary, and a consumer's own
 * Tailwind — so it catches what publint/attw/smoke can't: a stylesheet a
 * bundler drops, a "use client" boundary that doesn't hold in an RSC tree,
 * or a tailwind.css @theme mapping that no longer compiles.
 *
 * Runs after `npm run build` (see `npm run test:consumer`). Needs network
 * access the first time to install the fixture's own dependencies.
 */
import { execFileSync } from "node:child_process";
import { existsSync, readdirSync, readFileSync, rmSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const fixture = path.join(root, "fixtures/next-consumer");
const npm = process.platform === "win32" ? "npm.cmd" : "npm";

function run(cmd, args, cwd) {
  console.log(`\n$ ${cmd} ${args.join(" ")}  (in ${path.relative(root, cwd) || "."})`);
  execFileSync(cmd, args, { cwd, stdio: "inherit" });
}

let failures = 0;
function check(label, fn) {
  try {
    if (fn() === false) throw new Error("assertion returned false");
    console.log(`  ✓ ${label}`);
  } catch (err) {
    failures++;
    console.error(`  ✗ ${label}\n    ${err.message}`);
  }
}

/** Every file under `dir` (recursive) whose name matches `re`. */
function findFiles(dir, re, out = []) {
  if (!existsSync(dir)) return out;
  for (const entry of readdirSync(dir)) {
    const full = path.join(dir, entry);
    if (statSync(full).isDirectory()) findFiles(full, re, out);
    else if (re.test(entry)) out.push(full);
  }
  return out;
}

// 1. Pack the package exactly as publish would (honours `files`/`exports`).
for (const old of findFiles(fixture, /-forsight-.*\.tgz$/)) rmSync(old);
run(npm, ["pack", "--silent", "--pack-destination", fixture], root);
const [tarball] = findFiles(fixture, /^fors-corp-forsight-.*\.tgz$/);
if (!tarball) throw new Error("npm pack produced no tarball");

// 2. Install the fixture's own deps (Next/React/Tailwind), then the tarball
//    on top without saving it — the fixture's package.json stays clean.
run(npm, ["install", "--no-audit", "--no-fund", "--loglevel=error"], fixture);
run(npm, ["install", "--no-save", "--no-audit", "--no-fund", "--loglevel=error", tarball], fixture);

// 3. Production build.
rmSync(path.join(fixture, ".next"), { recursive: true, force: true });
run(path.join(fixture, "node_modules/.bin/next"), ["build"], fixture);

// 4. Assert on the artifact.
console.log("\nChecking built consumer app...\n");
const cssFiles = findFiles(path.join(fixture, ".next/static"), /\.css$/);
const css = cssFiles.map((f) => readFileSync(f, "utf8")).join("\n");
check("build emitted at least one CSS bundle", () => cssFiles.length > 0);
check("library styles.css survived the bundler (design tokens present)", () =>
  css.includes("--forsight-accent")
);
check("library component styles survived the bundler", () =>
  css.includes("--forsight-ink-surface")
);
check("consumer Tailwind v4 generated a token utility from tailwind.css (@theme)", () =>
  /\.bg-accent\s*\{[^}]*var\(--forsight-accent\)/.test(css)
);
check("consumer Tailwind v4 generated a token radius utility", () =>
  /\.rounded-md\s*\{[^}]*var\(--forsight-radius-md\)/.test(css)
);

// Next writes the prerendered "/" route as .next/server/app/index.html.
const htmlFiles = findFiles(path.join(fixture, ".next/server/app"), /^index\.html$/);
const html = htmlFiles.map((f) => readFileSync(f, "utf8")).join("\n");
check("page was statically prerendered", () => htmlFiles.length > 0);
check("library Button rendered inside a Server Component tree", () =>
  html.includes('data-testid="fixture-button"')
);
check(
  "prerendered Button carries its Tailwind classes",
  () =>
    /data-testid="fixture-button"[^>]*class="[^"]*bg-accent/.test(html) ||
    /class="[^"]*bg-accent[^"]*"[^>]*data-testid="fixture-button"/.test(html)
);
check("chart series tokens survived the bundler", () => css.includes("--forsight-viz-1"));
check("library StatCard (chart family) rendered inside a Server Component tree", () =>
  html.includes('data-testid="fixture-statcard"')
);
check("server-safe theme entry ran in the root layout (anti-flash script inlined)", () =>
  html.includes('localStorage.getItem("forsight-theme")')
);

console.log("");
if (failures > 0) {
  console.error(`${failures} consumer check(s) failed.`);
  process.exit(1);
}
console.log("Consumer fixture built and verified.");
