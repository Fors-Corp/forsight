/**
 * vitest-axe@0.1.0 ships its type augmentation as a global `Vi` namespace
 * (the pre-Vitest-2 convention) instead of `declare module "vitest"`, so it
 * doesn't merge with the `Assertion` interface this vitest version actually
 * uses. Declare the matcher ourselves instead of patching the dependency —
 * mirrors the root design-system package's src/test-types.d.ts.
 */
import "vitest";

declare module "vitest" {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars -- must match the real Assertion<T>'s arity to merge with it
  interface Assertion<T = unknown> {
    toHaveNoViolations(): void;
  }
  interface AsymmetricMatchersContaining {
    toHaveNoViolations(): void;
  }
}
