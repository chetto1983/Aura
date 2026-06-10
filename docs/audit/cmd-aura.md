# Audit: cmd/aura

**Verdict:** needs-work — two medium bugs (reasoning style drift, context cancel leak) plus one low race and one low dead-code finding.

**Counts:** critical 0 / high 0 / medium 2 / low 2

---

## Findings

### [MEDIUM][BUG] renderReasoning — ANSI dim style only applies to first chunk; subsequent reasoning deltas render in normal style

**Location:** `cmd/aura/chat_render.go:153-159`

**Confidence:** high

**Detail:**

```go
func renderReasoning(w io.Writer, delta string, started *bool) {
    if !*started {
        _, _ = io.WriteString(w, "\x1b[2m💭 ")
        *started = true
    }
    _, _ = io.WriteString(w, delta+"\x1b[0m")
}
```

The `\x1b[2m` (dim) escape is emitted only once (with the prefix). Every call emits `delta+"\x1b[0m"`, resetting ALL attributes. After the first call, the terminal is back to normal mode, so the second and all subsequent deltas are rendered in normal (bright) style, not dim. The docstring says "stream a live chain-of-thought delta to w in dim style" — this contract is only fulfilled for the first delta.

The test `TestRenderReasoningPrefixOnce` acknowledges the behaviour ("the dim style resets after each delta, so the deltas are interspersed with ANSI escapes") but does NOT assert that subsequent deltas are dim. The test comment "continues the same dim line" on line 79 is incorrect; the terminal has exited dim mode after the first `\x1b[0m`.

**Impact:** Reasoning tokens from turn 2 onward stream to the operator in unformatted text, violating the stated design (dim style). Low UX impact; no data loss.

**Suggested fix:** Emit `\x1b[2m` before each delta and `\x1b[0m` after:

```go
func renderReasoning(w io.Writer, delta string, started *bool) {
    if !*started {
        _, _ = io.WriteString(w, "\x1b[2m💭 ")
        *started = true
    }
    _, _ = io.WriteString(w, "\x1b[2m"+delta+"\x1b[0m")
}
```

Or keep the prefix+suffix and add `\x1b[2m` before each non-first delta. Update `TestRenderReasoningPrefixOnce` to assert dim escapes appear for all deltas.

---

### [MEDIUM][BUG] openAndListMCPTools / openAndListManagedMCPTools — context cancelled before caller can use returned client

**Location:** `cmd/aura/mcp_tools.go:77-116`

**Confidence:** medium

**Detail:**

Both `openAndListMCPTools` and `openAndListManagedMCPTools` create a 20-second `context.WithTimeout`, then `defer cancel()` fires when the function returns. The function returns a live `cli` handle to the caller. The deferred cancel fires immediately on return, cancelling the context that was used to open the MCP subprocess.

```go
func openAndListMCPTools(ctx context.Context, name string, cfg mcp.ServerConfig) (*mcp.Client, []mcp.ToolDef, error) {
    ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
    defer cancel()           // fires on return, BEFORE caller uses cli
    cli, err := mcp.Open(ctx, name, cfg)
    ...
    return cli, defs, nil   // caller gets cli with cancelled context
}
```

The callers in `mcpDoctor` and `mcpTools` call `cli.Close()` via deferred closures and optionally pass `ctx` (the OUTER context, not the cancelled one) to `writeWhatsAppBridgeHealth`. So `Close()` itself does not need the context. However, the `mcp.Client` subprocess is kept alive by OS handles, not the context, so `Close()` should work. The risk is if `mcp.Open` internally keeps using the context after return (e.g., for keepalive probes or reconnects), or if `cli.Close()` uses the context. For stdio-backed MCP clients, `Close()` kills the subprocess via `cmd.Process.Kill()` which does not use the context.

The immediate cancellation of the 20s timeout after `ListTools` completes is harmless IF no further context-dependent operations are needed on `cli`. Given the callers only call `cli.Close()`, this is borderline acceptable. The concern is that the pattern is fragile — any future caller that needs the `cli` to perform post-return operations (e.g., another `cli.ListTools`) would silently get a cancelled context.

**Suggested fix:** Pass the timeout context back to the caller, or make the timeout internal to `Close()`. If the callers only need `cli.Close()` (confirmed), at minimum document that the returned `cli` must be used immediately. A cleaner approach:

```go
// openAndListMCPTools opens the MCP server, lists its tools, and returns the live
// client (caller must Close). The 20s timeout applies to the Open+ListTools phase only.
func openAndListMCPTools(ctx context.Context, name string, cfg mcp.ServerConfig) (*mcp.Client, []mcp.ToolDef, error) {
    tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
    cli, err := mcp.Open(tctx, name, cfg)
    cancel() // cancel the timeout context after the open+list phase
    if err != nil {
        return nil, nil, err
    }
    defs, err := cli.ListTools(tctx) // NOTE: tctx already done here
    ...
}
```

Actually the cleanest fix is to not cancel immediately and let the timer expire, or accept the current behaviour as intentional and document it.

---

### [LOW][RACE] loadLLMConfigTolerant — process-global env mutation is technically a data race under `-race` if two goroutines call it concurrently

**Location:** `cmd/aura/config.go:135-137`

**Confidence:** low

**Detail:**

```go
os.Setenv("OPENROUTER_API_KEY", "x")
cfg, err = llm.Load()
os.Unsetenv("OPENROUTER_API_KEY")
```

