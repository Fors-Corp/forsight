import type { Meta, StoryObj } from "@storybook/react";
import { ChartLegend } from "./ChartLegend";

const meta: Meta<typeof ChartLegend> = {
  title: "Forsight/Data Viz/ChartLegend",
  component: ChartLegend,
  parameters: {
    docs: {
      description: {
        component:
          "Series key for a chart. Rendered automatically by the multi-series charts; export it for custom plots built on ChartFrame.",
      },
    },
  },
};
export default meta;
type Story = StoryObj<typeof ChartLegend>;

export const SingleSeries: Story = {
  name: "Single series, no value",
  render: () => <ChartLegend items={[{ label: "Response time" }]} />,
};

export const SingleSeriesWithValue: Story = {
  name: "Single series, with value",
  render: () => <ChartLegend items={[{ label: "Response time", value: "142ms" }]} />,
};

export const MultiSeries: Story = {
  name: "Multi series, no value",
  render: () => (
    <ChartLegend
      items={[
        { label: "us-east", seriesIndex: 0 },
        { label: "eu-west", seriesIndex: 1 },
        { label: "Other", seriesIndex: 2 },
      ]}
    />
  ),
};

export const MultiSeriesWithValues: Story = {
  name: "Multi series, with values",
  render: () => (
    <ChartLegend
      items={[
        { label: "us-east", seriesIndex: 0, value: "62%" },
        { label: "eu-west", seriesIndex: 1, value: "28%" },
        { label: "Other", value: "10%" },
      ]}
    />
  ),
};
