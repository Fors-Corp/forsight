import { expect, userEvent, waitFor } from "storybook/test";

/**
 * Proves a plotted chart's keyboard-cursor tooltip parks on the physical side
 * opposite the active mark — the same side under `dir="ltr"` and `dir="rtl"`,
 * because the plot's x-axis is a physical pixel space that never mirrors
 * (see `ChartFrame.tsx`, "time flows left-to-right in both text directions").
 *
 * The check is rendered geometry (`getBoundingClientRect`), not `toHaveClass`:
 * a logical `start-2`/`end-2` class reads the same in the DOM whichever way
 * the browser resolves it, and only the painted position shows which physical
 * side it landed on — the reason `Switch.stories.tsx`'s "RTL" story measures
 * its thumb the same way.
 *
 * Expects the story to render the chart twice, once under each of
 * `<div dir="ltr">` and `<div dir="rtl">`. `activeMark` selects, inside the
 * chart's focusable plot wrapper, the SVG element the chart highlights at the
 * cursor index; it is re-queried after every key press because the charts
 * re-render that mark as the cursor moves.
 */
export async function expectTooltipOppositeCursor(
  canvasElement: HTMLElement,
  activeMark: string
): Promise<void> {
  // The tooltip wrapper is the plot's only absolutely positioned child.
  const tooltip = ":scope > div.absolute";

  for (const dir of ["ltr", "rtl"] as const) {
    const plot = canvasElement.querySelector<HTMLElement>(`[dir="${dir}"] [tabindex="0"]`);
    if (!plot) throw new Error(`No focusable plot rendered under dir="${dir}"`);
    const rectOf = (selector: string) => {
      const node = plot.querySelector(selector);
      if (!node) throw new Error(`${selector} not found under dir="${dir}" with the cursor active`);
      return node.getBoundingClientRect();
    };

    plot.focus();

    // Both geometry reads poll through `waitFor` rather than reading once:
    // Chromium answers an SVG element's `getBoundingClientRect()` from the
    // last painted frame when only its geometry attributes changed, and a
    // chart that moves one mark in place as the cursor moves (ComboChart's
    // `<circle>`, whose `cx` React updates without remounting) can still
    // report the previous slot's position on the first read after a key
    // press. Polling is the same assertion a frame later; a tooltip on the
    // wrong side never converges and fails with the real numbers.

    // Last slot: the cursor sits in the right half of the plot, so the
    // readout parks on the left and its right edge stops short of the mark.
    await userEvent.keyboard("{End}");
    await waitFor(() =>
      expect(rectOf(tooltip).right).toBeLessThanOrEqual(rectOf(activeMark).left + 2)
    );

    // First slot: the mirror image — readout on the right, clear of the mark.
    await userEvent.keyboard("{Home}");
    await waitFor(() =>
      expect(rectOf(tooltip).left).toBeGreaterThanOrEqual(rectOf(activeMark).right - 2)
    );

    await userEvent.keyboard("{Escape}");
    await expect(plot.querySelector(tooltip)).toBeNull();
  }
}
