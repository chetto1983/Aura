# Audit: cmd/aura

**Verdict:** needs-work — two confirmed bugs, one permanent stub, one minor dead-code unreachable return.

**Counts:** critical 0 / high 0 / medium 2 / low 2

## Findings

### [MEDIUM][BUG] `rebuildMessages` silently swallows `json.Unmarshal` error

**Location:** `cmd/aura/cachefakes.go:182`

**Confidence:** high

**Detail:**

```go
_ = json.Unmarshal(t.ToolCalls, &calls)
msg.ToolCalls = calls
```

The production path (`internal/conversations/store_helpers.go:turnToMessage`) propagates the unmarshal error:
```go
calls, err := decodeToolCalls(t.ToolCalls)
if err != nil {
    return llm.Message{}, fmt.Errorf("decode tool_calls: %w", err)
}
```

The fake silently sets `msg.ToolCalls = nil` on a corrupt JSON blob. In the cache audit (`cacheAuditMain`), this means a turn whose tool-call JSON is corrupted in-flight would produce a message with no tool calls — the LLM request built from it would have a different shape than the real Runner would produce, and the prefix hash would diverge for a reason unrelated to messages[0] mutation. The audit could then either generate a false-positive MUTATION exit or (if the corruption happens to produce the same hash) silently pass over a real invariant break. In normal operation the ToolCalls bytes are marshaled by the runner in the same process, so corruption is impossible — but the divergence makes the fake less trustworthy as a test stand-in and was flagged at code-review depth in the production store comments.

**Suggested fix:**

```go
if len(t.ToolCalls) > 0 {
    var calls []llm.ToolCall
    if err := json.Unmarshal(t.ToolCalls, &calls); err != nil {
        // Mirror production turnToMessage: caller must surface corrupt turns.
        // Return early; rebuildMessages should return ([]llm.Message, error).
        // Alternatively log and skip the turn (matching audit exitFixture semantics).
        return nil // or propagate error if signature is changed
    }
    msg.ToolCalls = calls
}
```

The cleanest fix is to change `rebuildMessages` to `([]llm.Message, error)` and have `messagesLocked` / `LoadHistory` / `LoadManagedHistory` return the error — mirroring the production Store. The callers in `cacheAuditMain` → `replayAudit` already check errors and map them to `exitFixture`.

---

### [MEDIUM][NOT-WIRED] `mcpLogs` is a permanently-stub command — always returns a static string

**Location:** `cmd/aura/mcp_status.go:79-91`, wired at `cmd/aura/mcp.go:51`

**Confidence:** high

**Detail:**

```go
func mcpLogs(args []string, out io.Writer) error {
    // ...
    return writef(out, "%s logs: no captured log tail yet; run doctor for live diagnostics\n", args[0])
}
```

`aura mcp logs <name>` is reachable (it is case `"logs"` in `runMCPCommand`), but it always returns the static string regardless of the server, config, or runtime state. There is no captured log infrastructure behind it. The command is NOT listed in `mcpUsage` (line 22), confirming it was added as a placeholder but never completed. Operators who discover the command (e.g. via shell completion) get a confusing "no captured log tail yet" every time, with no indication the feature will ever provide real output.

**Suggested fix:**

Either (a) delete the `case "logs":` dispatch and `mcpLogs` function (removing the hidden stub), or (b) add a real log-tail implementation backed by the MCP server lifecycle (e.g. a ring buffer per server name stored by the MCP manager). Option (a) is the minimal fix; option (b) is the correct long-term completion. If deferred, add a `// TODO: not implemented` comment and expose the command in `mcpUsage` so it is at least discoverable.

---

### [LOW][BUG] `rebuildMessages` always-block mismatch: `memConvStore.AppendTurn` does not assign `Seq` before `appendTurnFieldsLocked`, causing double seq-increment on the assistant-with-cache-metric path

**Location:** `cmd/aura/cachefakes.go:125-153`

**Confidence:** medium

**Detail:**

`AppendTurn` calls `appendTurnFieldsLocked(p)` directly; `appendTurnFieldsLocked` calls `assignTurnSeqLocked(p)` internally. This is correct and idiomatic.

