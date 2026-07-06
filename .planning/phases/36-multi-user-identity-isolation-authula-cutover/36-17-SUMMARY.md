---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 17
subsystem: security
tags: [multi-user, identity-isolation, telegram, fail-closed, shell, capability, auth, agui]

# Dependency graph
requires:
  - phase: 36-11
    provides: "startTurn.scopeTurnToIdentity single-choke-point per-user turn scoping this plan makes fail-closed"
  - phase: 36-14
    provides: "identity store on chatEnv + shell owner-authority seam (shell_bg_owner.go Caps) this plan wires live"
  - phase: 36-03
    provides: "background-shell owner binding + adminShellCapability (D-18) exemption path"
provides:
  - "fail-closed scopeTurnToIdentity(ctx, chatID) (context.Context, bool) + startTurn drop-on-unresolved (HI-03)"
  - "private-chat enforcement in requireLinkedMessage/requireLinkedCallback (gate sender-id key == turn-scope chat-id key)"
  - "live ShellPoll.Caps/ShellKill.Caps wiring at serve boot (D-18 admin cross-session recovery reachable; owner-only default preserved) (VERIF-7)"
  - "blank-principal RequireAuth rejection regression test (LO-02)"
affects: [36-18, telegram, agui, agent/tools]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Channel principal resolution FAILS CLOSED: an unresolved/divergent-key inbound update is DROPPED, never left on an unscoped ctx that a downstream resolver upgrades to the local admin"
    - "Personal-DM appliance invariant enforced at the gate (Chat.Type==ChatPrivate) so the reject-unlinked sender-id key and the turn-scope chat-id key are provably identical"
    - "Deferred capability seam wired at the composition root only (nil Caps = owner-only fail-closed on pool-free paths; live store set at serve boot for the admin exemption)"

key-files:
  created:
    - internal/channels/telegram/bot_dispatch_failclosed_test.go
    - internal/agui/auth_blank_principal_test.go
  modified:
    - internal/channels/telegram/bot_dispatch_turn.go
    - internal/channels/telegram/bot_dispatch_auth.go
    - internal/channels/telegram/bot_dispatch_test.go
    - cmd/aura/main.go
    - cmd/aura/serve.go
    - cmd/aura/serve_provisioning_test.go

key-decisions:
  - "scopeTurnToIdentity returns (ctx, bool); on a nil resolver OR a GetAccountByTelegramID miss it returns (ctx, false) so startTurn DROPS the turn — an unresolved principal is never run on an unscoped ctx (which every downstream resolver would upgrade to the local admin)"
  - "Private-chat check is FIRST in requireLinkedMessage/requireLinkedCallback (before the linked-account check) so a group/divergent-key update is rejected up front; with private-only, msg.Chat.ID == msg.Sender.ID"
  - "ShellPoll/ShellKill pointers retained on runtimeToolHandles with a nil Caps at buildBaseRegistryWithHandles (owner-only fail-closed for pool-free manifest paths); Caps set to chat.identity at serve boot behind a nil guard"
  - "MUSR-01/MUSR-03 NOT marked complete: HI-03/VERIF-7/LO-02 closed here, but the requirements are phase-spanning (live full-matrix E2E + push close at 36-18) — follows the 36-05..36-16 precedent"

patterns-established:
  - "A channel's principal-resolution miss DROPS the turn (fail closed), it never silently upgrades to the seeded local admin"

requirements-completed: []  # MUSR-01/MUSR-03 are phase-spanning; HI-03/VERIF-7/LO-02 closed here, requirements close at 36-18 (matches 36-05..36-16 precedent)

coverage:
  - id: D1
    description: "Telegram turn-scoping fails closed: an unresolved chat drops the turn (driver never invoked); a linked private chat runs scoped to its own identity; a non-private (group) chat is rejected up front"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_failclosed_test.go#TestTelegramFailClosedUnresolvedTurnDropped"
        status: pass
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_failclosed_test.go#TestTelegramScopeLinkedPrivateChatRunsAsIdentity"
        status: pass
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_failclosed_test.go#TestTelegramPrivateChatEnforcedNonPrivateRejected"
        status: pass
      - kind: integration
        ref: "go test -race ./internal/channels/telegram/ (WSL, CGO on) — clean"
        status: pass
    human_judgment: false
  - id: D2
    description: "ShellPoll.Caps/ShellKill.Caps wired to the live capability store at serve boot (D-18 admin cross-session recovery reachable) while a nil Caps keeps the owner-only fail-closed default; a foreign non-admin poll/kill stays denied"
    requirement: "MUSR-03"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_bg_owner_test.go#TestBackgroundJobOwnerDeny (owner-only deny unchanged)"
        status: pass
      - kind: integration
        ref: "go test -race ./internal/agent/tools/ ./cmd/aura/ (WSL, CGO on) — clean"
        status: pass
    human_judgment: false
  - id: D3
    description: "An authenticated web request whose principal is blank ('', ok=true) is REJECTED by RequireAuth (302 browser / 401 XHR), never scoped to local"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/agui/auth_blank_principal_test.go#TestRequireAuthRejectsBlankPrincipal"
        status: pass
    human_judgment: false

