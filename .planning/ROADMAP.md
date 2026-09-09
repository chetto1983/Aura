# Roadmap: Aura — v1.1.0 Production Launch — Multi-Tenant

## Overview

The release gate this repo already declares has never been run. Eight of its twelve reports
have no artifact on disk; all eight make targets and all six evidence scripts exist and are
self-tested. Aura already has an authorization model — `aura.capability_grants`,
`RequireCapability`, five capabilities enforced today and a cockpit admin panel that grants
them — and it ships with a `*` wildcard on the bootstrap operator that would grant every new
capability before anyone granted it. Per-identity data
scoping already works in SQL, in RLS, in a per-identity ArcadeDB database and a per-identity
Garage bucket, and has never been put under two concurrent users, under attack, across a
restart, or at the process and host level. The launch documentation was rewritten in a
parallel session and has never been followed by anyone who did not write it.

So this milestone is not a build. It is seven live runs against the stack, in the order that
makes each one mean something, with the code fixes each one forces. Every phase closes on a
real end-to-end run driven by the real agent — never on a unit suite. Unit tests, coverage
and mutation are how a phase gets to its live run; they are never the evidence that it
arrived.

The order is forced by one hard constraint and one soft one. The hard one:
`scripts/release_readiness_gate.py` accepts only reports bound to the exact
`git rev-parse HEAD` and under 24 hours old, so the twelve reports cannot be accumulated —
the last phase produces them together in one window and nothing may commit during it. The
soft one: each drill is only meaningful once the state it operates on exists. Restoring an
empty deployment proves nothing; measuring concurrency with one identity measures the wrong
thing; probing a permission boundary before the permissions exist probes ownership instead.

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Numbering starts at 1. The previous milestone's phase directories were deleted and its
numbering (45–54) is not carried forward.

- [ ] **Phase 1: Two Identities, Live and Separated** - Turn on the shipped multi-identity profile, provision a second identity through the documented path, and prove two concurrent turns share no data and no execution
- [ ] **Phase 2: Two Roles and a Budget** - An admin adds and removes users and nobody else can, every user may do everything else, and what bounds a user is a per-identity OpenRouter cap the provider enforces
- [ ] **Phase 3: The Boundary Under Attack** - A re-runnable adversarial suite plus a sandbox escape battery, every attempt refused and audited
- [ ] **Phase 4: Load, Chaos and Truthful Degradation** - Produce load, chaos and observability evidence for the first time, with two identities active
- [ ] **Phase 5: Restart, Rollback, Restore** - Prove isolation survives the operational lifecycle, producing the DR and rollback evidence in the same drill
- [ ] **Phase 6: A Stranger Can Install and Operate It** - A clean-machine walkthrough driven only by the written docs, from clone to two working identities and back from a restore
- [ ] **Phase 7: One SHA, Twelve Reports, One Window** - Freeze the candidate and produce all twelve reports together, then publish through the release workflows

## Phase Details

### Phase 1: Two Identities, Live and Separated

**Goal**: The shipped deployment profile runs two identities at the same time, and neither can reach the other's data or the other's execution.
**Depends on**: Nothing (first phase)
**Requirements**: ISO-01, ISO-02, ISO-02a, ISO-05, E2E-01, E2E-02
**Success Criteria** (what must be TRUE):

  1. `AURA_MUSR_ISOLATION` is on in the shipped deployment profile, and a second identity is provisioned from zero through the documented path — memory database, derived credential, object bucket, sandbox and skills root all land, with no manual SQL and no step outside the path. Today `internal/config/config.go:554` defaults it false and `internal/agui/onboarding_provision.go:128` refuses the provision outright.
  2. Two identities hold real conversations at the same time against one running `aura serve`, each doing useful work — a document search, a memory write, a sandbox command — and the pair is scored ≥9.8.
  3. Neither identity can read the other's documents, conversations, turns, approvals, memory facts or objects through any surface it can reach while both are live. Five of those planes are already proven: `TestTwoIdentityCrossDeny` passed against this live stack on 2026-09-07 — http read, store owner gate + RLS, MUSR-02, approvals, documents and Garage, in 2.98s. Long-term memory is the plane it does not cover.
  3a. That gate runs unattended, from a clean checkout and in CI. On 2026-09-07 it took two undocumented manual steps: an ad-hoc socat container for Garage's admin API — now fixed by publishing :3903 on loopback (`a3536af5d`) — and a hand-created disposable database, because the test rightly refuses to migrate the live one. Neither step is written down anywhere a second person would find it.
  4. One identity's turn cannot observe or affect the other's: per-turn context, tool state and in-flight results are separated, not merely row-filtered, and the separation is demonstrated at the `runner`/`LlmAgent` level rather than asserted from a row count.

