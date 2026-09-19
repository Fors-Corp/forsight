import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "./Card";

describe("Card", () => {
  it("renders composed content", () => {
    render(
      <Card>
        <CardHeader>
          <CardTitle>Rapids plan</CardTitle>
          <CardDescription>For growing teams</CardDescription>
        </CardHeader>
        <CardContent>Unlimited projects</CardContent>
      </Card>
    );
    expect(screen.getByText("Rapids plan")).toBeInTheDocument();
    expect(screen.getByText("For growing teams")).toBeInTheDocument();
    expect(screen.getByText("Unlimited projects")).toBeInTheDocument();
  });

  it("has no accessibility violations", async () => {
    const { container } = render(
      <Card>
        <CardHeader>
          <CardTitle>Rapids plan</CardTitle>
        </CardHeader>
      </Card>
    );
    expect(await axe(container)).toHaveNoViolations();
  });

  it("renders CardTitle as an h3 by default", () => {
    render(<CardTitle>Rapids plan</CardTitle>);
    expect(screen.getByText("Rapids plan").tagName).toBe("H3");
  });

  it("renders CardTitle at a different heading level via `as`, to fit the page outline", () => {
    render(<CardTitle as="h2">Rapids plan</CardTitle>);
    expect(screen.getByText("Rapids plan").tagName).toBe("H2");
  });

  it("adds hover styling when interactive is set", () => {
    const { container } = render(<Card interactive>Clickable body</Card>);
    const card = container.firstChild as HTMLElement;
    expect(card).toHaveClass("cursor-pointer");
    expect(card).toHaveClass("hover:border-accent");
  });

  it("omits hover styling by default", () => {
    const { container } = render(<Card>Static body</Card>);
    const card = container.firstChild as HTMLElement;
    expect(card).not.toHaveClass("cursor-pointer");
    expect(card).not.toHaveClass("hover:border-accent");
  });
});
