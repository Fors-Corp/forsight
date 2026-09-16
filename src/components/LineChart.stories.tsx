import type { Meta, StoryObj } from "@storybook/react";
import { expect, userEvent, within } from "storybook/test";
import { LineChart } from "./LineChart";

const hours = Array.from({ length: 24 }, (_, i) => `${String(i).padStart(2, "0")}:00`);
const rps = [
  820, 760, 690, 640, 610, 640, 780, 1120, 1580, 1720, 1690, 1740, 1810, 1770, 1690, 1620, 1580,
  1490, 1380, 1240, 1120, 1010, 940, 870,
];
const p95 = [
  180, 172, 168, 165, 162, 170, 195, 240, 320, 356, 344, 351, 388, 372, 340, 318, 305, 288, 262,
  240, 224, 210, 198, 190,
];

const meta: Meta<typeof LineChart> = {
  title: "Forsight/Data Viz/LineChart",
  component: LineChart,
  decorators: [
    (Story) => (
      <div className="w-full max-w-3xl p-4 sm:p-8">
        <Story />
      </div>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof LineChart>;

export const SingleSeries: Story = {
  args: {
    label: "Requests per second",
    description: "checkout-api, last 24 hours",
    labels: hours,
    series: [{ name: "checkout-api", values: rps }],
    area: true,
  },
};

export const MultipleSeries: Story = {
  args: {
    label: "p95 latency by region",
    description: "Milliseconds, last 24 hours",
    labels: hours,
    series: [
      { name: "us-east", values: p95 },
      { name: "eu-west", values: p95.map((v) => Math.round(v * 0.82)) },
      { name: "ap-south", values: p95.map((v) => Math.round(v * 1.31)) },
    ],
    valueFormat: (value) => `${value}ms`,
  },
};

export const WithGaps: Story = {
  name: "Missing samples",
  args: {
    label: "Queue depth",
    description: "The collector dropped two scrapes — the line breaks rather than inventing them.",
    labels: hours.slice(0, 10),
    series: [{ name: "worker", values: [12, 18, 24, null, null, 31, 26, 19, 14, 9] }],
  },
};

export const WithAnnotations: Story = {
  name: "With annotations",
  args: {
    label: "p95 latency, last 24 hours",
    description: "checkout-api",
    labels: hours,
    series: [{ name: "checkout-api", values: p95 }],
    annotations: [
      { value: 300, text: "SLO: 300ms", tone: "danger" },
      { label: "08:00", text: "Deploy v2.4.1", tone: "accent" },
    ],
  },
};

export const Projection: Story = {
  name: "Projection",
  args: {
    label: "p95 latency, next 3 hours",
    description: "checkout-api — dashed past 23:00 is the mlaas forecast, not a measurement.",
    labels: [...hours, "24:00", "25:00", "26:00"],
    series: [
      {
        name: "checkout-api",
        // dashedFrom is the last real sample (index 23, "23:00" — the
        // forecast origin). The three mlaas-predicted values after it draw
        // dashed, sharing that boundary point so the line has no gap.
        values: [...p95, 176, 162, 150],
        dashedFrom: 23,
      },
    ],
    valueFormat: (value) => `${value}ms`,
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    // The dashed stroke is the sighted-only half of the signal; the hidden
    // description (both the SVG <desc> and the sr-only table <caption>)
    // carries the same fact for a screen reader.
    await expect(canvas.getAllByText(/projected from 23:00/)).toHaveLength(2);
  },
};

export const KeyboardCursor: Story = {
  name: "Keyboard cursor",
  args: { ...SingleSeries.args } as Story["args"],
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const plot = canvasElement.querySelector("[tabindex='0']") as HTMLElement;
    plot.focus();
    await userEvent.keyboard("{End}");
    // Reading the last point out loud is what a mouse hover would show.
    await expect(canvas.getByRole("status")).toHaveTextContent("23:00");
    await userEvent.keyboard("{Escape}");
    await expect(canvas.getByRole("status")).toHaveTextContent("");
  },
};
