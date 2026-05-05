# Milestones - Aura

## v0.0-v0.12.0 (Informal - before GSD tracking)

Shipped through Phase 19a-19g.1 and sandbox.pyodide.5:
- Telegram agent with streaming + Markdown->HTML
- Mistral OCR pipeline
- SQLite scheduler + agent jobs
- Skills/MCP extensibility
- React dashboard with bearer auth
- Compounding memory (archive -> summarizer -> wiki review)
- AuraBot swarm
- Pyodide sandbox (execute_code enabled)
- XLSX/DOCX/PDF tools
- Settings store + setup wizard

Refer to `docs/implementation-tracker.md` for detailed slice-by-slice history.

## v1.0 - Production Readiness (COMPLETE)

Started: 2026-05-04
Completed: 2026-05-05

Goal: Make Aura safe to run as the daily production build by closing data-integrity, migration-safety, dashboard-security, memory-reliability, Telegram-regression, and release-gate blockers.

In scope:
- Shared SQLite pool with WAL, busy_timeout, and foreign_keys
- Versioned migrations and upgrade safety
- Observable conversation archive failures
- Dashboard token expiry
- Settings secret redaction
- Focused Telegram critical-path tests
- Final production release gates

Release gates passed:
- Automated Go/web/sandbox checks
- Migration fresh/upgrade/idempotence checks
- Release candidate package check
- Windows ZIP content inspection
- Manual Windows production smoke from the snapshot ZIP

Deferred to v1.1 Hardening Polish:
- MustResolveProfiles panic fix unless future evidence proves production/user-controlled reachability before v1.0
- File tool split
- Broad large-file refactors
- tray coverage polish
- telebot beta monitoring docs
- Full settings at-rest encryption unless redaction proves insufficient
- Arbitrary coverage targets outside Telegram critical paths

## v1.1 - Trustworthy Daily Use (ACTIVE)

Current milestone: v1.1 Trustworthy Daily Use
Current phase: Phase 1 - Panic Removal Gate
Current focus: remove production panic paths before observability and packaged Windows UX work

Goal: Make Aura safer and calmer for daily use by removing avoidable panics, surfacing quiet runtime failures, suppressing the packaged Windows console, and documenting dependency/platform watchpoints.

Hardening boundary:
- No new user-facing features
- No broad large-file refactors
- No memory-quality upgrades
- No settings at-rest encryption
- No separate `aura-console.exe`
