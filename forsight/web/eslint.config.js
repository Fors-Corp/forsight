import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";

// Mirrors the root eslint.config.js's rule choices (see that file for the
// reasoning behind each one). This dashboard is a separate npm project from
// the design system — it consumes @marcfs31/forsight as a pinned published
// dependency rather than the workspace source (CLAUDE.md, "two artifacts")
// — so it gets its own config instead of sharing the root's, the same way
// it already has its own tsconfig.json and vitest.config.ts.
//
// typescript-eslint does not yet support TypeScript 7's native compiler
// (https://github.com/typescript-eslint/typescript-eslint/issues/10940), so
// this project's "typescript" devDependency is aliased to the official
// TS6-compatibility package for tooling, while `tsc` itself stays on real
// TypeScript 7 under a separate alias — see package.json and the "For
// Marc" note in this change's PR description.
export default tseslint.config(
  {
    ignores: ["dist", "node_modules"],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    plugins: { "react-hooks": reactHooks },
    rules: {
      // v7's `recommended` enables the React Compiler rule set. Keep the
      // classic hooks rules the root project relies on instead of turning
      // on the compiler pipeline here.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",
      "@typescript-eslint/no-unused-vars": ["warn", { argsIgnorePattern: "^_" }],
    },
  }
);
