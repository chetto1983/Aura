---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 07
subsystem: database
tags: [postgres, raw-pgx, rls, share-links, audit-ledger, admin-audit-union]

# Dependency graph
requires:
  - phase: 37F-02
    provides: "aura.shared_links + aura.share_audit schema (migration 0040), config.ShareConfig"
  - phase: 37F-04
    provides: "internal/share Snapshot/redaction core + token.go Mint/Hash + expiry.go ResolveExpiry"
provides:
  - "share.Store — shared_links CRUD over raw pgx (Insert/GetForIdentity/ListForIdentity/ListForConversation/UpdateSnapshot/RevokeForIdentity/RevokeForConversation/DueForExpiry/ResolveByToken/ResolveLiveByID)"
  - "share.ErrShareNotFound sentinel (404-not-403 owner gate + lazy-resolve miss)"
  - "share.AuditWriter — share_audit append-only writer (5 audited actions)"
  - "db.WithIdentityTxRaw — raw-pgx sibling of db.WithIdentityTx for owner-scoped, sqlc-free stores"
  - "4th admin-audit union leg (source='share') in internal/agui/audit_store.go"
affects: [37F-08, 37F-10, 37F-11, 37F-12, admin-audit-ui]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Raw-pgx owner-scoped transaction carrier (db.WithIdentityTxRaw) as the sqlc-free sibling of db.WithIdentityTx, for stores that follow the PgAuditStore precedent"
    - "Dual lazy fail-closed resolvers (ResolveByToken for public/token, ResolveLiveByID for internal/bearer) sharing one duplicated-by-design liveness predicate"
    - "Sentinel-on-miss (ErrShareNotFound) returned from mutating methods via rows-affected==0, not just reads"

key-files:
  created:
    - internal/share/store.go
    - internal/share/audit.go
    - internal/share/store_integration_test.go
    - internal/share/store_mutations_integration_test.go
    - internal/share/audit_integration_test.go
    - internal/agui/share_audit_union_test.go
  modified:
    - internal/db/tx.go
    - internal/agui/audit_store.go

key-decisions:
  - "Added db.WithIdentityTxRaw (internal/db/tx.go) — a raw-pgx sibling of db.WithIdentityTx. sqlc.Queries wraps an unexported `db DBTX` field with no accessor, so a raw-pgx store (as this plan's acceptance criteria mandate, mirroring PgAuditStore) has NO way to run inside WithIdentityTx's *sqlc.Queries callback without adding a sqlc-generated query — which the plan explicitly prohibits. WithIdentityTxRaw mirrors WithIdentityTx's Begin/SET LOCAL/Commit/Rollback/panic semantics exactly, differing only in handing fn a raw pgx.Tx."
  - "FLAGGING A GAP: migration 0032 (the RLS the plan's must_haves cite as the backstop) enables row-level security ONLY on aura.conversations, aura.paused_states, and aura.conversation_turns — NOT on aura.shared_links. Every owner-scoped share.Store method still calls db.WithIdentityTxRaw (setting app.current_identity), matching the plan's literal requirement and future-proofing for when a policy is added, but TODAY that SET LOCAL is inert for this table: the explicit owner_identity_id predicate in each query is the ONLY enforcement layer, not a backstop-plus-predicate pair. This is documented at length in store.go's file header. Recommend a future migration add an owner policy to aura.shared_links to close this gap before the phase relies on the backstop being real."
  - "Corrected a file-attribution error in the plan's own read_first/action text: the plan says to update `internal/agui/audit_api.go:33`'s AuditEvent.Source doc comment, but AuditEvent (and its Source field) is actually defined in internal/agui/audit_store.go:32-33, not audit_api.go. Updated the doc comment at its real location instead of duplicating an orphaned comment into audit_api.go. The acceptance criterion itself hedges with '(or the equivalent updated comment)', which this satisfies."
  - "Added two extra integration test files beyond the plan's files_modified list: store_mutations_integration_test.go (ListForIdentity/ListForConversation/UpdateSnapshot/RevokeForConversation/DueForExpiry) and audit_integration_test.go (AuditWriter.Append). Task 3's own acceptance criterion requires internal/share package coverage under db_integration >= 85%; with only the plan's 9 named security-property tests, coverage was 55.1% (5 Store methods and both AuditWriter methods were never exercised by any test). Split into separate files rather than growing store_integration_test.go past 600 LOC (CLAUDE.md NO GOD CLASS). Coverage after: 85.0%."
  - "Revoke semantics: RevokeForIdentity treats an already-revoked link the same as a foreign/absent one (ErrShareNotFound on rows-affected==0) — revoke is not idempotent-silent. This is a design choice within the plan's freedom (the plan specifies only the method signature, `error` return, no idempotency requirement), matching the REST convention every other *ForIdentity mutation in this codebase follows (0 rows affected = not-found)."
  - "DueForExpiry accepts a caller-supplied `now time.Time` (not the DB clock) — this does NOT violate the plan's 'MUST use the DB clock' prohibition, which is scoped specifically to ResolveByToken/ResolveLiveByID's security-critical liveness predicate. DueForExpiry only selects the sweep's batch (a different, non-security-critical concern), mirroring expiry.go's existing deliberately-clock-free, caller-supplied-now convention. The actual fail-closed security gate is still the DB-clock predicate in the two resolvers, regardless of what the sweep considers 'due'."

