# Phase 28: Governance Boards + Web Onboarding — Specification

**Created:** 2026-06-19
**Ambiguity score:** 0.18 (gate: ≤ 0.20)
**Requirements:** 7 locked

## Goal

The operator can view the substrate's governance state read-only across three cockpit boards (MCP servers, the skills library, the scheduler), and complete a web onboarding wizard that drives the existing 5-step onboarding LoopAgent to seed an `Agent.md` profile AND fully provisions a new loginable identity (identity record + capability grants + Authula login + live Telegram channel link) — capability-gated, no-privilege-escalation, atomic on confirm, with raw secrets never exposed.

## Background

Grounded in the current codebase (scouted 2026-06-19):

- **All three governance backends exist and are mature, but have ZERO web/REST exposure — they are CLI-only today.**
  - **MCP**: `mcp.LoadManagedConfig` (`~/.aura/mcp/servers.json`) + `manager.SnapshotStatus(doc)` (Name / Trust class / Runtime / StartupState / AuthStatus / LastError) + redaction (`manager.RedactSecrets`, `secret.IsSecretEnvKey`). The **mounted tool count and doctor result are LIVE probes** (`aura mcp status` / `mcpDoctorAll`), not stored config — there is no cached tool count.
  - **Skills**: filesystem loader (active dir + `pending/` + `archived/`) + append-only `aura.skill_audit` ledger (`AuditStore.ListAudit`, dual-enforced no UPDATE/DELETE). Pending skills live in `pending/` and are non-runnable + injection-blocklisted by construction.
  - **Scheduler**: `aura.scheduler_tasks` + `aura.agent_job_runs` (with a `last_heartbeat_at` column) + `cron.Store` (`ListActiveTasks`, `GetTask`, run-history query) + `aura task doctor`.
- **The cockpit pattern is proven**: the Approval Center (Phase 25) and Graph Explorer (Phase 27) establish the template — a thin read-only `/api/*` handler in `internal/agui/*_api.go`, registered under `RequireAuth` in the parent mux (`cmd/aura/serve_webui.go`), consumed by a lazy React page + TanStack Query + i18n en/it (`web/src/i18n/resources.ts`). **No governance or onboarding UI exists yet** — the `settings` / `tree` / `displays` modes are placeholders.
- **The onboarding LoopAgent exists** (`internal/onboarding/`): a 5-step interview (Identity / Work / Projects / Social / Style → `StepDraft`) with `answer` / `confirm` / `edit` / `skip` / `cancel` / `restart` intents. The `s.prompted` flag enforces "one prompt per step" — this IS the **no-duplicate-LLM-turns** guarantee. An LLM answer-extractor with a raw-text fallback exists. `Agent.md` (8 sections) is rendered by `internal/profile/render.go` and stored at `~/.aura/agents/<id>/`. **The LoopAgent is CLI/Telegram-only — not reachable over HTTP/SSE today.**
- **Identity layer (Slice 1.7)**: `aura.identities` + `aura.capability_grants` (seeded `local` with `*`). Authula enrollment already binds operator→identity (`webauth.LinkOperator` / `ResolveIdentityID`, `aura.identity_auth_links`).
- **Telegram linking**: the `:9081` loopback setup wizard mints single-use, 1h-TTL Telegram deep-links (`https://t.me/<bot>?start=<token>`) + ASCII QR and consumes them atomically (`ConsumeOnboarding`). **Phase 30 (`30-SPEC.md`) currently owns the web Telegram link/QR surface** — this phase ABSORBS that surface per operator directive (see Boundaries).

## Requirements

1. **MCP governance board (GOV-01)**: Read-only view of every configured MCP server with a live health probe.
   - Current: MCP status/doctor/tool-count are CLI-only (`aura mcp status`); no web endpoint or UI exists; tool count + doctor are live probes, not stored.
   - Target: A cockpit board lists every server (source, trust class, enabled state, env health, startup state) and, on load, runs a live per-server doctor + mounted-tool-count probe with a bounded per-server timeout; raw secret env values are shown only as redacted chips.
   - Acceptance: Board renders all configured servers with status; a healthy server shows its live tool count + doctor OK; a dead/hung server shows a timed-out/error state for ITS row only while every other row still renders; no response or DOM contains a raw secret value.

