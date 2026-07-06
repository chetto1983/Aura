---
phase: 36-multi-user-identity-isolation-authula-cutover
verified: 2026-07-06T02:54:02Z
status: gaps_found
score: "2/4 ROADMAP success criteria cleanly VERIFIED, 1 UNCERTAIN (acceptance E2E unrun/unpushed), 1 FAILED/PARTIAL (provisioning dormant in production); 4/6 MUSR requirements cleanly VERIFIED (MUSR-02/03/04/05), 2 PARTIAL/gaps_found (MUSR-01, MUSR-06)"
overrides_applied: 0
gaps:
  - truth: "internal/db's db_integration tier proves migration 0026's admin-capability seed is reversible (36-01 coverage D1)"
    status: failed
    reason: >
      TestMigration0026LocalAdminCapsRoundTrip (internal/db/migrate_0026_integration_test.go)
      calls MigrateSteps(ctx, migrateURL, -1)/(+1) expecting this to step down/up EXACTLY
      migration 0026. golang-migrate's Steps(n) is a RELATIVE single step from whatever the
      CURRENT head version is, not a targeted version. The moment ANY migration is added above
      0026 (0027-0032 already exist, added in this very phase), MigrateSteps(-1) reverses the
      CURRENT LATEST migration instead of 0026, so 0026's governance.write/identity.create/
      agent.run grants are never actually removed by the test. This is CONFIRMED — not
      speculative — by an actual GitHub Actions run (28753262579, 2026-07-05, commit
      docs(36-07)) that failed with: "capability \"governance.write\" present=true, want
      present=false (set=map[*:true agent.run:true governance.write:true identity.create:true])".
      Re-read the test at current HEAD: the identical MigrateSteps(-1)/(+1) code is still
      present and unfixed, so this will fail again on every future live run (there are now 32
      migrations, so -1 reverses 0032 owner-RLS, not 0026's seed).
    artifacts:
      - path: "internal/db/migrate_0026_integration_test.go"
        issue: "Lines 94/104 use a relative MigrateSteps(ctx, url, -1)/(+1) instead of an absolute-version step targeted at migration 26; breaks permanently once later migrations exist"
    missing:
      - "Rewrite the down/up assertions to target migration 26 specifically (compute the down-delta as currentVersion-25, or use golang-migrate's version-targeted Migrate API) so the test isolates 0026's own reversibility regardless of how many migrations now sit above it"
  - truth: "Phase 36's code is pushed and CI-validated before the phase closes (CLAUDE.md 'git push at the end of a phase... check all CI are green')"
    status: failed
    reason: >
      origin/master (f44dde45, 2026-07-05T23:11) contains only 36-01, 36-02, 36-03, 36-04 and
      36-07. The local branch is 27 commits ahead; 36-05, 36-06, 36-08, 36-09, 36-10, 36-11 and
      36-12 (7 of 12 plans — the entire object-store isolation plane, the provisioning/
      de-provisioning saga, the conversation-delete lifecycle, the admin/audit UI, the Telegram
      multi-user routing, and the two-identity acceptance E2E + its own musr-e2e CI job) have
      NEVER been pushed and have NEVER been evaluated by any GitHub Actions job (Unit, CodeQL,
      Integration, lint) — only by this verification session's local Windows build/vet/test.
      The portion that WAS pushed had 3 job failures at its last run: db_integration/Knowledge-
      integration (root-caused above, still broken at HEAD), sqlc-generate-is-in-sync (caused by
      the Wave-1 sqlc drift 36-04 itself explicitly fixed — independently re-ran `sqlc generate`
      at current HEAD and confirmed zero diff, so this one IS fixed), and an unrelated
      pre-existing "quality snapshot gate" staleness check (a Phase-10 benchmark row, not
      touched by Phase 36).
    missing:
      - "git push the remaining 27 commits to origin/master"
      - "Confirm the full CI matrix (Unit, CodeQL, Integration db_integration, Knowledge integration + smoke, sqlc-generate, Build+vet+lint+deadcode+file-size, musr-e2e) is green on the actually-pushed HEAD"
  - truth: "Admin-create eagerly provisions the Garage bucket/key + per-identity mcp/skills/pyscripts dirs, and grace-window purge runs on schedule (MUSR-01 object-store+filesystem plane / MUSR-06 'provisioning shipped')"
    status: failed
    reason: >
      cmd/aura/serve_onboarding.go's buildOnboardingService never sets OnboardingDeps.
      ObjectStore / .Filesystem / .Journal (confirmed: zero non-test references to
      ObjectStoreProvisioner/FilesystemProvisioner anywhere in cmd/aura/). provisionResourceLegs
      / resumeResourceProvisioning (internal/agui/onboarding_provision_resources.go) silently
      skip each leg when its port is nil — by explicit design comment ("Unwired ports (nil) skip
      that leg... backward compatible") — so a real HTTP admin-create through the running
      `aura serve` binary creates the identity row + Authula user + first-login markers but NO
      Garage bucket/key and NO per-identity mcp/skills/pyscripts directories. Independently
      confirmed the grace-window purge cannot run at all today by ANY mechanism: `handlers.
      IdentityPurgeHandler` exists but is absent from cmd/aura/serve_dispatch.go's `real` handler
      map (the live cron dispatch table), AND no migration ever widened
      aura.scheduler_tasks.kind's CHECK constraint (0009_scheduler.up.sql) to admit the literal
      'identity_purge' — so a purge task could not even be inserted, let alone scheduled.
    artifacts:
      - path: "cmd/aura/serve_onboarding.go"
        issue: "OnboardingDeps{} literal never sets ObjectStore/Filesystem/Journal"
      - path: "cmd/aura/serve_dispatch.go"
        issue: "the `real` handler map has no entry for handlers.KindIdentityPurge"
      - path: "internal/db/migrations/0009_scheduler.up.sql"
        issue: "kind CHECK constraint was never widened to admit 'identity_purge'"
    missing:
      - "Construct the garageadmin-backed ObjectStoreProvisioner + profile.RootIdentityDir-backed FilesystemProvisioner + Postgres-backed SagaJournal adapters at the serve composition root and thread them into OnboardingDeps"
      - "Register handlers.IdentityPurgeHandler in the live dispatch map, plus a migration widening the scheduler kind CHECK (or a boot-time interval-sweeper wrapper), so grace-window de-provisioning purge actually runs"
  - truth: "Per-identity Garage credentials are consumed on the asset read/write path, so object bytes are stored per-identity (MUSR-01 object-store plane, D-08 intent)"
    status: failed
    reason: >
      internal/assets/service.go, audio_processor.go, document_processor.go and
      image_processor.go all depend on a single injected objectstore.Store + one shared
      s.Bucket field. objectstore.IdentityStore.Resolve (the per-identity credential resolver
      shipped in 36-06) has ZERO non-test consumers repo-wide (confirmed by grep). Every
      identity's asset bytes therefore still land in ONE shared Garage bucket under one static
      credential; the per-identity bucket-ACL isolation this phase built is never reached at
      request time. Mitigating context (not a new regression): asset RECORD/presigned-URL access
      is, and pre-Phase-36 already was, gated by assets.Store.GetForIdentity, so B cannot obtain
      A's asset metadata or URL through Aura's own API today — but the storage-level
      defense-in-depth D-08 exists to provide is inert.
    artifacts:
      - path: "internal/assets/service.go"
        issue: "Objects objectstore.Store + Bucket string are the sole object-store dependency; no per-identity resolver call"
    missing:
      - "Route internal/assets Put/Get/Delete through objectstore.IdentityStore.Resolve(ctx) to obtain per-identity bucket+credentials instead of the shared Store/Bucket"
  - truth: "Documents-plane (Neo4j) identity isolation holds by default on a deployed/current instance (MUSR-01 documents plane)"
    status: partial
    reason: >
      AURA_MUSR_ISOLATION defaults false (internal/config/config.go:495) and is the sole
      enforcement switch for the six scoped Cypher queries (36-05). It is entirely absent from
      internal/config/config_validate.go's server_production profile requirements (zero MUSR
      references), so even a deployment that passes `aura config validate --profile
      server_production` is not forced to enable it. Until an operator runs `aura documents
      backfill` and flips the var (docs/runbooks/musr-rollout.md), retrieval runs the
      pre-existing UNSCOPED query — the exact spike-085 identity-blind leak this phase exists to
      fix remains live by default. This is a deliberate, reversible, well-documented D-13 rollout
      (mechanism is sound and unit/live-tested when flipped) rather than a code defect, hence
      "partial" not "failed" — but the phase-goal's unconditional "no cross-identity leak across
      ... documents" framing does not hold out-of-the-box.
    missing:
      - "Either add AURA_MUSR_ISOLATION to the server_production profile's required-set (internal/config/config_validate.go) or explicitly record an operator decision + runbook execution for this deployment's go-live timing"
  - truth: "The MUSR-06 no-long-lived-token-in-URL static gate runs automatically as a regression check (36-11 -> 36-12 handoff commitment)"
    status: partial
    reason: >
      scripts/check-no-url-tokens.sh exists, self-tests clean, and passes on the current tree
      (independently executed: exit 0, self-test exit 0) — the underlying truth (no long-lived
      token in a URL today) holds. But the script is wired into NO CI job, Makefile target, or
      git hook (confirmed: zero references under .github/, Makefile, scripts/*.sh besides the
      script itself). 36-11-SUMMARY.md explicitly named "36-12 wires
      scripts/check-no-url-tokens.sh into ci.yml" as the next-plan expectation; 36-12's plan and
      summary never mention it and ci.yml has no reference to the script, so a future regression
      would go uncaught.
    missing:
      - "Add a CI step invoking bash scripts/check-no-url-tokens.sh (e.g. beside the existing check-file-size.sh step) so this becomes an enforced regression gate"
  - truth: "The D-18 admin cross-session shell poll/kill exemption is reachable (MUSR-03 parenthetical: '... or an explicit admin capability')"
    status: partial
    reason: >
      Confirmed in cmd/aura/main.go: `tools.ShellPoll{Shells: handles.BackgroundShells}` and
      `tools.ShellKill{...}` are constructed with no `Caps` field, so it is nil at the
      composition root -> fail-closed to owner-only (SECURE: a foreign poll/kill is still
      denied, matching the core "Session B cannot poll/kill session A's shell" requirement).
      Only the ADMIN escape-hatch clause is unreachable. Does not block ROADMAP SC #2.
    missing:
      - "Wire ShellPoll.Caps/ShellKill.Caps to the live identity/capability store at the composition root (36-10 already established the HasCapability(governance.write) admin seam this would consult)"
human_verification:
  - test: "Bring the full docker-compose stack (Postgres + Neo4j + Garage-admin + embedded Authula) up in WSL/CI and run the five-tag two-identity E2E + the neo4j_integration backfill test + the db_integration/garage_integration tiers with -race"
    expected: "TestTwoIdentityCrossDeny and TestProvisionLoginIsolatedRun (cmd/aura, five tags) pass; TestRLSBackstop/TestAuraAppLacksRLSBypass (internal/db); TestDocumentsFailClosed (internal/documents, neo4j_integration); TestGarageAdmin* (internal/objectstore/garageadmin, garage_integration); TestIdentityObjectStore* (internal/objectstore, db_integration); TestProvisioningSagaResumable (internal/agui, db_integration+garage_integration); the full -race matrix on internal/agent/tools, internal/runner, internal/channels/telegram, internal/agui, internal/db, internal/documents, internal/objectstore, internal/mcp, internal/skills, internal/profile all pass"
    why_human: "Requires a live Postgres+Neo4j+Garage+Authula stack (CGO/-race too) that this Windows verification host cannot provide; per CLAUDE.md 'Where to run what' this must run in WSL or CI, and per the confirmed CI history, at least one sibling test in the same tier family (TestMigration0026LocalAdminCapsRoundTrip) is independently proven broken, so the untested remainder needs an actual run, not an inference from code review alone"
  - test: "Decide and record an operator decision on the AURA_MUSR_ISOLATION rollout timing for this specific deployment (run `aura documents backfill` then flip the flag) or accept the current off-by-default state via a documented override"
    expected: "Either the runbook (docs/runbooks/musr-rollout.md) is executed against this deployment's real data and the flag is flipped on, or a VERIFICATION.md override records why leaving it off (for now) is acceptable"
    why_human: "This is a deployment/business decision (when to activate enforcement for real users), not a code-correctness question — the mechanism is verified sound; only the activation timing is a human call"
---

# Phase 36: Multi-User Identity Isolation + Authula Cutover — Verification Report

**Phase Goal:** Multi-User Identity Isolation + Authula Cutover — two identities run end-to-end with NO cross-identity leak across conversations, approvals, documents (Neo4j), object-store (Garage), background jobs, MCP/skills/pyscripts roots, and Telegram routing; Authula cutover with break-glass + provisioning; capability-per-route; no long-lived token in URLs.
**Verified:** 2026-07-06T02:54:02Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Summary

This report does NOT trust the 12 SUMMARY.md files' claims. Every truth below was checked against the actual codebase at `HEAD` (`ab7e03a9`): `go build`/`go vet` (untagged AND with all five live tags: `db_integration neo4j_integration garage_integration authula_integration musr_e2e`) were independently re-run and are clean; the untagged unit-test suite for every touched package was independently re-run and is green; the `check-no-url-tokens.sh` and `check-file-size.sh` static gates were independently re-run; `sqlc generate` was independently re-run against the pinned v1.31.1 and diffed (clean); a targeted frontend `vitest` spot-check (28 tests across the 4 new admin/audit files) was independently re-run and passed. Beyond the codebase, `gh run list`/`gh run view` were used to inspect **actual GitHub Actions CI history** for this repository — this surfaced a **confirmed, currently-unfixed test regression** and the fact that **7 of the phase's 12 plans have never been pushed or CI-evaluated at all**, neither of which any SUMMARY.md disclosed (nor could have, since the executor had no way to observe them from a local Windows sandbox).

**Verdict:** the phase's identity-isolation MECHANISMS are, on the whole, well-designed, thoroughly unit-tested, and match the SUMMARYs' code-level claims. But the phase is not ready to close: one independently-discovered test regression is proven broken on real CI and unfixed at HEAD; the back half of the phase has never been pushed or run through any CI job; and two of the six "no cross-identity leak" axes named in the phase goal (object-store consumption, documents-plane default) are demonstrably NOT active in the system as it would ship today. Status: **gaps_found**.

## Goal Achievement

### Observable Truths — ROADMAP Success Criteria

| # | Truth | Status | Evidence |
|---|---|---|---|
| 1 | Two-identity live E2E — B cannot list/get/delete/archive/resolve A's data (404/403); a B-created chat is owned by B and runs | ? UNCERTAIN | `cmd/aura/two_identity_e2e_test.go` (349 LOC) + harness (209 LOC) exist, are substantive, and independently compile clean under all five tags (`go build`/`go vet -tags 'db_integration neo4j_integration garage_integration authula_integration musr_e2e' ./...`). The underlying mechanism (owner-scoped stores, RLS, `handleRun`'s `GetForIdentity` owner-gate, D-06 404/403 mapping) is independently VERIFIED at the unit level — `go test ./internal/agui/ ./internal/conversations/ ./internal/askuser/ ./internal/runner/` all pass, including `owner_scoping_test.go` and `TestNewConversationOwnedByPrincipal`. However the E2E itself has **never been executed** (unrun on this host, never pushed to any CI-capable environment — see gap 2), and a sibling test in the same live-tier family is confirmed broken (gap 1), so I cannot certify the acceptance run passes. |
| 2 | Session B cannot poll/kill session A's shell; jobs expire by TTL | VERIFIED | `internal/agent/tools/shell_bg_owner.go`/`shell_bg_ttl.go` read in full; `go test ./internal/agent/tools/` independently re-run here and green, including `TestBackgroundJobID` (crypto/rand 128-bit), `TestBackgroundJobOwnerDeny` (foreign poll=404-shape/kill=403-shape), `TestBackgroundJobTTLExpiry`, `TestBackgroundJobReaperTicksAndReaps`. `StartReaper` confirmed wired at boot in `cmd/aura/serve.go`. Only `-race` is unrun (CGO disabled on this host). |
| 3 | Conversation delete evicts all session tool state | VERIFIED | `internal/runner/runner_delete.go` (`DeleteConversationLifecycle`) read in full: owner-gate-first, then cancel→expire→evict→terminate-jobs→delete, in order. All three surfaces (`internal/agui/conversations_api.go`, `cmd/aura/serve_channels.go` Telegram `/clear` adapter, `cmd/aura/chat.go`) confirmed routed through the ONE method (no raw store delete found on any surface). `go test ./internal/runner/` independently re-run and green, including `TestConversationDeleteLifecycle` (3 surface shapes), `TestConversationDeleteLifecycleForeignDenied`, `TestSessionKeyIsolation(Concurrent)`. Only `-race` is unrun. |
| 4 | Authula is the default with provisioning + break-glass; no token in URLs | ✗ FAILED / PARTIAL | Individually strong pieces: `AURA_WEB_AUTH_PROVIDER` defaults `"authula"` (VERIFIED, predates this phase); break-glass `aura identity recover` mint logic is unit-VERIFIED (`cmd/aura/recovery_test.go` green here); `scripts/check-no-url-tokens.sh` independently re-run, exit 0 clean + self-test catches a planted violation; capability-per-route confirmed (`cmd/aura/serve_webui_musr.go`'s 4 admin routes wrapped in `agui.RequireCapability(..., governanceWriteCapability)`). But the SC's own "provisioning" clause is demonstrably **not live in production** — see gap 3 (nil ObjectStore/Filesystem/Journal ports at the `serve` composition root) — so a real admin-create through the shipped binary does not actually provision the Garage bucket/key or per-identity filesystem roots it advertises. |

**Score:** 2/4 cleanly VERIFIED, 1 UNCERTAIN, 1 FAILED/PARTIAL.

### Requirements Coverage (MUSR-01..06)

| Requirement | Source Plan(s) | Description (REQUIREMENTS.md) | Status | Evidence |
|---|---|---|---|---|
| MUSR-01 | 36-02/04/05/06/07/10/12 | Owner-scoped conversation/approval/document/object-store/filesystem surfaces; B never lists/gets/deletes/archives/resolves A's data | ⚠ PARTIAL (gaps_found) | Postgres plane (conversations/approvals, RLS + `*ForIdentity` + D-06 404/403, incl. the 36-12 branch-route fix) is VERIFIED and **unconditionally active** (no flag). Documents plane mechanism is VERIFIED but **off by default** (gap 5). Object-store plane: bucket-per-identity + resolver mechanism VERIFIED at unit level, but boot-wiring (gap 3) and consumption (gap 4) are both confirmed absent — the plane is inert end-to-end in production today. Filesystem plane (MCP/skills/pyscripts rooting): traversal-safe rooting VERIFIED at unit level, but per-identity dirs are never created because provisioning never calls the port (gap 3). Admin audit UI VERIFIED (backend + 28 independently-re-run frontend tests green). |
| MUSR-02 | 36-04, 36-12 | New Web conversation owned by `identityctx.IdentityID(ctx)` | ✓ VERIFIED | `internal/runner/runner_conversation.go` `defaultConversationOwner` reads the principal (verified by code read); `TestNewConversationOwnedByPrincipal` independently re-run, green. The E2E's `musr02_b_creates_owns_runs` subtest also exercises this logic (compiles clean; unrun live). |
| MUSR-03 | 36-03 | Unguessable owner-bound job IDs; foreign poll/kill denied unless admin cap | ✓ VERIFIED (core), ⚠ admin-cap escape hatch inert (gap 7) | Core deny-by-default property independently re-run and green (see ROADMAP SC #2). The parenthetical admin exemption is unreachable (nil `Caps` at the composition root) — this makes the system MORE restrictive than spec, not less, so the primary observable clause holds. |
| MUSR-04 | 36-03 | Default 1h TTL; expiry terminates process group + records status; age metric | ✓ VERIFIED | `TestBackgroundJobTTLExpiry`, `TestBackgroundJobAge`, `TestBackgroundJobTTLDefaultAndOverride` independently re-run, green. |
| MUSR-05 | 36-09 | All conversation deletion routes through one lifecycle; cancels/expires/evicts/terminates before delete | ✓ VERIFIED | See ROADMAP SC #3 above. |
| MUSR-06 | 36-01/08/10/11/12 | Authula default + provisioning + break-glass; capability-per-route; no token in URLs | ⚠ PARTIAL (gaps_found) | See ROADMAP SC #4 above — every clause except "provisioning" is independently VERIFIED; "provisioning" is confirmed dormant (gap 3). REQUIREMENTS.md's own MUSR-06 line already caveats "Residual: the serve-time boot wiring... is a recorded follow-up... Live run pending WSL/CI" — this verification confirms that caveat is real and, per gap 3, more consequential than "just run it live" (the boot wiring itself must still be written). |

No orphaned requirements: every plan's `requirements:` frontmatter (36-01 through 36-12) was cross-referenced against REQUIREMENTS.md's `MUSR-01..06` set — the union covers all six with no unclaimed IDs. The ROADMAP top-level bullet also lists a `QUAL(Authula DSN test)` item for Phase 36, but REQUIREMENTS.md's Phase-36 coverage row and the ROADMAP's own "Phase Details" §36 requirements line both track it separately, and Phase 32's plan 32-09 (`Authula ensureAuthulaSearchPath DSN parsing`) already closed it before Phase 36 began — not a Phase 36 gap.

### Required Artifacts

| Artifact | Expected | Status | Details |
|---|---|---|---|
| `internal/db/tx.go` (`WithIdentityTx`) | RLS carrier, `SET LOCAL` via `set_config(is_local=true)` | ✓ VERIFIED | Read in full; exactly one `set_config` call, tx-scoped (never a bare `SET`). |
| `internal/db/migrations/0032_owner_rls.up.sql` | ENABLE RLS + owner policy on conversations/paused_states/conversation_turns | ✓ VERIFIED (see design-decision scrutiny below) | Read in full; policies present and well-commented; migration also installs the `paused_states_set_identity_trg` BEFORE INSERT trigger. |
| `internal/conversations/store_identity.go`, `internal/askuser/store_identity.go` | `*ForIdentity` owner-scoped methods | ✓ VERIFIED | Present; exercised by `owner_scoping_test.go` (pass) and the E2E (compiles clean). |
| `internal/documents/indexer.go`, `search.go`, `fail_closed_integration_test.go` | HAS_DOCUMENT ownership MERGE + flag-gated scoped queries + live fail-closed test | ✓ VERIFIED (mechanism), ⚠ PARTIAL (activation — gap 5) | Code present, substantive, unit-tested; live `neo4j_integration` test compiles clean, unrun. Enforcement gated behind `AURA_MUSR_ISOLATION`, default off. |
| `internal/objectstore/garageadmin/client.go`, `internal/objectstore/identity_store.go` | Garage Admin v2 client + per-identity credential resolver | ✓ VERIFIED (mechanism), ✗ ORPHANED at runtime (gaps 3+4) | Code present, substantive, unit-tested (`go test ./internal/objectstore/...` green). Zero non-test production consumers of `IdentityStore.Resolve`; zero production wiring of the provisioning legs that would populate `identity_object_store` rows for real users. |
| `internal/mcp/managed_config_identity.go`, `internal/skills/identity_root.go` | Per-identity MCP config + skills/pyscripts storage rooting | ✓ VERIFIED (mechanism), ⚠ never invoked for a real provisioned user (gap 3) | Code present, substantive, unit-tested. `NewSkillToolForIdentity`/`MountForIdentity` are not called anywhere from the live `aura serve` provisioning path. |
| `internal/agui/saga_journal.go`, `onboarding_provision_resources.go`, `deprovision.go`, `internal/cron/handlers/identity_purge.go` | Journaled provisioning/de-provisioning saga + purge handler | ✓ VERIFIED (mechanism, live-tested with manually-wired adapters in `provisioning_saga_resumable_test.go`), ✗ ORPHANED at the composition root (gap 3) | Mechanism itself is sound — `TestProvisioningSagaResumable` constructs the REAL adapters and proves forward-recovery + de-provision symmetry (compiles clean, unrun live). Never reachable from `aura serve`. |
| `internal/runner/runner_delete.go`, `runner_session.go` | Delete lifecycle + composite `(identity,session)` keying | ✓ VERIFIED | See ROADMAP SC #3. |
| `internal/channels/telegram/bot_dispatch_turn.go` | Per-user Telegram turn scoping | ✓ VERIFIED | `scopeTurnToIdentity` in `startTurn`, the single choke point for fresh/async-doc/HITL-resume turns; `TestTelegramPerUserTurnScopesToOwnIdentity` independently re-run, green. |
| `scripts/check-no-url-tokens.sh` | Static gate, no long-lived token in a URL | ✓ VERIFIED (script), ⚠ PARTIAL (not CI-wired — gap 6) | Independently executed: clean + self-test passes. Not referenced in any CI/Makefile/hook. |
| `internal/agui/audit_api.go`, `web/src/audit/AdminAuditView.tsx`, `web/src/settings/CapabilityAdminPanel.tsx` | Admin audit read API + grant/revoke + admin UI | ✓ VERIFIED | Backend unit tests green; independently re-ran the 4 relevant frontend test files (`CapabilityAdminPanel.test.tsx`, `AdminAuditView.test.tsx`, `AppShell.shell.test.tsx`, `SettingsWorkspace.test.tsx`) via `npx vitest run` — 28/28 tests pass. |
| `cmd/aura/two_identity_e2e_test.go`, `two_identity_e2e_harness_test.go` | D-29 two-identity cross-deny acceptance E2E | ✓ VERIFIED (exists, substantive, compiles clean under all 5 tags) | See ROADMAP SC #1 — logically sound, never executed. Notably provisions test identities via direct DB INSERT + direct Garage admin-client calls, NOT through the real onboarding HTTP flow — so even a green live run would not, by itself, prove the real `aura serve` admin-create path provisions resources (it can't, given gap 3). |
| `.github/workflows/ci.yml` `musr-e2e` job | Full live-stack CI job for the acceptance E2E | ✓ VERIFIED (well-constructed), ✗ NEVER RUN (gap 2) | Read in full: real Postgres/Neo4j/Garage/Authula bring-up, composed DSNs, Garage layout assignment, admin-API reachability pre-check, `-race`, no-skip-as-green env. Does not exist on `origin/master` — only in unpushed local commits. |

### Key Link Verification

| From | To | Via | Status | Details |
|---|---|---|---|---|
| `internal/agui/server.go` `handleRun` | `internal/conversations.Store.GetForIdentity` | owner-gate before `Runner.Turn` | ✓ WIRED | Read in full: `GetForIdentity` is called and 404s BEFORE `s.run.Turn(...)` is ever invoked — confirms the "owner gate first" pattern this whole isolation design leans on. |
| `internal/agui/conversations_branch_api.go` (3 routes) | `internal/conversations.Store.GetForIdentity` | `ownBranchConvOr404` at the top of each handler | ✓ WIRED | The 36-04 carry-forward gap (unscoped branch routes) is confirmed closed at 36-12: 3 call sites found, one per route. |
| `cmd/aura/serve_onboarding.go` `OnboardingDeps{}` | `internal/agui.ObjectStoreProvisioner` / `FilesystemProvisioner` / `SagaJournal` | composition-root field assignment | ✗ NOT WIRED | Zero assignment found — confirmed gap 3. |
| `internal/assets/service.go` | `internal/objectstore.IdentityStore.Resolve` | per-identity credential lookup | ✗ NOT WIRED | Zero call found — confirmed gap 4. |
| `cmd/aura/serve_dispatch.go` `real` handler map | `internal/cron/handlers.IdentityPurgeHandler` | `cron.KindIdentityPurge` entry | ✗ NOT WIRED | Entry absent — confirmed gap 3. |
| `internal/db/migrations/0009_scheduler.up.sql` `kind` CHECK | `'identity_purge'` | enum literal | ✗ NOT WIRED | Literal absent from the CHECK list even after migrations through 0032 — confirmed gap 3. |
| `scripts/check-no-url-tokens.sh` | `.github/workflows/ci.yml` | CI step invocation | ✗ NOT WIRED | Zero reference — confirmed gap 6. |
| `cmd/aura/main.go` `tools.ShellPoll{}`/`ShellKill{}` | identity/capability store | `.Caps` field | ✗ NOT WIRED (fails closed, secure) | Confirmed nil — gap 7. |

### Data-Flow Trace (Level 4) — RLS Design Decision Scrutiny

The task specifically asked me to scrutinize 36-04's RLS policy: **permissive-on-unset, fail-closed-on-mismatch** (not fail-closed-on-unset). Traced this end to end:

- `internal/db/migrations/0032_owner_rls.up.sql`'s policy: `USING (NULLIF(current_setting('app.current_identity', true), '') IS NULL OR identity_id = NULLIF(...)::uuid)`. When the session var is **unset** (the legacy pool / `db.WithTx` paths — the runner's own `AppendTurn`/`LoadHistory`, `aura chat`, Telegram, the 34-06 `ResumeCommitter`), the policy is **permissive** — RLS provides **zero** filtering for those callers, full stop.
- Traced the call graph: `internal/agui/server.go` `handleRun` (the web entry point that eventually drives the runner's `AppendTurn`) calls `s.conv.GetForIdentity(ctx, in.ThreadID, scopedIdentityID(ctx))` and 404s on a foreign/absent id **before** ever calling `s.run.Turn(...)`. So TODAY, every reachable path that lets a caller drive `AppendTurn`/`LoadHistory` on an arbitrary conversation id is gated by an app-level owner check first; RLS's "storage-enforced" backstop is real ONLY for the `WithIdentityTx`-wrapped surfaces (list/get/delete/archive/resolve — the AG-UI handlers), not universally.
- **Finding:** the RBAC-03 amendment's own framing ("kernel/storage-enforced... a forgotten WHERE identity_id must not leak") is accurate for the identity-scoped entry points that use `WithIdentityTx`, but it does **not** hold as a universal backstop — a future code path that hands the runner a user-supplied conversation id WITHOUT first calling the owner-gated `GetForIdentity` would NOT be caught by RLS, because the var would be unset at that point and the policy is permissive. This is architecturally reasoned and necessary (a strict fail-closed-on-unset policy would break the runner/CLI/Telegram write paths and make the D-06 403-vs-404 probe impossible, per the migration's own extensive comment), and no currently-reachable exploit path was found — but it is a narrower guarantee than "storage-enforced" suggests, and the migration comment itself says tightening is "deferred until every write/read path sets the var." **Recommend:** track this as a follow-up (not a Phase-36 blocker) once all conversation/turn/paused-state writers are converted to `WithIdentityTx`.
- Separately, `AURA_MUSR_ISOLATION` (the documents-plane flag) is a config default, not a data-flow issue — traced and reported as gap 5 above (off by default, not tied to `server_production` profile validation).

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Repo builds clean (untagged) | `go build ./...` | no output | ✓ PASS |
| Repo vets clean (untagged) | `go vet ./...` | no output | ✓ PASS |
| Repo builds clean under all 5 live tags | `go build -tags 'db_integration neo4j_integration garage_integration authula_integration musr_e2e' ./...` | no output | ✓ PASS |
| Repo vets clean under all 5 live tags | `go vet -tags '...' ./...` | no output | ✓ PASS |
| Untagged unit suite, phase-36 packages (batch 1) | `go test ./internal/db/... ./internal/conversations/... ./internal/askuser/... ./internal/agui/... ./internal/documents/... ./internal/objectstore/... ./internal/mcp/... ./internal/skills/... ./internal/profile/...` | all `ok` | ✓ PASS |
| Untagged unit suite, phase-36 packages (batch 2) | `go test ./internal/agent/tools/... ./internal/runner/... ./internal/channels/telegram/... ./internal/cron/... ./internal/webauth/... ./internal/config/... ./cmd/aura/...` | all `ok` | ✓ PASS |
| No-URL-token static gate | `bash scripts/check-no-url-tokens.sh` | `OK — no long-lived session/auth token in any URL/query string.` | ✓ PASS |
| No-URL-token gate self-test | `bash scripts/check-no-url-tokens.sh --self-test` | `self-test OK` | ✓ PASS |
| File-size cap | `bash scripts/check-file-size.sh` | `all source files within the 600-LOC cap` | ✓ PASS |
| sqlc generate in sync at HEAD | `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate` then `git diff --stat internal/db/sqlc/` | empty diff | ✓ PASS (fixes the historical CI failure — see gap 2) |
| Frontend admin/audit spot-check | `npx vitest run src/settings/__tests__/CapabilityAdminPanel.test.tsx src/audit/__tests__/AdminAuditView.test.tsx src/__tests__/AppShell.shell.test.tsx src/settings/__tests__/SettingsWorkspace.test.tsx` | `4 passed (4)` / `28 passed (28)` | ✓ PASS |
| `-race` on any touched package | `go test -race ./internal/config/` | `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` | ? SKIP (environment-blocked on this Windows host, as every SUMMARY honestly reported) |

### Probe Execution

No `scripts/*/tests/probe-*.sh` convention exists in this repository and none is declared by any Phase-36 PLAN/SUMMARY — SKIPPED (not applicable).

### CI History (independent evidence, not in any SUMMARY)

Using `gh run list`/`gh run view` against the real `chetto1983/Aura` GitHub repository:

- `origin/master` HEAD is `f44dde45` (2026-07-05T23:11), which only contains Phase-36 work through **36-04** and **36-07**. The local branch is **27 commits ahead** — **36-05, 36-06, 36-08, 36-09, 36-10, 36-11, 36-12 have never been pushed.**
- The last actual CI run for pushed Phase-36 work (commit `docs(36-07)`, run `28753262579`, 2026-07-05T20:05) shows **3 failed jobs**: `Integration tests (db_integration tag)` and `Knowledge integration + smoke (neo4j + sidecar)` both failed on `--- FAIL: TestMigration0026LocalAdminCapsRoundTrip` (root-caused above, gap 1, still broken at HEAD); `sqlc generate is in sync` failed on the Wave-1 drift 36-04 itself already fixed (independently reconfirmed clean at HEAD, above); `Build + vet + lint + deadcode + file-size cap` failed on an unrelated pre-existing "quality snapshot row... Phase 10 Slice 6 must re-measure" staleness check (not a Phase-36 regression).
- No CI run of any kind exists yet for 36-05 through 36-12 — including the `musr-e2e` job, which does not exist on `origin/master` at all.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|---|---|---|---|---|
| `internal/db/migrate_0026_integration_test.go` | 94, 104 | Relative-step migration test masquerading as version-targeted | 🛑 Blocker | See gap 1 — confirmed broken on live CI, unfixed at HEAD |
| — | — | No `TODO`/`FIXME`/`XXX`/`HACK`/`PLACEHOLDER` debt markers found in any of the 152 files changed in the `e8bbe1f8..HEAD` range | ℹ️ Info | Clean — the one `XXXXXX` grep hit is a `mktemp` filename template, not a debt marker |
| — | — | No hardcoded-empty-return stubs found; the "not available in this context" error strings in `shell_bg_owner.go`/`shell_exec.go` are legitimate nil-dependency guards, not placeholders | ℹ️ Info | Clean |

### Human Verification Required

See frontmatter `human_verification`. Two items: (1) run the full live-stack + `-race` matrix in WSL/CI once pushed, given a sibling test in the same family is confirmed broken; (2) decide and record the `AURA_MUSR_ISOLATION` activation timing for this deployment.

### Gaps Summary

The phase's identity-isolation **mechanisms** are well-built: RLS + owner-scoped stores for conversations/approvals (unconditionally active, independently unit-verified), background-job owner-binding + TTL (independently unit-verified), the conversation-delete lifecycle (independently unit-verified), Telegram per-user routing (independently unit-verified), the admin/audit UI (independently unit- and frontend-test-verified), and the documents/object-store isolation planes (built, unit- and mechanism-level-tested) are all real, substantive, non-stub code — this is not a case of hollow scaffolding.

But the phase is not ready to close, for concrete, independently-discovered reasons beyond what any SUMMARY.md disclosed:

1. **A confirmed, currently-broken test** (`TestMigration0026LocalAdminCapsRoundTrip`) exists in code this phase shipped, proven failing on real CI, unfixed at HEAD, and structurally guaranteed to keep failing (it reverses whatever the CURRENT LATEST migration is, not migration 0026, forever).
2. **7 of 12 plans were never pushed** and have never been evaluated by any CI job at all — a direct violation of this project's own stated git-push discipline, and the reason the `musr-e2e` acceptance job has literally never run.
3. **Two of the six "no cross-identity leak" axes named in the phase goal are not active** in the system as it would ship today: object-store (Garage) consumption is never wired to the per-identity resolver (assets always use the shared bucket — mitigated by a pre-existing Postgres ownership check, but the new storage-level defense is inert), and documents-plane (Neo4j) enforcement is off by default with no tie-in to the `server_production` profile validator.
4. **Provisioning does not actually provision** the Garage/filesystem resources it advertises when exercised through the real `aura serve` binary — the saga mechanism is sound (proven with manually-wired adapters in a live-tagged test) but the composition-root wiring was never done, and the grace-window purge scheduler is unreachable by any path (missing dispatch-map entry AND missing DB schema support).
5. Two lower-severity, non-blocking findings: the MUSR-06 static gate is not CI-wired despite an explicit 36-11→36-12 handoff commitment to do so; the D-18 admin cross-session shell recovery escape hatch is inert (fails closed/secure, just unavailable).

None of the residual gaps the SUMMARYs themselves flagged (36-08 boot wiring, 36-06 S3 consumption, 36-03 admin exemption) were found to be overstated — if anything, this verification found the boot-wiring gap to be MORE consequential than described (the grace-window purge is structurally unschedulable, not just unregistered) and surfaced two additional, previously-undocumented issues (the migration-test regression and the unpushed/CI-unvalidated back half of the phase) that materially change the "is this phase actually done" answer from "yes, pending a live run" to "no — fix a confirmed bug, push the work, and let CI actually prove it, first."

---

_Verified: 2026-07-06T02:54:02Z_
_Verifier: Claude (gsd-verifier)_
