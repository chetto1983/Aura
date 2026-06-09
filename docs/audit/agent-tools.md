# Audit: internal/agent/tools

**Verdict:** needs-work — two confirmed data races (shared mutable fields, no synchronization) and one Bytes-field inconsistency in send_file.

**Counts:** critical 0 / high 2 / medium 1 / low 1

---

## Findings

### [HIGH][RACE] TaskTool.router lazy-init is a data race under concurrent requests

**Location:** `internal/agent/tools/task.go:149-160`

**Confidence:** high

**Detail:**
`TaskTool.actionRouter()` checks `t.router == nil` and writes `t.router = NewActionRouter(...)` with no mutex or `sync.Once`. A single `*TaskTool` is constructed once in `buildBaseRegistry` (`cmd/aura/main.go:107`) and stored in the shared `*Registry`. The Telegram polling loop dispatches each inbound message on a goroutine that eventually calls `runner.Turn` → `LlmAgent.runTool` → `tool.Execute` → `t.actionRouter()` on the SAME `*TaskTool` instance concurrently. Two goroutines can simultaneously observe `t.router == nil`, both write `t.router`, and one of the new `*ActionRouter` values is immediately discarded — a classic unsynchronized read-modify-write on a pointer field. Under `-race` this will trip.

```go
func (t *TaskTool) actionRouter() *ActionRouter {
    if t.router == nil {          // <-- unsynchronized read
        t.router = NewActionRouter(...)  // <-- unsynchronized write
    }
    return t.router
}
```

**Suggested fix:** Replace the manual nil-check with `sync.Once`:
```go
type TaskTool struct {
    ...
    routerOnce sync.Once
    router     *ActionRouter
}

func (t *TaskTool) actionRouter() *ActionRouter {
    t.routerOnce.Do(func() {
        t.router = NewActionRouter(map[string]ActionFunc{...})
    })
    return t.router
}
```

---

### [HIGH][RACE] SkillTool.router lazy-init is a data race under concurrent requests

**Location:** `internal/agent/tools/skill.go:159-182`

**Confidence:** high

**Detail:**
Identical pattern to the `TaskTool` finding above. `SkillTool.actionRouter()` guards `t.router` with a bare nil-check and no synchronization. A single `*SkillTool` instance is shared across all concurrent Telegram-session turns (built once in `buildBaseRegistry`, stored in the shared registry). Multiple concurrent calls to `Execute` reach `actionRouter()` simultaneously and race on `t.router`.

```go
func (t *SkillTool) actionRouter() *ActionRouter {
    if t.router == nil {          // <-- unsynchronized read
        t.router = NewActionRouter(...)  // <-- unsynchronized write
    }
    return t.router
}
```

**Suggested fix:** Same `sync.Once` pattern as above. Note that `ToolSearch` already uses `sync.Once` for its index (`search.go:207`) — mirror that discipline here.

---

### [MEDIUM][BUG] send_file.Execute sets Bytes to filename length, not preview length

**Location:** `internal/agent/tools/send_file.go:99-101`

**Confidence:** high

**Detail:**
`ToolResult.Bytes` is documented as "full length of the original content in bytes" (`spec.go:56`). Every other non-spilling `ToolResult` construction in the package sets `Bytes` to `len(Preview)` (see `current_time.go:49,57`, `task.go:225,288,327,339,351`, `text_response.go:45`). In `send_file.Execute`, `Preview` is the string `"queued <filename> for delivery"` but `Bytes` is set to `len(filepath.Base(path))` — just the filename length, which is shorter than the preview string:

```go
return ToolResult{
    Preview: fmt.Sprintf("queued %s for delivery", filepath.Base(path)),
    Bytes:   len(filepath.Base(path)),   // wrong: should be len(Preview)
    Meta:    &meta,
}, nil
```

For a file named `results.xlsx` (11 chars), `Preview` is `"queued results.xlsx for delivery"` (32 chars) but `Bytes` reports 11. Consumers that use `Bytes` for audit/metrics (e.g., `ToolInvocationStore`) will undercount the preview size.