**Closes on (live run)**: `cmd/aura/two_identity_e2e_test.go` (tag `musr_e2e`) promoted from harness to a run against a live `aura serve` with two provisioned identities, immediately followed by two authenticated concurrent `/agent/run` conversations through the AG-UI gateway — one per identity, each with real tool calls — scored against the CLAUDE.md ≥9.8 bar.
**Plans**: 7/7 plans executed

Plans:
**Wave 1**

- [x] 01-01-PLAN.md — TRACER: a second identity provisioned end to end through `aura identity create`, with the eager sandbox saga leg, all four E2E-02 resources landing in one run (wave 1)
- [x] 01-07-PLAN.md — the Authula TOTP enrollment contract measured from the installed module before anything is built on it, and RESEARCH.md's open questions dispositioned (wave 1)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01-02-PLAN.md — the shipped multi-identity profile in `.env.example` and the installer heredoc, the sandbox-image boot preflight, the installer image step, and the non-strict INFO line (wave 2)
- [x] 01-03-PLAN.md — the long-term-memory cross-deny plane on all three surfaces, the fifth build tag landing in one commit with every command that selects on it, and the tenant edge battery (wave 2)
- [x] 01-05-PLAN.md — the concurrent-runner white-box separation test under `-race` and goleak, proving all four shared execution surfaces disjoint (wave 2)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 01-04-PLAN.md — `make musr-e2e`, the extracted disposable-stack library, the CI job that calls the same target, and the runbook Acceptance section (wave 3)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 01-06-PLAN.md — the committed two-identity live-run harness, its blocking machine checks, and the recorded ≥9.8 rubric (wave 4)

### Phase 2: Two Roles and a Budget

