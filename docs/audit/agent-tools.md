# Audit: internal/agent/tools

**Verdict:** needs-work — two real defects (nil-dereference panic path + silent grep truncation); one validation gap.
**Counts:** critical 0 / high 1 / medium 1 / low 1

---

## Findings

### [HIGH][BUG] TaskTool panics on nil Store when any action is dispatched

**Location:** `internal/agent/tools/task.go:199, 285, 325, 337, 349`
**Confidence:** high

**Detail:**
`TaskTool.Store` is a `taskStore` interface field that can be legitimately nil. The production composition root (`cmd/aura/serve_adapters.go:38-43`) constructs `TaskTool` with `Store = nil` when no cron store is wired (the "pool-free manifest path" used for `aura tools` CLI and other contexts). In that state, any call to `Execute` — which routes through `actionRouter().Dispatch` — hits one of the five action handlers. Every handler calls `t.Store.<Method>(...)` without a nil guard:

- `actionSchedule` → `t.Store.CreateScheduledTask(...)` (line 199)
- `actionList` → `t.Store.ListScheduledTasks(...)` (line 285)
- `actionCancel` → `t.Store.CancelScheduledTask(...)` (line 325)
- `actionRunNow` → `t.Store.RunScheduledTaskNow(...)` (line 337)
- `actionApprove` → `t.Store.ApproveScheduledTask(...)` (line 349)

All five cause a nil-pointer-dereference panic at the interface dispatch.

Compare: `SandboxExec` (line 81-83) and `SwarmSpawn` (line 96-98) both nil-guard their runners and return a structured `"not configured"` result. `SkillTool` nil-guards `Writer` in every write action. `TaskTool` is the only tool that omits this pattern.

The risk is real in any context that registers the tool for its Spec (manifest) but does not wire the store — e.g., if the model is invoked on a pool-free agent that has the task tool in its registry for discovery but not for execution.

**Suggested fix:**
Add a nil-Store guard at the top of each action handler (or centralise it in a helper):

```go
func (t *TaskTool) requireStore(action string) error {
    if t.Store == nil {
        return fmt.Errorf("task %s: no store is configured in this context", action)
    }
    return nil
}
```

Call `requireStore` at the start of `actionSchedule`, `actionList`, `actionCancel`, `actionRunNow`, and `actionApprove`, returning the error as a `ToolResult{}` + error pair — matching the pattern used by `SkillTool.writeAction` for `t.Writer == nil`.

---

### [MEDIUM][BUG] grepFile silently truncates on oversized lines — scanner.Err() never checked

**Location:** `internal/agent/tools/fs_grep.go:118-127`
**Confidence:** high

**Detail:**
`grepFile` configures a `bufio.Scanner` with a custom max-token size of 1 MB (`scanner.Buffer(..., 1024*1024)`), then iterates with `scanner.Scan()`. After the loop exits it does **not** call `scanner.Err()`. When a line exceeds 1 MB, `scanner.Scan()` returns `false` and sets `scanner.Err()` to `bufio.ErrTooLong`. The caller (`Execute`) receives a partial match set with no indication that scanning was cut short: the walk continues to the next file and the tool returns `[no matches]` or a partial set as if the scan completed normally.

This produces silently wrong results for files with very long lines (minified JS, binary-ish data that passed the NUL heuristic, concatenated single-line JSON, etc.). The user and the model see an apparent miss with no error.

```go
// current — after the for loop, grepFile just returns:
}

// fix — add after the scanner loop:
if err := scanner.Err(); err != nil {
    // Append a sentinel line so callers know the file was partially scanned.
    *out = append(*out, fmt.Sprintf("%s: [scan error: %v — line too long?]", rel, err))
}
```

Alternatively, return the error up to `Execute` and surface it in the result. Appending a sentinel is the least-invasive fix that keeps grep non-fatal for large repos while making the truncation observable.

---

### [LOW][BUG] swarm_spawn.Execute does not validate len(goals) >= 1 before delegating to the runner

**Location:** `internal/agent/tools/swarm_spawn.go:91-99`
**Confidence:** high

**Detail:**
The JSON schema for `swarm_spawn` declares `"minItems": 1` on the `goals` array, but schema validation is advisory only (the model controls the payload). `Execute` guards against `len(goals) > MaxGoals` (line 91) but never checks `len(goals) == 0`. An empty-goals call bypasses the tool-level guard and reaches `e.Runner.Run(ctx, []string{})`.

