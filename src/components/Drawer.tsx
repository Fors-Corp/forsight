import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { cn } from "../lib/cn";

export const Drawer = DialogPrimitive.Root;
export const DrawerTrigger = DialogPrimitive.Trigger;
export const DrawerClose = DialogPrimitive.Close;

/**
 * Edge-anchored panel for row detail, filters, or focused editing next to
 * the content it relates to — built on the same `@radix-ui/react-dialog`
 * primitive as `Dialog` (real focus-trap/dismiss/Escape behavior), just
 * restyled to slide in from an edge instead of appearing centered. Compose
 * as `<Drawer><DrawerTrigger asChild><Button>Open</Button></DrawerTrigger>
 * <DrawerContent><DrawerHeader><DrawerTitle>...</DrawerTitle>
 * </DrawerHeader>...<DrawerFooter>...</DrawerFooter></DrawerContent>
 * </Drawer>`. `side` is logical (`"start"` | `"end"`, default `"end"`) and
 * flips the physical edge automatically under `dir="rtl"` — see
 * `Sidebar.tsx`'s mobile drawer for the same technique. Use `Dialog` instead
 * for a centered, viewport-anchored modal.
 */
export interface DrawerContentProps extends React.ComponentPropsWithoutRef<
  typeof DialogPrimitive.Content
> {
  /** Logical edge to anchor to and slide from. Defaults to `"end"` (right in LTR, left in RTL). */
  side?: "start" | "end";
  hideClose?: boolean;
}

export const DrawerContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  DrawerContentProps
>(({ className, children, side = "end", hideClose, ...props }, ref) => (
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay
      className={cn(
        "fixed inset-0 z-50 bg-black/60",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-base"
      )}
    />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed inset-y-0 z-50 flex w-[calc(100vw-2rem)] max-w-md flex-col border-ink-border bg-ink-surface p-6 shadow-lg",
        "overflow-y-auto",
        "focus-visible:outline-none",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-base",
        side === "start"
          ? [
              "start-0 border-e",
              "ltr:data-[state=closed]:slide-out-to-left-full ltr:data-[state=open]:slide-in-from-left-full",
              "rtl:data-[state=closed]:slide-out-to-right-full rtl:data-[state=open]:slide-in-from-right-full",
            ]
          : [
              "end-0 border-s",
              "ltr:data-[state=closed]:slide-out-to-right-full ltr:data-[state=open]:slide-in-from-right-full",
              "rtl:data-[state=closed]:slide-out-to-left-full rtl:data-[state=open]:slide-in-from-left-full",
            ],
        className
      )}
      {...props}
    >
      {children}
      {!hideClose && (
        <DialogPrimitive.Close
          aria-label="Close"
          className="absolute end-4 top-4 flex min-h-10 min-w-10 items-center justify-center rounded-sm text-fg-muted transition-colors duration-base hover:text-fg focus-visible:outline-none focus-visible:shadow-focus-ring"
        >
          <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
            <path
              d="M1 1L13 13M13 1L1 13"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
            />
          </svg>
        </DialogPrimitive.Close>
      )}
    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
));
DrawerContent.displayName = "DrawerContent";

export function DrawerHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("mb-4 flex flex-col gap-1.5 pe-6", className)} {...props} />;
}

export const DrawerTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title
    ref={ref}
    className={cn("font-heading text-lg font-semibold text-fg", className)}
    {...props}
  />
));
DrawerTitle.displayName = "DrawerTitle";

export const DrawerDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn("font-sans text-sm text-fg-secondary", className)}
    {...props}
  />
));
DrawerDescription.displayName = "DrawerDescription";

export function DrawerFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("mt-6 flex flex-wrap items-center justify-end gap-2", className)}
      {...props}
    />
  );
}
