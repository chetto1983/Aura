# Audit: internal/agent

**Verdict:** needs-work — two dead-code issues and one efficiency bug are actionable; no critical defects.
**Counts:** critical 0 / high 0 / medium 2 / low 3

## Findings

### [MEDIUM][DEAD-CODE] `requestID` parameter accepted and immediately discarded in `finalEvent`

**Location:** `internal/agent/llm_agent_events.go:193-196`

**Confidence:** high

**Detail:**
`finalEvent` declares `requestID string` as a parameter but immediately blanks it with `_ = requestID` (line 196). The field is never used inside the function body. All three call sites in the package pass the local `requestID` variable, wasting a string copy per call. The only reason to keep a dead parameter would be forward-compat, but the method is unexported and the `requestID` is already reachable via `ic.RequestID`.

```go
// llm_agent_events.go:193-196
func (a *LlmAgent) finalEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID, answer, finish string, usage llm.Usage,
) *Event {
	_ = requestID   // dead — never read
```

Call sites (all three pass `requestID` uselessly):
- `internal/agent/llm_agent.go:265`
- `internal/agent/llm_agent.go:329`
- `internal/agent/llm_agent_finalize.go:245`

**Suggested fix:** Drop `requestID` from the `finalEvent` signature and remove it from all three call sites. If future OTel wiring needs it, use `ic.RequestID.String()` directly inside the function.

---

### [MEDIUM][BUG] Double `Build` call on every agent-loop iteration when adaptive reasoning is enabled

**Location:** `internal/agent/llm_agent.go:201-204`

**Confidence:** high

**Detail:**
On every iteration of the main `for {}` loop in `LlmAgent.Run`, the code unconditionally calls `a.builder.Build(...)` (line 201) and assigns the result to `req`. Immediately after, when `adaptiveTierOK` is true, it calls `a.builder.BuildWithReasoningTier(...)` (line 203) and reassigns `req`, discarding the first result entirely. Both calls invoke `buildBase` internally, which calls `reg.RenderToolDefs()` — allocating and marshalling the full tool-definition list. For a registry with N tools, this means N-tool renders happen twice per LLM call when adaptive reasoning is active (OpenRouter provider + `AdaptiveReasoning: true`).

```go
// llm_agent.go:201-204
req := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget) // allocated and discarded
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(...)  // replaces req entirely
}
```

**Suggested fix:** Use a conditional assignment instead of the unconditional `Build`:

```go
var req llm.Request
if adaptiveTierOK {
    req = a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, adaptiveTier)
} else {
    req = a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)
}
```

---

### [LOW][DEAD-CODE] Four exported `Budget` methods referenced only from test files

**Location:** `internal/agent/budget.go:267`, `internal/agent/budget.go:328`, `internal/agent/budget.go:322`, `internal/agent/budget.go:259`

**Confidence:** high

**Detail:**
The following exported `Budget` methods have zero production call sites. Every reference is in a `_test.go` file:

| Method | Defined at | Test references only |
|---|---|---|
| `SoftCapExceeded()` | `budget.go:267` | `budget_test.go:433,480,493,499` |
| `NodeTimeout()` | `budget.go:328` | `budget_test.go:457` |
| `WithDeadline()` | `budget.go:322` | `budget_test.go:443` |
| `SetMaxSteps()` | `budget.go:259` | `budget_test.go:413`, `workflow/parallel_test.go:130`, `agenttest/mocks_test.go:21` |

`SoftCapExceeded` and `NodeTimeout` are intended for the Phase-12 swarm and the per-node timeout feature respectively (referenced in planning docs but not yet consumed). `WithDeadline` was planned for the Runner's context cancellation path. `SetMaxSteps` is used only for test setup.

None of these constitutes a production defect today. Flagged as dead-code for the fixer to decide: either expose via an internal test-helper (`export_test.go`) to avoid exporting them, or accept as forward-compat API stubs and document them as such.

**Suggested fix (optional):** Move `SetMaxSteps` (purely a test-setup helper) to `export_test.go`. Mark `SoftCapExceeded`, `NodeTimeout`, and `WithDeadline` with a `// Future: consumed by Phase-N` comment so the next audit pass knows they are intentional forward-compat stubs, not dead code.

