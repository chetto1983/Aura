---
phase: 52-mid-turn-steering
plan: 03
status: complete
closed: 2026-08-25
requirements: [RESUME-01]
code_commit: 0d5f48dfd
---

# 52-03 — unanswered approval expiry

The final approval-resume concern is closed. Unanswered persisted approvals now have a bounded,
operator-configurable lifetime and expire through the same atomic claim used by human decisions.

## Shipped

- Added `AURA_ASKUSER_PAUSE_TTL_SEC`, defaulting to 172800 seconds; `<=0` disables expiry.
- Added an owner-scoped due-approval query over the existing `created_at` clock; no migration was
  required.
- Added immediate boot and bounded periodic sweeps with joined shutdown.
- Added internal `expired` semantics, HTTP Gone for late answers, and rejection of public
  `expired` decisions.
- Preserved single-winner behavior by committing the refusal and `RoleTool` turn through the same
  atomic `ResumeCommitter` path.
- Discarded exact gateway and shell pending challenges on expiry without granting them.
- Removed the unreachable `gateway.WithResolvedApproval` production seam and its test-only branch;
  the production cross-turn approval-ledger integration remains covered.

## Closing evidence

| Gate | Result |
|---|---|
| Touched-package WSL vet/build/race | Green |
| Lint, formatting, sqlc, file-size, dead-code | Green |
| Disposable PostgreSQL selection/RLS/atomicity/race | Green; exactly one winner and one `RoleTool` |
| Migration 0102 real up/down/up | Green |
| Ask-user + Runner DB coverage | 86.2%; changed critical functions >=85% |
| Mutation testing | 14/14 killed, 100% |
| Real OpenRouter-agent E2E | 10.0/10; turn 2 -> 3, 3752 prompt tokens |

The live E2E initially exposed CRLF contamination when sourcing the Windows `.env` directly in
WSL; the run used a process-local normalized stream and did not alter the secret file. The fixed
disposable `aura-expiry-pg` container and its test data were removed after verification.
