import { describe, expect, it } from "vitest";
import { FORSIGHT_PALETTES, type ForsightPalette } from "../palettes";

const AA_NORMAL_TEXT = 4.5;
/** WCAG 1.4.11 — graphical objects (chart marks carry meaning, but are not text). */
const AA_NON_TEXT = 3;

function hexToRgb(hex: string): [number, number, number] {
  const clean = hex.replace("#", "");
  const r = parseInt(clean.slice(0, 2), 16);
  const g = parseInt(clean.slice(2, 4), 16);
  const b = parseInt(clean.slice(4, 6), 16);
  return [r, g, b];
}

function channelToLinear(c: number): number {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
}

function relativeLuminance(hex: string): number {
  const [r, g, b] = hexToRgb(hex).map(channelToLinear);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrastRatio(hexA: string, hexB: string): number {
  const lA = relativeLuminance(hexA);
  const lB = relativeLuminance(hexB);
  const lighter = Math.max(lA, lB);
  const darker = Math.min(lA, lB);
  return (lighter + 0.05) / (darker + 0.05);
}

function rgbToHex(rgb: [number, number, number]): string {
  return `#${rgb.map((c) => Math.round(c).toString(16).padStart(2, "0")).join("")}`;
}

/**
 * Composite a partly transparent color over an opaque one, the way a browser
 * paints a translucent box-shadow against the surface behind it. An alpha of 1
 * is the identity, so a solid token passes through unchanged.
 */
function blendOver(hex: string, alpha: number, backdrop: string): string {
  const fg = hexToRgb(hex);
  const bg = hexToRgb(backdrop);
  return rgbToHex(
    [0, 1, 2].map((i) => alpha * fg[i] + (1 - alpha) * bg[i]) as [number, number, number]
  );
}

/** Every fg/bg pairing an existing component actually renders. */
function pairsFor(p: ForsightPalette): Array<[string, string, string]> {
  return [
    ["fg on bg", p.fg, p.bg],
    ["fg on surface", p.fg, p.surface],
    ["fg-secondary on bg", p.fgSecondary, p.bg],
    ["fg-secondary on surface", p.fgSecondary, p.surface],
    ["fg-secondary on surface-2", p.fgSecondary, p.surface2],
    // fg-muted is used on every ink surface (Table header on surface-2,
    // captions on surface), not just bg — check all three.
    ["fg-muted on bg", p.fgMuted, p.bg],
    ["fg-muted on surface", p.fgMuted, p.surface],
    ["fg-muted on surface-2", p.fgMuted, p.surface2],

    ["accent-fg on accent", p.accentFg, p.accent],
    ["accent-fg on accent-hover", p.accentFg, p.accentHover],
    ["accent-fg on accent-active", p.accentFg, p.accentActive],
    ["accent (as text) on surface", p.accent, p.surface],
    ["accent (as text) on accent-subtle", p.accent, p.accentSubtle],
    ["accent (as text) on surface-2", p.accent, p.surface2],
    // JSONViewer's node-toggle hover state (hover:text-accent) sits on bg,
    // not surface — check that pairing too.
    ["accent (as text) on bg", p.accent, p.bg],

    ["spark-fg on spark", p.sparkFg, p.spark],
    ["spark-fg on spark-hover", p.sparkFg, p.sparkHover],
    ["spark (as text) on spark-subtle", p.spark, p.sparkSubtle],

    ["danger-fg on danger", p.dangerFg, p.danger],
    ["danger (as text) on danger-subtle", p.danger, p.dangerSubtle],
    // LineChart/BarChart annotation lines draw danger/warning tone text
    // directly on the plot's bg, not inside a -subtle badge.
    ["danger (as text) on bg", p.danger, p.bg],

    ["success-fg on success", p.successFg, p.success],
    ["success (as text) on success-subtle", p.success, p.successSubtle],
    // CopyButton's copied-state icon draws text-success directly on bg.
    ["success (as text) on bg", p.success, p.bg],

    ["warning-fg on warning", p.warningFg, p.warning],
    ["warning (as text) on warning-subtle", p.warning, p.warningSubtle],
    ["warning (as text) on bg", p.warning, p.bg],
  ];
}

describe("Forsight token contrast (WCAG AA, 4.5:1)", () => {
  for (const [themeName, palette] of Object.entries(FORSIGHT_PALETTES)) {
    describe(`${themeName} theme`, () => {
      for (const [label, fg, bg] of pairsFor(palette)) {
        it(`${label} passes AA`, () => {
          const ratio = contrastRatio(fg, bg);
          expect(ratio).toBeGreaterThanOrEqual(AA_NORMAL_TEXT);
        });
      }
    });
  }
});

/**
 * The focus ring is the only thing that says which control has keyboard focus,
 * so WCAG 1.4.11 governs it: 3:1 against the color it is drawn against. It is
 * a box-shadow outside the control, so it composites against the page behind
 * the control — the bare bg, a Card's surface, or surface-2 — and all three
 * have to clear the bar at the ring's real alpha. A translucent ring is what
 * failed here before (45% teal measured 2.0-2.7:1 in both themes), which is
 * exactly the class of bug this file exists to catch, so the alpha is part of
 * the mirrored token and part of the assertion.
 */
describe("Forsight focus ring contrast (WCAG 1.4.11, 3:1 non-text)", () => {
  for (const [themeName, palette] of Object.entries(FORSIGHT_PALETTES)) {
    describe(`${themeName} theme`, () => {
      for (const [surfaceName, surface] of [
        ["bg", palette.bg],
        ["surface", palette.surface],
        ["surface-2", palette.surface2],
      ] as const) {
        it(`focus ring on ${surfaceName} clears 3:1`, () => {
          const painted = blendOver(palette.focusRing, palette.focusRingAlpha, surface);
          expect(contrastRatio(painted, surface)).toBeGreaterThanOrEqual(AA_NON_TEXT);
        });
      }
    });
  }
});

/**
 * Chart series colors are graphical objects, not text, so the bar is WCAG
 * 1.4.11's 3:1 against the surface they are drawn on — never 4.5:1, and never
 * used *as* text (chart labels wear the fg/fg-secondary/fg-muted tokens).
 *
 * The three light-theme slots below sit under 3:1 on the white surface. That is
 * the data-viz "relief" allowance, and it holds only because every chart
 * component in this library ships a legend plus a screen-reader data table, so
 * a series is never identified by its color alone. The list is pinned here so a
 * future palette edit can't quietly widen it — adding a slot to it is a
 * deliberate decision, not a passing test.
 */
const LIGHT_VIZ_RELIEF = new Set(["#1baf7a", "#eda100", "#e87ba4"]);

describe("Forsight chart series contrast (WCAG 1.4.11, 3:1 non-text)", () => {
  for (const [themeName, palette] of Object.entries(FORSIGHT_PALETTES)) {
    describe(`${themeName} theme`, () => {
      palette.viz.forEach((color, i) => {
        // A chart can be drawn straight on the page (bg) or inside a Card
        // (surface); both have to hold up.
        for (const [surfaceName, surface] of [
          ["bg", palette.bg],
          ["surface", palette.surface],
        ] as const) {
          it(`viz-${i + 1} on ${surfaceName} clears 3:1 (or is a documented relief slot)`, () => {
            const ratio = contrastRatio(color, surface);
            if (themeName === "light" && LIGHT_VIZ_RELIEF.has(color)) {
              expect(ratio).toBeGreaterThanOrEqual(2);
            } else {
              expect(ratio).toBeGreaterThanOrEqual(AA_NON_TEXT);
            }
          });
        }
      });

      it("has eight distinct slots", () => {
        expect(new Set(palette.viz).size).toBe(8);
      });
    });
  }
});
