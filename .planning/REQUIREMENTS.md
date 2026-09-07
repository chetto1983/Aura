# Requirements: Aura — v1.1.0 Production Launch — Multi-Tenant

**Defined:** 2026-09-07
**Core Value:** When Aura says she did something, she did it — and she can find what she knew.

Every requirement below is written so a machine or a live run can decide it. A requirement whose
only evidence is a checkbox is not done, and this milestone inherits nothing from v2.1.0 to prove
that rule matters: that milestone's own audit found 24 of 61 requirements marked complete with no
verification artifact behind them.

## v1 Requirements

### Release Gate (REL)

`docs/release-readiness.md` already defines the contract and `scripts/release_readiness_gate.py`
already enforces it. Every make target and every evidence script exists. None of the eight missing
reports has ever been produced, so the work is executing them against a live stack and closing what
they break — not writing them.

- [ ] **REL-01**: `make evidence-contracts` passes on the candidate commit, so every report's shape is validated before its content is trusted
- [ ] **REL-02**: `security-report.json` is produced and passes — exact-SHA CodeQL for Go and JS, govulncheck, workflow pinning, strict-profile tests
- [ ] **REL-03**: `coverage-report.json` passes at ≥85% statements on the owned surface with the `db_integration` tier, no empty or filtered tier
- [ ] **REL-04**: `docker-coverage-report.json` passes at ≥85% merged statements for the owned sandbox surface under native `docker_integration`
- [ ] **REL-05**: `agent-memory-eval-report.json` passes all-tier MRS with ArcadeDB package coverage ≥85%
- [ ] **REL-06**: `mutation-report.json` shows ≥70% killed separately for gateway, identity, profile, sandbox and frontend
- [ ] **REL-07**: `capability-eval.json` executes and passes every declared scenario, with zero skipped or missing
- [ ] **REL-08**: `load-report.json` meets the declared supported concurrency with success ratio and p95 inside budget, measured with at least two identities active
- [ ] **REL-09**: `chaos-report.json` executes the DB, MCP, Garage and process-kill scenarios, degrades truthfully and recovers
- [ ] **REL-10**: `dr-report.json` restores Postgres, sidecars, Garage and tenant-shaped ArcadeDB memory, checksum-verified
- [ ] **REL-11**: `observability-report.json` passes negative fixtures, runtime smoke, live health and readiness, dashboards, alerts and runbooks
- [ ] **REL-12**: `rollback-report.json` proves distinct image digests, previous config boots, migrations stay compatible and the candidate is restored healthy
- [ ] **REL-13**: `make release-readiness` emits `release-readiness-report.json` accepting all twelve inputs, each bound to the exact candidate SHA and under 24 hours old
- [ ] **REL-14**: The `Production Readiness` GitHub workflow completes on the candidate branch and the tag-triggered `Release` workflow publishes against that exact commit

### Access Control (RBAC)

Aura already has an authorization model and it is not Authula's. `aura.capability_grants`
(migration `0004_identity`, RLS fail-closed since `0087`) stores `(identity_id, capability)`;
`RequireCapability` (`internal/agui/auth.go:245`) is the HTTP gate; `governance.write`,
`governance.read`, `identity.create`, `agent.run` and `share.public` are enforced today; and
`web/src/admin/` already grants and revokes them through `/api/admin/identities/{id}/capabilities`.
Authula stays what it already is here — authentication — and authorization stays Aura's. Adding a
second permission system would be the exact fault that put `euroteltr/rbac` out of scope.

One thing must be fixed before any new capability is added. `HasCapability` resolves
`capability = '*' OR capability = $2`, and the bootstrap operator carries a seeded `*`
(`0004_identity.up.sql:31`, `cmd/aura/serve_bootstrap.go:258`). A capability added under that
wildcard is granted to the operator before anyone grants it — a gate that ships open. This is not a
new discovery: migration `0099` refused to put approval scopes in this table for exactly that
reason, and `0026` already made `local`'s grants explicit so the admin contract would "survive any
future narrowing of the wildcard". The narrowing is this milestone's. The blast radius is small and
measured: only the two bootstrap paths mint `*`, and every onboarding-provisioned identity is
already refused it in two places (`onboarding_session.go:14` declares the no-escalation rule,
`onboarding_provision.go:497` enforces it server-side).

