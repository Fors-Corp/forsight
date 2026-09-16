import { describe, expect, it } from "vitest";
import { probeUptime } from "./Overview";
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
