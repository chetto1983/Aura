---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 09
subsystem: runner
tags: [conversation-delete, lifecycle, session-keying, identity-isolation, background-jobs, eviction, musr-05, d-23]

# Dependency graph
requires:
  - phase: 36-03
    provides: "background jobs owner-bound (ownerID,sessionID) + crypto IDs + TTL reaper (BackgroundShells, shell_bg_owner.go)"
  - phase: 36-04
    provides: "db.WithIdentityTx RLS carrier + owner-scoped *ForIdentity (Get/Delete) with D-06 404/403 in conversations + agui"
provides:
  - "DeleteConversationLifecycle(ctx, identityID, convID): the single owner-scoped conversation-delete entry point — cancel active work → expire pauses → evict session tools → terminate bg jobs → THEN delete persistence (MUSR-05 / D-22)"
  - "Composite (identity, session) sessionKey (D-23): the per-thread run lock + a NEW live-turn cancel registry are both keyed by it, never by the session id alone"
  - "tools.SessionJobTerminator + BackgroundShells.TerminateSession: the (owner, session)-scoped kill of a conversation's live background shells"
  - "All three delete surfaces (AG-UI DELETE, Telegram /clear, aura chat delete) route through the ONE lifecycle — no raw store delete remains on any surface"
affects: [36-11, 36-12]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Composite (identity, session) map keying for shared in-memory runner state, with a `local` fallback for the no-principal path (D-23/D-25)"
    - "Owner gate FIRST in a destructive lifecycle: GetForIdentity short-circuits a foreign/absent id before any teardown, so a foreign delete can never grief another identity's live session"
    - "Registry-ranged session-lifecycle interfaces (SessionEvictor for finished state, SessionJobTerminator for live jobs) the runner reaches the process-scoped tool state through"

key-files:
  created:
    - internal/runner/runner_session.go
    - internal/runner/runner_delete.go
    - internal/runner/conversation_delete_lifecycle_test.go
  modified:
    - internal/runner/runner.go
    - internal/runner/interfaces.go
    - internal/agent/tools/evict.go
    - internal/agent/tools/shell_bg_owner.go
    - internal/agui/server.go
    - internal/agui/conversations_api.go
    - internal/agui/conversations_branch_api.go
    - cmd/aura/serve_channels.go
    - cmd/aura/chat.go

key-decisions:
  - "Owner gate runs BEFORE teardown (Rule 2 security): AutoResolveForConversation + evictSessionToolState are NOT identity-scoped, so without a prior GetForIdentity a foreign delete could expire another identity's pauses + evict its tool state even though the final delete 403s. The gate resolves that — a foreign/absent id short-circuits with (0, nil)"
  - "Tool session state (todo/shell-cwd/shell-approvals) STAYS keyed by convID (== WithToolCallContext sessionID == Event.ThreadID): re-keying it to a composite string would break SSE ThreadID routing + the sidecar layout + the gateway ledger. The (identity, session) composite is enforced on the runner's OWN maps (thread lock + live-turn cancel registry) and by the owner-gate — the conversation id is itself a globally-unique, owner-immutable UUID (two identities never share one)"
  - "background-job termination needed a NEW (owner, session)-scoped kill (BackgroundShells.TerminateSession + SessionJobTerminator); plan-03 shipped only per-shell-id owner-gated kill, which the runner can't drive without the shell ids (Rule 2)"
  - "Telegram /clear wired via a runner-backed clearBackend adapter at the composition root (serve_channels.go), not in bot_dispatch_turn.go — the /clear deletion seam is the clearBackend, wired in serve; the behavior (invoke DeleteConversationLifecycle) matches the plan key_link exactly"

patterns-established:
  - "A single ordered delete lifecycle owning cancel→expire→evict→jobs→delete, invoked identically from every surface, with the owner gate as the fail-closed prefix"

requirements-completed: [MUSR-05]

# Metrics
duration: ~55 min
completed: 2026-07-06
---

# Phase 36 Plan 09: Conversation-Delete Lifecycle + (identity, session) Session Keying Summary

