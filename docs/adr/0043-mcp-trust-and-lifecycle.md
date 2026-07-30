# ADR 0043 — MCP trust and lifecycle

- **Status:** Accepted
- **Date:** 2026-07-31
- **Requirement:** OPS-06 / F-025
- **Relates to:** `prd.md` Amendments #103 and #106

## Context

MCP combines local child processes and remote HTTP authorities. Transport ambiguity, trust defaults,
protocol drift, unbounded frames, or reconnect schema drift can turn a configured integration into
an undeclared capability.

## Decision

Managed configuration is authoritative. A server has exactly one transport, explicit enable state,
trust class, and bounded mount/call/close budgets. Remote HTTP defaults blocked and must match the
configured authority and egress policy; local commands are fixed argv with filtered environment.
Legacy JSON cannot override managed governance in strict profiles.

Initialization validates a supported protocol version, non-empty server identity, and tools
capability before mount. Reconnect may refresh schemas only when the accepted raw tool-name set is
identical; add/remove/rename/collision drift requires process restart. Frames, bodies, schemas,
results, and concurrent shutdown are bounded. Domain failures remain failures through gateway and
idempotency layers.

## Consequences

- Health is cheap process reachability; readiness may exercise required semantic capabilities.
- A probe must dial and enumerate, never infer health from configuration.
- Trust writes are explicit, audited, identity-scoped, and transport-compatible.
- Protocol, timeout, trust, domain-error, reconnect-drift, and shutdown tests are release evidence.
