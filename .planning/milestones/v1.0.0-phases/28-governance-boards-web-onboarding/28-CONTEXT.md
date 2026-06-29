# Phase 28: Governance Boards + Web Onboarding - Context

**Gathered:** 2026-06-19
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver three **read-only** governance cockpit boards (MCP servers, skills
library, scheduler) **and** a **web onboarding wizard** that drives the existing
5-step `internal/onboarding` LoopAgent → `Agent.md` *and* fully provisions a new
**loginable** identity (`aura.identities` row + `capability_grants` + an Authula
login credential + a live Telegram channel link). Provisioning is
**capability-gated, no-privilege-escalation, atomic on confirm, no-secret-leak**.

All three governance backends are mature but **CLI-only today** — this phase adds
the first web/REST exposure over the proven Phase-24/25/27 cockpit pattern (thin
`/api/*` handler behind `RequireAuth`, lazy React page, TanStack Query, i18n
en/it). **No writes** — governance write surfaces are Phase 29; scheduler writes
are v2; the `ui_control` shell is a follow-up milestone.

**This phase absorbs the old Phase 30** (Telegram link + QR → requirement
ONBD-01b). Requirements are **locked by `28-SPEC.md`** (7 requirements, 13
acceptance criteria) — this discussion captured *how* to implement, not *what*.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**7 requirements are locked.** See `28-SPEC.md` for full requirements,
boundaries, and acceptance criteria. Downstream agents MUST read `28-SPEC.md`
before planning or implementing — requirements (GOV-01..03, ONBD-01a/01b/02,
the cross-cutting auth/no-leak boundary) are **not** duplicated here.

**In scope (from SPEC.md):**
- Three read-only cockpit boards: MCP servers, skills library
  (active/pending/archived/audit), scheduler (tasks + run history).
- The MCP board's live, per-server, **timeout-bounded** doctor + tool-count
  probe (read-only execution, mutates nothing).
- A web onboarding wizard driving the existing 5-step LoopAgent → `Agent.md`
  (confirm/edit/skip, **no duplicate LLM turns**).
- Full identity provisioning from the wizard: `identities` row +
  `capability_grants` + Authula login + live Telegram channel link (deep-link +
  QR), capability-gated, no-escalation, atomic, audited.
- New authenticated `/api/*` REST endpoints (read for boards; the create
  mutation behind `RequireCapability`).
- i18n en/it; web ≥85% vitest + ≥70% Stryker on touched dirs; Go owned-surface
  ≥85%; Playwright e2e + contrast-check (WCAG AA) on new surfaces.

**Out of scope (from SPEC.md):**
- **All governance WRITE actions** (skills approve/activate/archive/install; MCP
  install/edit/enable/disable/remove) → **Phase 29**.
- **Scheduler write** (cancel/run-now/approve/create via HTTP) → **v2 (GOVW-03)**.
- **`ui_control` / operator-OS shell** (panels, modes, command palette, dockable
  windows) → **v2 (SHELL)**.
- The standalone `:9081` loopback setup wizard — remains as-is; this phase adds
  the cockpit surface, it does not remove/refactor the loopback flow.
- Multimodal input on onboarding endpoints — text/JSON only; Telegram stays the
  multimodal channel.
- **Phase 30 is absorbed here** — ROADMAP + `30-SPEC.md` amendment flagged (D-09).

</spec_lock>

<decisions>
## Implementation Decisions

### Governance boards surface (GOV-01..03)
- **D-01 — One new `governance` workspace mode with 3 internal tabs.** Add
  `'governance'` to `web/src/shell/modes.ts` `MODES` + `LIVE_MODES`; the
  `AppShell` center swaps to a lazy `Governance` workspace when
  `surface === 'governance'` (mirrors the Phase-27 `graph` swap). Internal tabs:
  **MCP / Skills / Scheduler**. NOT three separate top-level modes; NOT folded
  into the placeholder `settings` mode. Minimal nav footprint, one workspace to
  learn — consistent with minimal-industrial shape.
