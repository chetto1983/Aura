# Phase 29: Governance Write — MCP Configuration + Skills Install - Context

**Gathered:** 2026-06-20
**Status:** Ready for planning

<domain>
## Phase Boundary

The cockpit **WRITE** surface over the EXISTING backend: the Phase-16 MCP manager
control plane (`SaveManagedConfig`, `RedactEnv`, trust classes, mount-time risk
policy, `ProbeServer`) + the Phase-11 scoring-gated skill install/create/delete +
`ask_user` approval + append-only audit. The operator installs / env-edits /
trusts / enable-disable-removes MCP servers and installs / approves / restores /
archives skills **entirely from the web UI — no terminal, no hand-editing of
`servers.json` or any env file**.

This is the LAST milestone-v1.0.0 phase — the highest-risk write surfaces land
only after auth (Phase 24), the approval center (Phase 25), and the read-only
governance boards (Phase 28) are proven. Only **four thin gap-fillers** are new
code: (1) an append-only `aura.mcp_audit` ledger, (2) an in-place MCP env-edit
path, (3) skills HTTP write endpoints, (4) the `governance.write` capability.
No new core capability — no new MCP transport, no new skill execution model, no
new gate/scoring logic.

Requirements are **LOCKED by `29-SPEC.md`** (7 requirements, 14 acceptance
criteria, 8 prohibitions, ambiguity 0.146). This discussion captured *how* to
implement, not *what*.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**7 requirements are locked.** See `29-SPEC.md` for full requirements,
boundaries, and acceptance criteria. Downstream agents MUST read `29-SPEC.md`
before planning or implementing — requirements (MCPW-01..03, SKW-01..03, the
cross-cutting cockpit-driven/no-leak boundary) are **not** duplicated here.

**In scope (from SPEC.md):**
- Cockpit MCP write surface: recipe + custom-stdio install (CLI-equiv + managed-
  config destination preview), in-place env editing with redaction + four-state
  distinction (required/optional/missing/placeholder) + soft placeholder warning,
  reversible enable/disable, confirmed remove, cockpit trust-approval (populating
  the today-empty `ApprovedBy`/`ApprovedAt`/`Reason`).
- A new append-only `aura.mcp_audit` ledger (migration parallel to
  `skill_audit`/`identity_audit`) for install/edit/enable/disable/remove/trust.
- A thin in-place MCP env-edit backend path (load→set→whole-entry atomic write,
  credential-preserving) — the one MCP backend gap-filler.
- Cockpit skill write surface: install from a source field or skills.sh catalog
  search, routed through the existing Writer gate with the full validation
  checklist + content hash/preview + risk tier + destination surfaced.
- Skill approval queue reusing the HITL `Interrupt`/`Resume[]` protocol + resume
  token; restore/archive across active/pending/archived/audit tabs; the skills
  audit ledger surfaced with the new write rows.
- New skills.sh search + install + skill create/update/delete/restore/archive
  HTTP endpoints (the skills backend gap-filler) — wrapping the existing
  Writer/gate, not new gate logic.
- New authenticated `/api/*` mutating endpoints behind `RequireAuth` + a
  governance-write capability.
- i18n en+it; web ≥85% vitest + ≥70% Stryker on touched dirs; Go owned-surface
  ≥85%; Playwright e2e + contrast-check (WCAG AA) on new surfaces.

**Out of scope (from SPEC.md):**
- The model's ungated in-sandbox self-extension (`npx skills add`, no ceremony,
  directive 2026-06-05) — left exactly as-is; Phase 29 adds only the operator-
  gated cockpit path (clean two-path boundary).
- Scheduler write (cancel/run-now/approve/create via HTTP) → v2 (GOVW-03); the
  scheduler board stays read-only.
- `ui_control` / operator-OS shell (open_panel, set_mode, command palette,
  dockable windows) → v2 (SHELL).
- New core capability — no new MCP transport, no new skill execution model, no
  new gate/scoring logic (only the four named thin gap-fillers).
