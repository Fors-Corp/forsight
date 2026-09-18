import * as React from "react";
import * as AlertDialogPrimitive from "@radix-ui/react-alert-dialog";
import { cn } from "../lib/cn";
import { buttonVariants, type ButtonProps } from "./Button";

export const AlertDialog = AlertDialogPrimitive.Root;
export const AlertDialogTrigger = AlertDialogPrimitive.Trigger;

/**
 * Confirmation dialog for actions that can't be undone (delete, revoke,
 * discard). Unlike `Dialog`, `AlertDialogContent` does not close on an
 * outside click by default (Radix's alert-dialog primitive) and has no `X`
 * affordance — closing focus also always returns to `AlertDialogCancel`,
 * never the trigger, so a keyboard/AT user always lands on the safe choice
 * first. Escape still closes it (same as `Cancel`), matching the platform
 * convention that Escape always means "back out." Compose as
 * `<AlertDialog><AlertDialogTrigger asChild>
 * <Button variant="danger">Delete</Button></AlertDialogTrigger>
 * <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>...
 * </AlertDialogTitle><AlertDialogDescription>...</AlertDialogDescription>
 * </AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>Cancel
 * </AlertDialogCancel><AlertDialogAction>Delete</AlertDialogAction>
 * </AlertDialogFooter></AlertDialogContent></AlertDialog>`.
 * `AlertDialogAction` defaults to the `danger` `Button` variant (pass
 * `variant="primary"` for a consequential-but-not-destructive confirmation).
 * For non-destructive modal content (forms, focused tasks) use `Dialog`
 * instead — it supports the usual dismiss affordances.
 */
export const AlertDialogContent = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Content>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Portal>
    <AlertDialogPrimitive.Overlay
      className={cn(
        "fixed inset-0 z-50 bg-black/60",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 duration-base"
      )}
    />
    <AlertDialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed left-1/2 top-1/2 z-50 w-[calc(100vw-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-ink-border bg-ink-surface p-6 shadow-lg",
        "max-h-[calc(100vh-2rem)] overflow-y-auto",
        "focus-visible:outline-none",
        "data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 duration-base",
        className
      )}
      {...props}
    />
  </AlertDialogPrimitive.Portal>
));
AlertDialogContent.displayName = "AlertDialogContent";

export function AlertDialogHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("mb-4 flex flex-col gap-1.5", className)} {...props} />;
}

export const AlertDialogTitle = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Title
    ref={ref}
    className={cn("font-heading text-lg font-semibold text-fg", className)}
    {...props}
  />
));
AlertDialogTitle.displayName = "AlertDialogTitle";

export const AlertDialogDescription = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <AlertDialogPrimitive.Description
    ref={ref}
    className={cn("font-sans text-sm text-fg-secondary", className)}
    {...props}
  />
));
AlertDialogDescription.displayName = "AlertDialogDescription";

export function AlertDialogFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("mt-6 flex flex-wrap items-center justify-end gap-2", className)}
      {...props}
    />
  );
}

export interface AlertDialogActionProps
  extends
    React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Action>,
    Pick<ButtonProps, "variant" | "size"> {}

/** Primary action — the destructive (or otherwise consequential) choice. Defaults to the `danger` variant; pass `variant`/`size` to override. */
export const AlertDialogAction = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Action>,
  AlertDialogActionProps
>(({ className, variant = "danger", size, ...props }, ref) => (
  <AlertDialogPrimitive.Action
    ref={ref}
    className={cn(buttonVariants({ variant, size }), className)}
    {...props}
  />
));
AlertDialogAction.displayName = "AlertDialogAction";

export interface AlertDialogCancelProps
  extends
    React.ComponentPropsWithoutRef<typeof AlertDialogPrimitive.Cancel>,
    Pick<ButtonProps, "variant" | "size"> {}

/** Secondary action that backs out without making the change. */
export const AlertDialogCancel = React.forwardRef<
  React.ElementRef<typeof AlertDialogPrimitive.Cancel>,
  AlertDialogCancelProps
>(({ className, variant = "secondary", size, ...props }, ref) => (
  <AlertDialogPrimitive.Cancel
    ref={ref}
    className={cn(buttonVariants({ variant, size }), className)}
    {...props}
  />
));
AlertDialogCancel.displayName = "AlertDialogCancel";