2. **Skills governance board (GOV-02)**: Read-only view across four lifecycle tabs.
   - Current: skills lifecycle is CLI/filesystem (`aura skills audit`) + the `aura.skill_audit` ledger; no web surface.
   - Target: A cockpit board shows separate **active / pending / archived / audit** tabs; each skill row shows capability scope, last used, use count, TTL/archive state, risk tier, and content hash; the audit tab shows append-only ledger rows.
   - Acceptance: Each tab lists the correct lifecycle set; a pending skill cannot be run or activated from the board (no such control exists) and its body is not injected into any LLM context; the audit tab renders ledger rows in reverse-chronological order.

3. **Scheduler governance board (GOV-03)**: Read-only view of tasks + run history.
   - Current: scheduler is CLI-only (`aura task list/runs/doctor`); no web surface.
   - Target: A cockpit board lists scheduled tasks (kind, schedule, next run, status) with per-task run history and last heartbeat, paginated, mutating nothing.
   - Acceptance: Board lists tasks with next-run + status; selecting a task shows its run history (status / started / heartbeat / summary) paginated with a default limit; no control mutates any task or run.

4. **Web onboarding interview (ONBD-02)**: The wizard drives the existing 5-step LoopAgent with confirm/edit/skip and no duplicate LLM turns.
   - Current: the onboarding LoopAgent is CLI/Telegram-only; not reachable over HTTP/SSE.
   - Target: A web wizard drives the SAME `internal/onboarding` state machine through Identity / Work / Projects / Social / Style → `Agent.md` draft, with `confirm` / `edit` / `skip`, emitting each step prompt exactly once.
   - Acceptance: Completing the wizard writes an `Agent.md` whose 8 sections match the LoopAgent output; replaying a step does not emit a second LLM extraction turn for that step; `skip` ends without writing a profile and `edit` re-renders the draft from the same extracted facts (no re-prompt).

5. **Identity provisioning (ONBD-01a)**: The wizard creates a new loginable identity, capability-gated, no-escalation, atomic.
   - Current: identity creation is not exposed to the cockpit; only the seeded `local` identity exists; capability_grants seed `local` with `*`.
   - Target: The wizard writes an `aura.identities` row + `aura.capability_grants` + an Authula login credential so the new identity is loginable; only an operator holding the identity-create capability may proceed; the new identity can never be granted `*` nor any capability the creator lacks; provisioning is atomic and writes an audit row.
   - Acceptance: An operator WITHOUT the identity-create capability is rejected (no row written); a grant of `*` or a creator-lacked capability is rejected; an abandoned/failed flow leaves no partial identity (no orphan `identities`/`capability_grants`/Authula row); a successful creation writes exactly one immutable audit row and the new identity can authenticate.

6. **Telegram channel link (ONBD-01b — absorbs Phase 30)**: The wizard links the new identity's Telegram channel live via deep-link + QR.
   - Current: Telegram deep-link/QR mint+consume exists only on the `:9081` loopback setup wizard; no cockpit surface; Phase 30 holds the unstarted spec.
   - Target: The wizard mints a single-use, time-bounded onboarding token, renders a `https://t.me/<bot>?start=<token>` deep-link + a scannable QR, and links the new identity's Telegram channel when the token is consumed, invalidating it on consume.
   - Acceptance: The wizard renders a working deep-link + scannable QR; consuming the token once links the identity's Telegram account; a second consume or a consume after expiry is rejected; the bot token is never rendered or logged.

7. **Authenticated REST boundary + no-leak plumbing (cross-cutting)**: New endpoints follow the proven pattern with secrets-never-leaked and safe empty/error states.
   - Current: no governance/onboarding endpoints exist; the Approval/Graph endpoints establish the `/api/*` + `RequireAuth` (+ `RequireCapability` on mutations) pattern.
   - Target: All board + wizard data is served by new `/api/*` endpoints behind `RequireAuth`, with the identity-create mutation additionally behind `RequireCapability`; raw secrets (Authula password, MCP env, Telegram bot token) are never returned in any response or written to logs; empty datasets and an unavailable backend render a safe empty/error state.
   - Acceptance: Every new endpoint requires auth (401 unauthenticated); the create mutation requires the capability (403 without it); an empty dataset renders an empty state (no crash); a forced backend failure renders a sanitized error (no stack/secret leak); a log scan over a full provisioning run contains no secret value.

