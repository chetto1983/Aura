---
phase: 37A-web-artifact-delivery-lane
plan: 01
subsystem: assets
tags: [postgres, migration, s3, garage, object-store, rfc6266, content-disposition, streaming, identity]

# Dependency graph
requires:
  - phase: 37-agent-file-delivery
    provides: assets.Service (IngestTelegramFile, objectsFor, AssetKey), aura.assets schema, AssetService interface in internal/agui
provides:
  - "Migration 0035: widens aura.assets.source_kind CHECK to admit 'agent' (up), reverses via delete-then-narrow (down)"
  - "assets.SourceAgent constant + AgentIngestRequest struct (delivery-only ingest input)"
  - "assets.Service.IngestAgentFile — delivery-only ingest mirroring IngestTelegramFile but skipping Limits.Validate (D-04) and processAsset (D-03); stores under per-identity object key, returns owned Accepted asset with SourceKind=agent"
  - "assets.Service.OpenForIdentity — owner-scoped streaming read; checks ownership BEFORE touching the object store, returns (ReadCloser, asset) for owner and error (→404 upstream) for non-owner"
  - "internal/agui contentDisposition(filename) — RFC-6266 dual-param helper (filename= + filename*=UTF-8''); CRLF/quote-injection-safe, unicode-preserving"
  - "OpenForIdentity added to the internal/agui AssetService interface seam"
