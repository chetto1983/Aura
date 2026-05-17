# Telegram tool-call progress UX — research and recommendation

Date: 2026-05-17
Author: Claude Code research subagent
Scope: how to render `EventToolStart` / `EventToolEnd` inside the existing Telegram placeholder so users stop seeing a silent `⏳` for 13s–7min.

---

## TL;DR (read this first)

Adopt **one placeholder message, edited progressively, structured as a short Italian header + an expandable Telegram blockquote of one-line-per-tool-call entries**, with checkmarks for completed / spinner-glyph for in-flight / cross for failed. When narrative content > 30 chars arrives, the status pane gets **demoted to a single-line footer** above the streaming answer (or compressed entirely once the final answer is delivered).

This is essentially the **nanobot pattern** (`utils/tool_hints.py` + Telegram `<blockquote expandable>`) adapted to Aura's stricter privacy rule (no arg values, only keys) and reused with Aura's existing `EventToolStart` / `EventToolEnd` events. No new runtime events, no new throttle, single commit, ~200 LOC inside `internal/channels/telegram/`.

Acceptance criteria, parallel-call behavior, and the handoff protocol with `Outbound.ConsumeStream` are below.

---

## Step 1 — Local source survey

| Source | Useful? | What it actually does for tool progress |
|---|---|---|
| `D:/tmp/picobot/` | No | Telegram adapter is bare `sendMessage` / no streaming, no edits, no tool hints (`internal/channels/telegram.go` lines 122-144). Skip. |
| `D:/tmp/nanobot/` | **Yes, primary** | Per-iteration `before_execute_tools` hook builds a `tool_hint` string via `utils/tool_hints.py::format_tool_hints()` and ships it as a progress message with `_tool_hint=True`. Telegram adapter renders it as `<blockquote expandable>...</blockquote>` (collapsed by default). Format: `read foo.md, grep "pattern" × 2, $ ls /tmp`. See `nanobot/channels/telegram.py:50-52`, `nanobot/agent/loop.py:141-148`, `nanobot/utils/tool_hints.py:30-54`. |
| `D:/tmp/codex.md` | Mostly no | OpenAI's article is about the loop, not the UI. Mentions only "Codex streams progress to stderr" and shows "up to three recent, non-empty output lines per background terminal" in the TUI — confirms the *truncated last-3-lines* pattern for command output but no chat-channel guidance. |
| `D:/tmp/cli-printing-press/` | No | CLI generator, not a chat UI. Already studied in `docs/cli-printing-press-study.md`; the 3 patterns to adopt have nothing to do with tool progress. Skip. |
| `D:/tmp/elysia/` | Partially | Uses typed websocket payloads (`tool_call`, `status`, `query_completed`) emitted from `api/routes/query.py`. Right idea (typed event stream) but a WS multi-bubble UI doesn't translate to Telegram's single-message edit model. Reinforces "typed events" but Aura already has them. |
| `D:/tmp/paper.md` | No | Kimi K2.5 technical report — multi-modal agent training, no UX. Skip. |
| `D:/tmp/hermes-agent/` | Useful as confirmation | `agent/display.py::build_tool_preview` is a CLI/TUI per-tool one-line summary keyed off the primary arg (`terminal→command`, `web_search→query`, `read_file→path`, etc.). Same shape as nanobot but for ANSI terminals. **Confirms the pattern is universal**: an arg-key dispatch table → a one-line preview. Aura cannot show the value, but the dispatch table approach is portable. |
| `D:/tmp/claude/` | No | Just cache-break diffs. Skip. |
| `D:/tmp/ralph-src/` | No | Shell orchestrator, not chat. Skip. |
| `D:/tmp/mem0/` | Not opened — orthogonal | Memory framework, not relevant per the user's note. Skip. |

### Nanobot code in one screen

`utils/tool_hints.py` — a per-tool format registry, then deduplicates consecutive identical hints with `× N`:

