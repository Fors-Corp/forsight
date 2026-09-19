import * as React from "react";
import { cn } from "../lib/cn";

type Orientation = "horizontal" | "vertical";

interface TabsContextValue {
  value: string;
  setValue: (value: string) => void;
  baseId: string;
  orientation: Orientation;
}
const TabsContext = React.createContext<TabsContextValue | null>(null);

function useTabsContext() {
  const ctx = React.useContext(TabsContext);
  if (!ctx) throw new Error("Tabs.* components must be rendered inside <Tabs.Root>");
  return ctx;
}

const triggerId = (baseId: string, value: string) => `${baseId}-tab-${value}`;
const panelId = (baseId: string, value: string) => `${baseId}-panel-${value}`;

export interface TabsRootProps extends React.HTMLAttributes<HTMLDivElement> {
  /** Value of the initially active tab (uncontrolled). */
  defaultValue: string;
  /** Active tab value (controlled). */
  value?: string;
  onValueChange?: (value: string) => void;
  /** Arrow-key axis and visual flow. Defaults to `"horizontal"`. */
  orientation?: Orientation;
}

const Root = React.forwardRef<HTMLDivElement, TabsRootProps>(
  (
    { className, defaultValue, value, onValueChange, orientation = "horizontal", ...props },
    ref
  ) => {
    const [internal, setInternal] = React.useState(defaultValue);
    const baseId = React.useId();
    const active = value ?? internal;
    const setValue = React.useCallback(
      (v: string) => {
        setInternal(v);
        onValueChange?.(v);
      },
      [onValueChange]
    );
    return (
      <TabsContext.Provider value={{ value: active, setValue, baseId, orientation }}>
        <div
          ref={ref}
          className={cn(
            "flex gap-3",
            orientation === "vertical" ? "flex-row" : "flex-col",
            className
          )}
          {...props}
        />
      </TabsContext.Provider>
    );
  }
);
Root.displayName = "TabsRoot";

const List = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, onKeyDown, ...props }, ref) => {
    const { orientation } = useTabsContext();

    const handleKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
      onKeyDown?.(event);
      if (event.defaultPrevented) return;

      const next = orientation === "vertical" ? "ArrowDown" : "ArrowRight";
      const prev = orientation === "vertical" ? "ArrowUp" : "ArrowLeft";
      if (!["Home", "End", next, prev].includes(event.key)) return;

      const list = event.currentTarget;
      const tabs = Array.from(
        list.querySelectorAll<HTMLButtonElement>('[role="tab"]:not([disabled])')
      );
      if (tabs.length === 0) return;
      const currentIndex = tabs.indexOf(document.activeElement as HTMLButtonElement);

      let nextIndex: number;
      if (event.key === "Home") nextIndex = 0;
      else if (event.key === "End") nextIndex = tabs.length - 1;
      else if (event.key === next) nextIndex = (currentIndex + 1) % tabs.length;
      else nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;

      event.preventDefault();
      tabs[nextIndex]?.focus();
      tabs[nextIndex]?.click();
    };

    return (
      <div
        ref={ref}
        role="tablist"
        aria-orientation={orientation}
        onKeyDown={handleKeyDown}
        className={cn(
          "flex w-fit max-w-full gap-1 rounded-md border border-ink-border bg-ink-surface p-1",
          orientation === "vertical" ? "flex-col" : "items-center overflow-x-auto",
          className
        )}
        {...props}
      />
    );
  }
);
List.displayName = "TabsList";

export interface TabsTriggerProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  value: string;
}

const Trigger = React.forwardRef<HTMLButtonElement, TabsTriggerProps>(
  ({ className, value, ...props }, ref) => {
    const { value: active, setValue, baseId } = useTabsContext();
    const selected = active === value;
    return (
      <button
        ref={ref}
        type="button"
        role="tab"
        id={triggerId(baseId, value)}
        aria-selected={selected}
        aria-controls={panelId(baseId, value)}
        tabIndex={selected ? 0 : -1}
        onClick={() => setValue(value)}
        className={cn(
          "shrink-0 rounded-sm px-3 py-1.5 font-sans text-sm font-medium transition-colors duration-base focus-visible:outline-none focus-visible:shadow-focus-ring disabled:pointer-events-none disabled:opacity-50",
          selected ? "bg-accent text-accent-fg" : "text-fg-secondary hover:text-fg",
          className
        )}
        {...props}
      />
    );
  }
);
Trigger.displayName = "TabsTrigger";

export interface TabsPanelProps extends React.HTMLAttributes<HTMLDivElement> {
  value: string;
}

const Panel = React.forwardRef<HTMLDivElement, TabsPanelProps>(
  ({ className, value, ...props }, ref) => {
    const { value: active, baseId } = useTabsContext();
    if (active !== value) return null;
    return (
      <div
        ref={ref}
        role="tabpanel"
        id={panelId(baseId, value)}
        aria-labelledby={triggerId(baseId, value)}
        tabIndex={0}
        className={cn(
          "font-sans text-sm text-fg focus-visible:outline-none focus-visible:shadow-focus-ring",
          className
        )}
        {...props}
      />
    );
  }
);
Panel.displayName = "TabsPanel";

/**
 * Compound tabbed navigation implementing the WAI-ARIA Tabs pattern:
 * `<Tabs.Root>` holds state, `<Tabs.List>` wraps `<Tabs.Trigger value="...">`
 * buttons, and each `<Tabs.Panel value="...">` renders only when active.
 *
 * Keyboard: Tab moves into the active trigger only (roving tabindex); once
 * the tablist has focus, Arrow keys move between triggers and activate them
 * (automatic activation), Home/End jump to first/last, and disabled triggers
 * are skipped. Each panel is linked to its trigger via `aria-labelledby` and
 * is itself focusable so keyboard users can reach its content. Pass
 * `orientation="vertical"` on `Tabs.Root` to switch the arrow-key axis and
 * lay the list out as a sidebar.
 */
export const Tabs = { Root, List, Trigger, Panel };
