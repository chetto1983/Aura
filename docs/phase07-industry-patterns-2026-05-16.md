# Phase 7 — Industry Patterns Mined for Typed Memory & Hybrid Retrieval

**Date:** 2026-05-16
**Author:** programmer agent (industry-source mining run)
**Status:** input to Phase 7 (Rebuild RAG / Typed Memory) and Phase 7A (Compact Archive Hygiene)
**Sources scanned:** `D:/tmp/mem0/`, `D:/tmp/nanobot/`, `D:/tmp/llm_wiki/`, `D:/tmp/elysia/`, `D:/tmp/logseq/`, `D:/tmp/recursive-llm/`, `D:/tmp/cli-printing-press/`, `D:/tmp/aura-agent-loop-papers/`, `D:/tmp/paper.md`
**Method:** read-only mining (no tests/mocks/node_modules/dist), every pattern is file:line cited.

> Phase 7's plan already rejected wholesale adoption of mem0; this report documents what is still mineable as composable patterns. Section E is the operational deliverable: it names the Phase 7A first slice.

---

## A. Catalogue of Concrete Patterns

| Pattern name | Source file:line | What it does | Aura applicability |
|---|---|---|---|
| **Reciprocal Rank Fusion (RRF) with K=60 for wiki search** | `D:/tmp/llm_wiki/src/lib/search.ts:38-53,339-352` | Token search and embedding search produce two independently-ranked lists with incommensurable scores (token 1-400 vs cosine 0-1). RRF fuses on **rank only**: `fused(p) = sum 1/(K + rank_L(p))`, K=60. Page absent from a list contributes 0 from that side. Comments cite Cormack et al. SIGIR 2009. | **HIGH — adopt verbatim.** Aura's wiki and Qdrant already produce two ranked lists; the planning doc (PRD §7) already calls for RRF. This file is a 300-line drop-in template. |
| **Materialize vector-only pages before fusing** | `D:/tmp/llm_wiki/src/lib/search.ts:301-330` | When the vector list returns a page absent from the token results, the loader walks per-type directories (`entities/`, `concepts/`, `sources/`, `synthesis/`, `comparison/`, `queries/`) to read+rank it. Without this, vector-only hits cannot surface even if rank=1. | **HIGH** — Aura's "speculative wiki search" suffers the same trap whenever Qdrant returns a page the FTS pass missed. |
| **Graph-relevance scoring with typed signals** | `D:/tmp/llm_wiki/src/lib/graph-relevance.ts:30-43,247-287` | Pairwise relevance combines 4 signals with explicit weights: direct link (3.0), source-overlap (4.0), common-neighbor Adamic-Adar (1.5), type-affinity (1.0). Type-affinity is a hardcoded 5×5 matrix (entity↔concept=1.2, source↔source=0.5, query↔query=0.5). | **HIGH** — Aura's wiki already has `type:` and `related:` in frontmatter; this is the math layer the wiki has been missing. Composes with RRF as a graph-aware re-rank. |
| **Markdown-aware chunker with heading breadcrumb** | `D:/tmp/llm_wiki/src/lib/text-chunker.ts:1-43,73` | Each chunk carries a `headingPath` like `"## Techniques > ### Flash Attention"`. Split priority: heading sections > paragraphs > lines > sentences > whitespace > char. Never splits inside fenced code blocks or markdown tables. YAML frontmatter stripped before chunking. Tiny chunks (<200) merged into neighbours. | **HIGH** — directly applicable to Aura's wiki ingest. The breadcrumb pattern is what makes short chunks self-describing without re-embedding context. |
| **Hybrid additive scoring (semantic + BM25 + entity-boost), NOT RRF** | `D:/tmp/mem0/mem0/utils/scoring.py:60-121` | mem0 over-fetches 4× the limit (min 60) on semantic, computes BM25 scores via sparse vectors, normalises BM25 to [0,1] via query-length-adaptive sigmoid, sums semantic + bm25 + entity_boost, divides by max_possible (1.0/2.0/2.5 depending on which signals fired). Threshold gates the *semantic* score *before* combining. | **REJECT for fusion path.** mem0's additive math is inferior to RRF for our shape (no entity store yet, BM25 sigmoid params are heuristic). **MINE** the threshold-before-combine and over-fetch idioms. |
| **Sigmoid BM25 normalisation tuned by query length** | `D:/tmp/mem0/mem0/utils/scoring.py:16-54` | `get_bm25_params(query)` returns (midpoint, steepness) by token count: ≤3 tokens → (5.0, 0.7); 4-6 → (7.0, 0.6); 7-9 → (9.0, 0.5); 10-15 → (10.0, 0.5); >15 → (12.0, 0.5). Then `normalize_bm25 = 1/(1+exp(-steepness*(raw-midpoint)))`. | **LOW** — only relevant if Aura goes BM25 + additive. RRF avoids this entirely. Keep as reference if RRF underperforms. |
| **Search must scope to entity (user/agent/run); reject top-level entity kwargs** | `D:/tmp/mem0/mem0/memory/main.py:100-110,1193-1197` | Search refuses to execute unless `filters` contains at least one of `user_id`/`agent_id`/`run_id`. Passing them as top-level kwargs raises ValueError. | **HIGH** — Aura's wiki search currently has no scoping. For multi-tenant or per-conversation memory this enforcement prevents leakage and accidental cross-context recall. |
| **Hash-dedup before embedding** | `D:/tmp/mem0/mem0/memory/main.py:786-803` | Build set of existing MD5 hashes from prior payloads; reject candidates whose hash matches existing or earlier batch member. Skipping happens **before** the embedding call — saves the LLM/embed budget on duplicates. | **HIGH** — Aura's embedding cache is SHA-keyed by content but doesn't reject duplicate writes at the source. |
| **Anti-hallucination UUID→int mapping in update prompts** | `D:/tmp/mem0/mem0/memory/main.py:716-721` | Existing memories are presented to the LLM as `{"id": "0", ...}`, `{"id": "1", ...}` not raw UUIDs. After parsing, `uuid_mapping["0"] → real UUID`. Stops the LLM hallucinating UUIDs. | **MEDIUM** — Useful if Aura ever does LLM-mediated memory updates. Cheap discipline. |
| **Lemmatization key on metadata for keyword recall** | `D:/tmp/mem0/mem0/memory/main.py:805-810` | Every stored memory gets a sibling `text_lemmatized` field. Query is lemmatized identically before BM25. Survives plural/tense mismatch. | **MEDIUM** — directly applicable if Aura adopts FTS. SQLite FTS5 has tokenizers but lemmatization is a different layer. |
| **Reranker as optional post-step, never replacement** | `D:/tmp/mem0/mem0/memory/main.py:1229-1236` | `rerank=True` triggers a cross-encoder rerank over the fused results. If the reranker throws, the original fused list is returned — never empty. | **HIGH** — `boot non-fatal` matches Aura's existing policy. Pattern to copy when GLM-OCR or a future cross-encoder lands. |
| **Memory file = MEMORY.md + history.jsonl + dream cursor** | `D:/tmp/nanobot/nanobot/agent/memory.py:41-67` | Three-layer typed memory: `MEMORY.md` (long-term facts), `history.jsonl` (append-only event log with auto-incrementing cursor), `SOUL.md`/`USER.md` (identity overlays). Each file owns one shape. | **HIGH** — closest analogue to Aura's wiki + run_events + SOUL/USER/AGENTS overlays. Confirms Aura's existing typed-layer instinct is correct. |
| **strip_think() before persisting to history** | `D:/tmp/nanobot/nanobot/agent/memory.py:264-270` and `D:/tmp/nanobot/nanobot/utils/helpers.py:18-43` | Every history entry passes through `strip_think` to remove `<think>...</think>` blocks, unclosed prefixes, channel markers, malformed opening tags, and trailing partial control tags. If stripped content is empty but raw wasn't, persists empty string rather than re-polluting via replay. | **CRITICAL for Phase 7A.** This is the closest template for "raw model output never enters durable memory". See Section E. |
| **Defensive hard cap in append_history()** | `D:/tmp/nanobot/nanobot/agent/memory.py:253-263,441` | `_HISTORY_ENTRY_HARD_CAP = 64_000`. Belt-and-suspenders cap at the write boundary in case any caller forgot its own cap. Rate-limits the warning so spam doesn't flood logs. | **HIGH** — Aura's conversation archive has no hard cap at the write boundary. A misbehaved tool result can balloon a row. |
| **Two-cap consolidation: raw archive vs summary** | `D:/tmp/nanobot/nanobot/agent/memory.py:438-441,634-667` | `_RAW_ARCHIVE_MAX_CHARS = 16_000` (fallback when LLM summary fails) vs `_ARCHIVE_SUMMARY_MAX_CHARS = 8_000` (successful summary). On LLM failure, raw-archive with `[RAW] N messages` prefix so downstream Dream can identify and re-process. Cursor advances **either way** to prevent re-summarising the same chunk. | **HIGH** — Aura's archive has no degraded-mode breadcrumb. |
| **Dream cursor never advances on incomplete pass** | `D:/tmp/nanobot/nanobot/agent/memory.py:1060-1073` | Only advances `dream_cursor` when AgentRunner returns `stop_reason == "completed"`. Logs "cursor NOT advanced" otherwise and retries next cron cycle. | **HIGH** — Aura's wiki maintenance has no equivalent retry-safe cursor; failures silently drop work. |
| **Per-line age annotation from git blame** | `D:/tmp/nanobot/nanobot/agent/memory.py:888-932` | Before feeding MEMORY.md to Dream Phase 1, each non-blank line older than 14 days gets a suffix `← 30d`. If git unavailable OR line/age counts disagree (working-tree drift), skips annotation rather than mis-tagging. | **MEDIUM** — Aura uses go-git already; this is a one-day add that signals freshness directly inside the context without an extra DB column. |
| **Skill bank dedup at index, not query time** | `D:/tmp/llm_wiki/src/lib/dedup.ts:1-29,167-231,317-420` | Three-stage dedup: (1) walk wiki/entities + wiki/concepts to extract `{slug, title, description, tags}`, (2) LLM detector returns groups + reason + confidence (`high`/`medium`/`low`), (3) merger LLM produces canonical body, then deterministic frontmatter union for `sources`/`tags`/`related`, plus cross-reference rewriting across every other wiki page. Pre-merge snapshot saved for rollback. | **HIGH** — directly maps to Aura's wiki "soft-collision" problem (same entity, different slug). Pattern proven in production llm_wiki. |
| **Cross-reference rewrites on merge** | `D:/tmp/llm_wiki/src/lib/dedup.ts:456-491` | When merging duplicate slugs, rewrites `[[old-slug]]` and `[[old-slug|alias]]` body links AND `related:` frontmatter (both inline and block YAML forms) AND deduplicates the resulting list case-insensitively (first-seen casing wins). | **HIGH** — Aura's wiki currently has no link-rewrite on rename/merge; orphans accrue. |
| **FTS5 trigram-tokenizer SQLite virtual table with triggers** | `D:/tmp/logseq/src/main/frontend/worker/search.cljs:15-51` | Search-side: `CREATE VIRTUAL TABLE blocks_fts USING fts5(id, title, page, tokenize="trigram")`. Index-side: AFTER INSERT / DELETE / UPDATE triggers keep the FTS table in sync with the source `blocks` table automatically — no manual reindex. | **HIGH** — Aura uses SQLite and currently has no FTS. The trigger-driven pattern is exactly the "always-fresh projection" Phase 7 wants for one fewer reindexer to maintain. Trigram tokenizer handles CJK + substring matching out of the box. |
| **Hybrid skill resolution: exact > semantic > generate** | `D:/tmp/aura-agent-loop-papers/2604.27221-Web2BigTable.txt:414-432` | Three-priority resolver: (1) exact-name local skill match; (2) hybrid BM25 + ChromaDB(bge-m3) fused via RRF, optionally cross-encoder reranked; (3) on miss, LLM synthesises a new skill (function OR knowledge skill — Markdown with YAML frontmatter, validated via AST). Newly synthesised skills are instantaneously indexed into BM25 and embed stores via singleton refresh. | **HIGH** — academic confirmation that RRF + BM25 + dense embedding is the production-grade hybrid retrieval shape (not additive). Also confirms knowledge-skill = Markdown + frontmatter is normative. |
| **Token-budget triggered consolidation, NOT message-count** | `D:/tmp/nanobot/nanobot/agent/memory.py:669-769` | Consolidator estimates session prompt tokens (probe through real `_build_messages` chain), compares against `context_window_tokens - max_completion - safety_buffer (1024)`. If over budget, loops at most 5 rounds picking user-turn boundaries that remove enough tokens. Persists `_last_summary` to session metadata for replay. | **HIGH** — Aura's sliding window is message-count based (cap 50); a single fat tool result can blow the context even at 20 messages. Token-budget gating is more robust. |
| **Replay-overflow guard** | `D:/tmp/nanobot/nanobot/agent/memory.py:529-580` | If the post-consolidation tail still exceeds `replay_max_messages`, archive the head of the tail to keep the replay window honest. Critical because consolidation can leave messages that exceed downstream limits even though the token budget is fine. | **MEDIUM** — guards an edge case worth knowing about. |
| **No entity params at top level, ever — use filters dict** | `D:/tmp/mem0/mem0/memory/main.py:99-110` | Centralised `_reject_top_level_entity_params(kwargs, "search")` keeps every public method enforcing the same scoping contract. | **MEDIUM** — code-organisation pattern, not a memory pattern per se. |
| **Sources frontmatter array drives graph signal** | `D:/tmp/llm_wiki/src/lib/graph-relevance.ts:79-99,260-265` | Pages declare `sources: [a.pdf, b.pdf]` in frontmatter. Two pages sharing a source contribute +4.0/source to their relevance. Stronger than direct wikilinks (+3.0). | **HIGH** — Aura's wiki already has `sources:` in the schema; this turns a metadata field into a recall signal for free. |

