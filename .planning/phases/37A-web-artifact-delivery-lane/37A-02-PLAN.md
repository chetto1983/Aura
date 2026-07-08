---
phase: 37A-web-artifact-delivery-lane
plan: 02
type: execute
wave: 2
depends_on: ["37A-01"]
files_modified:
  - internal/agent/tools/send_file.go
  - internal/agent/tools/send_file_ingest.go
  - internal/agent/tools/send_file_sandbox.go
  - internal/agent/tools/send_file_ingest_test.go
  - cmd/aura/main.go
  - cmd/aura/serve.go
  - cmd/aura/send_file_asset_adapter.go
  - cmd/aura/send_file_asset_adapter_test.go
  - internal/channels/telegram/artifact_test.go
autonomous: true
requirements: [WEBART-01, WEBART-02]

must_haves:
  truths:
    - "A send_file under an authenticated identity with a thread id ingests the file (via the AssetDeliverer seam) and the aura.artifact descriptor gains asset_id + mime_type on ingest SUCCESS; size_bytes (the stat size) rides ALWAYS, like path/tool_call_id (D-01 always-ingest-when-authenticated, channel-agnostic; the new fields ride the EXISTING aura.artifact event verbatim per D-07 — no new event). tool_call_id is emitted UNCONDITIONALLY whenever a tool-call context is present — on SUCCESS *and* on DEGRADE (asset_id absent) — as the web reducer's correlation key: 37A-04's isArtifactDescriptor REQUIRES it to attach ANY local_artifact card, so a degrade with no tool_call_id would leave D-02's 'web shows the render-only card' silently unreachable (non-sensitive; Telegram reads only path and ignores it)"
    - "BOTH delivery tails ingest: host-path Execute AND routed deliverFromBox produce an asset_id (both funnel through the ctx-aware emitDelivery)"
    - "Any ingest miss — nil Assets, empty identity, empty thread id, or an IngestAgentDelivery error — degrades to a {path,filename,caption,tool_call_id,size_bytes} descriptor with NO asset_id and NO mime_type (tool_call_id + size_bytes stay so 37A-04's reducer still attaches the render-only 'delivery unavailable' card showing filename + size — D-02), and never errors the turn (D-02 best-effort degrade; the empty-thread-id / no-ConvID case is D-05)"
    - "The descriptor still carries path on success and on degrade, so Telegram delivery is unregressed"
    - "The production SendFile (cmd/aura serve boot) has Assets wired; the static registry / manifest / CLI paths leave Assets nil → path-only degrade"
  artifacts:
    - path: "internal/agent/tools/send_file_ingest.go"
      provides: "AssetDeliverer interface + ingest/degrade helper + toolCallIDFromCtx"
      contains: "type AssetDeliverer interface"
    - path: "internal/agent/tools/send_file.go"
      provides: "SendFile.Assets field + ctx-aware emitDelivery + descriptor enrichment"
      contains: "Assets AssetDeliverer"
    - path: "cmd/aura/send_file_asset_adapter.go"
      provides: "sendFileAssetAdapter structural AssetDeliverer impl over *assets.Service"
      contains: "IngestAgentDelivery"
    - path: "cmd/aura/main.go"
      provides: "SendFile retained on runtimeToolHandles"
      contains: "SendFile"
    - path: "cmd/aura/serve.go"
      provides: "handles.SendFile.Assets set post-buildAssetService"
      contains: "SendFile.Assets"
  key_links:
    - from: "internal/agent/tools/send_file.go Execute + send_file_sandbox.go deliverFromBox"
      to: "the shared ctx-aware emitDelivery"
      via: "both callers pass ctx into emitDelivery"
      pattern: "emitDelivery\\(ctx"
    - from: "emitDelivery"
      to: "IngestAgentDelivery via the ingest/degrade helper"
      via: "best-effort ingest resolving identityctx.IdentityID + toolCallCtx(ctx).sessionID"
      pattern: "IngestAgentDelivery|ingestForDelivery"
    - from: "cmd/aura/serve.go"
      to: "handles.SendFile.Assets"
      via: "sendFileAssetAdapter{chat.assets} set after chat.assets is non-nil (serve.go:292)"
      pattern: "SendFile.Assets ="
    - from: "descriptor tool_call_id"
      to: "toolCallCtx(ctx).toolCallID"
      via: "in-package tool-call ctx (result.go), NO internal/agent import; emitted UNCONDITIONALLY on success AND degrade (always-present correlation key)"
      pattern: "toolCallID"
  prohibitions:
    - "SendFile.Spec() MUST keep Deferred:true and Mutating:false — do NOT flip Mutating (it would arm the completion-gate critic / require a ledger reservation)"
    - "No channel-named field or branch may be added to the descriptor or the tool (the substrate names no channel; the channel-agnostic grep test must still pass)"
    - "The degraded descriptor MUST NOT contain an asset_id key"
    - "send_file.go / send_file_ingest.go MUST NOT import internal/agent (import cycle) — read the thread id via toolCallCtx(ctx).sessionID, NOT agent.SwarmContext"
    - "buildRegistry()/buildRegistryWithMCP signatures MUST NOT change to carry the asset service — wire post-construction on runtimeToolHandles only"
