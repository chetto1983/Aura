# Plan — Telegram tool-call visibility

Date: 2026-05-17
Source research: [docs/telegram-tool-ui-research-2026-05-17.md](telegram-tool-ui-research-2026-05-17.md)
Reference impl (read first): `D:/tmp/nanobot/nanobot/utils/tool_hints.py`, `D:/tmp/nanobot/nanobot/channels/telegram.py:50-52,627-746`, `D:/tmp/nanobot/nanobot/agent/loop.py:120-181`

## Why

Verified on the live DB ([cmd/debug_convdump/main.go](../cmd/debug_convdump/main.go), chat 1148481707, 2026-05-16/17): when the LLM emits a tool-only turn the user sees `⏳` for 13s — **416s (7 min)** with no signal, and types "Non mi hai risposto" / "???". The runtime already emits `EventToolStart` / `EventToolEnd` ([internal/agent/runtime.go:35-38,149-176](../internal/agent/runtime.go)); the Telegram adapter ignores them ([internal/channels/telegram/invocation_builder.go:298-347](../internal/channels/telegram/invocation_builder.go)).

## What

A status pane rendered inside the existing placeholder message, edited progressively. Single Telegram message, single mutex, single 600ms throttle shared with the streaming content pipe — no race with `Outbound.ConsumeStream`. Italian copy. Privacy-respecting (only `arg_keys`, never values).

Pattern lifted from nanobot, adapted to Aura's stricter privacy rule and its existing event/throttle infrastructure.

## Slices (one commit each, master-direct, stop+verify between)

### Slice 1 — `statusPane` core (no wiring yet, no UX change)

**New file**: `internal/channels/telegram/status_pane.go` (~150 LOC).

- `type toolEntry struct { callID, name string; argKeys []string; startedAt time.Time; state running|done|failed; elapsedMs int64; preview string }`
- `type roundSummary struct { totalMs int64; entries []toolEntry }` (preview cleared once archived)
- `type statusPane struct { mu sync.Mutex; placeholder *tele.Message; activeRound []toolEntry; roundHistory []roundSummary; contentMode bool; lastEdit *time.Time; pendingEdit *time.Timer; bot tele.API; recipient tele.Recipient; logger *slog.Logger }`
- Methods:
  - `OnToolStart(callID, name string, argKeys []string)` → append entry, schedule debounced edit
  - `OnToolEnd(callID string, success bool, elapsed time.Duration, preview string)` → mutate matching entry; if round complete, archive into `roundHistory` (cap 3); schedule edit
  - `EnterContentMode()` → flip `contentMode = true`, schedule edit (so any pending blockquote collapses to the footer-line layout)
  - `Footer() string` → returns the Layout D italic footer the Outbound prepends to its content body
  - `Finalize()` → cancels any pending edit; called from `EventFinal` so the streaming pipe owns the last word
- Body composer: pure function `composeStatusBody(activeRound, roundHistory, contentMode) string` → returns the MarkdownV2 string to render. **No I/O.**
- `editMessageThrottled()`: if `time.Since(*lastEdit) >= 600ms`, edit immediately + update `*lastEdit`. Otherwise reset `pendingEdit` to fire at `*lastEdit + 600ms`. Uses `tgtelegram.RenderForEntities` for parsing → same pipeline as Outbound, no new escape code.

**Tests** (`status_pane_test.go`):
- `composeStatusBody` for Layout A/B/C/D/E — golden strings.
- Coalesce: 4 `OnToolStart` within 50ms → exactly 1 `bot.Edit` call (mock `tele.API`).
- "Not modified" guard: identical body → no edit.
- Long tool names: 4× `mcp_github__create_issue` → collapsed header truncates to `… 1 altri`.
- Failure: `OnToolEnd(success=false, preview="exit 127\nsegfault")` → renders `❌ … — exit 127` (first line, ≤80 chars).
- `EnterContentMode()` flips body to footer-only on the next edit.

**Verify**: `go test ./internal/channels/telegram/...` green; `go vet ./...`; `go build ./...`. No runtime behavior change yet (struct exists, nobody calls it).

**Commit**: `feat(telegram): add status pane for tool-call progress rendering (no wiring)`

---

### Slice 2 — wire `statusPane` into `invocation_builder.go` + `outbound.go` + `chat_client.go`

Three touched files, one commit. This is the slice that flips the UX.