**Goal**: An admin adds and removes users and nobody else can; every user may do everything else; and what bounds a user is money, enforced by OpenRouter rather than by Aura's own accounting.
**Depends on**: Phase 1 — a permission check and a per-identity budget both need a second principal to be meaningful. With one identity every check trivially passes and there is nobody to bill.
**Requirements**: RBAC-01, RBAC-02, RBAC-03, RBAC-04, RBAC-05, RBAC-06, RBAC-07, RBAC-08, RBAC-09, RBAC-10, RBAC-11, CRED-01, CRED-02, CRED-03, CRED-04, CRED-05, CRED-06, CRED-07, CRED-08, CRED-09, REL-06
**Success Criteria** (what must be TRUE):

  1. The `*` wildcard is retired. Bootstrap (`cmd/aura/serve_bootstrap.go:258`) mints the explicit set — `identity.create`, `identity.delete` and the four every user holds — a migration rewrites existing wildcard rows into it, and `HasCapability`'s SQL no longer expands `*`. Migration `0026` already made `local`'s grants explicit precisely so the admin contract would survive this narrowing; `0099` refused to store approval scopes in this table because the wildcard would have shipped its gate open.
  2. Exactly two capabilities are administrative, and every capability Aura enforces is declared in one place with its meaning. Every other capability is granted to each identity at provisioning, so a user can mount an MCP server, author a skill, run a shell in the sandbox and approve a destructive action — only user management is refused. This is a deliberate narrowing of the milestone's original design, which would have added five capabilities and enforced each at its call site: isolation already keeps an identity inside its own perimeter, and a permission every identity holds is not a permission.
  3. Admin is bootstrap-only and has no path to escalation. `POST` and `DELETE /api/admin/identities/{id}/capabilities` refuse `identity.create` and `identity.delete` for every caller, so the administrative capability never transits the API, and the last administrative identity cannot remove or deactivate itself.
  4. The cockpit creates an identity and removes one without leaving the UI. Creation is the leg that does not exist; the reverse saga does — `internal/agui/deprovision.go` tears down every plane the provisioning saga built, is journaled and idempotent, and today is reachable only from `aura identity deactivate|purge` with no HTTP route at all.
  5. Each identity is minted its own OpenRouter key through the Provisioning API at provisioning, at a zero cap, stored encrypted per identity on the pattern `internal/mcpoauth/store.go` already establishes (AES-256-GCM, KEK from `AURA_AUTHULA_SECRET`, RLS, `ON DELETE CASCADE`). The deployment key is never a fallback: an identity without its own key is refused rather than billed to the operator. The work this forces is real — `llm.Load()` resolves `OPENROUTER_API_KEY` once at boot and `chat.cfg.LLM` reaches the agent at construction (`internal/agent/llm_agent_construct.go:36`), so no per-identity credential resolution exists today.
  6. An admin sets an identity's cap and reset interval from the cockpit and can change both. An identity at a zero cap is refused cleanly before the model is called, not by a raw provider 403 mid-turn; an identity with credit runs; and the cockpit shows cap, remaining and spend. The displayed figure comes from Aura's own in-band ledger — `cost` arrives on every OpenRouter response including streaming, `internal/agent/turn_usage.go` already sums it across a turn's calls and `aura.cache_metrics.cost_usd` already persists it — because the provider's own counter lags a spend by 30 to 40 seconds. Measured 2026-09-08: lowering a cap bites in 5s, raising one takes about 25s to unblock, and the cockpit must say so rather than look broken.
  7. Removing an identity revokes its OpenRouter key, and the revocation is verified rather than assumed. What it cannot do is erase the provider's record: a deleted key's consumption still appears in OpenRouter's analytics, so identity deletion is complete on our planes and incomplete on theirs, and that is stated rather than discovered.
  8. `mutation-report.json` shows ≥70% killed separately for gateway, identity, profile, sandbox and frontend — the refusal branches this phase adds are provably killed, not merely covered.

**Closes on (live run)**: with the two identities Phase 1 left live — A administrative, B an ordinary user — the real agent is driven as each. B installs an MCP server, writes a skill, runs a shell command and approves a destructive tool call, and all four succeed. B attempts to create an identity and to remove one, and both are refused and readable afterwards out of the audit trail by their own query. A creates a third identity and removes it, both through the cockpit rather than by curl, and the reverse saga is observed to land on every plane. B is then set to a zero cap and its next turn is refused before the model is called; A gives B credit and B's next turn runs; B's spend appears in the cockpit against B and not against A. Scored ≥9.8.
**Plans**: 3/10 plans executed

Plans:
**Wave 1**

- [x] 02-01-PLAN.md — TRACER: the wildcard retired end to end and one identity's turn running on its own encrypted key, both halves on one path per layer (wave 1)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 02-02-PLAN.md — the administrative pair made ungrantable through any API, the last-admin protection inside the saga, the uniform grant at provisioning, and the declaration-location CI check (wave 2)
- [x] 02-03-PLAN.md — the OpenRouter Provisioning-API client: mint at a real zero cap, patch, read, and a revoke that proves itself (wave 2)
- [x] 02-04-PLAN.md — the capability-denial ledger and its leg on the audit feed the cockpit already reads (wave 2)
- [x] 02-05-PLAN.md — the credit-exhausted refusal that fires before the network, and the deployment-key fallback closed at every agent-construction site (wave 2)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 02-06-PLAN.md — the mint leg at provisioning with compensation, the verified revoke leg on removal, and the management credential beside the inference key (wave 3)

