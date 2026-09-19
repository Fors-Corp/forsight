import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ErrorBoundary } from "./ErrorBoundary";

/** Throws on render, unconditionally — stands in for the live trigger this
 *  boundary exists for (Models.tsx's drift cell calling `.toFixed` on a
 *  field a separately-versioned mlaas response can leave out) without
 *  needing that page's whole data shape here. */
function Boom(): never {
  throw new Error("boom");
}

function Fine() {
  return <p>fine</p>;
}

describe("ErrorBoundary", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders its children normally when nothing throws", () => {
    render(
      <ErrorBoundary>
        <Fine />
      </ErrorBoundary>
    );

    expect(screen.getByText("fine")).toBeInTheDocument();
    expect(screen.queryByText("Something went wrong")).not.toBeInTheDocument();
  });

  // The core regression: before this component existed, a throw here
  // propagated past main.tsx's `StrictMode > App` with no boundary of any
  // kind, unmounting the whole root and leaving the test's container (and
  // a real page) empty. Rendering the fallback instead — not a blank
  // container — is exactly what ErrorBoundary is for.
  it("renders the fallback, not a blank page, when a child throws during render", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

    const { container } = render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>
    );

    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reload page" })).toBeInTheDocument();
    expect(container).not.toBeEmptyDOMElement();

    consoleError.mockRestore();
  });

  it("logs the error and component stack instead of swallowing it silently", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});

    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>
    );

    const loggedThisBoundary = consoleError.mock.calls.some(
      (call) => call[0] === "forsight dashboard: a page failed to render"
    );
    expect(loggedThisBoundary).toBe(true);

    consoleError.mockRestore();
  });

  it("reloads the page when the fallback's action is clicked", async () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    // jsdom's own location.reload is a real, non-configurable method that
    // logs "Not implemented: navigation" rather than actually reloading —
    // and isn't individually replaceable (Object.defineProperty on just
    // `reload` throws "Cannot redefine property"). `location` itself,
    // as a property of `window`, is configurable, so the whole object is
    // swapped for one that carries every other live property/method
    // (assign, replace, hash, href, ...) plus a mockable `reload`.
    const originalLocation = window.location;
    const reload = vi.fn();
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...originalLocation, reload },
    });

    const user = userEvent.setup();
    render(
      <ErrorBoundary>
        <Boom />
      </ErrorBoundary>
    );
    await user.click(screen.getByRole("button", { name: "Reload page" }));

    expect(reload).toHaveBeenCalledTimes(1);

    Object.defineProperty(window, "location", { configurable: true, value: originalLocation });
    consoleError.mockRestore();
  });
});
