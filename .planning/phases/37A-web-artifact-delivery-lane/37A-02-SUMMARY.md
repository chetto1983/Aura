---
phase: 37A-web-artifact-delivery-lane
plan: 02
subsystem: agent-tools
tags: [send_file, asset-ingest, garage, artifact-descriptor, composition-root, telegram-regression, degrade]

# Dependency graph
requires:
  - phase: 37A-01
    provides: "assets.Service.IngestAgentFile, AgentIngestRequest, SourceAgent (delivery-only ingest seams)"
provides:
  - "tools.AssetDeliverer interface + SendFile.Assets field — primitive-typed injection seam so internal/agent/tools never imports internal/assets"
  - "ctx-aware (*SendFile).emitDelivery — both delivery tails (host Execute + routed deliverFromBox) best-effort ingest and enrich the aura.artifact descriptor"
  - "Descriptor enrichment: always-present tool_call_id + size_bytes (success AND degrade); asset_id + mime_type only on ingest success"
  - "cmd/aura sendFileAssetAdapter — structural AssetDeliverer over *assets.Service (os.Open -> IngestAgentFile -> asset.ID)"
  - "runtimeToolHandles.SendFile retained + serve-boot .Assets wiring (VERIF-7 post-construction, nil on static/manifest/CLI paths)"
  - "Telegram enriched-descriptor non-regression proof (WEBART-02 cross-channel)"
affects: [37A-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Primitive-typed capability seam wired post-construction on a retained runtime handle (mirrors ShellPoll/ShellKill.Caps VERIF-7) so the substrate never imports the concrete service"
    - "Best-effort ingest inside a shared ctx-aware delivery tail: both callers funnel through one emitDelivery, so ingest + degrade cover both tails with a single change (Landmine 1)"
    - "Always-present correlation key + stat size ride the descriptor on success AND degrade; only genuinely ingest-derived keys (asset_id/mime_type) are success-gated"

key-files:
  created:
    - internal/agent/tools/send_file_ingest.go
    - internal/agent/tools/send_file_ingest_test.go
    - cmd/aura/send_file_asset_adapter.go
    - cmd/aura/send_file_asset_adapter_test.go
  modified:
    - internal/agent/tools/send_file.go
    - internal/agent/tools/send_file_sandbox.go
    - cmd/aura/main.go
    - cmd/aura/serve.go
    - internal/channels/telegram/artifact_test.go

key-decisions:
  - "tool_call_id + size_bytes ride the descriptor UNCONDITIONALLY (success AND degrade): they are known before the ingest, like path — tool_call_id is 37A-04's web-reducer correlation key (a degrade with no tool_call_id would make D-02's render-only 'delivery unavailable' card unreachable)"
  - "ingestForDelivery takes the already-stat'd size (from emitDelivery's size gate) rather than re-stat'ing and returning it — single stat, avoids a TOCTOU size skew between the descriptor's size_bytes and the ingested size"
  - "Reused the existing exported tools.ToolCallIDFromContext instead of adding an in-package toolCallIDFromCtx duplicate (CLAUDE.md REUSABLE CODE / dupl gate)"

patterns-established:
  - "Post-construction capability wiring on a retained handle: construct nil-capable at registry build (path-only degrade), set the live capability at serve boot behind a nil guard"
  - "Degrade-never-errors delivery: any ingest miss keeps the legacy path-only descriptor and returns no Go error, so a delivery failure never wedges the turn"

requirements-completed: [WEBART-01, WEBART-02]

# Metrics
duration: ~40min
completed: 2026-07-08
---

# Phase 37A Plan 02: send_file Ingest Lane + Composition-Root Wiring Summary

**A channel-driven, authenticated send_file now turns a delivered host file (host-path AND sandbox-routed) into an owned Garage asset and an enriched `aura.artifact` descriptor (`asset_id`/`mime_type` on success; always-present `tool_call_id`/`size_bytes`), while every failure mode degrades to today's path-only behavior without erroring the turn and Telegram stays byte-for-byte unregressed.**

## Performance

- **Duration:** ~40 min
- **Completed:** 2026-07-08
- **Tasks:** 3 (all committed atomically)
- **Files created:** 4 · **Files modified:** 5

## Accomplishments
- `tools.AssetDeliverer` seam + `SendFile.Assets` + a ctx-aware `(*SendFile).emitDelivery` that best-effort ingests on BOTH delivery tails (host `Execute` and routed `deliverFromBox`) — the substrate imports neither `internal/assets` nor `internal/agent`.
- Descriptor enrichment: `tool_call_id` + `size_bytes` ride ALWAYS (success and the four-way degrade); `asset_id` + `mime_type` ride only on ingest success. Zero translator / event-schema change — the keys ride the existing `aura.artifact` event verbatim.
- Composition root wired: `runtimeToolHandles.SendFile` retained, `sendFileAssetAdapter` bridges the seam to the live `*assets.Service`, and serve boot sets `.Assets` behind a nil guard right after `buildAssetService` — static/manifest/CLI paths stay nil (path-only degrade, D-02).
- Telegram cross-channel non-regression proven: an enriched descriptor still delivers the document (path-driven, extra keys ignored); the missing-path no-op guard is intact.

## Task Commits

1. **Task 1: AssetDeliverer seam + ctx-aware emitDelivery ingest/degrade (both tails)** — `ce6c6b99` (feat)
2. **Task 2: composition-root wiring — retained SendFile handle + sendFileAssetAdapter + serve-boot Assets set** — `a2335fd9` (feat)
3. **Task 3: Telegram non-regression — enriched descriptor still delivers** — `98d1cca1` (test)

_Task 1 is TDD in spirit (the degrade matrix + both-tails + success-fields tests ship in the same commit as the impl, since emitDelivery is an existing tail being extended, not a green-field feature)._

## Files Created/Modified
- `internal/agent/tools/send_file_ingest.go` (new) — `AssetDeliverer` interface (primitive-typed) + `ingestForDelivery` best-effort helper (degrades on nil deliverer / unscoped identity / empty thread id / ingest error) + `guessDeliveryMIME`.
- `internal/agent/tools/send_file.go` — `Assets AssetDeliverer` field; `emitDelivery` becomes a ctx-aware method that enriches the descriptor (always-present `tool_call_id`/`size_bytes`, success-only `asset_id`/`mime_type`). `Spec()` Deferred:true/Mutating:false unchanged.
- `internal/agent/tools/send_file_sandbox.go` — routed tail passes `ctx` into `s.emitDelivery` so `deliverFromBox` ingests too.
- `internal/agent/tools/send_file_ingest_test.go` (new) — daemon-free: success field-set, four-way degrade matrix, both-tails ingest (a fake routed backend replays an in-memory tar), descriptor fields.
- `cmd/aura/main.go` — `SendFile *tools.SendFile` on `runtimeToolHandles`; `sf` retained (`handles.SendFile = sf`) before `reg.Register(sf)`.
- `cmd/aura/serve.go` — `chat.toolHandles.SendFile.Assets = sendFileAssetAdapter{svc: chat.assets}` behind a nil guard, immediately after `buildAssetService`.
- `cmd/aura/send_file_asset_adapter.go` (new) — `sendFileAssetAdapter` (`os.Open` -> `IngestAgentFile` -> `asset.ID`); compile-time `var _ tools.AssetDeliverer`.
- `cmd/aura/send_file_asset_adapter_test.go` (new) — recording `StoreBackend` proves the `os.Open`->request field mapping + returned id; registry retains a non-nil `SendFile` with nil `Assets`; `os.Open`-first ordering.
- `internal/channels/telegram/artifact_test.go` — enriched-descriptor-still-sends + pathless-enriched-still-ignored (production `artifact.go` untouched).

## Decisions Made
- `tool_call_id` + `size_bytes` are emitted unconditionally (success and degrade) — the web reducer's correlation key + the stat size are known before the ingest, so they degrade with `path`, not with `asset_id`.
- `ingestForDelivery` receives the already-stat'd size from `emitDelivery`'s size gate (single stat, no TOCTOU skew) rather than re-stat'ing and returning it.
- Reused `tools.ToolCallIDFromContext` rather than adding a duplicate in-package accessor.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] golangci-lint cross-worktree cache contamination blocked the first commit**
- **Found during:** Task 1 commit (pre-commit lint hook).
- **Issue:** The shared golangci-lint cache replayed 62 normally-excluded findings (`revive`/`gosec` in `internal/assets`, `internal/objectstore`, `internal/llm`, `internal/mcp`, …) attributed to a *sibling* worktree path (`agent-adceec7a6ed3f6b11`), so `.golangci.yml` path-based exclusions failed to match and the hook exited 1. `golangci-lint run ./internal/agent/tools/` in isolation reported 0 issues.
- **Fix:** `golangci-lint cache clean`, then re-ran the hook command → 0 issues, exit 0. Environmental (concurrent worktrees sharing GOCACHE), not a code defect. No code change.

