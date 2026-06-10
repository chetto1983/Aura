# Audit: internal/agent

**Verdict:** needs-work — two medium findings (wasted build allocation on hot path; hardcoded nudge-prefix mirror in `prompt` sub-package) plus one low dead-code note.

**Counts:** critical 0 / high 0 / medium 2 / low 1

---

## Findings

### [MEDIUM][BUG] Double `PromptBuilder.Build` on every LLM call when adaptive reasoning is active

**Location:** `internal/agent/llm_agent.go:201-203`

**Confidence:** high

**Detail:**

```go
req := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)   // line 201
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)  // line 203
}
```

`adaptiveTierSet` is set to `true` on the first iteration (lines 197-200). From the second LLM call onward `adaptiveTierOK` is invariant for the whole run. When it is `true` — i.e., every run where the provider is OpenRouter and `AdaptiveReasoning=true` — the `Build()` at line 201 constructs a complete `llm.Request`: it copies the message slice (`append(append([]llm.Message(nil), history...), ...)` inside `buildBase`) and calls `reg.RenderToolDefs()` (O(n) over the registry). The entire resulting object is then **immediately discarded** and replaced by `BuildWithReasoningTier` at line 203, which does the same work. In the default production configuration (OpenRouter + adaptive reasoning enabled) every LLM turn past the first allocates twice.

`BuildWithReasoningTier` is a strict superset of `Build` (calls `buildBase` then applies the tier then injects cache-control). The first `Build` is fully redundant when `adaptiveTierOK`.

**Suggested fix:** Hoist the branch before the first build call:

```go
var req llm.Request
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)
} else {
    req = a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)
}
```

---

### [MEDIUM][BUG] `prompt.isSyntheticUserHint` duplicates nudge-prefix literals from `internal/agent` constants — silent drift risk

**Location:** `internal/agent/prompt/reasoning_policy.go:97-108`

**Confidence:** high

**Detail:**

`isSyntheticUserHint` is called by `LastGenuineUserContent`, which the adaptive-reasoning router uses to identify the user's real request. It hardcodes string prefixes that must match the nudge constants in the `agent` package:

| Hardcoded prefix in `reasoning_policy.go` | Actual constant in `internal/agent` |
|---|---|
| `"Stop calling tools."` | `finalizeNudge` (llm_agent_finalize.go:28) |
| `"You have run out of tool-call budget"` | `recoveryNudgeGeneric` (llm_agent_finalize.go:42) |
| `` "You have already called `" `` | `recoveryNudgeToolPrefix` (llm_agent_finalize.go:39) |
| `"Your last response was empty."` | `recoveryNudgeEmpty` (llm_agent_finalize.go:71) |
| `"Completion check FAILED:"` | `completionVetoPrefix` (llm_agent_completion.go:36) |

The `prompt` package cannot import `internal/agent` (would close an import cycle, noted in the package comment), so these strings are duplicated by value. If any nudge constant changes — e.g., the finalize nudge gets a different opening sentence — `isSyntheticUserHint` silently stops recognising that nudge. The adaptive-reasoning router would then treat a recovery nudge as the user's genuine request, routing it through the classifier and potentially spending reasoning tokens on internal loop-recovery messages. The compiler gives no warning.

A secondary observation: three of the five filters (`<budget>`, `<workspace>`, `<current_time>`, `<today>`, and `"Stop calling tools."`) can never match when the function is called on `a.history`, because budget hints and the finalize nudge are always tail-injected to **copies** of history — they never appear in `a.history`. These are dead guards. Only the four recovery/veto nudge filters are reachable in practice.

**Suggested fix:**

Export the nudge-prefix constants from `internal/agent` under a thin, cycle-free bridge. Since `prompt` already imports `internal/llm` (not `internal/agent`), the cleanest path is to move the five string constants into a new `internal/agent/agentconst` package (or into `internal/llm/nudge.go` if semantically acceptable), which both `internal/agent` and `internal/agent/prompt` can import without a cycle. Alternatively, expose a `NudgePrefixes() []string` function from a cycle-free helper package and drive both `isAgentNudge` and `isSyntheticUserHint` from it.

---

### [LOW][DEAD-CODE] `prompt.isSyntheticUserHint`: three filters are unreachable in the only production call site

**Location:** `internal/agent/prompt/reasoning_policy.go:99-101, 103`

**Confidence:** high

**Detail:**

`isSyntheticUserHint` checks for these prefixes among others:

