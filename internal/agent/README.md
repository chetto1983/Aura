# internal/agent

A small, reusable LLM/tool loop without Telegram coupling. The package's
single export is `Runner`, used by background agents in `internal/swarm` and
by any future caller that wants the agent semantics without the chat
streaming layer.

Distinct from [`internal/agentloop`](../agentloop/README.md) — agent.Runner
has its own loop, its own parallel tool fan-out, and its own budget
exhaustion handling. The two will likely consolidate in a follow-up (see
F-036 in the 2026-05-11 audit), but until then think of agent as the
"background workers" path and agentloop as the "Telegram conversation" path.

## Boundaries

| In | Out |
|---|---|
| `Task{ Prompt, ToolAllowlist, MaxToolCalls, MaxToolResultChars, UserID }` | `Result{ Content, Messages, LLMCalls, ToolCalls, Tokens, Elapsed }` |
| `llm.Client` (Send-only, no streaming) | One assistant message stream |
| `*tools.Registry` | Tool execution (filtered by ToolAllowlist) |

Streaming asymmetry (F-025): the runner is `Send`-only. Background agents
return a single Result; there is no chat to progressively edit. If a caller
ever needs streamed tokens (dashboard live view of a swarm worker), it must
plumb a streaming overload through `Task`.

## Public API

```go
runner, err := agent.NewRunner(agent.Config{
    LLM:           llmClient,
    Tools:         toolRegistry,
    Model:         "claude-opus-4-6",
    MaxIterations: 5,
    Timeout:       60 * time.Second,
    ToolTimeout:   30 * time.Second,
    Logger:        slog.Default(),
})
result, err := runner.Run(ctx, agent.Task{
    SystemPrompt:        "You are AuraBot's swarm worker.",
    Prompt:              "Summarize the last 24h of conversations.",
    ToolAllowlist:       []string{"search_memory", "read_source"},
    MaxToolCalls:        6,
    MaxToolResultChars:  8000,
    UserID:              "42",
})
fmt.Println(result.Content)
```

## Behavioural notes

### Timeout precedence (F-005)

- `Timeout` (default 60s) caps the whole `Run` via `context.WithTimeout`.
  Once fired, every in-flight tool's ctx is cancelled and partial outcomes
  are stamped with `FormatToolError(ctx.Err())` (F-001).
- `ToolTimeout` (default 30s) caps each individual tool call. Tools fan
  out in parallel within one turn, so the effective wall-clock for a turn
  is bounded by `max(toolTimeouts) + LLM RTT`, not the sum.
- A misbehaving tool that ignores its ctx still cannot pin Run open beyond
  `Timeout` because the parallel-fanout wait races `ctx.Done()` (F-001).

### Parallel tool execution

`executeToolCalls` spawns one goroutine per `llm.ToolCall` and writes to a
fixed-length `results` slice (one slot per call, no append). The outer
ctx is honored via a `select` between `wg.Wait` (via a done channel) and
`ctx.Done()`. Each call's `Arguments` map is cloned shallowly before the
goroutine receives it so a tool that mutates its input map (idiomatic
mistake) cannot stomp a sibling.

### Tool-allowlist case handling (F-018)

`cleanToolList` lowercases entries; comparisons at the gate point lowercase
the LLM-emitted `call.Name`. A case-mismatched tool name (e.g.
`"Web_Fetch"`) cannot slip past or accidentally fail the gate.

### Argument validation (F-017)

`call.Arguments` flow directly to `tools.Registry.Execute`. Each tool is
responsible for its own schema validation. The clone protects shared
state, not contents.

### Iteration exhaustion (F-028)

When `MaxIterations` is reached, the runner returns:

```
Agent loop stopped after reaching the maximum iteration limit.
Last tool result (truncated):
...first 400 chars of the most recent result...
```

The natural-language synthesis path (`agentloop.finalizeAnswerAfterBudget`)
is a higher-fidelity alternative and the natural F-036 follow-up.

### Log redaction (F-038)

`redactToolError` scrubs URL query credentials (`?token=`, `?api_key=`,
`?secret=`, `?auth=`, `?bearer=`) and long base64-shaped blobs before
logging a tool error. The LLM still sees the full err via
`FormatToolError`; only logs are sanitized.

## Files

| File | Purpose |
|---|---|
| `runner.go` | The full `Runner` implementation: Config, Task, Result, the Run loop, parallel tool fan-out, tool-budget splitting, governance hooks. |

## Testing

```powershell
go test ./internal/agent/
go test -race ./internal/agent/   # F-037 parallel race test must pass here
```

## Audit history

- 2026-05-11 — 6-pillar audit of the agent stack: 3 CRITICAL, 8 HIGH, 12
  MEDIUM, 9 LOW + 4 INFO. Findings + fix commits cross-referenced in
  [`.planning/audits/agent-stack-2026-05-11/REVIEW.md`](../../.planning/audits/agent-stack-2026-05-11/REVIEW.md).
  Deferred: F-035 (extract `runIteration` from agentloop.Run god-function)
  and F-036 (consolidate `agent.Runner` and `agentloop.Run`) — both
  architectural refactors logged as INFO.