---

<objective>
Wire the ingest lane into `send_file` and the composition root so that a channel-driven, authenticated turn turns a delivered file into an owned Garage asset and an enriched `aura.artifact` descriptor — while CLI/static paths and every failure mode degrade to today's path-only behavior, and Telegram stays byte-for-byte unregressed.

Purpose: this is the WEBART-01 tool-side ingest call + the WEBART-02 descriptor enrichment. It joins 37A-01's `assets.Service.IngestAgentFile` (via a `cmd/aura` adapter) to the tool through a narrow `AssetDeliverer` interface, mirroring the shipped `ShellPoll/ShellKill.Caps` VERIF-7 post-construction wiring so the substrate never imports `internal/assets`.

Output: `AssetDeliverer` + `SendFile.Assets` + a ctx-aware `emitDelivery` that ingests on BOTH tails; the success-only `asset_id`/`mime_type` descriptor keys PLUS the always-present `tool_call_id` correlation key and `size_bytes` stat size (both emitted on success AND degrade); the `sendFileAssetAdapter` + `runtimeToolHandles.SendFile` + serve-boot set; a Telegram non-regression assertion.

## Research corrections honored (do not regress)
- **D-08 literal is UNREACHABLE (RESEARCH Gap A):** `internal/agent/tools` cannot import `internal/agent`, so `agent.SwarmContext(ctx).ConvID` is impossible from `send_file.go`. Use the in-package equivalent `toolCallCtx(ctx).sessionID` (== ConvID, proven `llm_agent.go:545-546`, `result.go:19,47`) for the thread id, `identityctx.IdentityID(ctx)` for the identity, and `toolCallCtx(ctx).toolCallID` for the correlation id. D-08's intent (reuse the ambient thread id; defer the `threadctx` cleanup) is preserved.
- **`tool_call_id` on the descriptor — ALWAYS, not success-only (WEBART-02 / Landmine 5 / cross-plan reachability):** `aura.artifact`'s value is only the descriptor map and carries no correlation id, so the web reducer (37A-04) can't attach ANY card without it — its `isArtifactDescriptor` guard REQUIRES `tool_call_id` as the `ensureTool`/`writeTool` key. Emit `tool_call_id` on the descriptor UNCONDITIONALLY whenever a tool-call context is present — on ingest SUCCESS **and** on the D-02 Put-error / D-05 empty-thread DEGRADE (asset_id absent). Emitting it success-only would leave the D-02 "web shows the render-only card" decision silently violated (the "delivery unavailable" degrade card would be unreachable — the reducer attaches nothing). It is a non-sensitive correlation id available from the tool-call context regardless of channel/identity; Telegram reads only `path` and ignores it (unregressed). The descriptor still rides Meta→ArtifactDelta→`emitToolResultCustom` verbatim — ZERO translator/`llm_agent_events` change (Landmine 6).
- **BOTH tails funnel through `emitDelivery` (Landmine 1):** doing the ingest inside a ctx-aware `emitDelivery` covers host-path `Execute` AND routed `deliverFromBox`. Missing the routed tail silently degrades the PRIMARY use case (a DOCX built inside the sandbox) to path-only on web — the both-tails test is mandatory.
- **Telegram non-regression is the top blast-radius risk (success criterion 2):** the descriptor still carries `path`; `telegram/artifact.go:55` reads only `path` and ignores extra keys. Ship the enriched-descriptor-still-sends assertion.
</objective>

