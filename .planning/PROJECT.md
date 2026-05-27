---
name: Aura
codename: aura
created: 2026-05-12
last_milestone_archived: 2026-05-12
last_updated: 2026-05-27
---

# Aura

## What This Is

A user-owned personal-agent platform. The substrate is domain-neutral — channel-neutral chat hub (Telegram, web `/api/chat`, cron, silent/swarm), LLM agent loop with parallel tool dispatch, Markdown-on-disk wiki under Git, hybrid graph + vector search, source ingestion (PDF/docx/xlsx → wiki), audio I/O, MCP plugin host, skills runtime. Personality and workflows are plugin-shaped: prompt overlays (`SOUL.md`, `AGENT.md`, `USER.md`, `TOOLS.md`), installable skills, MCP servers. Ships as one Go binary + embedded React dashboard + Docker stack with 7 sidecars.

## Core Value

**The wiki is the graph.** Markdown pages with `[[wiki-links]]` ARE the knowledge graph — write once, retrievable forever, owned by the user. Everything else (tools, channels, audio, dashboard) is in service of growing and querying that graph accurately. If retrieval-from-own-knowledge breaks, Aura is pointless.

## Requirements

### Validated

<!-- Shipped and confirmed valuable. -->

- ✓ **Telegram bot + agent loop** — channel-neutral `chat.Hub`, parallel tool dispatch, streaming Telegram edits — `internal/channels/telegram/`, `internal/chat/`, `internal/agent/loop.go`
- ✓ **Wiki as graph memory** — atomic Markdown writes (`temperature=0`), per-slug mutex, go-git tracking, FTS5 + graph indexes — `internal/wiki/store.go`
- ✓ **Source ingestion pipeline** — PDF/docx/xlsx/pptx + 6 more formats via Mistral OCR + markitdown sidecar + LLM extractor — `internal/storage/sources/{store,ocr,ingest,markitdown}/`
- ✓ **Hybrid search** — Qdrant vector (256d embeddinggemma via llama.cpp sidecar) + FTS5 + memoryindex — `internal/storage/{search,qdrant,memoryindex}/`
- ✓ **Embedded React 19 dashboard** — Vite build + `//go:embed`, bearer-token auth, settings catalog, wiki/sources/tasks/skills/MCP/conversations endpoints — `internal/api/`, `web/`
- ✓ **Audio I/O** — Whisper STT + Pocket-TTS Italian voice (giovanni INT8), per-chat voice mode (off/voice_only/all) — `docker/whisper/`, `docker/pocket-tts/`, `internal/channels/telegram/`
- ✓ **MCP plugin host** — stdio + Streamable-HTTP JSON-RPC 2.0, dynamic registration, fsnotify reload of `mcp.json` — `internal/mcp/`
- ✓ **Skills runtime** — Anthropic SKILL.md format with frontmatter, content-hash reconciler — `internal/skills/`
- ✓ **Scheduler + tasks** — SQLite-backed `scheduled_tasks`, due-task dispatch — `internal/cron/`
- ✓ **TokenJuice compaction** — KV-aware payload summarizer in the agent executor — `internal/tokenjuice/`
- ✓ **First-run setup wizard** — loopback web form, Telegram token + LLM config bootstrap — `internal/api/setup_server.go`
- ✓ **Conversation archive** — per-turn archive with tool_calls JSON, replay anchor — SQLite `conversations`
- ✓ **Backup pipeline** — S3-compatible export to local Garage — `internal/backup/`
- ✓ **Multi-channel substrate** — Telegram, web `/api/chat`, cron, silent/swarm channels all behind `chat.Hub`
- ✓ **DB recovery toolkit** — REINDEX + FTS5 rebuild recipe for SQLite WAL corruption — `internal/dbrecovery/`

### Active

<!-- Current scope. Building toward these. -->

- [ ] **Codebase cleanup (Phase-CLEAN milestone)** — 29 atomic commits across 6 waves: errcheck, staticcheck, CI hard-gate, cross-file dupl folds, intra-file dupl folds, test cleanup. Goal: zero new lint findings, CI hard-gate flipped on. Plan in `docs/phase-clean-plan-2026-05-27.md`.

### Out of Scope

<!-- Explicit boundaries. Includes reasoning to prevent re-adding. -->

