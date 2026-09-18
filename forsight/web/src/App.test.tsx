import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import App from "./App";
import { submitAuthToken, type ForseerQueryFacet } from "./api";
import { THEME_STORAGE_KEY } from "./theme";

type FetchResponses = Record<string, unknown>;

/**
 * Routes the mocked global fetch by pathname (query strings are stripped, so
 * `/api/v1/forseer/query?q=...` still matches `/api/v1/forseer/query`).
 * Anything not listed 404s, which every polling hook in api.ts already
 * treats the same as a transient failure: keep the last snapshot.
 */
function mockFetch(responses: FetchResponses) {
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

/** The full request URLs (path + query string) every mocked fetch call was
 *  made with, in call order — for asserting on *what was asked for*, not
 *  just what was served back (mockFetch's routing strips query strings, so
 *  it can't tell two differently-scoped reads of the same path apart). */
function fetchedUrls(fetchMock: ReturnType<typeof mockFetch>): string[] {
  return fetchMock.mock.calls.map(([input]) =>
    typeof input === "string" ? input : input.toString()
  );
}

// Every hook App() mounts polls once on the first render; stub them all to
// an empty-but-successful response so a test only has to override the one
// endpoint it cares about.
const emptyEndpoints: FetchResponses = {
  "/api/v1/metrics": [],
  "/api/v1/logs": [],
  "/api/v1/traces": [],
  "/api/v1/forseer/insights": [],
  "/api/v1/forseer/clusters": [],
  "/api/v1/forseer/summary": { enabled: false, summary: "" },
  "/api/v1/forseer/budget": { label: "Error-log budget", consumed: 0 },
  "/api/v1/forseer/timeline": [],
};

describe("App", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // Finding 1 (MEDIUM): the Forseer narrative slot used to render nothing at
  // all when XAI_API_KEY is unset — the documented, common default — unlike
  // every other empty/disabled state on the page.
  it("shows a muted disabled note instead of nothing when the AI narrative is off", async () => {
    mockFetch(emptyEndpoints);
    render(<App />);

    expect(
      await screen.findByText("AI narrative disabled — set XAI_API_KEY to enable")
    ).toBeInTheDocument();
  });

  it("renders the summary, not the disabled note, once the AI narrative is enabled", async () => {
    mockFetch({
      ...emptyEndpoints,
      "/api/v1/forseer/summary": { enabled: true, summary: "Everything looks steady." },
    });
    render(<App />);

    expect(await screen.findByText("Everything looks steady.")).toBeInTheDocument();
    expect(
      screen.queryByText("AI narrative disabled — set XAI_API_KEY to enable")
    ).not.toBeInTheDocument();
  });

  // Finding 2 (MEDIUM): Budget.SLO was already sent by the backend but the
  // frontend's ForseerBudget type dropped it silently. Confirms the value
  // from the API response actually reaches the rendered ErrorBudget label.
  it("shows the error-log SLO from the budget API response", async () => {
    mockFetch({
      ...emptyEndpoints,
      "/api/v1/forseer/budget": { label: "Error-log budget", consumed: 10, slo: 0.02 },
    });
    render(<App />);

    expect(await screen.findByText(/2% SLO/)).toBeInTheDocument();
  });

  // Finding 3 (LOW): the Timeline section rendered nothing at all — not even
  // a placeholder — with no stitched events, unlike every sibling panel.
  it("shows the Timeline EmptyState when there are no stitched events", async () => {
    mockFetch(emptyEndpoints);
    render(<App />);

    expect(await screen.findByText("No timeline events yet")).toBeInTheDocument();
  });

  it("renders Timeline items instead of the empty state once events land", async () => {
    mockFetch({
      ...emptyEndpoints,
      "/api/v1/forseer/timeline": [
        {
          id: "evt-1",
          time: "2026-01-01T00:00:00Z",
          title: "CPU spike",
          description: "anomaly",
          tone: "danger",
        },
      ],
    });
    render(<App />);

    expect(await screen.findByText("CPU spike")).toBeInTheDocument();
    expect(screen.queryByText("No timeline events yet")).not.toBeInTheDocument();
  });
});

/**
 * Regression coverage for the "Ask Forseer" NL-query box (App.tsx's submit
 * handler): it used to unconditionally overwrite `filters` with whatever
 * queryForseer() returned, silently wiping any chip the parse didn't itself
 * reproduce, with zero feedback when the parse understood nothing at all.
 * These tests mock every endpoint App() polls and drive the real submit
 * flow through Testing Library rather than unit-testing the merge helper in
 * isolation, since the bug was in how App wired the response into state.
 */

