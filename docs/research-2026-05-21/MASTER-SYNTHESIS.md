# Master Synthesis — Research Scout 2026-05-21

Aggregates the 7 parallel scouts launched after Phase-DRIFT + EN-prompt restructure closed.
Each row links to the full deliverable in this directory.

---

## Source artifacts

| # | Agent | Output | Pages |
|---|-------|--------|-------|
| 1 | Codex deep-dive | `codex-patterns.md` | 464 |
| 2 | Elysia + nanobot | `elysia-nanobot-patterns.md` | ~580 (21 patterns) |
| 3 | picobot + cli-printing-press | `picobot-cpp-patterns.md` | ~440 (15 patterns) |
| 4 | hermes + openhuman + recursive-llm | `hermes-openhuman-recursive-patterns.md` | 642 |
| 5 | Online 2026 state-of-the-art | `online-2026-state-of-the-art.md` | 359 |
| 6 | Codebase cleanup audit | `codebase-cleanup-audit.md` | (full repo audit) |
| 7 | Web/Telegram consolidation | `web-telegram-consolidation.md` | (pending) |

---

## Top picks across all scouts — ranked by impact × cheapness

### Tier 1 — ship-now wins (low effort, high impact)

1. **Provider prompt-cache breakpoints** (Codex #1, Online #1) — Anthropic `cache_control`, OpenAI/DeepSeek `prompt_cache_key`. Codex maps it to `thread_id`. **~10-15 LOC** in `internal/llm/client.go`. TTFT −13 to −31%, 70-90% input cost cut. **No risk.** Locked layout: `[soul+agent+tools] → CACHE → [history] → CACHE → [user]`.

2. **Server-driven `end_turn` termination** (Codex #2) — exit loop on provider `end_turn: true` instead of treating MaxIterations as primary signal. **~25-40 LOC** in `internal/agent/loop.go`. Frees long analytical turns; iter cap becomes emergency brake only.

3. **Tool-output spill-to-disk + reference envelope** (Elysia/nanobot #1) — replace `[truncated]` marker with on-disk full payload + path + preview. Reuses Aura's SHA-keyed source store. **~180 LOC.** Kills the "I dropped data, retry the search" loop.

4. **Repeated external-lookup throttle** (Elysia/nanobot #2) — block identical `web_search` query / `web_fetch` URL after attempt N=2. Pure Go signature-map gate before `Execute`. **~80 LOC.** Measurable kill of chatty thrashing.

5. **`tasks_completed_string` inline state block** (Elysia/nanobot #3) — XML-tagged per-turn action ledger (`<task_N>` SUCCESSFUL/UNSUCCESSFUL) injected before step hint. **~120 LOC.** Prevents "I haven't searched yet" relapse after the LLM forgets prior tool results.

6. **Tool description ≤200 char audit + single system message** (picobot #2) — extend existing first-line marker audit; collapse multi-system-message build. **~5 min + 0.5 session.** Saves 500-1500 tokens/turn.

### Tier 2 — substantive wins (M effort)

7. **Stream-time parallel tool dispatch** (Codex #3, Online #3) — Codex `FuturesOrdered` pattern. Fire goroutines as tool_call JSON args close, not after stream end. **~130-180 LOC** across `internal/llm/client.go` + `internal/agent/loop.go`. 1.4-3.7× wall-clock speedup on tool-heavy turns.

8. **Kitchen-sink action-enum tool collapse** (picobot #1) — collapse `source_*` (6) + `scheduler_*` (3) + `wiki_*` (3) into ~3 enum-dispatched tools. **~−1100 LOC.** Hits the 22→8 surface target.

9. **openhuman payload_summarizer** (Hermes #1) — trait-based interception of oversized tool results, circuit-breaker (3-fail off-for-session), parent-only scope. **~300 LOC.** US-P8-G now concrete. Ship before Phase-RAG so big retrieval results don't blow context.

10. **Hybrid retrieval lock-in (BM25 + dense + cross-encoder + RRF k=60)** (Online #2) — Phase-WIKI-B already on this path; lock the recipe (`bge-reranker-v2-m3` sidecar). +27% nDCG@10, MRR@3 0.433→0.605.

11. **Web/Telegram 1+1 consolidation** (scout #7) — new `internal/agentcore.Builder` + `PerTurnHooks`; transports become thin hook providers. **8 stories, net −90 LOC INCLUDING the 7 deferred features.** Biggest single story CONS-04 (−360 LOC) needs `AURA_AGENTCORE_BUILDER` feature flag for 1 week of live traffic before delete. **LIVE BUG SURFACED**: `/api/chat` doesn't load AGENT.md/SOUL.md overlays — CONS-01 fixes it (~200 LOC touch, ship first).

### Tier 3 — cleanup hygiene (M effort, mechanical)

12. **God-file splits** (Cleanup audit #3-5) — `migrations.go` (1 431 LOC → 24 files), `probe_chat/cases.go` (1 587 LOC → 4-5 files), `memoryindex/store.go` (1 143 LOC → 4 files). **0 net LOC** but unlocks Phase-RAG/KV churn.

13. **MCP setup handler fold** (Cleanup audit #1) — `mcp_database_setup.go` + `mcp_setup.go` → generic `mcpSetupHandler[T]`. **−180 to −250 LOC.**

14. **Files registry parameterised registrar** (Cleanup audit #2) — `files_{docx,pdf,xlsx}.go` → one registrar. **−300 to −380 LOC** (largest file-level clone in repo).

15. **`os.Root` sandboxing** (picobot #3) — Go 1.24 native; deletes ~150 LOC manual path validation + workspace_validation.go; removes CVE class.

### Tier 4 — context-engineering substrate (L effort)

16. **hermes ContextEngine ABC + ContextCompressor** (Hermes #2) — pluggable compression interface, `SUMMARY_PREFIX` filter-safe markers, deterministic tool-result pre-pruning, JSON-validity-preserving arg truncation, streaming context-tag scrubber. **~400-600 LOC.** Required substrate for Phase-COMP / TokenJuice.

17. **openhuman TOML AgentDefinition + AgentTier + 3 spawn primitives** (Hermes #3) — 17 builtin agents, 8 `omit_*` prompt-section toggles, `agent_tier = chat|reasoning|worker`, statically validated hierarchy (chat→chat forbidden), `subagents` separate from `tools`. **~900 LOC.** Phase 8 substrate, now concrete.

18. **Token-budget compaction at 70% + JIT context loading** (Online #5) — Chroma "context rot" finding: length degrades 18 SOTA models. Compact at 70% wall, JIT-load wiki/source bodies on demand. Aura's 50-msg cap → token-budget cap.

---

## Anti-patterns reaffirmed (do NOT lift)

Multiple scouts independently confirmed:

- **Fast-path classifier bypassing agent loop** — picobot has it (`remember` regex pre-handler), Codex doesn't, openhuman doesn't, elysia/nanobot don't. 4/5 sources reject (the 5th has it AND it's the only one with quality issues). vLLM's "Semantic Router" is *replica-routing*, not *loop-bypass* — orthogonal.
- **LLM-as-reranker for retrieval** (picobot two-tier memory ranker). 2026 consensus: cross-encoder reranker, not LLM.

---

## Recommended sequencing (proposal, awaiting user confirm)

**Wave 1 — instrument-and-quick-wins** (≤1 session, low risk):
- Top picks #1 (prompt_cache_key), #2 (end_turn), #6 (description audit)
- Net: −500-1500 tok/turn, TTFT −13-31%, free emergency-brake from iter cap

**Wave 2 — output discipline + state surface** (≤2 sessions):
- Top picks #3 (spill-to-disk), #4 (lookup throttle), #5 (tasks_completed)
- Net: ~380 LOC up, but kills thrashing categories

**Wave 3 — consolidation + cleanup** (≤3 sessions, 8 atomic Ralph stories CONS-01..08):
- Top pick #11 (web/telegram 1+1) — CONS-01 first (fixes overlay-loading bug on web)
- Then #13 (mcp setup fold), #14 (files registrar) in parallel
- Net: −90 LOC + 7 features restored on web (streaming, voice, markdown, soft-budget, compaction, tools_allowed, tools_used) — full transport parity

**Wave 4 — agent loop architecture** (≤3 sessions):
- Top picks #7 (parallel dispatch), #8 (tool collapse), #9 (payload summarizer), #16 (ContextEngine)
- Net: 22→8 tools, 1.4-3.7× speedup, COMP substrate

**Wave 5 — god-file split** (mechanical, last):
- Top pick #12 (migrations/probe_chat/memoryindex splits)
- Net: 0 LOC, 3 god-files → 33 ≤300 LOC files

---

## Expected end-state if all waves land

- **Latency**: p50 5s → 2s; p95 10s → 3-5s (combined: cache hit + parallel dispatch + lookup throttle + token compaction)
- **Lines**: ~−1500 net LOC (cleanup deletes > additions)
- **Tools**: 22 → ~10 (kitchen-sink collapses)
- **Bench**: strict-pass 3/20 → 12-15/20 (cache + reranker + throttle + state ledger)
- **Operational**: 0 god-files, 0 production duplication clusters, 1+1 transport shape, golangci-lint clean

---

## Live bug found by scout #7 (FIX FIRST per CLAUDE.md "bugs are fixed when found")

`/api/chat` constructs the system prompt via `conversation.RenderSystemPrompt(now, loc)` alone — it never loads the operator overlays (`AGENT.md`, `SOUL.md`, `USER.md`, `TOOLS.md`). Telegram path uses `ComposeAgentPrompt` which DOES load them. Result: web users get a slim base prompt while Telegram users get the full agent personality. CONS-01 fixes by extracting prompt composition into a shared function both transports call. **~200 LOC touch, ship before any other consolidation.**

## Risks called out by scouts

- **payload_summarizer recursive dispatch** (openhuman has it disabled by default — `threshold_tokens=0` — after observing summarizer-summarizing-summarizer). Aura must wire `summarizer` agent's own summarizer to `None` and set thresholds above expected summary-output size.
- **hermes per-platform tool config cost lesson** — they had a $4.63 cron incident from default-on tools. Aura's lesson: default-off paid tools.
- **Provider cache layout sensitivity** — Anthropic vs OpenAI vs DeepSeek differ in breakpoint syntax. Wrap behind `internal/llm` provider adapter.
- **Migration file split** — must not break Go embed/include directives or test fixtures. Mechanical but boring.

---

*Update this doc as scouts report or when sequencing changes.*
