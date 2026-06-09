# Audit: internal/eval

**Verdict:** needs-work — three concrete bugs (dead-code guard, dead struct fields, swarm index misalignment) plus one not-wired enforcement gap; no critical security issues.

**Counts:** critical 0 / high 1 / medium 3 / low 2

---

## Findings

### [HIGH][NOT-WIRED] `dimCachePrefix` declared "asserted" but omitted from `enforce()` gate

**Location:** `internal/eval/scoring_cot_eval.go:261` (`enforce`), `scoring_cot_eval.go:277` (`classOf`)

**Confidence:** high

**Detail:**
`classOf(dimCachePrefix)` returns `"asserted"` (default branch), `thresholdOf` returns `"100% (asserted)"`, and `writeReport` counts it toward `overallPass/overallTotal` as an asserted dimension. However `enforce()` (line 261) iterates only:

```
dimStreamingFidelity, dimToolLoop, dimCostHonesty, dimBudget, dimCancellation, dimGuardrail
```

`dimCachePrefix` is absent. A `cache_prefix_stability` failure (e.g. `agent.SystemPrompt` mutated or gained a timestamp) appears in the report as a failing "asserted" row and lowers `overallPass/overallTotal`, but never calls `t.Errorf`, so the test suite still reports PASS. The gate is silently bypassed.

**Suggested fix:** Add `dimCachePrefix` to the `enforce` loop alongside the other asserted dimensions:

```go
for _, d := range []dimension{dimStreamingFidelity, dimToolLoop, dimCostHonesty,
    dimBudget, dimCancellation, dimGuardrail, dimCachePrefix} {
```

---

### [MEDIUM][DEAD-CODE] `cancelledOK` guard in `dimStreamingFidelity` block is unreachable

**Location:** `internal/eval/harness_cot_eval_test.go:226`, `internal/eval/scoring_cot_eval.go:84`

**Confidence:** high

**Detail:**
The only scenario with `cancelMidTurn: true` is `cancel-mid`, whose `dimensions` is `[]dimension{dimCancellation}`. It does NOT include `dimStreamingFidelity`. The guard at line 226:

```go
ok := !cancelledOK(sc, c) && streamingClean(c)
```

is inside `if contains(sc.dimensions, dimStreamingFidelity)`. Since no scenario combines `cancelMidTurn: true` with `dimStreamingFidelity`, `cancelledOK` always returns `false` inside this block, making `!cancelledOK(sc,c)` always `true`. The guard is dead.

A latent risk: if a future scenario were added with both flags, `ok` would be `!true && ...` = `false`, recording a false-negative pass of `dimStreamingFidelity` as a failure (the comment says "cancel scenarios don't assert fidelity here" — which implies the intent is to skip, not fail, but the code records `false`).

**Suggested fix:** Remove the `cancelledOK` guard and just use `streamingClean(c)`. If cancel+fidelity combinations are ever intended, document the expected behavior explicitly:

```go
ok := streamingClean(c)
```

---

### [MEDIUM][DEAD-CODE] `swarmExpect` fields `mailToken`, `waToken`, `mailQuery`, `minWorkers` are never read

**Location:** `internal/eval/dataset_cot_eval.go:111–117`, populated at lines `255–259`

**Confidence:** high

**Detail:**
`swarmExpect` declares four fields that are populated in `swarmScenarios()` but never accessed outside of their definition:

- `mailToken string` — populated as `swarmMailTagMarker` but never read (the harness uses the separately injected `mailTag` variable)
- `waToken string` — populated as `swarmWATagMarker` but never read (the harness uses `waTag`)
- `mailQuery string` — populated as `swarmMailTagMarker` but never accessed via `.mailQuery` anywhere in the codebase
- `minWorkers int` — populated as `2` but `enforceSwarm` hardcodes `res.workers < 2` rather than reading `sc.swarm.minWorkers`

The hardcoded `2` in `enforceSwarm` means the enforcement threshold cannot be changed via the dataset without editing two places.

**Suggested fix:** Delete `mailToken`, `waToken`, `mailQuery` (or replace with documented comments explaining the substitution mechanism). Change `enforceSwarm` to read `sc.swarm.minWorkers` if the parameterization is intentional, otherwise document why the hardcoded `2` is correct.

