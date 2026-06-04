---
phase: 09-swarm-minimal
reviewed: 2026-06-04T00:00:00Z
depth: standard
files_reviewed: 43
files_reviewed_list:
  - cmd/aura/main.go
  - cmd/aura/main_test.go
  - cmd/aura/mcp.go
  - cmd/aura/swarm_demo.go
  - cmd/aura/swarm_demo_test.go
  - internal/agent/event.go
  - internal/agent/llm_agent.go
  - internal/agent/llm_agent_pause.go
  - internal/agent/llm_agent_pause_internal_test.go
  - internal/agent/mcptools/bridge.go
  - internal/agent/mcptools/bridge_test.go
  - internal/agent/mcptools/mount.go
  - internal/agent/mcptools/mount_test.go
  - internal/agent/swarm_context.go
  - internal/agent/tools/ask_user.go
  - internal/agent/tools/ask_user_test.go
  - internal/agent/tools/swarm_spawn.go
  - internal/agent/tools/swarm_spawn_test.go
  - internal/askuser/store.go
  - internal/askuser/store_test.go
  - internal/config/config.go
  - internal/config/config_test.go
  - internal/eval/dataset_cot_eval.go
  - internal/eval/harness_cot_eval_test.go
  - internal/eval/harness_swarm_e2e_test.go
  - internal/eval/judge_cot_eval.go
  - internal/eval/scoring_cot_eval.go
  - internal/runner/fakes_test.go
  - internal/runner/runner_persist.go
  - internal/runner/runner_persist_test.go
  - internal/swarm/brief.go
  - internal/swarm/brief_registry_test.go
  - internal/swarm/main_test.go
  - internal/swarm/registry.go
  - internal/swarm/report.go
  - internal/swarm/report_test.go
  - internal/swarm/runner_adapter.go
  - internal/swarm/runner_adapter_test.go
  - internal/swarm/swarm.go
  - internal/swarm/swarm_depth.go
  - internal/swarm/swarm_property_test.go
  - internal/swarm/swarm_test.go
  - docs/aura-quality-snapshot.md
findings:
  critical: 2
  warning: 6
  info: 4
  total: 12
status: issues_found
---

# Phase 9: Code Review Report

**Reviewed:** 2026-06-04
**Depth:** standard
**Files Reviewed:** 43
**Status:** issues_found

## Summary

Reviewed the Phase 9 "Swarm (Minimal)" surface: the greenfield `internal/swarm`
coordinator, the cycle-free `swarm_spawn` deferred-tool seam, the MCP
bridge/allowlist hardening, the `proxied_*` 3-layer pause plumb, and the
operator-gated cot_eval E2E harness.

The concurrency core is sound: budget accounting uses a shared `*atomic.Int32`
with TOCTOU-safe decrement-then-restore, the wave fan-out copies the
errgroup leak-safety invariants and correctly diverges to per-child failure
isolation (a child error returns `nil` so siblings are never cancelled),
transcript dumps are path-controlled (flat `w<i>` ids), and the `Registry`
shared across worker goroutines is read-only during a run (no data race). goleak
guards every concurrency test. Good work there.

Two BLOCKERs were found, both in the `proxied_*` HITL plumb that is the headline
deliverable of this phase:

1. The `proxied_from_child_id` contract is internally contradictory: the DB
   column and `parseUUID` demand a UUID, but the swarm report carries flat
   `w1..wN` child ids — a model that follows the documented tool description and
   relays a real child id will make the entire pause-persistence transaction
   fail, breaking exactly the D-05 swarm-proxy feature this phase ships.
2. `detectPause` re-executes the `ask_user` tool with `context.Background()`,
   discarding cancellation/deadline — a worker (or the parent) that is being torn
   down on timeout still runs tool side-effects during pause detection.

The remaining findings are robustness/quality issues (a timeout-clobber race in
`runChild`, an over-counting eval heuristic, several test-quality gaps where the
happy-path-only fixtures mask the BLOCKER above).

## Critical Issues

### CR-01: `proxied_from_child_id` UUID contract is unsatisfiable from the swarm report — relaying a real child id fails the whole pause write

**File:** `internal/askuser/store.go:115-121`, `internal/agent/tools/ask_user.go:105`, `internal/swarm/report.go:32-41`, `internal/db/migrations/0003_paused_states.up.sql:17`

**Issue:** The three layers of the D-05 proxied plumb disagree on the type of a
child id.

- `ChildReport.ChildID` (report.go:35) is the deterministic flat worker id
  `"w1".."wN"` (D-15/D-16, confirmed in 09-CONTEXT.md). There is **no UUID
  anywhere** in the report a model could copy.
