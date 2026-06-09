# Audit: internal/cron/handlers

**Verdict:** needs-work — one not-wired bool return value, one cosmetically redundant double-timeout, otherwise clean.

**Counts:** critical 0 / high 0 / medium 1 / low 2

## Findings

---

### [MEDIUM][NOT-WIRED] `MissedBackupAlert` bool return value is never consumed by its only production caller

**Location:** `internal/cron/handlers/backup.go:79`, `backup.go:222-237`

**Confidence:** high

**Detail:**

`MissedBackupAlert` is exported and documented as returning `true` when an alert fires "so the dispatcher can also ride it through the Notifier (D-21)". Its docstring explicitly promises this use case. In the only production call site (line 79):

```go
MissedBackupAlert(h.Variant, job.MissedSince, time.Now().UTC())
```

the return value is discarded. The alert is delivered exclusively via `slog.Warn` — the Notifier path described in the function comment is never exercised. Callers outside this package (non-test) do not exist (verified by grep across `D:/Aura`). The exported function's advertised contract (Notifier riding) has no wiring to make it real.

This means a missed-backup-past-24h event produces a daemon log line but never reaches the user's Telegram route (D-19/D-21), contrary to the SC#3 requirement description.

**Suggested fix:**

Either (a) have `Run` incorporate the alert result into the returned summary/error so the dispatcher's existing Notifier call picks it up, or (b) have `BackupHandler.Run` call the Notifier directly (via a field on the struct), or (c) lower the visibility to unexported and remove the misleading bool (if log-only is the accepted design). The simplest correct fix that preserves the Notifier path:

```go
// inside Run, after MissedBackupAlert fires:
alerted := MissedBackupAlert(h.Variant, job.MissedSince, time.Now().UTC())
// ... rest of Run ...
summary := fmt.Sprintf("backup %s ok → %s ...", ...)
if alerted {
    summary = "[SC#3 MISSED BACKUP ALERT] " + summary
}
return summary, nil
```

This threads the alert through the Notifier via the normal dispatcher summary-notification path (dispatch.go line 158), without adding a Notifier dependency to the handlers package.

---

### [LOW][BUG] `BackupHandler.Run` creates a redundant inner `WithTimeout` that duplicates the dispatcher's outer budget

**Location:** `internal/cron/handlers/backup.go:97`

**Confidence:** medium

**Detail:**

`BackupHandler.Meta()` returns `MaxDuration: backupMaxDuration` (30 minutes). The cron dispatcher (`dispatch.go`) applies this as a hard deadline to the `ctx` it passes to `Run`. Inside `Run`, another `context.WithTimeout(ctx, backupMaxDuration)` is applied — with the same duration. Because both timers start almost simultaneously, the inner `runCtx` always expires at approximately the same wall time as the outer, making the inner one purely redundant. The only distinction is that the inner cancel is deferred, which is correct hygiene, but the second `backupMaxDuration` constant adds no safety margin.

This is not a functional bug (the behavior is correct), but it obscures intent: a reader might think the inner timeout is shorter or deliberately independent of the outer one.

**Suggested fix:**

Drop the inner `WithTimeout` and just use `ctx` directly in `exec.CommandContext`. The outer dispatcher-applied deadline already provides the bound. If a tighter inner bound is desired, document the intended relationship explicitly:

```go
// ctx already carries the Meta().MaxDuration deadline from the dispatcher;
// pass it directly to CommandContext.
cmd := exec.CommandContext(ctx, docker, args...)
```

---

### [LOW][DEAD-CODE] `MissedBackupAlert` bool return value is never read in production (dead return slot)

**Location:** `internal/cron/handlers/backup.go:227`

**Confidence:** high

**Detail:**

The `bool` return of `MissedBackupAlert` has zero production consumers (verified by grep: only `backup_test.go` reads it). The function is exported solely for the test and for a production wiring path (Notifier) that does not exist. This is a subset of the NOT-WIRED finding above, flagged separately because the dead return slot also signals that the exported surface is larger than needed.

If the Notifier wiring gap (medium finding above) is accepted as won't-fix, the function should be unexported (`missedBackupAlert`) to remove the false contract from the public API.

**Suggested fix:**

If the Notifier path remains unwired, make the function unexported:
```go
func missedBackupAlert(variant BackupVariant, missedSince, now time.Time) bool {
```
and update the single call site and the tests (which are in `package handlers`, so they can still call it).

---

## Clean sections (classes with no findings)

**BUGS (excluding the double-timeout):** No nil-pointer derefs, no unchecked errors that matter (all `os.Remove`/`os.Stat` errors are either surfaced or deliberately best-effort with a log), no swallowed errors in the happy path, no off-by-one. The `for attempt := 0; attempt <= maxAutoRejects; attempt++` loop correctly runs `maxAutoRejects + 1` iterations. `agentJobGoal` and `reminderText` swallow JSON parse errors intentionally and correctly (empty means "no content" in both cases — degraded but not silently wrong). The `backupDir` tilde expansion correctly handles `~`, `~/path`, and `~path` (the last being non-standard but deterministic).

**RACES:** No goroutine spawning in any handler. `AgentJobHandler.Run` is synchronous. `drain` runs the iterator synchronously; the `for-range` over `iter.Seq2` is single-threaded. `SkillTTLSweepHandler.now` is an unexported, read-once field with no concurrent access. `BackupHandler.dockerCLI` is set at construction and read-only thereafter. No shared mutable state.

**ITERATOR SAFETY (drain early return):** When `drain` returns early on detecting `ev.Actions.AwaitingInput != nil` (agentjob.go:124–126), this is safe: `LlmAgent.Run` calls `emitPauses` then immediately `return`s, so the pause event IS the last yield of that iterator invocation. The Go 1.23 range-over-func mechanism correctly handles the early return (yield returns false on the next call attempt, which never arrives). No goroutine leak; confirmed by the package-level `goleak.VerifyTestMain`.

**NOT-WIRED (handlers registration):** All five `TaskKind` constants (`KindReminder`, `KindAgentJob`, `KindBackupPostgres`, `KindBackupNeo4j`, `KindSkillTTLSweep`) and all five handler types are wired in `cmd/aura/serve.go:232–241`. The `handlerAdapter` at `cmd/aura/serve.go:258–287` correctly projects both `HandlerMeta` and `Job` fields. `SnippetSweeper` is satisfied by `snippetSweeperAdapter` in `cmd/aura/serve_adapters.go:330`.

**DEAD CODE:** `childRegistry`, `newAgentWorker`, `appendLine`, `agentJobGoal`, `assistantAskUserTurn`, `askUserKind`, `newJobBudget`, `drain`, `reminderText` — all called from production paths within the package. `backupDir`, `sweepRetention`, `resolveDocker`, `dumpArgv`, `dumpFilename`, `filePrefix`, `containerName`, `retention` — all called from `BackupHandler.Run`.
