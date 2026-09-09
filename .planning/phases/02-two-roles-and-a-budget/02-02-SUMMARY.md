---
phase: 02-two-roles-and-a-budget
plan: 02
subsystem: auth
tags: [rbac, capability-grants, deprovisioning, provisioning-saga, ci-gate]

# Dependency graph
requires:
  - phase: 02-01
    provides: internal/identity/capabilities.go's declared name set (Administrative()/UserSet()/All()/IsAdministrative), migration 0121 retiring the wildcard, the tracer test's live composition pattern
provides:
  - internal/identity/capability_policy.go — the single pure file deciding every refusal this phase adds (CanGrantThroughAPI/CanRevokeThroughAPI/CanRemoveIdentity/CanDeactivateIdentity)
  - The capabilities API (POST/DELETE /api/admin/identities/{id}/capabilities) refusing both administrative names for every caller, before any store call
  - The deprovision saga (Deactivate/Purge, covering CLI + cron + the future HTTP route) refusing an administrative identity's self-removal
  - The onboarding saga granting exactly identity.UserSet() unconditionally, refusing (not narrowing) a request naming an administrative capability
  - scripts/check_capability_declaration.sh wired into CI and `make quality`, keeping the capability declaration in exactly one file
affects: [02-03, 02-04, 02-05, 02-06, 02-07, 02-08, 02-09, 02-10]

# Actuals (#2632)
actuals:
  tokens: 15070   # chars/4 over this session's own 2 commits' patch text (95a6acda9 + b5a669d07; excludes Task 1's prior commits and the interleaved foreign commit 7ea283d8c)
  tasks: 2        # Task 2 + Task 3 (Task 1 was already committed by a prior process before this continuation began)
  commits: 2
  plan_head_before: 588921442ec

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Refusal predicates live in exactly one pure file (internal/identity/capability_policy.go): zero I/O, zero context, zero database — a mutant of the decision cannot be masked by a transport-layer mapping, and go-mutesting's single-file-per-scope constraint (scripts/critical_mutation_gate.py) can actually reach every branch."
    - "A caller-context headless sentinel (headlessDeprovisionCaller in deprovision.go) lets a single refusal predicate serve three callers (HTTP, CLI, cron sweep) without the sweep ever colliding with 'caller equals subject' — the sentinel can never equal a real identity id by construction."
    - "A provisioning request's capability list stops SELECTING the grant (RBAC-03): the saga always grants identity.UserSet(), and the request is inspected only to refuse an administrative name — every other well-formed/malformed/undeclared value is ignored, not validated."
    - "A CI declaration-uniqueness check derives its name list from the declaration file itself (grep pattern over `^const Cap... = \"...\"`), never hard-codes the names — a hard-coded list in the checker would itself be the second declaration point the check exists to forbid."
key-files:
  created:
    - internal/identity/capability_policy.go
    - internal/identity/capability_policy_test.go
    - internal/agui/capability_api_test.go
    - internal/agui/deprovision_last_admin_test.go
    - internal/agui/onboarding_provision_grants.go
    - internal/agui/onboarding_provision_grants_test.go
    - scripts/check_capability_declaration.sh
  modified:
    - internal/agui/audit_api.go
    - internal/agui/audit_api_test.go
    - internal/agui/audit_api_branches_test.go
    - internal/agui/deprovision.go
    - internal/agui/onboarding_api.go
    - internal/agui/onboarding_provision.go
    - internal/agui/onboarding_provision_fakes_test.go
    - internal/agui/onboarding_provision_test.go
    - cmd/aura/serve_provisioning.go
    - .github/workflows/ci.yml
    - Makefile