- **Video generation (out & in deferred)** — $0.05–0.50/sec output prohibitive; video IN deferred to optional Gemini integration (`reference_phase_mm_multimodal_costs_2026-05-18`)
- **External graph DB (KuzuDB / Neo4j / Zep)** — the wiki IS the graph; adding a separate graph DB duplicates state and adds failure modes (`project_graph_memory_core_strategy`)
- **Fast-path classifier / router** — confirmed anti-pattern by 4 independent production systems; correct pattern is tightening the agent loop (`feedback_check_tmp_sources_then_brainstorm_best`)
- **GPU embeddings** — CUDA 2000ms/query vs CPU 32ms for single-query latency-bound workload; CPU image is the default (`feedback_gpu_not_for_embedding_workload`)
- **Native mobile app** — Telegram is the mobile UX; no parallel Android/iOS client
- **Multi-tenant / SaaS** — Aura is user-owned single-tenant; "platform" means plugin-extensible, not multi-user
- **Marketplace / app store** — substrate is platform-shaped but not productized; stress-test with own use before marketplace decisions (`feedback_aura_is_platform_shaped`)
- **Mistral chat LLM** — embedding backend locked on embeddinggemma-300m via llama.cpp; Mistral OUT for chat (`feedback_embedding_backend_stays_mistral`)
- **Regex for natural-language triggers** — too fragile across language/tense/paraphrase; use structured ground truth (registry, tool_use blocks, log) instead (`feedback_no_regex_for_nlp`)

## Context

- **Single-user / self-hosted by design** — user is `dvdmarchetto@gmail.com`, Telegram is the primary channel, Docker stack runs on a 16-core mini-PC shared with user work (CPU budget rule applies).
- **Recent shipped scope (2026-05-12 → 2026-05-27, ~2 weeks GSD-free):**
  - Phase-OP+ (substrate ops), Phase-FIX (4 stories), Phase-MM Wave 1.5/2/3 (audio IN + TTS swap to pocket-tts), Phase-WIKI-B Wave A/B + WIKI-FIX (bedrock gate lifted), Phase-CONS (in-flight consolidation per CONCERNS H-7)
  - 32+ commits in single sessions; Codex used as parallel-session implementer (`feedback_codex_parallel_session_pattern`, `feedback_codex_over_ralph`)
- **Operating discipline (live rules):**
  - Bugs are fixed when found, not deferred (CLAUDE.md §BUGS)
  - Deep refactor on touch — every file edited is cleaned in the same commit (CLAUDE.md §DEEP REFACTOR)
  - All prompts in English; output language set by directive only (`feedback_all_prompts_in_english_only`)
  - Probes verify the artifact (bytes / DB rows / files), not the reply (CLAUDE.md §NEVER BE SUPERFICIAL)
- **Roadmap beyond Phase-CLEAN (memory snapshot, will refresh after milestone closes):** Phase-ONB (onboarding) → Phase-RAG Layer 1 (retrieval bench) → Phase-MCP-UI (dashboard for community MCPs) → Phase-ROUNDUP (productivity MCP survey/swap) → DGX Spark bundle (business-gated on Andrea per `project_aura_dgx_spark_bundle_vision`).

## Constraints

- **Tech stack**: Go 1.26.2 (no CGO — pure-Go SQLite via modernc), React 19 + Vite + Tailwind v4, Python 3.12/3.13 for sidecars only. — Locked: Go single-binary + embedded SPA is the deploy shape.
- **Self-hosted only**: no SaaS, no cloud auth, no telemetry. — User-owned is the product.
- **Telegram-first UX**: every feature must work in Telegram before being added to the dashboard. — Dashboard is admin/inspection, not primary chat.
- **CPU budget**: mini-PC is shared with user's work; embed sidecar ≤4 threads, index concurrency ≤4, no busy-loop polling. — `feedback_minipc_cpu_budget`.
- **Determinism**: wiki writes at `temperature=0`, versioned prompts (`schema_version`, `prompt_version`), atomic temp+rename, file-level mutex. — Reproducible knowledge base.
- **File size**: 600 LOC max per file (CLAUDE.md §GOD CLASS); 1 violator currently grandfathered (`cmd/probe_chat/cases.go` at 1511 — Phase-CLEAN target).
- **Security**: tool argument privacy (only names + arg keys logged, never values); secrets via 3-way wiring (settings → secretKeyMappings → `secrets.Key`); bearer tokens SHA-256 hashed in `api_tokens`.
- **Cross-platform dev (Windows host) / Linux deploy**: SQLite journal mode forced to DELETE on Windows bind-mount (CONCERNS C-2); npm lock cross-platform drift workaround in CI (CONCERNS H-3).

