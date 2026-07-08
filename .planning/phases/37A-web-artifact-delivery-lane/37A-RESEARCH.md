# Phase 37A: Web Artifact Delivery Lane - Research

**Researched:** 2026-07-08
**Domain:** Go backend (asset ingest + authenticated streaming download) + AG-UI SSE event plumbing + React/TS cockpit display + Postgres migration
**Confidence:** HIGH (all claims verified against the current tree at HEAD `b46170e7`; every cited anchor re-confirmed line-for-line 2026-07-08)

## Summary

The 13 locked decisions (D-01..D-13) settle the implementation *how*; this research does the four jobs the decisions do not: (1) a full Validation Architecture, (2) anchor-drift verification against today's tree, (3) resolution of the discretionary implementation gaps, (4) landmines the locked decisions miss. **Every cited anchor in CONTEXT.md is still current** — no drift since 2026-07-08 gather. The phase touches five surfaces: `send_file` (two delivery tails), a new `assets.Service` ingest method + a new streaming download method, migration `0035`, the `GET /api/assets/{id}/download` route, and the web `sseAdapter`/`LocalArtifactDisplay`.

Three findings materially de-risk the plan. **First**, `toolCallCtx(ctx).sessionID == conversationID == agent.SwarmContext.ConvID` (proven at `llm_agent.go:545-546`, `evict.go:8`, `task.go:220-225`) — so `send_file` reads the thread id **in-package** (`toolCallCtx(ctx).sessionID`) with NO `internal/agent` import cycle; D-08's literal `agent.SwarmContext(ctx).ConvID` is *unreachable* from the tools package and must be read via this equivalent seam instead. **Second**, RLS is **not** enabled on `aura.assets` (migration `0032` covers only `conversations`/`paused_states`/`conversation_turns`), so the agent-ingest write path needs no GUC/transaction threading — it reuses the exact `IngestTelegramFile` orchestration. **Third**, the Meta→`ArtifactDelta`→`aura.artifact` event carries the descriptor map *verbatim* (`llm_agent_events.go:141`, `translator.go:365-377`), so WEBART-02's new fields need **zero** changes in `llm_agent_events.go` or `translator.go` — they ride the descriptor built in `emitDelivery` (`send_file.go:136-140`).

**Primary recommendation:** Mirror `IngestTelegramFile` into a new `assets.Service.IngestAgentFile` (skip `Limits.Validate`, `SourceKind=SourceAgent`, stop before `processAsset`); inject it into `SendFile` via a **narrow interface field set post-construction on `runtimeToolHandles`** (mirroring the shipped `ShellPoll/ShellKill.Caps` VERIF-7 precedent) so CLI/static paths leave it nil → path-only degrade; resolve identity via `identityctx.IdentityID(ctx)` and thread id via `toolCallCtx(ctx).sessionID` at `Execute` time. Add `asset_id`+`size_bytes`+`mime_type`+**`tool_call_id`** to the descriptor. Keep all tests daemon-free using `internal/objectstore/fake.go` so they count toward the 85% floor.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (verbatim)

**Ingest lane (WEBART-01)**
- **D-01 — Always ingest when authenticated (channel-agnostic).** Any `send_file` under an authenticated identity writes bytes to Garage (per-identity `AssetKey`) + creates a thread-scoped owned `assets.Asset`, mirroring `assets.Service.IngestTelegramFile` (`internal/assets/service.go:257`). The `aura.artifact` event carries **both** the legacy `{path}` (Telegram keeps using it, unregressed) **and** `asset_id`/`filename`/`size_bytes`/`mime_type` (web uses these). Preserves the "substrate names no channel" invariant that `send_file`'s doc-comment guards — do NOT make the tool channel-aware.
- **D-02 — Ingest failure → degrade to path-only, best-effort.** On Garage `Put` error / asset-service error while the host file is still readable: emit the event with `{path}` only (no `asset_id`). Telegram still delivers; web shows the render-only `local_artifact` card (no download button). Delivery NEVER wedges the turn. A nil asset service or no-authenticated-identity (CLI) → today's host-path behavior.
- **D-03 — Skip the asset processing pipeline (delivery-only).** Create + upload + `MarkAccepted` the asset but do **NOT** call `processAsset()` / enqueue processing (embeddings / doc-extraction / knowledge-graph indexing). A generated report must not silently become searchable memory. (Opt-in indexing = deferred idea.)
- **D-04 — Flat 50 MiB gate only; bypass per-modality `assets.Limits` for `source_kind=agent`.** The file already passed `send_file`'s 50 MiB delivery gate (`maxSendFileBytes`, `send_file.go:47`); `send_file`'s ceiling is the single authoritative delivery gate; skip `Limits.Validate` for agent deliveries.
- **D-05 — Empty `ConvID` (no thread) → degrade to path-only.** An authenticated identity with an empty thread id can't produce a thread-scoped asset; degrade to the same `{path}`-only path as D-02 (one consistent fallback).

**Data model (WEBART-01, fork a)**
- **D-06 — New `agent` source_kind via migration `0035`.** Forward-only migration (next slot is `0035`; latest on disk is `0034_scheduler_sandbox_reap_kind`) relaxing the `assets.source_kind` CHECK (currently `web|telegram|cli`, `migrations/0020_assets.up.sql:4`) to include `agent`, plus an `assets.SourceAgent` constant. Write both `.up.sql` and `.down.sql`.

**Event / thread plumbing (WEBART-02, fork b)**
- **D-07 — Extend the existing `aura.artifact` event, do not add a new event.** WEBART-02 fields ride the existing `ArtifactEventName = "aura.artifact"` CUSTOM event (`internal/agui/translator.go:19`), emitted from `Actions.ArtifactDelta`. Telegram's `artifactDescriptor` already tolerates extra map fields (`telegram/artifact.go:74`).
- **D-08 — Reuse `agent.SwarmContext(ctx).ConvID` for the thread id this phase.** It already carries the conversation id end-to-end on every channel-driven run. The "swarm concern leaking into a non-swarm delivery tool" smell is a **deferred cleanup** (a dedicated `threadctx` key), not resolved now.

**Download route (WEBART-03)**
- **D-09 — Stream-through Go (`io.Copy`), no Range, request-`ctx`-scoped.** New `GET /api/assets/{id}/download` registered in `registerAssetRoutes` (`internal/agui/assets_api.go:11`), behind `RequireAuth`. Read Garage `GetObject.Body` → `io.Copy(w, body)`; pass the request context so a client disconnect cancels the store read. **Not** a presigned redirect — mirror LibreChat's strategy interface so presign is a future drop-in.
- **D-10 — Serve `Content-Disposition: attachment` + `Content-Type: application/octet-stream` (stored-XSS guard).** Force `attachment` and a neutral serve type regardless of the sniffed MIME. The content-sniffed `mime_type` still rides the SSE event; it is NOT trusted as the serve header.
- **D-11 — Dual RFC 6266 filename encoding.** `attachment; filename="<ascii-fallback>"; filename*=UTF-8''<percent-encoded>`. Use `mime.FormatMediaType` or port LibreChat's ~10-line helper. The *download* filename may keep unicode even though `send_file`/telegram ASCII-fold the *caption*.
- **D-12 — Ownership = single scoped query, 404 on miss, regression test.** Enforce via `assets.Store.GetForIdentity` (`internal/assets/store.go:56`); a not-found OR not-owned request returns **404**, never a 403. No unauthenticated download surface is added. Ship a regression test proving a non-owner → 404.

