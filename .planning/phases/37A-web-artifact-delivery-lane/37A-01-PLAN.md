---
phase: 37A-web-artifact-delivery-lane
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/db/migrations/0035_assets_source_kind_agent.up.sql
  - internal/db/migrations/0035_assets_source_kind_agent.down.sql
  - internal/db/migrate_0035_integration_test.go
  - internal/assets/types.go
  - internal/assets/ingest_agent.go
  - internal/assets/ingest_agent_test.go
  - internal/assets/service.go
  - internal/assets/open_for_identity_test.go
  - internal/agui/asset_service.go
  - internal/agui/assets_api_test.go
  - internal/agui/content_disposition.go
  - internal/agui/content_disposition_test.go
autonomous: true
requirements: [WEBART-01, WEBART-03]

must_haves:
  truths:
    - "Migration 0035 admits source_kind='agent'; INSERT succeeds after up, 23514 before up / after down"
    - "assets.Service.IngestAgentFile stores bytes under the identity's per-identity object key and returns an owned Accepted asset with SourceKind=agent"
    - "IngestAgentFile ingests a file that would fail a per-modality Limits cap (Limits.Validate is bypassed for agent deliveries, D-04)"
    - "assets.Service.OpenForIdentity returns a ReadCloser + asset for the owner and an error (→404 upstream) for a non-owner, checking ownership BEFORE touching the object store"
    - "contentDisposition(filename) emits exactly one filename= and one filename*=UTF-8'' param, preserves unicode, and contains no raw CR/LF for any input"
  artifacts:
    - path: "internal/db/migrations/0035_assets_source_kind_agent.up.sql"
      provides: "CHECK widen admitting 'agent'"
      contains: "'agent'"
    - path: "internal/db/migrations/0035_assets_source_kind_agent.down.sql"
      provides: "reverse widen (delete agent rows then narrow)"
      contains: "DELETE FROM aura.assets WHERE source_kind = 'agent'"
    - path: "internal/assets/types.go"
      provides: "SourceAgent constant + AgentIngestRequest struct"
      contains: "SourceAgent"
    - path: "internal/assets/ingest_agent.go"
      provides: "IngestAgentFile ingest orchestration (skip Limits + processAsset)"
      contains: "func (s *Service) IngestAgentFile"
    - path: "internal/assets/service.go"
      provides: "OpenForIdentity owner-scoped streaming read"
      contains: "func (s *Service) OpenForIdentity"
    - path: "internal/agui/asset_service.go"
      provides: "OpenForIdentity on the AssetService interface"
      contains: "OpenForIdentity"
    - path: "internal/agui/content_disposition.go"
      provides: "RFC-6266 dual-param Content-Disposition helper"
      contains: "func contentDisposition"
  key_links:
    - from: "internal/assets/ingest_agent.go"
      to: "objectstore per-identity Put"
      via: "objectsFor(identityctx.WithIdentityID(ctx, req.IdentityID)) + AssetKey"
      pattern: "objectsFor\\(identityctx.WithIdentityID"
    - from: "internal/assets/service.go OpenForIdentity"
      to: "assets.Store.GetForIdentity (ownership) then objects.Get"
      via: "ownership gate precedes object read"
      pattern: "GetForIdentity"
    - from: "internal/agui/content_disposition.go"
      to: "url.PathEscape UTF-8 filename* + ASCII-folded filename fallback"
      via: "dual RFC-6266 param build"
      pattern: "filename\\*=UTF-8''"
  prohibitions:
    - "IngestAgentFile MUST NOT call s.processAsset (D-03: a deliverable must not become searchable memory)"
    - "IngestAgentFile MUST NOT call s.Limits.Validate at either the pre-Put or post-Put site (D-04)"
    - "contentDisposition MUST NOT emit a raw CR or LF byte into the header value (T-HdrInj)"
    - "OpenForIdentity MUST NOT call objects.Get before the GetForIdentity ownership gate passes (T-IDOR)"
    - "No new external package is installed (go.mod / go.sum byte-unchanged; Go stdlib + vendored only)"
---

