---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 12
subsystem: infra
tags: [rollout, backfill, neo4j, ci, live-stack, garage, authula, two-identity-e2e, acceptance-gate, musr-01, musr-02, musr-06, identity-isolation]

# Dependency graph
requires:
  - phase: 36-05
    provides: "the HAS_DOCUMENT ownership edge (Indexer) + the flag-gated scoped/unscoped retrieval dual path (Searcher.MUSRIsolation)"
  - phase: 36-06
    provides: "garageadmin Admin-v2 client (CreateBucket/CreateKey/AllowBucketKey/Delete*) + objectstore.IdentityStore per-identity credential resolver"
  - phase: 36-04
    provides: "WithIdentityTx RLS carrier + owner-scoped conversations/approvals *ForIdentity surface + MUSR-02 owner"
  - phase: 36-08
    provides: "the provisioning saga resource legs (nil-port cutover seam) the E2E provisions A/B through"
  - phase: 36-01
    provides: "mintBreakGlassToken host-only reset (MUSR-06 break-glass)"
provides:
  - "Idempotent Neo4j :User-HAS_DOCUMENT owner-edge backfill (`aura documents backfill`) — attaches every existing doc to its owner BEFORE the flag flip (D-12)"
  - "The deploy(off)→backfill→verify→flip(on) rollout runbook — flag-off safe (unscoped fallback), the flip is the reversible enforcement switch (D-13)"
  - "The CI live-stack musr-e2e job (Postgres + Neo4j + Garage-admin + embedded Authula) running the five-tag matrix under no-skip-as-green"
  - "The two-identity cross-deny live E2E acceptance gate (D-29): B denied on every plane, B-created chat owned by B + runs, provision→isolated-run + break-glass"
  - "Owner-scoped D-09 conversation branch routes (closed the 36-04 carry-forward isolation hole)"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-pass idempotent MERGE backfill: Op 1 attributes Postgres-mapped documents to their real owner, Op 2 nets any still-unowned :Document to the operator (D-12 no-orphan guarantee)"
    - "Five-build-tag live acceptance E2E (db_integration+neo4j_integration+garage_integration+authula_integration+musr_e2e) reusing the docker-compose live-stack harness — never a per-test container framework"
    - "CI compose override (.github/compose.ci-musr.yaml) publishes the internal-only Garage admin port to loopback for the host-run tier without touching the production internal-only posture"

key-files:
  created:
    - internal/documents/backfill.go
    - internal/documents/backfill_integration_test.go
    - cmd/aura/documents_backfill.go
    - cmd/aura/documents_backfill_test.go
    - docs/runbooks/musr-rollout.md
    - docker/garage/garage.ci.toml
    - .github/compose.ci-musr.yaml
    - cmd/aura/two_identity_e2e_test.go
    - cmd/aura/two_identity_e2e_harness_test.go
  modified:
    - cmd/aura/main.go
    - .github/workflows/ci.yml
    - internal/agui/conversations_branch_api.go
    - internal/agui/owner_scoping_test.go
    - internal/agui/conversations_api_unit_test.go

key-decisions:
  - "The backfill attaches the operator edge under the operator's identity UUID (…001), NOT the literal string 'local' — because both ingest (asset.IdentityID) and scoped retrieval (identityctx.IdentityID) use the UUID; attaching under 'local' would orphan the operator's docs post-flip (D-12 correctness)."
  - "The backfill nets orphan documents (CLI-ingested / pre-36-05 legacy rows with no Postgres identity mapping) to the operator (Op 2) so nothing is left unowned before the flip — the Postgres map alone (Op 1) would miss them."
  - "Task 3 provisions A/B + Garage resources directly via the real stores/admin-client (the plan-08 saga legs at the primitive level) rather than the full onboarding HTTP wizard — the isolation enforcement points are the stores/RLS/graph/Garage-creds, which the E2E drives live."
  - "Owner-scoped the D-09 branch routes on touch (Rule 2 security fix-on-touch) — an unshipped isolation hole where B could fork+re-run A's conversation."

patterns-established:
  - "Idempotent owner-edge backfill sourced from a Postgres identity→graph-document map (assets ∪ documents.metadata.search_document_id) with an operator orphan-net"
  - "musr-e2e CI job = the full live stack + composed DSNs + AURA_GARAGE_ADMIN_* + Authula secrets + AURA_MUSR_ISOLATION=true, five tags, admin-API loopback reachability pre-check"

