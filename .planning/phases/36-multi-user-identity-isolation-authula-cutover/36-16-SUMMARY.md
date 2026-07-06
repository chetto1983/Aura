---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 16
subsystem: security
tags: [multi-user, identity-isolation, documents, neo4j, config-validate, provisioning, onboarding, fail-closed]

# Dependency graph
requires:
  - phase: 36-05
    provides: "config.MUSRIsolation flag + the six fail-closed scoped documents-plane Cypher queries"
  - phase: 36-14
    provides: "buildOnboardingService resource-leg wiring (ObjectStore/Filesystem/Journal) this plan layers MUSRIsolation onto"
  - phase: 36-12
    provides: "documents backfill (OperatorIdentity edge) + docs/runbooks/musr-rollout.md the boot WARN points at"
provides:
  - "config.gateMUSRIsolation — server_production Fatal validation when AURA_MUSR_ISOLATION is off (VERIF-5)"
  - "provision-time refusal (errIsolationDisabled) when isolation is off — arming the documents leak by adding a 2nd principal is impossible (CR-01)"
  - "boot-time >1-identity-flag-off WARN in buildOnboardingService"
  - "empty->local UUID normalization on document_search (ownerFromContext) + ingest (OperatorIdentity) — operator's own docs stay reachable post-flip (ME-01, LO-03)"
affects: [36-18, documents, agui, config]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Defense-in-depth coupling: refuse-to-arm (provision refusal) + fail-validation (server_production gate) + loud boot WARN — three layers so a default-off security control cannot ship silently"
    - "empty->local UUID normalization at every plane boundary (ownerFromContext / OperatorIdentity) so the CLI operator's local-owned data stays reachable under fail-closed isolation"

key-files:
  created:
    - internal/agui/onboarding_provision_isolation_test.go
  modified:
    - internal/config/config_validate.go
    - internal/config/config_validate_test.go
    - internal/agui/onboarding_session.go
    - internal/agui/onboarding_provision.go
    - internal/agui/onboarding_api.go
    - cmd/aura/serve_onboarding.go
    - internal/agent/tools/document_search.go
    - internal/documents/indexer.go
    - internal/agent/tools/document_search_test.go
    - internal/documents/indexer_test.go

key-decisions:
  - "Isolation refusal placed at the top of Provision's step-0 PRE-VALIDATE (after in-memory session resolution, before the first cross-store write / Authula lookup) so an abandoned/foreign session still returns its own error, and the refusal precedes any write"
  - "errIsolationDisabled maps to HTTP 409 with its fixed, secret-free actionable message (admins hold identity.create) — the action conflicts with current server config"
  - "gateMUSRIsolation scoped to server_production ONLY (mirrors gateReplication) — single_user_hardened + lenient tiers are single-principal, no requirement"
  - "MUSR-01 NOT marked complete: CR-01 (documents coupling) is closed here, but MUSR-01 is phase-spanning (object-store consumption + the live full-matrix E2E + push close at 36-18) — follows the 36-05/06/08/10/12 precedent"
  - "RLS permissive-on-unset (ME-02) NOT tightened + the D-06 403/404 oracle (LO-01) accepted — recorded follow-ups, per the plan's explicit instruction"

patterns-established:
  - "Security control coupling: a default-off enforcement flag gets an in-code refusal + a profile-validate Fatal + a boot WARN, not just a runbook"

requirements-completed: []  # MUSR-01 is phase-spanning; CR-01/VERIF-5 closed here, requirement closes at 36-18 (matches prior 36-xx precedent)

coverage:
  - id: D1
    description: "server_production config-validate gate: AURA_MUSR_ISOLATION off is Fatal; other tiers unaffected; aggregates into Validate()"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/config/config_validate_test.go#TestGateMUSRIsolation"
        status: pass
      - kind: unit
        ref: "internal/config/config_validate_test.go#TestValidateProfile (AURA_MUSR_ISOLATION in wantKnobs)"
        status: pass
    human_judgment: false
  - id: D2
    description: "provisioning refuses (errIsolationDisabled, zero writes) when isolation off; proceeds when on; boot WARN on >1 identity + flag off"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/agui/onboarding_provision_isolation_test.go#TestProvisionRefusedWhenIsolationOff"
        status: pass
      - kind: unit
        ref: "internal/agui/onboarding_provision_isolation_test.go#TestProvisionProceedsPastIsolationGateWhenOn"
        status: pass
    human_judgment: false
  - id: D3
    description: "empty->local UUID normalization on document_search + ingest; operator (local UUID) retrieves own docs with the flag on, cross-identity denied"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/agent/tools/document_search_test.go#TestDocumentSearchToolThreadsOwnerIdentity"
        status: pass
      - kind: unit
        ref: "internal/documents/indexer_test.go#TestIndexerNormalizesEmptyIdentityToOperator"
        status: pass
      - kind: integration
        ref: "internal/documents/fail_closed_integration_test.go#TestDocumentsFailClosed (neo4j_integration, live Neo4j)"
        status: pass
      - kind: integration
        ref: "internal/documents/backfill_integration_test.go#TestDocumentsBackfill (neo4j_integration, live Neo4j)"
        status: pass
    human_judgment: false

