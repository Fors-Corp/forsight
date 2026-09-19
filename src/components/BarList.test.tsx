import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import { BarList } from "./BarList";

const items = [
  { label: "/api/checkout", value: 1240 },
  { label: "/api/search", value: 620 },
  { label: "/api/health", value: 0 },
];

describe("BarList", () => {
  it("prints every value as text next to its bar", () => {
    render(<BarList items={items} />);
    expect(screen.getByText("1.2k")).toBeInTheDocument();
    expect(screen.getByText("620")).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });

  it("scales bars against the largest value by default", () => {
    const { container } = render(<BarList items={items} />);
    const bars = container.querySelectorAll("span[aria-hidden='true']");
    expect((bars[0] as HTMLElement).style.width).toBe("100%");
    expect((bars[2] as HTMLElement).style.width).toBe("0%");
  });

  it("accepts an explicit ceiling so sibling lists share a scale", () => {
    const { container } = render(<BarList items={items} max={2480} />);
    const bars = container.querySelectorAll("span[aria-hidden='true']");
    expect((bars[0] as HTMLElement).style.width).toBe("50%");
  });

  it("survives an all-zero list without dividing by zero", () => {
    const { container } = render(<BarList items={[{ label: "none", value: 0 }]} />);
    const bar = container.querySelector("span[aria-hidden='true']") as HTMLElement;
    expect(bar.style.width).toBe("0%");
  });

  it("makes rows links when a drill-down is given", () => {
    render(<BarList items={[{ ...items[0], href: "/traces?route=checkout" }]} />);
    expect(screen.getByRole("link", { name: /\/api\/checkout/ })).toHaveAttribute(
      "href",
      "/traces?route=checkout"
    );
  });

  it("renders the non-link branch for a row whose href is an unsafe scheme", () => {
    render(<BarList items={[{ ...items[0], href: "javascript:alert(document.cookie)" }]} />);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("/api/checkout")).toBeInTheDocument();
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<BarList items={[{ ...items[0], href: "#x" }, items[1]]} />);
    expect(await axe(container)).toHaveNoViolations();
  });
});
