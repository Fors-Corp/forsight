import { configureAxe } from "vitest-axe";

/**
 * jsdom has no real layout/paint engine, so axe-core's `color-contrast`
 * check either logs noisy `getComputedStyle`/canvas "not implemented"
 * errors or hangs outright measuring pseudo-element text — a well-known
 * jsdom limitation, not a real accessibility signal. Mirrors the root
 * design-system package's src/test-utils/axe.ts (same rationale, same
 * disabled rule); this dashboard has no token-level contrast test of its
 * own to fall back on since it consumes @marcfs31/forsight's tokens rather
 * than defining any. Every other axe rule (ARIA roles/names, labels,
 * heading order, live regions, structure) still runs normally.
 */
export const axe = configureAxe({
  rules: { "color-contrast": { enabled: false } },
});