**2. [Minor - signature] `ingestForDelivery(ctx, deliverer, hostPath, filename, size)`**
- The plan's literal helper signature returns `size`; I pass the already-known size in instead (single stat, avoids a size-skew TOCTOU between `size_bytes` and the ingested size). Behavior identical; no acceptance grep pins the signature.

**3. [Minor - doc comment] Reworded an `emitDelivery` doc comment**
- The initial comment named a channel; the `TestSendFileChannelAgnostic` grep guard forbids a channel name in `send_file.go`. Reworded to "a path-consuming channel" — no behavior change.

No architectural (Rule 4) deviations; no checkpoint returned.

## Verification
- `go build ./...` + `go vet ./cmd/aura/` clean; `gofmt -l` clean on all touched files.
- `go test ./internal/agent/tools/ ./internal/channels/telegram/ ./cmd/aura/` green (all daemon-free — count toward the coverage floor).
- Grep guards: `send_file.go`/`send_file_ingest.go` import neither `internal/assets` nor `internal/agent`; `emitDelivery(ctx` threaded on both tails; `Spec()` Deferred:true/Mutating:false; `handles.SendFile = sf`; `SendFile.Assets =` set behind a nil guard after `buildAssetService`; channel-agnostic grep test passes.
- go.mod / go.sum byte-unchanged.
- **Not run locally:** `go test -race` (needs CGO + a C compiler; deferred to WSL/CI per project setup, same constraint as 37A-01).

## Known Stubs
None — the lane is fully wired: the production `SendFile` ingests via the live `*assets.Service` at serve boot; only the static/manifest/CLI paths intentionally leave `Assets` nil (D-02 path-only degrade, covered by the wiring-guard test).

## Next Phase Readiness
- **37A-04** (web reducer) can now rely on `aura.artifact` carrying `tool_call_id` (correlation key) + `size_bytes` on every delivery, and `asset_id`/`mime_type` on authenticated success — the exact keys its `isArtifactDescriptor` guard + `local_artifact` synthesis consume.

## Self-Check: PASSED
- All 4 created files present on disk.
- All 4 commits (`ce6c6b99`, `a2335fd9`, `98d1cca1`, `3ce4033d`) present in git history.

---
*Phase: 37A-web-artifact-delivery-lane*
*Completed: 2026-07-08*
