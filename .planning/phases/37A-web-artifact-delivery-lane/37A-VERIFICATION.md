---
phase: 37A-web-artifact-delivery-lane
verified: 2026-07-08T17:30:00Z
status: human_needed
score: 21/21 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Real-browser download UX: a live turn where the agent send_file's a DOCX, click the download button, confirm the file streams with the correct filename + attachment disposition, and the browser never receives a raw host/container path."
    expected: "The button appears on asset_id-present cards; clicking triggers a same-origin authenticated GET to /api/assets/{id}/download; the saved file matches the source bytes and filename; DevTools network tab shows no raw host/container path anywhere in the request/response."
    why_human: "Requires the full running stack (serve + Garage + a real agent turn) and a real browser save-dialog; jsdom/vitest cannot exercise on-disk save behavior or DevTools inspection."
  - test: "Telegram artifact still delivered on the live Bot API after the aura.artifact descriptor enrichment (asset_id/size_bytes/mime_type/tool_call_id added)."
    expected: "A send_file call in a live Telegram conversation still delivers the document via the Bot API exactly as before the enrichment."
    why_human: "Requires a live Telegram Bot API token + a real chat; the existing regression test (TestArtifactEnrichedDescriptorStillSends) proves the consumer code path is unregressed at the unit level but cannot prove the live Bot API round-trip."
---

# Phase 37A: Web Artifact Delivery Lane Verification Report

