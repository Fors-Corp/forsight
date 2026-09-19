import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button, EmptyState } from "@marcfs31/forsight";

interface ErrorBoundaryProps {
  children: ReactNode;
}

interface ErrorBoundaryState {
  error: Error | null;
}

/**
 * The dashboard had no error boundary anywhere: main.tsx renders
 * `StrictMode > App` with nothing implementing `getDerivedStateFromError`,
 * so an uncaught render error anywhere under a page unmounts the whole
 * React root and leaves a blank tab — no heading, no nav, nothing a user
 * can act on. There is a live way to trigger it today: a model whose
 * `driftThreshold` a separately-versioned mlaas response leaves out (see
 * Models.tsx's drift cell) throws on `.toFixed`, and that field is
 * deliberately typed non-optional — this boundary is the fix, not a
 * defensive null-check at that call site.
 *
 * A boundary can only be a class component — React has no hook equivalent
 * of `getDerivedStateFromError`/`componentDidCatch` as of this file's
 * writing. `App` renders one of these around each page's slot (see
 * App.tsx), keyed on the route, so switching pages after a crash starts
 * from a clean boundary instead of replaying the fallback the previous
 * page left behind.
 */
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // React already logs this to the console on its own via its default
    // uncaught-error handling; this line adds the component stack, which
    // that default log doesn't always carry, for whoever reads the
    // browser console after a report of a blank page.
    console.error("forsight dashboard: a page failed to render", error, info.componentStack);
  }

  render() {
    if (this.state.error) {
      return (
        <EmptyState
          role="alert"
          title="Something went wrong"
          description="This page hit an unexpected error and couldn't continue. Reloading usually fixes it; if it keeps happening, check the browser console for details."
          action={<Button onClick={() => window.location.reload()}>Reload page</Button>}
          className="mx-auto max-w-md"
        />
      );
    }
    return this.props.children;
  }
}
