---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 11
subsystem: auth
tags: [telegram, identity-isolation, identityctx, multi-user, musr-06, d-23, d-24, no-url-token, static-gate]

# Dependency graph
requires:
  - phase: 36-08
    provides: "cross-store provisioning saga + IdentityLinker.LinkUser (1:N-ready Authula<->identity binding) + web-initiated Telegram link mint"
  - phase: 36-09
    provides: "runner-backed Telegram /clear via the clearBackend seam + composite (identity, session) keying (D-23) the turn identity now feeds"
provides:
  - "Per-user Telegram turn routing: startTurn.scopeTurnToIdentity wraps the turn ctx with identityctx.WithIdentityID(account.IdentityID) at the single choke point every turn spawn passes through (fresh message, async document-convert, HITL-resume continuation), so downstream stores/tools scope to the linked user (D-23/D-24)"
  - "scripts/check-no-url-tokens.sh: the MUSR-06 static gate asserting no long-lived session/auth token is emitted into a URL/query string (with the <=1h setup-bootstrap carve-out), CI-wireable + negative-self-tested"
  - "Documented reject-unlinked fail-closed gate (no agent run for an unknown chat-id) pointing to the web-initiated linking flow"
affects: [36-12]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single per-turn identity-scoping choke point (startTurn): scope once where every spawn path converges instead of at each dispatch handler — covers text/voice/photo/asset + async-document-convert + HITL-resume with no duplication"
    - "Static security gate modelled on scripts/check-file-size.sh (git ls-files scan, comment-line filtering, --self-test negative proof, deterministic exit codes) for a codebase-wide invariant"

key-files:
  created:
    - internal/channels/telegram/bot_dispatch_multiuser_test.go
    - scripts/check-no-url-tokens.sh
  modified:
    - internal/channels/telegram/bot_dispatch_turn.go
    - internal/channels/telegram/bot_dispatch_auth.go
    - internal/agui/settings_api.go

key-decisions:
  - "Scope identity in startTurn (not runTurnWithAssets alone): startTurn is the SINGLE choke point all three turn-spawn paths funnel through (runTurnWithAssets fresh turn, the onDocument async-convert callback, and the hitlFor HITL-resume continuation). Scoping there closes T-36-11-I2 for every path with one change, in the plan's key_link file (bot_dispatch_turn.go), instead of scoping the primary path only and leaking the async/resume paths"
  - "Resolve the turn owner by chat id via the SAME account seam the reject-unlinked gate uses. Aura Telegram is a personal DM channel (chat id == sender telegram user id, the identity the gate validated, and the whole channel already keys convID(chatID)), so a per-chat lookup yields exactly the turn's owner and is available to the message-less HITL-resume path"
  - "MUSR-06 gate targets LONG-LIVED session/auth keys only (session/authula_session/access_token/bearer/...); the generic start=/token= bootstrap keys are deliberately excluded so the <=1h Telegram ?start= + setup wizard ?token= (onboardingTTL=1h) carve-out passes with no per-line allowlist"
  - "Redaction/secret-scan modules excluded from the gate (documented): they STRIP tokens from URLs, so their source + fixtures legitimately hold ?access_token=/?token= patterns — the mitigation, not a leak"
  - "settings_api.go Telegram-link route kept self-scoped USER (D-02, mints for the authenticated requester, no operator-pinning) — confirmed correct + documented, not restructured; the write-class capability gate loosening is the 36-12 Authula cutover"

patterns-established:
  - "Per-user channel routing: a channel resolves each inbound principal to its own identity and threads it via identityctx into the turn ctx, so the runner + tool stores owner-scope transparently"

requirements-completed: []  # MUSR-06 is phase-spanning — mechanism (per-user routing + no-URL-token gate) delivered here; the Authula default cutover + capability-per-route + two-identity live E2E close it at 36-12. `requirements mark-complete` intentionally NOT run (matches the 36-01/02/08/10 precedent).