function jsonResponse(body: unknown, ok = true) {
  return Promise.resolve({ ok, json: async () => body }) as ReturnType<typeof fetch>;
}

type QueryResult = { facets: ForseerQueryFacet[]; matched: boolean };

/** Stubs `fetch` for every endpoint App() calls on mount, plus a caller-supplied
 * responder for /api/v1/forseer/query so each test can script its own phrases. */
function installFetchMock(handleQuery: (q: string) => QueryResult) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url.startsWith("/api/v1/forseer/query")) {
      const q = new URL(url, "http://localhost").searchParams.get("q") ?? "";
      return jsonResponse(handleQuery(q));
    }
    if (url.startsWith("/api/v1/forseer/budget")) {
      return jsonResponse({ label: "Error-log budget", consumed: 0 });
    }
    if (url.startsWith("/api/v1/forseer/summary")) {
      return jsonResponse({ enabled: false, summary: "" });
    }
    // metrics, logs, traces, forseer/insights, forseer/clusters,
    // forseer/timeline all render fine from an empty list.
    return jsonResponse([]);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

async function askForseer(user: ReturnType<typeof userEvent.setup>, phrase: string) {
  const input = screen.getByRole("textbox", { name: "Ask Forseer" });
  await user.clear(input);
  if (phrase) await user.type(input, phrase);
  await user.click(screen.getByRole("button", { name: "Apply" }));
}

describe("Ask Forseer query box", () => {
  it("merges a recognized query's facets into existing filters instead of replacing them", async () => {
    const fetchMock = installFetchMock((q) => {
      if (q === "logs from checkout-api") {
        return {
          facets: [{ key: "source", label: "Source", value: "checkout-api" }],
          matched: true,
        };
      }
      if (q === "critical") {
        return { facets: [{ key: "status", label: "Status", value: "error" }], matched: true };
      }
      return { facets: [], matched: false };
    });
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    await askForseer(user, "logs from checkout-api");
    await screen.findByRole("button", { name: "Remove Source: checkout-api filter" });

    await askForseer(user, "critical");

    // The second, unrelated-key facet is added...
    await screen.findByRole("button", { name: "Remove Status: error filter" });
    // ...without wiping out the first submit's chip. A wholesale replace
    // (the pre-fix behavior) would have dropped this.
    expect(
      screen.getByRole("button", { name: "Remove Source: checkout-api filter" })
    ).toBeInTheDocument();
  });

  it("overrides only the keys a query's facets touch, replacing a same-key chip rather than duplicating it", async () => {
    const fetchMock = installFetchMock((q) => {
      if (q === "warn")
        return { facets: [{ key: "status", label: "Status", value: "warn" }], matched: true };
      if (q === "critical")
        return { facets: [{ key: "status", label: "Status", value: "error" }], matched: true };
      return { facets: [], matched: false };
    });
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    await askForseer(user, "warn");
    await screen.findByRole("button", { name: "Remove Status: warn filter" });

    await askForseer(user, "critical");

    await screen.findByRole("button", { name: "Remove Status: error filter" });
    // Only one "Status" chip at a time — FilterBar's AND semantics can
    // never satisfy two conflicting status facets simultaneously.
    expect(
      screen.queryByRole("button", { name: "Remove Status: warn filter" })
    ).not.toBeInTheDocument();
  });

  it("leaves existing filters untouched and shows feedback for an unrecognized query", async () => {
    const fetchMock = installFetchMock((q) => {
      if (q === "logs from checkout-api") {
        return {
          facets: [{ key: "source", label: "Source", value: "checkout-api" }],
          matched: true,
        };
      }
      return { facets: [], matched: false };
    });
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    await askForseer(user, "logs from checkout-api");
    await screen.findByRole("button", { name: "Remove Source: checkout-api filter" });

    await askForseer(user, "banana banana banana");

    // The pre-existing chip survives an unrecognized submit...
    expect(
      screen.getByRole("button", { name: "Remove Source: checkout-api filter" })
    ).toBeInTheDocument();
    // ...and the box says so instead of silently doing nothing.
    expect(await screen.findByText(/didn.t recognize that phrase/i)).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Ask Forseer" })).toHaveAttribute(
      "aria-invalid",
      "true"
    );
  });

  it("shows the supported-vocabulary hint by default, before any query is submitted", async () => {
    const fetchMock = installFetchMock(() => ({ facets: [], matched: false }));
    render(<App />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    expect(screen.getByText(/critical\/severe/i)).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Ask Forseer" })).not.toHaveAttribute(
      "aria-invalid",
      "true"
    );
  });

  it("does not call the query endpoint or touch filters when submitted empty", async () => {
    const fetchMock = installFetchMock(() => ({
      facets: [{ key: "status", label: "Status", value: "error" }],
      matched: true,
    }));
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    fetchMock.mockClear();

    await user.click(screen.getByRole("button", { name: "Apply" }));

    expect(fetchMock).not.toHaveBeenCalledWith(expect.stringContaining("/api/v1/forseer/query"));
    expect(screen.queryByRole("button", { name: /^Remove /i })).not.toBeInTheDocument();
  });
});

