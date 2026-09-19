import type { Meta, StoryObj } from "@storybook/react";
import { expect, within } from "storybook/test";
import { RadioGroup, RadioGroupItem } from "./RadioGroup";

const meta: Meta = {
  title: "Forsight/Forms/RadioGroup",
};
export default meta;
type Story = StoryObj;

export const Default: Story = {
  render: () => (
    <RadioGroup defaultValue="hobby" className="flex flex-col gap-2">
      {[
        ["hobby", "Hobby — free"],
        ["pro", "Pro — $29/month"],
        ["enterprise", "Enterprise — custom pricing"],
      ].map(([value, label]) => (
        <div key={value} className="flex items-center gap-2">
          <RadioGroupItem value={value} id={value} />
          <label htmlFor={value} className="font-sans text-sm text-fg">
            {label}
          </label>
        </div>
      ))}
    </RadioGroup>
  ),
};

export const MinimumTouchTarget: Story = {
  render: () => (
    <RadioGroup defaultValue="hobby" aria-label="Plan">
      <RadioGroupItem value="hobby" aria-label="Hobby" />
    </RadioGroup>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const radio = canvas.getByRole("radio", { name: "Hobby" });
    const rect = radio.getBoundingClientRect();
    // WCAG 2.5.8: minimum 24x24 CSS px touch target.
    await expect(rect.width).toBeGreaterThanOrEqual(24);
    await expect(rect.height).toBeGreaterThanOrEqual(24);
  },
};