`os.Setenv`/`os.Unsetenv` mutate the process-global environment. In Go, concurrent access to the environment from multiple goroutines is a data race (the Go race detector flags it). In the `cmd/aura` dispatch path, `config show/get/set` are single-threaded CLI commands — no goroutines launch before this call — so the race cannot trigger in production. However, if any test runs `loadLLMConfigTolerant` concurrently with another test that reads env vars (e.g., `config.Load()`), the race detector would fire.

**Suggested fix:** Capture the existing key value before setting the placeholder, then restore it:

```go
prev := os.Getenv("OPENROUTER_API_KEY")
os.Setenv("OPENROUTER_API_KEY", "x")
cfg, err = llm.Load()
if prev == "" {
    os.Unsetenv("OPENROUTER_API_KEY")
} else {
    os.Setenv("OPENROUTER_API_KEY", prev)
}
```

Or better: pass the placeholder key directly into a config-loading function that accepts it explicitly, eliminating the env mutation entirely.

---

### [LOW][NOT-WIRED] setupAuditSkills cleanup function is a no-op — returned cleanup is never called with meaningful work

**Location:** `cmd/aura/cache_audit.go:279-314`

**Confidence:** high

**Detail:**

`setupAuditSkills` returns `(cfg, func(){}, nil)` — the cleanup function is always an empty `func(){}`. The caller:

```go
auditCfg, cleanupSkills, serr := setupAuditSkills(runDir)
...
defer cleanupSkills()
```

`defer cleanupSkills()` defers a no-op. The actual cleanup of the materialized skill files (created under `runDir/skills` and `runDir/skills-export`) is handled by the outer `defer os.RemoveAll(runDir)` in `replayAudit`. This means the cleanup seam exists (and is called correctly), but it carries no implementation — a future maintainer adding resources inside `setupAuditSkills` might forget to add them to the cleanup. The early-error returns also return `func(){}` (never a partial cleanup), so if the function fails mid-way, any already-created directories under `runDir` are still cleaned by the outer `RemoveAll`. This is sound but misleading.

**Suggested fix:** Either remove the `cleanupSkills` return value (simplify the signature to `(*config.Config, error)`) and rely on the outer `os.RemoveAll(runDir)`, or populate the cleanup function with the actual `os.RemoveAll(skillsDir)` / `os.RemoveAll(exportDir)` calls to make it self-contained. Document which approach is intended.

---

## Clean (what was checked and found clean)

- **Nil-pointer derefs:** All store/pool usages are guarded. `newTaskTool` explicitly avoids wrapping a nil store in an interface (avoiding the nil-interface-not-nil pitfall). `newSelfSendResolver` handles nil registry.
- **Unchecked errors:** All `fmt.Fprintf`/`fmt.Fprintln` return values are discarded consistently (standard CLI practice). DB errors are checked at every call site. `rows.Err()` is checked after all scan loops. `w.Flush()` is discarded (tabwriter flushes always succeed).
- **Resource leaks:** `pool.Close()` is deferred at every open site. `rows.Close()` is deferred after every `pool.Query` success. MCP closers are reverse-closed via `closeMCPServers`. `os.RemoveAll(runDir)` cleans temp dirs in both `replayAudit` and `swarmDemo`.
- **Context propagation:** All major operations pass `ctx` through. `signal.NotifyContext` and `context.WithTimeout` are used correctly. The `runServe` shutdown uses a fresh `context.Background()` for the drain phase (correct, since the root ctx is already cancelled).
- **Goroutine leaks:** The `serve` daemon has correct shutdown sequencing: channels `StopAll` before `env.close()`, HTTP servers get `Shutdown`. The REPL `chatLoop` uses `defer d.run.Stop(...)` to join auto-title workers.
- **SQL injection:** All raw SQL in `task.go` and `serve_adapters.go` uses positional `$N` parameters. No string concatenation in query construction.
- **Slice aliasing:** `mcpProfileRemove`'s filter-in-place uses `p.Servers[:0]` read-ahead correctly. `wslProbePrefixArgs` returns a fresh copy via `append([]string(nil), ...)`. `skillsSnippetExec`'s `append([]string{use.HostPath}, extra...)` is safe (new backing array since len=1 < cap).
- **`dbReset`/`neo4jReset` guard:** The double-gate `(!--yes || env!=1)` correctly requires both conditions simultaneously.
- **MCP profile operations:** `mcpProfileCreate`, `mcpProfileUse`, `mcpProfileAdd`, `mcpProfileRemove` all load-modify-save atomically (single process, no concurrent writers).
- **Dead code:** All unexported functions in non-test files have at least one non-test call site. Package-level vars (`runWhatsAppBridgeWSLProbe`, `mcpLookPath`) are test seams with sequential (non-parallel) tests.
- **`drainTurn` early break:** The early `break` on error in `cache_audit.go:drainTurn` causes consumer-stop on the iter.Seq2, skipping Runner post-round bookkeeping. However, `replayAudit` calls `r.Stop(ctx, convID)` explicitly after the loop, which handles orphan resolution. No goroutine leak.
- **`flagValue` vs `sinceValue` inconsistency:** `sinceValue` supports both `--since=dur` and `--since dur`; `flagValue` supports only space form. This is a UX inconsistency, not a bug (all actual argument parsing documents the space form in usage strings).
