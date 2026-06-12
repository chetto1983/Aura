# Bug Report — Aura `internal/agent` (re-audit 2026-06-12)

Master list of correctness, reliability, security, and operational findings. Severity-ordered. Each finding cites evidence, problem, impact, failure scenario, recommended fix, and suggested test coverage.

**Severity scale:** P0 = critical production blocker / data loss / unsafe execution / system-wide hang. P1 = serious correctness/reliability/security issue, must fix before production. P2 = important maintainability/observability/architecture issue. P3 = improvement/cleanup/hardening.

**Tally:** 0 × P0 · 8 × P1 · 20 × P2 · 12 × P3.

**On the prior cycle.** The 2026-06-10/11 pass closed both P0s and most P1s; those closures verify (see [`re-audit-2026-06-12.md`](re-audit-2026-06-12.md) and the "Verified closed" appendix). This report lists what remains open or newly found. Two prior closures are over-credited and re-listed here as live P1s (B-01 ⊃ R-05, B-04 ⊃ R-09).

---

# P0 — Critical

**None.** Both prior P0s verify closed:

- **R-01** (untrusted output re-enters prompt unmarked): closed for the 8 direct feeders by the `trust.go` provenance envelope (NFKC + `html.EscapeString` + crypto/rand nonce, routed through `renderToolResultForPrompt` on both error and success paths). **Residual hole tracked as B-02** (swarm path).
- **R-02** (hung MCP server wedges the daemon): closed by a per-call timeout + ctx-aware read + reconnect-no-replay (`mcptools/bridge_reconnect.go`).

---

# P1 — Must fix before production

## [P1] B-01 — Mutating tool side effects re-execute after an intra-turn crash (R-05 over-credited)

- Evidence:
  - File: `internal/agent/llm_agent.go`; `internal/runner/runner_persist.go`; `internal/conversations/store_helpers.go`
  - Location: `dispatch` → `executeBatch` then per-call persist (`llm_agent.go:364-388`); `persistToolTurn` (`runner_persist.go:89-123`); `repairToolMessagePairsWith` (`store_helpers.go:232-267`)
  - Component: tool execution ↔ persistence ordering
- Problem: Per-event journaling (the R-05 mitigation) closed the *loss of observability* of intra-turn state but not the re-execution hazard. The ordering is: (1) `executeBatch` runs the tool's `Execute` — for `fs_write`/`shell_exec`/`swarm_spawn` the host side effect is now committed; (2) only on the subsequent event is the assistant `tool_calls` turn + the `RoleTool` result persisted. There is no write-ahead intent row and no idempotency key. If the process dies between (1) and (2), reload sees an assistant `tool_calls` turn with no matching answer (or no assistant turn at all), and `repairManagedToolMessagePairs` **drops the dangling group** from model-visible history — so the model never learns the tool already ran and re-issues the identical call. The dedup ring that would catch a repeat lives on the in-memory `Budget`, minted fresh per turn (`runner.go` `buildAgent`), so it provides zero cross-turn protection.
- Impact: Duplicated mutating side effects on crash recovery — a file written twice, an email/shell command run twice, a `swarm_spawn` re-fanned. Silent; no error surfaces.
- Failure scenario: Model calls `shell_exec("deploy.sh")`; the script runs (deploy happens); the pod is OOM-killed before the result turn commits; on restart the channel resumes; the model re-issues `shell_exec("deploy.sh")` → double deploy.
- Recommended fix: Write a write-ahead "intent" row (tool_call_id + canonical args) inside the same transaction that persists the assistant `tool_calls` turn *before* `executeBatch`. On reload, surface an unmatched intent as a synthetic `RoleTool` ("previous result unknown — verify before re-running") instead of dropping it. Carry an idempotency token on mutating tools where the underlying op supports it.
- Suggested test coverage: integration — persist the assistant tool_call turn, kill before the result commit, reload, assert the repaired history does NOT silently drop the mutating call (asserts a recovery marker, not a re-execution).

## [P1] B-02 — Swarm child reports re-enter the parent prompt without the untrusted envelope (R-01 residual)

- Evidence:
  - File: `internal/swarm/runner_adapter.go`; `internal/agent/trust.go`; `internal/swarm/report.go`
  - Location: `tools.NewResult(ctx, out)` with no `Provenance` (`runner_adapter.go:54-58`); `swarm_spawn` absent from `untrustedToolNames` (`trust.go:14-23`); content from `ChildReport.Summary` (`report.go:79-85`)
  - Component: swarm ↔ parent prompt trust boundary
