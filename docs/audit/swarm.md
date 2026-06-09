# Audit: internal/swarm

**Verdict:** needs-work — one silent-misconfiguration bug; one structural dead code

**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

---

### [MEDIUM][BUG] Zero child-timeout silently kills every worker

**Location:** `internal/swarm/swarm.go:109-118`
**Confidence:** high

**Detail:**

`runWave` computes the per-child context deadline as:

```go
childTimeout := time.Duration(rc.Cfg.SwarmChildTimeoutSec) * time.Second
// …
childCtx, ccancel := context.WithTimeout(egCtx, childTimeout)
```

`context.WithTimeout(parent, 0)` expands to `context.WithDeadline(parent, time.Now())` — the deadline is in the past at the moment of construction, so `childCtx` is already cancelled when `runChild` receives it. Every LLM call inside the worker immediately fails with `context.DeadlineExceeded`, which the post-loop normalization re-labels to `{failed, "timeout"}`. The result is that every goal silently fails as "timeout" with no error distinguishing the zero-timeout misconfiguration from a genuine overrun.

`config.envIntDefault("AURA_SWARM_CHILD_TIMEOUT_SEC", 120)` has no lower-bound guard (it passes through a parsed `0` without error), and `runWave` does not guard either. Setting `AURA_SWARM_CHILD_TIMEOUT_SEC=0` triggers the failure silently at runtime with no warning log or startup error.

**Suggested fix:**

Add a lower-bound guard in `runWave` before the loop (matching how `concurrent < 1` is clamped to 1 at `swarm.go:54`):

```go
childTimeout := time.Duration(rc.Cfg.SwarmChildTimeoutSec) * time.Second
if childTimeout <= 0 {
    childTimeout = 120 * time.Second // match the builtin default
}
```

Alternatively, add validation in `config.Load` rejecting `SwarmChildTimeoutSec <= 0` at startup (fail-fast, matching the `Budget.NewBudget` pattern).

---

### [LOW][DEAD-CODE] Redundant `context.WithCancel` wrapper in `runWave`

**Location:** `internal/swarm/swarm.go:105-107`
**Confidence:** high

**Detail:**

```go
eg, egCtx := errgroup.WithContext(ctx)
egCtx, cancel := context.WithCancel(egCtx)
defer cancel()
```

`errgroup.WithContext` already manages its own internal cancel: it fires when `eg.Wait()` returns (after all goroutines finish). Because every goroutine always returns `nil` (D-02: child failures are captured into the report slot, never propagated as errors), the errgroup's cancel fires exactly when `eg.Wait()` returns — the same point at which `defer cancel()` fires. The extra `context.WithCancel` wrapping adds a second cancel that is simultaneous with the errgroup's own cancel. It provides no additional protection and no observable difference in cancellation semantics.

This is pure structural dead code: the wrapped `egCtx` with the extra cancel is the same cancellation point, just with an extra goroutine resource reference. The comment block references the `#61611` spawn-loop guard (`if egCtx.Err() != nil { return nil }`) which is valid and necessary — but it would work identically without the extra `WithCancel` layer, since the errgroup's own `egCtx` carries the same cancellation signal.

**Suggested fix:**

Remove the extra `context.WithCancel` and its `defer cancel()`:

```go
eg, egCtx := errgroup.WithContext(ctx)
// errgroup manages egCtx cancellation; no extra wrapper needed
for i := start; i < end; i++ {
    ...
}
_ = eg.Wait()
```

---

## What was checked and found clean

- **Nil dereference:** `ev` is checked for `nil` at line 161 before dereference (`*ev` at line 164); `ev.Actions.AwaitingInput` is a pointer field, guarded at line 165. No nil-deref risk.
- **Concurrent writes to `reports`:** Each goroutine writes to a unique `reports[idx]` slot (non-overlapping index range `start..end-1`); no shared-slot race. Verified: `idx := i` is captured inside the loop body (Go 1.26 loop-var fix is in effect per `go.mod`).
- **Error swallowing:** `dumpTranscript` intentionally swallows write errors (D-18 best-effort transcript); errors are logged via `slog.Warn` before swallowing. The return type is `error` but callers use `_ =`, consistent with the best-effort contract.
- **Goroutine leaks:** `runWave` defers `cancel()` and calls `eg.Wait()` unconditionally, draining all spawned goroutines. The `block` test path (worker holds on `<-ctx.Done()`) is unblocked by `context.WithTimeout` on the per-child context. `goleak.VerifyTestMain` + per-test `goleak.VerifyNone` confirm leak-free behavior.
- **Context propagation:** `ic.Ctx = ctx` (the per-child deadline context) is threaded into `agent.InvocationContext` which propagates it into every LLM call and tool dispatch.
- **Budget sharing:** `rc.ParentBudget.Child(width)` shares the same `*atomic.Int32` step counter by pointer (D-10); no per-child fresh budget is allocated. Children compete on the shared counter, which is the intended design.
- **`maxDepth()` env reads:** Called once per `Run` invocation (not in a hot path). Consistent with `config.envIntDefault` approach in the rest of the codebase. Not worth caching.
- **Depth guard correctness:** `checkDepth(depth, max)` returns false when `depth >= max`; the production adapter always injects `Depth: 1` and the default `maxDepth()` is 2, so depth=1 always passes. Forward-compat code (workers cannot recurse because `swarm_spawn` is stripped from their registry via `tools.Without`).
- **JSON marshaling:** `marshalReports` uses `json.Marshal` over `[]ChildReport`; no custom marshaler, no field mismatches. `ChildReport` tags are verified by `TestChildReport`.
- **Double goals-cap check:** Both `SwarmSpawn.Execute` (in tools package) and `preflight` check `len(goals) > MaxGoals`. When called through the adapter the tools-layer check fires first; the preflight check is a defense-in-depth for direct `swarm.Run` callers (e.g., `swarm_demo`). Redundant but intentional.
- **Dead exported symbols:** `StatusFailed` and `StatusNeedsUserInput` are exported but not referenced outside the package. They are part of the `ChildReport.Status` public API contract (JSON wire format), so consumers parsing the JSON array can compare against them. Not flagged.