key-decisions:
  - "TestNoEscalation (internal/agui/onboarding_provision_test.go) was REWRITTEN, not deleted or left red: it pinned the pre-Phase-2 subset-of-creator-grants contract, which D-01/RBAC-03 and the plan's own Task 2 action text explicitly retire ('this is no longer a subset check, it never was here'). Every no-write assertion on a genuine refusal path was kept; the five subtests asserting a rejection for '*'/undeclared/malformed capability names were replaced with a table proving those inputs are now ignored and the grant is always identity.UserSet()."
  - "Two more pre-existing tests (TestMutateCapabilityGuards's wildcard subtest in audit_api_branches_test.go; TestGrantCapabilityRejectsInvalidName in audit_api_test.go) expected 400 for a capability the STORE rejected. Task 2a's own instruction ('map every sentinel to 403') moved that rejection earlier, into identity.CanGrantThroughAPI, which folds ValidateCapabilityName's ErrWildcardManaged/ErrInvalidCapability into the same 403 every other refusal gets — the store is never reached for these inputs anymore. Both tests were updated to assert 403 and that the store's Grant/RevokeCapability is never called, which is a STRICTER assertion than the one it replaced, not a weaker one."
  - "cmd/aura/serve_provisioning.go and internal/agui/onboarding_api.go were edited even though neither appears in the plan's top-level `files_modified` frontmatter list. Both edits are the plan's own Task 2 action text made concrete: serve_provisioning.go's ListPurgeable/ResolveTarget project IsAdministrative (Task 2b: 'DeprovisionTarget does not carry it today... a narrow new consumer-side port'), and onboarding_api.go's doc-comment update on OnboardingProvisionRequest is Task 2d's explicit instruction ('leave a comment naming 02-08 as the plan that removes the field'). Both are in-scope; the frontmatter list is simply not exhaustive of every file a task's action text implies."
  - "TDD ordering (RED before GREEN) could not be honored for Task 2: the prior executor process wrote the full implementation before crashing on two API 500s, leaving zero of the task's tests written. This continuation wrote the tests against the already-existing implementation, which is a GREEN-only history for this task — not a fabricated RED commit. Every new refusal assertion was independently verified to genuinely exercise the guard: the two guard calls in deprovision.go (identity.CanDeactivateIdentity/CanRemoveIdentity) were temporarily removed, the corresponding new tests (TestLastAdminCannotRemoveSelf, TestLastAdminSelfRemovalIsIdempotentlyRefused) were confirmed to fail while the unrelated tests in the same run stayed green, then the file was restored byte-identical from a backup and re-verified building and passing clean. capability_api_test.go's assertions were verified the ordinary way (they exercise the already-committed, independently-tested capability_policy.go predicates from Task 1)."

patterns-established:
  - "Pattern: when a plan retires a prior contract a still-standing test pins, rewrite that test with an inline comment citing the decision (D-xx/RBAC-xx) and the plan/task that retires it, keep every no-write/no-side-effect assertion the original had, and never weaken an assertion — strengthen it if the new contract allows (e.g. adding 'store never called' where the old test only checked a status code)."
  - "Pattern: proving a shell-script gate's negative case means creating the violating fixture, running the script, observing the failure output, then deleting the fixture — never reading the script and reasoning about what it would do. Applied twice here (a planted second declaration, and a temporarily emptied declaration file), each restored from a real backup and re-verified building clean afterward."

requirements-completed: [RBAC-02, RBAC-03, RBAC-06, RBAC-07, RBAC-09]