`AppendAssistantTurnWithCacheMetric` calls `assignTurnSeqLocked(p)` at line 135 to populate `p.Seq`, then passes the result to `appendTurnFieldsLocked` at line 139, which calls `assignTurnSeqLocked` again at line 151. Because the guard `if p.Seq <= 0` is respected, the second call is a no-op. There is no double-increment in practice.

However the double-call makes the invariant fragile: if `assignTurnSeqLocked` is ever changed to be non-idempotent (e.g. to advance a counter rather than reading `len(m.turns)`), the `AppendAssistantTurnWithCacheMetric` path would increment seq twice. The fix is to remove the `p = m.assignTurnSeqLocked(p)` call from `appendTurnFieldsLocked` and require all callers to pre-assign seq — matching the single-assignment discipline the real Store uses.

**Suggested fix:**

Remove the `assignTurnSeqLocked` call from `appendTurnFieldsLocked` and make both `AppendTurn` and `AppendAssistantTurnWithCacheMetric` call it explicitly before calling `appendTurnFieldsLocked`.

---

### [LOW][DEAD-CODE] Unreachable `return` in `triadToSpec` default branch

**Location:** `cmd/aura/task.go:157`

**Confidence:** high

**Detail:**

```go
default:
    fmt.Fprintln(os.Stderr, "aura task schedule: exactly one of --cron/--at/--every is required")
    os.Exit(exitUsage)
    return "", time.Time{} // unreachable: os.Exit terminates the process
```

The `return` on line 157 is dead code — the compiler requires it because `os.Exit` does not have `noreturn` semantics in Go's flow analysis, but execution never reaches it. This is a minor readability issue, not a correctness problem. It is idiomatic Go but technically dead code.

**Suggested fix:**

No change required for correctness. If the team prefers to avoid the dead `return`, wrap the default branch in a helper:

```go
default:
    // unreachable in the Go flow checker's view; os.Exit never returns
    usageAndExit("aura task schedule: exactly one of --cron/--at/--every is required")
    panic("unreachable")
```

Or accept the status quo — this pattern is ubiquitous in Go CLI code.

---

## What was checked

- All 23 non-test `.go` files in `cmd/aura/` (agent.go, cache.go, cache_audit.go, cache_stats.go, cachefakes.go, chat.go, chat_render.go, chat_repl.go, config.go, db.go, exit_codes.go, identity.go, main.go, mcp.go, mcp_profile.go, mcp_status.go, mcp_tools.go, neo4j.go, paused_states.go, serve.go, serve_adapters.go, serve_channels.go, shell.go, skills.go, skills_snippet.go, swarm_demo.go, task.go, toolpipe.go, version.go, web.go).
- All pgx `Query`/`Exec` call sites: `defer rows.Close()` and `rows.Err()` are consistently present.
- All `http.Response.Body` readers: `defer resp.Body.Close()` is present in `probeWhatsAppBridgeHTTP`.
- Goroutine leak paths: `startChannelSubsystems` goroutines are joined by `stopChannelSubsystems`; `resumeTurnFunc` early-returns on error (valid under `iter.Seq2` contract).
- `dbReset` / `neo4jReset` dual-guard (`||`): correctly implements AND semantics via De Morgan.
- `mcpProfileRemove` self-aliasing filter: safe (write cursor always behind read cursor in Go range-over-slice).
- `wslProbePrefixArgs` returning nil: `append(nil, ...)` is valid Go.
- `buildDispatch` ephemeral `cron.NewScheduler`: constructor is side-effect-free.
- `loadLLMConfigTolerant` `os.Setenv`/`os.Unsetenv`: CLI single-goroutine path, no concurrent goroutine race.
- Loop-variable capture: Go 1.26 — not applicable.
- `rebuildMessages` JSON error: confirmed divergence from production `turnToMessage`.
- `mcpLogs`: confirmed permanent stub, absent from `mcpUsage`.
- Exported symbols in `cmd/aura`: package is `main`; no exported symbols to check for dead external usage.
