# Phase 29: Governance Write — MCP Configuration + Skills Install - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-20
**Phase:** 29-governance-write-mcp-configuration-skills-install
**Areas discussed:** Write-endpoint REST shape, mcp_audit + env-edit path, skills.sh transport, Write UI + approval reuse, deep-research/UI directive

---

## Area selection

| Option | Description | Selected |
|--------|-------------|----------|
| Write-endpoint REST shape | Verb-on-resource vs explicit action sub-paths | ✓ |
| mcp_audit + env-edit path | New 0022 ledger + in-place env-edit | ✓ |
| skills.sh transport | npx skills find/add wiring, toggle, gate | ✓ |
| Write UI + approval reuse | Extend Phase-28 tabs vs separate surface; approval-queue reuse | ✓ |

**User's choice:** All four areas + free-text addition: *"deep research online best 2026 industrial UI/UX pattern and on D:/tmp"* (a binding researcher directive, not a fork — captured as a `<specifics>` mandate + `/gsd-ui-phase 29` hint).

---

## Write-endpoint REST shape

| Option | Description | Selected |
|--------|-------------|----------|
| Resource + named action sub-paths | Reuse Phase-28 prefixes; POST /api/governance/mcp, PATCH .../{name}/env, POST .../{name}/trust, .../{enable|disable}, DELETE .../{name}; skills install/restore/archive; each behind RequireCapability; one action = one audit row | ✓ |
| Pure REST CRUD verbs | PUT/PATCH/DELETE, lifecycle as PATCH state field — trust/restore/archive don't map cleanly, fragile to audit | |

**User's choice:** Resource + named action sub-paths (recommended).
**Notes:** Clearest log/audit mapping; matches the Phase-28 read prefixes + the `POST /agent/run` / `POST /api/onboarding/start` mutation-behind-capability precedent. → CONTEXT D-01.

---

## mcp_audit ledger + in-place env-edit contract

| Option | Description | Selected |
|--------|-------------|----------|
| identity_id actor, atomic tx, secret-preserving whole-write | 0022 mirrors 0010/0021; actor=identity_id; one row/mutation (+reason on trust); mutation+audit atomic in one db.WithTx; env-edit = load→set→whole-write via SaveManagedConfig; redacted-placeholder secret preserved | ✓ |
| Authula user id as actor | Record auth-layer user id instead of aura identity_id — diverges from identity_audit 0021 | |
| You decide / planner | Lock only mirror-0010/0021 + atomic + secret-preserving | |

**User's choice:** identity_id actor, atomic tx, secret-preserving whole-write (recommended).
**Notes:** Consistent with the identity_audit 0021 precedent + the no-escalation principal. → CONTEXT D-02..D-05.

---

## Skills.sh discovery + install transport

| Option | Description | Selected |
|--------|-------------|----------|
| Host exec + --ignore-scripts + Writer gate, server-flag toggle | Run npx on host, ALWAYS --ignore-scripts, server flag toggle, body through Writer gate | ✗ (REJECTED) |
| Run inside the sandbox-agent | Execute in sandbox for isolation; needs body-extraction plumbing | |
| You decide / planner | Lock only --ignore-scripts + gate + toggle + RISKY | |
| **(operator override)** Run in Aura's existing container, scripts ALLOWED, no forced --ignore-scripts | Container IS the isolation boundary; risk control = approval gate + Writer validation, not script-disabling; install still RISKY + gated + Writer-validated; external discovery behind explicit toggle | ✓ |

**User's choice (after correction):** Operator REJECTED "always --ignore-scripts" — *"always --ignore-scripts is stupid not do in this way"* — and on follow-up: *"aura already run on container."*
**Notes:** Forcing `--ignore-scripts` cripples legitimate skills and is security theater when the whole process is already containerized. The container Aura runs inside is the blast-radius boundary; scripts run; the control is the RISKY approval gate + the Writer validation (sanitized env / SKILL.md parse / body cap / injection blocklist / sanitized name/path) + the canonical ContentHash. **This deviates from the LOCKED 29-SPEC (SKW-01 checklist item #1, the "incl. --ignore-scripts" constraint, prohibition #5) → BLOCKING SPEC-amendment the planner lands FIRST (CLAUDE.md PRD-first; Phase-28 D-07 pattern).** → CONTEXT D-06..D-09.

---

## Write UI placement + skill-approval queue

| Option | Description | Selected |
|--------|-------------|----------|
| Extend Phase-28 tabs in place; reuse the unified approval center | Write controls onto the existing governance MCP/Skills tabs; RISKY skill actions → Phase-25 /api/approvals cross-thread queue (Interrupt/Resume[] + resume token); MCP trust-approve = inline operator action | ✓ |
| Separate governance-local approval list | A governance-only pending list distinct from the chat approval center — two surfaces, duplicated plumbing | |
| You decide / planner | Lock only extend-in-place + reuse Interrupt/Resume[] | |

**User's choice:** Extend Phase-28 tabs in place; reuse the unified approval center (recommended).
**Notes:** One unified approval center "like Claude Code", cross-thread badge already built; no model-facing approve; pending inert. → CONTEXT D-10..D-12.

---

## Claude's Discretion

- `governance.write` capability string (parity with agent.run / identity.create; already in auth_test.go:494).
- Destructive/denied MCP tool allowlist behavior (Phase-16 mount-time risk policy, unchanged) + fail-soft probe row isolation (Phase-28 contract).
- REST DTOs, pagination defaults, validation error shapes, recipe RequiredEnv → guided-form generation, duplicate-name rejection, interrupted-install atomicity.
- web/src/governance/ write-component layout, lazy-chunk boundaries, empty/loading/error states, desktop+mobile breakpoints, i18n en+it keys.

## Deferred Ideas

- Scheduler write (cancel/run-now/approve/create via HTTP) → v2 (GOVW-03).
- `ui_control` / operator-OS shell → v2 (SHELL).
- New core MCP/skill capability (transport / execution model / gate logic).
- skills.lock.json computedHash interop (Aura canonical hash only).
- OAuth dynamic client registration for HTTP MCP (Phase-16 deferral).
- The model's ungated in-sandbox self-extension — left as-is.
- Multi-user RBAC beyond capability_grants — post-v1.0.0.
