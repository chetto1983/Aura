# internal/agent

A small, reusable LLM/tool loop without Telegram coupling. The package's
two main exports are:

- **`agent.Run`** — the streaming-capable loop entry used by `chat.Hub`
  (Telegram conversation path, swarm workers via adapter).
- **`agent.RunTask`** — the stateless one-shot wrapper for non-streaming
  callers (web chat `/api/chat`, cron background jobs). Callers read limits
  fresh from their config and pass them in `RunTaskDeps` each call; there
  is no constructor or shared mutable state.

Background agents in `internal/swarm` use `RunTask` via a thin adapter in
`cmd/aura`. The two paths share the same `loop.go` core; there is no
duplicate loop body.

## Contracts

### Task — input

```go
type Task struct {
    SystemPrompt        string
    Prompt              string
    Messages            []llm.Message    // alternative to Prompt
    ToolAllowlist       []string         // empty = all tools
    UserID              string
    Temperature         *float64
    MaxToolCalls        int              // 0 = unlimited
    MaxToolResultChars  int              // 0 = governance default (8000)
    FinalizationTimeout time.Duration
    CompleteOnDeadline  bool
}
```

### Result — output

```go
type Result struct {
    Content   string
    Messages  []llm.Message
    LLMCalls  int
    ToolCalls int
    Tokens    llm.TokenUsage
    Elapsed   time.Duration
}
```

### RunTaskDeps — wiring

```go
type RunTaskDeps struct {
    LLM             llm.Client
    Tools           *tools.Registry
    Model           string
    ReasoningEffort string
    PhantomGuard    *PhantomToolGuard
    Logger          *slog.Logger
    // Per-call limits — read once at RunTask entry.
    MaxIterations int
    Timeout       time.Duration
    ToolTimeout   time.Duration
}
```

## Public API

### agent.RunTask — non-streaming one-shot

```go
result, err := agent.RunTask(ctx, agent.RunTaskDeps{
    LLM:           llmClient,
    Tools:         toolRegistry,
    Model:         "claude-sonnet-4-6",
    MaxIterations: 5,
    Timeout:       60 * time.Second,
    ToolTimeout:   30 * time.Second,
    Logger:        slog.Default(),
}, agent.Task{
    SystemPrompt:       "You are AuraBot's background worker.",
    Prompt:             "Summarize the last 24h of conversations.",
    ToolAllowlist:      []string{"search_memory", "read_source"},
    MaxToolCalls:       6,
    MaxToolResultChars: 8000,
    UserID:             "42",
})
fmt.Println(result.Content)
```

### agent.Run — streaming (chat.Hub path)

`Run(ctx, Invocation) (RunResult, error)` drives the inner loop. The
`Invocation` struct wires the `ChatClient` (streaming-capable), `Executor`
(tool fan-out), `State` (message accumulator), and `Options` (budget caps).
Callers outside `chat.Hub` and its adapters should use `RunTask` instead.

## Behavioural notes

### Timeout precedence

- `RunTaskDeps.Timeout` (default 60s) caps the whole `RunTask` call via
  `context.WithTimeout`. Once fired, every in-flight tool's ctx is
  cancelled and partial outcomes are stamped with `FormatToolError(ctx.Err())`.
- `RunTaskDeps.ToolTimeout` (default 30s) caps each individual tool call.
  Tools fan out in parallel within one turn, so the effective wall-clock
  for a turn is bounded by `max(toolTimeouts) + LLM RTT`, not the sum.

### Parallel tool execution

`executor.go` (`agentExecutor`) spawns one goroutine per `llm.ToolCall` and
writes to a fixed-length `results` slice (one slot per call, no append). The
outer ctx is honored via a `select` between `wg.Wait` (via a done channel)
and `ctx.Done()`. Each call's `Arguments` map is cloned shallowly before the
goroutine receives it so a tool that mutates its input map cannot stomp a sibling.

### Tool-allowlist case handling

`CleanToolList` lowercases entries; comparisons at the gate lowercase the
LLM-emitted `call.Name`. A case-mismatched tool name (e.g. `"Web_Fetch"`)
cannot slip past or accidentally fail the gate.

### Iteration exhaustion

When `MaxIterations` is reached, `RunTask` returns:

```
Agent loop stopped after reaching the maximum iteration limit.
Last tool result (truncated):
...first 400 chars of the most recent result...
```

The natural-language synthesis path (`loop.go::finalizeAnswerAfterBudget`)
is a higher-fidelity alternative used by the streaming (`agent.Run`) path.

### Log redaction

`runtask_helpers.go::limitToolContent` truncates oversized tool results in
the `MaxIterationsHit` fallback message. Tool argument values are never
logged (tool argument privacy rule).

## Files

| File | Purpose |
|---|---|
| `task.go` | `Task`, `Result`, `RunTaskDeps` type definitions (the public API contract). |
| `runtask.go` | `RunTask` — stateless one-shot function wrapping the loop. |
| `runtask_helpers.go` | Shared helpers: `initialMessages`, `CleanToolList`, `limitToolContent`. |
| `runtime.go` | `Run` — streaming-capable loop entry used by `chat.Hub`. |
| `loop.go` | `runLoop` — the inner iteration engine (shared by both paths). |
| `executor.go` | `agentExecutor` — parallel tool fan-out with per-tool timeout. |
| `state.go` | `agentState` — message accumulator + `PhantomCorrector`. |
| `no_stream_client.go` | `NoStreamClient` — adapts `llm.Client.Send` to the `ChatClient` interface. |

## Testing

```powershell
go test ./internal/agent/
go test -race ./internal/agent/   # parallel race test must pass
```

## Audit history

- 2026-05-11 — 6-pillar audit of the agent stack: 3 CRITICAL, 8 HIGH, 12
  MEDIUM, 9 LOW + 4 INFO. Findings + fix commits cross-referenced in
  [`.planning/audits/agent-stack-2026-05-11/REVIEW.md`](../../.planning/audits/agent-stack-2026-05-11/REVIEW.md).
- 2026-05-15 — Phase-G: deleted the legacy `runner.go` + `runner_test.go`
  (−806 LOC). All consumers migrated to `agent.RunTask`. Audit finding F-036
  (consolidate duplicate loop bodies) resolved.