```go
strings.HasPrefix(trimmed, "<budget>")
strings.HasPrefix(trimmed, "<workspace>")
strings.HasPrefix(trimmed, "<current_time>")
strings.HasPrefix(trimmed, "<today>")
strings.HasPrefix(trimmed, "Stop calling tools.")
```

`LastGenuineUserContent` (the sole caller) is invoked in `adaptiveReasoningTier` with `a.history`. Budget/workspace/time hints are injected in `buildBase` only to a **copy** of history (`msgs = append(append([]llm.Message(nil), history...), ...)`) and are never appended to `a.history`. Similarly, `finalizeNudge` ("Stop calling tools…") is passed to `synthesize` on a copy, not to `a.history`. Therefore the first four `<tag>` prefix checks and the `"Stop calling tools."` check in `isSyntheticUserHint` will never be `true` when called from `adaptiveReasoningTier`.

This is a maintenance hazard: a reader of `reasoning_policy.go` may assume these branches are exercised and rely on them for correctness, when they are not. If a future refactor ever moves budget hints into the real history, the function would silently start filtering legitimate user requests.

**Suggested fix:** Document the invariant explicitly (these hints are always on copies) and remove the dead filters, or move them to a comment explaining why they are absent. This is also partly addressed by the medium finding above (consolidating the constants).

---

## What was checked

- All non-test `.go` files in `internal/agent/` (agent.go, budget.go, budget_dedup.go, errors.go, event.go, llm_agent.go, llm_agent_completion.go, llm_agent_events.go, llm_agent_finalize.go, llm_agent_pause.go, llm_agent_reasoning.go, llm_agent_stream_retry.go, prompt.go, swarm_context.go, tracing.go)
- All non-test `.go` files in `internal/agent/prompt/` (builder.go, cache_anthropic.go, hash.go, reasoning_policy.go)
- All non-test `.go` files in `internal/agent/workflow/` (loop.go, parallel.go, sequential.go, workflow.go)
- All non-test `.go` files in `internal/agent/mcptools/` (bridge.go, name.go)
- All non-test `.go` files in `internal/agent/agenttest/` (fakeclient.go, mocks.go)
- Test files read for context: export_test.go, llm_agent_test.go, llm_agent_wire_validity_test.go, tracing_test.go, parallel_test.go

**Classes checked and found clean:**

- **Nil-pointer derefs / unchecked errors:** No unchecked errors that matter. `yield` results are checked at every non-terminal call site. `span.End()` and `cancel()` are called on every exit path from the main loop.
- **Context propagation:** `ic.Ctx` flows through every tool dispatch path including `detectPause` (which is acceptable since `ask_user.Execute` ignores the context). `synthesize` and `runCompletionCritic` both derive a timeout context from `ic.Ctx`.
- **Resource leaks:** No unclosed `Body`/`rows`/files in this package. Stream channels are drained on early consumer exit (drain-to-close pattern in `consume`). `time.NewTimer` in `streamWithOpenRetry` is correctly stopped/drained.
- **Races:** `otel.SetTracerProvider` is a global mutation in `newTracerProvider`, but no test in this package uses `t.Parallel()`, so tests execute sequentially and there is no observable race under the current test structure. `RecordingAgent` (agenttest) mutates fields without locks but is only used in sequential test scenarios. `ParallelAgent` correctly serialises all `yield` calls in the iterator frame (fan-in pattern, not concurrent yield).
- **Budget logic:** The `ConsumeStep` TOCTOU-safe pattern (decrement-check-restore) is correct. `branchConsumed` is a separate `atomic.Int32` on each `Budget` value, never shared across branches. `Child()` shares `steps` by pointer but forks a new `branchConsumed`. Dedup ring (two-phase, caller-canonicalizes) is consistently applied.
- **`mcptools.registerBridged` all-or-nothing:** The two-pass structure (validate-first, register-second) correctly implements the all-or-nothing guarantee: any error in the first pass aborts before any `reg.Register` call.
- **`truncateBytes` negative-n:** All call sites use positive constants; no path produces a negative cap.
- **`appendSyntheticToolResults` after completion-gate veto:** `calls[i+1:]` is safe; `i` is the `text_response` index, `i+1` is either in-bounds or empty slice.
- **`terminalBudgetEvent`:** Not dead — called only from `finalizeEvent`, which is itself called from `finalize`. The two former bare-yield call sites were correctly replaced by `finalize` calls.
