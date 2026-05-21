# Elysia Fast-Lookup Pattern — How It Avoids Multi-Round Latency

Date: 2026-05-21
Source repo: `D:/tmp/elysia/` (Weaviate-backed agentic RAG, Python+DSPy)
Cross-ref: `docs/restructure-research/elysia-zone-map-2026-05-21.md` (same repo, zone-map analysis).

## TL;DR

Elysia hits low lookup latency with **two structural tricks**: (1) the `text_response` (a.k.a. `FakeTextResponse`) tool has the answer **embedded as a `function_inputs.text` string written by the decision LLM itself in the SAME routing call**, so conversational replies cost exactly **one LLM round-trip total** (`tools/text/text.py:165-209`); (2) hard `recursion_limit=5` decision-tree cap (`tree/objects.py:626-629`, `tree/tree.py:152`) + `end_actions` self-termination flag on every Decision (`tree/prompt_templates.py:136-144`), so the model is forced to declare completion or be cut off. There is **no streaming**, **no parallel collection fan-out** (the multi-collection loop in `_handle_search` is sequential, `tools/retrieval/util.py:597-642`), and **no fast-path classifier** — the speedup is entirely architectural: trivial replies don't traverse the search branch at all.

## How Elysia avoids round-trip thrashing

### 1. `text_response` is a one-shot exit tool — the LLM writes the answer as a tool input

`tools/text/text.py:165-209` — `FakeTextResponse`:
```python
inputs = { "text": { "type": str, "description": "...DIRECT response to the user..." } }
end = True
# __call__ just does: yield Response(text=inputs["text"])
```

The routing LLM (`DecisionPrompt`, `tree/prompt_templates.py:5-144`) outputs both `function_name="text_response"` AND `function_inputs={"text": "<the actual answer>"}` in the SAME decision call. The tool then yields `inputs["text"]` verbatim — no second LLM call to "generate the response". For a conversational lookup like "what is X?", total LLM cost = **1 decision call**.

Contrast: `final_text_response` / `cited_summarize` (lines 22-75, 131-162) — these DO take a second LLM call, but the LLM picks them only when retrieved data must be summarised. The routing prompt explicitly steers trivial questions to `text_response`: "use this to answer conversational questions not related to other tools" (`tools/text/text.py:170-178`).

### 2. Hard recursion cap — `recursion_limit=5` (default), surfaced in the prompt

`tree/objects.py:626-629` sets `self.recursion_limit = 3` if unset, but `tree/tree.py:152` overrides to `5`. The `tree_count` ("X/Y" string) is rendered into EVERY decision prompt (`tree/prompt_templates.py:34-41`):
> "Consider ending the process as X approaches Y."

The model sees pressure to terminate. At line 808-812 of `tree/objects.py`, the last iteration triggers a final-iteration banner that escalates the urgency.

### 3. `end_actions: bool` output field — model self-terminates per decision

`tree/prompt_templates.py:136-144` — every DecisionPrompt requires the model to output `end_actions: bool`. If True, the loop breaks at `tree/tree.py:1625-1631`:
```python
completed = (
    self.current_decision.function_name == "text_response"
    or self.current_decision.end_actions
    or self.current_decision.impossible
    or self.tree_data.num_trees_completed > self.tree_data.recursion_limit
)
```
Four independent termination conditions, all OR'd. The LLM doesn't need to pick a leaf tool to end — it can `end_actions=True` mid-tree.

### 4. `run_if_true` hardcoded shortcut — bypass decision LLM entirely

`tree/tree.py:1546-1565` + `objects.py:156-187` — a tool can implement `run_if_true(tree_data, ...) -> (bool, dict)` and be invoked **before** the decision LLM runs. If True, the tool fires automatically with the returned inputs. **Zero LLM cost for that step.** No tool in the default tree uses it, but the hook is built-in for "if this condition is met, just run it".

### 5. Multi-collection in ONE Weaviate call

`tools/retrieval/util.py:559-565` — `execute_weaviate_query` takes `target_collections: list[str]` and `_handle_search` (line 568-644) loops collections sequentially. Importantly the **query-planning LLM call is ONE call covering N collections** (`tools/retrieval/query.py:265-335`) — not N separate planning calls. The Weaviate I/O is sequential per-collection but the LLM cost is fixed at 1 plan call regardless of collection count.

### 6. No streaming

`Tree.async_run` (`tree/tree.py:1525+`) is an `AsyncGenerator` that yields **structured events** (`Response`, `Status`, `TreeUpdate`, `Completed`), not token-streams. Each `Response(text=...)` is a complete string from one `aforward()` call. DSPy's `aforward` doesn't stream tokens by default — Elysia waits for the full LLM response before yielding.

## Patterns Aura should adopt

