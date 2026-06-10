# Audit: internal/swarm

**Verdict:** needs-work — one real bug (zero-duration timeout kills all workers silently); package is otherwise clean.
**Counts:** critical 0 / high 0 / medium 1 / low 0

## Findings

### [MEDIUM][BUG] Zero SwarmChildTimeoutSec creates immediately-expired child context

**Location:** `internal/swarm/swarm.go:109-118`
**Confidence:** high

**Detail:**
`runWave` computes `childTimeout := time.Duration(rc.Cfg.SwarmChildTimeoutSec) * time.Second` with no guard for zero. `context.WithTimeout(egCtx, 0)` produces a context that is already expired at creation (`ctx.Err() == context.DeadlineExceeded` immediately). Every child goroutine starts with a dead context, the stream open fails with `context.DeadlineExceeded`, and the D-11 normalization at line 185 turns every report into `{failed, "timeout"}`. The operator intent for `SwarmChildTimeoutSec=0` is almost certainly "no per-child timeout" (rely on the parent's context), but the code implements "instant kill all workers."

The default is 120 s (from `config.go:238`) and all callers in the repo set explicit positive values (swarm_demo.go: 30, tests: 30), so this is not triggered by normal operation. It becomes a silent footgun if an operator sets `AURA_SWARM_CHILD_TIMEOUT_SEC=0`.

**Suggested fix:**
```go
// runWave, after computing childTimeout:
var childCtx context.Context
var ccancel context.CancelFunc
if childTimeout > 0 {
    childCtx, ccancel = context.WithTimeout(egCtx, childTimeout)
} else {
    childCtx, ccancel = context.WithCancel(egCtx) // no per-child deadline
}
defer ccancel()
```
Or add a guard in `config.go`'s `Load` (or in `runWave`) to clamp `SwarmChildTimeoutSec <= 0` to the default.

---

## Clean (everything else checked)

The following areas were explicitly verified and found clean:

**Races:**
- `reports[idx]` written by goroutines: each goroutine writes a distinct index (`idx := i` capture before `eg.Go`), no aliasing. No data race.
- `routerClient` in tests uses a `sync.Mutex` correctly.
- No shared mutable state in production code paths (`ChildReport` fields are written once per goroutine, read after `eg.Wait()`).

**Resource leaks:**
- `context.WithTimeout`/`context.WithCancel` cancel functions are always deferred inside goroutines (`defer ccancel()` at line 119) and at the wave level (`defer cancel()` at line 107). No context leaks.
- `dumpTranscript` opens files with `defer f.Close()` (line 69). Write errors are swallowed with `slog.Warn` — intentional (best-effort transcript, D-18).
- `errgroup.Wait()` is always called (line 124), preventing goroutine leaks regardless of child outcome.

**Dead code:**
- All unexported functions (`preflight`, `runWave`, `runChild`, `optionLabels`, `structuredBrief`, `maxDepth`, `checkDepth`, `dumpTranscript`, `marshalReports`) are referenced within the package.
- All unexported constants are used: `budgetReserve` (line 90/93), `swarmSpawnTool` (line 139), `workerOverlay`/`brief*` (brief.go, asserted by test).

**Not-wired code:**
- `RunnerAdapter`/`NewRunnerAdapter` are wired at `cmd/aura/main.go:133` and in `internal/eval/harness_swarm_e2e_test.go:162`.
- `swarm.Run` is called by `RunnerAdapter.Run` (production path) and directly by `cmd/aura/swarm_demo.go:101` (demo path).
- `StatusOK`/`StatusFailed`/`StatusNeedsUserInput` are consumed by `cmd/aura/swarm_demo_test.go` and the swarm tests.
- `ChildReport` type and its JSON fields are consumed by callers that unmarshal the `Run` output.

**Logic:**
- D-11 timeout normalization (line 185) correctly preserves `StatusNeedsUserInput` as `{failed,"timeout"}` when a paused worker's deadline fires — the parent cannot proxy a stale pause from a timed-out worker; this is acceptable behavior even if not explicitly documented.
- The errgroup context shadowing (`egCtx, cancel := context.WithCancel(egCtx)`) is intentional defense: the `WithCancel` layer ensures `childCtx`s inherited by workers are cancelled when `runWave` returns even if the errgroup's own internal cancel races. The chain (errgroup-ctx → WithCancel-ctx → WithTimeout-ctx) is correct.
- `#61611` spawn-loop guard is valid: prevents goroutine bodies from executing when a parent cancellation fires between goroutine launch and body entry.
- `Budget.Child(width)` is called `width` times in the pre-goroutine loop before any step is consumed, so all `width` children see the same `Remaining()` and receive equal soft caps (IN-04 invariant satisfied by design for intra-wave siblings; inter-wave soft caps decrease as earlier waves drain the pool — this is intentional per the Budget.Child doc).
- `maxDepth()` reads `os.Getenv` per-call (once per `Run` invocation). Not a race or hotpath concern.
