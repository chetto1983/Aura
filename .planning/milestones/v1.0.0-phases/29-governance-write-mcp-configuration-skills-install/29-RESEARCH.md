# Phase 29: Governance Write — MCP Configuration + Skills Install — Research

**Researched:** 2026-06-21
**Domain:** Cockpit WRITE surface (Go REST + React) over the existing Phase-11/16/25/28 backend
**Confidence:** HIGH (every reuse symbol located + signature quoted in the live tree; UI/UX grounded in the locked design system + curated D:/tmp + verified 2026 online canon)

## Summary

Phase 29 is a **write-over-existing-backend** phase. The MCP manager (Phase 16), the skills Writer/gate/resume/audit (Phase 11), the HITL `/api/approvals` queue (Phase 25), and the read-only governance boards (Phase 28) all exist and were located precisely. Only **four thin gap-fillers are net-new code**: (1) the `aura.mcp_audit` append-only ledger (`0022_mcp_audit`, mirroring `0021_identity_audit` verbatim), (2) an in-place MCP env-edit path (load→set-one→whole-entry atomic write via `SaveManagedConfig`, reusing `mergeEnvPreserveCredentials` for D-05), (3) the skills HTTP write endpoints + a **net-new Go-side `npx skills` install transport** (the CLI/native installer was deleted in plan 11-09 — this is the largest single new piece), and (4) the `governance.write` capability string (already referenced at `auth_test.go:494`).