/**
 * Hash routing (route.ts + the App shell). The dashboard is served from an
 * embedded filesystem at whatever path the agent mounts it on, so the page
 * is chosen from `location.hash` and nothing else — a stale or unknown hash
 * must land on the overview, never on a blank main.
 */
describe("App routing", () => {
  const endpointsWithModels: FetchResponses = {
    ...emptyEndpoints,
    "/api/v1/forseer/models": [],
    "/api/v1/mlaas/status": {
      configured: false,
      reachable: false,
      models: [],
      forecasts: [],
      predictions: [],
      jobs: [],
    },
  };

  afterEach(() => {
    window.location.hash = "";
    vi.unstubAllGlobals();
  });

  it("renders the overview at #/ with the Overview link current", async () => {
    window.location.hash = "#/";
    mockFetch(endpointsWithModels);
    render(<App />);

    expect(await screen.findByText("Host CPU over time")).toBeInTheDocument();
    expect(screen.queryByText("mlaas is not configured")).not.toBeInTheDocument();
    const overviewLinks = screen.getAllByRole("link", { name: "Overview" });
    expect(overviewLinks.length).toBeGreaterThan(0);
    for (const link of overviewLinks) expect(link).toHaveAttribute("aria-current", "page");
    for (const link of screen.getAllByRole("link", { name: "Models" })) {
      expect(link).not.toHaveAttribute("aria-current", "page");
    }
  });

  it("renders the Models page at #/models", async () => {
    window.location.hash = "#/models";
    mockFetch(endpointsWithModels);
    render(<App />);

    expect(await screen.findByRole("heading", { level: 1, name: "Models" })).toBeInTheDocument();
    expect(await screen.findByText("mlaas is not configured")).toBeInTheDocument();
    expect(screen.queryByText("Host CPU over time")).not.toBeInTheDocument();
    for (const link of screen.getAllByRole("link", { name: "Models" })) {
      expect(link).toHaveAttribute("aria-current", "page");
    }
  });

  it("switches pages when the hash changes after mount", async () => {
    window.location.hash = "";
    mockFetch(endpointsWithModels);
    render(<App />);
    expect(await screen.findByText("Host CPU over time")).toBeInTheDocument();

    window.location.hash = "#/models";
    window.dispatchEvent(new HashChangeEvent("hashchange"));

    expect(await screen.findByRole("heading", { level: 1, name: "Models" })).toBeInTheDocument();
    expect(screen.queryByText("Host CPU over time")).not.toBeInTheDocument();
  });

  it("falls back to the overview for a hash it does not know", async () => {
    window.location.hash = "#/nope";
    mockFetch(endpointsWithModels);
    render(<App />);

    expect(await screen.findByText("Host CPU over time")).toBeInTheDocument();
  });

  // H9: hash-route navigation swapped the whole main content with no cue a
  // screen-reader user could pick up on — document.title never changed
  // (every history entry and open tab read "forsight") and nothing moved
  // focus, so the user stayed parked on the nav link they had just
  // activated. document.title now names the route, on mount and on every
  // navigation after it.
  it("sets document.title to the current route and updates it on navigation", async () => {
    window.location.hash = "#/";
    mockFetch(endpointsWithModels);
    render(<App />);
    await screen.findByText("Host CPU over time");

    expect(document.title).toBe("forsight — Overview");

    window.location.hash = "#/models";
    window.dispatchEvent(new HashChangeEvent("hashchange"));

    await screen.findByRole("heading", { level: 1, name: "Models" });
    expect(document.title).toBe("forsight — Models");
  });

  // The other half of H9: focus moves to the new page's <h1> on navigation,
  // per the ARIA APG client-navigation pattern, so the user lands somewhere
  // that announces the page changed instead of staying on the link they
  // just activated.
  it("moves focus to the new page's h1 when the route changes", async () => {
    window.location.hash = "#/";
    mockFetch(endpointsWithModels);
    render(<App />);
    await screen.findByText("Host CPU over time");

    window.location.hash = "#/models";
    window.dispatchEvent(new HashChangeEvent("hashchange"));

    const heading = await screen.findByRole("heading", { level: 1, name: "Models" });
    await waitFor(() => expect(document.activeElement).toBe(heading));
  });

  // Focusing the heading on the very first render would steal focus from
  // the top of the document the instant the page loads — its own bug, and
  // the reason the fix above tracks "has a navigation actually happened"
  // rather than focusing on every render.
  it("does not move focus on first mount", async () => {
    window.location.hash = "#/";
    mockFetch(endpointsWithModels);
    render(<App />);
    await screen.findByText("Host CPU over time");

    expect(document.activeElement).toBe(document.body);
  });

  // Fix 3 (MEDIUM): the mobile drawer used to stay open after tapping a nav
  // link, since `<a href>` navigation only changes location.hash and never
  // touched the sidebar's mobileOpen state. Any route change — a tap, a
  // back/forward navigation, or (as exercised here, since jsdom has no real
  // navigation) a hashchange dispatch — must close it.
  it("closes the mobile nav drawer when the route changes", async () => {
    window.location.hash = "#/";
    mockFetch(endpointsWithModels);
    const user = userEvent.setup();
    render(<App />);
    await screen.findByText("Host CPU over time");

    await user.click(screen.getByRole("button", { name: "Open navigation" }));
    expect(await screen.findByRole("dialog", { name: "Main navigation" })).toBeInTheDocument();

    window.location.hash = "#/models";
    window.dispatchEvent(new HashChangeEvent("hashchange"));

    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Main navigation" })).not.toBeInTheDocument()
    );
  });
});

