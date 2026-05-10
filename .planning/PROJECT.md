# Aura

## What This Is

Aura is a standalone second-brain Telegram assistant with an embedded React dashboard. It owns its source inbox, extracted evidence, compiled wiki, graph, review queue, and audit trail. The LLM Wiki memory pattern is the core: immutable sources become normalized extraction evidence, durable facts live in markdown wiki pages, and review-gated tools promote findings into the wiki.

## Core Value

Aura remembers what you tell it and answers questions from durable, searchable memory — without losing context, corrupting state, or exposing internal machinery to the user.

## Current Milestone: v4.0 Production Hardening

**Goal:** Fix critical correctness bugs, harden the LLM path with explicit tool-based wiki writes, add resilience patterns, and remove legacy code paths now that Qdrant is the canonical vector store.

**Target features:**
- Per-user mutex to prevent race conditions from concurrent Telegram messages
- Context leak cleanup with inactivity-based eviction
- Tool-based wiki page creation replacing fragile `looksLikeWikiYAML` heuristic
- Variable-temperature LLM retry (not blind 0-temp retry)
- Async wiki reindex with backpressure
- Circuit breaker on failover LLM providers
- Per-user token budget with global hard cap
- Embedding persistence (PGVector on SQLite)
- Explicit git commit tracking with `unversioned` flag on failures
- Removal of all legacy code paths superseded by Qdrant

## Active Product Truth

- Users can upload and manage evidence from Telegram and the dashboard.
- Source ingestion supports PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX.
- Extracted sources write `extract.md` and `extract.json`; PDF OCR also preserves `ocr.md` and `ocr.json`.
- `extract_complete` sources are eligible for wiki ingestion through the dashboard/API ingest action and the existing ingest pipeline.
- The dashboard is served from Go-embedded React assets under `internal/api/dist`.
- Qdrant (`aura_memory_v1_compact`) is the canonical vector store; SQLite FTS provides exact/fallback search.
- The runtime loop is compact: `internal/agentloop` with toolset-based tool gating.
- Telegram is a thin adapter over `internal/agentruntime`; session/result/finalization is runtime-owned.

## Guardrails

- Keep raw sources immutable.
- Use wiki `Body` content and `[[slug]]` links for durable pages.
- Keep upload-format claims truthful across API, Telegram, and frontend text.
- Prefer narrow, auditable extractors for shipped formats.
- No raw tool output or internal markers (`Evidence envelope`, `exit_code`, `elapsed_ms`, `source_id`) in user-facing answers.

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-05-10 after v4.0 milestone start*
