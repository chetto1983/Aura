# Phase 29: Governance Write — MCP Configuration + Skills Install - Pattern Map

**Mapped:** 2026-06-21
**Files analyzed:** 23 (8 Go net-new/modified, 2 SQL net-new, 1 SPEC amend, ~12 frontend net-new/modified)
**Analogs found:** 23 / 23 (every artifact mirrors a live, shipped analog — Phase 29 is ~95% reuse)

> Verified against the live tree this pass. RESEARCH.md's exact-file-location anchors were re-read at the cited `path:line` and the real excerpts pulled below. Every line number is current as of HEAD.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/db/migrations/0022_mcp_audit.{up,down}.sql` | migration | append-only ledger | `internal/db/migrations/0021_identity_audit.{up,down}.sql` | **exact** (mirror verbatim) |
| `internal/mcp/manager/audit.go` (NEW) | store | append-INSERT in tx | `internal/skills/audit_store.go` (`InsertAuditTx`/`List`) | exact (role-match) |
| `internal/mcp/manager/envedit.go` (NEW) | service | transform (load→set→write) | `internal/mcp/manager/config.go` (`ImportProfile`/`mergeEnvPreserveCredentials`) | **exact** (same merge primitive) |
| `internal/agui/governance_write_api.go` (NEW) | controller | request-response (mutation) | `internal/agui/governance_api.go` + `onboarding_api.go` | exact (thin-handler-over-provider) |
| `internal/agui/governance_write_seam.go` (NEW) | provider iface | seam | `internal/agui/governance_seam.go` (`MCPBoardProvider`+setters) | exact (consumer-side narrow seam) |
| `internal/skills/installer.go` (NEW — largest piece) | service | file-I/O + exec transport | `internal/skills/writer.go` (`WriteInstallPending` sink) | role-match (sink exists; `npx` transport net-new) |
| skills HTTP write endpoints (in `governance_write_api.go`) | controller | request-response | `internal/agui/approvals_api.go` (`handleResolveApproval`) + `onboarding_api.go` | exact |
| `cmd/aura/serve_webui.go` (MODIFY — mount + consts) | route wiring | mount discipline | itself: `governance*Route` consts + `RequireCapability` mounts (lines 165-181, 317-332) | **exact** (extend in place) |
| `internal/agui/auth.go` (`governance.write` const consumer) | middleware | auth gate | `internal/agui/auth.go` `RequireCapability` (auth.go:261) | exact (already-built, reuse) |
| `web/src/governance/McpEnvEditForm.tsx` (NEW) | component | four-state form | `web/src/governance/McpServerDetail.tsx:65-87` (env `<section>`) | exact (replaces read section in edit mode) |
| `web/src/governance/McpInstallPanel.tsx` (NEW) | component | guided form | `McpServerDetail.tsx` `<dl>`/`Field` idiom + `BoardLayout` detail slot | role-match |
| `web/src/governance/SkillInstallPanel.tsx` (NEW) | component | RISKY form | `McpServerDetail.tsx` `<dl>`/`Field` + `SkillsBoard.tsx` tabs | role-match |
| `web/src/governance/{McpBoard,SkillsBoard}.tsx` (MODIFY) | component | board+controls | themselves (Phase-28 read boards) | exact (extend in place — D-10) |
| `web/src/governance/governanceApi.ts` (MODIFY) | data layer | write fns | itself: `getJSON` (governanceApi.ts:1-12) | exact (add `postJSON`/`patchJSON`/`deleteJSON`) |
| `web/src/approvals/InlineApprovalCard.tsx` (MODIFY) | component | approval card | itself (Phase-25 HITL card) | exact (extend with source/hash/risk — D-11) |
| `web/src/i18n/resources.governance.ts` (MODIFY) | i18n | en+it bundle | itself (201 lines, governanceEn/governanceIt) | exact (add keys to BOTH) |

---

## Pattern Assignments

### 1. `internal/db/migrations/0022_mcp_audit.{up,down}.sql` (migration, append-only ledger)

**Analog:** `internal/db/migrations/0021_identity_audit.up.sql` (the LATEST shipped migration — `0022` is the correct next slot). **Mirror field-for-field; the `mcp_audit` schema is flat like 0021 (NO D-29 coherence CHECK — that matrix is skill-specific).**

