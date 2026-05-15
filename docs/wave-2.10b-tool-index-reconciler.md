# Wave 2.10.b — Tool Index Reconciler (Hash-Based, Cache-Aware)

> **Status: SHIPPED 2026-05-13 in commit `2367f502`. Plan preserved as evidence; implementation is in `internal/agent/tools/index/`.**

**Status:** plan (not implemented)
**Date drafted:** 2026-05-13
**Predecessor:** Wave 2.10 (install bootstrap, commit `eb7e61ad`)
**Successor:** Wave 2.10.c (MCP `tools/list_changed` notification + mid-runtime MCP reload — deferred)

---

## 1. Goal

Keep the Qdrant `aura_tool_search_v2` collection in sync with the live tool registry **without restart**. Today the index is built once at boot via `Registry.BuildVectorIndex()` ([registry_search_vector.go:124-221](internal/tools/registry_search_vector.go#L124-L221)) and becomes stale the moment a tool description, schema, or skill manifest changes.

After Wave 2.10.b, every change source triggers a **hash-based reconcile**: SHA-256 over canonical JSON of `(name, description, input_schema, embed_model)` → diff wanted vs indexed → upsert only the changed/added, delete only the removed, leave the rest untouched. Re-embedding is gated on the existing `embed_cache` SQLite table so unchanged tools cost zero embedding API calls on every reconcile.

---

## 2. Scope decision

### In scope

1. **`internal/toolindex/`** new package: `Reconciler` type + content-hash function + SQLite state table CRUD + comprehensive tests.
2. **`tool_index_state` SQLite table** — migration adds `(tool_name, content_hash, point_id, embed_model, indexed_at)`. This is the "indexed set" the Reconciler diffs against, because Qdrant has no Scroll API (audit confirmed: [qdrant/client.go:18-26](internal/qdrant/client.go#L18-L26) only exposes Search/Upsert/Delete/CollectionInfo).
3. **Replace boot-time `BuildVectorIndex`** with `Reconciler.Reconcile()`. Same warm-cache semantics, plus per-tool diff: no re-embed on no-op.
4. **Triggers wired**:
   - Boot completion (synchronous Reconcile before agent loop)
   - Long-lived goroutine `Reconciler.Run()` drains a notify channel with debounce
   - fsnotify on `mcp.json` parent dir → Notify (debounced 500 ms)
   - Hook on `/api/skills/install` and `/api/skills/delete` post-success → Notify
   - Periodic safety-net `time.Ticker` every 10 min
   - `POST /api/tools/reindex` — synchronous Reconcile for debug + dashboard
5. **`Registry.Unregister(name string)`** — needed for symmetric cleanup; today only Register exists.

### Out of scope (explicit defer)

| Item | Why deferred | Where it lands |
|------|-------------|----------------|
| MCP `notifications/tools/list_changed` subscription | Requires non-trivial transport-layer rewrite (filter inbound notifications by missing `id`, dispatch table). Audit confirmed [mcp/client.go](internal/mcp/client.go) is strictly request-response today. | Wave 2.10.c |
| Mid-runtime MCP server reload on `mcp.json` edit | Needs a new `internal/mcp.Manager` that owns the client set and can connect/disconnect on diff. fsnotify in 2.10.b only Notifies the Reconciler — useful only when the registry actually changes (today: never, because MCP tools are boot-only). | Wave 2.10.c |
| React dashboard for reindex status | Wave 2.11 territory. The new `POST /api/tools/reindex` endpoint is enough for CLI debug today. | Wave 2.11 |

Honest scope statement: **2.10.b alone does not unlock "edit mcp.json → new tools appear without restart"**. It unlocks "edit a tool's description in code → next reconcile re-embeds only that tool, not all 60". The full "auto-update on system change" the user originally asked for needs 2.10.b + 2.10.c together, but 2.10.b is the foundation: 2.10.c is mostly plumbing on top.

---

## 3. Architecture

### 3.1 Content hash

```go
// internal/toolindex/hash.go
package toolindex

// IndexableTool is the minimum surface needed to compute a content hash and
// build the embedding text. Decoupled from internal/tools so the package
// stays testable without the full Registry.
type IndexableTool struct {
    Name        string
    Description string
    InputSchema map[string]any
}

// ContentHash returns a stable lowercase-hex SHA-256 over a canonical JSON
// representation of the tool plus the embedding model identifier. The model
// is part of the hash because re-running with a different embedder must
// invalidate every cached embedding (different vector space).
func ContentHash(t IndexableTool, embedModel string) (string, error) {
    payload, err := canonicalJSON(map[string]any{
        "name":         t.Name,
        "description":  t.Description,
        "input_schema": t.InputSchema,
        "embed_model":  embedModel,
    })
    if err != nil {
        return "", err
    }
    sum := sha256.Sum256(payload)
    return hex.EncodeToString(sum[:]), nil
}
```

`canonicalJSON` sorts every map's keys recursively before marshaling and uses `encoding/json` with no whitespace. This is the standard "canonical JSON" recipe — same bytes everywhere regardless of map iteration order.

### 3.2 SQLite state table

Migration `migrations/0014_tool_index_state.sql`:

```sql
CREATE TABLE IF NOT EXISTS tool_index_state (
    tool_name    TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    point_id     TEXT NOT NULL,     -- uuidv5(namespace, tool_name) — stable
    embed_model  TEXT NOT NULL,     -- e.g. "embeddinggemma:256d"
    indexed_at   TEXT NOT NULL      -- RFC3339 UTC
);
CREATE INDEX IF NOT EXISTS idx_tool_index_state_hash ON tool_index_state(content_hash);
```

Tiny table (60 rows today), one txn per Reconcile.

### 3.3 Reconciler

```go
// internal/toolindex/reconciler.go
type Reconciler struct {
    provider    ToolProvider        // .Definitions() → []llm.ToolDefinition
    qdrant      QdrantClient        // .Upsert / .Delete / .CollectionInfo
    embedder    Embedder            // .Embed([]string) → [][]float32
    embedCache  EmbedCache          // .Get(text) (vec, ok)  / .Put(text, vec)
    state       StateStore          // SQLite CRUD over tool_index_state
    collection  string              // "aura_tool_search_v2"
    embedModel  string              // namespace tag for hash + cache
    vectorDim   int                 // 256
    logger      *slog.Logger

    notify chan Reason              // buffered 16
    debounce time.Duration          // 500 ms default
    periodic time.Duration          // 10 min default
}

type Reason string
const (
    ReasonBoot        Reason = "boot"
    ReasonSkillsChanged Reason = "skills_changed"
    ReasonMCPConfig   Reason = "mcp_config_changed"
    ReasonManual      Reason = "manual"
    ReasonPeriodic    Reason = "periodic"
)

type Report struct {
    Reason    Reason
    Upserted  []string
    Deleted   []string
    Unchanged int
    Errors    []string
    Elapsed   time.Duration
}

// Reconcile is the core synchronous operation. Safe to call concurrently —
// internal mutex serializes; concurrent callers see the same report value.
func (r *Reconciler) Reconcile(ctx context.Context, reason Reason) Report

// Notify enqueues a reconcile request. The Run() goroutine merges
// notifications within the debounce window so a burst of skill installs
// triggers exactly one Reconcile.
func (r *Reconciler) Notify(reason Reason)

// Run is the long-lived goroutine started at boot. Combines debounce of
// Notify channel + periodic ticker. Stops on ctx.Done().
func (r *Reconciler) Run(ctx context.Context)
```

### 3.4 Reconcile algorithm

```
1. Snapshot wanted = provider.Definitions()
   Each definition → IndexableTool{Name, Description, InputSchema}
   Build wantedMap: tool_name → (content_hash, IndexableTool)

2. Snapshot indexed = state.LoadAll()
   indexedMap: tool_name → state row

3. Diff:
   For each wanted_name:
     if indexed has same hash → unchanged, skip
     else → upsert (new or changed)
   For each indexed_name not in wanted → delete

4. Upsert pass (batch):
   For each upsert tool:
     content_text = searchableToolEmbeddingText(def)  // existing helper
     content_sha = embed_cache.contentSHA(content_text)
     If embed_cache.Get(content_text, namespace) → reuse vector
     Else → embedder.Embed([content_text]) → vector; embed_cache.Put
     Point{ID: uuidv5("aura-tools", tool_name), Vector, Payload: {name, description, content_hash}}
   qdrant.Upsert(collection, points)  // single batched call

5. Delete pass (batch):
   point_ids = [state row.point_id for each removed tool_name]
   qdrant.Delete(collection, point_ids)

6. State update pass (single SQLite txn):
   For each upserted: state.Set(tool_name, content_hash, point_id, embed_model, now)
   For each deleted: state.Remove(tool_name)

7. Return Report{...}
```

Idempotency: re-running with no input changes results in `Upserted=0, Deleted=0, Unchanged=60` and zero embedding/Qdrant calls.

### 3.5 Triggers wiring

| Trigger | Code path | Debounce |
|---------|-----------|----------|
| Boot | `setup.go` calls `Reconciler.Reconcile(ctx, ReasonBoot)` synchronously, then `go reconciler.Run(ctx)` | none |
| `mcp.json` change | New `internal/mcpwatch/` package: `fsnotify` parent dir, filter by name, call `reconciler.Notify(ReasonMCPConfig)` | 500 ms |
| Skill install | `internal/api/skills_write.go:handleSkillInstall` post-success → `deps.ToolReconciler.Notify(ReasonSkillsChanged)` | 500 ms |
| Skill delete | same, `handleSkillDelete` | 500 ms |
| Manual | `POST /api/tools/reindex` handler calls `Reconcile()` synchronously, returns Report as JSON | none (sync) |
| Periodic | `Reconciler.Run()` has `time.NewTicker(10 * time.Minute)` → Notify | none |

### 3.6 New API endpoint

```
POST /api/tools/reindex
Authorization: Bearer <token>

Response 200:
{
  "reason": "manual",
  "upserted": ["search_memory", "doc"],
  "deleted": [],
  "unchanged": 58,
  "elapsed_ms": 1240,
  "errors": []
}
```

Bearer-gated. Useful when the dashboard wants to expose "refresh tool index" or for debugging post-deploy drift.

---

## 4. File-by-file changes

### New files

| Path | Purpose |
|------|---------|
| `internal/toolindex/hash.go` | `ContentHash` + canonical JSON |
| `internal/toolindex/hash_test.go` | Determinism, schema canonicalization, embed_model field sensitivity |
| `internal/toolindex/reconciler.go` | `Reconciler` type + Reconcile/Notify/Run |
| `internal/toolindex/reconciler_test.go` | Diff scenarios (add/remove/change/no-op), mocked Qdrant + embedder |
| `internal/toolindex/state.go` | SQLite CRUD on `tool_index_state` |
| `internal/toolindex/state_test.go` | LoadAll/Set/Remove + concurrent safety |
| `internal/toolindex/types.go` | Interface boundaries (ToolProvider, QdrantClient, Embedder, EmbedCache, StateStore) |
| `internal/mcpwatch/watcher.go` | fsnotify on `mcp.json` with debounce |
| `internal/mcpwatch/watcher_test.go` | Event coalescing, debounce window |
| `internal/db/migrations/0014_tool_index_state.sql` | Schema |

### Modified files

| Path | Change |
|------|--------|
| `internal/tools/registry.go` | Add `Unregister(name string)`. Keep `BuildVectorIndex` as a deprecated shim that calls Reconcile under the hood for one cycle, then remove in 2.10.c. |
| `internal/telegram/setup.go` | Build Reconciler, call boot Reconcile, spawn Run goroutine, spawn mcpwatch goroutine, pass Reconciler to `api.Deps` |
| `internal/api/router.go` | Add `Reconciler` field to `Deps`. Add `POST /tools/reindex` route. |
| `internal/api/tools_reindex.go` (new) | Handler |
| `internal/api/skills_write.go` | Call `deps.Reconciler.Notify(ReasonSkillsChanged)` on success paths |
| `cmd/aura/main.go` | Nothing structural — Reconciler is set up inside `telegram/setup.go` |

### go.mod

Add `github.com/fsnotify/fsnotify`. Already used by several Aura deps transitively; pin to v1.7+.

---

## 5. Tests

### Unit (`internal/toolindex/*_test.go`)

- **`TestContentHash_Deterministic`**: same input → same hash, 1000 iterations. Map key order doesn't matter.
- **`TestContentHash_FieldSensitivity`**: change name/description/schema/embed_model individually → hash changes each time.
- **`TestContentHash_SchemaCanonicalization`**: `{"properties": {"a": ..., "b": ...}}` vs `{"properties": {"b": ..., "a": ...}}` → same hash.
- **`TestReconcile_FreshBoot`**: empty state + N wanted tools → all upserted, zero unchanged.
- **`TestReconcile_NoChange`**: state matches wanted exactly → zero upserted, zero deleted, N unchanged, **zero embedding calls** (assert via mock embedder counter).
- **`TestReconcile_DescriptionChange`**: 1 tool description changed → exactly 1 upsert + 1 embedding call, N-1 unchanged.
- **`TestReconcile_ToolRemoved`**: 1 tool removed from wanted → 1 delete, state row removed.
- **`TestReconcile_ToolAdded`**: 1 new tool → 1 upsert + 1 embedding call.
- **`TestReconcile_EmbedCacheHit`**: same hash as before but state was wiped → 1 upsert but **zero embedder calls** (cache hit on contentSHA).
- **`TestReconcile_EmbeddingFailure_NoStateMutation`**: embedder returns error → no state row written; next run retries.
- **`TestNotify_Debounce`**: 10 rapid `Notify()` calls within 100 ms → exactly 1 Reconcile inside Run() (debounce window 500 ms).

### Integration (`internal/toolindex/reconciler_integration_test.go`)

- Spin up a real SQLite (TempDir), a httptest Qdrant mock, a httptest embedding mock, and a fake `ToolProvider`. Run a full sequence: boot → add tool → reconcile → change description → reconcile → remove tool → reconcile. Assert exact embedding-call and Qdrant-call counts at each step.

### Live verify

After implementation:
1. `docker compose up -d` (full stack)
2. `POST /api/tools/reindex` → confirm Report shows all 60 tools unchanged on second call.
3. Edit `internal/tools/wiki_page.go` description string in a comment-only nop way (rebuild Aura) → `POST /api/tools/reindex` → confirm `Upserted: [wiki_page]`, embedding-cache hit count.
4. Install a skill via dashboard → confirm Reconciler fires within 500 ms (log inspection).
5. `touch runtime-workspace/mcp.json` → confirm fsnotify fires + Reconcile runs.

---

## 6. Performance + cost analysis

Current state-of-affairs in production (60 tools, embedding via local llama-embed):

- Boot Reconcile (cold cache + fresh state): ~3-5 s = 60 embedder calls × ~30-80 ms each, parallelizable.
- Boot Reconcile (warm embed cache): ~0.5 s = 60 cache hits + 1 Qdrant upsert batch.
- No-op Reconcile (state matches wanted): ~50 ms = 60 SQLite reads + 1 hash compare per tool + zero network.
- Single-tool change Reconcile: ~100-200 ms = 1 embedder call + 1 Qdrant upsert + 1 SQLite txn.

Periodic 10-min safety-net cost: ~50 ms per tick. Negligible.

---

## 7. Done criteria

Wave 2.10.b ships when **all** of:

1. `internal/toolindex/` exists with: hash.go, reconciler.go, state.go, types.go + tests, ≥15 unit tests green
2. `internal/mcpwatch/` exists with fsnotify watcher + debounce + tests
3. Migration `0014_tool_index_state.sql` runs cleanly on existing DBs
4. `Registry.Unregister(name)` added; existing `Register/Tools/Search` paths still pass their tests
5. Boot path calls `Reconciler.Reconcile(ctx, ReasonBoot)` synchronously and spawns `Run()` goroutine
6. `POST /api/tools/reindex` returns Report JSON, Bearer-gated
7. Skill install/delete hook fires Notify on success
8. fsnotify on `mcp.json` triggers Notify (visible in logs)
9. `go test ./...` 47/47 pkg green (was 46 pre-2.10.b; +1 = toolindex)
10. Live verify: 5 scenarios from §5 pass
11. `docs/wave-2.10b-tool-index-reconciler.md` (this file) updated to "Status: shipped" + commit hash

---

## 8. Estimated effort

- `internal/toolindex/` package + tests: **1 day**
- Migration + state CRUD: **0.25 day**
- `internal/mcpwatch/` + tests: **0.25 day**
- Boot/API/hook wiring: **0.25 day**
- `Registry.Unregister` + plumbing: **0.1 day**
- Live verify + bug-fixing: **0.5 day**

Total: **~2.5 days** of focused work.

---

## 9. Open decisions (chiudere prima di iniziare)

1. **Hash includes `embed_model`?** Pro: changing embedder force-invalidates everything atomically. Con: the hash you read post-update doesn't match the hash you wrote pre-update — but that's the point. **Reco: yes, include it.**

2. **State row lifecycle on Reconcile error?** If embedder fails for tool X, do we leave the state row stale (last good) or remove it (force retry next reconcile)? **Reco: leave stale.** Next reconcile re-evaluates; if the tool's content is unchanged the next embedder call will retry the same text, which the cache will probably have served previously.

3. **`POST /api/tools/reindex` synchronous or async?** Sync simpler; async returns 202 + status endpoint. For ~60 tools cold, full reindex is <5 s. **Reco: sync.** Add async later if a 60-tool deployment becomes a 600-tool deployment.

4. **Should the Reconciler hold a global lock during the SQLite txn + Qdrant calls?** A second concurrent Reconcile (e.g. fsnotify and skill install in the same 500 ms) could collide. **Reco: yes — a single `sync.Mutex` in Reconciler serializes; the debounce logic in Run already merges most cases; concurrent direct callers (the manual endpoint) just queue.**
