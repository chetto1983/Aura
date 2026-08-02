# Telegram Bot UI/UX Best Practices — research to inform Aura's Telegram channel

Date: 2026-06-08. Scope: best-practice Telegram bot UI/UX, mapped to Aura's
`internal/channels/telegram/` (Go, `gopkg.in/telebot.v4`). Sources: official Bot API
docs + community guides (cited inline), curated local sources in `D:/tmp`, and the
`github.com/go-telegram/ui` widget library. Every external claim carries a URL.

Aura's current Telegram surfaces (the system this informs):
- **Status pane** — msg #1, edited in place per turn: tool list with 🟡/✅/❌, a 💭
  reasoning line, a running-cost footer; throttled edits (`status_pane.go`).
- **Content renderer** — msg #2: streamed answer, converted to Telegram HTML with a plain-text
  fallback on the "can't parse entities" 400; markdown tables → PNG via sendPhoto
  (`renderer.go`, `html.go`, `tables.go`).
- **HITL inline keyboards** via `ask_user`: approval (Sì/No), choice (2–4 buttons +
  Annulla), clarification (ForceReply) (`hitl.go`, `bot_dispatch.go`).
- **Multimodal** — voice, photo and documents are ingested as assets and processed by
  the shared pipeline (`internal/assets`); TTS-out (the voice-note reply) is the one
  media leg the channel still owns.
- **Just-fixed live bug** — `callback_data` must stay ≤64 bytes; long option values 400
  with `BUTTON_DATA_INVALID`. Aura now embeds the option **index**, resolving it back to
  the value server-side (`hitl.go: choiceMarkup` / `resolveChoiceValue`). This is exactly
  the industry-standard workaround (see §3).

---

## 1. Executive summary — top improvements, prioritized (impact × effort)

| # | Improvement | Impact | Effort | Where |
|---|---|---|---|---|
| 1 | ✅ **Adopt the index→server-side-map callback_data pattern everywhere, and harden it.** Choice buttons carry indices and `callbackData()` guards the 64-byte cap. | High | Done | `hitl.go` |
| 2 | ✅ **Remove the keyboard once a HITL pause is answered.** Valid taps now clear the prompt markup immediately. | High | Done | `hitl.go`, `bot_dispatch.go` |
| 3 | ✅ **Always answer the callback with context.** HITL taps now show short confirmation toasts before resume rendering. | High | Done | `bot_dispatch.go: onCallback` |
| 4 | ✅ **Adopt HTML parse mode for the answer renderer.** `gotg_md2html` converts Markdown-ish model output to Telegram-safe HTML, with the existing plain-text fallback retained. | High | Done | `html.go`, `renderer.go` |
| 5 | ✅ **Paginate long `/search` output.** Long result sets now use inline prev/next pagination with server-side state. | Med | Done | `commands.go`, `bot_dispatch.go` |
| 6 | ✅ **Register a command menu via setMyCommands.** Live `Start` registers the 10 slash-commands for Telegram autocomplete. | Med | Done | `bot.go` |
| 7 | ✅ **Split / chunk content over 4096 chars.** Finalized long answers are sent as multiple Telegram-sized messages. | Med | Done | `renderer.go` |
| 8 | ✅ **Add a "Stop / Annulla" inline button to the status pane.** The button reuses the existing per-chat cancel registry. | Med | Done | `status_pane.go`, `bot_dispatch.go` |

No implementation backlog remains from this doc. Future optional spikes, closed for this pass:
native streaming via `sendMessageDraft` once the deployed Bot API and telebot expose it;
"/" command scoping per chat type; a settings menu for TTS/verbosity toggles.

---

## 2. Hard constraints & limits (concrete Bot-API numbers)