The single most important sequencing fact: **D-09 is a BLOCKING SPEC-amendment** the planner must land FIRST (before the skills-install wave), because operator directives D-06/D-07 (run `npx skills` WITH scripts in Aura's container; NO forced `--ignore-scripts`) contradict the LOCKED 29-SPEC.md (SKW-01 checklist item #1, the constraint, and prohibition #5 all name `--ignore-scripts`). Per CLAUDE.md PRD-first, the amendment is a gsd-tooling commit, not a direct SPEC Write, mirroring Phase-28's D-07.

The deep-research mandate (online + D:/tmp) confirms the design is the minimal industrial shape: redaction-at-source (key-only env DTO), append-only audit ledger, a single confirmation for the recoverable `Remove` (no type-to-confirm), and pending-inert + Writer-validation-before-approval. The 2026 online canon (Anthropic's "93% of permission prompts approved → shrink what reaches the human", redaction-at-source-not-sink, approval-on-registration) maps 1:1 onto the locked plan. The UI-SPEC already executed the visual contract from the same curated D:/tmp references (elysia-frontend config surfaces, odysseus admin idiom, NN/g confirmation guidance).

**Primary recommendation:** Sequence the SPEC-amendment as wave 0. Then build the `mcp_audit` migration + audit store + a single `MutatingProvider` seam carrying all six MCP write verbs (each `db.WithTx(config-write + one audit row)`), the env-edit path on `mergeEnvPreserveCredentials`, the skills HTTP endpoints wrapping `Writer.WriteInstallPending`/`Activate`/`Archive`/`Restore` + the new `npx skills` transport, and extend the Phase-28 React boards in place. Reuse — do not re-invent — every backend primitive below.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| MCP install (recipe/custom) | API / Backend (`internal/agui` handler → `internal/mcp` `SaveManagedConfig`) | DB (audit row) | Config persistence + audit are server-owned; the form is a thin client |
| MCP env-edit (4-state + redaction) | API / Backend (env-edit path) | Frontend (four-state render + redacted chip) | Secret preservation MUST be server-side (`mergeEnvPreserveCredentials`); the client never holds the value |
| MCP trust-approve | API / Backend (writes `ApprovedBy/At/Reason` + audit) | Frontend (inline operator action) | Operator decision, not model-gated; populates today-empty trust fields |
| MCP enable/disable/remove | API / Backend (`SaveManagedConfig` + audit) | Frontend (toggle/confirm) | Lifecycle mutation + audit are atomic server-side |
| MCP live probe / tool-count | API / Backend (`ProbeServer`, bounded timeout) | Frontend (per-row async) | Already shipped (Phase 28); reuse the per-row isolation contract |
| Skill install (source/catalog) | API / Backend (`npx skills` transport → Writer gate → `pending/`) | Frontend (RISKY panel + checklist) | Supply-chain fetch + validation + staging are server-owned; UI surfaces the result |
| Skill approval/activation | API / Backend (`/api/approvals` resolve → `ResumeHandler` → `Activate`) | Frontend (reuse Phase-25 queue) | No model-facing approve; activation is the resume bridge only |
| Skill restore/archive | API / Backend (`Writer.Restore`/`Archive` + audit) | Frontend (tab buttons) | FS move + audit are server-owned; collision-guard is server-side |
| Append-only audit (mcp + skill) | DB (role grant + dual trigger) | API (read projection) | Tamper-evidence is a DB invariant, not an app check |
| Auth / capability gate | Frontend Server (SSR mux `RequireAuth`) → API (`RequireCapability`) | DB (`capability_grants`) | Whole-origin gate + per-route capability; the principal is server-bound |

## Standard Stack

This phase invents **no new dependencies**. It reuses the shipped stack. The one external runtime tool — the `npx skills` CLI — is verified below.

### Core (all already in the tree)
| Library / Module | Version | Purpose | Why Standard |
|------------------|---------|---------|--------------|
| `internal/mcp` + `internal/mcp/manager` | Phase 16 | Managed-config CRUD, trust classes, probe, catalog | The MCP control plane; reuse, never re-invent |
| `internal/skills` | Phase 11 | Writer gate, install-pending, activate/restore/archive, audit | Full skill lifecycle exists; HTTP endpoints wrap it |
| `internal/agui` | Phase 24/25/28 | Thin REST handlers, `RequireAuth`/`RequireCapability`, `/api/approvals` | The route + auth + approval substrate |
| `internal/db` (`WithTx`, `sqlc`) + `golang-migrate` | shipped | Atomic mutation+audit tx; the migration runner | `db.WithTx` is the D-04 atomicity primitive |
| `internal/secret` (`IsSecretEnvKey`) | shipped | Canonical secret-env predicate | One redaction denylist (B-09 fix); reuse |
| `@tanstack/react-query` (`useMutation`/`invalidateQueries`) | shipped | Frontend write data layer | The board write hooks; `governanceApi.ts` adds `postJSON`/`patchJSON`/`deleteJSON` |
| `react-i18next` | shipped | en+it copy | `resources.governance.ts` (201 lines) — add keys to BOTH locales |

### Supporting — the external skills transport
| Tool | Version | Purpose | When to Use |
|------|---------|---------|-------------|
| `skills` (npx CLI, `vercel-labs/skills`) | `1.5.12` `[VERIFIED: npm registry]` | `npx skills find <q>` / `npx skills add <src> -y` discovery+install | Only in the cockpit skill-install transport, run inside Aura's container (D-06) |

**Installation:** none. `npx skills` is invoked at runtime via `exec.CommandContext` inside Aura's container (the new gap-filler #3 transport). Strip ANSI; `-y` non-interactive; provenance in the body (spike-proven, `Skill("spike-findings-Aura")`).

**Version verification (executed 2026-06-21):**
```
npm view skills version            → 1.5.12
npm view skills repository.url      → git+https://github.com/vercel-labs/skills.git
npm view skills time.modified       → 2026-06-18T17:38:26.661Z   (actively maintained)
npm view skills scripts.postinstall → (empty — no postinstall script)
```

## Package Legitimacy Audit

> The only external package this phase invokes is the `skills` npx CLI (the skills.sh transport). No new Go modules or npm deps are added.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `skills` | npm | maintained (last publish 2026-06-18) | n/a (CLI, npx-invoked) | github.com/vercel-labs/skills | OK (no SLOP/SUS verdict) | Approved — already the spike-proven, in-production model-path transport |

**Packages removed due to slopcheck [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** none.

slopcheck (present in env) returned no flag for `skills`; `npm view skills scripts.postinstall` is empty (no postinstall network/FS hook). The package is the Vercel-Labs `skills` CLI already used by the model self-extension path — provenance and registry both confirmed. Note: per D-06/D-07 the *install scripts of fetched skills* are deliberately permitted (container = isolation boundary); that is orthogonal to the `skills` CLI's own (absent) postinstall.

## Exact-File-Location Findings

> Format: **symbol** — `path:line` — signature/quote — **EXISTS / NET-NEW** — note for the planner.

### MCP backend (Phase 16 — `internal/mcp/`)

- **`LoadManagedConfig` / `SaveManagedConfig`** — `internal/mcp/managed_config.go:101` / `:122` — `func SaveManagedConfig(path string, doc ManagedConfig) error` (atomic: `MarshalIndent` → `os.WriteFile(path, data, 0o600)` → `os.Chmod 0o600`; `validateManagedServers` first). **EXISTS.** The env-edit + install + lifecycle writes all funnel here. Atomic temp+rename note: current impl writes directly with `0o600` (not a temp+rename), so an interrupted write could in theory truncate — **planner: the concurrency edge (MCPW-01 "interrupted install leaves prior config intact") should add a temp+rename to `SaveManagedConfig` OR document that the write is small+atomic-enough; verify against the SPEC edge.**
- **`ManagedConfigPath`** — `internal/mcp/managed_config.go:87` — honors `AURA_MCP_CONFIG` override, else `~/.aura/mcp/servers.json`. **EXISTS.** This is the destination-preview source for MCPW-01. (Note: the SPEC also names `AURA_MCP_SERVERS_JSON` as a read-only overlay — that overlay is merged at mount but `ManagedConfigPath`/`SaveManagedConfig` only ever write the managed file; the preview must show `ManagedConfigPath()` as the write target, and *separately* note if an `AURA_MCP_SERVERS_JSON` overlay is in effect.)
- **`ManagedServer` + `ManagedTrust`** — `internal/mcp/managed_config.go:51` / `:65` — `ManagedTrust{ Class, ApprovedBy, ApprovedAt, Reason string }`. **EXISTS** (fields defined). `ApprovedBy/ApprovedAt/Reason` are **never populated today** (confirmed — `mcpTrust` sets only `Class`). The trust-approve write (D-12) populates all three.
- **`BuiltInCatalog()` + `CatalogEntry.RequiredEnv`** — `internal/mcp/manager/catalog.go:101` / `:93` — `RequiredEnv []string` field on `CatalogEntry`. **EXISTS but never validated/surfaced.** The 4 recipes (calculator/calendar/whatsapp/memory) — **none currently sets a non-empty `RequiredEnv`** (calculator has `Env: ["PYTHONUNBUFFERED=1"]` but no `RequiredEnv`). Planner: the guided-form (MCPW-01) renders `RequiredEnv` per recipe; for the shipped recipes this may be empty — the form still must handle the populated case, and a future recipe with `RequiredEnv` drives the soft-warning path. `LookupCatalog(name)` — `catalog.go:186` — resolves one recipe.
- **`RedactEnv` / `mergeEnvPreserveCredentials` / `isPlaceholderValue`** — `internal/mcp/manager/config.go:78` / `:95` / `:145` — `func RedactEnv(env []string) []string` (→ `KEY=${KEY}` for secret keys); `mergeEnvPreserveCredentials(existing, incoming []string)` (preserves an EXISTING real credential against any incoming override, but lets a real value replace a placeholder); `isPlaceholderValue(key, value) → value == "${KEY}" || value == ""`. **EXISTS.** **This is the D-05 substrate.** The env-edit path = load → apply the submitted env over the existing entry via `mergeEnvPreserveCredentials` semantics (a submitted `${KEY}` redacted-placeholder is treated as "unchanged" and the stored secret is preserved) → `SaveManagedConfig`. `ImportProfile` — `config.go:47` — uses exactly this merge with `OverwriteCredentials` opt; the env-edit can call a thin variant or `ImportProfile` with a single-server incoming doc.
- **`secret.IsSecretEnvKey`** — `internal/secret/envkey.go:69` — `func IsSecretEnvKey(name string) bool` (case-insensitive substring vs `secretEnvMarkers`: key/token/secret/pass/auth/bearer/credential/private/cert/dsn/conn/pwd/cookie/session/jwt/url/uri). **EXISTS.** Drives the redacted-chip flag (already wired in `envChips`, `governance_api.go:209`).
- **Trust classes** — `internal/mcp/managed_config.go:21-25` — `TrustTrustedRecipe/TrustTrustedLocal/TrustSandboxedLocal/TrustRemoteHTTP/TrustBlocked`; custom defaults to `TrustBlocked` (`mcpAdd` sets `trustClass := mcp.TrustBlocked`, `cmd/aura/mcp.go:141`). **EXISTS.** `NormalizedTrust(name)` — `managed_config.go:220` — infers recipe→trusted, http→remote, else blocked.
- **`RunnableManagedServers`** — `internal/mcp/manager/runtime.go:53` — `func RunnableManagedServers(doc) (map[string]ManagedServer, error)` — **silently `continue`s on `normalizedTrustForServer(server) == TrustBlocked`** (line 60-62) and on disabled. **EXISTS.** The SPEC wants the blocked-skip to **surface a warning row** — planner: the board already reports `StartupBlocked` via `SnapshotStatus` (status.go:55), so the "warning" is a render of that existing state, plus the fail-soft probe warning (already shipped). No backend change strictly required for the blocked-warning; the destructive/denied-tool surfacing is the mount-time risk policy (below).
- **Mount-time risk policy** — the per-tool risk enforcement happens **before** registry insert in the mount path (`MountManagedServer`, referenced in catalog comments). `RuntimeLaunchConfig` — `runtime.go:87` — returns `errMCPServerBlocked` when trust is blocked. **EXISTS** (Phase 16, UNCHANGED per D-discretion). The denied/destructive-tool-shown-explicitly behavior is the Phase-28 read board's job extended with a `danger` marker; no new gate logic.
- **`ProbeServer`** — `internal/mcp/probe.go:43` — `func ProbeServer(ctx context.Context, name string, server ManagedServer) ProbeResult` (dials, `tools/list`, counts; HTTP endpoints reported reachable-by-config; redacts Err; bounded by caller ctx). **EXISTS.** Already wired into the board (`handleMCPProbe`, `governance_api.go:240`, 3s timeout, per-row isolation). Reuse verbatim post-install/trust for the live tool-count.
- **CLI mutators write NO audit row (CONFIRMED):** `mcpInstall` (`cmd/aura/mcp.go:96`), `mcpAdd` (`:127`), `mcpSetEnabled` (`:290`), `mcpRemove` (`:315`), `mcpTrust` (`cmd/aura/mcp_profile.go:153`) all call `SaveManagedConfig` with **no `mcp_audit` write** (the table does not exist yet). `mcpInstall` copies `recipe.Server` directly — **does NOT prompt/validate `RequiredEnv`** (`mcp.go:119`). `mcpTrust` sets `Trust = ManagedTrust{Class: TrustTrustedLocal}` only — **leaves `ApprovedBy/At/Reason` empty** (`mcp_profile.go:169`). All four gaps confirmed exactly as CONTEXT predicted.

### Skills backend (Phase 11 — `internal/skills/`)

- **`ComputeSkillTier` / `GateRecommended`** — `internal/scoring/scoring.go:126` / `:140` — `delete → Destructive`, `create/update/install → Risky` (`:129-131`); `GateRecommended(t) → t == Risky || t == Destructive` (`:140`). **EXISTS.** Install is always Risky → always gated. The UI RISK-TIER badge reads this.
- **`SanitizeName` / `ValidateForWrite` / `violatesBlocklist`** — `internal/skills/validator.go:41` / `:82` / `:58` — `SanitizeName` enforces `^[a-z0-9-]{1,64}$` AND name==dir; `ValidateForWrite(fm, body, blocklist, bodyCapBytes, allowBlocklisted)` runs name → description-len → body-cap → type-enum → NFKC-normalized+case-folded injection blocklist with matched-byte-position (`ErrBlocklisted: matched %q at byte %d`). **EXISTS.** These are the **FIVE checklist items the UI surfaces** (sanitized name/path, `SKILL.md` parse, body cap, injection-literal blocklist, sanitized env) — the `--ignore-scripts` sixth item is REMOVED per D-09.
- **`HashSkillFiles` / `HashSkillDir`** — `internal/skills/contenthash.go` (referenced from `writer.go:108`, `writer_activate.go:33`) — canonical `sha256:` byte-sorted (relPath,bytes). **EXISTS.** The content-hash the UI shows.
- **`Writer.WriteInstallPending`** — `internal/skills/writer.go:179` — `func (w *Writer) WriteInstallPending(ctx, fm Frontmatter, body, stagedDir, hash string, actor AuditActor) (string, error)` — promotes a staged tree into `pending/<name>/` (symlink-stripped, atomic temp+rename), records the D-29 install audit tuple (`action=install`, `gate_recommended=true, gate_taken=false`) **inside `db.WithTx`**, NEVER self-activates → returns `StatusPendingApproval`. **EXISTS.** **This is the skill-install sink** — the new HTTP install endpoint stages the `npx skills`-fetched tree to a temp dir, then calls this.
- **`Writer.WriteMutation` / `WriteMutationByName`** — `writer.go:94` / `:232` — create/update/install/delete entry; computes tier+gate, `ValidateForWrite`, hash, writes pending + audit in `db.WithTx`. **EXISTS.** The create/update/delete HTTP endpoints (SPEC skills gap-filler) wrap `WriteMutationByName`.
- **`Writer.Activate` / `Archive` / `Restore` / `Delete` / `DiscardPending`** — `internal/skills/writer_activate.go:24` / `:68` / `:112` / `:144` + `resume.go:66` — each moves dirs + (de)materializes + one audit row in `db.WithTx`. **EXISTS.** **Restore collision landmine:** `Restore` (`:112`) calls `promoteDir` (`:253`) which does `os.RemoveAll(dst)` first → it would **SILENTLY OVERWRITE an active skill of the same name**. SKW-03 requires a name-collision to be **REJECTED**. Planner: the new HTTP restore endpoint MUST check `active/<name>` existence and 409 BEFORE calling `Writer.Restore` (the backstop edge).
- **`ResumeHandler.Resume`** — `internal/skills/resume.go:48` — `func (h *ResumeHandler) Resume(ctx, action, name, pausedToken string, actor) error` — `accept → Writer.Activate(ApprovalAskUser, token)`; `decline/cancel → DiscardPending`. **EXISTS.** **This is the skill-install activation bridge** — the `/api/approvals` resolve path already routes to the Runner's `SubmitAnswers`, which (when the pause was a skill-approval pause) invokes this handler. NO model-facing approve. Reuse verbatim.
- **`ask_user` pause → `paused_states`** — `ErrAwaitingUserInput` → `aura.paused_states`; the install gates via this pause and surfaces in the cross-thread `/api/approvals` queue. **EXISTS.** A pending skill lives in `pending/` and the loader scans `active/` ONLY (`StageReader.ListStage`, `stage_reader.go:41`) — pending is non-runnable + never injected by construction.
- **`StageReader.ListStage` + `StageSkill` + `UsageSidecar`** — `internal/skills/stage_reader.go:41` / `:26` + `snippet_usage.go:19` — `ListStage(pendingDir, archiveDir, stage)`; `UsageSidecar{ LastUsedAt, UseCount }` + status. **EXISTS.** Drives the four-tab metadata (last used / use count / TTL/archive). `StampUsage` (`:117`), TTL sweep (`:157`).
- **`aura.skill_audit` + `AuditStore.List` + `InsertAuditTx`** — migration `0010`; `internal/skills/audit_store.go:69` (`AuditStore`), `:101` (`AuditInsert`), `:150` (`InsertAuditTx`), `:170` (`List`). **EXISTS.** Append-only (role SELECT+INSERT + dual triggers + D-29 coherence CHECK). The audit tab already reads it (`handleSkillsAudit`, `governance_api.go:343`).
- **Skills HTTP write endpoints: ZERO exist today (CONFIRMED).** `governance_api.go` has only the six GET reads. **NET-NEW (gap-filler #3).**
- **Go-side `npx skills` installer: DOES NOT EXIST (CONFIRMED).** Plan 11-09 (amendment #51 / D-40) **DELETED the catalog client + native installer + their tool actions** (`internal/agent/tools/skill.go:25`). The ONLY `npx skills` invocation today is the **model** path via `shell_exec` in the sandbox (no Go-callable installer). **NET-NEW (gap-filler #3, the largest piece):** a Go transport that runs `npx skills find`/`add -y` (`exec.CommandContext`) inside Aura's container, strips ANSI, stages the fetched tree to a temp dir, parses its `SKILL.md` frontmatter, computes the canonical hash, then hands off to `Writer.WriteInstallPending`. Per D-06/D-07 the install runs WITH scripts (container = isolation).

### Approval / HITL (Phase 25)

- **`/api/approvals` queue** — `internal/agui/approvals_api.go:47` — `GET /api/approvals` (`handleListApprovals` → `s.approvals.ListPendingAll(ctx, limit)`, `SanitizeString` on the question) + `POST /api/approvals/{token}/resolve` (`handleResolveApproval` → uuid-guard → `resolveAction(accept|decline|cancel)` → `s.run.SubmitAnswers(map[token]{Action,Content})`). **EXISTS.** The skill-install approval is an `ask_user` pause that lands in this same cross-thread queue; the resolve path drives the Runner → `ResumeHandler` (D-11). Reuse — do not mint a second queue.

### REST / route + auth pattern

- **`registerGovernanceRoutes`** — `internal/agui/governance_api.go:144` — the six GET reads on `Server.Mux`. **EXISTS.** The new write routes register here (e.g. `registerGovernanceWriteRoutes`).
- **Thin-handler-over-provider shape** — `governance_api.go` handlers nil-check `s.governance.MCP`/`.Skills` (503 when unwired), one provider call, JSON projection, `SanitizeString`/`sanitizeErr`, `envChips` (key-only). **EXISTS.** The write handlers mirror this exactly.
- **`governance_seam.go` provider interfaces** — `internal/agui/governance_seam.go:28` (`MCPBoardProvider`), `:37` (`SkillsBoardProvider`), `:53` (`GovernanceProviders`), `:101` (`SetGovernanceProviders`). **EXISTS (read-only).** **NET-NEW:** a mutating provider seam (e.g. `MCPWriteProvider` / `SkillsWriteProvider`, or extend the existing bundles) carrying install/env-edit/trust/enable/disable/remove + skill install/activate/restore/archive — declared consumer-side, wired at the composition root.
- **`RequireAuth` / `RequireCapability` / `withPrincipal` / `principalFrom`** — `internal/agui/auth.go:194` / `:261` / `:287` / `:296` — `RequireCapability(next, deps, capability)` reads the principal, `HasCapability(principal, capability)`, 403 on missing/denied; `*`-wildcard handled by the store; loopback dev = pass-through. **EXISTS.** **`governance.write` reference confirmed at `auth_test.go:494`** (`RequireCapability(next, deps, "governance.write")` → expects 403 when ungranted). Lock the capability as `governance.write`.
- **Parent-mux mount discipline** — `cmd/aura/serve_webui.go:317-322` (read governance routes behind `RequireCapability(governance.read)`) + `:331-332` (onboarding CREATE behind `identity.create`) + `:258`/`:284-285`/`:292` (other mutations behind `agentRunCapability`). **EXISTS.** **NET-NEW:** add `const governanceWriteCapability = "governance.write"` and mount each new mutating route `mux.Handle(<METHOD PATH>, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))` as a method+path-specific sibling under the `/api/` carve-out (Go 1.22 longest-pattern precedence over the bare subtree). **CSRF LANDMINE:** `auth.go:18` explicitly says *"Re-evaluate if Phase 28/29 introduces a cross-origin write surface"* — Phase 29 IS the largest write surface. Same-origin SameSite=Strict still covers the SPA (no cross-origin write path), but the planner must consciously affirm this (the TanStack `useMutation` calls are same-origin `credentials:'same-origin'`).

### Migration precedent (the `mcp_audit` model — D-02)

- **`0021_identity_audit.up.sql`** — `internal/db/migrations/0021_identity_audit.up.sql` — the EXACT template: `CREATE TABLE` with `id uuid PK`, `created_at timestamptz`, actor + action fields; `reject_*_mutation()` plpgsql function; `BEFORE UPDATE OR DELETE ... FOR EACH ROW` trigger; **SEPARATE** `BEFORE TRUNCATE ... FOR EACH STATEMENT` trigger (Pitfall 1: a row trigger never fires for TRUNCATE); `GRANT SELECT, INSERT ... TO aura_app` (no UPDATE/DELETE/TRUNCATE); `GRANT ALL ... TO aura_migrate`. **EXISTS.** Plus `0010_skill_audit` (the same pattern with a D-29 coherence CHECK — `mcp_audit` is flat like `0021`, no CHECK). **`0021` IS the latest shipped migration (confirmed — no `0022`+).** `0022_mcp_audit` is the correct next slot. The `mcp_audit` schema: `id`, `created_at`, `actor_identity_id text` (D-03: the capability-layer principal, NOT the raw Authula id), `action text` (install/edit/enable/disable/remove/trust), `server_name text`, `reason text` (NULL except on trust). **NET-NEW migration + a thin `MCPAuditStore`** mirroring `AuditStore` (`InsertMCPAuditTx` callable inside `db.WithTx`).
- **`db.WithTx`** — `internal/db/tx.go:22` — `func WithTx(ctx, pool, fn func(*sqlc.Queries) error) error`. **EXISTS.** Every MCP mutation = `db.WithTx(SaveManagedConfig-driven write + InsertMCPAuditTx)` so the config write and the audit row commit together (D-04). **Caveat:** `SaveManagedConfig` writes a FILE, not a SQL row — the atomicity is "FS write then audit INSERT in tx"; mirror the skills `WriteInstallPending` reconcilable-FS-before-tx ordering (FS write is reconcilable by a boot scan; the tx is the audit INSERT). The planner must decide ordering: write config file → `db.WithTx(audit INSERT)`; if the audit INSERT fails, the config file is already written (audited-but-… inverted). Safer: `db.WithTx` wrapping a closure that (a) writes the file and (b) inserts the row, with the file-write inside the tx scope so a tx rollback leaves the prior file — BUT a file write is not transactional. **Recommendation:** write to a temp file, `db.WithTx(INSERT audit)`, then on tx success `os.Rename(temp, path)`; on tx failure discard the temp. This gives true atomicity (no applied-but-unaudited, no audited-but-not-applied) and gives `SaveManagedConfig` the temp+rename the concurrency edge wants. Flag as a design decision.

### Frontend mount points (D-10 — extend Phase-28 in place)

- **`web/src/governance/`** — `GovernanceWorkspace.tsx`, `McpBoard.tsx`, `McpServerDetail.tsx`, `SkillsBoard.tsx`, `SkillDetail.tsx`, `BoardLayout.tsx`, `governanceView.tsx` (`BoardStateView`/`boardStatus` five-state machine), `governanceApi.ts`. **EXISTS.** `McpServerDetail.tsx:48-87` is the env-keys `<section>` the env-edit form replaces in edit mode; the `<dl>`/`Field` idiom + redacted-chip render (`:71-82`) are the reuse anchors.
- **`governanceApi.ts`** — `web/src/governance/governanceApi.ts:1-209` — has `getJSON` (rejects non-200 incl 401) + the six read fns; comment line 10: *"The boards are READ-ONLY (all writes are Phase 29) — there is no postJSON here."* **NET-NEW:** add `postJSON`/`patchJSON`/`deleteJSON` (same `credentials:'same-origin'`, `retry:false`, `encodeURIComponent`) + the write DTOs (install request, env-edit request, trust request, skill-install request, catalog-search response).
- **`web/src/shell/modes.ts:1`** — `MODES = [...,'governance',...]`; `governance` live since Phase 28. **EXISTS** — no mode change.
- **`web/src/AppShell.tsx:29`** — `GovernanceWorkspace = lazy(() => import('./governance/GovernanceWorkspace'))`; `:97` `isFocusedWorkspace = surface === 'graph' || 'governance'`. **EXISTS** — the write controls live inside the existing lazy chunk.
- **`web/src/approvals/`** — `ApprovalList.tsx`, `InlineApprovalCard.tsx`, `ApprovalBadge.tsx`, `approvalState.ts`, `useApprovals.ts`. **EXISTS.** The skill-approval entry reuses these (D-11) — extend the card to show source/hash/preview/risk-tier/resume-token; do NOT mint a new surface or a second badge.
- **`web/src/i18n/resources.governance.ts`** (201 lines) — **EXISTS.** Add every new key to BOTH `governanceEn` and `governanceIt` (the UI-SPEC Copywriting Contract enumerates them).
- **Frontend gates** — `web/scripts/contrast-check.mjs` (WCAG-AA pairs), Vitest ≥85%, Stryker ≥70%, Playwright e2e + axe. **EXISTS.** New fg/bg pairs (e.g. `text-warning on surface-2`) must be added to the contrast script.

## Architecture Patterns

### System Architecture Diagram (write data flow)

```
                          Browser (React, web/src/governance/*)
                                   │  TanStack useMutation
                                   │  POST/PATCH/DELETE  credentials:'same-origin'
                                   ▼
                 SSR mux (cmd/aura/serve_webui.go)
                 RequireAuth  ──►  RequireCapability("governance.write")
                                   │  (principal bound on ctx)
                                   ▼
                 agui write handler (internal/agui/governance_write_api.go [NEW])
                 ─ parse + size-cap body, sanitize, nil-check provider (503)
                 ─ ONE provider call
                                   │
            ┌──────────────────────┼───────────────────────────────┐
            ▼                      ▼                                ▼
   MCP write provider [NEW]   Skills write provider [NEW]    /api/approvals (Phase 25)
   ─ install/env-edit/trust/  ─ install (npx skills [NEW]    ─ resolve token →
     enable/disable/remove      → Writer gate → pending/)      Runner.SubmitAnswers →
            │                  ─ create/update/delete           ResumeHandler.Resume →
            ▼                  ─ restore/archive                 Writer.Activate
   db.WithTx {                          │                              │
     temp config write +                ▼                              ▼
     InsertMCPAuditTx [NEW]    db.WithTx { FS stage +         pending/ → active/
   } → os.Rename(temp,path)      InsertAuditTx (skill_audit) }  + skill_audit row
            │                          │
            ▼                          ▼
   ~/.aura/mcp/servers.json    ~/.aura/skills/{pending,active,archived}/
   aura.mcp_audit [NEW table]   aura.skill_audit (existing)
            │
            ▼  (post-write, async, per-row, bounded 3s)
   ProbeServer → live tool-count → board re-fetch
```

### Recommended structure (additive only)
```
internal/agui/
├── governance_write_api.go     # NEW: the write handlers (mirror governance_api.go)
├── governance_write_seam.go    # NEW: MCPWriteProvider / SkillsWriteProvider interfaces + setters
internal/mcp/manager/
├── envedit.go                  # NEW: load→set-one→whole-write (reuse mergeEnvPreserveCredentials)
├── audit.go                    # NEW: MCPAuditStore + InsertMCPAuditTx (mirror skills/audit_store.go)
internal/skills/
├── installer.go                # NEW: npx skills find/add transport → stage → WriteInstallPending
internal/db/migrations/
├── 0022_mcp_audit.up.sql       # NEW: mirror 0021_identity_audit verbatim
├── 0022_mcp_audit.down.sql     # NEW
web/src/governance/
├── McpInstallPanel.tsx         # NEW (detail-pane slot)
├── McpEnvEditForm.tsx          # NEW (four-state + redacted chip)
├── SkillInstallPanel.tsx       # NEW (RISKY panel, 5-item checklist)
├── (extend) McpServerDetail / SkillsBoard / governanceApi.ts / resources.governance.ts
```

### Pattern: atomic mutation + audit (D-04)
**What:** Every MCP write commits the config change and its audit row together.
**When:** install / env-edit / enable / disable / remove / trust.
```go
// Source: internal/skills/writer.go:210 (WriteInstallPending) — the proven shape
// MCP variant: temp-write the config, INSERT audit in tx, rename on commit.
tmp := writeConfigTemp(path, nextDoc)              // reconcilable FS
err := db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
    return InsertMCPAuditTx(ctx, q, MCPAuditInsert{
        ActorIdentityID: principal, Action: "trust", ServerName: name, Reason: reason,
    })
})
if err != nil { os.Remove(tmp); return err }       // no applied-but-unaudited
return os.Rename(tmp, path)                          // commit the config atomically
```

### Pattern: env-edit credential preservation (D-05)
```go
// Source: internal/mcp/manager/config.go:95 (mergeEnvPreserveCredentials)
// Submitted env over existing: a redacted ${KEY} placeholder preserves the stored secret.
next.Env = mergeEnvPreserveCredentials(existing.Env, submitted.Env)
// isPlaceholderValue(key, "${KEY}") == true → existing real secret wins, never overwritten.
```

### Anti-Patterns to Avoid
- **Re-implementing the skills gate / scoring / activation.** Wrap `Writer.*` — never re-derive tier, validation, or the resume bridge.
- **A second approval queue or badge.** Reuse `/api/approvals` + the cross-thread badge (D-11).
- **Sending an env VALUE to the client** (even masked). The backend sends key-only chips (`envChips`); the edit form holds the redacted placeholder, never the value.
- **Forcing `--ignore-scripts`** (D-06/D-07 — the operator rejected it; container is the boundary).
- **A bare `/api/` mux registration** (shadows `/api/integrations/` — T-24-07). Method+path-specific routes only.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| MCP config persistence | a JSON writer | `mcp.SaveManagedConfig` | atomic `0o600`, validation, normalization already correct |
| Secret preservation on edit | a "did it change?" diff | `mergeEnvPreserveCredentials` + `isPlaceholderValue` | the placeholder-vs-real semantics are already exact (config.go:95/145) |
| Secret-key detection | a regex | `secret.IsSecretEnvKey` | one canonical denylist (B-09 fix); divergence re-opens the leak |
| Skill validation checklist | re-parsing frontmatter/blocklist | `skills.ValidateForWrite` | NFKC+casefold+matched-position already implemented (validator.go) |
| Skill install staging + audit | a pending writer | `Writer.WriteInstallPending` | atomic temp+rename + D-29 audit tuple in `db.WithTx` already done |
| Skill activation | a promote+materialize+audit | `Writer.Activate` via `ResumeHandler.Resume` | the no-model-approve resume bridge already exists |
| Append-only audit | app-level "don't update" | the role-grant + dual-trigger migration | DB-enforced tamper-evidence (0021 template) |
| Atomic mutation+audit | manual begin/commit | `db.WithTx` | the proven tx helper |
| Cross-thread approval queue | a new queue | `/api/approvals` + `SubmitAnswers` | the Phase-25 HITL queue + resume token |
| Live MCP tool-count | a new prober | `mcp.ProbeServer` | bounded, redacted, per-row-isolated (Phase 28) |

**Key insight:** ~95% of Phase 29 is wiring HTTP + React over primitives that already exist and are battle-tested (Phase 11/16 shipped, 90.3% coverage). The genuinely new logic is narrow: the `mcp_audit` table+store, the env-edit merge call, the `npx skills` Go transport, and the `governance.write` constant. Treat everything else as a reuse contract.

## Deep-Research UI/UX Recommendations (binding operator mandate — online + D:/tmp)

> The UI-SPEC (`29-UI-SPEC.md`, approved) already executed the visual contract from these same sources and locked it. This section CONFIRMS the mappings against the live D:/tmp components + the 2026 online canon and adds the concrete layout detail the planner needs. Take LAYOUT/INTERACTION only — NO dockable-window/command-palette machinery (deferred `ui_control`).

### D:/tmp curated codebases (read, confirmed)
- **`elysia-frontend/app/components/configuration/SettingKey.tsx`** (read): a setting row = key-input (1/3 width) + value-input (flex-1) + an eye-toggle (`IoMdEye`/`IoMdEyeOff`, **only when `!editable`**) + edit/check/cancel/trash buttons; `type={visible ? "password" : "text"}`; start-non-editable → edit → confirm/cancel. **Aura mapping (UI-SPEC §2, confirmed):** the four-state env row mirrors this layout BUT **drops the eye-reveal for secrets** — Aura never sends the value, so there is nothing to reveal; the field shows the redacted placeholder and "leave untouched = preserved". The non-secret rows are plain mono inputs exactly like elysia's non-protected keys. The edit→confirm/cancel affordance maps to `Save changes`/`Discard changes`.
- **`elysia-frontend/.../WarningCard.tsx`** (read): `border border-warning bg-warning/10 rounded-md` + `IoWarning` (size 20, `text-warning`, `flex-shrink-0`) + a `text-warning` heading + "The following settings need to be configured:" + a `list-disc` of issues. **Aura mapping (UI-SPEC §2 soft-warning, confirmed):** the placeholder/missing-required soft-warning card is this exact shape (the UI-SPEC copies the `border-warning bg-warning/10` token + the bulleted offending-keys list). Aura uses a CSS dot (no `react-icons`) and `--motion-fast` (no framer-motion). **Save still allowed** (informational, never a blocker — the F-2 decision).
- **`elysia-frontend/.../sections/ApiKeysSection.tsx`** + **`EnvImportModal.tsx`** + **`dialog/ConfirmationModal.tsx`** (located, cited by UI-SPEC): the required-key guided affordance, paste-env-with-format-help + disabled-submit-until-nonempty, and the destructive `AlertDialog` with an error-styled non-default action button. **Aura mapping:** the recipe guided-form (RequiredEnv) + the custom-stdio env entry + the `Remove "{{name}}"?` confirm (action-specific labels, destructive button not default-focused).
- **`odysseus/routes/mcp_routes.py` + `static/js/admin.js` + `modalManager.js`** (located, cited by UI-SPEC): confirmed the master-list + modal-confirm admin idiom; vanilla-JS, pattern-level only (too divergent to borrow component shape).

### Online 2026 industrial canon (verified this pass, filling the UI-SPEC's noted gap)
- **Redaction at the source, not the sink** (Microsoft Foundry MCP security guide; lunar.dev MCP secret-management playbook): "log argument shapes by default — keys, types, lengths — never values; opt non-sensitive fields back in explicitly." **→ Validates Aura's key-only `envChips` DTO** (`governance_api.go:209`) — the value never crosses the wire by construction. `[CITED: learn.microsoft.com/azure/foundry/mcp/security-best-practices]`
- **Approval-on-registration + change-management for third-party software** (tyk.io MCP governance; gitguardian MCP governance framework; CSA Singapore AD-2026-003): an admin registers/approves the server before tools are usable; version-pinning + review/approval for adopting third-party software/APIs. **→ Validates the trust-approve gate (custom defaults to `blocked`) + the RISKY skill-install staging → approval queue.** `[CITED: tyk.io/learning-center/mcp-server-governance-best-practices]`
- **Tiered governance: governance burden matches risk; shrink what reaches the human reviewer** (agility-at-scale tiered workflows; latchworkflow approval design): Anthropic field data — operators approved **~93% of Claude Code permission prompts**; "a confirmation dialog interrupts people already committed and asks them to commit again — largely ineffective; spend the human reviewer last, on actions that survive every cheaper filter." **→ Strongly validates the Phase-29 design:** Writer-validation (the 5-item checklist) runs BEFORE the approval queue (the cheap filters first); a single confirm for the recoverable `Remove` (NO type-to-confirm — the UI-SPEC's explicit decision); pending-inert so nothing runs before the human says yes. `[CITED: latchworkflow.com/blog/approval-design-high-risk-operations]`
- **Destructive-action confirmation = focused dialog restating the specific action** (SaaS UI workflow patterns; OWASP MCP cheat sheet): a confirm dialog for irreversible/high-impact actions; action-specific labels. **→ Validates the `Remove "{{name}}"?` dialog with `Remove server`/`Keep server` (not Yes/No), destructive button not default-focused (NN/g).** `[CITED: cheatsheetseries.owasp.org/cheatsheets/MCP_Security_Cheat_Sheet]`

### Mapped to the UI-SPEC component layouts (the planner's checklist)
| UI-SPEC surface | Confirmed pattern source | Concrete layout note for the planner |
|-----------------|--------------------------|--------------------------------------|
| §1 MCP install panel | elysia EnvImportModal + ApiKeysSection; online approval-on-registration | detail-pane slot; 2-segment Recipe\|Custom; recipe → `RequiredEnv` guided form; live CLI-equiv + `ManagedConfigPath()` destination preview labelled `Will write to:`; duplicate-name → inline `aria-invalid` error, Install disabled |
| §2 env-edit (4-state + redacted chip) | elysia SettingKey (minus eye-reveal) + WarningCard | replaces the read-only env `<section>` (McpServerDetail.tsx:48-87); required/optional/missing/placeholder chips (dot+label); redacted placeholder = preserve-on-untouched; soft-warning card (border-warning bg-warning/10) above submit; `Save changes`/`Discard changes` |
| §3 lifecycle (enable/disable/trust/remove + denied-tool) | elysia ConfirmationModal; online destructive-confirm | inline control cluster (NOT a kebab); enable/disable toggle (success dot / muted dot, idempotent); Trust&approve inline form requiring a reason → populates `ApprovedBy/At/Reason`; Remove confirm dialog (action-specific labels, not default-focused); denied tool = explicit `danger` marker, fail-soft per-row probe warning |
| §4 skill install (RISKY-honest) | online supply-chain change-mgmt + tiered-risk; D-09 | detail-pane; source field OR skills.sh search behind the `External discovery` toggle (reflects `AURA_SKILLS_EXTERNAL_DISCOVERY`); pre-activation `<dl>`: source/hash/preview/destination + RISKY badge + the **FIVE** checklist items (NO `--ignore-scripts`) + container-isolation note; submit = `Stage for approval` (never Install/Activate) |
| §5 approval queue entry | online "shrink what reaches the human" + pending-inert | reuse `web/src/approvals/*`; card shows source·hash·preview·risk·resume-token above the existing verbs; NO run/activate affordance; terminal chips for expired/consumed token |
| §6 lifecycle tabs (restore/archive) | online tiered governance; existing four-tab board | active→`Archive skill`, archived→`Restore skill` (collision-reject), pending→no control, audit→no mutate; per-row metadata (cap scope/last used/use count/TTL/hash); audit newest-first |

## D-09 SPEC-Amendment Specifics (BLOCKING — planner lands FIRST)

D-06/D-07 (run `npx skills` WITH scripts in Aura's container; the container IS the isolation boundary; control = approval gate + Writer validation, NOT script-disabling) **deviate from the LOCKED `29-SPEC.md`**. Per CLAUDE.md PRD-first, the planner MUST land a SPEC-amendment commit via gsd tooling BEFORE the skills-install implementation wave (mirroring Phase-28's D-07). discuss-phase stayed non-mutating; the planner makes the edit. The exact LOCKED lines to amend in `29-SPEC.md`:

1. **Requirement SKW-01, "Target" sentence (line 39)** — the validation-checklist enumeration:
   > `(`--ignore-scripts`, sanitized env, `SKILL.md` parse, body cap, injection-literal blocklist, sanitized name/path)` ... `install is always rendered as **RISKY supply-chain input** — `--ignore-scripts` is never presented as safe.`
   **→ Replace with:** the FIVE-item checklist `(sanitized env, `SKILL.md` parse, body cap, injection-literal blocklist, sanitized name/path)`; install isolation = Aura's container boundary; install scripts are permitted; install remains RISKY + gated (approval queue) + Writer-validated.

2. **Requirement SKW-01 acceptance (line 101)** — "six validation-checklist items":
   > `each surface source + content hash/preview + risk tier + the six validation-checklist items + destination`
   **→ Replace** "six" with "five" (drop the `--ignore-scripts` item).

3. **Constraints, "Install is RISKY supply-chain" (line 87)**:
   > `the cockpit always renders skill install (incl. `--ignore-scripts`) as RISKY with the full validation checklist`
   **→ Replace** the parenthetical `(incl. `--ignore-scripts`)` with the container-isolation framing: install runs inside Aura's container (the isolation boundary); install scripts are permitted; the control is the approval gate + Writer validation; install remains RISKY supply-chain input.

4. **Prohibitions table, row #5 (line 147)**:
   > `MUST NOT present an `--ignore-scripts` skill install as "safe" — the cockpit always renders install as RISKY supply-chain input with the full validation checklist`
   **→ Replace with:** MUST NOT present a skill install as "safe" — the cockpit always renders install as RISKY supply-chain input, gated by the approval queue and Writer validation, isolated by Aura's container boundary (install scripts are permitted; the control is NOT `--ignore-scripts`).

5. **Acceptance Criteria checklist item (line 101 of the `## Acceptance Criteria` block)** — "the six validation-checklist items":
   > `surface source + content hash/preview + risk tier + the six validation-checklist items + destination`
   **→ Replace** "six" with "five".

6. **Canon-referral breadcrumb (line 152)** — the closing note references `RISKY+`--ignore-scripts`+gated`:
   > `the reason install is RISKY+`--ignore-scripts`+gated (prohibition #5)`
   **→ Replace** `RISKY+`--ignore-scripts`+gated` with `RISKY + container-isolated + approval-gated + Writer-validated`.

The UI-SPEC (§4, line 162-170) is ALREADY correct for the post-amendment world (it renders the FIVE-item checklist + the container-isolation note explicitly), so no UI-SPEC edit is needed — only the SPEC.

## Recommended Implementation Approach + Sequencing

1. **Wave 0 — SPEC-amendment (BLOCKING, D-09).** Land the six `29-SPEC.md` edits above via gsd tooling as the first commit. Nothing else proceeds for the skills-install path until this is in.
2. **Wave 1 — `mcp_audit` foundation.** `0022_mcp_audit.{up,down}.sql` (mirror `0021` verbatim) + `MCPAuditStore`/`InsertMCPAuditTx` (mirror `internal/skills/audit_store.go`) + the `governance.write` capability const + the `RequireCapability` mount wiring (no routes yet). Pure backend, fully unit+integration testable (live PG append-only assertion).
3. **Wave 2 — MCP write backend + endpoints.** The env-edit path (`mergeEnvPreserveCredentials`-based), the install/trust/enable/disable/remove paths each `db.WithTx(temp-config-write + audit INSERT) → rename`, the `MCPWriteProvider` seam, the `governance_write_api.go` handlers + route registration. Reuse `ProbeServer` for the post-write tool-count. (MCPW-01/02/03.)
4. **Wave 3 — Skills write backend + endpoints.** The `npx skills find/add -y` Go transport (`internal/skills/installer.go`, container-run, ANSI-strip, stage → frontmatter parse → hash → `WriteInstallPending`), the create/update/delete/restore/archive HTTP endpoints (wrap `Writer.*`, restore-collision 409 guard), the catalog-search endpoint behind `AURA_SKILLS_EXTERNAL_DISCOVERY`, the `SkillsWriteProvider` seam. The approval reuses `/api/approvals` (no new code beyond surfacing the install as an `ask_user` pause). (SKW-01/02/03.)
5. **Wave 4 — Frontend (extend in place).** `governanceApi.ts` write fns + DTOs; `McpInstallPanel`/`McpEnvEditForm` + extend `McpServerDetail`/`McpBoard`; `SkillInstallPanel` + extend `SkillsBoard`; extend `web/src/approvals/*` for the skill card; en+it keys in `resources.governance.ts`; new contrast pairs. (cross-cutting + UI-SPEC contract.)
6. **Wave 5 — Validation + gates.** Go ≥85% owned-surface (full tag matrix), Vitest ≥85%, Stryker ≥70%, Playwright e2e + axe + contrast-check, the log-scan-for-secrets assertion, the append-only-rejection integration test.

Per CLAUDE.md "one module per slice, andiamo calmi" — keep each wave a tight commit set; the backend waves (1-3) are independently gateable before the frontend lands.

## Common Pitfalls

### Pitfall 1: Audit-without-apply / apply-without-audit on the file+tx boundary
**What goes wrong:** `SaveManagedConfig` writes a file, the audit row is a SQL INSERT — they are not natively one transaction. A naive "save then insert" can leave a written config with no audit row (or vice versa).
**How to avoid:** temp-write the config, `db.WithTx(INSERT audit)`, then `os.Rename` only on tx success (discard temp on failure). This also gives the concurrency edge its temp+rename.
**Warning sign:** an `mcp_audit` count that disagrees with the `servers.json` state after an induced tx failure.

### Pitfall 2: Restore silently overwrites an active skill
**What goes wrong:** `Writer.Restore` → `promoteDir` → `os.RemoveAll(dst)` clobbers an active skill of the same name. SKW-03 requires rejection.
**How to avoid:** the HTTP restore handler checks `active/<name>` existence and 409s BEFORE calling `Writer.Restore`.
**Warning sign:** an archived restore replacing a live skill with no error.

### Pitfall 3: TRUNCATE bypasses a row trigger
**What goes wrong:** a single `BEFORE UPDATE OR DELETE FOR EACH ROW` trigger does NOT fire for TRUNCATE — the append-only guarantee leaks.
**How to avoid:** copy `0021` exactly — a SEPARATE `BEFORE TRUNCATE FOR EACH STATEMENT` trigger + the role grant (SELECT+INSERT only).
**Warning sign:** a TRUNCATE against `mcp_audit` succeeding in a test.

### Pitfall 4: A secret value crossing the wire (even masked)
**What goes wrong:** sending the env value (masked or not) so the edit form can prefill it.
**How to avoid:** key-only chips (`envChips`); the edit form holds the redacted `${KEY}` placeholder; preserve-on-untouched via `mergeEnvPreserveCredentials`. Log-scan the full install+edit run.
**Warning sign:** any env VALUE in a `/api/governance/*` response body or the DOM.

### Pitfall 5: Bare `/api/` route registration
**What goes wrong:** `mux.Handle("/api/", ...)` shadows `/api/integrations/` (T-24-07).
**How to avoid:** method+path-specific routes only (`POST /api/governance/mcp`, etc.); Go 1.22 longest-pattern precedence keeps siblings authoritative.

### Pitfall 6: `npx skills` ANSI + non-interactivity
**What goes wrong:** the CLI emits ANSI color + may prompt; un-stripped output corrupts the staged body, an un-`-y` invocation hangs.
**How to avoid:** `-y` non-interactive + strip ANSI (spike-proven, `Skill("spike-findings-Aura")`); parse provenance/installs from the body. Run inside Aura's container (scripts permitted, D-06).

## Runtime State Inventory

> Phase 29 is additive (new endpoints/tables/UI), NOT a rename/refactor/migration. No existing runtime state is renamed.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `aura.mcp_audit` is a NEW table (no existing data to migrate); `servers.json` + `~/.aura/skills/*` are written by existing paths the new endpoints reuse | none (additive migration only) |
| Live service config | the MCP install path writes `~/.aura/mcp/servers.json` (the same file the CLI + Phase-16 manager write); no NEW external service config introduced | none — the cockpit writes the same managed file |
| OS-registered state | None — no Task Scheduler / pm2 / launchd registration touched | None |
| Secrets/env vars | NEW server-side flag `AURA_SKILLS_EXTERNAL_DISCOVERY` (D-08) the cockpit toggle reflects; `governance.write` is a `capability_grants` row, not an env var; the seeded `local` identity holds `*` so it passes | document the new flag in the env catalog; grant `governance.write` is implicit via `*` for `local` |
| Build artifacts | the embed `internal/webui/dist` must be rebuilt after the frontend lands (the single-binary `//go:embed` invariant) | rebuild dist before close (existing discipline) |

**Nothing found in OS-registered state:** None — verified by the additive scope (no `cmd/aura` registration code is modified; only new routes + a migration).

## Validation Architecture

> Nyquist validation is enabled (`config.json workflow.nyquist_validation: true`). This section maps every requirement + prohibition + SPEC edge to an observable signal so a VALIDATION.md can be derived.

### Test Framework
| Property | Value |
|----------|-------|
| Go framework | stdlib `testing` + table-driven (golang-testing skill); `httptest` for handlers; live PG via build tag `db_integration`; live MCP probe via the mcp client |
| Go config | none (go test); integration tags `db_integration`, `neo4j_integration` (the latter not needed here) |
| Go quick run | `go test ./internal/agui/ ./internal/mcp/... ./internal/skills/ ./internal/db/...` |
| Go full suite | `go test -tags 'db_integration' -race ./...` + `make coverage` (≥85% owned-surface, live stack) |
| Frontend framework | Vitest (≥85% statements/branches/functions/lines) + Stryker (≥70% killed) + Playwright e2e + axe + `web/scripts/contrast-check.mjs` |
| Frontend quick run | `cd web && npm run test` |
| Frontend full | `cd web && npm run test:coverage && npm run e2e && node scripts/contrast-check.mjs` |

### Phase Requirements → Test Map
| Req | Behavior | Test Type | Automated signal | File status |
|-----|----------|-----------|------------------|-------------|
| MCPW-01 | recipe + custom install writes expected `servers.json`; duplicate rejected; CLI-equiv + destination previewed | Go unit (handler) + integration (FS assert) + Playwright | `go test ./internal/agui -run TestMCPInstall`; e2e asserts preview text + post-install row | ❌ Wave 2 |
| MCPW-01 | interrupted install leaves prior config intact (atomic) | Go integration (induced tx fail → file unchanged) | temp+rename assertion | ❌ Wave 2 |
| MCPW-02 | edit one env preserves other values + unchanged secret; no raw secret on wire/DOM | Go unit (`mergeEnvPreserveCredentials` property) + Playwright DOM scan + **log-scan** | property test: submit `${KEY}` → stored secret unchanged; e2e greps response+DOM | ❌ Wave 2/4 |
| MCPW-02 | four env states render distinct; placeholder-required → soft warning, save still succeeds | Vitest (render) + Playwright | snapshot of the 4 chips; e2e saves with placeholder, asserts warning + persisted blocked | ❌ Wave 4 |
| MCPW-02 | each of install/edit/enable/disable/remove/trust appends exactly one `mcp_audit` row; UPDATE/DELETE rejected | Go integration (live PG) | count==1 per action; `UPDATE mcp_audit` raises `insufficient_privilege` | ❌ Wave 1/2 |
| MCPW-03 | trust-approve populates `ApprovedBy/At/Reason` + audit + flips runnable + tool-count | Go integration + Playwright | assert fields non-empty; probe returns count after approve | ❌ Wave 2 |
| MCPW-03 | denied/destructive tool shown explicitly + absent from registry; fail-soft per-row warning | Go unit (mount policy) + Vitest | denied tool marked, not in mounted set; sibling rows render | ❌ Wave 2/4 |
| SKW-01 | install from source + catalog surfaces source/hash/preview/5-checklist/destination, labelled RISKY | Go unit (transport+gate) + Vitest + Playwright | checklist has 5 items (NO `--ignore-scripts`), RISKY badge present | ❌ Wave 3/4 |
| SKW-01 | over-cap body / blocklist hit fails checklist; empty source rejected | Go unit (`ValidateForWrite`) | body cap+1 → `ErrInvalidStructure`; blocklist → `ErrBlocklisted` w/ position | ❌ Wave 3 |
| SKW-01 | NFKC/compat injection literal still caught; non-ASCII name rejected | Go unit (`violatesBlocklist` fuzz) | fullwidth variant caught; bad name → `ErrInvalidName` | ✅ exists (`validator_fuzz_test.go`) — extend |
| SKW-02 | gated action → approval queue w/ source/preview/risk/token; pending absent from active loader | Go integration + Vitest | `/api/approvals` lists it; loader `List()` excludes pending | ❌ Wave 3/4 |
| SKW-02 | activation only on resume (no model approve); expired/consumed token → terminal, no activation | Go unit (`ResumeHandler` + token reuse) + Vitest | reused token → 404/terminal; no `Activate` without resume | ❌ Wave 3/4 |
| SKW-03 | restore↔archive reflected next fetch; name-collision restore rejected (409) | Go integration | archive→archived, restore→active; colliding restore → 409, active untouched | ❌ Wave 3 |
| SKW-03 | four tabs render correct set + empty states; audit newest-first append-only; no UI mutate | Vitest + Playwright + Go integration | tab snapshots; audit `created_at desc, id desc`; no mutate control | ❌ Wave 3/4 |
| cross-cutting | every new endpoint 401 unauth + 403 without `governance.write`; sanitized error; empty state | Go unit (auth) + Vitest | `RequireCapability` 401/403 (mirror `auth_test.go:494`); error has no stack/secret | ❌ Wave 1-4 |
| cross-cutting | log scan over full MCP+skill install run = no secret; en+it keys present | Go integration (log capture) + Vitest (i18n parity) | grep secret patterns in captured logs = 0; both locales have every key | ❌ Wave 5 |

### Edge Coverage → validation signal (SPEC `## Edge Coverage`, 20/20)
| Edge (category·req) | Signal |
|---------------------|--------|
| boundary·MCPW-01 (probe under/over timeout) | Go: server at sub-3s shows count; >3s → row timed-out, board renders (reuse `handleMCPProbe` isolation test) |
| idempotency·MCPW-01 (duplicate name) | Go handler test: duplicate → reject, no second entry; re-save identical → no-op |
| concurrency·MCPW-01 (interrupted install) | Go integration: induced failure → prior config intact (temp+rename) |
| empty·MCPW-02 (remove last server / clear required) | Go: empty config valid; clear required → soft warning, no crash |
| ordering·MCPW-02 (deterministic by name) | Go: rows sorted post-edit (reuse `SnapshotStatus` sort) |
| idempotency·MCPW-02 (double remove → 404, ≤1 audit) | Go integration: second remove 404, audit count==1 |
| concurrency·MCPW-02 (concurrent env edits) | Go: whole-entry write, no interleave; interrupted leaves valid file |
| unclassified·MCPW-03 (denied tool excluded + fail-soft) | Go unit + Vitest: denied tool marked+absent, sibling rows render |
| boundary·SKW-01 (body cap ±1) | Go: cap+1 fails, at-cap passes (`ValidateForWrite`) |
| empty·SKW-01 (empty source / no search matches) | Go + Vitest: empty source → safe error; no matches → empty state |
| encoding·SKW-01 (NFKC injection / non-ASCII name) | Go fuzz (`validator_fuzz_test.go`, extend) |
| unclassified·SKW-02 (pending inert / token terminal) | Go + Vitest: loader excludes pending; reused token terminal |
| boundary·SKW-03 (skill at TTL → backend-reported tab) | Go: no client TTL recompute; backend stage authoritative |
| adjacency·SKW-03 (colliding restore) | Go integration: 409, no overwrite |
| empty·SKW-03 (each tab empty state) | Vitest: four empty-state snapshots |
| ordering·SKW-03 (audit newest-first stable tiebreak) | Go: `created_at desc, id desc` (reuse `AuditStore.List`) |

### Prohibition → backstop signal (the non-inferable, property/held-out)
- **No raw secret anywhere** → a **held-out log-scan + DOM-grep** over a full MCP-install + env-edit + skill-install run (greps token/DSN/password patterns; expects 0). The property: for ALL env values, the `/api/governance/*` response and DOM contain only `KEY` + `redacted`, never the value.
- **Redacted placeholder preserved, never written as placeholder text** → a **property test**: for any stored secret `S` and submitted `${KEY}`, after env-edit the stored value is exactly `S` (never `"${KEY}"`). Backstop because the "no overwrite" cannot be inferred from a single example — it must hold for every secret key.
- **No model-facing approve / pending non-injectable** → assert no code path activates a skill outside `ResumeHandler.Resume`/CLI; the loader's `List()`/manifest excludes `pending/` (held-out: a pending skill body never appears in any LLM-context-building call).
- **Append-only mcp_audit + skill_audit** → live-PG integration: `UPDATE`/`DELETE`/`TRUNCATE` each raise `insufficient_privilege`.
- **No silent destructive-tool mount when an allowlist exists** → mount-policy unit test: a denied tool is absent from the runtime registry and explicitly surfaced.

### Sampling Rate
- **Per task commit:** `go test ./internal/<touched>/` + `go vet` + `go build` + (frontend tasks) `cd web && npm run test`.
- **Per wave merge:** `go test -tags db_integration -race ./internal/agui/ ./internal/mcp/... ./internal/skills/ ./internal/db/...` + `cd web && npm run test:coverage`.
- **Phase gate:** `make coverage` (≥85% owned-surface, live stack) + `cd web && npm run test:coverage && npm run e2e && node scripts/contrast-check.mjs` all green before `/gsd-verify-work`.

### Wave 0 Gaps (test infra to add)
- [ ] `internal/agui/governance_write_api_test.go` — covers MCPW-01..03 + SKW-01..03 handler behavior (401/403, sanitized error, empty state)
- [ ] `internal/mcp/manager/audit_test.go` + `…/audit_store_integration_test.go` — live-PG append-only + atomic mutation+audit
- [ ] `internal/mcp/manager/envedit_test.go` — the `mergeEnvPreserveCredentials` property (redacted-placeholder preserved)
- [ ] `internal/skills/installer_test.go` — the `npx skills` transport (ANSI strip, stage, frontmatter parse, hash) with a fake `npx`
- [ ] `web/src/governance/__tests__/McpEnvEditForm.test.tsx` + `SkillInstallPanel.test.tsx` — four-state render, RISKY badge, 5-item checklist
- [ ] `web/e2e/governance-write.spec.ts` — the full cockpit install→approve→mounted + env-edit + restore/archive flow + axe
- [ ] add new contrast pairs to `web/scripts/contrast-check.mjs` (`text-warning on surface-2`, etc.)
- [ ] extend `internal/skills/validator_fuzz_test.go` if the 5-item checklist surfacing needs new coverage

## Security Domain

> `security_enforcement` is not explicitly `false` → enabled. SSRF / traversal / at-rest hashing are canon-referred to `/gsd-secure-phase` per the SPEC; this block notes the threat surface, not the mitigation design.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control (in-tree) |
|---------------|---------|-----------------------------|
| V2 Authentication | yes | `RequireAuth` (HMAC cookie / Authula) — Phase 24, unchanged |
| V4 Access Control | yes | `RequireCapability("governance.write")` + `*`-rejection + no-escalation (`HasCapability`) |
| V5 Input Validation | yes | `ValidateForWrite` (skill body), `validateManagedServers` (config), body size-cap (`MaxBytesReader`), `encodeURIComponent` |
| V6 Cryptography | n/a (no new crypto) | secrets stored `0o600`; never hand-rolled |
| V7 Error/Logging | yes | `SanitizeString`/`sanitizeErr`/`RedactSecrets` (no DSN/token/host leak); key-only env DTO |
| V9 Communications | partial | same-origin SameSite=Strict (re-affirm per `auth.go:18` Phase-29 note) |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation | Owner |
|---------|--------|---------------------|-------|
| Secret leak in response/DOM/log | Information Disclosure | key-only chips + `SanitizeString` + log-scan test | this phase |
| Audit tamper / repudiation | Tampering/Repudiation | role grant + dual triggers (0021 pattern) | this phase |
| Model self-approves a mutation | Elevation of Privilege | no model-facing approve; resume-only activation; `RequireCapability` | this phase |
| Prompt injection via pending skill | Tampering | loader scans `active/` only; NFKC blocklist at write | this phase (reuse) |
| SSRF on MCP probe / skill-source fetch | Spoofing/ID | configured-servers-only probe (`probe.go:21`); DISP-04 SSRF guard | **`/gsd-secure-phase`** (canon-referred) |
| Skill name/path traversal | Tampering | `SanitizeName` `^[a-z0-9-]{1,64}$` chokepoint | **`/gsd-secure-phase`** (canon-referred) |
| Authula password at-rest | Info Disclosure | Authula hashing | **Authula + `/gsd-secure-phase`** |
| CSRF on the new write surface | Tampering | SameSite=Strict same-origin (re-affirm; no cross-origin write path) | this phase (affirm) |

## State of the Art

| Old Approach | Current Approach | When | Impact |
|--------------|------------------|------|--------|
| `--ignore-scripts` on every skill install (LOCKED in 29-SPEC) | container-as-isolation: scripts permitted, control = approval gate + Writer validation | 2026-06-20 operator directive (D-06/07/09) | the SPEC-amendment (Wave 0); the UI renders 5 checklist items, not 6 |
| MCP config edit = CLI remove+re-add | in-place cockpit env-edit (whole-entry atomic, credential-preserving) | this phase | reuse `mergeEnvPreserveCredentials`; the universal industrial whole-write shape |
| No MCP config-mutation audit (ecosystem-wide gap) | append-only `mcp_audit` ledger | this phase | forward-aligned with MCP 2.4 enterprise audit logs; reuses Aura's 0021 pattern |
| Skill discovery via CLI/native installer | model path = `npx skills` in sandbox (CLI deleted 11-09); cockpit path = NEW Go `npx skills` transport | this phase | the install transport is genuinely net-new Go |

**Deprecated/outdated:**
- The skills CLI `install`/catalog legs — **deleted** in plan 11-09 (amendment #51 / D-40). Do NOT resurrect; build the cockpit transport fresh.
- The `--ignore-scripts`-as-safe framing — superseded by D-09 (pending the Wave-0 amendment).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `SaveManagedConfig` writes directly (not temp+rename); the concurrency edge wants temp+rename added | Exact-File-Location (MCP) | low — verified by reading the source (`managed_config.go:135` is a direct `WriteFile`); the recommendation stands either way |
| A2 | The shipped recipes have empty `RequiredEnv`, so the guided-form's populated path is exercised only by a future recipe or a test fixture | Exact-File-Location (MCP) | low — the form must still handle the populated case; a test fixture covers it |
| A3 | The skill-install `ask_user` pause surfaces in `/api/approvals` exactly like a model-proposed skill mutation (the queue is pause-source-agnostic) | Approval/HITL | medium — if the cockpit-initiated install needs a distinct pause origin, the planner verifies the `ResumeHandler` dispatch keys on the pause being a skill-approval pause (resume.go is name+token based, so it should be transport-agnostic) |
| A4 | `governance.write` has no naming conflict (only the `auth_test.go:494` reference exists) | Exact-File-Location (auth) | low — grep found only the test reference; lock it |

**Two assumptions (A1/A2) are verified-from-source low-risk; A3 is the one the planner should confirm during planning (the pause-origin dispatch).**

## Open Questions

1. **Atomic config-file + audit-row ordering.**
   - What we know: `db.WithTx` is SQL-only; `SaveManagedConfig` writes a file.
   - What's unclear: the exact temp/rename/tx interleave for true all-or-nothing.
   - Recommendation: temp-write config → `db.WithTx(INSERT audit)` → `os.Rename` on success (discard temp on failure). This also satisfies the MCPW-01 concurrency edge. Lock in Wave 2.

2. **Skill-install pause origin in `/api/approvals`.**
   - What we know: the queue aggregates `ask_user` pauses cross-thread; `ResumeHandler.Resume` is name+token based.
   - What's unclear: whether a cockpit-initiated install creates the pause via the same code path a model-proposed mutation does (so it appears in the queue identically).
   - Recommendation: the install endpoint stages via `WriteInstallPending` (pending, no pause) and ALSO mints the `ask_user` pause that surfaces in the queue; verify the resume dispatch routes a cockpit-origin skill pause to `ResumeHandler`. Confirm in Wave 3 (A3).

3. **`RequiredEnv` population for shipped recipes.**
   - What we know: the field exists but the 4 recipes set it empty/absent.
   - Recommendation: the guided form renders whatever `RequiredEnv` the recipe declares (empty → no required rows); a test fixture recipe with a non-empty `RequiredEnv` drives the soft-warning path. No SPEC change.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| PostgreSQL (`aura.*`) | `mcp_audit` + `skill_audit` ledgers, `db.WithTx` | ✓ (live stack, compose) | 15.x | — (blocking for the audit integration tier) |
| `npx` / Node | the `npx skills` install transport (runs in Aura's container) | ✓ (in the runtime container per D-06) | Node 24 (build image) | — (the cockpit install path requires it; degrade = source-field install of a local path only) |
| `skills` npx CLI | skills.sh discovery+install | ✓ (npx-fetched, `1.5.12`) | 1.5.12 | source-field (owner/repo/URL/path) install without catalog search |
| Live MCP server (for probe) | the post-install/trust tool-count | ✓ (recipe sidecars via compose) | — | the board renders the static row + a fail-soft probe warning if a server is down (Phase-28 isolation) |
| `mcp-neo4j-cypher` | NOT needed for Phase 29 (no graph write) | n/a | — | — |

**Missing dependencies with no fallback:** none for the core path (PG + Node are present in the live stack/container).
**Missing dependencies with fallback:** if external discovery is toggled OFF (`AURA_SKILLS_EXTERNAL_DISCOVERY`), the catalog SEARCH is disabled but source-field install still works.

## Sources

### Primary (HIGH confidence)
- The Aura codebase (read in full this session): `internal/mcp/managed_config.go`, `internal/mcp/redact.go`, `internal/mcp/probe.go`, `internal/mcp/manager/{catalog,config,runtime,status}.go`, `internal/secret/envkey.go`, `cmd/aura/{mcp,mcp_profile,skills,serve_webui}.go`, `internal/agui/{governance_api,governance_seam,auth,auth_test,onboarding_api,approvals_api}.go`, `internal/skills/{writer,writer_activate,validator,resume,stage_reader,audit_store,snippet_usage}.go`, `internal/scoring/scoring.go`, `internal/db/{tx.go,migrations/0021_identity_audit.up.sql}`, `web/src/governance/{governanceApi.ts,McpServerDetail.tsx}`, `web/src/shell/modes.ts`, `web/src/AppShell.tsx` — exact symbols + signatures + line anchors.
- `29-SPEC.md`, `29-CONTEXT.md`, `29-UI-SPEC.md`, `.planning/REQUIREMENTS.md`, `.planning/config.json` — the locked contract.
- `D:/tmp/elysia-frontend/app/components/configuration/{SettingKey,WarningCard}.tsx` (read) + the located `ApiKeysSection/EnvImportModal/ConfirmationModal.tsx` + `D:/tmp/odysseus/routes/mcp_routes.py` — the curated deep-research references.
- `npm view skills …` (executed 2026-06-21) — registry/version/repo/postinstall verification; slopcheck (no flag).

### Secondary (MEDIUM confidence, verified online 2026 canon)
- [Microsoft Foundry MCP security best practices](https://learn.microsoft.com/en-us/azure/foundry/mcp/security-best-practices) — redaction-at-source.
- [tyk.io MCP server governance best practices](https://tyk.io/learning-center/mcp-server-governance-best-practices/) — approval-on-registration.
- [latchworkflow approval design for high-risk operations](https://latchworkflow.com/blog/approval-design-high-risk-operations) — "shrink what reaches the human" + Anthropic 93% data.
- [OWASP MCP Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/MCP_Security_Cheat_Sheet.html) + [agility-at-scale tiered workflows](https://agility-at-scale.com/ai/generative/risk-classification-and-tiered-workflows/) + [CSA Singapore AD-2026-003](https://www.csa.gov.sg/alerts-and-advisories/advisories/ad-2026-003/) — tiered governance + supply-chain change-management.

### Tertiary (LOW confidence)
- None — every claim is sourced to the codebase (HIGH) or verified online/D:/tmp (MEDIUM).

## Metadata

**Confidence breakdown:**
- Exact-file-location (the planner's core need): HIGH — every reuse symbol located, signature quoted, EXISTS/NET-NEW classified, line-anchored in the live tree.
- Standard stack: HIGH — no new deps; the one external tool (`skills` npx) registry-verified + slopcheck-clean.
- Architecture / sequencing: HIGH — the four gap-fillers + the SPEC-amendment-first ordering are derived from the read code, not assumed.
- UI/UX: HIGH — the locked UI-SPEC + the same curated D:/tmp components (read) + verified 2026 online canon all converge.
- Pitfalls / validation: HIGH — each pitfall is grounded in a specific read line (e.g. `promoteDir` RemoveAll, the TRUNCATE trigger, the file+tx boundary).

**Research date:** 2026-06-21
**Valid until:** 2026-07-21 (stable — the backend is shipped Phase-11/16 code; the `skills` CLI version may bump but the transport contract is stable).
