import * as React from "react";
import { cn } from "../lib/cn";

export interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  /**
   * Adds hover styling (border highlight) to suggest the card is interactive.
   * **Note**: This is visual only — the component does not handle clicks or
   * keyboard navigation. Wrap the card in a `<button>`, `<a>`, or add
   * `role`/`tabIndex` to enable real interactivity.
   */
  interactive?: boolean;
}

/** Surface container for grouped content — the base layout primitive. */
export const Card = React.forwardRef<HTMLDivElement, CardProps>(
  ({ className, interactive, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        "rounded-lg border border-ink-border bg-ink-surface p-5 shadow-sm",
        interactive && "transition-colors duration-base hover:border-accent cursor-pointer",
        className
      )}
      {...props}
    />
  )
);
Card.displayName = "Card";

export const CardHeader = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("mb-3 flex flex-col gap-1", className)} {...props} />
  )
);
CardHeader.displayName = "CardHeader";

type CardTitleElement = "h1" | "h2" | "h3" | "h4" | "h5" | "h6";

export interface CardTitleProps extends React.HTMLAttributes<HTMLHeadingElement> {
  /**
   * Semantic heading level. Defaults to `h3`, the level that fits a Card
   * nested a couple of levels below a page's own `h1`/`h2`. Card is used
   * at every depth of a dashboard though, so a fixed `h3` reliably skips a
   * level wherever a Card's title is meant to be the top heading on the
   * page (or one level under it) — pass `as` to pick the level the
   * surrounding outline actually needs, e.g. `as="h1"`.
   */
  as?: CardTitleElement;
}

/**
 * Card section heading. Renders as `<h3>` by default; pass `as` (matching
 * `Heading`'s convention) to fit the level `CardTitle` needs in the
 * document outline where it's used.
 */
export const CardTitle = React.forwardRef<HTMLHeadingElement, CardTitleProps>(
  ({ className, as: Comp = "h3", ...props }, ref) => (
    <Comp
      ref={ref}
      className={cn("font-heading text-lg font-semibold text-fg", className)}
      {...props}
    />
  )
);
CardTitle.displayName = "CardTitle";

export const CardDescription = React.forwardRef<
  HTMLParagraphElement,
  React.HTMLAttributes<HTMLParagraphElement>
>(({ className, ...props }, ref) => (
  <p ref={ref} className={cn("font-sans text-sm text-fg-secondary", className)} {...props} />
));
CardDescription.displayName = "CardDescription";

export const CardContent = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("font-sans text-sm text-fg", className)} {...props} />
  )
);
CardContent.displayName = "CardContent";

export const CardFooter = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("mt-4 flex items-center gap-2", className)} {...props} />
  )
);
CardFooter.displayName = "CardFooter";
