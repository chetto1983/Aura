# Audit: internal/cron/handlers

**Verdict:** needs-work — two real defects and one not-wired return value found across 5 source files.

**Counts:** critical 0 / high 0 / medium 2 / low 1

## Findings

---

### [MEDIUM][BUG] `drain` captures intermediate event content into the audit summary

**Location:** `internal/cron/handlers/agentjob.go:117–131`

**Confidence:** high

**Detail:**

`drain` keeps only the most-recently-seen `ev.LLMResponse.Content != ""` as its returned `content`. The `LlmAgent` event stream sets `LLMResponse.Content` on multiple event kinds beyond the terminal `finalEvent`:

- `chunkEvent` — each streaming text delta (e.g., `"Hello"`, `" world"`) overwrites `content` with an incomplete fragment.
- `toolResultEvent` — carries the tool's result preview (e.g., `"ok"`, `"2+2=4"`) in `LLMResponse.Content`.
- `toolPreviewEvent` — carries internal debug strings (`"parse error"`, `"completion gate: not done"`) in `LLMResponse.Content`.

In the happy path (agent terminates normally), the last event is `finalEvent` which carries the full accumulated answer, so `content` ends up correct. The bug triggers only when the agent exits `drain` via an `AwaitingInput` (ask_user pause) without having emitted a `finalEvent` first. In that case the last `LLMResponse.Content != ""` event before the pause may be:

1. A `toolResultEvent` (the tool just ran before the model asked the user) — `content` = the tool's raw result preview, not an LLM answer.
2. A `chunkEvent` (the model started streaming text then switched to ask_user) — `content` = a partial text fragment.
3. A `toolPreviewEvent` with `"parse error"` (the model issued a malformed `text_response`, then issued `ask_user`) — `content` = `"parse error"`.

The caller at `agentjob.go:85–87` then unconditionally appends non-empty `content` to the audit summary. Sequences (1)–(3) corrupt the `agent_job_runs.summary` row with data that is not a model answer.

**Suggested fix:**

Filter `drain` to only capture content from events that represent the model's final answer. The `finalEvent` is distinguishable: it has both a non-empty `LLMResponse.Content` and a non-empty `LLMResponse.FinishReason` (set to `"stop"` or `"length"`), while `chunkEvent`, `toolResultEvent`, and `toolPreviewEvent` all have an empty `FinishReason`. Add a `FinishReason != ""` guard:

```go
if ev.LLMResponse != nil && ev.LLMResponse.Content != "" && ev.LLMResponse.FinishReason != "" {
    content = ev.LLMResponse.Content
}
```

This is zero-cost and isolates the final-answer content from intermediate streaming and tool-result events.

---

### [MEDIUM][NOT-WIRED] `MissedBackupAlert` return value is never consumed; the Notifier path it documents is unimplemented

**Location:** `internal/cron/handlers/backup.go:79` and `backup.go:222–237`

**Confidence:** high

**Detail:**

`MissedBackupAlert` is exported and documented as returning `true` "so the dispatcher can also ride it through the Notifier, D-21." The function is called exactly once in production code, at `backup.go:79`:

```go
MissedBackupAlert(h.Variant, job.MissedSince, time.Now().UTC())
```

The return value is unconditionally discarded. No caller outside the test suite reads the return value. The `internal/cron/claim.go:39` reference is a comment only. Grep across the entire repo (`D:/Aura`) confirms zero non-test, non-definition, non-comment uses of the boolean return.

The result is that a backup missed past the 24-hour SC#3 window emits a `slog.Warn` line, but the Notifier path (which would send the alert through the user's notification route, D-21) is never triggered — the handler silently discards the signal after logging.

**Suggested fix:**

