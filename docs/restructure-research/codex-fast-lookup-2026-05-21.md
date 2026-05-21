# Codex (OpenAI Codex CLI) — Fast Lookup Latency Study

Date: 2026-05-21
Target: how does codex avoid the N-round-trip latency Aura is paying on simple lookups?
Source tree: `D:/tmp/codex/codex-rs/`

---

## TL;DR

Codex has no "classifier → direct respond" fast path; it goes through the same agent loop for everything. The wins are **structural latency hiding**: (a) WebSocket prewarm so the first tool call doesn't pay TLS+TCP setup, (b) **stream-time tool dispatch** — the moment an `OutputItemDone` arrives over the LLM stream the tool future is `push_back`'d into a `FuturesOrdered` while the same stream keeps flowing, so the next text/tool overlaps the previous tool's I/O, and (c) prompt rules that explicitly tell the model "skip planning on single-step queries" + "parallelize independent reads" rather than fanning into iterative tool calls. The model can also emit text-then-tool-then-text inside one sampling response so the LLM round can both answer AND verify in one trip.

---

## HOW codex hits low latency on lookups

### 1. WebSocket prewarm hides connection setup (~hundreds of ms)
- `codex-rs/core/src/session_startup_prewarm.rs:174` — `schedule_startup_prewarm` runs at session boot; opens the model WebSocket + builds the tool spec + base prompt BEFORE the user even submits.
- `codex-rs/core/src/tasks/regular.rs:60` — first user turn calls `consume_startup_prewarm_for_regular_turn`; if ready, the `ModelClientSession` is reused (`turn.rs:139`), so turn 1 pays zero handshake cost.
- Telemetry tracks this explicitly: `STARTUP_PREWARM_AGE_AT_FIRST_TURN_METRIC`.

### 2. Stream-time tool dispatch (the big one)
- `codex-rs/core/src/session/turn.rs:1750` — `let mut in_flight: FuturesOrdered<...>` holds tool futures.
- `turn.rs:1813` — on every `ResponseEvent::OutputItemDone(item)` event from the LLM stream, `handle_output_item_done` is invoked.
- `stream_events_utils.rs:374` — if the item is a tool call, a `tool_future` is built and at `turn.rs:1891` immediately `in_flight.push_back(tool_future)`.
- The `loop` then continues consuming the SAME stream — the LLM keeps emitting reasoning/text/more tool calls while the previous tool's I/O is running in parallel.
- `turn.rs:2164` — `drain_in_flight` only joins the futures AFTER the stream's `Completed` event.
- Practical effect: a turn that reads file foo + file bar + answers can fire both reads concurrently AND start answering, all inside a single `run_sampling_request` invocation.

### 3. Parallel-safe tool annotation drives the runtime
- `codex-rs/core/src/tools/parallel.rs:88` — `tool_supports_parallel` is read per-call; if true, the runtime takes a `RwLock::read` so multiple parallel tools share the lock, else `RwLock::write` (serial).
- `view_image.rs:77`, `unified_exec/exec_command.rs:87`, `mcp_resource/*.rs` all return `true`. Shell with side effects (`shell_command.rs:144`) gates parallel on safety options. Read-only tools default parallel-safe.

### 4. Prompt explicitly forbids planning the trivial case + mandates parallel reads
- `core/gpt_5_2_prompt.md:40` — "Do not use plans for simple or single-step queries that you can just do or answer immediately."
- `core/gpt_5_2_prompt.md:252` — "Parallelize tool calls whenever possible — especially file reads, such as `cat`, `rg`, `sed`, `ls`. Use `multi_tool_use.parallel`."
- `protocol/src/prompts/base_instructions/default.md:39` — preamble exception: "Avoid adding a preamble for every trivial read (e.g., `cat` a single file)."
- `core/gpt_5_2_prompt.md:164` — "skip heavy formatting for single, simple actions."
- Net effect: for "what's at line 42 of foo.py?" the model is pushed toward a single `sed -n '42p' foo.py` call, returning the answer text in the SAME sampling response, not iterating.