```python
_TOOL_FORMATS = {
    "read_file":  (["path", "file_path"],  "read {}",     True,  False),
    "write_file": (["path", "file_path"],  "write {}",    True,  False),
    "grep":       (["pattern"],            'grep "{}"',   False, False),
    "exec":       (["command"],            "$ {}",        False, True),
    "web_search": (["query"],              'search "{}"', False, False),
    "web_fetch":  (["url"],                "fetch {}",    True,  False),
    ...
}
# MCP tools render as `server::tool("arg")`; unknown as `name("arg")`.
# Output: "read foo.md, grep \"pattern\" × 2"
```

`channels/telegram.py:50-52` — the rendered hint becomes a *collapsed-by-default expandable blockquote*, which the Telegram client renders as a one-line summary the user can tap to expand:

```python
def _tool_hint_to_telegram_blockquote(text: str) -> str:
    return f"<blockquote expandable>{_escape_telegram_html(text)}</blockquote>" if text else ""
```

`agent/loop.py:133-148` — fires `_on_progress(hint, tool_hint=True)` from the `before_execute_tools` hook so the user sees the hint BEFORE the tools run, then `after_iteration` fires another progress event with the typed `tool_events` array (start/end/error per call_id) — this is exactly Aura's `EventToolStart`/`EventToolEnd` pair, the only difference being Aura's payload carries `arg_keys` instead of `arguments`.

---

## Step 2 — Web research (the patterns that actually exist in production)

### Telegram-specific facts that constrain the design

- **Rate limit on `editMessageText`** to the same message: ~30 edits/sec is the bot-wide cap, but the safe sustained rate for **the same message** is **2-3 edits/sec**. Aura's existing `streamingEditThrottle = 600ms` (1.67 edits/sec) is already correctly sized.
- **Telegram Bot API 9.3 (March 2026)** introduced `sendMessageDraft` for native streaming bubbles in private chats. Bot API 9.5 opened it to all bots. **We intentionally do not use this** — it's private-chat only, fragments the codepath (group chats / topics still need `editMessageText`), and would require a transport change in `internal/telegram`. Out of scope for this slice.
- **`expandable_blockquote`** is a first-class `MessageEntity` type (confirmed via core.telegram.org/bots/api). Renders collapsed with a single visible line + tap-to-expand. Available in MarkdownV2 as `**>blockquote content||` (the `||` suffix makes it expandable) or directly as an HTML `<blockquote expandable>` entity.
- **`sendChatAction(typing)`** times out client-side after ~5 seconds — must be refreshed every ~4s if used. Nanobot does this in a background `_typing_loop`. Useful as a complementary signal but not a status channel.

### How the major agents render in-flight tool calls

| Tool | Render shape | Per-call separation | Args shown? | Final state |
|---|---|---|---|---|
| **Claude Code TUI** | `[Bash] npm test`, `[Read] /path/file.go`, `[Grep] pattern: "foo" path: "src/"` — one line per call, prepended with `⏺` while running and (per the Hatchet TUI write-up) the tool emoji/checkmark when done | One line per call (alternate-screen renderer in full-screen mode keeps only visible lines) | Full args, syntax-highlighted | Output collapsed into expandable block; only first ~10 lines shown by default |
| **Codex CLI** | Stderr stream during the run; final agent message to stdout. Background terminal previews show **up to 3 recent non-empty output lines** | Per command | Yes (full command + workdir) | Final result visible; `--json` available for structured consumption |
| **Cursor chat** | A pill/card per tool call inline in the conversation (`Searching files…` → `Searched 47 files`), with the underlying snippet expandable | One pill per call | Yes (visible in the pill subtitle) | Pill collapses, content available on tap |
| **Vercel AI SDK 5 `useChat`** | Each tool call becomes a `tool-<NAME>` part on `message.parts` with states `partial-call → call → result`. Renderer decides; tool-call streaming is on by default so inputs render as they generate | Per part | Yes (controlled by renderer) | Final state replaces partial |
| **ChatGPT (consumer)** | Status text in the composer ("Searching the web…", "Reading sources…"), a shimmer animation, then inline result citations. One status line at a time, replaced as the model moves through stages | One label at a time | No (just the verb) | Replaced by the answer; sources appear as chips |
| **nanobot Telegram** | Single expandable blockquote with comma-separated hints (`read x.md, grep "y" × 2`) appended above/below the streamed content | One blockquote per round, items comma-separated inside | Yes (paths/queries) | Stays as a collapsed blockquote next to the final answer |

