/**
 * Flat TS mirror of the color values in src/styles/tokens.css, used by
 * src/tokens/__tests__/contrast.test.ts to check WCAG contrast without a DOM.
 * Keep in sync by hand when a color changes in tokens.css — the test is the
 * guardrail against a value that silently fails contrast, not against the
 * two files drifting apart in general.
 */

export interface ForsightPalette {
  bg: string;
  surface: string;
  surface2: string;
  fg: string;
  fgSecondary: string;
  fgMuted: string;
  accent: string;
  accentHover: string;
  accentActive: string;
  accentSubtle: string;
  accentFg: string;
  spark: string;
  sparkHover: string;
  sparkSubtle: string;
  sparkFg: string;
  danger: string;
  dangerSubtle: string;
  dangerFg: string;
  success: string;
  successSubtle: string;
  successFg: string;
  warning: string;
  warningSubtle: string;
  warningFg: string;
  /**
   * The focus ring, as the box-shadow actually paints it: a color and the
   * alpha it is drawn at. The ring sits outside the control, so it composites
   * against whatever surface is behind it — the alpha is part of the rendered
   * color and has to be mirrored here for the contrast test to see the truth.
   */
  focusRing: string;
  focusRingAlpha: number;
  /** Categorical chart series, slots 1-8 in fixed assignment order. */
  viz: readonly [string, string, string, string, string, string, string, string];
}

export const DARK_PALETTE: ForsightPalette = {
  bg: "#0b0f14",
  surface: "#12181f",
  surface2: "#1a222b",
  fg: "#eaf0f5",
  fgSecondary: "#a7b4c0",
  fgMuted: "#8a95a1",
  accent: "#16c7b0",
  accentHover: "#3fdbc5",
  accentActive: "#10a090",
  accentSubtle: "#113330",
  accentFg: "#04231f",
  spark: "#ff8a3d",
  sparkHover: "#ffa766",
  sparkSubtle: "#3a2414",
  sparkFg: "#23130a",
  danger: "#ff5470",
  dangerSubtle: "#3a1420",
  dangerFg: "#2a0a10",
  success: "#2fd97a",
  successSubtle: "#10301f",
  successFg: "#04170c",
  warning: "#ffc93d",
  warningSubtle: "#332a10",
  warningFg: "#241d08",
  focusRing: "#16c7b0",
  focusRingAlpha: 1,
  viz: ["#3987e5", "#d95926", "#199e70", "#c98500", "#d55181", "#008300", "#9085e9", "#e66767"],
};

export const LIGHT_PALETTE: ForsightPalette = {
  bg: "#f7f9fb",
  surface: "#ffffff",
  surface2: "#eef2f5",
  fg: "#0d1420",
  fgSecondary: "#3c4650",
  fgMuted: "#5a6570",
  accent: "#0b6c5e",
  accentHover: "#0d7c69",
  accentActive: "#094f45",
  accentSubtle: "#e3f5f2",
  accentFg: "#ffffff",
  spark: "#a04a08",
  sparkHover: "#8a3f06",
  sparkSubtle: "#fdecd9",
  sparkFg: "#ffffff",
  danger: "#c22a45",
  dangerSubtle: "#fbe4e8",
  dangerFg: "#ffffff",
  success: "#157a44",
  successSubtle: "#e2f6ea",
  successFg: "#ffffff",
  warning: "#8a6200",
  warningSubtle: "#fdf3d9",
  warningFg: "#ffffff",
  focusRing: "#0b6c5e",
  focusRingAlpha: 1,
  viz: ["#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4", "#008300", "#4a3aa7", "#e34948"],
};

export const FORSIGHT_PALETTES = { dark: DARK_PALETTE, light: LIGHT_PALETTE } as const;
