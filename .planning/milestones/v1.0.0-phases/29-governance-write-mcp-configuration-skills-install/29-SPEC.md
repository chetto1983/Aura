# Phase 29: Governance Write — MCP Configuration + Skills Install — Specification

**Created:** 2026-06-20
**Ambiguity score:** 0.146 (gate: ≤ 0.20)
**Requirements:** 7 locked

## Goal

The operator can fully configure MCP servers (recipe / custom-stdio install, in-place env editing with redaction, enable/disable/remove, cockpit trust-approval) and govern the skills lifecycle (install from a source field or a skills.sh catalog search → risk-tiered approval queue → activate, restore/archive, immutable audit) **entirely from the cockpit — no terminal, no hand-editing of any config or env file** — as the web WRITE surface over the EXISTING backend (the Phase-16 MCP manager control plane + the Phase-11 scoring-gated skill install/create/delete + `ask_user` approval + append-only audit), with only thin additive gap-fillers, not new core capability.

## Background

Grounded in the current codebase (scouted 2026-06-20, three backend surveys + spike review + deep research across D:/tmp, OpenClaw docs, and online):

- **MCP manager (Phase 16) — full config CRUD exists, CLI-only.** `mcp.LoadManagedConfig` / `SaveManagedConfig` (`~/.aura/mcp/servers.json`, atomic temp+rename, `0o600`; `AURA_MCP_CONFIG` path override; the ephemeral `AURA_MCP_SERVERS_JSON` overlay is read-only-merge); `BuiltInCatalog()` (4 recipes: calculator/calendar/whatsapp/memory) with a `RequiredEnv []string` field that is **defined but never used/validated**; `aura mcp install|add|enable|disable|remove|trust`; `RedactEnv`/`secret.IsSecretEnvKey`/`ImportProfile` credential-preservation; trust classes (`trusted_recipe`/`trusted_local`/`sandboxed_local`/`remote_http`/`blocked`, custom defaults to `blocked`); mount gate `RunnableManagedServers` silently skips blocked servers; risk labels enforced **before** registry insert (denied tools never reach the model); live `ProbeServer`/doctor tool-count (not stored). **Gaps:** no env-*edit* command (edit = remove+re-add); trust `ApprovedBy`/`ApprovedAt`/`Reason` fields exist but are **never populated**; **no MCP config-mutation audit ledger**; blocked servers are skipped with no warning row.
- **Skills (Phase 11) — full install/gate/approve/audit exists, CLI/agent-tool-only.** Scoring gate `ComputeSkillTier` (delete=Destructive, create/update/install=Risky; `GateRecommended` true for Risky/Destructive); validation checklist at the Writer/Loader (`SanitizeName` `^[a-z0-9-]{1,64}$`, body cap, NFKC-normalized + case-folded injection blocklist with matched-position reporting, `SKILL.md` frontmatter parse via real YAML + CRLF normalization); content hash (`sha256:` byte-sorted (relPath,bytes)); `ask_user` pause (`ErrAwaitingUserInput` → `aura.paused_states`) + CLI `aura skills approve`; activation **only** via approval resume or operator CLI (no model-facing approve action exists); pending lives in `pending/` (loader scans active only — pending is non-runnable + never materialized/injected); active/pending/archived dirs + restore/archive + `.usage.json` (last_used/use_count/status) + daily TTL sweep; append-only `aura.skill_audit` (migration 0010, role grant SELECT+INSERT only + dual UPDATE/DELETE/TRUNCATE triggers, D-29 coherence CHECK). **Gaps:** zero HTTP skill-write endpoints; **no backend catalog** — discovery is sandbox-`npx skills`-only.
- **Web pattern (Phase 25/28).** Thin `/api/*` handler → one provider call → JSON projection, registered on the parent mux behind `RequireAuth` (+ `RequireCapability` on mutations: caps today are `agent.run`, `identity.create`, `governance.read`); HITL `/api/approvals` (cross-thread `ListPendingAll` + accept/decline/cancel resolve over a uuid resume token); secret redaction (`SanitizeString` 3-pass + env rows sent **key-only** with a `redacted` flag, never values); append-only audit pattern proven twice (`skill_audit` 0010, `identity_audit` 0021, role+dual-trigger); frontend TanStack Query `useMutation` + `invalidateQueries` (`credentials:'same-origin'`, `retry:false`, `encodeURIComponent`); i18n en+it in `web/src/i18n/resources*.ts`. The **six Phase-28 read boards** (`/api/governance/mcp`, `…/mcp/{name}/probe`, `…/skills`, `…/skills/audit`, `…/scheduler`, `…/scheduler/{id}/runs`) are read-only; Phase 29 extends them with writes.
- **Spikes + research.** The ungated in-sandbox `npx skills add` self-extension (no ceremony, operator directive 2026-06-05) is the **model** path — the cockpit operator install is the **distinct, gated, RISKY supply-chain** path; both share the existing gate→approval→activate pipeline. `npx skills find`/`add -y` is the spike-proven skills.sh transport (`-y` non-interactive; strip ANSI; provenance/installs in the body). Use Aura's canonical content hash, **not** skills.lock.json interop (locale-sensitive). Deep research verdict: in-place env-edit = universal load→set→whole-write atomic shape (Codex/Nanobot/Aura Phase-16 all whole-write; reuse `SaveManagedConfig`); a config-mutation **audit ledger is absent across the ecosystem** (Codex/Nanobot/Picobot/Odysseus + Aura's Phase-16 design) but is mandated by MCPW-02, is forward-aligned (MCP 2.4 enterprise audit logs), and reuses Aura's existing append-only pattern — adopted as the minimal industrial shape. OpenClaw confirmed irrelevant (plugin-hosting ≠ MCP-config; out-of-scope in both the plugin verdict and the Phase-16 design).