### Patterns to keep

1. **One placeholder, edited in place** (every chat agent does this on Telegram). Multiple bubbles per tool call would clobber rate limits in 4-parallel scenarios and pollute the chat history.
2. **One line per tool call** when there are few; collapse to `name × N` when the same tool repeats. (nanobot, Cursor.)
3. **Three-state glyph**: pending / done / failed. Universal across Claude Code, Cursor, ChatGPT, every IDE plugin.
4. **Expandable blockquote** for the tool list — Telegram-native, costs one tap to expand, takes one screen line when collapsed. (nanobot.)
5. **Status verb in the user's language** — ChatGPT's "Searching…" / "Reading…" pattern works because it's a verb the user understands at a glance.
6. **Demote the status pane once narrative content arrives** — ChatGPT, Claude Code, Cursor all do this: the spinner/status fades to a compact summary above the actual answer.

### Patterns to reject for Aura

- **Multiple bubbles per tool call** (one bubble per dispatch): would burn the global 30 msg/sec limit on a 4-parallel scenario and trash chat scrollback. Reject.
- **Bot API 9.3 `sendMessageDraft`**: private-chat only, fragments transport. Defer.
- **Emoji-as-spinner animation** (`⏳→⌛→⏳` rotating via edits): each rotation eats one of the 1.67 edits/sec budget that should be reserved for content edits. The Telegram "typing…" indicator already gives that signal for free. Reject.
- **Showing arg values** (paths, queries, URLs): violates the Aura privacy rule (`internal/chat/agentloop.go:34` already enforces "arg KEYS only — values intentionally omitted"). The hint shows only the tool name + the *list of keys*, never the values.
- **Per-tool-call separate edit** (edit the placeholder once per `EventToolStart` *and* per `EventToolEnd`): with 4 parallel tools + 4 rounds = 32 edits, ~19s minimum at the 600ms throttle just for status churn. Coalesce.

---

## Step 3 — Recommendation for Aura

### Render format

The placeholder lives in the same Telegram message as the final answer (the one created at `invocation_builder.go:186`). It cycles through three layouts:

#### Layout A — Tool round in flight, no narrative yet

```
🛠 Sto lavorando…
**>3 strumenti in corso·web_search · search_memory · execute_shell
🟡 web_search (args: query)
🟡 search_memory (args: query, top_k)
🟡 execute_shell (args: command)||
```

(`**>` and `||` are the MarkdownV2 markers for an expandable blockquote. The first line *inside* the blockquote is what shows when collapsed — make it the count + comma-separated names. Each indented line below is one per call; `🟡` = in-flight.)

When collapsed (the default), the user sees only:

```
🛠 Sto lavorando…
▸ 3 strumenti in corso · web_search · search_memory · execute_shell
```

Tap → expands to the per-call detail.

#### Layout B — One tool completed, others still running

```
🛠 Sto lavorando…
**>3 strumenti · 1 fatto·web_search · search_memory · execute_shell
✅ web_search (1.2s)
🟡 search_memory (args: query, top_k)
🟡 execute_shell (args: command)||
```

#### Layout C — Round complete, next round starting

After every `EventToolEnd` for a round (i.e., when the count of completed = count of started for that round), append the round to a "history" tail and reset the active list. After round 2:

```
🛠 Sto lavorando…
**>round 2 · 2 strumenti in corso·search_memory · web_fetch
🟡 search_memory (args: query)
🟡 web_fetch (args: url)
— round 1 (1.4s): ✅ web_search · ✅ search_memory · ❌ execute_shell||
```

The history footer line(s) live INSIDE the blockquote so they don't bloat the collapsed view. Keep at most last 3 rounds; older rounds collapse into `… N rounds earlier`.

#### Layout D — Narrative content has started arriving (handoff)

`ConsumeStream` has now received >30 chars of `tok.Content` and is editing the same message. Status pane demotes to a **single italic footer line** ABOVE the content:

```
_🛠 4 strumenti usati in 3 round · 2.8s_

Ho cercato la guida fiscale e ho trovato che la scadenza per la dichiarazione…
```