requirements-completed: [WEBSHARE-02, WEBSHARE-03]

coverage:
  - id: D1
    description: "share.Store CRUD (9 methods) persists/reads aura.shared_links with owner-gate + lazy fail-closed resolvers"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "grep-based structural verify (WithIdentityTxRaw count, tier='internal' predicate, hash-indexed equality, no subtle/ConstantTimeCompare, rows.Err() count) — internal/share/store.go"
        status: pass
      - kind: integration
        ref: "internal/share/store_integration_test.go + store_mutations_integration_test.go (db_integration tag, 14 tests, live Postgres)"
        status: pass
    human_judgment: false
  - id: D2
    description: "share_audit append-only ledger + 4th admin-audit union leg surfaces share events with source='share'"
    requirement: "WEBSHARE-03"
    verification:
      - kind: unit
        ref: "grep-based structural verify (no UPDATE/DELETE on share_audit, no PII fields in audit.go) — internal/share/audit.go"
        status: pass
      - kind: integration
        ref: "internal/agui/share_audit_union_test.go#TestAuditUnionIncludesShare + internal/share/audit_integration_test.go#TestShareAuditWriterAppend (db_integration tag, live Postgres)"
        status: pass
    human_judgment: false

duration: ~2h (includes coordinator-requested re-verification round)
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 07: Share Storage + Audit Union Summary

**`share.Store` (raw pgx, 9 methods) persists `aura.shared_links` behind a dual-resolver fail-closed gate (`ResolveByToken` for public/token, `ResolveLiveByID` for internal/bearer D-10), and `share.AuditWriter` feeds a new 4th `source="share"` leg into the existing admin audit union — with a new `db.WithIdentityTxRaw` transaction carrier closing the sqlc-vs-raw-pgx gap the plan's own constraints created**

## Performance

- **Duration:** ~2h (this session picked up mid-execution and included a coordinator-requested full re-verification pass, an environmental DB re-seed, and closing a coverage gap discovered during verification)
- **Completed:** 2026-07-17T14:52 CEST
- **Tasks:** 3 planned, all complete (+2 deviation-driven additions: `db.WithIdentityTxRaw`, extra coverage test files)
- **Files:** 8 total (6 created, 2 modified)

## Accomplishments

