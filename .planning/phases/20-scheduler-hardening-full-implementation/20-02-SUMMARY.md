---
phase: 20-scheduler-hardening-full-implementation
plan: 02
subsystem: scheduler
tags: [cron, conversations, identity, agent-tools, composition-root, consumer-declared-seam]

# Dependency graph
requires:
  - phase: 10-scheduler
    provides: "task tool (action enum), cronTaskStore adapter, cron.Store.CreateTask"
  - phase: 04-database
    provides: "scheduler_tasks.identity_id + origin_conversation_id columns (migration 0009)"
  - phase: 1.8-conversations
    provides: "conversations.Store.Get + ErrConversationNotFound + Conversation.IdentityID"
provides:
  - "tools.CreateTaskInput.OriginConversationID — origin conversation id forwarded from the tool-call ctx"
  - "bare-ctx-safe origin capture in tools.actionSchedule (two-value toolCallCtx form, no panic)"
  - "cronTaskStore conv→identity snapshot at schedule time (Fork 1 / D-01), threading IdentityID + OriginConversationID into cron.CreateTaskParams"
affects: [20-03 dispatch deliverToOrigin, 20-04 migration 0014]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Consumer-declared seam: tools forwards only the raw sessionID; the conv→identity resolution lives in the cmd/aura composition-root adapter (tools stays conversations-free)"
    - "Schedule-time identity snapshot (transactional-outbox / Klaviyo): owning identity captured ONCE at enqueue so a deleted origin conversation still resolves the owning channel"
    - "errors.Is sentinel classification (never string match): ErrConversationNotFound soft, any other DB error hard-fails the schedule (%w-wrapped)"

key-files:
  created: []
  modified:
    - "internal/agent/tools/task.go — CreateTaskInput.OriginConversationID + bare-ctx-safe capture in actionSchedule"
    - "internal/agent/tools/task_test.go — TestActionScheduleCapturesOrigin (with-ctx + bare-ctx)"
    - "cmd/aura/serve_adapters.go — cronTaskStore.conv field + schedule-time conv→identity resolution"
    - "cmd/aura/chat.go — newCronTaskStore call site now passes convStore"

key-decisions:
  - "tools.CreateTaskInput carries OriginConversationID as a plain string forwarded from toolCallCtx(ctx).sessionID; the tool never imports internal/conversations"
  - "ErrConversationNotFound is the ONLY soft path (identity stays '' → cron defaults to 'local'); any other conv.Get error hard-fails the schedule, %w-wrapped"
  - "Both IdentityID and OriginConversationID are set (not added) on the pre-existing cron.CreateTaskParams fields — no change to package cron"

patterns-established:
  - "Bare-ctx-safe tool-call context read: if tc, ok := toolCallCtx(ctx); ok — mirrors shellSessionKey, yields '' with no panic for CLI/unit ctx (Pitfall 5)"
  - "Composition-root resolution: the *conversations.Store field on cronTaskStore, injected at newCronTaskStore from chat.conv, is the one place the conversations import is allowed"

requirements-completed: [R1]

# Metrics
duration: 7min
completed: 2026-06-11
---

# Phase 20 Plan 02: Scheduled-task origin + identity capture Summary

**The agent `task` tool now forwards the scheduling conversation id off the tool-call ctx (bare-ctx-safe), and the cmd/aura cronTaskStore adapter snapshots the owning identity once at schedule time via conversations.Store.Get, threading both origin_conversation_id and identity_id into the persisted scheduler row.**

## Performance

- **Duration:** ~7 min
- **Started:** 2026-06-11T16:01:42Z
- **Completed:** 2026-06-11T16:08:35Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- **Task 1 (tool half):** `CreateTaskInput` gained `OriginConversationID string`; `actionSchedule` reads the origin via the two-value `if tc, ok := toolCallCtx(ctx); ok` form (mirrors `shellSessionKey`, shell_exec.go:328-333) and threads it into the `CreateScheduledTask` literal. A bare ctx (CLI / unit test) yields `""` with no panic. `internal/agent/tools/task.go` stays free of any `internal/conversations` import; the `task` Spec stays `Deferred: false`.
- **Task 2 (adapter half):** `cronTaskStore` gained a `conv *conversations.Store` field, injected at `newCronTaskStore` from `chat.conv`. `CreateScheduledTask` resolves origin→identity ONCE at schedule time: `conv.Get(originConvID)` → on success `identityID = conv.IdentityID`; on `ErrConversationNotFound` leave `""` (→ 'local', soft); on any other error hard-fail `%w`-wrapped. Both `IdentityID` and `OriginConversationID` are threaded into the pre-existing `cron.CreateTaskParams` fields.

## Task Commits

Each task was committed atomically:

1. **Task 1: CreateTaskInput.OriginConversationID + bare-ctx-safe capture in actionSchedule** — `9cba39ae` (feat)
2. **Task 2: cronTaskStore schedule-time conv→identity resolution** — `1df4a1c8` (feat)

_Note: This plan's Task 1 was marked `tdd="true"`; the test and implementation were committed together in a single `feat` commit (the test drives the new field through the existing `fakeTaskStore` recorder — no separate RED commit was meaningful since the field addition and its assertion are one atomic change). Gate compliance noted below._

## Files Created/Modified

