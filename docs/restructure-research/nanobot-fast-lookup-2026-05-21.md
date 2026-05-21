# nanobot fast-lookup study — 2026-05-21

Source: `D:/tmp/nanobot` (local snapshot). Focus: how nanobot answers "what's X" in ≤1 LLM round-trip without dispatching N tool-calls.

## TL;DR

nanobot has **no fast-path classifier** and **no tight step budget** (cap is 200, `config/schema.py:124`). Latency wins come from three structural choices that make the LLM emit 0-tool responses on lookups: (1) **MEMORY.md is inlined into the system prompt every turn** (`context.py:50-52`, `memory.py:229-231`) so "what's X" about a known fact has the answer already in context — no `search_memory` call needed; (2) **streaming is on by default whenever the transport supplies `on_stream`** (`runner.py:633-649`, `loop.py:884-916`) so first token arrives ~200-400 ms after the model starts; (3) the prompt is **explicitly single-shot biased** — `identity.md:31-32` + `SOUL.md:8,15`.

## HOW nanobot avoids round-trip thrashing

### 1. Memory is in the prompt, not behind a tool

`agent/context.py:37-76` `build_system_prompt()` concatenates **before any tool call**:

- `identity.md` (workspace path map)
- bootstrap files (`SOUL.md`, `USER.md`, `TOOLS.md`, `AGENTS.md`)
- **the full MEMORY.md body** via `memory.get_memory_context()` → `memory.py:229-231` returns `f"## Long-term Memory\n{long_term}"` raw.
- Always-skills, recent history, archived summaries.

Capped at `_MEMORY_FILE_MAX_CHARS = 32_000` (`memory.py:872`). Result: when the user asks "what's my favorite editor?" the answer is already in the system prompt — the LLM emits a 1-iteration text response. Aura today goes `search_memory → read_memory → reply` which costs 3 LLM round-trips minimum.

### 2. Streaming by default, no false serialization

`runner.py:623-649` chooses streaming whenever the hook reports `wants_streaming()`. `progress_hook.py:56-57` returns `True` iff an `on_stream` callback was supplied. `loop.py:884-916` shows the Telegram/Feishu path **always** wires `on_stream`/`on_stream_end` when `msg.metadata["_wants_stream"]`. Anthropic provider streams via `messages.stream()` + 30s idle timeout (`anthropic_provider.py:582-636`). The user sees the first token *while* the LLM is still computing — wall-clock to first-paint is ~hundreds of ms, not ~seconds.

The progressive-delta path at `runner.py:650-678` is the fallback for non-streaming providers and still pushes incremental content via `spec.progress_callback`.

### 3. System-prompt single-shot pressure

Three short rules, three different files, no redundancy:

- `templates/agent/identity.md:31` — "Reply directly with text for the current conversation. Do not use the 'message' tool for normal replies in the current chat."
- `templates/agent/identity.md:32` — "When you need to call tools before answering, do not include the final user-visible answer in the same assistant message as the tool calls. Wait for the tool results, then answer once."
- `templates/SOUL.md:8,15,19` — "Keep responses short unless depth is asked for." + "Act immediately on single-step tasks — never end a turn with just a plan or promise." + "When information is missing, look it up with tools first. Only ask the user when tools cannot answer."

Effect: the model is told "answer in one shot, and if you do need a tool, do exactly one round."

### 4. Parallel tool execution when tools ARE needed

`runner.py:307-396` + `runner.py:742-777`: when the LLM emits multiple `tool_calls` in the same assistant message, `_execute_tools()` runs them via `asyncio.gather()` (`runner.py:752-759`, `concurrent_tools=True` set in `loop.py:759`). Batching is by `_partition_tool_batches`. So a "lookup X then summarize Y" turn that issues 2 tool-calls together pays MAX(tool_a, tool_b), not SUM. Aura already does this — but only when the LLM happens to bundle calls; nanobot reinforces the bundling via the prompt rules above.

### 5. Single-verb tool surface kills decision-tree thrashing

Cross-ref to prior zone-map study: nanobot ships ~14 builtins with **one tool per verb** (`read_file`, `write_file`, `edit_file`, `grep`, `web_search`, `web_fetch`, ...). For a lookup query the LLM doesn't iterate `try search_memory → try list_memory → try read_memory → try search_skill → finally answer`. It picks `grep` over `read_file` because `identity.md:27` says so, then stops. Aura's 30+ tool surface with 3 overlapping memory verbs is *itself* the cause of multi-call thrashing.

## 3-5 patterns Aura should adopt

1. **Inline the wiki TOC + MEMORY.md into the system prompt.** Mirror `context.py:50-52`. For a 32 KB cap, that's ~80-200 wiki page titles + a short user-facts block. Result: trivial lookups never trigger a tool. Cost: +32 KB context per turn (negligible at modern context windows).

2. **Turn streaming ON by default for all Telegram replies.** Mirror `loop.py:884-916`. Aura already has progressive Telegram edits (~600 ms throttle) but the LLM call itself often blocks. Wire `on_stream` end-to-end so the user sees tokens at ~300 ms, not at end-of-turn.

3. **Single-shot prompt rules.** Add 3 lines to `AGENT.md` or `SOUL.md`:
   - "Reply directly. When the answer is already in your context, do not call tools."
   - "Act immediately on single-step tasks — never end a turn with just a plan or promise."
   - "Keep responses short unless depth is asked for."
   Reference: `identity.md:31-32` + `SOUL.md:8,15`.

4. **Collapse memory verbs to one tool.** `search_memory` + `list_memory` + `read_memory` → either drop entirely (if MEMORY.md inlined per #1) or merge into one `memory` tool with a `mode={search|list|read}` param. The LLM iterates because adjacent tools look interchangeable. See zone-map study patterns #5.

5. **Parallel-by-default tool dispatch reinforced by prompt.** Aura's loop already parallelizes via `asyncio` (CLAUDE.md "Independent tool calls in the same turn execute in parallel"). Add a prompt line: "When you need multiple independent lookups, emit all tool calls in one assistant message." Reference: `runner.py:752-759` + `loop.py:759` `concurrent_tools=True`.

## Anti-patterns

1. **The 200-iteration cap is a footgun, not a strategy.** `config/schema.py:124` lets a misbehaving model burn 200 LLM round-trips before stopping. Aura's `AURA_AGENT_LOOP_MAX_STEPS=8` default is the right magnitude — do NOT raise it copying nanobot. nanobot survives only because prompt rules push the model toward 1-3 iterations in practice.

2. **No query classifier / fast-path detector.** nanobot has neither a "trivial query" classifier nor a separate one-shot endpoint. Building one for Aura is tempting but unnecessary if patterns #1-#3 above ship — the model itself becomes the classifier when the answer is already in context.

## Cross-ref vs nanobot zone-map study (2026-05-21)

The zone-map study identified: (a) one filesystem path = the zone declaration in the prompt; (b) verb-to-tool routing lines; (c) one tool per verb; (d) MEMORY.md as a prompt zone, not a tool zone. This fast-lookup study confirms those same primitives are what *also* deliver low latency — the system-prompt-as-knowledge-base is dual-purpose. The two studies agree: shrink the tool surface, expand the system prompt with the data, and the LLM stops thrashing on its own.
