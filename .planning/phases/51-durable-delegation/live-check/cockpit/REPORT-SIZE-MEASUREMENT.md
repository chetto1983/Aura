# Worker report steer-size measurement

Date: 2026-08-30
Phase: 51-12b live cockpit drive
Privacy: conversation, identity, job, child, and channel identifiers are intentionally omitted.

## Measured

- The running stack used the local `gemma-4-12b` model through the `llama.cpp` sidecar.
- One two-worker fan-out reached `swarm.child.completed status=ok` for both workers.
- The short worker report was delivered normally.
- The long worker completed successfully and its full Markdown report was archived as a
  thread-scoped agent asset (`size_bytes=16808`, `status=accepted`).
- Delivery then failed before the job's terminal transition with
  `delegation report push: steer: message exceeds max size`.
- The active steer implementation applies its configured/default per-message byte cap before
  inserting a `delegation_result`; the default is 32768 bytes.

This proves that archiving the full report and pushing the full JSON report are separate outcomes:
the durable artifact can succeed while the courtesy steer copy rejects the same report's JSON
encoding and leaves a completed worker eligible for retry.

## Decision boundary

The full uncapped report remains authoritative in the archived Markdown asset. The
`delegation_result` steer row carries a bounded `ChildReport` projection for live parent context
and fan-out notification bookkeeping. Identity, goal index, child id, status, attempts, and the
fan-out key remain intact; model-authored text fields are rune-capped before JSON encoding.

This measurement does not establish a new queue-size default, a maximum artifact size, or
multi-slot local-LLM throughput. Those remain separate configuration and performance concerns.