**Phase Goal:** Agent-generated files delivered by `send_file` reach the web cockpit as an authenticated same-origin download backed by a Garage object + identity-scoped `assets.Asset` — never a raw container/host path. Closes the gap where the web chat dropped `aura.artifact` SSE events (Telegram already consumes them via `internal/channels/telegram/artifact.go`).
**Verified:** 2026-07-08
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Migration 0035 admits `source_kind='agent'`; INSERT succeeds after up, fails 23514 before up / after down | ✓ VERIFIED | `internal/db/migrations/0035_assets_source_kind_agent.{up,down}.sql` exist and match the 0034 mirror shape. Ran `go test -tags db_integration ./internal/db/ -run TestMigration0035` LIVE against the running Postgres stack (127.0.0.1:5432) — **PASS** (1.88s), proving insert-after-up success, 23514 at v34, and a clean down+up straddle. This closes the "unknown" status the 37A-01-SUMMARY left open. |
| 2 | `assets.Service.IngestAgentFile` stores bytes under the identity's per-identity object key and returns an owned Accepted asset with `SourceKind=agent`, skipping `Limits.Validate` and `processAsset` | ✓ VERIFIED | `internal/assets/ingest_agent.go:115-128` calls the shared `ingestObject` with `enforceLimits: false, process: false`. `go test ./internal/assets/ -run TestIngestAgentFile` PASS — `TestIngestAgentFileStoresAcceptedAssetSkippingProcess` proves per-identity key + Accepted status + processor never invoked; `TestIngestAgentFileBypassesLimits` proves an over-cap file still ingests. |
| 3 | `assets.Service.OpenForIdentity` returns a ReadCloser+asset for the owner and an error for a non-owner, checking ownership BEFORE touching the object store | ✓ VERIFIED | `internal/assets/service.go:185-196` calls `GetForIdentity` before `objectsFor`/`objects.Get`. `TestOpenForIdentityNonOwnerBlocksBeforeStoreRead` asserts `objects.Get` call count is 0 on a non-owner miss. Tests PASS. |
| 4 | `contentDisposition(filename)` emits exactly one `filename=` and one `filename*=UTF-8''` param, preserves unicode, no raw CR/LF | ✓ VERIFIED | `internal/agui/content_disposition.go`. Property test (`pgregory.net/rapid`, already-vendored dep from phase 02-01, no new install) + table test both PASS — round-trips unicode, neutralizes CRLF/quote/backslash, single-param invariant proven for every fuzzed input. |
| 5 | A `send_file` under an authenticated identity + thread ingests the file and the `aura.artifact` descriptor gains `asset_id`+`mime_type` on success; `tool_call_id`+`size_bytes` ride ALWAYS (success and degrade) | ✓ VERIFIED | `internal/agent/tools/send_file.go:141-178` (`emitDelivery`) unconditionally sets `tool_call_id`+`size_bytes`, then conditionally adds `asset_id`+`mime_type` only on ingest success. `TestSendFile_IngestSuccess`/`TestSendFile_DescriptorFields` PASS with exact field assertions. |
| 6 | Both delivery tails ingest — host-path `Execute` AND routed `deliverFromBox` both funnel through the ctx-aware `emitDelivery` | ✓ VERIFIED | `send_file.go:123` (`Execute`) and `send_file_sandbox.go:49` (`deliverFromBox`) both call `s.emitDelivery(ctx, ...)`. `TestSendFile_IngestBothTails` PASS, asserting both tails produce distinct `asset_id`s. |
| 7 | Any ingest miss (nil Assets / empty identity / empty thread id / ingest error) degrades to `{path,filename,caption,tool_call_id,size_bytes}` with NO `asset_id`/`mime_type`, never errors the turn | ✓ VERIFIED | `internal/agent/tools/send_file_ingest.go` (`ingestForDelivery`) returns `ok=false` on every listed condition. `TestSendFile_DegradeMatrix` PASS across all 4 sub-cases, explicitly asserting absence of `asset_id`/`mime_type` and presence of `tool_call_id`/`size_bytes`. |
| 8 | The descriptor still carries `path` on success and degrade, so Telegram delivery is unregressed | ✓ VERIFIED | `send_file.go:157` always sets `descriptor["path"]`. `internal/channels/telegram/artifact_test.go::TestArtifactEnrichedDescriptorStillSends` (feeds path+asset_id+size_bytes+mime_type+tool_call_id through the real `consume` path) and `TestArtifactEnrichedPathlessStillIgnored` both PASS; production `artifact.go` untouched (test-only change). |
| 9 | The production `SendFile` (serve boot) has `Assets` wired; static/manifest/CLI paths leave `Assets` nil | ✓ VERIFIED | `cmd/aura/main.go:204-206` retains `handles.SendFile = sf` before registration; `cmd/aura/serve.go:297-299` sets `chat.toolHandles.SendFile.Assets = sendFileAssetAdapter{...}` behind a nil guard, textually after `buildAssetService`. `go build ./...` clean; `go test ./cmd/aura/` PASS. |
| 10 | `GET /api/assets/{id}/download` streams the owner's asset body with `Content-Disposition: attachment` and `Content-Type: application/octet-stream` | ✓ VERIFIED | `internal/agui/assets_api.go:35-59` (`handleAssetDownload`) sets headers before the first write, then `io.Copy`. `TestAssetDownload_Owner` PASS asserting body bytes, `attachment;` prefix, `application/octet-stream`, `nosniff`, `filename*=UTF-8''`, and Content-Length. |
| 11 | A non-owner (or non-existent id) request returns 404 — never 403, never 200 | ✓ VERIFIED | `assets_api.go:46-49`: any `OpenForIdentity` error → `http.StatusNotFound`. `TestAssetDownload_NonOwner` PASS, explicitly asserting `Code != http.StatusForbidden`. |
| 12 | An unauthenticated request is rejected — no unauthenticated download surface added | ✓ VERIFIED | The route is registered inside `registerAssetRoutes` (`assets_api.go:17`), and the whole `agui` mux is wrapped in `agui.RequireAuth` at `cmd/aura/serve_webui.go:526`. `TestAssetDownload_Unauth` PASS (401, service never reached). |
| 13 | A client disconnect cancels the Garage read (request-ctx-scoped `io.Copy`) with no goroutine leak | ✓ VERIFIED | `io.Copy(w, rc)` runs over `r.Context()`-scoped `rc` (from `OpenForIdentity(r.Context(), ...)`) with `defer rc.Close()`. `TestAssetDownload_ClientDisconnect` wraps `goleak.VerifyNone(t)` and PASSES. |
| 14 | The served Content-Type is always the neutral octet-stream regardless of the sniffed `mime_type` (stored-XSS guard) | ✓ VERIFIED | `assets_api.go:53` hardcodes `Content-Type: application/octet-stream` — never reads `asset.MIMEType`. Owner test seeds a hostile `image/svg+xml` sniffed mime and still asserts octet-stream is served. |
| 15 | `sseAdapter` routes an `aura.artifact` CUSTOM frame into a `local_artifact` DisplayPayload attached by `tool_call_id` (was dropped as a no-op) | ✓ VERIFIED | `web/src/chat/sseAdapter.ts:345-363` — the `aura.artifact` branch inside the CUSTOM case, guarded by `isArtifactDescriptor`, synthesizes and `writeTool`s a `local_artifact` payload. The old "aura.artifact is a no-op" test was rewritten (`sseAdapter.test.ts:401`) to assert the attach contract; both new tests PASS (`npx vitest run` — 39/39 tests green across the two changed test files). |
| 16 | `LocalArtifactDisplay` renders an authenticated download button (`<a href=/api/assets/{asset_id}/download download={filename}>`) when `asset_id` is present | ✓ VERIFIED | `web/src/chat/displays/LocalArtifactDisplay.tsx:65-89`. `LocalArtifactDisplay.test.tsx::'renders an authenticated same-origin download anchor when asset_id is present'` PASS, asserting exact `href`/`download` attribute values. |
| 17 | When `asset_id` is absent, the card shows filename+size+"delivery unavailable" and NO path chip | ✓ VERIFIED | `LocalArtifactDisplay.tsx:90-113` (the `role="note"` degraded branch). Test PASS. |
| 18 | The synthesized `local_artifact` payload NEVER carries a `path` field (either branch); the raw host/container path is never rendered | ✓ VERIFIED | `sseAdapter.ts:352-361` conditional spreads include only `filename`/`size_bytes`/`asset_id`/`mime_type` — `path` is never assigned. `sseAdapter.test.ts` explicitly asserts `expect(artifact).not.toHaveProperty('path')` in both the success and degrade cases. `LocalArtifactDisplay.test.tsx` has two dedicated "never renders a raw host/container path" tests (asset_id present AND absent) that inject a path onto the artifact and assert it's absent from the rendered DOM. |
| 19 | `internal/webui/dist` is rebuilt from the `web/src` changes and committed (web-dist-freshness passes) | ✓ VERIFIED | Commit `35a2eb0e` "rebuild embedded internal/webui/dist"; `git status --short internal/webui/dist` is clean on HEAD (no pending diff), confirming the committed bundle matches the committed src. `go build ./...` (embeds the dist) is clean. |
| 20 | WEBART-01..04 requirements are traceable and closed | ✓ VERIFIED | REQUIREMENTS.md marks WEBART-01/02/03/04 `[x]`. Every plan's frontmatter `requirements:` field maps cleanly: 37A-01→[WEBART-01,03], 37A-02→[WEBART-01,02], 37A-03→[WEBART-03], 37A-04→[WEBART-04]. No orphaned requirement IDs for this phase (WEBART-05..08 belong to Phase 37B per REQUIREMENTS.md, not this phase). |
| 21 | No debt markers / anti-patterns in the phase's changed files | ✓ VERIFIED | `grep -rn "TBD\|FIXME\|XXX\|TODO\|HACK\|PLACEHOLDER"` across all Go + TS files modified/created by this phase returned zero matches. |

