# Phase 36: Multi-User Identity Isolation + Authula Cutover - Pattern Map

**Mapped:** 2026-07-05
**Files analyzed:** 27 new/modified (grouped into 9 work clusters)
**Analogs found:** 25 / 27 (2 no-analog: Postgres RLS policy, Garage bucket-per-identity provisioning)

> RESEARCH.md did not exist at mapping time — the file list below was derived from
> `36-CONTEXT.md` decisions D-01..D-29 alone. When RESEARCH.md lands, the planner should
> reconcile any file it names that is not listed here.

> **Migration floor is 0025** (`0025_document_control_plane`). Every NEW migration this
> phase lands at **0026+** in ledger order.

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|-------------------|------|-----------|----------------|-------|
| `internal/documents/types.go` | model | transform | (self — add fields) | exact |
| `internal/documents/indexer.go` | service | file-I/O/graph-write | `docker/agent-memory/.../graph/queries.py` (`:User` MERGE) | exact |
| `internal/documents/search.go` | service | request-response/graph-read | `docker/agent-memory/.../graph/queries.py` (`:User` EXISTS) | exact |
| `internal/documents/retrieve.go` | service | request-response/graph-read | same memory `:User` filter | exact |
| `internal/agent/tools/document_search.go` | tool | request-response | `internal/agent/mcptools` memory scoping (`user_identifier`) | role-match |
| migration `0026_documents_user_backfill` (+ script) | migration | batch | `9a4ca594` memory backfill + `internal/db/migrations/0014` | role-match |
| migration `0027_owner_id_rls.up.sql` | migration | CRUD | `internal/db/migrations/0020_assets` + `0004_identity` | role-match |
| `internal/db/tx.go` (SET LOCAL seam) | utility | request-response | (self — `WithTx`) | exact |
| `internal/assets/store.go` + new `*ForIdentity` stores | store | CRUD | `internal/assets/store.go` `GetForIdentity` | exact |
| `internal/agui/settings_api.go` | controller | CRUD | (self — already gated) | exact |
| `cmd/aura/serve_webui.go` (settings.model.write mount) | config/route | request-response | `serve_webui.go` `RequireCapability` mounts | exact |
| migration `0028_local_admin_caps` (seed) | migration | CRUD | `internal/db/migrations/0004_identity` seed | exact |
| `internal/webauth/identity_link.go` (LinkUser) | service | CRUD | `identity_link.go` `LinkOperator` | exact |
| `internal/agui/onboarding_provision.go` (provisioning saga extend) | service | event-driven/saga | `onboarding_provision.go` `Provision` | exact |
| new de-provisioning saga (`internal/agui/deprovision_*.go`) | service | event-driven/saga | `onboarding_provision.go` (symmetric) | role-match |
| migration `0029_saga_journal.up.sql` | migration | event-driven | `internal/db/migrations/0021_identity_audit` (append-only) | role-match |
| migration `0030_identity_soft_delete` | migration | CRUD | `0020_assets` `deleted_at` + `0004_identity` | role-match |
| break-glass CLI (`cmd/aura/identity.go` or new `recovery.go`) | CLI | request-response | `cmd/aura/identity.go` switch tree + `0023_identity_recovery` | exact |
| `internal/agent/tools/shell_bg.go` (owner-bind + TTL) | tool | event-driven | `shell_bg.go` (self) | exact |
| `internal/mcp/managed_config.go` (per-identity root) | config | file-I/O | `internal/profile/store.go` `profileDir` + `managed_config.go` | role-match |
| per-identity skills root (`internal/skills/*`) | service | file-I/O | `internal/profile/store.go` `profileDir` | role-match |
| Garage bucket-per-identity (`internal/objectstore/*`) | service | file-I/O | `internal/objectstore/s3.go` (no bucket-create analog) | partial |
| runner conversation-delete lifecycle | service | event-driven | `shell_bg.go` `Shutdown`/`Evict` + `askuser` auto-resolve | role-match |
| session keyed by `(identity,session)` | store | event-driven | `identityctx` + runner session map | partial |
| `internal/channels/telegram/bot_dispatch_*.go` (multi-user route) | controller | event-driven | `bot_dispatch_auth.go` `telegramUserIsLinked` | exact |
| admin audit UI + backing API (`internal/agui/audit_api.go` new) | controller | request-response | `internal/agui/settings_api.go` read + `0021`/`0022` tables | role-match |
| two-identity E2E (`*_integration_test.go`) | test | request-response | `internal/knowledge/graphview_integration_test.go` (combined tags) | exact |
| `internal/config/config.go` (`AURA_MUSR_ISOLATION` flag) | config | request-response | `config.go` `envDefault` / `SkillsDir` | exact |

---

## Pattern Assignments

### Cluster A — Documents plane leak fix (D-09/D-12): the memory `:User`-ownership template