<objective>
Build the four backend primitives every later 37A plan calls, none of which depend on the `send_file` tool or the download route: (1) migration `0035` + the `agent` source kind + the `AgentIngestRequest` type (data model, WEBART-01/D-06), (2) `assets.Service.IngestAgentFile` — the delivery-only ingest mirror of `IngestTelegramFile` that skips `Limits.Validate` (D-04) and `processAsset` (D-03), (3) `assets.Service.OpenForIdentity` — the owner-scoped streaming read the download route consumes (WEBART-03/D-12), and (4) the `contentDisposition` RFC-6266 helper that neutralizes CRLF/quote header injection while preserving unicode filenames (WEBART-03/D-11).

Purpose: these are the pure, daemon-free-testable seams that MUST land first so the Wave-2 ingest lane (37A-02) and download route (37A-03) can run in parallel against them. Every new surface here is unit-tested with `internal/objectstore/fake.go` (NO `garage_integration` tag) so it counts toward the `db_integration neo4j_integration` 85% coverage floor.

Output: migration `0035` pair + roundtrip test; `assets.SourceAgent`/`AgentIngestRequest`; `IngestAgentFile`; `OpenForIdentity` (+ the `agui.AssetService` interface line); `contentDisposition` (+ property test).

## Research corrections honored (do not regress)
- **D-08 unreachable literal:** irrelevant to this plan (no ctx thread-id read here) — the tool-side seam correction lands in 37A-02.
- **RLS-free write path:** `aura.assets` is NOT RLS-covered (`0032_owner_rls` covers only conversations/paused_states/conversation_turns), so `IngestAgentFile` reuses the exact pooled `IngestTelegramFile` orchestration — NO `app.current_identity` GUC / `WithIdentityTx` threading.
- **`mime.FormatMediaType` alone is insufficient (Landmine 4):** it drops the whole media-type on invalid runes and emits no `filename*`. Port the LibreChat dual-param helper (`D:\tmp\LibreChat\api\server\utils\files.js:67-88`), property-test it. Do NOT reuse `asciiCaption`/`foldToASCII` (those transliterate the caption; D-11 preserves unicode in the filename).
</objective>

<execution_context>
@.claude/get-shit-done/workflows/execute-plan.md
@.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37A-web-artifact-delivery-lane/37A-CONTEXT.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-RESEARCH.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-PATTERNS.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-VALIDATION.md
</context>

## Artifacts This Phase Produces (source-grounding exclusion list — whole phase)

