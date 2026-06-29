# Phase 28: Governance Boards + Web Onboarding - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-19
**Phase:** 28-governance-boards-web-onboarding
**Areas discussed:** Board nav home, Onboarding wizard surface, Identity provisioning flow, Phase 30 roadmap

---

## Board nav home

### Nav placement

| Option | Description | Selected |
|--------|-------------|----------|
| One Governance workspace | A single new `governance` mode with 3 internal tabs (MCP/Skills/Scheduler). Minimal nav footprint. | ✓ |
| Under Settings mode | Reuse the placeholder `settings` mode; governance as sections inside it. | |
| Three separate modes | Promote each board to its own top-level mode. | |

### Board layout

| Option | Description | Selected |
|--------|-------------|----------|
| Master list + detail | Dense rows; selecting a row opens a detail/inspector pane (doctor output, run history, audit). | ✓ |
| Flat table only | One dense table with inline status chips, no drill-down. | |
| Card grid | Each item a card in a responsive grid. | |

**User's choice:** One Governance workspace (3 tabs) + master-list + detail per board.
**Notes:** Consistent with minimal-industrial shape; mirrors the scheduler task→run-history + Graph-Explorer inspector pattern.

---

## Onboarding wizard surface

### Transport

| Option | Description | Selected |
|--------|-------------|----------|
| REST per-step JSON | POST step {intent,text} → {content,step,status,draft}. Matches SPEC text/JSON; LoopAgent is request/response. | ✓ |
| Hybrid (REST + SSE draft) | REST steps, SSE for the final draft generation. | |
| Full SSE stream | Whole wizard over an SSE run like chat. | |

### Wizard flow

| Option | Description | Selected |
|--------|-------------|----------|
| One full-screen wizard, all-in | Dedicated wizard provisions identity (login+grants+Telegram) AND runs the 5-step interview in one linear flow. | ✓ |
| Full-screen wizard, interview optional | Same wizard, interview as a skippable final stage. | |
| Tab in Governance workspace | Onboarding as a 4th Governance tab. | |

**User's choice:** REST per-step JSON + one dedicated full-screen wizard, all-in.
**Notes:** Matches the SPEC goal "seed Agent.md AND fully provisions"; the `prompted` flag preserves no-duplicate-LLM-turn.

---

## Identity provisioning flow

### Credential mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Operator sets email + temp password | Operator types creds; server-side Authula create (bypassing DisableSignUp route); TOTP on first login. | ✓ |
| System-generated one-time password | Operator types email only; system shows a strong password once for out-of-band delivery. | |
| You decide | Let research/planner pick. | |

### Capability picker

| Option | Description | Selected |
|--------|-------------|----------|
| Checklist of creator's own grants | Creator's grants (minus `*`) as a checklist; tick a subset. No-escalation by construction. | ✓ |
| Role presets | Predefined bundles, each a subset of creator grants. | |
| Minimal default + expand | Safe minimal grant default + an advanced add section. | |

### Multi-user reconciliation

| Option | Description | Selected |
|--------|-------------|----------|
| Accept + record PRD amendment | Phase 28 introduces multi-operator login; relax the `OperatorUserID` guard, add server-side user-create, record a PROJECT.md/ROADMAP PRD-amendment. Authz stays capability_grants. | ✓ |
| Loginable but minimal | Make login work but keep authz at capability_grants only, no PROJECT.md scope change beyond a note. | |
| Flag for separate decision | Defer to planner/researcher with options. | |

**User's choice:** Operator sets email + temp password; checklist of creator's own grants; accept multi-user + record the PRD amendment.
**Notes:** The PRD-amendment commit MUST land before provisioning impl (CLAUDE.md PRD-first). Atomicity is a cross-store saga (Authula's own pool), not a single PG tx — flagged for the planner (D-07b).

---

## Phase 30 roadmap

### Disposition

| Option | Description | Selected |
|--------|-------------|----------|
| Mark absorbed/done + pointer | Leave the entry, mark ✅ absorbed-into-28; 30-SPEC → tombstone → 28 ONBD-01b. Preserves traceability. | ✓ |
| Remove Phase 30 entirely | Delete the entry, rows, and 30-SPEC.md. | |
| Thin stub | Keep a minimal Phase 30 for residual scope. | |

### Timing

| Option | Description | Selected |
|--------|-------------|----------|
| Defer to planner | Record as a required pre-impl amendment; planner lands ROADMAP + PROJECT.md edits via gsd tooling. | ✓ |
| Do it now | Run /gsd-phase to amend Phase 30 immediately. | |

**User's choice:** Mark absorbed/done + pointer; defer execution to the planner.
**Notes:** Bundle the Phase-30 ROADMAP/SPEC edit with the D-07 multi-user PRD-amendment commit. No direct ROADMAP Write (anti-pattern #15).

---

## Claude's Discretion

- Live MCP probe concurrency + per-server timeout value + status caching + isolated-row failure rendering.
- Exact REST shapes for `/api/governance/*`, `/api/onboarding/*`, and the provisioning create mutation; pagination defaults.
- Immutable identity-create audit-row shape (reuse skill_audit-style ledger or a new `aura.identity_audit`).
- Wizard session resume/abandonment TTL + atomic rollback semantics.
- `web/src/governance/` + `web/src/onboarding/` component layout, lazy-chunk boundaries, empty/loading/error states, desktop + mobile breakpoints.
- i18n keys (en+it) for all new copy.

## Deferred Ideas

- Governance WRITE surfaces → Phase 29 (reuses the Phase-25 Interrupt/Resume protocol).
- Scheduler write → v2 (GOVW-03).
- `ui_control` operator-OS shell → v2 (SHELL).
- Email-invite / self-service signup (no mailer wired).
- Full multi-user RBAC / route-scoping / session isolation → post-v1.0.0 (OQ-8).
- `:9081` loopback setup wizard refactor → out of scope.

## Operator directive (carried to CONTEXT.md)

- **Deep-research mandate** (online + `D:/tmp`): industrial admin/console + onboarding-wizard patterns, desktop AND mobile, industrial-grade design. Warrants `/gsd-ui-phase 28` + a DEEP-RESEARCH pass in the researcher.
