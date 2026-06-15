---
phase: 13-channels-telegram-multimodal
plan: 05
subsystem: channels
tags: [telegram, telebot, ag-ui, fanout, markdownv2, status-pane, goleak, synctest]

# Dependency graph
requires:
  - phase: 12-agui-gateway
    provides: "internal/agui Fanout (Subscribe-before-Run, drop-on-full cap-64) + Translate (pure *agent.Event → events.Event mapper) + IDGenerator"
  - phase: 13-channels-telegram-multimodal (13-04)
    provides: "channels.Channel interface (Name/Start/Stop/IsHealthy, subscriber-free Start) + telegram.Config/LoadConfig throttles"
  - phase: 13-channels-telegram-multimodal (13-03)
    provides: "mdv2.go EscapeMarkdownV2 + PlainTextFallback; tables.go ParseMarkdownTable/RenderTablePNG; llm/models.go vision caps"
  - phase: 13-channels-telegram-multimodal (13-01)
    provides: "telegram.Store (onboarding/accounts) + goleak TestMain harnesses"
provides:
  - "telegram.Telegram: telebot.v4 wrapper implementing channels.Channel (polling Start/Stop, goleak-clean)"
  - "handleTurn: per-turn AG-UI fanout consumer (Translate → NewFanout → Subscribe×2 → Run → dispatch)"
  - "status_pane.go: status-pane-B (msg #1 edited in place: tool list 🟡→✅/❌, 💭 reasoning, running-cost footer, coalescing throttle)"
  - "renderer.go: msg #2 streamed content, mdv2-escaped with plain-text fallback on 400, tables→PNG sendPhoto, per-chat rate limit, 4096 cap"
