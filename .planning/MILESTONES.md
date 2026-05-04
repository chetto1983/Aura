# Milestones — Aura

## v0.0–v0.12.0 (Informal — before GSD tracking)

Shipped through Phase 19a–19g.1 and sandbox.pyodide.5:
- Telegram agent with streaming + Markdown→HTML
- Mistral OCR pipeline
- SQLite scheduler + agent jobs
- Skills/MCP extensibility
- React dashboard with bearer auth
- Compounding memory (archive → summarizer → wiki review)
- AuraBot swarm
- Pyodide sandbox (execute_code enabled)
- XLSX/DOCX/PDF tools
- Settings store + setup wizard

Refer to `docs/implementation-tracker.md` for detailed slice-by-slice history.

## v1.0 — Production Readiness (IN PROGRESS)

Started: 2026-05-04

Goal: Make Aura safe to run as the daily production build by closing data-integrity, migration-safety, dashboard-security, memory-reliability, Telegram-regression, and release-gate blockers.

In scope:
- Shared SQLite pool with WAL, busy_timeout, and foreign_keys
- Versioned migrations and upgrade safety
- Observable conversation archive failures
- Dashboard token expiry
- Settings secret redaction
- Focused Telegram critical-path tests
- Final production release gates

Deferred to v1.1 Hardening Polish:
- MustResolveProfiles panic fix unless production reachability promotes it back to a blocker
- File tool split
- Broad large-file refactors
- tray coverage polish
- telebot beta monitoring docs
- Full settings at-rest encryption unless redaction proves insufficient
- Arbitrary coverage targets outside Telegram critical paths

## v1.1 — Hardening Polish (PLANNED)

Follow-up hardening work that is useful but not part of the v1.0 production release gate.
