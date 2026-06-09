# Audit: internal/agent

**Verdict:** needs-work — two correctness defects (misclassified reasoning tier, redundant LLM request build), three API-surface knobs parsed and stored but never consumed in production paths.

**Counts:** critical 0 / high 1 / medium 2 / low 2

---

## Findings

### [HIGH][BUG] `isSyntheticUserHint` misses `recoveryNudgeEmpty` — router classifies agent's own nudge as the user's request

**Location:** `internal/agent/prompt/reasoning_policy.go:97-107`
**Confidence:** high

**Detail:**
`LastGenuineUserContent` (called by `adaptiveReasoningTier` on every first loop iteration) walks history backward and calls `isSyntheticUserHint` to skip the agent's own injected user-role messages. `isSyntheticUserHint` filters: `<budget>`, `<workspace>`, `<current_time>`, `<today>`, "Stop calling tools.", "You have run out of tool-call budget", "You have already called \`", "Completion check FAILED:". It does NOT check for `recoveryNudgeEmpty` = `"Your last response was empty. Continue the task now — ..."` (defined in `internal/agent/llm_agent_finalize.go:71`).

The companion function `isAgentNudge` in `llm_agent_completion.go:229-233` correctly handles all four nudge variants including `recoveryNudgeEmpty`. The two filters diverged.

When `maybeRecoverEmptyResponse` fires (provider sent an empty completion), it appends the `recoveryNudgeEmpty` string as a user-role message. On the next turn, `LastGenuineUserContent` returns that nudge string. The adaptive reasoning router then classifies "Your last response was empty. Continue the task now" as the user's request — this scores as "high" reasoning (it matches the "multi-step analysis" heuristic), inflating both `reasoning_tokens` and `max_tokens` for a turn that should inherit the original user's tier. The misclassification is silent: no error, just a quietly wrong reasoning budget.

**Suggested fix:**
Add the `recoveryNudgeEmpty` prefix check to `isSyntheticUserHint` in `reasoning_policy.go`, mirroring `isAgentNudge`:

```go
strings.HasPrefix(trimmed, "Your last response was empty.") ||
```

Or better, define a shared package-level sentinel set that both functions draw from instead of maintaining two copies of the same list.

---

### [MEDIUM][BUG] Redundant `Build` call discarded unconditionally when adaptive reasoning is active

**Location:** `internal/agent/llm_agent.go:201-204`
**Confidence:** high

**Detail:**
On every loop iteration the code unconditionally calls `a.builder.Build(...)` (line 201), then immediately overwrites `req` with `a.builder.BuildWithReasoningTier(...)` (line 203) when `adaptiveTierOK` is true. The first `Build` call — which copies all of `a.history`, sorts and renders all `tools.ToolDef`s, applies `cache_control`, and clones the message slice — is pure waste: its result is thrown away on every turn when adaptive reasoning is enabled.

When `AURA_ADAPTIVE_REASONING=true` (the intended production path), `adaptiveTierOK` is true after the first turn, meaning EVERY subsequent loop iteration runs `Build` twice. With large histories and many registered tools this can be measurable (history clone + tool sort is O(N log N) per turn).

```go
// line 201 — always called, result thrown away when adaptiveTierOK:
req := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)
if adaptiveTierOK {
    // line 203 — replaces req entirely; the Build above is wasted
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)
}
```

**Suggested fix:**
Skip the unconditional `Build` when a tier is available:

```go
var req llm.Request
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)
} else {
    req = a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)
}
```

---

### [MEDIUM][NOT-WIRED] `Budget.NodeTimeout()` parsed and stored but never consumed in production

**Location:** `internal/agent/budget.go:138-142`, `169`, `299`, `326-328`
**Confidence:** high

**Detail:**
`AURA_LOOP_NODE_TIMEOUT_SEC` is parsed by `NewBudget`, stored in `b.nodeTimeout`, propagated to children via `Child()`, and surfaced via `NodeTimeout()`. No production caller ever calls `b.NodeTimeout()` outside tests. Grep across the entire repo (`D:/Aura`) confirms the only non-definition, non-test references are in `budget_test.go`. The intended behaviour (per D-13 and the PRD) is that when non-zero this value bounds a per-node sub-context so one hung tool cannot eat the whole wallclock budget. That context derivation is never performed — the knob is dead.

**Suggested fix:**
In `LlmAgent.Run`, after the per-call `context.WithTimeout` that applies `TotalTimeoutSec`, additionally derive a node-scoped child context when `ic.Budget.NodeTimeout() > 0`:

