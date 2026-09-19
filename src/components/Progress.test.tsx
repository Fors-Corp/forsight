import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import { Progress } from "./Progress";

describe("Progress", () => {
  it("exposes its value via ARIA", () => {
    render(<Progress value={42} aria-label="Upload progress" />);
    const bar = screen.getByRole("progressbar", { name: "Upload progress" });
    expect(bar).toHaveAttribute("aria-valuenow", "42");
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<Progress value={42} aria-label="Upload progress" />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("collapses the indicator and drops aria-valuenow when value is omitted", () => {
    const { container } = render(<Progress aria-label="Loading" />);
    const bar = screen.getByRole("progressbar", { name: "Loading" });
    expect(bar).not.toHaveAttribute("aria-valuenow");
    const indicator = container.querySelector("[style]") as HTMLElement;
    // value ?? 0 -> 100 - 0 = 100% translated out of view, same resting
    // position as an explicit value={0}.
    expect(indicator.style.transform).toBe("translateX(-100%)");
  });
});
