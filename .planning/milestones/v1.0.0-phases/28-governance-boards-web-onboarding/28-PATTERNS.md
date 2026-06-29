# Phase 28: Governance Boards + Web Onboarding - Pattern Map

**Mapped:** 2026-06-20
**Files analyzed:** 24 (new/modified across backend Go, sqlc/migrations, and React/i18n)
**Analogs found:** 23 / 24 (1 genuinely-new: the onboarding session store, modeled on a composite)

> **Method note:** This is a PURE INTEGRATION phase. Every analog below was read at file:line this session and the excerpt is verbatim from the shipped code. RESEARCH already nailed most analogs — this map **confirms + extracts the copy-from excerpts** so the planner can write exact `<read_first>`/`<action>` blocks. Where the 28-UI-SPEC §Component Inventory already pins a frontend file→analog, this map reuses that pin and adds the concrete excerpt. No source was modified.

---

## File Classification

### Backend (Go)

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/agui/governance_api.go` (NEW) | controller (REST handler) | request-response (read) | `internal/agui/graph_api.go` + `conversations_api.go` | exact |
| `internal/agui/onboarding_api.go` (NEW) | controller (REST handler) | request-response (read + create-mutation) | `internal/agui/graph_api.go` (read) + `server.go` handleRun (mutation) | exact |
| `internal/agui/onboarding_session.go` (NEW — server-held session store) | service (TTL session store) | request-response (stateful) | `internal/skills/loader.go` (goroutine-free TTL cache) + `onboarding.Session` | role-match (composite) |
| `internal/agui/server.go` (MODIFY — add `SetGovernanceProviders`/`SetOnboardingService` + `register*Routes` in `Mux()`) | config (DI seam + route registration) | n/a | `internal/agui/server.go` `SetGraphView` + `registerGraphRoutes` (self-precedent) | exact |
| `cmd/aura/serve_webui.go` (MODIFY — route consts + parent-mux mount) | route (parent-mux mount + auth gate) | request-response | `cmd/aura/serve_webui.go` `graphSchemaRoute` mount + `POST /agent/run` `RequireCapability` (self-precedent) | exact |
| `cmd/aura/serve.go` (MODIFY — build providers + `SetXxx` at composition root) | config (composition root wiring) | n/a | `cmd/aura/serve.go` `SetGraphView`/`SetApprovalStore` block (self-precedent) | exact |
| MCP probe extraction (`cmd/aura/mcp_status.go` MODIFY or new shared seam — `probeServer(ctx,name,server) ProbeResult`) | service (live probe) | request-response (fan-out, bounded) | `cmd/aura/mcp_status.go` `mcpDoctorAll` (refactor target) | role-match (text→struct) |
| Skills per-stage reader (`internal/skills/*.go` NEW — `os.ReadDir` over `pending/`+`archived/`) | service (filesystem reader) | file-I/O | `internal/skills/loader.go` `scan`/`parseFrontmatter` | role-match |
| `internal/cron/store_runs.go` (MODIFY — add `ListRunsForTask` wrapper) | model (store wrapper) | CRUD (paginated read) | `internal/cron/store.go` `ListActiveTasks` + `store_runs.go` `GetRun` | exact |
| `internal/db/queries/agent_job_runs.sql` (MODIFY — `ListRunsForTask :many`) | migration (sqlc query) | CRUD (paginated read) | `internal/db/queries/agent_job_runs.sql` existing `:many` queries | exact |
| `internal/identity/store.go` (MODIFY — add `ListCapabilities` wrapper) | model (store wrapper) | CRUD (read) | `internal/identity/store.go` `HasCapability`/`GrantCapability` (self-precedent) | exact |
| `internal/db/migrations/0021_identity_audit.{up,down}.sql` (NEW — immutable audit table) | migration | n/a | `internal/db/migrations/0010_skill_audit.up.sql` | exact |
| `internal/identity/audit_store.go` (NEW — append-only audit store) | model (append-only store) | CRUD (insert + read) | `internal/skills/audit_store.go` | exact |
| Provisioning saga (`internal/agui/onboarding_provision.go` or `internal/provisioning/*` NEW) | service (cross-store saga) | event-driven (ordered + compensation) | NO direct analog — composed from `db.WithTx` + `webauth` create + `identity.DeleteIdentity` + Authula `UserService.Delete` (see §No Analog) | partial |
| Server-side QR render (`internal/agui/*` or reuse `internal/setup/qr.go`) | utility | transform | `internal/setup/qr.go` `qrSVG` (deferred STUB — replace w/ vendored `rsc.io/qr`) | role-match (stub→impl) |

### Frontend (React / i18n) — analog pins from 28-UI-SPEC §Component Inventory

| New File | Role | Data Flow | Closest Analog | Match Quality |
|----------|------|-----------|----------------|---------------|
| `web/src/governance/GovernanceWorkspace.tsx` (lazy default export) | component (workspace) | request-response | `web/src/graph/GraphExplorer.tsx` | exact |
| `web/src/governance/McpBoard.tsx` + `McpServerDetail.tsx` | component (board + detail) | request-response (+ per-row live probe) | `GraphExplorer.tsx` (master) + `NodeInspector.tsx` (detail) | exact |
| `web/src/governance/SkillsBoard.tsx` (4 sub-tabs) + `SkillDetail.tsx` | component (tabbed board + detail) | request-response | `GraphExplorer.tsx` + `SeedFilterPanel.tsx` (filter/tab idiom) | role-match |
| `web/src/governance/SchedulerBoard.tsx` + `TaskRunHistory.tsx` | component (board + paginated list) | request-response (paginated) | `GraphExplorer.tsx` + `PathStrip.tsx` (list idiom) | role-match |
| `web/src/governance/governanceApi.ts` | utility (data hook layer) | request-response | `web/src/graph/graphApi.ts` | exact |
| `web/src/onboarding/OnboardingWizard.tsx` (lazy, full-screen overlay) | component (linear wizard) | request-response (per-step) | `GraphExplorer.tsx` (lazy chunk + state machine shape) | role-match (new shape) |
| `web/src/onboarding/onboardingApi.ts` | utility (data hook layer) | request-response | `web/src/graph/graphApi.ts` | exact |
| `web/src/shell/modes.ts` (MODIFY — add `'governance'`) | config | n/a | `web/src/shell/modes.ts` (self) | exact |
| `web/src/AppShell.tsx` (MODIFY — center swap to lazy Governance) | component (shell) | n/a | `web/src/AppShell.tsx` `surface === 'graph'` swap (self-precedent) | exact |
| `web/src/i18n/resources.governance.ts` + `resources.onboarding.ts` | config (i18n bundle) | n/a | `web/src/i18n/resources.graph.ts` + `resources.ts` spread | exact |

---

## Pattern Assignments

### `internal/agui/governance_api.go` (controller, request-response read)

**Analog:** `internal/agui/graph_api.go` (thin read handler) + `internal/agui/conversations_api.go` (helpers + register pattern)

**Consumer-side narrow interface** (graph_api.go:57-60) — declare ONE per provider, off the constructor:
```go
// GraphView is the narrow read-only graph surface the handlers consume (D-A2-02:
// declared consumer-side so the handler depends only on the two methods it calls).
type GraphView interface {
	Schema(ctx context.Context) (knowledge.GraphSchema, error)
	Query(ctx context.Context, in knowledge.GraphIntent) (knowledge.GraphResult, error)
}
```
→ For Phase 28 declare e.g. `GovernanceProviders` (or per-board `MCPBoard`/`SkillsBoard`/`SchedulerBoard`) interfaces with ONLY the methods the handlers call. A `Server` with the provider unwired answers 503 (see §Shared `SetXxx`).

**Route registration** (graph_api.go:66-69) — SPECIFIC method+path siblings under `/api/`, NEVER a bare `/api/`:
```go
func (s *Server) registerGraphRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/graph/schema", s.handleGraphSchema)
	mux.HandleFunc("POST /api/graph/query", s.handleGraphQuery)
}
```
→ Add `registerGovernanceRoutes(mux)` with the 6 GOV reads from RESEARCH §REST Endpoint Shapes:
`GET /api/governance/mcp`, `GET /api/governance/mcp/{name}/probe`, `GET /api/governance/skills`, `GET /api/governance/skills/audit`, `GET /api/governance/scheduler`, `GET /api/governance/scheduler/{id}/runs`.

**503-when-unwired + sanitized-502 read handler** (graph_api.go:75-86):
```go
func (s *Server) handleGraphSchema(w http.ResponseWriter, r *http.Request) {
	if s.graph == nil {
		http.Error(w, "graph view not configured", http.StatusServiceUnavailable)
		return
	}
	schema, err := s.graph.Schema(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	writeJSON(w, schema)
}
```
→ This is the exact shape for every GOV read: unwired→503, backend-fail→sanitized 502, success→`writeJSON`. Empty dataset → `writeJSON(w, {servers: []})` (200, never a crash — cross-cutting AC).

**{id} path-param uuid-guard BEFORE the store call** (conversations_api.go:79-86) — for `GET /api/governance/scheduler/{id}/runs`:
```go
func parseConvID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "conversation not found", http.StatusNotFound)
		return "", false
	}
	return id, true
}
```
→ A non-UUID task id is a clean 404 before `ListRunsForTask` (parity with T-25-02). For the MCP `{name}` param: look it up in the loaded config (404 if absent — prohibition #5, RESEARCH §Hard Problem 3); NEVER take a URL/command from the body.

**Pagination query parse** (conversations_api.go:24-30) — for `?limit=&offset=`:
```go
func parseLimit(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultSearchLimit
	}
	return n
}
```
→ Scheduler run-history default limit 25 / offset 0 (RESEARCH §REST shapes); skills audit default 100 (the store default, audit_store.go:171).

---

### `internal/agui/onboarding_api.go` (controller, request-response read + create-mutation)

**Analog:** `internal/agui/graph_api.go` (read + validate) + `internal/agui/server.go` `handleRun` (body-cap + principal read + mutation)

**Body size-cap + decode + server-side validate, NO business logic** (graph_api.go:95-116):
```go
func (s *Server) handleGraphQuery(w http.ResponseWriter, r *http.Request) {
	if s.graph == nil {
		http.Error(w, "graph view not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var intent knowledge.GraphIntent
	if err := json.NewDecoder(r.Body).Decode(&intent); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.validateGraphIntent(r.Context(), intent); err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	res, err := s.graph.Query(r.Context(), intent)
	...
}
```
→ Every onboarding POST (`/start`, `/{token}/step`, `/{token}/provision`, `/{token}/telegram-status`) follows this: `MaxBytesReader(maxRunBodyBytes)` (server.go:30, `1<<20`), decode→400, validate (enum/length: intent in {answer,confirm,edit,skip}; email/password length; capability-name grammar) → sanitized 400, then ONE service call. The saga + no-escalation re-validation live in the **service** (`onboarding_session.go`/saga), NOT the handler.

**Reading the authenticated principal inside the handler** (server.go:226-230) — needed for the create mutation's creator-grants check (D-06) and the audit `actor_identity_id`:
```go
identityID, ok := principalIdentityID(r)
if !ok {
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return
}
```
→ `principalIdentityID(r)` reads what `RequireAuth` stashed (auth.go:285-289 `withPrincipal`/`principalFrom`). The provision handler uses it as the creator id for the subset-⊆-creator validation and the immutable audit row.

**No-escalation server re-validation** (the must-NOT, SPEC prohibition #3) — compose `identity.Store.ListCapabilities` (the creator's grants) + reject `*` + assert each ticked cap ∈ creator-grants, BEFORE the saga. Backstopped by `GrantCapability`'s own `*` rejection (identity/store.go:177-185, below).

---

### `internal/agui/onboarding_session.go` (service, server-held TTL session store)

**Analog (composite):** `internal/skills/loader.go` (goroutine-free TTL cache discipline) + the `onboarding.Session` it wraps (session.go:112)

**Goroutine-free, mutex-guarded, lazy-sweep TTL** — mirror the loader's no-background-goroutine cache (loader.go:17 TTL constant, 113-127 lazy re-scan under mutex). RESEARCH §Hard Problem 4 specifies: `map[sessionToken]*sessionEntry{session *onboarding.Session, identityID, expiresAt}`, 15-min idle TTL refreshed per step, swept lazily on access. NO background goroutine (goleak-clean, [[feedback_minipc_cpu_budget]]).

**The wrapped state machine** (`onboarding.Session`, RESEARCH-verified):
- `Session.Apply(Input{Intent,Text,Answers})` (session.go:132) is the step driver; `Intent` consts (session.go:15-28); `Step`/`Status` consts (session.go:33-62).
- The **no-duplicate-LLM-turn** guarantee = the unexported `prompted` latch (session.go:124) + the step pointer. `edit`→`refreshDraft`→`ExtractDraft` (session.go:235-246,349-357) is the **deterministic** render, not the per-answer LLM extractor.
- `LLMAnswerExtractor.Extract(ctx, step, raw)` (extractor_llm.go:49) runs **exactly once** per inbound free-text answer (one-shot, `Temperature:0`, never errors, raw-text fallback). Call it ONCE in the step handler when an `answer` carries text, then `Apply`.

**Step response contract** (D-03 / RESEARCH §Hard Problem 4): `{content, step, status, draft?, preferences?}` projected via `writeJSON`.

---

### `internal/agui/server.go` (config — DI seam + route registration) [MODIFY]

**Analog:** self-precedent — `SetGraphView` (server.go:122-127) + the `Mux()` register block (server.go:148-158)

**`SetXxx` injection kept OFF the constructor** (server.go:122-127):
```go
// SetGraphView wires the read-only Phase-27 graph normalizer (GRAPH-01) ...
// until set, both routes answer 503 (a missing graph client must not abort serve boot).
// Kept off the constructor so existing NewServer callers/tests stay unchanged (D-A2-02).
func (s *Server) SetGraphView(gv GraphView) { s.graph = gv }
```
→ Add `func (s *Server) SetGovernanceProviders(...)` + `func (s *Server) SetOnboardingService(...)` — add the fields to `Server` struct (server.go:88-97), keep them off `NewServer` (server.go:104-106). 503 until wired.

**Register block in `Mux()`** (server.go:148-158) — colocate the new register calls beside the existing ones:
```go
s.registerConversationRoutes(mux)
s.registerApprovalRoutes(mux)
s.registerAssetRoutes(mux)
s.registerGraphRoutes(mux)
```
→ Add `s.registerGovernanceRoutes(mux)` + `s.registerOnboardingRoutes(mux)`.

---

### `cmd/aura/serve_webui.go` (route — parent-mux mount + auth gate) [MODIFY]

**Analog:** self-precedent — `graphSchemaRoute`/`graphQueryRoute` consts (serve_webui.go:147-150) + read mount (serve_webui.go:266-267) + the `POST /agent/run` `RequireCapability` mount (serve_webui.go:213)

**Route consts beside `graphSchemaRoute`** (serve_webui.go:140-150):
```go
const (
	graphSchemaRoute = "GET /api/graph/schema"
	graphQueryRoute  = "POST /api/graph/query"
)
```
→ Add the GOV read consts + the onboarding consts (including the two `RequireCapability`-gated mutations: `POST /api/onboarding/start` and `POST /api/onboarding/{sessionToken}/provision`).

**Read-GET mount (inherits whole-mux `RequireAuth`, NO capability gate)** (serve_webui.go:266-267):
```go
mux.Handle(graphSchemaRoute, aguiHandler)
mux.Handle(graphQueryRoute, aguiHandler)
```
→ All 6 GOV reads + `GET /api/onboarding/{token}/telegram-status` + `POST /api/onboarding/{token}/step` mount this way (the step is authz'd at `/start`). They are SPECIFIC method+path siblings under the `/api/` exclusion carve-out (serve_webui.go:90) — NEVER a bare `/api/` (would shadow `/api/integrations/`).

**Create-mutation mount behind `RequireCapability`** (serve_webui.go:213) — parity with `POST /agent/run`:
```go
mux.Handle("POST /agent/run", agui.RequireCapability(aguiHandler, auth, agentRunCapability))
```
→ `POST /api/onboarding/start` and `POST /api/onboarding/{sessionToken}/provision` mount as `agui.RequireCapability(aguiHandler, auth, "identity.create")`. The method+path-specific pattern wins Go 1.22 longest-pattern precedence; the gate fires AFTER `RequireAuth` binds the principal. **Introduce `identity.create`** as the new capability name (parity with `agentRunCapability = "agent.run"`, serve_webui.go:99; the seeded `local` holds `*` so it passes).

**The whole mux is wrapped once** (serve_webui.go:289 `return agui.RequireAuth(mux, auth), nil`) — every `/api/*` inherits the whole-origin gate (401 unauthenticated, the cross-cutting AC) for free.

---

### `cmd/aura/serve.go` (config — composition root wiring) [MODIFY]

**Analog:** self-precedent — the `SetApprovalStore`/`SetImageProxy`/`SetGraphView` block (serve.go:285-312)

**Build provider AFTER `agui.NewServer`, best-effort, then `SetXxx`** (serve.go:307-312):
```go
if gclient, gerr := knowledge.Open(ctx, &chat.cfg.Neo4j); gerr != nil {
	slog.Warn("aura serve: graph explorer unavailable", "err", gerr)
} else {
	chat.mcpClosers = append(chat.mcpClosers, gclient.Close)
	aguiServer.SetGraphView(knowledge.NewGraphView(gclient))
}
```
→ Build the governance providers (MCP config loader + skills loader/stage-reader + `cron.Store`) and the onboarding service (session store + saga, wired to `chat.pool`, `webauth` `authulaProvider`, `identity.Store`, `telegram.Store`) here, then `SetGovernanceProviders`/`SetOnboardingService`. `chat.pool` exposes the pgxpool; `authulaProvider` (serve.go:325 `buildAuthDeps`) exposes `CoreServices()`. A missing backend leaves the routes at 503 (must NOT abort boot).

---

### `internal/db/migrations/0021_identity_audit.{up,down}.sql` (NEW immutable audit table)

**Analog:** `internal/db/migrations/0010_skill_audit.up.sql` (the verified append-only template; next free slot confirmed = 0021, after `0020_assets`)

**The immutability triple: role grant + row trigger + statement trigger** (0010_skill_audit.up.sql:67-92):
```sql
CREATE FUNCTION aura.reject_skill_audit_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'aura.skill_audit is append-only: % is not permitted', TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;

CREATE TRIGGER skill_audit_no_update_delete
    BEFORE UPDATE OR DELETE ON aura.skill_audit
    FOR EACH ROW EXECUTE FUNCTION aura.reject_skill_audit_mutation();

-- SEPARATE statement trigger for TRUNCATE (Pitfall 1: a row trigger NEVER fires for TRUNCATE)
CREATE TRIGGER skill_audit_no_truncate
    BEFORE TRUNCATE ON aura.skill_audit
    FOR EACH STATEMENT EXECUTE FUNCTION aura.reject_skill_audit_mutation();

GRANT SELECT, INSERT ON aura.skill_audit TO aura_app;
GRANT ALL              ON aura.skill_audit TO aura_migrate;
```
→ Copy verbatim, renaming to `aura.identity_audit` / `reject_identity_audit_mutation`. Columns per RESEARCH §Hard Problem 1: `id uuid PK DEFAULT gen_random_uuid(), created_at timestamptz NOT NULL DEFAULT now(), actor_identity_id text NOT NULL, new_identity_id uuid NOT NULL, new_identity_name text NOT NULL, granted_capabilities text[] NOT NULL, authula_user_id text NOT NULL`. Plain (non-CONCURRENT) indexes on the fresh table (Pitfall 6, lines 60-65). **NO D-29 coherence CHECK** (that is skill-specific) — keep the schema flat.

---

### `internal/identity/audit_store.go` (NEW append-only store)

**Analog:** `internal/skills/audit_store.go` (the canonical `Store{pool,q}` append-only shape)

**INSERT-via-tx-bound-Queries + SELECT-only, sentinel error, SQLSTATE classification** (audit_store.go:65-76, 146-158):
```go
// AuditStore wraps a pgx pool and the generated Queries (canonical shape). It is
// INSERT + SELECT only — the append-only contract is enforced at the DB ...
type AuditStore struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}
func NewAuditStore(pool *pgxpool.Pool) *AuditStore { return &AuditStore{pool: pool, q: sqlc.New(pool)} }

// InsertAuditTx appends one audit row using a tx-bound Queries (the writer's db.WithTx
// closure passes it), so the INSERT participates in the surrounding atomic write.
func InsertAuditTx(ctx context.Context, q *sqlc.Queries, in AuditInsert) error {
	params, err := in.toParams()
	if err != nil { return fmt.Errorf("insert skill audit (tx): %w", err) }
	if _, err := q.InsertSkillAudit(ctx, params); err != nil {
		return fmt.Errorf("insert skill audit %q (tx): %w", in.SkillName, classifyAuditErr(err))
	}
	return nil
}
```
→ Mirror exactly: an `InsertIdentityAuditTx(ctx, q, in)` so the audit row can commit inside the same aura-leg `db.WithTx` (or RESEARCH-recommended L8: a tiny final `db.WithTx` after Leg C succeeds → "exactly one row, only on success"). Sentinel `ErrAuditImmutable` for SQLSTATE 42501 (audit_store.go:55-63). The `ErrAuditImmutable` round-trip is the AC test "immutable audit row".

---

### `internal/cron/store_runs.go` + `internal/db/queries/agent_job_runs.sql` (paginated run-history)

**Analog:** `internal/cron/store.go` `ListActiveTasks` (store.go:162) + the existing `:many` queries in `agent_job_runs.sql`

→ **Wave-0 GAP** (RESEARCH-verified, no migration needed — table + `last_heartbeat_at` already exist): add
```sql
-- name: ListRunsForTask :many
SELECT … FROM aura.agent_job_runs WHERE task_id = $1 ORDER BY started_at DESC LIMIT $2 OFFSET $3;
```
+ a thin `func (s *Store) ListRunsForTask(ctx, taskID string, limit, offset int) ([]Run, error)` wrapper mirroring the existing `GetRun`/`ListActiveTasks` projection (`Run` struct = store.go:77-90). Default limit 25 / offset 0 in the handler.

---

### `internal/identity/store.go` (add `ListCapabilities` wrapper) [MODIFY]

**Analog:** self-precedent — `HasCapability` (store.go:128-138) + `GrantCapability`'s `*`-rejection (store.go:177-185)

→ **Wave-0 GAP**: the `ListCapabilities` **sqlc query exists** (`internal/db/queries/capability_grants.sql:11`) but **no Store wrapper**. Add `func (s *Store) ListCapabilities(ctx, identityID string) ([]string, error)` mirroring `HasCapability`'s parse-UUID + `s.q.…` + error-wrap shape. The D-06 picker filters `*` out client+server-side. The no-escalation backstop is already in place:
```go
func (s *Store) validateGrantInput(identityID, capability string) (pgtype.UUID, error) {
	if capability == Wildcard {
		return pgtype.UUID{}, ErrWildcardManaged
	}
	...
}
```
→ `GrantCapability` rejects `*` BEFORE any DB call — the saga grants each ticked cap through it, so a `*` slip is rejected three ways (picker excludes / server re-validates / store rejects).

---

### MCP live probe extraction (`cmd/aura/mcp_status.go` → structured `probeServer`)

**Analog:** `cmd/aura/mcp_status.go` `mcpDoctorAll` (mcp_status.go:53-123) — refactor target

→ **Wave-0**: extract `func probeServer(ctx, name string, server …) ProbeResult` returning `{Name, OK, ToolCount, Detail, Err}` instead of writing text to a Writer (the current `writeRuntimeCheck`/`writeRecipeChecks` path). The board's static rows come from `manager.SnapshotStatus(doc)` (status.go:40, always succeeds). The probe is a SEPARATE per-row request with `context.WithTimeout(r.Context(), 3*time.Second)` (RESEARCH §Hard Problem 3) so a hung server fails ONLY its row. **L1 landmine**: a real mounted-tool-count needs `internal/mcp.Client.Open` + tools/list (the doctor today only does `LookPath`/endpoint-reachability). Probe iterates ONLY `doc.MCPServers` (prohibition #5), never a body-supplied target.

---

### Frontend — `web/src/governance/GovernanceWorkspace.tsx` + boards (component, request-response)

**Analog:** `web/src/graph/GraphExplorer.tsx`

**Lazy default export, mounted on `surface` swap** (GraphExplorer.tsx:1,35-37,71 + AppShell.tsx:25,241-242):
```tsx
// GraphExplorer is the lazy default export the AppShell mounts when surface==='graph' (its
// own Vite chunk so the Sigma stack never lands in the main bundle — Pitfall 7).
const SigmaCanvas = lazy(() => import('./SigmaCanvas').then((mod) => ({ default: mod.SigmaCanvas })));
export default function GraphExplorer({ threadId }: GraphExplorerProps) { ... }
```
AppShell center-swap (AppShell.tsx:25, 241-243):
```tsx
const GraphExplorer = lazy(() => import('./graph/GraphExplorer'));
...
{surface === 'graph' ? (
  <GraphExplorer threadId={activeThreadId} />
) : (
  <ExternalStoreChat threadId={activeThreadId} ... />
)}
```
→ Add `const GovernanceWorkspace = lazy(() => import('./governance/GovernanceWorkspace'));` + a `surface === 'governance'` arm. The wizard is a SEPARATE full-screen overlay (D-04), not a tab.

**Explicit view-state machine (loading/populated/empty/error/error-auth)** (GraphExplorer.tsx:43-50, 59-61):
```tsx
type ViewStatus = 'loading' | 'populated' | 'empty' | 'error-query' | 'error-schema' | 'error-auth';
function isAuthError(err: unknown): boolean {
  return err instanceof Error && err.message === 'HTTP 401';
}
```
→ Every board view carries these states (28-UI-SPEC §State contract). An expired session → VISIBLE `error-auth` (never a blank/crash); an unavailable backend → sanitized error copy.

**State render blocks (loading / error-auth / error / empty / populated)** (GraphExplorer.tsx:191-254) — copy the `role="status"` loading region, the `role="alert"` error+retry, and the empty-state idiom verbatim (token classes `text-text-muted`/`text-danger`/`min-h-[44px]`).

**Mobile bottom-sheet detail + backdrop-tap-to-dismiss** (GraphExplorer.tsx:262, 327-372) — the `lg:grid` desktop flip + the `fixed inset-x-0 bottom-0 z-40 max-h-[78svh]` mobile inspector sheet + the `bg-black/50 lg:hidden` backdrop. This is the board detail-pane + the skills/scheduler detail pattern (28-UI-SPEC: mobile = list with detail as bottom sheet).

---

### Frontend — `web/src/governance/governanceApi.ts` + `onboarding/onboardingApi.ts` (utility, data layer)

**Analog:** `web/src/graph/graphApi.ts`

**`same-origin` fetch, non-200 (incl 401) THROWS** (graphApi.ts:18-40):
```ts
async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  });
  if (!res.ok) {
    throw new Error(`HTTP ${String(res.status)}`);
  }
  return (await res.json()) as T;
}
```
→ Copy `getJSON`/`postJSON` verbatim. A non-200 (incl. 401) THROWS `Error("HTTP <n>")` so the TanStack Query error path routes the auth/error state — never a silent blank board. The onboarding `postJSON` drives `/start`, `/{token}/step`, `/{token}/provision`; the create-mutation 403 surfaces as `HTTP 403` for the picker/review error copy.

---

### Frontend — i18n `resources.governance.ts` + `resources.onboarding.ts`

**Analog:** `web/src/i18n/resources.graph.ts` (split bundle) + `resources.ts` spread

**Split bundle exporting `<feature>En`/`<feature>It`** (resources.graph.ts:7-8):
```ts
// split out of resources.ts to keep that file under the 600-LOC cap (CLAUDE.md "no god
// class"). resources.ts spreads graphEn/graphIt into each language's `translation` object.
// Add every key to BOTH en AND it — a missing key in either language is a defect.
export const graphEn = { graph: { title: 'Graph Explorer', ... } };
```
**Spread into both languages** (resources.ts:2, 144-145, 419-420):
```ts
import { graphEn, graphIt } from './resources.graph';
// en.translation:
      ...displayEn,
      ...graphEn,
// it.translation:
      ...displayIt,
      ...graphIt,
```
→ Create `governanceEn`/`governanceIt` + `onboardingEn`/`onboardingIt`; import + spread both into the en AND it `translation` objects. Also add `shell.modes.governance` to the existing `shell.modes` block (resources.ts:60-66 en, 335-341 it) for the nav label.

---

## Shared Patterns

### Authentication / Authorization (RequireAuth inherited; RequireCapability on mutations)
**Source:** `internal/agui/auth.go:259-276` (`RequireCapability`) + `cmd/aura/serve_webui.go:213,289`
**Apply to:** All new `/api/*` routes (read inherit `RequireAuth`); `POST /api/onboarding/start` + `/provision` add `RequireCapability(…, "identity.create")`
```go
func RequireCapability(next http.Handler, deps AuthDeps, capability string) http.Handler {
	if !deps.SecretConfigured {
		return next // loopback dev - auth disabled, pass-through with RequireAuth
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identityID := principalFrom(r.Context())
		if identityID == "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ok, err := deps.Identities.HasCapability(r.Context(), identityID, capability)
		if err != nil || !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

### Error redaction (secrets-never-leaked belt)
**Source:** `internal/agui/server_redact.go:41` (`sanitizeErr`) + `internal/agui/graph_api.go` (every wire error)
**Apply to:** Every error string in every new handler (HARDEN-08); plus `manager.RedactSecrets` (`internal/mcp/redact.go:6-15`) on every MCP string before the wire (env values, LastError); plus the fixed-message-on-provision-failure rule (NEVER `slog` the password/bot token, NEVER surface `err.Error()` verbatim — `internal/setup/handlers.go:44` precedent).
```go
// graph_api.go:82 — backend-fail → sanitized 502
writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
```
The no-secret-in-logs AC test (Wave 0) captures slog output over a full provision run and asserts absence of MCP env / Authula password / Telegram bot token.

### 503-when-unwired DI seam
**Source:** `internal/agui/server.go:122-127` (`SetGraphView`) + `graph_api.go:76-79` (nil-check)
**Apply to:** Every new provider — `SetGovernanceProviders`/`SetOnboardingService` off the constructor; handlers nil-check → 503; serve.go builds best-effort (a backend outage must NOT abort daemon boot).

### Append-only audit ledger (immutable identity-create row)
**Source:** `internal/db/migrations/0010_skill_audit.up.sql:67-92` (role grant + row trigger + statement trigger) + `internal/skills/audit_store.go` (`Store{pool,q}` INSERT+SELECT-only, `InsertAuditTx`)
**Apply to:** `0021_identity_audit` + `internal/identity/audit_store.go`. One immutable row per successful create, written in a `db.WithTx` (RESEARCH L8: tiny final tx after Leg C → exactly-one-on-success).

### Cross-store saga + per-leg compensation
**Source:** NO single analog — composed (see §No Analog Found)
**Apply to:** The provisioning service. Order: pre-validate → Leg B (Authula `UserService.Create`+`AccountService.Create`) → Leg A (`db.WithTx`: identity + grants + `LinkOperator`) → Leg C (`telegram.InsertPending`) → audit. Compensation: `identity.DeleteIdentity` (store.go:118, FK cascade) + Authula `UserService.Delete` (verified at authula_integration_test.go:80). Failure-injection tests B1/B2/A/C → no orphan (RESEARCH §Hard Problem 1).

### Frontend view-state + same-origin throwing fetch
**Source:** `web/src/graph/GraphExplorer.tsx:43-50,191-254` + `web/src/graph/graphApi.ts:18-40`
**Apply to:** Every board + wizard view (loading/populated/empty/error/error-auth, `role="status"`/`role="alert"`, mobile bottom-sheet); every data hook (`same-origin`, non-200 THROWS → TanStack auth/error route).

### Untrusted-output escaping (HARDEN-08)
**Source:** 28-UI-SPEC §Interaction & A11y Contract (line 207) — codebase posture: React-escaped text, NEVER `dangerouslySetInnerHTML`
**Apply to:** Every backend-supplied string (skill name, server name, env KEY, task summary, identity name) → `<span>/<dd>` text; mono for data-shaped values; redacted secret values as `bg-surface-3 font-mono` chips (never the value).

---

## No Analog Found

| File | Role | Data Flow | Reason / Planner guidance |
|------|------|-----------|---------------------------|
| Provisioning saga (`internal/agui/onboarding_provision.go` or `internal/provisioning/*`) | service | event-driven (ordered + compensation) | **No existing cross-store/cross-pool transactional write exists.** Aura's writes are single-pool `db.WithTx`. The saga is genuinely new — but every LEG has a verified analog: Leg A = `db.WithTx` (the skills writer + `InsertAuditTx` shape, audit_store.go:146-158); Leg B = the verified Authula `PasswordService.Hash` → `UserService.Create` → `AccountService.Create` sequence (RESEARCH §Authula, proven at `internal/webauth/authula_integration_test.go:76`); Leg C = `telegram.Store.InsertPending` (store.go:84). Compensation = `identity.DeleteIdentity` (store.go:118) + Authula `UserService.Delete` (authula_integration_test.go:80). The planner assembles the orchestration from these; RESEARCH §Hard Problem 1 has the exact ordered pseudo-code + the 6 failure-injection points. Use RESEARCH patterns + the per-leg analogs above, not a single template. |

---

## Metadata

**Analog search scope:** `internal/agui/`, `internal/skills/`, `internal/cron/`, `internal/identity/`, `internal/webauth/`, `internal/onboarding/`, `internal/mcp/`, `internal/setup/`, `internal/channels/telegram/`, `internal/db/migrations/` + `queries/`, `cmd/aura/`, `web/src/graph/`, `web/src/shell/`, `web/src/i18n/`, `web/src/AppShell.tsx`
**Files scanned (read at file:line this session):** 16 (plus RESEARCH's prior verified-seam map confirmed)
**Pattern extraction date:** 2026-06-20
**Confirms/refines RESEARCH:** RESEARCH §Exact Reuse Map nailed every analog; this map extracts the copy-from excerpts + reuses the 28-UI-SPEC §Component Inventory frontend pins. One RESEARCH correction stands: the skills audit method is `AuditStore.List` (not `ListAudit`).
