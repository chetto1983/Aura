# Proposed Patches — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc`
Patch-style recommendations for the major findings. **None are applied** — this is design guidance. Snippets are illustrative pseudocode anchored to real symbols; adapt to exact signatures when implementing. Each patch lists: affected file · affected function · reason · before/after behavior · implementation approach · tests required · rollback.

---

## PP-1 — Panic firewall in spawned goroutines (AG-001, P0)

- **Affected file(s):** `internal/agent/llm_agent_parallel.go`, `internal/agent/workflow/parallel.go`, `internal/swarm/swarm.go`, `internal/agent/tools/shell_bg.go`
- **Affected function(s):** `executeBatch`, `runSub`, `runWave`, `bgShell.start` reaper
- **Reason:** A panic in any spawned goroutine crashes the whole process; `recover()` in the parent goroutine cannot catch it. For `aura serve` this is daemon-wide.
- **Before:** `go func(k int){ …; results[k]=a.runTool(...) }(k)` — a panicking tool kills the process.
- **After:** the panic becomes a per-call `toolRunResult{Err:"panic: …"}` the model sees; the daemon survives.
- **Approach:**
  ```go
  // executeBatch goroutine body:
  go func(k int) {
      sem <- struct{}{}
      defer func() { <-sem }()
      defer wg.Done()
      defer func() {
          if r := recover(); r != nil {
              recordPanic("executeBatch", calls[k].Function.Name)
              results[k] = toolRunResult{
                  ToolCallID: calls[k].ID, ToolName: calls[k].Function.Name,
                  Err: fmt.Sprintf("panic: %v", r),
                  Preview: "error: tool panicked",
                  EndedAt: time.Now().UTC(),
              }
          }
      }()
      results[k] = a.runTool(ctx, budget, calls[k], startedAt)
  }(k)
  ```
  Mirror in `runSub` (forward a `{status:failed}` child report once) and `runWave`. Add a Runner-level `defer recover()` around the per-turn `Run` consumption as a backstop. **Do not** rely on this for AG-002 (concurrent-map-write is a fatal, not a panic).
- **Tests required:** `-race`+`goleak` table test with a panicking fake tool through `executeBatch`, `parallel.Run`, `swarm.runWave`; assert process survives + error surfaces.
- **Rollback:** purely additive defers; revert the commit. No behavior change on the happy path.

## PP-2 — Mutex on the dedup ring (AG-002, P1)

- **Affected file:** `internal/agent/budget_dedup.go`
- **Affected function:** `dedupRing.BeforeToolCall`, `AfterToolResult`, `push`, `countConsecutive`, `isPingPong`
- **Reason:** `entries`/`results` are mutated lock-free under a cross-file "serial caller" convention adjacent to concurrent `executeBatch`; a future change → fatal concurrent map write.
- **Before:** no lock; safe only by convention.
- **After:** all ring mutations guarded; safe regardless of caller.
- **Approach:** add `mu sync.Mutex` to `dedupRing`; `Lock`/`defer Unlock` at the top of each exported method. Keep `Budget.Child`'s per-branch ring fork.
- **Tests required:** `-race` concurrent `Before/AfterToolResult` hammer + multi-tool `dispatch` with `>1` parallel tools.
- **Rollback:** remove the mutex; revert. No API change.

## PP-3 — MCP reconnect resilience (AG-005/AG-006, P1)

- **Affected file:** `internal/agent/mcptools/bridge_reconnect.go`, `bridge.go`, `timeout.go`
- **Affected function:** `reconnectAfterTransport`, `reconnectLocked`, `Execute`, `configuredMCPCallTimeout`
- **Reason:** lock held across spawn+handshake+list; reconnect bound to the failed call's ctx; no backoff/breaker; `=0` disables the timeout.
- **Before:** one transport blip freezes the server; a crash-loop re-spawns every call; `=0` → unbounded hang.
- **After:** reconnect is single-flight off-lock with its own deadline; backoff + breaker bound a flapping server; `=0` means default.
- **Approach:**
  ```go
  // reconnect outside s.mu:
  defs, err, _ := s.reconnectGroup.Do("reconnect", func() (any, error) {
      rctx, cancel := context.WithTimeout(context.WithoutCancel(parent), reconnectTimeout)
      defer cancel()
      if s.breaker.Open() { return nil, ErrTransport }
      c, defs, err := openMCPClient(rctx, s.cfg)         // spawn+handshake+list, no s.mu held
      if err != nil { s.breaker.Failure(); backoffSleep(); return nil, err }
      s.mu.Lock(); s.client = c; s.mu.Unlock()           // publish under lock only
      s.breaker.Success(); return defs, nil
  })
  // timeout.go: sec==0 -> return defaultMCPCallTimeout; sec<0 -> infinite (explicit)
  ```
  Resolve+validate the timeout once at mount, store on the server.
