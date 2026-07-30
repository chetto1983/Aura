# ADR 0040 — Agent loop semantics

- **Status:** Accepted
- **Date:** 2026-07-31
- **Requirement:** OPS-06 / F-025
- **Relates to:** `prd.md` Amendments #96–#106.3

## Context

Aura executes one model turn as a bounded sequence of model calls, tool calls, pauses, retries,
and one terminal answer. Ambiguous ownership of the terminal action, retry state, or pause state
can repeat effects or report success after a failure.

## Decision

The runner owns the turn lifecycle and the durable conversation is authoritative. The model may
request tools, but it cannot commit persistence or bypass the gateway. A terminal response is
exclusive with mutating siblings. Pauses are durably minted and atomically claimed before answers
are appended. Retry keeps one logical operation identity and never replays a committed mutation.
Every loop is bounded by model-call, tool-call, token, wall-clock, and output limits. Exhaustion
produces a typed terminal error; it is never rewritten as success.

Cancellation stops new work. Already admitted scheduler jobs use their registered deadline and are
joined. User-request cancellation remains visible to the active turn and does not detach arbitrary
interactive work.

## Consequences

- Loop helpers may transform sampling/reasoning controls, never protected messages or tool state.
- Recovery reconstructs from durable turns, pauses, operation reservations, and sidecars.
- Tests must cover terminal exclusivity, retry exhaustion, pause claim-before-append, cancellation,
  and exact final-request budgeting.
- A future autonomous-loop mode requires a new ADR if it changes these ownership rules.