describe("Overview connection", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const cpuSample = [
    { name: "host.cpu.percent", value: 12.5, timestamp: new Date().toISOString() },
  ];

  it("announces the connection from one status region, once", async () => {
    mockFetch({ ...emptyEndpoints, "/api/v1/metrics": cpuSample });
    render(<App />);

    const region = await screen.findByRole("status", { name: "Agent connection" });
    await within(region).findByText("Receiving data");
    // AlertList carries its own live region; the connection must not be
    // announced from there as well.
    expect(screen.getAllByText("Receiving data")).toHaveLength(1);
  });

  it("reports the agent unreachable after three missed polls, keeping the last snapshot on screen", async () => {
    vi.useFakeTimers();
    try {
      let down = false;
      const responses: FetchResponses = { ...emptyEndpoints, "/api/v1/metrics": cpuSample };
      vi.stubGlobal(
        "fetch",
        vi.fn(async (input: RequestInfo | URL) => {
          if (down) throw new TypeError("connection refused");
          const path = String(input).split("?")[0];
          if (path in responses) {
            return new Response(JSON.stringify(responses[path]), {
              status: 200,
              headers: { "content-type": "application/json" },
            });
          }
          return new Response("", { status: 404 });
        })
      );
      render(<App />);
      await act(async () => {
        for (let i = 0; i < 10; i++) await Promise.resolve();
      });
      const region = screen.getByRole("status", { name: "Agent connection" });
      expect(within(region).getByText("Receiving data")).toBeInTheDocument();
      expect(screen.getByText("12.5")).toBeInTheDocument();

      down = true;
      // Three missed 5s polls is still live; the fourth tick is past the
      // three-interval window.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(20000);
      });
      expect(
        within(region).getByText(/^No data for \d+s — agent unreachable$/)
      ).toBeInTheDocument();
      expect(screen.getByText("12.5")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("Overview log templates", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const cluster = (template: string, count: number, pagingScore?: number) => ({
    id: `api|${template}`,
    template,
    source: "api",
    count,
    errorCount: 0,
    lastSeen: new Date().toISOString(),
    sample: template,
    ...(pagingScore === undefined ? {} : { pagingScore }),
  });

  it("ranks templates by count until the paging model is ready", async () => {
    mockFetch({
      ...emptyEndpoints,
      "/api/v1/forseer/clusters": [cluster("request served", 900), cluster("timeout", 40)],
    });
    render(<App />);
    // The title reads "by volume" before any cluster has arrived, so it is
    // the count that proves the data rendered.
    await screen.findByText("900");
    expect(screen.getByText("Log templates · by volume")).toBeInTheDocument();
  });

  it("ranks templates by what a burst is worth once every cluster carries a score", async () => {
    mockFetch({
      ...emptyEndpoints,
      "/api/v1/forseer/clusters": [
        cluster("request served", 900, 0.07),
        cluster("timeout", 40, 0.95),
      ],
    });
    render(<App />);
    await screen.findByText("Log templates · worth paging");
    const rows = screen.getAllByText(/^(95|7)%$/).map((el) => el.textContent);
    expect(rows).toEqual(["95%", "7%"]);
    expect(screen.queryByText("900")).not.toBeInTheDocument();
  });
});

describe("Overview time range", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString();
  const errorLog = (minutesAgo: number, message: string) => ({
    timestamp: ago(minutesAgo),
    severity: "error",
    message,
    source: "api",
  });

  it("offers only the ranges the store holds, defaults to 1h, and widens on request", async () => {
    const user = userEvent.setup();
    mockFetch({
      ...emptyEndpoints,
      "/api/v1/logs": [errorLog(5, "recent failure"), errorLog(180, "old failure")],
    });
    render(<App />);

    await screen.findByText("1 error log in the current window.");
    expect(screen.getByRole("radio", { name: "Last 1 hour" })).toBeChecked();
    // Three hours of data: 6h is the first option that covers it all, so
    // 24h and 7d would change nothing and are not offered.
    expect(screen.getByRole("radio", { name: "Last 6 hours" })).toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: "Last 24 hours" })).not.toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: "Last 7 days" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("radio", { name: "Last 6 hours" }));
    await screen.findByText("2 error logs in the current window.");
  });

  it("offers 15m and 1h before anything has arrived", async () => {
    mockFetch(emptyEndpoints);
    render(<App />);
    await screen.findByRole("radio", { name: "Last 1 hour" });
    expect(screen.getByRole("radio", { name: "Last 15 minutes" })).toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: "Last 6 hours" })).not.toBeInTheDocument();
  });
});