affects: [13-06 (HITL over pause), 13-07 (setup wizard + serve.go mount), 13-08 (send_file artifact), 13-09 (live bot Gate-3)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "turnDriver seam: consumer-side func type (runner.Runner.Turn satisfies it) so the channel drives a synthetic event stream in tests without a live Runner/DB"
    - "botSender seam: narrow Send/Edit interface (*tele.Bot satisfies it) so render consumers assert on the Send RESPONSE, never getUpdates"
    - "stopWaitPoller: zero-network telebot Poller injected in Offline mode so Start/Stop is exercised end-to-end goleak-clean"
    - "Per-turn Fanout (never one at channel start): Subscribe×2-before-Run, one Fanout per turn (honors fanout.go:51/:67 panic guards)"
    - "structured 400 detection: *tele.Error code+description (errors.As), never a bare string match on an arbitrary error"
    - "golden-per-event-family + testing/synctest deterministic throttle"

key-files:
  created:
    - internal/channels/telegram/bot.go
    - internal/channels/telegram/agui_subscriber.go
    - internal/channels/telegram/bot_test.go
    - internal/channels/telegram/renderer_test.go
    - internal/channels/telegram/status_pane_test.go
    - internal/channels/telegram/testdata/statuspane_*.golden (8 fixtures)
  modified:
    - internal/channels/telegram/renderer.go
    - internal/channels/telegram/status_pane.go

key-decisions:
  - "Channel constructor is NewChannel (not New) — Store.New already owns the package's New for the DB seam"
  - "Offline mode injects a stopWaitPoller (zero network) + DisableKeepAlives HTTP client so Start/Stop is goleak-clean without hitting the live API"
  - "convID = deterministic UUIDv5(chatID) under a fixed telegram namespace — one stable conversation per chat across restarts"
  - "Plain-text fallback latches per message (plain flag): once a 400 forced it, subsequent edits stay plain (content only grows; re-escaping would 400 again)"
  - "Reasoning accumulated RAW, collapsed only at display time so word boundaries between deltas survive"
  - "Microcompact read_tool_output pointer in a tool result resolves ✅ (successful, just-truncated), never ❌"

patterns-established:
  - "turnDriver/botSender/consumerFactory injection seams keep the channel unit-testable with no live Runner, DB, or network"
  - "stopWaitPoller pattern for goleak-clean Offline telebot lifecycle tests"

requirements-completed: [UX-02]

# Metrics
duration: ~30min
completed: 2026-06-08
---

# Phase 13 Plan 05: Telegram Channel Core + status-pane-B Render Summary

**The SC#2 surface: a telebot.v4 channel implementing channels.Channel with a goleak-clean polling lifecycle, a per-turn AG-UI Fanout consumer (Subscribe×2-before-Run, the Phase-12 seam, never re-implemented), a status-pane-B msg#1 (tool list 🟡→✅/❌, 💭 reasoning, cost footer) and a streamed msg#2 with mdv2 escaping + a plain-text fallback on a 400 can't-parse-entities + tables→PNG sendPhoto.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-06-08T08:47Z
- **Completed:** 2026-06-08T09:07Z
- **Tasks:** 2 (both TDD)
- **Files modified:** 15 (4 source + 3 test + 8 golden)

## Accomplishments

- `bot.go` — `Telegram` telebot.v4 wrapper implementing `channels.Channel`: `Start` constructs the bot (Offline-aware), registers the `OnText` handler, launches the polling goroutine tracked by a WaitGroup; `Stop` calls `Bot.Stop` and joins it goleak-clean. A compile-time `_ channels.Channel = (*Telegram)(nil)` assertion locks the contract.
- `agui_subscriber.go` — `handleTurn` does the exact per-turn wiring (research §2): `Translate(convID,runID,idgen,Turn(...))` → `NewFanout` → `Subscribe()` (status) → `Subscribe()` (content) BEFORE `Run` → dispatch. Consumes `internal/agui/fanout.go` — the event distribution is NOT re-implemented (the phase's biggest risk, avoided).
- `status_pane.go` — status-pane-B: msg #1 edited in place from the AG-UI event families (RUN_STARTED / TOOL_CALL_* / REASONING_* / STATE_DELTA / RUN_ERROR / RUN_FINISHED), coalesced to the 1500ms status throttle.
- `renderer.go` — msg #2 streamed content: mdv2-escaped MarkdownV2 sends with the SC#2 plain-text fallback (resend WITHOUT ParseMode on a Bot-API 400 "can't parse entities"), markdown tables → gridded PNG `sendPhoto`, per-chat rate limit, 4096/1024 caps.
- Tests prove: per-turn Subscribe×2-before-Run distribution over a real Translate→Fanout drive of a synthetic event stream; goleak-clean Start/Stop; the 400→plain-text fallback (asserted on the Send RESPONSE); the table→sendPhoto branch; a golden per event family incl. the microcompact pointer; the 1500ms throttle coalescing via `testing/synctest`.

## Task Commits

1. **Task 1: bot.go (Channel impl) + agui_subscriber.go (per-turn fanout)** — `cdb7c0a1` (feat)
2. **Task 2: status_pane.go + renderer.go (status-pane-B + mdv2 fallback + tables PNG)** — `4ea095f1` (feat)

_Both tasks are `tdd="true"`; the TDD cycle (failing test → impl → verify) was run within each task commit. Task 1 ships compiling drain skeletons for status_pane/renderer (the production consumers handleTurn binds); Task 2 replaces them with the full render bodies + their tests + goldens._

## Files Created/Modified

- `internal/channels/telegram/bot.go` (created, 249 LOC) — telebot wrapper, Channel impl, stopWaitPoller, throttle accessors, text handler.
- `internal/channels/telegram/agui_subscriber.go` (created, 100 LOC) — `handleTurn` per-turn fanout wiring + `convID` chat→conversation mapping.
- `internal/channels/telegram/renderer.go` (modified, 248 LOC) — content consumer: mdv2 + plain-text fallback + tables→PNG + rate limit + 4096 cap.
- `internal/channels/telegram/status_pane.go` (modified, 223 LOC) — status-pane-B consumer: tool list / reasoning / cost / coalescing throttle.
- `internal/channels/telegram/bot_test.go` (created) — fanout distribution + goleak Start/Stop + Name.
- `internal/channels/telegram/renderer_test.go` (created) — fakeBot double + stream/finalize + 400 fallback + table→photo + 4096 cap.
- `internal/channels/telegram/status_pane_test.go` (created) — golden-per-event + microcompact-pointer + synctest throttle.
- `internal/channels/telegram/testdata/statuspane_*.golden` (8 created) — one render golden per event family.

## Decisions Made

- **Constructor named `NewChannel`** — `Store.New(pool)` (13-01) already owns the package's `New`; the channel is the higher-level type that holds a `*Store`.
- **Offline lifecycle via `stopWaitPoller` + `DisableKeepAlives`** — telebot's `Settings.Offline` only skips the construction-time `getMe`; `Bot.Start` still drives the default LongPoller against the live API. A zero-network poller (blocks on stop, returns cleanly) makes the Start/Stop test exercise the full lifecycle with no network and no leaked HTTP/2 conn.
- **Plain-text fallback latches per message** — once a 400 forced plain mode, subsequent streamed edits stay plain (the content only grows; re-escaping would 400 again). Cleaner UX and avoids a thrash of failed MarkdownV2 edits.
- **Reasoning accumulated raw, collapsed at display** — fixed a real bug where collapsing per-delta merged word boundaries ("Sto cercando" + "i dati" → "Sto cercandoi dati").

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Reasoning delta word-boundary merge**
- **Found during:** Task 2 (status_pane render)
- **Issue:** Collapsing whitespace on each `p.thinking + delta` append stripped the trailing space of one delta before the next concatenated, merging adjacent words on the 💭 line.
- **Fix:** Accumulate the raw reasoning string; collapse whitespace only at display time in `text()`.
- **Files modified:** internal/channels/telegram/status_pane.go
- **Verification:** the `reasoning` golden now renders "Sto cercando i dati meteo." (space preserved).
- **Committed in:** `4ea095f1` (Task 2 commit)

**2. [Rule 2 - Missing Critical] goleak-clean Offline polling lifecycle**
- **Found during:** Task 1 (Start/Stop test)
- **Issue:** With `Offline: true` the bot still launched the live LongPoller, leaving a kept-alive HTTP/2 connection goroutine that the package goleak TestMain flagged.
- **Fix:** Inject a `stopWaitPoller` (zero network I/O) in Offline mode and set `DisableKeepAlives` on the poller's HTTP client (the openai_compat goleak discipline).
- **Files modified:** internal/channels/telegram/bot.go
- **Verification:** `go test ./internal/channels/telegram/` (and `-race`) goleak-clean.
- **Committed in:** `cdb7c0a1` (Task 1 commit)

**3. [Rule 3 - Blocking] Constructor name collision with Store.New**
- **Found during:** Task 1 (first build)
- **Issue:** `func New(d Deps) *Telegram` collided with the existing `func New(pool *pgxpool.Pool) *Store` in store.go (same package).
- **Fix:** Renamed the channel constructor to `NewChannel`.
- **Files modified:** internal/channels/telegram/bot.go, bot_test.go
- **Verification:** package builds clean.
- **Committed in:** `cdb7c0a1` (Task 1 commit)

---

**Total deviations:** 3 auto-fixed (1 bug, 1 missing critical, 1 blocking)
**Impact on plan:** All three were necessary for correctness (T-13-05-MdV2Send/PollLeak threat mitigations + a render bug). No scope creep — the surface delivered is exactly the plan's two-task scope.

## Issues Encountered

- telebot `Settings.Offline` semantics (skips getMe but still polls) — resolved with the `stopWaitPoller` (see deviation 2).
- `itoa` name collision with an existing `tables_test.go` helper — switched the chat-id formatter to `strconv.FormatInt`.
- One staticcheck QF1008 (embedded-field selector `photo.File.FileReader`) — simplified to `photo.FileReader`.

## Threat Flags

None — no new security surface beyond the plan's threat model. The plan's STRIDE register (T-13-05-MdV2Send, T-13-05-PollLeak, T-13-05-FanoutMisuse, T-13-05-FloodSend) is fully mitigated: mdv2 escape + plain-text fallback (renderer), Stop-joins-poller + goleak (bot), Subscribe×2-before-Run + one-Fanout-per-turn (subscriber, proven by the synthetic-stream test), per-chat rate limit + content throttle (renderer).

## Known Stubs

None that block the plan's goal. The channel's composition-root wiring (serve.go mount, conversation-row creation per chat, onboarding gate) is explicitly plan 13-07's scope; `handleTurn` is self-contained and unit-proven here. HITL handoff on `RUN_FINISHED(interrupt)` is plan 13-06; the renderer/status-pane finalize on success and the interrupt outcome flows through Translate untouched.

## Next Phase Readiness

- The Telegram channel core is complete and unit/-race/goleak green; ready for 13-06 (HITL), 13-07 (setup wizard + serve.go registry mount), 13-08 (send_file artifact → sendDocument).
- The live bot Gate-3 (real `TELEGRAM_BOT_TOKEN`, on-device render verification) is plan 13-09 — no live Telegram exercised here by design.
- No blockers.

## Self-Check: PASSED

- All 8 source/test files + 8 status-pane goldens + SUMMARY.md verified on disk.
- Both task commits verified in git log: `cdb7c0a1` (Task 1), `4ea095f1` (Task 2).
- `go vet` + `go build ./...` + `go test ./internal/channels/telegram/` + `go test -race` green; `golangci-lint run ./internal/channels/telegram/...` 0 issues; all files ≤600 LOC.

---
*Phase: 13-channels-telegram-multimodal*
*Completed: 2026-06-08*
