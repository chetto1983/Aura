## Current Position

Current milestone: v1.1 Trustworthy Daily Use
Current phase: Phase 1 - Panic Removal Gate
Current focus: remove production panic paths before observability and packaged Windows UX work
Status: v1.1 Trustworthy Daily Use started; v1.0 Production Readiness remains complete
Last activity: 2026-05-05 - Started v1.1 Trustworthy Daily Use docs and active state

## Project Reference

See: .planning/PROJECT.md (updated 2026-05-05)

**Core value:** Durable, compounding personal memory that grows smarter with every conversation - without relying on external note-taking apps.
**Current focus:** remove production panic paths before observability and packaged Windows UX work.

## Roadmap

See: .planning/ROADMAP.md

**Phases:**
- Phase 1: Panic Removal Gate
- Phase 2: Production Error Observability
- Phase 3: Platform And Dependency Hygiene
- Phase 4: Release Gate Lite

## Recent Activity

[2026-05-05] Started v1.1 Trustworthy Daily Use. Current milestone: v1.1 Trustworthy Daily Use. Current phase: Phase 1 - Panic Removal Gate. Current focus: remove production panic paths before observability and packaged Windows UX work.
[2026-05-04] Bootstrap new-milestone: audited CONCERNS.md and narrowed v1.0 to production-readiness blockers
[2026-05-04] Defined v1.0 requirements around DB, migrations, memory reliability, dashboard security, Telegram critical paths, and release gates
[2026-05-04] Created ROADMAP.md: 6 production-readiness phases with v1.1 deferrals recorded
[2026-05-04] Reconciled v1.0 around Production Readiness: DB foundation, migration safety, memory reliability, dashboard security, Telegram regression harness, and release gate.
[2026-05-05] Merged PR #1: Phase 1 DB Foundation + Phase 2 Migration Safety. `master` now has shared SQLite pool startup, versioned migrations, v3.0.2 upgrade coverage, and lazy schema ownership removed from shared constructors.
[2026-05-05] Completed Phase 3 Memory Reliability. Telegram archive helper logs direct append failures with chat/turn/role metadata and focused tests cover archive success plus failure observability.
[2026-05-05] Completed Phase 4a Dashboard Token Expiry. `api_tokens.expires_at` is migrated/backfilled, new tokens default to 720-hour TTL, production wiring applies `DASHBOARD_TOKEN_TTL_HOURS`, and expired tokens return distinct `token_expired` 401 bodies.
[2026-05-05] Completed Phase 4b Settings Secret Redaction. `GET /settings` no longer returns raw secret values or active values for LLM, embedding, Mistral, or Ollama keys; the dashboard treats configured-secret markers as placeholders only and does not save them back as raw values.
[2026-05-05] Completed Phase 5 Telegram Regression Harness. Focused hermetic tests now cover archive behavior, streaming edits, text access control, and document/OCR trigger behavior without live Telegram credentials.
[2026-05-05] Phase 6 automated gate passed through GoReleaser snapshot packaging. Fixed the Windows amd64 resource hook by adding `goversioninfo -64`; manual Windows production smoke followed as the final release gate.
[2026-05-05] Manual Windows production smoke passed from `dist/aura_3.0.3-snapshot_windows_x86_64.zip`; `REL-01` and v1.0 Production Readiness are complete.
