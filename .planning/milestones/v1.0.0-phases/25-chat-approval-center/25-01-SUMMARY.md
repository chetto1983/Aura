---
phase: 25-chat-approval-center
plan: 01
subsystem: api
tags: [agui, conversations, sse, reasoning, rest, http, postgres, fts]

# Dependency graph
requires:
  - phase: 24-web-foundation
    provides: "RequireAuth whole-origin gate + the single-binary serve_webui parent mux"
  - phase: 04-conversations
    provides: "conversations.Store (List/Get/Search/Rename/UpdateStatus/Delete) + context_rot_events"
  - phase: 08-agui
    provides: "agui Server + translator (showReasoning gating) + the narrow ConversationStore interface"
provides:
  - "GET /api/conversations (+ ?archived=true) — conversation list"
  - "GET /api/conversations/search?q=&limit= — FTS snippet hits"
  - "GET /api/conversations/{id} — single row incl. session-cumulative aggregates (D-10 footer seed)"
  - "GET /api/conversations/{id}/rot-events — microcompact ladder events (D-11 gauge markers)"
  - "POST /api/conversations/{id}/rename | /archive | /unarchive + DELETE /api/conversations/{id}"
  - "Cockpit SSE stream now emits live REASONING_* delta text (D-01)"
  - "conversations.ListContextRotEvents read wrapper + RotEvent projection"
affects: [25-02, 25-03, 25-04, chat-frontend, conversation-sidebar, runtime-footer, context-budget-gauge]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Thin REST adapter over a shipped Store: uuid.Parse-guard -> exactly one store method -> JSON, no business logic"
    - "Consumer-interface widening (accept interfaces, return structs) — *conversations.Store satisfies it implicitly"
    - "Specific /api/ subtree mount (never bare /api/) to avoid the integrations-proxy shadow (Pitfall 6)"

key-files:
  created:
    - internal/agui/conversations_api.go
    - internal/agui/conversations_api_test.go
    - internal/agui/conversations_api_unit_test.go
  modified:
    - internal/agui/types.go
    - internal/agui/server.go
    - internal/conversations/context.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve_webui_test.go
    - internal/agui/translator_reasoning_test.go
    - internal/channels/telegram/agui_subscriber_test.go

key-decisions:
  - "D-01: cockpit SSE passes showReasoning=true at the handleRun call site (was hard-coded false); cockpit-scoped, Telegram + Fanout postures provably untouched"
  - "D-10: GET /api/conversations/{id} returns the single Conversation incl. the session-cumulative token/cost aggregates as the footer reload seed"
  - "D-11: rot-events exposed via a thin ListContextRotEvents wrapper over the EXISTING sqlc query — no new query, no new migration"
  - "Registered BOTH /api/conversations/ (subtree) and the exact /api/conversations (list, no trailing slash) so the list GET is not 301-redirected into the subtree and lost"

patterns-established:
  - "Thin HTTP adapter discipline: each handler is one parse + one uuid.Parse guard + one store call + JSON projection; errors redacted with sanitizeErr"
  - "Error-projection unit coverage via an errConvStore double so the 500/404/400 branches run in CI without the live stack"

requirements-completed: [CHAT-02, CHAT-03]

# Metrics
duration: ~55min
completed: 2026-06-17
---

# Phase 25 Plan 01: Chat + Approval Center — Conversation REST Adapter & Cockpit Reasoning Summary

**Wired the thin `/api/conversations…` REST surface (browse/search/get/rename/archive/delete + rot-events) over the shipped conversations.Store behind the Phase-24 RequireAuth gate, and flipped the cockpit SSE path to stream live reasoning deltas (D-01) cockpit-scoped — all minimal-mechanism wiring over shipped seams, adding zero new dependencies.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-06-17 (execution start)
- **Completed:** 2026-06-17
- **Tasks:** 3 of 3 (+ 1 coverage-hardening commit)
- **Files created/modified:** 11