| Constraint | Value | Source |
|---|---|---|
| Message **text** max length | **4096** characters (UTF-16 code units) | [core.telegram.org/bots/api](https://core.telegram.org/bots/api) |
| Media **caption** max length | **1024** characters | [core.telegram.org/bots/api](https://core.telegram.org/bots/api) |
| **callback_data** size | **1–64 bytes**, UTF-8; over → `400 BUTTON_DATA_INVALID` | [core.telegram.org/bots/api](https://core.telegram.org/bots/api), [seroperson.me callback_data](https://seroperson.me/2025/02/05/enhanced-telegram-callback-data/) |
| Inline keyboard: buttons **per row** | up to **8** | [botnamefinder inline-keyboard guide](https://botnamefinder.com/blog/telegram-inline-keyboard-builder-guide) |
| Inline keyboard: **total** buttons | up to **100** (Bot API 7.0+) | [botnamefinder inline-keyboard guide](https://botnamefinder.com/blog/telegram-inline-keyboard-builder-guide) |
| **answerCallbackQuery** text | **0–200** characters; `show_alert` toggles toast vs modal; `cache_time` default 0 | [core.telegram.org/bots/api](https://core.telegram.org/bots/api), [aiogram answer_callback_query](https://docs.aiogram.dev/en/latest/api/methods/answer_callback_query.html) |
| Unanswered callback spinner | client shows a loading spinner for up to **30 s** if you never call answerCallbackQuery | [wyu inline keyboard UX guide](https://wyu-telegram.com/blogs/444/) |
| **Global** send rate | ~**30 messages/second** per bot token (shared across sendMessage, editMessage, sendChatAction, …); over → 429 | [core.telegram.org/bots/faq](https://core.telegram.org/bots/faq), [gramio rate-limits](https://gramio.dev/rate-limits) |
| **Per-chat** send rate | avoid **>1 message/second** in a single chat; bursts tolerated, then 429 | [core.telegram.org/bots/faq](https://core.telegram.org/bots/faq) |
| **Per-group** rate | ≤ **20 messages/minute** | [core.telegram.org/bots/faq](https://core.telegram.org/bots/faq) |
| 429 response | carries `retry_after`; bot blocked for that duration (can be 35 s+) | [gramio rate-limits](https://gramio.dev/rate-limits) |
| **sendChatAction** lifetime | action lasts ~**5 seconds** or until the bot sends a message; re-send to keep it alive | [mindstudio set typing](https://www.mindstudio.ai/capabilities/telegram-set-typing), [latenode typing thread](https://community.latenode.com/t/can-telegram-bots-show-online-status-or-typing-indicator/29695) |
| getFile download cap | **20 MB** per file | [core.telegram.org/bots/api](https://core.telegram.org/bots/api) |
| **sendMessageDraft** (native streaming) | Bot API **9.5** — streams partial text with native typing animation (alternative to edit-based streaming) | [openclaw streaming issue #33220](https://github.com/openclaw/openclaw/issues/33220) |

Aura conformance check:
- ✅ `renderer.go` encodes `telegramTextCap = 4096`, `telegramCaptionCap = 1024`.
- ✅ `config.go` defaults: status throttle 1500 ms, content throttle 500 ms, per-chat
  rate 1000 ms — all within the per-chat ≤1 msg/s budget.
- ✅ `bot_typing.go` re-pulses the chat action every 4 s (under the ~5 s expiry) — correct.
- ✅ `renderer.go` splits finalized long answers into multiple Telegram-sized messages
  instead of silently truncating the tail.
- ✅ callback_data uses compact indices for choices, and `callbackData()` fails loudly in
  tests if a future inline-button payload exceeds Telegram's 64-byte cap.

---

## 3. Inline keyboard & HITL UX best practices

### 3.1 Inline vs reply keyboards — when to use which
- **Inline keyboards** (buttons attached to a message): the tap fires a callback that runs
  *behind the scenes* — no message is posted to the chat. Use for **actions on a specific
  message**: approve/decline, pick an option, paginate, cancel.
  [telegrambots reply-markup guide](https://telegrambots.github.io/book/2/reply-markup.html)
- **Reply keyboards** (custom keyboard replacing the system keyboard): a tap **sends a
  message** as the user. Use for persistent top-level menus / quick canned inputs.
  [telegrambots reply-markup guide](https://telegrambots.github.io/book/2/reply-markup.html)
- **ForceReply**: forces the client into a reply box bound to your prompt — good for
  free-text answers (clarification).

**Aura mapping** (`hitl.go: prompt`): approval/choice → inline keyboards (correct);
clarification → `ForceReply` (correct). This is the textbook split. ✅

### 3.2 callback_data strategies (the 64-byte ceiling)
The universally recommended pattern is **never put the payload in callback_data; put a
short id / index and keep the real value server-side.**
[seroperson enhanced callback_data](https://seroperson.me/2025/02/05/enhanced-telegram-callback-data/),
[wyu inline keyboard UX guide](https://wyu-telegram.com/blogs/444/)

- Use a **structured prefix convention** so callbacks route by pattern, not long if/else:
  `menu:settings`, `page:2`, `hitl:<token>:<idx>`.
  [wyu inline keyboard UX guide](https://wyu-telegram.com/blogs/444/)
- Minimize button **text** too — each UTF-8 emoji is up to 4 bytes; emoji in *button
  text* doesn't count against callback_data but bloats the keyboard JSON; keep it lean.
  [seroperson enhanced callback_data](https://seroperson.me/2025/02/05/enhanced-telegram-callback-data/)
- For genuinely complex payloads, base85-encoded protobuf fits more into 64 bytes than
  base64 — overkill for Aura, but the direction of travel.
  [seroperson enhanced callback_data](https://seroperson.me/2025/02/05/enhanced-telegram-callback-data/)

**Aura mapping** (`hitl.go`): the just-fixed bug is the canonical case. `choiceMarkup`
now encodes `token|accept|<index>` and `resolveChoiceValue` maps the index back to the
full option value from `PendingFor`. This is exactly right. Recommendation: make the
"index, never prose" rule a documented invariant for *all* inline buttons in the package,
and add a defensive length assert (`len(data) <= 64`) at `callbackData()` so a future
longer token/action fails loudly in tests rather than live with a 400.

### 3.3 answerCallbackQuery — always answer, answer with feedback
- **Always** call answerCallbackQuery, even with no text, or the client shows a spinner on
  the button for up to 30 s. [wyu inline keyboard UX guide](https://wyu-telegram.com/blogs/444/)
- `text` (0–200 chars) shows as a **toast** at the top of the chat; `show_alert=true`
  shows a **modal** the user must dismiss. Use the toast for confirmations
  ("Confermato ✓"), the alert for things the user must read (a destructive-action
  warning). [aiogram answer_callback_query](https://docs.aiogram.dev/en/latest/api/methods/answer_callback_query.html),
  [core.telegram.org/bots/api](https://core.telegram.org/bots/api)

**Aura mapping** (`bot_dispatch.go: onCallback` / `onCallbackFallback`): Aura now answers
HITL callbacks with short toast text ("Confermato", "Rifiutato", "Annullato") before the
resume turn renders, clearing the spinner and giving immediate feedback. ✅

### 3.4 Keep the prompt un-stuck — edit the keyboard after a tap
- After a selection, **edit the message in place** (editMessageText /
  editMessageReplyMarkup) rather than leaving the old buttons live; an app-like flow
  removes the keyboard or replaces it. [wyu inline keyboard UX guide](https://wyu-telegram.com/blogs/444/)
- To remove a keyboard after selection, edit with an empty reply markup.
  [aiogram answer_callback_query](https://docs.aiogram.dev/en/latest/api/methods/answer_callback_query.html)

**Aura mapping** (`hitl.go`, `bot_dispatch.go`): after a valid HITL callback, Aura now
edits the original prompt markup away immediately. Resolved pauses no longer leave stale
live buttons in chat history. ✅

### 3.5 Choice cardinality & pagination
- For ≤ ~12 options use a single static keyboard; for 13–48 use top-N static + a "More" /
  pagination button (90% of users pick the top 6). [wyu inline keyboard UX guide](https://wyu-telegram.com/blogs/444/)
- Pagination pattern: prev/next buttons that **edit the same message** with the next page +
  a "page X/Y" indicator; close button deletes the message. (See go-telegram/ui paginator,
  §5.) [ksinn pagination](https://github.com/ksinn/python-telegram-bot-pagination)

**Aura mapping**: `choiceMarkup` renders one button **per row** with an Annulla footer —
fine for 2–4 options (the `ask_user` contract). If choice cardinality ever grows, group
into rows of 2–3 (≤8/row hard limit) and paginate past ~10. `/search` (up to 20 hits) is
the real pagination candidate today (§5, backlog).

---

## 4. Status / progress & streaming UX

### 4.1 The "agent is working" indicator
- Send `sendChatAction(typing)` immediately, and **re-send under the ~5 s expiry** for the
  whole long-running task so it never goes silent.
  [mindstudio set typing](https://www.mindstudio.ai/capabilities/telegram-set-typing)
- **Stop it explicitly when done.** A recurring real-world bug class: the typing indicator
  persists forever because the keepalive loop wasn't cancelled on completion.
  [openclaw typing-persists #27177](https://github.com/openclaw/openclaw/issues/27177),
  [openclaw typing-persists #26761](https://github.com/openclaw/openclaw/issues/26761)

**Aura mapping** (`bot_typing.go`): `pulseChatAction` pulses every 4 s and the returned
`stop()` closes `done`, joins the goroutine, and also exits on ctx cancel — goleak-safe,
and it's `defer`-stopped in every handler (`runTurn`, `onVoice`, `onPhoto`, `onDocument`).
This already avoids the persistent-typing footgun. ✅ Telegram also auto-clears the action
when the next message is sent, which the final answer does.

### 4.2 Edit-in-place streaming + throttling
- Best practice for streaming an LLM answer: send the first message once the first tokens
  arrive, then **edit it progressively, throttled** (~every 500 ms or ~50 tokens), with a
  final edit on completion. Throttle edits to ~2–3/s/message to stay well under limits.
  [openclaw streaming #33220](https://github.com/openclaw/openclaw/issues/33220)
- Global ~30 edits/s, but edits share the global counter with sends — throttle.
  [tdlib rate-limit #3034](https://github.com/tdlib/td/issues/3034)
- **Native option (newer):** Bot API 9.5 `sendMessageDraft` streams partial text with a
  native typing animation — smoother than edit-based streaming and avoids the "edited"
  tag. [openclaw streaming #33220](https://github.com/openclaw/openclaw/issues/33220)

**Aura mapping** (`renderer.go`, `status_pane.go`): Aura already does coalesced edit-in-
place on both messages with injectable clocks; content throttle 500 ms, status throttle
1500 ms, per-chat rate 1000 ms — all conformant. The two-message split (status pane vs
content) is a strong pattern: it keeps progress noise out of the final answer. ✅
**Forward option:** behind a flag, swap content streaming to `sendMessageDraft` when the
deployed Bot API server supports 9.5 — smoother UX, fewer edit calls. Track, don't rush.

### 4.3 When to collapse / finalize the status pane
- Progressive disclosure: show live progress while working, then collapse to a compact
  final state so the chat isn't cluttered with intermediate spam.
  [openclaw streaming #33220](https://github.com/openclaw/openclaw/issues/33220)

**Aura mapping** (`status_pane.go`): on `RUN_FINISHED` it keeps a compact final 💭 line,
collapses successful tool work to a one-line summary, and keeps full tool rows on failure
where the ❌ row is diagnostic. ✅

---

## 5. go-telegram/ui widget catalog

Repo: `github.com/go-telegram/ui` (module `go 1.20`, **depends on
`github.com/go-telegram/bot` v1.14.0 — NOT telebot**). Status: pre-v1.0, "API may change".
Source: [github.com/go-telegram/ui readme.md](https://github.com/go-telegram/ui).

**Architectural pattern (shared by all widgets):** each widget, on `New(...)`, calls
`b.RegisterHandler(HandlerTypeCallbackQueryData, prefix, MatchTypePrefix, callback)` with a
random 16-char `prefix`. callback_data is `prefix + command` (e.g. `<prefix>start`,
`<prefix>end`, `<prefix>nop`, or `prefix + nodeID`) — the **prefix-routing** pattern from
§3.2. Every callback first calls `AnswerCallbackQuery` (the §3.3 rule), then **edits the
same message** to advance state. State lives in the widget struct in memory. Crucial
caveat from the README: *"UI components register own bot handlers on init. If you restart
the bot instance, inline buttons in already opened components can't work"* — so use a
**stable `WithPrefix`** instead of the random one if you need buttons to survive a restart.
[github.com/go-telegram/ui readme.md](https://github.com/go-telegram/ui)

| Widget | What it does | Callback / state | Aura verdict |
|---|---|---|---|
| **datepicker** | Calendar to pick a date; custom locale; include/exclude date filters | prefix-routed callbacks edit the message month→month; on-select handler fires with the date | **Skip** (no date-entry surface in Aura today; revisit if scheduler gets a Telegram UI) |
| **inline keyboard** | Helper to build inline keyboard layouts | thin builder over `models.InlineKeyboardButton` | **Already have** equivalent (telebot `ReplyMarkup`) — no need |
| **reply keyboard** | Helper to build reply (custom) keyboard markup | n/a (reply keyboard) | **Skip** for now; consider a persistent reply-keyboard menu later (§7) |
| **paginator** | Paginates a `[]string` with `perPage`, custom separator, prev/start/end/close buttons; "close" deletes the message and unregisters the handler | prefix-routed `start/end/nop/close`; current page in struct; edits same message | ✅ **Ported pattern** → `/search` results pagination with server-side page state |
| **slider** | Carousel of slides (image + text) with prev/next/select | prev/next wrap-around edit the photo+caption; select optionally deletes | **Skip** (no carousel need; photo-grid not a current surface) |
| **progress** | A progress bar message for long tasks; `SetValue(0..100)` edits in place; optional cancel button with `OnCancel`, `deleteOnCancel` | one cancel button = `CallbackData: prefix`; `SetValue` → `EditMessageText`; `Done()` unregisters | ✅ **Ported cancel pattern** → status-pane one-tap cancel; determinate progress remains optional |
| **dialog** | Multi-node menu tree: each `Node` has text + `[][]Button`; a button is either a URL or a `NodeID` that navigates to another node (optionally editing in place when `inline`) | callback_data = `prefix + NodeID`; in-memory node graph; answers callback then edits/sends next node | **Port the pattern** if Aura grows a settings/menu surface (TTS toggle, verbosity, language) |

**Library-vs-pattern verdict:** Aura is on **telebot.v4**; go-telegram/ui targets
**go-telegram/bot**. The two bot frameworks have incompatible handler/markup types
(`models.InlineKeyboardButton` vs `tele.InlineButton`, `RegisterHandler` vs
`bot.Handle`), so **direct use is not viable** — adopting these means **porting the
~50–150-LOC pattern** per widget into telebot idioms, not importing the library. The good
news: the patterns are small and map cleanly onto Aura's existing seams (`hitl.go` already
implements prefix-by-`Unique` routing + answer-callback + index encoding). The three worth
porting are **paginator** (search), **progress** (cancel button / determinate progress),
and **dialog** (future settings menu).

---

## 6. Local-source findings (`D:/tmp`)

Read (pre-vetted): `picobot` (Go agent w/ Telegram channel — closest analog),
`nanobot`, `codex` (docs), `cli-printing-press`. Relevant findings:

- **picobot** (`internal/channels/telegram.go`) — a **negative example / floor**: it does
  raw `POST /sendMessage` over `net/http` with only `chat_id` + `text`. **No** parse mode,
  **no** chat action / typing, **no** inline keyboards, **no** message chunking, **no**
  edit-in-place. It confirms Aura's channel is already far more advanced (status pane,
  streaming edits, HITL keyboards, multimodal, HTML parse mode + fallback). The one transferable
  idea: picobot keeps the channel *dead simple* and pushes all logic to a hub — a reminder
  not to over-engineer the transport. Nothing to copy into Aura beyond that discipline.
- **nanobot / codex / cli-printing-press** — no Telegram UI surface. codex's TUI is a
  terminal renderer (not Bot-API). cli-printing-press generates CLIs for external APIs.
  None contribute Telegram-specific UX patterns. (This matches the local memory note that
  these are curated *agent/CLI* sources, not bot-UI sources.)

Net: the **authoritative reusable pattern source for this task is go-telegram/ui** (§5),
not the `D:/tmp` repos. picobot's value is as a "don't regress to this" baseline.

---

## 7. Prioritized backlog — file-mapped improvements for `internal/channels/telegram/*`

Ordered by impact ÷ effort. Each is a self-contained change.

1. ✅ **Toast feedback on callback** — `bot_dispatch.go: onCallback`.
   Pass `c.Respond(&tele.CallbackResponse{Text: …})` with a per-action confirmation
   ("Confermato"/"Rifiutato"/"Annullato"). *Rationale:* instant visible feedback; spec'd
   ≤200 chars. *Effort: ~1h.*

2. ✅ **Remove/disarm the HITL keyboard on resolve** — `hitl.go` (+ small state in
   `bot_dispatch.go`). Capture the prompt `*tele.Message` in `hitl.send`; on resolve edit
   it to "✅ <label>" / "❌ Rifiutato" / "Annullato" with markup cleared.
   *Rationale:* no stale/ambiguous live buttons; permanent record of the choice.
   *Effort: ~half day* (needs to thread the message handle through the resolve path).

3. ✅ **setMyCommands on Start** — `bot.go` (one call after the bot is built).
   Register the 10 commands from `helpText` so they appear in the "≡" menu and `/`-autocomplete.
   *Rationale:* discoverability; the commands already exist, just unsurfaced.
   *Effort: ~1–2h.* [core.telegram.org/bots/features](https://core.telegram.org/bots/features)

4. ✅ **Chunk long content instead of truncating** — `renderer.go: sendText` / `capRunes`.
   Split >4096-char content on paragraph/sentence boundaries into sequential messages
   (respecting the per-chat 1 msg/s gate already present). *Rationale:* `capRunes` silently
   drops the tail of long answers today. *Effort: ~half day.*

5. ✅ **Paginate `/search`** — `commands.go: search` + `bot_dispatch.go` callbacks (port go-telegram/ui
   paginator pattern into telebot). prev/next inline buttons editing one message,
   `page X/Y` indicator, close button; callback_data = `srch|<page>` (index, not prose).
   *Rationale:* 20 concatenated hits can exceed 4096 and are unreadable. *Effort: ~1 day.*

6. ✅ **Status-pane cancel button** — `status_pane.go` + `commands.go`.
   Add a single "⏹ Annulla" inline button to msg #1 wired to the existing per-chat
   cancel registry (`commands.registerTurn`/`cancel`). One tap = `/cancel`. callback_data
   = `cancel|<chatID-derived-token>`. *Rationale:* one-tap stop for long turns; mirrors
   go-telegram/ui progress cancel. *Effort: ~1 day.*

7. ✅ **callback_data length guard + invariant doc** — `hitl.go: callbackData`.
   Add a defensive `len(data) <= 64` assertion (panic in tests / log+truncate-safe in prod)
   and document "index, never prose" as the package invariant. *Rationale:* turn the live
   bug class into a compile/test-time guard. *Effort: ~1–2h.*

8. ✅ **Adopt parse_mode=HTML for streamed answers** — `html.go`, `renderer.go`.
   Decision: use `github.com/PaulSonOfLars/gotg_md2html` `MD2HTMLV2` for message chunks
   and table captions, send with `tele.ModeHTML`, and retain the plain-text fallback for
   any 400 "can't parse entities" response. *Rationale:* avoids MarkdownV2 backslash noise
   while keeping Telegram-safe formatting and raw-HTML escaping.

9. ✅ **Keep final status-pane detail visible** — `status_pane.go: text/handle`.
   On `RUN_FINISHED`, keep the full tool list and a safe reasoning lifecycle label visible
   when it fits; provider reasoning text is excluded/redacted and never shown raw. *Rationale:*
   final Telegram turns remain auditable without exposing hidden chain-of-thought.

10. ✅ **Close native streaming via sendMessageDraft as gated** — `renderer.go`.
    Decision: keep edit-based streaming. `sendMessageDraft` requires Bot API 9.5 support
    in the deployed server and telebot surface before Aura can use it safely behind a
    config flag. *Rationale:* this is an upstream capability gate, not a local UX defect;
    reopen when the dependency stack exposes the method. [openclaw streaming #33220](https://github.com/openclaw/openclaw/issues/33220)

---

### Sources
- Telegram Bot API reference — https://core.telegram.org/bots/api
- Telegram Bots FAQ (rate limits) — https://core.telegram.org/bots/faq
- Telegram Bot features (commands, deep links, menu) — https://core.telegram.org/bots/features
- Telegram inline keyboard UX guide — https://wyu-telegram.com/blogs/444/
- Inline keyboard builder guide (row/total limits) — https://botnamefinder.com/blog/telegram-inline-keyboard-builder-guide
- MarkdownV2 escape guide — https://botnamefinder.com/blog/telegram-markdownv2-escape-characters
- Enhanced callback_data (protobuf+base85, server-side map) — https://seroperson.me/2025/02/05/enhanced-telegram-callback-data/
- gramio rate-limits guide — https://gramio.dev/rate-limits
- tdlib edit rate-limit discussion — https://github.com/tdlib/td/issues/3034
- aiogram answerCallbackQuery docs — https://docs.aiogram.dev/en/latest/api/methods/answer_callback_query.html
- telegrambots reply-markup guide — https://telegrambots.github.io/book/2/reply-markup.html
- MindStudio set-typing capability — https://www.mindstudio.ai/capabilities/telegram-set-typing
- openclaw: streaming via editMessageText — https://github.com/openclaw/openclaw/issues/33220
- openclaw: typing persists bug — https://github.com/openclaw/openclaw/issues/27177
- go-telegram/ui widget library — https://github.com/go-telegram/ui
- ksinn python pagination — https://github.com/ksinn/python-telegram-bot-pagination