# Coverage metadata
coverage:
  - id: D1
    description: "A linked Telegram user's turn context carries identityctx.WithIdentityID(account.IdentityID) — two distinct users get ISOLATED identity contexts, never each other's and never the local-admin fallback (D-23/D-24, T-36-11-I2)"
    requirement: "MUSR-06"
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_multiuser_test.go#TestTelegramPerUserTurnScopesToOwnIdentity"
        status: pass
    human_judgment: false
  - id: D2
    description: "An unknown/unlinked chat-id drives NO agent run and is pointed to the web-initiated linking flow (reject-unlinked fail-closed gate, T-36-11-S)"
    requirement: "MUSR-06"
    verification:
      - kind: unit
        ref: "internal/channels/telegram/bot_dispatch_multiuser_test.go#TestTelegramUnlinkedChatDrivesNoTurnAndPromptsWebLinking"
        status: pass
    human_judgment: false
  - id: D3
    description: "No long-lived session/auth token appears in any URL/query string across the Go + web source (MUSR-06, T-36-11-I); <=1h Telegram ?start= / setup ?token= bootstrap allowed"
    requirement: "MUSR-06"
    verification:
      - kind: automated
        ref: "bash scripts/check-no-url-tokens.sh (exit 0 on the current tree)"
        status: pass
      - kind: automated
        ref: "bash scripts/check-no-url-tokens.sh --self-test (planted ?session_token= violation caught)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Web-initiated Telegram linking stays self-scoped USER (D-02): POST /api/settings/telegram/link mints for the authenticated requester, no operator-pinning; the code rides the <=1h ?start= bootstrap deep-link"
    requirement: "MUSR-06"
    verification:
      - kind: unit
        ref: "internal/agui/onboarding_api_test.go#TestCreateTelegramLinkMintsCurrentIdentityRecoveryQR (existing; behavior confirmed + documented, no behavior change)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Full multi-user proof under the race detector + the phase two-identity live E2E (two real Telegram identities, isolated contexts end-to-end)"
    verification:
      - kind: e2e
        ref: "go test -race ./internal/channels/telegram/ + phase two-identity live E2E — must run in WSL/CI (no CGO/gcc on this Windows host)"
        status: unknown
    human_judgment: true
    rationale: "Race tier + live two-identity E2E cannot run on this Windows host (CGO disabled, no live stack); the phase-level E2E is the 36-12 close criterion. No-skip-as-green: verify live before phase close."

# Metrics
duration: ~30 min
completed: 2026-07-06
status: complete
---

# Phase 36 Plan 11: Multi-User Telegram Identity Routing + MUSR-06 No-URL-Token Gate Summary

**Every inbound Telegram turn now scopes to the linked user's Aura identity via `identityctx.WithIdentityID(account.IdentityID)` at the single `startTurn` choke point (fresh message, async document-convert, and HITL-resume all covered), an unlinked chat-id still runs no agent and is pointed to the web-initiated linking flow, and a new CI-wireable static gate proves no long-lived session token ever appears in a URL/query string (MUSR-06 / D-23 / D-24).**

## Performance

- **Duration:** ~30 min
- **Completed:** 2026-07-06
- **Tasks:** 2
- **Files created/modified:** 5 (2 created, 3 modified)

## Accomplishments
- **Per-user turn routing (D-23/D-24):** `startTurn.scopeTurnToIdentity` wraps the turn ctx with the linked user's identity, resolved from the chat's account through the same seam the reject-unlinked gate uses. Because `startTurn` is the one point every turn spawn converges on, per-user isolation holds for a fresh message, the async document-convert callback, and a HITL-resume continuation alike — closing the T-36-11-I2 cross-user-context leak on all paths.
- **Reject-unlinked, web-linking prompt (T-36-11-S):** confirmed + documented the fail-closed gate — an unknown chat-id drives no agent run; the reply now points to the web-initiated linking flow (Settings→Telegram one-time code → `/start`), keeping the "attivazione" contract the existing tests assert.
- **MUSR-06 static gate:** `scripts/check-no-url-tokens.sh` asserts no long-lived session/auth token is emitted into a URL/query across Go + web source, with the ≤1h setup-bootstrap carve-out (Telegram `?start=`, setup wizard `?token=`). Negative-self-tested (a planted `?session_token=` is caught), deterministic exit codes, executable, CI-wireable.
- **Adversarial proof:** a two-user isolation test asserts each user's turn carries its OWN identity, never the other's and never the local admin (…001).

## Task Commits

Each task was committed atomically:

1. **Task 1: Per-user Telegram identity routing into the turn context (D-24)** - `0db75d12` (feat)
2. **Task 2: No-long-lived-token-in-URL static gate + audit** - `2523dfa8` (chore)

## Files Created/Modified
- `internal/channels/telegram/bot_dispatch_turn.go` - `scopeTurnToIdentity` helper + `identityctx.WithIdentityID(account.IdentityID)` scoping in `startTurn` (the single per-turn choke point).
- `internal/channels/telegram/bot_dispatch_auth.go` - documented the D-24 fail-closed `telegramUserIsLinked` gate; `activationRequiredMsg` now points to the web-initiated Settings→Telegram linking flow (keeps the "attivazione" keyword).
- `internal/agui/settings_api.go` - doc comment on `handleCreateSettingsTelegramLink` encoding the D-02 self-scoped-USER / D-24 web-initiated / MUSR-06 ≤1h-bootstrap contract (no behavior change).
- `internal/channels/telegram/bot_dispatch_multiuser_test.go` (new) - two-user identity-isolation proof + unlinked-no-turn/web-prompt proof.
- `scripts/check-no-url-tokens.sh` (new, executable) - the MUSR-06 no-URL-token static gate + `--self-test`.