## Requirements

1. **MCP install from the cockpit (MCPW-01)**: Recipe or custom-stdio install, fully cockpit-driven, with a pre-save preview.
   - Current: install is CLI-only (`aura mcp install <recipe>` / `aura mcp add … -- <cmd>`); recipe `RequiredEnv` is never surfaced/prompted; no HTTP install endpoint; no destination/CLI-equivalent preview.
   - Target: a cockpit panel installs an MCP server from a built-in recipe (with its required env surfaced as a guided form) or a custom stdio command (command + args + env), shows the **equivalent CLI command** and the **managed-config destination** (`~/.aura/mcp/servers.json`, or the `AURA_MCP_SERVERS_JSON`/`AURA_MCP_CONFIG` override source in effect) **previewed before save**, persists via `SaveManagedConfig`, and after cockpit trust-approval + mount-time risk policy the server appears mounted with its live tool count — no terminal, no env-file editing.
   - Acceptance: installing a recipe and a custom stdio server from the cockpit each writes the expected `servers.json` entry; the preview shows the CLI-equivalent + the exact destination path **before** the write; a server name that already exists is rejected (no duplicate entry); after trust-approval the board shows it mounted with a live tool count.

2. **MCP env edit + enable/disable/remove + audit (MCPW-02)**: In-place env editing with redaction and reversible lifecycle, every mutation audited.
   - Current: there is NO env-edit command (edit = remove+re-add); enable/disable/remove exist CLI-only and write **no** audit row; there is no MCP audit ledger; `RedactEnv`/`ImportProfile` credential-preservation exists but is wired only into export/import.
   - Target: the cockpit edits an env value in place (load → set one value → whole-entry atomic write, preserving an unchanged secret submitted as its redacted placeholder); a saved secret renders as a **redacted chip** and is never returned raw; **required / optional / missing / placeholder** states are visually distinct; a still-placeholder **required** recipe var raises a **soft warning** (save allowed; server stays blocked/unhealthy until filled); the operator disables a server reversibly and removes a server behind a **confirmation**; every mutation (install / env-edit / enable / disable / remove / trust-approve) writes an append-only `aura.mcp_audit` row (new migration, reusing the 0010/0021 role+dual-trigger pattern) capturing actor + timestamp + action (+ reason on trust).
   - Acceptance: editing one env value preserves the other env values and any unchanged secret; no API response or DOM contains a raw secret value; the four env states render distinctly and a placeholder required var shows the soft warning while the save still succeeds; disable is reversible; remove requires confirmation; each of install/edit/enable/disable/remove/trust appends exactly one `mcp_audit` row that cannot be updated or deleted.

