import { defineConfig } from "tsup";
import { forsightTreeshakePlugin } from "./scripts/forsight-treeshake-plugin";

export default defineConfig({
  // `index` is the client component bundle (its source starts with
  // "use client", which tsup preserves per-entry); `theme` and `chart` are
  // server-safe utilities entries with no directive; `tailwind-preset` is
  // the Tailwind v3 JS preset (build-time only, never imported by app code).
  // The library's own v4 build loads it via `@config` rather than as a
  // runtime import.
  entry: {
    index: "src/index.ts",
    theme: "src/theme-entry.ts",
    chart: "src/chart-entry.ts",
    "tailwind-preset": "src/tailwind-preset.ts",
  },
  // ESM is the primary target; the CJS build is a compatibility shim so
  // `require()` from CommonJS tooling still resolves (validated by
  // `npm run test:package`).
  format: ["esm", "cjs"],
  dts: true,
  sourcemap: true,
  clean: true,
  external: ["react", "react-dom"],
  // Annotate forwardRef/memo/cva as PURE, strip displayName from the compile
  // input, and name render functions — without this, a named import of a
  // single component pulls in the entire barrel (~75 KB brotli).
  esbuildPlugins: [forsightTreeshakePlugin()],
});