- `aura.paused_states.proxied_from_child_id` is column type `uuid` (migration
  0003, line 17).
- The `ask_user` tool description (ask_user.go:105) instructs the model: *"Fill
  ONLY when relaying a child agent's needs_user_input report: the originating
  child's id (a ground-truth uuid from the swarm report)."*
- `askuser.Store.Insert` (store.go:116-120) calls `parseUUID("proxied_from_child_id", *p.ProxiedFromChildID)`
  which returns an error on any non-UUID string.

So when the model does exactly what the tool tells it — relay the child id from
the report, which is `"w1"` — `parseUUID` fails, `Insert` returns an error,
`runner_persist.go:persistPause` propagates it, and the **pause is never
persisted**. The HITL resume flow this phase ships (D-04/D-05 child→parent proxy)
is dead on the documented happy path. The unit test masks it (see WR-05).

**Fix:** Pick one contract end-to-end. The cleanest, given D-16 fixes child ids
as `w1..wN`, is to make the column and the boundary text-typed:

```sql
-- migration: proxied_from_child_id is a flat worker id (w1..wN), not a uuid
ALTER TABLE aura.paused_states ALTER COLUMN proxied_from_child_id TYPE text;
```
```go
// store.go — store the relay child id verbatim, like proxied_tool_call_id
ProxiedFromChildID: pgtype.Text{String: deref(p.ProxiedFromChildID), Valid: p.ProxiedFromChildID != nil},
```
and correct the ask_user description to say "the child's id (e.g. \"w2\") from
the swarm report", removing the false "uuid" claim. Add a test that feeds the
actual `"w1"` value through `persistPause` → `Insert` and asserts success.

---

### CR-02: `detectPause` runs the `ask_user` tool with `context.Background()`, ignoring cancellation and the per-child / wallclock deadline

**File:** `internal/agent/llm_agent_pause.go:76`

**Issue:** Pause detection pre-executes the candidate tool to harvest the
sentinel:

```go
_, err := tool.Execute(context.Background(), json.RawMessage(call.Function.Arguments))
```

Using `context.Background()` severs the call from the live invocation context.
For the stock `ask_user` this is benign (it does no I/O), but `pauseCalls`
filters only by **tool name** (`call.Function.Name != askUserToolName`), and the
name is resolved from `tools.AskUser{}.Spec().Name`. Two problems follow:

1. The agent's own deadline/cancellation (`ic.Ctx`, the per-child `WithTimeout`
   in swarm, the budget wallclock `WithDeadline`) does not propagate into this
   pre-execution. If a future `ask_user` variant (or a registry where the
   `ask_user` name is bound to a heavier tool) performs work, that work runs even
   while the worker is being torn down on timeout — defeating the D-11 timeout
   and the D-13 wallclock cancellation guarantees this phase relies on.
2. It is inconsistent with `runTool` (llm_agent.go:296), which correctly threads
   `ic.Ctx` plus the tool-call and swarm contexts. The pre-exec path bypasses
   both `WithToolCallContext` and `WithSwarmContext`.

**Fix:** Thread the invocation context into detection. Propagate `ic.Ctx`
(carried through `pauseCalls`) instead of `context.Background()`:

```go
func (a *LlmAgent) detectPause(ctx context.Context, call llm.ToolCall) (*tools.ErrAwaitingUserInput, bool) {
    tool, ok := a.registry.Get(call.Function.Name)
    if !ok {
        return nil, false
    }
    _, err := tool.Execute(ctx, json.RawMessage(call.Function.Arguments))
    var pause *tools.ErrAwaitingUserInput
    if errors.As(err, &pause) {
        return pause, true
    }
    return nil, false
}
```
and pass `ic.Ctx` down from `Run` → `pauseCalls` → `detectPause`.

## Warnings

### WR-01: `runChild` timeout-normalization can clobber a legitimately-completed worker report

**File:** `internal/swarm/swarm.go:174-177`

**Issue:** After the event-drain loop, `runChild` unconditionally rewrites any
report to `{failed, "timeout"}` whenever the child ctx deadline has tripped:

```go
if ctx.Err() == context.DeadlineExceeded {
    report.Status, report.Error = StatusFailed, "timeout"
    report.Summary = ""
}
```

`ctx` here is the per-child `WithTimeout` context. If a worker streams its final
`ok` answer and *then* the deadline elapses before this check runs (the worker
finished at the wire, but the goroutine was descheduled past the deadline), a
valid `StatusOK` + summary is silently discarded and reported as a timeout
failure. The drain loop already sets `StatusFailed` when the stream surfaces the
cancellation as an error, so the only thing this block adds is the clobber of a
race-completed success.

**Fix:** Only normalize to timeout when the worker did **not** already produce a
terminal result:

