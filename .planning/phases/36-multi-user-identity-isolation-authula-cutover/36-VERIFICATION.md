---
phase: 36-multi-user-identity-isolation-authula-cutover
verified: 2026-07-06T15:02:33Z
status: passed
score: "4/4 ROADMAP success criteria VERIFIED; 6/6 MUSR-01..06 requirements VERIFIED; all 7 prior gaps CLOSED; both human_verification items RESOLVED"
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: "2/4 ROADMAP SC VERIFIED, 1 UNCERTAIN, 1 FAILED/PARTIAL; 4/6 MUSR VERIFIED, 2 PARTIAL"
  gaps_closed:
    - "VERIF-1: migrate_0026 reversibility test is version-targeted (stepDownToV26 := 26 - head); db_integration + Knowledge CI jobs green"
    - "VERIF-2: back half of phase pushed; CI run 28799334452 on HEAD 207200c8 is 20/20 GREEN incl. the live -race MUSR two-identity E2E (268s); git log origin/master..HEAD empty"
    - "VERIF-3: provisioning wired at serve boot (buildProvisioningPorts -> deps.ObjectStore/Filesystem/Journal), migration 0033 widens scheduler kind CHECK to admit identity_purge, dispatch-map entry cron.KindIdentityPurge, seedIdentityPurgeSweep at boot, deactivation auth-gate in RequireAuth"
    - "VERIF-4: assets route through objectstore.IdentityStore via resolveObjects; buildAssetService wires a real *IdentityStore resolver into service + all processors (serve.go:292)"
    - "CR-01/VERIF-5: gateMUSRIsolation emits Fatal under server_production when off; Provision returns errIsolationDisabled before any write when off; document_search threads ownerFromContext; rollout ACTIVE (flag=true in .env + running container)"
    - "VERIF-6: check-no-url-tokens.sh wired as a blocking CI step (ci.yml:72-73); Build+vet+lint job green"
    - "VERIF-7: ShellPoll/ShellKill .Caps wired at serve boot (serve.go:279-283); Telegram scopeTurnToIdentity fail-closed; blank-principal RequireAuth guard (auth.go:287) + TestRequireAuthRejectsBlankPrincipal"
  human_verification_resolved:
    - "HV#1 (live-stack + -race matrix): CI run 28799334452 ran the full live Postgres+Neo4j+Garage+Authula matrix green on Linux, incl. -race Unit and the 268s musr-e2e two-identity cross-deny E2E"
    - "HV#2 (AURA_MUSR_ISOLATION rollout): activate-now decided + executed — `aura documents backfill` 0-doc no-op (D-12 satisfied), then AURA_MUSR_ISOLATION=true set in .env (line 181) and confirmed live in the running aura container"
  gaps_remaining: []
  regressions: []
gaps: []
human_verification: []
---

# Phase 36: Multi-User Identity Isolation + Authula Cutover — Verification Report (RE-VERIFICATION)

**Phase Goal:** Multi-User Identity Isolation + Authula Cutover — two identities run end-to-end with NO cross-identity leak across conversations, approvals, documents (Neo4j), object-store (Garage), background jobs, MCP/skills/pyscripts roots, and Telegram routing; Authula cutover with break-glass + provisioning; capability-per-route; no long-lived token in URLs.
**Verified:** 2026-07-06T15:02:33Z
**Status:** passed (phase complete — all gaps closed)
**Re-verification:** Yes — post-gap-closure re-verification against green CI run 28799334452 (HEAD `207200c8`)

## Re-Verification Summary

This is the **post-gap-closure re-verification**. The initial verification (2026-07-06T02:54:02Z) returned `gaps_found` with 7 gaps + 2 human_verification items. Gap-closure plans 36-13..36-18 were then executed. This re-verification independently re-checks every gap against the **actual codebase at HEAD `207200c8`** and the **live CI history** — it does NOT trust the 36-13..36-18 SUMMARY.md files.

**Key independent findings this pass:**

