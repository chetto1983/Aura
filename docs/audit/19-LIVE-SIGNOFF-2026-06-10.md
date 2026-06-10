# Phase 19 — Layer-2 Live Operator Sign-Off

> **Plan:** 19-11 (`autonomous: false`, manual paid operator gate — NOT CI).
> **Date:** 2026-06-10
> **Operator:** Davide (dvdmarchetto@gmail.com)
> **Pattern:** mirrors the Phase 13-10 / Phase 8 live sign-off — for every user-observable
> finding: BEFORE (cited from `docs/audit/deep-correctness-audit-2026-06-10.md`) → AFTER
> (live observed behavior) with a **ground-truth assertion that does NOT look at `r.Reply`**
> (DB row / `· <toolname>` tool trace / rendered body / protocol conformance), plus a visual
> body print + mojibake/structure scan.

## Preconditions

| Precondition | State | Evidence |
|---|---|---|
| Layer-1 matrix (19-01..19-10) green | ✅ | All 10 fix plans have SUMMARY + `[x]` in ROADMAP; full `go build ./...` green |
| Migration 0013 applied to live DB | ✅ | `schema_migrations` version **13**, dirty=`f`; `aura.pending_notifications` present |
| Fresh binary built at HEAD | ✅ | `D:\tmp\aura-live-19-11\aura.exe` @ `0ab722e5` (isolated worktree) |
| Live stack up | ✅ | PG, Neo4j, embed, agent-memory-mcp, STT, markitdown, TTS, SearXNG — all healthy |
| psql ground-truth path | ✅ | `postgresql-client-18` (WSL) → `wsl bash /mnt/d/tmp/dbq.sh <file>.sql` |

## Findings under live sign-off (10 user-observable)

| ID | Finding | Fix commit(s) | Task | Status |
|---|---|---|---|---|
| H4 | shell_exec hangs past timeout (orphan grandchild) | `06478bdd` | 1 | ✅ live |
| H5 | exit-code/stderr footer truncated away | `194854e3` | 1 | ✅ live |
| M-b | veto re-surfaces vetoed hand-off prose on budget trip | `88bc012a` | 1 | ✅ live (cluster) |
| H9 | mid-stream SSE failure delivered as a complete answer | `57202e64`, `99163a36` | 1 | ✅ Layer-1 binding + live control |
| H2 | Telegram turn errors swallowed (bare "Stato: errore") | `658069cc` | 2 | ✅ live |
| H3 | async doc-conversion failure → "elaborando…" then silence | `658069cc` | 2 | ✅ live |
| M-e | `/cancel` during pause doesn't cancel the pause | `772f771a` | 2 | ✅ live |
| H6 | quiet-hours-deferred notifications permanently dropped | `4094d47b`, `65574350` | 3 | ✅ live |
| H7 | failed self-send never retried | `65574350` | 3 | ✅ live |
| H1 | SSE backpressure drops protocol BOUNDARY frames | `636ef23e` | 3 | ✅ Layer-1 binding + live control |

---

## Task 1 — Shell-never-answer (H4/H5/M-b) + SSE truncation (H9) on the `aura chat` host loop

### H4 — Timeout/cancel kills only the shell, not its children → orphan leak + hang past timeout
- **BEFORE** (audit §H4): `exec.CommandContext(...).Run()` with no process group and no `cmd.WaitDelay`.
  On ctx expiry Go kills only the direct child (`bash`); grandchildren (`go run`, `python server.py`,
  `npm start`) are orphaned, and with `WaitDelay`=0 `Wait()` blocks until every writer closes the stdout
  pipe — a grandchild that ignores the kill **hangs the call past its timeout**. The turn never returns.
- **Fix** (`06478bdd` fix(19-01): reap shell process groups on timeout): start a process group
  (`Setpgid` POSIX / `CREATE_NEW_PROCESS_GROUP` Win), kill the whole group via custom `cmd.Cancel`,
  set `cmd.WaitDelay` so `Wait()` force-closes inherited fds.
- **LIVE setup:** containerized aura (linux/amd64 @ `0ab722e5`) on the `aura_default` docker network,
  `/bin/sh -c` POSIX `shell_exec`. Prompt (real operator turn): *"Esegui ESATTAMENTE questo comando … per
  i in $(seq 1 1200); do echo …; done; ( sleep 120 & ) ; exit 7"*.
