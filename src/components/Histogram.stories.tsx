import type { Meta, StoryObj } from "@storybook/react";
import { Histogram } from "./Histogram";
import { expectTooltipOppositeCursor } from "../test-utils/chart-cursor-side";

const meta: Meta<typeof Histogram> = {
  title: "Forsight/Data Viz/Histogram",
  component: Histogram,
  parameters: { layout: "fullscreen" },
};
export default meta;
type Story = StoryObj<typeof Histogram>;

export const RequestDuration: Story = {
  render: () => (
    <div className="p-8">
      <Histogram
        label="Request duration distribution, last hour"
        description="p95 is in the 100–150ms bucket."
        buckets={[
          { label: "0–25ms", count: 420 },
          { label: "25–50ms", count: 980 },
          { label: "50–100ms", count: 1640 },
          { label: "100–150ms", count: 860 },
          { label: "150–250ms", count: 310 },
          { label: "250ms+", count: 90 },
        ]}
      />
    </div>
  ),
};

export const SingleBucket: Story = {
  render: () => (
    <div className="p-8">
      <Histogram label="Payload size" buckets={[{ label: "0–1KB", count: 40 }]} />
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
          <Histogram
            label={`Request duration distribution, ${dir.toUpperCase()}`}
            buckets={[
              { label: "0–25ms", count: 420 },
              { label: "25–50ms", count: 980 },
              { label: "50–100ms", count: 1640 },
              { label: "100–150ms", count: 860 },
              { label: "150–250ms", count: 310 },
              { label: "250ms+", count: 90 },
            ]}
          />
        </div>
      ))}
    </div>
  ),
  play: async ({ canvasElement }) => {
    // Every bar but the active one takes `opacity-45`, so the bar without it
    // is the one the cursor is reading.
    await expectTooltipOppositeCursor(canvasElement, "svg path.fill-accent:not(.opacity-45)");
  },
};