3. **MCP trust-approval + mount-time risk policy (MCPW-03)**: Cockpit-completable trust approval; denied/destructive tools never silently mounted.
   - Current: a custom server defaults to `blocked`; trust approval is CLI-only (`aura mcp trust`) and sets only `Trust.Class` (leaving `ApprovedBy`/`ApprovedAt`/`Reason` empty); blocked servers are silently skipped at mount with no warning row; per-tool risk enforcement happens before registry insert.
   - Target: the cockpit exposes a **trust-approve action** completing the install→approve→mounted flow without the CLI and populating `ApprovedBy`/`ApprovedAt`/`Reason` + an `mcp_audit` row; mutations pass trust-approval + mount-time risk policy before any tool enters the registry; a destructive or denied tool is **shown explicitly** and is **never silently mounted when an allowlist exists**; fail-soft mount warnings surface per server without stalling the board.
   - Acceptance: an unapproved custom server is `blocked` and mounts no tools; cockpit trust-approval flips it to runnable, writes the approval fields + an audit row, and the board then shows its mounted tool count; with an allowlist present, a denied/destructive advertised tool is shown explicitly and is absent from the runtime registry while allowed tools still mount; a failing/hung server surfaces a fail-soft warning on its row only.

4. **Skill install from source or skills.sh catalog (SKW-01)**: Cockpit install with the full validation checklist surfaced; install always RISKY.
   - Current: skill install is CLI/agent-tool/sandbox-`npx`-only; there is no backend catalog and no HTTP install endpoint; the validation checklist + content hash exist at the Writer/Loader but are not surfaced to a web client.
   - Target: the cockpit installs a skill from a **source field** (owner/repo, URL, or path) or a **skills.sh full-catalog search** result (the spike-proven `npx skills find`/`add -y` transport, behind an explicit operator-visible "external discovery enabled" toggle), routing the fetched body through the existing Writer gate; before activation the pipeline surfaces source, **content hash + preview**, **risk tier**, the **validation checklist** (sanitized env, `SKILL.md` parse, body cap, injection-literal blocklist, sanitized name/path) and the destination; install isolation = Aura's container boundary; install scripts are permitted; install is always rendered as **RISKY supply-chain input** + gated (approval queue) + Writer-validated.
   - Acceptance: a cockpit install from a source field and from a skills.sh search result each stage a skill through the gate showing all five validation-checklist items + content hash/preview + risk tier + destination; the install is labelled RISKY (never "safe"); an over-cap body or a blocklist hit fails the checklist; the external-discovery toggle state is explicit; an empty/invalid source is rejected with a safe error.

5. **Skill approval queue + resume activation (SKW-02)**: RISKY/DESTRUCTIVE actions queue with a resume token; pending stays inert; only operator approval activates.
   - Current: gated skill mutations stage to `pending/` + pause via `ask_user` (`ErrAwaitingUserInput` → `paused_states`) and surface in the Phase-25 cross-thread `/api/approvals` queue; activation is via approval resume or CLI `aura skills approve`; pending is non-runnable + never injected; no model-facing approve action exists. The Runner is documented as the **sole writer** of `aura.paused_states` (T-04-19) and the pause is **name-gated to `ask_user`** at the agent (only an `ask_user` tool call mints a pause). A **cockpit** REST install has no agent loop, so nothing in the model path mints the pause SKW-02 requires.
   - Target: a RISKY/DESTRUCTIVE skill action (install/create/update/delete) enters the approval queue with source, content preview, risk tier, and a **resume token**, reusing the HITL `Interrupt`/`Resume[]` protocol; a pending skill is **not runnable and not prompt-injectable**; activation happens **only** on the approval resume — there is **no model-facing approve path**; a stale/expired/already-consumed resume token renders its terminal state with no silent activation. **D-13 (Option A — the install→approval bridge):** the cockpit governance-write skills-install path mints an **operator-origin `ask_user` pause** via `askuser.Store.Insert` from the capability-gated cmd/aura skills-write provider, carrying `Kind=approval` + `ResumeContext={type:"skill_approval", skill_name}` (identical shape to a model-proposed skill-approval pause). It surfaces in the SAME unified `/api/approvals` queue (source-agnostic `ListPendingAll`) and resolves through the SAME source-agnostic `Runner.SubmitAnswers` → `ResumeHandler.Resume` → `Writer.Activate` (accept) / `DiscardPending` (decline/cancel). This **widens** the T-04-19 invariant from "the Runner is the sole writer of `aura.paused_states`" to "**the Runner AND the capability-gated operator-origin governance-write path** are the writers of `aura.paused_states`". Security envelope: the operator-origin pause is mintable **only** behind `RequireCapability(governance.write)` — no model/agent/unauthenticated caller can mint it; resolution is **operator-only** (no model approve); the staged install **never auto-activates** (activation happens only on the operator resume).
   - Acceptance: a gated cockpit action appears in the approval queue with source/preview/risk-tier/resume-token; the pending skill is absent from the active loader (cannot run, not injected); approving via the queue activates it (one audit row); an expired or already-consumed token cannot activate and shows a terminal state; no model tool call can approve or self-activate; the operator-origin pause is mintable only behind `governance.write`.