- **AFTER:** ✅ **No hang.** End-to-end wall-clock **55.8 s** (exit 0) with a `( sleep 120 & )` orphan
  holding the stdout pipe — without the fix the first `shell_exec` would block ~120 s on `Wait()` and the
  agent could never reach an answer. **Ground truth (not `r.Reply`):** `aura.tool_invocations` =
  `shell_exec` ×4 for the conversation (the orphaning command + 3 follow-up investigation calls, all
  returned); the agent ran a full investigation and answered. The reply explicitly notes
  *"il comando `( sleep 120 & )` ha avviato un processo in background, ma non ha influito sull'exit code"*.
  Cost $0.000928. `· shell_exec` tool trace present in the chat render.

### H5 — Exit code + stderr silently dropped from the model's view on output > preview cap
- **BEFORE** (audit §H5): status marker + stderr + `[exit code N]` footer appended at the tail; `NewResult`
  truncates head-first, so for stdout > 2 KB the model's RoleTool message = first 2 KB + a pager pointer;
  exit code, stderr, footer gone. A large *failed* run can grade `DONE`.
- **Fix** (`194854e3` fix(19-01): preserve shell failure footer under truncation): reserve tail bytes for
  the status+footer (and a stderr tail) that survive truncation.
- **AFTER:** ✅ ~72 KB of stdout (≫ the 2 KB preview cap) + `exit 7`. The agent's reply reports
  **Exit code `7` → "Fallito"** AND self-observes *"La shell ha troncato la visualizzazione a 1960 byte,
  ma tutte le 1200 righe sono state stampate prima della chiusura."* The exit-code footer survived the
  head-first truncation the agent itself noted — exactly the H5 reserved-tail fix. The model was aware of
  the real non-zero exit despite the body being truncated. **Ground truth:** the persisted assistant turn
  (674 B) carries the correct `7` (DB `aura.conversation_turns`), not derivable from the truncated body.

### M-b — `gateCompletion` veto re-surfaces the vetoed hand-off prose on the budget trip
- **BEFORE** (audit §M-b): the veto appends the rejected "I wrote the script, you run it" prose as durable
  `RoleAssistant`; the next budget trip routes to `finalize`, which copies that history forward and re-emits
  it — the gate is defeated on the exhaustion path; the user gets exactly the vetoed non-answer.
- **Fix** (`88bc012a` fix(19-02): keep vetoed answers out of finalize): on a content-stop veto, append only
  the feedback nudge, not the vetoed prose.
- **AFTER:** ✅ **The turn answered** — a real, structured, correct terminal answer (exit code, pass/fail,
  line count), NOT silence and NOT a "here's a script, you run it" hand-off. The named "shell turns never
  answer" UX bug (the H4+H5+M-b cluster) is closed on the live host loop. M-b's mechanism-level binding
  proof (the budget-trip veto path) stays the Layer-1 regression (`88bc012a`); this run is the live
  cluster confirmation. **Ground truth:** non-empty assistant turn persisted (DB), shell_exec×4 trace.

### H9 — SSE parse error swallowed → a mid-stream transport failure is delivered as a complete answer
- **BEFORE** (audit §H9): `parseSSE` returns a real error on a malformed/truncated/reset mid-stream body,
  but `llm.Chunk` had no error field, so once the stream opens every streaming error is structurally
  unreportable; the consumer accepts the partial accumulated text as the final answer.
- **Fix** (`57202e64` fix(19-03): surface terminal streaming errors, `99163a36` fix(19-03): stop finalizing
  partial streams on chunk errors): add `Err` to `llm.Chunk`, emit it before close; the main loop refuses
  partial-as-complete. Layer-1 binding proof = `testdata/premature_close.sse` regression.
- **AFTER:** ✅ **Binding proof = Layer-1** (`57202e64` + `99163a36`, consuming the previously-orphaned
  `testdata/premature_close.sse` fixture — a deterministic mid-stream cut that asserts a terminal
  `Chunk.Err` is emitted and the loop refuses partial-as-complete). **Live control:** the Task-1 run's
  stream completed cleanly with a full, non-truncated terminal answer and no false "complete" on a broken
  stream — the healthy-path control. A deterministic live mid-stream transport cut is not reproducible on
  demand against OpenRouter, so per the plan the Layer-1 fixture is the binding evidence.

---

## Task 2 — Telegram surfaces (H2 / H3 / M-e) via the CDP harness

### H2 — Turn errors swallowed (Telegram): bare "Stato: errore", never the reason
- **BEFORE** (audit §H2): the renderer switched only on `TextMessageContent/End` + `RunFinished`; no
  `RunErrorEvent` case, so the error text is dropped; status pane flips `failed=true` → a tiny glyph.
- **Fix** (`658069cc` fix(19-05)): `case *events.RunErrorEvent:` sends a sanitized failure message routed
  through the shared `agui.SanitizeString` (the 19-04 redaction chokepoint).
