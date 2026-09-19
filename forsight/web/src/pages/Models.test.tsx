import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "../test-utils/axe";
import Models, { forecastChart } from "./Models";
import type { ForseerCard, Metric, MlaasForecast, MlaasStatus } from "../api";

// `unknown` covers a plain value, a `Response`, or (for a test that wants to
// hold a request open, e.g. to check the page's pending state) a Promise of
// either — `mockFetch` awaits whatever the responder returns.
type Responder = (req: { method: string; body: unknown; url: URL }) => unknown;
// `object | Responder` rather than `unknown | Responder`: the latter collapses
// to `unknown` and the responder's parameters lose their contextual types.
type FetchResponses = Record<string, object | Responder>;

/**
 * Routes the mocked fetch by pathname, the same way App.test.tsx does, with
 * one addition: an entry may be a function that sees the method, the parsed
 * JSON body and the URL, so a test can assert on what a POST sent. Anything
 * unlisted 404s, which the hooks treat as "keep the last snapshot".
 */
function mockFetch(responses: FetchResponses) {
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = typeof input === "string" ? input : input.toString();
    const url = new URL(raw, "http://localhost");
    const entry = responses[url.pathname];
    if (entry === undefined) return new Response("", { status: 404 });
    let body: unknown = null;
    if (typeof init?.body === "string") {
      try {
        body = JSON.parse(init.body);
      } catch {
        body = init.body;
      }
    }
    const value = await (typeof entry === "function"
      ? (entry as Responder)({ method: init?.method ?? "GET", body, url })
      : entry);
    if (value instanceof Response) return value;
    return new Response(JSON.stringify(value), {
      status: 200,
      headers: { "content-type": "application/json" },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

const notConfigured: MlaasStatus = {
  configured: false,
  reachable: false,
  models: [],
  forecasts: [],
  predictions: [],
  jobs: [],
};

const connected: MlaasStatus = {
  configured: true,
  url: "http://127.0.0.1:8090",
  reachable: true,
  lastSync: "2026-09-15T10:00:00Z",
  models: [
    {
      name: "forsight-cpu-forecast",
      job: "Say where host CPU is heading over the next hour.",
      task: "forecast",
      plugin: "holtwinters",
      dataset: "forsight-host-cpu",
      datasetRows: 1200,
      reads: ["host.cpu.percent, one-minute means"],
      state: "ready",
      champion: 3,
      metric: "rmse",
      holdout: 1.2345,
      live: 1.5,
      liveWindow: 40,
      newLabels: 12,
      driftMax: 0.0421,
      driftFeature: "host.cpu.percent",
      driftThreshold: 0.2,
      activeJob: false,
      predictionsLogged: 160,
    },
    {
      name: "forsight-memory-forecast",
      job: "Say where host memory is heading over the next hour.",
      task: "forecast",
      plugin: "holtwinters",
      dataset: "forsight-host-memory",
      datasetRows: 0,
      reads: ["host.memory.percent, one-minute means"],
      state: "waiting-for-data",
      champion: 0,
      metric: "rmse",
      liveWindow: 0,
      newLabels: 0,
      // No champion has ever been trained, so mlaas has nothing to
      // compare recent inputs against yet — driftMax stays absent.
      driftThreshold: 0.2,
      activeJob: false,
      predictionsLogged: 0,
    },
    {
      name: "forsight-log-severity",
      job: "Give a log line the level this deployment would have given it.",
      task: "classification",
      plugin: "bayes",
      dataset: "forsight-logs",
      datasetRows: 900,
      reads: ["log lines whose source declared a level"],
      state: "ready",
      champion: 2,
      metric: "accuracy",
      holdout: 0.917,
      liveWindow: 0,
      newLabels: 30,
      // At or above mlaas's own retrain.drift_threshold: the Badge must
      // read as critical, not just a bigger number.
      driftMax: 0.24,
      driftFeature: "message",
      driftThreshold: 0.2,
      activeJob: false,
      predictionsLogged: 75,
    },
  ],
  forecasts: [],
  predictions: [],
  jobs: [],
};

const forseerCards: ForseerCard[] = [
  {
    name: "log severity",
    job: "Give a log line the level this deployment would have given it.",
    reads: ["log message"],
    fallback: "keyword rule",
    ready: true,
    trained: 640,
    accuracy: 0.82,
    fallbackAccuracy: 0.6,
    graded: 500,
  },
];

const baseEndpoints: FetchResponses = {
  "/api/v1/metrics": [],
  "/api/v1/forseer/models": forseerCards,
  "/api/v1/mlaas/status": connected,
};

describe("Models page", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("explains how to configure mlaas when it is not configured", async () => {
    mockFetch({ ...baseEndpoints, "/api/v1/mlaas/status": notConfigured });
    render(<Models />);

    expect(await screen.findByText("mlaas is not configured")).toBeInTheDocument();
    expect(
      screen.getByText(
        "forsight run --mlaas-url http://127.0.0.1:8090 --mlaas-api-key-file /path/to/mlaas/data/api_key"
      )
    ).toBeInTheDocument();
    expect(screen.getByText("Not configured")).toBeInTheDocument();
  });

  it("renders every managed model with its state badge and scores once connected", async () => {
    mockFetch(baseEndpoints);
    render(<Models />);

    expect(await screen.findByText("Connected to http://127.0.0.1:8090")).toBeInTheDocument();

    // The model name also sits in each action button's sr-only label, so
    // anchor the row on the button rather than the bare text.
    const cpu = screen.getByRole("button", { name: "Train forsight-cpu-forecast" }).closest("tr");
    expect(cpu).not.toBeNull();
    expect(within(cpu!).getByText("ready")).toBeInTheDocument();
    expect(within(cpu!).getByText("v3")).toBeInTheDocument();
    expect(within(cpu!).getByText("rmse 1.23")).toBeInTheDocument();
    expect(within(cpu!).getByText("1.50 over 40 labels")).toBeInTheDocument();
    expect(within(cpu!).getByText("0.04 of 0.20")).toBeInTheDocument();
    expect(within(cpu!).getByText("host.cpu.percent")).toBeInTheDocument();

    const memory = screen.getByRole("button", { name: "Train forsight-memory-forecast" }).closest("tr");
    expect(within(memory!).getByText("waiting-for-data")).toBeInTheDocument();
    // No data yet, so nothing to train or tune.
    expect(within(memory!).getByRole("button", { name: "Train forsight-memory-forecast" })).toBeDisabled();
    expect(within(memory!).getByRole("button", { name: "Tune forsight-memory-forecast" })).toBeDisabled();

    // The Forseer card next to it, with the score against its rule.
    expect(screen.getByText("82% vs rule 60% on 500 graded")).toBeInTheDocument();
    // "Ready" is also the column header; the badge is the span.
    expect(screen.getByText("Ready", { selector: "span" })).toBeInTheDocument();
  });

  // The "Served by mlaas" StatusDot's label flips between "Checking
  // mlaas…", "Connected to …" and "Unreachable: …" on its own 10s poll
  // (useMlaasStatus), with nothing else on the page announcing the change —
  // same problem Overview.tsx's "Agent connection" region already solved
  // for its own StatusDot. Without a named live region around it, a screen
  // reader user has no way to learn the connection came up or went down
  // short of re-reading the card.
  it("announces the mlaas connection status from its own named live region", async () => {
    mockFetch(baseEndpoints);
    render(<Models />);

    const region = await screen.findByRole("status", { name: "mlaas connection" });
    await within(region).findByText("Connected to http://127.0.0.1:8090");
  });

  it("announces mlaas unreachable from the same live region, not just in the table", async () => {
    mockFetch({
      ...baseEndpoints,
      "/api/v1/mlaas/status": {
        ...connected,
        reachable: false,
        lastError: "connection refused",
      },
    });
    render(<Models />);

    const region = await screen.findByRole("status", { name: "mlaas connection" });
    await within(region).findByText("Unreachable: connection refused");
  });

  it("shows drift as not-yet-measured rather than a flattering zero, and flags a model at its retrain threshold", async () => {
    mockFetch(baseEndpoints);
    render(<Models />);
    await screen.findByText("Connected to http://127.0.0.1:8090");

    // A model whose live window has not filled must never render a bare
    // "0.00" — the flattering zero MODELS.md forbids on any card.
    const memory = screen.getByRole("button", { name: "Train forsight-memory-forecast" }).closest("tr");
    expect(within(memory!).getByText("not enough data yet")).toBeInTheDocument();
    expect(within(memory!).queryByText(/of 0.20/)).not.toBeInTheDocument();

    // At or above mlaas's own retrain.drift_threshold, the cell states the
    // measured value, the threshold it is judged against, and which
    // feature drifted — not just a number.
    const severity = screen.getByRole("button", { name: "Train forsight-log-severity" }).closest("tr");
    expect(within(severity!).getByText("0.24 of 0.20")).toBeInTheDocument();
    expect(within(severity!).getByText("message")).toBeInTheDocument();
  });

  it("POSTs to the model's train route on click and reports the queued job", async () => {
    const posts: string[] = [];
    mockFetch({
      ...baseEndpoints,
      "/api/v1/mlaas/models/forsight-cpu-forecast/train": ({ method, url }) => {
        posts.push(`${method} ${url.pathname}`);
        return { jobId: 12, alreadyQueued: false };
      },
    });
    const user = userEvent.setup();
    render(<Models />);

    const train = await screen.findByRole("button", { name: "Train forsight-cpu-forecast" });
    expect(train).toBeEnabled();
    await user.click(train);

    expect(await screen.findByText("Job #12 queued")).toBeInTheDocument();
    expect(posts).toEqual(["POST /api/v1/mlaas/models/forsight-cpu-forecast/train"]);
  });

  it("shows the upstream error when a train request is refused", async () => {
    mockFetch({
      ...baseEndpoints,
      "/api/v1/mlaas/models/forsight-cpu-forecast/train": () =>
        new Response(JSON.stringify({ error: "model has no dataset yet" }), {
          status: 409,
          headers: { "content-type": "application/json" },
        }),
    });
    const user = userEvent.setup();
    render(<Models />);

    await user.click(await screen.findByRole("button", { name: "Train forsight-cpu-forecast" }));

    expect(await screen.findByText("model has no dataset yet")).toBeInTheDocument();
  });

  it("draws one chart per forecast with an Observed series and a dashed Forecast series", async () => {
    mockFetch({
      ...baseEndpoints,
      "/api/v1/metrics": [
        { name: "host.cpu.percent", value: 40, timestamp: "2026-09-15T10:00:10Z" },
        { name: "host.cpu.percent", value: 42, timestamp: "2026-09-15T10:00:40Z" },
        { name: "host.cpu.percent", value: 45, timestamp: "2026-09-15T10:01:05Z" },
      ],
      "/api/v1/mlaas/status": {
        ...connected,
        forecasts: [
          {
            model: "forsight-cpu-forecast",
            metric: "host.cpu.percent",
            origin: "2026-09-15T10:01:00Z",
            points: [
              { at: "2026-09-15T10:06:00Z", value: 47 },
              { at: "2026-09-15T10:16:00Z", value: 50 },
              { at: "2026-09-15T10:31:00Z", value: 52 },
              { at: "2026-09-15T11:01:00Z", value: 55 },
            ],
          },
        ],
      },
    });
    render(<Models />);

    await screen.findByText("Connected to http://127.0.0.1:8090");
    const legendEntries = await screen.findAllByRole("listitem");
    const labels = legendEntries.map((li) => li.textContent);
    expect(labels).toContain("Observed");
    expect(labels).toContain("Forecast");
    expect(screen.queryByText("No forecasts yet")).not.toBeInTheDocument();
    // The chart's data table carries the same rows for a screen reader.
    expect(screen.getByRole("rowheader", { name: "Forecast" })).toBeInTheDocument();
    // The projection is dashed from its first minute, and the chart says so
    // in text as well — the data table's caption, which names the table for
    // a screen reader — so the distinction is never sighted-only.
    expect(screen.getByRole("table", { name: /Forecast is projected from/ })).toBeInTheDocument();
  });

  it("marks a prediction that disagrees with the declared level", async () => {
    mockFetch({
      ...baseEndpoints,
      "/api/v1/mlaas/status": {
        ...connected,
        predictions: [
          {
            at: "2026-09-15T10:02:00Z",
            message: "connection refused to db-primary:5432",
            declared: "error",
            forseer: "warn",
            mlaas: "error",
          },
        ],
      },
    });
    render(<Models />);

    const row = (await screen.findByText("connection refused to db-primary:5432")).closest("tr");
    expect(row).not.toBeNull();
    const cells = within(row!).getAllByRole("cell");
    // Declared, Forseer, mlaas — the disagreeing answer is a Badge, the
    // agreeing one plain text.
    expect(cells[2]).toHaveTextContent("error");
    expect(within(cells[3]).getByText("warn").tagName).toBe("SPAN");
    expect(within(cells[4]).queryByText("error")?.tagName).toBe("TD");
    // The disagreement isn't conveyed by Badge color alone — a visually
    // hidden span spells it out for anyone who can't see (or rely on) it.
    expect(cells[3]).toHaveTextContent("disagrees with declared");
  });

  it("asks both models about a typed log line and shows both answers", async () => {
    const calls: string[] = [];
    mockFetch({
      ...baseEndpoints,
      "/api/v1/forseer/classify": ({ url }) => {
        calls.push(`classify ${url.searchParams.get("message")}`);
        return { severity: "error", source: "model", ready: true };
      },
      "/api/v1/mlaas/models/forsight-log-severity/predict": ({ method, body }) => {
        calls.push(`${method} predict ${JSON.stringify(body)}`);
        return {
          model: "forsight-log-severity",
          version: 2,
          predictions: [{ value: "warn", unusual: { message: "3 unseen tokens" } }],
        };
      },
    });
    const user = userEvent.setup();
    render(<Models />);

    await screen.findByText("Connected to http://127.0.0.1:8090");
    await user.type(screen.getByRole("textbox", { name: "Log line" }), "disk almost full");
    await user.click(screen.getByRole("button", { name: "Classify" }));

    const forseerRow = (await screen.findByText("Forseer", { selector: "td" })).closest("tr");
    expect(forseerRow).toHaveTextContent("error");
    expect(forseerRow).toHaveTextContent("model");
    const mlaasRow = screen.getByText("mlaas", { selector: "td" }).closest("tr");
    expect(mlaasRow).toHaveTextContent("warn");
    expect(mlaasRow).toHaveTextContent("v2");
    expect(mlaasRow).toHaveTextContent("3 unseen tokens");

    await waitFor(() =>
      expect(calls).toEqual([
        "classify disk almost full",
        'POST predict {"rows":[{"message":"disk almost full"}]}',
      ])
    );
  });

  // Fix 4 (MEDIUM): before the first /api/v1/mlaas/status response lands,
  // the page used to say "mlaas is not configured" — a guess, not something
  // the agent had actually said yet.
  it("shows a pending state before the first mlaas status response, not the not-configured empty state", async () => {
    let resolveStatus: (value: Response) => void = () => {};
    const statusPromise = new Promise<Response>((resolve) => {
      resolveStatus = resolve;
    });
    mockFetch({ ...baseEndpoints, "/api/v1/mlaas/status": () => statusPromise });
    render(<Models />);

    expect(await screen.findByText("Checking mlaas…")).toBeInTheDocument();
    expect(screen.getByText("Waiting for the first response from mlaas…")).toBeInTheDocument();
    expect(screen.queryByText("mlaas is not configured")).not.toBeInTheDocument();

    resolveStatus(
      new Response(JSON.stringify(notConfigured), {
        status: 200,
        headers: { "content-type": "application/json" },
      })
    );

    expect(await screen.findByText("mlaas is not configured")).toBeInTheDocument();
  });

  it("shows the last error and still renders the models table when mlaas is unreachable", async () => {
    mockFetch({
      ...baseEndpoints,
      "/api/v1/mlaas/status": {
        ...connected,
        reachable: false,
        lastError: "dial tcp 127.0.0.1:8090: connect: connection refused",
      },
    });
    render(<Models />);

    expect(
      await screen.findByText("Unreachable: dial tcp 127.0.0.1:8090: connect: connection refused")
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Train forsight-cpu-forecast" })).toBeInTheDocument();
  });

  it("POSTs to the model's tune route and reports an already-queued job; the jobs table shows started/finished times", async () => {
    mockFetch({
      ...baseEndpoints,
      "/api/v1/mlaas/models/forsight-cpu-forecast/tune": () => ({ jobId: 4, alreadyQueued: true }),
      "/api/v1/mlaas/status": {
        ...connected,
        jobs: [
          {
            id: 1,
            model: "forsight-cpu-forecast",
            kind: "train",
            trigger: "manual",
            status: "done",
            createdAt: "2026-09-15T09:00:00Z",
            startedAt: "2026-09-15T09:00:05Z",
            finishedAt: "2026-09-15T09:04:00Z",
          },
          {
            id: 2,
            model: "forsight-log-severity",
            kind: "tune",
            trigger: "schedule",
            status: "running",
            createdAt: "2026-09-15T09:10:00Z",
            startedAt: "2026-09-15T09:10:02Z",
          },
        ],
      },
    });
    const user = userEvent.setup();
    render(<Models />);

    await user.click(await screen.findByRole("button", { name: "Tune forsight-cpu-forecast" }));
    expect(await screen.findByText("Job #4 already queued")).toBeInTheDocument();

    const row1 = screen.getByText("1", { selector: "td" }).closest("tr");
    const cells1 = within(row1!).getAllByRole("cell");
    // Id, Model, Kind, Trigger, Status, Started, Finished — job 1 finished.
    expect(cells1[5]).not.toHaveTextContent("—");
    expect(cells1[6]).not.toHaveTextContent("—");

    const row2 = screen.getByText("2", { selector: "td" }).closest("tr");
    const cells2 = within(row2!).getAllByRole("cell");
    // Job 2 has started but not finished yet.
    expect(cells2[5]).not.toHaveTextContent("—");
    expect(cells2[6]).toHaveTextContent("—");
  });

  // Fix 1 (MEDIUM): mlaas's own terminal success status is "done", not
  // "succeeded" — jobVariant used to leave every real "done" job neutral.
  it('maps a "done" job to the success badge variant and leaves "cancelled" neutral', async () => {
    mockFetch({
      ...baseEndpoints,
      "/api/v1/mlaas/status": {
        ...connected,
        jobs: [
          {
            id: 1,
            model: "forsight-cpu-forecast",
            kind: "train",
            trigger: "manual",
            status: "done",
            createdAt: "2026-09-15T09:00:00Z",
          },
          {
            id: 2,
            model: "forsight-cpu-forecast",
            kind: "tune",
            trigger: "manual",
            status: "cancelled",
            createdAt: "2026-09-15T09:00:00Z",
          },
        ],
      },
    });
    render(<Models />);

    const doneBadge = await screen.findByText("done");
    expect(doneBadge.className).toContain("text-success");

    const cancelledBadge = screen.getByText("cancelled");
    expect(cancelledBadge.className).toContain("text-fg-secondary");
    expect(cancelledBadge.className).not.toContain("text-success");
  });

  // Fix 6 (MEDIUM): a single `busy` string let a second in-flight action
  // clear the first's loading/disabled state.
  it("disables both Train and Tune for a row while either action is in flight", async () => {
    let resolveTrain: (value: Response) => void = () => {};
    const trainPromise = new Promise<Response>((resolve) => {
      resolveTrain = resolve;
    });
    mockFetch({
      ...baseEndpoints,
      "/api/v1/mlaas/models/forsight-cpu-forecast/train": () => trainPromise,
    });
    const user = userEvent.setup();
    render(<Models />);

    const train = await screen.findByRole("button", { name: "Train forsight-cpu-forecast" });
    const tune = screen.getByRole("button", { name: "Tune forsight-cpu-forecast" });
    expect(train).toBeEnabled();
    expect(tune).toBeEnabled();

    await user.click(train);
    expect(train).toBeDisabled();
    expect(tune).toBeDisabled();

    resolveTrain(
      new Response(JSON.stringify({ jobId: 9, alreadyQueued: false }), {
        status: 200,
        headers: { "content-type": "application/json" },
      })
    );

    expect(await screen.findByText("Job #9 queued")).toBeInTheDocument();
    expect(tune).toBeEnabled();
  });

  it("disables Train/Tune for a model mid-job and skips the mlaas half of Try when it isn't ready", async () => {
    const predictCalls: string[] = [];
    mockFetch({
      ...baseEndpoints,
      "/api/v1/forseer/classify": () => ({ severity: "warn", source: "rule", ready: false }),
      "/api/v1/mlaas/models/forsight-log-severity/predict": ({ url }) => {
        predictCalls.push(url.pathname);
        return { model: "forsight-log-severity", version: 1, predictions: [{ value: "warn" }] };
      },
      "/api/v1/mlaas/status": {
        ...connected,
        models: connected.models.map((m) =>
          m.name === "forsight-log-severity" ? { ...m, state: "training", champion: 0, activeJob: true } : m
        ),
      },
    });
    const user = userEvent.setup();
    render(<Models />);

    const row = (await screen.findByRole("button", { name: "Train forsight-log-severity" })).closest("tr");
    expect(within(row!).getByRole("button", { name: "Train forsight-log-severity" })).toBeDisabled();
    expect(within(row!).getByRole("button", { name: "Tune forsight-log-severity" })).toBeDisabled();

    await user.type(screen.getByRole("textbox", { name: "Log line" }), "disk almost full");
    await user.click(screen.getByRole("button", { name: "Classify" }));

    const forseerRow = (await screen.findByText("Forseer", { selector: "td" })).closest("tr");
    expect(forseerRow).toHaveTextContent("rule — model not ready");
    const mlaasRow = screen.getByText("mlaas", { selector: "td" }).closest("tr");
    expect(mlaasRow).toHaveTextContent("not ready");
    expect(predictCalls).toEqual([]);
  });

  it("shows Warming up and an unmeasured note for an ungraded card, omits the rule comparison with no fallback score, and shows the empty state with no cards", async () => {
    mockFetch({
      ...baseEndpoints,
      "/api/v1/forseer/models": [
        {
          name: "log severity",
          job: "Give a log line the level this deployment would have given it.",
          reads: ["log message"],
          fallback: "keyword rule",
          ready: false,
          trained: 10,
          accuracy: -1,
          fallbackAccuracy: -1,
          graded: 0,
        },
        {
          name: "cpu forecast",
          job: "Say where host CPU is heading.",
          reads: ["host.cpu.percent"],
          fallback: "",
          ready: true,
          trained: 500,
          accuracy: 0.5,
          fallbackAccuracy: -1,
          graded: 200,
        },
      ] satisfies ForseerCard[],
    });
    render(<Models />);

    expect(await screen.findByText("Warming up")).toBeInTheDocument();
    expect(screen.getByText("unmeasured — no labels for this job")).toBeInTheDocument();
    const scoreText = screen.getByText(/50% on 200 graded/);
    expect(scoreText.textContent).not.toContain("vs rule");
  });

  it("shows the No Forseer models yet empty state when there are no cards", async () => {
    mockFetch({ ...baseEndpoints, "/api/v1/forseer/models": [] });
    render(<Models />);

    expect(await screen.findByText("No Forseer models yet")).toBeInTheDocument();
  });

  // Fix 8 (MEDIUM): the page used to poll the whole metrics window every
  // 10s even with no forecast to chart it against.
  it("never requests /api/v1/metrics when there are no forecasts to chart", async () => {
    const metricsCalls: string[] = [];
    mockFetch({
      ...baseEndpoints,
      "/api/v1/metrics": ({ url }) => {
        metricsCalls.push(url.pathname + url.search);
        return [];
      },
    });
    render(<Models />);

    await screen.findByText("Connected to http://127.0.0.1:8090");
    expect(screen.getByText("No forecasts yet")).toBeInTheDocument();
    expect(metricsCalls).toEqual([]);
  });

  it("requests metrics with a since= query once there is a forecast to chart", async () => {
    const metricsCalls: string[] = [];
    mockFetch({
      ...baseEndpoints,
      "/api/v1/metrics": ({ url }) => {
        metricsCalls.push(url.pathname + url.search);
        return [];
      },
      "/api/v1/mlaas/status": {
        ...connected,
        forecasts: [
          {
            model: "forsight-cpu-forecast",
            metric: "host.cpu.percent",
            origin: "2026-09-15T10:01:00Z",
            points: [{ at: "2026-09-15T10:06:00Z", value: 47 }],
          },
        ],
      },
    });
    render(<Models />);

    await screen.findByText("Connected to http://127.0.0.1:8090");
    await waitFor(() => expect(metricsCalls.length).toBeGreaterThan(0));
    expect(metricsCalls[0]).toMatch(/^\/api\/v1\/metrics\?since=/);
  });

  it("keeps the last good mlaas status across an intermittent 502, after 10s of continued polling", async () => {
    vi.useFakeTimers();
    try {
      let call = 0;
      mockFetch({
        ...baseEndpoints,
        "/api/v1/mlaas/status": () => {
          call += 1;
          return call === 1 ? connected : new Response("", { status: 502 });
        },
      });
      render(<Models />);

      // The mount effect's first poll is a plain fetch/json/setState promise
      // chain, not a scheduled timer callback — fake timers don't advance it,
      // so it's drained with plain microtask flushes rather than
      // `advanceTimersByTimeAsync` (and `findByText`'s own polling can't be
      // used here either, since it relies on real timers this suite's
      // testing-library version can't detect as faked).
      await act(async () => {
        for (let i = 0; i < 10; i++) await Promise.resolve();
      });
      expect(screen.getByText("Connected to http://127.0.0.1:8090")).toBeInTheDocument();

      // The 10s interval fires a second poll, which 502s — the hook must
      // keep showing the last good snapshot instead of reverting to a
      // pending or not-configured state.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10000);
      });
      expect(screen.getByText("Connected to http://127.0.0.1:8090")).toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("Models accessibility", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // Regression coverage for the section-heading outline and the mlaas
  // connection announcement: every Card on this page sits as a direct child
  // of the page, right under its own <h1> ("Models"), so each
  // CardSectionHeading must render as an <h2> (CardTitle's own default,
  // <h3>, would jump a level) and MlaasModelsCard's StatusDot must sit in a
  // named live region, not bare in the header — axe's heading-order and
  // aria-required-children/name rules catch exactly those.
  it("has no axe violations once connected, with models, forecasts and a Forseer card all rendered", async () => {
    mockFetch(baseEndpoints);
    const { container } = render(<Models />);
    await screen.findByText("Connected to http://127.0.0.1:8090");

    expect(await axe(container)).toHaveNoViolations();
  });

  it("has no axe violations in the not-configured empty state", async () => {
    mockFetch({ ...baseEndpoints, "/api/v1/mlaas/status": notConfigured });
    const { container } = render(<Models />);
    await screen.findByText("mlaas is not configured");

    expect(await axe(container)).toHaveNoViolations();
  });
});

describe("forecastChart", () => {
  // Fix 2 (MEDIUM): the x-axis used to be a plain "observed then forecast"
  // concatenation, which went non-monotonic once observed history (the
  // newest 60 one-minute buckets) ran past the forecast's origin.
  it("stays strictly increasing, with both series the same length as the labels, once observed history runs past the origin", () => {
    const metrics: Metric[] = [];
    for (let minute = 0; minute <= 20; minute++) {
      metrics.push({
        name: "host.cpu.percent",
        value: 40 + minute,
        timestamp: `2026-09-15T10:${String(minute).padStart(2, "0")}:30Z`,
      });
    }
    const forecast: MlaasForecast = {
      model: "forsight-cpu-forecast",
      metric: "host.cpu.percent",
      // Ten minutes before the newest observed bucket — a forecast made a
      // while ago that the operator is now looking back on.
      origin: "2026-09-15T10:05:00Z",
      points: [
        { at: "2026-09-15T10:10:00Z", value: 50 },
        { at: "2026-09-15T10:15:00Z", value: 52 },
      ],
    };

    const chart = forecastChart(forecast, metrics);

    expect(chart.observed).toHaveLength(chart.labels.length);
    expect(chart.forecast).toHaveLength(chart.labels.length);
    expect(new Set(chart.labels).size).toBe(chart.labels.length);
    for (let i = 1; i < chart.labels.length; i++) {
      expect(chart.labels[i] > chart.labels[i - 1]).toBe(true);
    }
    // Observed isn't clipped at the origin: it keeps going through 10:20,
    // past the last forecast point at 10:15.
    expect(chart.observed[chart.observed.length - 1]).not.toBeNull();
  });

  it("points dashedFrom at the first forecast minute, so the whole projection is drawn dashed", () => {
    const metrics: Metric[] = [
      { name: "host.cpu.percent", value: 40, timestamp: "2026-09-15T10:00:30Z" },
      { name: "host.cpu.percent", value: 41, timestamp: "2026-09-15T10:01:30Z" },
    ];
    const forecast: MlaasForecast = {
      model: "forsight-cpu-forecast",
      metric: "host.cpu.percent",
      origin: "2026-09-15T10:01:00Z",
      points: [
        { at: "2026-09-15T10:06:00Z", value: 47 },
        { at: "2026-09-15T10:16:00Z", value: 50 },
      ],
    };

    const chart = forecastChart(forecast, metrics);

    // Two observed minutes, then the forecast: the series is null up to the
    // index dashedFrom names and carries the first forecast value there.
    expect(chart.forecastFrom).toBe(2);
    expect(chart.forecast.slice(0, 2)).toEqual([null, null]);
    expect(chart.forecast[2]).toBe(47);
  });

  it("has no dashedFrom when the forecast carries no points", () => {
    const forecast: MlaasForecast = {
      model: "forsight-cpu-forecast",
      metric: "host.cpu.percent",
      origin: "2026-09-15T10:01:00Z",
      points: [],
    };
    expect(forecastChart(forecast, []).forecastFrom).toBeUndefined();
  });
});