// Item 11: memory and disk get the same Sparkline-on-a-StatCard-plus-chart
// treatment CPU already had, via a real radio group like TimeRange's.
describe("Overview memory and disk charts", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString();
  const hostMetrics = [
    { name: "host.cpu.percent", value: 10, timestamp: ago(1) },
    { name: "host.cpu.percent", value: 12.5, timestamp: ago(0) },
    { name: "host.memory.percent", value: 40, timestamp: ago(1) },
    { name: "host.memory.percent", value: 44, timestamp: ago(0) },
    { name: "host.disk.percent", value: 70, timestamp: ago(1) },
    { name: "host.disk.percent", value: 71, timestamp: ago(0) },
  ];

  it("gives Host memory and Host disk a Sparkline too, not only Host CPU", async () => {
    mockFetch({ ...emptyEndpoints, "/api/v1/metrics": hostMetrics });
    render(<App />);
    await screen.findByText("Host CPU over time");

    // The title renders before any poll answers; the sparklines do not.
    expect(await screen.findByRole("img", { name: /^Host CPU trend:/ })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /^Host memory trend:/ })).toBeInTheDocument();
    expect(screen.getByRole("img", { name: /^Host disk trend:/ })).toBeInTheDocument();
  });

  it("is a radio group with Host CPU checked by default, and starts the chart on CPU", async () => {
    mockFetch({ ...emptyEndpoints, "/api/v1/metrics": hostMetrics });
    render(<App />);
    await screen.findByText("Host CPU over time");

    expect(screen.getByRole("radiogroup", { name: "Chart metric" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /^Host CPU/ })).toBeChecked();
    expect(screen.getByRole("radio", { name: /^Host memory/ })).not.toBeChecked();
    expect(screen.getByRole("radio", { name: /^Host disk/ })).not.toBeChecked();
    // The chart draws once its history poll has answered, not with the title.
    expect(
      await screen.findByRole("img", { name: "Host CPU percent over time" })
    ).toBeInTheDocument();
  });

  it("clicking the Host memory stat repoints the chart at memory's history", async () => {
    const user = userEvent.setup();
    mockFetch({ ...emptyEndpoints, "/api/v1/metrics": hostMetrics });
    render(<App />);
    await screen.findByText("Host CPU over time");

    await user.click(screen.getByRole("radio", { name: /^Host memory/ }));

    expect(await screen.findByText("Host memory over time")).toBeInTheDocument();
    expect(screen.queryByText("Host CPU over time")).not.toBeInTheDocument();
    expect(
      await screen.findByRole("img", { name: "Host memory percent over time" })
    ).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /^Host memory/ })).toBeChecked();
    expect(screen.getByRole("radio", { name: /^Host CPU/ })).not.toBeChecked();
  });

  it("moves and selects the charted stat with the arrow keys, wrapping at the ends", async () => {
    const user = userEvent.setup();
    mockFetch({ ...emptyEndpoints, "/api/v1/metrics": hostMetrics });
    render(<App />);
    await screen.findByText("Host CPU over time");

    screen.getByRole("radio", { name: /^Host CPU/ }).focus();
    await user.keyboard("{ArrowRight}");
    expect(screen.getByRole("radio", { name: /^Host memory/ })).toBeChecked();
    expect(screen.getByRole("radio", { name: /^Host memory/ })).toHaveFocus();
    expect(await screen.findByText("Host memory over time")).toBeInTheDocument();

    // Wraps past Host disk back to Host CPU.
    await user.keyboard("{ArrowRight}{ArrowRight}");
    expect(screen.getByRole("radio", { name: /^Host CPU/ })).toBeChecked();
    expect(await screen.findByText("Host CPU over time")).toBeInTheDocument();
  });

  it("keeps only the checked stat in the tab order, same as TimeRange", async () => {
    mockFetch({ ...emptyEndpoints, "/api/v1/metrics": hostMetrics });
    render(<App />);
    await screen.findByText("Host CPU over time");

    expect(screen.getByRole("radio", { name: /^Host CPU/ })).toHaveAttribute("tabindex", "0");
    expect(screen.getByRole("radio", { name: /^Host memory/ })).toHaveAttribute("tabindex", "-1");
    expect(screen.getByRole("radio", { name: /^Host disk/ })).toHaveAttribute("tabindex", "-1");
  });
});

