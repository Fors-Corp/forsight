import type { Meta, StoryObj } from "@storybook/react";
import { expect, userEvent, within, waitFor } from "storybook/test";
import { CopyButton } from "./CopyButton";

const meta: Meta<typeof CopyButton> = {
  title: "Forsight/Data Display/CopyButton",
  component: CopyButton,
};
export default meta;
type Story = StoryObj<typeof CopyButton>;

export const Default: Story = {
  render: () => <CopyButton value="npm install @fors-corp/forsight" />,
};

export const InContext: Story = {
  render: () => (
    <div className="flex w-96 items-center justify-between rounded-md border border-ink-border bg-ink-bg px-3 py-2 font-mono text-sm text-fg">
      <span className="truncate">sk_live_51Hc9...a8Fn</span>
      <CopyButton value="sk_live_51Hc9examplea8Fn" label="Copy API key" />
    </div>
  ),
};

/**
 * Real-browser interaction: click copies and briefly shows the confirmed
 * state. Doesn't assert on `navigator.clipboard.readText()` — the test
 * runner's browser context has no `clipboard-read` permission granted, and
 * the accessible-name flip to "Copied!" only happens after
 * `writeText()` resolves, so it's already proof the copy succeeded.
 */
export const CopyConfirmation: Story = {
  render: () => <CopyButton value="hello world" resetAfter={100000} />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const button = canvas.getByRole("button", { name: "Copy" });
    await userEvent.click(button);
    await waitFor(() =>
      expect(canvas.getByRole("button", { name: "Copied!" })).toBeInTheDocument()
    );
  },
};
