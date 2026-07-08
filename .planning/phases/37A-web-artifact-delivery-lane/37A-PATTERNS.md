# Phase 37A: Web Artifact Delivery Lane - Pattern Map

**Mapped:** 2026-07-08
**Files analyzed:** 23 (9 backend prod + 5 frontend prod + a migration pair + 8 test files)
**Analogs found:** 21 with a concrete in-tree analog / 23 total (2 have no direct analog: the RFC-6266 helper → property-based-testing skill, the `sendFileAssetAdapter` → interface-satisfaction convention)

> RESEARCH.md already verified every anchor line-for-line (zero drift). This document pulls the **concrete excerpts the planner pastes into `<read_first>` / `<action>`** and **resolves the two Open Questions** the research handed off (Open Q1 frontend mapping = definitively resolved below; Open Q2 `Content-Length` = resolved).

---

## File Classification

| New/Modified File | New? | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|---|
| `internal/assets/ingest_agent.go` (rec. new; or add to `service.go`) | new | service | file-I/O → CRUD-create | `assets.Service.IngestTelegramFile` `service.go:257` | exact |
| `internal/assets/service.go` — add `OpenForIdentity` | mod | service | streaming read | `Service.Finalize` objectsFor+`hashAndSniff` `service.go:140,377` | role-match |
| `internal/assets/types.go` — `SourceAgent` + `AgentIngestRequest` | mod | model | — (const + struct) | `SourceTelegram` `:66` + `TelegramIngestRequest` `:129` | exact |
| `internal/agent/tools/send_file_ingest.go` (rec. new) — `AssetDeliverer` iface + ingest/degrade helper | new | tool + provider-seam | file-I/O / event-driven | `send_file_sandbox.go` (`<name>_<concern>.go` split precedent) | exact |
| `internal/agent/tools/send_file.go` — `Assets` field + ctx-aware `emitDelivery` + descriptor enrich | mod | tool (controller) | event-driven / transform | own `emitDelivery` `:123-148` | exact |
| `internal/agent/tools/send_file_sandbox.go` — pass ctx to `emitDelivery` | mod | tool | file-I/O | own `deliverFromBox` `:24-50` | exact |
| `internal/agui/assets_api.go` — `handleAssetDownload` + route | mod | route/controller | request-response → **streaming body** | `handleAssetGet` `assets_api.go:103` | exact |
| `internal/agui/content_disposition.go` (rec. new) — RFC-6266 helper | new | utility | pure transform | — (no analog; port LibreChat `files.js:67-88`) | none |
| `internal/agui/asset_service.go` — add `OpenForIdentity` to iface | mod | interface | — | existing `AssetService` `:10-22` | exact |
| `internal/db/migrations/0035_assets_source_kind_agent.{up,down}.sql` | new | migration | batch / DDL | `0034_scheduler_sandbox_reap_kind.{up,down}.sql` | exact |
| `cmd/aura/main.go` — `SendFile` on `runtimeToolHandles` + retain ptr | mod | config (composition) | — (wiring) | `ShellPoll/ShellKill` handles `main.go:114-123,181-186` | exact |
| `cmd/aura/serve.go` — set `handles.SendFile.Assets` post-`buildAssetService` | mod | config (composition) | — (wiring) | `.Caps` set-site `serve.go:279-284` | exact |
| `cmd/aura` — `sendFileAssetAdapter` (structural `AssetDeliverer` impl) | new | provider (adapter) | — | interface-satisfaction convention | partial |
| `web/src/chat/displays/types.ts` — `DisplayArtifact.asset_id?/mime_type?` | mod | model (wire type) | — | existing `DisplayArtifact` `:51-55` | exact |
| `web/src/chat/sseAdapter.ts` — `aura.artifact` CUSTOM case | mod | store/reducer | event-driven / transform | `aura.display` CUSTOM branch `:305-316` | exact |
| `web/src/chat/sseAdapter_frames.ts` — doc-comment only (no new type) | mod | model (wire type) | — | `CustomFrame` `:103-107` already covers it | exact |
| `web/src/chat/displays/LocalArtifactDisplay.tsx` — download-button branch | mod | component | request-response (render) | own path-chip branch `:57-66` | exact |
| **TESTS** | | | | | |
| `internal/assets/ingest_agent_test.go` | new | test | unit (fakes) | `service.go` tests + `objectstore/fake.go` | role-match |
| `internal/agent/tools/send_file_ingest_test.go` | new | test | unit (fakes) | existing `send_file` tests + fake `AssetDeliverer` | role-match |
| `internal/agui/asset_download_test.go` | new | test | httptest + fake store | `assets_api_test.go` (`fakeAssetService`, `SetAssetService`) | exact |
| `internal/agui/content_disposition_test.go` | new | test | property-based | property-based-testing skill (rapid/gopter) | none |
| `internal/db/migrate_0035_integration_test.go` | new | test | `db_integration` roundtrip | `migrate_0034_integration_test.go` | exact |
| `internal/channels/telegram/artifact_test.go` | extend | test | regression unit | own `TestArtifactConsumeChannelDrainsAll` `:154` | exact |
| `web/src/chat/__tests__/sseAdapter.test.ts` | **rewrite** `:383` | test | vitest reducer | own no-op test (encodes the old drop contract) | exact |
| `web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` | extend | test | vitest RTL | existing card tests | exact |