**Web UI (WEBART-04, fork c)**
- **D-13 — Extend `LocalArtifactDisplay`, do not add a new display type.** When the `aura.artifact` event carries an `asset_id`, `LocalArtifactDisplay.tsx` swaps its inert path chip for a download button (`<a href="/api/assets/{id}/download" download={filename}>`). No `asset_id` (degraded) → today's render-only card. `sseAdapter.ts` must stop ignoring `aura.artifact` (`sseAdapter.ts:309`) and route it into this card. **Footgun to avoid (Elysia `RenderDisplay.tsx:121`):** an unknown display type silently renders null — keep the backend/frontend type-string contract explicit.

### Claude's Discretion (verbatim)
- Exact migration filename/suffix for `0035` (e.g. `0035_assets_source_kind_agent`), the `mime.FormatMediaType` vs ported-helper choice for D-11, and the internal signature for injecting `assets.Service` into `SendFile` — all planner/executor discretion, provided the decisions above hold.

### Deferred Ideas (OUT OF SCOPE)
- Dedicated `threadctx` package; presigned-redirect delivery; HTTP Range / resumable downloads; inline in-browser preview allowlist; opt-in knowledge indexing of a deliverable (`send_file index:true`); agent-asset retention/TTL policy (Phase 39). **Also out of scope this phase:** the "Artefatti" side panel + "Scarica tutto" (WEBART-05..08 = Phase 37B).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| WEBART-01 | `send_file` stores bytes in the authenticated identity's Garage store + creates a thread-scoped owned `assets.Asset` (mirroring `IngestTelegramFile`); no raw path as the delivery handle | New `assets.Service.IngestAgentFile` mirrors `service.go:257-327` minus `Limits.Validate` (D-04) + `processAsset` (D-03); `SourceKind=SourceAgent` (D-06); identity via `identityctx.IdentityID(ctx)`, thread via `toolCallCtx(ctx).sessionID`; migration `0035` relaxes `0020_assets.up.sql:4` CHECK. RLS-free write path (0032 does not cover `assets`). |
| WEBART-02 | `aura.artifact` carries `asset_id`, `filename`, `size_bytes`, `mime_type`; Telegram unregressed (regression test) | Descriptor built in `emitDelivery` (`send_file.go:136-140`) flows Meta→`ArtifactDelta`→CUSTOM event **untouched** (`llm_agent_events.go:141`, `translator.go:365-377`). Add the 4 fields + `tool_call_id` there. Telegram still reads `path` (`telegram/artifact.go:55`, `artifact_test.go:161`) → unregressed. Zero translator/event changes. |
| WEBART-03 | `GET /api/assets/{id}/download` streams from Garage with `Content-Disposition: attachment`, `GetForIdentity` ownership, inherits `RequireAuth`; non-owner → 404/403; no unauth surface | New route in `registerAssetRoutes` (`assets_api.go:11`) modeled on `handleAssetGet` (`:103-119`); new `assets.Service` streaming method (`objects.Get` → `io.Copy`); `RequireAuth` inherited whole-origin (`auth.go:183`); `principalIdentityID` (`assets_api.go:161`); 404 on `GetForIdentity` miss (D-12). |
| WEBART-04 | Web consumes `aura.artifact`, renders authenticated download button; no raw path to browser; CLI/no-identity degrades; nil asset service does not break delivery | `sseAdapter.ts:305-316` CUSTOM branch gains an `aura.artifact` case; `LocalArtifactDisplay.tsx:57-66` swaps path chip → `<a>` download when `asset_id` set; `DisplayArtifact` type (`types.ts:51-55`) gains `asset_id`/`mime_type`. Degrade = no `asset_id` → today's card. |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Byte persistence (agent artifact → object store) | API / Backend (`assets.Service`) | Database / Storage (Garage + Postgres row) | Ingest is a backend orchestration mirroring the Telegram lane; the tool is a thin caller |
| Delivery-handle production (`asset_id`) | API / Backend (`send_file` tool) | — | The tool resolves identity/thread from ctx and calls the ingest seam; substrate stays channel-unaware |
| Event fan-out (`aura.artifact` with metadata) | API / Backend (agent loop + AG-UI translator) | — | Descriptor rides the existing `ArtifactDelta` seam untouched; no new event tier |
| Authenticated streaming download | API / Backend (agui `Server` route) | Database / Storage (Garage read) | Same-origin, auth-middleware-gated stream-through; NOT a CDN/presign tier (D-09) |
| Download-button render | Browser / Client (React `LocalArtifactDisplay`) | Frontend Server (SSE stream) | Pure client render off the SSE-reduced state; `<a href>` hits the backend route |
| Ownership enforcement | API / Backend (`GetForIdentity`) + Database (app-layer scope) | — | Assets are NOT RLS-covered; ownership is the single `WHERE id=$1 AND identity_id=$2` query (D-12) |

## Standard Stack

**No new third-party dependencies.** The entire phase is built from Go stdlib + already-vendored internal packages + existing web deps. This is a deliberate, verified finding — a delivery lane, not a redesign.

### Core (all pre-existing / stdlib)
| Component | Source | Purpose | Why Standard |
|-----------|--------|---------|--------------|
| `mime.FormatMediaType` | Go stdlib `mime` | RFC-6266 `filename*` param encoding (D-11) | Already imported in `internal/assets/service.go:11`; emits `filename*=UTF-8''...` correctly. `[VERIFIED: codebase grep]` |
| `net/url.PathEscape` / `QueryEscape` | Go stdlib `net/url` | Percent-encode the UTF-8 filename for the `filename*` param | stdlib; no dep |
| `io.Copy` | Go stdlib `io` | Stream Garage body → `http.ResponseWriter` (D-09) | Same pattern already used in `hashAndSniff` (`service.go:383`) and `send_file_sandbox.go:91` |
| `objectstore.Store.Get` | `internal/objectstore/types.go:55` | `Get(ctx, ref) (io.ReadCloser, Attrs, error)` — the download read | Existing interface; fake available (`objectstore/fake.go`) |
| `objectstore.Store.Put` / `AssetKey` | `internal/objectstore/types.go:53,60` | Ingest write under per-identity key | Existing; `IngestTelegramFile` uses it verbatim (`service.go:301`) |
| `internal/objectstore/fake.go` | in-repo fake | Daemon-free unit tests of ingest + download | Keeps tests in the coverage gate's tag set |

### Supporting (frontend, pre-existing)
| Component | Source | Purpose |
|-----------|--------|---------|
| vitest + @testing-library/react | `web/` | Unit tests for `sseAdapter` reducer + `LocalArtifactDisplay` |
| i18next `useTranslation` | `LocalArtifactDisplay.tsx:2` | Download-button label needs a new `display.artifact.download*` i18n key |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `mime.FormatMediaType` | Ported LibreChat ~10-line RFC-6266 helper (`api/server/utils/files.js:67-88`) | The port gives explicit control of the ASCII fallback (strip non-ASCII to `_`), which `FormatMediaType` does not do for the plain `filename=` param. **Recommend the ~15-line helper** — see Landmine 4: `FormatMediaType` alone will re-quote/encode the plain param unpredictably for non-ASCII and does not emit the dual-param form in one call. |
| Narrow injected interface | Direct `*assets.Service` field on `SendFile` | `tools → assets` is acyclic (`assets` does not import `agent`), so a direct field compiles. **Recommend a narrow interface** (`AssetDeliverer`) defined in the tools package for substrate-cleanliness + the golang-structs-interfaces "interface segregation" rule + to keep `send_file`'s "names no channel/no concrete service" invariant. |

