---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 05
subsystem: documents
tags: [neo4j, cypher, documents, rag, graphrag, identity-isolation, multi-user, fail-closed, spike-085]

# Dependency graph
requires:
  - phase: 36-02
    provides: "config.MUSRIsolation (AURA_MUSR_ISOLATION) rollout flag — the scoped-vs-unscoped path selector this plan consumes"
  - phase: 36-04
    provides: "established owner-scoping pattern (WithIdentityTx + *ForIdentity + D-06) mirrored here for the graph plane"
provides:
  - "IdentityID threaded through IngestRequest -> ExtractedDocument -> SearchRequest"
  - "(:User {identifier})-[:HAS_DOCUMENT]->(:Document) ownership edge MERGEd atomically on every ingest (flag-independent)"
  - "six identity-scoped Cypher variants with an unconditional EXISTS ownership filter, flag-gated by config.MUSRIsolation (D-13)"
  - "document_search tool threads the principal via identityctx.IdentityID(ctx)"
  - "fail_closed_integration_test.go — live spike-085 harness (flag-ON fail-closed + flag-OFF reversibility)"
affects: [36-06, 36-10, 36-12]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "graph-side identity isolation: an unconditional post-YIELD EXISTS ownership predicate (index can't be node-restricted) fails closed on empty/foreign identity"
    - "flag-as-path-selector (D-13): config.MUSRIsolation picks the scoped vs the RETAINED unscoped query constant — NOT an in-query `= \"\" OR EXISTS` conditional (Pitfall 5)"
    - "ingest attaches ownership flag-independently so the graph is owner-ready before the plan-12 flip (no re-ingest)"

key-files:
  created:
    - internal/documents/fail_closed_integration_test.go
  modified:
    - internal/documents/types.go
    - internal/documents/ids.go
    - internal/documents/indexer.go
    - internal/documents/search.go
    - internal/documents/retrieve.go
    - internal/documents/graphrag.go
    - internal/documents/service.go
    - internal/agent/tools/document_search.go
    - internal/assets/document_processor.go
    - cmd/aura/docs.go

key-decisions:
  - "The AURA_MUSR_ISOLATION flag (NOT an in-query conditional) is the fail-closed-vs-fallthrough selector; the pre-existing unscoped query is RETAINED, not hard-removed (D-13 reversibility)"
  - "Ingest MERGEs the HAS_DOCUMENT owner edge on EVERY upsert regardless of the flag — the flag gates read enforcement only, so plan 12 can flip without a re-ingest"
  - "Empty identity fails closed via BOTH an unconditional `$identity <> \"\"` graph predicate AND a Go-side guard (defense-in-depth); the integration test proves the query predicate independently of the guard"
  - "Ownership MERGE uses the mandatory MERGE -> WITH u -> MATCH fence (spike-085 gotcha #a), mirroring the shipped memory LINK_USER_TO_ENTITY shape verbatim (D-09)"

patterns-established:
  - "identity-scoped Cypher variant = base query + `AND $identity <> \"\" AND EXISTS { (:User {identifier:$identity})-[:HAS_DOCUMENT]->(:Document {id: <node>.document_id}) }`; $identity bound in both paths, ignored by the unscoped query"
  - "consumer wiring: set MUSRIsolation on BOTH the Service and its injected Searcher from one config.MUSRIsolation so dense/sparse-fallback/expand seeds scope together"

requirements-completed: []

metrics:
  duration_minutes: 15
  tasks_completed: 3
  files_created: 1
  files_modified: 10
  commits: 3
  completed_date: 2026-07-05
---

# Phase 36 Plan 05: Documents-plane Identity Isolation (spike-085 leak fix) Summary

Closed the PROVEN documents-plane leak (spike 085): the `internal/documents` Neo4j pipeline was identity-blind, so a `document_search` for any identity returned every identity's chunks. Mirrored the shipped memory `:User`-ownership pattern (D-09) — a `HAS_DOCUMENT` ownership edge on ingest plus a FLAG-GATED identity-scoped retrieval path across all six queries — wired to the D-13 reversible-rollout flag `AURA_MUSR_ISOLATION` (default OFF; plan 12 flips it ON post-backfill).

## What shipped