# Metrics
duration: ~50 min
completed: 2026-07-06
status: complete
---

# Phase 36 Plan 16: Couple documents-plane isolation to provisioning + config fail-fast + identity scoping (CR-01/VERIF-5) Summary

**CR-01 closed: the default-OFF documents-plane (Neo4j) isolation is now coupled to provisioning (refuse-to-arm) + server_production validation (Fatal) + a loud boot WARN, and empty->local normalization keeps the operator's own local-owned docs reachable after the flip.**

## Performance

- **Duration:** ~50 min
- **Started:** 2026-07-06T09:45:00+02:00 (approx)
- **Completed:** 2026-07-06T10:35:00+02:00 (approx)
- **Tasks:** 3
- **Files modified:** 9 modified + 1 created

## Accomplishments
- **VERIF-5 gate:** `config.gateMUSRIsolation` makes `AURA_MUSR_ISOLATION` a required `server_production` secret-equivalent — a prod deploy that passes `aura config validate` is now forced to enable isolation (Fatal when off), the hardened↔prod differentiator (mirrors `gateReplication`).
- **CR-01 code coupling:** `Provision` returns `errIsolationDisabled` BEFORE any cross-store write when the flag is off. The onboarding saga only ever creates ADDITIONAL, non-local identities, so refusing here makes arming the documents leak (adding a 2nd principal with the flag off) structurally impossible — the exploit path (fresh deploy, admin creates user B, B reads operator A's chunks) can no longer be reached.
- **Loud boot WARN:** `buildOnboardingService` emits a LOUD `slog.Warn` when `>1` live identity exists while the flag is off (the documents plane is UNSCOPED right now), pointing the operator at `aura documents backfill` + the D-13 rollout runbook.
- **Availability fixes (ME-01, LO-03):** `document_search` threads `ownerFromContext(ctx)` (empty->local UUID …001) instead of the raw empty identity, so the CLI operator's own local-owned documents stay retrievable with isolation on; `indexer.UpsertSparse` normalizes an empty ingest identity to `documents.OperatorIdentity` so an empty-identity ingest attributes to the operator instead of orphaning under `(:User {identifier:""})`.
- **LIVE CR-01 proof:** the `neo4j_integration` documents-isolation tier passed against live Neo4j — `flag_on_identity_B_cross_deny` + `flag_on_identity_A_finds_own` + fail-closed-empty + GraphRAG-cross-deny + flag-off-fallback, plus the backfill tier.

## Task Commits

Each task was committed atomically (direct git commit, real hooks):

1. **Task 1: server_production config-validate gate for AURA_MUSR_ISOLATION (VERIF-5)** - `6c75a123` (feat)
2. **Task 2: Refuse provisioning + boot WARN when isolation is off (CR-01 code coupling)** - `9ab107e1` (feat)
3. **Task 3: Empty->local normalization on the documents search + ingest paths (ME-01, LO-03)** - `15e0066b` (feat)

**Plan metadata:** this commit (docs: complete plan)

## Files Created/Modified
- `internal/config/config_validate.go` - `gateMUSRIsolation` (server_production Fatal) + wired into `ValidateProfile`
- `internal/config/config_validate_test.go` - `TestGateMUSRIsolation` table test + `AURA_MUSR_ISOLATION` added to the `TestValidateProfile` aggregation assertion + `MUSRIsolation:true` in the otherwise-valid prod fixture
- `internal/agui/onboarding_session.go` - `OnboardingDeps.MUSRIsolation` + `onboardingService.musrIsolation` field + constructor wiring
- `internal/agui/onboarding_provision.go` - `errIsolationDisabled` sentinel + refusal at the top of Provision's step-0 pre-validate
- `internal/agui/onboarding_api.go` - `writeOnboardingError` maps `errIsolationDisabled` to a clean 409
- `cmd/aura/serve_onboarding.go` - sets `deps.MUSRIsolation = chat.cfg.MUSRIsolation` + `warnIfMultiUserWithoutIsolation` boot WARN
- `internal/agent/tools/document_search.go` - `ownerFromContext(ctx)` (empty->local UUID) for the search IdentityID; dropped the now-unused `identityctx` import
- `internal/documents/indexer.go` - empty ingest identity normalized to `OperatorIdentity` before the ownership MERGE
- `internal/agui/onboarding_provision_isolation_test.go` (NEW) - refusal-when-off + proceeds-when-on
- `internal/agent/tools/document_search_test.go` - `TestDocumentSearchToolThreadsOwnerIdentity` (empty->local + verbatim web principal)
- `internal/documents/indexer_test.go` - `TestIndexerNormalizesEmptyIdentityToOperator` (empty->operator + verbatim)
- `internal/agui/onboarding_provision_fakes_test.go` + `internal/agui/onboarding_provision_integration_test.go` - `MUSRIsolation:true` on the shared/live saga-service helpers so saga-body tests clear the new gate

## Decisions Made
- **Refusal placement:** at the top of the step-0 PRE-VALIDATE block — after in-memory `sessionForRequester` resolution, before the nil-port check / `validateNoEscalation` / Authula `UserByEmail`. This keeps `errOnboardingSessionNotFound`/`errOnboardingForbidden` for an abandoned/foreign session (they short-circuit earlier) while guaranteeing the refusal precedes the first cross-store write. `validateOnboardingProvision` (shape validation) still runs first, so a malformed request is still a validation error.
- **HTTP status:** `errIsolationDisabled` -> 409 Conflict with its fixed message surfaced (only admins holding `identity.create` reach the route; the message is secret-free and actionable). Follows `provisionFail`'s fixed-label convention (no secret echo).
- **Gate scope:** `server_production` only (single_user_hardened is the single-node appliance tier where multi-user isolation is not required — matches `gateReplication`'s hardened↔prod differentiator).
- **Test-helper coupling:** the shared `sagaService` + the live `db_integration` `service()` helper now set `MUSRIsolation:true` so the existing saga-body / compensation / live-orphan tests reach the saga past the new gate; the gate itself is covered by the dedicated isolation test. Abandoned/mismatched-session tests are untouched (they return before the gate).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Wired errIsolationDisabled into the HTTP error mapping (409)**
- **Found during:** Task 2
- **Issue:** The plan added the sentinel + refusal but the handler's `writeOnboardingError` had no case for it, so it would have fallen through to a generic 502 (a policy refusal rendered as a backend failure, and losing the actionable operator message).
- **Fix:** Added an `errors.Is(err, errIsolationDisabled)` case mapping to 409 with the sanitized (secret-free) message, and updated the mapping doc comment.
- **Files modified:** internal/agui/onboarding_api.go
- **Verification:** `go build`/`go vet` clean; untagged `go test ./internal/agui/` green.
- **Committed in:** `9ab107e1` (Task 2 commit)

**2. [Rule 2 - Missing Critical / Coverage] Added focused unit tests for the Task-3 normalization**
- **Found during:** Task 3
- **Issue:** The plan's Task-3 `files` listed only the two source files, but its acceptance criteria call for behavior assertions (empty-principal search threads the local UUID; empty-ingest passes OperatorIdentity). Without them the ME-01/LO-03 availability fixes had no regression net (exactly the kind of silent zero-results regression that only shows post-flip).
- **Fix:** Added `TestDocumentSearchToolThreadsOwnerIdentity` (document_search_test.go) and `TestIndexerNormalizesEmptyIdentityToOperator` (indexer_test.go), each proving empty->local and the verbatim non-empty path.
- **Files modified:** internal/agent/tools/document_search_test.go, internal/documents/indexer_test.go
- **Verification:** both green untagged + under `-race` in WSL.
- **Committed in:** `15e0066b` (Task 3 commit)

**3. [Rule 3 - Blocking] Test-helper MUSRIsolation:true so the new gate doesn't break existing saga tests**
- **Found during:** Task 2
- **Issue:** The provision-time refusal gate would make every existing saga-body test (which builds services via `sagaService` / the live `service()` helper without setting isolation) return `errIsolationDisabled`, breaking ~25 provision tests + the live saga tier.
- **Fix:** Set `MUSRIsolation:true` in the two shared helpers (`onboarding_provision_fakes_test.go` `sagaService`, `onboarding_provision_integration_test.go` `service`). This is correct: those tests exercise the saga legs, not the isolation gate, which is covered separately.
- **Files modified:** internal/agui/onboarding_provision_fakes_test.go, internal/agui/onboarding_provision_integration_test.go
- **Verification:** untagged `go test ./internal/agui/` + `-race` green.
- **Committed in:** `9ab107e1` (Task 2 commit)

---

**Total deviations:** 3 auto-fixed (2 missing-critical/coverage, 1 blocking)
**Impact on plan:** All within the plan's stated acceptance/verification intent (409 mapping, behavior-assertion tests, gate/test coupling). No scope creep — no query/schema/dependency changes.

## Recorded Follow-ups (accepted, per plan)

- **ME-02 — RLS permissive-on-unset:** `internal/db/migrations/0032_owner_rls.up.sql` is permissive when `app.current_identity` is unset BY DESIGN (it backstops only the `WithIdentityTx` paths). It was **NOT** tightened in this plan — tightening to fail-closed-on-unset would break the runner/CLI/Telegram write paths and the D-06 403-vs-404 probe. Deferred until every writer sets the GUC, per the migration's own comment. Leaving it as-is is correct for now.
- **LO-01 — D-06 403-vs-404 existence oracle:** `writeForeignMutateStatus` returns 403 on a foreign-but-existing conversation vs 404 for absent, confirming existence across identities. Accepted design tradeoff (unguessable v7 UUIDs make enumeration infeasible). Note only, no change.

## Threat Flags
None — no new network endpoints, auth paths, file-access patterns, or schema changes. The config gate, provision refusal, and empty->local normalization are all within existing surfaces. No new dependencies (go.mod/go.sum byte-unchanged; no query files touched, so `sqlc generate` is trivially zero-diff).

## Issues Encountered
- Direct `git commit` exceeds the 2-min Bash foreground limit because the file-size lefthook scans the whole tree (~70s). Resolved by running each per-task commit with `run_in_background` and polling until it lands (per the documented host workflow).

## Verification (real results)
- `CGO_ENABLED=0 go build ./...` — exit 0 (clean).
- `CGO_ENABLED=0 go vet ./...` — exit 0 (clean).
- Native untagged `go test ./internal/config/ -run 'Validate|Profile|MUSR'` — ok (server_production+off Fatal; +on clean; other tiers unaffected).
- Native untagged `go test ./internal/agui/ -run 'Isolation|Provision'` — ok (flag-off refuses with zero writes; flag-on proceeds).
- Native untagged `go test ./internal/config/ ./internal/agui/ ./internal/agent/tools/ ./internal/documents/ ./cmd/aura/` — all ok.
- **LIVE WSL `neo4j_integration`** `go test -tags neo4j_integration -run 'TestDocumentsFailClosed|TestDocumentsBackfill' ./internal/documents/ -count=1 -v` — **PASS (17.95s real, not skipped)**: `TestDocumentsBackfill` PASS (13.60s); `TestDocumentsFailClosed` PASS (4.34s) with all 5 subtests green (empty-fail-closed, B cross-deny, A finds own, graphrag cross-deny, flag-off fallback). This is the CR-01 live evidence.
- **`-race` (WSL, CGO on):** `internal/config`, `internal/agent/tools`, `internal/documents`, `cmd/aura`, `internal/agui` all ok.

## Next Phase Readiness
- CR-01 (Critical) / VERIF-5 closed: documents-plane isolation is now enforced-by-default-when-multi-user (refuse-to-arm + server_production Fatal + boot WARN) and the operator's own local-owned docs stay reachable post-flip.
- Remaining for 36-18 (phase close): the live full-matrix two-identity E2E (Garage/Authula stack, `musr-e2e` CI job), full-matrix coverage ≥85%, mutation spot-check, CI-green + push. MUSR-01 stays `[ ]` until that live E2E + push (phase-spanning, matches 36-05/06/08/10/12).
- ME-02 (RLS tightening) + LO-01 (403/404 oracle) recorded as accepted follow-ups; not blockers.

## Self-Check: PASSED
- All modified/created source files present on disk (7/7 spot-checked, incl. the SUMMARY).
- All three task commits found in git history: `6c75a123`, `9ab107e1`, `15e0066b`.
- Source assertions hold: `gateMUSRIsolation` (3 refs) in config_validate.go; `errIsolationDisabled` (3 refs) in onboarding_provision.go; `ownerFromContext(ctx)` (1 ref) in document_search.go; `OperatorIdentity` (1 ref) in indexer.go.

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-06*