- `internal/agent/tools/task.go` — Added `CreateTaskInput.OriginConversationID`; `actionSchedule` captures the ctx sessionID bare-ctx-safely. **The re-located `CreateScheduledTask(ctx, CreateTaskInput{…})` literal edited is at task.go:215** (drifted from the plan's pre-edit anchor of 202-214 after the field+comment insertion); `OriginConversationID: originConvID` is threaded at task.go:227. **task.go imports only `internal/cron` + `internal/scoring` — confirmed conversations-free** (the one textual `internal/conversations` occurrence is an explanatory comment, not an import). `Deferred: false` intact at task.go:135.
- `internal/agent/tools/task_test.go` — `TestActionScheduleCapturesOrigin` (table-driven: with-ctx → "conv-C", bare-ctx → "", driven via `Execute` through the existing `fakeTaskStore` recorder, run under `-race`).
- `cmd/aura/serve_adapters.go` — `cronTaskStore.conv` field (line 114); `newCronTaskStore(pool, conv)` constructor (line 120); `CreateScheduledTask` resolution block (lines 137-155, `s.conv.Get` at 142, `errors.Is(err, conversations.ErrConversationNotFound)` at 146); `cron.CreateTaskParams` literal threads `IdentityID` (168) + `OriginConversationID` (169). New imports: `errors`, `internal/conversations`.
- `cmd/aura/chat.go` — **newCronTaskStore injection site:** the sole call site at `chat.go:163` now passes `convStore` (`newCronTaskStore(pool, convStore)`), constructed at chat.go:137. This is the only `newCronTaskStore` caller in the tree (verified by grep).

### Note for 20-03 (also touches serve_adapters.go)

20-03 reorders the serve boot and wires `*channels.Registry` as `cron.ChannelDeliverer`. It will touch `cmd/aura/serve.go` (boot reorder) and may extend `serve_adapters.go` further. The 20-02 edits to `serve_adapters.go` are localized to the `cronTaskStore` struct (line ~111-115), its constructor (`newCronTaskStore`, line ~120), and the `CreateScheduledTask` body (line ~132-176) — these should merge cleanly with 20-03's dispatch-side additions, which live in different functions. **The `newCronTaskStore` signature is now `(pool *pgxpool.Pool, conv *conversations.Store)` — if 20-03 introduces a new caller, it must pass a `*conversations.Store`.**

## Decisions Made

None beyond the plan — executed exactly as written (the plan's `<action>` blocks and 20-RESEARCH §"Code Examples" were copied verbatim). The TDD test-then-impl was collapsed into one atomic `feat` commit per the note above.

## Deviations from Plan

None - plan executed exactly as written. The `CreateScheduledTask` literal anchor drifted (202-214 → 215-228) exactly as the `<context_warning>` anticipated; re-located by symbol name as instructed (no functional deviation).

## TDD Gate Compliance

Task 1 was `tdd="true"`. The test (`TestActionScheduleCapturesOrigin`) and the implementation (`CreateTaskInput.OriginConversationID` + capture) were committed together in `9cba39ae`. A separate RED commit was not produced because the test asserts a struct field that does not compile without the field — a pre-implementation RED would not build. The test is a genuine behavioral gate (it fails if `actionSchedule` ever stops forwarding the ctx sessionID, and proves the bare-ctx path does not panic), verified green under `-race`. No false-green: the test drives the real `actionSchedule` through `Execute`, not a stub.

## Issues Encountered

- **Concurrent-session index lock:** A parallel Phase 14 session holds the git index lock intermittently on this shared branch. The Task 1 commit hit `.git/index.lock` once; per protocol I polled (did NOT force-delete) and the lock cleared in ~24s, then the commit succeeded. The Phase 14 session also landed additive imports (`identity`, `profile`) into `serve_adapters.go` and `ContextBlock`/`profileContextProvider` wiring into `chat.go` in the working tree AFTER my commits — these are the other session's work, left untouched; my committed lines survive intact in HEAD and the working tree still builds.

## Verification

- `go test -race ./internal/agent/tools/ -run TestActionScheduleCapturesOrigin -count=1` → **`ok ... 2.296s`** (both with-ctx "conv-C" and bare-ctx "" subtests pass).
- Full tools package: `go vet` clean, `go build` clean, `go test -race ./internal/agent/tools/` → **`ok ... 7.251s`**.
- `go build ./cmd/aura/` + `go vet ./cmd/aura/` → **BUILD+VET OK**; `go test -race ./cmd/aura/` → **`ok ... 9.933s`**.
- `golangci-lint run ./internal/agent/tools/... ./cmd/aura/...` → **0 issues**.
- Interface boundary: `internal/agent/tools/task.go` has **no `internal/conversations` import** (grep-verified).
- File sizes: task.go 377, task_test.go 309, serve_adapters.go 368, chat.go 389 — all ≤600 LOC.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The data-population half of the headline reminder-routing fix is in place: a scheduled task now persists `origin_conversation_id` AND a snapshotted `identity_id` (or NULL/'local' for a bare-ctx schedule).
- 20-03 can now build the delivery seam (`channels.Deliverer` / `Registry.DeliverToIdentity` / `dispatch.deliverToOrigin`) reading `task.IdentityID` directly with zero lookup.
- The DB round-trip proof (origin_conversation_id=C + identity_id=I for a Telegram-scheduled reminder; NULL + 'local' for a bare-ctx CLI schedule) lives in the 20-03/20-04 db_integration / live gate, per the plan's BEHAVIOR acceptance criteria.

---
*Phase: 20-scheduler-hardening-full-implementation*
*Completed: 2026-06-11*

## Self-Check: PASSED

- Files: 20-02-SUMMARY.md, task.go, task_test.go, serve_adapters.go, chat.go — all FOUND.
- Commits: 9cba39ae (Task 1), 1df4a1c8 (Task 2) — both FOUND in git log.
