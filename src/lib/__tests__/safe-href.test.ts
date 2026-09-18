import { afterEach, describe, expect, it, vi } from "vitest";
import { isSafeHref } from "../safe-href";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("isSafeHref", () => {
  it("rejects a javascript: URI", () => {
    expect(isSafeHref("javascript:alert(document.cookie)")).toBe(false);
  });

  it("rejects a data: URI", () => {
    expect(isSafeHref("data:text/html,<script>alert(1)</script>")).toBe(false);
  });

  it("rejects a vbscript: URI", () => {
    expect(isSafeHref("vbscript:msgbox(1)")).toBe(false);
  });

  it("rejects a string new URL can't parse even against the page origin", () => {
    expect(isSafeHref("http://")).toBe(false);
  });

  it("accepts a relative URL", () => {
    expect(isSafeHref("/traces?route=checkout")).toBe(true);
  });

  it("accepts a bare fragment", () => {
    expect(isSafeHref("#trace-checkout")).toBe(true);
  });

  it("accepts an absolute http: URL", () => {
    expect(isSafeHref("http://example.com/traces")).toBe(true);
  });

  it("accepts an absolute https: URL", () => {
    expect(isSafeHref("https://example.com/traces")).toBe(true);
  });

  it("accepts a protocol-relative URL, resolved against the current (https) origin", () => {
    vi.stubGlobal("location", { origin: "https://forsight.example" });
    expect(isSafeHref("//example.com/traces")).toBe(true);
  });

  // The three components that call this are inside the package's "use client"
  // boundary, which Next.js still renders on the server — where `location`
  // does not exist. Reading it unguarded threw a ReferenceError the `catch`
  // swallowed, marking every href unsafe during SSR and rehydrating a
  // BarList row from <div> to <a>. The verdict has to be origin-independent.
  describe("with no document (SSR)", () => {
    it("still accepts a relative URL", () => {
      vi.stubGlobal("location", undefined);
      expect(isSafeHref("/traces?route=checkout")).toBe(true);
    });

    it("still accepts an absolute https: URL", () => {
      vi.stubGlobal("location", undefined);
      expect(isSafeHref("https://example.com/traces")).toBe(true);
    });

    it("still rejects a javascript: URI", () => {
      vi.stubGlobal("location", undefined);
      expect(isSafeHref("javascript:alert(document.cookie)")).toBe(false);
    });

    it("still rejects a data: URI", () => {
      vi.stubGlobal("location", undefined);
      expect(isSafeHref("data:text/html,<script>alert(1)</script>")).toBe(false);
    });
  });
});
