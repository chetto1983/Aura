---
id: approval-resume-defects
created: 2026-08-24
source: PRD amendment #133
severity: security
resolves_phase:
---

# Three measured defects in the approval resume path — two closed

Phases 47 and 48 were cancelled on 2026-08-24 (the tool surface works — amendment #139).
These three findings came out of reading LibreChat against our own resume path and are **not**
tool-surface ceremony: they are defects in `internal/agui/server_project.go` and
`internal/runner/runner_resume.go`. Recorded here so cancelling the phases does not lose them.

Full detail and evidence: **PRD amendment #133** (commit `3a53bea7e`).

## 1. Closed 2026-08-25 — per-pause decision policy enforced

Every production pause writer now persists an explicit, server-authored `allowed_decisions`
policy in `resume_context`. Runner parses that persisted policy fail-closed and validates each
single or batch answer before any claim, cancellation, hook, or answer-turn side effect. A missing,
null, malformed, or unknown policy is rejected rather than widened by a compatibility fallback.
Both AG-UI resume paths and the Approval Center map a forbidden decision to HTTP 403.

Migration 0102 backfills every existing pause with the exact historical decision set and normalizes
non-object contexts. Its real disposable-PostgreSQL integration test drives 0101 → 0102 → 0101 →
0102, proving backfill, object-field preservation, down removal, and repeatable re-application.

Closing evidence:

- focused Runner and AG-UI regressions prove forbidden single/batch decisions fail before side
  effects and allowed decisions retain the existing atomic/idempotent path;
- the critical policy parser/validator killed **20/20 mutants = 100%** in WSL;
- `internal/runner` aggregate coverage measured **85.3%**;
- the real migration round trip passed against a fresh disposable database;
- a real OpenRouter-agent E2E minted a decline-only pause, proved a crafted accept left the same
  token pending, then allowed decline and re-drove the production agent from 2 to 3 persisted turns
  with **3750 prompt tokens**;
- targeted WSL race tests passed for `internal/runner`, `internal/agui`, and `internal/askuser`;
  targeted production and test dead-code scans returned zero findings after the eight reported
  unreachable elements were removed.

## 2. Closed 2026-08-25 — empty accepted answers are rejected

The defect was reproduced red at the wire and Store boundaries, then closed with one shared
invariant. `askuser.ValidateResumeAnswer` rejects empty or whitespace-only accepted content;
`encodeAnswer` invokes it at the persistence boundary; Runner validates the effective answer used
for both the persisted claim and injected `RoleTool` turn; and both synchronous and detached AG-UI
resume paths return HTTP 400 before `SubmitAnswers`. Decline/cancel still permits empty caller
content. Scheduled approvals are preserved because Runner validates their server-authored outcome,
not the caller's empty placeholder.

Closing evidence:

- `TestEncodeAnswer_RejectsEmptyAcceptedContent` and the mutation-oriented Store cases;
- `TestSubmitAnswerRejectsEmptyAcceptBeforeCommit` plus the batch variant;
- `TestServer_ResumeRejectsEmptyAcceptedContent` for missing, empty, and whitespace payloads;
- `TestMarkResumedBatch_EmptyAcceptedContentRollsBack`, proving no earlier claim survives a later
  invalid answer in the same transaction;
- `go-mutesting --match ValidateResumeAnswer internal/askuser/store.go`: **4/5 = 80% killed**;
  the one reported survivor was byte-identical to the original file;
- `scripts/coverage_docker.sh` green against disposable PostgreSQL: **26827/31139 = 86.2%** owned
  coverage across the full `db_integration` matrix.

## 3. Closed 2026-08-25 — pending approvals never expired

Unanswered `kind=approval` pauses now expire after `AURA_ASKUSER_PAUSE_TTL_SEC` (48 hours by
default, `<=0` explicitly disables expiry). The daemon runs an immediate boot sweep and a bounded
periodic sweep for each active owner. Expiry uses the existing atomic resume claim to persist an
internal `expired` refusal and its matching `RoleTool` answer in one transaction; the public API
rejects `expired`, late decisions return Gone, and gateway/shell pending challenges are discarded
without granting approval.

Closing evidence:

- targeted WSL unit/race, vet, build, lint, file-size, sqlc, and dead-code gates are green;
- disposable PostgreSQL proves due-row selection, kind/resolution filtering, owner RLS, atomic
  visibility, and an expiry-versus-human race with exactly one winner and one `RoleTool` turn;
- the real migration-0102 up/down/up round-trip remains green; expiry required no schema migration;
- the touched ask-user/Runner database matrix measured 86.2% aggregate coverage, with every changed
  critical expiry function at or above 85%;
- `ExpirePendingApprovals` mutation testing killed 14/14 mutants;
- a fresh real OpenRouter agent advanced from two to three turns, consumed 3752 prompt tokens, and
  truthfully reported that the approval expired and nothing executed: **10.0/10**.

## Constraint on any fix

Our idempotency story is stronger than LibreChat's and must survive: `MarkResumed`'s
`RowsAffected==0` gate returns `ErrPauseNotFound` for an unknown OR already-resumed token, the
`WHERE resumed_at IS NULL` conditional update IS the idempotency key (D-06), and
`CommitResumeBatch` claims every pause under sorted-token deadlock-free ordering in ONE cross-store
transaction. **New validation goes inside that transaction's front door, never as a second path
around it.**

## What is NOT claimed

No production exploit was demonstrated. Reaching the resume route already requires an
authenticated, owner-scoped session and a valid pause token, so #1 was an
authorization-granularity gap rather than an open door. All three defects in this record are now
closed through fail-closed wire behavior, real-PostgreSQL transaction/race evidence, and fresh
real-agent E2E runs. The 48-hour TTL remains an explicit product policy rather than an empirically
optimized duration, and this closure does not claim a new repository-wide coverage run.