**Installation:** none.

## Package Legitimacy Audit

**Not applicable — this phase installs zero external packages.** All code uses Go stdlib (`mime`, `net/url`, `io`, `net/http`), already-vendored internal packages (`internal/assets`, `internal/objectstore`, `internal/identityctx`, `internal/agui`), and existing web dependencies (vitest, react-i18next). `go.mod`/`go.sum`/`package.json` are expected **byte-unchanged** (matches the 36-1x / 37-10 precedent). If the executor believes a new package is needed, that is a plan deviation requiring the Package Legitimacy Gate first.

## Anchor Verification (CONTEXT.md drift check — Focus #2)

Every `repo/path:line` cited in CONTEXT.md, re-confirmed against the current tree 2026-07-08:

| CONTEXT anchor | Claimed | Current tree | Status |
|----------------|---------|--------------|--------|
| `send_file.go` `emitDelivery` | `:123` | `:123` (`func emitDelivery`), descriptor at `:136-140` | ✅ CURRENT |
| `send_file.go` `maxSendFileBytes` | `:47` | `:47` (`50 << 20`) | ✅ CURRENT |
| `send_file.go` `Router.Route` | `:97` | `:97` (`s.Router.Route(ctx)`) | ✅ CURRENT |
| `send_file_sandbox.go` `deliverFromBox` | (the other tail) | `:24` (`func (s *SendFile) deliverFromBox`), emits via `emitDelivery` at `:49` | ✅ CURRENT — **BOTH tails funnel through `emitDelivery`** (see Landmine 1) |
| `assets/service.go` `IngestTelegramFile` | `:257` | `:257` | ✅ CURRENT |
| `assets/service.go` `GetForIdentity` | `:174` | `:174` | ✅ CURRENT |
| `assets/service.go` `processAsset` (D-03 skips) | — | `:336`; called at tail of `IngestTelegramFile` (`:326`) | ✅ CURRENT |
| `assets/service.go` `Limits.Validate` (D-04 skips) | — | called at `:270` AND `:309` in `IngestTelegramFile` | ✅ CURRENT — **two call sites to skip** |
| `assets/types.go` `SourceKind` constants | `:60-67` | `:64-68` (`SourceWeb`/`SourceTelegram`/`SourceCLI`); add `SourceAgent` | ✅ CURRENT (const block at 64-68) |
| `assets/store.go` `GetForIdentity` | `:56` | `:56` (`WHERE id AND identity_id` via `GetAssetForIdentity`) | ✅ CURRENT |
| `0020_assets.up.sql` `source_kind` CHECK | `:4` | `:4` (`CHECK (source_kind IN ('web','telegram','cli'))`) — inline column check → auto-named `assets_source_kind_check` | ✅ CURRENT |
| `agui/assets_api.go` `registerAssetRoutes` | `:11` | `:11` | ✅ CURRENT |
| `agui/assets_api.go` `handleAssetGet` (template) | `:103` | `:103-119` | ✅ CURRENT |
| `agui/assets_api.go` `principalIdentityID` | `:161` | `:161` | ✅ CURRENT |
| `agui/translator.go` `ArtifactEventName` | `:19` | `:19` (`= "aura.artifact"`) | ✅ CURRENT |
| `agui/translator.go` `Actions.ArtifactDelta` emit | `:133`,`:366` | standalone branch guard `:133`, emit `:137`; the **live** tool-result path is `emitToolResultCustom` `:365-377` (emit `:367`) | ✅ CURRENT — see Landmine 6 |
| `telegram/artifact.go` `artifactDescriptor` | `:74` | `:74`; reads `path` at `:55` | ✅ CURRENT |
| `web/src/chat/sseAdapter.ts:309` (ignored) | `:309` | `:305-316` CUSTOM branch; only `aura.display` handled, `aura.artifact` falls through | ✅ CURRENT |
| `web/src/chat/sseAdapter_frames.ts` classifier | — | `CustomFrame {type,name,value}` at `:103-107`; comment `:112` "aura.artifact ... not modelled" | ✅ CURRENT |
| `web/src/chat/displays/LocalArtifactDisplay.tsx` | — | path chip at `:57-66`; `formatSize` at `:20`; no download action (comment `:6-9`) | ✅ CURRENT |
| latest migration on disk | `0034` | `0034_scheduler_sandbox_reap_kind` (both `.up`/`.down`) — **`0035` free** | ✅ CONFIRMED |
| composition root `SendFile` registration | — | `cmd/aura/main.go:199` (`&tools.SendFile{WorkspaceRoot: workspace, Router: sandboxRouter}`) | ✅ CURRENT — single production registration |

**Verdict: zero drift.** All decisions are safe to plan against verbatim.

## Architecture / Integration Mechanics (discretionary gaps — Focus #3)

### Gap A — Injecting `assets.Service` into `SendFile` (identity + thread resolution)

**The import-cycle fact.** `send_file.go` lives in `internal/agent/tools`. `internal/agent/tools` **cannot** import `internal/agent` (the reverse edge exists: `agent` imports `tools`). Therefore D-08's literal instruction — read `agent.SwarmContext(ctx).ConvID` — is **impossible from `send_file.go`**. [VERIFIED: codebase grep]

**The clean equivalent.** The conversation id is available in-package via the tool-call context: `WithToolCallContext(spanCtx, a.sessionID, call.ID, a.runDir, a.previewCap)` (`llm_agent.go:545`) sets `sessionID = a.sessionID`, and `WithSwarmContext(..., a.sessionID, ...)` (`:546`) uses the **same** `a.sessionID` as `ConvID`. The equivalence `sessionID == conversationID == ConvID` is documented at `evict.go:8` ("sessionID == conversationID per D-26") and `task.go:220-225`. So `send_file` reads the thread id as **`toolCallCtx(ctx).sessionID`** — no import cycle, and `""` when absent → D-05 degrade. [VERIFIED: codebase grep]

**Identity resolution.** `identityctx.IdentityID(ctx)` (`internal/identityctx/identityctx.go:26`) resolves the authenticated principal at `Execute` time — the channel scopes the turn ctx (`telegram scopeTurnToIdentity` wraps `identityctx.WithIdentityID`; web principal scoping does the same), and `context.WithValue` chaining preserves it down to the tool ctx. `internal/agent/tools` **already imports** `internal/identityctx` (`shell_bg_owner.go`), so there is zero cycle risk and an established precedent. Empty identity (CLI/no-principal) → D-02/D-05 degrade. [VERIFIED: codebase grep]

**The injection seam — recommended.** Add a narrow interface field on `SendFile`, wired **post-construction on `runtimeToolHandles`**, exactly mirroring the shipped `ShellPoll/ShellKill.Caps` VERIF-7 pattern (`main.go:181-184` retains the pointers on `handles`; `serve.go` sets `.Caps` at boot behind a nil guard):

```go
// in internal/agent/tools (new, keeps send_file free of the assets import)
type AssetDeliverer interface {
    // IngestAgentDelivery stores host-file bytes under the identity's object store and
    // returns the created thread-scoped asset id. Primitive-typed so the tools package
    // never imports internal/assets (substrate names no concrete service). Best-effort:
    // any error => the caller degrades to path-only (D-02).
    IngestAgentDelivery(ctx context.Context, identityID, threadID, hostPath, filename, mimeType string, size int64) (assetID string, err error)
}

type SendFile struct {
    // ... existing fields ...
    Assets AssetDeliverer // nil on CLI/static registry => path-only (D-02). Set at serve boot.
}
```