- **Tests required:** concurrent-call-during-slow-reconnect (no head-of-line stall); crash-loop server (breaker opens after N); hung server with `=0` bounded by default; goleak.
- **Rollback:** feature-flag the new reconnect path; revert to the in-lock reconnect if regressions appear.

## PP-4 — Command-hook sandbox + fail-soft (AG-003/AG-004, P1)

- **Affected file:** `internal/agent/hooks_command.go`, `hooks.go`
- **Affected function:** `run`, `verifyTrust`, `BeforeModel`, `HookManager.*`
- **Reason:** TOCTOU between hash and exec; full `os.Environ()` (secrets) to the child; unvalidated wholesale request rewrite; any hook error aborts the turn.
- **Before:** `cmd.Env = append(os.Environ(), h.env...)`; hash-then-exec; `*req = *decision.Request`; error → turn dies.
- **After:** minimal env; exec the held fd; validated/bounded rewrites with an audit record; non-security hook failures degrade (fail-open) instead of aborting.
- **Approach:**
  ```go
  // minimal env:
  cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, h.env...)
  // close TOCTOU: open once, hash the fd, exec /proc/self/fd/N (or require root-owned path)
  // validate rewrite:
  if decision.Request != nil {
      if err := validateHookRequest(decision.Request); err != nil { return nil, err }
      reasoningtrace.Record("hook_rewrite", map[string]any{"hook": h.name, "kind": "request"})
      *req = *decision.Request
  }
  // HookManager: per-hook FailPolicy; on err under fail_open -> log+metric+allow
  ```
- **Tests required:** child env has no secret-named vars; oversized/invalid rewrite rejected; failing hook completes under fail_open, aborts under fail_closed; rewrite emits an audit record.
- **Rollback:** the env-allowlist and fail-policy are config-gated; default the policy to today's behavior behind a flag if needed, then flip.

## PP-5 — Secret boundary: DSN-aware redaction (AG-010, P1)

- **Affected file:** `internal/secret/envkey.go`, `internal/agent/tools/shell_exec_env.go`
- **Affected function:** `IsSecretEnvKey`, `secretEnvMarkers`, output redactor
- **Reason:** `AURA_DB_URL=postgres://u:PASS@h` passes the substring denylist and reaches shell children.
- **Before:** denylist = `key,token,secret,pass,auth,bearer,credential,private,cert`.
- **After:** DSN-shaped keys are recognized; DSN credentials in output are masked.
- **Approach:**
  ```go
  secretEnvMarkers = append(secretEnvMarkers, "url", "dsn", "uri", "conn", "pwd", "cookie", "session", "jwt")
  // plus a value-scan: if value matches `^[a-z][a-z0-9+.-]*://[^:/?#]+:[^@/?#]+@`, treat as secret
  // output redactor: add pattern ://([^:@/]+):([^@/]+)@  -> ://$1:***@
  ```
  Beware false-positives: `url`/`uri` are broad — pair the name marker with the value-scan so only credential-bearing values are stripped/redacted.
- **Tests required:** `IsSecretEnvKey("AURA_DB_URL")==true`; a shell child cannot read the DSN; redactor masks `postgres://u:p@h`; a non-credential `*_URL` is not over-redacted.
- **Rollback:** revert the marker list; the value-scan is independent and can be toggled.

## PP-6 — Observability minimum (AG-012/AG-013/AG-033, P1)

- **Affected file:** `internal/agent/metrics.go`, `tracing.go`, `llm_agent.go` (+ dispatch/finalize emit points)
- **Affected function:** new metric registrations; `mintSpanID`; turn/tool emit points
- **Reason:** no latency/error/cost/outcome metrics; no slog; telemetry can panic.
- **Before:** ~5 counters; span-only latency; `mintSpanID` panics on entropy failure.
- **After:** full metric set + slog + never-panic telemetry.
- **Approach:**
  ```go
  // metrics.go (non-default registry):
  turnTotal = factory.NewCounterVec("aura_agent_turn_total", []string{"outcome"})
  llmDur    = factory.NewHistogram("aura_agent_llm_call_duration_seconds", buckets)
  llmErr    = factory.NewCounterVec("aura_agent_llm_errors_total", []string{"kind"})
  toolErr   = factory.NewCounterVec("aura_agent_tool_errors_total", []string{"tool"})
  // emit: at each terminal turnReason -> turnTotal.WithLabelValues(turnReason).Inc()
  // tracing.go mintSpanID:
  if _, err := rand.Read(id[:]); err != nil { recordSpanIDFailure(); return [8]byte{} }
  // slog at turn start / terminal / tool error keyed by request_id, thread_id
  ```
- **Tests required:** each terminal `turnReason` increments its counter; a tool error increments `tool_errors_total`; injecting an entropy failure does not panic.
- **Rollback:** metrics/logs are additive; revert. The `mintSpanID` change is strictly safer.