**Full up-migration to mirror** (`0021_identity_audit.up.sql:15-62`):
```sql
CREATE TABLE aura.identity_audit (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at            timestamptz NOT NULL DEFAULT now(),
    actor_identity_id     text        NOT NULL,
    new_identity_id       uuid        NOT NULL,
    new_identity_name     text        NOT NULL,
    granted_capabilities  text[]      NOT NULL,
    authula_user_id       text        NOT NULL
);

CREATE INDEX identity_audit_new_identity_idx ON aura.identity_audit (new_identity_id);
CREATE INDEX identity_audit_created_at_idx   ON aura.identity_audit (created_at DESC);

-- Append-only enforcement function. Raises on any UPDATE/DELETE/TRUNCATE.
CREATE FUNCTION aura.reject_identity_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'aura.identity_audit is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

-- Row trigger for UPDATE/DELETE (fires FOR EACH ROW).
CREATE TRIGGER identity_audit_no_update_delete
    BEFORE UPDATE OR DELETE ON aura.identity_audit
    FOR EACH ROW EXECUTE FUNCTION aura.reject_identity_audit_mutation();

-- SEPARATE statement trigger for TRUNCATE (Pitfall 1: a row trigger NEVER fires
-- for TRUNCATE — it must be a FOR EACH STATEMENT trigger on the TRUNCATE event).
CREATE TRIGGER identity_audit_no_truncate
    BEFORE TRUNCATE ON aura.identity_audit
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_identity_audit_mutation();

-- Role separation: aura_app gets SELECT + INSERT ONLY. aura_migrate owns DDL.
GRANT SELECT, INSERT ON aura.identity_audit TO aura_app;
GRANT ALL              ON aura.identity_audit TO aura_migrate;
```

**Down-migration to mirror** (`0021_identity_audit.down.sql:8-11`):
```sql
DROP TRIGGER IF EXISTS identity_audit_no_truncate      ON aura.identity_audit;
DROP TRIGGER IF EXISTS identity_audit_no_update_delete ON aura.identity_audit;
DROP TABLE   IF EXISTS aura.identity_audit;
DROP FUNCTION IF EXISTS aura.reject_identity_audit_mutation();
```

**Adaptation for `0022_mcp_audit`** (D-02/D-03; RESEARCH §Migration precedent):
- Table `aura.mcp_audit`. Columns: `id uuid PK DEFAULT gen_random_uuid()`, `created_at timestamptz NOT NULL DEFAULT now()`, `actor_identity_id text NOT NULL` (**D-03: the capability-layer principal `principalFrom`, NOT the raw Authula user id** — same choice as 0021), `action text NOT NULL` (one of `install`/`edit`/`enable`/`disable`/`remove`/`trust`), `server_name text NOT NULL`, `reason text` (**nullable — NULL except on `trust`**).
- Rename the function → `aura.reject_mcp_audit_mutation()`, the triggers → `mcp_audit_no_update_delete` / `mcp_audit_no_truncate`. Keep the **two separate triggers** (Pitfall 3 — TRUNCATE bypasses a row trigger). Keep the `GRANT SELECT, INSERT … TO aura_app` / `GRANT ALL … TO aura_migrate` exactly.
- Indexes: `mcp_audit_server_idx (server_name)` + `mcp_audit_created_at_idx (created_at DESC)` (newest-first reads, like 0021). Plain (non-CONCURRENT) — Pitfall 6.

---

### 2. `internal/mcp/manager/audit.go` (store, append-INSERT in tx) — NEW

**Analog:** `internal/skills/audit_store.go` (`AuditInsert`/`InsertAuditTx`/`List`). Mirror the tx-bound insert so it participates in the surrounding `db.WithTx` (D-04).

**Insert-in-tx pattern** (`audit_store.go:147-159`):
```go
// InsertAuditTx appends one audit row using a tx-bound Queries (the writer's
// db.WithTx closure passes it), so the INSERT participates in the surrounding
// atomic write.
func InsertAuditTx(ctx context.Context, q *sqlc.Queries, in AuditInsert) error {
	params, err := in.toParams()
	if err != nil {
		return fmt.Errorf("insert skill audit (tx): %w", err)
	}
	if _, err := q.InsertSkillAudit(ctx, params); err != nil {
		return fmt.Errorf("insert skill audit %q (tx): %w", in.SkillName, classifyAuditErr(err))
	}
	return nil
}
```

**Newest-first list pattern** (`audit_store.go:170-189`): `List(ctx, AuditFilter)` clamps `limit<=0 → 100`, `limit>MaxInt32 → MaxInt32`, builds `pgtype` optionals, calls the sqlc `ListSkillAudit` ordered `created_at DESC`.