**`invocation_builder.go`** (around line 186 and 298-347):
- After `placeholder, _ := c.Bot().Send(c.Recipient(), "⏳")` construct `pane := newStatusPane(c.Bot(), c.Recipient(), placeholder, b.Logger())`.
- Pass `pane` into `newStreamingChatClient(..., pane)`.
- In `OnEvent`, add two cases BEFORE the existing `EventStats / EventFinal / EventQuestionRequested`:
  ```go
  case agent.EventToolStart:
      pane.OnToolStart(event.ToolCallID, event.ToolName, event.ToolArgKeys)
  case agent.EventToolEnd:
      pane.OnToolEnd(event.ToolCallID, event.ToolSuccess,
          time.Duration(event.ToolElapsedMs)*time.Millisecond,
          event.ToolResultPreview)
  ```
- In `EventFinal`, call `pane.Finalize()` BEFORE the existing placeholder edit/delete logic. This way the streaming pipe owns the last edit.

**`chat_client.go`** (`streamingChatClient`):
- Add `pane *statusPane` field; thread through `newStreamingChatClient`.
- Pass `pane` into `outbound.ConsumeStream`.

**`outbound.go`** (`ConsumeStream`):
- Signature gains a trailing `pane *statusPane` param.
- The first time `sb.Len() >= streamingMinThreshold` (the existing `flush()` first-edit branch), call `pane.EnterContentMode()`.
- In `flush()` and the final-edit branch, the body sent to Telegram becomes `pane.Footer() + "\n\n" + composeStreamingMessage(cot, content)` — but only when `contentMode` and `pane != nil`. When the model finishes with content (the `tok.Done && !resp.HasToolCalls` branch), drop the footer entirely so the user sees only the answer.
- **Shared `lastEdit`**: refactor `lastEdit time.Time` into `lastEdit *time.Time` passed by pointer into both Outbound and statusPane (or move it onto statusPane and have Outbound call `pane.markEdited()`). Single source of truth. This is the *only* fiddly part — get the mutex right.

**Italian copy strings** (literal, locked):

| Trigger | String |
|---|---|
| Always-on header | `🛠 Sto lavorando…` |
| Collapsed blockquote, 1 tool active | `▸ {name} in corso` |
| Collapsed, N>1 active | `▸ {N} strumenti in corso · {names joined ' · '}` |
| Collapsed, mixed | `▸ {N} strumenti · {K} fatti{; M errori se >0} · {names}` |
| Per-call running | `🟡 {name} (args: {arg_keys joined ', '})` — drop `(args: …)` if empty |
| Per-call done | `✅ {name} ({elapsed.Seconds():.1f}s)` |
| Per-call failed | `❌ {name} — {preview first line, ≤80 chars}` (fallback `errore`) |
| Round footer in history | `— round {N} ({total_ms}ms): {glyphs + names}` |
| Layout D demoted footer | `_🛠 {N} strumenti usati in {K} round · {total_elapsed.Seconds():.1f}s_` |

**Verify**:
- `go build ./...`, `go vet ./...`, `go test ./internal/channels/telegram/...`, `go test -race ./internal/{api,mcp,skills,channels/telegram}/`.
- **Live probe**: bring up `docker compose up -d --build aura`, send 3 Telegram messages that exercise the scenarios:
  1. "trova in memoria cosa sai di phantom guard" → 1 tool, ~1s → Layout A→B→D
  2. "cerca su web le ultime release di go" → 1 tool ~5-10s → Layout A persistent, then Layout D
  3. "scansiona la lan e cerca info su 192.168.1.1" → multi-round, parallel tools (`execute_shell` ×N + `web_search`) → Layout A→B→C with history, then Layout D
- After each: read `data/aura.db` via `/tmp/convdump.exe -n 20 -chat 1148481707` and confirm `tool_calls` archive still matches turn structure.
- Visual check on Telegram client: confirm collapsed blockquote takes 1 line + tap-to-expand shows full list. Confirm `is_not_modified 400` does not appear in logs.

**Commit**: `feat(telegram): show in-flight tool calls in the placeholder (was silent ⏳)`

---

### Slice 3 — native `typing…` indicator (small, additive)

**`invocation_builder.go`**: at placeholder-send time, kick a background goroutine that calls `c.Notify(tele.Typing)` every 4s until the run finishes (cancel via the existing run context). Nanobot does the same; Telegram's `chat_action` times out client-side at 5s.