**Score:** 21/21 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/db/migrations/0035_assets_source_kind_agent.{up,down}.sql` | CHECK widen/narrow for `source_kind='agent'` | ✓ VERIFIED | Present, matches 0034 mirror shape; live db_integration test passes. |
| `internal/assets/types.go` (`SourceAgent`, `AgentIngestRequest`) | New source kind + delivery-only request type | ✓ VERIFIED | Both present; `AgentIngestRequest` has `Reader io.Reader`, no `SourceRef`/`ChatID`. |
| `internal/assets/ingest_agent.go` (`IngestAgentFile`) | Delivery-only ingest skipping Limits+processAsset | ✓ VERIFIED | Present; verified via `ingestObject{enforceLimits:false, process:false}`. |
| `internal/assets/service.go` (`OpenForIdentity`) | Owner-scoped streaming read | ✓ VERIFIED | Present; ownership gate precedes store read. |
| `internal/agui/asset_service.go` (`AssetService.OpenForIdentity`) | Interface extension | ✓ VERIFIED | Present; `go build ./internal/agui/` compiles. |
| `internal/agui/content_disposition.go` (`contentDisposition`) | RFC-6266 dual-param helper | ✓ VERIFIED | Present; property+table tested. |
| `internal/agui/assets_api.go` (`handleAssetDownload` + route) | Streaming download endpoint | ✓ VERIFIED | Present; route registered inside `registerAssetRoutes`. |
| `internal/agent/tools/send_file_ingest.go` (`AssetDeliverer`) | Primitive-typed injection seam | ✓ VERIFIED | Present; `tools` package imports neither `internal/assets` nor `internal/agent` (grep-confirmed). |
| `internal/agent/tools/send_file.go` (`SendFile.Assets`, `emitDelivery`) | Ctx-aware descriptor enrichment | ✓ VERIFIED | Present, wired on both tails. |
| `cmd/aura/send_file_asset_adapter.go` (`sendFileAssetAdapter`) | Structural `AssetDeliverer` bridge | ✓ VERIFIED | Present; compile-time `var _ tools.AssetDeliverer = sendFileAssetAdapter{}`. |
| `cmd/aura/main.go` / `serve.go` (composition-root wiring) | Retained handle + serve-boot Assets set | ✓ VERIFIED | Present; both grep-confirmed at exact call sites. |
| `web/src/chat/displays/types.ts` (`DisplayArtifact.asset_id?`/`mime_type?`) | New optional fields | ✓ VERIFIED | Present with doc comments explaining semantics. |
| `web/src/chat/sseAdapter.ts` (`aura.artifact` branch) | Reducer consuming the descriptor | ✓ VERIFIED | Present; path-free synthesis confirmed. |
| `web/src/chat/displays/LocalArtifactDisplay.tsx` (download button / degraded card) | User-facing render | ✓ VERIFIED | Present; both branches confirmed path-free. |
| `internal/webui/dist` | Rebuilt embedded bundle | ✓ VERIFIED | Committed at `35a2eb0e`; clean working tree confirms freshness. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `ingest_agent.go` | objectstore per-identity Put | `objectsFor(identityctx.WithIdentityID(...))` + `AssetKey` | ✓ WIRED | Confirmed in `ingestObject` (shared helper both `IngestAgentFile`/`IngestTelegramFile` call). |
| `service.go OpenForIdentity` | `GetForIdentity` → `objects.Get` | ownership gate precedes object read | ✓ WIRED | Line order confirmed; test proves zero `Get` calls on non-owner miss. |
| `content_disposition.go` | RFC-6266 dual-param output | `filename*=UTF-8''` | ✓ WIRED | Property test confirms. |
| `send_file.go Execute` + `send_file_sandbox.go deliverFromBox` | shared `emitDelivery` | `emitDelivery(ctx` on both callers | ✓ WIRED | Both call sites confirmed; both-tails test passes. |
| `emitDelivery` | `IngestAgentDelivery` | `ingestForDelivery` best-effort helper | ✓ WIRED | Confirmed; degrade matrix covers every miss path. |
| `serve.go` | `handles.SendFile.Assets` | post-`buildAssetService` nil-guarded set | ✓ WIRED | Confirmed at `serve.go:297-299`. |
| `registerAssetRoutes` | `GET /api/assets/{id}/download` → `handleAssetDownload` | `mux.HandleFunc` | ✓ WIRED | Confirmed; whole mux wrapped in `RequireAuth`. |
| `handleAssetDownload` | `assets.OpenForIdentity` | ownership → stream | ✓ WIRED | Confirmed; 404 on any error. |
| `sseAdapter.ts aura.artifact branch` | tool part via `tool_call_id` | `ensureTool` → `writeTool` | ✓ WIRED | Confirmed. |
| `LocalArtifactDisplay download button` | `GET /api/assets/{id}/download` | `<a href={...}>` | ✓ WIRED | Confirmed exact href construction. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|---------------------|--------|
| `LocalArtifactDisplay` | `payload.artifact` (asset_id/filename/size_bytes/mime_type) | `sseAdapter.ts` `aura.artifact` reducer branch, sourced from the real `aura.artifact` SSE CUSTOM frame emitted by `send_file.go`'s `emitDelivery`, itself populated from a live `IngestAgentFile` call against Garage via `sendFileAssetAdapter` | Yes — traced end-to-end: `Garage Put` (via `objects.Put` in `ingestObject`) → `assets.Asset.ID` → `descriptor["asset_id"]` → SSE `aura.artifact` event → `sseAdapter` synthesis → rendered `<a href>` | ✓ FLOWING |
| `GET /api/assets/{id}/download` | `asset.FileName`/`asset.SizeBytes`/object body | `OpenForIdentity` → `s.Store.GetForIdentity` (real DB row) + `objects.Get` (real Garage object) | Yes — no static/empty fallback; any lookup failure is a 404, not a synthesized empty body | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Migration 0035 roundtrip against the LIVE Postgres stack | `POSTGRES_PASSWORD=*** PGHOST=127.0.0.1 PGPORT=5432 go test -tags db_integration ./internal/db/ -run TestMigration0035` | `PASS (1.88s)` | ✓ PASS |
| Backend unit suite for the four touched packages | `go test ./internal/assets/... ./internal/agui/... ./internal/agent/tools/... ./internal/channels/telegram/... ./cmd/aura/...` | all `ok` | ✓ PASS |
| Download route security suite | `go test ./internal/agui/ -run 'TestAssetDownload_Owner\|TestAssetDownload_NonOwner\|TestAssetDownload_Unauth\|TestAssetDownload_ClientDisconnect' -v` | 4/4 PASS (incl. goleak-clean disconnect) | ✓ PASS |
| send_file ingest/degrade/both-tails suite | `go test ./internal/agent/tools/ -run 'TestSendFile' -v` | all PASS (1 unrelated Windows-symlink test SKIPPED, pre-existing/unrelated) | ✓ PASS |
| Full Go build | `go build ./...` | clean, no output | ✓ PASS |
| Web typecheck | `cd web && npx tsc --noEmit` | clean | ✓ PASS |
| Web lint on the changed files | `cd web && npx eslint --max-warnings=0 src/chat/sseAdapter.ts src/chat/displays/LocalArtifactDisplay.tsx src/chat/displays/types.ts` | clean | ✓ PASS |
| Web unit suite for the two changed test files | `cd web && npx vitest run src/chat/__tests__/sseAdapter.test.ts src/chat/displays/__tests__/LocalArtifactDisplay.test.tsx` | 39/39 tests PASS | ✓ PASS |
| `internal/webui/dist` freshness | `git status --short internal/webui/dist` | clean (no diff on HEAD) | ✓ PASS |

**Not independently re-run (deferred to WSL/CI per project convention — no gcc on this Windows host):** `go test -race`. This is a pre-existing, project-documented Windows-host limitation (CLAUDE.md: "WSL is the full primary dev environment... native `go test -race` works"), not a phase-specific gap; every SUMMARY across 37A-01..04 documents the same constraint.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|--------------|--------|----------|
| WEBART-01 | 37A-01, 37A-02 | `send_file` delivery stored in Garage as an owned `assets.Asset`, no raw host/container path used as delivery handle | ✓ SATISFIED | `IngestAgentFile` + `emitDelivery`/`ingestForDelivery` wiring; descriptor still carries `path` only for Telegram parity (not as the web delivery handle) |
| WEBART-02 | 37A-02 | `aura.artifact` carries `asset_id`/`filename`/`size_bytes`/`mime_type`, not a path; Telegram regression-tested | ✓ SATISFIED | Descriptor enrichment confirmed; Telegram regression tests pass |
| WEBART-03 | 37A-01, 37A-03 | `GET /api/assets/{id}/download` streams with attachment disposition, ownership-enforced, RequireAuth-inherited, non-owner 404 | ✓ SATISFIED | `handleAssetDownload` fully confirmed with 4/4 security tests passing |
| WEBART-04 | 37A-04 | Web chat consumes `aura.artifact`, renders authenticated download button, no raw path to browser, CLI/no-identity degrades gracefully | ✓ SATISFIED | `sseAdapter.ts` + `LocalArtifactDisplay.tsx` confirmed path-free in both branches |

No orphaned requirements — REQUIREMENTS.md maps only WEBART-01..04 to Phase 37A (WEBART-05..08 are explicitly routed to Phase 37B).

### Anti-Patterns Found

None. Grep scan for `TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER|not yet implemented|coming soon` across every file created/modified by this phase (Go + TypeScript) returned zero matches. No empty-return stubs, no hardcoded-empty data flows, no console.log-only implementations found in the reviewed source.

### Human Verification Required

### 1. Real-browser download UX

**Test:** In a running full stack (serve + Garage + a real browser session), have the agent call `send_file` to deliver a DOCX (or any file) during a live turn. Confirm the download button appears on the `local_artifact` card, click it, and inspect the saved file + the browser's network tab.
**Expected:** The button targets `/api/assets/{asset_id}/download`; the download completes with the correct filename and `Content-Disposition: attachment`; the saved bytes match the source file; no raw host/container path is visible anywhere in the DOM, network request, or response headers.
**Why human:** Requires the full running stack (serve + Garage + a real turn) and a real browser save-dialog interaction; jsdom/vitest cannot exercise on-disk save behavior or a DevTools inspection of the live response.

### 2. Telegram Bot API non-regression (live)

**Test:** In a live Telegram conversation, have the agent call `send_file` and confirm the document is still delivered via the Bot API after the `aura.artifact` descriptor enrichment.
**Expected:** The document delivers exactly as before the phase (byte-for-byte behavior); the extra descriptor keys (`asset_id`/`size_bytes`/`mime_type`/`tool_call_id`) have no visible effect on the Telegram side.
**Why human:** Requires a live Telegram Bot API token and a real chat session. The unit-level regression test (`TestArtifactEnrichedDescriptorStillSends`) proves the code path handling the enriched descriptor is unregressed, but cannot prove the live Bot API round-trip.

### Gaps Summary

No gaps found. All 21 derived observable truths across the four plans (37A-01 backend primitives, 37A-02 send_file ingest lane + composition-root wiring, 37A-03 authenticated download route, 37A-04 web consume + download button) are independently verified against the actual source code — not just SUMMARY claims. The one previously "unknown"-status item (the migration 0035 db_integration test) was run live against the running Postgres stack during this verification and PASSED. All backend and frontend unit/integration test suites relevant to this phase pass; `go build`, `tsc --noEmit`, and `eslint` are clean; the embedded web bundle is fresh; no debt markers exist in the touched files. The only remaining items are the two Manual-Only checks the plans themselves correctly deferred to human verification (real-browser download UX and live Telegram Bot API non-regression) — these route to `human_needed`, not to a gap, per the phase's own documented Manual-Only sections.

---

*Verified: 2026-07-08*
*Verifier: Claude (gsd-verifier)*