**Adaptation:** `MCPAuditInsert{ActorIdentityID, Action, ServerName, Reason string}` + `InsertMCPAuditTx(ctx, q *sqlc.Queries, in MCPAuditInsert) error` + `(s *MCPAuditStore) List(ctx, filter) ([]MCPAuditRow, error)` (newest-first). Needs new sqlc queries (`InsertMcpAudit`, `ListMcpAudit`) on the `0022` table. The skills D-29 coherence-CHECK machinery (`toParams` NULL-mapping for `ApprovalSource`/`PausedStateToken`, `classifyAuditErr`) is NOT needed — the flat 0022 schema only has the append-only triggers, so a simpler `InsertMcpAuditParams` suffices.

---

### 3. `internal/mcp/manager/envedit.go` (service, transform) — NEW (the one MCP backend gap-filler)

**Analog:** `internal/mcp/manager/config.go` — `ImportProfile` (config.go:47) calls `mergeEnvPreserveCredentials` (config.go:95) which is **the exact D-05 substrate**.

**Credential-preserve merge** (`config.go:95-135`, the load-bearing logic):
```go
func mergeEnvPreserveCredentials(existing, incoming []string) []string {
	existingByKey := map[string]string{}
	// ...build existingByKey + existingOrder...
	for _, entry := range incoming {
		key, _, ok := cutEnv(entry)
		// ...
		// Preserve an EXISTING real credential against any incoming override: the merge
		// must never clobber a configured secret. But when the existing value is itself a
		// placeholder, the incoming value wins so a real credential replaces it.
		if prior, ok := existingByKey[key]; ok && secret.IsSecretEnvKey(key) && !isPlaceholderValue(key, prior) {
			out = append(out, prior)   // ← the unchanged-secret-preserved path
			continue
		}
		out = append(out, entry)
	}
	// ...append existing keys not present in incoming...
}
```

**Placeholder predicate** (`config.go:145-147`) — drives the four-state UI + the "submitted `${KEY}` = unchanged" semantics:
```go
func isPlaceholderValue(key, value string) bool {
	return value == "${"+key+"}" || value == ""
}
```

**The whole-entry atomic write sink** (`internal/mcp/managed_config.go:122-140`) — `SaveManagedConfig` validates, MkdirAll `0o700`, `MarshalIndent`, `os.WriteFile(path, data, 0o600)`, `os.Chmod 0o600`. **RESEARCH note (A1, verified):** this is a DIRECT `WriteFile`, NOT temp+rename. The MCPW-01 concurrency edge ("interrupted install leaves prior config intact") + the D-04 atomic-mutation-with-audit ordering both want a **temp-write → `db.WithTx(audit INSERT)` → `os.Rename`** wrapper (see Shared Pattern: atomic mutation+audit).

**Adaptation:** `func SetServerEnv(doc *mcp.ManagedConfig, name string, submitted []string) error` that does load→merge one server's `Env` via `mergeEnvPreserveCredentials(existing.Env, submitted)`→re-set the whole entry. The simplest reuse is `ImportProfile(base, singleServerIncomingDoc, ImportOptions{OverwriteCredentials:false})` (config.go:47) which already runs exactly this merge per server. `mergeEnvPreserveCredentials`/`isPlaceholderValue`/`cutEnv` are **unexported in package `manager`** — the env-edit path lives in that same package, so it can call them directly (no export needed). RedactEnv (config.go:78) is the outbound side (already wired into the board via `envChips`).

---

### 4. `internal/agui/governance_write_api.go` (controller, request-response) — NEW

**Analog:** `internal/agui/governance_api.go` (the thin-handler-over-provider read shape) + `internal/agui/onboarding_api.go` (the mutation body-decode + size-cap + validate + one-service-call shape) + `internal/agui/approvals_api.go` (the path-token-guard + verb-map shape).

**Thin read-handler shape to mirror** (`governance_api.go:158-183`, `handleMCPList`):
```go
func (s *Server) handleMCPList(w http.ResponseWriter, _ *http.Request) {
	if s.governance.MCP == nil {
		http.Error(w, "mcp board not configured", http.StatusServiceUnavailable) // 503 when unwired
		return
	}
	doc := s.governance.MCP.Servers()
	statuses := mcpmanager.SnapshotStatus(doc)
	rows := make([]mcpServerRow, 0, len(statuses))
	for _, st := range statuses {
		server := doc.MCPServers[st.Name]
		rows = append(rows, mcpServerRow{
			Name:    st.Name,
			// ...
			EnvKeys:   envChips(server.Env),                  // ← value NEVER serialized
			LastError: mcp.RedactSecrets(st.LastError),
		})
	}
	writeJSON(w, map[string]any{"servers": rows})
}
```

