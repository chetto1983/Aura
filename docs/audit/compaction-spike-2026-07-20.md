# Compaction SPIKE — wire vs. remove (ADR + go/no-go)

Spike date: 2026-07-20
Gates: consolidated-fix-plan-2026-07-20.md items **0.1** (scope), **1.9** (compaction wiring), **2.3** (compaction UI). This spike is the one hard ordering dependency for Wave 1/2.
Decision stance (locked pre-spike): **default-to-REMOVE**, burden of proof on keeping.

---

## 0. Reconciliation with PRD amendment #21 (correction — read after first draft)

**A first draft of this ADR mis-framed Aura as "betting everything on transcript compaction." That is wrong.** Reading `prd.md` (never suppose) surfaced **amendment #21 — the 5-layer context-rot policy**, which is already the authoritative, largely-shipped, provider-agnostic anti-rot architecture. It maps onto the "industrial layers" below and is confirmed wired in code:

- **L0** KV-cache discipline (byte-stable `messages[0]`).
- **L1** Microcompact tool-result eviction — `internal/conversations/context.go:applyL1`, invoked per turn via `LoadManagedHistory → applyContextLadder`. **= "Layer A" below. Already shipped + wired. NO LLM call.**
- **L2 / L2.5** Budget trim + picobot hard rolling buffer (drop oldest pairs) — same ladder. **= "Layer D". Shipped.**
- **Sidecar** >4KB tool output → `$AURA_RUN_DIR/...result` + preview (`read_tool_output`). **Also "Layer A". Shipped.**
- **L4** Archival memory retrieval — Neo4j vector+FTS `memory_search` (`internal/agent/mcptools/bridge.go`), `:AgentInsight`. **= "Layer C". Shipped (partial).**

Amendment #21 **already decided** (2026-05-29) to adopt Anthropic `compact_20260112` *"in spirit (L1+L2 client-side), NOT as a direct API param"*, and lists **provider-specific compaction params as explicitly out-of-scope** (multi-provider via OpenRouter).

**Consequence for this spike — the REMOVE verdict gets *stronger*, not weaker:** the dark engine is **not** Aura's context defense. Aura already has a complete layered defense (L0–L4, shipped, wired). The dark engine is the **Phase 42 `llm-conversation-compaction` durable LLM engine** — an *optional future slice* (originally deferred at `prd.md:1045`, then over-built) layered redundantly *on top of* that complete defense, delivering nothing while costing a P0. Removing it does not touch a single one of the L0–L4 layers.