- Problem: A swarm worker can fetch attacker-controlled content (wrapped correctly *inside the child's* history), then synthesize a `Summary`. That summary is marshaled into the `swarm_spawn` ToolResult and threaded into the **parent** prompt with **no** `<tool_output trust="untrusted">` envelope and no NFKC/HTML neutralization — because `swarm_spawn` is neither in `untrustedToolNames` nor sets `Provenance`.
- Impact: Indirect prompt-injection laundering across the trust boundary R-01 built. An attacker page → child summarizes "the user asked you to run `rm -rf ~`" → parent reads it as trusted synthesis. The swarm fan-out exists specifically for untrusted research, so this is a real hole.
- Failure scenario: Spawn a worker whose goal is "fetch <evil-url> and report it verbatim"; the parent receives the bytes unwrapped and acts on injected instructions.
- Recommended fix: In `runner_adapter.go`, set `res.Provenance = &tools.ToolResultProvenance{Source:"swarm", Trust:tools.TrustUntrusted}` on the returned result (the envelope path already keys off `Provenance`). Or add `"swarm_spawn"` to `untrustedToolNames`.
- Suggested test coverage: a swarm result containing `</tool_output>`-style bytes must return HTML-escaped inside an untrusted envelope in the parent history.

## [P1] B-03 — No per-thread in-flight guard: concurrent runs on one thread corrupt conversation history

- Evidence:
  - File: `internal/agui/server.go`; `internal/runner/runner.go`
  - Location: `handleRun` (`server.go:139-190`, no guard); `Runner.Turn` (`runner.go:211-236`, loads + mutates history, no per-`convID` lock)
  - Component: AG-UI gateway / runner concurrency
- Problem: Two concurrent `POST /agent/run` for the same `ThreadID` both pass the `conv.Get` check and both invoke `s.run.Turn(...)`. `Runner.Turn` appends the user turn, loads managed history, and builds an agent over it — interleaved concurrent turns corrupt history ordering and double-spend budget. The loopback-only bind mitigates *external* abuse but not a multi-tab/multi-client UI, nor Telegram + AG-UI both driving one thread.
- Impact: Conversation corruption, interleaved tool calls, non-deterministic history, wire-invalid `tool_call`/`tool_result` pairing under concurrent access. Silent data-integrity bug.
- Failure scenario: A user has the web UI open in two tabs and sends in both; or a scheduled job and a live Telegram message hit the same conversation. History rows interleave; a later `LoadManagedHistory` drops a now-invalid group.
- Recommended fix: Add a `sync.Map[threadID]*sync.Mutex` (or per-thread singleflight / in-flight set) in `Runner` so AG-UI and Telegram share one guard; reject a second concurrent run on the same thread with `409 Conflict`, or serialize.
- Suggested test coverage: concurrent `handleRun` on one thread → second returns 409 (or serializes provably); a race-detector test on interleaved turns.

## [P1] B-04 — Self-extension gate open for `always:false` skills; tool schema + comments lie; operator alert lost (R-09 regressed by P5 policy)

- Evidence:
  - File: `internal/skills/writer.go`; `internal/agent/tools/skill.go`
  - Location: `WriteMutation` / `modelMutationBypassesGate` (`writer.go:94-148`); contradicted by schema `skill.go:99-112`, `writeAction` `skill.go:164-188`, doc comment `skill.go:14-25`
  - Component: skill self-extension / tool contract
- Problem: A "P5 (2026-06-10)" change made model-authored `skill create`/`update` with `always:false` **bypass the gate and auto-activate** (`StatusActive`, `ApprovalAuto`); `action=delete` de-materializes immediately. But the model-facing schema still states *"create/update/delete are STAGED as pending and require explicit human approval … you cannot approve your own changes,"* and the file doc comment repeats it. The headless `Alerter` only fires on the now-**unreachable** pending fallback, so the operator gets **no alert** when the model self-extends in-box. The injection blocklist (`ValidateForWrite`) is the only remaining barrier — a static string blocklist, not a semantic gate.
- Impact: The model can write a new `always:false` skill body and immediately `action=use` it within the same conversation, with no human in the loop and no alert. The compensating control R-09 promised (gate + alert) is gone for the common case, and the contract text actively misleads any operator/auditor reading the tool spec.
- Failure scenario: `skill{action:create,name:"x",description:"d",body:"…"}` with no `always` → `StatusActive` immediately (codified by `writer_activate_mutation_test.go:33-49`).
- Recommended fix: (a) Update the `skill.go` schema `description` + doc comments to state the true policy (in-box create/update auto-activate; `always:true` and delete differ). (b) Fire the `Alerter` (or an audit-log row) on the **ungated** auto-activate path, not only the pending fallback. (c) Confirm `delete` should truly be unattended; if not, route it through the `always`-style gate.
- Suggested test coverage: assert an alert/audit row is emitted on the ungated model auto-activate; a schema-snapshot test that the description matches the live gating policy.

## [P1] O-01 — `aura serve` never boots the tracer; agent core has zero structured logging → production is blind

- Evidence:
  - File: `cmd/aura/serve.go`; `cmd/aura/chat_repl.go`; `internal/agent/*.go`; `cmd/aura/main.go`
  - Location: `serve.go:69-138` (no `newTracer` call); only caller is `chat_repl.go:32` (REPL). Agent core: no `slog`/`log.Printf`/stderr anywhere. `main.go` never calls `slog.SetDefault`.
  - Component: observability bootstrap
- Problem: The OTel `TracerProvider` is installed only in the interactive `aura chat` REPL. The long-lived daemon `aura serve` — which hosts the scheduler, AG-UI gateway, and Telegram channel (the actual production surface) — never calls `NewTracerProvider`. With no global provider, `otel.Tracer(...).Start()` resolves to the no-op tracer, so the single `llm.request` span is silently dropped under `serve`. Separately, the agent loop emits no logs at all — failures (retry exhaustion, empty-response recovery, stream errors, dedup trips) are visible only via debug-gated `reasoningtrace` (off in prod). Where the daemon does log, it uses Go's default text handler with no `service`/`request_id`/`thread_id` correlation.
- Impact: Production has no distributed tracing and no machine-parseable correlated logs. A user-reported failure cannot be traced to a turn; incidents are undebuggable without re-running with `AURA_REASONING_TRACE=1`.
- Recommended fix: Boot the tracer in `runServe` from `cfg.OtelExporter`/`OtelEndpoint` with a deferred bounded `Shutdown`; install `slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, …)))` with base `service`/`version` attrs in `main()`; thread a `slog.Logger` (with `request_id`/`thread_id`) into the loop chokepoints currently calling `reasoningtrace.Record`, at minimum for WARN/ERROR events.
- Suggested test coverage: a `serve`-boot test asserting the global provider is non-noop after boot; a capture-handler test asserting a turn emits structured records with correlation keys.

## [P1] O-02 — No latency/error/cost metrics, no Prometheus (4 expvar counters only)

- Evidence:
  - File: `internal/agent/metrics.go`; exposed at `internal/agui/server.go:83` (`/debug/vars`)
  - Location: `metrics.go:6-9` — four monotonic `expvar.Int` counters
  - Component: metrics
- Problem: The only metrics are counters (`budget_consume_step`, `tool_dispatch`, `llm_stream_open`, `llm_stream_retry`). Missing: latency histograms (turn, per-tool, LLM call — the timing data exists at `llm_agent.go:366` but is never recorded), error counters (tool failures, stream errors, finalize-on-budget, 4xx/5xx), gauges (in-flight turns, active SSE connections, SSE drop count), and cost (token usage is on the span but not a metric). No `promhttp` endpoint; expvar `/debug/vars` is not Prometheus-scrapeable.
- Impact: No SLO dashboards, no alerting on latency regression or error spikes, no cost visibility. The dropped-SSE-event path (`server.go:291`, WARN only) is invisible to monitoring.
- Recommended fix: Adopt `prometheus/client_golang`; add histograms (`aura_turn_duration_seconds`, `aura_tool_duration_seconds{tool}`, `aura_llm_request_duration_seconds`), counters (`…_errors_total{kind}`, `…_sse_dropped_total`), gauges (`…_inflight_turns`, `…_sse_connections`), and a cost counter from `llm.Usage`; mount `promhttp.Handler()` at `GET /metrics`.
- Suggested test coverage: registry assertion that a turn increments the histogram/error counters; `/metrics` returns Prometheus text format.

## [P1] D-01 — No production container: privileged agent binary runs uncontainerized; sidecars unhardened

- Evidence:
  - File: repo root (no `Dockerfile`, no `.dockerignore`); `compose.yaml`
  - Location: `compose.yaml` defines only sidecars; the `aura` binary is not a service; no service declares `user:`/`read_only:`/`cap_drop:`/`mem_limit`/`cpus` (only stt/tts have a `deploy.resources` block)
  - Component: deployment / runtime isolation
- Problem: There is no container image for the agent runtime itself — it ships as a host binary (goreleaser). For a runtime that executes arbitrary `shell_exec` and code snippets, the absence of containerized, non-root, resource-bounded deployment is a significant isolation gap. Sidecars run as root with full caps and (mostly) no memory ceiling; an `AgentJobMaxDurationSec`-bounded but memory-unbounded job can OOM the host.
- Impact: No runtime isolation for the privileged agent process; no memory/CPU ceiling; the blast radius of a compromised sidecar is full container root.
- Recommended fix: Add a multi-stage distroless/alpine `Dockerfile` for aura with a non-root `USER`; add an `aura` compose service with `read_only: true` + `cap_drop: [ALL]` + `mem_limit`/`cpus` + a `/healthz` healthcheck; add `cap_drop`/`mem_limit` to sidecars.
- Suggested test coverage: CI image build; container-runs-as-non-root assertion; healthcheck smoke.

---

# P2 — Important

## [P2] M-01 — L1 microcompact rewrites `ask_user` answers (and small tool results) to a dead `read_tool_output` pointer (R-28)

- Evidence: `internal/conversations/context.go:208-229` (`applyL1`), `:255-260` (`readToolOutputPointer`); `internal/agent/tools/read_tool_output.go:81-93`
- Problem: `applyL1` rewrites the content of *every* `RoleTool` turn older than `evictAfter` (default 10) to a `read_tool_output(tool_call_id=…)` pointer, gated only on role + `Seq==1`. An `ask_user` answer is a `RoleTool` turn whose content is the *user's actual answer text*, never spilled to a sidecar. The pointer resolves to `no output for tool_call_id` (`read_tool_output.go:93`). Same for any small/un-spilled tool result.
- Impact: In any conversation longer than `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` rounds, the substance of every HITL answer is silently deleted and replaced by a pointer that errors. The model loses the user's clarifying answers and re-asks or proceeds on a wrong assumption.
- Failure scenario: User answers "use staging, not prod" at round 3; by round 14 the answer is evicted to a dead pointer; the model defaults to prod.
- Recommended fix: Only rewrite a `RoleTool` turn to a pointer when it actually has a sidecar (`ContentSidecarPath != ""` or the `[output truncated:` footer is present). Leave sidecar-less small results (incl. `ask_user`) inline; L2.5 pair-drop is the correct eviction for those.
- Test: build a history with an `ask_user` answer older than `evictAfter`; assert the content survives verbatim and is not a pointer.

## [P2] M-02 — `SubmitAnswer` is non-atomic across two transactions → duplicate resume turn bricks the round (R-27)

- Evidence: `internal/runner/runner_resume.go:77-82` (`injectAnswer` then `MarkResumed`); batch path `:112-123`; `internal/askuser/store.go:233`
- Problem: `injectAnswer` (its own `AppendTurn` tx) and `MarkResumed` (separate `pool.Exec`) are two transactions. A crash/error between them leaves the answer durably written but `paused_states.resumed_at IS NULL`; a retry re-injects a second `RoleTool` answer with the same `tool_call_id`. On reload `validToolResultGroup` rejects the duplicate-id group, dropping the whole round — the model loses the answer and re-asks (the infinite-pause class R-06 fixed elsewhere).
- Impact: Wire-invalid history and a re-asked question on a resume retry (e.g. Telegram redelivering a callback after a crash).
- Recommended fix: Make inject+mark one transaction — a `Conv`-store seam running `InsertConversationTurn` + the `markResumedSQL UPDATE … WHERE resumed_at IS NULL` in a single `db.WithTx`; `RowsAffected==0` then makes a re-submit a no-op.
- Test: resolve a token, simulate a retry of the same token, assert exactly one answer turn and `ErrPauseNotFound` on the second submit.

## [P2] M-03 — `hardCap<=0` silently disables all L2/L2.5 context-window protection (R-29)

- Evidence: `internal/conversations/context.go:73-83` (clamp negative→0), `:149,153` (`hardCap == 0` returns early, skipping L2.5)
- Problem: `hardCap = ContextWindow - max(MaxOutputTokens,20000) - 13000`, clamped to 0 when negative. The L2 gate `if hardCap == 0 || tokensAfterL1 <= hardCap` returns early on 0, skipping pair-drop entirely. A model whose `ContextWindow ≤ ~33000` (or a misconfig where the reservation exceeds the window) gets **no** protection: history grows until the provider hard-rejects with a 400/413.
- Impact: On small-window models (the Slice-13 local vLLM fallback is the stated trigger) or a config typo, turns 400 with no graceful `ErrContextWindowExceeded` and no compaction — exactly when protection is most needed.
- Recommended fix: Treat `hardCap <= 0` as a configuration error (or apply a per-model floor): return `ErrContextWindowExceeded` immediately with a clear message rather than silently disabling the ladder. Do not conflate "cap is 0" with "under cap."
- Test: `ContextConfig{ContextWindow:32000, MaxOutputTokens:8000}` with an over-cap history → assert it does NOT return the raw history unprotected.

## [P2] B-05 — Circuit breaker is per-turn → no cross-turn protection during a provider outage

- Evidence: `internal/agent/llm_agent.go:117` (`breaker: llm.NewBreaker(3, 30*time.Second)` in the constructor); `runner.go` `buildAgent` constructs a fresh `LlmAgent` per turn; consumed `llm_agent_stream_retry.go:25-57`
- Problem: The breaker is constructed fresh inside `NewLlmAgent`, and the runner builds a new `LlmAgent` every user turn — so failure count and open-state reset each turn. The breaker can only trip *within* a single turn (across its router/main/critic/finalize sub-calls); the 30s cooldown never spans turns. `.Success()` is fed only on stream-open success, never after a clean completion.
- Impact: During a real provider outage, every new turn re-attempts at full rate; the breaker's stated purpose (stop hammering a down provider across requests) is not delivered.
- Recommended fix: Hoist the breaker to a process-lifetime singleton owned by `Runner` (alongside `r.client`) and inject it into `LlmAgentConfig`, so open-state/cooldown persist across turns.
- Test: two consecutive turns against a client returning 503; assert the second is short-circuited by `ErrBreakerOpen` with no network call.

## [P2] B-06 — Breaker-open in the main loop surfaces as a hard error instead of a graceful finalize

- Evidence: `internal/agent/llm_agent.go:225-231` (`streamWithOpenRetry` returns `ErrBreakerOpen` → `yield(nil, err)`); `llm_agent_stream_retry.go:26-28`
- Problem: When the breaker is open, the main loop routes `ErrBreakerOpen` to the `iter.Seq2` **error slot**. D-04 reserves that slot for *real* infra failures; a breaker refusal is a self-inflicted backpressure decision. It bypasses the `finalize()`/stub-digest safety net that guarantees a non-empty answer.
- Impact: Mid-conversation, a breaker trip produces a raw error to the user/channel rather than a graceful "trouble reaching the model" finalize — inconsistent with the always-answer guarantee the rest of the loop upholds.
- Recommended fix: On `errors.Is(err, llm.ErrBreakerOpen)` at the open-stream call site, route to `finalize(… reason="breaker_open" …)` (which degrades to the stub when the breaker is still open).
- Test: pre-open the injected breaker, run one turn, assert a non-empty terminal Event with a breaker reason and no error in the error slot.

## [P2] B-07 — `LoopAgent` hot-spins on a budget-owning sub under `maxIterations==0` (latent, off the production path)

- Evidence: `internal/agent/workflow/loop.go:67-157` (the `subOwnsBudget` branch at 92-100 + the skipped empty-pass charge at 149)
- Problem: With `maxIterations==0` and a sub that returns `OwnsBudget()==true`, the per-iteration empty-pass `ConsumeStep` guard is skipped. An `LlmAgent` whose budget is exhausted finalizes (consuming no step), the loop sees no Escalate, and re-runs the same sub forever; only the wallclock `ctx.Err()` check stops it. Not reachable today (the runner drives `*LlmAgent` directly; `NewLoop` wraps only a non-owning mock in dry-run), but live the instant anyone composes `workflow.NewLoop(name, 0, llmAgent)`.
- Impact: A CPU busy-spin emitting unbounded duplicate "final answer" Events for up to the full wallclock window.
- Recommended fix: Require `maxIterations > 0` when any sub is a `BudgetOwner`, or break the loop when a budget-owning sub returns without forward progress (`Remaining()<=0`), or charge one workflow step per iteration even for budget-owners.
- Test: `NewLoop("t", 0, budgetOwningStubThatFinalizesWithoutConsuming)` with a small `MaxSteps`; assert termination within a bounded number of yielded Events.

## [P2] O-03 — `otel.SetErrorHandler(func(error){})` silences ALL otel errors process-wide (R-31)

- Evidence: `internal/agent/tracing.go:63` (inside `newTracerProvider`, otlp branch)
- Problem: The global no-op handler — justified as suppressing connection-refused noise on REPL exit — is process-global and permanent. It swallows every future otel error: export failures after a collector goes down mid-run, batch-queue-full drops, malformed-span errors. With a real collector deployed, silent trace loss is invisible. It is also set only on the otlp path, so behavior differs by exporter mode.
- Impact: When a collector IS deployed, dropped traces cannot be detected.
- Recommended fix: Replace the no-op with a rate-limited handler that logs at debug/warn via the structured logger; or scope suppression to `Shutdown` only, not the process lifetime.
- Test: inject an export error and assert it is observable (logged/counted), not silently dropped.

## [P2] O-04 — No boot-time validation/fail-fast for required secrets; no secret-redacting log handler (R-32 adjacent)

- Evidence: `internal/config/config.go:187,219,275` (`os.Getenv` for `POSTGRES_PASSWORD`/`NEO4J_PASSWORD`/`AURA_SETUP_TOKEN`); only fail-fast is `llm.Load()`; numeric/bool knobs silently fall back (`envIntDefault`, `envBoolDefault`); `serve.go:130` logs raw `err`
- Problem: Empty `NEO4J_PASSWORD`/`POSTGRES_PASSWORD` surface as late dial failures, not boot errors. Typo'd numeric knobs boot with the default, masking misconfiguration. There is no central required-env validation at `serve` boot. `SanitizeString` exists for the AG-UI wire but is not applied to the daemon's `slog` error lines (which can embed a DSN).
- Impact: A misconfigured daemon boots and fails opaquely at first use; risk of DSN/credential leakage into stderr logs.
- Recommended fix: Add `Config.Validate()` called by `bootServe` (fail-fast on empty required secrets; WARN on parse-fallback); wrap the default `slog` handler with a `SanitizeString`-applying `ReplaceAttr`.
- Test: boot with empty `NEO4J_PASSWORD` → non-zero exit with a clear message; a log-redaction test on a DSN-bearing error.

## [P2] O-05 — `/healthz` checks only Postgres; no Neo4j/provider readiness; no `/readyz` split (R-14 residual)

- Evidence: `cmd/aura/serve.go:178-191` (`HealthCheck` = `pool.Ping` only); `internal/agui/server.go:89-109`
- Problem: The runtime hard-depends on Neo4j (memory/graph), the embed sidecar, and the LLM provider — none are probed by `/healthz`. There is no liveness/readiness split, so an orchestrator can't distinguish "restart me" from "don't route yet."
- Impact: A healthy-reporting daemon can be fully unable to serve a memory-backed turn; load balancers route to it.
- Recommended fix: Extend `HealthCheck` to probe Neo4j + embed reachability; add `/readyz` requiring all deps, keep `/healthz` as pure liveness; make provider reachability a shallow cached check (don't burn tokens per probe).
- Test: `/healthz` 503 with Neo4j down; `/readyz` distinguishes from `/healthz`.

## [P2] B-08 — No stream idle-timeout watchdog: a stalled-but-open stream is bounded only by the whole-call timeout

- Evidence: `internal/agent/llm_agent.go:184` (per-call `WithTimeout(TotalTimeoutSec)`); grep for an idle/per-chunk watchdog in `internal/llm` + `internal/agent` returns nothing
- Problem: A single `TotalTimeoutSec` bounds the whole LLM call. There is no per-chunk idle watchdog: a provider that opens a stream and then stalls (chunks stop arriving but the connection stays open) is only cut off when the *total* call deadline elapses — which on long legitimate turns is set generously. Codex (`client.rs:531`) and nanobot (`runner.py:724`) both run an explicit idle-timeout watchdog; the prior audit's own "patterns to adopt" listed it.
- Impact: A stalled stream can hold a turn open far longer than necessary, tying up the in-flight slot and (without B-03's guard) the thread.
- Recommended fix: Track chunk arrival time in `consume`; if no chunk arrives within `AURA_LLM_STREAM_IDLE_TIMEOUT_SEC` (default ~300s, matching platform idle), close the stream and treat it as a retryable transport error (the existing mid-stream retry path handles re-issue).
- Test: a fake client that opens then stalls; assert the turn aborts within the idle window, not the total window.

## [P2] B-09 — Two/three divergent `secretEnvKey` implementations; shell variant misses bare `*_KEY` (R-07 divergence)

- Evidence: `internal/agent/tools/shell_exec_env.go:22-30` vs `internal/mcp/client.go:164-172` (+ a third `isSecretEnvKey` at `internal/mcp/manager/config.go:144`)
- Problem: Same security concept, divergent blocklists. The MCP filter includes the marker `"key"`; the shell filter does not. So `shell_exec`'s child-env filter passes `ENCRYPTION_KEY`, `PRIVATE_KEY`, `SIGNING_KEY`, `SSH_KEY`, `GPG_KEY`, `STRIPE_KEY` (they contain `key` but match none of shell's markers `token/secret/password/passwd/api_key/apikey/auth/bearer/credential`). The MCP filter strips them. Shell uses a denylist (leaky by construction); MCP uses an allowlist (correct, stronger).
- Impact: Inconsistent redaction across the two surfaces that most handle secrets. Low isolation impact for shell (the model can read the full env via `env` under the full-terminal model — the denylist is preview hygiene), but the inconsistency gives a false sense of stripping.
- Recommended fix: Extract one `internal/secret.IsSecretEnvKey` with a canonical blocklist (add `"key"`, consider `"private"`, `"cert"`); call it from all three sites; document that shell deliberately inherits the full env (denylist is best-effort hygiene).
- Test: a table of secret-shaped names asserted identically against both call sites.

## [P2] B-10 — Destructive-shell gate is regex-bypassable and off by default (R-19 residual)

- Evidence: `internal/agent/tools/shell_exec_env.go:71-103` (`destructiveShellMatch`), `shell_exec.go:114` (`commandForGate`), `shell_approval.go`
- Problem: The gate is opt-in (empty default → disabled) AND, when configured, is a line-level regex over the raw command. `rm -r -f`, `rm --recursive --force`, `find . -delete`, `$(echo rm) -rf`, base64-decode-pipe, a Python `shutil.rmtree`, or "write the command to a file then run the file" (which the schema itself suggests) all bypass any reasonable pattern. No default patterns ship in any `.env`, so most deployments run with it entirely off.
- Impact: The gate is a speed-bump against the model literally spelling a dangerous command, not a containment boundary. Consistent with the documented full-host-trusted-operator philosophy, but should not be relied on as a sandbox.
- Recommended fix: Document explicitly that this is advisory, not a sandbox; if stronger containment is desired, gate at the intent layer (an `ask_user` the model raises) or route untrusted commands through the named sandbox escalation; at minimum ship a conservative default pattern set.
- Test: a property test enumerating common `rm`/`drop` spellings documenting which evade a given pattern (so operators calibrate expectations).

## [P2] B-11 — `shell_exec.go` is a god-class at the 600-LOC ceiling

- Evidence: `internal/agent/tools/shell_exec.go` — 598 LOC (CLAUDE.md hard ceiling 600); `llm_agent.go` 579 LOC is the same risk
- Problem: CLAUDE.md mandates "refactor on touch, split into `<name>_<concern>.go`" and "never create a file >600 LOC." `shell_exec.go` is one edit from breaching; any touch must split it.
- Impact: Maintainability debt; the next feature edit is forced to refactor under pressure or breach the gate.
- Recommended fix: Pre-emptively split `shell_exec.go` (arg-parsing → `shell_exec_args.go`; output capture is already partly in `shell_exec_env.go`).
- Test: file-size gate (already enforced) stays green post-split.

## [P2] M-04 — Sidecar spill written outside the append transaction → orphan-on-rollback unreclaimed in a live conversation

- Evidence: `internal/conversations/store.go:296-300,375-411` (`maybeSpill` writes the file before `insertTurnAndAggregates` runs in `db.WithTx`); `internal/conversations/orphan_scan.go:60-106` (boot-only, whole-conversation-orphan only)
- Problem: For an oversized turn, `maybeSpill` writes `<seq>.content` to disk before the DB tx. The boot scan only removes a sidecar **dir** when the *whole conversation* has no DB row. A single rolled-back turn in a *live* conversation leaves an orphan `<seq>.content` the boot scan never reclaims. Correctness is unaffected (load reads the DB row's path), but disk leaks.
- Impact: Slow disk leak of orphaned spill files on any append rollback in a live conversation.
- Recommended fix: Write the spill inside the tx success path (defer until after commit), or have a periodic sweep cross-check `<seq>.content` files against actual `content_sidecar_path` rows and reclaim unreferenced ones.
- Test: force an aggregates-update failure on an oversized turn; assert no orphan `<seq>.content` survives the next sweep.

## [P2] M-05 — `dropOldestRound` can consume the only/newest user turn (R-30 residual)

- Evidence: `internal/conversations/context.go:281-313` (`dropOldestPairs`/`dropOldestRound`), `:293-298` (dangling tool-head trim)
- Problem: The error path is correct (oversized-beyond-reduction returns `ErrContextWindowExceeded`, not silent). But a narrow silent edge remains: in a body shaped `[user(oversized), assistant, tool]`, dropping the round yields `[assistant, tool]`, the tool head is trimmed, and the *current user request* is gone while the remainder may pass under cap — the turn runs with no user message visible.
- Impact: Rare, but a turn can answer from stale context with the user's actual request silently absent.
- Recommended fix: Never drop the *newest* round in `dropOldestPairs`; protect the tail round like the head. If the newest round alone exceeds cap, return `ErrContextWindowExceeded`.
- Test: newest user turn oversized, older rounds small → assert the newest user turn survives or an error is returned.

## [P2] M-06 — `$AURA_RUN_DIR` sidecars + reasoningtrace grow monotonically (no TTL/rotation) (R-33)

- Evidence: `internal/conversations/orphan_scan.go:44-58` (boot-only, orphan-only), `:160-175` (`warnIfOversized` never purges); `internal/reasoningtrace/reasoningtrace.go:64` (O_APPEND, no rotation); `llm_agent.go:214-224` (serializes full `a.history` per turn into the trace)
- Problem: `ScanOrphans` runs once at boot and only removes no-DB-row dirs. A long-lived conversation accumulates `<seq>.content`/`.result` sidecars with no per-conversation cap and no TTL. `reasoningtrace.Record` appends JSONL with no rotation and logs the *entire* history each turn, so per-turn volume scales with conversation length.
- Impact: Slow disk exhaustion in a long-running daemon; reasoning-trace (if on) is a fast grower. A full disk then fails a sidecar write and turns lose data.
- Recommended fix: Periodic (not boot-only) sweep that prunes resolved/archived conversation sidecars past a TTL + a per-conversation byte budget; size-based rotation for reasoningtrace; stop serializing full history per turn (log a digest/length).
- Test: drive many turns, assert reasoningtrace rotates at the cap; assert an archived conversation's sidecars are reclaimed by the sweep.

## [P2] O-06 — SIGTERM hard-cancels in-flight conversational turns (asymmetric drain) (R-34)

- Evidence: `cmd/aura/serve.go:80,109,127-131` — HTTP `Shutdown` + scheduler tick drain bounded, but the in-flight `Runner.Turn` under the request ctx is cancelled mid-LLM-call
- Problem: HTTP/scheduler are drained on SIGTERM, but a turn mid-tool-execution or mid-stream is aborted, not given a bounded window to reach a terminal frame.
- Impact: Deploys/restarts abandon in-flight user turns with no terminal `RUN_FINISHED`/persisted answer.
- Recommended fix: Give in-flight turns a bounded completion grace on shutdown (derive the turn ctx so it gets a bounded `WithTimeout` rather than immediate cancel), within `aguiShutdownTimeout`.
- Test: SIGTERM during an in-flight turn → the turn reaches a terminal frame within the grace window or is cleanly errored, not silently dropped.

## [P2] O-07 — No Windows CI lane despite OS-specific kill-path shipped in Windows binaries (R-36)

- Evidence: every job in `.github/workflows/ci.yml` is `runs-on: ubuntu-latest`; `internal/agent/tools/shell_exec_windows.go:23` (`taskkill /F /T`); goreleaser builds Windows binaries
- Problem: The Windows process-group-kill path and `runtime.GOOS=="windows"` branches compile into shipped Windows binaries but never run in CI. The race detector and kill-path tests are Linux-only; the primary dev host is Windows 11.
- Impact: Windows-specific regressions ship unverified.
- Recommended fix: Add a `windows-latest` job running `go build`, `go vet`, and `internal/agent/tools` unit tests (race if feasible); gate kill-path tests to run on Windows.
- Test: the Windows lane itself; a `taskkill` kill-path test on `windows-latest`.

## [P2] R-41 — Per-session tool state (todo, background shells) never evicted in the daemon

- Evidence: `internal/agent/tools/todo.go:19,96-100` (`byID` map), `internal/agent/tools/shell_bg.go:33,38` (`shells` map), `cmd/aura/main.go:126,142` (singleton registration)
- Problem: `buildBaseRegistry` registers one `TodoTool` and one `BackgroundShells` whose maps are keyed by session id; in `aura serve`/Telegram they live for the process lifetime across all conversations. `conv.Delete`/`chatDelete` purge the DB row + filesystem but never drop the registry's `byID[key]`/`shells[key]` entries.
- Impact: Unbounded in-memory growth proportional to distinct conversations served; background-shell entries pin OS processes/buffers for dead conversations.
- Recommended fix: Add an `Evict(sessionID)` method to session-scoped tools, called from the `ConversationCleaner`/archive/delete path; optionally TTL idle entries.
- Test: create N session keys, delete the conversations, assert `byID`/`shells` no longer hold those keys.

## [P2] T-01 — No fuzz, no benchmarks, and no documented mutation score for any agent-core file

- Evidence: `grep "func Fuzz"`/`func Benchmark` in `internal/agent` → 0; mutation scores documented only for telegram/skills/web/agui files, none for `budget.go`/`budget_dedup.go`/`llm_agent_completion.go`/tools
- Problem: The agent runtime parses untrusted LLM-emitted tool-call JSON, MCP descriptions, shell args, and filenames, yet has no fuzz harness; the per-call budget/dedup hot path has no benchmark; CLAUDE.md mandates mutation ≥70% on each phase's critical files but no agent-core file has a recorded score.
- Impact: Parsing-panic/injection surfaces and hot-path regressions go unmeasured; the mutation mandate is unmet for the core.
- Recommended fix: Add `FuzzToolArgsUnmarshal` (per tool arg struct) + `FuzzMCPDescriptionFraming`; `BenchmarkBudget_BeforeToolCall`/`BenchmarkDedupRing_Push`; run + record `go-mutesting` on `budget*.go`, `llm_agent_completion.go`.
- Test: the harnesses themselves.

---

# P3 — Improvement / hardening

## [P3] M-07 — `anyInt` drops `json.Number` → token-zeroing on any JSON-decoded final Event (R-42, dormant)
`internal/runner/runner_persist.go:412-423` — `anyInt` has no `json.Number` case (sibling `anyFloat` does, per IN-03). The live runner path is in-process so it's dormant; any future JSON round-trip of a final Event (AG-UI replay, queue, cross-process relay) persists `prompt/completion/cached_tokens = 0` while `cost_usd` survives. **Fix:** add `case json.Number: n,err := f.Int64(); if err==nil { return int(n) }`. One line, zero risk. **Test:** `anyInt(json.Number("100"))==100` + a final-Event round-trip.

## [P3] B-12 — Mid-stream retry replays already-streamed partial chunks to the user (cosmetic)
`internal/agent/llm_agent.go:245-256` + `cmd/aura/chat_render.go:105-117`. On a retryable mid-stream error, `consume` has already yielded partial chunks (written to screen); the retry streams the clean answer and `flushRemainder` resets/re-emits. The persisted answer is correct, but the user *saw* the discarded partial. **Fix:** buffer streamed chunks until the stream completes cleanly (emit on success, drop on retry). **Test:** render path with `TextThenErr(retryable,"partial")` then a clean stream; assert no garbled duplicate.

## [P3] B-13 — Stream-open retry classifier still substring-matches network error text (R-38 residual)
`internal/agent/llm_agent_stream_retry.go:96,99-115`. After typed checks (HTTPError 429/5xx, `net.Error.Timeout`, `url.Error`), it falls through to `retryableNetworkText` matching substrings ("wsarecv", "connection reset", "unexpected eof", bare "eof"). Brittle to locale-translated OS errors / Go string changes. **Fix:** prefer typed sentinels (`io.ErrUnexpectedEOF`, `syscall.ECONNRESET/ECONNREFUSED` via `errors.As/Is`); keep text only as a last resort. **Test:** `errors.Is(err, syscall.ECONNRESET)` classifies retryable independent of the message.

## [P3] O-08 — Span coverage is `llm.request`-only; no turn span, no tool spans
`internal/agent/tracing.go:114` + `llm_agent.go:186`. A turn spans multiple LLM calls + tool dispatches, but there is no root `agent.turn` span to parent them and tool executions (which can dominate latency) are untraced. **Fix:** start an `agent.turn` root span in `Run`; a `tool.execute` span per dispatched call with `tool.name`/duration/error. **Test:** span-tree assertion — one turn span parents N `llm.request` + M `tool.execute`.

## [P3] B-14 — `Registry.Register` silently overwrites duplicate names (R-45)
`internal/agent/tools/registry.go:102-104` — `r.tools[name] = t` last-writer-wins. MCP `Mount` guards collisions itself, but the built-in registration has no guard; a double-register of `fs_read` (or a name collision) silently shadows. Not model-reachable today. **Fix:** return an error (or panic at boot) on duplicate name. **Test:** double `Register` of one name → error.

## [P3] B-15 — Bridged MCP argument-schema descriptions are indexed/printed unframed and uncapped (R-22 residual)
`internal/agent/tools/bridge.go:140-143` (raw `inputSchema`) → `search.go:177` prints `s.Parameters` raw; `tool_search` results are not in the untrusted envelope path. A hostile mounted MCP server can pack injection text into a property `description` that reaches the model unframed when it loads the spec. MCP servers are operator-configured, so this needs a hostile/compromised server. **Fix:** cap+frame bridged `Parameters` descriptions like the top-level description, or document that MCP schemas are trusted-by-mount. **Test:** a bridged tool with an injection-laden property description → framed/capped in the loaded spec.

## [P3] B-16 — `fs_grep`/`fs_glob` walk has no node/time budget (`path:"/"` full-disk scan)
`internal/agent/tools/fs_grep.go:66-89`, `fs_glob.go:62-83`. Capped by `max_results` but `WalkDir` ignores ctx, so a rare pattern over a huge tree walks the whole filesystem; `budget.NodeTimeout` cancels the surrounding goroutine but not the walk. Low risk (self-DoS). **Fix:** add a node-count/deadline cap that the walk checks. **Test:** a deep tree with no match → walk aborts at the node cap.

## [P3] T-02 — `foldToASCII` (filename Unicode-folding) only 23.5% covered
`internal/agent/tools/send_file.go:193`. The ASCII-folding path for non-ASCII Telegram document names is the lowest-covered real-logic function; most case arms (accents, CJK, emoji) are untested — a malformed multibyte filename could yield a broken/empty download name on the primary channel. **Fix:** table test over Latin-1 accents, combining marks, CJK, emoji, empty-after-fold.

## [P3] T-03 — Deferred-tool `Spec()` constructors at 0% coverage
`tools/fs_read.go:20`, `fs_write.go:22`, `fs_glob.go:24`, `fs_grep.go:27`, `fs_edit.go:23`, `search.go:36`. No test asserts the deferred-tool spec is well-formed (name, JSON schema, `Deferred:true`); a malformed schema ships uncaught and `tool_search` returns it to the model. **Fix:** one `TestDeferredToolSpecsAreWellFormed` golden test iterating the registry.

## [P3] T-04 — `agenttest` test helpers dilute the coverage floor (measurement artifact)
`internal/agent/agenttest/mocks.go`/`fakeclient.go` at 42.6%, imported only by `_test.go` but counted by the gate (`scripts/coverage_gate.sh:44` does not exclude them). Uncovered mock methods are dead weight in the owned-surface denominator. **Fix:** add the `agenttest` path to the gate filter (it's genuinely test infrastructure, like `sqlc`), or build-tag it out.

## [P3] M-08 — `EnsureConversation` race reconciliation can mask a real create failure
`internal/runner/runner.go:186-197`. On `NewConversationWithID` error it re-`Get`s and returns nil ("a concurrent creator won"), swallowing the original create error whenever the re-Get succeeds — including a transient that left the row half-initialized. **Fix:** distinguish a `23505` unique-violation (benign race) from other errors via SQLSTATE before swallowing. **Test:** concurrent `EnsureConversation` → one row, no error; a non-unique create error is surfaced.

## [P3] R-26 — Ledger is best-effort, not a pre-execution audit gate (tracked, by design)
`internal/runner/runner_persist.go:76-81` — a ledger insert failure is logged and execution continues; the ledger is observability, not permission. Accepted as a documented decision; revisit if a write-ahead intent row (B-01) lands, which would also serve as the audit gate.

---

## Appendix — prior findings verified CLOSED (do not re-open)

R-02 (MCP per-call timeout + ctx-aware read), R-03 (`fs_edit` empty `old_string` rejected), R-04 (`WithDeadline`/`NodeTimeout` wired + `shell_exec` clamp — genuinely called now), R-06 (one-transaction pause + load-time repair), R-07-shell (child-env denylist + preview redaction — see B-09 for the divergence), R-08 (subprocess ring/tail cap + env knobs), R-10 (model-facing `task approve` removed), R-11 (retry/backoff + breaker checked before call — see B-05 for lifetime), R-12 (parallel fan-out semaphore cap, default 4), R-13 (mid-stream bounded re-issue), R-15 (single budget owner via `OwnsBudget`), R-16 (ctx check + empty-pass charge — see B-07 for the budget-owner edge), R-17 (coverage floor now gates `agent/tools`), R-18 (`send_file` double-`EvalSymlinks` fence), R-20 (MCP reconnect-no-replay), R-21 (MCP default `Mutating`), R-23/R-24/R-43 (sidecar opaque-id grammar + `0o600` + `read_tool_output` clamp), R-37 (finalize/critic errors recorded, fail-open by design), R-39 (terminal-after-batch partition removes the synthetic-duplicate path), R-35 (background-shell `Shutdown` + cap + pruning).
