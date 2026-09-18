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
});
