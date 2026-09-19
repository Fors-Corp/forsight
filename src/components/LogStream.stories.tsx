import type { Meta, StoryObj } from "@storybook/react";
import { LogStream, type LogEntry } from "./LogStream";

const entries: LogEntry[] = [
  {
    id: "1",
    timestamp: "14:02:03.114",
    level: "info",
    message: "starting rollout 4f21a",
    source: "deployer",
  },
  {
    id: "2",
    timestamp: "14:02:04.902",
    level: "debug",
    message: "pulling image ghcr.io/forsight/checkout:4f21a",
    source: "kubelet",
  },
  {
    id: "3",
    timestamp: "14:02:11.338",
    level: "info",
    message: "2/4 pods ready",
    source: "kubelet",
  },
  {
    id: "4",
    timestamp: "14:02:19.771",
    level: "warn",
    message: "readiness probe failed: connect: connection refused",
    source: "checkout-7f9",
  },
  {
    id: "5",
    timestamp: "14:02:22.010",
    level: "error",
    message: "dial tcp 10.4.1.22:5432: i/o timeout — falling back to replica",
    source: "checkout-7f9",
  },
  {
    id: "6",
    timestamp: "14:02:31.556",
    level: "info",
    message: "4/4 pods ready, rollout complete",
    source: "deployer",
  },
];

const meta: Meta<typeof LogStream> = {
  title: "Forsight/Observability/LogStream",
  component: LogStream,
  decorators: [
    (Story) => (
      <div className="w-full max-w-3xl p-4 sm:p-8">
        <Story />
      </div>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof LogStream>;

export const Default: Story = {
  args: { label: "checkout-api production logs", entries },
};

export const DeployFeed: Story = {
  name: "Announced deploy feed",
  args: {
    label: "Rollout 4f21a",
    entries,
    announce: true,
    maxHeight: 200,
  },
};

export const Empty: Story = {
  args: { label: "checkout-api production logs", entries: [] },
};

const manyEntries: LogEntry[] = Array.from({ length: 2000 }, (_, i) => ({
  id: String(i),
  timestamp: `14:${String(Math.floor(i / 60)).padStart(2, "0")}:${String(i % 60).padStart(2, "0")}`,
  level: (["debug", "info", "info", "warn", "error"] as const)[i % 5],
  message: `request ${2000 - i} — ${i % 7 === 0 ? "slow query on replica-3" : "handled"}`,
  source: i % 3 === 0 ? "checkout-7f9" : undefined,
}));

export const LargeVolume: Story = {
  name: "2000 entries (windowed)",
  args: { label: "checkout-api production logs", entries: manyEntries },
};