- **D-02 — Each board = master-list + detail/inspector.** A dense list/table of
  rows (server / skill / task) with a detail pane on row-select:
  - **MCP**: row = source / trust class / enabled / env-health / startup state
    (redacted secret chips only); detail = live per-server doctor result +
    mounted tool list/count (the live probe).
  - **Skills**: four lifecycle **tabs** (active / pending / archived / audit);
    row = capability scope / last used / use count / TTL-archive state / risk
    tier / content hash; audit tab = append-only ledger rows, newest-first.
    Pending rows have **no** run/activate control (by construction).
  - **Scheduler**: row = kind / schedule / next run / status; detail = per-task
    paginated run history (status / started / heartbeat / summary).
  Mirrors the scheduler task→run-history shape + the Graph-Explorer inspector.

### Onboarding wizard (ONBD-02 + ONBD-01a/01b)
- **D-03 — Transport = REST per-step JSON (no SSE).** New `/api/onboarding/*`
  endpoints: a **step** endpoint applies an `onboarding.Intent`
  (`answer`/`confirm`/`edit`/`skip`) to the server-held `onboarding.Session` and
  returns `{content, step, status, draft, preferences}`. The LoopAgent is a
  request/response state machine; the `Session.prompted` flag is the
  **no-duplicate-LLM-turn** guarantee; the single LLM extraction at draft time
  runs server-side inside the step call. SSE is unjustified for a text/JSON
  wizard (SPEC constraint).
- **D-04 — One dedicated full-screen wizard, all-in.** A focused linear wizard
  (own route/overlay, **not** a Governance tab) runs the whole flow:
  provision identity (login + capability grants) → Telegram link (deep-link +
  QR) → 5-step `Agent.md` interview → confirm. Provisioning and the interview
  are **one** flow (SPEC goal: "seed `Agent.md` AND fully provisions").
- **D-04 clarification (operator-approved 2026-06-20):** the cross-store write
  executes atomically at the final Confirm — the wizard collects
  credentials/capabilities/Telegram-intent/interview answers into the
  server-held session and runs legs A+B+C only on Create; this is orphan-free
  and supersedes a literal provision-first reading of the screen order. The
  wizard UI step order stays credentials → capability picker → Telegram link+QR
  → 5-step interview → review+Create, with the saga firing at Create.

### Identity provisioning (ONBD-01a)
- **D-05 — Credential = operator sets email + initial (temp) password.** The
  operator types the new user's email + an initial password in the wizard; the
  credential is minted via Authula's **server-side** user-create
  (`UserService.Create`/equivalent) — bypassing the public `DisableSignUp`
  route (that flag only blocks the `/auth/signup` HTTP route, not programmatic
  creation). The new user enrolls TOTP on first login (no mailer is wired —
  no email-invite flow available). The password is **never returned or logged**
  after creation (no-leak).
- **D-06 — Capability picker = checklist of the creator's own grants minus
  `*`.** Render the creating operator's own `capability_grants` (with `*`
  excluded) as a checklist; the operator ticks a subset. No-escalation holds by
  construction — you can only grant from your own visible set. The server
  **re-validates** (subset ⊆ creator-grants AND no `*`) before the atomic write
  (belt-and-suspenders).
- **D-07 — PRD-AMENDMENT (BLOCKING): Phase 28 intentionally introduces a 2nd
  web-loginable identity → multi-operator login.** This collides with PROJECT.md's
  "single-operator / multi-user is post-v1.0.0" mandate AND the `webauth` wiring's
  `OperatorUserID` **single-user guard** (it *errors* when >1 Authula user exists)
  AND the enrollment assumption that pins the lone user to the `local` identity.
  RESOLUTION: relax/replace the single-user `OperatorUserID` pin so >1 Authula
  user resolves correctly through `aura.identity_auth_links`
  (`ResolveIdentityID`); add the server-side user-create path; **authz stays
  `capability_grants`-based — NO new RBAC, NO route-scoping beyond
  `RequireCapability`.** The planner **MUST land a PROJECT.md + ROADMAP
  PRD-amendment commit BEFORE the provisioning implementation** (CLAUDE.md
  PRD-first principle — deviations require a PRD-amendment commit first).
- **D-07b — Atomicity is a cross-store saga, not a single PG tx.** Provisioning
  spans `aura.*` (identities + capability_grants + identity_auth_links + audit,
  one pgx tx) **and** the Authula `authula` schema/pool (separate
  `database/sql` pool) **and** the Telegram-link token. Authula's own pool means
  this is **not** a single transaction — the planner designs an
  all-or-nothing-on-confirm flow with compensation/rollback (delete the Authula
  user + aura rows if any leg fails). Acceptance: an abandoned/failed flow leaves
  **no** orphan identity/grant/Authula/Telegram row, ever.

