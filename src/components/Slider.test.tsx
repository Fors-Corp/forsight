import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "../test-utils/axe";
import { Slider } from "./Slider";

describe("Slider", () => {
  it("renders a thumb per value", () => {
    render(<Slider defaultValue={[20, 80]} max={100} aria-label="Range" />);
    expect(screen.getAllByRole("slider")).toHaveLength(2);
  });

  it("exposes the current value via ARIA", () => {
    render(<Slider defaultValue={[40]} max={100} aria-label="Volume" />);
    expect(screen.getByRole("slider", { name: "Volume" })).toHaveAttribute("aria-valuenow", "40");
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<Slider defaultValue={[40]} max={100} aria-label="Volume" />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("falls back to a single thumb at zero when neither value nor defaultValue is given", () => {
    render(<Slider max={100} aria-label="Untouched" />);
    const thumbs = screen.getAllByRole("slider");
    expect(thumbs).toHaveLength(1);
    expect(thumbs[0]).toHaveAttribute("aria-valuenow", "0");
  });

  it("reads its position from a controlled value", () => {
    render(<Slider value={[30]} max={100} aria-label="Controlled" onValueChange={() => {}} />);
    expect(screen.getByRole("slider", { name: "Controlled" })).toHaveAttribute(
      "aria-valuenow",
      "30"
    );
  });

  it("labels each thumb individually for a range slider", () => {
    render(<Slider defaultValue={[20, 80]} max={100} aria-label={["Min price", "Max price"]} />);
    expect(screen.getByRole("slider", { name: "Min price" })).toHaveAttribute(
      "aria-valuenow",
      "20"
    );
    expect(screen.getByRole("slider", { name: "Max price" })).toHaveAttribute(
      "aria-valuenow",
      "80"
    );
  });

  it("moves the thumb and updates aria-valuenow on ArrowRight/ArrowLeft", async () => {
    render(<Slider defaultValue={[40]} max={100} aria-label="Volume" />);
    const thumb = screen.getByRole("slider", { name: "Volume" });
    thumb.focus();

    await userEvent.keyboard("{ArrowRight}");
    expect(thumb).toHaveAttribute("aria-valuenow", "41");

    await userEvent.keyboard("{ArrowLeft}{ArrowLeft}");
    expect(thumb).toHaveAttribute("aria-valuenow", "39");
  });

  it("jumps to the min/max bounds on Home/End", async () => {
    render(<Slider defaultValue={[40]} max={100} aria-label="Volume" />);
    const thumb = screen.getByRole("slider", { name: "Volume" });
    thumb.focus();

    await userEvent.keyboard("{End}");
    expect(thumb).toHaveAttribute("aria-valuenow", "100");

    await userEvent.keyboard("{Home}");
    expect(thumb).toHaveAttribute("aria-valuenow", "0");
  });
});
