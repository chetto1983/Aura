---
id: approval-resume-defects
created: 2026-08-24
source: PRD amendment #133
severity: security
resolves_phase:
---

# Three measured defects in the approval resume path

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

## 2. An empty answer resumes silently

`payloadString(nil)` returns `""`, and `answerTurn` injects the supplied content as the `RoleTool`
answer. An accept carrying no payload resumes the model with an empty answer instead of being
refused. LibreChat 400s on exactly this: *"defaults ({} / '') would resume with an empty
input/result the user didn't approve."*

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

None of these was demonstrated exploitable. Reaching the resume route already requires an
authenticated, owner-scoped session and a valid pause token, so #1 is an authorization-granularity
gap rather than an open door. Nothing here was observed live — all three were traced through the
code.