> **Phase symbols:** see `37A-01-PLAN.md` §"Artifacts This Phase Produces" for the full phase symbol list (whole-phase source-grounding exclusion — do not flag cross-plan symbols as drift).

<execution_context>
@.claude/get-shit-done/workflows/execute-plan.md
@.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37A-web-artifact-delivery-lane/37A-CONTEXT.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-RESEARCH.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-PATTERNS.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-01-SUMMARY.md
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: AssetDeliverer seam + ctx-aware emitDelivery ingest/degrade + descriptor enrichment (both tails)</name>
  <files>internal/agent/tools/send_file_ingest.go, internal/agent/tools/send_file.go, internal/agent/tools/send_file_sandbox.go, internal/agent/tools/send_file_ingest_test.go</files>
  <read_first>
    - internal/agent/tools/send_file.go (`SendFile` struct :27; `Execute` :85; `emitDelivery` :123-148 — the shared tail both callers hit; `Spec()` :54-77 keeps Deferred:true/Mutating:false; `asciiCaption`)
    - internal/agent/tools/send_file_sandbox.go (`deliverFromBox` :24, emits via `emitDelivery` :49 — the routed tail that MUST also ingest)
    - internal/agent/tools/result.go (`toolCallContext` :19-20 with `sessionID`/`toolCallID`; the `toolCallCtx(ctx)` accessor :47-50 — the in-package seam)
    - internal/identityctx/identityctx.go (`IdentityID(ctx)` :26 → "" when unscoped; already imported in tools via shell_bg_owner.go)
    - internal/assets/ingest_agent.go + types.go (IngestAgentFile + AgentIngestRequest from 37A-01 — the adapter target; the tool itself must NOT import assets)
    - 37A-PATTERNS.md §4 (AssetDeliverer + injection seam) + §5 (emitDelivery enrichment, both tails) + 37A-RESEARCH.md Gap A/Gap C + Landmine 1/5/6
  </read_first>
  <action>
    Create `internal/agent/tools/send_file_ingest.go` (new concern file, mirrors the `send_file_sandbox.go` split so `send_file.go` stays under 600 LOC): define `type AssetDeliverer interface { IngestAgentDelivery(ctx context.Context, identityID, threadID, hostPath, filename, mimeType string, size int64) (assetID string, err error) }` (primitive-typed so `tools` never imports `internal/assets`); a helper `ingestForDelivery(ctx, deliverer, hostPath, filename string) (assetID string, size int64, mimeType string, ok bool)` that returns `ok=false` (degrade) when `deliverer == nil` OR `identityctx.IdentityID(ctx) == ""` OR `toolCallCtx(ctx).sessionID == ""` OR `IngestAgentDelivery` errs — resolving identity via `identityctx.IdentityID(ctx)`, thread via `toolCallCtx(ctx).sessionID`, and passing the stat size + a sniffed/guessed mime; and `toolCallIDFromCtx(ctx) string` returning `toolCallCtx(ctx).toolCallID`. Add `Assets AssetDeliverer` to the `SendFile` struct in `send_file.go` (nil ⇒ path-only, D-02). Change `emitDelivery` to a method/func taking `ctx` and the `*SendFile` (or `s.Assets`) — both callers (`Execute` :116 and `deliverFromBox` :49) already hold ctx; pass it. Inside the shared tail, after building the legacy `{path,filename,caption}` descriptor and passing the size gate (which has already stat'd the file for the `maxSendFileBytes` check): FIRST add — UNCONDITIONALLY, both riding the descriptor on SUCCESS *and* DEGRADE exactly like `path` — `tool_call_id` = `toolCallIDFromCtx(ctx)` whenever it is non-empty (a tool-call context is present; it is the web reducer's correlation key and MUST ride on success AND degrade, else 37A-04 attaches no card and D-02's render-only "delivery unavailable" card is unreachable — `tool_call_id` is NOT gated on ingest success) AND `size_bytes` = the already-stat'd file size the size-gate computed for the `maxSendFileBytes` check (the same value `ingestForDelivery` hands to `IngestAgentDelivery` on success) whenever it is available (the size is known BEFORE the ingest, so it degrades with `path`, NOT with `asset_id`). THEN best-effort ingest: if `ingestForDelivery(...)` returns ok, add ONLY `asset_id` + `mime_type` to the descriptor (the genuinely ingest-derived keys — `asset_id` the created row id, `mime_type` the `hashAndSniff` content-type); on not-ok leave those two off (the descriptor stays `{path,filename,caption,tool_call_id,size_bytes}`). Do NOT touch `Spec()` — Deferred:true/Mutating:false stay. Write `internal/agent/tools/send_file_ingest_test.go` with a fake `AssetDeliverer`.
  </action>
  <behavior>
    - success: `Assets` set + identityctx scoped + non-empty sessionID → descriptor has path + filename + caption + asset_id + size_bytes + mime_type + tool_call_id; the fake recorded one IngestAgentDelivery with the resolved identity/thread/hostPath
    - degrade matrix (each set up with a tool-call context carrying a toolCallID → descriptor has path + tool_call_id + size_bytes but NO asset_id and NO mime_type, ToolResult carries no Go error): (a) `Assets == nil`; (b) identityctx unset (empty identity); (c) empty sessionID (no thread); (d) `IngestAgentDelivery` returns an error
    - both tails: a host-path `Execute` AND a routed `deliverFromBox` (fake router) each produce an asset_id when the deliverer succeeds (Landmine 1)
    - Spec() unchanged: Deferred==true, Mutating==false
  </behavior>
  <acceptance_criteria>
    - `internal/agent/tools/send_file_ingest.go` contains `type AssetDeliverer interface` and `IngestAgentDelivery`; `send_file.go` struct contains `Assets AssetDeliverer`
    - `go build ./internal/agent/tools/` compiles WITHOUT importing internal/agent or internal/assets (grep the two files' imports — neither package appears)
    - `go test -race ./internal/agent/tools/ -run 'TestSendFile_Degrade|TestSendFile_Ingest|TestSendFile_DescriptorFields'` exits 0; the degrade matrix has all four cases AND asserts the degrade descriptor carries `tool_call_id` (present) + `path` (present) + `size_bytes` (present) with NO `asset_id` (absent) and NO `mime_type` (absent); the both-tails case is present (grep the test)
    - a grep confirms `emitDelivery(ctx` (ctx threaded) and that `Spec()` still returns `Deferred: true`/`Mutating: false`
    - the existing channel-agnostic grep test for send_file still passes (no channel-named token added)
  </acceptance_criteria>
  <verify>
    <automated>go build ./... &amp;&amp; go vet ./... &amp;&amp; go test -race ./internal/agent/tools/ -run 'SendFile'</automated>
  </verify>
  <done>send_file ingests on both tails via the AssetDeliverer seam, enriches the descriptor with asset_id/mime_type on ingest success, ALWAYS rides tool_call_id + size_bytes whenever available (success AND degrade — the web correlation key + the stat size), degrades to a {path,filename,caption,tool_call_id,size_bytes} descriptor (no asset_id, no mime_type) on any miss without erroring the turn, and keeps Deferred/Mutating + channel-agnostic invariants.</done>
</task>

<task type="auto">
  <name>Task 2: Composition-root wiring — retain SendFile on runtimeToolHandles + sendFileAssetAdapter + serve-boot Assets set</name>
  <files>cmd/aura/main.go, cmd/aura/serve.go, cmd/aura/send_file_asset_adapter.go, cmd/aura/send_file_asset_adapter_test.go</files>
  <read_first>
    - cmd/aura/main.go (`runtimeToolHandles` :114-123 — the ShellPoll/ShellKill retention precedent; the `reg.Register(&tools.SendFile{...})` at :199 to change into retain+register; the ShellPoll retain shape :181-186)
    - cmd/aura/serve.go (the `.Caps` set-site :279-284 — the exact post-construction precedent; `chat.assets = buildAssetService(...)` :292 — set `handles.SendFile.Assets` immediately AFTER this line, where chat.assets is non-nil; `SetAssetService(chat.assets)` :364 confirms *assets.Service is the concrete)
    - cmd/aura/document_processor_wiring.go (`buildAssetService` — where the adapter can live near, the only package importing both tools and assets)
    - internal/assets/ingest_agent.go (IngestAgentFile signature from 37A-01)
    - 37A-PATTERNS.md §4a/4b/4d (handle retention, serve-boot set, the sendFileAssetAdapter) + 37A-RESEARCH.md Gap A (Assumption A2 — ordering confirmed)
  </read_first>
  <action>
    Add `SendFile *tools.SendFile` to `runtimeToolHandles` in `main.go` with a doc-comment mirroring the ShellPoll/ShellKill note (retained so serve boot sets `.Assets`; nil on pool-free manifest paths → path-only degrade, D-02). Change `main.go:199` from an inline `reg.Register(&tools.SendFile{...})` to construct `sf := &tools.SendFile{WorkspaceRoot: workspace, Router: sandboxRouter}`, assign `handles.SendFile = sf`, then `reg.Register(sf)`. Create `cmd/aura/send_file_asset_adapter.go` with `type sendFileAssetAdapter struct{ svc *assets.Service }` and a method `IngestAgentDelivery(ctx, identityID, threadID, hostPath, filename, mimeType string, size int64) (string, error)` that `os.Open`s hostPath (already workspace-fenced by send_file — `#nosec` with a justification comment), `defer`-closes it, calls `svc.IngestAgentFile(ctx, assets.AgentIngestRequest{IdentityID, ThreadID, FileName, MIMEType, SizeBytes, Reader})`, and returns `asset.ID` (or the error). In `serve.go`, immediately after `chat.assets = buildAssetService(...)` (:292), add a nil-guarded set: `if chat.toolHandles.SendFile != nil { chat.toolHandles.SendFile.Assets = sendFileAssetAdapter{svc: chat.assets} }`. Do NOT change `buildRegistry`/`buildBaseRegistryWithHandles` signatures. Write `cmd/aura/send_file_asset_adapter_test.go` (docker-free) proving the wiring guard: the adapter satisfies `tools.AssetDeliverer`; the built registry retains a non-nil `handles.SendFile`; and the adapter forwards to a fake/recording `IngestAgentFile` seam (or asserts the `os.Open`→request mapping).
  </action>
  <acceptance_criteria>
    - `runtimeToolHandles` contains a `SendFile *tools.SendFile` field; `main.go` retains the pointer before `reg.Register(sf)` (grep `handles.SendFile = sf`)
    - `cmd/aura/send_file_asset_adapter.go` contains `func (a sendFileAssetAdapter) IngestAgentDelivery`; a compile-time `var _ tools.AssetDeliverer = sendFileAssetAdapter{}` (or equivalent) asserts satisfaction
    - `serve.go` sets `chat.toolHandles.SendFile.Assets` behind a nil guard, textually AFTER the `chat.assets = buildAssetService` line
    - `go build ./... && go vet ./...` clean; `go test ./cmd/aura/ -run 'SendFileAsset|WiresAssets|SendFile'` exits 0 (docker-free wiring guard)
    - `buildRegistry`/`buildBaseRegistryWithHandles` signatures unchanged (grep confirms the static/manifest paths still call them without an asset arg → SendFile.Assets stays nil there)
    - go.mod / go.sum byte-unchanged
  </acceptance_criteria>
  <verify>
    <automated>go build ./... &amp;&amp; go vet ./... &amp;&amp; go test ./cmd/aura/ -run 'SendFile'</automated>
  </verify>
  <done>The production SendFile has Assets wired at serve boot via sendFileAssetAdapter over the live *assets.Service; static/manifest/CLI paths leave it nil (path-only degrade); a docker-free wiring guard proves both.</done>
</task>

<task type="auto">
  <name>Task 3: Telegram non-regression — enriched descriptor still delivers the document</name>
  <files>internal/channels/telegram/artifact_test.go</files>
  <read_first>
    - internal/channels/telegram/artifact.go (`artifactDescriptor` :74; reads ONLY `path` :55 and ignores extra keys — the unregressed contract)
    - internal/channels/telegram/artifact_test.go (`TestArtifactConsumeChannelDrainsAll` :154-169 and the `artifactCustom` helper + `docBot` fake at :140-162 — the analog to extend)
    - 37A-PATTERNS.md §5 (Telegram stays unregressed) + 37A-VALIDATION.md WEBART-02 regression row
  </read_first>
  <action>
    Extend `internal/channels/telegram/artifact_test.go` with a regression case: feed an ENRICHED descriptor carrying `path` PLUS the new keys (`path`, `filename`, `caption`, `asset_id`, `size_bytes`, `mime_type`, `tool_call_id`) through the same `consume`/`consumeEvent` path the existing `TestArtifactConsumeChannelDrainsAll` uses, and assert the document is STILL sent (the extra keys are ignored, `path` still drives delivery). Reuse the existing `artifactCustom` helper + `docBot` fake — no new fakes. Keep the existing pathless-artifact no-op assertion intact.
  </action>
  <behavior>
    - an aura.artifact descriptor with path + asset_id + size_bytes + mime_type + tool_call_id → `consumeEvent` returns ok and a document is recorded (unregressed)
    - a descriptor with the new keys but NO path → still a no-op (missing-path guard intact)
  </behavior>
  <acceptance_criteria>
    - `internal/channels/telegram/artifact_test.go` contains a case passing `asset_id`/`size_bytes` alongside `path` and asserting a document is still sent
    - `go test -race ./internal/channels/telegram/ -run 'Artifact'` exits 0
    - no change to `internal/channels/telegram/artifact.go` production code (grep: the file is not in files_modified — regression is test-only)
  </acceptance_criteria>
  <verify>
    <automated>go test -race ./internal/channels/telegram/ -run 'Artifact'</automated>
  </verify>
  <done>An enriched descriptor still delivers via telegram/artifact.go (path-driven), proving WEBART-02's cross-channel non-regression; the missing-path no-op guard is intact.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| tool ctx → identity/thread resolution | the tool reads the authenticated principal + thread id off the ambient ctx to scope the ingest; an unscoped ctx must degrade, never fall back to a foreign identity |
| host file → object store | the delivered file's bytes cross into the per-identity Garage store via the adapter (bytes already 50-MiB-gated + workspace-fenced by send_file) |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37A-02-Scope | Spoofing / Information Disclosure | ingest identity resolution | mitigate | Identity via `identityctx.IdentityID(ctx)`; an empty/unscoped identity DEGRADES to path-only (no ingest, no foreign write) — the ingest can only ever write under the resolved principal's own key (`IngestAgentFile` uses `objectsFor(WithIdentityID(req.IdentityID))`). Asserted by the empty-identity degrade case (Task 1) |
| T-37A-02-Mem (agent artifact → searchable memory) | Information Disclosure | ingest call | mitigate | The adapter calls `IngestAgentFile` (D-03: no processAsset), never a processing/indexing path — carried from 37A-01 |
| T-37A-02-Gate | Elevation of Privilege | `SendFile.Spec()` classification | mitigate | Keep `Mutating:false` — flipping it would arm the completion-gate critic + force a ledger reservation per delivery (GATE-03). Asserted by the Spec()-unchanged check (Task 1) |
| T-37A-02-Regress | Denial of Service (channel) | telegram consumer | mitigate | The descriptor still carries `path`; the enriched-descriptor-still-sends regression test (Task 3) proves Telegram delivery is not wedged by the new keys |
| T-37A-02-SC | Tampering | package installs | accept | Zero installs (Go stdlib `os` + vendored). go.mod/go.sum byte-unchanged. No `[ASSUMED]`/`[SUS]`/`[SLOP]` package — no legitimacy checkpoint |
</threat_model>

<verification>
- `go build ./... && go vet ./...` + `gofmt` clean.
- `go test -race ./internal/agent/tools/ ./internal/channels/telegram/ ./cmd/aura/` green (all daemon-free — count toward the coverage floor).
- Grep guards: `send_file.go`/`send_file_ingest.go` import neither `internal/agent` nor `internal/assets`; `Spec()` returns Deferred:true/Mutating:false; the degraded descriptor has no `asset_id` (and no `mime_type`) but DOES carry `tool_call_id` + `size_bytes` (web-card reachability + the filename/size degrade card).
- The channel-agnostic grep test for send_file still passes.
- go.mod/go.sum byte-unchanged.
</verification>

<success_criteria>
A channel-driven authenticated `send_file` (host path AND sandbox-routed) ingests to an owned asset and emits `asset_id`+`mime_type` (success) plus an always-present `tool_call_id` correlation key and `size_bytes` stat size on `aura.artifact`; every failure mode degrades to a `{path,filename,caption,tool_call_id,size_bytes}` descriptor (no asset_id, no mime_type) without erroring the turn; Telegram is unregressed; the production tool is wired, static/CLI paths are nil. Zero translator/event changes.
</success_criteria>

<output>
Create `.planning/phases/37A-web-artifact-delivery-lane/37A-02-SUMMARY.md` when done.
</output>
</output>
