---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 15
subsystem: infra
tags: [objectstore, garage, s3, identity, multi-user, pgx, isolation]

# Dependency graph
requires:
  - phase: 36-06
    provides: objectstore.IdentityStore.Resolve(ctx) per-identity credential resolver + Garage Admin API v2 client
  - phase: 36-14
    provides: daemon provisioning saga that CREATES the per-identity Garage bucket + persists creds (so a resolved bucket exists at request time)
provides:
  - Per-identity object-resolution seam (ObjectResolver + StoreFactory + resolveObjects) consumed by the asset Service + its audio/document/image processors
  - Composition-root wiring of the IdentityStore resolver + a caching per-identity S3Store factory in buildAssetService
  - A live garage_integration && db_integration asset cross-deny test (A's bytes isolated from B; unprovisioned C -> shared fallback)
affects: [36-18, phase-close, MUSR-01]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-identity object-store consumption: resolve creds via IdentityStore.Resolve(ctx) at request time; explicit local/ErrNoRows->shared fallback (never a foreign bucket, F-007)"
    - "Caching StoreFactory (sync.Map keyed on access key) builds one S3Store per identity over the shared endpoint/region/path-style"
    - "Owner identity stamped into ctx from the asset record (req.IdentityID / asset.IdentityID) so the background durable-worker path resolves deterministically"

key-files:
  created:
    - internal/assets/object_resolver.go
    - internal/assets/object_resolver_unit_test.go
    - internal/assets/object_resolver_test.go
  modified:
    - internal/assets/service.go
    - internal/assets/audio_processor.go
    - internal/assets/document_processor.go
    - internal/assets/image_processor.go
    - cmd/aura/document_processor_wiring.go

key-decisions:
  - "Processor resolution passes the REAL shared bucket (cfg.ObjectStoreBucket) to resolveObjects, NOT asset.ObjectBucket — the plan's literal pseudocode would wrongly short-circuit a per-identity owner to the shared global-key store (Rule 1 fix realizing the plan's stated intent)"
  - "Service methods stamp the asset owner into ctx before resolving — the durable-worker path carries no identityctx principal, so objectsFor(ctx) would otherwise fall back to shared"
  - "MUSR-01 stays OPEN (phase-spanning): the live full-matrix two-identity E2E + push close at 36-18, matching the 36-05/06/08/10/12/14 precedent"

patterns-established:
  - "resolveObjects fallback discipline: nil-seam/ErrNoRows/local -> shared; per-identity -> factory(creds); real error propagates"
  - "assets package integration split: object_resolver_unit_test.go (untagged) + object_resolver_test.go (garage_integration && db_integration), mirroring store_unit_test.go/store_test.go"

requirements-completed: []

coverage:
  - id: D1
    description: "resolveObjects fallback discipline — nil-seam/ErrNoRows/local principal -> shared store+bucket; per-identity -> factory(creds); real errors propagate; never a foreign bucket (F-007)"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/assets/object_resolver_unit_test.go#TestResolveObjectsFallbackDiscipline"
        status: pass
    human_judgment: false
  - id: D2
    description: "Asset Service routes Presign/Finalize/IngestTelegramFile/Delete/hashAndSniff through the per-identity resolver — a resolver returning A's creds lands A's bytes in A's bucket, NOT the shared store; nil resolver keeps the shared bucket"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/assets/object_resolver_unit_test.go#TestServiceIngestRoutesBytesToPerIdentityBucket"
        status: pass
      - kind: unit
        ref: "internal/assets/object_resolver_unit_test.go#TestServicePresignNilResolverKeepsSharedBucket"
        status: pass
    human_judgment: false
  - id: D3
    description: "The audio/document/image processors read an asset with the OWNER's per-identity creds (storeForAsset resolves asset.IdentityID); nil bundle -> shared; unprovisioned -> shared"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/assets/object_resolver_unit_test.go#TestObjectResolverBundleStoreForAsset"
        status: pass
    human_judgment: false
  - id: D4
    description: "Composition root builds the IdentityStore resolver + caching per-identity S3Store factory and wires Service.IdentityObjects/PerIdentityStore + the 3 processors; objectstore.NewIdentityStore now has a non-test asset-path consumer"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "go test ./cmd/aura/ (assets_test.go buildAssetService nil-pool shared path) — pass"
        status: pass
      - kind: integration
        ref: "internal/assets/object_resolver_test.go#TestAssetObjectStoreCrossDenyLive (garage_integration && db_integration)"
        status: unknown
    human_judgment: false
  - id: D5
    description: "Live cross-deny: A's asset bytes land in A's Garage bucket, readable ONLY with A's resolved creds, DENIED to B's scoped creds; an unprovisioned identity C falls back to the shared bucket (no crash, no foreign bucket)"
    requirement: "MUSR-01"
    verification:
      - kind: integration
        ref: "internal/assets/object_resolver_test.go#TestAssetObjectStoreCrossDenyLive"
        status: unknown
    human_judgment: false
    rationale: "CI-gated: the Garage Admin API :3903 is unpublished on this host (curl exit 7); compiles clean under both tags, runs at 36-18 in the musr-e2e CI job."

# Metrics
duration: 35min
completed: 2026-07-06
status: complete
---

# Phase 36 Plan 15: Per-identity object-store consumption on the asset path Summary

**Routed the asset Service + audio/document/image processors through `objectstore.IdentityStore.Resolve(ctx)` so each identity's asset bytes land in ITS OWN Garage bucket under ITS OWN key — closing VERIF-4/HI-01 where the resolver shipped in 36-06 had ZERO non-test consumers.**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-06T10:30:00Z
- **Completed:** 2026-07-06T11:05:00Z
- **Tasks:** 2
- **Files modified:** 8 (3 created, 5 modified)

## Accomplishments
- New `internal/assets/object_resolver.go`: `ObjectResolver` + `StoreFactory` seams + `resolveObjects` helper with the explicit local/`ErrNoRows`→shared fallback (never a foreign bucket — F-007), + `ObjectResolverBundle` for the processors.
- `assets.Service` now carries `IdentityObjects` + `PerIdentityStore` and routes Presign/Finalize/IngestTelegramFile/Delete/hashAndSniff through `objectsFor(ctx)`; the three processors read an asset with the OWNER's per-identity creds.
- Composition root (`buildAssetService`) builds the `IdentityStore` resolver + a caching per-identity `S3Store` factory (sync.Map keyed on access key) when the pool + `AURA_AUTHULA_SECRET` are present, and threads them into the Service + the 3 processors — `objectstore.NewIdentityStore` now has a NON-test production consumer on the asset request path.
- A live `garage_integration && db_integration` cross-deny test (self-contained disposable migrated pool + live Garage) proving A↔B storage-enforced isolation + the unprovisioned-C shared fallback.

## Task Commits

Each task was committed atomically:

1. **Task 1: Per-identity object-resolution seam + route the asset object ops through it** — `406a1e75` (feat)
2. **Task 2: Wire the resolver + caching per-identity S3Store factory at the composition root + cross-deny test** — `248e5676` (feat)

**Plan metadata:** _(this docs commit)_

## Files Created/Modified
- `internal/assets/object_resolver.go` (created) — `ObjectResolver`/`StoreFactory`/`resolveObjects` + `ObjectResolverBundle.storeForAsset`.
- `internal/assets/object_resolver_unit_test.go` (created, untagged) — fake resolver + recording factory prove the fallback discipline + the per-identity write routing.
- `internal/assets/object_resolver_test.go` (created, `garage_integration && db_integration`) — live A/B cross-deny + unprovisioned-C fallback.
- `internal/assets/service.go` (modified) — `IdentityObjects`/`PerIdentityStore` fields + `objectsFor(ctx)`; routed Presign/Finalize/IngestTelegramFile/Delete; `hashAndSniff` takes the resolved store.
- `internal/assets/audio_processor.go`, `document_processor.go`, `image_processor.go` (modified) — optional `PerIdentityObjects *ObjectResolverBundle`; object Get on the owner's resolved store.
- `cmd/aura/document_processor_wiring.go` (modified) — `buildObjectResolverBundle` + `newCachingPerIdentityStoreFactory`; wires the resolver+factory into the Service + processors.

## Decisions Made
- **Processor resolution uses the REAL shared bucket, not `asset.ObjectBucket`** (see Deviation 1). This realizes the plan's stated intent ("the processor only needs the OWNER's creds to read it").
- **Service methods stamp the owner identity into ctx** before resolving (see Deviation 2) — required so the background durable-worker path (`ProcessAccepted`→processor) resolves per-identity instead of falling back to shared.
- **MUSR-01 stays `[ ]`** — phase-spanning; the live full-matrix two-identity E2E + push close at 36-18 (matches the 36-05/06/08/10/12/14 precedent). `requirements mark-complete` intentionally NOT run.
- **No new dependencies, migrations, or query files** — `go.mod`/`go.sum` byte-unchanged; `sqlc generate` trivially zero-diff (no queries touched).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Processor resolution passed a per-identity bucket as the shared-bucket sentinel**
- **Found during:** Task 1 (processor threading)
- **Issue:** The plan's literal pseudocode `resolveObjects(ictx, resolver, factory, p.Objects, asset.ObjectBucket)` passes `asset.ObjectBucket` as `sharedBucket`. `resolveObjects` short-circuits to the SHARED store when `creds.Bucket == sharedBucket`; for a per-identity owner `creds.Bucket == asset.ObjectBucket`, so the processor would read the owner's bucket with the SHARED global key — which per F-007 (per-bucket grants) cannot read a per-identity bucket → a 403 on every per-identity asset. This contradicts the plan's own prose ("the processor only needs the OWNER's creds to read it").
- **Fix:** Added `ObjectResolverBundle.SharedBucket` (set to `cfg.ObjectStoreBucket`) and pass the REAL shared bucket to `resolveObjects`, so a per-identity owner resolves to the owner's store while `asset.ObjectBucket` is still read on it.
- **Files modified:** `internal/assets/object_resolver.go`, the 3 processors, `cmd/aura/document_processor_wiring.go`
- **Verification:** `TestObjectResolverBundleStoreForAsset` (per-identity → owner store, NOT shared); `-race` green.
- **Committed in:** `406a1e75` / `248e5676`

**2. [Rule 2 - Missing Critical] Stamp the owner identity into ctx on the service object path**
- **Found during:** Task 1 (service routing)
- **Issue:** The asset durable-worker path (`ProcessAccepted`→`processAsset`→processor) runs on a background context that carries NO `identityctx` principal; the Telegram ingest path is likewise not guaranteed to. A literal `objectsFor(ctx)` there would resolve to the shared bucket, silently defeating per-identity isolation on the write/finalize path.
- **Fix:** Each service method (`Presign`/`Finalize`/`IngestTelegramFile`/`Delete`) calls `objectsFor(identityctx.WithIdentityID(ctx, ownerID))` where `ownerID` is the authoritative record owner (`req.IdentityID`/`identityID`). The web path already stamps identityctx (auth.go:309) so this is belt-and-suspenders there and load-bearing for the background path; the object bucket now deterministically matches the record owner.
- **Files modified:** `internal/assets/service.go`
- **Verification:** `TestServiceIngestRoutesBytesToPerIdentityBucket` (bytes land in the owner store, NOT shared).
- **Committed in:** `406a1e75`

**3. [Rule 2 - Missing Test] Added an untagged unit test file (Task 1 listed none)**
- **Found during:** Task 1
- **Issue:** Task 1's acceptance calls for a unit assertion "via a fake ObjectResolver + a recording StoreFactory", but Task 1's `files` list named no test file (Task 2 owns the live-tagged `object_resolver_test.go`). A live-tagged file cannot hold untagged unit tests.
- **Fix:** Added `internal/assets/object_resolver_unit_test.go` (untagged), mirroring the package's existing `store_unit_test.go`/`store_test.go` split.
- **Files modified:** `internal/assets/object_resolver_unit_test.go`
- **Verification:** untagged `go test ./internal/assets/` + `-race` green.
- **Committed in:** `406a1e75`

---

**Total deviations:** 3 auto-fixed (1 bug, 2 missing-critical/test)
**Impact on plan:** Deviation 1 is a genuine correctness fix (the literal code would 403 every per-identity asset read); Deviations 2–3 are correctness/coverage completions the plan's prose and acceptance already imply. No scope creep — no files touched beyond the plan's `files_modified` plus the one untagged unit-test file.

## Issues Encountered
None during planned work.

## Verification Results (real)
- `CGO_ENABLED=0 go build ./...` → exit 0; `go vet ./...` → exit 0.
- `go build -tags 'garage_integration db_integration' ./internal/assets/` → exit 0 (compiles); test binary compiles under both tags → exit 0.
- Untagged `go test ./internal/assets/ ./cmd/aura/` → **green** (shared-path behavior preserved; assets_test.go nil-pool call unchanged).
- **WSL `-race ./internal/assets/ ./cmd/aura/` → exit 0** (CGO on).
- Garage admin `:3903` reachability probe → `curl` exit 7 (unreachable) → the `garage_integration` cross-deny tier is **CI-gated at 36-18** (expected, not a failure).
- Grep: `objectstore.NewIdentityStore` non-test consumer at `cmd/aura/document_processor_wiring.go:76`; `IdentityStore.Resolve` consumed via `Service.objectsFor` + `ObjectResolverBundle.storeForAsset` on the asset path.

## Next Phase Readiness
- The asset read/write path now enforces D-08 bucket-per-identity isolation at request time; the mechanism is unit- + compile-proven.
- **36-18 must run** the live `garage_integration db_integration` cross-deny test (musr-e2e CI job / WSL with :3903 published) + the full-matrix coverage ≥85% + push, and then close MUSR-01.

## Self-Check: PASSED

- Files verified present: `internal/assets/object_resolver.go`, `object_resolver_unit_test.go`, `object_resolver_test.go`, `36-15-SUMMARY.md`.
- Commits verified present: `406a1e75` (Task 1), `248e5676` (Task 2), `8f561f5a` (docs).

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-06*