---

## B. Academic Concepts Cited in Papers

These are theoretical / methodological — not concrete code patterns. Use as backup citations for design choices.

1. **Reciprocal Rank Fusion (RRF, Cormack et al. SIGIR 2009)** — fuse N ranked lists by rank, not by raw score. *Cited at: `D:/tmp/llm_wiki/src/lib/search.ts:52` (production-implemented) and `D:/tmp/aura-agent-loop-papers/2604.27221-Web2BigTable.txt:418` (academic).* K=60 is the canonical constant.
2. **Toolformer (Schick et al. 2023)** — LLM trained to autonomously decide when to call tools; relevant to Aura's tool-call discipline. *Cited at: `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:90,454`.*
3. **BAAI/bge-m3 multilingual embedding model + ChromaDB hybrid pipeline** — concrete production stack pairing BM25 + bge-m3 + RRF for >8000 skill catalogue. *Cited at: `D:/tmp/aura-agent-loop-papers/2604.27221-Web2BigTable.txt:417-418`.* Confirms 256d MRL truncation of embeddinggemma is in family.
4. **Adamic-Adar common-neighbour score** — graph-relevance signal that weights shared neighbours inversely by their degree (rare common neighbours count more). *Implemented at: `D:/tmp/llm_wiki/src/lib/graph-relevance.ts:267-280`.*
5. **Implicit information-retrieval signals (Joachims-style)** — query reformulation, session abandonment, dwell time as implicit relevance feedback; applied here to agent trajectories. *Cited at: `D:/tmp/aura-agent-loop-papers/2604.00356-Signals.txt:71,122-127`.*
6. **SES Memory / experiential evolution** — extract high-value skill snippets from successful trajectories, store back for reuse; Lemon Agent's continuous-learning loop. *Cited at: `D:/tmp/aura-agent-loop-papers/2602.07092-Lemon-Agent.txt:115,343`.*
7. **Recursive Character Text Splitter (Langchain origin)** — markdown-tuned recursive split priority: headings > paragraphs > lines > sentences > whitespace > char. *Implemented at: `D:/tmp/llm_wiki/src/lib/text-chunker.ts:10-20`.*
8. **Trigram FTS5 tokenizer (SQLite)** — substring matching that handles CJK and partial matches without explicit lemmatization. *Used at: `D:/tmp/logseq/src/main/frontend/worker/search.cljs:51`.*

