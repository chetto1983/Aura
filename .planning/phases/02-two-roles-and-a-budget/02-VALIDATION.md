---
phase: "02"
slug: "two-roles-and-a-budget"
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: "2026-09-08"
---

# Phase 02 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded by `/gsd-plan-phase 2` from `02-RESEARCH.md` § Validation Architecture.
> Task IDs are filled in once PLAN.md files exist; the requirement rows below are already binding.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (backend)** | Go standard `testing` + `//go:build` tags. Tags in use: `db_integration`, `garage_integration`, `authula_integration`, `musr_e2e`, `arcadedb_integration`, `docker_integration` |
| **Framework (frontend)** | Vitest (`web/vitest.config.ts`) + Stryker (`web/stryker.config.json`) — pre-existing, unchanged by this phase |
| **Config file** | none for Go (build-tag lines are the config); `web/vitest.config.ts` / `web/stryker.config.json` for the frontend |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/<touched>/` |
| **Full suite command** | `make quality-full` + `make critical-mutation` + `make web-quality` + `make musr-e2e` |
| **Coverage floor** | 85% aggregate, package-local policy per `scripts/coverage_package_policy.json` (`Makefile:92-93`, `scripts/coverage_gate.sh:29`) |
| **Estimated runtime** | quick ~30s per package · `make quality-full` several minutes (stack must be up) · `make musr-e2e` live |

**Pre-pinned low packages this phase lands on** (`scripts/coverage_package_policy.json`, measured 2026-09-07):
`internal/approvalgrants` 4/57 = 7.0% (line 15) and `internal/webauth` 187/360 = 51.9% (line 79), both
`"mode": "baseline"` — new code there must hold the pinned non-regression floor, not jump to 85%.
`internal/identity` is `"mode": "target"` (line 38) — the full 85% floor applies to the RBAC work landing there.

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test -race ./internal/<touched>/` (CLAUDE.md Post-edit validation); `cd web && npm run test` for frontend-touching tasks.
- **After every plan wave:** the tagged tier the wave's files require (`db_integration` at minimum — every RBAC/CRED row below needs it), plus `cd web && npm run test -- --coverage` for frontend waves.
- **Before `/gsd-verify-work`:** `make quality-full` green, `make critical-mutation` green (with the `GO_SCOPES` decision below resolved), `make web-quality` green, then the closing live run.
- **Max feedback latency:** ~60 seconds at task level (package-scoped race run).

---

## Per-Task Verification Map