6. **Skill restore/archive across tabs + immutable audit (SKW-03)**: Lifecycle management from the cockpit; append-only audit.
   - Current: restore/archive are CLI/agent-tool-only; `.usage.json` carries last_used/use_count/status; the Phase-28 read board already shows active/pending/archived/audit tabs read-only; the `skill_audit` ledger is append-only and exposed read-only.
   - Target: the cockpit **restores** and **archives** skills across the separate **active / pending / archived / audit** tabs, each row showing capability scope, last used, use count, and TTL/archive state; the skills **audit ledger shows the install (and every lifecycle action) as an append-only row, newest-first**.
   - Acceptance: archive moves an active skill to archived and restore moves it back (reflected on next fetch); a restore whose name collides with an active skill is rejected with a safe error (no silent overwrite); each tab renders the correct set (and an empty state when empty); the audit tab renders newest-first with a stable tiebreak; no UI control mutates a `skill_audit` row.

7. **Fully cockpit-driven write boundary + no-leak plumbing (cross-cutting)**: Every flow completes in the cockpit, authenticated, capability-gated, secrets never leaked.
   - Current: no governance-write endpoints exist; the only mutation caps are `agent.run` / `identity.create`; the Phase-28 governance boards are read-only behind `governance.read`.
   - Target: all MCPW/SKW flows are completable from the cockpit with **no terminal and no manual config/env-file editing**; new mutating `/api/*` endpoints sit behind `RequireAuth` **and** a governance-write capability (e.g. `governance.write`, parity with `POST /agent/run`); raw secrets (MCP env values, Authula password, Telegram bot token) are never returned in any response, rendered in the DOM, or written to logs; empty datasets and an unavailable backend render safe empty/error states; new copy is i18n en+it.
   - Acceptance: each MCPW-01..03 and SKW-01..03 task can be completed start-to-finish in the cockpit (no CLI step); every new endpoint returns 401 unauthenticated and 403 without the governance-write capability; a forced backend failure renders a sanitized error (no stack/secret leak) and an empty dataset renders an empty state; a log scan over a full MCP-install + skill-install run contains no secret value; new copy has both en and it keys.

## Boundaries

**In scope:**
- Cockpit MCP write surface: recipe + custom-stdio install (with CLI-equiv + destination preview), in-place env editing with redaction + four-state distinction + soft placeholder warning, reversible enable/disable, confirmed remove, cockpit trust-approval (populating `ApprovedBy`/`ApprovedAt`/`Reason`).
- A new append-only `aura.mcp_audit` ledger (migration parallel to `skill_audit`/`identity_audit`) for install/edit/enable/disable/remove/trust.
- A thin in-place MCP env-edit backend path (load→set→whole-entry atomic write, credential-preserving) — the one MCP backend gap-filler.
- Cockpit skill write surface: install from a source field or skills.sh catalog search (`npx skills find`/`add -y` transport behind an explicit external-discovery toggle), the install pipeline routed through the existing Writer gate with the full validation checklist + content hash/preview + risk tier + destination surfaced.
- Skill approval queue reusing the HITL `Interrupt`/`Resume[]` protocol + resume token; restore/archive across active/pending/archived/audit tabs; the skills audit ledger surfaced with the new write rows.
- New skills.sh search + install + skill create/update/delete/restore/archive HTTP endpoints (the skills backend gap-filler) — wrapping the existing Writer/gate, not new gate logic.
- New authenticated `/api/*` mutating endpoints behind `RequireAuth` + a governance-write capability.
- i18n en+it for all new copy; web ≥85% vitest + ≥70% Stryker on touched dirs; Go owned-surface ≥85%; Playwright e2e + contrast-check (WCAG AA) on new surfaces.