## PP-7 — Capability gate on mutating tools (AG-007/AG-011, P1)

- **Affected file:** `internal/agent/llm_agent_dispatch.go`, `internal/agent/tools/skill_write.go`, `tools/skill.go`
- **Affected function:** `dispatch` (pre-exec), `writeAction`
- **Reason:** mutating MCP tools and self-authored skills run with no per-call authorization.
- **Before:** `Mutating` only sets `sideEffected`; skill auto-activates.
- **After:** `Mutating && Untrusted` consults `capability_grants`; skill activation gated or honestly documented + alerted; dead schema removed.
- **Approach:**
  ```go
  // dispatch, before executeBatch, for each runnable call:
  if spec.Mutating && provenance == TrustUntrusted {
      switch grants.Authorize(ic.Ctx, call, spec) {
      case Deny:    vetoResults[i] = denied(call); continue
      case Confirm: return askUserConfirm(call)   // route through ask_user
      case Allow:   // proceed
      }
  }
  // skill_write.go: delete skillParamsSchema; if ungated-by-policy, emit operator alert + audit
  ```
- **Tests required:** mutating MCP tool without a grant refused/confirmed; self-authored skill stays pending or alerts; exactly one skill schema referenced.
- **Rollback:** the gate defaults to `Allow` when no grants are configured (today's behavior) — safe to ship dark, then tighten.

## PP-8 — Default-untrusted provenance for unknown tools & swarm reports (AG-052, P1 in deployment)

- **Affected file:** `internal/agent/trust.go`, `internal/swarm/runner_adapter.go`
- **Affected function:** `untrustedSource`, swarm report projection
- **Reason:** unknown-tool output and `swarm_spawn` child reports (which embed children's web/fs output) are treated as trusted and not enveloped → indirect prompt-injection laundering.
- **Before:** hardcoded `untrustedToolNames` set; default = trusted.
- **After:** default = untrusted unless explicitly marked trusted; swarm reports carry untrusted provenance.
- **Approach:** invert the default in `untrustedSource` (unknown → true), keep an explicit `trustedToolNames` allowlist for built-ins that are genuinely safe; stamp `Provenance.Trust = TrustUntrusted` on swarm child reports so the parent wraps them.
- **Tests required:** a swarm child's malicious web content is enveloped in the parent prompt; a built-in safe tool stays trusted.
- **Rollback:** revert the default; the allowlist remains a no-op.

## PP-9 — Config validation + active wallclock (AG-036/AG-006/AG-027/AG-041, P2)

- **Affected file:** `internal/agent/budget.go`, `internal/agent/mcptools/timeout.go`, composition root
- **Affected function:** `NewBudget`, `configuredMCPCallTimeout`, run-ctx setup
- **Reason:** `max_steps=0` silently disables the runtime; MCP timeout parsed per-call (malformed = loop-fatal); wallclock is a soft step-boundary gate.
- **Before:** no positivity check; hot-path env parse; `WithDeadline` unwired.
- **After:** fail-loud at boot on bad config; timeout resolved once; wallclock an active ctx deadline.
- **Approach:** `if maxSteps < 1 || wallclockSec < 1 { return errMalformed }` in `NewBudget`; resolve MCP timeout at mount; `ic.Ctx = Budget.WithDeadline(parent)` at the run root.
- **Tests required:** `NewBudget(0)`/negative errors; a malformed MCP timeout fails at boot not mid-run; total wall-time is bounded.
- **Rollback:** the validations are independent; revert individually.

## PP-10 — fs size cap (AG-014, P2)

- **Affected file:** `internal/agent/tools/fs_read.go`, `fs_write.go`, `fs_edit.go`
- **Affected function:** `FSRead.Execute`, `FSWrite.Execute`, `FSEdit.Execute`
- **Reason:** no size ceiling → OOM/turn-wedge on a multi-GB file.
- **Before:** `os.ReadFile` whole file; `fs_read` also `string(b)` = 2× memory.
- **After:** files over `AURA_FS_MAX_READ_BYTES` are rejected with a clear error (or auto-paged via offset/limit).
- **Approach:** `os.Stat` then reject over cap, or `io.LimitReader` with a hard limit; `fs_read` suggests `offset`/`limit` paging in the error.
- **Tests required:** a file over the cap returns a clean error, not an OOM; under-cap reads unaffected.
- **Rollback:** raise the cap to `MaxInt` to disable; revert.

---

## Patch sequencing

PP-1 → PP-2 (crash firewall) → PP-3 (MCP) → PP-5 → PP-6 (secret + visibility) → PP-4 → PP-7 → PP-8 (gating) → PP-9 → PP-10 (hardening). PP-1 and PP-2 are the smallest and remove the two crash-class defects — land them first, each behind its own atomic commit with the named regression test.