```go
if nt := ic.Budget.NodeTimeout(); nt > 0 {
    callCtx, cancel = context.WithTimeout(callCtx, nt)
}
```

Or file a TODO/open-question tracking the gap, since the D-13 comment marks this as deferred.

---

### [LOW][NOT-WIRED] `Budget.WithDeadline` and `Budget.SoftCapExceeded` have no production callers

**Location:** `internal/agent/budget.go:263-271` (`SoftCapExceeded`), `319-323` (`WithDeadline`)
**Confidence:** high

**Detail:**
Both exported methods are defined, unit-tested, and mentioned in design docs:

- `WithDeadline(parent context.Context)` — designed to thread the budget's wallclock into an in-flight LLM/tool call's context so long-running calls are cancelled end-to-end (D-13). No production caller invokes it; the `LlmAgent` loop uses a plain `context.WithTimeout` against `TotalTimeoutSec` instead.
- `SoftCapExceeded()` — designed as a passive per-branch fairness signal for `ParallelAgent` scheduling (D-12). `ParallelAgent` never calls it; it only checks `ic.Budget.ConsumeStep()`.

Both are exported API surface that the `ParallelAgent` and `LlmAgent` were intended to consume per the D-12/D-13 design decisions, but the wiring was deferred and never completed. They are not dead code (their behaviour is tested), but they are not-wired in the production execution path.

**Suggested fix:**
Either wire the methods (add the `WithDeadline`-derived context inside the per-call timeout chain; add a `SoftCapExceeded` fairness check in `ParallelAgent.Run`), or mark them `// Planned: D-12/D-13 — not yet wired in production` to make the gap explicit and prevent confusion during future phases.

---

### [LOW][DEAD-CODE] `terminalBudgetEvent` has no external callers; sole use is key extraction inside `finalizeEvent`

**Location:** `internal/agent/llm_agent_events.go:222-232`
**Confidence:** medium

**Detail:**
`terminalBudgetEvent` was the original budget-exhaustion terminal event builder. After Phase 07.1 (forced finalization), both trip sites were re-routed to `finalize()`, so `terminalBudgetEvent` is never yielded to the caller. Its sole surviving use is in `finalizeEvent` (line 246), which calls it only to extract the `StateDelta` key names for observability consistency:

```go
for k, v := range a.terminalBudgetEvent(ic, spanID, parentSpanID, reason).Actions.StateDelta {
    ev.Actions.StateDelta[k] = v
}
```

This is clever for byte-consistency, but it means a full `*Event` is allocated and immediately discarded (only its map is read). The function is not dead-code in the strict sense (it is called), but its semantic role — yielding an Escalate event as the last action — is dead. The comment at line 219 says "never the iter.Seq2 error slot" and "termination is Event-only", but no run actually ends on this event anymore.

**Suggested fix:**
Extract the StateDelta key constants to package-level consts (`terminationReasonKey = "termination_reason"`, `limitHitKey = "limit_hit"`) and use them directly in both `terminalBudgetEvent` and `finalizeEvent`. This eliminates the allocation and makes the key-sharing explicit without the current indirect approach. Low priority — no correctness impact.

---

## What was checked

Audited all non-test Go files in `internal/agent` (root package, `workflow/`, `prompt/`, `mcptools/`, `agenttest/`, `tools/`): `agent.go`, `budget.go`, `budget_dedup.go`, `errors.go`, `event.go`, `llm_agent.go`, `llm_agent_completion.go`, `llm_agent_events.go`, `llm_agent_finalize.go`, `llm_agent_pause.go`, `llm_agent_reasoning.go`, `llm_agent_stream_retry.go`, `prompt.go`, `swarm_context.go`, `tracing.go`, `workflow/{workflow,loop,parallel,sequential}.go`, `prompt/{builder,cache_anthropic,hash,reasoning_policy}.go`, `mcptools/{bridge,mount,name}.go`, `agenttest/{fakeclient,mocks}.go`, `tools/{action,ask_user,spec,registry,manifest,result}.go`.

Grepped across the full repo (`D:/Aura`) to verify every usage claim and confirm production vs test-only references. No goroutine leak paths found in production code (all `consume` drain paths cover the stopped case). No unchecked errors that carry real consequences. No slice aliasing or integer overflow risks. The `otel.SetErrorHandler` global mutation in `tracing.go` is not a race because no tests run that path in parallel. Go module version is 1.26.4, so loop-capture is not a concern.