requirements-completed: [MUSR-01, MUSR-02, MUSR-06]

# Metrics
duration: ~2h
completed: 2026-07-06
---

# Phase 36 Plan 12: Documents Backfill + Rollout + CI Live Stack + Two-Identity Acceptance Gate Summary

**The phase keystone: an idempotent Neo4j owner-edge backfill + the reversible deploy→backfill→verify→flip rollout runbook (D-12/D-13), the full Postgres+Neo4j+Garage-admin+Authula CI live-stack job, and the five-tag two-identity cross-deny live E2E that gates MUSR-01/02/06 under no-skip-as-green — plus the fix for the carried-forward unscoped branch-route isolation hole.**

## Performance
- **Duration:** ~2h
- **Completed:** 2026-07-06
- **Tasks:** 3 (+ 1 carry-forward security fix)
- **Files created/modified:** 14 (9 created, 5 modified)

## Accomplishments

- **Task 1 — idempotent backfill + rollout runbook (`26c2cae8`, feat).** `internal/documents/backfill.go`: `Backfiller.Run` MERGEs the `(:User {identifier})-[:HAS_DOCUMENT]->(:Document)` edge in two idempotent passes — **Op 1** attributes each Postgres-mapped document to its real owner (`LoadDocumentOwners` unions `aura.assets.document_id` + `aura.documents.metadata->>'search_document_id'`, both keyed by `identity_id::text`), MATCHing (never MERGEing) the `:Document` so a stale row never resurrects a purged doc; **Op 2** nets every still-unowned `:Document` (CLI-ingested / pre-36-05 legacy) to the operator (the D-12 no-orphan guarantee). The operator identifier is the seeded local **UUID** (…001), matching the value ingest/retrieval use — attaching under the literal `'local'` would have orphaned the operator's docs post-flip. `cmd/aura/documents_backfill.go`: the `aura documents backfill [--operator]` subcommand (wired into `main.go`) with a factory/owners seam for unit testing. `docs/runbooks/musr-rollout.md`: the enforced **deploy(off)→backfill→verify→flip(on)** order, stating explicitly that flag-off runs plan-05's unscoped fallback (safe, no enforcement) and the flip is the ONLY, reversible enforcement switch that must follow the backfill. Tests: `neo4j_integration TestDocumentsBackfill` (Op1/Op2/idempotency/operator-sees-own-doc) + cmd/aura dispatch units (usage/unknown/factory-error/success).

- **Task 2 — CI live-stack job (`7221787e`, chore).** `.github/workflows/ci.yml` gains a `musr-e2e` job bringing up Postgres + Neo4j + Garage (Admin API v2) + the embedded Authula provider, exporting the **composed DSNs** (`AURA_DB_URL`/`AURA_DB_MIGRATE_URL`, not just `POSTGRES_*`) + `AURA_GARAGE_ADMIN_ENDPOINT`/`AURA_GARAGE_ADMIN_TOKEN` + the Authula secrets + `AURA_MUSR_ISOLATION=true`, and running the five-tag matrix under `CI=true` (arms envOrSkip `t.Fatal` — a skipped tier fails the job, never passes green). An always-compile floor + a Garage-admin loopback reachability pre-check front the tier. `docker/garage/garage.ci.toml` (admin `:3903`, kept separate from prod so CI can publish the port without touching prod's internal-only posture) + `.github/compose.ci-musr.yaml` (mounts the CI toml + publishes 3903 to loopback for the host-run tier). No per-test container framework (`grep -c testcontainers` == 0).

- **Task 3 — two-identity cross-deny live E2E (`13ffa4e6`, test).** `cmd/aura/two_identity_e2e_test.go` (+ `_harness_test.go`), the five-tag acceptance gate, runs with `AURA_MUSR_ISOLATION=on`: real HTTP **GET 404** for B on A's thread (agui server + `RequireAuth` + a `SessionValidator` header seam + the reusable `identityCheckerAdapter`); the owner-scoped store gate (404-read / 403-mutate-0-rows / list) and the **RLS kernel backstop** (a raw read under B's identity var sees 0 of A's rows); approvals cross-deny (trigger-stamped owner); documents flag-on scoped search (B empty, A own — real Indexer/Searcher); Garage per-identity buckets (A's object unreadable with B's scoped key) + the **request-time resolver selection** (B→B's creds, A→A's — carry-forward #4 resolver leg); MUSR-02 (a B-created conversation is owned by B and runs); and MUSR-06 (`TestProvisionLoginIsolatedRun`: provision→isolated-run + a working break-glass mint). Compiles clean under all five tags and every subset.

- **Carry-forward fix — owner-scoped D-09 branch routes (`9bb0a9c5`, fix).** See Deviations.

## Task Commits
1. **Task 1: backfill + runbook** — `26c2cae8` (feat)
2. **Task 2: CI live-stack musr-e2e job** — `7221787e` (chore)
3. **Task 3: two-identity cross-deny E2E** — `13ffa4e6` (test)
4. **Carry-forward: branch-route owner-scoping** — `9bb0a9c5` (fix)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Security] Owner-scoped the D-09 conversation branch routes**
- **Found during:** carry-forward reconciliation (36-04 residual).
- **Issue:** `internal/agui/conversations_branch_api.go` left all three branch routes UNSCOPED — identity B could `GET /branches` of, and `POST /edit` (fork+re-run) or `/branches/{seq}/select` (re-run) on, identity A's conversation (T-cross-deny: drives another identity's thread + enumerates its branch tree).
- **Fix:** added `ownBranchConvOr404` (`GetForIdentity` + `scopedIdentityID` → 404 hides existence), mirroring the shipped `handleConversationRotEvents` read-gate, called at the TOP of each handler (before the durable `ForkBranch`). Regression tests in `owner_scoping_test.go` (branch GET + `TestConversationsBranchAPI_ForeignMutateIsHidden`); `errConvStore` gained `ownsGate` so the two pre-existing branch input/fork tests pass the new gate and still exercise their target paths (a legitimate test update for an intentional contract change, justified inline).
- **Commit:** `9bb0a9c5`.