- `internal/share/store.go` — `share.Store` over raw pgx (`PgAuditStore` precedent, no sqlc query added): `Insert`, `GetForIdentity`, `ListForIdentity`, `ListForConversation`, `UpdateSnapshot`, `RevokeForIdentity`, `RevokeForConversation`, `DueForExpiry`, `ResolveByToken`, `ResolveLiveByID`. `ErrShareNotFound` is the uniform 404-not-403 sentinel, returned both on read-miss (`pgx.ErrNoRows`) and mutate-miss (`rows-affected == 0`).
- The lazy fail-closed pair: `ResolveByToken` (hash-indexed `token_hash = $1` equality, never a `subtle.ConstantTimeCompare` scan) and `ResolveLiveByID` (D-10 bearer-within-auth, `tier = 'internal'` folded into SQL, no owner filter by design) share the byte-for-byte identical `revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())` predicate on the DB clock — an expired or revoked link 404s even if the expiry sweep never runs.
- `internal/share/audit.go` — `share.AuditWriter.Append` inserts the five audited actions (`create`/`update`/`revoke`/`expire`/`open`) into `aura.share_audit`; no plaintext token, no recipient PII/IP/User-Agent ever recorded.
- `internal/agui/audit_store.go` gains a 4th `UNION ALL` leg (`source='share'`) projecting `aura.share_audit`, keyed on the same `identity_id = ANY($1::text[])` array as the other three ledgers; the file header and `AuditEvent.Source` doc comment are updated in the same commit.
- **Deviation:** `internal/db/tx.go` gains `db.WithIdentityTxRaw` — the raw-pgx sibling of `db.WithIdentityTx`, needed because `sqlc.Queries` has no raw-SQL escape hatch and this store is raw pgx by explicit plan mandate.
- 24 integration tests (14 new methods/security-properties + `TestAuditUnionIncludesShare` + existing regression) proven live against Postgres under `db_integration`, package coverage raised from 55.1% to **85.0%** (the plan's own Task 3 acceptance floor).

## Task Commits

Each task was committed atomically:

1. **Task 1: share.Store — shared_links CRUD (+ db.WithIdentityTxRaw deviation)** - `4059e2556` (feat)
2. **Task 2: share_audit writer + 4th admin-audit union leg (+ coverage deviation test)** - `c177cc950` (feat)
3. **Task 3: store integration tests (+ coverage-floor deviation tests)** - `cbabc92d5` (test)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `internal/share/store.go` (432 LOC) - `share.Store` raw-pgx CRUD + dual lazy resolvers
- `internal/share/audit.go` (66 LOC) - `share.AuditWriter` append-only writer
- `internal/share/store_integration_test.go` (426 LOC) - 9 named security-property tests from the plan
- `internal/share/store_mutations_integration_test.go` (263 LOC, deviation) - remaining Store method coverage
- `internal/share/audit_integration_test.go` (68 LOC, deviation) - `AuditWriter.Append` coverage
- `internal/agui/share_audit_union_test.go` (60 LOC) - `TestAuditUnionIncludesShare`
- `internal/db/tx.go` (modified, deviation) - added `WithIdentityTxRaw`
- `internal/agui/audit_store.go` (modified) - 4th union leg + header + `Source` doc comment

## Decisions Made

- **`db.WithIdentityTxRaw` added** (see key-decisions above) — the only way to satisfy both "every owner-scoped method runs through `db.WithIdentityTx`" and "raw pgx, no sqlc query" simultaneously, since `sqlc.Queries.db` is unexported.
- **RLS gap flagged, not silently relied upon.** `aura.shared_links` carries no RLS policy as of migration 0040 (0032 covers only `conversations`/`paused_states`/`conversation_turns`). `WithIdentityTxRaw` is still called on every owner-scoped method (matching the plan's literal requirement and forward-compatible with a future policy), but today the `SET LOCAL app.current_identity` is inert for this table — the explicit `owner_identity_id` predicate is the sole enforcement layer. Documented prominently in `store.go`'s file header per this executor's `security_emphasis` instruction to surface such assumption mismatches rather than quietly relying on them.
- **File-attribution correction**: the plan's Task 2 text cites `internal/agui/audit_api.go:33` for the `AuditEvent.Source` doc comment, but that type/field is actually defined in `audit_store.go:32-33`. Updated the real location; did not duplicate a disconnected comment into `audit_api.go`. The acceptance criterion's own "(or the equivalent updated comment)" hedge covers this.
- **Coverage-floor test additions** (see key-decisions above) — Task 3's own acceptance criterion (`internal/share` coverage under the gate tags `>= 85%`) was unmet (55.1%) with only the plan's named tests; added two more files to reach 85.0%, split to respect the 600-LOC file cap.
- **Environmental fix, not a code deviation:** the seeded `local` identity (`...0001`) was found ABSENT from the live `aura` database mid-verification (a parallel session's coverage/reset run wiped it — the project's documented "Re-seed local identity for db_integration" gotcha). Re-seeded via the exact idempotent statements from migration 0004 (`INSERT ... ON CONFLICT DO NOTHING` on both `aura.identities` and the `*` wildcard grant). This is a data repair against the shared dev database, not a code change, and is not part of this plan's deliverable.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking issue] Added `db.WithIdentityTxRaw`**
- **Found during:** Task 1, before writing any store.go code
- **Issue:** The plan requires every owner-scoped method to route through `db.WithIdentityTx` (grep-gated, `>= 7` occurrences) AND requires the store to be raw pgx with no sqlc query added. `db.WithIdentityTx`'s callback type is `func(*sqlc.Queries) error`, and `sqlc.Queries` wraps an unexported `db DBTX` field with no accessor — there is structurally no way to run a raw SQL string inside that callback without either adding a sqlc-generated query (explicitly prohibited) or extending the `db` package.
- **Fix:** Added `db.WithIdentityTxRaw(ctx, pool, identityID, func(pgx.Tx) error) error` to `internal/db/tx.go`, mirroring `WithIdentityTx`'s exact transaction lifecycle (Begin/SET LOCAL `app.current_identity`/Commit/Rollback/panic-repropagate) but handing `fn` the raw `pgx.Tx`. Since `db.WithIdentityTxRaw` contains the substring `db.WithIdentityTx`, the plan's literal grep acceptance criterion is satisfied by genuine calls, not by gaming the pattern.
- **Files modified:** `internal/db/tx.go`
- **Verification:** `go build ./...`, `go vet ./internal/db/`, `golangci-lint run ./internal/db/...` all clean; the two pre-existing `WithIdentityTx` call sites (`internal/conversations/store_identity.go`, `internal/askuser/store_identity.go`) are untouched and still pass.
- **Committed in:** `4059e2556`

**2. [Rule 2 - Missing critical] Added two extra integration test files for the Task 3 coverage floor**
- **Found during:** Post-Task-3 verification, measuring `internal/share` coverage under `db_integration`
- **Issue:** Task 3's own acceptance criterion states "`internal/share` package coverage under the gate tags is >= 85%." With only the plan's 9 named tests, measured coverage was 55.1% — `ListForIdentity`, `ListForConversation`, `UpdateSnapshot`, `RevokeForConversation`, `DueForExpiry` (all 0.0%) and `AuditWriter.NewAuditWriter`/`Append` (0.0%, from Task 2) were never exercised by any test.
- **Fix:** Added `store_mutations_integration_test.go` (5 tests covering the remaining Store methods) and `audit_integration_test.go` (1 test covering `AuditWriter.Append`). Coverage after: 85.0%.
- **Files modified:** `internal/share/store_mutations_integration_test.go` (new), `internal/share/audit_integration_test.go` (new)
- **Verification:** `go test -tags db_integration -race -p 1 -count=1 ./internal/share/ -coverprofile=...` → `85.0%`; every new test PASS with realistic per-test timing (0.17-0.27s).
- **Committed in:** `c177cc950` (audit_integration_test.go), `cbabc92d5` (store_mutations_integration_test.go)

**3. [Test bug, Rule 1] Fixed duplicate identity name in TestShareStoreOwnerGate**
- **Found during:** First live db_integration run
- **Issue:** `seedIdentity` named rows `"share-store-" + t.Name()`; `TestShareStoreOwnerGate` calls it twice (ownerA, ownerB) within the SAME test function, producing two identical names against `aura.identities.name UNIQUE` (SQLSTATE 23505).
- **Fix:** Appended a fresh UUIDv7 to the seeded name so repeated calls within one test never collide.
- **Files modified:** `internal/share/store_integration_test.go`
- **Verification:** Full `db_integration` suite re-run, all green.
- **Committed in:** `cbabc92d5`

---

**Total deviations:** 3 auto-fixed (1 blocking-issue infrastructure addition, 1 missing-coverage addition, 1 test bug), all documented above. No architectural-change (Rule 4) escalation was needed.

## Verification Evidence (db_integration tier genuinely executed, not skipped)

Per this executor's mandate to prove the integration tier ran rather than compiled-and-skipped:

```
go test -tags db_integration -race -p 1 -count=1 -v ./internal/share/
  ok  github.com/chetto1983/aura/internal/share  4.321s
  24 tests, individual per-test timings 0.15s-0.35s (real DB round trips per test,
  not a sub-second "skip tell")

go test -tags db_integration -race -p 1 -count=1 -v ./internal/agui/ -run \
  'TestAuditUnionIncludesShare|TestPgAuditStoreListActivityForIdentity'
  ok  github.com/chetto1983/aura/internal/agui  1.800s (0.28s + 0.35s individual)

go test -tags db_integration -race -p 1 -count=1 ./internal/share/ ./internal/agui/
  internal/share: ok, 2.840s
  internal/agui: 1 pre-existing unrelated failure (see Deferred Items) — everything else
  green after the local-identity re-seed below
```

Package coverage under `db_integration` (tagged, `go tool cover -func`): **85.0%** total,
every method in this plan's two new files individually >= 69.2% (weakest:
`RevokeForIdentity` 69.2%, `ResolveByToken` 71.4%; strongest: `Insert`/`New`/`scanLink`
100.0%).

## Known Environmental State (not a deviation, logged for transparency)

The seeded `local` operator identity (`00000000-0000-0000-0000-000000000001`) was found
ABSENT from the live `aura` database mid-verification — a parallel session's
coverage/reset run wiped it (the project's documented gotcha). Re-seeded via the exact
idempotent `INSERT ... ON CONFLICT DO NOTHING` statements from migration 0004 (identity
row + `*` wildcard grant) before the final full-package verification run. This is a data
repair, not a code change; flagged here in case the parallel session's activity recurs.

## Deferred Items (logged, not fixed — out of scope)

Appended to `.planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md`:

- `TestHandleCheckTelegramAvailabilityBranches/no_token_configured_reports_not-configured`
  (`internal/agui/settings_api_branches_test.go`) fails under `db_integration`, both before
  and after the local-identity re-seed. Zero code-path overlap with this plan's files
  (share/audit); almost certainly a `TELEGRAM_BOT_TOKEN`-shaped env leak from the parallel
  session's `.env`/shell state. Not fixed here (SCOPE BOUNDARY).

## User Setup Required

None — no new environment variables, no new external service configuration. The new
`db.WithIdentityTxRaw` function and `share.Store`/`share.AuditWriter` types are pure
additions with no wiring into the running server yet (that is plan 37F-08's
`share.Service` and plan 37F-12's route mounts).

## Next Phase Readiness

- `share.Store`, `share.ErrShareNotFound`, `share.AuditWriter`, `share.AuditAction`, and
  `db.WithIdentityTxRaw` are all available for plan 37F-08 (`share.Service` — Create/
  Update/Revoke/ResolveByToken/ResolveInternal/ExpireDue) to build on directly.
- The admin audit UI already surfaces `source="share"` rows with zero frontend changes
  needed (D-14 "surfaces in the existing admin audit UI" is confirmed end-to-end live).
- Recommend a follow-up migration adding an owner-isolation RLS policy to
  `aura.shared_links` (mirroring 0032's `conversations`/`paused_states` policies) so the
  `WithIdentityTxRaw` backstop this plan wires becomes a real second enforcement layer
  rather than inert-but-harmless. Not blocking — the explicit `owner_identity_id`
  predicate already provides full correctness on its own.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 8 created/modified files verified present on disk (`internal/share/store.go`,
`internal/share/audit.go`, `internal/share/store_integration_test.go`,
`internal/share/store_mutations_integration_test.go`,
`internal/share/audit_integration_test.go`, `internal/agui/share_audit_union_test.go`,
`internal/db/tx.go`, `internal/agui/audit_store.go`); all 3 task commit hashes
(`4059e2556`, `c177cc950`, `cbabc92d5`) verified present in `git log --oneline --all`.
