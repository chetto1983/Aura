---
phase: 42-llm-conversation-compaction
plan: 04
subsystem: assets
tags: [go, multimodal, content-parts, authorization, compaction]
requires: [42-03]
provides: [typed immutable content parts, reachability retention, capability projection, typed L1 edits]
affects: [42-05, provider-adapters, compaction-reconstruction]
requirements-completed: [IC-02, IC-08, IC-14]
completed: 2026-07-13
status: complete
---

# Phase 42 Plan 04: Typed Content Parts and Provider Projection Summary

**Authorized digest-verified content parts now retain reachable media and project verified bytes or explicit stored reference fallbacks without mutating canonical turns.**

## Accomplishments

- Added typed content-part metadata, immutable reachability links, authorization-on-reload, digest validation, migrated/quarantine outcomes, and reachability-aware expiry GC.
- Added additive migration 0037 for durable content-part metadata and immutable turn/checkpoint/memory reachability edges.
- Added provider-neutral capability projection that emits verified original bytes, explicit reference-only fallback, or fails mandatory unsupported modality.
- Added pure typed L1 edits that preserve authorized references and leave canonical turns unchanged.

## Task Commits

1. **Typed schema, lifecycle, authorization, and reachability** — `a936bca35`
2. **Capability projection and typed L1 safety** — `6ab5a2a22`

## Deviations from Plan

None beyond normal gate remediation.

## Issues Encountered

- Gate retry: the first Task 1 hook ran 130 seconds and rejected undocumented exported APIs; GoDoc was added and the single retry passed.
- Gate retry: native Windows cannot run `-race` without CGO; the exact race/database-tag suite passed under WSL with `CGO_ENABLED=1`.
- The file-size hook took 126–133 seconds per commit; every commit remained single-flight and was never bypassed.

## Verification

- `go test ./internal/assets ./internal/llm ./internal/conversations -run 'ContentPart|ContentProjection|ReferenceOnly|L1Typed' -count=1` — PASS.
- `CGO_ENABLED=1 go test -race -tags=db_integration ./internal/assets ./internal/llm ./internal/conversations -run 'Content|Artifact|L1' -count=1` — PASS under WSL.
- Normal pre-commit gofmt, vet, lint, and 600-line file-size gates — PASS.

## Self-Check: PASSED

- Task commits exist in history.
- Migration up/down files and typed projection artifacts exist.
- Unrelated `.planning/graphs/` files remain unstaged.
