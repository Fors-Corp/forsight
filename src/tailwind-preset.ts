import type { Config } from "tailwindcss";
import animate from "tailwindcss-animate";

/**
 * Tailwind v3 preset, exported as "@marcfs31/forsight/tailwind-preset".
 *
 * Maps the Forsight tokens (CSS custom properties in src/styles/tokens.css) into
 * the `bg-*` / `text-*` / `border-*` / `rounded-*` / `shadow-*` utility
 * vocabulary the components are built with, so a consumer app on Tailwind v3
 * can use the same class names in its own markup:
 *
 *   // tailwind.config.ts
 *   import forsightPreset from "@marcfs31/forsight/tailwind-preset";
 *   export default { presets: [forsightPreset], content: [...] };
 *
 * This repo's own build loads the same preset via `@config` in
 * src/styles/package.css and globals.css. Tailwind v4 consumers use the
 * "@marcfs31/forsight/tailwind.css" export instead. Keep both in sync.
 */
const forsightPreset: Partial<Config> = {
  theme: {
    extend: {
      colors: {
        ink: {
          bg: "var(--forsight-ink-bg)",
          surface: "var(--forsight-ink-surface)",
          "surface-2": "var(--forsight-ink-surface-2)",
          border: "var(--forsight-ink-border)",
          "border-subtle": "var(--forsight-ink-border-subtle)",
        },
        fg: {
          DEFAULT: "var(--forsight-fg)",
          secondary: "var(--forsight-fg-secondary)",
          muted: "var(--forsight-fg-muted)",
        },
        accent: {
          DEFAULT: "var(--forsight-accent)",
          hover: "var(--forsight-accent-hover)",
          active: "var(--forsight-accent-active)",
          subtle: "var(--forsight-accent-subtle)",
          fg: "var(--forsight-accent-fg)",
        },
        spark: {
          DEFAULT: "var(--forsight-spark)",
          hover: "var(--forsight-spark-hover)",
          subtle: "var(--forsight-spark-subtle)",
          fg: "var(--forsight-spark-fg)",
        },
        danger: {
          DEFAULT: "var(--forsight-danger)",
          subtle: "var(--forsight-danger-subtle)",
          fg: "var(--forsight-danger-fg)",
        },
        success: {
          DEFAULT: "var(--forsight-success)",
          subtle: "var(--forsight-success-subtle)",
          fg: "var(--forsight-success-fg)",
        },
        warning: {
          DEFAULT: "var(--forsight-warning)",
          subtle: "var(--forsight-warning-subtle)",
          fg: "var(--forsight-warning-fg)",
        },
        "focus-ring": "var(--forsight-focus-ring)",
        "viz-1": "var(--forsight-viz-1)",
        "viz-2": "var(--forsight-viz-2)",
        "viz-3": "var(--forsight-viz-3)",
        "viz-4": "var(--forsight-viz-4)",
        "viz-5": "var(--forsight-viz-5)",
        "viz-6": "var(--forsight-viz-6)",
        "viz-7": "var(--forsight-viz-7)",
        "viz-8": "var(--forsight-viz-8)",
      },
      fontFamily: {
        heading: "var(--forsight-font-heading)",
        sans: "var(--forsight-font-sans)",
        mono: "var(--forsight-font-mono)",
      },
      borderRadius: {
        sm: "var(--forsight-radius-sm)",
        md: "var(--forsight-radius-md)",
        lg: "var(--forsight-radius-lg)",
        xl: "var(--forsight-radius-xl)",
      },
      transitionDuration: {
        fast: "var(--forsight-duration-fast)",
        base: "var(--forsight-duration-base)",
      },
      boxShadow: {
        sm: "var(--forsight-shadow-sm)",
        md: "var(--forsight-shadow-md)",
        lg: "var(--forsight-shadow-lg)",
        "focus-ring": "0 0 0 1px var(--forsight-ink-bg), 0 0 0 3px var(--forsight-focus-ring)",
      },
      keyframes: {
        // tailwindcss-animate's fade/zoom/slide utilities cover every other
        // overlay; Accordion animates a measured height instead, which
        // needs its own keyframes reading Radix's own CSS variable.
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
        // Collapsible is a different Radix primitive with its own height
        // variable — Accordion's keyframes above don't apply to it.
        "collapsible-down": {
          from: { height: "0" },
          to: { height: "var(--radix-collapsible-content-height)" },
        },
        "collapsible-up": {
          from: { height: "var(--radix-collapsible-content-height)" },
          to: { height: "0" },
        },
      },
      animation: {
        "accordion-down": "accordion-down var(--forsight-duration-base) ease-out",
        "accordion-up": "accordion-up var(--forsight-duration-base) ease-out",
        "collapsible-down": "collapsible-down var(--forsight-duration-base) ease-out",
        "collapsible-up": "collapsible-up var(--forsight-duration-base) ease-out",
      },
    },
  },
  plugins: [animate],
};

export default forsightPreset;
