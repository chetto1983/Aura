---
phase: 13-channels-telegram-multimodal
plan: 06
subsystem: channels
tags: [telegram, telebot, hitl, ask_user, runner-resume, artifact, onboarding, commands, ag-ui]

# Dependency graph
requires:
  - phase: 13-01
    provides: telegram.Store (ConsumeOnboarding single-use consume-then-INSERT), telegram_accounts/telegram_setup_pending migrations
  - phase: 13-02
    provides: send_file tool + the channel-agnostic AG-UI CUSTOM artifact event (agui.ArtifactEventName)
  - phase: 13-04
    provides: telegram Config, channels framework
  - phase: 13-05
    provides: bot.go (NewChannel/Telegram, botSender seam, handleTurn per-turn fanout), renderer/status_pane consumers
provides:
  - "commands.go — 10 bot-intercept slash-commands; /cost==CLI (llm.CostUSD), /search==CLI (SearchConversationTurns), /cancel ctx-cancel"
  - "hitl.go — ask_user pause → InlineKeyboard/ForceReply → runner.SubmitAnswer → runner.Turn(nil) resume; render-only over the Runner"
  - "artifact.go — AG-UI artifact CUSTOM event consumer → tele.Document sendDocument (ASCII-safe caption)"
  - "onboarding.go — /start <token> → Store.ConsumeOnboarding → INSERT telegram_accounts; single-use rejection of replayed/expired tokens"
  - "agui.ArtifactEventName — exported canonical custom-event name for channel consumers"