- [ ] **RBAC-01**: The `*` wildcard is retired. Bootstrap mints the explicit capability set instead, a migration replaces existing wildcard rows with that set, and `HasCapability` no longer expands `*`
- [ ] **RBAC-02**: Every capability Aura enforces is declared in one place with its meaning, so a reviewer can read the authorization surface without grepping call sites
- [ ] **RBAC-03**: A fresh install lands the first operator with the administrative capability set explicitly granted and auditable — no wildcard, no manual SQL
- [ ] **RBAC-04**: Installing or mounting an MCP server requires its capability, and is refused without it
- [ ] **RBAC-05**: Authoring, updating or installing a skill requires its capability, and is refused without it
- [ ] **RBAC-06**: Running a shell in the sandbox requires its capability, and is refused without it
- [ ] **RBAC-07**: Approving a destructive action requires its capability — an identity cannot approve an action it lacks the right to take
- [ ] **RBAC-08**: Administering other identities (provisioning, deprovisioning, granting) requires its capability, and an identity cannot grant itself a capability it does not hold
- [ ] **RBAC-09**: An authorization decision denies by default — an unknown capability, an unresolved principal or a store error refuses rather than admits
- [ ] **RBAC-10**: Every denial is auditable: who, which capability, which route, when — and the admin surface can read them back
- [ ] **RBAC-11**: The cockpit admin section creates an identity and grants its capabilities without leaving the UI, extending `web/src/admin/` rather than adding a surface beside it

### Isolation (ISO)

Each plane is already scoped per identity — documents filter in SQL, conversations carry RLS from
migration 0032, memory is one ArcadeDB database and derived credential per identity, objects are a
per-identity Garage bucket. What has never been established is that the boundary holds under two
concurrent users, under attack, across a restart, and at the process and host level.

- [ ] **ISO-01**: `AURA_MUSR_ISOLATION` is on in the shipped deployment profile, and provisioning a second identity succeeds through the documented path
- [ ] **ISO-02**: Two identities working concurrently cannot read each other's documents, conversations, turns, approvals, memory facts or objects
- [ ] **ISO-03**: A deliberate boundary-crossing attempt fails: guessed identifiers on every read endpoint, a shared link outside its grant, a tool given another identity's identifier
- [ ] **ISO-04**: A prompt-injection attempt to make the agent read or write another identity's memory fails, and the attempt is visible in the audit trail
- [ ] **ISO-05**: One identity's turn cannot observe or affect another's execution — context, tool state and in-flight results are separated, not merely row-filtered
- [ ] **ISO-06**: A resource exhausted by one identity (loop budget, sandbox, tokens) does not deny service to another
- [ ] **ISO-07**: No identity can reach the host from its sandbox: the Docker socket, the host filesystem outside its roots, and the environment of a launched stdio MCP server are all unreachable
- [ ] **ISO-08**: Isolation survives a service restart — derived credentials and per-identity databases reattach to the right identity, never to another
- [ ] **ISO-09**: Isolation survives an image rollback and a restore from backup, including tenant-shaped ArcadeDB memory
- [ ] **ISO-10**: Deprovisioning an identity removes its data from every plane, and leaves the other identities intact

### End-to-End Proof (E2E)

The governing rule of this milestone: a phase closes on a real run against the live stack, driven by
the real agent, integrated with the phases around it. CLAUDE.md sets the bar at >9.8 on a real
scenario. Unit tests are how we get there, never the evidence that we arrived.