# Metrics
duration: ~45 min
completed: 2026-07-06
status: complete
---

# Phase 36 Plan 17: Fail-closed Telegram scoping + shell admin-cap wiring + blank-principal regression (HI-03/VERIF-7/LO-02) Summary

**The Telegram channel and background-shell tool now fail CLOSED: an unresolved/divergent-key inbound turn is dropped (never run as the seeded local admin), non-private chats are rejected up front so the gate's sender-id key equals the turn-scope chat-id key, the D-18 admin poll/kill exemption is reachable (Caps wired to the live store) with the owner-only default intact, and a blank-principal web request is proven rejected.**

## Performance

- **Duration:** ~45 min
- **Started:** 2026-07-06T11:10:00+02:00 (approx)
- **Completed:** 2026-07-06T11:55:00+02:00 (approx)
- **Tasks:** 2 (Task 1 TDD RED→GREEN)
- **Files modified:** 6 modified + 2 created

## Accomplishments
- **HI-03 (High) closed:** `scopeTurnToIdentity` now returns `(context.Context, bool)` and FAILS CLOSED — a nil resolver or a `GetAccountByTelegramID` miss returns `(ctx, false)` so `startTurn` DROPS the turn instead of proceeding on an unscoped ctx. An unscoped ctx would let every downstream resolver (`sessionIdentity`, `ownerFromContext`, `defaultConversationOwner`) apply its `local` (admin) fallback, so a group-chat / divergent-key update would have run a linked non-admin's turn in the operator context. `startTurn` is the single choke point (fresh / async-doc / HITL-resume), so all three paths inherit the drop.
- **Key unification:** `requireLinkedMessage` + `requireLinkedCallback` reject non-private chats up front (`msg.Chat.Type != tele.ChatPrivate` → `activationRequiredMsg`/`activationRequiredToast`). With private-only, `msg.Chat.ID == msg.Sender.ID`, so the reject-unlinked gate's sender-id key and `startTurn`'s chat-id scope key are provably the same id — closing the exact divergence the review found.
- **VERIF-7 closed:** `runtimeToolHandles` now retains the `*ShellPoll`/`*ShellKill` pointers; `buildBaseRegistryWithHandles` registers the retained pointers with a nil `Caps` (owner-only fail-closed for the pool-free manifest paths), and `serve.go` sets `.Caps = chat.identity` at boot behind a nil guard. The D-18 admin cross-session recovery exemption (`adminShellCapability = governance.write`, held by the seeded local admin via 0026) is now reachable, while a foreign non-admin poll/kill stays denied (nil Caps ⇒ owner-only preserved everywhere else).
- **LO-02 locked:** a new regression test proves an authenticated session that validates to a BLANK identity (`("", true)`) is rejected by `RequireAuth` (302 browser / 401 XHR) and never reaches the wrapped handler — guarding the invariant `RequireAuth` + `session_validate` already uphold against a future change turning the no-principal `local` fallback into a blank-principal impersonation.
- **LIVE -race proof:** `go test -race ./internal/channels/telegram/ ./internal/agent/tools/ ./internal/agui/ ./cmd/aura/` all clean under WSL (CGO on).

## Task Commits

Task 1 was TDD (RED→GREEN); each commit is atomic (direct git commit, real hooks):

1. **Task 1 (RED): failing fail-closed telegram scoping + private-chat tests** - `42bab8fb` (test)
2. **Task 1 (GREEN): fail-closed turn scoping + private-chat enforcement (HI-03)** - `7c526296` (feat)
3. **Task 2: wire shell poll/kill admin caps at serve boot + blank-principal regression (VERIF-7/LO-02)** - `fe94ced7` (feat)
4. **Lint escape fix: drop dead nil-check (staticcheck SA4023)** - `3e9b5ef2` (style)

