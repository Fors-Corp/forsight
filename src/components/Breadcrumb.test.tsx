import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { axe } from "../test-utils/axe";
import {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "./Breadcrumb";

function ExampleBreadcrumb() {
  return (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="/projects">Projects</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator />
        <BreadcrumbItem>
          <BreadcrumbPage>Settings</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  );
}

describe("Breadcrumb", () => {
  it("marks the current page distinctly from links", () => {
    render(<ExampleBreadcrumb />);
    expect(screen.getByRole("link", { name: "Projects" })).toBeInTheDocument();
    expect(screen.getByText("Settings")).toHaveAttribute("aria-current", "page");
  });

  it("has no accessibility violations", async () => {
    const { container } = render(<ExampleBreadcrumb />);
    expect(await axe(container)).toHaveNoViolations();
  });
});

describe("BreadcrumbLink", () => {
  it("renders its non-link branch for a javascript: href", () => {
    render(<BreadcrumbLink href="javascript:alert(document.cookie)">Projects</BreadcrumbLink>);
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
    expect(screen.getByText("Projects")).toBeInTheDocument();
  });
});