**Verify**: same probe scenarios as Slice 2; confirm the native "typing…" dots show for the full duration of a >5s tool run. No new tests (it's fire-and-forget; mock would be noise).

**Commit**: `feat(telegram): refresh chat_action(typing) every 4s during a turn`

---

## Out of scope (deliberately deferred)

- **Bot API 9.3 `sendMessageDraft`** — private-chat only, would fragment transport. Note in `docs/` for future.
- **WebUI dashboard live tool-call view** — DB has everything; React-side build is separate.
- **Localizing tool names** (`execute_shell` → "shell", `mcp_github__create_issue` → "GitHub issue") — copy work, separate pass.
- **Reasoning CoT persistent display** — `composeStreamingMessage` already does the ephemeral version; persistent surfacing is a different design conversation.

## What this does NOT touch

- `internal/agent/*` — no new event types, no runtime change.
- `internal/chat/*` — events flow unchanged.
- `internal/db/migrations` — archive schema unchanged.
- `RenderForEntities` / MarkdownV2 pipeline — body composed as a string, parsed by existing code.
- `streamingEditThrottle = 600ms` — kept (research confirms it's correctly sized for Telegram's per-message edit ceiling).

## Risks + mitigations

| Risk | Mitigation |
|---|---|
| Edit collision between statusPane and Outbound | Shared `*time.Time` `lastEdit` + single mutex (slice 2). Probe with `-race`. |
| `400 Bad Request: message is not modified` spam | "Not modified" guard in `editMessageThrottled` — skip edit when rebuilt body equals last body (unit test). |
| MarkdownV2 escape bugs in blockquote | Reuse `RenderForEntities` — no new escape code. Golden tests for layouts A/B/C/D/E. |
| `expandable_blockquote` not rendered on old Telegram clients | Degrades gracefully to a normal blockquote (visible but not collapsed). Worst case: chat history takes a few extra lines per turn. Accept. |
| `OnToolStart` arriving for a `call_id` that never gets `OnToolEnd` (tool crash before runtime catches it) | `Finalize()` archives any still-running entries as `failed` with `preview = "(no end signal)"`. Defensive. |
| Long-running tool (e.g. `execute_shell` 2-min timeout) → no liveness signal | Slice 3 (`chat_action: typing` refresh) covers it. |

## Acceptance — what to see manually after Slice 2+3

Reproduce against the live bot, three turns:

| Scenario | Expected user-visible sequence |
|---|---|
| 1 fast tool (~1s `search_memory`) | `⏳` → `🛠 Sto lavorando…` + collapsed `▸ search_memory in corso` (within ~600ms) → `_🛠 1 strumento usato · 1.0s_` + answer body → just the answer |
| 1 slow tool (~12s `web_search`) | Layout A appears, persists; native `typing…` dots throughout; → `✅ web_search (12.4s)` collapsed view → Layout D → answer |
| 4-parallel + multi-round (nmap-like) | `▸ 4 strumenti in corso` (1 edit, not 4); progressive `▸ 4 · 2 fatti`; round 1 archived to footer when round 2 opens; Layout D demotion at first content; on `tok.Done`, just the answer |
| Failure (`execute_shell` exit ≠ 0) | `❌ execute_shell — exit 127` in expanded view; `1 errore` in collapsed header; loop continues |
| 4+ silent rounds → final answer (the original bug) | Each round visible in placeholder; user never sees silent `⏳`; final answer cleanly replaces footer |

## Anti-patterns (do NOT do)

1. Second placeholder bubble next to the answer — fragments the eye, doubles edit pressure.
2. Edit per-event without coalescing — 32 edits in 4×4 = 19s of throttle latency.
3. Animated spinner via edits — wastes the edit budget; native `typing…` is free.
4. Show arg values — violates privacy. `arg_keys` only.
5. Show `preview` on success — preview can carry URLs with tokens, source text. Show ONLY on failure.
6. Hand-rolled MarkdownV2 escape — reuse `RenderForEntities`.

---

## Execution order recommendation

Land Slice 1 + tests first (no UX risk). Then Slice 2 + manual Telegram probe (this is the slice the user actually feels). Slice 3 last (small additive polish). Between each slice, stop + verify per the project convention.