Not found in this scan (so do not cite as evidence): Self-RAG, GraphRAG community detection, ColBERT late-interaction, HyDE hypothetical-document embeddings, RAGAS evaluation. They may be in the PDFs but the .txt extractions don't surface them. Treat as "academically interesting, not industry-validated in this corpus".

---

## C. Anti-patterns — What These Projects Explicitly Do NOT Do

1. **mem0 does NOT extract from system messages, ever.** Hard-coded penalty language in the prompt: "GENERATE FACTS SOLELY BASED ON THE USER'S MESSAGES. DO NOT INCLUDE INFORMATION FROM ASSISTANT OR SYSTEM MESSAGES." *Source: `D:/tmp/mem0/mem0/configs/prompts.py:67-68,108-110`.* Generic acknowledgments ("Sure!", "Great question!") are also forbidden (`prompts.py:492`). **Anti-pattern Aura risks: today the runtime sees tool results and could phantom-extract from them as if they were assertions.**
2. **mem0 does NOT extract from echoed-back content.** "If the user said it and the assistant echoed it, extract only once from the user's version." *Source: `D:/tmp/mem0/mem0/configs/prompts.py:574`.* A single assistant message may contain both echo + new facts; only the new facts get extracted.
3. **nanobot does NOT silently retry consolidation.** When the LLM consolidation call fails, raw-archive the messages as `[RAW] N messages` and **break out of the loop** rather than hammer the LLM. *Source: `D:/tmp/nanobot/nanobot/agent/memory.py:751-754`.* The next cron cycle retries — never retry in-loop.
4. **nanobot does NOT advance the Dream cursor on incomplete runs.** Stop-reason must be `"completed"` exactly. Exceptions or unfinished iterations leave the cursor in place. *Source: `D:/tmp/nanobot/nanobot/agent/memory.py:1060-1073`.* **Anti-pattern Aura must avoid: silent "advanced because we tried" cursors.**
5. **llm_wiki does NOT search raw PDFs/DOCX at query time.** They removed `raw/sources/` from the search path explicitly because text extraction added 5-15s per search. The cost-of-search is bounded; the wiki summary page is the citation handle. *Source: `D:/tmp/llm_wiki/src/lib/search.ts:230-249`.* **Anti-pattern Aura risks: pulling raw OCR text into retrieval instead of the curated wiki page.**
6. **llm_wiki does NOT use `New-Item -Force` semantics** — token search ranks are snapshotted *before* vector search runs, so adding vector-only candidates doesn't shift token ranks. *Source: `D:/tmp/llm_wiki/src/lib/search.ts:267-272`.* **General anti-pattern: hidden state mutation between sub-stages of a pipeline.**
7. **mem0 does NOT use raw UUIDs in update prompts.** UUIDs are mapped to integers 0..N before being shown to the LLM; the LLM cannot hallucinate UUIDs because it only sees ints. *Source: `D:/tmp/mem0/mem0/memory/main.py:716-721`.* **Anti-pattern Aura risks: LLM hallucinating wiki slugs or run_event IDs.**
8. **llm_wiki does NOT trust the LLM's JSON shape.** Tolerant JSON extraction with brace-balancing; LLM-emitted code fences and preambles get stripped; invalid groups are filtered out so callers never see garbage. *Source: `D:/tmp/llm_wiki/src/lib/dedup.ts:250-310`.* **Anti-pattern: trusting `JSON.parse` on raw LLM output.**