```go
if ctx.Err() == context.DeadlineExceeded && report.Status == StatusOK && report.Summary == "" {
    report.Status, report.Error = StatusFailed, "timeout"
}
```
i.e. treat a populated `Summary` (or a non-OK status the loop already set) as
authoritative over a post-hoc deadline observation.

### WR-02: `countSwarmWorkers` over-counts when a child summary contains the literal `"goal_index"`

**File:** `internal/eval/scoring_cot_eval.go:140-149`

**Issue:** The hard-floor worker count is derived by substring-counting
`"goal_index"` in the raw tool-result text:

```go
n := strings.Count(tr, `"goal_index"`)
```

This is structural-data-via-string-scan over model-and-tool-controlled content (a
worker's `summary` is free text that can quote the phrase `"goal_index"`, and an
MCP tool result threaded through the same `toolResults` slice can too). A worker
whose summary echoes the JSON key inflates the count, letting the D-22 `≥2
workers` HARD FLOOR pass on a single real worker. MEMORY explicitly warns
"niente regex/substring su linguaggio naturale — use structured ground truth".

**Fix:** Parse the swarm tool result as JSON and count array elements instead of
scanning text:

```go
var reports []struct{ GoalIndex int `json:"goal_index"` }
if json.Unmarshal([]byte(tr), &reports) == nil && len(reports) > max {
    max = len(reports)
}
```
This is the deterministic ground truth the rest of the harness claims to assert.

### WR-03: `swarm-demo` writes per-child transcripts into a shared `os.TempDir()` under a fixed conv id

**File:** `cmd/aura/swarm_demo.go:83`, `cmd/aura/swarm_demo.go:88`

**Issue:** The demo wires `RunDir: os.TempDir()` with `ConvID: "swarm-demo"`, so
the engine's `dumpTranscript` writes to `<tmp>/swarm-demo/swarm/w<i>.jsonl` — a
predictable, world-shared path (`/tmp` on multi-user hosts). Two concurrent
`aura swarm-demo` invocations (or a hostile co-tenant pre-creating
`<tmp>/swarm-demo` as a non-writable file) collide on a fixed path. The transcript
write is best-effort so it won't crash, but it pollutes a shared location and the
files persist (no TTL sweep in the demo). The real run dir (`defaultRunDir`,
`UserCacheDir/aura`) is per-user; the demo bypasses it.

**Fix:** Use a per-invocation temp dir:

```go
runDir, err := os.MkdirTemp("", "aura-swarm-demo-")
if err != nil { return fmt.Errorf("swarm-demo run dir: %w", err) }
defer os.RemoveAll(runDir)
// ... Cfg.RunDir: runDir
```

### WR-04: `Bridge` allowlist silently drops requested tools the server never advertised — a typo in the allowlist becomes a silent capability gap

**File:** `internal/agent/mcptools/bridge.go:84-97`

**Issue:** The per-server allowlist is intersected with the advertised tools by
skipping any advertised tool not in `allow` (the correct direction), but there is
**no reverse check**: an allowlist entry that matches no advertised tool is
silently ignored. So `mcpAllowlist("mail")` returning `["send_email",
"fetch_emails", "search_emails", "get_thread"]` against a server that renamed
`search_emails` → `searchEmails` mounts 3 tools, not 4, with no warning. Given the
D-20 allowlist is a **security control** (scoping destructive footguns out), the
inverse failure mode — an intended *capability* silently missing — degrades the
agent with no signal, and an operator has no way to notice short of diffing the
mounted-names list by hand.

**Fix:** After the bridge loop, log (or return alongside the names) any allowlist
entry that matched nothing:

```go
for _, name := range allow {
    if _, mounted := seenAllowed[name]; !mounted {
        slog.Warn("mcp allowlist entry matched no advertised tool",
            "namespace", namespace, "tool", name)
    }
}
```
This is WARN-not-fatal to preserve fail-soft boot, but it surfaces the gap.

### WR-05: `TestPersistPause_ForwardsProxiedIDs` uses a synthetic UUID, hiding the CR-01 real-world `w<i>` failure

**File:** `internal/runner/runner_persist_test.go:25`

**Issue:** The only test exercising the proxied-id plumb feeds
`ProxiedFromChildID: "11111111-1111-1111-1111-111111111111"` — a valid UUID that
sails through `parseUUID`. The actual value the swarm produces and the model is
told to relay is `"w1"`, which the same path rejects (CR-01). The test is
green-by-construction on a value the system never generates, providing false
confidence that the D-05 plumb works end-to-end. This is the "asilo nido" pattern
CLAUDE.md forbids: the fixture is chosen to pass rather than to model reality.