## Accomplishments
- CHAT-02: seven thin conversation routes, each mapping 1:1 to exactly one `conversations.Store` method, malformed ids clean-404 (never 500), errors redacted on the wire — verified live against the `aura.*` schema with `-race`.
- CHAT-03 (backend half): the cockpit SSE stream now emits real `REASONING_*` delta text (D-01), cockpit-scoped, with the Telegram `t.deps.ShowReasoning` posture provably unchanged and a golden test on each side.
- The `/api/conversations/` subtree mounts specifically behind `RequireAuth` with the `/api/integrations/` proxy proven unshadowed after the new mount.
- `internal/agui` owned-surface coverage rose 88.0% → 91.2% (every conversation handler now 100%), comfortably above the 85% floor.

## Task Commits

Each task was committed atomically:

1. **Task 1: Conversation REST adapter + widened consumer interface (CHAT-02)** — `276f2c2f` (feat)
2. **Task 2: Cockpit reasoning-on flip (D-01 / CHAT-03 backend)** — `64eb764b` (feat)
3. **Task 3: Mount /api/conversations/ behind RequireAuth (Pitfall 6)** — `1719680d` (feat)
4. **Coverage hardening: conversation-API error paths + parseLimit** — `c738c1a5` (test)

## Files Created/Modified
- `internal/agui/conversations_api.go` — seven thin handlers (list, get-single-with-aggregates, search, rename, archive/unarchive, delete, rot-events) + the route registration on the agui Server.Mux.
- `internal/agui/conversations_api_test.go` — `db_integration` coverage of every route→Store-method mapping + malformed-id and not-found edge cases against the live DB.
- `internal/agui/conversations_api_unit_test.go` — untagged error-projection coverage (500 redaction, 404 mapping, 400 validation, parseLimit) via an `errConvStore` double.
- `internal/agui/types.go` — widened the consumer-side `ConversationStore` interface with List/Search/UpdateStatus/Rename/SetTitleIfNull/Delete/ListContextRotEvents.
- `internal/agui/server.go` — registered the `/api/conversations/` routes in `Mux()`; flipped the cockpit `Translate(…, true)` at `handleRun` with the whole-origin-private justification comment.
- `internal/conversations/context.go` — added the thin `ListContextRotEvents` store wrapper + `RotEvent` projection over the existing sqlc query (no new query/migration).
- `cmd/aura/serve_webui.go` — mounted the specific `/api/conversations/` subtree (+ exact list path) behind `RequireAuth`; constants `conversationsRoutePrefix` / `conversationsListRoute`.
- `cmd/aura/serve_webui_test.go` — assert the new subtree reaches the AG-UI handler, the integrations proxy is unshadowed, and the gate is inherited.
- `internal/agui/translator_reasoning_test.go` — `TestHandleRunCockpitStreamsReasoning` proves the call-site flip at the handler.
- `internal/channels/telegram/agui_subscriber_test.go` — `TestHandleTurnReasoningPostureFollowsConfig` proves the Telegram posture stays config-driven, not a forced `true`.

## Decisions Made
- **D-01 cockpit flip is cockpit-scoped, not global.** The call graph was confirmed first: `handleRun` (server.go) is the cockpit-only SSE path; Telegram uses its own `agui.Translate(…, t.deps.ShowReasoning)` call site and the programmatic pump uses `Subscribe`/`NewFanout` — neither flows through `handleRun`. The flip therefore touches only the cockpit. Machine-checkable: Telegram `Translate(.*, true)` count == 0, `Translate(.*ShowReasoning` count == 1.
- **rot-events needed NO new query or migration.** `ListContextRotEvents` already existed in sqlc; only a thin store read wrapper + JSON projection were added (T-25-SC: zero new dependency, pure wiring).
- **Both `/api/conversations/` and the exact `/api/conversations` are registered.** Go 1.22 ServeMux would 301-redirect the no-trailing-slash list GET into the subtree, dropping it, so the exact path is registered explicitly — caught and fixed during Task 3 (see deviations).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Registered the exact `/api/conversations` list path alongside the subtree**
- **Found during:** Task 3
- **Issue:** The plan specified `mux.Handle("/api/conversations/", aguiHandler)` (trailing-slash subtree). On the parent mux, Go 1.22 ServeMux 301-redirects the exact `/api/conversations` (the list GET, which has no trailing slash) into the subtree, which the agui sub-mux's exact `GET /api/conversations` route would then never match — the list endpoint would be effectively unreachable through the parent mux.
- **Fix:** Added `mux.Handle(conversationsListRoute, aguiHandler)` (exact `/api/conversations`) alongside the subtree, with a comment explaining the 301 hazard. The behavioral test asserts both the exact list path and a `{id}` subtree path reach the AG-UI handler.
- **Files modified:** cmd/aura/serve_webui.go, cmd/aura/serve_webui_test.go
- **Commit:** `1719680d`

