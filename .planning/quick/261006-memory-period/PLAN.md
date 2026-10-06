# Complete period recall for the agent

Source: operator's `cosa-abbiamo-parlato-ieri.md`, exported 2026-10-06.
Work directly on master. Preserve the unrelated existing dirty files.

Measured before implementation: VM 192.168.101.158 runs d2f078572; ArcadeDB
26.10.1 returns 48 projected turns in 9 conversations for the 5 October Berlin
day. The exported recent read returned only six conversation windows.

Risk: bounded identity-scoped read. No migration, environment variable, new tool,
dependency adapter, persisted episode, or lossy semantic deduplication.

1. Extend existing memory_recall with mode period and explicit from/to; continue
   through scroll using the same validated cursor transport. Reject contradictory
   or irrelevant selectors. Keep every source turn and deterministic chronological
   boundaries, including equal timestamps and conversations spanning midnight.
2. Teach the embedded memory skill to use period for dated questions, exhaust the
   cursor and synthesize related conversations without collapsing separate tests.
3. Verify validation and cursor tenancy before SQL; live disposable-database tests
   exercise date offsets, boundaries, ties, deletion, exclusions and pagination.
   Run package race tests, vet/build, lint, and the VM's real agent on the original
   question. Deploy through the existing image workflow; verify both revisions.

Acceptance: all 48 original projected turns are recoverable exactly once; the
agent sees morning and afternoon conversations and groups the repeated reminder
tests with source evidence. The response does not equate a stored assistant reply
with proof of external delivery. Every touched source file remains <=600 lines.