**Key-only env projection (the no-leak belt — reuse as-is)** (`governance_api.go:209-222`, `envChips`): projects `KEY=VALUE` → `{key, redacted: secret.IsSecretEnvKey(key)}`, **dropping the value entirely**. The write handlers MUST reuse this for every env-bearing response (Pitfall 4 — no secret value on the wire/DOM).

**Mutation handler shape to mirror** (`onboarding_api.go:207-224`, `prepareOnboardingMutation`): nil-check provider (503) → `r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)` → decode+validate (sanitized 400) → read principal via `principalIdentityID(r)` (401 if absent) → one service call. The generic `handleOnboardingMutation[Req,Resp]` (onboarding_api.go:182) is the exact decode→validate→call→writeJSON skeleton.

**Path-token guard + verb-map shape** (`approvals_api.go:114-143`, `handleResolveApproval`): `uuid.Parse`-guard the path value to a clean 404 BEFORE any backend round-trip, map the verb to a closed set (reject others with 400), one provider call, `sanitizeErr` on error.

**Route registration to mirror** (`governance_api.go:144-151`, `registerGovernanceRoutes`) — SPECIFIC method+path siblings, never bare `/api/`:
```go
func (s *Server) registerGovernanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/governance/mcp", s.handleMCPList)
	mux.HandleFunc("GET /api/governance/mcp/{name}/probe", s.handleMCPProbe)
	// ...
}
```

**Adaptation — add a `registerGovernanceWriteRoutes(mux)` (D-01 named-action sub-paths):**
```
POST   /api/governance/mcp                      (install recipe/custom-stdio)
PATCH  /api/governance/mcp/{name}/env           (in-place env edit)
POST   /api/governance/mcp/{name}/trust         (trust-approve, reason in body)
POST   /api/governance/mcp/{name}/enable
POST   /api/governance/mcp/{name}/disable
DELETE /api/governance/mcp/{name}               (remove, confirmation client-side)
POST   /api/governance/skills/install
POST   /api/governance/skills/{name}/restore
POST   /api/governance/skills/{name}/archive
GET    /api/governance/skills/catalog?q=…       (skills.sh search, behind the toggle)
(+ create/update/delete per the SPEC skills gap-filler)
```
Each handler: nil-check the write provider (503), size-cap+decode+sanitized-validate, read `principalFrom`/`principalIdentityID`, ONE provider call, key-only env projection on any echo, `sanitizeErr` on failure. The post-install/post-trust live tool-count reuses `ProbeServer` via the existing `MCPBoardProvider.Probe` (governance_api.go:240, 3s bounded, per-row).

---

### 5. `internal/agui/governance_write_seam.go` (provider interface) — NEW

**Analog:** `internal/agui/governance_seam.go` — the consumer-side narrow interfaces + off-constructor setters (D-A2-02).

**Provider iface + bundle + setter pattern** (`governance_seam.go:28-31`, `:53-57`, `:101`):
```go
type MCPBoardProvider interface {
	Servers() mcp.ManagedConfig
	Probe(ctx context.Context, name string, server mcp.ManagedServer) mcp.ProbeResult
}

type GovernanceProviders struct {
	MCP       MCPBoardProvider
	Skills    SkillsBoardProvider
	Scheduler SchedulerBoardProvider
}

func (s *Server) SetGovernanceProviders(p GovernanceProviders) { s.governance = p }
```

**Adaptation:** declare `MCPWriteProvider` (`InstallServer`, `SetServerEnv`, `TrustApprove`, `SetEnabled`, `RemoveServer` — each returning after `db.WithTx(write+audit)`) and `SkillsWriteProvider` (`Install`, `Restore`, `Archive`, plus `CreateUpdateDelete` wrapping `Writer.WriteMutationByName`) + a `GovernanceWriteProviders` bundle + `SetGovernanceWriteProviders`. Declared HERE (consumer-side) so each handler depends only on the methods it calls; wired at the daemon composition root after `NewServer`. A nil field → its routes answer 503 (exactly as the read handlers nil-check).

---

### 6. `internal/skills/installer.go` (service, file-I/O + exec transport) — NEW (the largest net-new piece)

**Analog (the sink):** `internal/skills/writer.go` — `WriteInstallPending` (writer.go:179) is the pending+audit landing the transport hands off to. **The transport itself (running `npx skills`) is genuinely net-new** — plan 11-09 deleted the prior native installer (RESEARCH: `internal/agent/tools/skill.go:25` removal).

