# Bug Report — `internal/agent`

**Audit cycle:** 2026-06-15 · **HEAD:** `136325dc` · **Branch:** `master`
**Audited path:** `d:\Aura\internal\agent` (loop, tools, mcptools, workflow, prompt, hooks, tracing, budget) + verified cross-package call-sites in `internal/secret`, `internal/web`, `internal/swarm`, `internal/reasoningtrace`, `internal/db`.
**Method:** Independent evidence-based pass — 4 parallel deep-dive readers (tools/security, MCP, prompt/hooks/observability, workflow/swarm/budget) + a manual line-by-line read of the core loop (`llm_agent.go`, `_dispatch`, `_finalize`, `_completion`, `_pause`, `_stream_retry`, `_consume`, `hooks_command.go`, `prompt.go`). Every claim cites `file:line`; unverifiable items are marked **NEEDS CONFIRMATION**.

> **Threat-model calibration.** The runtime makes a *documented* design choice (amendment #50 / D-15c): for a single trusted operator on their own machine, the host shell + filesystem **are** the capability — there is no sandbox and no path fence. `shell_exec` / `fs_write` arbitrary execution is the **intended capability, not a bug**, and is excluded from findings as such. Findings are graded against (a) that single-operator model and (b) the *deployment reality* that the same binary runs as a long-lived **multi-channel `serve` daemon** (Telegram + AG-UI + scheduler), which changes the blast radius of crashes and the relevance of cross-turn isolation.

> **Continuity.** A prior multi-cycle audit (2026-06-10 → 06-13) used the `B-##/O-##/D-##/M-##/R-##` scheme; its dated cycle docs (`reconciliation-2026-06-13.md`, `re-audit-2026-06-12.md`, validation notes) are preserved in this directory. This cycle re-derived findings independently and uses a fresh `AG-###` scheme; where a finding corresponds to a prior one it is cross-referenced (e.g. *prior B-04*). The internal sub-IDs (`LP-1`, `F-17`, `AUR-01`, …) are this cycle's deep-reader IDs, kept for traceability.

---

## Severity summary

| Severity | Count | Definition |
|---|---|---|
| **P0** | 1 | Critical: system-wide failure / unsafe execution / data loss |
| **P1** | 12 | Serious reliability/correctness/security — fix before production |
| **P2** | 26 | Important maintainability/observability/architecture |
| **P3** | 24 | Improvement / cleanup / future hardening |
| **Total actionable** | 63 | (excludes ~15 confirmed-good / no-fix observations) |

The agent **core reasoning loop is genuinely strong** — see `architecture-review.md` for the positives (bounded recovery counters, non-empty terminal contract, KV-cache discipline, SSRF hardening, untrusted-output nonce wrapping, shared-atomic budget that defeats the `max_steps^depth` fan-out bomb). The findings concentrate in the **operational perimeter**: panic isolation, MCP resilience, hook safety, observability, and secret hygiene.

---

# P0 findings

## [P0] AG-001 — Unrecovered panics in spawned goroutines crash the entire daemon

- **Evidence:**
  - File: `internal/agent/llm_agent_parallel.go`
  - Location: `executeBatch`, lines 43–50 — `go func(k int){ sem<-{}; defer <-sem; defer wg.Done(); results[k]=a.runTool(...) }`
  - Also: `internal/agent/workflow/parallel.go:95–100,137` (`eg.Go` → `runSub`; errgroup does **not** recover panics); `internal/agent/tools/shell_bg.go:174–178` (background reaper goroutine on `context.Background()`); `internal/swarm/swarm.go` `runWave` (per-child goroutines).
  - Relevant component: parallel tool dispatch, workflow fan-out, background shells, swarm.
  - Cross-check: a repo-wide grep confirms the **only** `recover()` in production (non-test) code in `internal/` is `internal/db/tx.go:28` (a DB-rollback guard). There is **zero panic recovery in any agent/swarm goroutine.**
- **Problem:** Tool `Execute` bodies are arbitrary (`shell_exec`, `web_fetch`, MCP bridge JSON decode, `swarm_spawn`, skill writers). A `nil`-deref, a slice out-of-range in a decoder, or an MCP bridge fault inside any goroutine spawned by `executeBatch`/`runSub`/`runWave` panics. A `recover()` in the *parent* goroutine cannot catch a panic in a *child* goroutine — so the panic unwinds past the goroutine boundary and terminates the process.
- **Impact:** For the CLI one-shot this is "the turn crashes." For the **`serve` daemon** — the production shape that fans Telegram + AG-UI + scheduler turns through the same process — one panicking tool call in **one** user's turn takes down the process for **all** users and channels. That is the prompt's P0 definition: *system-wide failure*.
- **Reproduction / failure scenario:** A tool whose `Execute` panics (e.g. a malformed MCP server response that trips a `nil` map access in a custom bridge, or a future tool with an unchecked index) is dispatched in a multi-tool turn; `executeBatch` spawns it in a goroutine; the panic crashes `aura serve`; every active Telegram/AG-UI session drops.
- **Recommended fix:** Wrap every spawned goroutine body in `defer func(){ if r:=recover(); r!=nil { /* convert to a per-call error preview / failed child report */ } }()`. In `executeBatch` translate the recovered panic into a `toolRunResult{Err:"panic: …"}` so the model sees an error preview; in `parallel.go`/`runWave` translate to a `{status:failed}` child report forwarded once. Add a top-level recover in the Runner's per-turn goroutine as a backstop. **Note:** a concurrent-map-write (AG-002) is a Go *fatal*, not a panic, and `recover()` will **not** save it — fix AG-002 separately.
- **Suggested test coverage:** A `-race` + table test that dispatches a deliberately-panicking fake tool through `executeBatch` and `parallel.Run`, asserting the process survives and the panic surfaces as a per-call/per-child error. Add to `goleak`-guarded `TestMain`.

---

# P1 findings

## [P1] AG-002 — `dedupRing` mutated without a mutex; race-free only by unenforced convention

- **Evidence:** `internal/agent/budget_dedup.go` — `BeforeToolCall` (122–162), `AfterToolResult` (170–192) mutate `r.entries` (slice) and `r.results` (map) with no lock. Concurrency contract documented in `llm_agent_parallel.go:24–27`.
- **Problem:** The ring is mutated only by `dispatch`'s **serial** pre/post loops (`llm_agent_dispatch.go:52,104`), while `executeBatch` runs `runTool` **concurrently**. So today it is race-free — but the safety is an unenforced architectural convention spanning three files. A future change that moves a dedup call inside `runTool`, or shares a ring across parallel branches, introduces a concurrent map write.
- **Impact:** A concurrent map write is a Go **runtime fatal** (`fatal error: concurrent map writes`), not a recoverable panic — `recover()` cannot save it (so AG-001's fix does not cover this). Latent crash-class race directly in tension with the existence of `executeBatch`.
- **Reproduction / failure scenario:** Any refactor that lets two goroutines touch one `Budget`'s ring concurrently → intermittent fatal under load; near-impossible to reproduce in a debugger.
- **Recommended fix:** Add a `sync.Mutex` to `dedupRing` guarding `entries`/`results` (cheap; removes the cross-file coupling). `Budget.Child` already forks a distinct ring per parallel branch — keep that; the mutex is belt-and-suspenders.
- **Suggested test coverage:** A `-race` test fanning a multi-tool turn through `dispatch` with `>1` parallel tools, plus a direct concurrent `BeforeToolCall`/`AfterToolResult` hammer test.

## [P1] AG-003 — Command hook: exec TOCTOU + full `os.Environ()` (secrets) handed to subprocess + unvalidated request rewrite

- **Evidence:** `internal/agent/hooks_command.go` — `verifyTrust` hashes the file at line 206 (`fileSHA256`); `run` execs it at line 182 (`exec.CommandContext`) → TOCTOU window. Line 183: `cmd.Env = append(os.Environ(), h.env...)`. `BeforeModel` line 114: `*req = *decision.Request` (wholesale request replacement from hook stdout, no validation/size cap).
- **Problem:** Three compounding issues. (1) The `//nolint:gosec` comment asserts atomicity the OS does not provide — an attacker who can write the hook path can swap the binary between hash and `execve`. (2) Every hook subprocess inherits the **entire** process environment, including `OPENROUTER_API_KEY`, `TELEGRAM_BOT_TOKEN`, `POSTGRES_PASSWORD`, `NEO4J_PASSWORD`. (3) A hook's stdout JSON can replace the whole `llm.Request` (model, messages, tools), inject an assistant answer, or rewrite a tool call — with **no schema/size validation and no audit log** of what it changed.
- **Impact:** Within the single-operator model these are *capabilities* (a hook is operator-configured and SHA-gated). But the env exposure is a broad defense-in-depth hole (the rest of the codebase works hard to redact those exact secrets — see AG-009/AG-010), and a hook fault/compromise silently rewrites prompts/answers/tool-calls. **In any multi-tenant deployment these become P0.**
- **Reproduction / failure scenario:** A configured hook on a writable path; or a hook that reads `$POSTGRES_PASSWORD` and exfiltrates it; or a buggy hook that emits an oversized `Request` accepted unconditionally.
- **Recommended fix:** Default `cmd.Env` to a minimal allowlist (`PATH` + explicit `cfg.Env`); never inherit provider/DB secrets. Open the file once and exec the held fd (Linux `/proc/self/fd/N`) or require the hook binary on a root-owned, non-operator-writable path. Validate hook-supplied `Request`/`ToolCall`/`ToolResult` against bounds (message count, byte size, model allowlist) and `reasoningtrace.Record` every rewrite/deny.
- **Suggested test coverage:** Assert (a) secret-named env vars absent from the child env, (b) an oversized/invalid hook `Request` is rejected, (c) a rewrite emits an audit record.

## [P1] AG-004 — A failing/misconfigured hook aborts the entire turn (no fail-soft policy)

- **Evidence:** `internal/agent/hooks_command.go` (97–170), `internal/agent/hooks.go` (85–124), `internal/agent/llm_agent.go:213–216,429–432`, `internal/agent/llm_agent_dispatch.go:39,90`. Any hook error returns a Go `error`; `HookManager` propagates the first; `Run`/`dispatch` surface it on the `iter.Seq2` error slot → turn dies.
- **Problem:** A hook timeout (2s default), a non-zero exit, an unparseable decision, an `unknownDecisionError`, or a trust-hash mismatch (operator updated the hook binary but not its configured SHA) all hard-abort the user's turn. There is no `fail_open`/`fail_closed` knob. This contrasts with the deliberate fail-soft everywhere else (breaker-open → finalize, memory unavailable → continue, critic broken → fail open).
- **Impact:** A single misconfigured or transiently-slow plugin takes down the agent loop with a raw error to the user — a plugin fault should be containable.
- **Recommended fix:** Per-hook failure policy (`fail_open` default for non-security hooks): on error, `reasoningtrace.Record` + metric + treat as `allow`; reserve hard-abort for explicitly security-gating hooks. Wrap each hook call in `recover()` (ties to AG-001).
- **Suggested test coverage:** Hook returns error/timeout/panic under `fail_open` → turn completes; under `fail_closed` → turn aborts with a clear reason.

## [P1] AG-005 — MCP reconnect holds the lock across spawn+handshake, binds to the failed call's ctx, and has no backoff/circuit-breaker

- **Evidence:** `internal/agent/mcptools/bridge_reconnect.go` — `reconnectAfterTransport`/`reconnectLocked` (~97–155) call `openMCPClient` (subprocess spawn + `initialize` handshake) **and** `ListTools` while holding `s.mu`; `currentClient()`/`Close()` also take `s.mu`. `bridge.go:70–81` derives `callCtx` from the per-call timeout and passes it into reconnect.
- **Problem:** (F-1) During a reconnect every other in-flight/incoming call to that server blocks on `s.mu`, including shutdown — one transport blip freezes the whole server's tool surface for the spawn+handshake duration. (F-2) Reconnect reuses the failed call's ctx, so a short per-call timeout (or a cancelled turn) aborts the *shared* server recovery. (F-3) There is **no backoff and no circuit breaker** — a crash-looping server is re-spawned on every single tool call forever (PID/fd/CPU churn on the shared mini-PC).
- **Impact:** Head-of-line blocking + recovery livelock under load; a permanently-dead server is hammered indefinitely.
- **Recommended fix:** Do the expensive reconnect outside the lock via `singleflight` (or `reconnecting bool` + `sync.Cond`), then swap `s.client` under the lock. Reconnect with `context.WithoutCancel(parent)` + a dedicated reconnect timeout. Add exponential backoff with a ceiling and an N-consecutive-failure circuit breaker with a cooldown.
- **Suggested test coverage:** Concurrent calls during a slow reconnect assert no head-of-line stall beyond a bound; a crash-looping fake server asserts backoff + breaker-open after N failures.

## [P1] AG-006 — `AURA_MCP_CALL_TIMEOUT_SEC=0` disables the per-call timeout → unbounded hang + held mutex freezes the server

- **Evidence:** `internal/agent/mcptools/timeout.go:24–26` (`if sec==0 { return 0,nil }`), `bridge.go:76` (`if timeout>0 { … WithTimeout }`).
- **Problem:** A config value of `0` means *no per-call deadline*. If the parent ctx is also unbounded (production drives `context.Background()` per `cmd/aura/agent.go`), a server that hangs after accepting a request blocks `client.CallTool` indefinitely on the stdio read, holding the client mutex — freezing the entire server and leaking the call goroutine until process kill.
- **Impact:** One env typo removes the only backstop against a hung MCP server.
- **Recommended fix:** Treat `0` as "use default"; require an explicit sentinel (`-1`) for infinite; ensure the agent turn always carries a hard ceiling ctx. Resolve+validate the timeout **once at mount/boot** (fail loud), not per call (see AG-027).
- **Suggested test coverage:** Boot rejects/normalizes `0`; a hung fake server is cancelled by the default deadline; goroutine count returns to baseline.

## [P1] AG-007 — Mutating MCP tools have no per-call capability/trust gate; reconnect can silently flip `Mutating`

- **Evidence:** `internal/agent/llm_agent_dispatch.go:100–102` (only consumer of `Mutating` is `a.sideEffected=true`), `internal/agent/llm_agent.go:535`, `internal/agent/mcptools/bridge_reconnect.go:117–128` + `bridge.go:48–57` (`refreshSpec` recomputes `Mutating` from the server's new `tools/list` on reconnect).
- **Problem:** Trust is binary at the **server** boundary (`TrustBlocked` servers never launch). Once mounted, *any* tool a server offers executes with no per-call authorization — the PRD's `capability_grants` (Slice 1.7) is not consulted in dispatch. A reconnected server can change a tool from read-only → mutating under a name the operator already trusts.
- **Impact:** For an autonomous agent this is the highest-leverage security gap: a `TrustRemoteHTTP`/`TrustTrustedRecipe` server's mutating tools run unconditionally; a swapped server binary can downgrade a tool's mutation flag silently.
- **Recommended fix:** Gate `Mutating && Provenance.Trust==Untrusted` MCP calls through `capability_grants` (or a configurable allow/confirm policy) at dispatch. `slog.Warn` when a reconnect changes `Mutating` or the required-args set; consider pinning `Mutating` to the first-seen value.
- **Suggested test coverage:** A mutating MCP tool without a grant is refused/confirmed; a reconnect that flips `Mutating` emits a warning and does not silently downgrade.

## [P1] AG-008 — Reasoning-router LLM fallback adds a synchronous extra round-trip (≤8s) per turn when the embedding sidecar degrades

- **Evidence:** `internal/agent/llm_agent_reasoning.go:50–90` (`adaptiveReasoningTier`).
- **Problem:** The embedding classifier (~10ms) is the fast path, but on **any** miss — sidecar down, `Embed` error, low-margin abstain, empty vector — it falls through to a blocking LLM router call (`streamWithOpenRetry`, `MaxTokens:32`, timeout up to `reasoningRouterTimeout` ≈ 8s) *before* the real model call. This is exactly the "adaptive-reasoning router = latency root cause" the embedding classifier was built to eliminate (project memory `adaptive_reasoning_router_latency`).
- **Impact:** When the granite sidecar is unavailable, **every turn** pays two LLM round-trips and up to 8s of added latency.
- **Recommended fix:** Make the LLM-router fallback opt-in (env), or short-circuit to a static `ReasoningTierLow` when the classifier is wired but abstains; cap the router timeout far below 8s; add a circuit breaker so a persistently-down sidecar stops triggering the router every turn.
- **Suggested test coverage:** With the embedder forced to error, assert the tier defaults statically and no router LLM call is issued (or only once, then breaker-open).

## [P1] AG-009 — Full prompts/history/user text logged to the reasoning trace; redaction covers only named env-var secrets

- **Evidence:** `internal/reasoningtrace/*` (`Record`/`redactString`, ~61–165); call-sites `internal/agent/llm_agent.go:281–291` (`"history": a.history`), `internal/agent/llm_agent_reasoning.go:62–68` (`"user": user`).
- **Problem:** When `AURA_REASONING_TRACE` is on, `agent_request_built` writes the **entire conversation history** every turn, and the router request logs raw user text, to a JSONL file (default `os.TempDir()/aura-reasoning-trace.jsonl`, mode 0600). The only redaction scans `os.Environ()` and replaces values of vars whose **name** contains KEY/TOKEN/PASSWORD/SECRET (≥8 chars). It does **not** redact user PII, secrets the user typed into chat, the Agent.md profile, or tokens not living in a conveniently-named env var. It is also O(prompt_size × len(environ)) per record on a hot path.
- **Impact:** A full plaintext conversation log at rest (GDPR/privacy), with a redaction routine that gives false confidence; predictable temp path.
- **Recommended fix:** Don't dump full `history` per turn (log a hash + length, or gate the full dump behind a louder `AURA_REASONING_TRACE=full` with a documented PII warning). Cap per-field size before redaction. Document that enabling the trace persists plaintext conversations.
- **Suggested test coverage:** With the trace on, assert `history` is not written verbatim by default; assert a known PII token is redacted/omitted.

## [P1] AG-010 — DB password leaks into `shell_exec` children via URL/DSN-shaped env vars

- **Evidence:** `internal/agent/tools/shell_exec.go:424` (`mergeEnv` via `secret.IsSecretEnvKey`), `internal/secret/envkey.go:20–44` (substring denylist: `key,token,secret,pass,auth,bearer,credential,private,cert`).
- **Problem:** The project's own convention composes `AURA_DB_URL` = `postgres://user:PASSWORD@host/...` (CLAUDE.md, integration env). The key name `AURA_DB_URL` contains none of the 9 markers, so the password-bearing DSN is inherited by every `shell_exec` child and readable via `env`/`printenv`. Same class: `*_DSN`, `*_URI`, `*_CONN`, `*_PWD`, `SENTRY_DSN`, `JWT`/`COOKIE`/`SESSION`-named vars. The output-redaction patterns (`shell_exec_env.go:133–140`) also don't match a DSN credential.
- **Impact:** The shell already has host access in the trust model, but the *output redaction* effort signals an intent to keep credentials away from the model; a `cat $AURA_DB_URL` defeats it and the DB password can then be emitted to a channel or `web_fetch`.
- **Recommended fix:** Add URL/DSN markers (`url,dsn,uri,conn,pwd,cookie,session,jwt`) to `secretEnvMarkers`, and/or value-scan inherited env for `scheme://user:pass@` and redact the credential. Add a `://[^:]+:[^@]+@` DSN pattern to the output redactor.
- **Suggested test coverage:** `IsSecretEnvKey("AURA_DB_URL")==true`; a child cannot read the composed DSN; output redactor masks `postgres://u:p@h`.

## [P1] AG-011 — Skill self-activation is ungated despite gated-looking comments/spec; dead duplicate schema

- **Evidence:** `internal/agent/tools/skill_write.go:164–174` (`writeAction`: `status != "pending_approval"` is the *live* path — "in-box self-extension is ungated… auto-activates a model-authored mutation"); `internal/agent/tools/skill.go:99–112` vs `:114–137` (two schemas: unused `skillParamsSchema` claims approval-gating; the live `skillParamsSchemaHonest` admits immediate activation).
- **Problem:** The schema description and many comments state create/update/delete are "STAGED as pending and require explicit human approval — you cannot approve your own changes," but the code auto-activates model-authored skills with no human review, and the gating is delegated out of the package with no enforcement here. A dead duplicate schema const drifts from the live one (they already disagree on the gating story). `skill_write.go:186` can also return the `ask_user` pause sentinel, contradicting the `TestAskUserOnlyPauseConstraint` "only ask_user may pause" invariant (dormant on the live path).
- **Impact:** The model can write executable instruction-skills (loaded into future system prompts) that activate without operator review — a self-modification / prompt-injection-persistence surface. *(Directly matches prior **B-04**.)*
- **Recommended fix:** Delete the unused `skillParamsSchema`; reconcile the comments with the ungated behavior; confirm with the operator that ungated self-activation is intended and document the trust boundary; either restore the operator alert or update the spec to stop claiming approval.
- **Suggested test coverage:** Assert exactly one schema is referenced; a self-authored skill either stays pending (if gated) or emits an audit/alert (if ungated by design).

## [P1] AG-012 — No latency / error / cost / outcome metrics — no SLOs, no alerting

- **Evidence:** `internal/agent/metrics.go:11–68` — counters exist only for budget steps, tool-dispatch count, LLM-stream-open, LLM-stream-retry, and a tool-duration histogram.
- **Problem:** Missing: an **LLM-call latency** histogram (the dominant latency has no metric, only a span); **turn-outcome counters** (the rich `turnReason` taxonomy — `stream_open_error`, `breaker_open`, `tool_args_truncated`, `empty_response`, `max_steps`, … — is exported nowhere); **tool failure** counters; **token/cost** counters; **hook** counters.
- **Impact:** No SLO/alerting from metrics alone — you can see *that* tools ran, not how often turns fail or how slow the model is. *(Matches prior **O-02**.)*
- **Recommended fix:** Add `aura_agent_turn_total{outcome}`, `aura_agent_llm_call_duration_seconds`, `aura_agent_llm_errors_total{kind}`, `aura_agent_tool_errors_total{tool}`, token counters, hook counters. Also resolve MET-2: duplicated expvar+Prometheus globals panic on re-registration — use a non-default registry.
- **Suggested test coverage:** Assert each terminal `turnReason` increments its labeled counter; a tool error increments the tool-error counter.

## [P1] AG-013 — Agent core emits no structured (slog) logs; tracing degrades silently and can panic the process

- **Evidence:** `internal/agent/*.go` (observability is reasoning-trace JSONL — off by default — + OTel spans + expvar/Prometheus counters; no `slog`); `internal/agent/tracing.go:41–74` (OTLP defaults to `localhost:4317`; spans silently dropped with no collector, no boot/readiness signal) and `:96–102` (`mintSpanID` **panics** the process on a `crypto/rand` failure).
- **Problem:** Production runs without queryable structured logs; OTLP export failures are invisible (no boot log of exporter mode/endpoint, no `span_export_failures` metric); a transient entropy hiccup crashes the daemon for a non-load-bearing telemetry ID. *(Matches prior **O-01** on the logging/tracer-in-daemon axis.)*
- **Impact:** Blind production operation; a telemetry ID coupled to a hard panic.
- **Recommended fix:** Add `slog` structured logging at turn/LLM/tool boundaries with request_id/thread_id correlation. `mintSpanID` should fall back to a zero ID + error log/metric, never panic. Emit one INFO boot log of exporter mode + endpoint; expose `aura_agent_span_export_failures_total`.
- **Suggested test coverage:** Inject a failing entropy source → no panic; assert a boot log states the exporter mode.

---

# P2 findings (condensed — file:line + fix)

| ID | Src | File:Line | Problem | Fix |
|----|-----|-----------|---------|-----|
| AG-014 | AUR-07 | `tools/fs_read.go:47`, `fs_write.go:58`, `fs_edit.go:79` | No file-size cap on read/write/edit; `fs_read` loads whole file (+`string(b)` = 2× mem) → OOM/turn-wedge on a large file | Add `AURA_FS_MAX_READ_BYTES` (stat-then-reject or hard-limit stream); auto-page over cap |
| AG-015 | AUR-06 | `tools/shell_bg.go:174–178` | `BackgroundShells` is not a `SessionEvictor`; finished-but-unpolled bg buffers (≤1 MiB each) leak in a long-lived daemon | Implement `SessionEvictor`; prune finished shells on poll/kill, not only on next `start` |
| AG-016 | AUR-16 | `tools/task.go:117,372` | `agent_job` goals gated only by `rm/drop/delete` keywords; a benign-looking schedule fires an arbitrary full-tool agent turn later | Gate all `agent_job` schedules to `pending_approval`, or surface the goal to the operator at fire time |
| AG-017 | AUR-05 | `tools/shell_bg.go:104–131` | Fragile double-`readOff` bookkeeping in bg `snapshot`; correct but under-tested across a trim boundary | Collapse to one compaction step + byte-exact paging test |
| AG-018 | AUR-02 | `tools/shell_exec_session.go:17`, `shell_approval.go:35` | Model-supplied `cwd` unvalidated → opaque exec error; approval digest not normalized (`/tmp` vs `/tmp/`) → double prompt | `Stat` the cwd + clean error; `filepath.Clean` before digesting |
| AG-019 | AUR-14 | `tools/send_file.go:121` | Workspace fence silently disabled when `WorkspaceRoot==""` (active in prod, but a quiet downgrade) | Fail closed when root empty in non-CLI contexts |
| AG-020 | AUR-18 | `tools/search.go:62–82` | MCP reconnect that changes an existing tool's description doesn't re-embed it → stale semantic vector degrades tool selection silently (WR-01) | Per-tool description hash; force ranker rebuild on change |
| AG-021 | AUR-03 | `tools/shell_exec_env.go:78–100` | Destructive-command gate is advisory regex, trivially bypassable (`T=/; rm -rf $T`) — documented; restated so the report doesn't over-credit it | None within model; the only real boundary is least-privilege/sandbox |
| AG-022 | F-15 | `mcptools/name.go`, `cmd/aura/main.go:209–228` | Cross-server namespaced-name collision drops the *whole* second server silently (fail-soft `continue`, WARN only) | Validate namespace uniqueness at boot; hash-suffix cross-registry collisions; escalate the WARN |
| AG-023 | F-16 | `mcptools/name.go:18–33` | `sanitizeName` collapses distinct namespaces (`my.srv`/`my/srv`/`my srv`) to one prefix → reinforces AG-022 | Detect post-sanitization namespace collisions at boot |
| AG-024 | F-4 | `mcptools/bridge_reconnect.go:56–63` | After-send transport failure surfaced as a retryable inline error → mutating MCP tools can double-execute (at-least-once) | Thread a "failed after send" sentinel; mark non-retryable to the model |
| AG-025 | F-6 | `mcptools/bridge.go:155–181` | Per-field description cap only; a hostile/buggy server can advertise thousands of properties / deep `$defs` → manifest+index bloat | Cap total marshalled schema bytes + property count; fall back to `emptyObjectSchema` |
| AG-026 | F-21 | `mcptools/bridge.go:82–84` | Server error/`isError` text inlined to the model (distrusted-wrapped, good) but bypasses the byte caps applied to descriptions | Cap inlined MCP error text via `tools.NewResult` preview ceiling (confirm `previewCap`) |
| AG-027 | F-18 | `mcptools/bridge.go:70`, `timeout.go` | Timeout parsed per-call from env; a malformed value makes **every** MCP call loop-fatal mid-run | Resolve+validate once at boot (fail loud), store on the server |
| AG-028 | F-12 | `mcptools/mount.go:66–75` | `openManagedServer` appears to be dead code (violates DEEP-REFACTOR-ON-TOUCH) — **NEEDS CONFIRMATION** (`deadcode ./...`) | Remove if unreferenced |
| AG-029 | F-13 | `mcptools/bridge.go:48`, `tools/spec.go:92–117` | Registry "immutable post-boot" is load-bearing but the safety against reconnect spec-swaps rests on an undocumented `atomic.Value` contract | Document the atomic-swap contract; add a `-race` reconnect-during-dispatch test |
| AG-030 | CMD-4 | `hooks_command.go:192–198` | Non-zero hook exit + a parseable non-allow decision silently swallows `runErr` → a crashed-after-emitting hook's rewrite is applied as success | Require exit 0 for `rewrite`; allow `deny` on non-zero only; document |
| AG-031 | CACHE-2 | `prompt/hash.go:29–42` (test-only use) | No **runtime** cache-prefix invariant; `PrefixHash` is used only in tests, so a `BeforeModel` hook rewrite can bust the provider cache undetected | Compute+compare `PrefixHash` each turn after `BeforeModel`; emit a `prefix_drift` metric |
| AG-032 | REAS-2 | `prompt/reasoning_classifier.go:191–220` | `ensureAnchors` holds `c.mu` across 3 embed round-trips + a Neo4j `LoadExamples` → cold-start serializes all concurrent turns | Build the bank outside the lock (or `singleflight`); take the lock only to publish |
| AG-033 | TRC-1 | `tracing.go:96–102` | `mintSpanID` panics the process on `crypto/rand` failure (also folded into AG-013) | Fall back to zero ID + error metric; never panic for telemetry |
| AG-034 | EVT-2 | `event.go:94–114` | `ToolInvocation` carries raw `Arguments`/`ResultPreview`/`Meta` — confirm the ledger projection redacts+caps — **NEEDS CONFIRMATION** (persistence layer) | Apply `reasoningtrace.redactString`-class redaction + size cap before DB write |
| AG-035 | WF-1 | `workflow/loop.go:67,162` | `maxIterations==0` loop bounded only by budget + the no-progress guard; a one-step-per-iter child loops near `max_steps` (unbounded iteration count by design) | Default iteration ceiling even when `maxIterations==0`; require a wallclock-bounded ctx |
| AG-036 | WF-2 | `budget.go:111–118,185` | `max_steps`/`wallclock` not validated `>0`; `=0` silently disables the runtime ("budget exhausted before first step") | Reject `maxSteps<1`/`wallclock<1` at `NewBudget` (matches the existing `softFrac` check) |
| AG-037 | WF-8 | `workflow/workflow.go:19–29` | `findInTree` has no cycle/visited guard → stack-overflow on a cyclic/diamond agent tree (the `Agent` interface is open by design) | Depth-bound or carry a visited-set keyed by agent pointer |
| AG-038 | BG-2 | `budget.go:239`, `swarm.go:90` | `Remaining()`-based swarm admission is TOCTOU-racy; the `budgetReserve=3` parent-synthesis reserve is best-effort, not actually reserved | Atomically subtract+restore the reserve, or document as best-effort |
| AG-039 | DD-2 | `budget_dedup.go:183–191` | `results` map grows unbounded over a run (never pruned on ring eviction); bounded by `max_steps` today, real under a large/uncapped budget | Prune `results[fp]` when `fp` is evicted from `entries`, or cap map size |
| AG-040 | DD-3 | `budget_dedup.go:107–114` | Dedup catches only period-1/period-2 cycles; a period-3+ tool oscillation (A-B-C-A-B-C) burns the full budget before stopping | Generalize to detect any repeated subsequence in the window; document the limit |
| AG-041 | TO-1 | `budget.go:323` (unwired) | Budget wallclock is a step-boundary gate; `WithDeadline` is **not** wired into the loop/swarm run ctx → wall-time is soft (overshoot ≤ one per-call timeout/step) — **NEEDS CONFIRMATION** at `cmd/aura/agent.go` | Thread `Budget.WithDeadline(parent)` into the root `ic.Ctx` |
| AG-042 | ST-1 | `llm_agent.go:51` (D-26) | All run state (history, budget counters, dedup ring, recovery counters) is in-memory; no checkpoint → no crash resumability (pause/resume *is* durable via the Runner; crash recovery is not) | Periodic snapshot of history+counters keyed by sessionID (Runner concern) |
| AG-043 | WF-5 | `workflow/parallel.go:107–129` | Result-closer goroutine leak edge under concurrent child-error + consumer-break (appears safe; needs proof) | `goleak` stress test breaking the consumer at every Event index under N children |

---

# P3 findings (condensed)

| ID | Src | File:Line | Problem | Fix |
|----|-----|-----------|---------|-----|
| AG-044 | AUR-13 | `tools/skill.go:99–112` | Dead duplicate `skillParamsSchema` const (drifts from the live one) | Delete it (folded into AG-011) |
| AG-045 | AUR-08 | `tools/fs_edit.go:62–79` | TOCTOU + non-atomic in-place write (last-writer-wins); parallel swarm edits race | temp-file + `os.Rename`; document last-writer-wins |
| AG-046 | AUR-10 | `tools/fs_grep.go:83` vs `fs_glob.go:51` | Inconsistent glob semantics (`filepath.Match` no `**` vs `**`-aware) → a model reusing `**/*.go` gets zero grep matches | Unify on `globToRegexp` or document the difference |
| AG-047 | AUR-04 | `tools/shell_exec_env.go:133–140` | Secret-redaction patterns don't cover DSN credentials (ties to AG-010) | Add DSN pattern |
| AG-048 | AUR-09 | `tools/fs_grep.go`, `fs.go` | File symlinks followed in walks/reads; sizes not budgeted (acceptable under no-fence model) | Note only |
| AG-049 | AUR-11 | `internal/web` transport | No destination-port restriction in the SSRF gate (any port on a public IP) | Optional port policy if port-scanning is in scope |
| AG-050 | AUR-15 | `tools/result.go:75`, `read_tool_output.go:75` | Sidecar path safety rests on an un-asserted `WithToolCallContext` invariant | Assert `runDir` is absolute + within run root |
| AG-051 | AUR-17 | `tools/skill_write.go:186` | Skill tool can return the `ask_user` pause sentinel (dormant), contradicting the "only ask_user pauses" invariant | Reconcile (folded into AG-011) |
| AG-052 | AUR-19 | `trust.go:14–31` | Untrusted-output detection defaults to **trusted** for unknown tools; `swarm_spawn` child reports (which embed children's web/fs output) are not marked untrusted | Default fail-safe (unknown→untrusted); propagate untrusted provenance through `swarm_spawn` *(relates to prior B-02)* |
| AG-053 | F-7/9/10 | `mcptools/bridge.go:88`, `bridge_reconnect.go:66–79,17–19` | Minor: all-MCP-untrusted is good; `Close` doesn't cancel in-flight calls; `openMCPClient` mutable global test seam | Note / hygiene |
| AG-054 | CMD-5 | `hooks_command.go:278–287` | Bare hook command names resolved against runtime `$PATH` | Require absolute paths; reject bare names |
| AG-055 | REAS-3 | `prompt/reasoning_classifier.go:100–106,59–90` | Greeting pre-filter + seeds are Italian-only; other-language greetings pay an embed round-trip and may misroute | Document the Italian-corpus assumption or add multilingual seeds |
| AG-056 | TRC-2 | `tracing.go:41–74` | OTLP exporter silently drops spans without a collector; no readiness signal (folded into AG-013) | Boot log + export-failure metric |
| AG-057 | MET-2 | `metrics.go:11–38` | Duplicated expvar+Prometheus globals; `promauto` panics on re-registration → can't instantiate twice | Use a custom registry; document why both |
| AG-058 | HM-1 | `hooks.go:85–124` | Undocumented first-error/first-result-wins short-circuit; no `recover()` around in-process hooks | Document; wrap hook calls in recover (ties AG-001/AG-004) |
| AG-059 | WF-3 | `workflow/loop.go:150–157` | Empty-pass step charge doesn't bound a non-returning child within a single `Run` | Document leaf contract; rely on wallclock ctx (AG-041) |
| AG-060 | WF-6 | `workflow/parallel.go:123` | Escalate `cancel()` is checkpoint-based not preemptive; siblings may spend a few more steps | Document |
| AG-061 | WF-7 | `workflow/sequential.go:62–68` | Chain-abort on sub error has no observability marker for which sub aborted | Emit a `chain_aborted_at` StateDelta |
| AG-062 | SC-1 | `swarm_context.go:24–52` | `SwarmContextValue` shares live `*Registry`/`Client` across workers; concurrent-read contract is implicit | Document the contract; `-race` swarm fan-out test |
| AG-063 | BG-1/4 | `budget.go:225–236,260–262` | `used`/`remaining` are independent atomic reads (momentarily-inconsistent `<budget>` hint); `SetMaxSteps` boot-only safety is unenforced | Snapshot both under one read if hint accuracy matters; boot-phase guard |
| AG-064 | LP-3 | `llm_agent_parallel.go:41–50` | All N tool goroutines spawned eagerly; `limit` gates execution not spawn | Worker-pool if very wide turns are expected |

**Confirmed-good / no-fix (not counted):** SSRF hardening in `internal/web` (pinned-IP dial, scheme allowlist, metadata-IP block, per-hop redirect re-validation, size cap) · `trust.go` nonce-fenced untrusted-output envelope · KV-cache stable-prefix discipline (`prompt/builder.go`, tail-injected budget) · deterministic `canonicaljson` hashing · shared-`atomic.Int32` budget defeating the `max_steps^depth` fan-out bomb · non-empty terminal contract (finalize → synthesis → Italian stub) · bounded recovery/completion/truncation counters (no unbounded `continue`) · `goleak`-guarded `TestMain` · property/fuzz tests (`agent_fuzz_test.go`, `loop_property_test.go`, `budget_dedup_test.go`).

---

## Architectural debt vs implementation bugs

- **Implementation bugs (fix in place):** AG-001 (panic recover), AG-002 (dedup mutex), AG-006 (`=0` timeout), AG-010 (DSN denylist), AG-014 (fs size cap), AG-022/023 (name collisions), AG-030 (hook exit swallow), AG-033 (span panic), AG-036 (budget validation), AG-044 (dead schema).
- **Architectural debt (design change):** AG-003/AG-004 (hook sandboxing + fail-soft policy), AG-005 (MCP reconnect model), AG-007 (capability-grant gate), AG-008 (reasoning-router fallback policy), AG-009 (trace privacy model), AG-011 (self-extension trust boundary), AG-012/AG-013 (observability model), AG-041/AG-042 (active deadline + checkpointing).