**One runner lifecycle now owns conversation deletion across AG-UI, Telegram `/clear`, and the CLI — cancel active work → expire pending pauses → evict session tools → terminate background jobs → THEN owner-scoped delete — with the runner's per-conversation in-memory state (thread lock + a new live-turn cancel registry) keyed by the composite `(identity, session)` so concurrent multi-user can never collide (MUSR-05 / D-22 / D-23).**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-06
- **Tasks:** 3
- **Files created/modified:** 19 (3 created, 16 modified)

## Accomplishments

- **Task 1 — composite (identity, session) keying (D-23):** `runner_session.go` introduces the comparable `sessionKey{identity, session}` + `sessionIdentity(ctx)` (empty principal → seeded `local`, D-25) + `newSessionKey`/`keyForIdentity`. The per-thread run lock (`lockForThread`/`TryLockThread`) is now keyed by it — `TryLockThread` gained a `ctx` so the AG-UI pre-lock keys by the principal. A NEW live-turn cancel registry (`sessions sync.Map` + `trackSession`/`cancelSession`) records each in-flight turn's ctx-cancel under the composite key; `turnLocked` registers it (ctx is owner-scoped there) and deregisters on turn end. The lock + cancel methods live in `runner_session.go` (moving them OUT of `runner.go`, which stays ≤600).
- **Task 2 — the single delete lifecycle + all three surfaces wired:** `runner_delete.go` `DeleteConversationLifecycle(ctx, identityID, convID)` runs the **owner gate first** (`GetForIdentity` → a foreign/absent id short-circuits with `(0, nil)`, mapped to 403/404), then in order (1) `cancelSession` (abort the owner's in-flight turn), (2) `AutoResolveForConversation` (expire pauses, best-effort), (3) `evictSessionToolState` (SessionEvictor + gateway ledger), (4) `terminateSessionJobs` (the new owner-scoped bg kill), (5) `DeleteForIdentity` (owner-scoped persistence delete + sidecar purge). The AG-UI DELETE route, a runner-backed Telegram `/clear` adapter (`serve_channels.go`), and `aura chat delete` all call this ONE method — no raw store delete remains on any surface.
- **Task 3 — the lifecycle test:** `conversation_delete_lifecycle_test.go` asserts the exact `cancel → expire → evict → terminate → delete` order from all three surface shapes (via a shared step log + recording conv/pause stores + a recording SessionEvictor/SessionJobTerminator tool), the eviction-before-delete guarantee, the foreign-delete owner gate (0 rows, NO teardown, owner's turn NOT cancelled), and the D-23 composite keying (two identities on the same session id keyed apart, plus a concurrent same-session-id-different-identity race proof).

## Task Commits

1. **Tasks 1 + 2: lifecycle + composite session keying (implementation)** — `4d934b3c` (feat)
2. **Task 3: lifecycle + composite session-key isolation test** — `37341895` (test)

**Plan metadata:** (this commit) `docs(36-09)`

> Tasks 1 and 2 landed in ONE commit: the delete lifecycle (Task 2) cannot compile without the (identity, session) session registry + the `ConversationStore` owner-scoped methods (Task 1's foundation), and the interface changes co-locate in the same files (`internal/agui/server.go` carries both the `TryLockThread` ctx change and the `DeleteConversationLifecycle` addition), so an artificial split would leave a non-building intermediate. Each of the two commits reflects a fully building, green working tree.

## Files Created/Modified
- `internal/runner/runner_session.go` (created) — `sessionKey` + `sessionIdentity`/`newSessionKey`/`keyForIdentity`; the composite-keyed `lockForThread`/`TryLockThread`; the live-turn cancel registry (`trackSession`/`cancelSession`).
- `internal/runner/runner_delete.go` (created) — `DeleteConversationLifecycle` (owner gate + ordered teardown) + `resolveOwnerIdentity` + `terminateSessionJobs`.
- `internal/runner/conversation_delete_lifecycle_test.go` (created) — ordering + eviction-before-delete + foreign-gate + composite-keying isolation (unit; the `-race` variant gates in WSL/CI).
- `internal/runner/runner.go` (modified) — `sessions sync.Map` field; register the live-turn cancel in `turnLocked`; `runTurn` passes `ctx` to `lockForThread`; removed the old convID-keyed lock methods.
- `internal/runner/interfaces.go` (modified) — `ConversationStore` gained `GetForIdentity` + `DeleteForIdentity` (the lifecycle's owner gate + delete).
- `internal/agent/tools/evict.go` (modified) — `SessionJobTerminator` interface (the owner-scoped live-job kill analog of `SessionEvictor`).
- `internal/agent/tools/shell_bg_owner.go` (modified) — `BackgroundShells.TerminateSession(ownerID, sessionID)` (kill + drop the session's running shells) + `ShellPoll.TerminateSession` forwarder.
- `internal/agui/server.go` (modified) — `Runner` interface += `DeleteConversationLifecycle`; `threadTryLocker.TryLockThread` gained `ctx`; `handleRun` call site.
- `internal/agui/conversations_api.go` (modified) — `handleDeleteConversation` routes through `s.run.DeleteConversationLifecycle` (no raw store delete).
- `internal/agui/conversations_branch_api.go` (modified) — `TryLockThread(ctx, …)` call site.
- `cmd/aura/serve_channels.go` (modified) — `telegramClearAdapter` routing the Telegram `Clear` backend through the lifecycle (`identityctx` import added).
- `cmd/aura/chat.go` (modified) — `aura chat delete` routes through the lifecycle (owner-scoped to `local`; 0 rows → not-found).
- Test fakes (modified for the new interface methods): `internal/runner/fakes_test.go`, `internal/runner/runner_wiring_test.go`, `internal/agui/server_test.go`, `internal/agui/server_p1_test.go`, `internal/agui/conversations_api_unit_test.go`, `cmd/aura/cachefakes.go`, `cmd/aura/cmdfakes_test.go`.

## Decisions Made
- **Owner gate is the fail-closed prefix (security, Rule 2).** `AutoResolveForConversation(convID)` and `evictSessionToolState(convID)` are keyed only by conversation id, so running them before confirming ownership would let a foreign caller grief another identity (expire its pauses, evict its tool state) on a delete the persistence layer would then 403. Resolving `GetForIdentity` first short-circuits a foreign/absent id — `cancelSession` and `terminateSessionJobs` are already identity-scoped, so post-gate the whole teardown is owner-only.
- **Tool session state stays convID-keyed; the composite key lives on the runner's own maps.** The `SessionID` the agent hands tools is also `Event.ThreadID` and the sidecar/ledger key, so re-keying it to a composite string would break SSE routing + spillover + the gateway ledger. Instead the identity component fences the runner's thread lock and the live-turn cancel registry — the two places a cross-identity action would be a real collision/leak — and the delete lifecycle's owner gate guarantees eviction only ever runs for the confirmed owner. The conversation id is itself globally unique and owner-immutable (v7 web / v5 Telegram), so keying per-conversation tool state by it cannot collide across identities.
- **A new (owner, session) bg-kill was required.** Plan-03 shipped per-shell-id owner-gated kill (`ShellKill`), but the runner holds no shell ids at delete time. `BackgroundShells.TerminateSession(ownerID, sessionID)` kills exactly the deleted conversation's live shells (matching the plan-03 `(ownerID, sessionID)` binding), reached via the new `SessionJobTerminator` the runner ranges the registry for — the owner-scoped analog of the existing `SessionEvictor` eviction path.
- **Telegram wiring at the composition root.** The `/clear` deletion seam is the `clearBackend` (wired in `serve_channels.go`), not `bot_dispatch_turn.go`; a `telegramClearAdapter` routes it through the lifecycle, reading the owner from `identityctx` (empty → `local` today; auto-upgrades to the linked identity once the phase-12 Telegram cutover threads a principal, no rework here).
- **`aura chat delete` is now owner-scoped to `local` (intentional).** Previously an unscoped store delete; the CLI runs as `local` (D-25), so it now deletes only `local`-owned conversations and reports not-found for a foreign/absent id — the correct multi-user isolation posture.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical / Security] Owner gate added before teardown + `ConversationStore` owner-scoped methods**
- **Found during:** Task 2
- **Issue:** The plan's ordered teardown (expire pauses, evict tools) runs on steps that are NOT identity-scoped; without a prior ownership check a foreign caller could grief another identity's live session via a delete the persistence layer would ultimately deny (403).
- **Fix:** `DeleteConversationLifecycle` resolves `GetForIdentity(convID, owner)` FIRST and short-circuits a miss; added `GetForIdentity` + `DeleteForIdentity` to the runner's `ConversationStore` interface (both satisfied by `*conversations.Store` from plan 04).
- **Files:** `internal/runner/runner_delete.go`, `internal/runner/interfaces.go` (+ fakes)
- **Commit:** `4d934b3c`

**2. [Rule 2 - Missing Critical] Owner-scoped background-job termination**
- **Found during:** Task 2
- **Issue:** Lifecycle step 4 ("handle background jobs") had no mechanism — plan-03 only exposes per-shell-id kill, and the runner has no shell ids at delete time.
- **Fix:** Added `BackgroundShells.TerminateSession(ownerID, sessionID)` + `ShellPoll.TerminateSession` + the `tools.SessionJobTerminator` interface the runner ranges the registry for.
- **Files:** `internal/agent/tools/evict.go`, `internal/agent/tools/shell_bg_owner.go`
- **Commit:** `4d934b3c`

**3. [Rule 3 - Blocking] `TryLockThread` gained a `ctx` for composite thread-lock keying**
- **Found during:** Task 1
- **Issue:** The D-23 prohibition ("never key a shared in-memory map by session-id alone") applies to the per-thread run lock, which was keyed by conversation id; keying it by `(identity, session)` needs the principal, which `TryLockThread(threadID)` did not carry.
- **Fix:** `TryLockThread(ctx, threadID)` across the `agui.threadTryLocker` interface + its two call sites (`handleRun`, `rerunBranch`) + the `guardedScriptedRunner` fake + the runner wiring test. Behaviorally inert (the AG-UI owner-gate 404s a foreign request before the lock, so a given convID is only ever locked by its one owner) — the change is defence-in-depth compliance.
- **Files:** `internal/agui/server.go`, `internal/agui/conversations_branch_api.go`, `internal/agui/server_p1_test.go`, `internal/runner/runner_wiring_test.go`
- **Commit:** `4d934b3c`

**4. [File deviation] Telegram wiring in `serve_channels.go`, not `bot_dispatch_turn.go`**
- **Found during:** Task 2
- **Issue:** The plan's `files_modified` listed `internal/channels/telegram/bot_dispatch_turn.go`, but the `/clear` deletion seam is the `clearBackend`, wired at the composition root.
- **Fix:** Added `telegramClearAdapter` in `cmd/aura/serve_channels.go` routing the `Clear` dep through `DeleteConversationLifecycle`; the behavior matches the plan `key_link` (Telegram `/clear` invokes the lifecycle) exactly.
- **Commit:** `4d934b3c`

## Verification
- `go build ./...` + `go vet ./...` — clean, repo-wide.
- `go test ./internal/runner/ ./internal/agent/tools/ ./internal/agui/ ./internal/channels/telegram/ ./cmd/aura/` — all green (untagged) on this Windows host, incl. the new `TestConversationDeleteLifecycle` (3 surface shapes), `TestConversationDeleteLifecycleForeignDenied`, `TestSessionKeyIsolation`, `TestSessionKeyIsolationConcurrent`.
- **NO-SKIP-AS-GREEN:** `go test -race` is NOT runnable on this host (no cgo/gcc — `-race requires cgo`). The untagged tests pass and the `-race` tier (esp. `TestSessionKeyIsolationConcurrent`, the same-session-id-different-identity race proof) + the phase-level two-identity live E2E MUST run green in WSL/CI before phase close — honestly `unknown` here.

## Requirements
- **MUSR-05** marked complete: all conversation deletion (AG-UI, Telegram `/clear`, CLI) routes through the single lifecycle that cancels active work, expires pauses, evicts session tools, and handles background jobs before deleting persistence — delivered + unit-proven (matching the 36-03 MUSR-03/04 precedent of closing a self-contained, unit-proven mechanism; the `-race` + live two-identity E2E gate in WSL/CI at 36-12).

## Self-Check: PASSED
- Created files exist: `runner_session.go`, `runner_delete.go`, `conversation_delete_lifecycle_test.go`, `36-09-SUMMARY.md`.
- Commits present: `4d934b3c` (feat), `37341895` (test).
