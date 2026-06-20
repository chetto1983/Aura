# Phase 28: Governance Boards + Web Onboarding — Research

**Researched:** 2026-06-20
**Domain:** Integration phase — exposing 3 mature CLI-only governance backends + the onboarding LoopAgent + identity/Authula/Telegram provisioning over NEW authenticated `/api/*` REST + React cockpit pages, following the proven Phase-25/27 template.
**Confidence:** HIGH (every seam read at file:line in this session; saga + Authula create path verified against the vendored module source)

> **Method note:** This is an INTEGRATION phase. All backends exist. This research is a **reuse map with exact signatures**, two hard-problem resolutions (cross-store saga + live MCP probe concurrency), the D-07 PRD-amendment exact-text targets, and the mandatory `## Validation Architecture`. Claims are tagged `[VERIFIED: <file:line>]` (read this session), `[CITED: <doc>]`, or `[ASSUMED]`. No external packages are installed by this phase (QR render reuses an already-vendored lib), so the Package Legitimacy Audit is N/A and noted as such.

---

<user_constraints>
## User Constraints (from 28-CONTEXT.md)

### Locked Decisions (D-01 … D-09 — implementation, not requirements)
- **D-01** — ONE new `governance` workspace mode with 3 internal tabs (MCP / Skills / Scheduler). Add `'governance'` to `web/src/shell/modes.ts` `MODES`+`LIVE_MODES`; `AppShell` center-swaps to a lazy `Governance` workspace on `surface === 'governance'` (mirror the Phase-27 `graph` swap). NOT three top-level modes; NOT folded into `settings`.
- **D-02** — Each board = master-list + detail/inspector. MCP row = source/trust/enabled/env-health/startup (redacted chips); detail = live doctor + tool count. Skills = 4 lifecycle tabs (active/pending/archived/audit); row = capability scope/last used/use count/TTL-archive/risk tier/content hash; audit = append-only newest-first; pending rows have NO run/activate control. Scheduler row = kind/schedule/next-run/status; detail = paginated run history.
- **D-03** — Transport = REST per-step JSON (no SSE). `/api/onboarding/*` step endpoint applies an `onboarding.Intent` (answer/confirm/edit/skip) to the **server-held** `onboarding.Session`, returns `{content, step, status, draft, preferences}`. `Session.prompted` = the no-duplicate-LLM-turn guarantee; the single LLM extraction runs server-side inside the step call.
- **D-04** — ONE dedicated full-screen wizard (own route/overlay, NOT a governance tab): provision identity (login + capability grants) → Telegram link (deep-link + QR) → 5-step Agent.md interview → confirm. One flow.
- **D-05** — Credential = operator sets email + initial (temp) password; minted via Authula's **server-side** user-create (bypasses the disabled `/auth/signup` route). New user enrolls TOTP on first login (no mailer). Password NEVER returned or logged after creation.
- **D-06** — Capability picker = checklist of the **creator's own** grants minus `*`. Server re-validates (subset ⊆ creator-grants AND no `*`) before the atomic write (belt-and-suspenders).
- **D-07** — **PRD-AMENDMENT (BLOCKING)**: Phase 28 introduces a 2nd web-loginable identity → multi-operator login. Relax the single-user `OperatorUserID` pin so >1 Authula user resolves through `aura.identity_auth_links`; add the server-side user-create path; **authz stays `capability_grants`-based — NO new RBAC, NO route-scoping beyond `RequireCapability`.** Land the PROJECT.md + ROADMAP + prd.md amendment commit **BEFORE** provisioning impl.
- **D-07b** — Atomicity is a **cross-store saga**, not a single PG tx (Authula owns its own `database/sql` pool over the `authula` schema). All-or-nothing-on-confirm with compensation/rollback. Acceptance: no orphan identity/grant/Authula/Telegram row, ever.
- **D-08** — Reuse the existing `:9081` setup-wizard mint/`ConsumeOnboarding` (single-use, 1h TTL); surface it in the web wizard. Mint a token for the NEW identity, render `t.me/<bot>?start=<token>` + scannable QR; replay/expired consume rejected; bot token never rendered/logged.
- **D-09** — Mark ROADMAP Phase-30 `✅ absorbed-into-28` with a pointer; convert `30-SPEC.md` to a tombstone → `28-SPEC §ONBD-01b`. Bundle this edit with the D-07 amendment commit, via gsd tooling (NEVER a direct ROADMAP Write — anti-pattern #15).

### Claude's Discretion (research/planner-resolved — resolved in this doc)
- Live MCP probe concurrency model + per-server timeout + isolated-row failure + optional cache → **§Hard Problem 3**.
- REST shapes `/api/governance/*` + `/api/onboarding/*` + the create mutation + pagination defaults → **§REST Endpoint Shapes**.
- Audit-row shape for identity creation (immutable) → **new `aura.identity_audit` (migration 0021)**, §Hard Problem 1.
- Wizard resume/abandonment: server-held session TTL; abandoned → atomic rollback → **§Hard Problem 4**.
- `web/src/governance/` + `web/src/onboarding/` layout, lazy-chunk boundaries, empty/loading/error, desktop+mobile breakpoints → **§Industrial Pattern Survey + §UI mount map** (held to 28-UI-SPEC).
- i18n keys (en+it) → split bundles `resources.governance.ts` + `resources.onboarding.ts` (28-UI-SPEC §Copywriting).

### Deferred Ideas (OUT OF SCOPE — do not build)
- ALL governance WRITE surfaces (skills approve/activate/archive/install; MCP install/edit/enable/disable/remove) → **Phase 29**.
- Scheduler write (cancel/run-now/approve/create via HTTP) → **v2 (GOVW-03)**.
- `ui_control` / operator-OS shell (dockable windows, icon rail, command palette, `open_panel`/`set_mode`) → **v2 (SHELL)** — the deep-research mandate says take layout IDEAS ONLY, no shell machinery.
- Email-invite / self-service signup (no mailer wired) — D-05 uses operator-set credentials.
- Full multi-user RBAC / route-scoping / per-identity session isolation — authz stays `capability_grants` only.
- The `:9081` loopback setup wizard refactor — out of scope; add the cockpit surface, leave loopback as-is.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support (which findings enable it) |
|----|-------------|----------------------------------------------|
| **GOV-01** | MCP board read-only + live per-server doctor + tool-count probe, timeout-bounded, isolated failure, redacted secrets | Reuse map §MCP; concurrency model §Hard Problem 3; `mcpDoctorAll` + `SnapshotStatus` + `RedactSecrets` seams verified |
| **GOV-02** | Skills board read-only across active/pending/archived/audit; per-skill metadata; pending non-runnable | Reuse map §Skills; `skills.Loader.List()` + `AuditStore.List()` (append-only) verified; **Wave-0 gap: per-tab loaders + per-skill metadata aggregation** |
| **GOV-03** | Scheduler board read-only — tasks + paginated run history + heartbeat | Reuse map §Scheduler; `cron.Store.ListActiveTasks`/`GetTask` verified; **Wave-0 gap: NO paginated `ListRunsForTask` query exists — must add (migration not needed, sqlc query only)** |
| **ONBD-01** (a+b) | New loginable identity (identity + grants + Authula login + live Telegram link), capability-gated, no-escalation, atomic, audited | §Hard Problem 1 (saga); §Hard Problem 4 (session); Authula `SignUp`-sequence verified; Telegram `InsertPending`/`ConsumeOnboarding` reuse verified |
| **ONBD-02** | Web wizard drives the 5-step LoopAgent → Agent.md, confirm/edit/skip, no duplicate LLM turns | §Hard Problem 4; `onboarding.Session` + `prompted` flag + `ExtractDraft` + `profile.render` verified |
</phase_requirements>

---

## Summary

Phase 28 is a **pure integration phase over an already-complete substrate**. Every governance backend (MCP manager, skills loader + audit ledger, cron store), the onboarding state machine, the identity/capability store, the embedded Authula provider, and the Telegram mint/consume token flow exist and are battle-tested — they are simply **CLI-only and have no web exposure**. The work is to expose them over NEW authenticated `/api/*` REST endpoints (read for the 3 boards; one capability-gated create mutation) and to build a React `governance` workspace + a full-screen onboarding wizard, following the **proven Phase-25 Approval Center + Phase-27 Graph Explorer template** (`internal/agui/*_api.go` thin handlers registered on `Server.Mux()`, parent-mux mount behind `RequireAuth` in `cmd/aura/serve_webui.go`, lazy React page + TanStack Query + split i18n bundle). No new transport is invented.

The two genuinely hard problems are both in the onboarding/provisioning leg: (1) provisioning spans the `aura.*` schema (one pgx tx) **and** the Authula `authula` schema on its **own `database/sql` pool** **and** a Telegram token — Authula's separate pool makes this a **cross-store saga**, not a single transaction, so it needs an explicit ordered flow with a compensation leg per step; and (2) the live MCP doctor/tool-count probe must run per-server with a bounded timeout so a dead/hung server fails **only its own row** and never stalls the board render. Both are resolved concretely below. The D-07 PRD-amendment (relax the single-operator boundary) is **BLOCKING** and must land before any provisioning code, bundled with the D-09 Phase-30 absorption, via gsd tooling.

**Primary recommendation:** Build in the SPEC's locked sequence — **boards first (low-risk reads)**, then the onboarding wizard (high-risk provisioning). Land the D-07/D-09 PRD-amendment commit before the provisioning wave. Reuse the verified seams below verbatim; the only net-new backend code is (a) one sqlc query `ListRunsForTask` for GOV-03 pagination, (b) one migration `0021_identity_audit` for the immutable provisioning audit row, (c) the saga orchestrator, and (d) the thin REST handlers. Everything else is wiring + frontend.

**Primary recommendation:** Use the existing `SetXxx(...)`-on-`*agui.Server` injection pattern (verified `SetGraphView`/`SetApprovalStore`/`SetImageProxy`) to wire the new governance + onboarding stores from the `cmd/aura/serve.go` composition root — never expand the `NewServer` constructor.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| MCP server list + live doctor/tool-count probe | **API/Backend** (`internal/agui` handler → `internal/mcp` + `mcp/manager`) | Browser (render redacted rows) | The probe spawns/dials MCP servers — a privileged server-side action; the browser only renders. Probe NEVER moves to the client (prohibition #5: configured-servers-only). |
| Skills lifecycle view (active/pending/archived/audit) | **API/Backend** (`internal/agui` → `internal/skills` loader + `AuditStore`) | Browser (tabbed render) | Filesystem scan + append-only PG ledger are server-owned; pending skills must be non-runnable by construction (loader never mounts pending into LLM context). |
| Scheduler tasks + run history | **API/Backend** (`internal/agui` → `internal/cron.Store`) | DB (pagination via SQL LIMIT/OFFSET) | Read-only over `aura.scheduler_tasks` + `aura.agent_job_runs`. No mutation. Pagination is a DB concern. |
| Onboarding interview state machine | **API/Backend** (server-held `onboarding.Session` + `ExtractDraft`) | Browser (renders prompt/draft) | The LLM extraction is server-side (single turn per step); the session MUST live server-side so a replayed step does not re-trigger the LLM (D-03). Browser is a thin step driver. |
| Identity provisioning (identity + grants + Authula user + Telegram link) | **API/Backend** (saga orchestrator) | DB + Authula pool + Telegram token | Spans 3 stores; atomicity + no-escalation + audit are server-enforced. The capability checklist is server-validated (D-06), never client-trusted. |
| Auth boundary (RequireAuth + RequireCapability) | **Frontend Server (parent mux)** | API handlers | Inherited whole-origin gate from `serve_webui.go`; the create mutation adds `RequireCapability` (parity with `POST /agent/run`). |
| Boards/wizard render, empty/loading/error, responsive | **Browser/Client** (React lazy chunks) | — | Pure presentation over JSON; desktop+mobile both first-class per the deep-research mandate + 28-UI-SPEC. |

---

## Industrial Pattern Survey (deep-research mandate — D:/tmp + online)

> Mandate (28-CONTEXT.md `<specifics>`, 2026-06-19): "deep research online and on D:/tmp — industrial patterns, desktop AND mobile, industrial-grade design." Take **layout/interaction ideas only** — NO dockable-window / icon-rail / command-palette machinery (the deferred `ui_control` shell). Findings below are concrete (repo/file → pattern → why it fits Aura's blue cockpit), per [[feedback_check_tmp_sources_then_brainstorm_best]].

### D:/tmp findings (curated codebases — read first)

| Source (file) | Pattern observed | Why it fits Aura's governance boards / wizard | Confidence |
|---------------|------------------|------------------------------------------------|------------|
| **`D:/tmp/elysia-frontend/app/components/explorer/DataExplorer.tsx`** | Master-list + detail composition: `DataTable` (the list) swaps to `DataMetadata` (the inspector) on row-select; pagination via `page`/`pageSize`/`setPage` state + `sortOn`/`ascending`; `loadingData` skeleton; a `query`/`usingQuery` search bar. `framer-motion` for pane transitions. | This IS the D-02 master-list+detail board shape. Each board (MCP/Skills/Scheduler) = a `DataTable`-style list → detail pane on select. Aura uses `motion` (token durations) for the same transition, `Skeleton` for loading. Direct 1:1 mapping. | HIGH [VERIFIED: D:/tmp/elysia-frontend/app/components/explorer/DataExplorer.tsx] |
| **`.../explorer/DataTable.tsx`** | `selectedRow` state (number\|null); `stickyHeaders` (`sticky top-0 bg-background z-10`) for dense scroll tables; `hover:bg-foreground_alt` row hover; sortable column headers (`onClick` → toggle sort); the detail view replaces the table when a row is selected (`selectedRow === null ? <table> : <detail selectedCell={data[selectedRow]}>`). | Aura's dense operator tables (`--row-h: 34px`, operator density) want exactly this: sticky header, hover row, click→detail. The "table OR detail" swap is the **mobile bottom-sheet** story too (28-UI-SPEC: mobile = list with detail as bottom sheet). | HIGH [VERIFIED: D:/tmp/elysia-frontend/app/components/explorer/DataTable.tsx] |
| **`.../configuration/ConfigurationDashboard.tsx` + `ConfigSidebar.tsx`** | A settings dashboard with a left sidebar (categories) + a main panel of typed rows (`SettingInput`/`SettingDropdown`/`SettingCheckbox`/`SettingKey`); `WarningCard` for degraded states; `ModelBadge`/status chips. | The MCP board's redacted-env rows + trust/startup status chips map onto the `SettingKey` (a key shown without its secret value) + status-badge idiom. The Skills board's lifecycle tabs map onto the sidebar-category idea (but Aura uses a tab strip per D-01/28-UI-SPEC, not a sidebar). | HIGH [VERIFIED: D:/tmp/elysia-frontend/app/components/configuration/] |
| **`.../explorer/DataMetadata.tsx`** | Inspector pane = labelled sections with an explicit empty/edit state; reads from a typed payload; section-by-section render. | The board detail panes (MCP doctor result, skill detail, task run history) are read-only versions of this — labelled `<dl>/<dt>/<dd>` sections, mono for data-shaped values (28-UI-SPEC Typography). | HIGH [VERIFIED: D:/tmp/elysia-frontend/app/components/explorer/DataMetadata.tsx] |
| **`D:/tmp/aura-uiux/*.png`** (01-login … 09-mobile-inspector) | The SHIPPED Aura cockpit's own approved mockups: login (05 SPA pattern), graph empty/populated/inspector, **mobile** chat/graph/inspector. | These are the **locked visual reference** the new surfaces must match (blue palette, Fraunces/Atkinson/Commit Mono, mobile bottom-sheet inspector). The governance boards + wizard inherit this exact look — the mobile-inspector PNG (09) is the bottom-sheet pattern the boards reuse. | HIGH [VERIFIED: D:/tmp/aura-uiux/ listing] |
| **`D:/tmp/nanobot/webui/src/components`** | A minimal embedded operator webui (the skill self-extension reference). Small bespoke component set over a single design system, no heavyweight router. | Confirms the minimal-industrial shape (no shell machinery): bespoke components over tokens, lazy chunks. Aura already follows this. | MEDIUM [VERIFIED: D:/tmp/nanobot/webui/src/components listing] |
| **`D:/tmp/odysseus/static/`** | A static-asset operator UI (vanilla JS modules: calendar/editor/model/research). No SPA framework; server-rendered + progressive JS. | A counter-example: Aura is correctly a Vite+React SPA (richer interactivity for the live MCP probe + wizard). Odysseus confirms "operator console" can be dense + utilitarian; take the **density** cue, not the vanilla-JS architecture. | LOW [VERIFIED: D:/tmp/odysseus/static listing] |

**Key D:/tmp takeaway:** elysia's `DataExplorer`/`DataTable`/`DataMetadata` triplet is the single most directly-reusable IDEA set — it is literally master-list + paginated dense table + detail inspector with loading/empty states, which is exactly D-02. Aura already ships the React equivalent in `web/src/graph/` (`GraphExplorer` + `NodeInspector`), so the build is "apply the elysia interaction model through Aura's existing graph-workspace components," not a from-scratch design.

### Online findings (where curated sources fall short — desktop+mobile, industrial)

| Source | Pattern | Application | Confidence |
|--------|---------|-------------|------------|
| Admin Dashboard UI/UX best practices (Medium / ui-patterns.com dashboard pattern) [CITED: ui-patterns.com/patterns/dashboard] | Operational dashboards need **explicit "last updated" / freshness cues** + clear visual state for live-probed data; read-only via permission controls, not greyed-out write buttons. | The MCP board's live probe must show a per-row `Checking…` → `Healthy · N tools` / `Timed out` state with a `role="status"` live region (28-UI-SPEC already specifies this). Read-only is enforced by **rendering no write controls at all** (D-02), not disabled buttons. | MEDIUM [CITED: medium.com admin-dashboard-best-practices-2025] |
| Software wizard pattern (Wikipedia) + access-provisioning lifecycle (BigID) [CITED: en.wikipedia.org/wiki/Wizard_(software)] | A wizard breaks a complex task into a **linear sequence of small steps** ending in a **review+confirm**; provisioning best-practice = permission request → review by authority → credential delivery, with the grant explicitly reviewed before issuance. | The onboarding wizard's final screen is a **review summary** (email + chosen capabilities + Telegram-link state) → "Create identity" CTA (28-UI-SPEC: this is a CONSTRUCTIVE confirm, NOT danger-styled). The capability picker (D-06) IS the "permission request reviewed before issuance" step. | MEDIUM [CITED: bigid.com access-provisioning-lifecycle] |
| Responsive table → card pattern (general industrial practice) | Below the desktop grid breakpoint, a dense data table degrades to stacked cards or a list-with-bottom-sheet detail; 44px touch targets; never an icon-only control. | 28-UI-SPEC already locks this: `lg` (1024px) grid flip, single-column stack below, mobile bottom-sheet detail (Graph Explorer pattern), 44px min targets. The boards inherit the shipped `web/src/graph` mobile story. | HIGH (cross-checked against 28-UI-SPEC + the shipped graph mobile e2e) |

**Mobile is first-class (operator may run on a phone):** every board's master-list is the dominant mobile view; the detail/inspector is a backdrop-dismissable bottom sheet (`max-h-[80svh]`, the shipped Graph Explorer sheet pattern). The wizard is full-screen single-column on mobile with the linear stepper collapsed to a compact progress indicator. All held to `web/scripts/contrast-check.mjs` WCAG-AA (the locked gate) + the blue design system. [CITED: 28-UI-SPEC §Interaction & A11y Contract]

---

## Exact Reuse Map (file:line + signatures)

> Every entry below was read this session. The planner writes `<read_first>`/`<action>` blocks against these exact symbols.

### MCP board (GOV-01)

| Symbol | Location | Signature / shape | Use |
|--------|----------|-------------------|-----|
| `manager.SnapshotStatus` | `internal/mcp/manager/status.go:40` | `func SnapshotStatus(doc mcp.ManagedConfig) []StatusSnapshot` — sorted by name | The board's row source: `StatusSnapshot{Name, Profiles, Trust, Runtime, StartupState, AuthStatus, LastError}` (status.go:28–36). Deterministic order (R1 ordering edge). [VERIFIED: internal/mcp/manager/status.go:40] |
| `manager.RedactSecrets` | `internal/mcp/manager/status.go:72` → `mcp.RedactSecrets` `internal/mcp/redact.go:13` | `func RedactSecrets(s string) string` — masks `*TOKEN/SECRET/PASS/KEY/AUTH/BEARER*=…` + `Authorization: Bearer …` | Apply to every string before it reaches the wire (env values, LastError). [VERIFIED: internal/mcp/redact.go:6-15] |
| `secret.IsSecretEnvKey` | (referenced by CONTEXT) | — | Use to render env **KEYS** with a `redacted` chip for the value (28-UI-SPEC: `bg-surface-3 text-text-muted font-mono`). Confirm exact path during plan (`internal/secret` or `internal/mcp`). [ASSUMED — not read this session] |
| `mcpDoctorAll` (live probe) | `cmd/aura/mcp_status.go:53` | `func mcpDoctorAll(ctx, out io.Writer) error` — iterates `SnapshotStatus`, per-server `writeRuntimeCheck` (mcp_status.go:93: `exec.LookPath(server.Command)` or "http endpoint configured") + `writeRecipeChecks` (mcp_status.go:106) | **This is the CLI live-probe path to refactor into a reusable, structured, per-server probe function** the handler calls in parallel. Today it writes text to a Writer; the handler needs a `[]ProbeResult` return. **Wave-0: extract a `func probeServer(ctx, name, server) ProbeResult` that returns `{Name, OK, ToolCount, Detail, Err}` instead of writing text.** [VERIFIED: cmd/aura/mcp_status.go:53-123] |
| Tool-count source | — | The doctor today does NOT count mounted tools — it does a `LookPath`/endpoint reachability check. **A real mounted-tool-count requires dialing the server via `internal/mcp` client (`client.go`/`http_client.go`) + listing tools.** | **Landmine:** "mounted tool count" is a heavier live probe than the current doctor. The planner must decide: reuse `internal/mcp.Client.Open` + a tools/list call per server (bounded timeout), or scope GOV-01 tool-count to the run-time-launchable check. The SPEC acceptance says "live tool count + doctor OK," so a real list call is needed. [VERIFIED: cmd/aura/mcp_status.go has no tool count] |
| `LoadManagedConfig` / `loadManagedMCPConfig` | `cmd/aura/mcp_status.go:21` calls `loadManagedMCPConfig()` (loads `~/.aura/mcp/servers.json`) | `func loadManagedMCPConfig() (mcp.ManagedConfig, <path>, error)` | The config source for the board. The handler needs the same loader (lift to a shared seam or call `mcp.LoadManagedConfig` directly). [VERIFIED: cmd/aura/mcp_status.go:21] |

### Skills board (GOV-02)

| Symbol | Location | Signature | Use |
|--------|----------|-----------|-----|
| `skills.Loader.List` | `internal/skills/loader.go:91` | `func (l *Loader) List() []Skill` — name-sorted, TTL-cached (1s), lazy re-scan, goroutine-free | The **active** tab source. `Skill{Name, Description, Always, Type, Language, Body, Dir}` (loader.go:26-34). The loader scans `Roots` in order; **the active dir is one root**. [VERIFIED: internal/skills/loader.go:91-100] |
| pending / archived dirs | loader scans `Roots` (loader.go:132 `scan`) | — | **Landmine / Wave-0 gap:** the `Loader` merges all roots into ONE active snapshot — it has **no built-in "pending"/"archived" partition**. `pending/` + `archived/` are sibling dirs the lifecycle writer manages (`internal/skills/writer.go`/`writer_activate.go`), NOT loader roots. The board needs a **per-stage filesystem reader** (a thin `os.ReadDir` over `$AURA_SKILLS_DIR/pending` + `/archived`, parsing each `SKILL.md` via the existing `parseFrontmatter`). Confirm the exact stage-dir layout in `writer_activate.go` during plan. [VERIFIED: internal/skills/loader.go:132-174 — single merged snapshot] |
| pending non-runnable | loader.go:213-220 | load-time NFKC injection blocklist scan; pending skills are NOT in the active `Roots` so are never loaded into the manifest/LLM context by construction | **GOV-02 prohibition #1 holds by construction:** the loader only mounts active-root skills; pending bodies never enter context. The board reads pending metadata via a separate path that NEVER calls the mount/use path. Cite loader.go:207-220 (the body never crosses into context without the active-root + blocklist gate). [VERIFIED: internal/skills/loader.go:207-220] |
| `skills.AuditStore.List` | `internal/skills/audit_store.go:169` | `func (s *AuditStore) List(ctx, f AuditFilter) ([]AuditRow, error)` — **newest-first** (the SQL orders `created_at DESC`), `Limit<=0 → 100` | The **audit** tab source. **NOTE: the method is `List`, NOT `ListAudit`** (CONTEXT.md said `ListAudit` — correct it). `AuditFilter{SkillName, Since, Limit}` (audit_store.go:161). `AuditRow{ID, CreatedAt, ActorID, IdentityID, SkillName, Action, ContentHash, ApprovalSource, PausedStateToken, GateRecommended, GateTaken, BlocklistOverride}` (audit_store.go:81-94). Append-only is DB-enforced (no UPDATE/DELETE grant + triggers, audit_store.go:16-24). [VERIFIED: internal/skills/audit_store.go:169-195] |
| per-skill metadata (capability scope, last used, use count, risk tier, content hash) | split across sources | — | **Landmine / Wave-0:** these fields are NOT all on one struct. `content hash` = `skills.contenthash` (`internal/skills/contenthash.go`); `use count` / `last used` = `internal/skills/snippet_usage.go` (snippet reuse) — verify which applies to instruction vs snippet skills; `risk tier` = derived at install (audit `Action` + gate fields). **The board's per-skill row aggregates from ≥3 sources** — the planner must map each field to its source during plan. Content hash + audit are confirmed; usage + risk-tier mapping need a plan-time read of `snippet_usage.go`. [VERIFIED partial: contenthash.go + audit_store.go exist; usage aggregation ASSUMED] |

### Scheduler board (GOV-03)

| Symbol | Location | Signature | Use |
|--------|----------|-----------|-----|
| `cron.Store.ListActiveTasks` | `internal/cron/store.go:162` | `func (s *Store) ListActiveTasks(ctx) ([]Task, error)` — ordered by next fire | The task-list source. `Task{ID, Kind, ScheduleKind, CronExpr, EveryMinutes, RunAt, TZ, Payload, StepBudget, Status, NextRunAt, NotifyRoute, IdentityID, OriginConversationID, CreatedAt, UpdatedAt}` (store.go:57-74). [VERIFIED: internal/cron/store.go:162-172] |
| `cron.Store.GetTask` | `internal/cron/store.go:146` | `func (s *Store) GetTask(ctx, id string) (Task, error)` — `ErrTaskNotFound` on miss | Per-task detail header. [VERIFIED: internal/cron/store.go:146-159] |
| run-history query | **MISSING** | only `GetRun(id)` (store_runs.go:60), `ScanStaleRuns` (store_runs.go:142), `DueTasks` (store_runs.go:85) exist | **Wave-0 GAP (concrete):** there is **NO `ListRunsForTask(taskID, limit, offset)` query**. `agent_job_runs.sql` has InsertRun/GetRun/UpdateHeartbeat/CompleteRun/ScanStaleRuns/MarkUnknownRecovery only (agent_job_runs.sql:1-33). **The planner MUST add a new sqlc query** `ListRunsForTask :many` (`SELECT … FROM aura.agent_job_runs WHERE task_id = $1 ORDER BY started_at DESC LIMIT $2 OFFSET $3`) + a `Store.ListRunsForTask` wrapper. No migration needed (table + `last_heartbeat_at` column already exist). `Run{ID, TaskID, Status, StepBudget, StartedAt, LastHeartbeatAt, CompletedWithHash, Summary, LastError, MissedSince, PausedStateToken, CompletedAt}` (store.go:77-90) is the detail row. [VERIFIED: internal/db/queries/agent_job_runs.sql:1-33 + internal/cron/store_runs.go] |

### Onboarding wizard (ONBD-02)

| Symbol | Location | Signature / behavior | Use |
|--------|----------|----------------------|-----|
| `onboarding.Session` | `internal/onboarding/session.go:112` | fields `IdentityID, IdentityName, Step, Status, Answers, DraftAgentMD, Preferences` + **unexported** `pending []Input` + `prompted bool` | The server-held state machine. **MUST be held server-side across REST step calls** (D-03 / §Hard Problem 4) — it carries the LLM-extracted `Answers` + `DraftAgentMD`. [VERIFIED: internal/onboarding/session.go:112-124] |
| `Session.Apply` | `session.go:132` | `func (s *Session) Apply(in Input) (Transition, error)` — switches on `in.Intent` | The step driver. `Input{Intent, Text, Answers}` (session.go:97-102); `Transition{Content, StateDelta map[string]any, Terminal}` (session.go:104-109). [VERIFIED: internal/onboarding/session.go:132-158] |
| `Intent` consts | `session.go:15-28` | `IntentAnswer/Confirm/Edit/Skip/Cancel/Restart` | The REST step body's `intent` field. [VERIFIED: internal/onboarding/session.go:15-28] |
| `Step` / `Status` consts | `session.go:33-62` | `StepIdentity/Work/Projects/Social/Style/Draft`; `StatusActive/Draft/Completed/Skipped/Canceled` | The `{step, status}` in the step response. [VERIFIED: internal/onboarding/session.go:33-62] |
| `Session.prompted` | `session.go:124`, set in `nextTransition`/`applyQueued` (session.go:165-194) | the no-duplicate-prompt latch | **The no-duplicate-LLM-turn guarantee** (§Hard Problem 4): `edit` re-renders from the SAME `Answers` via `refreshDraft` (session.go:235-246) → no re-prompt. **Edit's LLM call:** `edit`→`refreshDraft`→`ExtractDraft` runs the render extraction, NOT the per-answer LLM extractor; the per-step `LLMAnswerExtractor.Extract` runs only on the inbound free-text answer (see below). [VERIFIED: internal/onboarding/session.go:124,165-194,235-246] |
| `onboarding.ExtractDraft` | called in `refreshDraft` (session.go:350) | `func ExtractDraft(a Answers) (Draft, error)` (in `extractor.go`) — renders the 8-section Agent.md from accumulated Answers | The draft render. NOT an LLM round-trip per se — it's the deterministic profile render over Answers. [VERIFIED: internal/onboarding/session.go:349-357] |
| `LLMAnswerExtractor.Extract` | `internal/onboarding/extractor_llm.go:49` | `func (e *LLMAnswerExtractor) Extract(ctx, step Step, raw string) (Answers, error)` — one-shot, tool-free, `Temperature:0`, `MaxTokens:256`, raw-text fallback on any failure (never errors) | **The single LLM extraction per free-text answer.** The REST step handler calls this ONCE when an `answer` carries free text, merges the result into the session, then `Apply`. Replay (same step re-submitted) does not re-run it because the session has already advanced + `prompted` gates the prompt. [VERIFIED: internal/onboarding/extractor_llm.go:49-85] |
| `profile.render` (8-section Agent.md) | `internal/profile/render.go` | renders Agent.md; stored at `~/.aura/agents/<id>/` | On `confirm`, the draft (`s.DraftAgentMD`) is persisted via the profile store. The wizard's "Completing writes an Agent.md matching the LoopAgent's 8-section output" (AC) is satisfied because the SAME `ExtractDraft`/`render` path is used. [VERIFIED: internal/profile/render.go exists; render_test.go covers 8 sections] |

### Identity provisioning (ONBD-01a)

| Symbol | Location | Signature | Use |
|--------|----------|-----------|-----|
| `identity.Store` | `internal/identity/store.go:48` | `Store{pool, q}` over sqlc; `New(pool)` | Writes `aura.identities` + `aura.capability_grants`. [VERIFIED: internal/identity/store.go:48-57] |
| `Store.GrantCapability` | `store.go:145` | `func (s *Store) GrantCapability(ctx, identityID, capability string) error` — rejects `*` (`ErrWildcardManaged`), validates name grammar (`ErrInvalidCapability`), idempotent (ON CONFLICT) | **The no-escalation enforcement point #1:** granting `*` is rejected BEFORE any DB call (store.go:178-180). The saga grants each ticked capability via this. [VERIFIED: internal/identity/store.go:145-157,177-185] |
| `Store.HasCapability` | `store.go:128` | `func (s *Store) HasCapability(ctx, identityID, capability string) (bool, error)` — wildcard-or-exact in SQL | Gate the create mutation (the creator must hold the identity-create capability) + the D-06 subset check (each ticked cap ⊆ creator's grants). [VERIFIED: internal/identity/store.go:128-138] |
| `ListCapabilities` (sqlc) | `internal/db/queries/capability_grants.sql:11` | `SELECT identity_id, capability, granted_at … WHERE identity_id = $1 ORDER BY capability ASC` | **The D-06 capability-picker source** (the creator's own grants). **Wave-0: there is no `Store.ListCapabilities` wrapper — only the raw sqlc query exists.** The planner adds a thin `func (s *Store) ListCapabilities(ctx, identityID) ([]string, error)` wrapper; the handler filters out `*` for the picker. [VERIFIED: internal/db/queries/capability_grants.sql:11-15 + no wrapper in store.go] |
| `Store.DeleteIdentity` | `store.go:118` | `func (s *Store) DeleteIdentity(ctx, name string) error` — grants cascade (FK ON DELETE CASCADE) | **Saga compensation leg for the aura rows** (deletes identity + cascades grants + identity_auth_links via the migration-0019 FK ON DELETE CASCADE). [VERIFIED: internal/identity/store.go:115-123 + 0019 FK at migration 0019:38] |
| `webauth.IdentityLinker.LinkOperator` | `internal/webauth/identity_link.go:60` | `func (l *IdentityLinker) LinkOperator(ctx, identityID, authulaUserID string) error` — upsert on UNIQUE `authula_user_id` (1:N-ready) | Binds the new Authula user → new identity in `aura.identity_auth_links`. [VERIFIED: internal/webauth/identity_link.go:60-72] |
| `webauth.IdentityLinker.ResolveIdentityID` | `identity_link.go:41` | `func (l *IdentityLinker) ResolveIdentityID(ctx, authulaUserID string) (string, error)` — `ErrLinkNotFound` on miss | The session-validate seam already resolves identity from the link table — **so once the 2nd link row exists, the 2nd user logs in and resolves correctly with NO code change** (this is why D-07 only needs to relax the `OperatorUserID` *pin*, not rewrite resolution). [VERIFIED: internal/webauth/identity_link.go:41-54] |
| immutable audit row | **NEW** | — | **Wave-0: create `aura.identity_audit` (migration 0021), append-only, modeled on `skill_audit`** (no UPDATE/DELETE grant + a trigger). Reuse the `skill_audit` immutability pattern (audit_store.go:16-24). One row per successful create, written **inside the same aura-leg pgx tx** as the identity/grants (so it commits/rolls-back atomically with them). Columns: `id, created_at, actor_identity_id, new_identity_id, new_identity_name, granted_capabilities text[], authula_user_id`. [ASSUMED design — no `identity_audit` exists today; skill_audit is the verified template] |

### Authula server-side user-create + delete (ONBD-01a / D-05 / saga)

> **Verified against the vendored module `github.com/Authula/authula@v1.11.0`.**

| Symbol | Location (module) | Signature | Use |
|--------|-------------------|-----------|-----|
| `Provider.CoreServices()` | `internal/webauth/authula.go:201` | `func (p *Provider) CoreServices() *authulaservices.CoreServices` | The gateway to the programmatic create path (already exposed). [VERIFIED: internal/webauth/authula.go:201-203] |
| `CoreServices` fields | `services/core.go:64` | `UserService, AccountService, SessionService, VerificationService, TokenService, PasswordService` | **All the services the saga needs are exposed.** [VERIFIED: github.com/Authula/authula@v1.11.0/services/core.go:64-71] |
| **The create sequence** (bypasses `DisableSignUp`) | `plugins/email-password/usecases/sign_up_usecase.go:65-77` | The use-case does: `hash := PasswordService.Hash(password)` → `user := UserService.Create(ctx, name, email, true, nil, nil)` → `AccountService.Create(ctx, user.ID, user.Email, models.AuthProviderEmail.String(), &hash)` | **D-05 create recipe (verified):** the saga replicates these 3 calls directly against `CoreServices()`, **skipping the use-case** (which would 400 on `DisableSignUp`, sign_up_usecase.go:51). `models.AuthProviderEmail.String() == "email"` (models/providers.go:6). [VERIFIED: github.com/Authula/authula@v1.11.0/plugins/email-password/usecases/sign_up_usecase.go:64-77 + models/providers.go:6] |
| `UserService.Create` | `services/core.go:12` | `Create(ctx, name, email string, emailVerified bool, image *string, metadata map[string]any) (*models.User, error)` | Step 2 of the create. The **Aura webauth integration test already proves this exact call** (`authula_integration_test.go:76`: `core.UserService.Create(ctx, "operator", email, true, nil, nil)`). [VERIFIED: services/core.go:12 + internal/webauth/authula_integration_test.go:76] |
| `AccountService.Create` | `services/core.go:21` | `Create(ctx, userID, accountID, providerID string, password *string) (*models.Account, error)` | Step 3 — attaches the hashed password as the `email` provider account. [VERIFIED: services/core.go:21] |
| `PasswordService.Hash` | `services/core.go:60` | `Hash(password string) (string, error)` | Step 1 — hash before storing. The raw password is never persisted. [VERIFIED: services/core.go:59-60] |
| `UserService.Delete` | `services/core.go:17` | `Delete(ctx, id string) error` | **Saga compensation leg for the Authula user** (the `AccountService` row cascades on user delete per Authula's FK). The Aura test already uses it in cleanup (`authula_integration_test.go:80`: `core.UserService.Delete(...)`). [VERIFIED: services/core.go:17 + authula_integration_test.go:80] |
| `Provider.OperatorUserID` | `internal/webauth/authula.go:210` | returns the lone user id; **errors when >1 user exists** (authula.go:228) | **D-07 amendment target #2:** this guard must be relaxed — see §Hard Problem 2. The enrollment pin uses this; with N users, resolution moves entirely to `ResolveIdentityID`. [VERIFIED: internal/webauth/authula.go:210-230] |

### Telegram link (ONBD-01b / D-08)

| Symbol | Location | Signature | Use |
|--------|----------|-----------|-----|
| `telegram.Store.InsertPending` | `internal/channels/telegram/store.go:84` | `func (s *Store) InsertPending(ctx, p InsertPendingParams) error` — `InsertPendingParams{OnboardingToken, IdentityID, GeneratedBy, ExpiresAt}` | **Mint a token for the NEW identity:** the wizard calls this with `IdentityID = <new identity UUID>`, `ExpiresAt = now+1h`. **This is the key reuse** — the token FKs the identity it was minted for, so consuming it links the NEW identity, not `local`. [VERIFIED: internal/channels/telegram/store.go:83-99] |
| `telegram.Store.ConsumeOnboarding` | `store.go:109` | `func (s *Store) ConsumeOnboarding(ctx, p ConsumeParams) (Account, error)` — one `db.WithTx` (mark consumed + INSERT account); `ErrTokenConsumed`/`ErrAccountExists` | The consume happens on the **Telegram side** (user taps the deep-link → `/start <token>` → `onboarding.handleStart` → `ConsumeOnboarding`). The web wizard does NOT call consume — it mints + renders the link + polls for completion. The identity in the consumed account = `pending.IdentityID` (store.go:124), i.e. the new identity. [VERIFIED: internal/channels/telegram/store.go:101-141] |
| deep-link builder | `internal/setup/handlers.go:183` | `func deepLink(botUsername, onboardingToken string) string` → `https://t.me/%s?start=%s` | Reuse the exact format. [VERIFIED: internal/setup/handlers.go:183-185] |
| completion poll | `internal/setup/handlers.go:165` (`pollCompletion` → `store.PendingConsumed`) | `PendingConsumed(ctx, token) (bool, error)` (store.go:155) | The wizard polls this (REST poll, NOT SSE per D-03) to flip "Telegram linked" once the user scans. [VERIFIED: internal/channels/telegram/store.go:155-164] |
| QR render | `internal/setup/qr.go:10` (`qrSVG` is a **STUB returning ""**) + `qrterminal` (ASCII, handlers.go:86) | — | **Landmine / Wave-0:** the web wizard needs a **scannable QR image/SVG**; `qrSVG()` is a deferred stub. **`rsc.io/qr v0.2.0` is ALREADY vendored** (indirect via `qrterminal`) — it produces a QR matrix the backend can render to SVG/PNG with ~30 LOC, OR the frontend renders the deep-link string with a small client QR lib. **Recommendation: render server-side to SVG via the already-present `rsc.io/qr`** (no new dependency, no client lib, no DOM injection of the token beyond the deep-link URL). The bot token is NEVER in the QR — only the `t.me/<bot>?start=<onboarding-token>` URL. [VERIFIED: internal/setup/qr.go:10-13 + go.mod has `rsc.io/qr v0.2.0`] |

### REST handler + route pattern (the model to mirror)

| Symbol | Location | Use |
|--------|----------|-----|
| `writeJSON` / `writeJSONStatus` | `internal/agui/conversations_api.go:64,68` | `writeJSON(w, v)` (200) / `writeJSONStatus(w, status, v)`. Reuse verbatim. [VERIFIED: internal/agui/conversations_api.go:64-74] |
| `sanitizeErr` / `SanitizeString` | `internal/agui/server_redact.go:41` | `sanitizeErr(err) string` — wraps `SanitizeString` (strips DSNs/userinfo). **Every wire error MUST pass through it** (HARDEN-08). [VERIFIED: internal/agui/server_redact.go:41-46] |
| thin-handler shape | `internal/agui/graph_api.go:75-116` | parse → uuid-guard {id} → validate (size-cap `MaxBytesReader`, enum, length) → ONE store call → `writeJSON`. NO business logic in the handler. [VERIFIED: internal/agui/graph_api.go:66-116] |
| `Server.Mux()` + `registerXxxRoutes` | `internal/agui/server.go:133` | Add `s.registerGovernanceRoutes(mux)` + `s.registerOnboardingRoutes(mux)` to `Mux()` (server.go:148-158 shows the existing register calls). [VERIFIED: internal/agui/server.go:133-160] |
| `SetXxx` injection | `internal/agui/server.go:111-127` | `SetApprovalStore`/`SetImageProxy`/`SetGraphView` are the pattern. **Add `SetGovernanceProviders(...)` + `SetOnboardingService(...)`** — keep them OFF the `NewServer` constructor (D-A2-02 narrow seams). 503 until wired. [VERIFIED: internal/agui/server.go:104-127] |
| parent-mux mount | `cmd/aura/serve_webui.go:233-267` | Read-GET routes (boards + onboarding step reads) mount behind the whole-mux `RequireAuth` (serve_webui.go:289). The **create mutation** mounts with `agui.RequireCapability(aguiHandler, auth, <cap>)` exactly like `POST /agent/run` (serve_webui.go:213). Add the route consts beside `graphSchemaRoute` (serve_webui.go:147-150). [VERIFIED: cmd/aura/serve_webui.go:213,233-267,289] |
| `RequireCapability` | `internal/agui/auth.go:259` | `func RequireCapability(next http.Handler, deps AuthDeps, capability string) http.Handler` — reads `principalFrom(ctx)`, `deps.Identities.HasCapability`, 403 on miss/err | The create-mutation gate. [VERIFIED: internal/agui/auth.go:259-273] |
| composition root wiring | `cmd/aura/serve.go:252-312` | Build the governance providers + onboarding service after `agui.NewServer` (serve.go:252), `SetXxx` them (mirror serve.go:285-311). `chat.pool`/`chat` exposes the pgxpool + stores. The Authula provider is `authulaProvider` (serve.go:325 `buildAuthDeps`). [VERIFIED: cmd/aura/serve.go:252-330] |
| read-GET-behind-RequireAuth precedent | `internal/agui/image_proxy.go` + serve_webui.go:260 | The exact "read GET, no capability gate, inherits RequireAuth" precedent the board reads follow. [VERIFIED: cmd/aura/serve_webui.go:256-260] |

### Frontend mount points

| Symbol | Location | Use |
|--------|----------|-----|
| `MODES` / `LIVE_MODES` | `web/src/shell/modes.ts:1,6` | Add `'governance'` to BOTH. Today: `MODES = ['chat','tree','graph','displays','settings']`, `LIVE_MODES = ['chat','graph']`. [VERIFIED: web/src/shell/modes.ts:1-6] |
| center swap | `web/src/AppShell.tsx` | Mirror the `surface === 'graph'` swap → lazy `<GovernanceWorkspace>`. The wizard is a SEPARATE full-screen route/overlay (D-04), not a tab. [VERIFIED: web/src/AppShell.tsx exists; modes.ts comment confirms the graph swap] |
| mirror template | `web/src/graph/` | `GraphExplorer.tsx` (workspace) + `NodeInspector.tsx` (detail) + `graphApi.ts` (`getJSON`/`postJSON`, `credentials:'same-origin'`, non-200 THROWS so TanStack routes auth/error). **Copy this structure for `web/src/governance/` + `web/src/onboarding/`.** [VERIFIED: web/src/graph/ listing] |
| i18n split bundle | `web/src/i18n/resources.graph.ts` + `resources.display.ts` | The precedent for `resources.governance.ts` + `resources.onboarding.ts` (keeps `resources.ts` < 600 LOC; spread into each language's `translation`). [VERIFIED: web/src/i18n/ listing] |
| component inventory | 28-UI-SPEC §Component Inventory | The exact file list + which shipped component each mirrors (already specified). [CITED: 28-UI-SPEC] |

---

## Hard-Problem Resolutions

### Hard Problem 1 — Cross-store atomic provisioning saga (D-07b)

**The problem:** provisioning writes span three independent stores that cannot share a transaction:
- **Leg A** — `aura.*` (one pgx tx via `db.WithTx`): `identities` row + N `capability_grants` + `identity_auth_links` + the immutable `identity_audit` row.
- **Leg B** — the **Authula `authula` schema on its OWN `database/sql` pool** (verified: `webauth.New` builds Authula's own pool, authula.go:108-113): `UserService.Create` + `AccountService.Create` (password). **Not in Leg A's tx.**
- **Leg C** — the Telegram-link token (`telegram.Store.InsertPending` — its OWN write; consume happens later, async, on the user's device).

**Resolution — ordered saga with per-leg compensation (all-or-nothing on confirm):**

```
PROVISION(email, password, capabilities[], creatorIdentityID):
  # ---- 0. PRE-VALIDATE (no writes; fail fast) ----
  assert creator HasCapability(identity-create-cap)            # 403 else (RequireCapability already did this)
  assert capabilities does NOT contain '*'                      # 400 (no-escalation #1)
  for cap in capabilities: assert creator HasCapability(cap)    # 400 (no-escalation #2: subset ⊆ creator)
  assert email non-empty AND Authula GetByEmail(email)==none    # 400 duplicate/empty (R5 empty edge)

  # ---- 1. LEG B FIRST (Authula user) — the leg most likely to fail on dup email ----
  hash   := PasswordService.Hash(password)
  user   := UserService.Create(ctx, name=email, email, emailVerified=true, nil, nil)   # B1
  acct   := AccountService.Create(ctx, user.ID, user.Email, "email", &hash)            # B2
  # compensation handle: COMP_B := func(){ UserService.Delete(ctx, user.ID) }  # acct cascades

  # ---- 2. LEG A (aura.* — ONE pgx tx, internally atomic) ----
  err := db.WithTx(pool, func(q){
            id := INSERT identities (name) RETURNING id
            for cap in capabilities: GrantCapability(q, id, cap)     # rejects '*' again (belt #3)
            LinkOperator(q, id, user.ID)                              # identity_auth_links
            INSERT identity_audit (actor=creator, new=id, caps, authula_user_id=user.ID)
         })
  if err != nil { COMP_B(); return rolled-back-error }   # A failed → undo Authula user

  # ---- 3. LEG C (Telegram token mint) ----
  token := uuid()
  err := telegram.InsertPending(ctx, {OnboardingToken:token, IdentityID:id, ExpiresAt:now+1h})
  if err != nil { DeleteIdentity(name) ; COMP_B() ; return rolled-back-error }  # C failed → undo A + B

  # ---- 4. SUCCESS ----
  return { identityID:id, deepLink: t.me/<bot>?start=<token>, qrSVG: render(deepLink) }
  # The Telegram *consume* is async (user scans later). It is NOT part of the atomic create —
  # an un-scanned token simply expires (1h TTL). "Identity created" = legs A+B+C committed;
  # "Telegram linked" is a later, idempotent, self-cleaning step (matches D-08 + the SPEC AC:
  # "consuming the token once links the channel").
```

**Why this ordering:** Leg B (Authula) is placed first because the **most common failure is a duplicate email**, and failing before any `aura.*` write means zero compensation. Leg A is internally atomic (one `db.WithTx`), so its only failure mode triggers a single compensation (delete the Authula user). Leg C is last and cheapest; its failure compensates A then B. **No leg is left half-done.**

**Compensation correctness:**
- `UserService.Delete(user.ID)` removes the Authula user; the `AccountService` row cascades (Authula FK). [VERIFIED via the integration-test cleanup at authula_integration_test.go:80 — the same Delete is exercised]
- `DeleteIdentity(name)` removes the identity; `capability_grants` + `identity_auth_links` cascade via the FK `ON DELETE CASCADE` (migration 0019:38, capability_grants FK from 0004). The `identity_audit` row — **if it were written for a later-failed flow** — is NOT a concern because the audit row is written **inside Leg A's tx**, so it only exists if Leg A committed; a Leg-C failure that deletes the identity must therefore also be allowed to delete its audit row, OR (cleaner) **the audit row is written only on full-saga success in a tiny final tx**. **Recommendation: write the audit row in a final `db.WithTx` AFTER Leg C succeeds**, so "exactly one immutable audit row per successful create" holds and a rolled-back flow has no audit row. (This is a deliberate refinement of the pseudo-code above — the planner picks: in-Leg-A-and-cascade-delete, or post-Leg-C-final-tx. The post-Leg-C option is simpler to reason about for the "exactly one row, only on success" AC.)

**Failure-injection points the planner MUST test (each → no orphan, ever):**
1. **B1 fails** (UserService.Create errors / dup email pre-check passes but create races) → no aura rows, no token. Assert: 0 identities, 0 Authula users with that email.
2. **B2 fails** (AccountService.Create errors after user created) → COMP_B deletes the user. Assert: 0 Authula users, 0 aura rows.
3. **A fails** (any grant/link/insert errors, or `WithTx` rollback) → COMP_B. Assert: 0 identities, 0 grants, 0 links, 0 Authula users.
4. **C fails** (InsertPending errors) → DeleteIdentity + COMP_B. Assert: 0 of everything.
5. **Abandoned flow** (operator closes the wizard before confirm) → §Hard Problem 4: the server-held session has a TTL; on expiry/abandon NOTHING was written (the saga only runs on the final confirm POST). Assert: a started-but-unconfirmed wizard leaves 0 rows.
6. **Double-submit confirm** (idempotency, R5) → the second create must not produce a 2nd identity. **Guard: the identity name (email) is UNIQUE in `aura.identities`** (verify the unique constraint exists; `GetIdentityByName` + the pre-check + the DB UNIQUE make the 2nd create a clean 409). [VERIFIED: identity.Store has GetIdentityByName/unique-name semantics, store.go:87]

**Concurrency (R5):** parallel creates of DIFFERENT emails are independent (different rows). Parallel creates of the SAME email → one wins, the other gets `ErrEmailAlreadyExists` (Authula) or the `aura.identities` UNIQUE (23505 → clean error). No corruption.

---

### Hard Problem 2 — D-07 PRD-amendment (BLOCKING)

**The collision (3 concrete pins to relax):**
1. **PROJECT.md** `.planning/PROJECT.md:90` — *"Multi-user con auth/RBAC reale … v1.0.0 aggiunge una boundary web minima … ma niente login multi-tenant, RBAC, o OAuth reale in questa milestone."* This explicitly forbids a 2nd login. [VERIFIED: .planning/PROJECT.md:90]
2. **`webauth.OperatorUserID`** `internal/webauth/authula.go:210-230` — **errors when >1 Authula user exists** (authula.go:228: `"%d users present — operator pin is ambiguous (multi-user is post-v1.0.0)"`). A 2nd user makes this return an error. [VERIFIED: internal/webauth/authula.go:210-230]
3. **The enrollment pin** — the `local` identity is pinned to the lone Authula user via `LinkOperator` at enrollment (identity_link.go:6 comment: *"the Authula user-id is pinned to the seeded `local` identity"*). [VERIFIED: internal/webauth/identity_link.go:5-11]

**EXACT amendment targets (the planner edits these — via gsd tooling for ROADMAP, direct edit for prd.md/PROJECT.md prose):**

| File | Location | Change |
|------|----------|--------|
| `.planning/PROJECT.md` | §Out of Scope, the *"Multi-user con auth/RBAC reale"* bullet (line 90) | Relax: *"v1.0.0 Phase 28 introduces a 2nd web-loginable identity via the cockpit onboarding wizard. Authz STAYS `capability_grants`-based — NO RBAC, NO route-scoping beyond `RequireCapability`, NO OAuth. Identity resolution generalizes 1:N via `aura.identity_auth_links` (already 1:N-ready, migration 0019)."* |
| `.planning/PROJECT.md` | §Requirements → Active, ONBD row | Note ONBD-01 now includes full provisioning of a loginable identity. |
| `prd.md` | the single-operator OQ notes (prd.md references OQ-4/OQ-8; grep `single-user`/`multi-user` — the §Slice 1.7 + identity section, e.g. prd.md:806,875,877) | Add a PRD-amendment block: *"Amendment #NN (Phase 28): the single-operator boundary is relaxed for web-loginable identities created via the onboarding wizard. capability_grants remains the only authz model. `OperatorUserID`'s >1-user error is replaced by 1:N resolution through `identity_auth_links`."* |
| `internal/webauth/authula.go` | `OperatorUserID` (authula.go:210-230) | **Behavioral change (lands with provisioning, not just docs):** the >1-user case must NOT error for the live login path. Options: (a) `OperatorUserID` stays an enrollment-only helper (used only to pin the FIRST user) and the login path uses `ResolveIdentityID` exclusively (it already does — identity_link.go:41); (b) relax the error to a warn + return the seeded-operator id. **Recommendation (a):** confirm the live session-validate path already resolves via `ResolveIdentityID` (it does, per the seam) so the `OperatorUserID` ambiguity error is only hit at enrollment — then the relaxation is "do not block a 2nd user at enrollment time," a small guarded change. The planner verifies no live request path calls `OperatorUserID`. |
| `docs/cockpit-overhaul/05-authula-auth-SPEC.md` | OQ-4 / OQ-8 single-operator notes | Mark OQ-8 (multi-user) partially resolved for the capability_grants-only path. |

**ROADMAP / Phase-30 absorption (D-09) — bundle into the SAME amendment commit, via gsd tooling (anti-pattern #15: NEVER a direct ROADMAP Write):**
- `.planning/ROADMAP.md:56` — the Phase-30 bullet → mark `✅ absorbed-into-28` with a one-line pointer to `28-SPEC §ONBD-01b`. [VERIFIED: .planning/ROADMAP.md:56]
- `.planning/ROADMAP.md:254` — the `### Phase 30:` section → tombstone pointer. [VERIFIED: .planning/ROADMAP.md:254]
- `.planning/phases/30-*/30-SPEC.md` → convert to a tombstone referencing `28-SPEC §ONBD-01b` (preserve traceability). [VERIFIED: 30-SPEC.md exists]

**Can `/gsd-phase` perform the ROADMAP edit?** The CONTEXT (D-09) + the anti-pattern #15 reference both say the ROADMAP edit must go through **gsd tooling, not a direct Write**. The planner uses the gsd roadmap-edit path (the same mechanism `/gsd-transition` / roadmap-status tooling uses) to flip the Phase-30 entry and add the absorption pointer; the prose edits to `prd.md`/`PROJECT.md` are ordinary file edits committed in the same PRD-amendment commit. **The amendment commit lands BEFORE the provisioning implementation wave** (CLAUDE.md PRD-first; the planner sequences it as the first task of the onboarding wave, or a pre-wave). [CITED: 28-CONTEXT.md D-07/D-09 + CLAUDE.md PRD-first principle]

---

### Hard Problem 3 — Live MCP probe concurrency (GOV-01 / Claude's Discretion)

**Resolution — bounded, parallel, per-row-isolated probes:**

- **Model:** the board-load handler (`GET /api/governance/mcp`) reads `SnapshotStatus(doc)` (cheap, deterministic, always succeeds → the board ALWAYS renders all rows) and returns the **static** rows immediately. The **live** doctor + tool-count is a **separate per-server probe**: either (a) a second endpoint `GET /api/governance/mcp/{name}/probe` the frontend fires per row, or (b) the list endpoint fans out probes server-side with a hard deadline and attaches per-row results. **Recommendation: (a) per-row probe endpoint** — it makes per-row isolation trivial (each row's probe is its own request; a hung server's request times out without touching siblings), matches the 28-UI-SPEC per-row `Checking…`→`Healthy`/`Timed out` live-region UX, and avoids one slow server inflating the whole list latency. The list endpoint returns static rows fast; the frontend lazily probes each visible row.
- **Per-server timeout:** wrap each probe in `context.WithTimeout(r.Context(), 3*time.Second)`. **Suggested value: 3s** — long enough for a healthy stdio spawn + tools/list, short enough that a hung server's row resolves to `Timed out` within the UX patience window. (The existing MCP client already has timeout semantics — HARDEN-03 added "sane =0/-1 timeout"; reuse the client's dial timeout, don't invent a new transport.) [CITED: HARDEN-03 / REQUIREMENTS.md:17]
- **Isolation:** because each row probes independently (option a), a dead/hung server fails ONLY its row (`Timed out`/`Error`, 28-UI-SPEC warning/danger). Sibling rows are unaffected (separate requests, separate contexts). The static list already rendered, so the board NEVER blanks (R1 boundary edge: "just over timeout → that row shows timed-out, board still renders").
- **If option (b) is chosen instead** (single fan-out endpoint), use `errgroup` with a bounded concurrency (e.g. `SetLimit(4)` per [[feedback_minipc_cpu_budget]] — never saturate the 16-core shared mini-PC) + a per-goroutine `context.WithTimeout`; a probe error/timeout sets that row's `{ok:false, error:"timed out"}` and NEVER propagates (collect, don't fail-fast). The handler returns 200 with mixed-state rows.
- **Cache:** **a short schema/status cache is justified ONLY for the tool-count list call** (dialing every server on every board open is wasteful + adds mini-PC load). **Recommendation: a 5–10s in-memory per-server probe cache** (mutex-guarded snapshot, mirroring `skills.Loader`'s 1s TTL goroutine-free cache, loader.go:17/113) so rapid re-renders/tab-switches don't re-spawn. The static `SnapshotStatus` needs no cache (it's a pure config read). [VERIFIED pattern: internal/skills/loader.go:17,113-127]
- **Prohibition #5 (configured-servers-only):** the probe iterates ONLY `doc.MCPServers` from `SnapshotStatus` — no operator-supplied/arbitrary outbound target. The handler accepts a `{name}` path param and looks it up in the loaded config (404 if absent); it NEVER takes a URL/command from the request body. [VERIFIED: the probe input is the config doc, cmd/aura/mcp_status.go:58]

---

### Hard Problem 4 — Onboarding-over-REST without duplicate LLM turns (ONBD-02 / D-03)

**The per-step JSON contract (confirmed):**

```
POST /api/onboarding/{sessionToken}/step
  body:  { intent: "answer"|"confirm"|"edit"|"skip", text?: string, answers?: {...} }
  resp:  { content: string, step: string, status: string,
           draft?: string, preferences?: {...} }
```

**How exactly-one-LLM-extraction-per-step holds:**
1. The `onboarding.Session` lives **server-side**, keyed by an opaque `sessionToken`, held in a TTL'd in-memory store (see below). It carries the accumulated `Answers` + `DraftAgentMD` + the unexported `prompted` latch (session.go:124).
2. On `intent:"answer"` **with free text**, the handler calls `LLMAnswerExtractor.Extract(ctx, session.Step, text)` **exactly once** (extractor_llm.go:49 — one-shot, never errors, raw-text fallback), merges the structured `Answers` into the session, then `session.Apply(Input{Intent:Answer, Answers:extracted})`. `Apply` advances the step (session.go:196-221).
3. **Replay** (the client re-POSTs the same step): the session has already advanced past that step, so a re-submit either advances to the NEXT step (new answer) or is a no-op on a terminal session (`ErrTerminal`, session.go:136). **The LLM is NOT re-invoked for the already-extracted step** because extraction is tied to the inbound free-text answer, not to rendering — and the step has moved on. The R4 adjacency edge ("submitting the same step twice does not advance twice or emit a duplicate LLM turn") is satisfied by the step pointer + `prompted` latch.
4. **`edit`** re-renders the draft from the **SAME** `Answers` via `refreshDraft`→`ExtractDraft` (session.go:235-246,349-357) — `ExtractDraft` is the **deterministic profile render**, NOT the per-answer LLM extractor. So `edit` emits **no new LLM extraction turn** (it only re-renders Agent.md from the facts already extracted). The AC ("edit re-renders the draft from the same extracted facts, no re-prompt") holds by construction. [VERIFIED: internal/onboarding/session.go:235-246,349-357 + extractor_llm.go:49]
5. **`skip`** sets `StatusSkipped` and writes NO profile (session.go:147-148). The R4 empty edge ("empty/skipped step recorded as empty/omitted without error") holds (`mergeAnswers` no-ops on empty, session.go:295-322).

**Session store + lifetime/TTL:** a server-side `map[sessionToken]*sessionEntry` guarded by a mutex, each entry `{session *onboarding.Session, identityID, expiresAt}` with a **15-minute idle TTL** (refreshed on each step). A lazy sweep (on access, no background goroutine — mirror the `skills.Loader` goroutine-free TTL discipline, loader.go:17) drops expired entries. **The session is created on wizard start** (the provisioning step in D-04 runs FIRST, so the session is keyed to the NEW identity being provisioned). **Abandonment path (ties to the saga, §Hard Problem 1.5):** the saga's final `confirm` is what runs the cross-store writes; an abandoned wizard (session expires) means the saga **never ran**, so there is nothing to roll back for the interview leg. **BUT** note the ordering tension: D-04 puts provisioning (the saga) BEFORE the interview, so the identity may already exist when the interview is abandoned. **Resolution: the interview's Agent.md write is the LAST step (`confirm`), and it is idempotent** — an abandoned interview after a successful provision leaves a loginable identity WITHOUT an Agent.md profile (acceptable — the profile is seeded on first real interaction, matching the existing onboarding LoopAgent behavior). The "no orphan identity" AC is about the **provision** atomicity (saga), not about the profile. The planner confirms this sequencing with the SPEC: SPEC AC for ONBD-01a is "no orphan identity/grant/Authula/Telegram row"; the Agent.md is ONBD-02 and is allowed to be absent if the interview is skipped/abandoned (AC: "skip ends without writing a profile"). [VERIFIED: internal/onboarding/session.go:147-148 + the SPEC AC separation]

> **Sequencing note for the planner:** there are two viable orders for the wizard — (1) provision-then-interview (D-04's literal order), or (2) interview-then-provision-on-final-confirm (saga runs once at the very end, so an abandoned wizard leaves ZERO rows). **Option (2) is strictly safer for the "no orphan, ever" AC** because the single atomic confirm runs all of provisioning. D-04 says "one flow" but does not forbid running the saga at the final confirm. **Recommend the planner run the full saga at the final `confirm`** (collect email/password/capabilities/Telegram-intent/interview-answers across the steps in the server-held session, then execute legs A+B+C atomically on confirm). This makes abandonment trivially orphan-free. Flag this to discuss-phase if D-04's literal ordering is load-bearing.

---

## REST Endpoint Shapes

> All under the `/api/` carve-out (exclusion-only in serve_webui.go:90; specific routes registered on `Server.Mux()`). All reads inherit `RequireAuth`; the create mutation adds `RequireCapability`. Errors via `sanitizeErr`. Empty datasets → safe empty state (200 + empty array). Backend unavailable → sanitized 502/503.

### Governance reads (GOV-01..03)

| Method + Path | Source | Response shape | Notes |
|---------------|--------|----------------|-------|
| `GET /api/governance/mcp` | `SnapshotStatus(doc)` | `{servers:[{name,trust,runtime,startupState,authStatus,envKeys:[{key,redacted:true}],lastError}]}` | Static, fast, always renders. Secrets redacted (values never sent). |
| `GET /api/governance/mcp/{name}/probe` | per-server live doctor + tools/list | `{name, ok, toolCount, detail, error?}` | Bounded 3s timeout, isolated, cached 5–10s. 404 if name not in config. |
| `GET /api/governance/skills?stage=active\|pending\|archived` | `Loader.List()` (active) + per-stage `os.ReadDir` (pending/archived) | `{skills:[{name,description,type,language,capabilityScope,lastUsed,useCount,ttlState,riskTier,contentHash}]}` | Pending rows carry NO action field. Per-stage reader is Wave-0. |
| `GET /api/governance/skills/audit?limit=&since=` | `AuditStore.List` | `{rows:[AuditRow…]}` newest-first | Default limit 100 (store default). |
| `GET /api/governance/scheduler` | `ListActiveTasks` | `{tasks:[{id,kind,scheduleKind,cronExpr,everyMinutes,nextRunAt,status,…}]}` | Ordered by next fire. |
| `GET /api/governance/scheduler/{id}/runs?limit=&offset=` | **NEW `ListRunsForTask`** | `{runs:[{id,status,startedAt,lastHeartbeatAt,summary,lastError,completedAt}], total?}` | **Default limit 25, offset 0.** Wave-0 sqlc query. Mutates nothing. |

### Onboarding step + provisioning (ONBD-01a/01b/02)

| Method + Path | Gate | Body / Response | Notes |
|---------------|------|-----------------|-------|
| `POST /api/onboarding/start` | `RequireCapability(identity-create-cap)` | resp `{sessionToken, step, content, capabilityOptions:[…creator grants minus '*']}` | Creates the server-held session; returns the D-06 picker options. The capability gate is here (you can't even start without the cap → matches "operator WITHOUT cap rejected, no row written"). |
| `POST /api/onboarding/{sessionToken}/step` | `RequireAuth` (session already authz'd at start) | body `{intent,text?,answers?}` → resp `{content,step,status,draft?,preferences?}` | The interview driver (§Hard Problem 4). One LLM extraction per free-text answer. |
| `POST /api/onboarding/{sessionToken}/provision` | `RequireCapability(identity-create-cap)` | body `{email,password,capabilities[],linkTelegram:bool}` → resp `{identityID, deepLink?, qrSvg?}` | **Runs the atomic saga (§Hard Problem 1).** Re-validates no-escalation server-side. Password write-only (never echoed). Returns the Telegram deep-link + server-rendered QR SVG. |
| `GET /api/onboarding/{sessionToken}/telegram-status` | `RequireAuth` | resp `{linked:bool}` | REST poll (NOT SSE per D-03) over `PendingConsumed`. Flips when the user scans. |

**Capability name for the gate:** introduce `identity.create` as the capability_grants name (parity with `agent.run` at serve_webui.go:99). The seeded `local` identity holds `*` so it passes; the name becomes load-bearing for created identities (which never get `*` nor `identity.create` unless the creator explicitly grants it AND holds it). [CITED: serve_webui.go:95-99 agentRunCapability pattern]

---

## Recommended Build Sequence

Per the SPEC's locked sequencing (interview round 2: "All four; boards first (low-risk) → onboarding (high-risk)"):

1. **Wave 0 — backend gaps + scaffolding** (no UI): add `ListRunsForTask` sqlc query + `Store.ListRunsForTask`; add `Store.ListCapabilities` wrapper; extract `probeServer(ctx,name,server) ProbeResult` from `mcpDoctorAll`; add the per-stage skills reader; add migration `0021_identity_audit` (append-only). Wire `SetGovernanceProviders`/`SetOnboardingService` seams on `*agui.Server`. **Test infra:** create `internal/agui/governance_api_test.go` + `onboarding_api_test.go` skeletons; `web/src/governance/__tests__/` + `web/src/onboarding/__tests__/`.
2. **Wave 1 — governance boards (GOV-01..03), low-risk reads.** Backend: `governance_api.go` (the 6 read routes). Frontend: `web/src/governance/` (workspace + 3 boards + detail panes + `governanceApi.ts` + `resources.governance.ts`); add `'governance'` to `modes.ts`; `AppShell` swap. The MCP live-probe concurrency (§Hard Problem 3) lands here.
3. **Wave 2 — PRD-amendment (BLOCKING, D-07/D-09).** The amendment commit (PROJECT.md + prd.md prose + ROADMAP via gsd tooling + 30-SPEC tombstone + the `OperatorUserID` relaxation). **Must precede Wave 3.**
4. **Wave 3 — onboarding wizard + provisioning saga (ONBD-01a/01b/02), high-risk.** Backend: `onboarding_api.go` (session store + step + provision saga + telegram-status) + the server-side QR render (`rsc.io/qr`). Frontend: `web/src/onboarding/` (full-screen wizard: credentials → capability picker → Telegram link+QR → 5-step interview → review+Create). The saga + compensation (§Hard Problem 1) + the session TTL (§Hard Problem 4) land here.
5. **Wave 4 — validation + polish.** Playwright e2e on all new surfaces (desktop+mobile); `contrast-check.mjs` on every new board + wizard screen; the log-scan no-leak test over a full provisioning run; Stryker on `web/src/governance/` + `web/src/onboarding/`.

---

## Risks / Landmines

| # | Risk | Mitigation |
|---|------|-----------|
| L1 | **"Mounted tool count" is heavier than the existing doctor** — `mcpDoctorAll` only does `LookPath`/endpoint-reachability, NOT a tools/list. A real count requires dialing each server via `internal/mcp.Client`. | The probe must `Client.Open` + tools/list per server under the 3s timeout (§Hard Problem 3). Scope: if a real list is too costly/flaky, the planner may render "reachable" + tool-count-when-available; the SPEC AC wants a live count for healthy servers, so prefer the real call with isolation. |
| L2 | **Skills loader has no pending/archived partition** — `List()` returns ONE merged active snapshot. | Add a per-stage `os.ReadDir` reader over `pending/`+`archived/` (parse via existing `parseFrontmatter`); confirm the stage-dir layout in `writer_activate.go` at plan time. |
| L3 | **NO run-history query exists** for GOV-03 pagination. | Wave-0: add `ListRunsForTask :many` sqlc + wrapper (no migration needed). |
| L4 | **`DisableSignUp` blocks the use-case `SignUp`, not just the route** (sign_up_usecase.go:51). | The saga uses the lower-level `CoreServices()` calls (`PasswordService.Hash` + `UserService.Create` + `AccountService.Create`) directly, bypassing the use-case — VERIFIED reachable + the Aura integration test already exercises `UserService.Create`/`Delete`. |
| L5 | **Authula's pool ≠ aura's pool** → no single-tx atomicity. | The cross-store saga with per-leg compensation (§Hard Problem 1). |
| L6 | **D-07 amendment is BLOCKING + the `OperatorUserID` >1-user error is a live landmine.** | Land the amendment commit before Wave 3; verify no live request path calls `OperatorUserID` (resolution is via `ResolveIdentityID`) so the relaxation is a small guarded enrollment-time change. |
| L7 | **QR is a deferred stub** (`qrSVG()` returns ""). | Render server-side via the already-vendored `rsc.io/qr` (no new dep); never put the bot token in the QR — only the `t.me/<bot>?start=<onboarding-token>` URL. |
| L8 | **Audit-row "exactly one, only on success"** vs in-tx-then-cascade. | Recommend writing the immutable `identity_audit` row in a tiny final `db.WithTx` AFTER Leg C succeeds (so a rolled-back flow has no audit row). |
| L9 | **Wizard sequencing** (provision-then-interview vs saga-on-final-confirm) affects orphan-freeness on abandonment. | Recommend running the full saga at the final `confirm` (§Hard Problem 4 note); flag to discuss-phase if D-04's literal order is load-bearing. |
| L10 | **`ListActiveTasks` shows only ACTIVE tasks** — cancelled/completed tasks won't appear. | Confirm the SPEC intent (the board says "scheduled tasks" — active is the right default; a "show all statuses" toggle is Phase-29/v2 scope). The run-history shows terminal runs regardless. |
| L11 | **Secrets-in-logs during provisioning** — the password + bot token must never hit logs. | The setup wizard's `handleToken` (handlers.go:44) is the exact precedent: NEVER `slog` the secret, never surface `err.Error()` verbatim (could echo the token). The provision handler logs a fixed message on failure; the password is hashed immediately + never logged. The no-leak log-scan test (Wave 4) asserts this over a full run. |

---

## Validation Architecture

> **MANDATORY** (nyquist_validation_enabled=true). VALIDATION.md is generated from this section. Every requirement maps to a testable seam; owned-surface coverage ≥85% across the `db_integration neo4j_integration` tag matrix; no-skip-as-green (tests `t.Fatal` under `$CI` when env unset). Frontend: Vitest ≥85% (statements/branches/functions/lines) + Stryker ≥70% killed on touched dirs; Playwright e2e + `contrast-check.mjs` WCAG-AA on every new surface (desktop+mobile).

### Test Framework

| Property | Value |
|----------|-------|
| Go framework | stdlib `testing` + table-driven; `goleak.VerifyNone` in `TestMain`; race detector; build tags `db_integration neo4j_integration`; mutation via `go-mutesting` (WSL) ≥70% on critical files [VERIFIED: CLAUDE.md gates + internal/cron/main_test.go pattern] |
| Go quick run | `go test ./internal/agui/ ./internal/cron/ ./internal/onboarding/ ./internal/identity/` |
| Go full (integration) | `go test -tags 'db_integration neo4j_integration' ./internal/...` with composed DSNs (`AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`) + `mcp-neo4j-cypher` on PATH [CITED: CLAUDE.md integration env] |
| Go coverage gate | `make coverage` → `scripts/coverage_gate.sh`, owned-surface floor ≥85% (tunable `AURA_COVERAGE_MIN`) [VERIFIED: CLAUDE.md] |
| Frontend framework | Vitest 4 + `@vitest/coverage-v8` (thresholds 85/85/85/85 wired); Stryker 9 (`@stryker-mutator/vitest-runner`); Playwright 1.61 [VERIFIED: web/vitest.config.ts:22-26 + web/package.json:21-24,52-79] |
| Frontend quick run | `cd web && vitest run --coverage` |
| Frontend e2e | `cd web && playwright test` (e2e dir exists: graph/chat/health-panel/shell specs are the precedent) [VERIFIED: web/e2e/ listing] |
| Frontend mutation | `cd web && stryker run` (≥70% killed on touched dirs) [VERIFIED: web/package.json:23] |
| Contrast gate | `cd web && node scripts/contrast-check.mjs` (WCAG-AA, the locked gate) [VERIFIED: web/scripts/contrast-check.mjs exists] |

### Phase Requirements → Test Map

| Req | Behavior | Test type | Automated command | Exists? |
|-----|----------|-----------|-------------------|---------|
| GOV-01 | MCP board renders all servers, secrets redacted (no raw secret in response) | unit | `go test ./internal/agui/ -run TestGovernanceMCP` | ❌ Wave 0 |
| GOV-01 | Live probe: healthy → tool count + OK; **hung server → that row times out, others render** (mockable: isolate a hung server) | unit (mock probe) | `go test ./internal/agui/ -run TestMCPProbeIsolation` | ❌ Wave 0 |
| GOV-01 | Probe targets ONLY configured servers (prohibition #5) | unit | `go test ./internal/agui/ -run TestMCPProbeConfiguredOnly` | ❌ Wave 0 |
| GOV-02 | active/pending/archived/audit tabs list the correct set; **pending non-runnable + not injected** (no action field; loader never mounts pending) | unit + integration | `go test -tags db_integration ./internal/agui/ -run TestGovernanceSkills` + `./internal/skills/ -run TestPendingNotMounted` | ❌ Wave 0 (loader pending-exclusion partly covered by existing loader tests) |
| GOV-02 | audit tab newest-first | integration | `go test -tags db_integration ./internal/agui/ -run TestSkillsAuditOrder` | ❌ Wave 0 (AuditStore.List order covered by audit_store_integration_test) |
| GOV-03 | tasks + **paginated** run history (default limit), mutates nothing | integration | `go test -tags db_integration ./internal/cron/ -run TestListRunsForTask` + `./internal/agui/ -run TestGovernanceScheduler` | ❌ Wave 0 (new query + handler) |
| ONBD-02 | wizard drives 5-step LoopAgent; **exactly one LLM extraction per step; edit re-renders no re-prompt; replay no 2nd turn** | unit (mock LLM client) | `go test ./internal/agui/ -run TestOnboardingStep` + `./internal/onboarding/ -run TestNoDuplicatePrompt` | ❌ Wave 0 (session state machine covered by session_test/session_edge_test) |
| ONBD-02 | completing writes 8-section Agent.md; skip writes none | unit | `go test ./internal/onboarding/ -run TestDraftRender` (exists) + new handler test | partial ✅ (render_test) |
| ONBD-01a | **no-escalation validator** (subset ⊆ creator AND no `*`) | unit | `go test ./internal/agui/ -run TestNoEscalation` | ❌ Wave 0 |
| ONBD-01a | **cross-store saga + each compensation leg** (failure-injection B1/B2/A/C) → no orphan | integration | `go test -tags db_integration ./internal/<provision-pkg>/ -run TestProvisionSaga` | ❌ Wave 0 (the critical test) |
| ONBD-01a | **immutable audit row** (exactly one on success; append-only) | integration | `go test -tags db_integration ./... -run TestIdentityAuditImmutable` | ❌ Wave 0 (migration 0021 + store) |
| ONBD-01a | double-submit create idempotent (R5) | integration | `go test -tags db_integration ./... -run TestProvisionIdempotent` | ❌ Wave 0 |
| ONBD-01b | Telegram mint for NEW identity; consume links it; replay/expired rejected | integration | `go test -tags db_integration ./internal/channels/telegram/ -run TestConsumeOnboarding` (exists) + new mint-for-new-identity test | partial ✅ (store_integration_test) |
| Cross-cutting | every endpoint 401 unauth; create mutation 403 without cap | unit | `go test ./internal/agui/ -run TestGovernanceAuthGate` | ❌ Wave 0 (RequireAuth/RequireCapability covered for existing routes) |
| Cross-cutting | empty dataset → empty state (no crash); backend fail → sanitized error | unit | `go test ./internal/agui/ -run TestGovernanceEmptyAndError` | ❌ Wave 0 |
| Cross-cutting | **log scan over a full provisioning run contains no secret** (MCP env, Authula password, Telegram bot token) | integration | `go test -tags db_integration ./... -run TestProvisionNoSecretInLogs` | ❌ Wave 0 (capture slog output, assert absence) |

### Frontend test map

| Surface | Vitest (≥85%) | Stryker (≥70%) | Playwright | contrast-check |
|---------|---------------|----------------|------------|----------------|
| `web/src/governance/` (workspace + 3 boards + detail + api) | component + state/loading/empty/error/auth-error | mutate touched dir | `e2e/governance.spec.ts` (desktop) + `e2e/governance-mobile` (bottom-sheet, 44px targets) + a11y (arrow-nav, tab roles) | every board screen (desktop+mobile) |
| `web/src/onboarding/` (wizard + api) | per-step contract, capability picker, review screen, error paths (403/duplicate/rolled-back) | mutate touched dir | `e2e/onboarding.spec.ts` (full flow desktop) + mobile (full-screen single-column) | every wizard screen incl. QR step |

### Sampling Rate

- **Per task commit:** `go test ./internal/<pkg>/` + `go vet ./...` + `go build ./...` (CLAUDE.md Gate 2); frontend `vitest run` on touched dir.
- **Per wave merge:** `make quality` (vet+build+file-size+lint+test-race+vuln) + `cd web && vitest run --coverage && playwright test && node scripts/contrast-check.mjs`.
- **Phase gate:** `make quality-full` (owned-surface ≥85% across the tag matrix, stack up) + frontend ≥85% + Stryker ≥70% + all Playwright green + contrast 0 failures, BEFORE `/gsd-verify-work`.

### Wave 0 Gaps (test infra + backend seams to create before feature code)

- [ ] `internal/db/queries/agent_job_runs.sql` — add `ListRunsForTask :many` (GOV-03 pagination) + regenerate sqlc + `internal/cron/store_runs.go` `ListRunsForTask` wrapper.
- [ ] `internal/identity/store.go` — add `ListCapabilities(ctx, identityID) ([]string, error)` wrapper over the existing `ListCapabilities` sqlc query (D-06 picker).
- [ ] `cmd/aura/mcp_status.go` (or a new shared seam) — extract `probeServer(ctx,name,server) ProbeResult` (structured) from `mcpDoctorAll` (GOV-01 probe).
- [ ] `internal/skills/` — add a per-stage reader for `pending/`+`archived/` (GOV-02 tabs).
- [ ] `internal/db/migrations/0021_identity_audit.{up,down}.sql` — append-only `aura.identity_audit` (modeled on `skill_audit`: no UPDATE/DELETE grant + trigger) + sqlc query + store.
- [ ] `internal/agui/server.go` — add `SetGovernanceProviders(...)` + `SetOnboardingService(...)` seams (mirror `SetGraphView`).
- [ ] `internal/agui/governance_api_test.go` + `onboarding_api_test.go` — handler unit-test skeletons (mock the probe + the LLM client + the stores).
- [ ] `web/src/governance/__tests__/` + `web/src/onboarding/__tests__/` — Vitest skeletons; `web/e2e/governance.spec.ts` + `onboarding.spec.ts`.
- [ ] `web/src/i18n/resources.governance.ts` + `resources.onboarding.ts` — en+it bundles (28-UI-SPEC copy contract).

---

## Security Domain

> `security_enforcement` is enabled (CLAUDE.md gates + the SPEC's no-leak/no-escalation prohibitions). The SPEC explicitly defers Authula password-at-rest hashing + generic outbound-SSRF to `/gsd-secure-phase` (canon); this phase keeps the bespoke controls below.

### Applicable controls

| ASVS category | Applies | Standard control (this phase) |
|---------------|---------|-------------------------------|
| V2 Authentication | yes | Authula (password+TOTP) — the new user enrolls TOTP on first login (D-05). No new auth mechanism; reuse the embedded provider. |
| V3 Session | yes | `__Host-authula_session` + the server-held onboarding session (15m TTL, opaque token). |
| V4 Access Control | yes | `capability_grants` ONLY (no RBAC). Create mutation behind `RequireCapability(identity.create)`; no-escalation enforced server-side (D-06: subset ⊆ creator AND no `*`, re-validated at the saga). |
| V5 Input Validation | yes | `MaxBytesReader` body cap + enum/length validation in handlers (graph_api.go pattern); email/password length; capability-name grammar (`capNameRe`, identity store.go:33). |
| V6 Cryptography | yes (delegated) | Password hashing = Authula `PasswordService.Hash` (NEVER hand-rolled). Raw password never persisted/logged. |
| V7 Error/Logging | yes | `sanitizeErr` on every wire error; fixed-message logging on provision failure (no `err.Error()` verbatim — the setup `handleToken` precedent, handlers.go:44); the no-secret-in-logs test. |

### Threat patterns for this stack

| Pattern | STRIDE | Mitigation |
|---------|--------|-----------|
| Raw MCP env / Authula password / Telegram bot token leaked in a response/DOM/log | Information disclosure | `RedactSecrets` + redacted chips + write-only password field + bot token never in the QR (only the deep-link URL) + the log-scan test |
| Privilege escalation via the capability picker (grant `*` or a cap the creator lacks) | Elevation of privilege | Picker shows only creator-grants-minus-`*` (D-06); server re-validates subset; `GrantCapability` rejects `*` at the store (store.go:178); no-escalation unit test |
| Orphan/half-provisioned identity from a partial failure | Tampering / repudiation | The cross-store saga + per-leg compensation (§Hard Problem 1) + the immutable audit row + the failure-injection tests |
| Live MCP probe used as an SSRF pivot to an arbitrary target | Tampering | Probe iterates ONLY the loaded config (prohibition #5); `{name}` looked up in config, never a body-supplied URL/command; generic SSRF on the configured endpoints is canon (`/gsd-secure-phase` + the `/api/image-proxy` SSRF guard) |
| Pending skill made runnable / prompt-injected from the board | Tampering | Board renders NO run/activate control for pending (D-02); the loader never mounts pending bodies into LLM context (loader.go active-roots-only) |
| Token replay (Telegram link reused / after expiry) | Spoofing | `ConsumeOnboarding` single-use chokepoint (store.go:101-141, `ErrTokenConsumed`) — already enforced |

---

## Assumptions Log

| # | Claim | Section | Risk if wrong |
|---|-------|---------|---------------|
| A1 | `secret.IsSecretEnvKey` exists at a discoverable path for redacting env KEYS | Reuse map §MCP | Low — `RedactSecrets` (verified) already masks values; the key-chip is cosmetic. Confirm path at plan. |
| A2 | Per-skill `use count`/`last used`/`risk tier` are reconstructable from `snippet_usage.go` + audit `Action`/gate fields | Reuse map §Skills | Medium — if a field has no source, the board shows it as "—"; the SPEC lists them as targets, so confirm each source at plan (read `snippet_usage.go`). |
| A3 | The immutable `identity_audit` table should be NEW (migration 0021), modeled on `skill_audit` | §Hard Problem 1 | Low — `skill_audit` is the verified append-only template; CONTEXT (D's discretion) explicitly allows a new `aura.identity_audit`. |
| A4 | `aura.identities.name` has a UNIQUE constraint (enables idempotent double-submit) | §Hard Problem 1 | Medium — `GetIdentityByName` implies uniqueness; confirm the constraint in migration 0004 at plan (if absent, add a pre-check + handle the race explicitly). |
| A5 | The live session-validate path resolves identity via `ResolveIdentityID` only (not `OperatorUserID`), so the D-07 relaxation is enrollment-time-only | §Hard Problem 2 | Medium — if a live path calls `OperatorUserID`, the >1-user error would break login; the planner MUST grep the live path before the amendment. |
| A6 | Running the full saga at the final `confirm` (option 2) is compatible with D-04's "one flow" | §Hard Problem 4 | Low — D-04 says one flow, not one-write-per-step; flag to discuss-phase if the literal provision-first order is load-bearing. |
| A7 | Server-side QR via `rsc.io/qr` (already vendored, indirect) is acceptable vs a client QR lib | Reuse map §Telegram | Low — both work; server-side keeps the token-in-URL handling server-owned and adds no client dep. |

---

## Open Questions

1. **Tool-count probe depth (L1).** The existing doctor does NOT count mounted tools. Does GOV-01 require a real per-server `tools/list` (dial each server), or is "reachable + count-when-cheaply-available" acceptable? — *Recommendation:* real list under the 3s isolated timeout; flag if mini-PC load is a concern. (SPEC AC wants a live count.)
2. **Wizard sequencing (L9/A6).** Provision-then-interview (D-04 literal) vs saga-on-final-confirm (orphan-free). — *Recommendation:* saga at final confirm; confirm with discuss-phase if D-04 ordering is load-bearing.
3. **`aura.identities.name` uniqueness (A4).** Confirm the UNIQUE constraint for idempotent create. — *Recommendation:* read migration 0004 at plan; add explicit handling if absent.
4. **Scheduler board status scope (L10).** Active-only tasks, or all statuses? — *Recommendation:* active-only (matches "scheduled tasks"); run-history shows terminal runs regardless. A "show all" toggle is v2.

---

## Environment Availability

| Dependency | Required by | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Postgres 17 (aura.* + authula schemas) | All boards + provisioning | ✓ (stack) | 17 | — (blocking) |
| Authula module | Provisioning (D-05) | ✓ vendored | `github.com/Authula/authula@v1.11.0` | — |
| `rsc.io/qr` | Web QR render (ONBD-01b) | ✓ vendored (indirect) | v0.2.0 | client QR lib (not preferred) |
| `mcp-neo4j-cypher` + MCP servers | MCP board live probe | ✓ (PATH) | 0.6.0 | probe returns per-row error (isolated) |
| Vitest/Stryker/Playwright/contrast-check | Frontend gates | ✓ | vitest 4 / stryker 9 / playwright 1.61 | — |
| Telegram bot token | Live Telegram link (ONBD-01b) | runtime (`TELEGRAM_BOT_TOKEN`) | — | the mint/deep-link works without a live bot; live consume needs a real bot (live UAT) |

**No new external package is installed by this phase** (QR reuses the vendored `rsc.io/qr`). → **Package Legitimacy Audit: N/A** (zero net-new dependencies; the planner confirms no new `require` lands).

---

## Sources

### Primary (HIGH — read this session, file:line)
- `internal/mcp/manager/status.go`, `runtime.go`; `internal/mcp/redact.go`; `cmd/aura/mcp_status.go` — MCP board + live probe seams.
- `internal/skills/loader.go`, `audit_store.go` — skills board seams (loader single-snapshot landmine; `List` not `ListAudit`; append-only).
- `internal/cron/store.go`, `store_runs.go`; `internal/db/queries/agent_job_runs.sql` — scheduler seams + the missing run-history query.
- `internal/onboarding/session.go`, `extractor_llm.go`; `internal/profile/render.go` — onboarding state machine + no-duplicate-LLM mechanism.
- `internal/identity/store.go`; `internal/db/queries/capability_grants.sql` — identity + no-escalation seams.
- `internal/webauth/authula.go`, `identity_link.go`; `internal/webauth/authula_integration_test.go` — Authula provider + `OperatorUserID` guard + the proven `UserService.Create`/`Delete` path.
- `github.com/Authula/authula@v1.11.0` — `services/core.go` (CoreServices fields), `plugins/email-password/usecases/sign_up_usecase.go` (the verified create sequence), `models/providers.go` (`AuthProviderEmail="email"`).
- `internal/channels/telegram/store.go`, `onboarding.go`; `internal/setup/handlers.go`, `token.go`, `qr.go` — Telegram mint/consume + deep-link + QR-stub.
- `internal/agui/server.go`, `graph_api.go`, `conversations_api.go`, `server_redact.go`, `auth.go`; `cmd/aura/serve_webui.go`, `serve.go` — REST pattern + mount + composition root.
- `web/src/shell/modes.ts`, `web/src/graph/` listing, `web/src/i18n/` listing; `web/vitest.config.ts`, `web/package.json`, `web/e2e/` — frontend mount points + test infra.
- `internal/db/migrations/` listing (0019 authula schema + identity_auth_links; next slot 0021).

### Secondary (curated D:/tmp — read this session)
- `D:/tmp/elysia-frontend/app/components/explorer/{DataExplorer,DataTable,DataMetadata}.tsx` + `configuration/{ConfigurationDashboard,ConfigSidebar}.tsx` — master-list+detail+pagination + settings-board patterns.
- `D:/tmp/aura-uiux/*.png` — the locked visual reference (login/graph/mobile-inspector).
- `D:/tmp/nanobot/webui/`, `D:/tmp/odysseus/static/` — minimal-industrial + density cues.

### Tertiary (online — MEDIUM, cross-checked against 28-UI-SPEC)
- [ui-patterns.com dashboard pattern](https://ui-patterns.com/patterns/dashboard) — operational dashboard freshness/read-only.
- [Admin Dashboard UI/UX Best Practices 2025 (Medium)](https://medium.com/@CarlosSmith24/admin-dashboard-ui-ux-best-practices-for-2025-8bdc6090c57d) — live-data cues, read-only via permissions.
- [Software wizard (Wikipedia)](https://en.wikipedia.org/wiki/Wizard_(software)) + [BigID access-provisioning lifecycle](https://bigid.com/blog/access-provisioning-lifecycle/) — linear wizard + permission-review-before-issuance.

### Phase docs (LOCKED — consumed)
- `28-SPEC.md`, `28-CONTEXT.md`, `28-UI-SPEC.md`; `.planning/REQUIREMENTS.md`; `.planning/PROJECT.md` (§Out of Scope:90); `.planning/ROADMAP.md` (:56,:254); `CLAUDE.md`.

---

## Metadata

**Confidence breakdown:**
- Reuse map (backend seams): **HIGH** — every symbol read at file:line this session.
- Saga + Authula create path: **HIGH** — verified against the vendored module + the existing Aura integration test exercises the exact calls.
- Live MCP probe concurrency: **HIGH** (model) / **MEDIUM** (tool-count depth — L1/OQ1).
- Skills per-skill metadata aggregation: **MEDIUM** — content hash + audit verified; usage/risk-tier sources need a plan-time read (A2).
- D-07 amendment targets: **HIGH** — exact file:line collisions located; A5 (live-path `OperatorUserID` usage) needs a confirming grep.
- Industrial pattern survey: **HIGH** (D:/tmp concrete) / **MEDIUM** (online generic).
- Validation architecture: **HIGH** — test infra all verified present.

**Research date:** 2026-06-20
**Valid until:** ~2026-07-20 (stable substrate; the only moving parts are the Authula module version and the frontend toolchain, both pinned).