---

## D. Aura-Fit Shortlist

Aura's current shape (from CLAUDE.md and recent commits):
- Wiki Markdown + YAML frontmatter (`title`, `slug`, `category`, `tags`, `related`, `sources`, `schema_version`, `prompt_version`), file-mutex'd, atomic writes, git-tracked.
- Conversation archive (`conversations` table, per-turn with `tool_calls` JSON when `CONV_ARCHIVE_ENABLED=true`).
- `run_events` ledger.
- `classifyToolError` taxonomy.
- Qdrant optional sidecar; embedding cache SHA-keyed in SQLite.
- `embeddinggemma-300m` via llama.cpp at 256d (MRL-truncated). Single path, no toggle.

### Top 3 to ADOPT

1. **llm_wiki RRF + materialize-vector-only + graph-relevance triplet** (`D:/tmp/llm_wiki/src/lib/search.ts` + `graph-relevance.ts`).
   *Why fit:* Aura already has the wiki Markdown layer with `type:`/`related:`/`sources:` frontmatter — every input the graph-relevance signal needs. Token search is FTS5 over wiki body. Vector search is Qdrant. Compose: FTS+Qdrant→RRF→graph-relevance re-rank using existing frontmatter. Zero new state stores.
   *Slice:* implement `searchWiki()` in Go matching the TS shape, K=60.