**Fix:** After fixing CR-01, change the fixture to the real child-id shape and
assert the row persists:

```go
ai := &agent.AwaitingInput{..., ProxiedFromChildID: "w2", ProxiedToolCallID: "call-x"}
// expect persistPause to SUCCEED and forward "w2" verbatim
```
Add a negative test only if the contract intentionally keeps UUID typing (then
CR-01's fix is the opposite direction and the swarm must emit UUID child ids).

### WR-06: `mcp add` rejects `--env`/`--disabled` flags placed after a 2-token-minimum but treats `--` greedily, allowing an empty command vector past `len(args) < 3`

**File:** `cmd/aura/mcp.go:147-188`

**Issue:** `mcpAdd` guards `len(args) < 3`, but a call like
`aura mcp add foo --disabled --` satisfies `len(args)==3` yet yields
`commandParts == []` (everything before `--` is a flag, nothing after). The
guard at line 186 (`len(commandParts) == 0`) does catch this and returns the
usage error, so it is not exploitable — but the `len(args) < 3` pre-check is
misleading dead validation: it implies "name + -- + command" minimum while the
real invariant is "a non-empty command after --", enforced 38 lines later. A
reviewer/maintainer reading the early guard will mis-model the contract.

**Fix:** Drop the brittle arity pre-check and rely on the precise post-parse
checks (empty name, empty command), or tighten the message so the two guards
don't contradict. Minor, but it is the kind of off-by-reasoning that grows bugs.

## Info

### IN-01: `swarmReportPath` is a hardcoded relative path that escapes the package dir

**File:** `internal/eval/harness_swarm_e2e_test.go:76`

**Issue:** `const swarmReportPath = "../../docs/aura-swarm-eval-2026-06-04.md"` is
written relative to the test's CWD. Under `go test ./internal/eval/` the CWD is
the package dir so it resolves, but any invocation from a different CWD writes to
the wrong place or fails the `os.WriteFile`. The date is also baked into the
constant, so re-runs overwrite the same file regardless of run date. Low impact
(operator-gated tier) but fragile.

**Fix:** Derive the docs dir from `runtime.Caller` or accept it via env, and use
the run timestamp in the filename.

### IN-02: `budgetReserve` magic constant duplicated semantics with no shared definition

**File:** `internal/swarm/swarm.go:20`, `internal/swarm/swarm.go:85-88`

**Issue:** `budgetReserve = 3` is the parent post-swarm synthesis reserve, used in
the preflight budget check. The recovery/finalize ceiling in `llm_agent.go`
(`max_steps + 2`) is a separate "+2" with the same flavor (steps reserved outside
the gate). The two reserves (3 here, 2 there) are independently chosen constants
governing the same shared step pool; a future tweak to one without the other can
make the preflight admit a spawn the children then can't finalize within. Document
the relationship or derive one from the other.

**Fix:** Add a comment cross-referencing the `llm_agent.go` `+2` finalize budget,
or centralize both reserves so the invariant `reserve >= finalize_headroom` is
visible.

### IN-03: `anyFloat`/`anyInt` in `runner_persist.go` silently coerce unexpected types to zero

**File:** `internal/runner/runner_persist.go:256-278`

**Issue:** `usageFromStateDelta` decodes the final Event's `StateDelta` (a
`map[string]any` that round-tripped through `json.Number` via the Event decoder).
`anyInt` handles `int/int64/float64` but a `json.Number` (which `Event.UnmarshalJSON`
sets via `dec.UseNumber()`) falls into `default: return 0`. If a persisted/replayed
Event ever reaches this path (vs. the in-process Event that carries native ints),
token counts silently become 0 and the cost/cache-metric row is wrong with no
error. Today the runner consumes in-process Events so the native-int branch hits,
but the type list is incomplete for the decoded shape the same struct can carry.

**Fix:** Add a `json.Number` case to both helpers:

```go
case json.Number:
    i, _ := n.Int64(); return int(i)   // anyInt
    f, err := f.Float64(); return f, err == nil  // anyFloat
```

### IN-04: `firstLine` / `firstMCPDescriptionLine` are duplicated across packages

**File:** `internal/agent/mcptools/bridge.go:170-177`, `cmd/aura/mcp.go:375-382`

**Issue:** Two byte-identical "first non-empty trimmed line" helpers exist
(`firstLine` in mcptools, `firstMCPDescriptionLine` in cmd/aura). CLAUDE.md
"REUSABLE CODE — never duplicate; extract a helper." Minor, but `dupl` is enabled
in `.golangci.yml` and this is the kind of pair it targets.

**Fix:** Export one (`mcptools.FirstDescriptionLine`) and call it from cmd/aura.

---

_Reviewed: 2026-06-04_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
