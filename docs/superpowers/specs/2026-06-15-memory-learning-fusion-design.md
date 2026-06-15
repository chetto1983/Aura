# Memory-Learning Fusion — Design Spec

**Date:** 2026-06-15
**Status:** Approved design (pre-plan). Brainstormed + industrially validated; awaiting writing-plans.
**Author:** Aura / operator session
**Supersedes nothing.** Extends Phase 15 (memory) by changing the *invocation model*, not the engine.

---

## 1. Problem

Aura has two parallel "learning" subsystems that are secretly the same machine, and a memory subsystem that is effectively dead:

- **Adaptive-learning (shipped, push/automatic):** `internal/semindex` (embedding index: `Classifier` Centroid + `Ranker` PerItem) + `internal/activelearn` (the loop: `Observe(text,vec,margin) → margin-gate → Oracle labels → Saver persists → Refresh re-folds`). Two live consumers ride it — reasoning-tier routing (`internal/agent/prompt/reasoning_classifier.go`) and tool-selection (`internal/toolselectlearn`) — each persisting `:ReasoningExample` / `:ToolSelectionExample` to Neo4j via one granite embedder (`internal/documents/embedder.go`).
- **Memory (broken, pull/manual):** the agent-memory MCP (`recipe:memory`, the neo4j-labs fork) is mounted but its tools are **Deferred** (`internal/agent/mcptools/bridge.go:136`) and **agent-decided** (D-01) / **pull-on-demand** (D-03).

### Two confirmed bugs (root-caused this session)

1. **Aura never stores memory about its work *proactively*.** Writes are 100% model-self-initiated against a Deferred tool with no inbound trigger; no production code path ever calls a store tool. The model writes **only when explicitly commanded** ("remember this") — never proactively while doing the user's actual work. **Model strength is not the cause:** the configured model is **DeepSeek-V4 Flash** (`deepseek/deepseek-v4-flash:exacto`, [compose.yaml:59](../../../compose.yaml#L59)), a strong model, and it still never self-initiates a write. (System prompt `<memory>` write guidance at `internal/agent/prompt.go:64` is a contributing but secondary cause.)
2. **Recall is pull-only.** The model must decide to call `memory_search`; it rarely does unless the user's phrasing explicitly triggers it (measured: 4 read calls in 209 turns).

### Empirical validation (live retest, 2026-06-15)
Measured against the running containerized stack (DeepSeek-V4 Flash; agent-memory MCP + Neo4j both healthy):
- **209 turns / 2 conversations / 190 tool calls** of real usage. Memory **writes = 0** (`memory_store_message`/`add_entity`/`add_fact`/`add_preference`/`start_trace` never invoked). Memory **reads = 4** (`memory_search` ×2, `memory_get_context` ×2).
- The Neo4j graph held only Aura's own learning seeds (`:ReasoningExample`, `:ToolSelectionExample`) — **zero** memory-engine content nodes.
- A fresh scenario with an explicit *"remember permanently"* command **did** trigger `memory_add_preference` ×3, which **persisted** to Neo4j with `valid_from` temporal fields. The write path works end-to-end; the model just never exercises it unprompted.

**Conclusion:** the bug is **behavioral/architectural, not model weakness** — a strong model still won't proactively capture durable facts. This is exactly what the deterministic write-back bookend (Element 3) fixes, and it confirms push-recall (Element 2) since pull-recall fires only 4/209.

### Why the engine makes this worse (industrial finding)

The agent-memory engine's `memory_store_message` **stores messages verbatim with no worthiness filter, no ADD/UPDATE/NOOP, no message-level dedup** (verified in the engine source: `short_term.py:558`, entity create with fresh uuid + no dedup at `short_term.py:1248`). Unlike mem0 — whose `add()` runs an internal extraction LLM that drops chitchat — this engine stores whatever it is handed. **Therefore the moment storage becomes automatic (push), a worthiness gate becomes mandatory; the engine provides no NOOP safety net.**

---

## 2. Goals / Non-goals

