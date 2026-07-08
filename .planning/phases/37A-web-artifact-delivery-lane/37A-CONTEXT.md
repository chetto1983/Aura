# Phase 37A: Web Artifact Delivery Lane - Context

**Gathered:** 2026-07-08
**Status:** Ready for planning

<domain>
## Phase Boundary

When the agent calls `send_file`, the produced file must reach the **web cockpit** as an authenticated, same-origin download — a Garage-backed, identity-scoped `assets.Asset` — instead of the raw container/host path the web chat currently drops on the floor (`sseAdapter.ts:309` ignores `aura.artifact`). Telegram already consumes `aura.artifact` via `internal/channels/telegram/artifact.go`; this phase closes the web gap **without regressing Telegram** and **without breaking the CLI/no-identity host-path behavior**.

**In scope:** the ingest lane in `send_file` (bytes → Garage + owned `assets.Asset`), the extra fields on the existing `aura.artifact` SSE event, a new `GET /api/assets/{id}/download` streaming route, and the web-chat download button.

**Out of scope (new capabilities → other phases):** asset retention/TTL policy (Phase 39), a dedicated `threadctx` package (deferred cleanup), presigned-redirect delivery, HTTP Range/resume, inline in-browser preview, opt-in knowledge-indexing of a deliverable.

</domain>

<decisions>
## Implementation Decisions

Requirements WEBART-01..04 are effectively locked (see canonical refs). These are the HOW decisions the discussion resolved, each grounded in the reference implementations cloned to `D:\tmp` (LibreChat, open-webui, assistant-ui, ag-ui, Elysia).

### Ingest lane (WEBART-01)
- **D-01 — Always ingest when authenticated (channel-agnostic).** Any `send_file` under an authenticated identity writes bytes to Garage (per-identity `AssetKey`) + creates a thread-scoped owned `assets.Asset`, mirroring `assets.Service.IngestTelegramFile` (`internal/assets/service.go:257`). The `aura.artifact` event carries **both** the legacy `{path}` (Telegram keeps using it, unregressed) **and** `asset_id`/`filename`/`size_bytes`/`mime_type` (web uses these). Preserves the "substrate names no channel" invariant that `send_file`'s doc-comment guards — do NOT make the tool channel-aware.
- **D-02 — Ingest failure → degrade to path-only, best-effort.** On Garage `Put` error / asset-service error while the host file is still readable: emit the event with `{path}` only (no `asset_id`). Telegram still delivers; web shows the render-only `local_artifact` card (no download button). Delivery NEVER wedges the turn — matches `send_file`'s existing best-effort stance. A nil asset service or no-authenticated-identity (CLI) → today's host-path behavior.
- **D-03 — Skip the asset processing pipeline (delivery-only).** Create + upload + `MarkAccepted` the asset but do **NOT** call `processAsset()` / enqueue processing (embeddings / doc-extraction / knowledge-graph indexing). Elysia's precedent is unambiguous: agent-produced deliverables are display + ephemeral, never routed back into the vector/knowledge store (source-ingestion and agent-output are two separate pipelines; `elysia/preprocessing/collection.py` runs only on source collections, agent results flow into ephemeral `tree/objects.py:125` `Environment`). A generated report must not silently become searchable memory. (Opt-in indexing = deferred idea.)
- **D-04 — Flat 50 MiB gate only; bypass per-modality `assets.Limits` for `source_kind=agent`.** The file already passed `send_file`'s 50 MiB delivery gate (`maxSendFileBytes`, `send_file.go:47`); re-rejecting at the asset layer for a smaller per-modality cap would half-deliver (Telegram gets it via `{path}`, web download fails) — a confusing late inconsistency. `send_file`'s ceiling is the single authoritative delivery gate; skip `Limits.Validate` for agent deliveries.
- **D-05 — Empty `ConvID` (no thread) → degrade to path-only.** An authenticated identity with an empty thread id can't produce a thread-scoped asset; degrade to the same `{path}`-only path as D-02 (one consistent fallback). Rare in practice — channel-driven web/Telegram runs always populate `ConvID`.

### Data model (WEBART-01, fork a)
- **D-06 — New `agent` source_kind via migration `0035`.** Add a forward-only migration (next slot is `0035`; latest on disk is `0034_scheduler_sandbox_reap_kind`) relaxing the `assets.source_kind` CHECK (currently `web|telegram|cli`, `migrations/0020_assets.up.sql:4`) to include `agent`, plus an `assets.SourceAgent` constant. Agent artifacts become first-class + distinguishable from human uploads (audit / future retention / filtering). Write both `.up.sql` and `.down.sql`.