### A. Make `final_response` carry the answer as a tool argument (highest ROI)

Currently Aura's agent loop forces: LLM picks tool → tool returns data → LLM picks `done` or continues → eventually LLM writes a separate response. That's 2-4+ LLM round-trips even for "what is X?".

Lift `FakeTextResponse` pattern: define a `final_response` "tool" whose `text` argument IS the answer. The first LLM call that picks `final_response{text: "..."}` ENDS the turn — `internal/agent/loop` short-circuits, never makes a second call.

Ref: `tools/text/text.py:165-209`. Aura today has streaming Telegram edits (per `internal/channels/telegram/outbound.go`), so this gives 1-call answers with progressive text.

### B. Hard per-turn LLM-call cap with the count rendered into the prompt

Aura's `AURA_AGENT_LOOP_MAX_STEPS=100` (`internal/config/config.go:121`) is effectively no cap. Elysia caps at 5 AND tells the model `"X/Y, consider ending as X approaches Y"`. Drop Aura's cap to ~5 for chat turns (not background jobs), inject `loop_step: "2/5"` into the system prompt, and the model self-paces.

Ref: `tree/objects.py:808-812`, `tree/prompt_templates.py:34-41`.

### C. `end_turn` boolean output, not "pick a done-tool"

Every Aura tool call should let the model optionally set `end_turn=true` in the same response — agent loop exits without another round. Today Aura relies on "model stops emitting tool_calls" which costs an extra LLM call to discover.

Ref: `tree/prompt_templates.py:136-144`, `tree/tree.py:1625-1631`.

### D. `run_if_true` hardcoded shortcut hook

For deterministic triggers (e.g., URL in user message → fetch it, file attachment → ingest it), let tools self-fire BEFORE the LLM routes. Saves one decision round-trip when the input is unambiguous.

Ref: `objects.py:156-187`, `tree/tree.py:1546-1565`.

### E. Fold multi-zone search into ONE planning LLM call

Aura today separately invokes `search_memory`, `search_wiki`, `search_workspace` — each is a tool call costing a round-trip. Elysia plans ONE query LLM call that takes `collection_names: list[str]` and the Weaviate engine fans out internally. Aura mirror: single `retrieve(zones=[...], query=...)` tool whose handler loops zones in-process. Already aligned with Pattern 1 from `elysia-zone-map-2026-05-21.md` — they reinforce each other.

Ref: `tools/retrieval/query.py:54-71`, `tools/retrieval/util.py:559-565`.

## Anti-patterns to avoid

### A. Don't add a "query classifier" LLM as a fast-path gate

Tempting answer: "classify the prompt with a tiny LLM, route trivial Qs past the agent". Elysia explicitly does NOT do this — adding a pre-router would be one more LLM call, exactly the cost they avoid. The fast-path is **in the same decision call** via `text_response{text=...}`, not a separate classifier. A classifier helps only if it can be a regex/embedding (sub-50ms); a mini-LLM costs ~1500ms on CPU per Aura's prior tests (`feedback_minillm_cpu_not_viable_for_tool_retrieval`).

### B. Don't try parallel collection fan-out

`_handle_search` (`tools/retrieval/util.py:597-642`) is a sequential `for collection in collections`. Elysia accepts that latency because Weaviate per-collection queries are <200ms each. For Aura's Qdrant + SQLite + workspace, parallel fan-out via `errgroup` is fine BUT it doesn't fix the dominant cost: the LLM rounds. Optimise tool-call count FIRST, fan-out SECOND.

## Cross-ref vs zone-map study

The zone-map study identified Elysia's **data-zone routing** as the key lift (one tool with `collection_names` param + runtime `data_information` metadata). This study identifies the **latency lift** as orthogonal: cap recursion + `text_response` carrying the answer + `end_actions` self-termination. Both layers cooperate — the small tool surface (7 leaves) means the decision LLM has fewer options to weigh per round, so each round is faster AND more decisive. Adopting BOTH gives Aura: fewer tool decisions per turn (zone-map) × fewer turns per question (this study) = multiplicative latency cut.

Estimated combined effect for "what is X?" type prompts: Aura today 11-42s × (1 decision call only) ÷ (4-12 calls today) → **2-5s ceiling** is plausible if Aura's per-call latency is ~2-4s on its current LLM.

---

Counts/refs verified by reading: `tree/tree.py:140-300, 1500-1820`, `tree/prompt_templates.py` (entire, 265 LOC), `tree/objects.py:570-815`, `tools/text/text.py` (entire), `tools/retrieval/query.py:1-120, 200-340, 440-560`, `tools/retrieval/util.py:512-645`, `objects.py:130-220`. Aura cap source: `internal/config/config.go:17, 121`.