// Item #128: a per-target UptimeBar under a "Probes" Card, built from the
// agent's probe.* metrics (forsight/internal/collector/probe). The card
// must be invisible on a deployment that never passed --probe, so the
// no-metrics case is asserted right alongside the populated one.
describe("Overview probe strips", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const now = () => new Date().toISOString();
  const probeMetrics = [
    // A near-current host metric alongside the probe samples: the fixture's
    // whole point is a deployment with a --probe target, but the range math
    // now reads its span from the (named, ranged) chart-metric histories and
    // logs, not from every metric in one unbounded read — so a fixture with
    // only probe.* samples would otherwise cover nothing and default to the
    // widest offered range instead of the 15m these tests assert.
    { name: "host.cpu.percent", value: 10, timestamp: now(), labels: {} },
    { name: "probe.http.up", value: 1, timestamp: now(), labels: { name: "checkout", url: "https://checkout.example.com" } },
    { name: "probe.tls.days_remaining", value: 5, timestamp: now(), labels: { name: "checkout", url: "https://checkout.example.com" } },
    { name: "probe.tls.valid", value: 1, timestamp: now(), labels: { name: "checkout", url: "https://checkout.example.com" } },
  ];

  it("shows a Probes card with an UptimeBar naming the target once probe metrics arrive", async () => {
    mockFetch({ ...emptyEndpoints, "/api/v1/metrics": probeMetrics });
    render(<App />);

    expect(await screen.findByRole("heading", { name: "Probes" })).toBeInTheDocument();
    // A single, almost-current sample holds the smallest range on offer
    // (15m — see "Overview time range" above), not the 1h default.
    expect(screen.getByText("checkout, last 15m")).toBeInTheDocument();
  });

  it("shows a warning-toned TLS expiry badge when the certificate is valid but expiring soon", async () => {
    mockFetch({ ...emptyEndpoints, "/api/v1/metrics": probeMetrics });
    render(<App />);

    expect(await screen.findByText("TLS expires in 5 days")).toBeInTheDocument();
  });

  it("shows no Probes heading at all when the agent has no --probe target configured", async () => {
    mockFetch(emptyEndpoints);
    render(<App />);

    await screen.findByText("Host CPU over time");
    expect(screen.queryByRole("heading", { name: "Probes" })).not.toBeInTheDocument();
  });
});