**The pending+audit sink to hand off to** (`writer.go:179-225`, `WriteInstallPending`):
```go
func (w *Writer) WriteInstallPending(ctx context.Context, fm Frontmatter, body, stagedDir, hash string, actor AuditActor) (string, error) {
	// ...MkdirAll pending root, copy staged tree symlink-stripped into a temp dir, rename into pending/<name>/...
	if err := db.WithTx(ctx, w.pool, func(q *sqlc.Queries) error {
		return InsertAuditTx(ctx, q, AuditInsert{
			ActorID:         actor.ActorID,
			IdentityID:      actor.IdentityID,
			SkillName:       fm.Name,
			Action:          AuditInstall,
			ContentHash:     hash,
			ApprovalSource:  ApprovalNone,
			GateRecommended: true,   // install is always gated…
			GateTaken:       false,  // …and NEVER self-activates (D-03)
		})
	}); err != nil {
		return "", fmt.Errorf("install pending %q: audit: %w", fm.Name, err)
	}
	return StatusPendingApproval, nil   // ← pending, inert, awaiting the approval resume
}
```

**The validation chokepoint the transport routes through** (`internal/skills/validator.go:82-106`, `ValidateForWrite`) — the FIVE-item checklist the UI surfaces (sanitized name/path via `SanitizeName` `^[a-z0-9-]{1,64}$`, description len, body cap, type enum, NFKC-normalized+case-folded injection blocklist with matched-byte-position):
```go
func ValidateForWrite(fm Frontmatter, body string, blocklist []string, bodyCapBytes int, allowBlocklisted bool) error {
	if err := SanitizeName(fm.Name, fm.Name); err != nil { return err }
	if len(fm.Description) > maxSkillDescriptionLen { return fmt.Errorf("%w: ...", ErrInvalidStructure) }
	if bodyCapBytes > 0 && len(body) > bodyCapBytes { return fmt.Errorf("%w: body %d bytes exceeds cap %d", ErrInvalidStructure, ...) }
	if fm.Type != TypeInstruction && fm.Type != TypeSnippet { return fmt.Errorf("%w: ...", ErrInvalidStructure) }
	if allowBlocklisted { return nil }
	if matched, pos, ok := violatesBlocklist(body, blocklist); ok {
		return fmt.Errorf("%w: matched %q at byte %d (NFKC-normalized)", ErrBlocklisted, matched, pos)
	}
	return nil
}
```

**The risk tier the UI badge reads** (`internal/scoring/scoring.go:126-140`):
```go
func ComputeSkillTier(action SkillAction, body string) RiskTier {
	switch action {
	case SkillDelete: return Destructive
	case SkillCreate, SkillUpdate, SkillInstall: return Risky   // install → always Risky
	default: return Risky
	}
}
func GateRecommended(t RiskTier) bool { return t == Risky || t == Destructive }
```

**Adaptation (net-new Go transport):** `func (i *Installer) Install(ctx, source string) (...)` runs `exec.CommandContext(ctx, "npx", "skills", "add", source, "-y")` (and `find <q>` for catalog search) **inside Aura's container — scripts PERMITTED, NO `--ignore-scripts`** (D-06/D-07; the container IS the blast boundary — [[feedback_container_is_isolation_not_ignore_scripts]]). Strip ANSI (spike-proven, `Skill("spike-findings-Aura")`), stage the fetched tree to a temp dir, parse its `SKILL.md` frontmatter, compute the canonical hash (`HashSkillFiles`/`HashSkillDir`, contenthash.go), run `ValidateForWrite`, then hand off to `WriteInstallPending`. **Pitfall 6:** `-y` non-interactive + ANSI-strip or the staged body corrupts / the call hangs.

---

### 7. Skills lifecycle endpoints — `Activate` / `Restore` / `Archive` reuse (in the write provider)

**Analog:** `internal/skills/writer_activate.go` + `internal/skills/resume.go`.

**The no-model-approve activation bridge** (`resume.go:48-57`, `ResumeHandler.Resume`) — **this is the ONLY activation path; reuse verbatim, do NOT mint a second one** (D-11):
```go
func (h *ResumeHandler) Resume(ctx context.Context, action, name, pausedToken string, actor AuditActor) error {
	switch action {
	case ResumeAccept:           return h.writer.Activate(ctx, name, ApprovalAskUser, pausedToken, actor)
	case ResumeDecline, ResumeCancel: return h.writer.DiscardPending(ctx, name, ApprovalAskUser, pausedToken, actor)
	default:                     return fmt.Errorf("skill resume %q: unknown action %q", name, action)
	}
}
```
The `/api/approvals` resolve (`approvals_api.go:134`) → `Runner.SubmitAnswers` → (skill-approval pause) → this handler. The cockpit install endpoint stages via `WriteInstallPending` (pending) AND mints the `ask_user` pause that surfaces in the cross-thread queue (RESEARCH Open Q2 / A3 — confirm the pause-origin dispatch in Wave 3).

