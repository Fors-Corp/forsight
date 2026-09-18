---
"@marcfs31/forsight": patch
---

Fix `AlertList` so the first alert after an empty state is announced.

Previously `AlertList` rendered `EmptyState` in place of its `<ul
aria-live="polite">` whenever `items` was empty, so the live region did
not exist yet at 0 items. Going from 0 to 1 alert mounted that `<ul>` for
the first time with its content already on it — a live region announces
mutations to content already inside it, not its own arrival, so most
AT/browser pairs never spoke the alert a responder needed to hear first.

The `<ul>` now stays mounted at every item count, the same way
`LogStream`'s `role="log"` region does, with the empty message rendered
as a child `<li>` (wrapping `EmptyState`, with its own `role="status"`
turned off so it doesn't compete with the list's own live region for the
same announcement).