This is the single most important mapping. The memory MCP graph was scoped by user in commit
**`9a4ca594`** (`fix(memory): scope MCP graph tools by user`). The documents plane copies its
shape **verbatim**: a `MERGE (:User)` owner edge on write + a fail-closed `EXISTS{}` /
`MATCH (:User)-[…]->` filter on every read.

**Analog — write ownership** (`docker/agent-memory/src/neo4j_agent_memory/graph/queries.py:349-353`):
```cypher
MERGE (u:User {identifier: $user_identifier})
ON CREATE SET u.id = $user_identifier, u.created_at = datetime()
...
MERGE (u)-[:HAS_ENTITY]->(e)
```

**Analog — read scoping (fail-closed)** (`queries.py:337-340`, the entity read):
```cypher
OPTIONAL MATCH (:User {identifier: $user_identifier})-[ue:HAS_ENTITY]->(node)
```
and the conversation read's belt-and-suspenders both-sides filter (`queries.py:186-189`):
```cypher
MATCH (c:Conversation)-[:HAS_MESSAGE]->(node)
OPTIONAL MATCH (u:User {identifier: $user_identifier})-[:HAS_CONVERSATION]->(c)
... AND (c.user_identifier = $user_identifier OR u IS NOT NULL)
```

**Go-side proof it works (copy the test shape too)** — `internal/agent/mcptools/memory_integration_test.go`
`TestMemoryLiveScopedGraphToolsAcceptUserIdentifier` (added in `9a4ca594`): write as `uid`, read
back `fact_count: 1`, then read as `uid+"-other"` and assert `fact_count: 0` (cross-identity deny).
The two-identity E2E (D-29) uses exactly this "write A, deny B" assertion.

---

#### `internal/documents/types.go` (model, transform)

**Delta:** add `IdentityID string` to three structs so identity threads end-to-end.

**Current** (`types.go:23-31`, `67-79`, `134-139`):
```go
type IngestRequest struct { SourceID string; SourceKind string; OriginalPath string; ... }
type ExtractedDocument struct { ID string; SourceID string; ...; Chunks []Chunk; CreatedAt time.Time }
type SearchRequest struct { Query string; DocumentID string; Limit int }
```
Add `IdentityID` to each (the isolation key). `SearchRequest.IdentityID == ""` MUST fail closed
(D-09.2), not fall back to global.

---

#### `internal/documents/indexer.go` (service, graph-write)

**Delta:** on upsert, atomically `MERGE (:User {identifier})` + `MERGE (:User)-[:HAS_DOCUMENT]->(:Document)`.
Note the `WITH` fence required between `MERGE` and a following `MATCH` (D-09.1).

**Analog to mirror** — the existing `chunkUpsertQuery` already shows the `MATCH … UNWIND … MERGE … MERGE (edge)`
shape (`indexer.go:239-261`):
```cypher
MATCH (d:Document {id: $document_id})
UNWIND $chunks AS chunk
MERGE (c:Chunk {id: chunk.id})
SET  c.document_id = chunk.document_id, ...
MERGE (d)-[:HAS_CHUNK]->(c)
RETURN count(c) AS chunks
```
And `documentUpsertQuery` (`indexer.go:219-236`) is where the `:User` owner MERGE lands. Apply the
memory `queries.py:349-353` pattern: add `$identity` param in `documentParams` (`indexer.go:122-137`),
then `MERGE (u:User {identifier: $identity}) MERGE (u)-[:HAS_DOCUMENT]->(d)` after the document MERGE.
`UpsertSparse` (`indexer.go:29-75`) passes the new param through the same `map[string]any` it already
builds for `documentUpsertQuery`/`chunkUpsertQuery`.

---

#### `internal/documents/search.go` + `internal/documents/retrieve.go` (service, graph-read)

**Delta:** add an owner `EXISTS{}` clause to **every** retrieval query. Empty identity fails closed.
The `chunk_text` fulltext index can't be node-restricted, so for `sparseSearchQuery` the filter is
applied **after** the `YIELD` (post-filter, D-09.2).