coverage:
  - id: D1
    description: "The two administrative capabilities (identity.create, identity.delete) are ungrantable/unrevokable through the capabilities API for every caller, including one who already holds them — the refusal happens in identity.CanGrantThroughAPI/CanRevokeThroughAPI before mutateCapability ever calls the store."
    requirement: "RBAC-06"
    verification:
      - kind: unit
        ref: "internal/identity/capability_policy_test.go#TestCanGrantThroughAPI_RefusesAdministrative"
        status: pass
      - kind: unit
        ref: "internal/identity/capability_policy_test.go#TestCanRevokeThroughAPI_MirrorsGrant"
        status: pass
      - kind: unit
        ref: "internal/agui/capability_api_test.go#TestAdminCapabilityGrantRefusesEscalation"
        status: pass
      - kind: unit
        ref: "internal/agui/capability_api_test.go#TestAdminCapabilityRevokeRefusesAdministrative"
        status: pass
      - kind: unit
        ref: "internal/agui/capability_api_test.go#TestAdminCapabilityGrantAllowsUserSet"
        status: pass
    human_judgment: false
  - id: D2
    description: "The last administrative identity cannot remove or deactivate itself through the CLI, the cron grace-window sweep, or the future HTTP route — one refusal predicate lives in the saga, and a headless caller (no context principal) is never mistaken for the subject."
    requirement: "RBAC-07"
    verification:
      - kind: unit
        ref: "internal/identity/capability_policy_test.go#TestCanRemoveIdentity_RefusesLastAdminSelf"
        status: pass
      - kind: unit
        ref: "internal/agui/deprovision_last_admin_test.go#TestLastAdminCannotRemoveSelf"
        status: pass
      - kind: unit
        ref: "internal/agui/deprovision_last_admin_test.go#TestLastAdminSelfRemovalIsIdempotentlyRefused"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every identity provisioned through the onboarding saga is granted exactly identity.UserSet() regardless of what the request's Capabilities field asked for; a request naming an administrative capability is refused with a named error rather than silently narrowed."
    requirement: "RBAC-03"
    verification:
      - kind: unit
        ref: "internal/agui/onboarding_provision_test.go#TestNoEscalation"
        status: pass
      - kind: integration
        ref: "internal/agui/onboarding_provision_grants_test.go#TestProvisionGrantsUniformCapabilitySet (db_integration)"
        status: unknown
      - kind: integration
        ref: "internal/agui/onboarding_provision_grants_test.go#TestProvisionRefusesAdministrativeRequest (db_integration)"
        status: unknown
      - kind: integration
        ref: "internal/agui/onboarding_provision_grants_test.go#TestProvisionGrantStepIsIdempotent (db_integration)"
        status: unknown
    human_judgment: true
    rationale: "This sandboxed session has no POSTGRES_PASSWORD / live-Postgres access (same constraint the predecessor plan 02-01's SUMMARY disclosed). All three db_integration tests were written against the SAME live-saga harness onboarding_provision_integration_test.go already uses, and `go vet -tags db_integration ./internal/agui/...` is clean, but none has been executed against a real database. A human with .env access must run: `go test -tags db_integration -race -count=1 -p 1 ./internal/agui/ -run 'TestProvisionGrantsUniformCapabilitySet|TestProvisionRefusesAdministrativeRequest|TestProvisionGrantStepIsIdempotent' -v`."
  - id: D4
    description: "A second capability-name declaration anywhere in cmd/ or internal/ (outside internal/identity/capabilities.go) fails the build; an emptied declaration file also fails rather than passing vacuously."
    requirement: "RBAC-02"
    verification:
      - kind: other
        ref: "bash scripts/check_capability_declaration.sh — run on the clean tree (PASS), on a tree with a planted internal/agui/zz_capability_probe.go declaring \"agent.run\" (FAIL, file:line printed, then deleted), and on a tree with capabilities.go temporarily emptied to its package comment (FAIL, then restored from backup)"
        status: pass
      - kind: other
        ref: ".github/workflows/ci.yml step 'Capability declaration gate (RBAC-02)'; Makefile capability-declaration target wired into `quality`; verified via `make capability-declaration` in WSL"
        status: pass
    human_judgment: false
  - id: D5
    description: "The authorization policy denies by default: an empty capability name, an empty identity id, and an undeclared/malformed name each refuse rather than pass silently."
    requirement: "RBAC-09"
    verification:
      - kind: unit
        ref: "internal/identity/capability_policy_test.go#TestCanGrantThroughAPI_EmptyAndUnknown"
        status: pass
      - kind: unit
        ref: "internal/identity/capability_policy_test.go#TestCanRemoveIdentity_EmptyIdsRefuse"
        status: pass
    human_judgment: false

duration: not separately timestamped — this continuation resumed a plan whose Task 1 a prior process had already completed and committed before crashing mid-Task-2; Task 2's commit (95a6acda9) and Task 3's commit (b5a669d07) landed at 2026-09-09T13:53:36+02:00 and 2026-09-09T13:54:10+02:00
completed: 2026-09-09
status: complete
---

# Phase 2 Plan 2: Two Roles and a Budget — the authorization contract Summary

**No path exists from a non-administrative identity to an administrative capability: the capabilities API, the deprovision saga, and the onboarding grant step each refuse by construction rather than by convention, and a CI check keeps the capability declaration in exactly one file.**

## Performance

- **Duration:** not separately timestamped for this continuation (see frontmatter `duration`)
- **Tasks:** 3 total (Task 1 committed by a prior process before this continuation began; Task 2 and Task 3 completed and committed here)
- **Files modified this continuation:** 13 (Task 2 commit) + 3 (Task 3 commit) = 16, with `.github/workflows/ci.yml` and `Makefile` touched once each across those counts

## Accomplishments