**Wave 4** *(blocked on Wave 3 completion)*

- [ ] 02-07-PLAN.md — the admin removal and credit routes, and the money columns widened so the in-band ledger can hold what a call actually costs (wave 4)

**Wave 5** *(blocked on Wave 4 completion)*

- [ ] 02-08-PLAN.md — the cockpit: identity roster, typed-confirmation removal, credit panel, three-phase wizard, refusal copy (wave 5)

**Wave 6** *(blocked on Wave 5 completion)*

- [ ] 02-09-PLAN.md — the admin spend overview: three reconciliation calls server-side, one endpoint, a zero-dependency dashboard (wave 6)

**Wave 7** *(blocked on Wave 6 completion)*

- [ ] 02-10-PLAN.md — the mutation gate pointed at this phase's refusal branches, and the scored live permission-and-credit matrix (wave 7)

### Phase 3: The Boundary Under Attack

**Goal**: A deliberate attempt to cross the identity boundary fails, is visible afterwards, and the operator can re-run the whole attempt himself.
**Depends on**: Phase 2 — an attack suite must probe the permission checks as well as ownership scoping. Run before RBAC exists, it proves only that a row filter works, and would have to be written twice.
**Requirements**: ISO-03, ISO-04, ISO-07, E2E-03, REL-04
**Success Criteria** (what must be TRUE):

  1. Every boundary-crossing attempt is refused: guessed identifiers on every read endpoint, a shared link used outside its grant, and a tool handed another identity's identifier.
  2. A prompt-injection turn that tries to make the agent read or write another identity's memory fails, and the attempt is visible in the audit trail rather than merely absent from the result.
  3. From inside a live sandbox, the host is unreachable: no Docker socket, no host filesystem outside the identity's roots, and no Aura credential in the environment of a launched stdio MCP server. `internal/sandbox/usersandbox/translate.go` currently mounts `aura-uv-cache`, `aura-npm-cache` and `aura-pip-cache` read-write into *every* identity's box, with `CapDrop: []string{}` and no `no-new-privileges`; `internal/skills/installer.go:378` still hands `os.Environ()` to `npx skills add` while the sibling fix `secret.InstallerEnv` already exists at `internal/mcp/process_env.go:68`.
  4. The suite is a scripted artifact an operator re-runs on demand, not a session transcript — it exits non-zero on the first attempt that succeeds.
  5. `docker-coverage-report.json` passes at ≥85% merged statements for the owned sandbox surface under native `docker_integration` — the delegated authority for `internal/sandbox/usersandbox`, which this phase is the one to touch.

**Closes on (live run)**: the adversarial suite executed end to end against the live two-identity stack — the HTTP/tool leg driven as identity B against identity A's real identifiers, and the sandbox leg executed as real commands inside a live box (reach the Docker socket, escape the workspace root, read the parent process environment, plant a wheel in the shared pip cache and try to make A's sandbox install it). Every leg refused; the run repeated after each fix until it is clean.
**Plans**: TBD

### Phase 4: Load, Chaos and Truthful Degradation

**Goal**: The deployment holds its declared concurrency with two identities working, degrades honestly when a dependency dies, recovers — and an operator can see all three happen.
**Depends on**: Phase 3 — concurrency measured before the boundary is enforced measures the wrong system, because a load run that passes by leaking across identities is worse than a failing one. Observability evidence also has to describe a deployment whose refusals already exist, or the dashboards and alerts get built against a shape that then changes.
**Requirements**: REL-08, REL-09, REL-11, ISO-06
**Success Criteria** (what must be TRUE):

  1. `load-report.json` is produced for the first time and passes: the declared supported concurrency is met with at least two identities active, success ratio and p95 inside the declared budget.
  2. `chaos-report.json` is produced for the first time and passes: the DB, MCP, Garage and process-kill scenarios all execute, the system degrades truthfully rather than lying about success, and it recovers.
  3. A resource exhausted by one identity — loop budget, sandbox, tokens — does not deny service to the other; the second identity's turn still completes while the first is starved.
  4. `observability-report.json` is produced for the first time and passes: negative fixtures, runtime smoke, live health and readiness, dashboards, alerts and runbooks.