- **Everything is pushed.** `git rev-parse HEAD` == `git rev-parse origin/master` == `207200c8`; `git log origin/master..HEAD` is **empty**. This closes the initial verification's most serious finding (VERIF-2: 7 of 12 plans never pushed / never CI-run).
- **CI run 28799334452 (workflow "CI", headSha `207200c8`) is 20/20 GREEN**, conclusion `success`. The parallel **CodeQL** (28799335150) and **Skills** (28799334742) workflows are also `success` on the identical HEAD, and these three are the **latest** runs on master (the immediately-prior `244ddcd2` CI run failed and was fixed by the final commit `207200c8`).
- **The MUSR two-identity cross-deny E2E genuinely RAN live under -race** — not skip-as-green. The job step "Two-identity cross-deny E2E (flag-on enforcement, full live stack)" invoked `go test -race -count=1 -p 1 -tags 'db_integration neo4j_integration garage_integration authula_integration musr_e2e' -run 'TestTwoIdentityCrossDeny|TestProvisionLoginIsolatedRun'` from 14:36:20 to 14:40:48 (~268s wall-clock, `ok cmd/aura 5.995s` test runtime — not sub-second). The harness helper `musrEnvOrSkip` **t.Fatals under `$CI`** when a required var is unset, and the CI env block populated all of them (incl. `AURA_MUSR_ISOLATION=true`), so a skip would have failed the job. No-skip-as-green satisfied.
- **The AURA_MUSR_ISOLATION rollout is ACTIVE**: `.env` line 181 is `AURA_MUSR_ISOLATION=true` and the running `aura` container reports `AURA_MUSR_ISOLATION=true`.

**Verdict:** all 7 gaps are independently confirmed CLOSED in code + CI; both human_verification items are RESOLVED; all 4 ROADMAP success criteria and all 6 MUSR requirements are now VERIFIED. **Phase 36 is complete.**

## Gap Closure Verification (the 7 prior gaps)