2. **nanobot strip_think + history hard-cap + raw_archive degraded-mode** (`D:/tmp/nanobot/nanobot/agent/memory.py:235-275,417-427`).
   *Why fit:* Aura's conversation archive currently persists whatever the model emits, including `<think>` blocks and template leaks. A defensive 64K cap at the write boundary + content sanitiser is a one-day add that hardens the archive against runaway tool outputs. See Section E.

3. **logseq FTS5 trigram + AFTER-INSERT/DELETE/UPDATE triggers** (`D:/tmp/logseq/src/main/frontend/worker/search.cljs:15-51`).
   *Why fit:* Aura already uses SQLite. The trigger pattern means the wiki FTS projection is **always fresh** — no reindex scheduler to maintain, no drift between source-of-truth and projection. Trigram tokenizer handles Italian/English/CJK uniformly without separate analyser config.

### One to REJECT

**mem0's additive scoring (semantic + bm25 + entity_boost) as the fusion path.** *Source: `D:/tmp/mem0/mem0/utils/scoring.py:60-121`.*
*Why reject:* (a) Aura has no entity store yet, so entity_boost = 0 — we'd be in a degenerate 2-signal additive mode which is mathematically equivalent to RRF only in pathological cases. (b) BM25 sigmoid params are heuristic (`get_bm25_params` returns 5 different `(midpoint, steepness)` pairs by token-count band) and have no theoretical grounding — they need re-tuning per corpus. (c) RRF is rank-only and immune to score-scale drift. **Adopt RRF (Pattern 1 above), keep mem0's `threshold-before-combine` and `over-fetch 4×` idioms.**