**Restore collision LANDMINE** (`writer_activate.go:112-138`, `Restore`) — `Restore` → `promoteDir` → `os.RemoveAll(dst)` would **silently overwrite an active skill of the same name**. SKW-03 requires REJECTION:
```go
func (w *Writer) Restore(ctx context.Context, name string, src ApprovalSource, actor AuditActor) error {
	if err := SanitizeName(name, name); err != nil { return ... }
	srcDir := filepath.Join(w.archiveDir, name)
	dstDir := filepath.Join(w.activeDir, name)
	if err := promoteDir(srcDir, dstDir); err != nil { ... }  // ← promoteDir does os.RemoveAll(dst) first!
	// ...materialize, SetUsageStatus("active"), auditActivationLike(AuditActivate)...
}
```
**Adaptation (Pitfall 2):** the HTTP restore handler MUST `stat active/<name>` and return **409** BEFORE calling `Writer.Restore`. (Note the action-constant nuance: restore audits as `AuditActivate`/`ApprovalCLI`, NOT a `restore` action — the 0010 action CHECK does not list `restore`.)

**Archive** (`writer_activate.go:68`): de-materializes + moves active→archived + one audit row in `db.WithTx`. Wrap directly.

---

### 8. `cmd/aura/serve_webui.go` (route + capability mount) — MODIFY in place

**Analog:** the file itself — the Phase-28 governance read mounts + the route/capability const blocks.

**Capability const to mirror** (`serve_webui.go:101-104`):
```go
// governanceReadCapability gates the Phase-28 governance board read surface.
const governanceReadCapability = "governance.read"
```

**Route consts to mirror** (`serve_webui.go:165-172`):
```go
const (
	governanceMCPListRoute     = "GET /api/governance/mcp"
	governanceMCPProbeRoute    = "GET /api/governance/mcp/{name}/probe"
	// ...
)
```

**Capability-gated mount to mirror** (`serve_webui.go:317-322` reads, `:331-332` create-mutation):
```go
mux.Handle(governanceMCPListRoute, agui.RequireCapability(aguiHandler, auth, governanceReadCapability))
// ...
mux.Handle(onboardingStartRoute,   agui.RequireCapability(aguiHandler, auth, identityCreateCapability))
mux.Handle(onboardingProvisionRoute, agui.RequireCapability(aguiHandler, auth, identityCreateCapability))
```

**Adaptation (D-discretion `governance.write`):** add `const governanceWriteCapability = "governance.write"` (parity with `agentRunCapability`/`identityCreateCapability`, serve_webui.go:99/181) + the new `governance*WriteRoute` method+path consts, and mount EACH new mutating route `mux.Handle(<METHOD PATH>, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))`. **Pitfall 5:** method+path-specific only — never a bare `/api/` (shadows `/api/integrations/`, T-24-07); Go 1.22 longest-pattern precedence keeps them authoritative.

---

### Frontend `web/src/governance/*` (extend Phase-28 in place — D-10)

**`McpEnvEditForm.tsx` (NEW)** — **Analog:** `McpServerDetail.tsx:65-87`, the env-keys read `<section>` it replaces in edit mode:
```tsx
<section className="flex flex-col gap-1">
  <h4 className="text-[13px] font-semibold uppercase tracking-wide text-text-muted">
    {t('governance.mcp.detail.envKeys')}
  </h4>
  {server.envKeys.length > 0 ? (
    <ul className="flex flex-wrap gap-1">
      {server.envKeys.map((chip) => (
        <li key={chip.key} className="... font-mono text-[13px] text-text-muted">
          <span className="break-all text-text">{chip.key}</span>
          {chip.redacted ? <span className="text-text-muted">· {t('governance.mcp.redacted')}</span> : null}
        </li>
      ))}
    </ul>
  ) : (...)}
</section>
```
**Adaptation:** render four-state chips (required/optional/missing/placeholder — dot+label), each row a mono input holding the **redacted `${KEY}` placeholder** for secrets (leave-untouched = preserved; NO eye-reveal — Aura never sends the value, per RESEARCH §elysia SettingKey-minus-eye mapping). Soft-warning card (`border-warning bg-warning/10`) above submit for a still-placeholder required var; **save still allowed** (F-2). `Save changes`/`Discard changes`.

