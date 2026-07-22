---
phase: 39-idempotency-observability-pack
plan: 06
subsystem: retention
tags: [retention, postgresql, idempotency, filesystem, export, scheduler, cli, observability]

# Dependency graph
requires:
  - phase: 39-idempotency-observability-pack
    plan: 01
    provides: "Durable operation identity and PostgreSQL migration head 0043"
  - phase: 39-idempotency-observability-pack
    plan: 02
    provides: "Trusted HTTP/CLI mutation metadata and operation-key contracts"
  - phase: 39-idempotency-observability-pack
    plan: 04
    provides: "Bounded retention metrics and OTel boundary catalog"
provides:
  - "Versioned class policy with locked TTL/disk defaults and durable active-work evidence"
  - "Deterministic dry-run tokens plus bounded SKIP LOCKED mark-remove-finalize operations"
  - "Shared retention CLI/scheduler execution over hardened local artifact roots"
  - "Owner-scoped verified ZIP export and export-delete through the canonical teardown lifecycle"
affects: ["39-07 learning retention", "39 phase closeout", "operations", "storage lifecycle"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "A dry-run token hashes the policy version and sorted trusted candidate identity/version/action snapshot"
    - "External removal is durably recorded before metadata finalization and is never repeated after removed/absent state"
    - "Automatic cleanup reconstructs paths from fixed adapter roots and trusted IDs, then revalidates ownership/version/activity"
    - "Owner export is atomically published and independently reopened, rehashed, and manifest-verified before teardown"

key-files:
  created:
    - internal/db/migrations/0044_retention_operations.up.sql
    - internal/db/queries/retention_operations.sql
    - internal/config/config_retention.go
    - internal/retention/engine.go
    - internal/retention/local.go
    - cmd/aura/retention.go
    - internal/cron/handlers/retention.go
    - internal/agui/owner_export.go
    - internal/agui/retention_api.go
  modified:
    - cmd/aura/serve.go
    - cmd/aura/idempotency.go
    - internal/agui/conversations_api.go
    - internal/agui/idempotency_http.go
    - internal/cron/store.go

key-decisions:
  - "Conversations remain unlimited and referenced artifacts follow conversation lifetime; the automatic engine currently selects only expired Aura-owned temporary, crash, full-trace, and metadata-trace roots."
  - "Disk pressure is signal-only at exact 70/80/85 percent boundaries and never expands the cleanup candidate set."
  - "Retention operations use planned/deleting/retryable/completed/failed state, lease-owned conditional transitions, and disjoint SKIP LOCKED claims."
  - "Tempo remains the exclusive owner of Tempo blocks; the Aura local adapter has no Tempo root and never enumerates one."
  - "Export-delete calls DeleteConversationLifecycle only after atomic publish and full archive/manifest re-verification; ordinary DELETE remains unchanged and creates no archive."

patterns-established:
  - "Scheduler and CLI invoke the same Engine.Plan/Engine.Apply methods; plan is the CLI default and apply requires the exact fresh token."
  - "Content-free retention audits contain policy version, aggregate counts/bytes, and finite failure classes, never paths, IDs, or payloads."

requirements-completed: [OBS-05]

coverage:
  - id: D14
    description: "One durable, bounded, non-overlapping two-phase engine serves scheduled sweeps and the dry-run-by-default operator CLI."
    requirement: OBS-05
    verification:
      - kind: integration
        ref: "internal/retention tagged PostgreSQL lifecycle and parallel SKIP LOCKED claim tests under Linux -race"
        status: pass
      - kind: unit
        ref: "engine/CLI/scheduler dry-run, token, crash, retry, cancellation, cap, and audit tests"
        status: pass
    human_judgment: false
  - id: D15-policy
    description: "Locked 24h/24h-or-7d/14d/follow-conversation/unlimited lifetimes and exact 70/80/85 disk signals are typed and validated."
    requirement: OBS-05
    verification:
      - kind: unit
        ref: "internal/config/config_retention_test.go and internal/retention/policy_test.go exact-boundary tables"
        status: pass
    human_judgment: false
  - id: D15-storage
    description: "Automatic cleanup accepts only adapter-owned trusted IDs, excludes Tempo, refuses symlink replacement, and never performs pressure-driven emergency deletion."
    requirement: OBS-05
    verification:
      - kind: unit
        ref: "internal/retention/local_test.go real-filesystem traversal, ownership, absence, root, freshness, and Linux symlink cases"
        status: pass
    human_judgment: false
  - id: D16-activity
    description: "Live turns, fresh workers, pauses, approvals, scheduler work, background tools, sandboxes, and artifact operations protect candidates at the documented freshness boundary."
    requirement: OBS-05
    verification:
      - kind: unit
        ref: "internal/retention/activity_test.go plus immediate pre-remove revalidation tests"
        status: pass
    human_judgment: false
  - id: D16-export
    description: "Owner export has a versioned deterministic manifest, bounded content, sizes/checksums, owner isolation, verified publish, canonical export-delete, and no plain-delete backup."
    requirement: OBS-05
    verification:
      - kind: unit
        ref: "internal/agui owner exporter and two-identity HTTP tests, including missing/replaced data and traversal failures"
        status: pass
      - kind: unit
        ref: "internal/runner canonical conversation deletion lifecycle regression suite under Linux -race"
        status: pass
    human_judgment: false

duration: 56min
completed: 2026-07-21
status: complete
---

# Phase 39 Plan 06: Idempotent Retention and Owner Export Summary

**Aura now has one versioned, auditable retention contract: deterministic plans become lease-owned two-phase cleanup, automatic work skips protected activity, and owner export-delete cannot begin teardown until a bounded archive is atomically published and independently verified.**

## Performance

- **Duration:** 56 minutes
- **Started:** 2026-07-21T19:00:07Z
- **Completed:** 2026-07-21T19:56:03Z
- **Tasks:** 3 TDD tasks
- **Implementation files:** 42

## Accomplishments

- Added migration 0044 with immutable retention operations/items, deterministic candidate keys, lease ownership, artifact results, counts/bytes/failure classes, and bounded `FOR UPDATE SKIP LOCKED` claims.
- Added typed class policy/config for 24-hour temporary/crash retention, environment-aware full-trace retention, 14-day metadata traces, conversation-following referenced artifacts, unlimited conversations, and exact 70/80/85 disk signals.
- Added durable activity evidence for every locked protection class and immediate ownership/version/activity revalidation before any external removal.
- Added crash-resumable mark-remove-finalize execution: removed/absent results survive retries, metadata comes last, retryable failures stop same-run hot loops, and aggregate audits/OTel metrics remain content-free.
- Added `aura retention` dry-run-by-default planning, explicit `apply --token`, daily scheduler seeding, bounded handler execution, and the same engine at both entry points.
- Added hardened local cleanup roots for temporary/crash/full-trace/metadata-trace artifacts with trusted basenames, `Lstat`, symlink refusal, fixed roots, and no Tempo enumeration.
- Added owner-only ZIP export with versioned policy/Aura/schema metadata, deterministic entry ordering, UTF-8 filenames, per-entry sizes/SHA-256, bounded streaming, atomic destination publish, and independent reopen verification.
- Added owner-only `GET .../owner-export` and idempotency-inventoried `POST .../export-delete`; export-delete reaches the existing ordered lifecycle only after verification, while plain delete remains backup-free.

## Task Commits

Each task kept explicit RED/GREEN history:

1. **Task 1: Policy, activity evidence, token, and durable state**
   - RED - `e1bf96544` (test): locked config and migration/SQL contracts.
   - GREEN - `fbe3f510b` (feat): policy/config, activity evidence, deterministic plans, migration 0044, sqlc store, and parallel claims.
2. **Task 2: Two-phase engine, CLI, and scheduler**
   - RED - `ee3898dd7` (test): dry-run, crash recovery, revalidation, absence, cancellation, and disk behavior.
   - GREEN - `93532fbaa` (feat): bounded engine, local adapter, audit/metrics, CLI, scheduler handler, seed, and production wiring.
3. **Task 3: Verified owner export and export-delete**
   - RED - `6d45f9218` (test): manifest integrity, failure atomicity, owner isolation, and teardown ordering.
   - GREEN - `d7e115fc3` (feat): bounded exporter/destination, API routes, owner snapshot adapter, verification, and canonical lifecycle call.

Corrective verification commit:

- `09d1205de` (fix): exercised every PostgreSQL lifecycle transition, fixed explicit timestamp typing in aggregate finalization, and raised tagged retention coverage to 88.1%.

## Files Created/Modified

- `internal/config/config_retention.go` and config wiring - profile-aware defaults and validated operational knobs.
- `internal/db/migrations/0044_retention_operations.*.sql` - durable operation/item state plus the `retention_sweep` scheduler kind.
- `internal/db/queries/retention_operations.sql` and generated sqlc - plan creation, bounded claims, external result recording, lease-owned transitions, and aggregate finalization.
- `internal/retention/{policy,plan,activity,store,engine,audit,local}.go` - the complete policy and execution boundary.
- `cmd/aura/retention.go` and daemon wiring - shared operator/scheduled runtime composition.
- `internal/cron/handlers/retention.go` - bounded silent-success scheduled owner.
- `internal/agui/owner_export.go` - manifest/archive production, atomic destination, and independent verification.
- `internal/agui/retention_api.go` - owner-scoped export and export-delete HTTP adapters.
- Focused unit/integration tests across config, DB contracts, retention, cron, CLI, AG-UI, runner, and share surfaces.

## Decisions Made

- Candidate plans contain trusted identity/conversation/artifact IDs and expected versions/actions, never filesystem paths or object-store URLs.
- Plan creation is audit persistence, not a storage mutation: it may write the immutable operation snapshot but cannot call any remover/finalizer.
- Automatic retries preserve removed/absent external results and repeat only idempotent metadata finalization; persistent retryable failures are left for a later invocation.
- Ownership mismatch is terminal failed; transient revalidation, version/activity drift, external outage, and metadata outage remain classified retryable outcomes.
- The local adapter owns only `$AURA_RUN_DIR/tmp`, `crash`, `traces/full`, and `traces/metadata`. Tempo-owned blocks are structurally unreachable.
- Canonical Garage-backed assets are not automatically selected while conversations remain unlimited; they are read owner-scoped into explicit exports and remain governed by conversation lifetime.
- The export snapshot holds the runner's per-thread try-lock when available, releases it after bundle construction, and always performs the owner gate before history or asset reads.
- Archive verification rejects unlisted/missing entries, size drift, checksum drift, unsafe names, and decompression beyond each manifest-declared size.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added the scheduler kind to the live migration**
- **Found during:** Task 2 production cron wiring.
- **Issue:** The 0044 retention tables existed, but the older scheduler kind constraint could not accept the newly seeded `retention_sweep` task.
- **Fix:** Widened the constraint in 0044 up and made down delete retention jobs/tasks before restoring the prior vocabulary.
- **Verification:** Disposable migration apply/down/up plus daily seed/dispatch tests pass.
- **Committed in:** `93532fbaa`.

**2. [Rule 1 - Bug] Corrected PostgreSQL timestamp inference in operation finalization**
- **Found during:** Fresh tagged lifecycle coverage after implementation commits.
- **Issue:** PostgreSQL inferred the `CASE ... THEN $1 ELSE NULL` parameter as text, so completed aggregate finalization failed despite generated Go type metadata.
- **Fix:** Cast the parameter explicitly to `timestamptz` and added a real lifecycle test covering removed, absent, retry, lease loss, terminal failure, and aggregate counts.
- **Verification:** Tagged retention coverage passes at 88.1%, including finalization, under Windows and Linux `-race` against disposable databases.
- **Committed in:** `09d1205de`.

**3. [Rule 3 - Blocking] Normalized Windows CRLF secrets for the WSL coverage invocation**
- **Found during:** Repository-wide tagged coverage gate.
- **Issue:** The Bash gate read a trailing carriage return from `.env`, making its disposable PostgreSQL DSN unparsable before tests started.
- **Fix:** Re-ran the unchanged gate with trimmed values exported from PowerShell and unique disposable database/Neo4j targets.
- **Verification:** Full owned tagged coverage passes at 85.3%; both disposable resources were verified absent afterward.
- **Committed in:** No source change required.

---

**Total deviations:** 3 auto-fixed (1 missing-critical, 1 implementation bug, 1 local verification blocker).
**Impact on plan:** The fixes complete scheduler deployability, prove actual PostgreSQL finalization, and preserve the required full-matrix gate without widening cleanup authority.

## Issues Encountered

- Native Windows cannot run Go's race detector in this setup. The focused and tagged retention surfaces ran under WSL's Linux Go toolchain with `-race`.
- Every database test refused `aura`/`postgres`, created a uniquely named disposable database, migrated it to 44, and dropped/verified it absent. The full matrix likewise used unique disposable PostgreSQL and Neo4j instances.
- Plan dry-run persists an immutable operation/audit snapshot. Tests therefore define "no mutation" as no deletion/removal/finalization effect, not zero bookkeeping writes.

## Threat Surface Scan

- Candidate and export identifiers reject slash, backslash, control, traversal, over-limit, and invalid UTF-8 forms; paths are reconstructed only below fixed adapter roots.
- `Lstat` runs both during discovery/revalidation and immediately before deletion. Symlink candidates/replacements are refused; Tempo is never enumerated.
- Only exact fresh plan tokens can apply. Ownership, version, and every durable activity class are revalidated immediately before removal.
- External removed/absent state is durable before metadata finalization. Lease-owner predicates reject stale workers, and disjoint claims use `SKIP LOCKED`.
- Disk pressure never adds candidates, cancels work, deletes canonical data, or invokes an emergency deletion path.
- Foreign/absent exports collapse to 404-equivalent behavior before history/assets and perform zero publish/delete work.
- Export streams are bounded, hashes are computed during write, publish is atomic, and the published archive is reopened and independently checked before teardown.
- Plain DELETE still calls only `DeleteConversationLifecycle`; it never touches the export source or destination.
- Audit/metric surfaces carry only finite operation/outcome/failure classes and aggregate counts/bytes, never identity, path, content, or raw error labels.

## Known Stubs

None introduced. The production automatic candidate source intentionally contains only the expiring local classes enabled by the locked defaults; conversation/attached Garage data remain non-expiring until configured, and bounded Neo4j learning retention is owned by Plan 39-07. A scan found no production TODO, FIXME, HACK, panic, or not-implemented marker.

## Verification

- `sqlc generate` - pass; generated output committed and clean.
- `go test ./... -count=1` - pass after all commits.
- `go vet ./...` and `go build ./...` - pass after all commits.
- Task-focused retention/config/DB/cron/CLI/AG-UI/runner/share commands - pass.
- WSL focused Linux `-race` across retention, conversations, documents, cron handlers, CLI, AG-UI, runner, and share - pass.
- WSL `-race -tags db_integration -count=1 -p 1 ./internal/retention` - pass against a disposable database migrated to 44.
- Migration 0044 apply/down/up and bounded parallel claims - pass against a disposable database; version restored to 44.
- Tagged retention coverage - 88.1% statements.
- `scripts/coverage_docker.sh` full `db_integration neo4j_integration` owned gate - 85.3% statements, pass.
- Every disposable PostgreSQL database and Neo4j container used by verification - dropped/removed and verified absent.
- Commit hooks, file-size cap, lint, `git diff --check`, and generated-code cleanliness - pass.

## User Setup Required

None. Existing Postgres migration/config startup applies migration 0044 and seeds the daily retention task. Operators can inspect with `aura retention` and must supply the exact returned token to `aura retention apply --token <token>`.

## Next Phase Readiness

- Plan 39-07 can implement bounded active-learning/Neo4j-adjacent retention against the established audit/metric catalog and finish the phase audit.
- OBS-05 is complete; the remaining Phase 39 work is Plan 39-07.
- No blockers.

---
*Phase: 39-idempotency-observability-pack*
*Completed: 2026-07-21*

## Self-Check: PASSED

**Files verified to exist:**
- FOUND: `internal/db/migrations/0044_retention_operations.up.sql`
- FOUND: `internal/db/migrations/0044_retention_operations.down.sql`
- FOUND: `internal/retention/engine.go`
- FOUND: `internal/retention/activity.go`
- FOUND: `internal/retention/local.go`
- FOUND: `cmd/aura/retention.go`
- FOUND: `internal/cron/handlers/retention.go`
- FOUND: `internal/agui/owner_export.go`
- FOUND: `internal/agui/retention_api.go`

**Commits verified to exist:**
- FOUND: `e1bf96544` (Task 1 RED)
- FOUND: `fbe3f510b` (Task 1 GREEN)
- FOUND: `ee3898dd7` (Task 2 RED)
- FOUND: `93532fbaa` (Task 2 GREEN)
- FOUND: `6d45f9218` (Task 3 RED)
- FOUND: `d7e115fc3` (Task 3 GREEN)
- FOUND: `09d1205de` (PostgreSQL lifecycle correction/coverage)

**Fresh plan-level verification:**
- Full unit suite, vet, build, focused Linux race, and tagged PostgreSQL race - pass.
- Migration 0044 apply/down/up on disposable Postgres - pass.
- Retention tagged coverage 88.1%; full owned tagged matrix 85.3% - pass.
- All disposable verification resources - verified absent.