The narrative content owns the body. The tool log is reachable via the conversation archive (`/api/conversations`) — not re-shown in the chat.

#### Layout E — Failure rendering

```
🛠 Sto lavorando…
**>round 1 · 1 errore·web_search
✅ web_search (0.9s)
❌ execute_shell (args: command) — exit 127||
```

Failure preview is the **single first line** of `EventToolEnd.preview` truncated to 80 chars (Aura already enforces preview generation). No stack trace. If `preview` is empty, fall back to "errore".

### Concrete copy strings (Italian, final answer)

| Trigger | String |
|---|---|
| Header (always) | `🛠 Sto lavorando…` |
| Collapsed blockquote, 1 tool | `▸ {tool_name} in corso` |
| Collapsed blockquote, N tools, all running | `▸ {N} strumenti in corso · {names joined}` |
| Collapsed blockquote, mixed | `▸ {N} strumenti · {K} fatti · {names}` |
| Per-call line, running | `🟡 {name} (args: {arg_keys joined})` — drop the `(args: …)` parenthesis if `arg_keys` is empty |
| Per-call line, done | `✅ {name} ({elapsed}s)` |
| Per-call line, failed | `❌ {name} — {preview first line, ≤80 chars}` |
| Round footer in history | `— round {N} ({total_ms}ms): {per-call glyph + name list}` |
| Demoted footer line (Layout D) | `_🛠 {N} strumenti usati in {K} round · {total_elapsed}s_` |

### Behavior on parallel tool calls

Aura's agent already runs independent tool calls in the same turn concurrently (`internal/agent`). The renderer keeps:

- `activeRound []toolEntry` — entries indexed by `tool_call_id`
- `roundHistory []roundSummary` — capped at 3

On `EventToolStart`: append a `{call_id, name, arg_keys, started_at, state: running}` to `activeRound`, recompute the blockquote, throttle-edit the placeholder.

On `EventToolEnd`: mutate the matching `call_id` entry to `{state: done|failed, elapsed_ms, preview}`. If `len(done) + len(failed) == len(activeRound)`, **archive the round** into `roundHistory` and reset `activeRound` to empty (next `EventToolStart` opens round N+1).

Edit-throttle is shared with the streaming throttle (600ms) — see Handoff section below.

### Handoff between status pane and content stream

`Outbound.ConsumeStream` already owns the placeholder once content starts streaming (see `internal/channels/telegram/outbound.go:90-194`). The status pane and content stream **must share the same edit throttle and the same message**, otherwise they race on `bot.Edit()` and one overwrites the other.

Proposed protocol — implement as a new `statusPane` struct in `internal/channels/telegram/`:

```
type statusPane struct {
    mu           sync.Mutex
    placeholder  *tele.Message
    activeRound  []toolEntry
    roundHistory []roundSummary
    contentMode  bool          // true once Outbound has started streaming text
    lastEdit     time.Time     // shared with Outbound — see below
    bot          tele.API
    recipient    tele.Recipient
}
```

Wiring (no new agent events, no new throttle):

1. `invocation_builder.go:298` `OnEvent` callback gains two cases:
   - `agent.EventToolStart` / `agent.EventToolEnd` → `statusPane.update(event)`, which rebuilds the message body and calls `editMessageThrottled()`.
2. `editMessageThrottled()` checks `lastEdit`; if <600ms, schedules a single tail-edit via a `time.AfterFunc(600ms - delta)` debounce (cancel any previous pending one). This is the same throttle Outbound uses.
3. `Outbound.ConsumeStream` gets a new optional param `*statusPane`. When `sb.Len() >= streamingMinThreshold` (the first content flush), it calls `statusPane.enterContentMode()`:
   - Marks `contentMode = true`.
   - Replaces the message body: header (footer line for Layout D) **above** `composeStreamingMessage(cot, content)`.
   - From this point, all `EventToolStart`/`EventToolEnd` updates only mutate the *footer line counters* (`{N} strumenti usati`), not the blockquote. The blockquote disappears once we're in contentMode.
4. The `lastEdit` field is shared (pointer) so the two writers respect the same 600ms budget and never race.
5. Final flush in `ConsumeStream` (the `tok.Done && !resp.HasToolCalls` branch) clears the footer entirely and renders only the answer.