**Out of scope:**
- **The model's ungated in-sandbox self-extension** (`npx skills add`, no ceremony, directive 2026-06-05) — left exactly as-is; Phase 29 adds only the operator-gated cockpit path (clean two-path boundary).
- **Scheduler write** (cancel / run-now / approve / create via HTTP) — deferred to v2 (GOVW-03); the scheduler board stays read-only.
- **`ui_control` / operator-OS shell** (open_panel, set_mode, command palette, dockable windows) — v2 (SHELL).
- **New core capability** — no new MCP transport, no new skill execution model, no new gate/scoring logic; Phase 29 wraps the Phase-11/16 backend (only the four named thin gap-fillers: env-edit path, `mcp_audit` ledger, skills HTTP write endpoints, governance-write capability).
- **skills.lock.json `computedHash` interop** — Aura's canonical content hash only (skills.lock is locale-sensitive, spike finding).
- **OAuth dynamic client registration for HTTP MCP** — deferred (Phase-16 deferral), not introduced here.
- **The standalone `:9081` loopback setup wizard** — unchanged; this adds the cockpit surface, it does not remove/refactor the loopback flow.
- **Multi-user RBAC beyond the existing `capability_grants`** — reuse the existing identity/capability layer only.
- **OpenClaw plugin hosting** — out of scope (plugin-host ≠ MCP-config).

## Constraints

- **Fully cockpit-driven (operator directive "easy for end user, no CLI, no env"):** every MCPW/SKW flow completes in the cockpit UI — no terminal command and no hand-editing of `servers.json` or any env file is required for install, env entry, trust-approval, skill install, or approval.
- **Write over the existing backend:** reuse the Phase-16 MCP manager (`SaveManagedConfig`, `RedactEnv`, trust classes, risk policy, `ProbeServer`), the Phase-11 skills Writer/gate/`ask_user`/`skill_audit`, the Phase-25 approval/resume protocol, and the Phase-28 read boards + REST pattern. Only the four named thin gap-fillers are new code.
- **Secrets never leaked:** raw MCP env values, the Authula password, and the Telegram bot token are never returned in an API response, rendered in the DOM, or written to logs — redacted chips / key-only env rows / `SanitizeString` only; an unchanged secret edited via its redacted placeholder is preserved, not overwritten with the placeholder text.
- **No privilege/trust escalation:** a custom/non-recipe server defaults to `blocked` and mounts nothing until explicit cockpit trust-approval; mutations are gated by the governance-write capability (no operator can grant a capability they lack — reuse the existing `HasCapability` + `*`-rejection invariants).
- **No model-facing approve; pending inert:** activation of a gated skill (and trust-approval of an MCP server) is operator-only via the approval resume / cockpit action; a pending skill is never loaded, run, materialized, or injected into any LLM context.
- **Operator-origin pause is capability-gated (D-13 / T-04-19 widening):** the cockpit skills-install path is a SECOND legitimate writer of `aura.paused_states` alongside the Runner, but ONLY behind `RequireCapability(governance.write)`. No model, agent, or unauthenticated caller can mint the pause (the agent stays name-gated to `ask_user`); the pause carries the same `skill_approval` `ResumeContext` so it resolves through the existing source-agnostic queue + resume bridge with no new approval/activation logic — the install never auto-activates.
- **Install is RISKY supply-chain:** the cockpit always renders skill install as RISKY with the full validation checklist — never "safe"; install runs inside Aura's container (the isolation boundary); install scripts are permitted; the control is the approval gate + Writer validation, never `--ignore-scripts`; external skills.sh discovery is on only behind an explicit, operator-visible toggle.
- **Append-only audit:** `mcp_audit` (new) and `skill_audit` (existing) are append-only via role grant (SELECT+INSERT only) + dual UPDATE/DELETE/TRUNCATE triggers; mutation + audit write are atomic in one `db.WithTx`.
- **Live MCP probe isolation:** the board's per-server doctor/tool-count probe keeps the Phase-28 bounded-timeout + row-isolation contract — a dead/hung server never stalls the board or blocks other rows.
- **Pattern fidelity:** thin `/api/*` handler → provider → JSON, parent-mux registration behind `RequireAuth`/`RequireCapability`, lazy React page + TanStack Query `useMutation`, en+it i18n. No new transport invented.
- **Quality gates (CLAUDE.md + frontend directive):** Go owned-surface coverage ≥85% across the full tag matrix; web vitest ≥85% (statements/branches/functions/lines) + Stryker ≥70% killed on touched dirs; Playwright e2e + contrast-check (WCAG AA) on new surfaces; i18n keys added to BOTH en + it; no source file >600 LOC.

