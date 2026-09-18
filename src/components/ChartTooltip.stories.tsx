import type { Meta, StoryObj } from "@storybook/react";
import { ChartTooltip } from "./ChartTooltip";

const meta: Meta<typeof ChartTooltip> = {
  title: "Forsight/Data Viz/ChartTooltip",
  component: ChartTooltip,
  parameters: {
    docs: {
      description: {
        component:
          "Readout surface for a chart's hover/keyboard cursor. The plotted charts position one for you; export it for custom plots built on ChartFrame. It's aria-hidden — the same reading is announced by the chart's own live region.",
      },
    },
  },
};
export default meta;
type Story = StoryObj<typeof ChartTooltip>;

export const SingleSeries: Story = {
  name: "Single series",
  render: () => <ChartTooltip title="14:00" rows={[{ label: "Response time", value: "142ms" }]} />,
};

export const MultiSeries: Story = {
  name: "Multi series",
  render: () => (
    <ChartTooltip
      title="14:00"
      rows={[
        { label: "us-east", seriesIndex: 0, value: "1.8k" },
        { label: "eu-west", seriesIndex: 1, value: "820" },
        { label: "Other", value: "140" },
      ]}
    />
  ),
};