// The split this covers: Overview used to poll GET /api/v1/metrics with no
// bound at all, fetching the store's whole retained history for every
// metric name on every 5s tick. It now issues one since-bounded, unscoped
// read for the newest point of every series (StatCards, container/process
// rows, the probe gate), plus one name-scoped, ranged read per CHART_METRICS
// entry, plus — only once a probe metric shows up — one name-scoped, ranged
// read per probe.* metric name. These tests assert on the request URLs
// themselves (mockFetch's routing strips query strings, so it can't tell two
// differently-scoped reads of the same path apart, and so returns the same
// fixture body to all of them; the coverage here is what got *asked for*,
// not what was returned).
describe("Overview bounded metrics reads", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const CHART_METRIC_NAMES = ["host.cpu.percent", "host.memory.percent", "host.disk.percent"];

  it("polls a since-bounded unscoped read plus one name-scoped ranged read per chart metric", async () => {
    const fetchMock = mockFetch(emptyEndpoints);
    render(<App />);
    await screen.findByText("Host CPU over time");

    const metricsUrls = fetchedUrls(fetchMock).filter((u) => u.startsWith("/api/v1/metrics"));

    // The "latest" read: bounded by since=, but not scoped to one name.
    expect(metricsUrls.some((u) => u.includes("since=") && !u.includes("name="))).toBe(true);
    // One ranged, name-scoped read per chart metric.
    for (const name of CHART_METRIC_NAMES) {
      expect(
        metricsUrls.some((u) => u.includes(`name=${name}`) && u.includes("since="))
      ).toBe(true);
    }
  });

  it("issues no probe.* read at all when the latest read carries no probe metric", async () => {
    const fetchMock = mockFetch(emptyEndpoints);
    render(<App />);
    await screen.findByText("Host CPU over time");

    const urls = fetchedUrls(fetchMock);
    expect(urls.some((u) => u.includes("name=probe."))).toBe(false);
    expect(screen.queryByRole("heading", { name: "Probes" })).not.toBeInTheDocument();
  });

  it("reads every probe.* metric, ranged, once the latest read carries one, and still renders the Probes card from them", async () => {
    const now = () => new Date().toISOString();
    const probeMetrics = [
      { name: "host.cpu.percent", value: 10, timestamp: now(), labels: {} },
      {
        name: "probe.http.up",
        value: 1,
        timestamp: now(),
        labels: { name: "checkout", url: "https://checkout.example.com" },
      },
    ];
    const fetchMock = mockFetch({ ...emptyEndpoints, "/api/v1/metrics": probeMetrics });
    render(<App />);

    expect(await screen.findByRole("heading", { name: "Probes" })).toBeInTheDocument();
    expect(screen.getByText("checkout, last 15m")).toBeInTheDocument();

    const urls = fetchedUrls(fetchMock);
    for (const name of ["probe.http.up", "probe.tls.days_remaining", "probe.tls.valid"]) {
      expect(urls.some((u) => u.includes(`name=${name}`) && u.includes("since="))).toBe(true);
    }
  });
});

/**
 * Item 18: forsight/internal/api/auth.go rejects every route but the
 * dashboard's static shell with a 401 once --auth-token is set, so the page
 * itself always loads — these tests drive the dialog that api.ts's
 * fetchWithAuth opens once one of App's own pollers hits that 401.
 */