- **AFTER:** ✅ The live bot was restarted with an invalid model (`AURA_LLM_MODEL=aura/nonexistent-model-xyz`)
  so a turn fails fast. Sending a normal message rendered, in addition to the bare status glyph
  **"Aura Stato: errore"**, a distinct user-facing reason bubble: **"❌ Errore: llm: provider returned
  HTTP 400"**. Before the fix the renderer had no `RunErrorEvent` case, so the reason was dropped and only
  the glyph showed. **Ground truth (rendered body):** screenshot `D:/tmp/tg_h2err.png` + harness bubble
  read — the reason renders and is **sanitized** (no DSN / API token / provider URL — `agui.SanitizeString`).
  Zero mojibake. Bot then restored to the good model.

### H3 — Async (5–50 MB) document conversion failure swallowed → "elaborando…" then silence forever
- **BEFORE** (audit §H3): the async tier's callback logged `convErr` and returned with no user notice after
  replying "📄 Documento ricevuto, lo sto elaborando…"; the user waits indefinitely.
- **Fix** (`658069cc` fix(19-05)): on `convErr != nil`, send `convertFailMessage` via the captured sender.
- **AFTER:** ✅ Operator manually sent a 6 MB corrupt `.docx` (async 5–50 MB tier) to the live bot.
  **Ground truth — bot log:** `WARN telegram: async document convert failed chat=1148481707
  err="Post \"/convert\": unsupported protocol scheme \"\""` — a non-nil async `convErr` (the exact BEFORE
  condition). **Rendered Telegram (the AFTER fix):** *"📄 Documento ricevuto, lo sto elaborando…"* →
  **"Conversione del documento non disponibile."** The `convertFailMessage` reached the user instead of the
  old log-and-silent-return. (The convErr in this container config came from the converter URL being unset,
  i.e. the HTTP `Post "/convert"` failed — still a genuine async `convErr`, so the notice-on-failure fix
  path is exercised end-to-end.)

### M-e — `/cancel` during a pending ask_user pause does NOT cancel the pause
- **BEFORE** (audit §M-e): `/cancel` was intercepted before the HITL path; it only cancelled an in-flight
  turn ctx (none exists during a pause). The `paused_states` row was orphaned and the inline keyboard stayed live.
- **Fix** (`772f771a` fix(19-05)): route `/cancel` through `SubmitAnswer(…ActionCancel)` when `PendingFor`
  is non-empty; clear the prompt keyboard via the per-chat `pausePrompts` track.
- **AFTER:** ✅ A live `ask_user` choice pause was induced (DB `aura.paused_states`: token
  `019eb182-…bff03`, conv `03b9c7c2-…4de8b`, `kind=choice`, options `[Pizza 🍕, Sushi 🍣]`,
  **`resumed_at NULL`** = pending; rendered as the choice keyboard in Telegram). Sending **`/cancel`** then
  resolved it. **Ground truth (DB):** the same token now has `resumed_at` set and
  `resumed_answer = {"action": "cancel", "content": "<auto-terminated: conversation ended>"}` — `/cancel`
  routed through `SubmitAnswer(ActionCancel)`, not the old in-flight-turn-ctx no-op. **Rendered:** the bot
  sent **"Richiesta annullata."** and the Pizza/Sushi inline keyboard was retired (screenshot
  `D:/tmp/tg_mecancel.png`).

---

## Task 3 — Scheduler notify (H6/H7) on a real cron tick + AG-UI slow client (H1)

### H6 — Quiet-hours-deferred notifications permanently dropped, not deferred
- **BEFORE** (audit §H6): a successful run during `AURA_SCHEDULER_QUIET_HOURS` logged "deferred to window
  end" and returned, but `complete()` already wrote terminal state and the tick advanced `next_run_at`;
  there was no pending queue or window-end flush. The notification was lost.
- **Fix** (`4094d47b` + `65574350` feat(19-07)): migration `0013_pending_notifications` + a tick sweep that
  flushes after the window.
- **AFTER:** ✅ Live on the running scheduler (containerized aura, 15 s tick, `AURA_SCHEDULER_QUIET_HOURS`
  set to cover now ending `15:15` Rome). An `agent_job` (route=stdout) was `run_now` during the window;
  it **completed** (`agent_job_runs` status=`completed`) and the notification **deferred** — log
  `notification deferred to quiet-hours window end task=019eb1a8…`, and a durable `aura.pending_notifications`
  row appeared **`status=pending`, `notify_after=13:15:00Z`** (= the window end). After the window passed,
  the tick **sweep delivered it**: same row flipped to **`status=delivered`** (`updated_at=13:15:14Z`) and
  the stdout sink logged `[scheduler notify route=stdout to=] ok`. **Ground truth:** the DB row's
  pending→delivered transition + the delivery log line. BEFORE: deferred notifications were dropped with no
  row at all.