---

## E. Phase 7A Shortlist — "Raw Output Stays in Archive but Never Enters Durable Memory"

**Phase 7A trigger (from 2026-05-15 debug):** raw tool output and `<think>` blocks were leaking into the conversation archive, then back through replay into downstream context — corrupting future turns. The hygiene rule needed: archive captures everything (audit trail), but durable memory (wiki, summary) sees only sanitized + size-capped content.

### Best-fit pattern: nanobot's two-layer hygiene at `append_history()`

**Citation:** `D:/tmp/nanobot/nanobot/agent/memory.py:235-275` (definition of `append_history`) and `D:/tmp/nanobot/nanobot/utils/helpers.py:18-43` (`strip_think`).

**The shape:**

```python
# memory.py:235-275 — every history write is sanitized + capped
def append_history(self, entry: str, *, max_chars: int | None = None) -> int:
    limit = max_chars if max_chars is not None else _HISTORY_ENTRY_HARD_CAP   # 64_000
    cursor = self._next_cursor()
    ts = datetime.now().strftime("%Y-%m-%d %H:%M")
    raw = entry.rstrip()
    if len(raw) > limit:
        # rate-limited warning — caller forgot its cap
        if not self._oversize_logged:
            self._oversize_logged = True
            logger.warning("history entry exceeds {} chars ({}); truncating...", limit, len(raw))
        raw = truncate_text(raw, limit)
    content = strip_think(raw)
    if raw and not content:
        # raw was non-empty but stripped to empty (pure template leak);
        # persist empty rather than re-pollute downstream via replay
        logger.debug("history entry {} stripped to empty (likely template leak); "
                     "persisting empty content to avoid re-polluting context", cursor)
    record = {"cursor": cursor, "timestamp": ts, "content": content}
    # ... append to history.jsonl
```