### 5. Single tool primitive (unified_exec / shell) collapses many would-be tool calls
- `core/src/tools/handlers/unified_exec/exec_command.rs` — one tool, shell-shaped; lookups (head/sed/rg/wc/cat) are all sub-cases inside ONE call. Aura's split between `read_source` / `search_memory` / `read_memory` etc. forces extra dispatch hops; codex's design lets `rg -n 'foo' bar.py` answer a lookup in one tool turn.

### 6. Reasoning effort tunable per-turn (operational, not auto-classified)
- `core/src/session/turn.rs:1741` — `turn_context.reasoning_effort` is passed into `client_session.stream(...)`. The effort value is set per-turn-context, can be lowered to `Minimal` (e.g., `awaiter.toml:2` = `"low"`), but there is no automatic per-query classifier — it's an operator/agent-config choice.

### What codex does NOT do
- No query classifier ("is this a lookup?") — confirmed: nothing in `core/src/` matches direct-respond/fast-path heuristics.
- No skip-the-LLM cache for "trivial" asks.
- No speculative pre-execution of likely tools before the LLM names them. (`session_startup_prewarm` prewarms the *connection*, not specific tool predictions.)

---

## Patterns Aura should adopt

1. **Stream-time tool dispatch.** Right now Aura buffers the whole LLM streaming response then enters a tool loop. Mirror codex's `FuturesOrdered`: the moment `Stream()` emits a complete tool-call fragment, fire the tool goroutine and keep reading the stream. A lookup that needs `read_source` + answer can have the read started while the LLM is still emitting the reply text — 200-400ms saved per tool on a typical turn.
2. **Per-tool `SupportsParallel()` flag honored by the dispatcher.** Today Aura runs "independent tool calls in same turn in parallel" but only after the LLM round completes. Combine with #1 so reads stack across the stream boundary, not just within one round.
3. **WebSocket / HTTP/2 prewarm of the embeddings + LLM endpoints at session start.** Aura's first agent message currently pays a fresh TLS handshake; a 1-RTT prewarm at telegram-bot startup eliminates that for the first turn. `aura-init-models` is the natural host.
4. **System-prompt anti-iteration rule.** Add an explicit clause: "for single-fact lookups, emit the read tool AND the final assistant message in the same response — do not wait for the tool result before composing the answer template." (codex's `gpt_5_2_prompt.md:40` + `:252`). The LLM can call the tool and produce text in the same sampling round; Aura's prompt currently encourages "call tool, then in next round answer".
5. **Collapse lookup-shaped tools into one shell-like primitive.** Aura's 4 source/memory tools (`search_memory`, `list_memory`, `read_memory`, `read_source`) force the LLM to dispatch-then-pick; codex's `unified_exec` lets one call (`sed -n '42p' file`) be the entire ground-truth fetch. Even just adding a single `wiki_grep(pattern, path)` would cut the typical 4-tool lookup chain to 1.

## Anti-patterns to avoid

1. **Don't build a query-type classifier.** Codex deliberately doesn't have one. Classifier latency (50-200ms) + miss-cost (wrong fast-path → reroute through full agent loop = +1 LLM call) typically erases the wins on a 4s budget. Lean on prompt + stream-overlap.
2. **Don't prefetch / speculatively execute tools the LLM hasn't asked for.** Tempting on lookups ("user asked about line 42, pre-read the file"), but it costs latency on the misses, leaks intent, and creates phantom tool-result entries in the archive. Codex prewarms *the connection*, never the *tool*.

---

## Cross-ref vs prior codex output-discipline study

- `codex-output-discipline-2026-05-21.md` covers the *shape* of the final assistant message (brevity, file-ref format, no inline citations, markdown structure rules). That is **complementary** to this study: output discipline reduces decode tokens (less text to stream → lower wall-clock), this study reduces tool-loop round-trips.
- Together they explain codex's perceived snappiness: prewarm + stream-time dispatch + parallel reads collapse the *tool* axis, while output discipline collapses the *decode* axis. Aura should target both — fixing only one leaves the other as the bottleneck.
- The `core/gpt_5_2_prompt.md:40` ("don't plan single-step queries") rule sits at the seam: it's *both* an output-discipline rule (no plan tool call) and a latency rule (no plan = no extra sampling round). Worth lifting verbatim.