## Decisions Made
- **Scope in `startTurn`, not just the primary path.** Discovering that `startTurn` is the common spawn point for the fresh-message, async-document-convert (`bot_dispatch.go`), and HITL-resume (`bot_dispatch_hitl.go`) paths let one change in the plan's key_link file (`bot_dispatch_turn.go`) cover all three, rather than scoping `runTurnWithAssets` alone and silently leaving the async/resume turns unscoped (→ local-admin fallback = leak).
- **Resolve the owner by chat id.** Aura Telegram is a personal DM channel — chat id == sender telegram user id (the identity the gate validated), the whole channel already keys `convID(chatID)`, and the message-less HITL-resume path only has the chat id. So a per-chat lookup is both correct and universally available. Documented the DM assumption in the helper.
- **Gate targets long-lived keys only.** `session/authula_session/access_token/auth_token/refresh_token/id_token/bearer_token/session_token/session_id/...` in query position; generic `start=`/`token=` bootstrap keys are excluded so the ≤1h carve-out passes with no per-line allowlist. Redaction/secret-scan modules are excluded (they strip tokens from URLs — the mitigation).
- **settings_api.go left behavior-identical.** The link route was already self-scoped USER with no operator-pinning (`CreateTelegramLink(requester)`); confirmed + documented rather than restructured. The write-class capability-gate loosening is the 36-12 Authula cutover.

## Deviations from Plan

None material — the plan was executed as written. Two in-scope refinements worth noting (neither is new behavior beyond the plan's intent):

**1. [Rule 2 - Correctness/Security] Scoped ALL three turn-spawn paths, not just the primary.**
- **Found during:** Task 1 — tracing `startTurn` callers surfaced two additional spawn paths (async document-convert in `bot_dispatch.go`, HITL-resume in `bot_dispatch_hitl.go`) beyond `runTurnWithAssets`.
- **Fix:** placed the `identityctx.WithIdentityID` scoping inside `startTurn` (the shared choke point) instead of in `runTurnWithAssets`, so the async-convert and HITL-resume continuations are identity-scoped too. No extra files touched — the scoping lives entirely in `bot_dispatch_turn.go` (in `files_modified`), and the other two paths inherit it because they call `startTurn`.
- **Verification:** `TestTelegramPerUserTurnScopesToOwnIdentity` (per-user isolation) + `go build`/`go vet`/`go test` green.
- **Committed in:** `0db75d12`.

**2. [Preserved contract] Kept the "attivazione" keyword in the reject-unlinked reply.**
- **Found during:** Task 1 — refining `activationRequiredMsg` to point at web linking initially dropped the word, tripping `TestOnTextUnlinkedUserRequiresActivationNoTurn` / `TestOnReplyUnlinkedUserDoesNotDoubleActivationOnText` which assert the reply contains "attivazione".
- **Fix:** reworded to retain "attivazione" while pointing to Settings→Telegram (per CLAUDE.md, fixed the message to honor the existing test contract rather than editing the tests).
- **Verification:** both onboarding tests green.
- **Committed in:** `0db75d12`.

## Issues Encountered
- **`go test -race` cannot run on this Windows host** (`CGO_ENABLED=0`, no gcc). The untagged unit tiers pass; the `-race` tier (esp. the concurrent per-user isolation) MUST run green in WSL/CI before phase close (no-skip-as-green). Honestly `unknown` here.

## Known Stubs
None. No hardcoded empty/placeholder values were introduced; the identity routing and the gate are fully wired.

## Next Phase Readiness
- **36-12 (phase close):** MUSR-06 mechanism (per-user Telegram routing + no-URL-token gate) is in place. 36-12 wires `scripts/check-no-url-tokens.sh` into `ci.yml`, flips the Authula default + capability-per-route, and runs the two-identity live E2E (the `[x]` flip for MUSR-06). The gate is CI-wireable as-is (`bash scripts/check-no-url-tokens.sh`, exit 0 clean / 1 violation).
- **Blocker/watch:** run the `-race` tier + the two-identity live E2E on WSL/CI before declaring the phase done.

## Self-Check: PASSED

- All created/modified files present on disk (bot_dispatch_multiuser_test.go, check-no-url-tokens.sh, bot_dispatch_turn.go, bot_dispatch_auth.go, settings_api.go, 36-11-SUMMARY.md).
- Both task commits present in git history (`0db75d12` feat, `2523dfa8` chore).

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-06*
