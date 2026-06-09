# Audit: internal/cron/handlers

**Verdict:** needs-work — two not-wired fields + one swallowed-error diagnostic issue
**Counts:** critical 0 / high 0 / medium 1 / low 2

## Findings

### [MEDIUM][NOT-WIRED] `HandlerMeta.ReschedulesOnRecovery` is declared, set, and transferred — but never consumed

**Location:** `internal/cron/handlers/handler.go:44`, `internal/cron/handlers/agentjob.go:53`, `internal/cron/handlers/backup.go:68`, `internal/cron/handlers/reminder.go:28`, `internal/cron/handlers/skill_ttl.go:39`; consumption gap in `internal/cron/recover.go` (entire file) and `internal/cron/scheduler.go` (entire file)

**Confidence:** high

**Detail:** Every handler sets `ReschedulesOnRecovery` precisely per D-18 (true for agent_job/backup, false for reminder/skill_ttl_sweep). The field flows through `handlerAdapter.Meta()` in `cmd/aura/serve.go:275` into `cron.HandlerMeta`. However, neither `recover.go` (`catchUpMissed`, `recoverOrphans`) nor `scheduler.go` (`runMissed`, `tick`) reads this field. `catchUpMissed` (recover.go:55-82) fires every overdue task unconditionally regardless of the variant: a reminder whose fire was missed but `ReschedulesOnRecovery=false` is still dispatched by `runMissed` with a `MissedSince`. The field is dead specification — it documents intent but governs nothing at runtime. The tests in `agentjob_test.go:173`, `backup_test.go:142`, `reminder_test.go:49`, and `skill_ttl_test.go:39` assert the field value but none of those tests exercise the scheduling path that should branch on it.

**Suggested fix:** In `catchUpMissed` (recover.go), after resolving the handler via the dispatcher map, check `h.Meta().ReschedulesOnRecovery`: if false, skip adding to the `missed` slice (i.e. advance `next_run_at` but do not fire a catch-up run). Alternatively, if the PRD intent for a "no-reschedule" task is to still fire the catch-up once (contradicting the field name), document that intent and remove the field to prevent future confusion. The current state — the field is set everywhere and consumed nowhere — is a silent specification lie.

---

### [LOW][NOT-WIRED] `handlers.TaskKind` constants duplicate `cron.TaskKind` with no compile-time enforcement

**Location:** `internal/cron/handlers/handler.go:26-35` vs `internal/cron/store.go:28-39`

**Confidence:** high

**Detail:** Both packages define a `TaskKind` type (plain `string`) with five identical constants (`"reminder"`, `"agent_job"`, `"backup_postgres"`, `"backup_neo4j"`, `"skill_ttl_sweep"`). The duplication is intentional (D-24 import-cycle avoidance) and documented. The adapter in `cmd/aura/serve.go:273` uses an unchecked `cron.TaskKind(m.Kind)` cast to bridge them. There is no compile-time guarantee the two sets remain in sync: adding a new kind to `cron` but not to `handlers` (or vice versa) silently produces a handler map miss, which the dispatcher converts to a `"no handler for kind %q"` terminal run failure — visible only at runtime. Currently all five values match, so there is no live bug. The risk is future drift.

**Suggested fix:** Add a `TestTaskKindConstantsInSync` test in `internal/cron/handlers` that iterates both string tables and asserts equality. Alternatively, move the constants to a shared `internal/cron/kinds` sub-package importable by both without a cycle (neither `internal/cron` nor `internal/cron/handlers` would import each other — both import the leaf).

---

### [LOW][BUG] `agentJobGoal` swallows `json.Unmarshal` error, producing a misleading terminal error

**Location:** `internal/cron/handlers/agentjob.go:173-179`, called at `agentjob.go:63`

**Confidence:** high

**Detail:** `agentJobGoal` silently swallows any `json.Unmarshal` error and returns `""`. When `Run` receives an empty string it emits `fmt.Errorf("agent_job: payload has no goal")`. This error message is correct for a well-formed `{"goal":""}` but misleading for a corrupted or non-JSON payload — both produce the same opaque string. In practice this means a database corruption or a mis-serialized task payload (e.g. the scheduler stored something other than JSON in `payload`) is diagnosed as "no goal" with no indication of the unmarshal failure, making the audit trail harder to triage.

**Suggested fix:** Propagate the error:

```go
func agentJobGoal(payload []byte) (string, error) {
    var p agentJobPayload
    if err := json.Unmarshal(payload, &p); err != nil {
        return "", fmt.Errorf("unmarshal agent_job payload: %w", err)
    }
    return strings.TrimSpace(p.Goal), nil
}
```

Then in `Run`:
```go
goal, parseErr := agentJobGoal(job.Payload)
if parseErr != nil {
    return "", fmt.Errorf("agent_job: %w", parseErr)
}
if goal == "" {
    return "", fmt.Errorf("agent_job: payload has no goal")
}
```

## What was checked and found clean

- **Nil-pointer derefs:** `drain` guards `ev == nil` before accessing fields. `AwaitingInput`, `LLMResponse` are all pointer-guarded. `claim.release` is nil-safe.
- **Resource leaks:** No unclosed `os.ReadDir` entries, no goroutine leaks. `context.WithTimeout` cancels are always deferred. `exec.CommandContext` is bound to `runCtx`. `sweepRetention` is best-effort and does not leak resources.
- **Context propagation:** `agentjob.Run` creates `runCtx` from the parent `ctx` with a timeout; `drain` passes it to `InvocationContext.Ctx`; `backup.Run` does the same with `backupMaxDuration`. The parent `ctx` cancellation is honoured everywhere.
- **Races:** All auto-reject iterations are sequential (no goroutines inside `Run`). The shared `*agent.Budget` pointer is passed to sequential `drain` calls, never concurrently. No maps or shared state mutated concurrently.
- **Shared budget pointer:** Each `drain` call within `Run` shares the same `*Budget` — intentional design (the budget is global for the job). Sequential calls ensure no race.
- **`uuid.Must` panic risk:** `uuid.NewV7()` failure would panic, but this pattern is used uniformly across the codebase. Not flagged as an actionable local issue.
- **`sweepRetention` TOCTOU:** `e.Info()` after `ReadDir` can race with filesystem changes, but the function is documented best-effort and the sweep success/failure does not affect correctness.
- **Duplicate constant string values:** All five `handlers.TaskKind` strings are currently byte-identical to their `cron.TaskKind` counterparts — no live mismatch.
- **Dead code:** All exported and unexported symbols in the package are referenced. `childRegistry`, `newAgentWorker`, `appendLine`, `agentJobGoal`, `assistantAskUserTurn`, `askUserKind`, `newJobBudget`, `MissedBackupAlert`, `backupDir`, `sweepRetention`, `reminderText` are all called from production paths. The `dockerCLI` unexported field is accessible to tests in the same package. `SnippetSweeper` interface is satisfied by `snippetSweeperAdapter` in `cmd/aura/serve_adapters.go`.
