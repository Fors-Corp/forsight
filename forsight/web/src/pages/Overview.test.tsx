import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import Overview, { coveredSpanMs, offeredTimeRanges, probeUptime } from "./Overview";
import type { Metric } from "../api";

function iso(ms: number): string {
  return new Date(ms).toISOString();
}

function metric(name: string, value: number, ms: number, labels: Record<string, string>): Metric {
  return { name, value, timestamp: iso(ms), labels };
}

const CHECKOUT = { name: "checkout", url: "https://checkout.example.com" };

describe("probeUptime", () => {
  it("returns an empty array when the metrics carry no probe.* samples", () => {
    const metrics: Metric[] = [metric("host.cpu.percent", 12, 1000, {})];
    expect(probeUptime(metrics, 0, 2000)).toEqual([]);
  });

  it("tiles [since, now] into equal, oldest-first buckets — a boundary sample opens the next bucket, and 'now' itself lands in the last one", () => {
    const metrics: Metric[] = [
      metric("probe.http.up", 1, 0, CHECKOUT), // bucket 0, start
      metric("probe.http.up", 1, 999, CHECKOUT), // bucket 0, end
      metric("probe.http.up", 1, 1000, CHECKOUT), // bucket 1, start (boundary)
      metric("probe.http.up", 1, 4000, CHECKOUT), // bucket 3 == now
      // bucket 2 ([2000, 3000)) gets no sample at all
    ];
    const [target] = probeUptime(metrics, 0, 4000, 4);
    expect(target.segments).toHaveLength(4);
    expect(target.segments[0].status).toBe("operational");
    expect(target.segments[1].status).toBe("operational");
    expect(target.segments[2]).toMatchObject({ status: "unknown", detail: "no checks" });
    expect(target.segments[3].status).toBe("operational");
  });

  it("marks a bucket operational when every probe.http.up sample in it is up", () => {
    const metrics: Metric[] = [
      metric("probe.http.up", 1, 100, CHECKOUT),
      metric("probe.http.up", 1, 200, CHECKOUT),
    ];
    const [target] = probeUptime(metrics, 0, 1000, 1);
    expect(target.segments[0]).toEqual({ label: target.segments[0].label, status: "operational" });
  });

  it("marks a bucket outage when every sample in it is down", () => {
    const metrics: Metric[] = [
      metric("probe.http.up", 0, 100, CHECKOUT),
      metric("probe.http.up", 0, 200, CHECKOUT),
    ];
    const [target] = probeUptime(metrics, 0, 1000, 1);
    expect(target.segments[0]).toMatchObject({ status: "outage", detail: "2 of 2 checks failed" });
  });

  it("marks a bucket degraded when only some samples in it are down", () => {
    const metrics: Metric[] = [
      metric("probe.http.up", 1, 100, CHECKOUT),
      metric("probe.http.up", 0, 200, CHECKOUT),
      metric("probe.http.up", 1, 300, CHECKOUT),
    ];
    const [target] = probeUptime(metrics, 0, 1000, 1);
    expect(target.segments[0]).toMatchObject({
      status: "degraded",
      detail: "1 of 3 checks failed",
    });
  });

  it("marks a bucket unknown with a 'no checks' detail when no probe.http.up sample lands in it, even if other probe.* metrics do", () => {
    const metrics: Metric[] = [metric("probe.tls.valid", 1, 100, CHECKOUT)];
    const [target] = probeUptime(metrics, 0, 1000, 1);
    expect(target.segments[0]).toEqual({
      label: target.segments[0].label,
      status: "unknown",
      detail: "no checks",
    });
  });

  it("returns one entry per distinct target, sorted by name", () => {
    const metrics: Metric[] = [
      metric("probe.http.up", 1, 100, { name: "zeta", url: "https://zeta.example.com" }),
      metric("probe.http.up", 1, 100, { name: "alpha", url: "https://alpha.example.com" }),
    ];
    const targets = probeUptime(metrics, 0, 1000, 1);
    expect(targets.map((t) => t.name)).toEqual(["alpha", "zeta"]);
  });

  it("falls back to the url as the target's identity when the name label is absent", () => {
    const metrics: Metric[] = [
      metric("probe.http.up", 1, 100, { url: "https://no-name.example.com" }),
    ];
    const [target] = probeUptime(metrics, 0, 1000, 1);
    expect(target.name).toBe("https://no-name.example.com");
    expect(target.url).toBe("https://no-name.example.com");
  });

  it("takes the newest probe.tls.* sample in the window for tlsDaysRemaining and tlsValid, not the first or the largest", () => {
    const metrics: Metric[] = [
      metric("probe.http.up", 1, 100, CHECKOUT),
      metric("probe.tls.days_remaining", 40, 100, CHECKOUT),
      metric("probe.tls.valid", 1, 100, CHECKOUT),
      metric("probe.tls.days_remaining", 39, 900, CHECKOUT), // newest
      metric("probe.tls.valid", 0, 900, CHECKOUT), // newest
    ];
    const [target] = probeUptime(metrics, 0, 1000, 1);
    expect(target.tlsDaysRemaining).toBe(39);
    expect(target.tlsValid).toBe(false);
  });

  it("omits tlsDaysRemaining/tlsValid for a target with no probe.tls.* samples", () => {
    const metrics: Metric[] = [
      metric("probe.http.up", 1, 100, { name: "status-page", url: "http://status.example.com" }),
    ];
    const [target] = probeUptime(metrics, 0, 1000, 1);
    expect(target.tlsDaysRemaining).toBeUndefined();
    expect(target.tlsValid).toBeUndefined();
  });
});