## Acceptance Criteria

- [ ] An MCP recipe install and a custom-stdio install complete entirely in the cockpit (no CLI), each writing the expected `servers.json` entry; the CLI-equivalent + managed-config destination are previewed before the write; a duplicate name is rejected
- [ ] After cockpit trust-approval, a server appears mounted with its live tool count; the today-empty `ApprovedBy`/`ApprovedAt`/`Reason` fields are populated and an `mcp_audit` row is written
- [ ] Editing one MCP env value preserves the other values and any unchanged secret (submitted as its redacted placeholder); no API response or DOM contains a raw secret value
- [ ] Required / optional / missing / placeholder env states render distinctly; a still-placeholder required recipe var raises a soft warning but the save still succeeds and the server stays blocked until filled
- [ ] Disable is reversible; remove requires a confirmation; install/edit/enable/disable/remove/trust each append exactly one `mcp_audit` row, and an UPDATE/DELETE against `mcp_audit` is rejected (append-only proven)
- [ ] With an allowlist present, a destructive/denied advertised MCP tool is shown explicitly and is absent from the runtime registry while allowed tools still mount; a failing server surfaces a fail-soft warning on its row only
- [ ] A skill install from a source field and from a skills.sh catalog search each surface source + content hash/preview + risk tier + the five validation-checklist items + destination before activation, labelled RISKY (never "safe"); the external-discovery toggle state is explicit
- [ ] An over-cap body or an injection-blocklist hit fails the validation checklist; an empty/invalid source is rejected with a safe error
- [ ] A RISKY/DESTRUCTIVE skill action enters the approval queue with source/preview/risk-tier/resume-token; the pending skill is absent from the active loader (cannot run, not injected)
- [ ] Activation happens only on the approval resume (no model-facing approve); an expired or already-consumed resume token cannot activate and renders a terminal state
- [ ] Restore/archive move skills between active and archived (reflected on next fetch); a name-colliding restore is rejected; each of the four tabs renders the correct set with per-row metadata (capability scope, last used, use count, TTL/archive)
- [ ] The skills audit tab shows the install (and every lifecycle action) as an append-only row newest-first; no UI control mutates a `skill_audit` row
- [ ] Every new mutating endpoint returns 401 unauthenticated and 403 without the governance-write capability; an empty dataset renders an empty state and a forced backend failure renders a sanitized error (no crash, no stack/secret leak)
- [ ] A log scan over a full MCP-install + skill-install run contains no secret value; all new copy has both en and it i18n keys

## Edge Coverage

**Coverage:** 20/20 applicable edges resolved · 0 unresolved