**≤600-LOC refactor-on-touch check (CLAUDE.md NO GOD CLASS):**
- `service.go` is **422 LOC**; `IngestAgentFile` (~60) + `OpenForIdentity` (~10) inline → ~492 (under cap, but close). **Recommend a new `internal/assets/ingest_agent.go`** (same package) for `IngestAgentFile`+`AgentIngestRequest`; keep `OpenForIdentity` beside `GetForIdentity` in `service.go`. Keeps `service.go` stable.
- `send_file.go` is **280 LOC**; interface + field + ingest/degrade helper + enrich → ~335 inline (under cap). **Recommend a new `internal/agent/tools/send_file_ingest.go`** holding the `AssetDeliverer` interface + the ingest/degrade helper — mirrors the existing `send_file_sandbox.go` `<name>_<concern>.go` split and keeps `send_file.go` lean. `emitDelivery` (in `send_file.go`) gains a `ctx` param and calls the helper.
- `assets_api.go` is **164 LOC**; `handleAssetDownload` (~35) inline → ~200 (safe). Put the RFC-6266 helper in a **new `content_disposition.go`** (it must be property-tested in isolation).
- All other touched files stay well under cap.

---

## Pattern Assignments

### 1. `internal/assets/ingest_agent.go` — `IngestAgentFile` (service, file-I/O → CRUD-create)

**Analog:** `assets.Service.IngestTelegramFile` (`internal/assets/service.go:257-327`) — copy verbatim, apply the 4 deltas.

**Full analog to mirror** (`service.go:257-327`):
```go
func (s *Service) IngestTelegramFile(ctx context.Context, req TelegramIngestRequest) (Asset, error) {
	if s.Store == nil || s.Objects == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	if req.Reader == nil {
		return Asset{}, fmt.Errorf("telegram asset reader is nil")
	}
	name := cleanFileName(req.FileName)
	mimeType := normalizeMIME(req.MIMEType, name)
	modality := req.Modality
	if modality == "" || modality == ModalityUnknown {
		modality = InferModality(name, mimeType)
	}
	if err := s.Limits.Validate(modality, name, req.SizeBytes); err != nil {   // ← DELTA D-04: DELETE (skip #1)
		return Asset{}, err
	}
	objects, bucket, err := s.objectsFor(identityctx.WithIdentityID(ctx, req.IdentityID))
	if err != nil {
		return Asset{}, err
	}
	assetID := newAssetID()
	key := objectstore.AssetKey(req.IdentityID, assetID)
	sourceRef, err := telegramSourceRef(req)                                    // ← DELTA: DROP (agent has no source ref; leave SourceRef "")
	if err != nil {
		return Asset{}, err
	}
	asset, err := s.Store.Create(ctx, CreateRequest{
		IdentityID:        req.IdentityID,
		ThreadID:          req.ThreadID,
		SourceKind:        SourceTelegram,                                       // ← DELTA D-06: SourceAgent
		SourceRef:         sourceRef,                                           // ← DELTA: "" (drop)
		Scope:             ScopeThread,
		Modality:          modality,
		FileName:          name,
		MIMEType:          mimeType,
		DeclaredSizeBytes: req.SizeBytes,
		ObjectBucket:      bucket,
		ObjectKey:         key,
		Metadata:          map[string]any{},
	})
	if err != nil {
		return Asset{}, err
	}
	ref := objectstore.ObjectRef{Bucket: bucket, Key: key}
	attrs, err := objects.Put(ctx, ref, req.Reader, objectstore.PutOptions{MIMEType: mimeType, Size: req.SizeBytes})
	if err != nil {
		_, _ = s.Store.SetStatus(ctx, asset.ID, req.IdentityID, StatusFailed, "object_put_failed", err.Error())
		return Asset{}, err
	}
	if err = s.Limits.Validate(modality, name, attrs.SizeBytes); err != nil {   // ← DELTA D-04: DELETE (skip #2)
		updated, _ := s.Store.SetStatus(ctx, asset.ID, req.IdentityID, StatusRefused, "asset_refused", err.Error())
		_ = objects.Delete(context.WithoutCancel(ctx), ref)
		return updated, err
	}
	asset, err = s.Store.MarkUploaded(ctx, asset.ID, req.IdentityID, attrs.SizeBytes, attrs.ETag)
	if err != nil {
		return Asset{}, err
	}
	hash, sniffed, err := s.hashAndSniff(ctx, objects, ref, asset.FileName)
	if err != nil {
		return Asset{}, err
	}
	asset, err = s.Store.MarkAccepted(ctx, asset.ID, req.IdentityID, attrs.SizeBytes, hash, sniffed)
	if err != nil {
		return Asset{}, err
	}
	return s.processAsset(ctx, asset)                                           // ← DELTA D-03: STOP — `return asset, nil` here (no processAsset)
}
```