- `*assets.Service` satisfies this structurally via a thin `IngestAgentDelivery` adapter method (defined in `cmd/aura`, the only package importing both), so `assets` stays decoupled from `tools`.
- **Wiring path:** `main.go:199` currently `reg.Register(&tools.SendFile{...})`. Retain the `*SendFile` on `runtimeToolHandles` (like `handles.ShellPoll = sp`), then at serve boot set `handles.SendFile.Assets = sendFileAssetAdapter{chat.assets}` after `chat.assets` is built (`serve.go:292`). This leaves the static `buildRegistry()` path (`aura tools`, `cache_audit.go:190`) and `finetune/exporter` + spikes with `Assets == nil` → path-only degrade — exactly D-02. **Do NOT** change `buildRegistry()`/`buildRegistryWithMCP` signatures to carry the service; the post-construction set is smaller-blast-radius and matches the existing VERIF-7 precedent.

**Ordering note the planner must verify:** `chat.assets` is built at `serve.go:292` and `aguiServer.SetAssetService` at `:364`; the registry is built in the chat-boot path (`chat_boot.go:277`). Confirm the `handles.SendFile.Assets` set happens **after** `buildAssetService` returns (post-construction set makes ordering trivial — set it wherever `chat.assets` is known to be non-nil).

### Gap B — The ingest method (`assets.Service.IngestAgentFile`)

Copy `IngestTelegramFile` (`service.go:257-327`) into a new `IngestAgentFile(ctx, AgentIngestRequest) (Asset, error)` with exactly these deltas:

1. **Request type:** an `AgentIngestRequest{IdentityID, ThreadID, FileName, MIMEType, Modality, SizeBytes, Reader}` — no `ChatID/MessageID/FileID/SourceRef` (agent has no Telegram source ref; leave `SourceRef` empty).
2. **`SourceKind: SourceAgent`** (new constant, D-06).
3. **Skip `Limits.Validate`** at both `service.go:270` and `:309` (D-04 — the file already cleared `maxSendFileBytes`).
4. **Stop before `processAsset`** — return the asset immediately after `MarkAccepted` (`service.go:322-325`); do NOT call `s.processAsset` (`:326`) (D-03).
5. Keep the rest verbatim: `objectsFor(identityctx.WithIdentityID(ctx, req.IdentityID))` (`:273`), `AssetKey` (`:278`), `Store.Create` (`:283`), `objects.Put` (`:301`), `MarkUploaded` (`:314`), `hashAndSniff` (`:318`), `MarkAccepted` (`:322`).
6. **Reader:** the caller passes `os.Open(hostPath)` (an `io.ReadCloser`); `Put` takes an `io.Reader` (`objectstore/types.go:53`). Close it in the tool after ingest.
7. **`Modality`** is still needed for the `Create` row — call `InferModality(name, mimeType)` (used at `service.go:266-269`); it does not gate/reject, so D-04 is unaffected.

The **best-effort degrade (D-02)** lives in `send_file.go`, not the service: if `IngestAgentDelivery` returns an error (or `Assets == nil`, or `identityID == ""`, or `threadID == ""`), `emitDelivery` proceeds with the legacy `{path,filename,caption}` descriptor and no `asset_id`.

### Gap C — The descriptor enrichment (WEBART-02) + frontend correlation

`emitDelivery` (`send_file.go:123-148`) currently builds `{path, filename, caption}`. **The descriptor is the single source of the SSE fields** — it flows Meta→`ArtifactDelta` (`llm_agent_events.go:141`, `metaArtifact:163` reads `(*meta)["artifact"]` as `map[string]any`) → `emitToolResultCustom` (`translator.go:365-377`) → `events.NewCustomEvent("aura.artifact", WithValue(descriptor))`. **No change is needed in `llm_agent_events.go` or `translator.go`.** [VERIFIED: codebase grep]

`emitDelivery` must gain the ingest-derived fields. Because `emitDelivery` is a package function without ctx, either (a) pass the extra fields in, or (b) do the ingest in `Execute`/`deliverFromBox` and pass an enriched descriptor down. Recommended: `Execute`/`deliverFromBox` resolve identity+thread+call the ingest, then pass `assetID`, `size`, `mimeType`, `toolCallID` into `emitDelivery` (or a variant). Final descriptor on success:

```
{path, filename, caption, asset_id, size_bytes, mime_type, tool_call_id}
```

**Frontend correlation landmine (see Landmine 5):** the `aura.artifact` CUSTOM event value is *only* the descriptor map — unlike `aura.display` it carries **no** `tool_call_id`. The web reducer attaches display payloads to a tool part *by `tool_call_id`* (`sseAdapter.ts:311`). Add `tool_call_id` to the descriptor (available as `toolCallCtx(ctx).toolCallID`, set at `llm_agent.go:545` as `call.ID`) so the `sseAdapter` `aura.artifact` branch can `ensureTool(state, id, ...)` and merge the asset fields into a `local_artifact` `DisplayPayload`. This keeps D-13's "route into the existing card" without inventing a synthetic id.

### Gap D — The download route + a new streaming `Service` method

`s.assets` in the agui `Server` is typed as the `AssetService` interface (`server.go:151` `SetAssetService(service AssetService)`). It currently exposes `GetForIdentity`, `Presign`, `Finalize`, `ListForThread`, `BuildTurnContext`, etc. — **no streaming method.** Add one to BOTH the agui `AssetService` interface AND `assets.Service`:

```go
// assets.Service — resolves the per-identity store + streams the object body.
func (s *Service) OpenForIdentity(ctx context.Context, id, identityID string) (io.ReadCloser, Asset, error) {
    asset, err := s.GetForIdentity(ctx, id, identityID)      // D-12 ownership (404 on miss)
    if err != nil { return nil, Asset{}, err }
    objects, _, err := s.objectsFor(identityctx.WithIdentityID(ctx, identityID))
    if err != nil { return nil, Asset{}, err }
    rc, _, err := objects.Get(ctx, objectstore.ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey})
    return rc, asset, err
}
```

The handler (new `handleAssetDownload`, modeled on `handleAssetGet` `:103-119`):
1. `principalIdentityID(r)` → 401 if `!ok` (though `RequireAuth` already gated the origin — belt-and-suspenders, matches `handleAssetGet`).
2. `s.assets.OpenForIdentity(r.Context(), r.PathValue("id"), identityID)` → **404 on any error** (D-12; hides existence).
3. Set headers **before** first write: `Content-Type: application/octet-stream` (D-10), `Content-Disposition: attachment; filename="<ascii>"; filename*=UTF-8''<pct>` (D-11), optionally `Content-Length` from `asset.SizeBytes` and `X-Content-Type-Options: nosniff`.
4. `defer rc.Close()`; `io.Copy(w, rc)` with `r.Context()` already scoping the Garage read (D-09) — a client disconnect cancels the read.
5. Register `mux.HandleFunc("GET /api/assets/{id}/download", s.handleAssetDownload)` in `registerAssetRoutes` (`assets_api.go:11`). `RequireAuth` is inherited whole-origin (`auth.go:183`) — no per-route wiring. [VERIFIED: codebase grep]

### Gap E — Migration 0035 (mirror 0033/0034 verbatim)

The `0020` CHECK is an **inline column check** → Postgres auto-names it `assets_source_kind_check`. Mirror the `0034` widen shape exactly (`0034_scheduler_sandbox_reap_kind.up.sql:14-16`):