**`McpInstallPanel.tsx` / `SkillInstallPanel.tsx` (NEW)** — **Analog:** the `McpServerDetail.tsx:18-25` `Field`/`<dl>` idiom in the `BoardLayout` detail slot. Install panel: 2-segment Recipe|Custom; recipe → `RequiredEnv` guided form (the `CatalogEntry.RequiredEnv` field, catalog.go:93 — empty for the 4 shipped recipes per A2, but the form must handle the populated case); live CLI-equiv + `ManagedConfigPath()` destination preview labelled `Will write to:`; duplicate-name → inline `aria-invalid` error, Install disabled. Skill panel: source field OR skills.sh search behind the `External discovery` toggle (reflects `AURA_SKILLS_EXTERNAL_DISCOVERY`); pre-activation `<dl>` of source/hash/preview/destination + RISKY badge + the **FIVE** checklist items (NO `--ignore-scripts` — D-09) + container-isolation note; submit = `Stage for approval`.

**`governanceApi.ts` (MODIFY)** — **Analog:** itself (governanceApi.ts:1-12 comment line 10 explicitly: *"there is no postJSON here"*). Add `postJSON`/`patchJSON`/`deleteJSON` mirroring `getJSON` (`credentials:'same-origin'`, `retry:false`, non-200-incl-401 THROWS, `encodeURIComponent` on `{name}`) + the write DTOs (install request, env-edit request, trust request, skill-install request, catalog-search response). Board write hooks via `@tanstack/react-query` `useMutation` + `invalidateQueries`.

**`McpBoard.tsx` / `SkillsBoard.tsx` (MODIFY)** — **Analog:** themselves. McpBoard (McpBoard.tsx:1-15) keeps the per-row probe isolation; add the install-panel entry + the inline lifecycle control cluster (enable/disable toggle, Trust&approve inline form, Remove confirm). SkillsBoard (SkillsBoard.tsx:23-31, the four-tab `tablist`) adds active→`Archive`, archived→`Restore` (collision-reject), source-install entry; pending/audit stay mutation-free.

**`web/src/approvals/InlineApprovalCard.tsx` (MODIFY)** — **Analog:** itself (Phase-25 HITL card, InlineApprovalCard.tsx:40). Extend the card to show source·hash·preview·risk-tier·resume-token above the EXISTING three verbs (Answer/Decline/Cancel); NO run/activate affordance; terminal chip for expired/consumed token (the card already renders terminal state, InlineApprovalCard.tsx:24-25). Do NOT mint a second badge/queue (D-11).

**`web/src/i18n/resources.governance.ts` (MODIFY)** — add every new key to BOTH `governanceEn` and `governanceIt` (cross-cutting AC). New contrast pairs (e.g. `text-warning on surface-2`) → `web/scripts/contrast-check.mjs` (WCAG-AA).

---

## Shared Patterns