**Why this is the gold pattern for Phase 7A:**

1. **One choke point.** Every memory write — summary, raw fallback, dream output — goes through `append_history`. No backdoor.
2. **Two caps stacked.** `_HISTORY_ENTRY_HARD_CAP = 64_000` (catch-all at write boundary) PLUS per-caller caps `_RAW_ARCHIVE_MAX_CHARS = 16_000` and `_ARCHIVE_SUMMARY_MAX_CHARS = 8_000`. Belt and suspenders. *Source: `D:/tmp/nanobot/nanobot/agent/memory.py:438-441`.*
3. **`strip_think` runs before persistence, not before display.** This is the critical inversion: persist clean, replay clean. Aura today strips at render but persists raw → corrupted replay. *Source: `D:/tmp/nanobot/nanobot/agent/memory.py:264`.*
4. **Empty-after-strip is persisted as empty, not as raw.** Comment block on lines 265-270 specifies: if `strip_think` ate the whole entry (template leak), persist empty string explicitly. Falling back to `raw` would "undo strip_think's guarantees downstream during history replay / consolidation."
5. **Cursor advances either way.** Even if content is empty, the record is written and the cursor advances. Dream/consolidation cannot re-process the same broken chunk on the next cron cycle. *Source: `D:/tmp/nanobot/nanobot/agent/memory.py:271-275`.*
6. **Degraded-mode breadcrumb.** When LLM consolidation fails, `raw_archive()` writes `[RAW] N messages\n...formatted...` (`memory.py:417-427`) — downstream can later identify and re-summarise. The `[RAW]` prefix is a typed marker.
7. **Rate-limited warning.** One warning per process lifetime (`_oversize_logged` flag), not one per bad write — avoids log flooding.

### Concrete Phase 7A first slice (proposed)

| Step | Action | Location in Aura | Source pattern |
|---|---|---|---|
| 1 | Create `internal/archive/sanitize.go` with `StripThink(s string) string` and `TruncateForArchive(s string, max int) string`. Cover: `<think>...</think>`, unclosed `<think...`, channel markers `<|channel|>`, malformed `<think广场` style (no closing `>`), trailing partial `<thi`/`<thin`. | New file | `D:/tmp/nanobot/nanobot/utils/helpers.py:18-43,146-150` |
| 2 | Add a single choke-point `archive.WriteTurn(entry Entry) error` that ALWAYS runs sanitize → cap → write. Cap = 64_000 chars. Every existing archive writer must route through it. | `internal/archive/` (new) or extend `internal/conversation/` | `D:/tmp/nanobot/nanobot/agent/memory.py:235-275` |
| 3 | When `StripThink` returns empty but `Raw != ""`, write `Content = ""` and log `archive_template_leak` event with cursor. | Same choke point | `D:/tmp/nanobot/nanobot/agent/memory.py:264-270` |
| 4 | Two-tier caps: per-caller cap (summary 8K, raw 16K), then the global 64K belt-and-suspenders at the write boundary. | Constants in archive pkg | `D:/tmp/nanobot/nanobot/agent/memory.py:438-441` |
| 5 | When summarisation fails, write `Content = "[RAW] " + truncated_raw` with explicit `degraded=true` flag in metadata. Cursor advances. | `internal/agentloop/` consolidation path | `D:/tmp/nanobot/nanobot/agent/memory.py:417-427,751-754` |
| 6 | Probe test: feed a turn containing `<think>secret</think>real content`, assert (a) `conversations.content = "real content"`, (b) `run_events` has no `<think>`, (c) replay of the conversation through `/api/chat` does not surface `secret`. Fail loudly if `<think>` appears anywhere downstream of the archive. | New `cmd/probe_archive_hygiene` | per CLAUDE.md "validate with verified benchmarks" rule |

