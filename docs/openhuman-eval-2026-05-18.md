# OpenHuman evaluation — 2026-05-18

Analysis of `tinyhumansai/openhuman` (cloned to `D:/tmp/openhuman`, 64 MB, 2896 files) for patterns adoptable to Aura.

## What OpenHuman is

Open-source AI desktop assistant. **Architecture mismatch with Aura**:
- OpenHuman: Tauri v2 desktop app (React + Rust core in-process), Windows/macOS/Linux installer, mascot character, native voice, Google Meet agent
- Aura: Telegram bot + web dashboard, single Go binary, headless mostly

So most product surface DOES NOT transfer. But three engineering patterns are high-ROI.

## Pattern 1 — TokenJuice token-compression layer (HIGH ROI)

**Location**: `src/openhuman/tokenjuice/` (~10 files, vendored rules in `vendor/rules/*.json`)

**What it does**: every tool output passes through a rule-based compaction layer BEFORE entering the LLM context. JSON rules describe how to compact specific commands. Example: `git status` output with 50 modified files compresses to `M: file1.rs, M: file2.rs, ... (47 more)`. Built-in rules cover archive (tar/zip), build (esbuild/tsc/vite/webpack), cloud (aws/az/gcloud), docker, git, npm, cargo, etc.

**Three-layer overlay** (priority ascending):
1. Builtin — JSON files vendored in `vendor/rules/*.json`, embedded via `include_str!`
2. User — `~/.config/tokenjuice/rules/`
3. Project — `.tokenjuice/rules/` relative to cwd

**Safety**: `compact_tool_output(name, args, output, exit_code)` is pass-through-safe:
- Skip if output < 512 bytes (no headroom)
- Skip if compaction ratio > 0.95 (not worth it)
- Failure-preserving (rules can have `failure.head`/`failure.tail` blocks that activate on non-zero exit)

**Why Rust port matters**: original is TypeScript (`vincentkoc/tokenjuice`). The Rust port preserves the rule-engine semantics 1:1 but as a library callable from the agent loop.

**Aura state today**: `internal/agent/governance.go` does:
- Microcompact: append a summary to old messages
- Truncate: `MaxToolResultChars` cap (default 24000)
- That's it. NO rule-based intelligent reduction.

**Aura ROI estimate**:
- `web_fetch` HTML pages: 30-90% reduction (tons of nav/footer noise)
- `ingest_source` extraction artifacts: 20-50% reduction
- `search_memory` results: probably 10-20% (already compact)
- `execute_shell` long outputs: 50-80% reduction (cargo/npm/git)

If Aura's average per-turn token budget is ~30k input, this layer could shave 5-10k tokens off the heavy tool turns. At Sonnet $3/Mtoken input, that's $15-30/M turns saved + faster TTFR.

**Adoption path**:
- Go port (1-2 weeks): `internal/tokenjuice/` with rule loader + classify + reduce
- Reuse the JSON rules vendored in openhuman/src/openhuman/tokenjuice/vendor/rules/ (license-permitting — check LICENSE)
- Wire into `internal/agent/loop.go` tool-result path BEFORE `governance.Apply`
- Add `internal/agent/tools/registry/` hook for tool-specific compaction call

**Caveat**: licensing. openhuman LICENSE check before porting JSON rules verbatim. The CODE pattern is fine to re-implement.

## Pattern 2 — Memory hierarchical structure (MEDIUM ROI)

**Location**: `src/openhuman/memory/` with subdirs `conversations/`, `ingestion/`, `ops/`, `safety/`, `schemas/`, `store/`, `store/agentmemory/`, `store/unified/`.

**What it does (per README)**: every connected source is canonicalized into ≤3k-token Markdown chunks, scored, folded into hierarchical summary trees, stored in SQLite + Obsidian-compatible `.md` vault.

**Aura state today**:
- `wiki/` filesystem markdown pages (similar to Obsidian vault concept — already aligned!)
- `compact_memory_documents` SQLite table (Phase 7 work)
- `internal/storage/memoryindex` + `internal/storage/sources/ingest`
- NO hierarchical summary tree — chunks live flat

**Worth taking?** Partially. The "hierarchical summary tree" is the differentiator. Phase 7C/D/F work already covers the chunk-level. Adding a tree-summary layer is Phase-8-tier work, not urgent.

**Adoption path** (if pursued):
- Phase 7G or Phase-8: add `summary_tree` table that nests `compact_memory_documents` chunks under parent summaries
- LLM-driven roll-up: every N new chunks → re-summarize parent
- Retrieval: BFS from query → leaf chunks via summary nodes

**Caveat**: Aura's wiki already serves part of this purpose (LLM-maintained markdown pages with `[[wiki-links]]` graph). Adding a parallel tree would be redundant. Maybe better to fold this into the EXISTING wiki: store the page hierarchy as a tree, summarize parents.

## Pattern 3 — Auto-fetch loop for connected sources (MEDIUM ROI)

**README claim**: "every twenty minutes the core walks each active connection and pulls fresh data into the memory tree"