---

### [MEDIUM][BUG] `swarmFanoutMS` indexes `toolNames` and `toolResults` with the same index despite misaligned append semantics

**Location:** `internal/eval/harness_swarm_e2e_test.go:346–355`

**Confidence:** medium

**Detail:**
`c.toolNames` is populated by iterating over `resp.ToolCalls` (one entry per individual tool in a single LLM response — multiple tools can share one response). `c.toolResults` is populated one-per-preview-event (one entry per tool result event). `swarmFanoutMS` correlates them with the same index `i`:

```go
for i, name := range c.toolNames {
    if name != "swarm_spawn" || i >= len(c.toolResultMS) || i >= len(c.toolCallMS) {
        continue
    }
    if i < len(c.toolResults) && strings.HasPrefix(strings.TrimSpace(c.toolResults[i]), "[") {
        return c.toolResultMS[i] - c.toolCallMS[i]
    }
}
```

If the LLM issues N tool calls in a single response (parallel tool calls), `toolNames` grows by N per event while `toolResults` grows by 1 per event. For example: two tools issued together → `toolNames[0]=current_time, toolNames[1]=swarm_spawn`, but `toolResults[0]=current_time_result, toolResults[1]=swarm_spawn_result`. This happens to be aligned for sequential one-call-per-response patterns but breaks silently if the LLM issues parallel calls in one response. When misaligned, `swarmFanoutMS` returns 0 (timing becomes advisory-pass), masking whether the timing gate was actually met.

**Suggested fix:** Build a per-call-id map from `toolCallMS` → `toolResultMS` keyed on the `tool_call_id`, or accept that timing is always advisory and document that assumption explicitly.

---

### [LOW][DEAD-CODE] `anyFloat` in `capture_cot_eval.go` missing `json.Number` and `int64` cases present in the reference implementation

**Location:** `internal/eval/capture_cot_eval.go:214–223`

**Confidence:** medium

**Detail:**
`anyFloat` in `capture_cot_eval.go` handles `float64` and `int` only. The parallel implementation in `internal/runner/runner_persist.go:351–363` adds `int64` and `json.Number` cases, justified by comment "IN-03: the StateDelta is decoded with UseNumber (event.go), so cost_usd can arrive as a json.Number". The comment in `capture_cot_eval.go` explicitly says it mirrors `runner_persist.go`.

In the eval harness's specific execution path (in-process `iter.Seq2` — no JSON round-trip), `cost_usd` is stored as a native `float64` via `usageStateDelta` in `llm_agent_events.go:214`, so the missing cases do not cause a bug today. However, the divergence is a maintenance trap: if the eval harness is ever changed to deserialize events (e.g., for replay), the cost will silently read as `$0` with no error.

**Suggested fix:** Align `anyFloat` in `capture_cot_eval.go` with `runner_persist.go` by adding the `int64` and `json.Number` cases, and add a comment referencing the same IN-03 note.

---

### [LOW][BUG] Transport-error retry in `driveSkillsLoop` may record duplicate `toolCalls` entries

**Location:** `internal/eval/skills_cot_eval_test.go:341–355`

**Confidence:** medium

**Detail:**
`captureSkillCalls(res, c)` (line 345) is called unconditionally before the `c.runErr` check (line 351). If a transport error occurs after partial tool calls, `res.toolCalls` already contains entries from the errored turn. On retry (the same loop iteration increments `hop`, runs again), `captureSkillCalls` is called again. If the model re-issues the same tool calls on retry, `res.toolCalls` accumulates duplicates. The boolean flags (`selfInstall`, `installTarget`, `installSel`) are set-once idempotent, but `res.toolCalls` (the action-aware call log written to the report) will show doubled entries.

This does not affect gate correctness but corrupts the report's "Action-aware tool calls" row, making it harder to diagnose a failed run.

**Suggested fix:** Move `captureSkillCalls` to after the `runErr` guard, or snapshot and discard partial tool-call captures when retrying:

```go
if c.runErr != nil && !transportRetried {
    transportRetried = true
    // discard partial captures from the errored turn
    res.toolCalls = res.toolCalls[:priorLen]
    continue
}
captureSkillCalls(res, c)
```