**Suggested fix:**
```go
preview := fmt.Sprintf("queued %s for delivery", filepath.Base(path))
return ToolResult{
    Preview: preview,
    Bytes:   len(preview),
    Meta:    &meta,
}, nil
```

---

### [LOW][BUG] swarm_spawn.Execute does not validate an empty goals slice before delegating to the runner

**Location:** `internal/agent/tools/swarm_spawn.go:91-99`

**Confidence:** medium

**Detail:**
The `MaxGoals` cap check (`if e.MaxGoals > 0 && len(a.Goals) > e.MaxGoals`) is skipped when `MaxGoals == 0`. If the model sends `{"goals":[]}` (an empty slice — technically invalid per the schema's `minItems:1`, but not JSON-invalid), the cap check is bypassed and `e.Runner.Run(ctx, []string{})` is called. The swarm runner's `preflight` does handle `len(goals) == 0` gracefully and returns a model-readable error, so this is NOT a crash path. However, the `swarm_spawn` tool itself currently provides no early feedback before hitting the engine and its depth+budget checks, which runs counter to its stated design of "domain rejections ride in the NewResult string so the model self-corrects" (the over-cap path does this inline, the empty path does not).

The real cap-check comment says `if e.MaxGoals > 0` — which means MaxGoals=0 is a "no cap" sentinel, but it also silently skips the entire guard for the empty-goals case.

**Suggested fix:** Add a dedicated empty-goals guard before the cap check in `Execute`:
```go
if len(a.Goals) == 0 {
    return NewResult(ctx, `error: no goals provided — pass subtasks as {"goals":["<brief 1>","<brief 2>"]}; for a single task answer directly`)
}
```
This matches the behavior of the runner's `preflight` but fires earlier, before a swarm engine call.

---

## What was checked and found clean

- **Resource leaks**: all `os.File` handles in `fs_grep.go:grepFile` are guarded by `defer f.Close()` covering all return paths including the early binary-skip return.
- **Context propagation**: every `Execute` method receives and threads `ctx` through its subprocess calls (`exec.CommandContext`, `s.Runner.Run`, `e.Engine.Fetch`, `e.Engine.Search`). The `ShellExec` creates a child context with timeout and defers `cancel()`.
- **ShellExec.cwd map concurrency**: `workdir` and `storeCwd` both acquire `s.mu` before reading/writing `s.cwd`. The nil-map read in `workdir` (when `s.cwd` is nil) is safe in Go (returns zero value).
- **ToolSearch.buildIndex concurrency**: correctly uses `sync.Once` (`search.go:207`) and has a dedicated concurrent test (`search_test.go:321-338`).
- **validateID path traversal**: correctly checks for `..`, `/`, `\`, and `os.IsPathSeparator` before `filepath.Join`.
- **truncatePreview UTF-8 safety**: backs off to a rune boundary before cutting, correct.
- **ActionRouter dispatch**: unknown actions return a structured error naming valid actions; no panic path.
- **Registry.Validate fail-closed**: correctly excludes `tool_search` itself from the non-deferred count.
- **Registry.Render sort stability**: alphabetical sort by Name on every call; cache-stable.
- **sourceOrientation recursion guard**: type-asserts away `*ToolSearch` before calling `Spec()` to prevent infinite recursion; correct.
- **NewResult sidecar write degrade**: write failures return a clean preview with a note instead of propagating an error (D-29 by design).
- **swarm_spawn empty goals**: handled by the runner's `preflight` returning a model-readable error; no crash.
- **payloadJSON nil handling**: returns `[]byte("{}")` for empty payloads; correct.
- **skill write gating**: `writeAction` always returns `*ErrAwaitingUserInput` on a successful pending write; no self-activation path exists in the tools package.
- **send_file size gate**: checks `info.Size() > maxSendFileBytes` before emitting the descriptor; correct.
- **fs_write/fs_edit skills fence**: `deniedSkillsWrite` uses `filepath.Rel` to detect escapes; correct.
- **errors.AsType usage**: `shell_exec.go:318` uses `errors.AsType[*exec.ExitError](err)` which is a Go 1.26 stdlib addition; the module is on Go 1.26.4 — compatible.