**Plan metadata:** this commit (docs: complete plan)

## Files Created/Modified
- `internal/channels/telegram/bot_dispatch_failclosed_test.go` (NEW) — three HI-03 proofs: unresolved turn dropped, linked private chat scoped to own identity, non-private (group) chat rejected up front
- `internal/channels/telegram/bot_dispatch_turn.go` — `scopeTurnToIdentity` fail-closed signature `(context.Context, bool)`; `startTurn` returns early (drops the turn) on `!ok`; doc-comments state the fail-closed posture
- `internal/channels/telegram/bot_dispatch_auth.go` — `requireLinkedMessage`/`requireLinkedCallback` reject non-private chats before the linked-account check
- `internal/channels/telegram/bot_dispatch_test.go` — `chatMsg` test helper sets `Type: tele.ChatPrivate` (the personal-DM invariant the new gate enforces)
- `cmd/aura/main.go` — `runtimeToolHandles.ShellPoll/ShellKill` fields; `buildBaseRegistryWithHandles` retains the pointers and registers them (nil Caps = owner-only)
- `cmd/aura/serve.go` — sets `ShellPoll/ShellKill.Caps = chat.identity` at serve boot behind a nil guard (D-18 admin exemption reachable)
- `internal/agui/auth_blank_principal_test.go` (NEW) — `TestRequireAuthRejectsBlankPrincipal` (302 browser / 401 XHR; next never reached)
- `cmd/aura/serve_provisioning_test.go` — dropped a dead `if purger == nil` runtime check (staticcheck SA4023) in favor of a compile-time interface-satisfaction assertion (lint-escape fix, see deviations)

## Decisions Made
- **Fail-closed over best-effort:** the previous `scopeTurnToIdentity` returned the ctx unchanged on a miss (best-effort); this plan makes the miss authoritative — the turn is refused, not silently downgraded. This is the correct posture for a per-user isolation surface (deny an unresolved principal, never upgrade it).
- **Private-check FIRST:** placing the `Chat.Type != ChatPrivate` reject before the linked-account check means a group message from a linked member is rejected on the chat-type invariant, not the membership check — proving the chat-type gate is the up-front control that unifies the two keys.
- **Caps stays nil at build, set at serve:** the pointer-retention + serve-time `.Caps` set keeps the pool-free manifest paths (`aura tools`, `buildRegistry`) owner-only fail-closed while making the admin exemption reachable only where the identity store exists — a zero-behavior-change wiring for every non-serve path.
- **Requirements not marked complete:** MUSR-01/MUSR-03 are phase-spanning; HI-03/VERIF-7/LO-02 are the hardening this plan owns, but the requirement checkboxes close at 36-18 with the live full-matrix E2E + push (matches 36-05..36-16).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Patched the shared `chatMsg` test helper to model the personal-DM invariant (Type=ChatPrivate)**
- **Found during:** Task 1 (private-chat enforcement)
- **Issue:** The new `requireLinkedMessage`/`requireLinkedCallback` reject any chat whose `Chat.Type != ChatPrivate`. The shared `chatMsg` helper (used by ~all dispatch, media, callback, HITL, and multiuser tests) built messages with an empty `Chat.Type`, so the new gate would have rejected every helper-built message and broken the whole existing telegram suite. `bot_dispatch_test.go` is not in the plan's `files_modified`.
- **Fix:** `chatMsg` now sets `Type: tele.ChatPrivate` (with a doc-comment) — the real inbound invariant for a personal-DM appliance; the new Test 3 builds an explicit non-private (group) message. Every existing test stays valid (they route linked private DMs, which the gate passes).
- **Files modified:** internal/channels/telegram/bot_dispatch_test.go
- **Verification:** full untagged `go test ./internal/channels/telegram/` green (before the fix only the 2 new tests failed = clean RED; after the fix all green); WSL `-race` clean.
- **Committed in:** `42bab8fb` (RED, test-support)