**Queries to scope (all in these two files):**
- `sparseSearchQuery` (`search.go:169-185`) — post-`YIELD` `EXISTS` (fulltext can't pre-restrict).
- `docScopedSparseQuery` (`search.go:194-211`) — add `EXISTS` on the `MATCH (node:Chunk …)`.
- `vectorSeedQuery` (`retrieve.go:209-224`) — post-`YIELD` `EXISTS` (HNSW `queryNodes` can't pre-restrict).
- `docScopedVectorSeedQuery` (`retrieve.go:233-249`) — add `EXISTS` on the `MATCH`.
- `neighborExpandQuery` (`retrieve.go:255-271`) — the 1-hop expand MUST also be owner-filtered or a
  foreign neighbour leaks through a winner's `:NEXT_CHUNK` edge.

**Excerpt to modify** (`search.go:169-183`) — current unscoped fulltext seed:
```cypher
CALL db.index.fulltext.queryNodes('chunk_text', $query, {limit: $candidate_limit})
YIELD node, score
WHERE ($document_id = "" OR node.document_id = $document_id)
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
RETURN node.document_id AS document_id, ...
```
Delta: add to the `WHERE` (fail-closed — `$identity` must be non-empty and own the doc):
```cypher
  AND $identity <> ""
  AND EXISTS { (:User {identifier: $identity})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }
```
Every `Read`/`Retrieve` call site (`search.go:40-45,61-65`; `retrieve.go:92-96,166-169`) must add
`"identity": req.IdentityID` to the params map — mirrors how `document_id` is already threaded.

---

#### `internal/agent/tools/document_search.go` (tool, request-response)

**Delta:** read `identityctx.IdentityID(ctx)` and set `SearchRequest.IdentityID` (D-09.3).

**Current** (`document_search.go:80-84`):
```go
hits, err := t.Searcher.Retrieve(ctx, documents.SearchRequest{
    Query:      args.Query,
    DocumentID: strings.TrimSpace(args.DocumentID),
    Limit:      args.Limit,
})
```
Delta: add `IdentityID: identityctx.IdentityID(ctx)`. The ingest path (asset→documents pipeline)
sets `IngestRequest.IdentityID` the same way — `internal/assets/store.go` already carries
`IdentityID` on every asset (see Cluster D), so the ingest worker has it in hand.

---

### Cluster B — Kernel-enforced isolation: Postgres RLS + `*ForIdentity` (D-07)

#### migration `0027_owner_id_rls.up.sql` (migration, CRUD) — **NO direct RLS analog**

There is **zero** existing RLS in the codebase (`ROW LEVEL SECURITY` / `CREATE POLICY` /
`current_setting` search returned nothing). The planner writes the policy shape from RESEARCH;
the **migration file shape** copies the `aura.*` owner-scoped table analogs:

**Owner column analog** (`internal/db/migrations/0020_assets.up.sql:2-3`):
```sql
CREATE TABLE aura.assets (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
    ...
```
**Owner index analog** (`0020_assets.up.sql:48-49`): `CREATE INDEX assets_identity_created_idx ON aura.assets (identity_id, created_at DESC);`
**Role-grant analog** (`0020_assets.up.sql:68-71`): `GRANT SELECT, INSERT, UPDATE ON … TO aura_app;`

RLS policy the planner adds (per D-07, `USING (owner_id = current_setting('app.current_identity'))`)
is new SQL, but the migration wrapper/comment/grant style copies `0020`.

#### `internal/db/tx.go` (utility) — the `SET LOCAL` injection point

**Analog (self):** `WithTx` (`tx.go:22-39`) is the ONE DRY tx seam every multi-statement write reuses.
```go
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(*sqlc.Queries) error) (err error) {
    tx, err := pool.Begin(ctx)
    ...
    return fn(sqlc.New(tx))
}
```
**Delta (D-07):** inject `SET LOCAL app.current_identity = $1` right after `pool.Begin` using the
`identityctx.IdentityID(ctx)` principal, so RLS activates for every statement in the tx. This is the
single choke point — RLS backstops any forgotten `*ForIdentity` filter.

#### `*ForIdentity` store methods (store, CRUD) — the additive method-pair convention

**Analog (exact):** `internal/assets/store.go` is the canonical `*ForIdentity` template. Every method
takes `identityID` and passes it to the sqlc param struct:
```go
// store.go:56-73
func (s *Store) GetForIdentity(ctx context.Context, id, identityID string) (Asset, error) {
    pgID, err := pgUUID("asset id", id)
    ...
    pgIdentityID, err := pgUUID("identity_id", identityID)
    ...
    row, err := s.q.GetAssetForIdentity(ctx, sqlc.GetAssetForIdentityParams{
        ID: pgID, IdentityID: pgIdentityID,
    })
    ...
}
```
**Delta:** for every store touched (jobs/documents/skills/MCP config), add the `…ForIdentity` variant
alongside the base method, `local` (UUID `00000000-…-0001`) as the CLI/no-principal fallback (see the
`localIdentity` const, `internal/toolinvocations/store_integration_test.go:31-33`). The `pgUUID`/
`uuidString` helpers (`store.go:325-338`) are copy-ready. **Cross-identity semantics (D-06):** a
`GetForIdentity` miss = 404 (existence hidden); a known-foreign-id mutate = 403 (see Cluster F for
where the handler maps store-miss → status).

---

### Cluster C — Admin vs user: capability gating (D-01/D-02/D-03/D-25/D-26)

**The entire admin mechanism already exists** — this cluster is additive wiring, not new machinery.

#### `internal/agui/auth.go` (middleware) — the admin seam (READ, do not rewrite)

`HasCapability` is already an interface (`auth.go:35-38`) and every mutating route already calls
`RequireCapability` (`auth.go:275-292`):
```go
func RequireCapability(next http.Handler, deps AuthDeps, capability string) http.Handler {
    ...
    ok, err := deps.Identities.HasCapability(r.Context(), identityID, capability)
    if err != nil || !ok { http.Error(w, "forbidden", http.StatusForbidden); return }
    next.ServeHTTP(w, r)
}
```
This is why Casbin is a zero-rework later swap (D-04) — do NOT touch this contract.

#### `cmd/aura/serve_webui.go` (route mounts) — add the settings-write gate

**Analog (exact):** the existing capability mounts (`serve_webui.go:358,382-394`) interpose
`RequireCapability` on method+path-specific patterns (Go 1.22 longest-pattern precedence):
```go
mux.Handle("POST /agent/run", agui.RequireCapability(aguiHandler, auth, agentRunCapability))
mux.Handle(assetsPromoteRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
```
Capability name constants live at `serve_webui.go:99` (`agent.run`), `:114` (`governance.write`),
`:274` (`identity.create`).
**Delta (D-03):** add a `settingsModelWriteCapability = "settings.model.write"` const and mount
`PUT /api/settings/{key}` + `DELETE /api/settings/{key}` behind `RequireCapability(…, settingsModelWriteCapability)`.
Discretion (D-03): `GET /api/settings` stays `governance.read` or read-for-all — planner's call.

#### `internal/agui/settings_api.go` (controller) — routes already shaped

`registerSettingsRoutes` (`settings_api.go:59-66`) already declares the PUT/DELETE routes and
`handlePutSetting`/`handleDeleteSetting` already read the principal (`principalIdentityID(r)`,
`settings_api.go:156,192`). No handler change — only the parent-mux gate in `serve_webui.go`.
Note (D-02): `POST /api/settings/telegram/link` (`settings_api.go:62`) is a **self-scoped USER** action
— it must NOT get the admin gate; it already resolves `requester` from the principal (`:283-288`).

#### migration `0028_local_admin_caps` (migration, seed) — seed `local` admin caps (D-25)

**Analog (exact):** the `0004_identity.up.sql:30-32` idempotent grant seed:
```sql
INSERT INTO aura.capability_grants (identity_id, capability)
    VALUES ('00000000-0000-0000-0000-000000000001', '*')
    ON CONFLICT DO NOTHING;
```
`local` already holds `*` (which `HasCapability` treats as all-caps, `identity/store.go:125-138`), so
the named grants may be redundant — but D-25 wants them explicit (`settings.model.write` +
`identity.create` + `governance.write`). Same `INSERT … ON CONFLICT DO NOTHING` shape.

#### break-glass / grant-revoke CLI (`cmd/aura/identity.go`) — D-16/D-26

**Analog (exact):** `cmd/aura/identity.go` is a hand-rolled switch dispatcher (NOT cobra — `identity.go:1-9`)
and **already ships `grant`/`revoke`** (`identity.go:98-124`):
```go
func identityGrant(ctx context.Context, store *identity.Store, args []string) {
    name, capability := identityNameCap(args)
    id, err := store.GetIdentityByName(ctx, name)
    ...
    if err := store.GrantCapability(ctx, id.ID, capability); err != nil { ... }
    fmt.Printf("ok: granted %q to %s\n", capability, name)
}
```
**Delta (D-26):** grant/revoke exist; add the audited Settings-page control (a new admin-gated route,
copy `settings_api.go` handler shape) that calls the same `GrantCapability`/`RevokeCapability`
(`identity/store.go:165-192`).
**Delta (D-16 break-glass):** add a `case "recover"` branch minting a short-lived reset token on the
host. The token table already exists — `aura.password_reset_tokens` (`0023_identity_recovery.up.sql:27-40`,
`token_hash PK`, `expires_at`, `max_attempts`). Reuse it; host access = proof of ownership. The
self-service Telegram reset in `cmd/aura/serve_password_reset.go` is the online counterpart to diff against.

---

### Cluster D — Provisioning & de-provisioning sagas (D-14/D-27)

#### `internal/agui/onboarding_provision.go` (service, saga) — extend the shipped saga

**Analog (exact, this IS the saga):** `Provision` (`onboarding_provision.go:126-277`) is already an
ordered cross-store saga with per-leg compensation across FOUR stores that cannot share a tx:
```go
// Leg B (Authula, fails cheapest): CreateUser → CreateAccount; COMP_B = DeleteUser
user, err := s.authula.CreateUser(ctx, identityName)          // :172
compB := func() { s.authula.DeleteUser(context.WithoutCancel(ctx), user.ID) }  // :176-180
// Leg A (aura, one db.WithTx): INSERT identity + grants + LinkOperator
identityID, err := s.auraLeg.CreateIdentityWithGrants(ctx, AuraLegParams{...}) // :187
if err != nil { compB(); ... }                                 // :193-199
// Leg C (Telegram mint) → COMP_C then COMP_A then COMP_B on failure // :218-233
// Audit row (tiny final tx, only on success)                  // :235-252
```
Each leg is idempotent-friendly (`ON CONFLICT`, `GetByEmail` pre-check `:159-165`) and compensations
use `context.WithoutCancel(ctx)` so cleanup survives a cancelled parent.
**Delta (D-14):** add three legs before the audit row — **Garage bucket + scoped key** (Cluster G),
**per-identity MCP-config & skills dirs** (Cluster E), **empty `Agent.md`** (`profiles.WriteProfile`,
already called at `:264,374`). Default capability grants become `agent.run` + self-Telegram (NOT admin
caps) — enforced by `validateNoEscalation` (`:442-471`) which already rejects `*` and any cap the
creator lacks. Journal each step (D-14 "journaled/resumable") — see `0029_saga_journal`.

#### new de-provisioning saga (service, saga) — symmetric to Provision (D-27)

**Analog (role-match):** mirror `Provision`'s leg+compensation structure in reverse. Steps: deactivate
(block login/kill sessions/terminate jobs) → grace window → scheduled purge saga (conversations, docs
+ `:User` edges, Garage bucket, memory node/edges, MCP/skills dirs, grants, Authula user). The
`AuraLegWriter.DeleteIdentity` (`onboarding_provision.go:101-105`) already cascades grants+link via FK
(`0004_identity.up.sql:14` `ON DELETE CASCADE`). The scheduled runner analog is
`internal/cron` + `internal/conversations/sweeper.go`.

#### migration `0029_saga_journal.up.sql` (migration) — resumable journal

**Analog (role-match):** the append-only audit table shape (`0021_identity_audit.up.sql:15-59`) is the
closest — a durable per-step ledger. Discretion (CONTEXT): new `aura.*` table vs outbox — planner's call.
If append-only, copy the trigger+grant pattern verbatim:
```sql
GRANT SELECT, INSERT ON aura.identity_audit TO aura_app;   -- :58 (no UPDATE/DELETE)
CREATE TRIGGER identity_audit_no_update_delete BEFORE UPDATE OR DELETE ...  -- :45-47
CREATE TRIGGER identity_audit_no_truncate BEFORE TRUNCATE ... FOR EACH STATEMENT ...  -- :51-53
```
NOTE: a resumable saga journal needs status transitions (step pending→done), so it likely needs
`UPDATE` — meaning it is NOT append-only. Use the `0023` mutable-with-audit split instead (mutable
`password_reset_challenges` `0023:10-25` + append-only `identity_recovery_audit` `0023:42-50`).

#### migration `0030_identity_soft_delete` (migration) — soft-delete columns (D-27)

**Analog (exact):** the add-column-to-existing-table delta (`0014_pending_notifications_identity.up.sql:4-7`):
```sql
ALTER TABLE aura.pending_notifications ADD COLUMN identity_id text;
COMMENT ON COLUMN ... IS '...';
-- The existing aura_app DML grant already covers the new column — no new GRANT.
```
Delta: `ALTER TABLE aura.identities ADD COLUMN deactivated_at timestamptz` (+ purge-after-grace column).
`deleted_at` precedent: `0020_assets.up.sql:33`.

---

### Cluster E — Per-identity MCP config + skills filesystem rooting (D-20/D-21)

#### per-identity dir rooting — `internal/profile/store.go` is the traversal-safe analog

**Analog (exact, critical — path-traversal safety):** `profileDir` (`profile/store.go:201-222`) is the
established per-identity filesystem rooting with containment checks:
```go
func (s *Store) profileDir(identity string) (string, error) {
    if !identityPattern.MatchString(identity) || strings.Contains(identity, "..") ||
        strings.ContainsAny(identity, `/\`) {
        return "", fmt.Errorf("%w: %q must match %s and contain no traversal", ErrInvalidIdentity, ...)
    }
    root, _ := filepath.Abs(s.root)
    dir, _  := filepath.Abs(filepath.Join(root, identity))
    rel, _  := filepath.Rel(root, dir)
    if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
        return "", fmt.Errorf("%w: %q escapes profile root", ErrInvalidIdentity, identity)
    }
    return dir, nil
}
```
The existing per-identity root convention: `~/.aura/agents` (`profile/store.go:85`), `AURA_PROFILE_DIR`
(`config.go:206`). **Delta (D-20/D-21):** add `~/.aura/mcp/{id}/servers.json` and `$AURA_SKILLS_DIR/{id}/`
+ `~/.aura/pyscripts/{id}/` roots using the identical `filepath.Join(root, identity)` + containment guard.
Never `filepath.Join` an untrusted identity without this guard.

#### `internal/mcp/managed_config.go` (config, file-I/O) — per-identity MCP config

**Analog (role-match):** `ManagedConfig` (`managed_config.go:32-70`) is the durable MCP registry
(Claude-Desktop `mcpServers` shape + Aura `enabled/trust/source` metadata). Trust classes at `:21-25`.
**Delta (D-20):** shared catalog stays read-only; per-identity `servers.json` under the rooted dir holds
per-identity enable/trust for class-(a) stdio servers. The class-(b) shared agent-memory server (`:8091`)
is admin-governed only — users can't toggle it. Identity-key `mcp_audit` (see Cluster H).

#### per-identity skills (service, file-I/O) — `newSkillToolForIdentity(ctx)` (D-21)

**Analog:** `internal/skills/builtin.go` (built-in materialization) + `config.go:389,545-546`
(`SkillsDir` default `~/.aura/skills`). Additive `*ForIdentity` (Cluster B convention) + `local`
fallback; built-ins shared read-only, per-identity dir for user skills. Skill **execution** isolation
is Phase 37 — this phase is **storage rooting only** (D-21 note, in-scope §domain).

---

### Cluster F — Background jobs owner-binding + TTL (D-17/D-18)

#### `internal/agent/tools/shell_bg.go` (tool, event-driven)

**Analog (self):** `BackgroundShells` (`shell_bg.go:30-41`) is the process-scoped registry; `start`
already sets a process GROUP and a cancel (`shell_bg.go:147-188`):
```go
setProcessGroup(cmd)                          // :153
cmd.Cancel = func() error { return killProcessGroup(cmd) }  // :154
cmd.WaitDelay = 5 * time.Second               // :155
```
`Shutdown` (`:259-306`) terminates the group and drains; `Evict` (`:218-225`) reclaims finished shells.
IDs today are sequential `sh_%d` (`:169-170`).
**Delta (D-17):** replace `sh_%d` with random unguessable IDs (`uuid.NewString()` per
`onboarding_provision.go:221`), bind each shell to `(identity, session/actor)` (store on `bgShell`),
add `default TTL = 1h` (env-overridable via the existing `shellBackgroundBufCap`/`Max` env pattern
`:428-455`) — on expiry record status + call `killProcessGroup` (the `Shutdown` cancel path already
does the terminate).
**Delta (D-18):** `shell_poll`/`shell_kill` (`:346,458`) authority = owner session/actor OR an admin
capability (cross-session recovery) — gate via `HasCapability` (Cluster C). Cross-identity poll of a
foreign job = 404; admin-cap poll allowed.

---

### Cluster G — Garage bucket-per-identity (D-08) — **partial analog only**

#### `internal/objectstore/s3.go` (service, file-I/O)

**Analog (partial):** `S3Store` (`s3.go:28-66`) wraps the AWS SDK v2 S3 client against Garage
(`region "garage"`, `:38-41`) with Put/Head/Get/List/Delete/PresignPut and `ConfigureBrowserUploadCORS`
(`:107-116`). **There is NO bucket-create or scoped-key analog** — today all assets share one bucket
(assets carry `object_bucket`/`object_key`, `0020_assets.up.sql:19-20`).
**Delta (D-08):** add `CreateBucketForIdentity` + Garage scoped-key creation (grants are per-bucket NOT
per-prefix, F-007). The `garage key create` CLI/admin-API call is a new provisioning-saga leg
(Cluster D). Bucket naming derives from identity id (apply the Cluster E traversal/charset guard to any
identity→bucket-name mapping). The planner should pull the Garage admin-API pattern from RESEARCH /
spike-findings `multiuser-per-identity-isolation.md` — no in-repo analog exists.

---

### Cluster H — Admin audit visibility (D-28)

#### new admin audit UI + backing API (controller, request-response)

**Analog — audit table shape (exact):** three append-only identity-keyed audit tables already exist to
model the new reads on: `identity_audit` (`0021`), `mcp_audit` (`0022`), `skill_audit` (`0010`),
`tool_invocations` (`0011`). The `mcp_audit` shape (`0022_mcp_audit.up.sql:15-26`):
```sql
CREATE TABLE aura.mcp_audit (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at timestamptz NOT NULL DEFAULT now(),
    actor_identity_id text NOT NULL,   -- the capability-layer principal
    action text NOT NULL,
    server_name text NOT NULL,
    reason text
);
CREATE INDEX mcp_audit_created_at_idx ON aura.mcp_audit (created_at DESC);  -- :31
```
**Delta (D-28):** these tables key on `actor_identity_id` already — add per-user query methods + an
admin-gated read API. **Backing-read controller analog (exact):** `settings_api.go:88-139`
`handleListSettings` (read → DTO projection → `writeJSON`), gated at the parent mux with
`RequireCapability` (Cluster C). SanitizeString any user text before it crosses the wire
(`approvals_api.go:86` precedent). Some tables lack an identity index (`mcp_audit` indexes `server_name`
not actor) — add `(actor_identity_id, created_at DESC)` indexes (the `assets_identity_created_idx`
shape, `0020:48-49`).

---

### Cluster I — Telegram multi-user routing (D-24)

#### `internal/channels/telegram/bot_dispatch_auth.go` + `bot_dispatch_turn.go` (controller, event-driven)

**Analog (exact):** the reject-unlinked gate already exists (`bot_dispatch_auth.go:80-100`):
```go
func (t *Telegram) telegramUserIsLinked(ctx context.Context, telegramUserID int64) bool {
    accounts := t.accountsForDispatch()
    ...
    if _, err := accounts.GetAccountByTelegramID(ctx, telegramUserID); err != nil {
        slog.Info("telegram auth: rejected unlinked sender", ...); return false  // :90-93
    }
    ...
}
```
And the turn already resolves identity from the account (`bot_dispatch_turn.go:52-53`):
```go
if account, err := t.linkedAccountForMessage(ctx, msg); err == nil {
    identityID = account.IdentityID
}
```
**Delta (D-24):** this is already multi-user-shaped — `GetAccountByTelegramID` → `account.IdentityID`
is per-user, not operator-pinned. The remaining work: (1) generalize
`IdentityLinker.LinkOperator` → `LinkUser` (see below) so provisioning binds any user's chat-id;
(2) unknown chat-id → reject + point to **web** linking (the `activationRequiredMsg` at `:11` already
does this); (3) wire `identityctx.WithIdentityID(ctx, account.IdentityID)` into the turn context so
downstream stores scope correctly (D-23). Reuse `telebot.v4` — no framework swap.

#### `internal/webauth/identity_link.go` (service, CRUD) — generalize LinkOperator (D-24)

**Analog (exact, this IS the generalization target):** `LinkOperator` (`identity_link.go:56-72`):
```go
func (l *IdentityLinker) LinkOperator(ctx context.Context, identityID, authulaUserID string) error {
    const q = `
        INSERT INTO aura.identity_auth_links (identity_id, authula_user_id)
        VALUES ($1::uuid, $2)
        ON CONFLICT (authula_user_id) DO UPDATE SET identity_id = EXCLUDED.identity_id`
    ...
}
```
The table is **already 1:N-ready** — the UNIQUE is on `authula_user_id`, NOT `identity_id`
(`identity_link.go:9-11` comment). The `ResolveIdentityID` request-path (`:41-54`) already works
multi-user. **Delta:** rename/add `LinkUser(ctx, identityID, authulaUserID)` (same body) and call it
from the provisioning saga's Leg A (`onboarding_provision.go` `CreateIdentityWithGrants` already calls
`LinkOperator` internally per `:96-98` comment). D-11: the operator's existing `local` link is untouched.

#### `internal/config/config.go` (config) — `AURA_MUSR_ISOLATION` rollout flag (D-13)

**Analog (exact):** the env-default pattern (`config.go:389`): `SkillsDir: envDefault("AURA_SKILLS_DIR", defaultSkillsDir())`.
**Delta:** add `MUSRIsolation bool` read via the existing bool-env helper; it gates the fail-closed flip
across all retrieval/query paths (Cluster A) so scoping code deploys flag-off, backfill runs, then flips on.

---

### Cluster J — Two-identity live E2E acceptance gate (D-29)

#### new `*_integration_test.go` (test, request-response)

**Analog (exact — combined two-substrate build tags):** `internal/knowledge/graphview_integration_test.go:1-18`:
```go
//go:build neo4j_integration && db_integration

// ... Run via:
//   make neo4j-migrate && go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/ -run TestGraphViewLive
// No-skip-as-green: testConfig/envOrSkipCI t.Fatal under $CI when their env is unset.
```
**Analog (exact — envOrSkip + composed DSN):** `internal/toolinvocations/store_integration_test.go:37-62`:
```go
func envOrSkip(t *testing.T, key string) string {
    v := os.Getenv(key)
    if v == "" {
        if os.Getenv("CI") != "" {
            t.Fatalf("integration test requires %s, but it is unset under CI — "+
                "a skipped integration test must not pass as green; wire it in ci.yml", key)
        }
        t.Skipf("integration test requires %s; ...", key)
    }
    return v
}
// bootstrapURL composes the superuser DSN from POSTGRES_PASSWORD (:51-62)
// migratedPool ensures roles+migrations then returns an aura_app pool (:66)
```
**Cross-deny assertion analog:** `memory_integration_test.go` `TestMemoryLiveScopedGraphToolsAcceptUserIdentifier`
(write as `uid` → `fact_count:1`; read as `uid+"-other"` → `fact_count:0`).
**Delta (D-29):** add `garage_integration` + `authula` to the tag set (or fold into the existing combined
tag); the CI stack gains Garage + Authula alongside Postgres + Neo4j. The E2E: provision identity A + B,
A uploads a doc + writes a conversation + starts a bg job, assert B gets 404/403 on all of A's resources
and empty document_search. GATES on Linux CI (t.Fatal under `$CI`); local may skip on RAM. **Reuse the
build-tag harness — NOT testcontainers** (D-29, would fork the CI model).

---

## Shared Patterns

### The principal — `identityctx.IdentityID(ctx)`
**Source:** `internal/identityctx/identityctx.go:18-21`
**Apply to:** every plane (documents, jobs, stores, MCP, skills, Telegram, RLS SET LOCAL)
```go
func IdentityID(ctx context.Context) string { id, _ := ctx.Value(key{}).(string); return id }
```
The auth boundary stashes it via `withPrincipal` (`agui/auth.go:301-305`, sets BOTH `principalKey{}` and
`identityctx.WithIdentityID`). `""` = CLI/no-principal → resolves to `local` fallback. `principalFrom`
(`auth.go:310-317`) reads it back. Any new scoped path reads `IdentityID(ctx)`; empty MUST fail closed
on graph retrieval (D-09).

### Additive `*ForIdentity` + `local` fallback
**Source:** `internal/assets/store.go:56-73` (method-pair), `toolinvocations/store_integration_test.go:31-33` (`localIdentity` const)
**Apply to:** every store touched (jobs, documents, skills, MCP config)
Base method stays; add `…ForIdentity(ctx, id, identityID)` passing `identity_id` to the sqlc params.
`local` = `00000000-0000-0000-0000-000000000001`.

### Capability gate — `RequireCapability` / `HasCapability` (interface, zero-rework for Casbin)
**Source:** `internal/agui/auth.go:275-292` (gate), `:35-38` (interface); mounts `cmd/aura/serve_webui.go:358`
**Apply to:** all admin-gated write routes (settings.model.write, identity.create, governance.write)
Interface `HasCapability(ctx, id, cap) (bool, error)` — `*` wildcard = all caps (`identity/store.go:125-138`).

### Cross-store saga with per-leg compensation
**Source:** `internal/agui/onboarding_provision.go:126-277`
**Apply to:** provisioning (D-14) + de-provisioning (D-27)
Ordered legs, cheapest-failure first; `compX()` closures using `context.WithoutCancel(ctx)`;
idempotent steps (`ON CONFLICT`/pre-check); one final audit row only on success.

### Append-only audit table (identity-keyed)
**Source:** `internal/db/migrations/0021_identity_audit.up.sql:36-59` (fn + row trigger + statement trigger + split grant)
**Apply to:** mcp_audit / skill_audit / tool_invocations identity-keying (D-28); saga journal (D-14, if append-only)
`GRANT SELECT, INSERT` only; row trigger for UPDATE/DELETE; **separate** statement trigger for TRUNCATE
(a row trigger never fires for TRUNCATE — Pitfall). Written inside the mutation's `db.WithTx`.

### Owner-scoped `aura.*` table + index + grant
**Source:** `internal/db/migrations/0020_assets.up.sql:2-3,48-49,68-71`
**Apply to:** every new owner-scoped table + `0027_owner_id_rls`
`identity_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE`; `(identity_id, created_at DESC)`
index; `GRANT SELECT, INSERT, UPDATE … TO aura_app` + `GRANT ALL … TO aura_migrate`.

### Path-traversal-safe per-identity dir rooting
**Source:** `internal/profile/store.go:201-222`
**Apply to:** `~/.aura/mcp/{id}`, `$AURA_SKILLS_DIR/{id}`, `~/.aura/pyscripts/{id}`, Garage bucket naming
`identityPattern` charset check + `..`/slash reject + `filepath.Rel` containment assertion. Never join an
untrusted identity into a path without this guard.

### Live-stack integration harness (build tags + composed DSN + no-skip-as-green)
**Source:** `internal/knowledge/graphview_integration_test.go:1-18` (combined tags), `internal/toolinvocations/store_integration_test.go:37-62` (envOrSkip + DSN compose)
**Apply to:** the two-identity E2E (D-29)
`//go:build … && …`; `envOrSkip` t.Fatal under `$CI`; compose DSN from `POSTGRES_PASSWORD`; inherit the
package goleak `TestMain` (don't add a second). Reuse — never testcontainers.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `0027_owner_id_rls.up.sql` (the RLS **policy** SQL) | migration | CRUD | Zero existing RLS in the codebase (`CREATE POLICY`/`current_setting`/`SET LOCAL` all absent). Migration *file* shape copies `0020_assets`; the `USING (owner_id = current_setting('app.current_identity'))` policy + `SET LOCAL` in `WithTx` come from RESEARCH/spike-findings. |
| Garage `CreateBucketForIdentity` + scoped-key (D-08) | service | file-I/O | `internal/objectstore/s3.go` has object CRUD + presign + CORS but NO bucket-create or key-provisioning. The `garage key create` admin-API pattern must come from RESEARCH / `spike-findings-Aura/references/multiuser-per-identity-isolation.md`. |

---

## Metadata

**Analog search scope:** `internal/{documents,agui,webauth,identity,identityctx,assets,objectstore,mcp,skills,profile,agent/tools,agent/mcptools,channels/telegram,db,knowledge,config,conversations,toolinvocations}`, `cmd/aura`, `internal/db/migrations/0001-0025`, git `9a4ca594`, `docker/agent-memory/.../graph/queries.py`
**Files scanned:** ~35 source files + 6 migrations + 1 git commit diff
**Pattern extraction date:** 2026-07-05
