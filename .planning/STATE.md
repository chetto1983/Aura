---
gsd_state_version: "1.0"
milestone: v1.1.0
milestone_name: Production Launch — Multi-Tenant
status: planning
last_updated: "2026-09-07T00:00:00.000Z"
last_activity: 2026-09-07
progress:
  total_phases: 7
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-09-07)

**Core value:** When Aura says she did something, she did it — and she can find what she knew.
**Current focus:** Phase 1 — Two Identities, Live and Separated

## Current Position

Phase: 1 of 7 (Two Identities, Live and Separated)
Plan: — (not yet planned)
Status: Ready to plan
Last activity: 2026-09-07 — Roadmap created; 47 v1 requirements mapped across 7 phases

Progress: [░░░░░░░░░░] 0%

## Milestone Shape

Seven phases, strictly sequential. Every phase closes on a real end-to-end run against the
live stack, driven by the real agent — never on unit evidence (E2E-05). Few substantial
phases, not many thin ones.

| Phase | Closes on |
|-------|-----------|
| 1. Two Identities, Live and Separated | `musr_e2e` against a live `aura serve` + two concurrent scored conversations |
| 2. Permissions Decide What a User May Do | Live permission matrix — five call sites × two identities, denials read back from the audit trail |
| 3. The Boundary Under Attack | Adversarial suite + sandbox escape battery against the live two-identity stack |
| 4. Load, Chaos and Truthful Degradation | `make load-chaos` + `make observability-evidence` with two identities active |
| 5. Restart, Rollback, Restore | `make restore-drill` + `rollback_rehearsal.py`, bracketed by real turns per identity |
| 6. A Stranger Can Install and Operate It | Clean-machine walkthrough driven only by the written docs |
| 7. One SHA, Twelve Reports, One Window | Twelve reports on a frozen tree in one <24h window, then the release workflows |

## Performance Metrics

**Velocity:**
- Total plans completed: 0
- Average duration: —
- Total execution time: —

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

**Recent Trend:**
- Last 5 plans: —
- Trend: —

*Updated after each plan completion*

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. Decisions taken during roadmap
creation:

- **Roadmap**: Phase numbering restarts at 1. The previous milestone's phase directories were
  deleted and its numbering (45–54) is not carried forward.
- **Roadmap**: The twelve release reports are deliberately NOT one phase. Eight have never
  been run and are expected to break; fixing code inside the 24-hour window would reset the
  candidate SHA and void the bundle. Phases 2–5 each execute the reports their own work
  touches and close what they break; phase 7 re-runs everything on a frozen tree.
- **Roadmap**: RBAC is wiring, not a build — `github.com/Authula/authula@v1.43.0` already
  ships `plugins/access-control/` with handlers, repositories, role hierarchy and its own
  migration set. Aura imports Authula today only for jwt/totp/session/email-password/csrf/
  rate-limit.
- **Roadmap**: No UI phase. Cockpit UI for role management is explicitly out of scope; the
  API is the contract for this milestone.

### Pending Todos

See `.planning/todos/`.

### Blockers/Concerns

Carried in from `.planning/codebase/CONCERNS.md` (measured 2026-09-07) — these are the
findings the roadmap's phases are expected to collide with, named so a phase does not
rediscover them:

- **Phase 3 will meet these three.** The shared package caches (`aura-uv-cache`,
  `aura-npm-cache`, `aura-pip-cache`) are mounted read-write into *every* identity's sandbox
  — a cross-tenant code-execution channel that survives container destruction
  (`internal/sandbox/usersandbox/translate.go:20-27`). `CapDrop: []string{}` with no
  `SecurityOpt` and no `User`, so the workload is root with Docker's full default caps.
  `internal/skills/installer.go:378` hands `os.Environ()` to `npx skills add`; the fix,
  `secret.InstallerEnv`, already exists at `internal/mcp/process_env.go:68` and was applied
  to the sibling MCP installer but never to skills.
- **Phase 2 will meet this.** `internal/approvalgrants` — the durable standing-approval store
  — has no test file at all (4/57 = 7.0%, the lowest in the repo), and it is exactly the
  package RBAC-07 lands on.
- **Phase 2 will meet this.** `internal/webauth` sits at 187/360 = 51.9%; the uncovered half
  is the rejection paths in session validation, OAuth token verification and identity linking.
- **Phase 5 will meet this.** There is no rotation path for `AURA_ARCADEDB_TENANT_SECRET`:
  passwords are derived by HMAC, ArcadeDB cannot re-scope an existing user, and a rotation is
  an untested `ALTER USER` sweep across all tenants.
- **Before phase 1 starts.** The working tree carries substantial uncommitted production code
  in `internal/arcadedb/` (`memory_graph_temporal.go`, `memory_mentions_read.go`, 18 modified
  tracked files, 12 untracked test files). No gate has run against it. Land or stash it before
  a phase begins — a candidate SHA cannot be frozen over ambient uncommitted state.
- **Standing.** Ten non-test Go files are within 20 lines of the 600-LOC ceiling and
  `internal/askuser/store.go` is exactly at it. Split before editing, not after the gate
  fires.

## Deferred Items

| Category | Item | Status | Deferred At | Milestone |
|----------|------|--------|-------------|-----------|
| Isolation | ISO-11 — one process per identity, if execution separation proves insufficient in ISO-05 | Deferred to v2 | 2026-09-07 | v1.1.0 |
| Isolation | ISO-12 — per-identity resource quotas configurable by the operator | Deferred to v2 | 2026-09-07 | v1.1.0 |
| Access control | RBAC-11 — per-resource ownership delegation | Deferred to v2 | 2026-09-07 | v1.1.0 |
| Access control | RBAC-12 — role assignment through the cockpit UI | Deferred to v2 | 2026-09-07 | v1.1.0 |

## Session Continuity

Last session: 2026-09-07
Stopped at: ROADMAP.md written, STATE.md initialised, REQUIREMENTS.md traceability filled.
Resume file: None

Next: `/gsd-plan-phase 1`