| # | Gap (prior) | Prior status | Now | Independent evidence at HEAD `207200c8` |
|---|---|---|---|---|
| VERIF-1 | migrate_0026 test used relative `MigrateSteps(-1)/(+1)` — reversed the LATEST migration, not 0026 | failed | ✓ CLOSED | `internal/db/migrate_0026_integration_test.go:99` now computes `stepDownToV26 := 26 - head` and positions DOWN to exactly v26 (line 100) BEFORE the ±1 straddle (lines 105/115), then restores to HEAD (line 125) and asserts a no-op re-migrate (line 134). Version-targeted, immune to migration count. CI witness: run 28799334452 **Integration tests (db_integration)** + **Knowledge integration + smoke** jobs both green (both run the migration suite live under -race). |
| VERIF-2 | back half (36-05/06/08..12 + 36-13..18) never pushed / never CI-run | failed | ✓ CLOSED | `git log origin/master..HEAD` empty; HEAD==origin/master==`207200c8`. CI run **28799334452 = 20/20 jobs `success`** on this exact SHA, incl. the five-tag `musr-e2e` job (ran live 268s under -race, verified above). CodeQL + Skills workflows also green on the same SHA; these are the latest runs. |
| VERIF-3 | provisioning dormant — nil ObjectStore/Filesystem/Journal, no purge dispatch entry, no kind-CHECK widen | failed | ✓ CLOSED | `cmd/aura/serve_onboarding.go:261-264` sets `deps.ObjectStore/Filesystem/Journal` from `buildProvisioningPorts(chat)` (non-nil when Garage admin cfg + pool present); `cmd/aura/serve_provisioning.go` supplies `newObjectStoreProvisionAdapter`/`newFilesystemProvisionAdapter`; `cmd/aura/serve_dispatch.go:57` registers `cron.KindIdentityPurge: handlers.IdentityPurgeHandler{...}`; `cmd/aura/serve.go:314` `seedIdentityPurgeSweep`; migration `0033_scheduler_identity_purge_kind.up.sql` widens the CHECK to admit `'identity_purge'` (+ symmetric down); deactivation gate `internal/agui/auth.go:229` denies a soft-deleted principal at RequireAuth. The live `TestProvisionLoginIsolatedRun` (in the green musr-e2e job) exercises the provisioning path end-to-end. |
| VERIF-4 | `objectstore.IdentityStore.Resolve` had zero non-test consumers | failed | ✓ CLOSED | `internal/assets/object_resolver.go` adds `ObjectResolver`/`resolveObjects`/`ObjectResolverBundle`; `internal/assets/service.go:59` calls `resolveObjects(...)`; `audio/document/image_processor.go` each carry `PerIdentityObjects *ObjectResolverBundle`; `cmd/aura/document_processor_wiring.go:76` constructs a real `objectstore.NewIdentityStore(...)` and threads it into the service + all processors; wired at boot in `cmd/aura/serve.go:292` (`buildAssetService`). |
| CR-01/VERIF-5 | documents-plane isolation default-off, not coupled | partial (Critical) | ✓ CLOSED | `internal/config/config_validate.go:241` `gateMUSRIsolation` returns a `Fatal` under `ProfileServerProduction` when the flag is off (wired at line 97); `internal/agui/onboarding_provision.go:160-161` returns `errIsolationDisabled` BEFORE any cross-store write when off (provisioning refuses to arm the leak); `internal/agent/tools/document_search.go:91` threads `ownerFromContext(ctx)`. Operationally closed too: flag = `true` in `.env` + live container. |
| VERIF-6 | no-URL-token static gate not CI-wired | partial | ✓ CLOSED | `.github/workflows/ci.yml:72-73` — blocking step "No long-lived token in URLs (MUSR-06)" → `bash scripts/check-no-url-tokens.sh`, inside the Build+vet+lint job. CI run 28799334452 that job (step #11) green. |
| VERIF-7 | shell admin-cap inert + Telegram not fail-closed + no blank-principal regression | partial | ✓ CLOSED | `cmd/aura/serve.go:279-283` wires `ShellPoll.Caps`/`ShellKill.Caps = chat.identity` at serve boot (admin escape-hatch now reachable; `shell_bg_owner_test.go:139-153` proves admin path consulted + foreign caller still denied). `internal/channels/telegram/bot_dispatch_turn.go:134` `scopeTurnToIdentity` FAILS CLOSED (nil resolver / account miss → `(ctx,false)` → `startTurn` drops the turn at line 88). `internal/agui/auth.go:287` blank-principal guard + `internal/agui/auth_blank_principal_test.go` `TestRequireAuthRejectsBlankPrincipal` (LO-02). |

## Human Verification Items (both RESOLVED)

| # | Item (prior) | Resolution |
|---|---|---|
| HV#1 | Live-stack + -race matrix must run in WSL/CI | ✓ RESOLVED — CI run 28799334452 ran the full live Postgres+Neo4j+Garage+Authula matrix green on Linux: **Unit tests (race detector)**, **Integration (db_integration)**, **Knowledge integration + smoke**, and the **MUSR two-identity cross-deny E2E** (268s, -race, flag-on) all `success`. |
| HV#2 | Decide + record AURA_MUSR_ISOLATION rollout timing | ✓ RESOLVED — activate-now. `aura documents backfill` returned a 0-doc no-op (`owners_sourced:0, orphans_attached_to_operator:0, edges_from_map:0` — D-12 satisfied), then `AURA_MUSR_ISOLATION=true` set in `.env:181` and confirmed live in the running `aura` container. Recorded in 36-18-SUMMARY.md. |

## Goal Achievement

### ROADMAP Success Criteria

| # | Truth | Prior | Now | Evidence |
|---|---|---|---|---|
| 1 | Two-identity live E2E — B cannot list/get/delete/archive/resolve A's data (404/403); a B-created chat is owned by B and runs | ? UNCERTAIN | ✓ VERIFIED | The `musr-e2e` CI job ran `TestTwoIdentityCrossDeny` + `TestProvisionLoginIsolatedRun` live under -race against the full Postgres+Neo4j+Garage+Authula stack with `AURA_MUSR_ISOLATION=true` and passed (green, 268s, genuinely executed — not skip). Underlying owner-scoped stores/RLS/`GetForIdentity` owner-gate were already unit-VERIFIED. |
| 2 | Session B cannot poll/kill session A's shell; jobs expire by TTL | ✓ VERIFIED | ✓ VERIFIED | Unchanged; owner-binding + TTL reaper unit-tested and green under -race in CI. Admin escape-hatch now additionally wired (VERIF-7) without weakening the deny-by-default core. |
| 3 | Conversation delete evicts all session tool state | ✓ VERIFIED | ✓ VERIFIED | Unchanged; `DeleteConversationLifecycle` single choke point, unit-tested, green in CI -race. |
| 4 | Authula is the default with provisioning + break-glass; no token in URLs | ✗ FAILED/PARTIAL | ✓ VERIFIED | Provisioning now actually wired at the serve composition root (VERIF-3) and exercised live by `TestProvisionLoginIsolatedRun`; break-glass `aura identity recover` VERIFIED; no-URL-token gate now a blocking CI step (VERIF-6); capability-per-route intact. |

**Score:** 4/4 ROADMAP success criteria VERIFIED.

### Requirements Coverage (MUSR-01..06)

| Requirement | Prior | Now | Evidence |
|---|---|---|---|
| MUSR-01 (owner-scoped all planes) | ⚠ PARTIAL | ✓ VERIFIED | Postgres plane (RLS + `*ForIdentity` + D-06 404/403) active; documents plane flag ON (live) + config-validate Fatal gate; object-store plane consumed at the asset path (VERIF-4) + provisioned at boot (VERIF-3); filesystem/MCP rooting reached via the now-wired provisioning legs. Live two-identity cross-deny E2E green. |
| MUSR-02 (new conv owned by principal) | ✓ VERIFIED | ✓ VERIFIED | `defaultConversationOwner` + `TestNewConversationOwnedByPrincipal`; E2E `musr02` subtest ran live. |
| MUSR-03 (owner-bound job IDs; foreign deny unless admin cap) | ✓ VERIFIED (core) | ✓ VERIFIED | Deny-by-default core green; the admin-cap exemption is now reachable (`ShellPoll/ShellKill.Caps` wired at boot). |
| MUSR-04 (1h TTL; expiry terminates + records; age metric) | ✓ VERIFIED | ✓ VERIFIED | Unchanged; TTL/reaper/age tests green. |
| MUSR-05 (single delete lifecycle) | ✓ VERIFIED | ✓ VERIFIED | Unchanged. |
| MUSR-06 (Authula default + provisioning + break-glass; cap-per-route; no token in URLs) | ⚠ PARTIAL | ✓ VERIFIED | Provisioning wired (VERIF-3); no-URL-token gate CI-wired (VERIF-6); all other clauses already VERIFIED. |

No orphaned requirements: the union of all plan `requirements:` frontmatter covers MUSR-01..06 with no unclaimed IDs. The `QUAL`(Authula DSN test) item was already closed in Phase 32 (32-09), not a Phase-36 gap.

### Key Link Verification (previously-broken links now WIRED)

| From | To | Via | Prior | Now |
|---|---|---|---|---|
| `cmd/aura/serve_onboarding.go` OnboardingDeps | `agui.ObjectStoreProvisioner`/`FilesystemProvisioner`/`SagaJournal` | `buildProvisioningPorts(chat)` field assignment | ✗ NOT WIRED | ✓ WIRED (serve_onboarding.go:261-264) |
| `internal/assets/service.go` | `objectstore.IdentityStore.Resolve` | `resolveObjects` seam | ✗ NOT WIRED | ✓ WIRED (service.go:59 + document_processor_wiring.go:76) |
| `cmd/aura/serve_dispatch.go` real map | `handlers.IdentityPurgeHandler` | `cron.KindIdentityPurge` entry | ✗ NOT WIRED | ✓ WIRED (serve_dispatch.go:57) |
| `internal/db/migrations` `kind` CHECK | `'identity_purge'` literal | migration 0033 | ✗ NOT WIRED | ✓ WIRED (0033_scheduler_identity_purge_kind.up.sql) |
| `scripts/check-no-url-tokens.sh` | `.github/workflows/ci.yml` | CI step | ✗ NOT WIRED | ✓ WIRED (ci.yml:72-73) |
| `cmd/aura` `ShellPoll{}`/`ShellKill{}` | identity/capability store | `.Caps` field | ✗ NOT WIRED (fail-closed) | ✓ WIRED (serve.go:279-283) |

### Behavioral / CI Evidence

| Behavior | Evidence | Status |
|---|---|---|
| Everything pushed | `git log origin/master..HEAD` empty; HEAD==origin/master==`207200c8` | ✓ PASS |
| CI (main) green on HEAD | run 28799334452 = 20/20 jobs `success` | ✓ PASS |
| CodeQL green on HEAD | run 28799335150 `success` | ✓ PASS |
| Skills green on HEAD | run 28799334742 `success` | ✓ PASS |
| Live two-identity E2E under -race | musr-e2e job step #13, `go test -race ... -run 'TestTwoIdentityCrossDeny\|TestProvisionLoginIsolatedRun'`, 268s wall, `ok cmd/aura 5.995s` | ✓ PASS (genuinely ran) |
| No-skip-as-green | `musrEnvOrSkip` t.Fatals under `$CI` if env unset; CI env fully populated incl. flag=true; job green | ✓ PASS |
| migrate_0026 reversibility | version-targeted test green in db_integration + Knowledge jobs | ✓ PASS |
| No-URL-token gate | ci.yml:72-73 blocking step green (build-and-lint step #11) | ✓ PASS |
| Coverage floor ≥85% | Knowledge job step #12 "Coverage gate (owned surface >= 85%, CLAUDE.md floor)" `success`; re-measured 85.9% (commit 6ea939ab) | ✓ PASS |
| MUSR flag live | `.env:181` = `AURA_MUSR_ISOLATION=true`; container reports `true` | ✓ PASS |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| — | — | No `TBD`/`FIXME`/`XXX` debt markers in any gap-closure production file (serve_provisioning.go, document_processor_wiring.go, object_resolver.go, config_validate.go, onboarding_provision.go, bot_dispatch_turn.go, migrate_0026_integration_test.go, auth.go) | ℹ️ Info | Clean |
| `internal/db/migrate_0026_integration_test.go` | 99 | (prior Blocker) relative-step migration test | ✓ Resolved | Now version-targeted (`stepDownToV26 := 26 - head`); green on live CI |

### Non-Blocking Architectural Note (carried, unchanged, not a gap)

The initial verification observed that the 0032 RLS policy is **permissive-on-unset, fail-closed-on-mismatch** — a universal storage-enforced backstop holds only for `WithIdentityTx`-wrapped surfaces, while runner/CLI/Telegram write paths (var unset) rely on the app-level `GetForIdentity` owner-gate first. This was explicitly assessed as **architecturally reasoned and non-blocking** (a strict fail-closed-on-unset would break the runner/CLI write paths and the D-06 403-vs-404 probe), with the migration's own comment deferring the tightening until every writer sets the var. It remains a tracked follow-up, not a Phase-36 blocker, and does not affect this re-verification's `passed` status.

### Gaps Summary

None. All 7 prior gaps are independently confirmed CLOSED in the codebase at HEAD `207200c8` and corroborated by the 20/20-green CI run 28799334452 (plus green CodeQL + Skills on the same SHA). Both human_verification items are RESOLVED (live matrix ran green; the AURA_MUSR_ISOLATION rollout is active). All 4 ROADMAP success criteria and all 6 MUSR-01..06 requirements are VERIFIED. Phase 36 is complete and ready to close.

Note: the ROADMAP still renders 36-18 as an unchecked `[ ]` line — this is a stale doc checkbox only; the 36-18 work (push + full CI green + live acceptance + rollout flip) is demonstrably done in code and CI.

---

_Verified: 2026-07-06T15:02:33Z_
_Verifier: Claude (gsd-verifier) — post-gap-closure re-verification against CI run 28799334452_