affects: [13-07 (serve.go composition root wires commands/hitl/artifact/onboarding into the bot), 13-09 (live bot Gate-3)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Consumer-side seams (searchBackend/costBackend/resumeRunner/onboardingStore) keep the channel unit-testable with no live Runner/DB/network"
    - "Bot-intercept-before-LLM dispatch (handled=true short-circuits handleTurn — a command can never drive an agent turn)"
    - "Render-only HITL over the Runner: the channel reads PendingFor + resolves SubmitAnswer, NEVER writes paused_states (Runner is sole writer)"
    - "Cross-slice invariant via verbatim backend reuse (llm.CostUSD, SearchConversationTurns) + a verbatim port of the CLI excerpt window"

key-files:
  created:
    - internal/channels/telegram/commands.go
    - internal/channels/telegram/commands_test.go
    - internal/channels/telegram/hitl.go
    - internal/channels/telegram/hitl_test.go
    - internal/channels/telegram/artifact.go
    - internal/channels/telegram/artifact_test.go
    - internal/channels/telegram/onboarding.go
    - internal/channels/telegram/onboarding_test.go
  modified:
    - internal/agui/translator.go

key-decisions:
  - "Exported agui.ArtifactEventName (with an internal alias) so the Telegram consumer keys on the SAME canonical custom-event name rather than a duplicated magic string (Rule 3 blocking)"
  - "choice pauses get a trailing Annulla (cancel) safety button so the user is never stuck — option buttons carry accept, the safety button carries cancel"
  - "/cost reads a TodayUsage seam (the composition root wires the cachemetrics/conversations aggregation) and renders via llm.CostUSD so Telegram == CLI byte-for-byte"
  - "searchExcerpt/clampExcerpt are a verbatim port of the CLI cmd/aura/chat_repl.go excerpt window (cross-slice invariant; cmd/aura is package main, not importable)"

patterns-established:
  - "Per-chat in-flight-turn cancel registry (register on turn start, unregister on end) backs /cancel ctx-cancel propagation (SC#3)"
  - "Callback payload is token|action|value (<64 byte ceiling); parseCallback validates token+action present, value optional"

requirements-completed: [UX-02, UX-03]

# Metrics
duration: ~35min
completed: 2026-06-08
---

# Phase 13 Plan 06: Telegram Interaction Surfaces Summary

**The Telegram command/HITL/artifact/onboarding surfaces: 10 bot-intercept commands (/cost==CLI, /search==CLI, /cancel ctx-cancel), an ask_user pause rendered as InlineKeyboard/ForceReply and resumed through the Runner, the artifact CUSTOM event → sendDocument consumer, and the /start single-use onboarding write — all render-only over the locked backends.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-06-08T11:15:00Z
- **Completed:** 2026-06-08T11:50:00Z
- **Tasks:** 3 (all TDD)
- **Files modified:** 9 (8 created + 1 modified)

## Accomplishments
- `commands.go`: 10 PRD commands intercepted BEFORE any LLM dispatch (T-13-06-CmdLLMBypass); `/cost` reuses `llm.CostUSD` over a `TodayUsage` seam and `/search` reuses `conversations.SearchConversationTurns` byte-for-byte (cross-slice invariant: Telegram == CLI, proven by tests asserting the SAME USD string + the SAME CLI excerpt window); `/cancel` fires the per-chat in-flight turn ctx-cancel (SC#3).
- `hitl.go`: an ask_user pause renders an `InlineKeyboard` (choice/approval) or a `ForceReply` (clarification); a callback / reply answer feeds `runner.SubmitAnswer` (three-action accept/decline/cancel) then drives `runner.Turn(convID,nil)` ONLY when remaining==0 and the action was not a cancel — the channel NEVER writes `paused_states` (T-13-06-PauseHijack; the Runner stays the sole writer).
- `artifact.go`: the channel-agnostic AG-UI CUSTOM artifact event (plan 13-02) renders to a `tele.Document{File: FromDisk(path), FileName, Caption}` sendDocument; the assertion is on the Send RESPONSE (`msg.Document.FileName`), never getUpdates; the caption is ASCII-sanitized defensively (Pitfall 4 / T-13-06-CaptionInject).
- `onboarding.go`: `/start <token>` parses the deep-link payload and calls the atomic `Store.ConsumeOnboarding`; a consumed/expired/unknown token writes NO account and replies with an invalid-link message (T-13-06-TokenReplay), a duplicate account is surfaced, success greets.

## Task Commits

Each task was committed atomically:

1. **Task 1: commands.go (10 bot-intercept; /cost==CLI, /search==CLI, /cancel)** - `2731729c` (feat)
2. **Task 2: hitl.go (pause → InlineKeyboard/ForceReply → Runner resume)** - `553d62c2` (feat)
3. **Task 3: artifact.go (event → sendDocument) + onboarding.go (/start token → INSERT account)** - `325c6b50` (feat)

_Note: each TDD task landed its tests + implementation in one commit (the test files are co-authored with the implementation; RED→GREEN was iterated locally per file before the atomic commit)._

## Files Created/Modified
- `internal/channels/telegram/commands.go` - 10-command dispatcher; reuses llm.CostUSD + SearchConversationTurns; per-chat cancel registry for /cancel
- `internal/channels/telegram/commands_test.go` - no-LLM intercept, /cost==CLI, /search==CLI, /cancel ctx-cancel
- `internal/channels/telegram/hitl.go` - render-only HITL over runner.PendingFor/SubmitAnswer; InlineKeyboard/ForceReply; resume on remaining==0
- `internal/channels/telegram/hitl_test.go` - options→InlineKeyboard, none→ForceReply, callback→resume, decline/cancel/error paths, no paused_states write
- `internal/channels/telegram/artifact.go` - artifact CUSTOM event → sendDocument; ASCII-safe caption; eventConsumer seam
- `internal/channels/telegram/artifact_test.go` - sendDocument response assertion, ASCII sanitization, non-artifact ignore, channel drain
- `internal/channels/telegram/onboarding.go` - /start <token> → ConsumeOnboarding → INSERT account; replay/expired/unknown rejection
- `internal/channels/telegram/onboarding_test.go` - valid onboard, replay rejected (no 2nd insert), unknown rejected, bare /start greeting, deep-link parse
- `internal/agui/translator.go` - exported ArtifactEventName (internal alias keeps the body + golden tests byte-identical)

## Decisions Made
- Exported `agui.ArtifactEventName` so the channel consumer keys on the canonical name (no duplicated magic string). The unexported `artifactEventName` alias keeps the translator body and its golden tests byte-identical.
- `/cost` aggregation lives behind a `costBackend.TodayUsage` seam (wired by the 13-07 composition root over the cachemetrics/conversations cost data); this file only formats via `llm.CostUSD` so the render is CLI-identical.
- `searchExcerpt`/`clampExcerpt` are a verbatim port of the CLI `excerpt`/`clampExcerpt` (`cmd/aura/chat_repl.go` is package `main`, not importable), preserving the byte-for-byte cross-slice invariant.
- choice pauses carry a trailing `Annulla` (cancel) safety button so the user can always escape a pause.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Exported agui.ArtifactEventName**
- **Found during:** Task 3 (artifact.go consumer)
- **Issue:** The artifact consumer must key on the SAME custom-event name the translator emits, but `artifactEventName` was unexported in `internal/agui` — duplicating the literal "aura.artifact" string in the channel would silently drift if the substrate renamed it.
- **Fix:** Exported `agui.ArtifactEventName` and kept `artifactEventName` as an internal alias so the translator body and its golden tests are byte-identical.
- **Files modified:** internal/agui/translator.go
- **Verification:** `go test ./internal/agui/` full suite green (golden shapes unchanged); artifact.go consumes by the exported name.
- **Committed in:** 325c6b50 (Task 3 commit)

**2. [Rule 1 - Lint] Reworded Italian prose flagged by the English misspell linter**
- **Found during:** Task 1 (commands.go)
- **Issue:** golangci-lint `misspell` (English dictionary, no Italian locale) flagged "Comando"→"Commando" and "al momento"→"al memento" in user-facing Italian strings.
- **Fix:** Reworded to "Istruzione non riconosciuta" and "per ora" (semantically identical, lint-clean) — the project floor is lint=0.
- **Files modified:** internal/channels/telegram/commands.go
- **Verification:** `golangci-lint run ./internal/channels/telegram/...` → 0 issues.
- **Committed in:** 2731729c (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 lint).
**Impact on plan:** Both necessary (consumer correctness + the lint=0 floor). No scope creep — the four files are exactly the plan's deliverables.

## Issues Encountered
- gofmt struct-alignment nits in the test files (whitespace) were caught by the package lint and fixed before each commit — no behavioral impact.

## User Setup Required
None - no external service configuration required (the bot token + setup wizard are wired by the 13-07 composition root; these files are channel-internal surfaces).

## Next Phase Readiness
- The four interaction surfaces are ready for the 13-07 composition root to wire into the bot: `commands.dispatch` is called before `handleTurn`; `hitl.prompt`/`handleCallback`/`handleTextReply` mount on the OnText/OnCallback handlers with the Runner's `PendingFor`/`SubmitAnswer` + the per-turn resume driver; `artifact` mounts as a third per-turn fanout consumer; `onboarding.handleStart` consumes the `/start` deep-link via the 13-01 `Store`.
- The db_integration round-trip for the onboarding /start path compiles under `-tags db_integration` (the live PG round-trip is delegated to the orchestrator's gate / plan 13-01 store test).
- A `costBackend.TodayUsage` implementation (cachemetrics/conversations daily aggregation) must be supplied by the 13-07 composition root for the live `/cost`.

## Self-Check: PASSED

---
*Phase: 13-channels-telegram-multimodal*
*Completed: 2026-06-08*