**2. [Rule 2 - Missing critical functionality] Added error-projection unit coverage**
- **Found during:** post-Task-3 coverage check
- **Issue:** The db_integration suite covers the happy-path route→method mapping, but the 500/error-redaction (T-12-10 DSN leak guard), 404 mapping, and 400 validation branches were only exercised on the live stack — leaving them uncovered in CI without the DB and below the per-handler coverage the floor expects.
- **Fix:** Added `conversations_api_unit_test.go` with an `errConvStore` double exercising every error branch (incl. asserting a DSN password in a store error is redacted on the wire). Coverage 88.0% → 91.2%, every handler 100%.
- **Files modified:** internal/agui/conversations_api_unit_test.go (new)
- **Commit:** `c738c1a5`

## Threat Model Coverage
- **T-25-01 (FTS tampering):** the search route binds the query string to the LOCKED sqlc `content % $1` contract via `SearchConversationTurns` — never interpolated or rewritten in the handler.
- **T-25-02 (malformed-id 500 leak):** `parseConvID` uuid.Parses every `{id}` BEFORE the store round-trip → clean 404; asserted across all id routes (live + unit).
- **T-25-03 (reasoning CoT live, accepted):** D-01 surfaces CoT live to the authenticated operator only (whole-origin-private cockpit); the trace still does not persist verbatim (HARDEN-05 unchanged); Telegram posture provably unchanged.
- **T-25-04 (unauthenticated access, mitigated):** the subtree inherits RequireAuth from the wrapped parent mux; asserted (401 with no cookie, reaches handler with a valid session).
- **T-25-05 (integrations shadow, mitigated):** only the specific `/api/conversations/` subtree is registered (no bare `/api/`); the integrations-proxy-still-routes assertion runs AFTER the new mount.
- **T-25-SC (supply chain, mitigated):** zero new dependencies; govulncheck on the touched packages is clean.

## Verification Evidence
- `go test -tags db_integration ./internal/agui/ -run TestConversationsAPI -race` → ok, 3.45s (real DB round-trip, well above the sub-second skip-tell floor).
- `go test ./internal/agui/ -run 'TestTranslate|Reasoning'` → ok (cockpit + translator + Telegram reasoning gating).
- `go test ./cmd/aura/ -run ServeWebui` → ok (mount + no-shadow + gate-inherited).
- `go test -tags db_integration ./internal/agui/` full package with `-race -p 1` → ok, coverage 91.2%.
- `govulncheck ./internal/agui/...` → No vulnerabilities found.
- Source assertions: `uuid.Parse` count 2, `sanitizeErr` count 5 in conversations_api.go; cockpit call site passes `true`; Telegram blanket-`true` count 0.

### Integration tiers exercised
- **db_integration:** RUN LIVE on the up `aura-postgres` stack (DSNs derived from `POSTGRES_PASSWORD`). NOT faked.
- **neo4j_integration:** not applicable to this plan (no graph surface touched).
- **govulncheck:** run locally (Windows). The full `make quality` / mutation tiers are the operator's WSL/CI gate per CLAUDE.md.

## Self-Check: PASSED