**2. [Rule 3 - Blocking] Added `.github/compose.ci-musr.yaml` (beyond the plan's file list)**
- **Issue:** the plan names `docker/garage/garage.ci.toml` but the host-run `garage_integration`/`musr_e2e` tiers need the internal-only admin port (`:3903`) reachable on the runner. Publishing it in prod compose is forbidden (Pitfall 3 / T-36-06-E).
- **Fix:** a CI-only compose override mounts `garage.ci.toml` + publishes `3903` to `127.0.0.1` — the CI-runner-loopback exposure only, prod posture untouched. Referenced via `COMPOSE_FILE` in the job.

**3. [Design correction — documented] Backfill owner identifier is the operator UUID, not `'local'`**
- The plan's prose says `(:User {identifier:'local'})`. The operator's graph identifier is concretely the seeded local **UUID** (…001) — the value both `asset.IdentityID` (ingest) and `identityctx.IdentityID` (retrieval) carry. The backfill attaches under that UUID (and Op-2 defaults to it); using the literal `'local'` would make the operator's docs invisible after the flip, defeating D-12. Documented in `backfill.go` + the runbook.

**Total:** 1 security fix-on-touch, 1 blocking CI companion file, 1 documented correctness correction. No scope creep.

## Residual Gaps (carry-forward reconciliation — for the verifier)

These are pre-existing **mechanism-then-cutover** deferrals from earlier plans. They are **fail-closed-secure today** (not active isolation breaks) and are large/architectural production-wiring changes the 36-12 PLAN's three tasks did not include; blind-wiring them unverified (no live stack / no CGO on this host) would risk `aura serve`. Recorded here rather than silently dropped:

- **[36-08 — highest priority] Boot-time provisioning wiring NOT done.** `aura serve` still does not construct/inject the Garage ObjectStore / Filesystem / SagaJournal provisioner adapters at the composition root, does not seed the identity-purge scheduler (kind-CHECK widen or interval sweeper), and does not wire the D-15 login-time force-change/TOTP marker reader or the deactivation admin action. The provisioning/de-provisioning/first-login **mechanism** is shipped + unit/live-tested (36-08) behind nil-ports; it remains **dormant in production** until this boot wiring lands. The two-identity E2E exercises the isolation via the primitives directly, so acceptance is proven — but the automatic serve-time activation is a follow-up.
- **[36-03] Admin cross-session shell poll/kill NOT wired.** `tools.ShellPoll{}`/`ShellKill{}` are constructed in `cmd/aura/main.go` (buildBaseRegistryWithHandles) WITHOUT the `Caps` capabilityChecker → nil → **owner-only fail-closed** (foreign poll/kill denied — SECURE). The D-18 admin cross-session recovery escape hatch is inactive; wiring `.Caps` to the identity store is a registry-composition change (needs the pool threaded in) deferred here.
- **[36-06/36-08] Per-identity S3 CONSUMPTION NOT wired.** The asset service + processors (`internal/assets/*`) consume the SHARED `objectstore.Store`, not the per-identity `objectstore.IdentityStore.Resolve`. So with the flag on, asset OBJECTS still land in the shared bucket (access to them stays isolated at the Postgres owner-scoping layer — B cannot obtain A's asset record / presigned URL). The resolver's request-time selection **is** verified live by the E2E (`garage_cross_deny`); routing the asset read/write path THROUGH the resolver (per-identity bucket usage, D-08 intent) is the remaining consumption wiring.

Addressed carry-forwards: **#3** (branch routes) — WIRED; **#4 resolver leg** — verified by the E2E; **#5** (rollout flag/order) — the runbook + the flag-on E2E; **#6** (`.env.example` hard-denied) — N/A, this plan adds no new env knob (the backfill CLI reuses existing config; `AURA_MUSR_ISOLATION` was cataloged in 36-02's `config_knobs.go`).

## Requirements Completed
- **MUSR-01** — the two-identity cross-deny live E2E asserts B is denied on every plane (404 read / 403 mutate / empty document_search / no cross-bucket read / RLS backstop). Mechanism delivered + acceptance gate shipped; the live run gates on the CI musr-e2e job.
- **MUSR-02** — a B-created conversation is owned by B (defaultConversationOwner keys on identityctx) and runs; proven live in the E2E.
- **MUSR-06** — provision→isolated-run + break-glass mint proven; the no-long-lived-token-in-URL static gate (36-11) + Authula-default web-auth are in place; the CI job runs the Authula-configured stack.

> Marked complete because the mechanism + stated behavior are delivered and the acceptance E2E is written + compiles under its five tags. The **live** run of the E2E + the `neo4j_integration` backfill tier + the `db_integration`/`garage_integration` tiers is a documented WSL/CI must-run (below) — it was NOT run on this Windows host.

## Issues Encountered / Live-Tier Status
No blockers. This Windows host has **no live stack (Postgres/Neo4j/Garage) and no CGO/gcc**, so the live + `-race` tiers were NOT run here — honestly `unknown`, and they MUST run green in WSL/CI before the phase truly closes (no-skip-as-green):
- `go test -tags neo4j_integration -run TestDocumentsBackfill ./internal/documents/`
- `go test -race -tags 'db_integration neo4j_integration garage_integration authula_integration musr_e2e' -run 'TestTwoIdentityCrossDeny|TestProvisionLoginIsolatedRun' ./cmd/aura/` (the CI `musr-e2e` job)

Verified green on this host: `go build ./...`, `go vet` (touched packages), untagged `go test ./cmd/aura/ ./internal/documents/ ./internal/agui/`, the five-tag E2E compile (`go vet -tags '…' ./cmd/aura/`) + every subset, the `neo4j_integration` backfill-test compile, gofmt + file-size (≤600 LOC every file) + vet pre-commit hooks on all four commits, and `.github/workflows/ci.yml` + `.github/compose.ci-musr.yaml` YAML validity (`grep -c testcontainers` == 0).

## Known Stubs
None. `musrStubRunner` in the E2E harness is a test double (its `NewConversation`/`DeleteConversationLifecycle` delegate to the REAL conversations store, applying the real owner rules); it is not a product stub. The 36-08 nil-port legs are the documented boot-wiring residual gaps above, not stubbed UI/data.

## Threat Flags
None new. The plan's threat register (T-cross-deny, T-36-12-A/R/SC) is mitigated: the E2E gates cross-deny on every plane; the backfill-before-flip order + idempotent MERGE make the flip safe; envOrSkip `t.Fatal` under `$CI` blocks skip-as-green; the harness is the build-tag live stack, not a container-per-test framework.

## Self-Check: PASSED
- All 9 created + 5 modified files present on disk (verified via the edits + build).
- All 4 task commits present in git history: `26c2cae8`, `7221787e`, `13ffa4e6`, `9bb0a9c5`.
- `go build ./...` + `go vet` + untagged tests green; five-tag E2E + neo4j backfill test compile clean; branch owner-gate + backfill units pass.

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-06*