**Closes on (live run)**: `make load-chaos` and `make observability-evidence` against the live stack with two provisioned identities driving real turns, with `make observability-check` on the sidecars first. `scripts/production_load_chaos.py` (501 lines) and `scripts/observability_evidence.py` (261 lines) both exist and neither has ever emitted a report; the run is expected to break, and closing what it breaks is the work of the phase.
**Plans**: TBD

### Phase 5: Restart, Rollback, Restore

**Goal**: Both identities survive the operational lifecycle intact and still separated — and the disaster-recovery and rollback evidence falls out of the same drill.
**Depends on**: Phase 4 — a restore is only meaningful against a deployment carrying real two-identity state (memory graphs, buckets, sandboxes, grants, roles), which phases 1–3 create and phase 4 exercises. Process-kill recovery in chaos is the cheap failure; restoring from backup is the expensive one, and doing it second means the cheap one has already flushed out the recovery bugs.
**Requirements**: ISO-08, ISO-09, ISO-10, REL-10, REL-12, E2E-04
**Success Criteria** (what must be TRUE):

  1. Isolation survives a service restart: derived ArcadeDB credentials and per-identity databases reattach to the right identity, never to another. `internal/arcadedb/tenant.go` derives the password by HMAC over `AURA_ARCADEDB_TENANT_SECRET`, so a misbinding here is silent, not loud.
  2. `dr-report.json` is produced for the first time and passes: Postgres, sidecars, Garage and tenant-shaped ArcadeDB memory all restored and checksum-verified.
  3. `rollback-report.json` is produced for the first time and passes: distinct image digests, the previous config boots, migrations stay compatible, and the candidate is restored healthy.
  4. The full restart → rollback → restore cycle runs with two provisioned identities and both are intact and correctly separated afterwards — each resumes a real conversation and sees only its own history and its own memory.
  5. Deprovisioning one identity removes its data from every plane — Postgres rows, ArcadeDB database and server user, Garage bucket, sandbox, skills root — and leaves the other identity untouched.

**Closes on (live run)**: `make restore-drill` and `scripts/rollback_rehearsal.py` against the two-identity deployment, bracketed by real turns: a scored conversation as each identity before the drill, then daemon restart, image rollback to the previous digest, restore from backup, then the same two conversations again — asserting each identity recovers its own history and neither has acquired the other's. Deprovision closes the run.
**Plans**: TBD

### Phase 6: A Stranger Can Install and Operate It