- skills.lock.json `computedHash` interop — Aura's canonical content hash only.
- OAuth dynamic client registration for HTTP MCP (Phase-16 deferral).
- The standalone `:9081` loopback setup wizard — unchanged.
- Multi-user RBAC beyond the existing `capability_grants`.
- OpenClaw plugin hosting (plugin-host ≠ MCP-config).

</spec_lock>

<decisions>
## Implementation Decisions

### REST shape for the write endpoints
- **D-01 — Resource + named action sub-paths under the Phase-28 read prefixes.**
  Each write is an explicit, named, individually-auditable action, NOT pure REST
  CRUD verbs (trust-approve / restore / archive don't map cleanly to HTTP verbs
  and would be fragile to audit per-action). Shape:
  - MCP: `POST /api/governance/mcp` (install recipe/custom-stdio),
    `PATCH /api/governance/mcp/{name}/env` (in-place env edit),
    `POST /api/governance/mcp/{name}/trust` (trust-approve),
    `POST /api/governance/mcp/{name}/{enable|disable}`,
    `DELETE /api/governance/mcp/{name}` (remove, behind confirmation).
  - Skills: `POST /api/governance/skills/install`,
    `POST /api/governance/skills/{name}/{restore|archive}`
    (+ create/update/delete per the SPEC skills gap-filler).
  - skills.sh search read: `GET /api/governance/skills/catalog?q=…`.
  Each mutating route registers behind `RequireAuth` **and**
  `RequireCapability("governance.write")`; **one named action = exactly one
  audit row.** Mirrors the Phase-28 read prefixes (`/api/governance/{mcp,skills,
  scheduler}`) and the `POST /agent/run` / `POST /api/onboarding/start`
  mutation-behind-capability precedent.

### `mcp_audit` ledger + in-place env-edit path
- **D-02 — New migration `0022_mcp_audit`, mirroring `0010`/`0021`.** Append-only
  via role grant (SELECT+INSERT only) + dual UPDATE/DELETE/TRUNCATE triggers (the
  proven `skill_audit`/`identity_audit` pattern; latest shipped migration is
  `0021_identity_audit`, so the new one is `0022`). Each of
  install/edit/enable/disable/remove/trust appends exactly one row.
- **D-03 — Actor = `identity_id` (the capability-layer principal).** The audit
  actor is the authenticated operator's `aura.identities` id — the SAME principal
  the `RequireCapability`/no-escalation check resolves — NOT the raw Authula user
  id (consistent with the `identity_audit` 0021 precedent). Row captures
  actor + timestamp + action; **+ reason on trust** (populates the today-empty
  `Trust.ApprovedBy`/`ApprovedAt`/`Reason`).
- **D-04 — Mutation + audit are atomic in one `db.WithTx`.** The config write and
  its audit row commit together or not at all (no audited-but-not-applied, no
  applied-but-unaudited).
- **D-05 — In-place env-edit = load → set one value → whole-entry atomic write
  via `SaveManagedConfig`** (temp+rename, `0o600`). An unchanged secret submitted
  as its **redacted placeholder** is **preserved**, never overwritten with the
  placeholder text (reuse `RedactEnv` / `ImportProfile` credential-preservation).
  A still-placeholder **required** recipe var raises a **soft warning** (save
  allowed; server stays blocked/unhealthy until filled). Deep-research verdict:
  this whole-write shape is the universal industrial pattern (Codex / Nanobot /
  Aura Phase-16 all whole-write) — reuse, don't invent.

### Skills.sh discovery + install transport
- **D-06 — Run `npx skills find`/`add -y` in Aura's existing runtime container;
  scripts ALLOWED (NO forced `--ignore-scripts`).** Operator directive
  (2026-06-20): *"always --ignore-scripts is stupid"* + *"Aura already runs on a
  container."* The container Aura already runs inside **IS** the blast-radius
  isolation boundary — there is no nested sandbox-agent jail for this path and no
  blanket script-disable. Forcing `--ignore-scripts` on every install would
  cripple legitimate skills that need a build/postinstall step and is security
  theater when the whole process is already containerized
  ([[feedback_never_run_exe_use_container]], [[feedback_aura_full_host_terminal_primary]]).
