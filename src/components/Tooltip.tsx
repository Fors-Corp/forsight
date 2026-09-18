import * as React from "react";
import * as TooltipPrimitive from "@radix-ui/react-tooltip";
import { cn } from "../lib/cn";
import { POPPER_ANIMATION_CLASSES } from "../lib/animation";

export const TooltipProvider = TooltipPrimitive.Provider;
export const Tooltip = TooltipPrimitive.Root;
export const TooltipTrigger = TooltipPrimitive.Trigger;

/**
 * Short hint shown on hover/focus of a `TooltipTrigger`. Wrap your app once
 * in a single `<TooltipProvider>` (root layout) — every `<Tooltip>` reads
 * shared hover-timing config from it. Touch gets no hover, and a tap never
 * opens it (Radix ignores touch pointer events by design) — an icon-only
 * trigger whose only explanation lives in `TooltipContent` is not
 * accessible on touch. Give the trigger a visible label, or reach for
 * `Popover` when the content matters and needs to be tap-reachable.
 */
export const TooltipContent = React.forwardRef<
  React.ElementRef<typeof TooltipPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TooltipPrimitive.Content>
>(({ className, sideOffset = 6, ...props }, ref) => (
  <TooltipPrimitive.Portal>
    <TooltipPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      className={cn(
        "z-50 rounded-sm border border-ink-border bg-ink-surface-2 px-2.5 py-1.5 text-xs font-sans text-fg shadow-md",
        POPPER_ANIMATION_CLASSES,
        className
      )}
      {...props}
    />
  </TooltipPrimitive.Portal>
));
TooltipContent.displayName = "TooltipContent";