## Boundaries

**In scope:**
- Three read-only cockpit boards: MCP servers, skills library (active/pending/archived/audit), scheduler (tasks + run history).
- The MCP board's live, per-server, timeout-bounded doctor + tool-count probe (read-only execution, mutates nothing).
- A web onboarding wizard driving the existing 5-step LoopAgent → `Agent.md` (confirm/edit/skip, no duplicate LLM turns).
- Full identity provisioning from the wizard: `aura.identities` row + `capability_grants` + Authula login + live Telegram channel link (deep-link + QR), capability-gated, no-escalation, atomic, audited.
- New authenticated `/api/*` REST endpoints (read for boards; create mutation behind `RequireCapability`).
- i18n en/it for all new copy; web ≥85% vitest coverage + ≥70% Stryker mutation on touched dirs; Go owned-surface ≥85% coverage; Playwright e2e + contrast-check on new surfaces.

**Out of scope:**
- **All governance WRITE actions** (skills approve/activate/archive/install; MCP install/edit/enable/disable/remove) — that is **Phase 29** (the read boards are the foundation those writes extend).
- **Scheduler write** (cancel / run-now / approve / create via HTTP) — deferred to **v2 (GOVW-03)**.
- **`ui_control` / operator-OS shell** (`open_panel`, `set_mode`, command palette, dockable windows) — **v2 (SHELL)**, highest abuse surface.
- The standalone `:9081` loopback setup wizard — it remains as-is; this phase adds the cockpit surface, it does not remove or refactor the loopback flow.
- Multimodal input on onboarding endpoints — endpoints are text/JSON; Telegram remains the multimodal channel.
- ⚠️ **Phase 30 (Telegram Onboarding Link + QR) is absorbed here** per operator directive ("Full Telegram link in Phase 28"). The ROADMAP + `30-SPEC.md` will need amendment (Phase 30 emptied/removed) — flagged for discuss-phase / a roadmap edit; this SPEC does NOT itself edit the ROADMAP.

## Constraints

- **Live MCP probe**: bounded per-server timeout; failures isolated to a single row; a dead/hung server must never block or stall the board render.
- **No privilege escalation**: a wizard-created identity can never receive `*` nor any capability the creating operator lacks.
- **Atomic provisioning**: identity + capability_grants + Authula login + Telegram link succeed together on final confirm or roll back entirely — no half-provisioned identity, ever.
- **Secrets never leaked**: raw MCP env values, the Authula password, and the Telegram bot token are never returned in an API response, rendered in the DOM, or written to logs (redacted chips / placeholders only).
- **Auth**: every new endpoint behind `RequireAuth`; the identity-create mutation additionally behind `RequireCapability` (parity with `POST /agent/run`).
- **Pattern fidelity**: new endpoints/pages follow the Approval Center (Phase 25) + Graph Explorer (Phase 27) pattern (`internal/agui/*_api.go` + parent-mux registration + lazy React page + TanStack Query). No new transport invented.
- **Quality gates** (per CLAUDE.md + frontend directive): Go owned-surface coverage ≥85% across the full tag matrix; web vitest ≥85% (statements/branches/functions/lines) + Stryker ≥70% killed on touched dirs; Playwright e2e + contrast-check (WCAG AA) on new surfaces; i18n keys added to BOTH en + it; no source file >600 LOC.

## Acceptance Criteria

