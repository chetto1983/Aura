---
phase: 03-llm-client-toolresult
plan: 03
subsystem: agent-tools
tags: [toolresult, sidecar, spillover, builtins, path-traversal, kv-cache]
requires:
  - "internal/config: RunDir + ToolPreviewCap (already wired, Phase 1)"
  - "internal/agent/tools: Spec/Registry/Tool (Phase 2)"
provides:
  - "tools.ToolResult{Preview,FullPath,Bytes,Truncated} + migrated Tool.Execute (ToolResult, error)"
  - "tools.NewResult ctx-injected preview/sidecar spillover helper (D-25) + WithToolCallContext"
  - "tools.ReadToolOutput byte-ranged builtin (D-27/A4, unknown-id hard-fail)"
  - "tools.CurrentTime cache-safe builtin (Req#14, RFC-3339 UTC + IANA tz)"
affects:
  - "internal/agent (Plan 04 LlmAgent): runTool dispatches Tool.Execute, injects WithToolCallContext, registers the two builtins"
  - "cmd/aura (Plan 05 chat): buildRegistry registers read_tool_output + current_time"
tech-stack:
  added: []
  patterns:
    - "ctx-injected ids for spillover (no per-tool reimplementation, DRY)"
    - "validate opaque-id-shaped segments before filepath.Join (path-traversal mitigation)"
    - "UTF-8 rune-boundary truncation backoff"
    - "wall clock read only in the tool path, never in messages[0] (KV cache discipline)"
key-files:
  created:
    - internal/agent/tools/result.go
    - internal/agent/tools/read_tool_output.go
    - internal/agent/tools/current_time.go
    - internal/agent/tools/result_test.go
    - internal/agent/tools/read_tool_output_test.go
    - internal/agent/tools/current_time_test.go
    - internal/agent/tools/main_test.go
  modified:
    - internal/agent/tools/spec.go
    - internal/agent/tools/text_response.go
    - internal/agent/tools/search.go
decisions:
  - "Spillover helper lives in internal/agent/tools (NewResult) — ctx-injected ids keep tools DRY (D-25)"
  - "read_tool_output offset/limit are BYTES (D-27/A4), not lines; schema text affirms bytes"
  - "Unknown/never-spilled tool_call_id -> Execute returns an error (RoleTool the model sees), not a panic (D-15)"
  - "session_id + tool_call_id validated (no .. / path separators) before filepath.Join; fixed conversations/ prefix never model-controlled (T-03-07)"
  - "Sidecar write failure degrades clean: preview + 'full output unavailable' note, no terminal error (D-29)"
  - "current_time reads the wall clock only in the tool path; never enters the cached prompt prefix (D-08)"
metrics:
  duration: ~25min
  completed: 2026-05-30
  tasks: 2
  files: 10
---

# Phase 3 Plan 3: ToolResult Migration + Spillover Helper + Builtins Summary

Migrated `Tool.Execute` from `(string, error)` to `(ToolResult, error)` in one coupled commit (spec.go + text_response.go + search.go), added the shared ctx-injected `tools.NewResult` preview/sidecar spillover helper with path-traversal-rejecting id validation, and landed the two non-deferred builtins `read_tool_output` (byte-ranged paging, unknown-id hard-fail) and `current_time` (cache-safe RFC-3339 UTC + IANA tz). SPEC Req#6/#7/#8/#14 + threat T-03-07/T-03-09 are green under `-race` with `golangci-lint` 0 issues.

## What Was Built

### Task 1 — Coupled Tool.Execute migration + tools.NewResult (commit c0da66fa)