describe("Auth token gate", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    // The token store and the open/rejected prompt state are module-level
    // in api.ts (shared by every fetchWithAuth caller), so each test must
    // put them back or leak state into whichever test runs next.
    submitAuthToken("");
  });

  /** 401s every endpoint until `token` has been sent as the Bearer value,
   *  then serves emptyEndpoints — the shape a real agent gives fetchWithAuth
   *  once the right token starts arriving. */
  function mockFetchRequiringToken(token: string) {
    return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const sent = new Headers(init?.headers).get("Authorization");
      if (sent !== `Bearer ${token}`) return new Response("", { status: 401 });
      const url = typeof input === "string" ? input : input.toString();
      const path = url.split("?")[0];
      if (path in emptyEndpoints) {
        return new Response(JSON.stringify(emptyEndpoints[path]), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response("", { status: 404 });
    });
  }

  it("shows the token dialog, not the page's data, when a poller 401s", async () => {
    vi.stubGlobal("fetch", mockFetchRequiringToken("right-token"));
    render(<App />);

    expect(
      await screen.findByRole("dialog", { name: "Access token required" })
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Access token")).toBeInTheDocument();
    // The neutral first-load copy, not the "rejected" one.
    expect(screen.queryByText(/was rejected/i)).not.toBeInTheDocument();
  });

  it("closes the dialog and unlocks the data once the right token is submitted", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", mockFetchRequiringToken("right-token"));
    render(<App />);
    await screen.findByRole("dialog", { name: "Access token required" });

    await user.type(screen.getByLabelText("Access token"), "right-token");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Access token required" })
      ).not.toBeInTheDocument()
    );
    expect(await screen.findByText("Host CPU over time")).toBeInTheDocument();
  });

  it("reopens the dialog as rejected when the submitted token is wrong", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", mockFetchRequiringToken("right-token"));
    render(<App />);
    await screen.findByRole("dialog", { name: "Access token required" });

    await user.type(screen.getByLabelText("Access token"), "wrong-guess");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await screen.findByText(/that token was rejected/i);
    expect(screen.getByLabelText("Access token")).toHaveValue("");
  });

  it("keeps the rejection on screen when the next scheduled poll 401s without a token", async () => {
    // Testing Library's async wrapper (under every user-event call) ends by
    // awaiting a setTimeout(0) that it only auto-advances for Jest's fake
    // timers; letting vitest's clock track real time is what fires it here.
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const user = userEvent.setup({ delay: null });
      vi.stubGlobal("fetch", mockFetchRequiringToken("right-token"));
      render(<App />);
      await act(async () => {
        for (let i = 0; i < 10; i++) await Promise.resolve();
      });
      expect(screen.getByRole("dialog", { name: "Access token required" })).toBeInTheDocument();

      await user.type(screen.getByLabelText("Access token"), "wrong-guess");
      await user.click(screen.getByRole("button", { name: "Continue" }));
      await act(async () => {
        for (let i = 0; i < 10; i++) await Promise.resolve();
      });
      expect(screen.getByText(/that token was rejected/i)).toBeInTheDocument();

      // The rejected token was cleared, so the next scheduled poll goes out
      // with no token and 401s again. That is not news about anything the
      // user did, and must not revert the dialog to its first-load wording.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });
      expect(screen.getByText(/that token was rejected/i)).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("does not show the dialog at all once a poll comes back with data", async () => {
    mockFetch(emptyEndpoints);
    render(<App />);

    await screen.findByText("Host CPU over time");
    expect(screen.queryByRole("dialog", { name: "Access token required" })).not.toBeInTheDocument();
  });
});

/**
 * The sidebar footer's theme Switch (App.tsx's ThemeToggle, backed by
 * useTheme in theme.ts). data-theme lives on <html> — outside whatever
 * render() mounts — and localStorage persists across tests in the same
 * jsdom environment, so both are reset in afterEach to keep every other
 * describe block in this file (which all assume the dark default) honest.
 */
describe("Theme toggle", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    document.documentElement.removeAttribute("data-theme");
    try {
      localStorage.clear();
    } catch {
      // Nothing to clean up if storage was never usable to begin with.
    }
  });

  it("is unchecked by default and leaves the document in the dark theme applyForsightTheme sets", async () => {
    mockFetch(emptyEndpoints);
    render(<App />);

    const toggle = await screen.findByRole("switch", { name: "Light theme" });
    expect(toggle).not.toBeChecked();
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });

  it("switches the document to light and stores the choice when clicked", async () => {
    const user = userEvent.setup();
    mockFetch(emptyEndpoints);
    render(<App />);

    const toggle = await screen.findByRole("switch", { name: "Light theme" });
    await user.click(toggle);

    expect(toggle).toBeChecked();
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
  });

  it("starts checked and applied when localStorage already holds \"light\"", async () => {
    localStorage.setItem(THEME_STORAGE_KEY, "light");
    mockFetch(emptyEndpoints);
    render(<App />);

    const toggle = await screen.findByRole("switch", { name: "Light theme" });
    expect(toggle).toBeChecked();
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });
});