- [ ] **E2E-01**: Two identities hold real conversations at the same time against one running stack, each doing useful work, and the run is scored ≥9.8
- [ ] **E2E-02**: A second identity is onboarded from zero to a useful conversation — memory database, object bucket, sandbox, skills root — with no manual step outside the documented path
- [ ] **E2E-03**: The adversarial scenario runs as a scripted suite an operator can re-run, not a one-off session, and every attempt is refused
- [ ] **E2E-04**: The full restart / rollback / restore cycle runs with two provisioned identities and both are intact and correctly separated afterwards
- [ ] **E2E-05**: Every phase in this milestone lands with its own live end-to-end run recorded, and no phase closes on unit evidence alone

### Launch Documentation (DOC)

Written for someone who has never read this codebase. The current `docs/` tree is development-facing
— audits, recon notes, evaluations, design records — and there is one operational runbook.

- [ ] **DOC-01**: An install guide takes a self-hoster from nothing to a running Aura on their own hardware, with prerequisites stated and every required secret explained
- [ ] **DOC-02**: The multi-user setup is documented: turning on isolation, provisioning identities, assigning roles, and what each role may do
- [ ] **DOC-03**: An upgrade guide covers moving between released versions, including migrations and the rollback path when an upgrade goes wrong
- [ ] **DOC-04**: A backup and restore guide covers Postgres, ArcadeDB per-identity memory, and objects — and its steps are the ones the DR gate actually exercises
- [ ] **DOC-05**: A troubleshooting guide covers the failures an operator will actually meet, each with the symptom, how to confirm it, and how to fix it
- [ ] **DOC-06**: The environment catalog is complete and honest — 338 `AURA_*` keys exist in the code today against roughly 60 documented; every key an operator must set is documented, and the rest are discoverable
- [ ] **DOC-07**: README's quick start is verified by following it on a clean machine, not by reading it
- [ ] **DOC-08**: Every `amendment #N` reference in the repo resolves. `2079e2fa7` consolidated `prd.md` from 12,739 lines to 554 and dropped all 198 numbered amendments; 344 files still cite one, including code comments that explain why the code is shaped as it is (`internal/skills/writer.go` #97, `internal/llm/config.go` #54, `cmd/aura/skills_roots.go` #214) and CLAUDE.md's own rules (#177, #203). Either an index maps each number to the commit that carries it, or the citations are rewritten — silence is not an option, because the reference reads as live

## v2 Requirements

Deferred. Tracked, not in this roadmap.

### Access Control

- **RBAC-11**: Per-resource ownership delegation (an identity granting another access to one document or conversation)
- **RBAC-12**: Role assignment through the cockpit UI rather than the API

### Isolation

- **ISO-11**: One process per identity, or an equivalent hard runtime boundary, if execution separation proves insufficient in ISO-05
- **ISO-12**: Per-identity resource quotas configurable by the operator

## Out of Scope

| Feature | Reason |
|---------|--------|
| `euroteltr/rbac` | In-memory on `sync.Map`, no persistence, last commit 2019 — and Aura already has a persisted authorization model (`aura.capability_grants` + `RequireCapability`) with a cockpit panel that grants against it. A second source of truth for permissions in one process is the fault, whichever library brings it |
| Authula's own access control (twelve `/access-control/*` endpoints, role hierarchy) | Capable and already in `go.mod`, but Aura's authorization is `aura.capability_grants` + `RequireCapability` — enforced today on five capabilities, RLS fail-closed, with a cockpit panel. Authula stays the authentication layer it already is; adopting its roles too would mean two permission systems in one process, the same fault as any other second engine |
| Casbin, OpenFGA, SpiceDB, Permify, Keto | Live and capable, but they replace a working, tested, enforced model rather than extend it, and they are sized for distributed authorization — not a self-hosted appliance with a handful of identities |
| Carrying over v2.1.0's open requirements | Closed without inheritance by operator decision. A defect that is real will resurface through this milestone's end-to-end runs, with fresh evidence rather than an inherited ledger |
| Hosted / SaaS offering | This milestone ships something others self-host. Running it as a service for third parties is a different threat model and a different operational commitment |

## Traceability

Every v1 requirement maps to exactly one phase. Mapped during roadmap creation, 2026-09-07.

| Requirement | Phase | Status |
|-------------|-------|--------|
| REL-01 | Phase 7 | Pending |
| REL-02 | Phase 7 | Pending |
| REL-03 | Phase 7 | Pending |
| REL-04 | Phase 3 | Pending |
| REL-05 | Phase 7 | Pending |
| REL-06 | Phase 2 | Pending |
| REL-07 | Phase 7 | Pending |
| REL-08 | Phase 4 | Pending |
| REL-09 | Phase 4 | Pending |
| REL-10 | Phase 5 | Pending |
| REL-11 | Phase 4 | Pending |
| REL-12 | Phase 5 | Pending |
| REL-13 | Phase 7 | Pending |
| REL-14 | Phase 7 | Pending |
| RBAC-01 | Phase 2 | Pending |
| RBAC-02 | Phase 2 | Pending |
| RBAC-03 | Phase 2 | Pending |
| RBAC-04 | Phase 2 | Pending |
| RBAC-05 | Phase 2 | Pending |
| RBAC-06 | Phase 2 | Pending |
| RBAC-07 | Phase 2 | Pending |
| RBAC-08 | Phase 2 | Pending |
| RBAC-09 | Phase 2 | Pending |
| RBAC-10 | Phase 2 | Pending |
| RBAC-11 | Phase 2 | Pending |
| ISO-01 | Phase 1 | Pending |
| ISO-02 | Phase 1 | Pending |
| ISO-03 | Phase 3 | Pending |
| ISO-04 | Phase 3 | Pending |
| ISO-05 | Phase 1 | Pending |
| ISO-06 | Phase 4 | Pending |
| ISO-07 | Phase 3 | Pending |
| ISO-08 | Phase 5 | Pending |
| ISO-09 | Phase 5 | Pending |
| ISO-10 | Phase 5 | Pending |
| E2E-01 | Phase 1 | Pending |
| E2E-02 | Phase 1 | Pending |
| E2E-03 | Phase 3 | Pending |
| E2E-04 | Phase 5 | Pending |
| E2E-05 | Phase 7 | Pending |
| DOC-01 | Phase 6 | Pending |
| DOC-02 | Phase 6 | Pending |
| DOC-03 | Phase 6 | Pending |
| DOC-04 | Phase 6 | Pending |
| DOC-05 | Phase 6 | Pending |
| DOC-06 | Phase 6 | Pending |
| DOC-07 | Phase 6 | Pending |
| DOC-08 | Phase 6 | Pending |

### By Phase

| Phase | Name | Requirements | REQ-IDs |
|-------|------|--------------|---------|
| Phase 1 | Two Identities, Live and Separated | 5 | ISO-01, ISO-02, ISO-05, E2E-01, E2E-02 |
| Phase 2 | Permissions Decide What a User May Do | 11 | REL-06, RBAC-01, RBAC-02, RBAC-03, RBAC-04, RBAC-05, RBAC-06, RBAC-07, RBAC-08, RBAC-09, RBAC-10 |
| Phase 3 | The Boundary Under Attack | 5 | REL-04, ISO-03, ISO-04, ISO-07, E2E-03 |
| Phase 4 | Load, Chaos and Truthful Degradation | 4 | REL-08, REL-09, REL-11, ISO-06 |
| Phase 5 | Restart, Rollback, Restore | 6 | REL-10, REL-12, ISO-08, ISO-09, ISO-10, E2E-04 |
| Phase 6 | A Stranger Can Install and Operate It | 8 | DOC-01, DOC-02, DOC-03, DOC-04, DOC-05, DOC-06, DOC-07, DOC-08 |
| Phase 7 | One SHA, Twelve Reports, One Window | 8 | REL-01, REL-02, REL-03, REL-05, REL-07, REL-13, REL-14, E2E-05 |

**Coverage:**
- v1 requirements: 48 total
- Mapped to phases: 48
- Unmapped: 0 ✓
- Duplicated across phases: 0 ✓

---
*Requirements defined: 2026-09-07*
