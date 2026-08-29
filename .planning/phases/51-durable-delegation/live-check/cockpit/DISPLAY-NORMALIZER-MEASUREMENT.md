# Background swarm display measurement

Date: 2026-08-30
Phase: 51-12b live cockpit drive
Privacy: conversation, identity, job, child, and channel identifiers are intentionally omitted.

## Measured

- Two fresh two-worker fan-outs were dispatched from the running cockpit through the local model.
- PostgreSQL stored each `swarm_spawn` tool result as an object with keys
  `note, queued, workers`; `workers` contained two rows with the expected goals and statuses.
- The production `queued` value was the JSON number `2`, matching
  `delegationQueuedResult.Queued int` in `internal/swarm/delegation_enqueue.go`.
- The shared display decoder declared an unused `Queued bool` field. Decoding therefore failed on
  the production payload before it could read `workers`.
- `GET /threads/{id}/messages` returned the matching tool call without a display payload, and a
  Playwright DOM probe found the escaped raw-result fallback (`tool-copy` and
  `tool-result-highlighted`) with no table.

## Decision boundary

The display decoder needs only the `workers` member. It must ignore wrapper metadata instead of
redeclaring the producer's count field with a conflicting type. The regression fixture must use
the production numeric shape and exercise `NormalizeToolPreview`, not only the inner decoder.

This measurement does not establish the worker pane's live behavior; that remains the next fresh
browser drive after the normalizer fix is deployed.