**2. [Rule 1 - Lint escape] Fixed a pre-existing staticcheck SA4023 in the touched cmd/aura package**
- **Found during:** Task 2 (post-implementation golangci-lint on the touched packages, per the host lint-discipline mandate)
- **Issue:** `cmd/aura/serve_provisioning_test.go` (a 36-14 file) had a dead `if purger == nil` runtime check after `var purger handlers.IdentityPurger = dep` where `dep` (a concrete `*Deprovisioner`) was already asserted non-nil — SA4023 "this comparison is never true". `go vet` misses it; golangci-lint flags it. This is exactly the class of sibling-plan lint escape the host rules flagged, and it sat in a package I touched (cmd/aura), so it broke the golangci-lint=0 mandate / phase CI-green close.
- **Fix:** replaced the dead runtime check with the compile-time interface-satisfaction assertion `var _ handlers.IdentityPurger = dep`, preserving the test's intent (it still proves `*Deprovisioner` satisfies `handlers.IdentityPurger`).
- **Files modified:** cmd/aura/serve_provisioning_test.go
- **Verification:** golangci-lint on the four touched packages → **0 issues**; `TestBuildDeprovisionerWiresPurger` still PASS.
- **Committed in:** `3e9b5ef2` (style)

---

**Total deviations:** 2 auto-fixed (1 blocking test-support, 1 lint escape)
**Impact on plan:** Both are within the plan's own intent (its private-chat enforcement needs a private-DM harness; the host mandates golangci-lint=0 on touched packages). No production behavior beyond the plan; no new dependencies; no query/schema/migration changes.

## Threat Flags
None — no new network endpoints, auth paths, file-access patterns, or schema changes. The change set is a fail-closed tightening of an existing channel scope, a composition-root capability wiring, and two regression tests. No new dependencies (go.mod/go.sum byte-unchanged; no query files touched → `sqlc generate` trivially zero-diff).

## Issues Encountered
- Direct `git commit` runs the lefthook file-size scan over the whole tree (~150s), exceeding the 2-min Bash foreground default. Resolved by committing with an extended timeout (per the documented host workflow). The commits landed; hooks (gofmt/vet/file-size) all green.

## Verification (real results)
- `CGO_ENABLED=0 go build ./...` — exit 0 (clean).
- `CGO_ENABLED=0 go vet ./...` — exit 0 (clean).
- gofmt — clean on all touched files.
- Native untagged targeted: `go test ./internal/channels/telegram/ -run 'FailClosed|Scope|Private|MultiUser'` (4 PASS), `go test ./internal/agui/ -run BlankPrincipal` (PASS), `go test ./internal/agent/tools/ -run 'ShellPoll|Owner|Kill'` (PASS).
- Native untagged full: `go test ./cmd/aura/ ./internal/agent/tools/ ./internal/agui/ ./internal/channels/telegram/` — all ok.
- **LIVE WSL `-race` (CGO on):** `./internal/channels/telegram/` (25.4s, ok), `./internal/agent/tools/` (3.8s, ok), `./internal/agui/` (36.7s, ok), `./cmd/aura/` (13.9s, ok) — the required telegram + agent/tools tier and the two other touched packages all clean.
- **golangci-lint** (WSL) on `./internal/channels/telegram/... ./internal/agent/tools/... ./internal/agui/... ./cmd/aura/...` — **0 issues** (after the SA4023 escape fix).

## Next Phase Readiness
- HI-03 (High) / VERIF-7 / LO-02 closed: the Telegram channel + background-shell tool fail closed, the D-18 admin recovery path is active with the owner-only default intact, and the blank-principal invariant is locked.
- Remaining for 36-18 (phase close, autonomous:false): the live full-matrix two-identity E2E (Garage/Authula `musr-e2e` CI job), full-matrix coverage ≥85%, mutation spot-check, CI-green + push, and the AURA_MUSR_ISOLATION rollout decision. MUSR-01/MUSR-03 stay `[ ]` until that live E2E + push (phase-spanning, matches 36-05..36-16).

## Self-Check: PASSED
- All created/modified source files present on disk (8/8 spot-checked, incl. the two new test files).
- All four task commits found in git history: `42bab8fb` (test), `7c526296` (feat), `fe94ced7` (feat), `3e9b5ef2` (style).
- Source assertions hold: `scopeTurnToIdentity(...) (context.Context, bool)` + `if !ok { ... return }` in bot_dispatch_turn.go; `msg.Chat.Type != tele.ChatPrivate` in bot_dispatch_auth.go (both gates); `ShellPoll`/`ShellKill` on `runtimeToolHandles` + `.Caps = chat.identity` in serve.go; `TestRequireAuthRejectsBlankPrincipal` in auth_blank_principal_test.go.

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-06*