- **D-07 — The risk control is container-isolation + the approval gate + Writer
  validation, NOT script-disabling.** The fetched body ALWAYS routes through the
  existing Writer gate (`SanitizeName` `^[a-z0-9-]{1,64}$`, body cap, NFKC-
  normalized + case-folded injection blocklist with matched-position reporting,
  `SKILL.md` frontmatter parse, sanitized name/path, canonical `ContentHash`
  `sha256:`) and is staged to `pending/` as **RISKY supply-chain input** — install
  is still labelled RISKY (the objection was to the mechanism, not the risk
  label). Activation only via the approval resume (D-11).
- **D-08 — External skills.sh discovery behind an explicit operator-visible
  toggle.** A server-side flag (e.g. `AURA_SKILLS_EXTERNAL_DISCOVERY`) the cockpit
  toggle reflects; no silent discovery escalation (SPEC prohibition #8). Aura's
  canonical content hash only — NOT skills.lock.json interop (locale-sensitive).
- **D-09 — BLOCKING SPEC-AMENDMENT (planner lands FIRST, before impl).** D-06/D-07
  deviate from the LOCKED `29-SPEC.md`: SKW-01's validation checklist lists
  `--ignore-scripts` as item #1, the constraint *"Install is RISKY supply-chain:
  ... incl. `--ignore-scripts`"*, and prohibition #5 *"MUST NOT present an
  `--ignore-scripts` skill install as 'safe'"*. CLAUDE.md PRD-first principle
  requires a SPEC-amendment commit BEFORE the implementation (same discipline as
  Phase-28's D-07 PRD-amendment). The amendment: replace the `--ignore-scripts`
  validation-checklist item + constraint + prohibition with **"install isolation =
  Aura's container boundary; install scripts are permitted; install remains RISKY
  + gated (approval queue) + Writer-validated (the five remaining checklist items:
  sanitized env, `SKILL.md` parse, body cap, injection-literal blocklist,
  sanitized name/path)."** discuss-phase stays NON-mutating — the planner makes
  the SPEC edit via gsd tooling (never a direct SPEC Write).

### Cockpit write UI + approval-queue reuse
- **D-10 — Extend the Phase-28 governance MCP/Skills tabs IN PLACE with write
  controls.** Install panel (recipe form with `RequiredEnv` surfaced as a guided
  form / custom-stdio command+args+env, CLI-equiv + destination preview before
  save), env-edit form (four-state rendering + redacted chips + soft warning),
  trust-approve action, enable/disable toggle, confirmed remove, restore/archive
  buttons. NOT a separate write surface — one `governance` workspace to learn
  (minimal-industrial shape for the backend, premium bar for the UI).
- **D-11 — RISKY/DESTRUCTIVE skill actions reuse the Phase-25 `/api/approvals`
  cross-thread queue** via `Interrupt`/`Resume[]` + a resume token (one unified
  approval center "like Claude Code", cross-thread badge already built —
  [[feedback_cockpit_premium_bar_over_minimal]]). A pending skill is NOT runnable
  and NOT prompt-injectable; activation happens **only** on the approval resume —
  there is **no model-facing approve path**; a stale/expired/already-consumed
  resume token renders its terminal state with no silent activation.
- **D-12 — MCP trust-approve is an inline operator-direct action on the server
  row**, not a model-gated `ask_user` pause (it is an operator decision, not a
  model-initiated mutation). It populates `ApprovedBy`/`ApprovedAt`/`Reason` + an
  `mcp_audit` row and flips the server runnable.

### Claude's Discretion (planner/research-resolved — no further operator input)
- **`governance.write` capability string** — the new mutating-capability string,
  parity with `agent.run` / `identity.create`. Already referenced in
  `internal/agui/auth_test.go:494`. Lock as `governance.write` unless the planner
  finds a conflict. No operator can grant a capability they lack (reuse the
  existing `HasCapability` + `*`-rejection invariants).
- **Destructive/denied MCP tool allowlist behavior** — Phase-16 mount-time risk
  policy is UNCHANGED: with an allowlist present, a denied/destructive advertised
  tool is shown explicitly and is absent from the runtime registry before model
  reach; fail-soft mount warnings surface per server without stalling the board
  (reuses the Phase-28 bounded-timeout + row-isolation probe contract).
- **REST request/response DTOs, pagination defaults, validation error shapes**,
  the recipe `RequiredEnv` → guided-form generation, the duplicate-name rejection
  path, and the interrupted-install atomicity (atomic temp+rename leaves prior
  config intact).
- **`web/src/governance/` write-component layout** (install panel / env form /
  approval surface), lazy-chunk boundaries, empty/loading/error states,
  **desktop + mobile** breakpoints (held to the locked blue design system + the
  WCAG-AA contrast gate), and i18n keys (en + it) for every new string.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (researcher + planner) MUST read these before planning or
implementing.**

### Phase scope & requirements (LOCKED)
- `.planning/phases/29-governance-write-mcp-configuration-skills-install/29-SPEC.md`
  — **locked requirements** (MCPW-01..03, SKW-01..03, cross-cutting cockpit/no-leak),
  14 acceptance criteria, 20/20 edge coverage, 8 prohibitions. **Read before planning.**
- `.planning/ROADMAP.md` §"Phase 29: Governance Write — MCP Configuration + Skills
  Install" (~lines 260-282) + the dependency note (depends on Phases 24/25/28).
- `.planning/REQUIREMENTS.md` — the GOVW/MCPW/SKW rows.
- `.planning/PROJECT.md` §"Current Milestone v1.0.0" — the single-binary
  `//go:embed` invariant; the `capability_grants`-only authz mandate (no new RBAC).

### PRD-first amendment target (D-09 — BLOCKING, planner lands FIRST)
- `.planning/phases/29-governance-write-mcp-configuration-skills-install/29-SPEC.md`
  — amend SKW-01 validation-checklist item #1 (`--ignore-scripts`), the "Install is
  RISKY supply-chain … incl. `--ignore-scripts`" constraint, and prohibition #5,
  replacing script-disabling with **container-isolation + approval-gate + Writer
  validation** as the risk control (scripts permitted). Via gsd tooling, BEFORE the
  skills-install implementation. (CLAUDE.md PRD-first; pattern = Phase-28 D-07.)

### MCP manager backend (LOCKED shape — Phase 16, reuse, do not re-invent)
- `internal/mcp/` — `LoadManagedConfig` / `SaveManagedConfig`
  (`~/.aura/mcp/servers.json`, atomic temp+rename, `0o600`; `AURA_MCP_CONFIG` path
  override; the read-only `AURA_MCP_SERVERS_JSON` overlay), `BuiltInCatalog()`
  (4 recipes + the `RequiredEnv []string` field — defined but never validated, to
  be surfaced now), `RedactEnv` / `secret.IsSecretEnvKey` / `ImportProfile`
  credential-preservation, the trust classes
  (`trusted_recipe`/`trusted_local`/`sandboxed_local`/`remote_http`/`blocked`;
  custom defaults to `blocked`), `RunnableManagedServers` (silently skips blocked
  — now surface a warning), mount-time risk-policy (risk labels enforced before
  registry insert), `ProbeServer` / doctor live tool-count. Researcher: locate the
  exact files for the trust-approve write + the env-edit path + the `mcpDoctorAll`
  probe.

### Skills backend (LOCKED shape — Phase 11, reuse, do not re-invent)
- The skills Writer/Loader gate — `ComputeSkillTier` (delete=Destructive,
  create/update/install=Risky; `GateRecommended` for Risky/Destructive),
  `SanitizeName` `^[a-z0-9-]{1,64}$`, body cap, NFKC-normalized + case-folded
  injection blocklist (matched-position reporting), `SKILL.md` frontmatter parse
  (real YAML + CRLF normalization), canonical `ContentHash` (`sha256:` byte-sorted
  (relPath,bytes)).
- The `ask_user` pause (`ErrAwaitingUserInput` → `aura.paused_states`), the
  active/`pending/`/`archived/` dirs (loader scans active only — pending is
  non-runnable + never materialized/injected), `.usage.json`
  (last_used/use_count/status) + daily TTL sweep, restore/archive,
  `aura skills approve`.
- `aura.skill_audit` (migration `0010`) — append-only (role SELECT+INSERT + dual
  UPDATE/DELETE/TRUNCATE triggers, D-29 coherence CHECK); `AuditStore.ListAudit`.
- The spike-proven `npx skills find`/`add -y` transport (`-y` non-interactive;
  strip ANSI; provenance/installs in the body) — `Skill("spike-findings-Aura")`.

### Migration precedent (the `mcp_audit` model — D-02)
- `internal/db/migrations/0010_*` (skill_audit) + `internal/db/migrations/0021_identity_audit.{up,down}.sql`
  — the role grant + dual-trigger append-only pattern to mirror for `0022_mcp_audit`.

### Approval / HITL protocol (LOCKED — Phase 25, D-11)
- `internal/agui/approvals_api.go` — the cross-thread `/api/approvals` queue
  (`ListPendingAll` + accept/decline/cancel over a uuid resume token); the
  `Interrupt`/`Resume[]` protocol the skill-install approval extends.

### REST handler + route pattern (the model for the new write endpoints)
- `internal/agui/governance_api.go` + `internal/agui/governance_seam.go`
  (Phase 28 reads — `/api/governance/{mcp,skills,scheduler}`) — the thin-handler-
  over-provider shape + the read DTOs the writes extend.
- `internal/agui/conversations_branch_api.go` + `internal/agui/onboarding_api.go`
  — the mutation-behind-`RequireCapability` precedent (`/api/onboarding/start`
  behind `identity.create`).
- `internal/agui/auth.go` — `RequireAuth`, `RequireCapability` (the
  `capability_grants` check; `*` wildcard; the `governance.write` test at
  `auth_test.go:494`), `withPrincipal`.
- `internal/agui/server.go` `Mux()` + `cmd/aura/serve_webui.go` — the `/api/`
  carve-out + parent-mux registration + `RequireCapability` mount discipline.

### Frontend mount points (D-10 — extend Phase-28 in place)
- `web/src/governance/` — `GovernanceWorkspace.tsx`, `McpBoard.tsx`,
  `McpServerDetail.tsx`, `SkillsBoard.tsx`, `SkillDetail.tsx`, `governanceApi.ts`,
  `governanceView.tsx` (the Phase-28 read boards to extend with write controls).
- `web/src/shell/modes.ts` (the `governance` mode) + `web/src/AppShell.tsx`
  (center-swap seam) + `web/src/i18n/resources.ts` (en+it).
- Frontend gates: `web/scripts/contrast-check.mjs` (WCAG-AA), Vitest ≥85%,
  Stryker ≥70%, Playwright e2e ([[feedback_frontend_quality_gates_coverage_mutation]]).

### UI/UX contract (UI hint: YES — run `/gsd-ui-phase 29`)
- `docs/design/aura-deep-search-figma/ux-spec.md` Frame 08 (governance/settings
  write) + Non-Goals — researcher maps the relevant frames.
- `docs/cockpit-overhaul/` (`00-VALIDATION.md` umbrella + per-spec ledgers) — the
  **locked BLUE design system** ([[project_cockpit_palette_deviation_blue_vs_graphite]]),
  fonts (Fraunces / Hanken Grotesk / Commit Mono), shell/drawer/mobile contract.

### Prior phase context (carried forward — do NOT re-decide)
- `.planning/phases/28-governance-boards-web-onboarding/28-CONTEXT.md` — the
  read-board + REST + audit + capability + mode-swap-workspace patterns this phase
  extends; the deep-research-mandate precedent.
- `.planning/phases/25-chat-approval-center/25-CONTEXT.md` — the
  `Interrupt`/`Resume[]` HITL protocol (Phase 29 extends it for skill install).
- `.planning/phases/24-web-foundation-serve-auth-health/24-CONTEXT.md` — the
  `/api/` carve-out + `RequireAuth` whole-origin gate + the `capability_grants`
  principal seam every new route inherits.

### Architecture / security canon
- `.planning/research/ARCHITECTURE.md` — the four-layer write-protection model
  (proxy → principal → `capability_grants` → risk/approval gate).
- Canon-referral (per SPEC): SSRF on the MCP probe + skill-source fetch = web-
  safety canon (DISP-04 + `/api/image-proxy` SSRF guard) owned by
  `/gsd-secure-phase`; skill name/path traversal = the `SanitizeName` grammar
  chokepoint + `/gsd-secure-phase`; Authula password at-rest hashing = Authula +
  `/gsd-secure-phase`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **MCP**: `SaveManagedConfig` (atomic whole-write — the env-edit substrate),
  `RedactEnv`/`secret.IsSecretEnvKey`/`ImportProfile` (redaction + credential
  preservation), `BuiltInCatalog().RequiredEnv` (surface as a guided form),
  trust classes + mount-time risk policy + `RunnableManagedServers`,
  `ProbeServer`/`mcpDoctorAll` (live tool-count). No new MCP logic — thin write
  adapters + the env-edit path + the `mcp_audit` row.
- **Skills**: the Writer/Loader gate (`ComputeSkillTier`, `SanitizeName`, body
  cap, injection blocklist, `SKILL.md` parse, `ContentHash`), the active/`pending/`
  /`archived/` dirs + restore/archive + TTL sweep, `AuditStore.ListAudit`
  (append-only). The new HTTP endpoints WRAP these — no new gate logic.
- **Approval/HITL**: `internal/agui/approvals_api.go` cross-thread queue +
  `Interrupt`/`Resume[]` resume token — the skill-install approval reuses it.
- **REST/route**: `internal/agui/governance_api.go` + `auth.go`
  (`RequireAuth`/`RequireCapability`/`withPrincipal`) + `server.go` `Mux()` +
  `cmd/aura/serve_webui.go` mount — the new write routes mirror these.
- **Migration**: `0010` (skill_audit) + `0021_identity_audit` — the role+dual-
  trigger append-only template for `0022_mcp_audit`.
- **Frontend**: `web/src/governance/*` (extend in place) + `modes.ts` +
  `AppShell.tsx` + `i18n/resources.ts` + the locked blue token system.
- **Skills.sh transport**: the spike-proven `npx skills find`/`add -y` (`-y`,
  strip ANSI) — runs in Aura's existing runtime container (D-06).

### Established Patterns
- **Resource + named-action sub-paths** behind `RequireAuth` +
  `RequireCapability("governance.write")`, under the Phase-28 `/api/governance/*`
  prefixes; one named action = one audit row (D-01).
- **Append-only audit ledger** (role SELECT+INSERT + dual UPDATE/DELETE/TRUNCATE
  triggers) — `0022_mcp_audit` mirrors `0010`/`0021`; mutation+audit atomic in one
  `db.WithTx` (D-02/D-04).
- **Secrets-never-leaked redaction belt** — `RedactEnv`/`IsSecretEnvKey` +
  key-only env rows + `SanitizeString`; an unchanged redacted-placeholder secret
  is preserved, never overwritten (D-05).
- **Container-as-isolation** — Aura runs in a container, so the install runs there
  with scripts; the risk control is the approval gate + Writer validation, not
  `--ignore-scripts` (D-06/D-07; [[feedback_never_run_exe_use_container]]).
- **Unified approval center** — reuse the Phase-25 `/api/approvals` cross-thread
  queue + `Interrupt`/`Resume[]`; no model-facing approve (D-11).
- **Minimal-industrial backend, premium-bar UI** —
  ([[feedback_no_atomic_bombs_minimal_industrial_shape]] +
  [[feedback_cockpit_premium_bar_over_minimal]]) one `governance` workspace,
  reuse the proven plumbing, four named thin gap-fillers only.

### Integration Points
- `surface==='governance'` → the existing Governance workspace MCP/Skills tabs
  gain write controls → `POST/PATCH/DELETE /api/governance/mcp|skills/*` behind
  `RequireCapability("governance.write")`.
- MCP install/edit/trust/enable/disable/remove → mutate via the Phase-16 manager
  → `SaveManagedConfig` + one `mcp_audit` row (atomic `db.WithTx`) → board
  re-fetches → live doctor/tool-count probe (bounded timeout, isolated row).
- Skill install (source field or skills.sh catalog) → `npx skills find/add -y` in
  Aura's container → Writer gate (validate + `ContentHash`) → stage `pending/` →
  the `/api/approvals` queue (resume token) → approval resume activates →
  `skill_audit` row.
- Restore/archive → the skills loader move + a `skill_audit` row; the four tabs
  (active/pending/archived/audit) re-fetch.

### Known tensions to resolve (planner)
- **D-09 SPEC-amendment is BLOCKING** — the `--ignore-scripts` deviation must land
  as a SPEC-amendment commit BEFORE the skills-install implementation.
- **Skills HTTP write endpoints are net-new** (the named gap-filler) — zero exist
  today; wrap the existing Writer/gate, do NOT re-implement gate logic.
- **`mcp_audit` is net-new** — no MCP config-mutation ledger exists; the
  enable/disable/remove CLI commands today write NO audit row.

</code_context>

<specifics>
## Specific Ideas

- **Operator directive (2026-06-20): deep-research mandate (binding on the
  researcher).** *"deep research online best 2026 industrial UI/UX pattern and on
  `D:/tmp`."* Mirrors the Phase-27/28 deep-research passes
  ([[feedback_check_tmp_sources_then_brainstorm_best]] — "best not easiest"). The
  `gsd-phase-researcher` MUST do a DEEP-RESEARCH pass (online + curated `D:/tmp`
  codebases — e.g. `elysia-frontend`, `odysseus`, admin/console references) for
  **2026 industrial admin/governance WRITE UI/UX**: install wizards/forms,
  permission/trust-approval UX, secret-redaction-chip + four-state env editors,
  risk-tiered approval queues, lifecycle-tab management, destructive-action
  confirmation patterns — **desktop AND mobile, industrial-grade**, held to the
  locked blue design system + WCAG-AA. Take layout/interaction ideas only — NO
  dockable-window / command-palette machinery (the deferred `ui_control` shell).
- **UI hint: YES — run `/gsd-ui-phase 29`** for the design contract before/with
  planning (the write surfaces are form-heavy + the highest-abuse surface).
- **Operator directive (2026-06-20): "always `--ignore-scripts` is stupid" +
  "Aura already runs on a container."** The container IS the isolation boundary;
  scripts run; the gate + Writer validation is the control (D-06/D-07/D-09).
- **Premium-but-industrial bar** ([[feedback_cockpit_premium_bar_over_minimal]],
  [[project_aura_dgx_spark_bundle_vision]]) — polished operator console, but
  minimal-industrial backend (reuse the proven REST + manager + approval plumbing).
- **Fully cockpit-driven** (operator directive "easy for end user, no CLI, no
  env") — every MCPW/SKW flow completes in the cockpit; no terminal, no hand-
  editing `servers.json` or any env file.

</specifics>

<deferred>
## Deferred Ideas

- **Scheduler write** (cancel / run-now / approve / create via HTTP) → v2 (GOVW-03);
  the scheduler board stays read-only.
- **`ui_control` / operator-OS shell** (dockable windows, icon rail, command
  palette, `open_panel`/`set_mode`) → v2 (SHELL) — highest abuse surface.
- **New core MCP/skill capability** — no new transport, no new skill execution
  model, no new gate/scoring logic; Phase 29 is a write-over-existing-backend.
- **skills.lock.json `computedHash` interop** — Aura's canonical content hash only
  (locale-sensitive, spike finding).
- **OAuth dynamic client registration for HTTP MCP** — Phase-16 deferral, not here.
- **The model's ungated in-sandbox self-extension** — left exactly as-is; Phase 29
  adds only the operator-gated cockpit path (clean two-path boundary).
- **Multi-user RBAC / route-scoping beyond `capability_grants`** — post-v1.0.0.

### Reviewed Todos (not folded)
None — no pending todos matched Phase 29.

</deferred>

---

*Phase: 29-governance-write-mcp-configuration-skills-install*
*Context gathered: 2026-06-20*
