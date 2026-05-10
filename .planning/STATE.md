---
gsd_state_version: 1.0
milestone: v4.0
milestone_name: milestone
status: executing
stopped_at: Phase 1 context gathered
last_updated: "2026-05-10T13:02:14.946Z"
last_activity: 2026-05-10 -- Phase 01 wave 2 complete (01-03 qdrant consumers migrated + 01-04 UserGate wired into telegram)
progress:
  total_phases: 4
  completed_phases: 0
  total_plans: 5
  completed_plans: 4
  percent: 80
---

# Project State

## Project Reference

See: `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`, and `.planning/research/`.

**Core value:** Aura remembers what you tell it and answers questions from durable, searchable memory -- without losing context, corrupting state, or exposing internal machinery to the user.
**Current focus:** Phase 01 — fondamenta-concurrency-safety

## Current Position

Phase: 01 (fondamenta-concurrency-safety) — VERIFIED with gaps (7/9 must-haves)
Plan: 5 of 5 plans complete; gap-closure plan pending
Status: Awaiting gap-closure plan (1 BLOCKER: warm-cache check; 1 WARNING: queued-turn notice + configurable inbox params)
Last activity: 2026-05-10 -- Phase 01 verification: gaps_found (see 01-VERIFICATION.md)

Progress: [##########] 100% (gap closure required before phase ships)

## Performance Metrics

**Velocity:**

- Total plans completed: 0
- Average duration: N/A
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

**Recent Trend:**

- No plans executed yet.

*Updated after each plan completion*

## Accumulated Context

### Decisions

Research completed for v4.0 Production Hardening (2026-05-10). Key decisions recorded in `.planning/REQUIREMENTS.md`, `.planning/ROADMAP.md`, and `.planning/research/`:

- **Phase 1**: UserGate uses a per-user actor/inbox model with `TryAcquire` from day one -- notification paths must never deadlock re-entering the same user's gate
- **Phase 1**: Separate tracking structure for inactivity eviction -- no `sync.Map.Range` for cleanup iteration
- **Phase 1**: Qdrant startup readiness and warm-cache validation ship before Qdrant-dependent reindex/tool retrieval work; warm check uses `points_count > 0`
- **Phase 2**: `expected_updated_at` on wiki write tool to prevent silent overwrite of manual dashboard edits
- **Phase 2**: Error classification separates transient failures (HTTP 429/5xx, timeout) from content failures before temperature retry
- **Phase 3**: Circuit breaker lock held for state check only (nanoseconds), released before network I/O
- **Phase 3**: Per-user budget check inside the UserGate serialization region for atomic accounting with conversation processing
- **Phase 4**: Build-tag verification across `linux`, `windows`, and `integration` before any legacy code removal

### Completed Slices

- 2026-05-10: Removed Ollama chat failover and Ollama web provider paths. LLM configuration is OpenAI-compatible only; web search supports `disabled` or `searxng`; wiki vector search now uses Qdrant when `QDRANT_URL` is configured. Verification: `go build ./...`, `go vet ./...`, `npm run build`, and `go test` for all packages except `internal/release` passed. Full `go test ./...` remains blocked by missing release packaging files (`.goreleaser.yml`, `.github/workflows/release.yml`) outside this slice.
- 2026-05-10: Cleaned stale handoff references after the Docker-first/Ollama removal pass. Release workflow tests now skip absent legacy desktop packaging files instead of failing the whole suite, Docker workflow assertions match the tracked workflow, and live handoff docs point to `.planning/STATE.md`/`.planning/ROADMAP.md` instead of removed tracker files. Verification: `go test ./...`, `go vet ./...`, and `go build ./...` passed.

### Pending Todos

None yet.

### Blockers/Concerns

None yet.

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| *(none)* | | | |

## Session Continuity

Last session: --stopped-at
Stopped at: Phase 1 context gathered
Resume file: --resume-file
