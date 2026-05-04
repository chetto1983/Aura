# Aura — Personal AI Agent with Compounding Memory

## What This Is

Aura is a personal AI agent, local-first, accessible via Telegram, that accumulates knowledge in a markdown wiki maintained by the LLM and extends itself with agentic tools (source ingestion, web, scheduler, skills, MCP, sandbox). An embedded React dashboard offers observability and control protected by bearer tokens issued via Telegram.

## Core Value

**Durable, compounding personal memory that grows smarter with every conversation — without relying on external note-taking apps.**

## Requirements

### Validated

- ✓ Telegram streaming agent with Markdown→HTML rendering (v4.0/v4.1, Phases 1–12)
- ✓ Mistral OCR PDF pipeline with sha256 dedup + auto-ingest (slices 1–6)
- ✓ SQLite-backed scheduler: reminders, maintenance, agent jobs (slices 8, 17h–17i)
- ✓ Anthropic skills loader + skills.sh catalog install/delete (slices 11c, 11e–11f)
- ✓ MCP client (stdio + Streamable HTTP) with dashboard tool execution (slices 11a–11d)
- ✓ React 19 dashboard: wiki/graph, sources, tasks, skills, MCP, pending users, settings (slices 10a–10e, 14d–14e)
- ✓ Dashboard bearer auth + /start approval queue (slices 10d, 11o)
- ✓ Compounding memory: archive → summarizer → wiki proposals → review queue (Phases 12, 17g, 18)
- ✓ AuraBot swarm: planner, synthesis, routing, toolset profiles (Phases 17a–17q, 19d–19g)
- ✓ Pyodide sandbox for offline code execution (sandbox.pyodide.0–5)
- ✓ XLSX/DOCX/PDF generation tools (slices 15a–15e)
- ✓ Settings store with DB-overrides-env (slices 14a–14d)
- ✓ Conversation archive cleanup (slice 14.cleanup)
- ✓ History cap, parallel tool calls, speculative wiki retrieval (slices 11k–11p)
- ✓ Progressive Telegram streaming edits (slices 11s–11t)

### Active

- [ ] Shared SQLite pool with WAL, busy_timeout, and foreign_keys
- [ ] Versioned migrations and upgrade safety
- [ ] Observable conversation archive failures
- [ ] Dashboard token expiry
- [ ] Settings secret redaction in API responses and dashboard state
- [ ] Focused Telegram critical-path tests
- [ ] Final production release gates

### Out of Scope

- New feature development — this milestone is hardening-only
- Replacing chromem-go with sqlite-vector — already evaluated and rejected in slice 11h
- Real-time dashboard features (WebSocket) — not needed for hardening
- Mobile app — web dashboard is sufficient

## Context

- **Codebase:** Go monolith with embedded React 19 SPA. 30 internal packages, ~2469 lines in implementation tracker.
- **Database:** SQLite (`aura.db`) with shared path across auth, scheduler, settings, swarm, embed cache stores.
- **Wiki:** Markdown files on disk with YAML frontmatter and `[[slug]]` links. Optionally Git-backed.
- **LLM:** OpenAI-compatible HTTP client as primary; Ollama fallback. Mistral for embeddings and OCR.
- **Deployment:** Single binary (`aura.exe`) with `//go:embed all:dist`. Windows tray icon. Dev bundle at `runtime/pyodide/`.
- **Known issues:** Documented in `.planning/codebase/CONCERNS.md`; v1.0 closes production-readiness blockers from that audit and defers polish, broad refactors, and lower-confidence triage items to v1.1 Hardening Polish.

## Constraints

- **Tech stack:** Go 1.24+, SQLite, React 19 + Vite + shadcn, Pyodide 0.29.3
- **No new dependencies:** Prefer pure-Go solutions; avoid CGO unless proven necessary
- **Backward compatible:** Must not break existing user data or workflows
- **Windows-first:** Tray, browser-open, and release packaging must work on Windows; non-Windows graceful degradation acceptable

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| chromem-go over sqlite-vector for embeddings | Avoids CGO/native extension loading; gets ~99% of the win | ✓ Good |
| Settings in SQLite with env overlay | First-run wizard → DB writes → env fallback; no restart for most changes | ✓ Good |
| Pyodide offline bundle over host Python | Deterministic, no user installs, reproducible | ✓ Good |
| Phase 19 skill proposals review-only (Option A) | Explicit admin handoff for install/smoke; no silent mutations | ✓ Good |
| Single `aura.db` over per-store files | Simpler backup/operability; WAL mitigates write contention | — Pending |

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

## Current Milestone: v1.0 Production Readiness

**Goal:** Make Aura safe to run as the daily production build by closing data-integrity, migration-safety, dashboard-security, memory-reliability, Telegram-regression, and release-gate blockers.

**In scope:**
- Shared SQLite pool with WAL, busy_timeout, and foreign_keys
- Versioned migrations and upgrade safety
- Observable conversation archive failures
- Dashboard token expiry
- Settings secret redaction
- Focused Telegram critical-path tests
- Final production release gates

**Deferred to v1.1 Hardening Polish:**
- MustResolveProfiles panic fix unless future evidence proves production/user-controlled reachability before v1.0
- File tool split
- Broad large-file refactors
- tray coverage polish
- telebot beta monitoring docs
- Full settings at-rest encryption unless redaction proves insufficient
- Arbitrary coverage targets outside Telegram critical paths

---

*Last updated: 2026-05-04 after v1.0 production-readiness reconciliation*