New Go symbols (do not flag as drift; this phase creates them):
- `assets.SourceAgent` const (`SourceKind = "agent"`) — internal/assets/types.go
- `assets.AgentIngestRequest` struct — internal/assets/types.go
- `assets.Service.IngestAgentFile(ctx, AgentIngestRequest) (Asset, error)` — internal/assets/ingest_agent.go
- `assets.Service.OpenForIdentity(ctx, id, identityID string) (io.ReadCloser, Asset, error)` — internal/assets/service.go
- `agui.AssetService.OpenForIdentity(...)` interface method — internal/agui/asset_service.go
- `agui.contentDisposition(filename string) string` — internal/agui/content_disposition.go
- `agui.Server.handleAssetDownload` + route `GET /api/assets/{id}/download` — internal/agui/assets_api.go (37A-03)
- `tools.AssetDeliverer` interface + `tools.SendFile.Assets` field + `ingestForDelivery`/`toolCallIDFromCtx` helpers — internal/agent/tools/send_file_ingest.go, send_file.go (37A-02)
- `runtimeToolHandles.SendFile` field + `sendFileAssetAdapter` — cmd/aura/main.go, cmd/aura/send_file_asset_adapter.go (37A-02)
- Descriptor keys on `aura.artifact`: `asset_id`, `size_bytes`, `mime_type`, `tool_call_id` (37A-02)
- Migration `0035_assets_source_kind_agent.{up,down}.sql`
- TS: `DisplayArtifact.asset_id?`/`mime_type?`, `isArtifactDescriptor`, the `aura.artifact` reducer branch, the download-button branch, i18n `display.artifact.download*`, rebuilt `internal/webui/dist` (37A-04)

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Migration 0035 (assets.source_kind → +agent) + SourceAgent constant + AgentIngestRequest type + db_integration roundtrip</name>
  <files>internal/db/migrations/0035_assets_source_kind_agent.up.sql, internal/db/migrations/0035_assets_source_kind_agent.down.sql, internal/db/migrate_0035_integration_test.go, internal/assets/types.go</files>
  <read_first>
    - internal/db/migrations/0020_assets.up.sql (line 4 — the inline `source_kind text NOT NULL CHECK (source_kind IN ('web','telegram','cli'))`; Postgres auto-names it `assets_source_kind_check`)
    - internal/db/migrations/0034_scheduler_sandbox_reap_kind.up.sql + .down.sql (the exact CHECK-widen shape to mirror: DROP CONSTRAINT → ADD CONSTRAINT widened; down pre-deletes the newly-admitted rows THEN narrows)
    - internal/db/migrate_0034_integration_test.go (the sibling `db_integration` roundtrip test to mirror — INSERT-ok-after-up / 23514-before-up / straddle down+up)
    - internal/assets/types.go (the `SourceKind` const block ~:64-68: SourceWeb/SourceTelegram/SourceCLI; and `TelegramIngestRequest` ~:129 as the struct analog for AgentIngestRequest)
    - 37A-PATTERNS.md §8 (migration excerpts) + §3 (SourceAgent) + §1 (AgentIngestRequest struct)
  </read_first>
  <action>
    Write `internal/db/migrations/0035_assets_source_kind_agent.up.sql`: `ALTER TABLE aura.assets DROP CONSTRAINT assets_source_kind_check;` then `ALTER TABLE aura.assets ADD CONSTRAINT assets_source_kind_check CHECK (source_kind IN ('web', 'telegram', 'cli', 'agent'));` with a header comment citing WEBART-01/D-06 and the 0020 inline-check auto-name. Write `.down.sql` mirroring `0034` down: `DELETE FROM aura.assets WHERE source_kind = 'agent';` FIRST, then DROP + re-ADD the constraint narrowed to `('web','telegram','cli')`. No grant change (0020 already GRANTed aura_app DML; aura_migrate owns DDL). Add `SourceAgent SourceKind = "agent"` to the const block in `internal/assets/types.go`. Add an `AgentIngestRequest` struct to `types.go` mirroring `TelegramIngestRequest` MINUS the Telegram source fields (ChatID/MessageID/FileID/SourceRef): fields `IdentityID, ThreadID, FileName, MIMEType string`, `Modality Modality`, `SizeBytes int64`, `Reader io.Reader`. Write `internal/db/migrate_0035_integration_test.go` (`//go:build db_integration`, `TestMigration0035`) mirroring the 0034 sibling: migrate to HEAD, assert an INSERT of a `source_kind='agent'` asset row succeeds; assert the same INSERT before-up / after-down returns Postgres error code `23514` (check_violation); use the sibling's `envOrSkip`/pool/step helpers verbatim (t.Fatal under $CI — no-skip-as-green). `sqlc generate` MUST be zero-diff (a CHECK widen touches no query).
  </action>
  <behavior>
    - INSERT source_kind='agent' → success after `0035` up
    - INSERT source_kind='agent' → error 23514 at schema &lt; 0035 and after 0035 down
    - down+up straddle leaves the schema clean and re-appliable
    - `sqlc generate` produces no diff
  </behavior>
  <acceptance_criteria>
    - `internal/db/migrations/0035_assets_source_kind_agent.up.sql` and `.down.sql` both exist; up contains `'agent'` in the CHECK; down contains `DELETE FROM aura.assets WHERE source_kind = 'agent'`
    - `internal/assets/types.go` contains `SourceAgent SourceKind = "agent"` and a `type AgentIngestRequest struct` with a `Reader io.Reader` field and NO `SourceRef`/`ChatID` field
    - `go test -tags db_integration ./internal/db/ -run TestMigration0035` exits 0 on the live stack (WSL/CI); locally without the stack it skips (never a compile-only pass)
    - `go build ./... && go vet ./...` clean; `sqlc generate` yields no diff (verify with `git status` on internal/*/sqlc output)
    - latest migration slot is now `0035`; no other `.up.sql`/`.down.sql` added
  </acceptance_criteria>
  <verify>
    <automated>go build ./... &amp;&amp; go vet ./... &amp;&amp; go test -tags db_integration ./internal/db/ -run TestMigration0035</automated>
  </verify>
  <done>Migration 0035 pair exists mirroring 0034; SourceAgent + AgentIngestRequest defined; TestMigration0035 asserts INSERT-agent-ok-after-up / 23514-before-up-and-after-down; sqlc zero-diff.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: assets.Service.IngestAgentFile + OpenForIdentity + AssetService interface extension (daemon-free, objectstore/fake.go)</name>
  <files>internal/assets/ingest_agent.go, internal/assets/ingest_agent_test.go, internal/assets/service.go, internal/assets/open_for_identity_test.go, internal/agui/asset_service.go, internal/agui/assets_api_test.go</files>
  <read_first>
    - internal/assets/service.go (`IngestTelegramFile` :257-327 — the EXACT mirror; `GetForIdentity` :174-179; `objectsFor` :58; `hashAndSniff` :377; the `Limits.Validate` call sites :270 and :309 to SKIP; the trailing `return s.processAsset(ctx, asset)` :326 to REPLACE with `return asset, nil`)
    - internal/objectstore/types.go (`Store.Get(ctx, ObjectRef) (io.ReadCloser, Attrs, error)` :55; `Store.Put` :53; `AssetKey` :60) and internal/objectstore/fake.go (the in-memory Store for daemon-free tests)
    - internal/assets/types.go (AgentIngestRequest from Task 1; `InferModality`, `CreateRequest`, `ScopeThread`, status constants)
    - internal/agui/asset_service.go (the `AssetService` interface to extend with `OpenForIdentity`)
    - internal/agui/assets_api_test.go (`fakeAssetService` :125-197 — a struct with configurable-field method fakes; add an `OpenForIdentity` method + `openResp io.ReadCloser`/`openAsset assets.Asset`/`openErr error` fields so the agui package still compiles AND 37A-03 can drive it)
    - 37A-PATTERNS.md §1 (the 4 deltas, marked line-by-line) + §2 (OpenForIdentity composed method) + §"Test Analogs"
  </read_first>
  <action>
    Create `internal/assets/ingest_agent.go` with `func (s *Service) IngestAgentFile(ctx context.Context, req AgentIngestRequest) (Asset, error)` copying `IngestTelegramFile` verbatim with EXACTLY four deltas: (1) DELETE both `s.Limits.Validate(...)` calls (pre-Put and post-Put) — D-04; (2) `SourceKind: SourceAgent` and drop `SourceRef`/`telegramSourceRef` (leave SourceRef empty) — D-06; (3) replace the final `return s.processAsset(ctx, asset)` with `return asset, nil` — D-03; (4) keep everything else byte-equivalent: nil-guard on `s.Store`/`s.Objects` and `req.Reader`, `cleanFileName`, `normalizeMIME`, `InferModality` when modality unset, `objectsFor(identityctx.WithIdentityID(ctx, req.IdentityID))`, `AssetKey`, `Store.Create`, `objects.Put` (+ `SetStatus(StatusFailed, "object_put_failed")` on Put error), `MarkUploaded`, `hashAndSniff`, `MarkAccepted`. Add `func (s *Service) OpenForIdentity(ctx context.Context, id, identityID string) (io.ReadCloser, Asset, error)` to `internal/assets/service.go`: call `s.GetForIdentity(ctx, id, identityID)` FIRST (ownership gate — an error returns `nil, Asset{}, err` before any store touch), then `objectsFor(identityctx.WithIdentityID(ctx, identityID))`, then `objects.Get(ctx, ObjectRef{Bucket: asset.ObjectBucket, Key: asset.ObjectKey})`; return `(rc, asset, err)`. Add `OpenForIdentity(context.Context, string, string) (io.ReadCloser, assets.Asset, error)` to the `agui.AssetService` interface (`asset_service.go`, add the `io` import) and implement it on `fakeAssetService` (return the configurable fields). Write `internal/assets/ingest_agent_test.go` and `internal/assets/open_for_identity_test.go` (both untagged, `internal/objectstore/fake.go` + a recording/fake `StoreBackend`).
  </action>
  <behavior>
    - IngestAgentFile: bytes land in the fake Put under `AssetKey(identityID, assetID)`; Store.Create called with SourceKind=SourceAgent + ScopeThread + ThreadID; MarkUploaded + hashAndSniff + MarkAccepted all called; the returned asset is Accepted
    - IngestAgentFile: `processAsset` is NOT invoked (assert via a StoreBackend that would record a further status transition, or assert the returned asset never enters a processing status)
    - IngestAgentFile: `Limits.Validate` is NOT invoked — a request whose SizeBytes exceeds a per-modality cap configured on `s.Limits` still ingests to Accepted
    - IngestAgentFile: a fake Put error → returns the error and the asset was moved to StatusFailed; the caller (37A-02) degrades — the method itself surfaces the error
    - OpenForIdentity: owner id → returns a non-nil ReadCloser whose bytes equal the stored object + the asset; a GetForIdentity miss → returns a non-nil error and `objects.Get` was NEVER called (ownership precedes the store read)
  </behavior>
  <acceptance_criteria>
    - `internal/assets/ingest_agent.go` contains `func (s *Service) IngestAgentFile`; a grep of the function body contains neither `Limits.Validate` nor `processAsset`
    - `internal/assets/service.go` contains `func (s *Service) OpenForIdentity`; the ownership call `GetForIdentity` textually precedes the `objects.Get` call in the function
    - `internal/agui/asset_service.go` interface contains `OpenForIdentity`; `go build ./internal/agui/` compiles (every AssetService implementer — production `*assets.Service` + `fakeAssetService` — has the method)
    - `go test ./internal/assets/ -run 'TestIngestAgentFile|TestOpenForIdentity'` exits 0; the skip-processAsset and skip-Limits assertions are present (grep the test for the assertions)
    - `go test -race ./internal/assets/ ./internal/agui/` exits 0
    - go.mod / go.sum byte-unchanged
  </acceptance_criteria>
  <verify>
    <automated>go build ./... &amp;&amp; go vet ./... &amp;&amp; go test -race ./internal/assets/ ./internal/agui/ -run 'IngestAgentFile|OpenForIdentity|Asset'</automated>
  </verify>
  <done>IngestAgentFile mirrors IngestTelegramFile minus Limits.Validate + processAsset with SourceKind=agent; OpenForIdentity gates ownership before the store read; AssetService interface + fake extended; daemon-free tests green counting toward the coverage floor.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: internal/agui/content_disposition.go — RFC-6266 dual-param helper (property-tested)</name>
  <files>internal/agui/content_disposition.go, internal/agui/content_disposition_test.go</files>
  <read_first>
    - internal/agui/assets_api.go (the agui package the helper joins; `sanitizeErr` for style; the handler that will call it lands in 37A-03)
    - internal/agent/tools/send_file.go (`asciiCaption`/`foldToASCII` — the transliterators to NOT reuse; D-11 preserves unicode in the download filename)
    - 37A-PATTERNS.md §7 (helper spec + output shape) + 37A-RESEARCH.md Landmine 4 (why FormatMediaType alone is insufficient) + §Validation Architecture (property-based target)
    - D:\tmp\LibreChat\api\server\utils\files.js (lines 67-88 — the ~15-line dual-param encoder to port)
    - .claude/skills/property-based-testing/SKILL.md (rapid/gopter usage) — and confirm `pgregory.net/rapid` or `gopter` is vendored (`grep -E 'pgregory.net/rapid|leanovate/gopter' go.mod`); if NEITHER is vendored, fall back to an exhaustive table-driven test (do NOT add a package without the Package Legitimacy Gate)
  </read_first>
  <action>
    Create `internal/agui/content_disposition.go` with `func contentDisposition(filename string) string` returning `attachment; filename="<ascii-fallback>"; filename*=UTF-8''<percent-encoded>`. Build the fallback by ASCII-folding + stripping control chars, `"`, `\`, CR, LF (and collapsing to a safe default like `download` when the result is empty); build the extended param with `url.PathEscape` over the UTF-8 filename. Always prefix `attachment; `. Do NOT reuse `asciiCaption`/`foldToASCII`. Write `internal/agui/content_disposition_test.go`: property-based (`pgregory.net/rapid` if vendored, else table-driven exhaustive) over unicode, embedded `"`, `\r\n`, `;`, path separators, empty, and >255-byte inputs — asserting the output has exactly one `filename=` and exactly one `filename*=`, contains no raw `\r`/`\n`, always starts with `attachment; `, and round-trips the unicode through `filename*`.
  </action>
  <behavior>
    - unicode filename (e.g. `relazióne.pdf`) → `filename="relazione.pdf"` (folded fallback) + `filename*=UTF-8''relazi%C3%B3ne.pdf`
    - filename containing `"`, `\`, CR, LF → those bytes never appear raw in the output; exactly one `filename=` and one `filename*=`
    - empty filename → a safe non-empty fallback (e.g. `filename="download"`)
    - `;`/path-separator/>255-byte inputs → still exactly one of each param, no raw CR/LF, output starts with `attachment; `
  </behavior>
  <acceptance_criteria>
    - `internal/agui/content_disposition.go` contains `func contentDisposition(`; it does NOT reference `asciiCaption`/`foldToASCII`
    - `go test ./internal/agui/ -run TestContentDisposition_Property` exits 0
    - the test asserts: exactly one `filename=` substring, exactly one `filename*=UTF-8''` substring, no `\r`/`\n` in the output, and a unicode round-trip — across the fuzz/table inputs (grep the test for these invariants)
    - `go vet ./internal/agui/` clean; go.mod/go.sum byte-unchanged (unless a vendored property lib is used — confirm it was ALREADY present)
  </acceptance_criteria>
  <verify>
    <automated>go build ./... &amp;&amp; go test ./internal/agui/ -run TestContentDisposition_Property</automated>
  </verify>
  <done>contentDisposition emits a safe RFC-6266 dual-param header for any input (no raw CRLF, exactly one filename= + one filename*=, unicode preserved); property/table test green in the agui package (counts toward the coverage floor).</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| agent-produced file bytes → object store | untrusted content (agent/user-influenced filename + bytes) crosses into per-identity Garage + a persisted asset row |
| stored asset → future served header | the stored `FileName` becomes a `Content-Disposition` header value (built by this plan's helper, emitted by 37A-03) |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37A-HdrInj (T-HdrInj) | Tampering | `contentDisposition` (filename → header) | mitigate | RFC-6266 helper strips CR/LF/`"`/`\` from the fallback + percent-encodes `filename*`; property-tested for "no raw CRLF, exactly one of each param" (Task 3) |
| T-37A-Mem (agent artifact → searchable memory) | Information Disclosure / Repudiation | `IngestAgentFile` | mitigate | D-03: `processAsset` is NEVER called — a delivered file is display+ephemeral, never embedded/indexed. Asserted by the skip-processAsset unit test (Task 2) |
| T-37A-IDOR-svc (T-IDOR at the service seam) | Information Disclosure | `OpenForIdentity` | mitigate | `GetForIdentity` ownership gate precedes `objects.Get`; a non-owner never reaches the store read. Asserted by the ownership-precedes-read unit test (Task 2); the 404 surface lands in 37A-03 |
| T-37A-StoreLeak (store-URL leak) | Information Disclosure | `OpenForIdentity` return type | mitigate | Returns an `io.ReadCloser` (stream-through), NOT a presigned/direct store URL (D-09) — the private per-identity bucket is never exposed |
| T-37A-01-SC | Tampering | package installs | accept | Zero package installs this plan (Go stdlib `mime`/`net/url`/`io` + vendored). go.mod/go.sum byte-unchanged; the property lib is used ONLY if already vendored, else table-driven fallback (no install). No `[ASSUMED]`/`[SUS]`/`[SLOP]` package — no legitimacy checkpoint required |
</threat_model>

<verification>
- `go build ./... && go vet ./...` clean; `gofmt` clean.
- `go test -race ./internal/assets/ ./internal/agui/` green (daemon-free — counts toward the `db_integration neo4j_integration` 85% floor).
- `go test -tags db_integration ./internal/db/ -run TestMigration0035` green on the live stack (WSL/CI); local skip is honest (t.Fatal under $CI).
- `sqlc generate` zero-diff; go.mod/go.sum byte-unchanged.
- Coverage-floor guard: IngestAgentFile, OpenForIdentity, and contentDisposition are ALL untagged unit-tested (NOT behind `garage_integration`).
</verification>

<success_criteria>
Migration `0035` admits `agent` (roundtrip proven); `IngestAgentFile` ingests to an owned Accepted asset skipping Limits + processAsset; `OpenForIdentity` gates ownership before the store read; `contentDisposition` is CRLF-safe + unicode-preserving. All daemon-free surfaces unit-tested; the download route (37A-03) and ingest lane (37A-02) can now build against these.
</success_criteria>

<output>
Create `.planning/phases/37A-web-artifact-delivery-lane/37A-01-SUMMARY.md` when done.
</output>