```sql
-- 0035_assets_source_kind_agent.up.sql
ALTER TABLE aura.assets DROP CONSTRAINT assets_source_kind_check;
ALTER TABLE aura.assets ADD  CONSTRAINT assets_source_kind_check
    CHECK (source_kind IN ('web', 'telegram', 'cli', 'agent'));
```

```sql
-- 0035_assets_source_kind_agent.down.sql  (pre-delete 'agent' rows, like 0033 down)
DELETE FROM aura.assets WHERE source_kind = 'agent';
ALTER TABLE aura.assets DROP CONSTRAINT assets_source_kind_check;
ALTER TABLE aura.assets ADD  CONSTRAINT assets_source_kind_check
    CHECK (source_kind IN ('web', 'telegram', 'cli'));
```

No grant change (`0020:68` already grants `aura_app` DML; `aura_migrate` owns DDL). `sqlc generate` is **zero-diff** (a CHECK widen touches no query). Add `SourceAgent SourceKind = "agent"` to `types.go:64-68`.

## Runtime State Inventory

This phase adds a migration + writes new-kind rows; the inventory:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `aura.assets` rows: existing `web`/`telegram`/`cli` rows are untouched; new `source_kind='agent'` rows written going forward. **Ordering constraint (Landmine 3):** migration `0035` (CHECK relax) MUST be applied before any `IngestAgentFile` writes an `'agent'` row, else `23514 check_violation`. golang-migrate applies `0035` at boot before serving, so ordering holds — but the executor must not seed `'agent'` rows in a test against a pre-0035 schema. | code edit + forward migration |
| Live service config | None — no external service (n8n/Datadog/Tailscale) references this string. Garage buckets are keyed by identity UUID (`AssetKey`), not by `source_kind`. | none |
| OS-registered state | None — no Task Scheduler / pm2 / systemd unit embeds asset source kinds. | none (verified: no OS registration touches `assets`) |
| Secrets / env vars | None new. Object-store creds (`AURA_OBJECT_STORE_*`, `AURA_AUTHULA_SECRET`) already consumed by `buildAssetService`/`buildObjectResolverBundle` (`document_processor_wiring.go:67-84`); the agent lane reuses them. | none |
| Build artifacts | `internal/webui/dist` (the embedded web bundle) must be **rebuilt + committed** after the `sseAdapter.ts`/`LocalArtifactDisplay.tsx` change — the `file-size` hook + `web-dist-freshness` CI job go red on a stale bundle (QUAL-01 precedent). | rebuild `web` dist + commit |

**RLS note (verified, load-bearing):** `aura.assets` is **not** RLS-covered. Migration `0032_owner_rls.up.sql` enables RLS only on `conversations`, `paused_states`, `conversation_turns`. The agent-ingest write path therefore needs **no** `app.current_identity` GUC and **no** `WithIdentityTx` — it reuses the exact pooled `IngestTelegramFile` orchestration. Ownership is enforced purely app-layer by `GetForIdentity` (`WHERE id AND identity_id`). [VERIFIED: `0032_owner_rls.up.sql` full read]

## Common Pitfalls / Landmines (Focus #4)

### Landmine 1: `send_file` has TWO delivery tails — both must ingest
`Execute` (host path, `send_file.go:116` → `emitDelivery`) **and** `deliverFromBox` (routed sandbox path, `send_file_sandbox.go:49` → `emitDelivery`) both funnel through `emitDelivery`. **Good news:** because both converge on `emitDelivery`, doing the ingest *inside* `emitDelivery` (or a shared helper it calls) covers both tails with one change. **But** `emitDelivery` currently has no ctx — the ingest needs ctx (for identity/thread/store). Refactor: give the shared tail the ctx (both callers have it), resolve+ingest there. Verify **both** `TestSendFile_*` host-path tests AND the routed `deliverFromBox` path exercise the ingest. Missing the routed tail = sandbox-produced artifacts (the *primary* WEBART use case: "a DOCX the model builds inside the sandbox", REQUIREMENTS.md:61) silently degrade to path-only on web.

### Landmine 2: keep `send_file` `Deferred:true` / `Mutating:false` and channel-agnostic
`Spec()` (`send_file.go:63-77`) sets `Deferred: true, Mutating: false`. Adding a Garage write does **not** change the classification (CONTEXT D-100 / code_context): it delivers, it does not mutate host state the completion-gate critic arms on. **Do not** flip `Mutating` — that would arm the completion gate and require a ledger reservation (GATE-03) for every file delivery. Also **do not** add channel-named fields or logic; the doc-comment invariant (`:15-22` "The substrate never names any channel") is enforced by an existing channel-agnostic grep test (13-02-SUMMARY). The descriptor keys (`asset_id`/`size_bytes`/`mime_type`) are channel-neutral metadata — safe.

### Landmine 3: migration-vs-write ordering (`23514`)
`IngestAgentFile` writes `source_kind='agent'`; the `0020` CHECK rejects it until `0035` lands. Boot applies migrations before serving, so production is safe. **The trap is tests:** a `db_integration` test that inserts an `'agent'` asset against a schema migrated only to `< 0035` gets `23514 check_violation`. Ensure the test harness migrates to HEAD. The `0035` roundtrip test itself must assert: INSERT `'agent'` OK after up; `23514` before up / after down (mirror `migrate_0033_integration_test.go`).