**The 4 deltas (exact):**
1. **Skip `Limits.Validate`** — delete BOTH call sites (`service.go:270` pre-Put and `service.go:309` post-Put). D-04: the file already cleared `maxSendFileBytes`.
2. **`SourceKind: SourceAgent`** — the new constant (see #3 below). Drop `SourceRef`/`telegramSourceRef` (leave empty).
3. **Stop before `processAsset`** — replace the final `return s.processAsset(ctx, asset)` with `return asset, nil`. D-03: a deliverable must NOT become searchable memory.
4. Keep everything else **verbatim**: `objectsFor(identityctx.WithIdentityID(...))`, `AssetKey`, `Store.Create`, `objects.Put`, `MarkUploaded`, `hashAndSniff`, `MarkAccepted`. `InferModality` still runs (needed for the `Create` row; it does not gate → D-04 unaffected).

**Request type** (new, in `types.go`, mirroring `TelegramIngestRequest` `types.go:129-140` minus the Telegram source fields):
```go
type AgentIngestRequest struct {
	IdentityID string
	ThreadID   string
	FileName   string
	MIMEType   string
	Modality   Modality
	SizeBytes  int64
	Reader     io.Reader   // caller passes os.Open(hostPath); close it in the tool after ingest
}
```

**RLS-free (verified):** `aura.assets` is NOT RLS-covered (`0032_owner_rls` covers only `conversations`/`paused_states`/`conversation_turns`), so this reuses the exact pooled orchestration — NO GUC/`WithIdentityTx` threading.

---

### 2. `internal/assets/service.go` — `OpenForIdentity` (service, streaming read)

**Analog:** the ownership read `GetForIdentity` (`service.go:174-179`) + the per-identity store resolve + `objects.Get` pattern from `Finalize` (`service.go:140-145`) / `hashAndSniff` (`service.go:377`).

**Ownership read to reuse** (`service.go:174-179`):
```go
func (s *Service) GetForIdentity(ctx context.Context, id, identityID string) (Asset, error) {
	if s.Store == nil {
		return Asset{}, fmt.Errorf("asset service is not configured")
	}
	return s.Store.GetForIdentity(ctx, id, identityID)
}
```

**The objectsFor + Get pattern to reuse** (from `Finalize` `:140-145` and `hashAndSniff` `:377`):
```go
objects, _, err := s.objectsFor(identityctx.WithIdentityID(ctx, identityID))   // per-identity store resolve
// ...
rc, attrs, err := objects.Get(ctx, objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey})
```

**Recommended new method** (composes the two — ownership FIRST so a miss is a 404 before any store touch):
```go
// OpenForIdentity resolves the owner-scoped asset (404 on miss/not-owned, D-12) and opens the
// per-identity object body for streaming. Caller closes the returned ReadCloser.
func (s *Service) OpenForIdentity(ctx context.Context, id, identityID string) (io.ReadCloser, Asset, error) {
	asset, err := s.GetForIdentity(ctx, id, identityID)                        // D-12 ownership gate
	if err != nil {
		return nil, Asset{}, err
	}
	objects, _, err := s.objectsFor(identityctx.WithIdentityID(ctx, identityID))
	if err != nil {
		return nil, Asset{}, err
	}
	rc, _, err := objects.Get(ctx, objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey})
	return rc, asset, err
}
```

`objectstore.Store.Get` signature (`objectstore/types.go:55`): `Get(context.Context, ObjectRef) (io.ReadCloser, Attrs, error)`.

**Also add to the agui `AssetService` interface** (`internal/agui/asset_service.go:10-22`) — one line:
```go
type AssetService interface {
	Presign(context.Context, assets.PresignRequest) (assets.PresignResponse, error)
	Finalize(context.Context, string, string) (assets.Asset, error)
	GetForIdentity(context.Context, string, string) (assets.Asset, error)
	OpenForIdentity(context.Context, string, string) (io.ReadCloser, assets.Asset, error)  // ← ADD (needs `io` import)
	ListForThread(context.Context, string, string) ([]assets.Asset, error)
	// ... rest unchanged ...
}
```

---

### 3. `internal/assets/types.go` — `SourceAgent` constant (model)

**Analog:** the existing `SourceKind` const block (`types.go:64-68`).
```go
// Supported asset source kinds.
const (
	SourceWeb      SourceKind = "web"
	SourceTelegram SourceKind = "telegram"
	SourceCLI      SourceKind = "cli"
	SourceAgent    SourceKind = "agent"   // ← ADD (D-06); first-class, distinguishable from human uploads
)
```

---

### 4. Injection seam — `AssetDeliverer` + `SendFile.Assets` wired post-construction (VERIF-7)

**Analog:** the shipped `ShellPoll/ShellKill.Caps` VERIF-7 pattern — a runtime handle retained on `runtimeToolHandles`, its capability field left nil at registry-build, then set at serve boot behind a nil guard. **This is the exact pattern to mirror for `SendFile.Assets`.**

**4a. The handle-retention + nil-at-build precedent** (`cmd/aura/main.go:114-123`):
```go
type runtimeToolHandles struct {
	BackgroundShells *tools.BackgroundShells
	ShellApprovals   *tools.ShellApprovals
	// ShellPoll / ShellKill are retained so serve boot can wire their .Caps to the live
	// capability store (VERIF-7 / D-18): the pool-free manifest paths construct them with a
	// nil Caps (owner-only fail-closed), and serve.go sets Caps = the identity store once it
	// exists...
	ShellPoll *tools.ShellPoll
	ShellKill *tools.ShellKill
	SendFile  *tools.SendFile   // ← ADD: retain so serve boot can set .Assets (nil → path-only degrade, D-02)
}
```

**Registration retaining the pointer** (`main.go:181-186` for ShellPoll; the `SendFile` line to change is `main.go:199`):
```go
sp := &tools.ShellPoll{Shells: handles.BackgroundShells}
sk := &tools.ShellKill{Shells: handles.BackgroundShells}
handles.ShellPoll = sp
handles.ShellKill = sk
reg.Register(sp)
reg.Register(sk)
// ...
reg.Register(&tools.SendFile{WorkspaceRoot: workspace, Router: sandboxRouter})   // ← main.go:199 CHANGE to:
//   sf := &tools.SendFile{WorkspaceRoot: workspace, Router: sandboxRouter}
//   handles.SendFile = sf
//   reg.Register(sf)
```

**4b. The serve-boot set-site precedent** (`cmd/aura/serve.go:279-284`) — set `.Assets` right AFTER `buildAssetService` (`serve.go:292`), which is confirmed to run after this `.Caps` block:
```go
if chat.toolHandles.ShellPoll != nil {
	chat.toolHandles.ShellPoll.Caps = chat.identity
}
if chat.toolHandles.ShellKill != nil {
	chat.toolHandles.ShellKill.Caps = chat.identity
}
// ... store := cron.New(chat.pool); objectStore := buildObjectStore(...) ...
chat.assets = buildAssetService(chat.cfg, chat.pool, objectStore)              // serve.go:292
// ← ADD immediately after (chat.assets is now non-nil):
//   if chat.toolHandles.SendFile != nil {
//       chat.toolHandles.SendFile.Assets = sendFileAssetAdapter{svc: chat.assets}
//   }
```
This confirms Assumption A2 (orderable) and leaves the static `buildRegistry()`/manifest paths (`aura tools`, `finetune/exporter`, spikes) with `SendFile.Assets == nil` → **D-02 path-only degrade**. Do NOT change `buildRegistry`/`buildRegistryWithMCP` signatures.

**4c. The narrow interface** (new, in `send_file_ingest.go` — keeps `tools` from importing `internal/assets`):
```go
// AssetDeliverer stores a host-file's bytes under the identity's object store and returns the
// created thread-scoped asset id. Primitive-typed so the tools package never imports internal/assets
// (substrate names no concrete service). Best-effort: any error => caller degrades to path-only (D-02).
type AssetDeliverer interface {
	IngestAgentDelivery(ctx context.Context, identityID, threadID, hostPath, filename, mimeType string, size int64) (assetID string, err error)
}
```
Add `Assets AssetDeliverer` to the `SendFile` struct (`send_file.go:27`, alongside `WorkspaceRoot`/`Router`).

**4d. The `cmd/aura` adapter** (structural impl — `cmd/aura` is the only package importing both `tools` and `assets`; put it near `buildAssetService`):
```go
type sendFileAssetAdapter struct{ svc *assets.Service }

func (a sendFileAssetAdapter) IngestAgentDelivery(ctx context.Context, identityID, threadID, hostPath, filename, mimeType string, size int64) (string, error) {
	f, err := os.Open(hostPath)   // #nosec — hostPath already workspace-fenced by send_file
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	asset, err := a.svc.IngestAgentFile(ctx, assets.AgentIngestRequest{
		IdentityID: identityID, ThreadID: threadID, FileName: filename,
		MIMEType: mimeType, SizeBytes: size, Reader: f,
	})
	if err != nil {
		return "", err
	}
	return asset.ID, nil
}
```

**4e. Identity + thread resolution at `Execute` time (in-package, NO import cycle):**
- Identity: `identityctx.IdentityID(ctx)` (`internal/identityctx/identityctx.go:26`) → `""` when unscoped (CLI) → D-02 degrade. `tools` already imports `identityctx` (`shell_bg_owner.go`).
- Thread: `toolCallCtx(ctx).sessionID` (`internal/agent/tools/result.go:19,47`) — this **is** the ConvID (`sessionID == conversationID == SwarmContext.ConvID`, proven `llm_agent.go:545-546`). D-08's literal `agent.SwarmContext(ctx).ConvID` is **unreachable** from `tools` (import cycle); this is the equivalent seam. `""` → D-05 degrade.
- ToolCallID (for the descriptor, Gap C): `toolCallCtx(ctx).toolCallID` (same struct, `result.go:20`).

The `toolCallContext` accessor already in-package (`result.go:47-50`):
```go
func toolCallCtx(ctx context.Context) (toolCallContext, bool) {
	v, ok := ctx.Value(toolCallContextKey{}).(toolCallContext)
	return v, ok
}
```

---

### 5. Descriptor + event flow — `emitDelivery` enrichment (tool, event-driven / transform)

**Analog:** the current `emitDelivery` descriptor build (`send_file.go:123-148`). **BOTH delivery tails funnel through it** (`Execute` `:116` host-path, `deliverFromBox` `send_file_sandbox.go:49` routed) — Landmine 1: doing the ingest in the shared ctx-aware `emitDelivery` covers both tails with one change.

**Current descriptor** (`send_file.go:136-140`):
```go
descriptor := map[string]any{
	"path":     path,
	"filename": filepath.Base(path),
	"caption":  asciiCaption(caption),
}
meta := ToolResultMeta{"artifact": descriptor}
```

**Enriched (success)** — add 4 fields; `emitDelivery` must gain a `ctx` param (both callers have ctx: `Execute` `:85`, `deliverFromBox` `:24`):
```go
descriptor := map[string]any{
	"path":     path,
	"filename": filepath.Base(path),
	"caption":  asciiCaption(caption),
}
// ALWAYS-present (known BEFORE ingest, like path): the web reducer's correlation key +
// the stat size the maxSendFileBytes gate already computed. Emitted on success AND degrade.
if tcid := toolCallIDFromCtx(ctx); tcid != "" {
	descriptor["tool_call_id"] = tcid   // Landmine 5: correlation key — MUST ride on degrade too
}
descriptor["size_bytes"] = size          // the stat size (always known)
// Best-effort ingest (D-01/D-02): add ONLY the ingest-derived fields on success; degrade silently on ANY miss.
if id, mt, ok := ingestForDelivery(ctx, s.Assets, path, filepath.Base(path)); ok {
	descriptor["asset_id"] = id
	descriptor["mime_type"] = mt
}
```
Final success descriptor: `{path, filename, caption, tool_call_id, size_bytes, asset_id, mime_type}`. Degraded: `{path, filename, caption, tool_call_id, size_bytes}` (no `asset_id`/`mime_type`) — the web reducer still attaches the render-only "delivery unavailable" card (D-02 honored) and `tool_call_id`+`size_bytes` make it reachable + show the size. `path` rides the descriptor for Telegram parity but the web reducer never copies it into the display payload (see Open Q1).

**Proof no translator/event change is needed (Landmine 6 / D-07):** the descriptor rides Meta → `Actions.ArtifactDelta` → the CUSTOM event **verbatim**. The live emit for a `send_file` result is `emitToolResultCustom` (NOT the standalone `:133` branch, which is unreachable for a tool result). `translator.go:365-377`:
```go
func emitToolResultCustom(yield func(events.Event, error) bool, ev *agent.Event) bool {
	if len(ev.Actions.ArtifactDelta) > 0 {
		if !yield(events.NewCustomEvent(artifactEventName, events.WithValue(ev.Actions.ArtifactDelta)), nil) {
			return false
		}
	}
	// ... aura.display below ...
}
```
`events.WithValue(ev.Actions.ArtifactDelta)` passes the whole map through — the 4 new keys ride for free. **Zero change** in `translator.go` / `llm_agent_events.go`.

**Telegram stays unregressed (D-07):** `artifactDescriptor` (`telegram/artifact.go:74-84`) reads only `path` (`:55`) and ignores extra keys:
```go
path := stringField(desc, "path")   // artifact.go:55 — reads ONLY path; asset_id/size_bytes/mime_type ignored
if path == "" {
	return nil, false
}
```

---

### 6. Download route — `handleAssetDownload` (route/controller, request-response → streaming)

**Analog:** `handleAssetGet` (`internal/agui/assets_api.go:103-119`) — the principal-read → `GetForIdentity` → 404-on-miss shape. Diverge by streaming the body (`io.Copy`) instead of `writeJSON`.

**Template** (`assets_api.go:103-119`):
```go
func (s *Server) handleAssetGet(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	asset, err := s.assets.GetForIdentity(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)   // ← 404-on-miss (D-12) — reuse for download
		return
	}
	writeJSON(w, asset)                                        // ← DIVERGE: stream the body instead
}
```

**Recommended download handler** (same head, streaming tail):
```go
func (s *Server) handleAssetDownload(w http.ResponseWriter, r *http.Request) {
	if s.assets == nil {
		http.Error(w, "asset service unavailable", http.StatusServiceUnavailable)
		return
	}
	identityID, ok := principalIdentityID(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rc, asset, err := s.assets.OpenForIdentity(r.Context(), r.PathValue("id"), identityID)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusNotFound)   // D-12: not-found OR not-owned → 404
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", "application/octet-stream")                 // D-10 stored-XSS guard
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", contentDisposition(asset.FileName))  // D-11 (helper, §7)
	w.Header().Set("Content-Length", strconv.FormatInt(asset.SizeBytes, 10))   // Open Q2 resolved below
	_, _ = io.Copy(w, rc)                                                      // D-09: r.Context() scopes the read
}
```

**Route registration** (`registerAssetRoutes` `assets_api.go:11-19`) — add one line inside the existing block; `RequireAuth` is inherited whole-origin (`auth.go:183`), no per-route wiring:
```go
func (s *Server) registerAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/assets/presign", s.handleAssetPresign)
	mux.HandleFunc("POST /api/assets/{id}/finalize", s.handleAssetFinalize)
	mux.HandleFunc("GET /api/assets/{id}", s.handleAssetGet)
	mux.HandleFunc("GET /api/assets/{id}/download", s.handleAssetDownload)   // ← ADD
	mux.HandleFunc("GET /api/assets", s.handleAssetList)
	// ... rest unchanged ...
}
```
> Route ordering note: Go 1.22+ `ServeMux` matches the more specific pattern (`/{id}/download`) over `/{id}` regardless of registration order — no precedence issue.

**Auth principal read** (`assets_api.go:161-164`) — reuse verbatim:
```go
func principalIdentityID(r *http.Request) (string, bool) {
	identityID := principalFrom(r.Context())
	return identityID, identityID != ""
}
```

---

### 7. `internal/agui/content_disposition.go` — RFC-6266 helper (utility, pure transform)

**No in-tree analog** (do NOT reuse `asciiCaption`/`foldToASCII` — those transliterate the *caption*; D-11 wants unicode-preserving *filename* encoding). Port LibreChat `api/server/utils/files.js:67-88` (~15 lines): (a) ASCII-fold + strip control/`"`/`\`/CR/LF for `filename="<fallback>"`; (b) `url.PathEscape`(UTF-8 name) for `filename*=UTF-8''<pct>`. Output shape:
```
attachment; filename="<ascii-fallback>"; filename*=UTF-8''<percent-encoded>
```
**Landmine 4:** `mime.FormatMediaType` alone is insufficient (drops the whole type on invalid runes; emits no `filename*`). This is the **property-based-testing target** (§Tests): fuzz unicode / embedded `"` / `\r\n` / `;` / path-seps / empty / >255-byte; assert exactly one `filename=` + one `filename*=`, no raw CR/LF, unicode round-trips.

---

### 8. Migration `0035_assets_source_kind_agent` (migration, DDL)

**Analog:** `0034_scheduler_sandbox_reap_kind.{up,down}.sql` (CHECK-widen). The `0020` CHECK is an inline column check → Postgres auto-named it `assets_source_kind_check`.

**Current CHECK to relax** (`0020_assets.up.sql:4`):
```sql
source_kind text NOT NULL CHECK (source_kind IN ('web', 'telegram', 'cli')),
```

**`0034` up-shape to mirror** (`0034_...up.sql:14-16`):
```sql
ALTER TABLE aura.scheduler_tasks DROP CONSTRAINT scheduler_tasks_kind_check;
ALTER TABLE aura.scheduler_tasks ADD  CONSTRAINT scheduler_tasks_kind_check
    CHECK (kind IN ('reminder', 'agent_job', ..., 'sandbox_reap'));
```

**`0035_assets_source_kind_agent.up.sql`:**
```sql
-- Relax the 0020 assets.source_kind CHECK to admit 'agent' (WEBART-01 / D-06): agent-produced
-- deliverables become first-class + distinguishable from human uploads. The 0020 constraint is an
-- inline column check → Postgres auto-named it `assets_source_kind_check`; drop + re-add with the
-- extra member. 0020 already GRANTed aura_app DML (aura_migrate owns DDL) — no grant change.
ALTER TABLE aura.assets DROP CONSTRAINT assets_source_kind_check;
ALTER TABLE aura.assets ADD  CONSTRAINT assets_source_kind_check
    CHECK (source_kind IN ('web', 'telegram', 'cli', 'agent'));
```

**`0035_...down.sql`** (mirror the `0034` down: DELETE the newly-admitted rows FIRST, else the narrowed CHECK aborts the down mid-chain → dirty DB). `0034_...down.sql:9-15`:
```sql
-- A down that narrows the CHECK must first delete the rows the widening admitted.
DELETE FROM aura.assets WHERE source_kind = 'agent';
ALTER TABLE aura.assets DROP CONSTRAINT assets_source_kind_check;
ALTER TABLE aura.assets ADD  CONSTRAINT assets_source_kind_check
    CHECK (source_kind IN ('web', 'telegram', 'cli'));
```
> `sqlc generate` is **zero-diff** (a CHECK widen touches no query). Landmine 3: migrate the test harness to HEAD before inserting an `'agent'` row (else `23514 check_violation`).

---

## Open Question Resolutions

### Open Q1 — Frontend mapping of `aura.artifact` → `LocalArtifactDisplay` (RESOLVED)

**DECISION: Synthesize a `local_artifact` `DisplayPayload` in the `sseAdapter` CUSTOM reducer branch from the descriptor, correlated by the new `tool_call_id` descriptor field. DO NOT thread `asset_id` into the existing `aura.display` `local_artifact` payload.**

**Why the `aura.display` route is impossible for `send_file`:** the `aura.display` `local_artifact` payload is produced ONLY by `normalizeCode` (`internal/agent/display/code.go:20-31`), which fires solely for code-producing tools (`sandbox_exec`/`shell_exec`) that name an `ArtifactFilename`:
```go
func normalizeCode(toolCallID string, in CodeInput) (Payload, bool) {
	if in.ArtifactFilename != "" {
		return Payload{Type: KindLocalArtifact, ToolCallID: toolCallID, Title: in.ArtifactFilename,
			Artifact: &Artifact{Filename: in.ArtifactFilename, SizeBytes: in.ArtifactSize, Path: in.ArtifactPath}}, true
	}
	// ...
}
```
`send_file` never flows through `normalizeCode` → there is **no** `aura.display` payload for a `send_file` result to thread into (Landmine 7 confirmed). The only carrier is `aura.artifact`.

**The exact reducer branch to extend** (`web/src/chat/sseAdapter.ts:305-316`) — the `aura.display` case is the verbatim template (correlate by `tool_call_id` → `ensureTool` → `writeTool({...part, display})`):
```ts
case 'CUSTOM': {
	// Phase 26: aura.display carries a typed DisplayPayload to attach to the
	// tool part by tool_call_id. ...Any other CUSTOM name (e.g. aura.artifact) is left unchanged...
	if (frame.name === 'aura.display' && isDisplayPayload(frame.value)) {
		const id = frame.value.tool_call_id;
		const part = ensureTool(state, id, state.tools.get(id)?.toolName ?? '');
		writeTool(state, { ...part, display: frame.value });
	}
	// ← ADD an aura.artifact branch here (37A): synthesize a local_artifact DisplayPayload.
	return state;
}
```

**The new branch to add** (synthesize + attach by the descriptor's `tool_call_id`):
```ts
if (frame.name === 'aura.artifact' && isArtifactDescriptor(frame.value)) {
	const d = frame.value;
	const id = d.tool_call_id;
	const part = ensureTool(state, id, state.tools.get(id)?.toolName ?? '');
	const display: DisplayPayload = {
		type: 'local_artifact',
		tool_call_id: id,
		artifact: {
			filename: d.filename,
			...(d.size_bytes !== undefined ? { size_bytes: d.size_bytes } : {}),
			...(d.asset_id !== undefined ? { asset_id: d.asset_id } : {}),
			...(d.mime_type !== undefined ? { mime_type: d.mime_type } : {}),
			// path is NEVER copied into the display payload — in EITHER branch (D-13 tightened per
			// operator decision: the browser must never receive a raw host/container path). `path`
			// stays a backend/Telegram-only descriptor field (D-01).
		},
	};
	writeTool(state, { ...part, display });
}
```
Add a small guard beside `isDisplayPayload` (the descriptor is NOT a `DisplayPayload` — it has no `type` field):
```ts
function isArtifactDescriptor(v: unknown): v is { tool_call_id: string; filename: string; path?: string;
	asset_id?: string; size_bytes?: number; mime_type?: string } {
	return typeof v === 'object' && v !== null
		&& typeof (v as { tool_call_id?: unknown }).tool_call_id === 'string'
		&& typeof (v as { filename?: unknown }).filename === 'string';
}
```

**Type change** (`web/src/chat/displays/types.ts:51-55`) — add two optional fields:
```ts
/** A produced file (type=local_artifact). Mirrors display.Artifact. */
export interface DisplayArtifact {
	readonly filename: string;
	readonly size_bytes?: number;
	readonly path?: string;
	readonly asset_id?: string;    // ← ADD (37A): present → authenticated download button
	readonly mime_type?: string;   // ← ADD (37A): file-icon hint only (NOT trusted as a serve header)
}
```

**No `DisplayRouter` change:** `local_artifact` already routes to `LocalArtifactDisplay` (`DisplayRouter.tsx:62`):
```tsx
case 'local_artifact':
	return <LocalArtifactDisplay payload={payload} />;
```

**No `sseAdapter_frames.ts` structural change:** `CustomFrame {type,name,value}` (`:103-107`) already models any CUSTOM name; the `AguiFrame` union already includes it. Only the stale doc-comment (`:109-116`, "aura.artifact … not modelled") should be updated.

**The `LocalArtifactDisplay` download branch** (`LocalArtifactDisplay.tsx:57-66` is the path-chip branch to make conditional on `asset_id`):
```tsx
// Current path-chip branch (:57-66):
{artifact?.path !== undefined && artifact.path !== '' ? (
	<span className="flex flex-wrap items-baseline gap-2">
		<span className="...">{t('display.artifact.pathLabel')}</span>
		<span className="...font-mono...">{artifact.path}</span>
	</span>
) : null}
```
Extend to: `asset_id` present → render `<a href={`/api/assets/${artifact.asset_id}/download`} download={filename}>` (download button, NO raw-path chip — D-13 "replace the chip"); `asset_id` absent → a render-only card showing filename + size + a "delivery unavailable" note (new i18n `display.artifact.deliveryUnavailable`), NO path chip (remove the raw-path branch `:57-66` entirely). New i18n keys `display.artifact.download*` + `display.artifact.deliveryUnavailable` (component already uses `useTranslation`, `:28`).

**Degrade semantics (tightened per operator decision — the browser NEVER receives a raw path):**
- `asset_id` present (authenticated web, the primary case incl. sandbox-built DOCX) → download button, raw path NOT rendered.
- `asset_id` absent → a render-only card showing **filename + size + a "delivery unavailable" note, NO path chip**. For any browser-reachable event this is ALWAYS an authenticated-but-ingest-failed session (`Put` error, D-02 / empty thread, D-05) — every SSE frame is post-`RequireAuth`, so it is NEVER the CLI/no-identity carve-out (that path has no SSE session). The reducer also omits `path` from the synthesized payload, so no raw host/container path reaches the browser in either branch. This preserves D-02's "render-only card on degrade" intent without the path leak (operator decision on the ROADMAP "never a raw path" success criterion); `tool_call_id` + `size_bytes` ride the degrade descriptor so the card is reachable and shows the size.

**Test contract flip (Landmine 5):** `web/src/chat/__tests__/sseAdapter.test.ts:383` currently asserts "an unrecognized CUSTOM name (aura.artifact) is a no-op" — this **encodes the old drop contract** and must be **rewritten** (not deleted) to assert `aura.artifact` now attaches a `local_artifact` card, with an explicit commit-message justification (the legitimate exception to CLAUDE.md's "NEVER MODIFY TESTS TO MAKE THEM PASS": the test asserts the behavior we intentionally change).

### Open Q2 — `Content-Length` on the download (RESOLVED)

**DECISION: set `Content-Length` from `asset.SizeBytes`.** It is the *stored* size, written from `attrs.SizeBytes` at `MarkAccepted` (`service.go:322`) AFTER a successful `Put` — authoritative, not the client-declared size. Landmine 8's truncation risk requires a partial stored object, which the ingest `Put`→`MarkUploaded`→`MarkAccepted` sequence rules out. Simpler than chunked; document the one assumption (stored size == object size post-`MarkAccepted`).

---

## Shared Patterns

### Auth (all `/api/assets/*` routes)
**Source:** `principalIdentityID` (`assets_api.go:161-164`) + `RequireAuth` whole-origin (`auth.go:183`).
**Apply to:** the new `handleAssetDownload`. `principalIdentityID(r)` → 401 if `!ok` (belt-and-suspenders; `RequireAuth` already gated the origin). No per-route auth wiring.

### 404-on-miss ownership (IDOR / D-12)
**Source:** `handleAssetGet` (`assets_api.go:113-117`) → `GetForIdentity` (`store.go:56`, `WHERE id=$1 AND identity_id=$2`).
**Apply to:** `handleAssetDownload` via `OpenForIdentity` (ownership FIRST). A not-found OR not-owned request → **404**, never 403 (existence-hiding). Ship the non-owner→404 regression test.

### Best-effort degrade (delivery never wedges the turn)
**Source:** `send_file`'s existing stance — `errorResult` carries NO Meta (`send_file.go:210-216`); telegram's "best-effort: a failed delivery must not wedge the turn" (`artifact.go:66`).
**Apply to:** the ingest — ANY miss (`Assets==nil` / `identityID==""` / `threadID==""` / `Put` err) → proceed with the `{path}`-only descriptor, no error surfaced.

### Per-identity object store
**Source:** `objectsFor(identityctx.WithIdentityID(ctx, id))` (`service.go:58,273`) + `objectstore.AssetKey(identityID, assetID)` (`objectstore/types.go:60`).
**Apply to:** BOTH `IngestAgentFile` (write) and `OpenForIdentity` (read) — an identity's bytes land in / stream from ITS OWN bucket.

### Runtime-handle post-construction wiring (VERIF-7)
**Source:** `ShellPoll/ShellKill.Caps` (`main.go:114-123,181-186` + `serve.go:279-284`).
**Apply to:** `SendFile.Assets` — nil at registry-build (path-only), set at serve boot behind a nil guard after `chat.assets` exists.

### CHECK-widen migration
**Source:** `0034` / `0033` (drop auto-named inline-check constraint → re-add widened; down = delete-new-rows-then-narrow).
**Apply to:** `0035` `assets_source_kind_check`.

---

## Test Analogs (Wave 0)

| New/extend test | Closest analog | Pattern to copy |
|---|---|---|
| `internal/assets/ingest_agent_test.go` | existing `assets` service tests + `internal/objectstore/fake.go` | fake `StoreBackend` + in-mem `objectstore.Store`; assert `processAsset` NOT called, `Limits.Validate` NOT called (oversized-per-modality still ingests), `SourceKind==agent`, `Put`-error → degrade |
| `internal/agent/tools/send_file_ingest_test.go` | existing `send_file` host-path tests + fake `AssetDeliverer` | degrade matrix (nil `Assets` / empty identity via unset `identityctx` / empty `sessionID` / `Ingest` err → descriptor has `path`, NO `asset_id`); BOTH tails ingest (Landmine 1); descriptor-fields-on-success |
| `internal/agui/asset_download_test.go` | `internal/agui/assets_api_test.go` (`fakeAssetService` `:153`, `SetAssetService` `:32`, httptest) | owner→200 + `Content-Disposition: attachment`/`filename*`/`octet-stream`/body==bytes; **non-owner→404**; unauth→401; client-disconnect cancels read (cancel ctx + goleak) |
| `internal/agui/content_disposition_test.go` | property-based-testing skill (`pgregory.net/rapid` or `gopter` — **verify vendored first**, Package Legitimacy Gate if adding) | fuzz unicode/CRLF/quote/`;`/empty/long → one `filename=` + one `filename*=`, no raw CRLF, unicode round-trips |
| `internal/db/migrate_0035_integration_test.go` | `internal/db/migrate_0034_integration_test.go` (exact sibling) | `db_integration` roundtrip: INSERT `agent` OK after up; `23514` before-up / after-down |
| `internal/channels/telegram/artifact_test.go` (extend) | own `TestArtifactConsumeChannelDrainsAll` (`:154-169`) | add an enriched-descriptor case (`path`+`asset_id`+`size_bytes` present) still sends the document — non-regression |
| `web/src/chat/__tests__/sseAdapter.test.ts` (**rewrite `:383`**) | own no-op test | flip: `aura.artifact` now attaches a `local_artifact` card by `tool_call_id`; degraded (no `asset_id`) → chip only |
| `web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` (extend) | existing card tests | `asset_id` present → `<a href="/api/assets/{id}/download" download>`; absent → path chip; never a raw path when `asset_id` set |

**Coverage-floor trap (CLAUDE.md):** the gate runs `db_integration neo4j_integration` ONLY. Keep `IngestAgentFile`, `OpenForIdentity`, the degrade logic, and the RFC-6266 helper **daemon-free unit-tested** via `internal/objectstore/fake.go` — do NOT gate behind `garage_integration` (outside the gate → ZERO coverage). Only the migration roundtrip needs `db_integration`. Rebuild + commit `internal/webui/dist` after the web change (`web-dist-freshness` CI job).

---

## No Analog Found

| File | Role | Data Flow | Reason / Fallback |
|---|---|---|---|
| `internal/agui/content_disposition.go` | utility | pure transform | No RFC-6266 encoder in-tree; `asciiCaption`/`foldToASCII` are transliterators (wrong for D-11 unicode-preserve). Port LibreChat `files.js:67-88`; property-test it. |
| `internal/agui/content_disposition_test.go` | test | property-based | No property test exists yet; use the property-based-testing skill. Verify `rapid`/`gopter` is vendored before use. |
| `cmd/aura` `sendFileAssetAdapter` | provider (adapter) | — | Thin structural `AssetDeliverer` impl; no direct analog, follows the interface-satisfaction convention (adapter defined in the only package importing both `tools` and `assets`). |

---

## Metadata

**Analog search scope:** `internal/assets`, `internal/agent/tools`, `internal/agui`, `internal/channels/telegram`, `internal/db/migrations`, `internal/objectstore`, `internal/identityctx`, `internal/agent/display`, `cmd/aura`, `web/src/chat`.
**Files read (analogs):** `service.go`, `types.go`, `store.go`, `send_file.go`, `send_file_sandbox.go`, `result.go`, `assets_api.go`, `asset_service.go`, `translator.go`, `artifact.go`, `artifact_test.go`, `objectstore/types.go`, `identityctx.go`, `display/code.go`, `main.go`, `serve.go`, `0020_assets.up.sql`, `0034_*.up/down.sql`, `sseAdapter.ts`, `sseAdapter_frames.ts`, `LocalArtifactDisplay.tsx`, `displays/types.ts`, `DisplayRouter.tsx`.
**Zero source files modified** (read-only pass; only this PATTERNS.md written).
**Pattern extraction date:** 2026-07-08
