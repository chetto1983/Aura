# Audit: internal/eval

**Verdict:** needs-work — three dead struct fields, one false comment that undermines a test's validity, one index-alignment bug in timing measurement, one double-recording of a dimension counter, and one permanently-false short-circuit expression.

**Counts:** critical 0 / high 1 / medium 3 / low 3

## Findings

---

### [HIGH][NOT-WIRED] `buildRegistry` / `e2eRegistry` silently diverge from production, invalidating the KV cache prefix test

**Location:** `internal/eval/scoring_cot_eval.go:230-238`, `internal/eval/harness_kvcache_e2e_test.go:49-58`

**Confidence:** high

**Detail:**
Both functions claim to mirror `cmd/aura/main.go buildRegistry` but register only 4 tools (`text_response`, `tool_search`, `read_tool_output`, `current_time`). Production `buildBaseRegistry` registers 14+ tools including `ask_user`, the `skill` tool, `task`, `web_search`, `web_fetch`, `shell_exec`, and all five `fs_*` tools. Non-deferred tools in production that are absent here: `ask_user`, `task`, `skill`, `web_search`, `web_fetch`. These are enumerated in the default manifest that becomes `messages[0]` — the byte-stable KV-cache prefix. `TestKVCacheWarmingE2E` explicitly asserts cache-prefix stability against the provider, but the `messages[0]` it sends is shorter than production's. A cache hit measured here would not predict production hit rates and the SC#4 ≥80% gate may pass/fail on a different prefix than what ships. `TestCoTEval` also exercises scenarios with a stripped registry: any scenario that accidentally triggers a deferred-tool lookup (e.g. `tool_search` resolving non-existent tools) will behave differently from production.

**Suggested fix:**
Replace `buildRegistry()` in `scoring_cot_eval.go` with a call to the canonical `buildSeamFreeSkillsRegistry(config.LoadDB(), "")` (which the skills E2E already uses and which registers the full non-swarm surface), or import `cmd/aura` `buildBaseRegistry` if it becomes exported. At minimum, update the comment to accurately state the subset and why (e.g., "CoT scenarios only need current_time; this is intentionally not the full production registry").

---

### [MEDIUM][NOT-WIRED] `swarmExpect.mailQuery`, `mailToken`, `waToken` are written but never read

**Location:** `internal/eval/dataset_cot_eval.go:113-115`, `257-259`

**Confidence:** high

**Detail:**
`swarmExpect` declares three fields: `mailQuery string`, `mailToken string`, and `waToken string`. All three are populated in `swarmScenarios()` (lines 257-259) with the placeholder markers `swarmMailTagMarker` / `swarmWATagMarker`. A grep across the entire repo shows zero reads of these fields — `harness_swarm_e2e_test.go` uses the runtime-generated `mailTag` and `waTag` variables directly in `mailReadBack` / `waReadBack`, never reading `sc.swarm.mailToken`, `sc.swarm.mailQuery`, or `sc.swarm.waToken`. The fields hold un-substituted placeholder strings (`%MAIL_TAG%`, `%WA_TAG%`) that would be wrong even if read.

**Suggested fix:**
Remove the three fields from `swarmExpect` and delete the corresponding assignments in `swarmScenarios`. If a future version needs the search query separate from the substituted tag, derive it at the call site from the runtime tag.

---

### [MEDIUM][NOT-WIRED] `swarmExpect.minWorkers` is written but never read — floor hard-coded in `enforceSwarm`

**Location:** `internal/eval/dataset_cot_eval.go:111`, `255`; `internal/eval/harness_swarm_e2e_test.go:493`

**Confidence:** high

**Detail:**
`swarmExpect.minWorkers` is set to `2` in `swarmScenarios()` and is described as "≥2 (autonomous parallelization must fan out)". However `enforceSwarm` (line 493) hard-codes the check as `res.workers < 2` rather than reading `sc.swarm.minWorkers`. If the threshold were ever changed in the dataset, `enforceSwarm` would silently keep the old value. No grep reference to `minWorkers` exists outside definition and assignment.

**Suggested fix:**
Either read the value in `enforceSwarm` (`if res.workers < sc.swarm.minWorkers { ... }`), or remove the field and leave the `< 2` literal documented with a constant.

---

### [MEDIUM][BUG] `swarmFanoutMS` aligns tool call timestamps with tool result timestamps by positional index, which is wrong when tools precede `swarm_spawn`

**Location:** `internal/eval/harness_swarm_e2e_test.go:345-355`

**Confidence:** medium

