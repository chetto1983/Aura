# Audit: internal/swarm

**Verdict:** needs-work — two low-to-medium defects; no critical or race issues found.
**Counts:** critical 0 / high 0 / medium 1 / low 1

## Scope checked

Files audited (all non-test .go files in `internal/swarm`):
- `swarm.go` — `Run`, `preflight`, `runWave`, `runChild`, `optionLabels`
- `swarm_depth.go` — `maxDepth`, `checkDepth`
- `brief.go` — `structuredBrief`
- `report.go` — `ChildReport`, `dumpTranscript`, `marshalReports`
- `runner_adapter.go` — `RunnerAdapter`, `NewRunnerAdapter`, `Run`

Test files read for behavior reference: `swarm_test.go`, `report_test.go`, `brief_registry_test.go`, `swarm_property_test.go`, `runner_adapter_test.go`, `main_test.go`.

Cross-repo grep for: all exported symbols, `ConvID` path use, `SwarmChildTimeoutSec`, `maxDepth`, wiring in `cmd/aura/main.go` and `internal/eval`.

Checked clean: `go vet ./internal/swarm/` (no output), `go test -race ./internal/swarm/` (ok, 3.8 s, goleak clean).

---

## Findings

### [MEDIUM][BUG] swarm-1 — No floor guard on `SwarmChildTimeoutSec = 0` makes all children immediately time out

**Location:** `internal/swarm/swarm.go:109–118`
**Confidence:** high

**Detail:**
`runWave` computes `childTimeout := time.Duration(rc.Cfg.SwarmChildTimeoutSec) * time.Second`. When `SwarmChildTimeoutSec` is 0 (zero value of `config.Config`, reachable by any caller that constructs `RunConfig` without going through `config.Load`), `childTimeout` is 0. `context.WithTimeout(egCtx, 0)` returns an already-expired context. Every child then sees `ctx.Err() == context.DeadlineExceeded` before executing a single statement inside `runChild`, and the D-11 normalization at line 185 converts all reports to `{status:"failed", error:"timeout"}`. `Run` returns a valid JSON array with all children failed — silently, with no preflight rejection.

The analogous knob `MaxSwarmConcurrent` already has a defensive floor (`if concurrent < 1 { concurrent = 1 }` at line 54). `SwarmChildTimeoutSec` has no equivalent, creating an inconsistency.

Production path: `config.Load()` defaults `SwarmChildTimeoutSec` to 120, so the default binary is safe. The gap is in direct `RunConfig` construction (tests, `cmd/aura/swarm_demo.go`, `internal/eval/harness_swarm_e2e_test.go`). Tests set the value explicitly (30 s), so no test currently covers the zero case — it's an untested silent footgun.

**Suggested fix:**
```go
// in runWave, after computing childTimeout:
if childTimeout <= 0 {
    childTimeout = 120 * time.Second // match AURA_SWARM_CHILD_TIMEOUT_SEC default
}
```
Or enforce via `preflight` with a model-readable error when `rc.Cfg.SwarmChildTimeoutSec <= 0`.

---

### [LOW][BUG] swarm-2 — D-11 timeout normalization silently clobbers `StatusNeedsUserInput` with no comment or test

**Location:** `internal/swarm/swarm.go:185–188`
**Confidence:** medium

**Detail:**
The D-11 normalization condition is:
```go
if ctx.Err() == context.DeadlineExceeded && (report.Status != StatusOK || report.Summary == "") {
    report.Status, report.Error = StatusFailed, "timeout"
    report.Summary = ""
}
```

The condition exempts only `StatusOK` with a non-empty Summary (the WR-01 success-guard). It does NOT exempt `StatusNeedsUserInput`. A child that correctly emitted a `needs_user_input` Event (e.g., a worker that posed an `ask_user` question) and whose `childCtx` expired in the race window *after* the stream drained will have its pause report overwritten to `{status:"failed", error:"timeout"}`. The parent then receives no question to proxy (D-05 relay is broken).

The code comment at lines 177–188 only discusses the `StatusOK` exemption case; the `StatusNeedsUserInput` downgrade is unmentioned. No test covers this path.

Per PRD D-11 ("a timeout produces `{status:failed, error:timeout}`"), this *may* be intentional — a timed-out pause is arguably undeliverable. However, the comment's silence and the missing test make the intent unclear, and the behavior can surprise callers who check for `needs_user_input` in the parent relay logic.

**Suggested fix:**
If the intent is intentional, add a comment:
```go
// StatusNeedsUserInput is intentionally NOT exempted: a pause surfaced after the
// child timeout has elapsed cannot be reliably relayed (D-11 / D-05 ordering).
```
If needs_user_input should be preserved, change the condition to:
```go
if ctx.Err() == context.DeadlineExceeded &&
    (report.Status != StatusOK || report.Summary == "") &&
    report.Status != StatusNeedsUserInput {
```
And add a test: a worker that pauses AND has an already-expired context must keep `StatusNeedsUserInput`.

---

## What was checked and found clean

- **Races:** `reports[idx]` writes are safe — distinct per-goroutine indices, waves run sequentially. `routerClient` in tests uses a mutex. `go test -race` is clean.
- **Goroutine leaks:** `egCtx` context tree (`ctx → egCtx1 (errgroup) → egCtx2 (WithCancel)`) is properly cancelled by `defer cancel()` + `eg.Wait()`. `goleak.VerifyTestMain` is installed.
- **Context propagation:** `childCtx` derived from `egCtx` propagates cancellation end-to-end through `runChild → worker.Run`. `preflight` is called from the main goroutine before any goroutines spawn — no concurrent env read race on `maxDepth()`.
- **Error handling:** `dumpTranscript` is explicitly best-effort (errors logged and swallowed by design per D-18). `marshalReports` error is the only real Go error return from `Run` — domain rejections ride in the string (D-15). This is correct.
- **Dead code:** All unexported symbols (`preflight`, `runWave`, `runChild`, `optionLabels`, `marshalReports`, `dumpTranscript`, `structuredBrief`, `maxDepth`, `checkDepth`) are referenced within the package. All exported symbols (`Run`, `RunConfig`, `ChildReport`, `Status*`, `RunnerAdapter`, `NewRunnerAdapter`) are used in `cmd/aura/main.go`, `cmd/aura/swarm_demo.go`, and `internal/eval`.
- **Not-wired code:** `RunnerAdapter` is wired in `cmd/aura/main.go:136` (`reg.Register(&tools.SwarmSpawn{Runner: swarm.NewRunnerAdapter(*cfg), ...})`). No orphaned handler, route, or flag detected.
- **Resource leaks:** `os.OpenFile` in `dumpTranscript` is closed via `defer f.Close()`. No unclosed `rows`, `Body`, or ticker.
- **Integer overflow:** `time.Duration(rc.Cfg.SwarmChildTimeoutSec) * time.Second` — `SwarmChildTimeoutSec` is `int`; on 64-bit this cannot overflow `time.Duration` (int64) for realistic values.
- **`idx := i` redundancy:** In Go 1.26 (per `go.mod`), loop variables are per-iteration; `idx := i` at `swarm.go:112` is a no-op safety copy from pre-1.22 practice. Not a bug — cosmetic noise mirroring `parallel.go`'s identical pattern.
