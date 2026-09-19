import "@testing-library/jest-dom/vitest";
// vitest-axe@0.1.0's own type augmentation targets a pre-Vitest-2 global `Vi`
// namespace and doesn't merge with this Vitest version's `Assertion`
// interface — see src/test-types.d.ts for the real augmentation. Its root
// "matchers" subpath also re-exports types with `export type *`, which makes
// `toHaveNoViolations` unusable as a value from there — import the real
// (non-type-only) declaration straight from dist instead. Mirrors the root
// design-system package's own vitest.setup.ts.
import { toHaveNoViolations } from "vitest-axe/dist/matchers.js";
import { afterEach, expect } from "vitest";
import { cleanup } from "@testing-library/react";

expect.extend({ toHaveNoViolations });

// RTL's own auto-cleanup only registers when it detects test-framework
// globals (afterEach on `globalThis`) — this project runs with
// `test.globals: false` and explicit vitest imports, so it never fires
// without this (mirrors the root design-system package's vitest.setup.ts).
afterEach(() => {
  cleanup();
});

/**
 * jsdom doesn't implement these — the design-system's Radix-based overlay
 * primitives (FilterBar's popover, among others rendered by App) call them
 * during pointer interaction and layout, so a render crashes without a
 * no-op polyfill even when nothing here asserts on them directly.
 */
if (!window.HTMLElement.prototype.hasPointerCapture) {
  window.HTMLElement.prototype.hasPointerCapture = () => false;
}
if (!window.HTMLElement.prototype.setPointerCapture) {
  window.HTMLElement.prototype.setPointerCapture = () => {};
}
if (!window.HTMLElement.prototype.releasePointerCapture) {
  window.HTMLElement.prototype.releasePointerCapture = () => {};
}
if (!window.HTMLElement.prototype.scrollIntoView) {
  window.HTMLElement.prototype.scrollIntoView = () => {};
}
if (!("ResizeObserver" in window)) {
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  // @ts-expect-error test-only polyfill
  window.ResizeObserver = ResizeObserverMock;
}
