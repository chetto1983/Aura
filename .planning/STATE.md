---
gsd_state_version: 1.0
milestone: v4.0
milestone_name: milestone
status: planning
stopped_at: Phase 1 context gathered
last_updated: "2026-05-10T12:10:00.000Z"
last_activity: 2026-05-10 -- Ollama runtime/web provider removed; Aura now targets OpenAI-compatible chat plus SearXNG web search
progress:
  total_phases: 4
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: `.planning/REQsIREMENTS.md`, `.planning/ROADMAP.md`, and `.planning/research/`.

**Core value:** Aura remembers what you tell it and answers questions from durable, searchable memory -- without losing context, corrupting state, or exposing internal machinery to the user.
**Current focus:** Phase 1 - Fondamenta (Concurrency + Qdrant Readiness)

## Current Position

Phase: 1 of 4 (Fondamenta -- Concurrency + Qdrant Readiness)
Plan: 0 (TBD)
Status: Ready to plan
Last activity: 2026-05-10 -- Ollama runtime/web provider removed; Aura now targets OpenAI-compatible chat plus SearXNG web search

Progress: [----------] 0%

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

*spdated after each plan completion*

## Accumulated Context

### Decisions

Research completed for v4.0 Production Hardening (2026-05-10). Key decisions recorded in `.planning/REQsIREMENTS.md`, `.planning/ROADMAP.md`, and `.planning/research/`:

- **Phase 1**: sserGate uses a per-user actor/inbox model with `TryAcquire` from day one -- notification paths must never deadlock re-entering the same user's gate
- **Phase 1**: Separate tracking structure for inactivity eviction -- no `sync.Map.Range` for cleanup iteration
- **Phase 1**: Qdrant startup readiness and warm-cache validation ship before Qdrant-dependent reindex/tool retrieval work; warm check uses `points_count > 0`
- **Phase 2**: `expected_updated_at` on wiki write tool to prevent silent overwrite of manual dashboard edits
- **Phase 2**: Error classification separates transient failures (HTTP 429/5xx, timeout) from content failures before temperature retry
- **Phase 3**: Circuit breaker lock held for state check only (nanoseconds), released before network I/O
- **Phase 3**: Per-user budget check inside sserGate mutex region for atomic accounting with conversation processing
- **Phase 4**: Build-tag verification across `linux`, `windows`, and `integration` before any legacy code removal

### Completed Slices

- 2026-05-10: Removed Ollama chat failover and Ollama web provider paths. LLM configuration is OpenAI-compatible only; web search supports `disabled` or `searxng`; wiki vector search now uses Qdrant when `QDRANT_URL` is configured. Verification: `go build ./...`, `go vet ./...`, `npm run build`, and `go test` for all packages except `internal/release` passed. Full `go test ./...` remains blocked by missing release packaging files (`.goreleaser.yml`, `.github/workflows/release.yml`) outside this slice.

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