Either (a) wire the return value into a Notifier call in `BackupHandler.Run` (send the SC#3 alert through the same `Notifier.Notify` path the dispatcher uses for normal outcomes), or (b) if the slog-only path is intentional, change the signature to `func MissedBackupAlert(variant BackupVariant, missedSince, now time.Time)` (no return value) to eliminate the documented-but-unused return and the misleading comment about the dispatcher.

---

### [LOW][BUG] `backupDir` tilde-expansion uses `os.PathSeparator` to strip the separator after `~`, breaking on Windows when the env var uses a forward-slash path

**Location:** `internal/cron/handlers/backup.go:190`

**Confidence:** medium

**Detail:**

```go
dir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), string(os.PathSeparator)))
```

On Windows, `os.PathSeparator = '\\'`. An env var value of `~/custom-backups` (forward-slash form, which is the documented default and what the test sets) produces:

1. `strings.TrimPrefix("~/custom-backups", "~")` → `"/custom-backups"` (leading forward slash)
2. `strings.TrimPrefix("/custom-backups", "\\")` → `"/custom-backups"` (no match — the separator is `\` not `/`)
3. `filepath.Join(home, "/custom-backups")` on Windows → `C:\custom-backups` (filepath.Join treats `/` as a rooted path, strips the drive)

The resulting path is wrong: it resolves to a root-relative path on the current drive instead of under the user's home. The unit test `TestBackupDirExpandsTildeAndEnv` does not detect this because `filepath.Join(home, "custom-backups")` on Windows matches the `filepath.Join(home, "/custom-backups")` only when `home` is on the same drive as the working directory (the test `t.Fatalf` checks string equality, not filesystem reachability).

In practice the backup daemon runs on Linux (the primary platform), so this is latent. It would surface if the operator ever runs the daemon on Windows with a `~`-prefixed `AURA_BACKUP_DIR`.

**Suggested fix:**

Strip any leading path separator (both `/` and `\\`) after removing `~`:

```go
rest := strings.TrimPrefix(dir, "~")
rest = strings.TrimLeft(rest, "/\\")
dir = filepath.Join(home, rest)
```

---

## What was checked and found clean

- **No goroutine leaks.** `backup.go` uses `exec.CommandContext` with `defer cancel()`; `agentjob.go` applies a `context.WithTimeout` + `defer cancel()` to the entire agent run. No background goroutines are spawned without joining.
- **No unchecked errors that matter.** `os.MkdirAll`, `exec.CommandContext`, `os.Stat`, `json.Marshal` in `assistantAskUserTurn` (the `_` discard is acceptable — the input is a compile-time-known map, never fails), and all DB-adjacent paths surface errors correctly.
- **No resource leaks.** `exec.Command.CombinedOutput()` closes the subprocess; no `io.ReadCloser` or `*sql.Rows` are opened.
- **No races.** All handlers are value receivers with no shared mutable state. The `budget` shared across `drain` iterations uses `sync/atomic` internally (verified in `internal/agent/budget.go`).
- **No dead code.** All five exported types (`HandlerMeta`, `Job`, `Handler`, `AgentDeps`, `SnippetSweeper`) and all exported consts, vars, and constructors are consumed by `cmd/aura/serve.go` or the test suite. Unexported helpers (`childRegistry`, `newAgentWorker`, `drain`, `newJobBudget`, `assistantAskUserTurn`, `askUserKind`, `agentJobGoal`, `appendLine`, `backupDir`, `sweepRetention`, `reminderText`) are all reachable from the package's `Run` paths.
- **No not-wired handlers.** Every `TaskKind` constant (`KindReminder`, `KindAgentJob`, `KindBackupPostgres`, `KindBackupNeo4j`, `KindSkillTTLSweep`) is registered in the dispatcher map at `cmd/aura/serve.go:232–241`.
- **No integer overflow risks.** `StepBudget int` is passed to `agent.NewBudget` which validates the `int32` range internally.
- **No SQL/JSON mishandling.** JSON payloads are unmarshalled into typed structs; errors are handled (silently degraded for `reminder`, error-returned for `agent_job`).
- **No context propagation gaps.** All long-running operations receive the `ctx` from `Run`, further narrowed by `context.WithTimeout`.