---

### [LOW][DEAD-CODE] `Event.SetAuthorIfEmpty` exported but never called in production

**Location:** `internal/agent/event.go:135`

**Confidence:** high

**Detail:**
`SetAuthorIfEmpty` is an exported method on `*Event`. Grep across the whole repo finds it called only from:
- `internal/agent/agenttest/mocks.go:147` — a test-helper package
- `internal/agent/event_test.go:376` — a direct unit test

No production code (cmd/, internal/runner, internal/swarm, internal/channels) ever calls it. The `agenttest` package is imported only by `_test.go` files.

**Suggested fix:** Move to `export_test.go` or document as a helper the Phase-12 AG-UI gateway is expected to use (if that is the intent). If the only caller is `agenttest`, unexport it to `setAuthorIfEmpty` in `agenttest/mocks.go` directly.

---

### [LOW][BUG] `otel.SetErrorHandler(no-op)` permanently silences global OTel error handler in OTLP mode

**Location:** `internal/agent/tracing.go:63`

**Confidence:** medium

**Detail:**
When `newTracerProvider` is called with `mode == "otlp"` (the default), it installs a global no-op OTel error handler (`otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))`) that is **never restored**. This is intentional in production to suppress "connection refused" noise from a missing collector. However:

1. In the test binary, `TestNewTracerProvider_OTLPNoCollector` triggers this path and leaves the no-op handler installed for the entire process lifetime. Any OTel error that occurs in subsequent tests (e.g. from a real-provider test that misbehaves) is silently swallowed.
2. The `t.Cleanup` in `TestNewTracerProvider_OTLPNoCollector` correctly shuts down the provider, but does not restore the original global error handler.

This is a test-isolation bug, not a production bug.

**Suggested fix:** In `TestNewTracerProvider_OTLPNoCollector`, save the previous error handler before the test and restore it in `t.Cleanup`:
```go
prev := otel.GetErrorHandler()
t.Cleanup(func() { otel.SetErrorHandler(prev) })
```
For production use, the current behavior (permanent no-op) is correct and documented.

## Coverage

Files read and checked (non-test):
- `agent.go` — Agent interface, InvocationContext, WithContext/WithSubAgent
- `errors.go` — ErrBudgetExhausted sentinel
- `budget.go` — Budget struct, NewBudget/NewBudgetFromEnv, ConsumeStep, Child, soft-cap, deadline
- `budget_dedup.go` — dedupRing, BeforeToolCall, AfterToolResult, ExemptToolsFromEnv
- `event.go` — Event struct, MarshalJSON/UnmarshalJSON, SetAuthorIfEmpty, helper types
- `llm_agent.go` — LlmAgent struct, NewLlmAgent, Run, dispatch, runTool, consume, parseTextResponse, canonicalArgs
- `llm_agent_completion.go` — gateCompletion, runCompletionCritic, sideEffectDigest, parseCriticVerdict
- `llm_agent_events.go` — newEvent, all event constructors, finalEvent, usageStateDelta, exitCodeFromMeta
- `llm_agent_finalize.go` — finalize, synthesizeWithFallback, stubDigest, synthesize, finalizeEvent, maybeRecover
- `llm_agent_pause.go` — pauseCalls, pauseToolCalls, detectPause, emitPauses, pauseEvent
- `llm_agent_reasoning.go` — adaptiveReasoningTier, parseReasoningRouterTier, normalizeReasoningTier
- `llm_agent_stream_retry.go` — streamWithOpenRetry, retryableStreamOpenError
- `prompt.go` — SystemPrompt constant, systemMessage
- `swarm_context.go` — SwarmContextValue, WithSwarmContext, SwarmContext
- `tracing.go` — newTracerProvider, NewTracerProvider, mintSpanID, rootSpanIDs, startLLMSpan, setSpanAttrs
- `workflow/loop.go`, `workflow/parallel.go`, `workflow/sequential.go` — workflow agents (scoped read for wiring verification)

Cross-repo grep confirmed: all production call sites verified for `ExemptToolsFromEnv`, `SwarmContext`, `NewTracerProvider`, `NewBudget`, `NewBudgetFromEnv`. No missing wiring found for the primary loop path.
