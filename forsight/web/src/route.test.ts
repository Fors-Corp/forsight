import { describe, expect, it } from "vitest";
import { routeFromHash } from "./route";

// routeFromHash's own doc comment promises this test — anything the hash
// doesn't explicitly name (including a near-miss like a trailing slash or a
// missing leading slash) must fall back to "overview" rather than throwing
// or landing on a blank page.
describe("routeFromHash", () => {
  it.each(["", "#", "#/"])("maps %j to overview", (hash) => {
    expect(routeFromHash(hash)).toBe("overview");
  });

  it('maps "#/models" to models', () => {
    expect(routeFromHash("#/models")).toBe("models");
  });

  it.each(["#/models/", "#models", "#/nope"])("falls back to overview for %j", (hash) => {
    expect(routeFromHash(hash)).toBe("overview");
  });
});