### Atomic mutation + audit (D-04) — applies to ALL six MCP write verbs
**Source:** `internal/skills/writer.go:210` (`WriteInstallPending`'s tx) + `internal/db/tx.go:22` (`db.WithTx`). **Critical caveat (Pitfall 1):** `SaveManagedConfig` writes a FILE, the audit row is a SQL INSERT — not natively one tx. RESEARCH's locked recommendation:
```go
tmp := writeConfigTemp(path, nextDoc)              // reconcilable FS (also gives the temp+rename the MCPW-01 concurrency edge wants)
err := db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
    return InsertMCPAuditTx(ctx, q, MCPAuditInsert{
        ActorIdentityID: principal, Action: "trust", ServerName: name, Reason: reason,
    })
})
if err != nil { os.Remove(tmp); return err }       // no applied-but-unaudited
return os.Rename(tmp, path)                          // commit the config atomically
```
**Apply to:** install / env-edit / enable / disable / remove / trust — each appends exactly ONE `mcp_audit` row (D-01: one named action = one audit row).

### Capability gate (`governance.write`) — applies to ALL new mutating endpoints
**Source:** `internal/agui/auth.go:261` (`RequireCapability`). **Already-built; reuse — the const is the only new code.** The `governance.write` string is **already referenced in `internal/agui/auth_test.go:494`** (asserts 403 when ungranted — verified):
```go
RequireCapability(next, deps, "governance.write").ServeHTTP(rec, req) // not granted → expects 403
```
```go
func RequireCapability(next http.Handler, deps AuthDeps, capability string) http.Handler {
	// ...
	identityID := principalFrom(r.Context())
	if identityID == "" { http.Error(w, "forbidden", http.StatusForbidden); return }
	ok, err := deps.Identities.HasCapability(r.Context(), identityID, capability)
	if err != nil || !ok { http.Error(w, "forbidden", http.StatusForbidden); return }
	next.ServeHTTP(w, r)
}
```
**Apply to:** every new `/api/governance/{mcp,skills}/*` mutating route. The seeded `local` identity holds `*` so it passes; no operator can grant a capability they lack (`HasCapability` + `*`-rejection invariant). RequireAuth (auth.go:194) provides the whole-origin 401 gate; this is the 403 layer.

### Secrets-never-leaked belt — applies to every env-bearing response + the install/edit log path
**Source:** `internal/agui/governance_api.go:209` (`envChips`, key-only) + `internal/mcp/manager/config.go:78` (`RedactEnv`, outbound `${KEY}`) + `internal/secret/envkey.go:69` (`secret.IsSecretEnvKey`, the one canonical denylist — B-09) + `SanitizeString`/`sanitizeErr`/`mcp.RedactSecrets`. **Apply to:** any `/api/governance/*` response that touches env, plus the log-scan backstop (Pitfall 4: no env VALUE in any response body, DOM, or log). The redacted-placeholder-preserved property (Shared Pattern #3 / `mergeEnvPreserveCredentials`) is the write-side half.

### Trust-approve populates the today-empty fields (D-12)
**Source:** `internal/mcp/managed_config.go:65-70` (`ManagedTrust`):
```go
type ManagedTrust struct {
	Class      string `json:"class,omitempty"`
	ApprovedBy string `json:"approvedBy,omitempty"`   // ← never populated today (mcpTrust sets only Class)
	ApprovedAt string `json:"approvedAt,omitempty"`   // ← "
	Reason     string `json:"reason,omitempty"`       // ← "
}
```
Trust classes (`managed_config.go:21-25`): `TrustTrustedRecipe`/`TrustTrustedLocal`/`TrustSandboxedLocal`/`TrustRemoteHTTP`/`TrustBlocked` (custom defaults to `TrustBlocked`). **Apply to:** the `POST /api/governance/mcp/{name}/trust` handler — set `Trust = ManagedTrust{Class: …, ApprovedBy: principal, ApprovedAt: now, Reason: body.reason}`, `SaveManagedConfig` (via the atomic-write wrapper) + an `mcp_audit` row with `action="trust"` + `reason`, flip runnable, re-probe for tool-count.

---

## No Analog Found

**None.** Every Phase-29 artifact mirrors a live, shipped analog. The closest things to "no analog":

| File | Role | Data Flow | Note |
|------|------|-----------|------|
| `internal/skills/installer.go` (the `npx skills` exec transport) | service | exec + file-I/O | The **sink** (`WriteInstallPending`) + the **validation** (`ValidateForWrite`) + the **hash** (`HashSkillDir`) all exist; only the `exec.CommandContext("npx","skills",…)` + ANSI-strip + stage glue is net-new (the prior native installer was deleted in 11-09). Pattern source for exec/ANSI: `Skill("spike-findings-Aura")`. |
| `aura.mcp_audit` (the table) | migration | — | First MCP config-mutation ledger (ecosystem-wide gap per RESEARCH), but it mirrors `0021` verbatim — so the PATTERN exists even though the TABLE is new. |

---

## Metadata

**Analog search scope:** `internal/db/migrations/`, `internal/mcp/` + `internal/mcp/manager/`, `internal/skills/`, `internal/scoring/`, `internal/secret/`, `internal/agui/`, `cmd/aura/`, `web/src/governance/`, `web/src/approvals/`, `web/src/i18n/`.
**Files scanned:** 23 (re-read at RESEARCH's cited `path:line` anchors and confirmed against the live tree).
**Key cross-cutting facts for the planner:**
- **D-09 SPEC-amendment is BLOCKING Wave 0** — `29-SPEC.md` lines 39/87/101/147/152 name `--ignore-scripts`; replace with container-isolation framing (5-item checklist) BEFORE the skills-install wave, via gsd tooling (mirror Phase-28 D-07). Not a pattern artifact, but it gates the install code.
- **`SaveManagedConfig` is a direct `WriteFile` today** (A1, verified at managed_config.go:135) — the temp+rename wrapper is both the D-04 atomicity mechanism AND the MCPW-01 concurrency-edge fix; one change serves both.
- **Restore silently overwrites** (Pitfall 2, writer_activate.go:126 `promoteDir`→`os.RemoveAll`) — the 409 collision guard is HTTP-handler-side, not in `Writer.Restore`.
- **`mergeEnvPreserveCredentials`/`isPlaceholderValue` are unexported in `package manager`** — the env-edit path must live in that package (or call `ImportProfile`, which is exported and runs the same merge).
**Pattern extraction date:** 2026-06-21
