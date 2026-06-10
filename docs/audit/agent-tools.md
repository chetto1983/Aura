# Audit: internal/agent/tools

**Verdict:** needs-work — one fully-implemented tool is never registered in production, making the artifact delivery pipeline unreachable by the LLM; one always-true conditional constitutes dead code.

**Counts:** critical 0 / high 1 / medium 1 / low 1

---

## Findings

### [HIGH][NOT-WIRED] `SendFile` tool implemented but never registered in production

**Location:** `internal/agent/tools/send_file.go:25` (struct), `cmd/aura/main.go:99–142` (registry builder)

**Confidence:** high

**Detail:**

`SendFile` (`send_file.go`) is a fully implemented, fully tested deferred tool. The agent event infrastructure (in `internal/agent/llm_agent_events.go:114–121`) lifts its `ToolResultMeta` artifact descriptor onto `Actions.ArtifactDelta`. The AG-UI translator (`internal/agui/translator.go:100–110`) maps that onto a `CUSTOM` event. The Telegram channel (`internal/channels/telegram/artifact.go`) handles that event and calls `sendDocument`. All three layers are unit-tested.

However, `buildBaseRegistry` in `cmd/aura/main.go` (lines 99–142) never calls `reg.Register(&tools.SendFile{})` — and neither does `buildRegistryWithMCP`. Grepping the entire repo for `SendFile` in non-test `.go` files returns only the tool definition and the spike in `.planning/`. The model is never shown `send_file` in its manifest and can never invoke it.

The entire artifact delivery pipeline is wired end-to-end except for the last inch: the tool registration.

**Suggested fix:**

Add `reg.Register(&tools.SendFile{})` to `buildBaseRegistry` in `cmd/aura/main.go` after the other deferred tools (e.g., after `reg.Register(&tools.SwarmSpawn{...})`). Because `SendFile` is `Deferred: true` and `Mutating: false`, it does not affect the `Validate()` non-deferred count and does not arm the completion-gate critic.

---

### [MEDIUM][DEAD-CODE] `ManifestEntry` is exported but has no production consumer outside the package

**Location:** `internal/agent/tools/manifest.go:13`

**Confidence:** medium

**Detail:**

`ManifestEntry` is an exported struct returned by `Registry.Render()`. Within the package itself, `Render()` is called by `RenderToolDefs()` and `RenderText()` — both of which consume only the individual fields. The only external callers of `Render()` in production code are:

- `internal/llm/client.go`: only a doc-comment reference, not a code call.
- `.planning/spikes/001-mail-mcp-live-mount/main.go:117–118`: a spike (not production).

No production binary accesses `ManifestEntry` fields directly. The type is effectively internal. It should either be unexported (`manifestEntry`) or a production caller should be added (e.g., a `aura tools` command that iterates entries to show which tools are deferred vs. active). The current state is not wrong by itself, but the exported surface is unused and untested from outside.

**Suggested fix:**

If no external consumer is planned, unexport to `manifestEntry` and update `Render()` to return `[]manifestEntry`. If an `aura tools` listing command or an HTTP debug handler is planned, wire one of those instead.

---

### [LOW][DEAD-CODE] Always-true branch guard in `FSWrite.Execute`

**Location:** `internal/agent/tools/fs_write.go:53`

**Confidence:** high

**Detail:**

```go
if dir := filepath.Dir(path); dir != "" {
    if err := os.MkdirAll(dir, 0o755); err != nil { ...
```

`filepath.Dir` never returns an empty string. For every possible input — empty string, `"."`, a bare filename, or an absolute path — it returns at minimum `"."`. The condition `dir != ""` is a vacuous true and the outer `if` can be removed. The `MkdirAll` call is always reached. This is dead code at the branch level (the false arm never executes), not at the function level.

**Suggested fix:**

```go
dir := filepath.Dir(path)
if err := os.MkdirAll(dir, 0o755); err != nil {
    return ToolResult{}, fmt.Errorf("fs_write: %w", err)
}
```

---

## Clean areas (what was checked and found sound)

- **Race safety**: `ShellExec.cwd` is guarded by `sync.Mutex` in both reader (`workdir`) and writer (`storeCwd`). `ToolSearch.buildIndex` uses `sync.Once` correctly — the happens-before guarantee of `Once.Do` makes post-build reads of `ts.deferred` and `ts.index` race-safe without an additional lock.
- **Context propagation**: All tools that call `NewResult` correctly require `WithToolCallContext` to be injected first; missing context is caught immediately with a clear error. Tools that bypass `NewResult` (e.g., `TextResponse`, `CurrentTime`, `task` actions, `AskUser`) do not need the context.
- **Path traversal (T-03-07)**: `validateID` in `result.go` rejects `..` and path separators before any `filepath.Join`. `sidecarPath` applies this to both session_id and tool_call_id. `read_tool_output` reuses the same path for the model-controlled tool_call_id paging argument.
- **Skills-library write fence**: `deniedSkillsWrite` guards both `FSWrite` and `FSEdit` against writes inside the skills directory, routing model write attempts through the gated `skill` authoring flow.
- **ToolSearch infinite-recursion guard**: `sourceOrientation` skips `*ToolSearch` before calling `Spec()` on registry tools, breaking what would otherwise be a mutual recursion (`Spec → sourceOrientation → Spec → …`).
- **BM25 determinism**: `appendSchema` sorts property keys before iterating; `buildIndex` sorts tools by name before building the corpus. Tie-breaking in `rank` is by ascending doc index. The result is byte-stable across runs.
- **Swarm empty-goals handling**: `swarm.Run` preflight catches `len(goals) == 0` and returns a corrective message; `SwarmSpawn.Execute` handles the over-cap case before delegating.
- **Task gating**: `actionSchedule` computes `scoring.ComputeTaskTier` and routes destructive payloads to `pending_approval` before `CreateScheduledTask`; the cron expression is validated by `cron.ParseSchedule` (gronx) before any persist.
- **Skill write flow**: `writeAction` validates the name, requires the writer seam, delegates to the writer (which runs the injection blocklist), then pauses via `ErrAwaitingUserInput`. There is no model-facing approve action.
- **Resource management**: `grepFile` wraps `f.Close()` in a deferred anonymous function. `os.MkdirAll` + `os.WriteFile` in `writeSidecar` degrade cleanly (D-29). No goroutine leaks (goleak gate in `TestMain`).
- **Integer/overflow safety**: `truncatePreview` backs off the cut point to a UTF-8 rune boundary using `utf8.RuneStart`. `read_tool_output` clamps `start` and `end` to `[0, total]` before slicing.
