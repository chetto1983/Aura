---
phase: 39-idempotency-observability-pack
plan: 07
subsystem: learning-retention
tags: [active-learning, neo4j, compaction, reservoir, scheduler, observability]

# Dependency graph
requires:
  - phase: 39-idempotency-observability-pack
    plan: 04
    provides: "Bounded OTel metric catalog and learning instrument identities"
provides:
  - "Hash-only 100,000-entry/30-day concurrent active-learning dedup state"
  - "90-day, 512-per-bucket, 10,000-per-store learned-example write/load bounds"
  - "Deterministic newest-quarter plus quality/novelty weighted bounded compaction"
  - "Non-overlapping daily compaction with catalog-owned capacity telemetry"
affects: ["Phase 39 closeout", "OBS-06", "reasoning classifier", "tool selection"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Reserve/commit/release separates in-flight learning attempts from committed dedup state"
    - "Learned and manual seed labels have disjoint read, count, cap, and deletion surfaces"
    - "SHA-256-seeded integer priorities make weighted reservoir selection process-stable"
    - "Neo4j deletes match hash plus updated_at version and verify only the bounded hash page when writes return empty acknowledgments"

key-files:
  created:
    - internal/config/config_learning.go
    - internal/activelearn/bounded_seen.go
    - internal/neostore/learning.go
    - internal/learningretention/compactor.go
    - internal/learningretention/reservoir.go
    - internal/learningretention/neo4j_store.go
    - internal/learningretention/telemetry.go
    - internal/cron/handlers/learning_compaction.go
    - internal/db/migrations/0045_scheduler_learning_compaction_kind.up.sql
  modified:
    - internal/activelearn/learner.go
    - internal/reasoningstore/store.go
    - internal/toolselectstore/store.go
    - internal/reasoninglearn/learner.go
    - internal/toolselectlearn/learner.go
    - internal/runner/runner.go
    - cmd/aura/chat_boot.go
    - cmd/aura/serve_dispatch.go
    - cmd/aura/serve.go

key-decisions:
  - "Dedup identity remains neostore.HashText over exact UTF-8 bytes; no raw observation or Unicode normalization enters SeenSet."
  - "TTL equality is retained: a committed hash is duplicate at exactly 30 days and becomes admissible only after the boundary."
  - "Manual ReasoningSeed and ToolSelectionSeed nodes remain cap-exempt and non-expiring through separate labels and source predicates."
  - "Compaction reserves ceil(capacity*25/100) newest hashes, then ranks the remainder by SHA-256 seed divided by fixed-point quality/novelty weight."
  - "Learning compaction uses one system-seeded scheduler kind, process-local non-overlap, bounded pages, and timestamp-versioned idempotent reruns."

patterns-established:
  - "Capacity admission is serialized per live store and performed in one conditional Cypher write guarded by an AuraLearningCapacity node."
  - "Compaction never reads embeddings or content; candidates contain only hash, finite bucket, timestamps, policy version, and clamped scores."

requirements-completed: [OBS-06]

coverage:
  - id: D17-memory
    description: "Hash-only SeenSet enforces exact 100k/30d concurrent bounds with retry release."
    requirement: OBS-06
    verification:
      - kind: unit
        ref: "activelearn/config cap, TTL, UTF-8, concurrency, queue, close, and retry tests under Linux -race"
        status: pass
    human_judgment: false
  - id: D17-stores
    description: "Reasoning and tool-selection learned stores enforce TTL, per-bucket/global caps, bounded deterministic loads, and pinned isolation."
    requirement: OBS-06
    verification:
      - kind: unit
        ref: "reasoningstore/toolselectstore exact Cypher and typed capacity tests"
        status: pass
    human_judgment: false
  - id: D17-compaction
    description: "Bounded deterministic compaction preserves the newest quarter, weighted remainder, global cap, pinned seeds, cancellation, and rerun convergence."
    requirement: OBS-06
    verification:
      - kind: integration
        ref: "10,003-node live Neo4j compaction against isolated labels under Linux -race"
        status: pass
      - kind: unit
        ref: "reservoir golden/order, bounded fake-store, partial failure, metric, and handler non-overlap tests"
        status: pass
    human_judgment: false

duration: 45min
completed: 2026-07-21
status: complete
---

# Phase 39 Plan 07: Bounded Learning Stores and Deterministic Compaction Summary

**Aura's active-learning state is now hard-bounded at every layer: exact-byte hash dedup in memory, transactional TTL/cap admission in Neo4j, and deterministic content-free background compaction that preserves recent, high-quality, novel, and pinned examples.**

## Performance

- **Duration:** 45 minutes
- **Started:** 2026-07-21T20:05:59Z
- **Completed:** 2026-07-21T20:51:26Z
- **Tasks:** 3 TDD tasks
- **Implementation files:** 38

## Accomplishments

- Replaced the unbounded learner map with a mutex-protected hash-only `SeenSet`, exact 100,000 hard cap, 30-day TTL, deterministic committed-entry eviction, and reserve/commit/release retry semantics.
- Added typed learning configuration and propagated it through both reasoning and tool-selection wrappers; the hot observation path remains non-blocking and shutdown remains joined.
- Added shared bounded Neo4j query builders with 90-day learned TTL, server-side per-bucket/global limits, conditional write admission, original creation-time preservation, clamped scores, and typed capacity outcomes.
- Isolated manual/pinned seeds under separate labels and APIs so they never consume learned capacity, expire, or enter compaction queries.
- Added a store-agnostic deterministic compactor: newest 25% reservation, SHA-256/fixed-point weighted remainder, bounded expiry/bucket/global passes, exact timestamp-version deletion, cancellation, and partial-failure convergence.
- Added catalog-owned OTel learning operation/size/oldest-age metrics with finite `data`/`other` classes and no raw text, query, hash, bucket, or tool-name labels.
- Added migration 0045, a silent-success `learning_compaction` scheduler kind, daily 03:15 Europe/Rome seeding, production graph-store wiring, and atomic non-overlap in the handler.
- Proved the live path with 10,003 isolated Neo4j learned nodes: one expired item and two global-pressure evictions converge to exactly 10,000, retain the newest reservation, preserve the pinned seed, and produce an empty second pass.

## Task Commits

1. **Task 1: Hash-only bounded learner dedup**
   - RED - `e4ebcf297` (test): cap, TTL, exact UTF-8, retry, close, and queue contract.
   - GREEN - `a37157a2e` (feat): concurrent SeenSet and bounded learner signals.
2. **Task 2: Bounded Neo4j learned stores**
   - RED - `036c1b4d1` (test): learned/pinned, load, TTL, hash, and capacity query contract.
   - GREEN - `f8714ecae` (feat): transactional admission, bounded loads, typed capacity, and production config.
3. **Task 3: Deterministic scheduled compaction**
   - RED - `848af743a` (test): reservoir, bounded batch, cancellation, rerun, global pressure, and non-overlap contract.
   - GREEN - `aae57e426` (feat): compactor, graph adapter, telemetry, scheduler, migration, and runtime wiring.

Corrective verification commits:

- `e26de4e1e` (test): live 10,003-node Neo4j compaction proof.
- `beebc99f0` (fix): CRLF-safe disposable coverage credential loading.
- `f16415d01` (fix): bounded post-delete verification for empty Neo4j write acknowledgments.

## Decisions Made

- `SeenSet` stores only lowercase SHA-256 strings and timestamps/state. Empty and whitespace-only observations remain filtered before hashing; byte-distinct Unicode remains byte-distinct.
- At capacity, only the deterministic oldest committed entry may be evicted; reserved in-flight work is never displaced.
- Same-hash learned writes remain updates at capacity, retain `created_at`, advance `updated_at`, and cannot create a second node.
- Store load and compaction limits live in Cypher. Go additionally validates/caps returned pages but never accepts an unbounded result and slices it afterward.
- Weighted selection clamps quality/novelty to fixed-point 0..1000 and uses weight `1 + 3*quality_milli + 2*novelty_milli`; lower unsigned SHA-256 priority divided by weight wins, with digest/hash tie-breaks.
- Deletion uses `source='learned'`, exact hash, and exact `updated_at` version. When the MCP write transport omits returned rows, one bounded hash-only read distinguishes applied deletes from concurrently updated survivors.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added scheduler kind migration 0045**
- **Found during:** Task 3 production registration.
- **Issue:** The existing PostgreSQL `scheduler_tasks.kind` constraint could not admit the newly seeded compaction task.
- **Fix:** Added reversible migration 0045 and cleanup-before-down behavior.
- **Files modified:** `internal/db/migrations/0045_scheduler_learning_compaction_kind.*.sql`.
- **Commit:** `aae57e426`.

**2. [Rule 3 - Blocking] Normalized CRLF secrets in the disposable coverage runner**
- **Found during:** Full tagged coverage verification under WSL.
- **Issue:** Windows `.env` carriage returns entered composed PostgreSQL URLs and stopped migration before tests.
- **Fix:** Strip only `\r` at `read_secret`, preserving every other credential byte.
- **Files modified:** `scripts/coverage_docker.sh`.
- **Commit:** `beebc99f0`.

**3. [Rule 1 - Bug] Verified applied deletes when Neo4j write acknowledgments contain no records**
- **Found during:** Live 10,003-node compaction verification.
- **Issue:** The MCP write tool applied deletes but returned no Cypher rows, so the report read zero deletions despite convergence.
- **Fix:** If no positive returned count exists, read back only the bounded requested hash page; concurrently updated hashes remain counted as survivors.
- **Files modified:** `internal/learningretention/neo4j_store.go`, `internal/learningretention/compactor_integration_test.go`.
- **Commit:** `f16415d01`.

---

**Total deviations:** 3 auto-fixed (1 missing critical, 1 verification blocker, 1 live transport bug).
**Impact on plan:** All fixes were required to deploy or prove the requested scheduled compaction; none broadened learning data access or deletion scope.

## Issues Encountered

- Native Windows cannot run this repository's Go race tier; every phase-specific race command and the full owned-package unit race matrix ran in WSL.
- The repository-wide disposable `db_integration neo4j_integration` coverage matrix migrated PostgreSQL to 45 and Neo4j successfully, then unrelated legacy DB tests failed because `EnsureRoles` attempted the live `aura` database with a mismatched `aura` role password. The Phase 39-07 live Neo4j tier was therefore run separately with dedicated temporary labels and passed; the disposable coverage resources cleaned up through the script trap.

## Threat Surface Scan

- No new HTTP, authentication, filesystem, or user-visible network surface was added.
- In-memory dedup retains no content and metrics retain no text/query/hash/raw bucket/tool name.
- Graph labels/property names are fixed composition constants, not user-controlled Cypher fragments.
- Learned deletion is source- and timestamp-version-scoped; manual seed labels are structurally unreachable from count/candidate/delete queries.
- The live integration test uses dedicated labels, deletes only those labels, and cleans them before and after the run.

## Known Stubs

None. The static-catalog instrument constructors intentionally panic on an impossible missing/invalid descriptor, matching existing observability ownership; no production TODO, FIXME, placeholder, empty datasource, or not-implemented path was introduced.

## Verification

- Phase-specific WSL Linux `-race` for activelearn/config and learningretention/cron handlers/cmd - pass.
- Reasoning/tool store load/save/cap/TTL/pinned/hash/query contract tests - pass.
- Full owned-package unit matrix from `scripts/go_packages.sh` under Linux `-race -count=1` - pass.
- `go test` across all touched packages, repository-wide `go build ./...`, and focused `go vet` - pass.
- Live `neo4j_integration` compaction with 10,003 learned nodes under Linux `-race -count=1` - pass in 6.9s.
- Migration 0045 applied as part of the disposable coverage database migration (45 migrations total) - pass.
- Commit hooks, gofmt, lint, vet, file-size cap, and `git diff --check` - pass.
- Full tagged owned coverage gate - attempted but blocked by unrelated legacy `EnsureRoles` live-`aura` password mismatch after successful disposable migrations; not reported as green.

## User Setup Required

None. The application applies migration 0045 through the existing migration flow and seeds one daily learning compaction task. Learning remains gated by the existing reasoning-learning switch; when Neo4j learning is unavailable, no graph stores are registered and the scheduled pass is a bounded no-op.

## Next Phase Readiness

- OBS-06 and all seven Phase 39 plans are implemented.
- Phase 39 is ready for milestone/phase audit; the only non-green repository-wide integration item is the pre-existing disposable coverage `EnsureRoles` credential mismatch documented above.
- No implementation blocker remains for Phase 40.

## Self-Check: PASSED

---
*Phase: 39-idempotency-observability-pack*
*Completed: 2026-07-21*