### Telegram channel link (ONBD-01b — absorbs Phase 30)
- **D-08 — Reuse the existing setup-wizard mint/consume, surface it in the web
  wizard.** Do NOT reinvent the token flow: reuse the `:9081` setup wizard's
  single-use, time-bounded (1h TTL) onboarding-token mint + `ConsumeOnboarding`
  atomic consume. Mint a token for the **new** identity, render
  `https://t.me/<bot>?start=<token>` + a scannable QR; consuming the token once
  links that identity's Telegram channel and invalidates the token; a replayed
  or expired consume is rejected; the bot token is **never** rendered or logged.

### Phase 30 disposition
- **D-09 — Mark Phase 30 absorbed/done with a pointer; defer execution to the
  planner.** Phase 30 (Telegram link+QR) is fully absorbed into 28 (ONBD-01b).
  Mark the ROADMAP Phase-30 entry `✅ absorbed-into-28` with a one-line pointer;
  convert `30-SPEC.md` into a tombstone referencing `28-SPEC.md §ONBD-01b`
  (preserve traceability). **Execution is DEFERRED to the planner** (discuss-phase
  stays non-mutating) — bundle the ROADMAP/SPEC edit with the **D-07
  PRD-amendment** commit, via `/gsd-phase` / gsd tooling (never a direct ROADMAP
  Write — anti-pattern #15).

### Claude's Discretion (research/planner-resolved — no further operator input)
- **Live MCP probe**: concurrency model + per-server timeout value + isolated-row
  failure rendering + any short schema/status cache. Constraint locked: bounded
  per-server timeout, a dead/hung server fails ITS row only and never stalls the
  board render.
- **REST shapes**: `/api/governance/*` (mcp/skills/scheduler reads),
  `/api/onboarding/*` (step), and the provisioning **create** mutation endpoint;
  pagination defaults for scheduler run-history + skills lists.
- **Audit-row shape** for identity creation (one immutable row per create): reuse
  an existing append-only ledger (skill_audit-style, no UPDATE/DELETE) or a new
  `aura.identity_audit` — planner decides; must be immutable.
- **Wizard resume/abandonment**: server-held session lifetime/TTL; abandoned flow
  → atomic rollback (D-07b), no partial identity.
- `web/src/governance/` + `web/src/onboarding/` component layout, lazy-chunk
  boundaries, empty/loading/error states, **desktop + mobile breakpoints** (held
  to the deep-research mandate + the locked blue design system + the WCAG-AA
  contrast gate).
- i18n keys (en + it) for every board + wizard string.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (researcher + planner) MUST read these before planning or
implementing.**

### Phase scope & requirements (LOCKED)
- `.planning/phases/28-governance-boards-web-onboarding/28-SPEC.md` — **locked
  requirements** (GOV-01..03, ONBD-01a/01b/02, cross-cutting auth/no-leak),
  13 acceptance criteria, edge coverage, prohibitions. **Read before planning.**
- `.planning/ROADMAP.md` §"Phase 28: Governance Boards + Web Onboarding"
  (~lines 223-241) + the Phase-30 entry (line 56, ~254) to be amended (D-09).
- `.planning/REQUIREMENTS.md` — GOV-01..03, ONBD-01..02 rows.
- `.planning/PROJECT.md` §"Current Milestone v1.0.0" + §Out of Scope — the
  single-binary `//go:embed` invariant AND the **single-operator / no-RBAC /
  "multi-user is post-v1.0.0" mandate that D-07's PRD-amendment must edit.**

### PRD-first amendment targets (D-07 / D-09)
- `prd.md` + `.planning/PROJECT.md` + `.planning/ROADMAP.md` — the planner lands
  a PRD-amendment commit BEFORE provisioning impl: (a) Phase 28 introduces a 2nd
  web-loginable identity (relax the single-operator boundary, authz stays
  `capability_grants`); (b) Phase 30 absorbed into 28.
- `.planning/phases/30-*/30-SPEC.md` — convert to a tombstone → `28-SPEC §ONBD-01b`.

### Governance backends (LOCKED shape — reuse, do not re-invent)
- **MCP**: `internal/mcp` — `LoadManagedConfig` (`~/.aura/mcp/servers.json`),
  `manager.SnapshotStatus(doc)` (Name/Trust/Runtime/StartupState/AuthStatus/
  LastError), `manager.RedactSecrets` + `secret.IsSecretEnvKey` (redaction).
  Live doctor + tool-count = the `aura mcp status` / `mcpDoctorAll` probe path
  (NOT stored — a live probe). Researcher: locate exact files under `internal/mcp/`.
- **Skills**: filesystem loader (active + `pending/` + `archived/`) + the
  append-only `aura.skill_audit` ledger (`AuditStore.ListAudit`, dual-enforced
  no UPDATE/DELETE). Pending skills are non-runnable + injection-blocklisted by
  construction.
- **Scheduler**: `internal/cron` `Store` (`ListActiveTasks`, `GetTask`,
  run-history query) over `aura.scheduler_tasks` + `aura.agent_job_runs`
  (`last_heartbeat_at` column); `aura task doctor`.

### Onboarding + profile (LOCKED state machine)
- `internal/onboarding/session.go` — the 5-step state machine (`Session.Apply`,
  `Intent`, `Step`, `Status`, the `prompted` flag = no-duplicate-LLM-turn).
- `internal/onboarding/extractor.go` + `extractor_llm.go` — LLM answer-extractor
  with raw-text fallback (`ExtractDraft`).
- `internal/profile/render.go` — renders the 8-section `Agent.md`, stored at
  `~/.aura/agents/<id>/`.

### Identity + auth (LOCKED shape — D-05/D-06/D-07)
- `internal/webauth/authula.go` — the embedded Authula provider:
  `DisableSignUp: true`, no mailer, `OperatorUserID` **single-user guard**
  (errors on >1 user), separate `authula` schema + `database/sql` pool (H1).
  D-05 needs a server-side user-create path; D-07 relaxes the single-user guard.
- `internal/webauth/identity_link.go` — `LinkOperator` / `ResolveIdentityID`
  over `aura.identity_auth_links` (UNIQUE on `authula_user_id`, already 1:N-ready).
- `internal/identity` — `aura.identities` + `aura.capability_grants` (seeded
  `local` with `*`). The capability checklist (D-06) reads the creator's grants.
- `docs/cockpit-overhaul/05-authula-auth-SPEC.md` — the Authula adoption spec
  (provider flag, hardenings H1/H2/H3, the single-operator OQ-4/OQ-8 notes D-07
  supersedes).

### Telegram link (D-08)
- The `:9081` loopback setup wizard's deep-link/QR mint + `ConsumeOnboarding`
  atomic consume (single-use, 1h TTL). Researcher: locate the setup-wizard +
  Telegram-link package (`internal/setup` / channel-setup) and the
  `ConsumeOnboarding` call site to reuse.

### REST handler + route pattern (model for the new endpoints)
- `internal/agui/conversations_api.go` (Phase 25) + `graph` API (Phase 27,
  `internal/agui/*_api.go`) — `writeJSON`/`writeJSONStatus`/`sanitizeErr` helpers
  + thin-handler-over-store shape to mirror.
- `internal/agui/server.go` `Mux()` + `cmd/aura/serve_webui.go` — the `/api/`
  carve-out, `RequireAuth` whole-origin gate, and `RequireCapability` mount
  discipline (the create mutation registers behind `RequireCapability`, parity
  with `POST /agent/run`).
- `internal/agui/image_proxy.go` — read-GET-behind-`RequireAuth` precedent.

### Prior phase context (carried forward — do NOT re-decide)
- `.planning/phases/24-web-foundation-serve-auth-health/24-CONTEXT.md` — the SPA
  host + `/api/` carve-out + `RequireAuth` whole-origin gate + the
  `capability_grants` principal seam every new route inherits.
- `.planning/phases/25-chat-approval-center/25-CONTEXT.md` — thin-HTTP-adapter
  discipline; the `Interrupt`/`Resume[]` HITL protocol (Phase 29 will extend it).
- `.planning/phases/26-typed-display-protocol-router/26-CONTEXT.md` — the
  image-proxy/`RequireAuth` `/api/` pattern + HARDEN-08 untrusted-output posture.
- `.planning/phases/27-neo4j-graph-explorer/27-CONTEXT.md` — the
  mode-swap-workspace + lazy-chunk + REST-board pattern this phase mirrors; the
  deep-research-mandate precedent (specifics below).

### UI/UX contract (UI hint: yes — consider `/gsd-ui-phase 28`)
- `docs/design/aura-deep-search-figma/ux-spec.md` — researcher maps the relevant
  frames for governance/settings + onboarding/system-event surfaces.
- `docs/cockpit-overhaul/` (`00-VALIDATION.md` umbrella + the per-spec ledgers) —
  the **locked blue design system**, fonts (Fraunces / Hanken Grotesk / Commit
  Mono), shell/drawer/mobile contract, `web/scripts/contrast-check.mjs` WCAG-AA gate.
- `web/src/shell/modes.ts` + `web/src/AppShell.tsx` (center swap seam) +
  `web/src/i18n/resources.ts` (en+it) — the frontend mount points.

### Architecture / security canon
- `.planning/research/ARCHITECTURE.md` — serve/embed + the four-layer write-
  protection model (proxy → principal → `capability_grants` → risk/approval gate).
- Canon-referral (per SPEC): Authula password at-rest hashing = Authula +
  `/gsd-secure-phase`; generic SSRF on the outbound probe = web-safety canon
  (DISP-04 + the `/api/image-proxy` SSRF guard) — SPEC prohibition #5 keeps only
  the bespoke "configured-servers-only" probe scope.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **MCP**: `manager.SnapshotStatus` + `RedactSecrets` + `secret.IsSecretEnvKey`
  + the `mcpDoctorAll` / `aura mcp status` live-probe path — the MCP board is a
  thin read adapter over these; no new MCP logic.
- **Skills**: the filesystem loader + `AuditStore.ListAudit` (append-only) — the
  skills board reads these; the four tabs map to active dir / `pending/` /
  `archived/` / the audit ledger.
- **Scheduler**: `cron.Store` (`ListActiveTasks`/`GetTask`/run-history) over
  `scheduler_tasks` + `agent_job_runs` — the scheduler board is a thin read.
- **Onboarding**: `onboarding.Session` + `ExtractDraft` + `profile.render` —
  the wizard drives the EXISTING state machine over REST; no LoopAgent rewrite.
- **Auth/provisioning**: `webauth` (`authula.go`, `identity_link.go`) +
  `identity` store + `capability_grants` — D-05/D-06/D-07 extend these.
- **REST/route**: `internal/agui/*_api.go` helpers + `serve_webui.go` mount +
  `image_proxy.go` precedent — the new endpoints mirror these.
- **Telegram**: the `:9081` setup wizard mint/`ConsumeOnboarding` — reused as-is.
- **Frontend**: `web/src/shell/modes.ts` (add `governance`), `AppShell.tsx`
  center swap, `web/src/i18n/resources.ts` (en+it), the locked blue token system.

### Established Patterns
- **Thin HTTP adapters behind `RequireAuth`, under the `/api/` carve-out** — read
  endpoints; the create mutation additionally behind `RequireCapability` (parity
  with `POST /agent/run`). No new transport invented.
- **Mode-swap workspace + lazy chunk** — Phase-27 precedent; the `governance`
  workspace + the onboarding wizard ship as lazy chunks inside `internal/webui/dist`.
- **Secrets-never-leaked redaction belt** — `RedactSecrets`/`IsSecretEnvKey` +
  `sanitizeErr`; no raw MCP env / Authula password / Telegram bot token in any
  response, DOM, or log.
- **Append-only audit ledger** (`skill_audit` dual-enforced no UPDATE/DELETE) —
  the model for the immutable identity-create audit row.
- **Minimal-industrial shape** ([[feedback_no_atomic_bombs_minimal_industrial_shape]])
  — one `governance` mode w/ tabs, reuse the Telegram mint/consume, no new RBAC.
- **Frontend quality gates** ([[feedback_frontend_quality_gates_coverage_mutation]])
  — Vitest ≥85% + Stryker ≥70% + Playwright + contrast; testable seams (mock the
  live MCP probe; unit-test the REST handlers + the no-escalation validator).

### Integration Points
- `surface==='governance'` → `AppShell` center swaps to the lazy Governance
  workspace (MCP/Skills/Scheduler tabs) → `/api/governance/*` reads.
- MCP tab → live per-server doctor + tool-count probe (bounded timeout, isolated
  failure) → redacted status rows + detail.
- Onboarding wizard → `/api/onboarding/*` step (REST) → server-held
  `onboarding.Session` → `Agent.md` draft → confirm.
- Provisioning confirm → atomic cross-store saga: `aura.identities` +
  `capability_grants` + `identity_auth_links` + immutable audit (pgx tx) +
  Authula server-side user-create (separate pool) + Telegram token mint/consume
  → compensation/rollback on any failure (D-07b).
- Capability checklist ← creator's `capability_grants` (minus `*`); server
  re-validates subset ⊆ creator + no-`*` before write (no-escalation).

### Known tensions to resolve (planner)
- **D-07 multi-user**: the `OperatorUserID` single-user guard + `DisableSignUp`
  + the identity-pin-to-`local` assumption all assume exactly one Authula user.
  Provisioning a 2nd loginable user breaks these — must be relaxed, and the
  PRD-amendment landed first.
- **D-07b atomicity**: Authula's own pool/schema means provisioning is a saga,
  not a single PG transaction — design compensation for partial-failure rollback.

</code_context>

<specifics>
## Specific Ideas

- **Operator directive (2026-06-19): deep-research mandate.** "deep research
  online and on `D:/tmp` — industrial patterns, desktop AND mobile, industrial-
  grade design." Mirrors the Phase-27 deep-research pass. The researcher MUST
  study curated references before proposing the build:
  - **Governance/admin console patterns**: dense read-only operator dashboards,
    status boards, master-list+detail/inspector layouts, lifecycle-tab tables,
    paginated run-history — survey curated `D:/tmp` codebases (e.g.
    `elysia-frontend`, `odysseus`, and any admin/console references) + online
    industrial admin-UI patterns. Take layout/interaction ideas only — NO
    dockable-window / icon-rail / command-palette machinery (that is the deferred
    `ui_control` shell).
  - **Onboarding/provisioning wizard patterns**: multi-step linear wizards,
    capability/permission pickers, credential issuance UX, QR-link pairing —
    online + `D:/tmp`.
  - **Desktop + mobile responsive, industrial-grade**: both breakpoints are
    first-class (operator may run the cockpit on a phone). Held to the locked
    blue design system + WCAG-AA contrast gate.
  - This warrants a **`/gsd-ui-phase 28`** design contract (UI hint: yes) and a
    DEEP-RESEARCH pass in the gsd-phase-researcher (online + `D:/tmp`,
    [[feedback_check_tmp_sources_then_brainstorm_best]] — "best not easiest").
- **Premium-but-industrial bar** ([[feedback_cockpit_premium_bar_over_minimal]],
  [[project_aura_dgx_spark_bundle_vision]]) — build the polished operator console
  operators expect, but keep it minimal-industrial (no over-engineering, reuse
  the proven REST + Telegram-mint plumbing).
- **PRD-first discipline is non-negotiable here** — D-07's multi-user expansion
  and D-09's Phase-30 absorption are exactly the architectural deltas CLAUDE.md
  requires a PRD-amendment commit for, *before* code.

</specifics>

<deferred>
## Deferred Ideas

- **All governance WRITE surfaces** (skills approve/activate/archive/install; MCP
  install/edit/enable/disable/remove) → **Phase 29** (reuses the Phase-25
  `Interrupt`/`Resume[]` approval protocol).
- **Scheduler write** (cancel / run-now / approve / create via HTTP) → **v2
  (GOVW-03)**.
- **`ui_control` / operator-OS shell** (dockable windows, icon rail, command
  palette, `open_panel`/`set_mode`) → **v2 (SHELL)** — highest abuse surface.
- **Email-invite / self-service signup** for new identities — no mailer is wired;
  D-05 uses operator-set credentials instead. A mailer + invite flow is a
  post-v1.0.0 nicety.
- **Full multi-user RBAC / route-scoping / per-identity session isolation** —
  D-07 keeps authz at `capability_grants` only; richer RBAC is post-v1.0.0 (OQ-8).
- **The `:9081` loopback setup wizard refactor** — out of scope; this phase adds
  the cockpit surface, the loopback flow stays as-is.

### Reviewed Todos (not folded)
None — no pending todos matched Phase 28.

</deferred>

---

*Phase: 28-governance-boards-web-onboarding*
*Context gathered: 2026-06-19*
