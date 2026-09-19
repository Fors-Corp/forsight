import type { HTMLAttributes } from "react";

/**
 * A Card section heading rendered as a real `<h2>`.
 *
 * `@marcfs31/forsight`'s `CardTitle` gained an `as` prop to pick its heading
 * level (design-system PR #171, "feat(pkg): let CardTitle choose its heading
 * level…"), which is exactly what every section below needs — each of
 * Overview's and Models' Cards sits as a direct child of the page, right
 * under the page's own `<h1>`, so `CardTitle`'s default `<h3>` skips a level
 * and axe's heading-order rule (rightly) flags every one of them.
 *
 * That fix isn't reachable yet: this dashboard builds against the
 * *published* `@marcfs31/forsight` package (see forsight/web/package.json's
 * caret pin and CLAUDE.md, "two artifacts"), and as of this change the
 * `as`-prop commit sits on `main` behind an unpublished changeset — `npm
 * view @marcfs31/forsight version` still answers with the release before it.
 * Rather than block this dashboard's heading-order and mlaas-status-region
 * fixes on that publish, this component reproduces `CardTitle`'s exact
 * default styling as a plain, always-`<h2>` element.
 *
 * Follow-up: once a release carrying the `as` prop is published and the pin
 * in forsight/web/package.json is bumped, delete this component and replace
 * every `<CardSectionHeading>` below with `<CardTitle as="h2">`.
 */
export function CardSectionHeading({ children, className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h2 className={["font-heading text-lg font-semibold text-fg", className].filter(Boolean).join(" ")} {...props}>
      {children}
    </h2>
  );
}