| Category | Requirement | Status | Resolution / Reason |
|----------|-------------|--------|---------------------|
| boundary | MCPW-01 | ✅ covered | Server responding just under the probe timeout shows live tool count; just over → that row shows timed-out while the board still renders (reuses Phase-28 probe isolation) |
| precision | MCPW-01 | ⛔ dismissed | Tool count is an integer display — no arithmetic/rounding/tie-break — N/A |
| idempotency | MCPW-01 | ✅ covered | Installing a server whose name already exists is rejected (no duplicate `servers.json` entry); re-saving identical config is a no-op |
| concurrency | MCPW-01 | ✅ covered | An interrupted install leaves the prior config intact (atomic temp+rename write); no partial/corrupt servers |
| adjacency | MCPW-02 | ⛔ dismissed | Env is a keyed map (last-write-wins), not an interval/range with merge-on-touch semantics — N/A |
| empty | MCPW-02 | ✅ covered | Removing the last server leaves a valid empty config + empty-state board; clearing a required env value triggers the soft warning, not a crash |
| ordering | MCPW-02 | ✅ covered | Server rows render in a deterministic order (by name) after any edit/disable/remove |
| idempotency | MCPW-02 | ✅ covered | Disable/enable are idempotent toggles; a double-submitted remove removes once then 404s — at most one `mcp_audit` row |
| concurrency | MCPW-02 | ✅ covered | Concurrent env edits resolve to a consistent whole-entry write (no interleaved corruption); an interrupted remove/disable leaves `servers.json` valid |
| unclassified | MCPW-03 | ✅ covered | With an allowlist present, a destructive/denied advertised tool is shown explicitly, excluded from the registry, and surfaces a fail-soft warning without stalling the board or other servers |
| boundary | SKW-01 | ✅ covered | A skill body one byte over the body cap fails the validation checklist (install rejected/flagged); at-cap passes |
| empty | SKW-01 | ✅ covered | An empty/invalid source field is rejected with a safe error (no install); a skills.sh search with no matches renders an empty state |
| encoding | SKW-01 | ✅ covered | An injection literal expressed via Unicode-compatibility/equivalent forms is still caught (NFKC-normalized, case-folded scan); a non-ASCII skill name is rejected by the sanitized-name grammar |
| precision | SKW-01 | ⛔ dismissed | No arithmetic/rounding in the install pipeline — N/A |
| unclassified | SKW-02 | ✅ covered | A pending skill is absent from the active loader (cannot run / not injected); an expired or already-consumed resume token cannot activate (terminal state shown); only the approval resume activates (no model-facing approve) |
| boundary | SKW-03 | ✅ covered | A skill exactly at its TTL appears in the tab the backend reports (no client-side TTL recompute); restore→active, archive→archived reflected on next fetch |
| adjacency | SKW-03 | ✅ covered | Restoring an archived skill whose name collides with an active skill is rejected with a safe error (no silent overwrite) |
| empty | SKW-03 | ✅ covered | Each of the four tabs (active/pending/archived/audit) renders an empty state when its set is empty (no crash) |
| ordering | SKW-03 | ✅ covered | The audit tab renders rows newest-first with a stable tiebreak (created_at desc, id desc); lifecycle tabs render in a deterministic order |
| precision | SKW-03 | ⛔ dismissed | Use count is an integer display — no arithmetic/rounding — N/A |

## Prohibitions (must-NOT)

**Coverage:** 9/9 applicable prohibitions resolved · 0 unresolved

| Prohibition (must-NOT statement) | Requirement | Status | Verification / Reason |
|----------------------------------|-------------|--------|------------------------|
| MUST NOT render, return, or log any raw secret (MCP env value, Authula password, Telegram bot token) in any board/wizard/API response, DOM, or log — redacted chips / key-only rows only | MCPW-01, MCPW-02, SKW-01 | resolved | test |
| MUST NOT overwrite a stored secret when the operator submits its redacted placeholder unchanged (an edit of one var must preserve untouched secrets) | MCPW-02 | resolved | test |
| MUST NOT let a model tool call approve, trust-approve, or self-activate an MCP config mutation or a skill — activation is operator-only via the approval resume / cockpit action | MCPW-03, SKW-02 | resolved | test |
| MUST NOT let any model, agent, or unauthenticated caller mint the operator-origin skill-approval pause — the `askuser.Store.Insert` write that bridges a cockpit install into `/api/approvals` (D-13) is mintable ONLY behind `RequireCapability(governance.write)`; the agent path stays name-gated to `ask_user` (T-04-19 widening is capability-scoped, not a blanket relaxation) | SKW-02 | resolved | test |
| MUST NOT let a pending skill run, be materialized, or have its body injected into any LLM context (manifest / always-block) while pending | SKW-02 | resolved | test |
| MUST NOT present a skill install as "safe" — the cockpit always renders install as RISKY supply-chain input, gated by the approval queue and Writer validation, isolated by Aura's container boundary (install scripts are permitted; the control is NOT `--ignore-scripts`) | SKW-01 | resolved | judgment |
| MUST NOT silently mount a destructive or denied MCP tool when an allowlist exists — denied tools are shown explicitly and excluded from the runtime registry before model reach | MCPW-03 | resolved | test |
| MUST NOT mutate MCP config (install / env-edit / enable / disable / remove / trust) without appending an immutable `mcp_audit` row, nor remove a server without an explicit confirmation | MCPW-02 | resolved | test |
| MUST NOT auto-trust a newly added custom/non-recipe server, and MUST NOT enable external skills.sh discovery without an explicit operator-visible toggle (no silent trust/discovery escalation) | MCPW-01, SKW-01 | resolved | judgment |