- [ ] MCP board renders every configured server (source, trust, enabled, env health, startup state) with redacted secret chips (no raw secret in response/DOM)
- [ ] MCP board shows live tool count + doctor per server; a dead/hung server fails its row only (timeout) while all other rows render
- [ ] Skills board shows separate active / pending / archived / audit tabs with the per-skill metadata (capability scope, last used, use count, TTL/archive, risk tier, content hash)
- [ ] A pending skill has no run/activate control on the board and its body is not injected into any LLM context
- [ ] Skills audit tab renders append-only ledger rows newest-first
- [ ] Scheduler board lists tasks (kind, schedule, next run, status) and per-task run history (status/started/heartbeat/summary), paginated, mutating nothing
- [ ] Onboarding wizard drives the 5-step LoopAgent → `Agent.md` with confirm/edit/skip; each step prompt is emitted exactly once (no duplicate LLM turn on replay/edit)
- [ ] Completing the wizard writes an `Agent.md` matching the LoopAgent's 8-section output for the target identity
- [ ] Identity-create is rejected for an operator without the capability (no row written); `*` or a creator-lacked capability grant is rejected
- [ ] An abandoned/failed wizard leaves no half-provisioned identity (no orphan identities/grants/Authula/Telegram rows); a successful create writes exactly one immutable audit row and the identity can authenticate
- [ ] The wizard renders a working Telegram deep-link + scannable QR; one consume links the channel; a replayed or expired token is rejected
- [ ] Every new endpoint returns 401 unauthenticated; the create mutation returns 403 without the capability
- [ ] Empty datasets render an empty state and an unavailable backend renders a sanitized error (no crash, no stack/secret leak); a log scan over a full provisioning run contains no secret value

## Edge Coverage

**Coverage:** 22/22 applicable edges resolved · 0 unresolved

| Category | Requirement | Status | Resolution / Reason |
|----------|-------------|--------|---------------------|
| boundary | R1 | ✅ covered | Server responding just under the timeout returns live data; just over → that row shows timed-out/unknown, board still renders (AC: probe row isolation) |
| adjacency | R1 | ⛔ dismissed | No interval/range-merge semantics in a status board — N/A |
| empty | R1 | ✅ covered | Zero configured MCP servers → board renders an empty state, no crash (AC: empty state) |
| ordering | R1 | ✅ covered | Server rows render in a deterministic order (by name) |
| precision | R1 | ⛔ dismissed | No arithmetic/rounding; tool count is an integer display — N/A |
| boundary | R2 | ✅ covered | Board reflects backend-reported lifecycle state; no client-side TTL recompute — a skill exactly at TTL appears in whichever tab the backend reports |
| precision | R2 | ⛔ dismissed | Use count is an integer; no rounding — N/A |
| unclassified | R3 | ✅ covered | Empty scheduler → empty state; run history paginates with a default limit and a stable last page, no mutation (AC: scheduler board) |
| adjacency | R4 | ✅ covered | Submitting the same step twice does not advance twice or emit a duplicate LLM turn (prompt emitted once per step) |
| empty | R4 | ✅ covered | An empty or skipped step answer is recorded as empty/omitted in the draft without error |
| ordering | R4 | ⛔ dismissed | Step order is a fixed sequence (Identity→Style), not a data-ordering/tie-break concern — N/A |
| adjacency | R5 | ⛔ dismissed | No range/interval semantics in identity creation — N/A |
| empty | R5 | ✅ covered | Creating an identity with an empty or duplicate name is rejected with a safe error; no row written |
| ordering | R5 | ⛔ dismissed | Single-record creation; no ordering — N/A |
| idempotency | R5 | ✅ covered | Double-submitting create does not produce two identities (atomic/idempotent on confirm) |
| concurrency | R5 | ✅ covered | An interrupted/abandoned flow leaves no half-provisioned identity (atomic rollback); parallel creates do not corrupt state |
| unclassified | R6 | ✅ covered | Consuming an already-consumed or expired token is rejected; token is single-use + time-bounded (invalidated on consume) |
| adjacency | R7 | ⛔ dismissed | No range semantics — N/A |
| empty | R7 | ✅ covered | Empty datasets render a safe empty state; an unavailable backend renders a safe error state, never a crash |
| ordering | R7 | ⛔ dismissed | List ordering is covered per-board (R1/R3); endpoints impose no additional ordering contract |
| idempotency | R7 | ✅ covered | Read (GET) endpoints are side-effect-free; the create mutation's idempotency is covered by R5 |
| concurrency | R7 | ✅ covered | A backend failure mid-request yields a safe sanitized error, not a partial/secret-leaking response |

