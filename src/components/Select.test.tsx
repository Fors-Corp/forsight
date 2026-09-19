import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { axe } from "../test-utils/axe";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "./Select";

function ExampleSelect() {
  return (
    <Select>
      <SelectTrigger aria-label="Deploy target">
        <SelectValue placeholder="Choose a target" />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="vercel">Vercel</SelectItem>
        <SelectItem value="netlify">Netlify</SelectItem>
      </SelectContent>
    </Select>
  );
}

describe("Select", () => {
  // Keyboard rather than click: Radix Select's click gesture depends on real
  // pointer-capture semantics jsdom doesn't implement, which made a
  // click-driven version of this test unreliable. Keyboard interaction is
  // the code path Radix documents as fully supported and exercises the same
  // open + select behavior in a way jsdom can actually simulate.
  it("opens and selects an item via keyboard", async () => {
    render(<ExampleSelect />);
    const trigger = screen.getByRole("combobox", { name: "Deploy target" });
    trigger.focus();
    await userEvent.keyboard("{Enter}");
    await screen.findByRole("listbox");
    await userEvent.keyboard("{ArrowDown}{Enter}");
    expect(trigger).toHaveTextContent("Netlify");
  });

  it("has no accessibility violations when closed", async () => {
    const { container } = render(<ExampleSelect />);
    expect(await axe(container)).toHaveNoViolations();
  });

  it("sizes the panel to the trigger width when using popper positioning", async () => {
    render(
      <Select>
        <SelectTrigger aria-label="Deploy target">
          <SelectValue placeholder="Choose a target" />
        </SelectTrigger>
        <SelectContent position="popper">
          <SelectItem value="vercel">Vercel</SelectItem>
        </SelectContent>
      </Select>
    );
    screen.getByRole("combobox", { name: "Deploy target" }).focus();
    await userEvent.keyboard("{Enter}");
    const listbox = await screen.findByRole("listbox");
    expect(listbox).toHaveClass("min-w-[var(--radix-select-trigger-width)]");
  });
});
