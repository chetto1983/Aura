---
id: approval-resume-defects
created: 2026-08-24
source: PRD amendment #133
severity: security
resolves_phase:
---

# Three measured defects in the approval resume path — one closed

Phases 47 and 48 were cancelled on 2026-08-24 (the tool surface works — amendment #139).
These three findings came out of reading LibreChat against our own resume path and are **not**
tool-surface ceremony: they are defects in `internal/agui/server_project.go` and
`internal/runner/runner_resume.go`. Recorded here so cancelling the phases does not lose them.

Full detail and evidence: **PRD amendment #133** (commit `3a53bea7e`).

## 1. No per-tool decision policy

`resumeAnswers` (`internal/agui/server_project.go:24`) maps `resolved→ActionAccept` /
`cancelled→ActionCancel`, and the Runner acts on `resp.Action` directly. Nothing anywhere
expresses "this pause may only be declined". Any pending pause can be accepted by anyone who
reaches the route with its token.

LibreChat returns **403** here: *"a crafted POST must not approve a tool the policy restricted to
(e.g.) reject/respond"*. Its threat model is that the **human's POST is untrusted** — a different
threat from the one the roadmap covered (that the *model* never sees a resume payload).

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

## 3. Pending approvals never expire

No TTL anywhere in `internal/gateway` expires a pending approval — only replay retention and an
orphan-scan TTL exist. LibreChat has `APPROVAL_EXPIRED_ERROR`, `expireApproval()`, a handler that
prunes the paused run's durable checkpoint, and explicit handling for a decision arriving after the
TTL lapsed.

## Constraint on any fix

Our idempotency story is stronger than LibreChat's and must survive: `MarkResumed`'s
`RowsAffected==0` gate returns `ErrPauseNotFound` for an unknown OR already-resumed token, the
`WHERE resumed_at IS NULL` conditional update IS the idempotency key (D-06), and
`CommitResumeBatch` claims every pause under sorted-token deadlock-free ordering in ONE cross-store
transaction. **New validation goes inside that transaction's front door, never as a second path
around it.**

## What is NOT claimed

No production exploit was demonstrated. Reaching the resume route already requires an
authenticated, owner-scoped session and a valid pause token, so #1 is an authorization-granularity
gap rather than an open door. The closing measurement for #2 proves fail-closed wire, Runner,
Store, and real-PostgreSQL transaction behavior; it does not prove #1's per-pause decision policy
or #3's expiry semantics, which remain open.