- **Task 1 (inherited, already committed) — one pure file decides every refusal.** `internal/identity/capability_policy.go` holds `CanGrantThroughAPI`/`CanRevokeThroughAPI`/`CanRemoveIdentity`/`CanDeactivateIdentity`, zero I/O, each refusal a distinct sentinel (`ErrCapabilityNotGrantable`, `ErrCapabilityNotDeclared`, `ErrLastAdministrator`, `ErrEmptyIdentityID`) asserted with `errors.Is` throughout. Verified unchanged and still green in this continuation.
- **Task 2 — the guards wired at the API, inside the saga, and at the provisioning grant.** `handleGrantCapability`/`handleRevokeCapability` (`internal/agui/audit_api.go`) call the Task 1 predicates before any store call; every sentinel maps to 403. `Deactivate`/`Purge` (`internal/agui/deprovision.go`) call `CanDeactivateIdentity`/`CanRemoveIdentity` before the first journal write, covering the CLI and the cron grace-window sweep with one check via a headless-caller sentinel that can never equal a real identity id. `onboarding_provision.go`'s saga now grants `identity.UserSet()` unconditionally (the split-out `onboarding_provision_grants.go` holds the creator pre-check + the administrative-name refusal); `onboarding_provision.go` measures at 534 lines (down from 568, still under the 600 cap) and `onboarding_provision_grants.go` at 63. `cmd/aura/serve_provisioning.go`'s `ListPurgeable`/`ResolveTarget` now project `IsAdministrative` as an `EXISTS` over `capability_grants` bound from `identity.Administrative()` — never a literal — which is what the saga's guard needs to tell a real last-administrator from an ordinary identity.
- **Task 3 — a CI check that keeps the declaration in one place.** `scripts/check_capability_declaration.sh` derives its name list from `internal/identity/capabilities.go`'s own `const Cap... = "..."` lines (never hard-coded), fails on any second quoted declaration in a non-test Go file under `cmd/`/`internal/`, and fails when the declaration file itself is emptied rather than passing vacuously. Both failure modes were proven by actually creating the violating fixture, observing the script fail and name the offending file, then deleting/restoring it — not by reading the script. Wired into `.github/workflows/ci.yml` beside `check_ci_go_packages.sh` and into the `Makefile`'s `quality` target via a new `capability-declaration` phony target.
- **TestNoEscalation retired and rewritten, with justification.** The pre-Phase-2 subset-of-creator-grants contract it pinned is exactly what D-01/RBAC-03 retires; the rewrite keeps every no-write assertion, adds the missing administrative-refusal coverage, and proves the previously-rejected inputs (`'*'`, undeclared names, malformed grammar) are now correctly ignored in favor of the uniform grant. Two more pre-existing tests (a 400-vs-403 assumption that Task 2a's own instruction retired) were updated the same way, made stricter rather than weaker.

## Task Commits

1. **Task 1 RED — failing test for last-administrator self-removal refusal** - `ae53dc47e` (test) — *inherited, committed by the prior process before this continuation*
2. **Task 1 GREEN — decide every refusal this phase adds in one pure file** - `588921442` (feat) — *inherited, committed by the prior process before this continuation*
3. *(foreign, not this plan)* `7ea283d8c` `feat(docs): give the documents MCP a host arm for opening a file` — landed on `master` between this continuation's commits from a concurrent session sharing the same working tree (`workflow.use_worktrees: false`); listed here only for git-log continuity, not because this plan authored it.
4. **Task 2 — the guards wired at the API, saga, and provisioning grant** - `95a6acda9` (feat)
5. **Task 3 — a CI check keeping the capability declaration in one place** - `b5a669d07` (feat)

**Plan metadata:** *(this commit, immediately following)*

## Files Created/Modified

- `internal/identity/capability_policy.go` / `capability_policy_test.go` - the single refusal-decision file (Task 1, inherited)
- `internal/agui/audit_api.go` - `checkCapabilityAPIPolicy` gates grant/revoke before any store call
- `internal/agui/capability_api_test.go` - new: proves the API guard refuses administrative names for every caller, store never reached
- `internal/agui/deprovision.go` - `CanDeactivateIdentity`/`CanRemoveIdentity` gate Deactivate/Purge; `headlessDeprovisionCaller` sentinel for the CLI/cron
- `internal/agui/deprovision_last_admin_test.go` - new: proves last-admin self-removal is refused via Deactivate and Purge, idempotently, without disturbing the headless sweep
- `internal/agui/onboarding_provision.go` - grants `identity.UserSet()` unconditionally; 534 lines (was 568)
- `internal/agui/onboarding_provision_grants.go` - new: the creator pre-check + administrative-name refusal, split out to stay under the 600-LOC cap
- `internal/agui/onboarding_provision_grants_test.go` - new (db_integration): proves the uniform grant, the administrative refusal, and grant-step idempotency against a live Postgres
- `internal/agui/onboarding_provision_fakes_test.go` - `fakeAuraLeg` now captures the capabilities the saga actually passed to `CreateIdentityWithGrants`
- `internal/agui/onboarding_provision_test.go` - `TestNoEscalation` rewritten for the new contract (see Deviations)
- `internal/agui/audit_api_test.go`, `internal/agui/audit_api_branches_test.go` - two pre-existing tests updated from 400 to 403 to match Task 2a's "map every sentinel to 403" instruction
- `internal/agui/onboarding_api.go` - doc-comment update on `OnboardingProvisionRequest.Capabilities`, naming plan 02-08 as the removal point
- `cmd/aura/serve_provisioning.go` - `ListPurgeable`/`ResolveTarget` project `IsAdministrative` via an `EXISTS` bound from `identity.Administrative()`
- `scripts/check_capability_declaration.sh` - new: the RBAC-02 declaration-uniqueness CI check
- `.github/workflows/ci.yml`, `Makefile` - wire the new check into CI and `make quality`

## Decisions Made

See `key-decisions` in frontmatter. The four most consequential: (1) `TestNoEscalation` was rewritten, not left red, with the retirement justified inline and every no-write assertion kept; (2) two more pre-existing tests were updated from 400 to 403 for the same reason, made stricter not weaker; (3) `serve_provisioning.go` and `onboarding_api.go` were edited despite not appearing in the plan's `files_modified` frontmatter, because both edits are literal instructions in Task 2's action text; (4) TDD RED→GREEN ordering could not be honored for Task 2 — the tests were written against an already-existing implementation, and every new refusal assertion was independently verified by temporarily removing the guard it tests and confirming the test failed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `TestNoEscalation` pinned a contract this plan retires — rewritten with justification**
- **Found during:** Task 2 verification (running the plan's own `<verify>` command for Task 2 first surfaced this as a pre-existing red test, per the measured starting state)
- **Issue:** `internal/agui/onboarding_provision_test.go`'s `TestNoEscalation` asserted the pre-Phase-2 subset-of-creator-grants contract (a request's capabilities validated against the creator's own grants; `'*'`/undeclared/malformed names all rejected). D-01/RBAC-03 and the plan's own Task 2 action text retire that contract by name ("this is no longer a subset check, it never was here").
- **Fix:** Rewrote the test to assert the new contract: an administrative name in the request is refused (kept the no-write assertion), the creator-must-hold-identity.create check is unchanged (kept as-is), and every other requested value — wildcard, undeclared, malformed grammar, a mix, or an empty list — is proven to be IGNORED, with the actual granted set asserted equal to `identity.UserSet()` via a new capture field on `fakeAuraLeg`.
- **Files modified:** `internal/agui/onboarding_provision_test.go`, `internal/agui/onboarding_provision_fakes_test.go`
- **Verification:** `go test -count=1 -v ./internal/agui/ -run TestNoEscalation` — all subtests pass.
- **Committed in:** `95a6acda9` (Task 2 commit)

**2. [Rule 1 - Bug] Two pre-existing tests expected 400 for a rejection the new guard now issues as 403 pre-store**
- **Found during:** Task 2 verification, running the full `internal/agui` suite after adding the new tests
- **Issue:** `TestMutateCapabilityGuards`'s wildcard subtest (`audit_api_branches_test.go`) and `TestGrantCapabilityRejectsInvalidName` (`audit_api_test.go`) set a fake store error (`ErrWildcardManaged`/`ErrInvalidCapability`) and expected the handler to reach the store and map it to 400. Task 2a's guard now runs `identity.CanGrantThroughAPI` BEFORE any store call, and that guard's `capabilityAPIPolicy` calls `ValidateCapabilityName` first — so both inputs are now refused at 403 before the store is ever touched, per Task 2a's own instruction to "map every sentinel to 403".
- **Fix:** Updated both tests to assert 403 and, going further than the originals, that the store's `GrantCapability` is never called for either input — a strictly stronger assertion, not a weakened one. Both changes are documented inline with the retirement reasoning.
- **Files modified:** `internal/agui/audit_api_branches_test.go`, `internal/agui/audit_api_test.go`
- **Verification:** `go test -count=1 ./internal/identity/ ./internal/agui/` — both packages green; re-verified under `-race` in WSL.
- **Committed in:** `95a6acda9` (Task 2 commit)

**3. [Rule 2 - Missing critical functionality, plan-directed] `cmd/aura/serve_provisioning.go` and `internal/agui/onboarding_api.go` edited despite not being in the plan's `files_modified` frontmatter**
- **Found during:** reading Task 2's action text before starting (per the mandatory read_first gate)
- **Issue:** The plan's top-level `files_modified` list for this plan omits both files, but Task 2's action text explicitly requires both edits: 2b says `DeprovisionTarget` needs `IsAdministrative` sourced through `IdentityDeactivator.ResolveTarget` (implemented in `serve_provisioning.go`, the composition-root adapter — there is no other place this lookup can live without `deprovision.go` reaching into a concrete store, which its own header forbids); 2d says to update the `OnboardingProvisionRequest` doc comment naming plan 02-08 as the field's removal point.
- **Fix:** Made both edits as instructed. No functional surprise — the prior (crashed) executor process had already written both edits; this continuation verified they satisfy the plan's own text rather than assuming so.
- **Files modified:** `cmd/aura/serve_provisioning.go`, `internal/agui/onboarding_api.go`
- **Verification:** `go build ./...`, `go vet ./...` clean; the `IsAdministrative` projection is exercised transitively by `deprovision_last_admin_test.go`'s use of `fakeDeactivator.targets[...]` (a substitute for the real adapter, so this specific SQL is not itself unit-tested — see Issues Encountered).
- **Committed in:** `95a6acda9` (Task 2 commit)

---

**Total deviations:** 3 auto-fixed (2 bug-fixes to pre-existing tests pinning a retired contract, 1 plan-directed-but-frontmatter-omitted file edit). **Impact:** all three were necessary to make the plan's own stated contract hold and to keep the repo's test suite honest; none represents scope creep — each is traceable to the plan's own Task 2 action text or to D-01/RBAC-03's explicit retirement of the prior contract.

## Issues Encountered

- **TDD RED→GREEN ordering could not be honored for Task 2.** The prior executor process wrote Task 2's full implementation (guards wired, saga split, grant semantics changed) before crashing on two API 500 errors, leaving none of the task's tests written. This continuation wrote the tests against the already-existing implementation — a GREEN-only history for this task, not a fabricated RED commit, as the measured starting state explicitly instructed. To compensate for the missing RED phase, every new refusal assertion touching `deprovision.go` was independently verified to be load-bearing: the two guard calls (`identity.CanDeactivateIdentity`/`CanRemoveIdentity`) were temporarily removed with a Python script, the corresponding new tests (`TestLastAdminCannotRemoveSelf`'s two refusal subtests, `TestLastAdminSelfRemovalIsIdempotentlyRefused`) were confirmed to fail while the unrelated subtests in the same run stayed green, and the file was then restored byte-identical from a backup (`git diff --stat` confirmed zero diff afterward) and re-verified building and passing clean. `capability_api_test.go`'s three new tests exercise the already-independently-tested Task 1 predicates through the transport layer and did not need the same flip-check.
- **The `db_integration`-tagged tests in `onboarding_provision_grants_test.go` were not executed against a live Postgres.** This sandboxed session has no `POSTGRES_PASSWORD` / live-database access, matching the constraint plan 02-01's own SUMMARY disclosed. `go vet -tags db_integration ./internal/agui/...` is clean and the three new tests reuse the exact same live-saga harness (`onboarding_provision_integration_harness_test.go`) that `onboarding_provision_integration_test.go`'s already-established live tests use, but none has been run. A human with `.env` access must run:
  ```
  go test -tags db_integration -race -count=1 -p 1 ./internal/agui/ -run 'TestProvisionGrantsUniformCapabilitySet|TestProvisionRefusesAdministrativeRequest|TestProvisionGrantStepIsIdempotent' -v
  ```
- **A concurrent session's in-flight, uncommitted edit to `internal/multimodal` (unrelated to this plan) transiently broke `go build ./...`/`go vet ./...` repo-wide multiple times during this continuation** (three different error signatures observed within a few minutes: an argument-count mismatch, an undefined symbol, then an undefined struct field — clear evidence of active concurrent editing, not a stable state). This is entirely outside plan 02-02's scope (`internal/multimodal`, `cmd/aura-media-index`, `cmd/aura/document_processor_wiring.go`, none of which this plan touches or has any instruction to touch) and was never edited by this continuation. By the time of the final verification pass the concurrent session's production-code refactor had stabilized (`go build ./...` clean); its own test files (`cmd/aura-media-index/main_test.go`) were still catching up to the new signature as of the last check, which is that session's work to finish, not this plan's. Every command scoped to this plan's own packages — `go vet ./internal/identity/... ./internal/agui/...`, `go build ./internal/identity/... ./internal/agui/... ./cmd/aura/...`, `go test -count=1 ./internal/identity/ ./internal/agui/`, and the full `-race` runs in WSL — was independently clean throughout, both before and after the transient repo-wide breakage.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Task 1's authorization predicates, Task 2's wiring at the API/saga/provisioning-grant boundaries, and Task 3's CI declaration gate are all committed and self-verified: `go build ./...` and `go vet ./internal/identity/... ./internal/agui/...` clean, `go test -count=1 ./internal/identity/ ./internal/agui/` green, `-race` clean via WSL for both packages (including the plan's own named `<verify>` test subsets for Task 1 and Task 2), `bash scripts/check-file-size.sh` clean with `onboarding_provision.go` at 534 lines (below both the 600 cap and its pre-split 568), and `bash scripts/check_capability_declaration.sh` proven both ways by actually creating and removing violating fixtures rather than by reading the script.
- **Blocker for `/gsd-verify-work`:** the three `db_integration`-tagged tests in `onboarding_provision_grants_test.go` (D3 above) need to be run by someone with `.env`/live-Postgres access before RBAC-03's uniform-grant and administrative-refusal claims are closed with live evidence rather than compile-cleanliness + unit-tier proof alone.
- The `capability_policy` `GO_SCOPES` mutation-testing entry (RBAC-06/RBAC-07/RBAC-09 branches) is deferred to plan `02-10` per this plan's own `<rel_06_mutation_scope_decision>` — the target file (`internal/identity/capability_policy.go`) exists and is single-file-mutable; the `scripts/critical_mutation_gate.py` edit and the mutation run itself are not this plan's work.
- Migration numbers are unchanged by this plan (no migration was needed or added); the next plan in this phase must still re-measure `ls internal/db/migrations/ | tail -1` per CLAUDE.md rather than assuming `0122` is still the tail.

## Self-Check: PASSED

- All 7 new/created key files confirmed present on disk (`[ -f ]`): `internal/identity/capability_policy.go`, `internal/identity/capability_policy_test.go`, `internal/agui/capability_api_test.go`, `internal/agui/deprovision_last_admin_test.go`, `internal/agui/onboarding_provision_grants.go`, `internal/agui/onboarding_provision_grants_test.go`, `scripts/check_capability_declaration.sh`.
- All 4 referenced commit hashes confirmed in `git log --oneline --all`: `ae53dc47e`, `588921442`, `95a6acda9`, `b5a669d07`.
- Re-ran plan acceptance-criteria greps: `audit_api.go` contains `identity.CanGrantThroughAPI(` and `identity.CanRevokeThroughAPI(` with no inline administrative-name comparison (only in a doc comment); `deprovision.go` contains `identity.CanRemoveIdentity(` and `identity.CanDeactivateIdentity(`; `onboarding_provision_grants.go` contains `identity.UserSet()`; `wc -l internal/agui/onboarding_provision.go` = 534 (below 600 and below the pre-split 568).
- `go build ./...`, `go vet ./internal/identity/... ./internal/agui/...`, `bash scripts/check-file-size.sh`, `bash scripts/check_capability_declaration.sh` all clean on current HEAD; `go test -count=1 ./internal/identity/ ./internal/agui/` and the `-race` variants (WSL) all green, including every test name the plan's own `<verify>` blocks for Task 1 and Task 2 name explicitly.

---
*Phase: 02-two-roles-and-a-budget*
*Completed: 2026-09-09*
