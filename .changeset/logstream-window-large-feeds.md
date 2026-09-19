---
"@fors-corp/forsight": patch
---

Window `LogStream` past 200 entries so a large feed doesn't mount thousands of DOM rows.

`LogStream` mounted one row per entry unconditionally, which meant a 2000-line
feed (the dashboard's own use case, reconciled every few seconds) mounted
2000 DOM nodes on every render. Past 200 entries, only the rows near the
current scroll position (plus a small overscan buffer) actually mount now,
with a pair of empty spacer rows standing in for the rest so the scrollbar
still represents the true total — a fixed-estimate scroll window, not a full
virtualizer, so no new dependency. At or under 200 entries, rendering is
byte-identical to before.

Accessibility is unchanged: each mounted row carries `aria-setsize`/
`aria-posinset` once windowed (the ARIA-specified technique for a list where
not every item is in the DOM), the region's keyboard reachability and
`aria-live`/`aria-relevant` semantics are unaffected either way, and axe
reports no violations at 2000 entries.