**Detail:**
`swarmFanoutMS` iterates `c.toolNames` and, for the first `swarm_spawn` entry at index `i`, reads `c.toolResultMS[i]` and `c.toolResults[i]`. The `toolCallMS` and `toolResultMS` slices are independent capture sequences — `toolCallMS[i]` is the timestamp of the i-th tool CALL, while `toolResultMS[i]` is the timestamp of the i-th tool RESULT. There is no guarantee that the i-th call's result is at `toolResults[i]`. If the model calls any tool before `swarm_spawn` (e.g. a `current_time` sanity check), `swarm_spawn` would be at call index 1, and `toolResults[1]` would be that earlier tool's result, not swarm_spawn's. The fanout duration would then be computed from the wrong timestamps, making the SC#1 timing gate measure the wrong interval.

**Suggested fix:**
Track results by tool call, not by list position. One approach: assign each tool call a monotonic slot at capture time and record which result corresponds to which call slot (e.g. via a shared map keyed on tool call order). Alternatively, since `isToolResultPreview` is keyed on `tool_call_id` in `StateDelta`, propagate the call ID into the result capture and join them explicitly.

---

### [LOW][DEAD-CODE] `cancelledOK` is always vacuously true for scenarios that exercise `dimStreamingFidelity`, making it a permanently-false guard

**Location:** `internal/eval/scoring_cot_eval.go:84`, `internal/eval/harness_cot_eval_test.go:226`

**Confidence:** high

**Detail:**
`cancelledOK(sc, c)` returns `sc.cancelMidTurn`. The only scenario with `cancelMidTurn=true` is `cancel-mid`, which has `dimensions: []dimension{dimCancellation}` — it does NOT include `dimStreamingFidelity`. Therefore the guard `!cancelledOK(sc, c)` at line 226 executes only when `contains(sc.dimensions, dimStreamingFidelity)` is true, which is only possible for non-cancel scenarios. For every scenario that reaches line 226, `sc.cancelMidTurn` is `false`, so `cancelledOK` always returns `false` and `!cancelledOK` is always `true`. The function's `*turnCapture` parameter is already blanked (`_`), confirming it is not needed. The guard effectively dead-codes itself: `ok := streamingClean(c)` is the real assertion.

**Suggested fix:**
Remove `cancelledOK` and simplify line 226 to `ok := streamingClean(c)`. If there is a future scenario where `cancelMidTurn` and `dimStreamingFidelity` coincide, add the guard back at that point with documentation.

---

### [LOW][BUG] `dimStreamingFidelity` is double-recorded for the `length-trunc` scenario, inflating the dimension's pass/total count

**Location:** `internal/eval/harness_cot_eval_test.go:226-233` and `293-297`

**Confidence:** high

**Detail:**
For the `length-trunc` scenario (which has both `dimStreamingFidelity` and `expectLength=true`), `dimStreamingFidelity` is recorded twice: once at line 229 (`record(dimStreamingFidelity, ok)`) and again at line 295 (`record(dimStreamingFidelity, cur && lengthOK)`). `dimResult.total` is incremented twice, and `dimResult.pass` is incremented 0, 1, or 2 times. The report row for this dimension would show e.g. `5/6` pass-rate across 5 scenarios + 1 scenario contributing 2 samples, rather than the accurate `4/5` for a dimension that exercises 5 scenarios each once. `enforce()` checks `r.pass == r.total` — a 2/2 from `length-trunc` passes even if the double-recording hid an intermediate failure.

**Suggested fix:**
Fold the length check directly into the initial `streamingClean` assertion rather than recording a second sample. For example:

```go
if contains(sc.dimensions, dimStreamingFidelity) {
    ok := streamingClean(c)
    if sc.expectLength {
        ok = ok && c.finish == "length" && strings.Contains(c.prose, "[risposta troncata: max_tokens]")
    }
    record(dimStreamingFidelity, ok)
    m.dimVerdicts[dimStreamingFidelity] = ok
}
```

---

### [LOW][DEAD-CODE] `dimResultRecordAdvisory`'s float64 parameter is blanked and the ratio value is silently discarded

**Location:** `internal/eval/scoring_cot_eval.go:209-212`

**Confidence:** high

**Detail:**
`dimResultRecordAdvisory(record func(dimension, bool), d dimension, _ float64)` accepts a float64 ratio but blanks it with `_`. The function always calls `record(d, true)` — the ratio is completely unused. The comment says "the report shows the ratio" but the ratio shown in the per-scenario metrics table comes from `m.cacheRatio`, not from this parameter. The function signature implies a dependency that does not exist. Callers pass `m.cacheRatio` (line 329), wasting no work, but any reader of the function signature would incorrectly infer that the ratio influences the record.

**Suggested fix:**
Remove the float64 parameter since it is unused:

```go
func dimResultRecordAdvisory(record func(dimension, bool), d dimension) {
    record(d, true)
}
```

Update the single call site accordingly.