describe("coveredSpanMs", () => {
  const NOW = 100_000_000;
  const ONE_HOUR = 60 * 60_000;
  const SIX_HOURS = 6 * 60 * 60_000;

  it("returns null when there are no usable timestamps", () => {
    expect(coveredSpanMs(NOW, NOW - ONE_HOUR, ONE_HOUR, [])).toBeNull();
  });

  it("returns the raw covered span when the oldest timestamp does not reach the range's start", () => {
    const since = NOW - ONE_HOUR;
    const tenMinutesAgo = NOW - 10 * 60_000;
    const span = coveredSpanMs(NOW, since, ONE_HOUR, [iso(tenMinutesAgo)]);
    expect(span).toBe(10 * 60_000);
  });

  it("returns rangeMs + 1 when the oldest timestamp reaches the start of the range (within 5%)", () => {
    const since = NOW - ONE_HOUR;
    // 4% of the range past `since` — inside the 5% tolerance.
    const nearStart = since + 0.04 * ONE_HOUR;
    const span = coveredSpanMs(NOW, since, ONE_HOUR, [iso(nearStart)]);
    expect(span).toBe(ONE_HOUR + 1);
  });

  it("keeps a widened range offered on the next poll even though its own read doesn't reach back that far — the state machine stays stable across two polls", () => {
    // Poll 1: viewing 1h, and the read comes back full (within 5% of
    // reaching the range's start) — offeredTimeRanges should widen to 6h so
    // the user can ask for more, without dropping 1h or 15m.
    const sinceOneHour = NOW - ONE_HOUR;
    const filled = [iso(sinceOneHour + 1000)];
    const span1 = coveredSpanMs(NOW, sinceOneHour, ONE_HOUR, filled);
    expect(span1).toBe(ONE_HOUR + 1);
    expect(offeredTimeRanges(span1).map((r) => r.value)).toEqual(["15m", "1h", "6h"]);

    // Poll 2: the user picked 6h, but the store only actually holds 3 hours
    // — the widened, name-scoped read's oldest sample is 3h old, nowhere
    // near the 6h range's own start.
    const sinceSixHours = NOW - SIX_HOURS;
    const threeHoursAgo = NOW - 3 * 60 * 60_000;
    const span2 = coveredSpanMs(NOW, sinceSixHours, SIX_HOURS, [iso(threeHoursAgo)]);
    expect(span2).toBe(3 * 60 * 60_000);
    // 6h is still the first range that covers a 3h span, so the range the
    // user just chose does not vanish from the list out from under them.
    expect(offeredTimeRanges(span2).map((r) => r.value)).toEqual(["15m", "1h", "6h"]);
  });
});

// Every hook Overview() mounts polls once on the first render; stub them all
// to an empty-but-successful response, the same shape App.test.tsx's
// emptyEndpoints uses, so a render exercises the page's real DOM rather than
// its loading state.
const emptyEndpoints: Record<string, unknown> = {
  "/api/v1/metrics": [],
  "/api/v1/logs": [],
  "/api/v1/traces": [],
  "/api/v1/forseer/insights": [],
  "/api/v1/forseer/clusters": [],
  "/api/v1/forseer/summary": { enabled: false, summary: "" },
  "/api/v1/forseer/budget": { label: "Error-log budget", consumed: 0 },
  "/api/v1/forseer/timeline": [],
};

function mockFetch(responses: Record<string, unknown>) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    const path = url.split("?")[0];
    if (path in responses) {
      return new Response(JSON.stringify(responses[path]), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }
    return new Response("", { status: 404 });
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

describe("Overview accessibility", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // Regression coverage for the section-heading outline: every Card on this
  // page sits as a direct child of the page, right under its own <h1>
  // ("forsight"), so each CardSectionHeading must render as an <h2> — a
  // <h3> (CardTitle's own default) would jump a level and axe's
  // heading-order rule catches exactly that.
  it("has no axe violations once the page has rendered its cards", async () => {
    mockFetch(emptyEndpoints);
    const { container } = render(<Overview />);
    await screen.findByText("Host CPU over time");

    expect(await axe(container)).toHaveNoViolations();
  });
});