**Estimated diff:** ~250 LoC Go + ~150 LoC probe. One file under 600 LoC; reusable; no duplication.

### Runner-up patterns considered (and why nanobot wins)

- **mem0's `_add_to_vector_store` hash-dedup** (`main.py:786-803`) prevents duplicate writes but does NOT sanitize content shape — orthogonal concern, useful for Phase 7B/C not 7A.
- **mem0's "do not extract from system messages" prompt** (`prompts.py:67-68`) is a prompt-engineering rule, not a write-boundary rule. Aura's wiki ingester needs both, but the *first slice* is the write boundary.
- **llm_wiki's ingest sanitiser** (`D:/tmp/llm_wiki/src/lib/ingest-sanitize.ts`, referenced in `ingest.ts:10`) targets prompt-injection in raw documents, not LLM emissions back into memory — orthogonal.
- **elysia error-ledger** — searched; no concrete error-ledger module found in Python sources (only minified next.js JS). The pattern Aura already adopted from elysia in Phase-J appears to be inspired by it but does not appear here as a single mineable file.

**Phase 7A delivers:** one choke-point write boundary, two stacked caps, content sanitiser, degraded-mode breadcrumb, probe that asserts ground-truth (no `<think>` survives replay). That is the minimum slice that closes the 2026-05-15 leak.

---

## Appendix: File-by-File Coverage

| Source | Files read in full | Files grepped | Verdict |
|---|---|---|---|
| `D:/tmp/mem0/mem0/` | `memory/main.py` (1-840, 1126-1410), `utils/scoring.py`, `configs/prompts.py:15-200,468-666`, `vector_stores/qdrant.py:400-445` | many | **HIGH yield** — additive scoring, dedup, scoping, prompts |
| `D:/tmp/nanobot/nanobot/agent/memory.py` | entire file (1087 lines) | + `utils/helpers.py:18-150` | **CRITICAL for Phase 7A** |
| `D:/tmp/llm_wiki/` | `src/lib/search.ts`, `graph-relevance.ts`, `dedup.ts`, `text-chunker.ts:1-80`, `llm-wiki.md`, `ingest.ts:1-100`, `embedding.test.ts:1-50` | many | **HIGH yield** — RRF, graph, chunking, dedup |
| `D:/tmp/elysia/` | `tools/retrieval/query.py:1-100`, `tools/retrieval/chunk.py:1-80`, `preprocessing/collection.py` (greps only) | many | **MEDIUM** — hybrid query tool, Weaviate-specific; error-ledger not found here |
| `D:/tmp/logseq/` | `src/main/frontend/worker/search.cljs:1-100` | many | **HIGH** — FTS5 trigger pattern is the always-fresh projection win |
| `D:/tmp/recursive-llm/` | `src/rlm/core.py:1-80` | full tree | **EMPTY** — no persistence, REPL-only |
| `D:/tmp/cli-printing-press/` | tree listing + greps | many | **EMPTY** — pipeline state, no RAG memory |
| `D:/tmp/aura-agent-loop-papers/` | `2604.27221-Web2BigTable.txt:410-450`, scattered greps | all 9 .txt files greped for RAG vocab | **MEDIUM** — Web2BigTable is the academic confirmation of BM25+dense+RRF |
| `D:/tmp/paper.md` | first 100 lines | full grep | **LOW** — about Kimi K2.5 Agent Swarm, not RAG |
