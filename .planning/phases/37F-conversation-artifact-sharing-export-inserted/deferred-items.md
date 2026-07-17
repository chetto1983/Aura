# Deferred Items — Phase 37F

Out-of-scope discoveries logged per the executor's SCOPE BOUNDARY rule (fix only what
the current task's changes directly caused; log everything else here instead of fixing it).

## 37F-04 — `internal/objectstore` package-wide coverage is below 85% (pre-existing, unrelated to this plan)

**Found during:** Task 2 (`ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix` GREEN commit),
while verifying the plan's own acceptance criterion `go test ./internal/objectstore/ -cover
-count=1 reports >= 85%`.

**What was found:** Plain `go test ./internal/objectstore/ -cover -count=1` reports **63.9%**
for the whole package — below the plan's literal acceptance-criteria threshold. Measured the
baseline at the pre-37F-04 commit (`34a87f589`, `git show 34a87f589:internal/objectstore/types.go`
swapped in temporarily, `share_key_test.go` removed, then both restored via `git checkout HEAD --`):
baseline was **63.6%**. This plan's 3 new functions are **100.0% covered** individually
(`go tool cover -func` confirms `ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix` all at
100.0%, matching `AssetKey`'s existing 100.0%) — the delta from 63.6% to 63.9% is a net
*improvement*, not a regression.

**Root cause (pre-existing, not introduced by 37F-04):** the package's remaining uncovered
functions are all live-infrastructure-gated:
- `internal/objectstore/s3.go`: `Put`/`Head`/`Get`/`List`/`Delete`/`ConfigureBrowserUploadCORS`
  (all 0.0%) — need a live Garage/S3 endpoint, tested under a different tag/tier.
- `internal/objectstore/identity_store.go`: `Put`/`Delete` (0.0%), `Resolve` (40.0%) — need a
  live Postgres connection, tested under `db_integration`.
- `internal/objectstore/filesystem.go`: `Delete` (0.0%).

A plain untagged `go test -cover` on this package will never clear 85% without either (a)
running the full `db_integration neo4j_integration` tag matrix per CLAUDE.md's coverage-gate
discipline, or (b) a dedicated task to add unit coverage for the S3/identity-store paths —
neither of which is this plan's scope (37F-04 is pure primitives, explicitly no DB/Garage per
its own `<objective>` and `project_toolchain` constraints).

**Disposition:** NOT fixed here — out of scope for 37F-04 (SCOPE BOUNDARY: only auto-fix
issues directly caused by the current task's changes). Logged for whichever future
phase/plan owns a package-wide `internal/objectstore` coverage push, or for the next
full-matrix (`db_integration neo4j_integration`) measurement to confirm whether the tagged
tiers already clear the floor (CLAUDE.md's owned-surface floor is measured across that full
matrix, not a plain untagged snapshot — this package may already clear 85% once db_integration-
tagged tests are included, which this plan did not re-run to confirm).

**Verification of this plan's own deliverable:** `internal/share` (this plan's other package)
measures 97.4% via plain `go test -cover`, comfortably above the floor. The 3 new
`internal/objectstore` functions are proven 100.0% individually via `go tool cover -func`.