- **Identity threading (Task 1, `fe8f1827`):** `IdentityID` added to `IngestRequest`, `ExtractedDocument`, `SearchRequest`; `BuildExtractedDocument` threads `req.IdentityID -> doc.IdentityID`. `documentUpsertQuery` now atomically MERGEs `(:User {identifier: $identity})-[:HAS_DOCUMENT]->(:Document)` in the SAME write as the `:Document` upsert (no extra Write call — the 5-write indexer contract holds), with the mandatory `MERGE -> WITH u -> MATCH` fence (spike-085 gotcha #a). Attached on EVERY ingest regardless of the flag. The asset document processor sets `IngestRequest.IdentityID` from `asset.IdentityID`.
- **Flag-gated dual retrieval (Task 2, `cc9181ef`):** `MUSRIsolation bool` on `Searcher` and `Service`. Six identity-scoped variants — `sparseSearchQueryScoped`, `docScopedSparseQueryScoped`, `vectorSeedQueryScoped`, `docScopedVectorSeedQueryScoped`, `neighborExpandQueryScoped`, `graphExpandQueryScoped` — each carrying the UNCONDITIONAL `$identity <> "" AND EXISTS {...}` ownership predicate (post-YIELD for the fulltext/HNSW seeds; in the WHERE for the MATCH pre-filters and expands). `Searcher.Search`/`Service.seedHits`/`expandWinners`/`GraphRAG` select scoped-vs-unscoped by the flag; the pre-existing unscoped queries are RETAINED (D-13). Belt-and-suspenders Go-side empty-identity guard. `document_search.go` threads `identityctx.IdentityID(ctx)`. Consumer wiring in `docs.go` sets the flag on the tool's Service+Searcher from `cfg.MUSRIsolation`.
- **Live fail-closed proof (Task 3, `2e722536`):** `fail_closed_integration_test.go` (`//go:build neo4j_integration`) ports the spike-085 harness onto the real Indexer/Searcher/Service — flag-ON empty/cross-deny/own + GraphRAG cross-deny, flag-OFF unscoped-fallback reversibility, with a raw-query sub-check proving the empty-identity predicate independently of the Go guard.

## Verification

- `go build ./...` + `go vet ./...` (repo-wide) — clean on this Windows host.
- `go test ./internal/documents/ ./internal/agent/tools/ ./internal/assets/ ./cmd/aura/` — all green (untagged). Existing unit tests unaffected: the flag defaults OFF, so the unscoped query constants are selected verbatim and the extra `$identity` param is ignored (no exact-map assertions).
- Acceptance greps: `HAS_DOCUMENT` and `WITH u` present in indexer.go; six `*Scoped` query constants; `grep '"" OR EXISTS' internal/documents/*.go` == 0 (no conditional fallthrough); `MUSRIsolation` present across 5 documents files; `identityctx.IdentityID` in document_search.go.
- `go test -tags neo4j_integration -run TestDocumentsFailClosed ./internal/documents/` — COMPILES + vets under the tag; SKIPS locally (no `NEO4J_PASSWORD`/live stack) and would `t.Fatal` under `$CI`. See Deferred Verification.

## Deviations from Plan

### Auto-added / auto-wired (Rule 2 / Rule 3 — necessary functionality)

**1. [Rule 3 - Blocking] `internal/documents/ids.go` (in documents package, not in files_modified)**
- `BuildExtractedDocument` threads `req.IdentityID -> doc.IdentityID`. Without it, identity never reaches the indexer's ownership MERGE. Commit `fe8f1827`.

**2. [Rule 3 - Blocking] `internal/documents/service.go` (in documents package, not in files_modified)**
- Added `Service.MUSRIsolation bool` — the vector-seed + expand stages use `s.Knowledge` directly (not via the Searcher interface), so the Service needs its own flag field. Commit `cc9181ef`.

**3. [Rule 2 - Missing functionality] `internal/assets/document_processor.go` (ingest wiring, beyond files_modified)**
- Set `IngestRequest.IdentityID = asset.IdentityID`. Task 1's action explicitly describes this ("the ingest path sets IdentityID from the asset's IdentityID"); the file was simply absent from `files_modified`. Without it every ingested doc gets an empty owner. Commit `fe8f1827`.

**4. [Rule 2 - Missing functionality] `cmd/aura/docs.go` (consumer wiring, beyond files_modified)**
- Set `MUSRIsolation` on the `Service`+`Searcher` from `cfg.MUSRIsolation` in `docsToolSearcher.Retrieve` (the live document_search backend) and `newDocsService`. The plan's `<artifacts_produced>` lists this ("Consumer wiring: config.MUSRIsolation threaded into Searcher/Service") — without it the flag is dead code. Safe because the flag defaults OFF (unscoped behavior unchanged until the plan-12 flip). Commit `cc9181ef`.

### Deviation from a literal instruction

**5. Task 3 "package goleak TestMain" — NOT added.** The documents package has no goleak `TestMain`, and the sibling `neo4j_integration` tiers (`search_prefilter_live_test.go`, `retrieve_prefilter_live_test.go`) don't add one either. Adding a `TestMain` under this tag would newly subject those siblings to goleak with the live mcp-subprocess's reader goroutines (the memory tier needed a bespoke `reapIdleHTTPConns` drain to keep goleak green). To avoid destabilizing the siblings I mirrored their exact hygiene instead — envOrSkip + `defer mcp.Close()`. The test's own writes are cleaned up on entry and via defer.

## Deferred Verification (no-skip-as-green honesty)

This Windows host has no CGO/gcc and no live Neo4j stack, so:
- **`-race`** was NOT run (CGO disabled). Must run in WSL/CI before phase close.
- **`go test -tags neo4j_integration -run TestDocumentsFailClosed ./internal/documents/`** was NOT executed live — it skips locally (honest: `t.Fatal` under `$CI`). This is the plan's core acceptance proof (flag-ON fail-closed + flag-OFF reversibility) and MUST run green on the live stack in WSL/CI before phase close. Status honestly `unknown` here; compile-verified + vets under the tag.

## MUSR-01 status (phase-spanning — NOT marked complete)

Per the established Phase-36 discipline (36-01/02/03/04/07), MUSR-01 stays `[ ]`. This plan closes the **documents plane** of MUSR-01 (the spike-085 leak fix, mechanism + live test), but the requirement is phase-spanning: Garage keys = plan 06, provisioning saga = plan 08, audit UI = plan 10, and the rollout FLIP that activates enforcement end-to-end = plan 12. `requirements mark-complete` intentionally NOT run.

## Known Stubs

None — no placeholder/empty-return code introduced. The flag-OFF unscoped path is not a stub: it is the intentional, documented D-13 transitional/reversible state (threat register T-36-05-I4 disposition = accept).

## Self-Check: PASSED

- `internal/documents/fail_closed_integration_test.go` — FOUND
- `.planning/phases/36-multi-user-identity-isolation-authula-cutover/36-05-SUMMARY.md` — FOUND
- Commits `fe8f1827`, `cc9181ef`, `2e722536` — all FOUND in git history
