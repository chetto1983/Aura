# internal/agentruntime

Runtime abstractions that sit between `internal/agentloop` and concrete
callers (Telegram, scheduler, future swarm). The package owns:

- `Run(ctx, Invocation)` — the supervisor that emits `Event` lifecycle
  signals around `agentloop.Run` and stamps each call with a correlation
  `run_id`.
- `SessionStore` / `Session` — per-user conversation context lifecycle.
- Terminal-tool finalization helpers (`TerminalToolFinalize`,
  `LooksLike*` predicates, English fallback strings).

## Boundaries

| In | Out |
|---|---|
| `Invocation{ Client, Executor, State, Tools, Logger, ... }` | `Result{ Text, Delivered, Stats, RunID, ... }` + `Event{ EventToolsExposed | EventStats | EventFinal }` callbacks |
| `SessionStore.Begin(userID, cfg)` | `*Session`, was-loaded bool |
| `TerminalToolFinalize(ctx, in)` | LLM-synthesized final answer with internal-marker scrub |

## Public API

```go
result, err := agentruntime.Run(ctx, agentruntime.Invocation{
    Client:        chatClient,
    Executor:      toolExecutor,
    State:         convCtx,
    Tools:         toolDefs,
    Toolset:       "registered",
    Logger:        slog.Default(),
    OnEvent:       func(e Event) { ... },
    Options:       agentloop.Options{ MaxIterations: 8, MaxElapsed: 2 * time.Minute },
})

store := agentruntime.NewSessionStore(userGate)
session, loaded := store.Begin(userID, convCfg)
defer session.Finish()
```

## Behavioural notes

### Run lifecycle + correlation (F-024)

Every `Run` generates an 8-byte hex `run_id`, attaches it to the logger
via `slog.With`, and surfaces it on both `Event.RunID` and `Result.RunID`.
Swarm and multi-conversation deployments can correlate logs across
goroutines without timestamp guesswork.

Structured logs:
- `agentruntime: run start` (tools_exposed count)
- `agentruntime: run end` (elapsed_ms, llm_calls, tool_calls, delivered,
  max_iterations_hit, max_elapsed_hit, error)

### Session concurrency model (F-012, F-016)

`SessionStore` MUST be constructed with a non-nil `*concurrency.UserGate` in
production:

```go
store := agentruntime.NewSessionStore(userGate)
```

The gate serializes message processing per user, so the `*conversation.Context`
owned by a Session is single-writer by construction. `gate == nil` mode shares
one Context across concurrent Begin calls for the same user and is intended
for single-threaded tests only.

### Session lifecycle (F-013, F-014, F-015)

- `Begin(userID, cfg)` lazy-allocates the Context (Load + LoadOrStore-on-miss)
  so a hot user does not allocate a throwaway `*conversation.Context` per
  call. The first cfg wins; subsequent Begin calls ignore later `cfg` values.
- `Finish()` marks a graceful end — silent on the happy path.
- `Abort()` marks a non-graceful end — emits a warn-level log so the
  operator can distinguish "user disconnected" from "we recovered from a
  panic".
- Snapshots survive both Finish and Abort by design: the dashboard and a
  returning user are intentional consumers. `PruneSnapshots(now, retentionDays)`
  is the operator-controlled cleanup; the bot wires it on a maintenance
  cadence. `Clear(userID)` removes both context and snapshot on explicit
  "forget this user" actions.

### Terminal-tool finalization

`TerminalToolPolicy` lets specific tools (terminal-shaped: `write_file`,
`apply_patch`, `execute_shell`, `execute_code`, `create_*`, ...) hand the
final answer to the LLM with `tools=nil`. The helper chain:

1. `TerminalToolFinalizationMessages(messages, terminalTool)` runs the
   shared `agentloop.ApplyGovernance` (microcompact / truncate / orphan
   drop) then appends a synthesis-prompt user message (F-031). Without
   governance the finalize call sends the full accumulated context at the
   moment it is largest — token-budget thrash.
2. `TerminalToolFinalize(ctx, in)` calls `llm.Send` with `tools=nil` and
   guards the response with `LooksLikeToolCallMarkup` / `LooksLikeUnsafeFinalAnswer`.
3. On guard trip → `TerminalToolFallbackResponse(toolName, rawResult)`
   emits a neutral English fallback (F-034). The previous Italian copy was a
   single-language holdover; English is now the default. A future i18n
   integration should route through `internal/i18n` when available.

### Unsafe-content detection (F-032, F-033)

All three predicates share one `unsafeMarkers` table tagged with a
`markerCategory` bitmask. Adding a new marker (e.g. a future `request_id`
internal field) means one entry in one table, not three.

`LooksLikeUnsafeFinalAnswer` no longer flags JSON-shaped text by shape
alone. A response is unsafe only when it carries an internal marker OR is
JSON-shaped AND co-occurs with an internal/tool-call marker. Users who ask
for a JSON answer get JSON.

## Files

| File | Purpose |
|---|---|
| `runner.go` | `Invocation`, `Event`, `Result`, `Run`. Wires agentloop with lifecycle events + run_id correlation. |
| `session.go` | `SessionStore` + `Session`. Per-user conversation context + snapshot persistence. |
| `terminal.go` | Terminal-tool finalization, `LooksLike*` content predicates, English fallback strings, sandbox/file-tool result formatters. |

## Testing

```powershell
go test ./internal/agentruntime/
go test -race ./internal/agentruntime/
```

## Audit history

- 2026-05-11 — 6-pillar audit of the agent stack: 3 CRITICAL, 8 HIGH, 12
  MEDIUM, 9 LOW + 4 INFO. Findings + fix commits cross-referenced in
  [`.planning/audits/agent-stack-2026-05-11/REVIEW.md`](../../.planning/audits/agent-stack-2026-05-11/REVIEW.md).
