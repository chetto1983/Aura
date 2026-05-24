# Tool-RAG Eval Harness — Phase plan (2026-05-20, v3)

Companion to the MCP productivity roundup. As Aura adds MCPs, the tool surface grows from ~50 → 100+ → 300+. Hand-authored fixtures don't scale — the eval must **auto-adapt from production data + tool definitions + published benchmarks**.

**Rule we accept**: no story may require manually writing N fixture queries per tool. Every tool added to the registry (native or MCP) must produce eval coverage automatically.

**Key resource discovered 2026-05-20**: `D:/tmp/bench_dataset.json` already holds **188 production queries** mined from Davide's real Aura conversations (chat_id 1148481707, 47% IT / 53% EN). This is the Layer-1 seed — not a future-thing.

## Current state — what exists today

- [internal/agent/tools/index/eval_topk_test.go](internal/agent/tools/index/eval_topk_test.go) runs **14 hand-authored fixture queries** through `Registry.Search()` (lexical BM25-style), asserts ≥70% rank the expected tool in top-3.
- [internal/agent/tools/index/eval_topk_fixture.json](internal/agent/tools/index/eval_topk_fixture.json) — the trivial fixture (queries leak tool names).
- [internal/agent/tools/registry/registry_search_vector.go](internal/agent/tools/registry/registry_search_vector.go) ships the production embedding path (`ToolVectorIndex.Search`, embeddinggemma-300m @ 256-d MRL via Qdrant), **not covered by the eval today**.
- [internal/agent/tools/registry/examples.go](internal/agent/tools/registry/examples.go) — every native tool carries `[]ToolCallExample` with natural-language `Description` + `Arguments`. Eval seed material.
- **`D:/tmp/bench_dataset.json`** — 188 production query→tool pairs mined from real chat history (NOT in repo, sits in user's tmp).

## Top-20 tool frequency in bench_dataset.json (signal for category weights)

```text
search_memory(54)  execute_shell(52)  read_file(44)  execute_code(32)
search_files(20)   web_search(19)     web_fetch(19)  list_files(17)
list_tools(14)     apply_patch(10)    mcp_mail_imap_search_messages(8)
read_source(7)     schedule_task(6)   write_file(6)  read_tool(5)
list_tasks(5)      run_task_now(4)    tool_search(4) daily_briefing(3)
lint_sources(3)
```

Caveats from the data:
- Many tool names are *legacy sub-action* names (`read_file`, `write_file` = today's `file` tool with `action`). Need a **name aliasing map** before scoring.
- Some queries are deliberately ambiguous (`"Che palle"`, `"Guarda adesso"`) — the LLM picked a tool, but no human would call any specific tool "the right one". These should be **filtered or marked hit@K-only**, not precision@1.
- Multi-tool ground-truth exists (one query → 4 expected tools). Eval must handle multi-label.

## Pattern survey — what `D:/tmp` already has

| Source | Pattern | Lift |
|---|---|---|
| `D:/tmp/bench_dataset.json` | Production-mined IT+EN queries with multi-tool ground truth | **PRIMARY DATASET** — use as Layer 1 directly |
| `D:/tmp/openhuman/tests/agent_retrieval_e2e.rs` | Tool-direct retrieval test pattern (deserialise→retrieval→serialise round-trip) | Test structure template — but it's smoke, not quality |
| `D:/tmp/mem0/evaluation/evals.py` | LLM-judge + BLEU + F1 parallel eval framework | Reference for *future* LLM-as-judge layer (out of scope v1) |
| Published benchmarks (web research) | [MCP-Bench](https://arxiv.org/abs/2508.20453) distractor stress, [HumanMCP](https://arxiv.org/abs/2602.23367) 2800-tool dataset, [MCP-Atlas](https://arxiv.org/abs/2602.00933) 500-task public subset, [BFCL V4](https://gorilla.cs.berkeley.edu/leaderboard.html) AST function-call eval | Distractor pattern + cross-MCP query corpus |

## Four eval layers — auto-adaptive

### Layer 1 — Production-mined fixtures (already-built — copy into repo)

`D:/tmp/bench_dataset.json` is the seed. Promote it into the repo as the primary eval input.

**Algorithm** (US-RAG-A01):

1. Copy `D:/tmp/bench_dataset.json` → `internal/agent/tools/index/testdata/bench_mined.json`.
2. Add `internal/agent/tools/index/alias_map.go` mapping legacy sub-action names to current tool names (`read_file` → `file/read`, `write_file` → `file/write`, `search_files` → `file/search`, `apply_patch` → `file/patch`, `read_source` → `source/read`, `lint_sources` → `source/lint`, etc.).
3. Add a `quality` field to each entry by classification:
   - `clean` — query has clear intent and unambiguous expected tool (~70%)
   - `ambiguous` — query is vague ("Che palle", "Guarda adesso") — scored at hit@5 only
   - `multi` — multi-tool ground-truth — scored as "any expected tool in top-K"
4. Build `cmd/eval_mine_conversations/` that re-runs the mining pipeline (against the live `conversations` table) so the dataset can be refreshed without dumping `D:/tmp`. Setting-gated (`TOOL_RAG_EVAL_MINING=true`).
5. Sanitization pass: regex-strip emails / phone / API tokens / long opaque IDs before checking into repo.

**Property**: as the user keeps chatting with Aura, the mining pipeline re-pulls and the dataset grows organically. Real distribution of real prompts.

### Layer 2 — Autogen from tool examples (covers new tools without production traces)

When a new MCP lands, it has no production traces yet — the bench_mined.json doesn't know about it. The tool definition carries `description + examples`; we synthesize fixture entries from those.

**Algorithm** (US-RAG-A01, included):

1. Iterate `Registry.AllToolDefinitions()`.
2. For each tool, extract `description` + each `example.Description`.
3. Three transformations per example:
   - **Direct**: example description as query (control)
   - **Tool-name-masked**: regex out tool name + tokens → semantic-only query
   - **Italian paraphrase**: cached IT translation seeded from EN description
4. **Coverage assertion**: every name in `Registry.AllToolNames()` produces ≥3 generated queries OR has ≥3 entries in `bench_mined.json` (one of the two — total coverage per tool ≥3).
5. Translation cache `internal/agent/tools/index/testdata/eval_autogen_translations.json` regenerated via `cmd/eval_translate` calling Aura's LLM (temperature=0). CI verifies cache hash freshness against tool examples hash.

**Property**: when a new MCP lands and exposes tools with proper examples, the eval auto-includes them. When the user then *uses* those MCP tools in real conversations, Layer 1 mining picks up authentic queries and over time replaces the synthetic ones.

### Layer 3 — Distractor stress (MCP-Bench pattern)

Per-query, inject 50-100 distractor tools into the search index and verify the expected tool stays in top-K. Catches "new MCP swamped the index" regressions automatically.

**Algorithm** (US-RAG-A02):

- Synthetic distractors: 100 fake tools in `testdata/distractors.json`, templated from Aura's description style.
- For each Layer-1/Layer-2 query: search the registry+distractors, assert top-K contains the expected tool.
- Per-tool **robustness score**: % of queries that still hit when distractors are added. Tools below 70% logged for description rework (Aura's [internal/agent/tools/registry/examples.go](internal/agent/tools/registry/examples.go) and tool descriptions are the lever).

### Layer 4 — External benchmark imports (optional reach)

Three 2026 datasets are directly usable; we filter to the tools Aura's registry exposes and import the rest. *Lower priority than Layers 1-3* because Layer 1 is the authentic distribution.

| Dataset | Scale | Use case |
|---|---|---|
| [HumanMCP](https://arxiv.org/abs/2602.23367) | 2800 tools, 308 servers | Cross-MCP query naturalness — useful AFTER MCP roundup wave 1 |
| [MCP-Bench](https://arxiv.org/abs/2508.20453) | 250 tools, distractor methodology | Validates Layer 3 distractor design |
| [MCP-Atlas](https://arxiv.org/abs/2602.00933) | 220 tools, 500 public tasks | Multi-step coverage (less relevant for single-tool retrieval eval) |
| [LiveMCPBench](https://arxiv.org/abs/2508.01780) | 70 servers, 527 tools, 95 daily tasks | Reproducible scenarios |

**Wiring** (US-RAG-A03):

- `cmd/eval_fetch_external/` one-shot fetcher pulling overlapping subsets.
- Cache to `testdata/external_eval_cache.json` (gitignored).
- Merge with Layer 1+2 entries.

## How the four layers combine

```text
TestDiscoveryTopKEval    (lexical path, today's harness, expanded)
TestVectorTopKEval       (vector path, new — embeddinggemma 256-d via Qdrant or stub)

   ┌─────────── inputs ───────────┐
   │  Layer 1: bench_mined.json (already-built, primary)
   │  Layer 2: autogen from examples (always on, fills new-MCP gap)
   │  Layer 3: distractor injection (always on, layered over 1+2)
   │  Layer 4: external benchmarks (opt-in, MCP roundup phase)
   └──────────────────────────────┘

   ┌─────────── outputs ──────────┐
   │  precision@1, hit@3, hit@5, MRR (global + per-category, weighted by quality field)
   │  p50/p95 latency on the vector path
   │  per-tool robustness score under distractors
   │  CI gate: fail if global hit@3 < threshold OR any tool robustness < 60%
   └──────────────────────────────┘
```

## Phase shape — Phase-RAG (3 stories, ~1 session via Ralph)

### US-RAG-A01 — Promote bench_mined.json + Layer-2 autogen + alias map

- Copy `D:/tmp/bench_dataset.json` → `internal/agent/tools/index/testdata/bench_mined.json` after sanitization sweep.
- Add `internal/agent/tools/index/alias_map.go` for legacy→current tool name mapping (~30 LOC).
- Classify each entry as `clean`/`ambiguous`/`multi` — write `internal/agent/tools/index/eval_classifier.go` (~80 LOC) that runs on the dataset to add the `quality` field.
- Implement Layer-2 autogen in `internal/agent/tools/index/eval_autogen.go` (~120 LOC).
- Translation cache `testdata/eval_autogen_translations.json` with IT/EN pairs (~50 entries from current 14 native tools).
- Helper `cmd/eval_translate/main.go` (~80 LOC) calling Aura's LLM with temperature=0 to refresh translations.
- Test `TestFixtureCoversAllTools` asserts every registered tool name has ≥3 fixture entries (Layer 1 + Layer 2 combined).

ETA: ~400 LOC + dataset import. **Single commit** (data, scaffolding, test).

### US-RAG-A02 — Vector path eval + Layer 3 distractors + lexical fixture sunset

- New `internal/agent/tools/index/eval_vector_test.go` mirroring the lexical eval but driving `ToolVectorIndex.Search` against Qdrant+embeddinggemma via docker network; SKIPS when sidecars unreachable (matches the existing `AURA_SKIP_TOOL_EVALS=1` gate pattern).
- New `internal/agent/tools/index/eval_distractor.go` (~80 LOC) implementing the distractor injection.
- `testdata/distractors.json` with 100 synthetic distractors.
- Per-category thresholds + p95 latency assertion (< 200ms per the memory budget).
- **Deprecate the old `eval_topk_fixture.json`** (delete after this lands — it's superseded by `bench_mined.json` + autogen).
- Update `eval_topk_test.go` to read from `bench_mined.json` + autogen output instead of the legacy fixture.

ETA: ~250 LOC. Single commit. Depends on A01.

### US-RAG-A03 — Mining pipeline + external benchmark fetcher + CI gate

- `cmd/eval_mine_conversations/main.go` (~120 LOC) — pulls fresh data from the live `conversations` SQLite table, sanitizes, writes to `testdata/bench_mined.json`. Setting-gated.
- `cmd/eval_fetch_external/main.go` (~150 LOC) — pulls HumanMCP / MCP-Bench / MCP-Atlas public subsets, filters to tools we expose, caches gitignored.
- `cmd/probe_tool_rag/main.go` (~120 LOC) — single binary that runs both eval paths (lexical + vector), prints precision@1 / hit@3 / hit@5 / MRR / p95 latency, exits non-zero on threshold miss.
- Makefile target `make tool-rag-eval` + CI invocation gated by `AURA_RUN_TOOL_RAG=1`.

ETA: ~400 LOC + Makefile. Single commit. Depends on A02.

## Where this slots in the roadmap

```text
Phase-MM Wave 3 (Ralph in flight) — pocket-tts TTS reply
        ↓
Phase-MCP-UI v1 (sketched 2026-05-20) — dashboard config framework
        ↓
Phase-RAG (this doc, 3 stories, ~1 session) — auto-adaptive eval lands BEFORE bulk MCP roundup
        ↓
MCP roundup wave 1 — first 3-5 MCPs from the survey
        ↓
Wave 2..N — eval auto-extends per new MCP family; distractor stress catches regressions
```

Phase-RAG must land **before** MCP roundup wave 1 — adding tools without the auto-expanding eval = flying blind into a known-degradation regime per MCP-Bench's own findings (retrieval precision drops 15-30% per 50 distractor tools without proper coverage).

## Open questions

1. **`bench_mined.json` privacy**: it carries Davide's real Italian queries. Sanitization (emails/phones/tokens regex) is the v1 minimum. Should we go further (run an LLM pass to redact named entities)? Recommend v1 = regex sanitization + repo-private, no public dataset release.
2. **Translation cache freshness**: when a new tool example lands, the IT translation is missing. Strict (test fails) vs lenient (fall back to EN). Recommend strict.
3. **Distractor naming collision**: synthetic distractors must check `Registry.HasName()` and re-roll. Already in the plan.
4. **Quality classification automation**: classifying entries as clean/ambiguous/multi requires judgment. A v1 heuristic (query length, presence of imperative verbs, count of tools in ground-truth) gets us 80% accurate; the rest can be hand-curated as the dataset grows.

## Sources

- [internal/agent/tools/index/eval_topk_test.go](internal/agent/tools/index/eval_topk_test.go) — current eval
- [internal/agent/tools/registry/examples.go](internal/agent/tools/registry/examples.go) — Layer-2 autogen seed
- `D:/tmp/bench_dataset.json` — Layer-1 mined production dataset
- `D:/tmp/openhuman/tests/agent_retrieval_e2e.rs` — retrieval test structure reference
- `D:/tmp/mem0/evaluation/evals.py` — future LLM-judge framework reference (out of scope v1)
- [MCP-Bench paper](https://arxiv.org/abs/2508.20453) — distractor methodology
- [HumanMCP paper](https://arxiv.org/abs/2602.23367) — cross-MCP query corpus
- [MCP-Atlas paper](https://arxiv.org/abs/2602.00933) — 500 public tasks
- [BFCL V4](https://gorilla.cs.berkeley.edu/leaderboard.html) — function-call shape reference

## Related memory

- `feedback_minillm_cpu_not_viable_for_tool_retrieval` — locks embed+cosine architecture
- `feedback_hyperv_port_forwarding_lie` — measurement discipline for vector-path latency
- `project_mcp_productivity_roundup_milestone` — milestone this eval gates
