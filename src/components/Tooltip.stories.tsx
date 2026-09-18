import type { Meta, StoryObj } from "@storybook/react";
import { expect, screen, userEvent, waitFor, within } from "storybook/test";
import { TooltipProvider, Tooltip, TooltipTrigger, TooltipContent } from "./Tooltip";
import { Button } from "./Button";

const meta: Meta = {
  title: "Forsight/Overlays/Tooltip",
  parameters: { layout: "fullscreen" },
};
export default meta;
type Story = StoryObj;

export const Default: Story = {
  render: () => (
    <div className="flex h-40 items-center justify-center">
      <TooltipProvider>
        <Tooltip defaultOpen>
          <TooltipTrigger asChild>
            <Button variant="ghost">Redeploy</Button>
          </TooltipTrigger>
          <TooltipContent>Rebuilds from the last successful commit</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>
  ),
  play: async () => {
    // Radix renders the visible tip plus a visually-hidden copy for SR — both count.
    const tips = await screen.findAllByText("Rebuilds from the last successful commit");
    await expect(tips.length).toBeGreaterThan(0);
  },
};

/**
 * Real-browser interaction: opens on hover and closes when the pointer
 * leaves or on Escape — jsdom can't simulate hover reliably, so this runs
 * only in the Storybook test runner. Touch never reaches this open path at
 * all (see `Tooltip.tsx`'s doc comment).
 */
export const OpensOnHoverClosesOnUnhoverOrEscape: Story = {
  render: () => (
    <div className="flex h-40 items-center justify-center">
      <TooltipProvider delayDuration={0}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost">Redeploy</Button>
          </TooltipTrigger>
          <TooltipContent>Rebuilds from the last successful commit</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const trigger = canvas.getByRole("button", { name: "Redeploy" });

    await userEvent.hover(trigger);
    await screen.findByText("Rebuilds from the last successful commit");

    await userEvent.unhover(trigger);
    await userEvent.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByText("Rebuilds from the last successful commit")).not.toBeInTheDocument()
    );
  },
};
