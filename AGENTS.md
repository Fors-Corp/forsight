# Forsight — rules for every coding agent

`CLAUDE.md` carries the repo's standing rules and its automation mandate;
this file holds rules that apply to any agent (Claude, Copilot, Codex)
editing the code. Each rule names the incident that made it one.

## Outbound HTTP

**Every outbound HTTP client is built once and shared, never per request.
Every response body is drained and closed on every branch. Every client has
a timeout and a bounded connection pool** (`MaxConnsPerHost`, and either
`DisableKeepAlives` or a finite `IdleConnTimeout`).

_Incident, 2026-10-10:_ the probe collector built a new `http.Transport`
for every probe. Each drained connection went back into that transport's
idle pool, which with `IdleConnTimeout: 0` never expires, and the transport
was dropped with the socket still open. After 3.5 days the agent held
16,314 sockets to one `--probe` target: every ephemeral port on the macOS
host, so every other program on the machine failed to connect. Bodies
_were_ closed; a rule about bodies alone would not have caught it. Fixed in
forsight 1.2.1 (#240), with `TestCollect_RepeatedProbesLeaveNoConnectionsOpen`
as the regression test.

A test for a new client counts its connections: an `httptest.Server`
`ConnState` hook, plus a counting `DialContext` for the client's own end.