Task IDs are `TBD` until `/gsd-plan-phase` writes the PLAN.md files; every row's requirement, test type and command is already fixed by research.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | TBD | TBD | RBAC-01 | — | `HasCapability` no longer expands `*`; migration rewrites wildcard rows | unit + `db_integration` | `go test -race -count=1 ./internal/identity/ -run TestHasCapability` · `go test -tags db_integration -race -count=1 -p 1 -run '^TestMigrateNNNN_' ./internal/db/` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | RBAC-02 | — | Every enforced capability declared in exactly one place | static assertion (no runtime behavior) | new shell test in the shape of `scripts/check_ci_go_packages.sh` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | RBAC-03 | — | Exactly `identity.create`+`identity.delete` administrative; all others granted at provisioning | `db_integration` | `go test -tags db_integration -race -count=1 -run TestProvisionGrantsUniformCapabilitySet ./internal/agui/` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | RBAC-04 | T-02-01 | `identity.create` refused without the capability | unit + handler | `go test -race -count=1 ./internal/identity/ -run TestHasCapability_IdentityCreate` + 403 handler test | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | RBAC-05 | T-02-02 | `identity.delete` required; removal runs the full reverse saga, not a row mark | `db_integration` / `musr_e2e` | HTTP-route test extending `internal/agui/deprovision_test.go:164` `TestDeprovisionPurgeReversesEveryLeg` | ⚠️ partial | ⬜ pending |
| TBD | TBD | TBD | RBAC-06 | T-02-03 | Admin pair never grantable via the capabilities API, for any caller | unit (handler) | `go test -race -count=1 ./internal/agui/ -run TestAdminCapabilityGrantRefusesEscalation` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | RBAC-07 | T-02-04 | Last administrative identity cannot remove or deactivate itself | unit + integration | `go test -race -count=1 ./internal/agui/ -run TestLastAdminCannotRemoveSelf` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | RBAC-08 | — | Fresh install grants the explicit admin set, no wildcard | integration + live | extend `cmd/aura/serve_bootstrap_test.go`; confirmed by the closing live run | ⚠️ partial | ⬜ pending |
| TBD | TBD | TBD | RBAC-09 | T-02-05 | Unknown capability / unresolved principal / store error all deny (fail closed) | unit (fake store returns error) | `go test -race -count=1 ./internal/identity/ -run TestHasCapability_FailsClosed` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | RBAC-10 | — | Every denial auditable (who/capability/route/when), readable from the admin surface | `db_integration` | extend `internal/identity/audit_store_test.go` + `internal/agui/audit_api_test.go` | ⚠️ partial | ⬜ pending |
| TBD | TBD | TBD | RBAC-11 | — | Cockpit creates and removes an identity without leaving the UI | frontend (Vitest) — mechanics only | `cd web && npm run test -- src/settings src/onboarding` | ⚠️ partial | ⬜ pending |
| TBD | TBD | TBD | CRED-01 | T-02-06 | Per-identity OpenRouter key minted at provisioning, stored encrypted, never returned to a browser | unit (httptest) + `db_integration` (RLS/cascade) | `go test -race -count=1 ./internal/<key-package>/` (mirrors `internal/llm/spend_test.go:21-35` `keyServer`) · `go test -tags db_integration -race -count=1 ./internal/<key-package>/` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | CRED-02 | — | New identity starts at a zero cap | unit (httptest asserts `POST /keys` body carries `limit: 0`) | `go test -race -count=1 ./internal/<provisioning-client>/ -run TestMintAtZeroCap` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | CRED-03 | — | Admin sets cap + reset interval from the cockpit and can change both | unit (handler) + frontend | `go test -race -count=1 ./internal/agui/ -run TestAdminSetCredit` + `cd web && npm run test -- src/settings` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | CRED-04 | T-02-07 | Cap enforced by OpenRouter, not by Aura's accounting | unit reproduces the refusal SHAPE only — **real enforcement is not CI-reproducible** | `go test -race -count=1 ./internal/<key-package>/ -run TestRefusesOn403`; provider-side guarantee covered by the live run only | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | CRED-05 | T-02-08 | Zero-credit turn refused before the model is called | unit (mirrors `llmNotConfiguredClient` sentinel pattern) | `go test -race -count=1 ./cmd/aura/ -run TestCreditExhaustedClient` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | CRED-06 | — | Cockpit shows cap/remaining/spend from Aura's in-band ledger, not the provider's lagged counter | `db_integration` + frontend | `go test -tags db_integration -race -count=1 ./internal/agent/ -run TestTurnUsage` + `cd web && npm run test -- src/settings` | ⚠️ partial | ⬜ pending |
| TBD | TBD | TBD | CRED-07 | T-02-09 | No fallback to the deployment key; fail closed | unit | `go test -race -count=1 ./internal/runner/ -run TestSnapshotResolverNoFallback` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | CRED-08 | — | Removing an identity revokes its key; revocation is verified, not assumed | unit (httptest: `DELETE` → `{"deleted":true}`, re-`GET` → 404) | `go test -race -count=1 ./internal/<key-package>/ -run TestRevokeVerifiesDeletion` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | CRED-09 | — | Local backend exempt, says so rather than showing `$0.00` | unit + frontend | `go test -race -count=1 ./internal/llm/ -run TestErrSpendNotApplicable` + frontend `Empty` composition test | ⚠️ partial | ⬜ pending |
| TBD | TBD | TBD | REL-06 | — | `mutation-report.json` ≥70% killed for gateway/identity/profile/sandbox/frontend | mutation | `make critical-mutation` → `scripts/critical_mutation_gate.py` — **see the scope decision below; not a drop-in pass** | ⚠️ see below | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `cmd/aura/llm_client_test.go` — does not exist today; required before CRED-05's `creditExhaustedClient` sentinel is testable at all.
- [ ] A capability-declaration-location assertion script (RBAC-02) — no precedent test exists; `scripts/check_ci_go_packages.sh` is the closest shape to copy.
- [ ] A shared httptest fixture for the OpenRouter Provisioning API (`POST`/`GET`/`PATCH`/`DELETE /keys`) — `internal/llm/spend_test.go:21-35`'s `keyServer` covers only `GET /key`; CRED-01/02/03/08 need mint/patch/delete stubs modeled on `02-OPENROUTER-API.md`'s documented response shapes.
- [ ] A migration test following the `TestMigrateNNNN_` convention (`ci.yml:345`'s existing `TestMigrate0019_`). The number is assigned at landing — `ls internal/db/migrations/ | tail -1` is the only source of the next free slot.
- [ ] **The `GO_SCOPES` mutation-scope decision** (below) must be taken before REL-06 / success criterion 8 can be marked plannable as covered.

---

## Blocking Decision — REL-06 mutation scope

`scripts/critical_mutation_gate.py:17-22` runs `go-mutesting` against exactly **one fixed file per scope**:

```python
GO_SCOPES = {
    "gateway": "internal/gateway/classify.go",
    "identity_isolation": "internal/identityctx/operator.go",
    "profile_validation": "internal/config/config_runtimeprofile.go",
    "sandbox": "internal/sandbox/usersandbox/spec.go",
}
```

**None of those four files is touched by this phase.** Re-running `make critical-mutation` unmodified
would pass the script while leaving criterion 8's actual requirement — "the refusal branches *this phase
adds* are provably killed" — completely unaddressed. `identity_isolation`'s target is no-principal
attribution (`internal/identityctx/operator.go:1-50`), not capability grants: a same-sounding name that
resolves to the wrong file if assumed rather than read.

The plan must take one of:
1. **Extend `GO_SCOPES`** with the files where RBAC-06/07's grant-refusal and CRED-05/07's credit-refusal
   logic actually land (go-mutesting scores one path per scope, so those files must be single-file-mutable).
2. **Accept the weaker guarantee** — REL-06 as wired measures four pre-existing boundaries plus the
   frontend, and "the refusal branches this phase adds" is covered instead by the 85% line-coverage floor
   on touched branches. This does not satisfy the phase's own wording of criterion 8; taking it means
   saying so.

`make critical-mutation` passing is **not** evidence for criterion 8 until this is recorded.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| The reverse saga is observed to land on every plane, driven through the cockpit rather than by curl | RBAC-05, RBAC-11 (criterion 4) | The saga's individual legs are unit/integration-tested (`deprovision_test.go`), but "observed, through the UI" is not expressible at any tier — `human_verify_mode: end-of-phase` in `.planning/config.json` | A creates a third identity and removes it entirely through the cockpit; confirm each plane (Postgres rows, ArcadeDB database, Garage bucket, Authula account, sandbox container, OpenRouter key) is gone |
| OpenRouter's analytics still show a deleted key's consumption | CRED-08 (criterion 7) | A *negative* claim about a third party's data retention. `02-OPENROUTER-API.md` correctly records it under "Not measured — do not assume"; no tier can prove it | Stays `[ASSUMED]`, never `[VERIFIED]`. Stated in the removal dialog's UI copy (UI-SPEC), not tested |
| Real provider-side cap enforcement | CRED-04 | CI has no real key — `ci.yml:366-377` sets `OPENROUTER_API_KEY: ci-degraded-no-network` deliberately | Covered by the 2026-09-08 measurement (CONTEXT.md M-04: `limit: 0` → HTTP 403 `Key limit exceeded`) and re-confirmed by the closing live run |
| "Without leaving the UI" | RBAC-11 | This repo has no browser-driven E2E harness for the cockpit (`web-integration-test`, `ci.yml:587`, is SearXNG/SSRF tooling). Vitest covers the mechanics only | The closing live run drives the full flow |
| Cap-change propagation latency is surfaced rather than looking broken | CRED-03, CRED-06 | Latency is a property of OpenRouter's edge, measured at ~5s down / ~25s up (2026-09-08) | Lower B's cap and observe the refusal within ~5s; raise it and observe the unblock within ~25s, with the cockpit saying so |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (5 items above)
- [ ] REL-06 `GO_SCOPES` decision recorded in a PLAN.md
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