## Key Decisions

<!-- Decisions that constrain future work. Add throughout project lifecycle. -->

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Wiki IS the graph (no external graph DB) | Avoid dual-state duplication; Markdown + [[links]] + go-git tracks deltas natively | ✓ Good |
| Embedding backend = embeddinggemma-300m via llama.cpp sidecar | 256d MRL truncation, CPU-only, 32ms/query; single path no toggle | ✓ Good |
| CPU embeddings (not GPU) | Latency-bound single-query workload; CUDA 2000ms vs CPU 32ms | ✓ Good |
| No fast-path classifier | 4 production systems (codex, elysia, nanobot, openhuman) confirm anti-pattern; tighten the agent loop instead | ✓ Good |
| All prompts in English | Mixing IT/EN degraded LLM rule-following (Ralph LAT-02); output language is a directive | ✓ Good |
| TokenJuice compaction (rivo/uniseg) | KV-cache savings 5-10k tok/turn on heavy turns; ported from openhuman | ✓ Good |
| Pocket-TTS giovanni INT8 (TTS) | Piper Paola robotic; pocket-tts RTF 0.61 + 200ms first-chunk streaming | ✓ Good |
| Codex over Ralph for implementation | Atomic per-concept commits; Ralph drifts with extra cleanup chores + driver-stuck after iter-1 | ✓ Good |
| Master-direct workflow (no feature branches) | Solo dev, atomic commits, no PR ceremony unless explicitly asked | ✓ Good |
| Channel-neutral `chat.Hub` (4 channels) | Substrate domain-neutral; new channels are adapters not new agent loops | ✓ Good |
| Wiki write determinism (`temperature=0`) | Reproducible knowledge base; same source = same page | ✓ Good |
| CLAUDE.md §DEEP REFACTOR ON TOUCH | Cleanup-later pattern forbidden; every commit leaves touched files clean | ✓ Good (Phase-CLEAN encodes this for the lint backlog) |
| SQLite journal_mode = DELETE on Windows | WAL + bind-mount corruption mode (live incident); Linux production unaffected | ⚠️ Revisit — needs code-side guard (CONCERNS C-2) |

## Current Milestone: v4.1 Codebase Cleanup (Phase-CLEAN)

**Goal:** Eliminate the lint backlog accumulated across 2 weeks of GSD-free shipping. Flip the CI hard-gate on so the no-debt rules from CLAUDE.md are mechanically enforced going forward.

**Target features (substrate health, not user-facing):**
- Errcheck findings → 0 (50 leaks across `internal/install/`, `internal/dbrecovery/`, HTTP body close, log lifecycle, file/dir handles, sandbox temp cleanup)
- Staticcheck findings → 0 (De Morgan rewrites, single commit)
- CI hard-gate flipped from warning → fail (lint + dupl + file-size)
- Cross-file dupl clusters → 0 production (currently 41, including 6-way wiki cluster)
- Intra-file dupl clusters → 0 production
- Test cleanup (12 commits) — fixture extraction once CI gate is live

**Key context:**
- Plan is fully drafted in `docs/phase-clean-plan-2026-05-27.md` (1031 LOC, 29 atomic commits across 6 waves)
- Sequence: W0 (baseline) → W1 (errcheck, 9) → W4 (staticcheck, 1) → W6 (CI hard-gate, 2) → W2 (cross-file dupl, 12) → W3 (intra-file dupl, 7) → W5 (test cleanup, 12)
- Execution shape: Codex one commit at a time OR Claude inline; deep-refactor-on-touch is mandatory per commit
- Active `prd.json` is Phase-CONS (separate, no overlap with Phase-CLEAN)
- US-CLEAN-10 (6-way wiki cluster) explicitly active — Phase-WIKI-B gate already lifted

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd:transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-05-27 after Phase-CLEAN milestone bootstrap (post-`.planning/` rescan).*