*Canon-referral breadcrumbs (not minted here):* SSRF / arbitrary outbound on the MCP probe and skill-source fetch is canon web-safety (the DISP-04 error classes + `/api/image-proxy` SSRF guard + Phase-28's "configured-servers-only" judgment) — owned by `/gsd-secure-phase`. Skill name/path traversal is canon (the `SanitizeName` grammar chokepoint + `/gsd-secure-phase`). Authula password at-rest hashing is canon credential/GDPR — owned by Authula + `/gsd-secure-phase`. Arbitrary-code-execution risk of `npx`/post-install scripts is the reason install is RISKY + container-isolated + approval-gated + Writer-validated (prohibition #5); the sandbox isolation mechanism itself is owned by the Phase-8 sandbox posture.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                                 |
|--------------------|-------|------|--------|-----------------------------------------------------------------------|
| Goal Clarity       | 0.90  | 0.75 | ✓      | Write-over-existing-backend; "no CLI, no env" directive sharpens it    |
| Boundary Clarity   | 0.82  | 0.70 | ✓      | skills.sh source + MCP edit/audit + model-path-untouched all locked    |
| Constraint Clarity | 0.85  | 0.65 | ✓      | No-leak, no-model-approve, RISKY-install, soft-warning, cockpit-trust  |
| Acceptance Criteria| 0.82  | 0.70 | ✓      | 14 pass/fail criteria                                                  |
| **Ambiguity**      | 0.146 | ≤0.20| ✓      | Gate passed (all dimensions above minimum)                            |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective     | Question summary                                          | Decision locked                                                                          |
|-------|-----------------|----------------------------------------------------------|------------------------------------------------------------------------------------------|
| 1     | Researcher      | Skill install source (catalog has no backend today)?     | skills.sh **full-catalog search + install** via the `npx skills find`/`add` transport     |
| 1     | Boundary Keeper | MCP env-edit + audit ledger scope (gaps in backend)?     | Deferred to deep research (online + D:/tmp + OpenClaw) → resolved Round 2                  |
| 1     | Boundary Keeper | Touch the ungated in-sandbox model self-extension?        | **Leave model self-extension untouched**; cockpit path is the only gated operator path    |
| 2     | Researcher      | Deep-research verdict on env-edit + config-audit?         | Env-edit in-place (load→set→whole-write); **add `mcp_audit` ledger** — "easy, no CLI/env"  |
| 2     | Failure Analyst | Required env var still a placeholder at save?             | **Soft warning** — save allowed, server stays blocked/unhealthy until filled              |
| 2     | Boundary Keeper | Complete MCP trust-approval in cockpit or via CLI?        | **Cockpit trust-approve action** — populates `ApprovedBy`/`ApprovedAt`/`Reason` + audit    |

---

*Phase: 29-governance-write-mcp-configuration-skills-install*
*Spec created: 2026-06-20*
*Next step: /gsd-discuss-phase 29 — implementation decisions (REST shapes for the write endpoints, the skills.sh search/install transport wiring, the `mcp_audit` migration + env-edit path, the governance-write capability string, cockpit panel/form layout)*