**Goals**
- Fix both bugs by moving the recall and store decisions **off the weak model** into deterministic, framework-driven bookends around `runner.Turn`.
- Make memory a first-class consumer of the existing `semindex`/`activelearn` substrate where that is the *industrial* role (recall reranking), without re-deriving the engine.
- Match best industrial practice: LLM-extraction write gate (mem0/Zep/Letta norm), hybrid rerank-before-inject (Zep/Hindsight/mem0 norm), temporal fact invalidation (Graphiti norm; Aura PRD pattern #24).

**Non-goals**
- Replacing or retiring the agent-memory MCP engine (it stays the extraction + vector-store + dedup + temporal engine).
- Shipping the learned-classifier write gate or the recalled-and-used reward loop as load-bearing v1 mechanisms — both are a 2025–26 *research* frontier (Learn-to-Memorize, Mem-α), not production. They ship dark behind a flag (Element 4).
- Changing the L1/L2/L2.5 conversation context ladder.

---

## 3. Architecture — turn lifecycle (the fused loop)

The fusion is a thin layer at the `runner.Turn` boundary. The agent loop itself is unchanged.

```
USER INPUT
  │
  ├─① RECALL (push)        embed input → MCP memory_search → candidates
  │                        → HYBRID rerank (RRF over vector+BM25 + recency/graph-boost)
  │                        → MMR diversity → token-budget
  │                        → inject <recalled_memory> as a PROTECTED, NON-CACHED block   [fail-soft]
  │
  ├─② AGENT LOOP           (unchanged; model may still pull more memory mid-turn)
  │
  ├─③ finalize()           answer produced
  │
  ├─④ USED SIGNAL          model-free overlap(answer, injected items) → per-item used/unused   [no LLM]
  │
  ├─⑤ WRITE-BACK GATE      (async, off hot path)
  │      ├─ optional pre-filter: cheap semindex.Classifier drops obvious chitchat   [flagged, Element 4]
  │      └─ AUTHORITATIVE: LLM extraction (mem0 ADDITIVE_EXTRACTION pattern, strong remote model)
  │             → returns [] for filler (the NOOP the engine lacks)
  │             → else extract durable facts → MCP store
  │
  ├─⑥ TEMPORAL             supersession/invalidation: drive engine valid_from/valid_until + SAME_AS
  │                        ("I quit coffee" closes "I love coffee"; never blind-append contradictions)
  │
  └─⑦ REWARD (shadow)      used/unused outcomes → activelearn.Observe(source=reward)
                           → advisory centroid nudge for the pre-filter   [flagged, ships dark]
```

①+⑤ fix the two bugs. ② is unchanged. ④+⑦ are the research-flavored self-tuning (dark by default). ⑥ is the industrial correctness mechanism that was missing from the first draft.

---

## 4. The five elements

### Element 1 — Keep agent-memory MCP as the engine (unchanged)
The engine remains the extraction (NER + optional LLM), vector store, dedup-on-explicit-entity, POLE+O typing, temporal `valid_from/valid_until`, and SAME_AS resolver. We add *learning and orchestration*, not storage.

### Element 2 — Push-recall + hybrid rerank (HIGHEST value, lowest risk)
- At turn start, `runner` embeds the user input (granite, already wired) and calls the engine's `memory_search` automatically (push) — fixing pull-only.
- The engine's search is **pure vector**; we add a local **hybrid reranker**: fuse vector score with **BM25** (reuse `internal/agent/tools/bm25.go`) and a recency/graph-boost signal via **Reciprocal Rank Fusion**, then **MMR** for diversity, then a hard **token budget**. Reference recipe: mem0 `scoring.py` (semantic + BM25 + entity-boost, additively fused). `semindex.Ranker` provides the embedding-rank substrate.
- Survivors inject as a **protected `<recalled_memory>` block** (same protection posture as the messages[1] always-block in `internal/conversations/context.go`).
- Pull-on-demand stays as a complement (the model can still call `memory_search` mid-turn).

### Element 3 — Write-back gate = LLM extraction (the industrial core)
- After `finalize()`, an async bookend (never blocks the turn) decides what to store.
- **The authoritative gate is an LLM extraction step** (mem0 `ADDITIVE_EXTRACTION` pattern) run on the strong remote model (DeepSeek-V4) off the hot path: given the turn (user request + assistant answer + key tool results), extract durable facts/decisions/preferences/entities/corrections, or return nothing for filler. This supplies the chitchat/NOOP filter the engine lacks.
- Extracted facts are handed to the engine's store/`add_*` tools (the engine does the actual write + its own entity dedup).
- The model never has to decide to write. (We may also keep the model's manual store as a complement, but it is no longer the mechanism.)

### Element 4 — Learned classifier pre-filter + reward loop (research, ships dark)
- An optional cheap `semindex.Classifier` (Centroid mode, seeded exemplars: store = preference/decision/entity/correction/reusable-approach; skip = chitchat/derivable/transient) runs **in front of** Element 3 to skip obvious chitchat before paying for the LLM extraction call (cost/latency pre-filter only — never the authority).
- The **recalled-and-used reward** (④) becomes an `activelearn.Observe(source=reward)` that advisorily nudges the pre-filter's centroids over time.
- Both are gated behind **`AURA_MEMORY_LEARNING`** (independent of `AURA_LLM_REASONING_LEARNING`, because memory writes are higher-stakes — fixes the WR-05 "no independent flag" smell). Default off. Explicitly labeled research-frontier, not a correctness dependency. Disabling them degrades cleanly to Elements 2+3+5.

### Element 5 — Temporal supersession / invalidation (the missing industrial mechanism)
- At write-back, when an extracted fact contradicts or updates an existing one, drive the engine's **validity window** (`valid_from`/`valid_until`) and **SAME_AS** resolver to *close* the old fact's window rather than blind-append (Graphiti bi-temporal model; Aura PRD pattern #24).
- Without this, recall faithfully injects stale contradictions ("I love coffee" + "I quit coffee").

---

## 5. Components (following existing conventions exactly)

| New / changed | Mirrors | Responsibility |
|---|---|---|
| `internal/memorylearn` | `internal/toolselectlearn` | Write-back orchestration: the LLM-extraction gate (Element 3), the optional classifier pre-filter + reward observer (Element 4, behind flag). Rides `internal/activelearn`. |
| `internal/memorystore` | `internal/toolselectstore` | Neo4j `:MemoryGateExample` {hash, label store/skip, embedding, source oracle\|reward} for the pre-filter; a small recall-usage log for the reward signal. APOC-JSON embeddings, MERGE by hash. **The actual memories stay in the engine's Neo4j — untouched.** |
| `internal/runner/runner_memory.go` | `runner_persist.go` | Wires ① recall-inject and ⑤+⑥ write-back/invalidation bookends into `Turn`. Fail-soft. |
| recall reranker (in `runner_memory.go` or a small `internal/memoryrecall`) | reuses `semindex.Ranker` + `internal/agent/tools/bm25.go` | Element 2 hybrid rerank/dedup/MMR/budget. |
| `internal/agent/prompt.go` `<memory>` block | — | Recall "provided automatically (`<recalled_memory>`)"; writes "handled automatically after the turn." Removes the broken self-trigger reliance. |
| `internal/config` | — | `AURA_MEMORY_RECALL` (push on/off), `AURA_MEMORY_LEARNING` (Element 4 dark-ship flag). |

File-size discipline (≤600 LOC) and one-concern-per-file apply; split `runner_memory.go` if it grows.

---

## 6. Data flow & persistence

- **Memories:** engine's Neo4j (POLE+O, vector, validity windows, SAME_AS) — unchanged.
- **Gate training examples:** `:MemoryGateExample` in Aura's learning store (same pattern/graph as `:ReasoningExample` / `:ToolSelectionExample`).
- **Recall-usage log:** lightweight record linking a recall event → returned memory ids → which were "used" (④), for the reward signal. Minimal table/edge; not on the hot path.
- **Embeddings:** granite sidecar (`internal/documents/embedder.go`), already wired; the same `Embedder` seam serves recall rerank and the pre-filter classifier.

---

## 7. Error handling, cache, safety

- **Fail-soft everywhere:** MCP or embedder down → skip recall-inject and write-back, warn-log, turn continues (matches `<memory>` "fail-soft"). Recall/write-back never propagate to the loop error slot.
- **Cache discipline (load-bearing):** `<recalled_memory>` is volatile per-turn → it must inject in a **non-cached position** (after the always-block, never in `messages[0]`), per the cache-poisoning-sites guidance. The one `<memory>` prompt edit busts the cached prefix **once** (acceptable; `messages[0]` stays byte-stable thereafter).
- **Write-back is async** — never blocks the turn (like the existing learners' non-blocking `Observe`); failures logged + dropped.
- **Cost control:** Element 3's LLM extraction is one off-hot-path strong-model call per *candidate* store turn; the Element 4 pre-filter (when enabled) suppresses calls on obvious chitchat. Consider per-conversation-end batching vs per-turn as a tuning knob (default: per-turn with pre-filter).
- **No secrets** in stored memories beyond what the engine already handles.

---

## 8. Increments (one slice each)

1. **Push-recall + hybrid rerank** (Elements 1–2 + prompt recall line). Highest value, no LLM-gate needed, fixes pull-only immediately. Independently shippable.
2. **LLM-extraction write-back gate + temporal invalidation** (Elements 3 + 5 + `memorylearn`/`memorystore` skeleton + prompt write line). Fixes never-stores; the industrial core.
3. **Optional classifier pre-filter + shadow reward loop** (Element 4, behind `AURA_MEMORY_LEARNING`). Research-flavored; ships dark; deferrable without breaking 1–2.

---

## 9. Testing

- **Unit:** hybrid rerank (RRF fusion + MMR + token-budget), `memorystore` MERGE/load, used-detection overlap, the LLM-extraction gate's NOOP-on-chitchat (mocked oracle), temporal supersession logic.
- **Integration** (`memory_integration` build tag, live engine, **no-skip-as-green** — `t.Fatal` under `$CI` when env unset, per CLAUDE.md):
  - `TestMemoryAutoRecall` — push injects a seeded memory without the model self-triggering.
  - `TestMemoryWriteBackGate` — a turn with a durable fact stores it; a chitchat turn stores nothing.
  - `TestTemporalSupersession` — a contradicting fact closes the prior fact's validity window.
  - `TestRecallUsedReward` (Element 4) — a used recall nudges a `:MemoryGateExample`.
  - Existing `TestMemoryLoopRecall` (pull path) stays green.
- Coverage floor 85% on owned surface (CLAUDE.md).

---

## 10. Industrial evidence (why this shape)

- **Write gate = LLM, not learned classifier:** every production system (mem0, Zep/Graphiti, Letta/MemGPT, Anthropic memory tool, ChatGPT bio) decides via an LLM; learned write-gating is research-only (Learn-to-Memorize arXiv 2508.16629, Mem-α 2509.25911). Sources confirmed online.
- **Engine stores verbatim (no NOOP):** agent-memory `short_term.py:558`/`:1248` — gate mandatory once push.
- **Hybrid rerank-before-inject:** mem0 `scoring.py` (semantic+BM25+entity-boost), Zep (RRF/MMR/cross-encoder), Hindsight 2025 (RRF→cross-encoder→budget). agent-memory's own search is pure-vector → local rerank is the high-value add.
- **Push-recall for reliability:** Hindsight (2025 production) chose auto-injection *because models call search tools inconsistently* — same failure mode as Aura's small model. Letta core blocks, LangMem procedural, ChatGPT saved memories all push.
- **Temporal invalidation:** Graphiti bi-temporal edges are the industrial mechanism for stale-fact handling; agent-memory exposes the primitives; aligns with Aura PRD pattern #24.

---

## 11. Open questions (for plan phase)
- Write-back trigger granularity: per-turn-with-pre-filter (default) vs per-conversation-end batch (cheaper, more mem0-like)? Decide empirically.
- Recall budget: how many tokens of `<recalled_memory>` to inject before it competes with the L2 budget? Start conservative (~5 items / small budget), tune.
- Used-detection precision: lexical overlap vs embedding overlap for the model-free "used" signal (④) — start lexical (cheapest), measure.