**Goal**: Someone who has never read this codebase installs, secures, upgrades, backs up, restores and troubleshoots Aura from the written documents alone.
**Depends on**: Phase 5 — DOC-02 documents the roles phase 2 created, DOC-04's steps must be the ones the DR gate in phase 5 actually exercised, and DOC-03's rollback path is phase 5's rehearsal. Written earlier, these document intentions rather than measured behaviour, which is the failure this milestone exists to stop.
**Requirements**: DOC-01, DOC-02, DOC-03, DOC-04, DOC-05, DOC-06, DOC-07, DOC-08
**Success Criteria** (what must be TRUE):

  1. A self-hoster gets from nothing to a running Aura on their own hardware following the install guide and the README quick start — prerequisites stated, every required secret explained — verified by doing it on a clean machine, not by reading it.
  2. The same walkthrough turns on isolation, provisions a second identity, assigns roles, and the reader can tell from the document what each role may do.
  3. Upgrading between two released versions succeeds by following the upgrade guide, including migrations, and the documented rollback path recovers a deliberately broken upgrade.
  4. The backup and restore guide is executed by hand and produces the same result as the DR gate — the steps in the document are the steps the gate exercises, not a parallel description of them.
  5. The troubleshooting guide covers the failures actually met during phases 1–5, each with symptom, confirmation and fix; every key an operator must set is documented and the rest are discoverable (338 `AURA_*` keys exist against roughly 60 documented); and every `amendment #N` citation in the repo resolves — `2079e2fa7` dropped all 198 numbered amendments while 344 files still cite one, including `internal/skills/writer.go` (#97), `internal/llm/config.go` (#54), `cmd/aura/skills_roots.go` (#214) and CLAUDE.md's own rules (#177, #203).

**Closes on (live run)**: a clean-machine walkthrough — a fresh host with nothing but the repository and the docs, driven only by what is written: clone, install, first conversation, enable isolation, provision a second identity, assign roles, upgrade, break the upgrade and roll back, back up, restore, and hold a real conversation as each identity afterwards. Every deviation from the text is a documentation defect; the walkthrough restarts after each fix. The prior parallel session already rewrote README, `docs/ARCHITECTURE.md`, `docs/CAPABILITIES.md`, `docs/TECHNICAL_OVERVIEW.md` and added `docs/BACKUP-RESTORE.md`, so DOC-01..DOC-05 start partly advanced — this phase verifies and completes them, it does not start from a blank page.
**Plans**: TBD

### Phase 7: One SHA, Twelve Reports, One Window

**Goal**: The candidate commit carries all twelve production-readiness reports, produced together, and the release workflows publish it.
**Depends on**: Phase 6 — and this is the sequencing constraint that shapes the whole roadmap. `make release-readiness` accepts only reports bound to the exact `git rev-parse HEAD` and newer than 24 hours (`docs/release-readiness.md`), so every report an earlier phase produced is stale and SHA-mismatched the moment the next phase commits. Phases 2–5 prove each gate *can* pass and fix what it breaks; this phase produces them *together*, on a frozen tree, in one continuous window during which nothing may commit. It must be last because any commit after it invalidates the whole bundle.
**Requirements**: REL-01, REL-02, REL-03, REL-05, REL-07, REL-13, REL-14, E2E-05
**Success Criteria** (what must be TRUE):

  1. `make evidence-contracts` passes on the candidate commit, so every report's shape is validated before its content is trusted.
  2. `security-report.json` is produced for the first time and passes: exact-SHA CodeQL for Go and JS, govulncheck, workflow pinning and strict-profile tests.
  3. `coverage-report.json` (≥85% statements on the owned surface with the `db_integration` tier, no empty or filtered tier), `agent-memory-eval-report.json` (all-tier MRS with ArcadeDB package coverage ≥85%) and `capability-eval.json` (every declared scenario executed and passed, zero skipped or missing) are all re-produced fresh on the candidate SHA — the copies on disk today are dated 2026-09-07, 2026-09-07 and 2026-08-24 and none of them survives this phase's freeze.
  4. `make release-readiness` emits `release-readiness-report.json` accepting all twelve inputs, each bound to the exact candidate SHA and under 24 hours old, with the SHA-256 of every input recorded.
  5. The `Production Readiness` GitHub workflow completes on the candidate branch, the tag-triggered `Release` workflow publishes against that exact commit, and every phase in this milestone has its own live end-to-end run recorded in its phase directory — no phase closed on unit evidence alone.

**Closes on (live run)**: one continuous window on a frozen tree. `make evidence-contracts`, then all twelve reports produced against the live stack in sequence — security, coverage, docker-coverage, agent-memory, mutation, capability, load, chaos, DR, observability, rollback, audit-closure — then `make release-readiness`, then the `Production Readiness` workflow on the candidate branch and the tag-triggered `Release`. The window is the run: if it takes longer than 24 hours, the earliest reports expire and it starts again.
**Plans**: TBD

## Dependency Order

```text
1 Two identities live ──▶ 2 Permissions ──▶ 3 Attack ──▶ 4 Load/chaos/obs ──▶ 5 Lifecycle ──▶ 6 Docs ──▶ 7 Release window
```

Strictly sequential, and each edge earns its place:

| Edge | Why it cannot be reversed or parallelised |
|---|---|
| 1 → 2 | A permission check with one principal always passes. RBAC-08 has nothing to administer until a second identity can be provisioned. |
| 2 → 3 | The attack suite must probe permission refusals as well as ownership scoping. Written first, it tests half the boundary and gets rewritten. |
| 3 → 4 | Load that passes by leaking across identities is worse than load that fails. The boundary must hold before concurrency means anything. |
| 4 → 5 | Restore needs real two-identity state to restore. Process-kill recovery (chaos) flushes out the cheap recovery bugs before the expensive drill. |
| 5 → 6 | DOC-04 must document the steps the DR gate exercised, DOC-03 the rollback that was rehearsed, DOC-02 the roles that exist. Documenting intentions is the failure mode this milestone exists to end. |
| 6 → 7 | **Hard constraint.** The twelve reports must share one SHA and one 24-hour window. Any commit after the window invalidates the bundle, so the window must come after the last commit — which is phase 6's documentation fixes. |

**Why the twelve reports are not one phase.** Eight have never been run and are expected to
break. Producing them for the first time inside the 24-hour window would mean fixing code
during the window, which resets the SHA and voids everything produced so far. So phases 2–5
each execute the reports their own work touches — mutation with RBAC, docker-coverage with
the sandbox, load/chaos/observability with concurrency, DR/rollback with the lifecycle — and
close what they break. Phase 7 then re-runs everything on a frozen tree, where a failure is
news rather than the expected case.

## Notes

**Frontend work is Phase 2's, not a phase of its own.** The cockpit already has an admin
section (`web/src/admin/adminApi.ts`, `AdminSection.tsx`, `useAdmin.ts`) that lists the
identity roster with each identity's grants and calls
`POST`/`DELETE /api/admin/identities/{id}/capabilities`. Measured live on 2026-09-07 under
Settings → Identity and permissions: a four-step "Create identity" wizard
(Credentials → Capabilities → …) and per-grant Revoke are already there, so creation exists
too — an earlier draft of RBAC-11 claimed it did not. What remains is smaller in one place and
larger in another: the wizard's capability step loses its per-capability choices, because every
identity now receives the same non-administrative set and the administrative pair is never
grantable at all, while the section gains a removal action that runs the reverse saga and a credit
panel showing each identity's cap, remaining and spend with the control to change the cap.
That is Phase 2's work, extending a surface that works rather than opening a second one. No other phase delivers frontend work, and no
separate UI phase exists. `frontend` appears in REL-06 as one of the five existing mutation
scopes, and the web E2E suite runs as existing evidence.

**Gates binding every phase** (CLAUDE.md, not restated per phase): coverage floor 85% across
the full tag matrix; mutation ≥70% on each phase's critical files; no non-test Go file over
600 LOC (ten files are within 20 lines of it — split before editing, not after the gate
fires); `go vet` / `build` / `test` / `-race` green per touched package; no skip-as-green in
CI. Migration numbers are assigned at landing time via `ls internal/db/migrations/ | tail -1`,
never deduced from a phase number.

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5 → 6 → 7

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Two Identities, Live and Separated | 7/7 | In Progress|  |
| 2. Permissions Decide What a User May Do | 3/10 | In Progress|  |
| 3. The Boundary Under Attack | 0/TBD | Not started | - |
| 4. Load, Chaos and Truthful Degradation | 0/TBD | Not started | - |
| 5. Restart, Rollback, Restore | 0/TBD | Not started | - |
| 6. A Stranger Can Install and Operate It | 0/TBD | Not started | - |
| 7. One SHA, Twelve Reports, One Window | 0/TBD | Not started | - |

---
*Roadmap created 2026-09-07 for milestone v1.1.0 Production Launch — Multi-Tenant.
47 v1 requirements, 47 mapped, 0 orphaned.*