The runner's `preflight` function (`internal/swarm/swarm.go:76-83`) does handle the empty case gracefully, returning an inline error string. So there is no panic and no data corruption. However:

1. The error message comes from the runner, not the tool, which is inconsistent with every other tool's validation pattern (validate at the boundary, return a tool-level error before calling the dependency).
2. If the runner is nil (e.g., in tests or unconfigured agents), `Execute` reaches `e.Runner == nil` check correctly — but if the runner were changed to not guard empty goals, this would become a real bug.
3. The tool's own over-cap error message is better formatted ("split into fewer parallel subtasks or answer directly") than the runner's self-correction hint; a direct goals-empty check here would produce a consistent UX.

**Suggested fix:**
```go
if len(a.Goals) == 0 {
    return NewResult(ctx, `error: goals is empty — pass at least two subtask briefs: {"goals":["<brief 1>","<brief 2>"]}; for a single task answer the user directly`)
}
```
Add this before the `MaxGoals` check (line 91).

---

## What was checked and found clean

- **Races**: `ShellExec.cwd` is guarded by `sync.Mutex` in both `workdir` and `storeCwd`. `ToolSearch.once`/`index`/`deferred` use `sync.Once` for lazy init — race-free by construction. `SkillTool.routerOnce` and `TaskTool.routerOnce` same pattern. No concurrent map writes found.
- **Nil-map read**: `workdir` reads a potentially nil `s.cwd` map under the mutex — Go reads from nil maps are safe (return zero value).
- **Resource leaks**: `grepFile` closes `f` with a deferred close. Walk callbacks release no other resources. `ShellExec.Execute` uses `cmd.Run()` (not `cmd.Start`), so the process is always waited; no goroutine leak. `context.WithTimeout` cancel is deferred immediately after creation.
- **Context propagation**: All tools that call `NewResult` pass the agent-injected context. `send_file.Execute` and `text_response.Execute` drop the context intentionally — their outputs are always tiny and never need spillover; `errorResult` returns a short JSON blob directly.
- **Path traversal (T-03-07)**: `validateID` in `result.go` blocks `..` and path separators before any `filepath.Join`; `deniedSkillsWrite` uses `withinDir` (filepath.Rel-based) for the skills-library fence.
- **BM25 division by zero**: `scoreDoc` guards `avgdl == 0` at line 148; denominator `f + bm25K1*(...)` cannot reach zero when `f >= 1`.
- **Dead code**: All exported and unexported symbols have at least one non-test caller in the repo. `resolvedSpec`, `renderTaskList`, `requireTaskID`, `payloadJSON`, `renderSnippetUse`, `shellQuotePath`, `rankSkills`, `grepFile`, `errorResult`, `asciiCaption`, `foldToASCII`, `sandboxUnavailable`, `fetchConvID`, `webErrorResult`, `buildIndex`, `sourceOrientation`, `toolSearchDescription`, `manifestDescription`, `Without`, `RenderText`, `Render`, `RenderToolDefs` — all verified as reached.
- **Not-wired**: All 9 `skill` actions wired in `actionRouter`. All 5 `task` actions wired. `SwarmSpawn`, `SandboxExec`, `SendFile`, `WebFetch`, `WebSearch`, `ShellExec`, `ReadToolOutput`, `CurrentTime`, `AskUser`, `TextResponse`, `ToolSearch`, `FSRead`, `FSWrite`, `FSEdit`, `FSGlob`, `FSGrep` — all reachable from `cmd/aura/main.go` registry wiring.
- **Schema/action mismatches**: `task` schema enum and router map are both `{schedule, list, cancel, run_now, approve}` — aligned. `skill` schema enum and router map are both `{list, info, use, create, update, delete, save_snippet, restore, archive}` — aligned.
- **Integer/overflow**: No unsafe integer conversions. `int64(maxSendFileBytes)` in `send_file.go:89` is safe (the constant is 50<<20 ≈ 52M, well within int64).
- **JSON correctness**: All `json.Marshal` calls on `map[string]string` / `map[string]any` with known-safe values; `_ = json.Marshal(...)` discards only in `sandboxExecOutput` / `sandboxUnavailableOutput` where the types are safe by construction.