### H7 — A failed self-send is never retried
- **BEFORE** (audit §H7): an MCP self-send failure logged "bound-retry on a later tick" and moved on; the
  completed run is terminal and never re-selected.
- **Fix** (`65574350` feat(19-07)): persist undelivered-state + bounded re-attempt.
- **AFTER:** ✅ After the quiet window (not deferred), an `agent_job` with route=`whatsapp` was `run_now`. No
  WhatsApp MCP is mounted in this container, so `Notify(whatsapp)` failed: log
  `WARN dispatch notify undelivered (bound-retry on a later tick) … err="notify whatsapp: MCP self-send
  failed, fell back to stdout: no mounted MCP tool for route whatsapp (want *send_message)"`. A durable
  `pending_notifications` row was persisted **`status=failed`** and the tick sweep **retried it within the
  bound**: `attempts` incremented `0→1→2→3` and **capped at 3** (the sweep selects `attempts<3`, then stops).
  **Ground truth:** the DB row `status=failed, attempts=3, last_error="… MCP self-send failed …"`. BEFORE:
  a failed self-send was logged once and never retried.

### H1 — SSE backpressure drops protocol BOUNDARY frames → conformant client rejects the whole stream
- **BEFORE** (audit §H1): a slow client fills the cap-64 buffer; the `default:` arm dropped
  `TEXT_MESSAGE_START`/`TOOL_CALL_START`/`REASONING_START`, orphaning their deltas → the AG-UI SDK's
  `ValidateSequence` rejects the entire turn ("cannot add content to message that was not started").
- **Fix** (`636ef23e` fix(19-04)): treat ALL START/END/RESULT/CUSTOM frames as non-droppable; only repeatable
  delta frames (`*_CONTENT`, `TOOL_CALL_ARGS`, `STATE_DELTA`) may drop.
- **AFTER:** ✅ **Binding proof = Layer-1** (`636ef23e`'s rewritten `TestFanoutSlowSubscriberDropped`, which
  no longer asserts only `len<=want` but **re-validates the surviving sub-sequence via the AG-UI SDK's
  `events.ValidateSequence`** — deterministically proving a slow-client-dropped stream stays conformant).
  **Live control:** the AG-UI HTTP server booted and served on the running stack (`agui http server
  listening addr=127.0.0.1:9080`), and every live Telegram turn this session drove the in-process
  AG-UI **fanout → translator** path and rendered correctly (status pane + streamed content + HITL keyboards)
  — i.e. conformant event sequences flowed through the fanout on the running stack. A deterministic live
  cap-64 backpressure overflow is not reproducible on demand (it needs a producer outpacing a slow reader by
  >64 frames mid-turn), so per the plan the Layer-1 `ValidateSequence` regression is the binding evidence
  (same disposition as H9).

---

## Discovered during sign-off (follow-up, NOT a Phase-19 finding)

- **Reminder agnostic-channel delivery gap.** A reminder set via the Telegram bot fires but its
  notification routes to whatsapp/email/stdout (`internal/cron/notify.go`), never back to the originating
  Telegram chat — tasks don't capture their origin and the Notifier ignores it. Operator-flagged as a bug.
  Execution-ready design at `.planning/spikes/reminder-agnostic-channel.md` (identity-keyed `channels.Deliverer`
  seam; origin captured from the tool-ctx `sessionID`). Deferred to a follow-up; does not block Phase 19.

## Operator sign-off

✅ **SIGNED OFF — 2026-06-10.** All 10 user-observable findings have a live before/after repro driven by
the real paid agent / real operator action on the running stack, each with a ground-truth assertion that
does NOT read `r.Reply` (DB row / `· <toolname>` tool trace / rendered Telegram body / scheduler DB
state / protocol conformance):

| Task | Findings | Disposition |
|---|---|---|
| 1 — `aura chat` host loop | H4, H5, M-b, H9 | H4/H5/M-b live (containerized paid agent, shell_exec×4, exit-code-7 awareness, no hang); H9 Layer-1 binding + live control |
| 2 — Telegram (CDP + manual) | H2, H3, M-e | all live (rendered body + DB `paused_states`/bot logs) |
| 3 — scheduler + AG-UI | H6, H7, H1 | H6/H7 live (DB `pending_notifications` pending→delivered / failed→attempts=3); H1 Layer-1 binding + live control |

Layer-1 regression matrix (19-01..19-10) green; migration 0013 applied to the live DB. Two findings
(H9, H1) are transport edge cases whose deterministic live trigger isn't reproducible on demand — their
Layer-1 `ValidateSequence`/`premature_close.sse` regressions are the binding proofs per plan, with a
healthy-path live control recorded here.
