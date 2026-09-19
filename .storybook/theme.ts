import { create, type ThemeVars } from "storybook/theming";

/**
 * Storybook's own chrome (sidebar, toolbar, Docs pages) is themed separately
 * from the stories inside it — Storybook renders its UI from a ThemeVars
 * object, not from our CSS tokens. These two themes mirror the dark/light
 * palettes in src/styles/tokens.css so the app around a story matches the
 * story. Keep the values in sync with tokens.css when a color changes.
 */
const shared = {
  brandTitle: "Forsight",
  brandUrl: "https://fors-corp.github.io/forsight/",
  fontBase: '"Inter", system-ui, -apple-system, sans-serif',
  fontCode: '"JetBrains Mono", ui-monospace, "SFMono-Regular", monospace',
  appBorderRadius: 8,
  inputBorderRadius: 8,
};

export const forsightDark: ThemeVars = create({
  ...shared,
  base: "dark",
  colorPrimary: "#16c7b0",
  colorSecondary: "#16c7b0",
  appBg: "#0b0f14",
  appContentBg: "#12181f",
  appPreviewBg: "#0b0f14",
  appBorderColor: "#232c36",
  textColor: "#eaf0f5",
  textMutedColor: "#8a95a1",
  textInverseColor: "#0b0f14",
  barBg: "#12181f",
  barTextColor: "#a7b4c0",
  barHoverColor: "#eaf0f5",
  barSelectedColor: "#16c7b0",
  buttonBg: "#1a222b",
  buttonBorder: "#232c36",
  booleanBg: "#1a222b",
  booleanSelectedBg: "#113330",
  inputBg: "#0b0f14",
  inputBorder: "#232c36",
  inputTextColor: "#eaf0f5",
});

export const forsightLight: ThemeVars = create({
  ...shared,
  base: "light",
  colorPrimary: "#0b6c5e",
  colorSecondary: "#0b6c5e",
  appBg: "#f7f9fb",
  appContentBg: "#ffffff",
  appPreviewBg: "#f7f9fb",
  appBorderColor: "#d7dee4",
  textColor: "#0d1420",
  textMutedColor: "#5a6570",
  textInverseColor: "#ffffff",
  barBg: "#ffffff",
  barTextColor: "#3c4650",
  barHoverColor: "#0d1420",
  barSelectedColor: "#0b6c5e",
  buttonBg: "#eef2f5",
  buttonBorder: "#d7dee4",
  booleanBg: "#eef2f5",
  booleanSelectedBg: "#e3f5f2",
  inputBg: "#ffffff",
  inputBorder: "#d7dee4",
  inputTextColor: "#0d1420",
});