**Search result**: no `auto_fetch` symbol in Rust code, likely named differently. Probably in `src/openhuman/integrations/` or similar.

**Aura state today**:
- `internal/cron/Scheduler` runs scheduled tasks
- `internal/storage/sources/store` is single-shot (user uploads, Aura ingests)
- No automatic periodic walk of "connections"

**Worth taking?** Modest. For Aura (Telegram-driven), the user is the source — auto-fetch is less applicable. BUT: could be useful for Phase-MM (auto-OCR pending PDFs after upload) or Phase-U (plugin-shaped integrations like "monitor this RSS feed every 20 min").

**Adoption path**:
- Phase-U feature: declare "auto-fetch interval" per plugin manifest
- Cron Scheduler triggers it
- Result routed to wiki via existing ingest pipeline

## Pattern 4 — Per-launch ephemeral bearer token (LOW ROI, security hardening)

**Location**: `app/src-tauri/src/core_process.rs` and `OPENHUMAN_CORE_TOKEN` env var.

**What it does**: every Tauri launch rotates a hex bearer for in-process Rust RPC. Frontend reads via `core_rpc_token` Tauri command.

**Aura state today**: bearer tokens issued via Telegram `/login` or DB INSERT, 7-day expiry, persist across container restarts (in `api_tokens` table).

**Worth taking?** Low. Aura's tokens are user-facing (dashboard login, probe runs) and need stability across sessions. The per-launch pattern is for in-process IPC where the token never leaves the host — different use case.

**Skip.**

## Pattern 5 — gitbooks/ public docs structure (LOW ROI, docs taste)

**Location**: `gitbooks/developing/`, `gitbooks/features/`, `gitbooks/legal/`, `gitbooks/overview/`.

**Aura state**: `docs/` is flatter, no public-facing book structure.

**Worth taking?** Modest, but only if Aura goes open-source/plugin-marketplace (Phase-U "marketplace" trigger). For single-author solo dev, the flat `docs/` is fine.

**Skip for now.**

## Patterns explicitly NOT taken

| OpenHuman feature | Why NOT for Aura |
|---|---|
| Mascot + lip-sync + Google Meet agent | Architectural mismatch (no desktop GUI) |
| 118+ integration registry | Aura uses MCP for that — already correct shape |
| Native voice (STT/TTS) | Already in Phase-MM plan (whisper-small-ita + Piper italiano locked) |
| Model routing (reasoning/fast/vision) | Phase-MM will introduce vision route; reasoning/fast not needed yet |
| Skills as separate GitHub repo | Aura is single-user; skill registry overhead not justified |
| Rust → Go | Re-implement, don't port directly |

## Recommendation: 1 thing to actually do

**Build `internal/tokenjuice/` for Aura (Pattern 1).**

Concrete plan:
1. Story: `US-AURA-TJ01 — implement internal/tokenjuice/ with builtin rules, port the classify+reduce algorithm in Go, wire into the agent loop tool-result hook`
2. ROI: 5-10k tokens/turn savings on heavy turns × ~$3/Mtoken Sonnet = $15-30/Mturns
3. Effort: 1-2 weeks (Rust port → Go re-implementation, plus the JSON rules vendored from openhuman with attribution)
4. Risk: low (TokenJuice is well-tested, the algorithm is deterministic, the integration is read-only and pass-through-safe)

This is Phase-T/Phase-MM-adjacent work — could land between Phase-QA3 and Phase-MM. Doesn't block multimodal.

## Action items

- [ ] After Phase-QA1.5+QA3 closes (~3-5h Ralph): consider `US-AURA-TJ01` as next milestone before Phase-MM
- [ ] Verify openhuman LICENSE allows re-implementing the rule JSON format / vendoring rules with attribution
- [ ] Reference [vincentkoc/tokenjuice](https://github.com/vincentkoc/tokenjuice) (original TypeScript) as a secondary source — may have evolved patterns the Rust port lacks
- [ ] Other patterns (memory tree, auto-fetch, gitbooks structure): documented here for future reference, NOT urgent

## Files of interest in the clone

For future re-reading:
- `D:/tmp/openhuman/src/openhuman/tokenjuice/mod.rs` — entry point + docs
- `D:/tmp/openhuman/src/openhuman/tokenjuice/classify.rs` — rule matching algorithm
- `D:/tmp/openhuman/src/openhuman/tokenjuice/reduce.rs` — main reduction pipeline
- `D:/tmp/openhuman/src/openhuman/tokenjuice/tool_integration.rs` — agent-loop glue + `compact_tool_output()` API
- `D:/tmp/openhuman/src/openhuman/tokenjuice/vendor/rules/` — all 30+ JSON rule files
- `D:/tmp/openhuman/AGENTS.md` — repo orientation for agents
- `D:/tmp/openhuman/gitbooks/developing/architecture.md` — narrative architecture

Cloned snapshot at `D:/tmp/openhuman/` per memory `feedback_check_tmp_sources_then_brainstorm_best` — read curated sources first, design after. Don't move or delete — user keeps their reference snapshots there.
