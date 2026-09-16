import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { connectionState, usePoll, type PollState } from "./api";

function ok(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

// The mount poll is a fetch/json/setState promise chain, not a timer: fake
// timers don't advance it, so it is drained with microtask flushes (the same
// pattern Models.test.tsx uses).
async function drain() {
  await act(async () => {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  });
}

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

let hidden = false;

describe("usePoll", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    hidden = false;
    Object.defineProperty(document, "hidden", { configurable: true, get: () => hidden });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("polls at once, then on the interval, and keeps the last snapshot across a failure", async () => {
    let call = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        call += 1;
        if (call === 1) return ok([1]);
        if (call === 2) return ok([1, 2]);
        return new Response("", { status: 503 });
      })
    );
    const { result } = renderHook(() => usePoll<number[]>("/x", 5000, []));
    expect(result.current).toEqual({ data: [], lastSuccessAt: null, lastErrorAt: null });

    await drain();
    expect(result.current.data).toEqual([1]);
    expect(result.current.lastSuccessAt).not.toBeNull();
    expect(result.current.lastErrorAt).toBeNull();

    await advance(5000);
    expect(result.current.data).toEqual([1, 2]);

    await advance(5000);
    const { data, lastSuccessAt, lastErrorAt } = result.current;
    expect(data).toEqual([1, 2]);
    expect(lastErrorAt).not.toBeNull();
    expect(Number(lastErrorAt)).toBeGreaterThan(Number(lastSuccessAt));
  });

  it("counts a parse that throws as a failure and keeps the previous snapshot", async () => {
    let call = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        call += 1;
        return ok(call === 1 ? { n: 1 } : null);
      })
    );
    const { result } = renderHook(() =>
      usePoll<number>("/x", 5000, 0, (raw) => {
        const body = raw as { n: number } | null;
        if (!body) throw new Error("empty body");
        return body.n;
      })
    );
    await drain();
    expect(result.current.data).toBe(1);
    await advance(5000);
    expect(result.current.data).toBe(1);
    expect(result.current.lastErrorAt).not.toBeNull();
  });

  it("does not fetch while the document is hidden, and polls at once when it is visible again", async () => {
    const fetchMock = vi.fn(async () => ok([]));
    vi.stubGlobal("fetch", fetchMock);
    renderHook(() => usePoll<number[]>("/x", 5000, []));
    await drain();
    expect(fetchMock).toHaveBeenCalledTimes(1);

    hidden = true;
    await advance(15000);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    hidden = false;
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
      await Promise.resolve();
    });
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("aborts the request in flight when it unmounts", async () => {
    let signal: AbortSignal | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn((_input: RequestInfo | URL, init?: RequestInit) => {
        signal = init?.signal ?? undefined;
        return new Promise<Response>(() => {});
      })
    );
    const { unmount } = renderHook(() => usePoll<number[]>("/x", 5000, []));
    await drain();
    expect(signal?.aborted).toBe(false);
    unmount();
    expect(signal?.aborted).toBe(true);
  });

  it("abandons a request that outlives two intervals and counts it as a failure", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (_input: RequestInfo | URL, init?: RequestInit) =>
          new Promise<Response>((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () =>
              reject(new DOMException("aborted", "AbortError"))
            );
          })
      )
    );
    const { result } = renderHook(() => usePoll<number[]>("/x", 5000, []));
    await drain();
    expect(result.current.lastErrorAt).toBeNull();

    // Ticks at most 5s, 10s and 15s after mount: the hung request is older
    // than two intervals by the third at the latest.
    await advance(15000);
    expect(result.current.lastErrorAt).not.toBeNull();
    expect(result.current.data).toEqual([]);
  });

  it("reads a function path fresh on every poll", async () => {
    const urls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        urls.push(String(input));
        return ok([]);
      })
    );
    renderHook(() => usePoll<number[]>(() => `/x?t=${Date.now()}`, 5000, []));
    await drain();
    await advance(5000);
    expect(urls).toHaveLength(2);
    expect(urls[0]).not.toEqual(urls[1]);
  });

  it("fires every poller sharing an interval on the same wall-clock boundary", async () => {
    const calls: number[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        calls.push(Date.now());
        return ok([]);
      })
    );
    renderHook(() => usePoll<number[]>("/a", 5000, []));
    await advance(1234);
    renderHook(() => usePoll<number[]>("/b", 5000, []));
    await drain();
    calls.length = 0;

    await advance(5000);
    expect(calls).toHaveLength(2);
    expect(calls[0]).toBe(calls[1]);
    expect(calls[0] % 5000).toBe(0);
  });
});

describe("connectionState", () => {
  const poll = (lastSuccessAt: number | null): PollState<unknown> => ({
    data: null,
    lastSuccessAt,
    lastErrorAt: null,
  });

  it("is waiting until any poller has succeeded", () => {
    expect(connectionState([poll(null), poll(null)], 5000, 100_000)).toEqual({
      state: "waiting",
      silentForMs: null,
    });
  });

  it("is live within three intervals of the newest success, whichever poller it was", () => {
    expect(connectionState([poll(80_000), poll(null), poll(90_000)], 5000, 105_000)).toEqual({
      state: "live",
      silentForMs: 15_000,
    });
  });

  it("is stale after three missed intervals, however many snapshots are still on screen", () => {
    expect(connectionState([poll(80_000), poll(84_999)], 5000, 100_000)).toEqual({
      state: "stale",
      silentForMs: 15_001,
    });
  });
});
