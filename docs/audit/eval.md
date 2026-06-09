# Audit: internal/eval

**Verdict:** needs-work — four not-wired struct fields, one misleading scoring predicate, and one medium logic defect in `streamingClean`.

**Counts:** critical 0 / high 0 / medium 2 / low 2

## Findings

### [MEDIUM][NOT-WIRED] `swarmExpect.minWorkers`, `mailQuery`, `mailToken`, `waToken` never consumed by harness

**Location:** `internal/eval/dataset_cot_eval.go:111-115, 255-259`
**Confidence:** high

`swarmExpect` declares four fields that are set in `swarmScenarios()` but are never accessed by `harness_swarm_e2e_test.go`:

- `minWorkers` (set to `2`) — `enforceSwarm` hard-codes the threshold `res.workers < 2` rather than reading `sc.swarm.minWorkers`.
- `mailQuery`, `mailToken`, `waToken` — all set to `swarmMailTagMarker`/`swarmWATagMarker` values. The harness's `mailReadBack` and `waReadBack` receive the per-run `mailTag`/`waTag` arguments directly; neither function reads these struct fields.

If the threshold or tag values in the dataset ever diverge from the harness hard-codes, the gate silently enforces the wrong threshold/tag.

**Suggested fix:** Either delete the four unused fields from `swarmExpect` and the corresponding dataset assignments, or thread them into the harness (`enforceSwarm` should use `sc.swarm.minWorkers`; `mailReadBack`/`waReadBack` should use `sc.swarm.mailToken`/`waToken` instead of the bare `mailTag`/`waTag` args — they are the same value today, but the field exists precisely to make the dataset the single source of truth).

---

### [MEDIUM][BUG] `hasTimestamp` false-positives on the stable `SystemPrompt` — `dimCachePrefix` always reports false

**Location:** `internal/eval/scoring_cot_eval.go:195-198`, `internal/eval/harness_cot_eval_test.go:164`
**Confidence:** high

`hasTimestamp` checks three independent `strings.Contains` calls:

```go
func hasTimestamp(s string) bool {
    return strings.Contains(s, "20") && (strings.Contains(s, ":") && strings.Contains(s, "T"))
}
```

`agent.SystemPrompt` (the constant checked in `harness_cot_eval_test.go:164`) contains all three substrings: `"20"` (e.g. `"2026"`), `":"` (punctuation throughout), and `"T"` (e.g. `"Think"`, `"The"`, `"Tools"`). As a result, `hasTimestamp(agent.SystemPrompt)` is always `true`, making `!hasTimestamp(agent.SystemPrompt)` always `false`, and therefore `stable` is always `false` — meaning `dimCachePrefix` is permanently recorded as failing even when the system prompt is provably unchanged.

The scored report emits an incorrect pass-rate for `cache_prefix_stability` on every run. Since `dimCachePrefix` is not listed in `enforce`, this does not fail the test, but the metric is always misleading.

**Suggested fix:** Replace the crude multi-field check with a regex or a narrower substring that only matches actual runtime-injected timestamps (e.g., a pattern like `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}` or check for the year + `T` adjacent to digits). Alternatively, since the intent is just to assert `agent.SystemPrompt == sysPrompt` (unchanged), drop the `hasTimestamp` check entirely — value equality already ensures no runtime mutation occurred.

---

### [LOW][NOT-WIRED] `skillsExpect.judgeBudget` written but never read

**Location:** `internal/eval/scenarios_skills.go:55, 102, 139`
**Confidence:** high

`skillsExpect.judgeBudget` is set to `judgeSkillsGate` in both skills scenarios (North-Star and snippet-reuse), but neither `runSkillsScenario` (in `skills_cot_eval_test.go`) nor the snippet-reuse harness reads it. Both call `runSkillsJudge` which directly uses the package-level constant `judgeSkillsGate`. If the gate threshold were changed in the scenarios, the harness would silently ignore the override.

**Suggested fix:** Either delete the field (since it always duplicates the constant) or wire it: pass `sc.skills.judgeBudget` as the gate argument to `runSkillsJudge` / `runRubricJudge` instead of going through the constant.

---

### [LOW][BUG] `streamingClean` patterns 2 and 3 contain literal backslash — they never fire

**Location:** `internal/eval/scoring_cot_eval.go:69`
**Confidence:** high

```go
bad := []string{`{"text":`, `"text\":`, `{"text\":`}
```

Patterns 2 and 3 are raw string literals containing a literal backslash character (`\`). The actual bytes are `"text\:` and `{"text\:` respectively. Neither of these 8/9-byte sequences would appear in a real SSE/JSON wire artifact; an actual escaped-JSON leak would look like `"text\":` in a regular (interpreted) string literal, which is `"text":` in bytes — already covered by pattern 1. The patterns therefore contribute nothing to the check and give a false sense of additional coverage.

**Suggested fix:** Remove patterns 2 and 3 (they are redundant dead checks), or replace them with the intended patterns using interpreted string literals: `"\"text\":"` and `"{\"text\":"`.

---

## Clean

The following were checked and found correct:

- **Goroutine safety / races:** No concurrent map writes. The cancellation goroutine (`time.Sleep + cancel()`) only calls a closure; no shared mutable state.
- **Context propagation:** Every live-agent call passes `turnCtx` / `bctx`; judge calls use timeout-derived contexts; all have `defer cancel()`.
- **Error paths:** `runJudgeUser` swallows mid-stream errors (the channel carries no error field), which is consistent with the rest of the codebase's `llm.Client` contract and noted in the doc comment.
- **Resource leaks:** `captureTurn` drains the `iter.Seq2` via range-over-func; early `break` on terminal/pause is safe per Go 1.22+ iterator contract. Report files are closed by `os.WriteFile` (not left open). Pool cleanup is registered via `t.Cleanup`.
- **`flushRemainder` divergence branch:** When `finalAnswer` does not start with `already`, `prose` is reset and `emit(finalAnswer)` is called. The `rawProse` accumulates `already + finalAnswer` (double-counting the already-seen partial content). This is an edge case (the final consolidated answer disagrees with accumulated streaming chunks) that can only occur on a malformed stream; the `rawProse` field is documented as a leak-surface scan and not a precision field, so this is acceptable.
- **`anyFloat` missing `int64`:** JSON-decoded `map[string]any` values are always `float64` for numbers; `int64` would not appear from JSON. Low practical risk.
- **`ptrOrNil` pointer-to-copy:** The returned pointer escapes correctly (Go's escape analysis heap-allocates the string when it escapes).
- **`AppendTurn` Seq:** The manual `n + 1 + i` in `answerPendingGenerically` is sequential and runs after the runner.Turn loop completes, so no concurrency hazard in practice. Using `Seq: 0` (auto-allocate) would be cleaner but is not a defect.
- **Build-tag hygiene:** All live-eval files carry `//go:build cot_eval` or `//go:build cot_eval || live_e2e`; `doc.go` and `chat50_prompts_test.go` carry no tag, keeping the package valid under the default build.
- **`cancelledOK` dead guard:** `cancelledOK` is only meaningful when a scenario has both `cancelMidTurn:true` AND `dimStreamingFidelity`; no current scenario has both, so the check always evaluates `!false = true` and is transparent. Redundant but not harmful.
