import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";
import prettierConfig from "eslint-config-prettier";

export default tseslint.config(
  {
    // .claude/worktrees holds throwaway git worktrees (full repo copies with
    // their own node_modules) created by Claude Code sessions — never lint them.
    ignores: [
      "dist",
      "storybook-static",
      "node_modules",
      ".storybook",
      ".claude/worktrees",
      // graft's generated CommonJS shims (statusline + hooks) — vendored
      // agent wiring this repo doesn't author or maintain, rewritten by
      // `graft init` / `graft upgrade`.
      ".claude/helpers",
      "fixtures/**/.next",
      // forsight and forseer are separate Go modules (golangci-lint covers
      // their .go source). forsight/web is a separate npm project nested
      // inside forsight/ — it has its own eslint.config.js mirroring these
      // same rules, run via its own `npm run lint`, not this one. The
      // embedded dashboard build (forsight/internal/api/webdist/) is
      // checked-in minified JS/CSS — linting it as this project's
      // TypeScript is what broke this exclusion's absence (1600+
      // "'document' is not defined" errors from a single-line bundle).
      "forsight",
      "forseer",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    plugins: { "react-hooks": reactHooks },
    rules: {
      // v7's `recommended` enables the React Compiler rule set. Keep the
      // classic hooks rules that this repo already relied on (v5 intent);
      // do not turn on the compiler pipeline here.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",
      "@typescript-eslint/no-unused-vars": ["warn", { argsIgnorePattern: "^_" }],
    },
  },
  {
    // Plain Node scripts (the smoke test, any future release/CI scripts) —
    // not part of the browser-facing component library, so they get Node's
    // globals instead.
    files: ["scripts/**/*.mjs"],
    languageOptions: {
      globals: {
        console: "readonly",
        process: "readonly",
        TextDecoder: "readonly",
        Buffer: "readonly",
      },
    },
  },
  // Must stay last — disables ESLint stylistic rules that would otherwise
  // fight Prettier's own formatting decisions.
  prettierConfig
);