## Prohibitions (must-NOT)

**Coverage:** 5/5 applicable prohibitions resolved · 0 unresolved

| Prohibition (must-NOT statement) | Requirement | Status | Verification / Reason |
|----------------------------------|-------------|--------|------------------------|
| MUST NOT make a pending skill runnable, activatable, or prompt-injectable from the skills board (no run/activate control; body never injected into an LLM context) | R2 | resolved | test |
| MUST NOT render or return any raw secret (MCP env value, Authula password, Telegram bot token) in any board/wizard/API response or write it to logs | R1, R5, R6, R7 | resolved | test |
| MUST NOT allow identity creation to escalate privilege — no `*` grant, and no capability the creating operator lacks | R5 | resolved | test |
| MUST NOT create a loginable identity without writing an immutable audit row | R5 | resolved | test |
| MUST NOT let the live MCP probe target anything beyond the already-configured servers (no operator-supplied or arbitrary outbound targets) | R1 | resolved | judgment |

*Canon-referral breadcrumbs (not minted here):* Authula password at-rest hashing/storage is canon credential/GDPR — owned by Authula + `/gsd-secure-phase`. Generic SSRF on outbound probes is canon web-safety (the DISP-04 error classes + the `/api/image-proxy` SSRF guard) — `/gsd-secure-phase`; prohibition #5 keeps only the bespoke "configured-servers-only" scope.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.85  | 0.75 | ✓      | 4 deliverables, max onboarding scope locked                  |
| Boundary Clarity   | 0.80  | 0.70 | ✓      | Read/write line crisp (Ph29 writes, v2 scheduler); Ph30 absorbed |
| Constraint Clarity | 0.80  | 0.65 | ✓      | Probe timeout/isolation, no-escalation, atomicity, no-leak   |
| Acceptance Criteria| 0.82  | 0.70 | ✓      | 13 pass/fail criteria                                        |
| **Ambiguity**      | 0.18  | ≤0.20| ✓      | Gate passed                                                  |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective     | Question summary                                  | Decision locked                                                                 |
|-------|-----------------|--------------------------------------------------|--------------------------------------------------------------------------------|
| 1     | Researcher      | What does onboarding "links identity" mean?      | Identity creation too (not profile-only)                                        |
| 1     | Researcher      | How does the MCP board source tool count/doctor? | Live probe on board load                                                        |
| 1     | Researcher      | How faithful is the web wizard to the LoopAgent? | Mirror the existing 5-step LoopAgent exactly                                    |
| 2     | Simplifier      | Irreducible core / sequencing of the 4 deliverables? | All four; boards first (low-risk) → onboarding (high-risk)                  |
| 2     | (clarify)       | What does "create identity" include?             | + everything (capabilities + auth + channel)                                   |
| 2     | (clarify)       | Can a created identity authenticate this phase?  | (reconciled in round 3)                                                         |
| 3     | Boundary Keeper | Reconcile "+everything" vs "inert record"        | Full live provisioning — created identity is loginable + channel live           |
| 3     | Boundary Keeper | Read/write line for the 3 boards?                | Read + read-only MCP probe only; all writes → Ph29; scheduler writes → v2       |
| 3     | Boundary Keeper | Does Ph28 touch channel linking vs Ph30?         | Full Telegram link in Phase 28 (absorb Phase 30)                               |
| 4     | Failure Analyst | Authz guard for identity creation?               | Capability-gated + no-escalation + audit row                                    |
| 4     | Failure Analyst | Live probe failure/timeout behavior?             | Per-server timeout + isolated failure (never stalls board)                      |
| 4     | Failure Analyst | Partial/abandoned flow + what must never leak?   | Atomic (all-or-nothing on confirm) + no raw secret rendered/returned/logged     |

---

*Phase: 28-governance-boards-web-onboarding*
*Spec created: 2026-06-19*
*Next step: /gsd-discuss-phase 28 — implementation decisions (board layout, REST shapes, wizard transport, Authula provisioning mechanism, Phase 30 ROADMAP amendment)*
