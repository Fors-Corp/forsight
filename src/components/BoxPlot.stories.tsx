import type { Meta, StoryObj } from "@storybook/react";
import { BoxPlot } from "./BoxPlot";
import { formatDuration } from "../lib/chart";
import { expectTooltipOppositeCursor } from "../test-utils/chart-cursor-side";

const meta: Meta<typeof BoxPlot> = {
  title: "Forsight/Data Viz/BoxPlot",
  component: BoxPlot,
  parameters: { layout: "fullscreen" },
};
export default meta;
type Story = StoryObj<typeof BoxPlot>;

export const LatencyByService: Story = {
  render: () => (
    <div className="p-8">
      <BoxPlot
        label="Request duration spread by service, last hour"
        valueFormat={formatDuration}
        boxes={[
          { label: "checkout", min: 40, q1: 80, median: 120, q3: 180, max: 420 },
          { label: "search", min: 20, q1: 35, median: 50, q3: 70, max: 160 },
          { label: "payments", min: 90, q1: 140, median: 210, q3: 310, max: 680 },
        ]}
      />
    </div>
  ),
};

export const SingleBox: Story = {
  render: () => (
    <div className="p-8">
      <BoxPlot
        label="Payload size"
        boxes={[{ label: "checkout", min: 1, q1: 2, median: 3, q3: 5, max: 12 }]}
      />
    </div>
  ),
};

/**
 * Fixed LTR/RTL comparison for the keyboard-cursor tooltip. The plot's x-axis
 * never mirrors under `dir="rtl"` (see `ChartFrame.tsx`), so the readout parks
 * on the physical side opposite the cursor in both directions — verified by
 * rendered position, not `toHaveClass`; see `expectTooltipOppositeCursor` and
 * `Switch.stories.tsx`'s "RTL" story for why.
 */
export const RTL: Story = {
  render: () => (
    <div className="flex flex-col gap-8 p-8">
      {(["ltr", "rtl"] as const).map((dir) => (
        <div key={dir} dir={dir}>
          <BoxPlot
            label={`Request duration spread by service, ${dir.toUpperCase()}`}
            valueFormat={formatDuration}
            boxes={[
              { label: "checkout", min: 40, q1: 80, median: 120, q3: 180, max: 420 },
              { label: "search", min: 20, q1: 35, median: 50, q3: 70, max: 160 },
              { label: "payments", min: 90, q1: 140, median: 210, q3: 310, max: 680 },
            ]}
          />
        </div>
      ))}
    </div>
  ),
  play: async ({ canvasElement }) => {
    // Every box but the active one is dimmed to opacity 0.45, so the one still
    // at full opacity is the glyph the cursor is reading.
    await expectTooltipOppositeCursor(canvasElement, 'svg g[opacity="1"]');
  },
};