### Landmine 4: RFC-6266 dual encoding is a header-injection surface (D-11)
The download filename comes from `asset.FileName` (user/agent-influenced). A naive `Content-Disposition: attachment; filename="`+name+`"` is a **CRLF/quote header-injection** vector (gin PR #3556, cited in D-11). `mime.FormatMediaType("attachment", map[string]string{"filename": name})` handles the plain param but does **not** emit the `filename*` UTF-8 form and will *drop* the whole media type if `name` contains bytes it deems invalid (returns `""`). **Recommend the ~15-line dual-param helper** (port LibreChat `files.js:67-88`): (a) ASCII-fold + strip control chars/`"`/`\`/CR/LF for the fallback `filename="..."`; (b) `url.PathEscape`(UTF-8 name) for `filename*=UTF-8''...`. This is a **prime property-based-testing target** (see Validation Architecture) — fuzz unicode, embedded `"`, `\r\n`, `;`, path separators, empty, and >255-byte names; assert the output header has exactly one `filename=` + one `filename*=`, no raw CR/LF, and round-trips the unicode. Note: `send_file`/telegram ASCII-fold the *caption* (`asciiCaption`, `send_file.go:223`) but the *download filename* keeps unicode (D-11) — **do not** reuse `asciiCaption`/`foldToASCII` for the filename (code_context explicit).

### Landmine 5: `aura.artifact` has no `tool_call_id` — frontend can't correlate
`aura.display` carries `tool_call_id` (`sseAdapter.ts:311`); `aura.artifact`'s value is just the descriptor map (`translator.go:367`). Without a correlation key the web reducer cannot `ensureTool`/attach the card. **Fix in the descriptor** (add `tool_call_id`, Gap C). Also note the current test `web/src/chat/__tests__/sseAdapter.test.ts:383` ("an unrecognized CUSTOM name (aura.artifact) is a no-op") **encodes the old drop contract** — it must be *rewritten* (not deleted) with an explicit commit-message justification per CLAUDE.md ("NEVER MODIFY TESTS TO MAKE THEM PASS unless the test itself is broken" — here the test asserts the behavior we are intentionally changing, which is the legitimate exception).

### Landmine 6: the artifact CUSTOM event is emitted from `emitToolResultCustom`, not the standalone branch
For a `send_file` result the descriptor rides the **same** event as the TOOL_CALL_RESULT, so the live emit is `emitToolResultCustom` (`translator.go:365-377`, called at `:104`), and the standalone artifact branch (`:133-141`) is *unreachable for a tool result* (documented `:96-107`). Any translator-level test/assertion must drive the tool-result path, not the standalone branch. (No code change needed — just don't be misled when reading/testing.)

### Landmine 7: `local_artifact` display today comes from shell/sandbox exec, not `send_file`
`normalizeCode` (`display/code.go:20-31`) produces the `local_artifact` `DisplayPayload` for **code-producing tools** that name an artifact file — NOT `send_file`. So today `send_file` on web renders *nothing* (its `aura.artifact` is dropped). D-13 routes `aura.artifact` into the *same* `LocalArtifactDisplay` component. The planner must decide the exact mapping (Gap C): build a synthetic `local_artifact` `DisplayPayload` from the `aura.artifact` descriptor in `sseAdapter` and attach by `tool_call_id`. `DisplayArtifact` (`types.ts:51-55`) needs `asset_id?: string` + `mime_type?: string` added; `LocalArtifactDisplay` renders `<a href="/api/assets/{asset_id}/download" download={filename}>` when `asset_id` is set, else the existing chip. Keep the type-string contract explicit (D-13 / Elysia footgun).

### Landmine 8: `Content-Length` + `io.Copy` mismatch on a truncated Garage object
If you set `Content-Length` from `asset.SizeBytes` but the Garage object is shorter (partial upload), `io.Copy` writes fewer bytes and the client sees a hung/short response. Since ingest sets `size` from `attrs.SizeBytes` after `Put` (`MarkAccepted`, `service.go:322`), `asset.SizeBytes` is the *stored* size — safe. But if resolving the per-identity store fails mid-stream, prefer letting Go compute length (omit `Content-Length`, use chunked) OR stream to a `bytes.Buffer` first for small files. **Recommend:** set `Content-Length` from `asset.SizeBytes` (authoritative post-`MarkAccepted`) and accept the simplification; document it.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Object store put/get | A bespoke Garage client call in `send_file` | `assets.Service` ingest mirror + `objectstore.Store.Get` | Per-identity credential resolution (`objectsFor`), hashing, MIME sniff, lifecycle rows all live in the service already (`service.go:257-327`) |
| Ownership check | A new `SELECT` in the download handler | `assets.Store.GetForIdentity` (`store.go:56`) | Ready-made owner-scoped read; 404-on-miss is the D-12 contract |
| Auth on the route | Per-route token check | `RequireAuth` whole-origin inheritance (`auth.go:183`) | Every `/api/assets/*` route already inherits it; adding one to `registerAssetRoutes` is enough |
| Content-Disposition encoding | Hand-concatenate `filename=`+name | Ported RFC-6266 dual-param helper (property-tested) | Header-injection + unicode-loss footguns (Landmine 4) |
| Thread-id plumbing | A new context key / `threadctx` package | `toolCallCtx(ctx).sessionID` | Already carries ConvID (`llm_agent.go:545`); the dedicated key is an explicit deferred idea |
| SSE event for the new fields | A new `aura.artifact.v2` event | Extend the descriptor map | Meta→ArtifactDelta→CUSTOM passes it through untouched (D-07); Telegram tolerates extra keys |

**Key insight:** this phase is ~90% *calling existing seams in the right order* and ~10% new code (one ingest method, one download method+handler, one migration, one descriptor enrichment, one frontend branch). The reference clones in `D:\tmp` (LibreChat/open-webui/assistant-ui/ag-ui/elysia) are for *pattern confirmation only* — every actual API this phase calls is already in-tree.

## Validation Architecture

> `workflow.nyquist_validation = true` (`.planning/config.json`) — this section is authoritative for `37A-VALIDATION.md`.

### Test Framework
| Property | Value |
|----------|-------|
| Backend framework | Go `testing` + `gotestsum`; table-driven; `-race`; `goleak` where goroutines/streams (golang-testing skill) |
| Backend config file | none (standard `go test`); lint `.golangci.yml`; coverage `scripts/coverage_gate.sh` |
| Property-based | `pgregory.net/rapid` or `gopter` (property-based-testing skill) for the RFC-6266 helper |
| Frontend framework | vitest + @testing-library/react (`web/`), jsdom |
| Backend quick run | `go test ./internal/agent/tools/ ./internal/assets/ ./internal/agui/ -run 'SendFile|Ingest|AssetDownload|Artifact'` |
| Backend full/gated | `bash scripts/coverage_docker.sh` (containerized stack) or WSL `make quality-full` (`db_integration neo4j_integration`) |
| Frontend quick run | `cd web && npx vitest run src/chat` |
| Migration roundtrip | `go test -tags db_integration ./internal/db/ -run TestMigration0035` |

### Coverage-floor trap (CLAUDE.md — MANDATORY awareness)
The coverage gate runs **`db_integration neo4j_integration` ONLY** — there is no `docker_integration`/`garage_integration` job in the gate. Therefore **every new pure-logic surface must be daemon-free unit-testable** or it drops the aggregate below 85% and fails CI ~20 min post-push:
- Use `internal/objectstore/fake.go` (in-memory Store) + a fake/real `assets.StoreBackend` so `IngestAgentFile` orchestration (skip-Limits, `SourceAgent`, skip-processAsset, Put-error degrade) and `OpenForIdentity` stream logic are covered **without** a live Garage. **Do NOT** gate these behind `garage_integration` — that tag is outside the gate and contributes ZERO coverage.
- The RFC-6266 helper, the degrade-decision logic in `send_file`, and the `Content-Disposition`/`octet-stream` header forcing are **pure** → 100% unit-coverable, no tags.
- Only the migration roundtrip (real Postgres) and the real-asset-row ownership assertions need `db_integration` (which IS in the gate).
- `web` coverage is measured separately (≥85% web floor per the WEBART-08 pattern) — vitest `--coverage`.

### Phase Requirements → Test Map
| Req | Behavior | Type | Automated command | File status |
|-----|----------|------|-------------------|-------------|
| WEBART-01 | `IngestAgentFile`: bytes→fake `Put`→`Create(source_kind=agent)`→`MarkUploaded`→`MarkAccepted`; **`processAsset` NOT called**; **`Limits.Validate` NOT called** (oversized-per-modality still ingests) | unit (fakes) | `go test ./internal/assets/ -run TestIngestAgentFile` | ❌ Wave 0 |
| WEBART-01 | `send_file` degrade matrix: nil `Assets` / empty identity (`identityctx` unset) / empty thread (`sessionID==""`) / `Put` error → descriptor has `path`, **no** `asset_id`; delivery never errors the turn | unit (fakes) | `go test ./internal/agent/tools/ -run TestSendFile_Degrade` | ❌ Wave 0 |
| WEBART-01 | Both tails ingest: host-path `Execute` AND routed `deliverFromBox` produce `asset_id` (Landmine 1) | unit (fakes) | `go test ./internal/agent/tools/ -run 'TestSendFile_Ingest'` | ❌ Wave 0 |
| WEBART-01 | migration `0035` roundtrip: INSERT `agent` OK after up; `23514` before-up/after-down | integration `db_integration` | `go test -tags db_integration ./internal/db/ -run TestMigration0035` | ❌ Wave 0 |
| WEBART-02 | descriptor carries `asset_id`+`filename`+`size_bytes`+`mime_type`+`tool_call_id` on success | unit | `go test ./internal/agent/tools/ -run TestSendFile_DescriptorFields` | ❌ Wave 0 |
| WEBART-02 | **Telegram unregressed:** descriptor still carries `path`; `artifact.consumeEvent` still sends a document; missing-path still no-op | regression unit | `go test ./internal/channels/telegram/ -run 'Artifact'` | ⚠️ extend `artifact_test.go:161` |
| WEBART-02 | Meta→ArtifactDelta lift passes new keys through (no llm_agent_events/translator change) | unit | `go test ./internal/agent/ -run 'Artifact' && go test ./internal/agui/ -run 'Artifact'` | ⚠️ extend existing |
| WEBART-03 | download: owner → 200, `Content-Disposition: attachment` + `filename*`, `Content-Type: application/octet-stream`, body == bytes | integration/unit (httptest + fake store) | `go test ./internal/agui/ -run TestAssetDownload_Owner` | ❌ Wave 0 |
| WEBART-03 | **non-owner → 404** (D-12 regression); **unauthenticated → 401/302** (RequireAuth) | integration/unit | `go test ./internal/agui/ -run 'TestAssetDownload_NonOwner|TestAssetDownload_Unauth'` | ❌ Wave 0 |
| WEBART-03 | client-disconnect cancels the Garage read (ctx-scoped `io.Copy`, D-09) | unit (cancel ctx + goleak) | `go test ./internal/agui/ -run TestAssetDownload_ClientDisconnect` | ❌ Wave 0 |
| WEBART-03 | RFC-6266 helper: unicode/CRLF/quote/`;`/empty/long inputs → exactly one `filename=`+one `filename*=`, no raw CRLF (Landmine 4) | **property-based** unit | `go test ./internal/agui/ -run TestContentDisposition_Property` | ❌ Wave 0 |
| WEBART-04 | `sseAdapter` reducer: `aura.artifact` CUSTOM frame → `local_artifact` display attached by `tool_call_id`; degraded (no `asset_id`) → chip only | unit (vitest) | `cd web && npx vitest run src/chat/__tests__/sseAdapter.test.ts` | ⚠️ **rewrite** `:383` |
| WEBART-04 | `LocalArtifactDisplay`: `asset_id` present → `<a href="/api/assets/{id}/download" download={filename}>`; absent → path chip; never renders a raw host path when `asset_id` set | unit (vitest) | `cd web && npx vitest run src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` | ⚠️ extend |
| WEBART-04 | CLI/no-identity degrade end-to-end (nil `Assets` → path chip, no download button) | unit (both tiers) | covered by degrade-matrix + LocalArtifactDisplay tests | — |

### Sampling Rate
- **Per task commit:** the package-scoped quick run for the touched package (e.g. `go test -race ./internal/agent/tools/`), + `cd web && npx vitest run src/chat` for web-touching commits. Post-edit Gate-2: `go vet ./... && go build ./...`.
- **Per wave merge:** full untagged `go test ./internal/agent/... ./internal/assets/ ./internal/agui/ ./internal/channels/telegram/` + `-race` on touched packages + `db_integration` migration roundtrip.
- **Phase gate:** `bash scripts/coverage_docker.sh` (owned-surface ≥85% across `db_integration neo4j_integration`) + web coverage ≥85% + rebuilt `internal/webui/dist` committed. Full suite green before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/assets/ingest_agent_test.go` — `TestIngestAgentFile` (skip-Limits/skip-processAsset/SourceAgent) with `objectstore/fake.go`
- [ ] `internal/agent/tools/send_file_ingest_test.go` — degrade matrix + both-tails-ingest + descriptor fields (fake `AssetDeliverer`)
- [ ] `internal/agui/asset_download_test.go` — owner-200 / non-owner-404 / unauth / disconnect / header assertions (httptest + `objectstore/fake.go`)
- [ ] `internal/agui/content_disposition_test.go` — property-based RFC-6266 (rapid/gopter)
- [ ] `internal/db/migrate_0035_integration_test.go` — `db_integration` roundtrip (mirror `migrate_0033_integration_test.go`)
- [ ] `internal/channels/telegram/artifact_test.go` — **extend** with an enriched-descriptor-still-sends assertion (non-regression)
- [ ] `web/src/chat/__tests__/sseAdapter.test.ts` — **rewrite** the `:383` no-op test to assert `aura.artifact` now attaches the card
- [ ] `web/src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` — **extend** with download-button-when-asset_id + chip-when-degraded
- [ ] Framework: none to install — Go `testing`+`gotestsum`, vitest, and a property lib (`pgregory.net/rapid` — verify vendored; if absent, `gopter` may already be present per property-based-testing skill) are the only test deps. **Confirm the property lib is vendored before use** (Package Legitimacy Gate if adding one).

## Security Domain

> `security_enforcement` not disabled — included.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control (this phase) |
|---------------|---------|------------------------------|
| V4 Access Control (IDOR) | **yes** | `GetForIdentity` owner scope; **404 on non-owner** (D-12, existence-hiding); no unauth surface. The core security property. |
| V5 Input Validation / Output Encoding | **yes** | RFC-6266 dual-encode (D-11) neutralizes CRLF/quote header injection in the filename; `application/octet-stream` + `attachment` (D-10) neutralizes stored-XSS via sniffed HTML/SVG |
| V1/V12 File & Resources | **yes** | 50 MiB gate already enforced (`maxSendFileBytes`); stream is request-ctx-scoped (no unbounded read); path fence unchanged (`fenceWithinRoot`) |
| V2 Authentication | inherited | `RequireAuth` whole-origin (`auth.go:183`) — no new auth code |
| V6 Cryptography | no | none (no new secrets/hashing; reuses existing sha256 sniff) |
| V9 Data Protection (SSRF/store URL leak) | **yes** | Stream-through NOT presign (D-09) — no direct/expiring Garage URL ever reaches the client; same-origin preserved |

### Known Threat Patterns for {Go asset delivery + AG-UI SSE + Garage}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| IDOR: identity B downloads A's asset by guessing/enumerating `{id}` | Information Disclosure | `GetForIdentity` (`WHERE id AND identity_id`) → **404** (D-12); v4/v7 UUID ids are unguessable; regression test mandatory |
| Stored XSS via served HTML/SVG artifact rendered in-origin | Tampering / Elevation | Force `Content-Type: application/octet-stream` + `Content-Disposition: attachment` regardless of sniffed MIME (D-10); `X-Content-Type-Options: nosniff` |
| HTTP response header injection via filename (CRLF) | Tampering | RFC-6266 helper strips CR/LF/`"`/`\` from the fallback + percent-encodes `filename*` (D-11, Landmine 4); property-tested |
| Store-URL leak / SSRF via presigned redirect | Information Disclosure | Explicitly NOT presign (D-09); private per-identity bucket never exposed to the client |
| Agent artifact silently becoming searchable memory | Information Disclosure / Repudiation | Skip `processAsset` (D-03) — no embedding/knowledge-graph indexing of deliverables |
| Unauthenticated download surface | Spoofing | Route registered inside `registerAssetRoutes` → inherits `RequireAuth`; no public path added |
| DoS via unbounded stream / goroutine leak on disconnect | Denial of Service | request-ctx-scoped `io.Copy` (D-09); `defer rc.Close()`; goleak in the disconnect test |

## Environment Availability
| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all backend | ✓ | go1.25/1.26 (WSL CGO) | — |
| Postgres (aura schema, ≥0034) | migration roundtrip, asset rows | ✓ (WSL/CI stack) | 5432 | `db_integration` tag skips locally, `t.Fatal` under `$CI` |
| Garage object store | full E2E ingest/download | ✓ (WSL stack) | S3 API | `objectstore/fake.go` for all daemon-free tests (recommended path) |
| Node + vitest | web unit tests | ✓ | `web/` toolchain | — |
| Property lib (rapid/gopter) | RFC-6266 property test | ⚠️ verify vendored | — | table-driven exhaustive cases if unavailable |

**Missing with no fallback:** none. **Missing with fallback:** live Garage for unit tests → use `objectstore/fake.go` (the recommended, coverage-counting path).

## State of the Art
| Old Approach | Current Approach | Impact |
|--------------|------------------|--------|
| Web drops `aura.artifact`; agent files only reachable via Telegram or a leaked host path | Same-origin authenticated Garage-backed download + owned `assets.Asset` | Closes the WEBART web-parity gap; no raw container path in the browser |
| Presigned S3 URL (LibreChat also ships this) | Stream-through behind auth middleware (D-09) | Correct for a private per-identity store; presign reserved as a future strategy drop-in |

**Deprecated/outdated:** the STATE.md "Phase 37A web artifact delivery" memory note ("sequence AFTER Phase 37 plan 37-07 (same send_file.go)") is **satisfied** — 37-07 shipped (`deliverFromBox` + `Router` on `SendFile` are on master); the box→host `CopyArtifactOut` staging this lane depends on is present (`send_file_sandbox.go`). This phase can proceed.

## Assumptions Log
| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The property lib (`pgregory.net/rapid` or `gopter`) is already vendored | Validation Architecture | LOW — fall back to exhaustive table-driven RFC-6266 cases; no plan block |
| A2 | Setting `handles.SendFile.Assets` post-construction is orderable after `buildAssetService` in the serve path | Gap A | LOW — verified the ShellPoll/ShellKill VERIF-7 precedent does exactly this (`main.go:181-184` + serve boot); planner should confirm the exact set-site |
| A3 | `identityctx.IdentityID(ctx)` resolves the principal at tool `Execute` time on channel-driven runs | Gap A | LOW — verified telegram `scopeTurnToIdentity` + web principal scoping wrap the turn ctx; context chaining preserves it. If a run path does NOT scope identity, that run degrades to path-only (safe, D-02) |
| A4 | `mime.FormatMediaType` alone is insufficient for D-11 (recommend the ported helper) | Landmine 4 / Standard Stack | LOW — behavior of `FormatMediaType` on invalid runes (drops the type) is documented; the ported helper is the reference-backed choice; either satisfies D-11 if property-tested |
| A5 | Rebuilding `internal/webui/dist` is required after web changes | Runtime State Inventory | LOW — QUAL-01 + `web-dist-freshness` CI job precedent confirms it |

**All other claims are `[VERIFIED: codebase grep]` against the current tree — see Anchor Verification.**

## Open Questions
1. **Exact frontend mapping of `aura.artifact` → `LocalArtifactDisplay` (Gap C / Landmine 5/7).**
   - What we know: D-13 says route `aura.artifact` into the existing card; the card is fed by `tool_call_id`-correlated `DisplayPayload`s; `aura.artifact` currently lacks `tool_call_id`.
   - What's unclear: whether the executor builds a synthetic `local_artifact` `DisplayPayload` in `sseAdapter` from the descriptor, vs. threading `asset_id` into the *existing* `aura.display` `local_artifact` payload (`display/code.go`) which only fires for shell/sandbox exec, not `send_file`.
   - Recommendation: add `tool_call_id` to the descriptor (backend) + a synthetic-`local_artifact` branch in `sseAdapter` (frontend). This is the D-13-literal path and reuses the correlated attach. Hand this to `gsd-pattern-mapper` for the precise TSX shape.
2. **`Content-Length` on the download (Landmine 8).** Recommend setting it from `asset.SizeBytes` (authoritative post-`MarkAccepted`); planner to confirm no partial-object edge in the existing Garage `Put` path.

## Sources
### Primary (HIGH confidence — direct file reads, current tree 2026-07-08)
- `internal/agent/tools/send_file.go`, `send_file_sandbox.go` — delivery tails, `emitDelivery`, size gate, fence
- `internal/assets/service.go` (`IngestTelegramFile:257`, `processAsset:336`, `Limits.Validate:270,309`, `hashAndSniff:376`), `store.go` (`GetForIdentity:56`), `types.go` (`SourceKind:64-68`)
- `internal/objectstore/types.go` (`Store` interface, `AssetKey`), `internal/objectstore/fake.go` (exists)
- `internal/agui/assets_api.go` (`registerAssetRoutes:11`, `handleAssetGet:103`, `principalIdentityID:161`), `server.go:151` (`SetAssetService`), `auth.go:183` (`RequireAuth` whole-origin)
- `internal/agui/translator.go` (`ArtifactEventName:19`, `emitToolResultCustom:365`), `internal/agent/llm_agent_events.go:141` (`metaArtifact:163`), `internal/agent/event.go:72` (`ArtifactDelta map[string]any`)
- `internal/agent/llm_agent.go:545-546` (`WithToolCallContext`/`WithSwarmContext` sessionID==ConvID), `internal/agent/tools/result.go:19-47` (`toolCallContext.sessionID`), `internal/agent/swarm_context.go`, `evict.go:8`, `task.go:220-225`
- `internal/channels/telegram/artifact.go` (`artifactDescriptor:74`, reads `path:55`), `artifact_test.go:161`
- `internal/db/migrations/0020_assets.up.sql:4`, `0032_owner_rls.up.sql` (assets NOT RLS-covered), `0034_scheduler_sandbox_reap_kind.up.sql` (widen pattern), latest slot = 0034
- `web/src/chat/sseAdapter.ts:305-316`, `sseAdapter_frames.ts:103-116`, `displays/LocalArtifactDisplay.tsx`, `displays/types.ts:51-55,108-128`, `displays/DisplayRouter.tsx:62`, `internal/agent/display/code.go:20-31`
- `cmd/aura/main.go:199` (SendFile registration), `document_processor_wiring.go:17-84` (`buildAssetService`), `serve.go:292,364`, `chat_boot.go:51,277`

### Secondary (MEDIUM — CONTEXT.md reference clones, pattern confirmation only)
- `D:\tmp\LibreChat` (RFC-6266 helper `files.js:67-88`, force-download `files.js:541-548`, owner-scoped download `fileAccess.js:83-149`), `D:\tmp\open-webui` (404-on-non-owned `files.py:752-812`), `D:\tmp\assistant-ui` (`file.tsx:162-190`), `D:\tmp\ag-ui` (CustomEvent shape), `D:\tmp\elysia` (index-vs-display separation, unknown-type-null footgun)

## Metadata
**Confidence breakdown:**
- Standard stack: HIGH — no new deps; every API verified in-tree
- Architecture / integration: HIGH — all seams read line-for-line; the two non-obvious facts (import-cycle → `toolCallCtx.sessionID`; assets-not-RLS) directly verified
- Pitfalls / landmines: HIGH — each traced to a specific file:line or a shipped precedent (ShellPoll VERIF-7, 0033/0034 widen, QUAL-01 dist)
- Frontend mapping: MEDIUM — D-13 direction clear, exact TSX/reducer shape is a `gsd-pattern-mapper` follow-up (Open Q1)

**Research date:** 2026-07-08
**Valid until:** 2026-08-07 (stable brownfield surface; re-verify only if `send_file.go`, `assets/service.go`, `translator.go`, or the migration set changes before planning)