This means the status pane and the streaming pipe physically write to the same `*tele.Message`, with a single mutex, single throttle, single source of truth on `lastEdit`. No edit collisions possible.

### Anti-patterns to avoid

1. **A second placeholder bubble for status next to the answer bubble** — doubles the chat scrollback, doubles edit pressure, fragments the user's eye. Reject.
2. **Edit on every `EventToolStart` AND every `EventToolEnd`** without coalescing — 32 edits in a 4-parallel × 4-round scenario = ~19s of throttle latency. Coalesce within the 600ms window: a single tail-debounce per window.
3. **Animating the spinner emoji** (`⏳/⌛/🔄` rotating via edits) — wastes the edit budget. Telegram's native `chat_action: typing` is free and already conveys liveness. Optionally keep the typing action running in a background goroutine (refresh every 4s) for the duration of the turn — like nanobot does.
4. **Showing arg values** anywhere — violates the privacy rule. Only `arg_keys` (literal field names like `query, top_k, command`) are safe.
5. **Showing tool result `preview` in success state** — the preview can carry source text, URLs with tokens, or PII (it's bounded but unsanitized). Show preview ONLY on failure (where it tends to be an error message), not on success. On success, the elapsed time is enough.
6. **Calling `editMessageText` without the "is_not_modified" guard** — Telegram returns `400 message is not modified` if the new body equals the current. The renderer must skip edits when the rebuilt body hasn't changed (nanobot does this at `telegram.py:624-625` via `_is_not_modified_error`). Worth a unit test.
7. **Building MarkdownV2 by hand for the blockquote** — Aura already has `internal/telegram/entity_messages.go` and `RenderForEntities`. Compose the body as a string in the renderer and let the existing entity pipeline parse it. No new escape logic.
8. **Long tool names breaking the collapsed line** (e.g., `mcp_github__create_issue` appearing 4× — 96 chars on one line). Cap the collapsed header at the first 3 tool names + `… N altri` if more.

### Acceptance criteria

For each scenario, a human should observe:

**(a) One tool call ~1s** (e.g., `search_memory`)
- Within 600ms of dispatch: `⏳` → `🛠 Sto lavorando… ▸ search_memory in corso`
- Within 600ms after return: same message becomes `🛠 Sto lavorando… ▸ 1 strumento · 1 fatto`
- Once narrative > 30 chars: message becomes `_🛠 1 strumento usato · 1.0s_` + answer body
- Final state: just the answer (footer wiped at `tok.Done`).

**(b) One tool call >10s** (e.g., `web_search` against slow SearXNG)
- Same Layout A appears within 600ms.
- The user expands the blockquote and sees `🟡 web_search (args: query)` with no elapsed counter (this is fine — Telegram clients have no concept of a "ticking" cell; the typing indicator carries the liveness).
- Background goroutine keeps `sendChatAction(typing)` alive every 4s so the user sees the native typing dots throughout the wait.
- When the tool returns, the message edits to `✅ web_search (12.4s)`.

**(c) 4 parallel tool calls** (e.g., one round of `web_search`, `search_memory`, `read_skill`, `read_memory`)
- 4 `EventToolStart` events arrive within milliseconds of each other.
- The renderer coalesces into a single edit (debounced 600ms): one message showing 4 lines, all 🟡.
- As each completes (random order), the renderer batches updates: at most one edit per 600ms window. Worst case for a 4-parallel round: ~3-4 edits total over the round's duration.
- Collapsed view always shows the running summary: `▸ 4 strumenti · 2 fatti · web_search · search_memory · read_skill · …`

**(d) Tool failure** (e.g., `execute_shell` returns non-zero)
- `EventToolEnd.success = false` + non-empty `preview`.
- Per-call line shows `❌ execute_shell — exit 127`.
- Round counter shows `1 errore` in the collapsed header.
- The agent loop continues (failure is reported to the model, not surfaced as a chat-level error unless terminal).
- No stack trace, no full preview leaked.

**(e) 4+ rounds of tool-only output** (the original phantom-guard scenario)
- Round 1: Layout A → Layout B as it progresses → Layout C (round archived to footer).
- Round 2 opens with fresh `activeRound`; round 1 visible in footer.
- After round 3, round 1 collapses into `… 1 round precedente`, only rounds 2+3 enumerated in the footer.
- The user sees evidence-of-progress at every step. No silent `⏳` for more than the 600ms throttle window.
- When the model finally emits content (round 5 say), Layout D kicks in and the whole tool history collapses into the single italic footer line above the answer.

### What this does NOT change

- No new event types in `internal/agent` or `internal/chat`. The two existing events carry everything the renderer needs.
- No new throttle. Reuses `streamingEditThrottle = 600ms` shared between status and content.
- No transport changes in `internal/telegram` (no `sendMessageDraft`).
- No change to `RenderForEntities` / `MarkdownV2` pipeline — the renderer just composes a body string.
- No change to the conversation archive — full tool history is still persisted there.

### Scope estimate (master-direct, single commit)

- New file: `internal/channels/telegram/status_pane.go` (~150 LOC: struct, `update()`, `enterContentMode()`, body composer, throttle).
- Touch: `invocation_builder.go:298-345` `OnEvent` — add the two cases, pass `statusPane` into the chat client.
- Touch: `outbound.go::ConsumeStream` — accept optional `*statusPane`, call `enterContentMode()` at first content flush; respect shared `lastEdit`.
- Touch: `chat_client.go::newStreamingChatClient` — accept and forward the `*statusPane`.
- Tests:
  - Unit: `status_pane_test.go` — body composition for layouts A/B/C/D/E with synthetic events.
  - Unit: throttle/coalesce — 4 parallel `EventToolStart` events produce exactly 1 edit.
  - Probe (uses `cmd/probe_chat` per CLAUDE.md): a long tool-loop conversation must show non-empty placeholder text within 1s of dispatch.

Estimated total: ~300 LOC including tests, one commit on master.

---

## Sources

Local references actually used:
- `D:/tmp/nanobot/nanobot/channels/telegram.py` lines 50-52, 219-227, 627-746
- `D:/tmp/nanobot/nanobot/utils/tool_hints.py` (whole file)
- `D:/tmp/nanobot/nanobot/utils/progress_events.py` (whole file)
- `D:/tmp/nanobot/nanobot/agent/loop.py` lines 120-181
- `D:/tmp/hermes-agent/agent/display.py` lines 170-276
- `D:/tmp/picobot/internal/channels/telegram.go` (negative result)
- `D:/tmp/codex.md` (loop article, not UI)
- `D:/tmp/cli-printing-press/` (already studied — irrelevant for this slice)

Aura code referenced:
- `D:/Aura/internal/chat/agentloop.go` lines 100-145 (the existing event emission)
- `D:/Aura/internal/chat/types.go` line 73-75 (event types)
- `D:/Aura/internal/channels/telegram/outbound.go` lines 22-194 (ConsumeStream)
- `D:/Aura/internal/channels/telegram/invocation_builder.go` lines 180-345 (placeholder, OnEvent wiring)

Web sources:
- [Telegram Bot API — MessageEntity types incl. expandable_blockquote](https://core.telegram.org/bots/api)
- [Telegram editMessageText rate limits — tdlib/td #3034](https://github.com/tdlib/td/issues/3034)
- [Telegram rate limits guide](https://hfeu-telegram.com/news/telegram-bot-api-rate-limits-explained-856782827/)
- [Vercel AI SDK 5 tool invocation rendering](https://sdk.vercel.ai/docs/ai-sdk-ui/chatbot-with-tool-calling) (returned ECONNREFUSED but pattern confirmed via search snippets and blog post)
- [Cursor tool call rendering](https://docs.cursor.com/chat/tools)
- [Codex CLI background terminal preview — `unified_exec`](https://github.com/openai/codex/blob/main/docs/exec.md)
- [Iris: streaming AI responses to Telegram in real time](https://iris.rezaulhreza.co.uk/blog/030-telegram-streaming) (text streaming only; no tool progress — confirmed the 1.5s flush / 20-char minimum delta numbers match Aura's sizing)
- [Claude Code TUI tool display format](https://callsphere.ai/blog/claude-code-tool-system-explained)