- **spec.go**: added `ToolResult{Preview, FullPath, Bytes, Truncated}` and migrated the `Tool` interface to `Execute(ctx, args) (ToolResult, error)`. The migration of all three on-the-old-signature tools (TextResponse, ToolSearch) landed in this single coupled commit per the SPEC Constraint (avoid re-opening the ≤600-LOC files twice).
- **result.go (NEW)**: `NewResult(ctx, content) (ToolResult, error)` reads `session_id`/`tool_call_id`/`run_dir`/`cap` from the ctx via `WithToolCallContext` (the agent injects them before each Execute — D-25). `≤cap` → preview-only, no disk write. `>cap` → `truncatePreview` (UTF-8 rune-boundary backoff) + a byte-based `read_tool_output(...)` footer pointer in history + the FULL bytes written to `<run_dir>/conversations/<session_id>/<tool_call_id>.result` (lazy `os.MkdirAll`, D-26). Write failure → preview + `[full output unavailable: <reason>]`, no error (D-29). `validateID` rejects `..` and path separators (`/`, `\`, `os.IsPathSeparator`) BEFORE `filepath.Join` (T-03-07).
- **text_response.go / search.go**: adapted to the new signature. `text_response` returns a direct small `ToolResult{Preview, Bytes}` (terminal, never spills); `tool_search` routes its output through `NewResult` so large deferred-spec selects page via the sidecar.
- **main_test.go**: `goleak.VerifyTestMain` parity (internal test package so spillover tests reach unexported helpers).

### Task 2 — read_tool_output + current_time builtins (commit b962d583)

- **read_tool_output.go (NEW, Deferred:false)**: parses `{tool_call_id, offset?, limit?}` (BYTES, default limit 2048 — D-27/A4); reuses `sidecarPath` id-validation (T-03-07); reads the byte range `[offset, offset+limit)` clamped to file size; unknown/never-spilled id → error naming the id (D-15/Req#7); negative offset rejected (T-03-09). Footer: `showing bytes X-Y of Z, next offset Y`.
- **current_time.go (NEW, Deferred:false)**: `time.Now().UTC()` RFC-3339 by default; non-empty IANA tz via `time.LoadLocation` carrying the offset; invalid tz / malformed args → error, not panic. Wall clock read only here, never in `messages[0]` (D-08).

## How To Verify

```
cd /d/Aura   # (WSL: /mnt/d/Aura)
go vet ./internal/agent/tools/ && go build ./...
go test ./internal/agent/tools/ -run 'TestNewResult|TestSidecarLayout|TestSidecarPathTraversal|TestTruncatePreview|TestTextResponse|TestToolSearch|TestReadToolOutput|TestCurrentTime'
BASH_ENV=~/.aura-toolchain.sh go test -race ./internal/agent/tools/   # native Windows; WSL native race is simpler
golangci-lint run ./internal/agent/tools/...   # 0 issues
```

All green: 21 test functions (incl. the rapid UTF-8 truncation property and the 8-case + 4-case traversal tables) pass; `-race` clean; `golangci-lint` 0 issues; every touched/new file ≤600 LOC (largest: result.go 142).

## Requirement / Threat Coverage

| Item | Where | Test |
|------|-------|------|
| Req#6 (preview/sidecar, UTF-8 boundary) | result.go | TestNewResult_LargeSpills / _SmallNoSidecar / TestTruncatePreview_Property+_RuneBoundary |
| Req#7 (byte ranges, unknown-id hard-fail) | read_tool_output.go | TestReadToolOutput_ByteSlice / _UnknownID / _Defaults / _OffsetPastEOF |
| Req#8 (sidecar layout) | result.go | TestSidecarLayout |
| Req#14 (current_time UTC + IANA) | current_time.go | TestCurrentTime_DefaultUTC / _IANAOffset / _InvalidTZ |
| T-03-07 (path traversal) | result.go sidecarPath | TestSidecarPathTraversal + TestReadToolOutput_PathTraversal |
| T-03-09 (offset/limit clamp) | read_tool_output.go | TestReadToolOutput_OffsetPastEOF / _NegativeOffset |
| D-29 (sidecar-fail degrade) | result.go | TestNewResult_WriteFailureDegrades |

## Deviations from Plan

None — plan executed exactly as written. Two minor, in-scope implementation choices worth noting:
- `tool_search` now routes its output through `NewResult` (rather than returning a bare string) so a large multi-spec select pages via the sidecar instead of bloating history; this is the DRY intent of D-25 and required injecting a ctx in its test.
- The `if CompositeLiteral{}.Method()` Go parse ambiguity required parenthesizing the composite literal in two `_Deferred` tests — a syntax fix during execution, no behavior change.

## Notes for Downstream Plans

- **Plan 04 (LlmAgent)**: before dispatching each tool, call `tools.WithToolCallContext(ctx, sessionID, toolCallID, cfg.RunDir, cfg.ToolPreviewCap)` and pass the returned ctx into `Tool.Execute`. `sessionID = Event.ThreadID` (D-26). Thread `ToolResult.Preview` into the `RoleTool` history message.
- **Plan 04/05**: register `tools.ReadToolOutput{}` and `tools.CurrentTime{}` in `buildRegistry` (both `Deferred:false`).
- A tool whose output exceeds the preview cap but is dispatched WITHOUT `WithToolCallContext` will get a clear error from `NewResult` (missing tool-call context) — the agent must always inject the ctx.

## Self-Check: PASSED
- internal/agent/tools/result.go — FOUND
- internal/agent/tools/read_tool_output.go — FOUND
- internal/agent/tools/current_time.go — FOUND
- internal/agent/tools/result_test.go — FOUND
- internal/agent/tools/read_tool_output_test.go — FOUND
- internal/agent/tools/current_time_test.go — FOUND
- internal/agent/tools/main_test.go — FOUND
- commit c0da66fa — FOUND
- commit b962d583 — FOUND
