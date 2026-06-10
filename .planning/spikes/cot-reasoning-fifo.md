# Spike / Design — CoT reasoning FIFO (perceived-latency display)

> Status: **DESIGNED, not implemented** (deferred 2026-06-10 to finish Phase 19 first).
> Origin: operator observed end users ask "perché Aura è lenta?" during the 30–60 s reasoning
> wait. Directive: show the CoT in a rolling **4096 UTF-8-char FIFO**, cleared **on each turn**
> and **on the final answer**. NOT a Phase-19 finding.

## Problem

A DeepSeek-V4 turn spends 30–60 s generating reasoning + running the tool loop. During that wait:
- **CLI `aura chat`** already streams the live CoT (`cmd/aura/chat_render.go:153 renderReasoning`, dim
  `💭`, amendment #57e) — but **unbounded, append-only, never cleared**.
- **Telegram** (the end-user surface) shows only `💭 in corso → completato`
  (`internal/channels/telegram/status_pane.go:112-127`) — the **actual reasoning text is redacted**
  (recorded to `internal/reasoningtrace`, never displayed). So the user sees a static "in corso" through
  the whole wait — exactly the "all thinking dumped at the end → feels slow, no feedback" failure mode.

## Research — how other repos solve it (D:/tmp + web)

**codex (OpenAI CLI, `D:/tmp/codex`) — the reference:**
- Does NOT dump raw reasoning; keeps `reasoning_buffer`, shows a 1-line shimmer header (first `**bold**`
  token) + a 3-line tail-ellipsized status row (`tui/src/status_indicator_widget.rs`,
  `STATUS_DETAILS_DEFAULT_MAX_LINES=3`). Truncation is **rune/codepoint-based** (`.chars().take(n)`).
- **Clears on new turn** (`tui/src/chatwidget/turn_runtime.rs on_task_started`) + **on reasoning-final**
  (`tui/src/chatwidget/streaming.rs:214`) + section-break (`:229`).
- Its real char-FIFO `RingBuffer{max, VecDeque<u8>}` (`feedback/src/lib.rs:291`, 4 MiB, tail-keep /
  front-evict) is **byte-granular and intentionally NOT UTF-8-safe** — it backs a *log dump, not a display*.
- `HeadTailBuffer` (`core/src/unified_exec/head_tail_buffer.rs`, 1 MiB) keeps head+tail, drops the middle.

**odysseus / nanobot / assistant-ui:** reasoning shown collapsibly, **bounded by viewport**
(`max-h-52`/`max-h-64` + auto-follow-bottom), full text retained; all pair it with a **spinner +
elapsed timer + tool-aware labels** ("Searching…", "Thought for 4s").

**Takeaways:**
1. Nobody char-caps the *visible* reasoning at a fixed char count — they summarize + scrollback, or cap the
   viewport. A 4096-**rune** tail window is a reasonable chat-bubble middle ground.
2. **Cap by rune, not byte**; trim on a char boundary (the one byte-FIFO is non-grapheme-safe *because*
   it's never displayed). Tail-keep / front-evict (most-recent reasoning visible).
3. Clear-on-turn + clear-on-final are both standard (codex does exactly this).
4. **Biggest perceived-latency win = spinner + elapsed timer**, more than the reasoning text itself.

Sources: codex#5339 (stream thinking live), codex#2756 (show CoT), Oracle "Agent Reasoning: The Thinking
Layer" (blogs.oracle.com/developers), codex TUI sources above.

## Design

New `internal/reasoningfifo` — a rune-capped rolling buffer (no channel/CLI coupling):
```go
type FIFO struct { max int; runes []rune } // max = 4096
func New(max int) *FIFO
func (f *FIFO) Push(delta string)   // append runes; if len>max, drop from FRONT to max (tail-keep)
func (f *FIFO) String() string      // current window
func (f *FIFO) Reset()              // clear (on turn-start + on final answer)
```
Rune-safe by construction (operates on `[]rune`). Reuse `reasoningtrace.RuneLen` for accounting.

**Wiring:**
- **Telegram** (`status_pane.go`): replace the `thinking="in corso"` placeholder with a `*reasoningfifo.FIFO`;
  `ReasoningMessageContentEvent` → `fifo.Push(e.Delta)` and render the last 4096 runes in the `💭` line
  (already edited-in-place + throttled to `AURA_TELEGRAM_STATUS_THROTTLE_MS` — a natural rolling region).
  `ReasoningStartEvent`/new turn → `fifo.Reset()`; `RunFinishedEvent`/`ReasoningEndEvent` (final answer)
  → `fifo.Reset()` (keep a compact "completato"). Respect existing redaction policy — if reasoning must
  stay redacted on Telegram for some models, gate the surfacing behind a config flag
  (`AURA_TELEGRAM_SHOW_REASONING`, default per privacy decision).
- **CLI** (`chat_render.go renderReasoning`): bound + clear — either render an in-place ANSI region (cursor
  save/restore, clear-line) showing the FIFO window, or keep the dim stream but `Reset()` on turn + final.
- **Elapsed timer:** add `· {Ns}` to the status line (Telegram pane footer + CLI), updated on the throttle
  tick — the cheapest perceived-latency win.

**Caps as env:** `AURA_REASONING_FIFO_RUNES` (default 4096).

## Real-latency note (separate from this)

This is *perceived* latency. The real 30–60 s is DeepSeek-V4 reasoning-token generation + the tool loop +
per-round LLM latency. A separate profile (instrument each LLM round + tool call wall-clock on a live turn)
is needed to actually cut it — deferred; offered as the "profile real latency" option.

## Verification

1. `aura serve` + a Telegram turn that reasons (e.g. a multi-step question). Confirm the `💭` line shows
   rolling reasoning text (not static "in corso") that stays ≤4096 runes, and resets when the answer lands.
2. Rune-safety: feed deltas with emoji/accents straddling the 4096 boundary; assert no mojibake (the FIFO
   never splits a rune). Unit-test `reasoningfifo` with multibyte boundaries + front-eviction.
3. CLI: confirm the reasoning region clears on turn + on final answer (no unbounded scrollback growth).
4. Elapsed timer increments during the wait.