Two claims in §3/§6 below are therefore adjusted: (a) my "Layers A/D/C" are mostly **already shipped** as L1/sidecar / L2 / L4 — the genuinely *new* work is small (strengthen L4 with consolidation hygiene); (b) the "delegate to native Anthropic compaction" idea in Layer B **contradicts an existing ratified decision** (amendment #21 out-of-scope). Newer 2026 evidence (the OpenRouter "Anthropic Skin" forwards native params/beta headers untouched — post-dates the 2026-05-29 assessment) makes it a **reopenable open question (§8 Q2)**, not a recommendation that silently overrides the PRD.

---

## 1. Question

Aura carries a bespoke *durable semantic-compaction engine* that is dark end-to-end. Do we **wire** it (finish 1.9 + 2.3) or **remove** it — and in either case, **how does Aura actually avoid context bloat and context rot** given the dual-provider constraint (OpenRouter + llama.cpp)?

The user framing that reshaped the spike: *"what can neo4j agent-memory offer to avoid context bloat and rot?"* — i.e. the real target is not "how do we compress the transcript better" but "how do we stop needing to."

---

## 2. Aura's current state (verified this session, not supposed)

The engine is confirmed dark:

- `FinalizeCompaction` (the CAS transaction that commits a compaction) has **zero non-test callers** — only the definition in `store_compaction.go:160` and its params struct. `grep -rn FinalizeCompaction internal --include='*.go' | grep -v _test` returns declarations only.
- **20 non-test `*compact*` Go files** in `internal/conversations` + `internal/conversations/compaction_eval` + `internal/agui/compaction_memory_api.go` + `internal/config/config_compaction.go` + telegram `compact.go` + prompt `builder_compaction`. A full rollout / eval / reconstruct / rebase / manifest / budget / claims / metrics apparatus.
- The **rollout evaluator** (`compaction_eval/evaluator.go` + `compaction_rollout*.go`) is the direct source of **BUG-6a** — it self-immolated on a `23505` from empty `{}` windows (Wave 0.1/0.2, now guarded + self-healing). The windows it evaluates are **never populated by real telemetry**; the trigger enum values are dead; `Preview`/`Restore` act on a table nothing fills.

**Cost of keeping:** ~20 files of stateful CAS/rollout machinery that has never run in production, has already produced one P0 crash-storm, and blocks reasoning about the conversation store. **Benefit delivered to date: zero.** Burden of proof is not met.

---

## 3. Industrial landscape — 2026 (four distinct paradigms)

The field has **not** converged on "summarize the transcript better." It has split into four complementary layers. Aura's bespoke engine bet everything on layer B alone — the weakest single bet.

### A. Deterministic tool-output reduction *at the boundary* (before tokens enter context)
- **tokenjuice** (`d:/tmp/tokenjuice`, cloned this session): a deterministic, **rule-driven** output compactor that hooks post-tool-use, observes noisy command output (`git status`, `pnpm test`, `rg`, `docker build`), and returns a rule-reduced payload. Rules are **inspectable JSON, not LLM vibes**; raw output stays retrievable via `--raw`/`--full` or artifact spillover; host integrations are thin wrappers over one core reducer.
- **Anthropic context editing** (`clear_tool_uses_20250919`, beta `context-management-2025-06-27`): **server-side** clearing of the oldest tool results once a token threshold is crossed, replaced by placeholders, **applied after prompt-cache lookup so it never busts the cache prefix**. `keep` / `clear_at_least` / `exclude_tools` knobs. Anthropic reports **+29% alone, +39% with the memory tool**. Tool results are the #1 bloat source — this is the highest-leverage layer, and it is *lossless-to-semantics* (the model still sees which tool ran, just not the wall of output).

### B. Transcript-level lossy compaction (summarize old turns) — *the layer Aura built*
- **Anthropic native compaction** (`compact-2026-01-12`, Opus 4.6): the API auto-summarizes earlier portions as the window fills. **76% MRCR-v2 @ 1M tokens** (4× Sonnet 4.5's 18.5%). Works across API / Bedrock / Vertex / Foundry.
- **Anchored iterative summarization** (the 2026 research consensus): merge each new summary **into a persistent state** rather than regenerating — higher continuity/completeness than regenerate-from-scratch.
- **ACON / ReSum**: threshold-triggered — invoke a compression LLM only when the budget is crossed.
- **OpenRouter middle-out** (`transforms:["middle-out"]`): crude drop-the-middle truncation; Aura already scoped this as **1.11** fail-safe. Lossy, no tool-pair awareness — last resort, not a mechanism.

### C. Extractive / graph-native memory (don't compress history — *extract what matters, retrieve on demand*)
This is the direct answer to "avoid bloat **and** rot": the working context stays small **by design** because raw history is never carried; rot is fought because retrieval returns only relevant salient facts, not an ever-growing summary.
- **mem0 v3** (`d:/tmp/mem0`, April-2026 algorithm): single-pass ADD-only extraction, entity linking, **multi-signal retrieval (semantic + BM25 + entity) fused**, temporal reasoning. **92.5 LoCoMo / 94.4 LongMemEval at ~7K tokens, p50 ~1s**. Passive extraction, token-efficient — the 2026 default for "remember the user."
- **neo4j agent-memory** (`d:/tmp/agent-memory`, Neo4j Labs) — see §4.
- **Letta / MemGPT** (studied via docs, not cloned — it is an agent *runtime*, and Aura already is one): three-tier core/recall/archival with **agent self-editing** memory. Better long-horizon coherence, but memory quality rides entirely on model judgment and it wants to own the loop. Not adoptable wholesale; the *core-memory-block-in-context* idea is worth stealing.

### D. Provider context-awareness signals (feed remaining-budget back to the model)
- Post-tool "context remaining" feedback + honest token accounting. Aura's **1.10** (llama.cpp token estimator) is exactly this layer and is **load-bearing regardless of the spike outcome**.

---

## 4. What neo4j agent-memory concretely offers Aura

Aura **already runs Neo4j + APOC + GDS**, already has a knowledge graph (`0001_init`/`0002_documents` Cypher migrations), already talks to it via `mcp-neo4j-cypher`, and already has an `internal/memory` package (Wave 1.8 is delete/update/list on it). agent-memory is the closest-fit reference for turning that into a bloat/rot defense:

- **POLE+O model** + short-term (conversations) / long-term (entities, preferences, facts) / reasoning (traces) split — parity with Aura's existing `:AgentEpisode`/`Insight` intent (CLAUDE.md Slice 11e).
- **`adopt_existing_graph(...)`** — layer the memory model over a graph you already run (Aura's exact situation) instead of a greenfield store.
- **Consolidation primitives** (`consolidation.py`, all `dry_run=True` by default, idempotent, audit-noded): `dedupe_entities` (embedding-similarity `:SAME_AS` merge), `summarize_long_traces`, `detect_superseded_preferences`. This is the **anti-rot hygiene job** Aura lacks — near-duplicate facts get merged/superseded instead of accreting.
- **Multi-tenant scoping** (`user_identifier=`) — maps to Aura's per-identity isolation (relevant to Wave 3.1 MUSR).
- **Buffered fire-and-forget writes** — extraction never blocks the response turn.
- **Bolt self-hosted / air-gapped path** — critical: satisfies Aura's **privacy/local** provider constraint. No dependency on a hosted service or on any specific LLM provider.
- **Bring-your-own-model** (native OpenAI/Anthropic/Bedrock + LiteLLM 100+, or `llm=None` + local sentence-transformers/spaCy/GLiNER) — extraction can run on the **local llama.cpp model**, keeping the whole memory path provider-agnostic.

**Caveat (honest fit):** it is a **Python** library; Aura is Go. We do **not** vendor it. We adopt its **patterns** (the consolidation Cypher, POLE+O shape, extract-on-write) against Aura's existing Neo4j via `mcp-neo4j-cypher`, or run it as an **MCP sidecar** the way Aura already runs `mcp-neo4j-cypher`. The value is the design, and the consolidation Cypher we can lift directly.

---

## 5. The dual-provider constraint — the decisive analysis

The plan's hard constraint: *"context management stays Aura-side and provider-agnostic."* The 2026 reality forces a nuance:

- **Layer A/B native (Anthropic context editing + compaction)** is best-in-class (+39%, 76% MRCR@1M) and passes through the **OpenRouter "Anthropic Skin" untouched** (native tool use / thinking / beta headers all forwarded). For the Anthropic-family path it is strictly better than anything Aura can build.
- But it is **Anthropic-family only**. The **llama.cpp local path** gets none of it. And OpenRouter's own middle-out plugin can silently truncate under Aura's feet if not disabled.

The correct reading of "provider-agnostic": **Aura must own a *fallback* mechanism that works on every provider, and delegate to the native one when it is strictly better.** Owning a *bespoke, always-on, provider-blind* engine (what Aura built) is the wrong interpretation — it reinvents, worse, what the Anthropic path already does for free, while still needing a local path.

---

## 6. Decision — REMOVE the bespoke engine; adopt a 4-layer provider-aware strategy

**Verdict: REMOVE.** Delete the dark durable semantic-compaction engine (rollout evaluator, `FinalizeCompaction` CAS chain, `compaction_eval`, reconstruct/rebase/manifest/claims, the `compaction_memory_api` surface, `config_compaction`). Wave **1.9 = NO-GO**, Wave **2.3 (compaction UI) = NO-GO** (nothing to mount). This is the `SPIKE=remove` branch: 0.1's control plane is deleted outright (0.1's *crash fix* stays until the delete lands, so live stops bleeding meanwhile).

Replace it with the layered strategy, in leverage order:

| Layer | Mechanism | Provider scope | Aura status | Priority |
|---|---|---|---|---|
| **A — tool-output reduction at the boundary** | tokenjuice-style **deterministic JSON-rule reducers** on tool results, spilling raw to `$AURA_RUN_DIR` (Aura *already* has sidecar spillover — this is a near-free fit); + delegate to Anthropic **`clear_tool_uses`** on the Anthropic path via beta header | all (rules) + Anthropic (native) | new, highest ROI | **P1** |
| **B — transcript compaction (fallback only)** | Anthropic **native `compact-2026-01-12`** on the Anthropic/OpenRouter-Skin path; **anchored iterative summarization with the LOCAL model** on the llama.cpp path (threshold-triggered, ACON-style); keep **1.11 middle-out** as the crude last resort | Anthropic (native) + local (Aura-owned) | replaces the dark engine, far smaller | **P2** |
| **C — extractive graph memory (anti-rot core)** | agent-memory patterns over Aura's **existing Neo4j**: extract-on-write salient facts/entities/preferences, `dedupe_entities`/`detect_superseded_preferences` consolidation sweep, multi-signal retrieval into a small working context. Ties into Wave **1.8** (memory tool surface) and **2.8** (memory→task provenance) | all (local extraction via llama.cpp or LiteLLM) | **= PRD L4, already shipped (partial)**; the *new* value is the **consolidation hygiene** (dedupe/supersede) L4 lacks | **P1 (design), P2 (build)** |

> **Corrected mapping (per §0):** A = shipped L1 + sidecar; D = shipped L2/L2.5; C = shipped-partial L4. The only genuinely new build is the **L4 consolidation sweep** (agent-memory `dedupe_entities`/`detect_superseded_preferences`) — the real anti-rot upgrade. B's native-compaction delegation is a **reopenable OQ**, not a default (amendment #21 ruled it out-of-scope). **The dominant deliverable of the SPIKE=remove branch is the *deletion* of the Phase 42 engine, not new construction.**
| **D — honest token accounting / context-awareness** | Wave **1.10** llama.cpp estimator + remaining-budget feedback | all | already planned, load-bearing | **P1 (already in Wave 1)** |

**Why this is right, not just smaller:** Aura bet on layer B alone and built the one layer the market now gives away on the Anthropic path. The durable win against *rot* is layer C (extraction+retrieval), which Aura is uniquely positioned for (Neo4j already in-stack) and which the bespoke engine never touched. Layer A is the highest ROI-per-line and reuses Aura's spillover plumbing.

---

## 7. Consequences / follow-through

- **Removal is a real slice**, not a delete: it needs its own phase (migrate away the `compaction_memory_api` AG-UI surface, drop `config_compaction` env vars from the catalog with a PRD amendment, remove the dead trigger enums, prune telegram `compact.go` + prompt `builder_compaction` paths). Sequence it **before** Wave 1.9/2.3 are reached so they simply don't exist.
- **PRD amendment required**: the PRD currently specifies the durable semantic-compaction engine as the context strategy. This spike's REMOVE + 4-layer replacement is an architectural deviation → PRD-amendment commit before code (CLAUDE.md PRD-first + §Q&A revision protocol).
- **Wave 0.1 crash fix stays** in place until the removal phase lands (live must not bleed in the interim).
- **New env catalog entries** (layered strategy): tool-reduction toggle + rule path (A), native-compaction beta-header toggle (B), local-summarization threshold (B), extraction/consolidation cadence (C). All `AURA_*`, catalogued in the PRD.
- **Wave 1.10 unaffected** (load-bearing either way). **Wave 1.11 middle-out** demoted to last-resort within layer B.
- **agent-memory / mem0 stay as pattern references at `d:/tmp`**; tokenjuice cloned there this session. Letta intentionally not cloned (runtime, not a library; core-memory-block idea captured).

## 8. Open questions to close before the removal phase

1. **Layer C substrate**: adopt agent-memory *patterns* natively in Go against Neo4j via `mcp-neo4j-cypher`, or run agent-memory as an **MCP sidecar** (like `mcp-neo4j-cypher` already is)? Sidecar is faster to prove; native is fewer moving parts. → prototype both in the layer-C build slice.
2. **Native compaction reachability**: verify empirically that `compact-2026-01-12` + `clear_tool_uses` beta headers survive Aura's specific OpenRouter routing (the Skin *should* forward them; issue #56317-class plugin interference must be ruled out by disabling OpenRouter's own compression plugin).
3. **Local summarization quality gate**: what LoCoMo/continuity bar must the llama.cpp anchored-summarization path clear to be trustworthy on the privacy path? (borrow mem0's open benchmark harness.)
4. **Removal blast radius**: does any *shipped* UI/telemetry read the compaction tables today? (live inspection says no — the tables are unfilled — but confirm before the drop migration.)

---

## TL;DR

The bespoke durable-compaction engine is dark, has already cost one P0, and bet on the exact layer (transcript summarization) the market now hands out for free on the Anthropic path. **Remove it.** Aura avoids bloat/rot with a 4-layer, provider-aware strategy: (A) deterministic tool-output reduction at the boundary + Anthropic `clear_tool_uses`, (B) native compaction on Anthropic / local anchored-summarization fallback on llama.cpp, (C) **extractive graph memory over Aura's existing Neo4j — the agent-memory patterns — as the real anti-rot core**, (D) honest token accounting (1.10). **1.9 and 2.3 are NO-GO.** Removal is its own PRD-amended phase, sequenced before Wave 1.9/2.3 are reached.