### Event / thread plumbing (WEBART-02, fork b)
- **D-07 — Extend the existing `aura.artifact` event, do not add a new event.** WEBART-02 fields ride the existing `ArtifactEventName = "aura.artifact"` CUSTOM event (`internal/agui/translator.go:19`), emitted from `Actions.ArtifactDelta`. Telegram's `artifactDescriptor` already tolerates extra map fields (`telegram/artifact.go:74`), so it stays unregressed.
- **D-08 — Reuse `agent.SwarmContext(ctx).ConvID` for the thread id this phase.** It already carries the conversation id end-to-end on every channel-driven run — zero new plumbing, smallest blast radius. The "swarm concern leaking into a non-swarm delivery tool" smell is recorded as a **deferred cleanup** (a dedicated `threadctx` key), not resolved now.

### Download route (WEBART-03)
- **D-09 — Stream-through Go (`io.Copy`), no Range, request-`ctx`-scoped.** New `GET /api/assets/{id}/download` registered in `registerAssetRoutes` (`internal/agui/assets_api.go:11`), behind `RequireAuth`. Read Garage `GetObject.Body` → `io.Copy(w, body)`; pass the request context so a client disconnect cancels the store read. **Not** a presigned redirect — Garage is a private per-identity store; presign would leak a direct (even expiring) store URL and needs client→store reachability, breaking the same-origin/auth-middleware guarantee (research verdict, LibreChat ships both behind a strategy interface — mirror that interface so presign is a future drop-in for huge files).
- **D-10 — Serve `Content-Disposition: attachment` + `Content-Type: application/octet-stream` (stored-XSS guard).** Force `attachment` and a neutral serve type regardless of the sniffed MIME — both LibreChat (`files.js:543`) and open-webui converge on this to kill in-origin HTML/SVG rendering. The content-sniffed `mime_type` still rides the SSE event (for the card's file icon); it is NOT trusted as the serve header.
- **D-11 — Dual RFC 6266 filename encoding.** `attachment; filename="<ascii-fallback>"; filename*=UTF-8''<percent-encoded>` — preserves accented/unicode filenames on modern browsers, ASCII fallback for legacy, closes the header-injection footgun (gin PR #3556). Use `mime.FormatMediaType` or port LibreChat's ~10-line helper (`api/server/utils/files.js:67-88`). Note: the *download* filename may keep unicode even though `send_file`/telegram ASCII-fold the *caption* (the web, unlike the Telegram Bot API, has no reason to transliterate).
- **D-12 — Ownership = single scoped query, 404 on miss, regression test.** Enforce via `assets.Store.GetForIdentity` (`WHERE id=$1 AND owner_identity=$2`-style, `internal/assets/store.go:56`); a not-found OR not-owned request returns **404** (existence-hiding, per open-webui/LibreChat convergence and OWASP IDOR guidance), never a 403 that confirms existence. No unauthenticated download surface is added. Ship a regression test proving a non-owner → 404.

### Web UI (WEBART-04, fork c)
- **D-13 — Extend `LocalArtifactDisplay`, do not add a new display type.** When the `aura.artifact` event carries an `asset_id`, `LocalArtifactDisplay.tsx` swaps its inert path chip for a download button (`<a href="/api/assets/{id}/download" download={filename}>`). No `asset_id` (degraded) → today's render-only card. One display type, consistent with the evidence-card design system, smallest web diff, and the path chip is **replaced** so the browser never receives a raw container/host path. `sseAdapter.ts` must stop ignoring `aura.artifact` (`sseAdapter.ts:309`) and route it into this card. **Footgun to avoid (Elysia `RenderDisplay.tsx:121`):** an unknown display type silently renders null — keep the backend/frontend type-string contract explicit.

### Claude's Discretion
- Exact migration filename/suffix for `0035` (e.g. `0035_assets_source_kind_agent`), the `mime.FormatMediaType` vs ported-helper choice for D-11, and the internal signature for injecting `assets.Service` into `SendFile` — all planner/executor discretion, provided the decisions above hold.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase spec (locked requirements)
- `.planning/ROADMAP.md` §"Phase 37A: Web Artifact Delivery Lane" (lines 378-393) — goal, success criteria, and the three flagged design forks (a/b/c).
- `.planning/REQUIREMENTS.md` §"Web Artifact Delivery Lane (WEBART)" (lines 61-68) — WEBART-01..04 acceptance text (locked).

### Backend — the ingest mirror + asset layer
- `internal/agent/tools/send_file.go` — the tool to modify; `emitDelivery` (`:123`) builds today's `{path,filename,caption}` descriptor; `maxSendFileBytes` (`:47`) is the 50 MiB gate; `Router.Route` (`:97`) is the box→host staging path (plan 37-07 dependency).
- `internal/agent/tools/send_file_sandbox.go` — the routed box-delivery tail (`deliverFromBox`), the other emit path that must also ingest.
- `internal/assets/service.go` — `IngestTelegramFile` (`:257`) is the exact ingest mirror (Garage `Put` + `Store.Create` + `MarkAccepted`); `GetForIdentity` (`:174`) is the ownership read for the download route; `processAsset` is what D-03 SKIPS.
- `internal/assets/types.go` — `SourceKind` constants (`:60-67`); add `SourceAgent`.
- `internal/assets/store.go` — `GetForIdentity` (`:56`) for the 404-on-miss ownership check (D-12).
- `internal/db/migrations/0020_assets.up.sql:4` — the `source_kind` CHECK to relax in migration `0035` (D-06).
- `internal/agui/assets_api.go` — `registerAssetRoutes` (`:11`); add `GET /api/assets/{id}/download`; `principalIdentityID` (`:161`) is the auth-principal read.

### Event / channel parity
- `internal/agui/translator.go` — `ArtifactEventName = "aura.artifact"` (`:19`); `Actions.ArtifactDelta` → CUSTOM event emit (`:133`, `:366`).
- `internal/channels/telegram/artifact.go` — the Telegram consumer that MUST stay unregressed; `artifactDescriptor` (`:74`) tolerates extra fields.

### Web
- `web/src/chat/sseAdapter.ts:309` — where `aura.artifact` is currently IGNORED (the WEBART-04 gap).
- `web/src/chat/sseAdapter_frames.ts` — the CUSTOM-frame classifier the download event must route through.
- `web/src/chat/displays/LocalArtifactDisplay.tsx` — the card to extend (D-13); already renders filename + size + path chip.

### External reference implementations (cloned to `D:\tmp` during discussion)
- `D:\tmp\LibreChat` — owner-scoped download middleware (`api/server/middleware/accessResources/fileAccess.js:83-149`), dual stream/presign S3 strategy (`packages/api/src/storage/s3/crud.ts:289,859`), RFC-6266 helper (`api/server/utils/files.js:67-88`), force-download header (`api/server/routes/files/files.js:541-548`).
- `D:\tmp\open-webui` — authenticated `/{id}/content` download + 404-on-non-owned (`backend/open_webui/routers/files.py:752-812`), inline-vs-attachment XSS policy (`backend/open_webui/main.py:2578`).
- `D:\tmp\assistant-ui` — the exact frontend lib; `File.Root/Icon/Name/Size/Download` compound (`packages/ui/src/components/assistant-ui/file.tsx:162-190`), MIME-icon selection (`:39`) — confirms "override the download href to a URL route."
- `D:\tmp\ag-ui` — the exact protocol; `CustomEvent{Name,Value}` carrying asset metadata (`sdks/community/go/pkg/core/events/custom_events.go:58,65`), SSE writer (`.../encoding/sse/writer.go:202`).
- `D:\tmp\elysia` + `D:\tmp\elysia-frontend` — index-vs-display separation (agent results → ephemeral `Environment`, never the vector store; `elysia/tree/objects.py:125`, `elysia/preprocessing/collection.py`), typed-display dispatch registry (`elysia-frontend/app/components/chat/RenderDisplay.tsx:51`), unknown-type-silently-null footgun (`:121`). No file/download transport (greenfield vs Elysia).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `assets.Service.IngestTelegramFile` (`internal/assets/service.go:257`): copy its structure verbatim for the agent-ingest lane (Garage `Put` under `objectstore.AssetKey(identityID, assetID)` + `Store.Create` with `SourceKind`/`ScopeThread`/`ThreadID`, then `MarkUploaded`/`hashAndSniff`/`MarkAccepted`) — but STOP before `processAsset` (D-03) and skip `Limits.Validate` (D-04).
- `assets.Store.GetForIdentity` (`internal/assets/store.go:56`): the ready-made owner-scoped read for the download route (D-12) — no new query needed.
- `internal/agui/assets_api.go` handlers: `handleAssetGet` (`:103`) is the template for the new download handler (principal read → `GetForIdentity` → 404 on error), differing only in streaming the body instead of JSON.
- `LocalArtifactDisplay.tsx`: already renders filename + `formatSize` + path chip — the download button is an additive branch, not a rewrite (D-13).
- `asciiCaption`/`foldToASCII` exist in BOTH `send_file.go` and `telegram/artifact.go` — do NOT reuse them for the download filename (D-11 wants RFC 6266 unicode preservation, not transliteration).

### Established Patterns
- Channel-agnostic delivery: `send_file` emits a descriptor onto `ToolResult.Meta`; the agent loop's `toolResultEvent` lifts it onto `Actions.ArtifactDelta`; the translator maps it to the `aura.artifact` CUSTOM event. Extend the descriptor, do not add a new event or a new emit path.
- Per-identity object store: `objectstore.AssetKey(identityID, assetID)` is the per-identity key convention (Phase 36 MUSR isolation); the ingest MUST use the identity-scoped store (`objectsFor(identityctx.WithIdentityID(...))`, `service.go:273`).
- Auth: `RequireAuth` gate + `principalIdentityID`/`principalFrom` for the authenticated identity on every `/api/assets/*` route.
- Deferred-tool pattern: `send_file` is `Deferred: true`, `Mutating: false` — adding a Garage write does not change its non-mutating classification (it delivers, it doesn't mutate host state the completion-gate critic cares about); keep it non-mutating.

### Integration Points
- `send_file.go` `Execute` + `send_file_sandbox.go` `deliverFromBox`: both delivery tails must gain the ingest (inject an `assets.Service` + resolve identity + `ConvID`).
- Composition root: wire the `assets.Service` (and its identity/thread resolution) into the `SendFile` tool for channel-driven runners; CLI/no-identity leaves it nil → path-only degrade.
- `registerAssetRoutes` (`internal/agui/assets_api.go:11`): add the one new route.
- `sseAdapter.ts` / `sseAdapter_frames.ts`: route `aura.artifact` (currently ignored) to the `LocalArtifactDisplay` card.

</code_context>

<specifics>
## Specific Ideas

- The user explicitly directed a "how do senior devs do this" research pass and cloned five reference repos into `D:\tmp` (LibreChat, open-webui, assistant-ui, ag-ui, Elysia + elysia-frontend). Every non-trivial decision above is anchored to a cited pattern in those clones — downstream agents should read the cited `repo/path:line` before reimplementing.
- Strong preference throughout for the smallest-blast-radius, reference-backed option (reuse existing card, reuse `ConvID`, stream-through vs presign, skip processing) over new infrastructure — this phase is a delivery lane, not a redesign.

</specifics>

<deferred>
## Deferred Ideas

- **Dedicated `threadctx` package** — replace the `SwarmContext.ConvID` reuse (D-08) with an honest thread-id context key set by the composition root at every run entrypoint. A rename that touches many entrypoints for no behavior change this phase; do it when the swarm-concern-leak is worth paying down.
- **Presigned-redirect delivery strategy** — for very large artifacts, behind the LibreChat-style strategy interface D-09 leaves room for. Not needed under the 50 MiB cap.
- **HTTP Range / resumable downloads** — `http.ServeContent` with an `io.ReadSeeker`. Premium for a 50 MiB-capped report/spreadsheet lane.
- **Inline in-browser preview allowlist** (pdf/images served inline, else attachment) — open-webui's fuller serve policy; a later UX enhancement over D-10's force-attachment.
- **Opt-in knowledge indexing of a deliverable** (`send_file index:true`) — the flagged variant of D-03, if an agent ever genuinely wants a produced file searchable in memory.
- **Agent-asset retention/TTL policy** — the `agent` source_kind (D-06) makes agent artifacts selectable; the actual retention/prune rule belongs to **Phase 39 (Idempotency + Observability Pack)**, which already owns bounded-retention caps.

### Reviewed Todos (not folded)
None — no pending-todo matches surfaced for this phase.

</deferred>

---

*Phase: 37A-Web Artifact Delivery Lane*
*Context gathered: 2026-07-08*
