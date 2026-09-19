import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

/**
 * Every `XxxProps` interface/type a component file defines and exports is
 * meant to be part of this package's public API — so a consumer can write
 * `interface MyWrapperProps extends SomeComponentProps` without reaching
 * past the barrel into `src/components/*`. It's easy for a new Props type
 * to go in without a matching line in `src/index.ts` (nothing else notices
 * — the component itself still works, just its type doesn't travel with
 * it). This pins the invariant: every exported `*Props` in `src/components`
 * is re-exported from the barrel, unless it's named here on purpose.
 *
 * Add to ALLOWLIST only for a Props type that's deliberately internal (e.g.
 * one that only makes sense composed with a sibling in the same file) —
 * name it and say why, so the gap is a decision, not an oversight.
 */
const ALLOWLIST: readonly string[] = [];

const componentsDir = path.join(__dirname, "../components");
const indexTs = readFileSync(path.join(__dirname, "../index.ts"), "utf8");

const componentFiles = readdirSync(componentsDir).filter(
  (file) => file.endsWith(".tsx") && !file.endsWith(".test.tsx") && !file.endsWith(".stories.tsx")
);

const PROPS_EXPORT_RE = /^export (?:interface|type) (\w+Props)\b/gm;

/** Every exported `*Props` name defined in `src/components/*.tsx`, mapped to the file that defines it. */
const definedProps = componentFiles.flatMap((file) => {
  const content = readFileSync(path.join(componentsDir, file), "utf8");
  return [...content.matchAll(PROPS_EXPORT_RE)].map((match) => ({ name: match[1], file }));
});

const isReExported = (name: string) => new RegExp(`\\btype ${name}\\b`).test(indexTs);

describe("src/index.ts prop-type parity", () => {
  it("found at least one exported *Props type to check (guards against a broken glob)", () => {
    expect(definedProps.length).toBeGreaterThan(20);
  });

  it("re-exports every component-defined *Props type not on the allowlist", () => {
    const missing = definedProps
      .filter(({ name }) => !ALLOWLIST.includes(name))
      .filter(({ name }) => !isReExported(name));

    expect(missing).toEqual([]);
  });

  it("keeps the allowlist free of names that are already exported (no stale entries)", () => {
    const staleAllowlistEntries = ALLOWLIST.filter((name) => isReExported(name));
    expect(staleAllowlistEntries).toEqual([]);
  });
});