affects: [37A-02, 37A-03, 37A-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Delivery-only ingest path that intentionally bypasses per-modality Limits caps (agent deliveries are trusted, already-authored artifacts, not user uploads)"
    - "Ownership check precedes object-store access in OpenForIdentity (fail-closed 404 before any I/O)"
    - "RFC-6266 dual-parameter Content-Disposition with header-injection neutralization (strip CR/LF, escape quotes/backslash, percent-encode unicode)"

key-files:
  created:
    - internal/db/migrations/0035_assets_source_kind_agent.up.sql
    - internal/db/migrations/0035_assets_source_kind_agent.down.sql
    - internal/db/migrate_0035_integration_test.go
    - internal/assets/ingest_agent.go
    - internal/assets/ingest_agent_test.go
    - internal/assets/open_for_identity_test.go
    - internal/agui/content_disposition.go
    - internal/agui/content_disposition_test.go
  modified:
    - internal/assets/types.go
    - internal/assets/service.go
    - internal/agui/asset_service.go
    - internal/agui/assets_api_test.go

key-decisions:
  - "IngestAgentFile skips Limits.Validate (D-04) and processAsset (D-03): agent deliveries are trusted post-authoring artifacts, not user uploads subject to per-modality caps or thumbnail/derivative processing"
  - "OpenForIdentity verifies identity ownership before any object-store read so a non-owner never triggers backend I/O (fail-closed → 404 upstream)"
  - "contentDisposition emits BOTH filename= (ASCII fallback) and filename*=UTF-8'' (unicode) per RFC 6266, and neutralizes CR/LF + quote/backslash to block response-header injection"

patterns-established:
  - "Delivery ingest bypass: a parallel ingest entrypoint that shares object-key/asset plumbing with the upload path but omits validation/processing steps unsuitable for trusted deliveries"
  - "Owner-before-IO: authorization checks strictly precede resource access in streaming reads"

requirements-completed: [WEBART-01, WEBART-03]

coverage:
  - id: D1
    description: "Migration 0035 admits source_kind='agent' (INSERT succeeds after up; 23514 check-violation before up / after down)"
    requirement: "WEBART-01"
    verification:
      - kind: integration
        ref: "internal/db/migrate_0035_integration_test.go (build tag db_integration)"
        status: unknown
    human_judgment: false
    rationale: "Integration test compiles + is committed but requires the live Postgres stack (db_integration tag) — skips locally on Windows; verifier must run it against the stack to mark pass."
  - id: D2
    description: "IngestAgentFile stores bytes under the identity's per-identity object key, returns an owned Accepted asset with SourceKind=agent, and ingests a file that would fail a per-modality Limits cap (Limits.Validate bypassed, D-04)"
    requirement: "WEBART-01"
    verification:
      - kind: unit
        ref: "internal/assets/ingest_agent_test.go"
        status: pass
    human_judgment: false
  - id: D3
    description: "OpenForIdentity returns ReadCloser+asset for the owner and an error (→404 upstream) for a non-owner, checking ownership before touching the object store"
    requirement: "WEBART-03"
    verification:
      - kind: unit
        ref: "internal/assets/open_for_identity_test.go"
        status: pass
    human_judgment: false
  - id: D4
    description: "contentDisposition(filename) emits exactly one filename= and one filename*=UTF-8'' param, preserves unicode, and contains no raw CR/LF for any input"
    requirement: "WEBART-03"
    verification:
      - kind: unit
        ref: "internal/agui/content_disposition_test.go (property-tested)"
        status: pass
    human_judgment: false

# Metrics
duration: ~40min
completed: 2026-07-08
status: complete
---

# Phase 37A Plan 01: Backend Delivery Primitives Summary

**Four dependency-free backend seams for the web artifact delivery lane: migration 0035 (`agent` source kind), `IngestAgentFile` (Limits-bypassing delivery ingest), `OpenForIdentity` (owner-scoped streaming read), and an RFC-6266 injection-safe `contentDisposition` helper.**

## Performance

- **Duration:** ~40 min (executor ~37 min across 3 atomic task commits; orchestrator completed test-verify + merge + SUMMARY after an executor API-disconnect)
- **Completed:** 2026-07-08
- **Tasks:** 3
- **Files created:** 8 · **Files modified:** 4

## Accomplishments
- Migration 0035 widens the `aura.assets.source_kind` CHECK to admit `'agent'` (up) and reverses safely via delete-then-narrow (down), with a `db_integration` test asserting 23514 before/after.
- `assets.Service.IngestAgentFile` — delivery-only ingest that mirrors `IngestTelegramFile` but intentionally skips `Limits.Validate` (D-04) and `processAsset` (D-03), storing under the identity's per-identity object key and returning an owned Accepted asset with `SourceKind=agent`.
- `assets.Service.OpenForIdentity` — owner-scoped streaming read that verifies ownership *before* touching the object store (non-owner → error → 404 upstream), consumed later by the 37A-03 download route.
- `contentDisposition` RFC-6266 helper emitting dual `filename=`/`filename*=UTF-8''` params, neutralizing CR/LF + quote/backslash header injection while preserving unicode (property-tested).

## Task Commits

Each task was committed atomically (worktree branch, merged to master via merge commit `31668d6c`):

1. **Task 1: migration 0035 + AgentIngestRequest** — `dbbd0729` (feat)
2. **Task 2: IngestAgentFile + OpenForIdentity asset seams** — `9ee60b20` (feat)
3. **Task 3: RFC-6266 contentDisposition helper (property-tested)** — `473c5683` (feat)

**Merge:** `31668d6c` (merge(37A-01): backend primitives into master)

## Files Created/Modified
- `internal/db/migrations/0035_assets_source_kind_agent.{up,down}.sql` — CHECK widen / reverse for `source_kind='agent'`
- `internal/db/migrate_0035_integration_test.go` — 23514 assertions before-up / after-down (db_integration)
- `internal/assets/types.go` — `SourceAgent` constant + `AgentIngestRequest` struct
- `internal/assets/ingest_agent.go` + `_test.go` — `IngestAgentFile` (Limits/processAsset bypass)
- `internal/assets/service.go` — `OpenForIdentity` (owner-before-IO); refactored on touch to share ingest plumbing
- `internal/assets/open_for_identity_test.go` — owner/non-owner ownership tests
- `internal/agui/asset_service.go` — `OpenForIdentity` added to the `AssetService` interface
- `internal/agui/assets_api_test.go` — fake updated for the widened interface
- `internal/agui/content_disposition.go` + `_test.go` — RFC-6266 helper (property tests)

## Decisions Made
- Delivery-only ingest bypasses per-modality `Limits.Validate` (D-04) and `processAsset` (D-03): agent deliveries are trusted, already-authored artifacts, not user uploads.
- Ownership is checked before any object-store I/O in `OpenForIdentity` (fail-closed 404).
- `contentDisposition` emits both ASCII-fallback and UTF-8 params and strips CR/LF to block response-header injection.

## Deviations from Plan
This deviation log was reconstructed by the orchestrator: the executor agent was terminated by an API disconnect (`FailedToOpenSocket`) after committing all 3 tasks but before authoring its own SUMMARY. From commit inspection:
- `internal/assets/service.go` was refactored on touch (net −25 lines) to add `OpenForIdentity` and consolidate shared ingest plumbing — a refactor-on-touch consistent with CLAUDE.md, not a scope change.
- No `checkpoint` (Rule 4 / architectural) was ever returned, so no architectural deviations occurred.

No functional deviations from the plan's `must_haves` were observed. If the executor made auto-fixes (Rules 1–3) inside task commits, they are captured in the committed diffs but not individually enumerated here.

## Issues Encountered
- **Executor API disconnect mid-Task-3.** The `gsd-executor` worktree agent lost its API socket after all task commits landed but before test-verify + SUMMARY. The orchestrator recovered: verified `go build ./...` + `go vet` clean (IDE-reported compile errors were stale gopls state), ran the package tests green, merged the branch, and authored this SUMMARY.
- **`-race` unavailable locally.** Native `go test -race` needs `CGO_ENABLED=1` + a C compiler; on this Windows host that runs in WSL/CI per project setup. Race verification deferred to WSL/CI (same constraint the executor faced).
- **Concurrent repo activity.** The repo was being edited/committed in parallel (graph-store spike docs, ADR 0038, cron changes, settings.json) throughout — all disjoint from 37A code paths; the merge was conflict-free (verified via file-overlap check before merging).

## User Setup Required
None — no external service configuration required. (The `db_integration` migration test needs the Postgres stack up for the verifier to prove D1.)

## Next Phase Readiness
- **37A-02** can now wire `IngestAgentFile` into `send_file` + the composition root.
- **37A-03** can now build `GET /api/assets/{id}/download` on `OpenForIdentity` + `contentDisposition`.
- Both Wave-2 plans have their Wave-1 dependencies satisfied on master.

---
*Phase: 37A-web-artifact-delivery-lane*
*Completed: 2026-07-08*
