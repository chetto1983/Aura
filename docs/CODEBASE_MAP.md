<!-- markdownlint-disable -->
# Aura — Codebase & Function Map

**Generated:** 2026-06-15 · **Module:** `github.com/chetto1983/aura` · **Go:** 1.26

An exhaustive, package-by-package inventory of Aura's Go codebase: every exported
function, type, and method (plus the load-bearing unexported logic), with a
one-line purpose grounded in the source. Produced by reading the actual tree
(`go list ./...` → 49 packages), **not** the planning docs (which are stale).

> This is the engineering reference. For the narrative architecture see
> [ARCHITECTURE.md](ARCHITECTURE.md); for the product/feature view see
> [CAPABILITIES.md](CAPABILITIES.md) and [TECHNICAL_OVERVIEW.md](TECHNICAL_OVERVIEW.md).

## How to read this

Packages are grouped into seven layers. Within each package: a 2–3 sentence role
summary, then per-file symbol listings. `Foo(...)` = exported function/method;
`type Foo` = exported type; lowercase entries are significant unexported helpers
kept because they carry core logic.

## Package index (49 packages + `cmd/aura`)

| Layer | Packages |
|---|---|
| **Agent runtime core** | `agent`, `agent/workflow`, `agent/panicobs`, `agent/agenttest` |
| **Tools & MCP** | `agent/tools`, `agent/mcptools`, `mcp`, `mcp/manager` |
| **LLM & reasoning router** | `llm`, `llm/openai_compat`, `agent/prompt`, `reasoningfifo`, `reasoninglearn`, `reasoningstore`, `reasoningtrace` |
| **Learning substrate, scoring & primitives** | `semindex`, `activelearn`, `scoring`, `toolselectlearn`, `toolselectstore`, `toolinvocations`, `cachemetrics`, `boundedbuffer`, `canonicaljson` |
| **Persistence & knowledge** | `db`, `db/sqlc`, `knowledge`, `documents`, `conversations`, `identity`, `profile`, `secret` |
| **Capabilities** | `swarm`, `web`, `skills`, `skilladapters`, `cron`, `cron/handlers`, `onboarding`, `eval`, `runner` |
| **Transport, channels & UX** | `agui`, `channels`, `channels/telegram`, `askuser`, `setup`, `config`, `obs`, `cmd/aura` |

---

## Agent Runtime Core

### `internal/agent` — the cornerstone runtime: the open `Agent` interface, `Event`/`Actions` model, `Budget` tree, and the `LlmAgent` tool-dispatch run-loop
This package owns the contract every later phase implements or consumes: an intentionally-open `Agent` interface (no unexported seal, diverging from google/adk-go), a single-Run-scoped `InvocationContext` passed by value, a forward-compatible OTel/W3C-shaped `Event`/`Actions`/`LLMResponse` model, a shared-`*atomic.Int32` `Budget` tree with a two-phase tool-call dedup ring, and `LlmAgent` — the budget-gated, streaming, tool-dispatching loop that drives the OpenAI-compat client. It also carries the canonical `SystemPrompt`, in-process + command hooks, untrusted-output trust wrapping, OTel span minting, and Prometheus/expvar metrics. ~3,500 LOC of non-test source across 25 files.

**agent.go**
- `Agent interface` — the open cornerstone contract: `Name() string`, `Description() string`, `Run(InvocationContext) iter.Seq2[*Event, error]`, `SubAgents() []Agent`, `FindAgent(name string) Agent`. Termination/budget-exhaustion travel as Events, never the error slot (D-04).
- `BudgetOwner interface` — optional capability: `OwnsBudget() bool`; agents enforcing their own budget gates expose it so workflow parents stay observational (no double-charge).
- `AgentOwnsBudget(a Agent) bool` — reports whether `a` opted into owning its budget via `BudgetOwner`.
- `InvocationContext struct` — single-Run-scoped, by-value: `Ctx`, `Agent`, `RequestID` (UUIDv7/TraceID), `SpanID [8]byte`, `ParentSpanID *[8]byte`, `Branch` (label only), `Budget *Budget`.
- `(InvocationContext) WithContext(ctx) InvocationContext` — returns a copy with replaced `Ctx`; never mutates the receiver (D-24).
- `(InvocationContext) WithSubAgent(sub) InvocationContext` — returns a copy re-pointed at `sub`, keeping the SAME `*Budget` (shared counter + same dedup ring, D-09).

**errors.go**
- `ErrBudgetExhausted` — exported sentinel for callers inspecting termination OUTSIDE the Event stream via `errors.Is`.

**event.go**
- `Event struct` — the single signal type: `RequestID`, `SpanID`, `ParentSpanID`, `Author`, `Branch`, `ThreadID`, `MessageID`, `LLMResponse`, `Actions`, `Timestamp`. Forward-compat AG-UI shape.
- `LLMResponse struct` — `Content`, `Reasoning` (stream-only CoT, never persisted), `ToolCalls []llm.ToolCall`, `FinishReason`.
- `Actions struct` — control signals: `Escalate`, `StateDelta`, `ArtifactDelta`, `AwaitingInput *AwaitingInput` (HITL pause), `ToolInvocation *ToolInvocation`, `DiscardStreamed` (mid-stream-retry repudiation, B-12).
- `ToolInvocationStart`/`ToolInvocationEnd` consts — lifecycle labels.
- `ToolInvocation struct` — typed audit payload around real tool execution (call id, name, args/bytes, batch index/size, timing, status, error, result preview/bytes/truncation/sidecar path, exit code, meta).
- `AwaitingInput struct` — pause payload (`Question`, `Options`, `Kind`, `Priority`, `ToolCallID`, `ResumeContext`, `OriginAgent`, `ProxiedFromChildID`, `ProxiedToolCallID`) — Event-side projection of `tools.ErrAwaitingUserInput`.
- `PauseOption struct` — `Label`/`Value`; redeclared (not imported from tools) to avoid a dependency cycle.
- `(*Event) SetAuthorIfEmpty(name)` — stamps Author when unset (D-14).
- `(Event) MarshalJSON()` / `(*Event) UnmarshalJSON()` — the single user-facing serialization path; via the unexported `eventWire` projection (hex span ids, `*uuid.UUID` omitempty, RFC3339Nano UTC), HTML-escaping off, byte-stable round-trip.
- unexported helpers: `hexPtr`, `uuidPtrIfSet`, `decodeSpan`, `decodeSpanPtr`.

**budget.go**
- `Budget struct` — bounds one run: shared `*atomic.Int32 steps`, `deadlineWallclock`, injectable `now`, `dedupWindow`, per-branch `dedupRing`, `branchSoftCap`/`branchConsumed`, `exemptTools`, `resultCap`, `nodeTimeout`, `softFrac`.
- `BudgetOptions struct` — explicit CLI>env>default overrides (`MaxSteps`, `MaxWallclockSec`, `DedupWindow`, `ExemptTools`, injectable `Now`), avoiding process-global env mutation.
- `NewBudgetFromEnv() (*Budget, error)` — env-only entry point.
- `NewBudget(opts) (*Budget, error)` — applies CLI>env>default precedence in one place, fail-fast on set-but-malformed `AURA_LOOP_*` values; validates ranges.
- `(*Budget) ConsumeStep() (ok bool, reason string)` — the TOCTOU-safe gate (wallclock check then atomic decrement-check-restore); returns only HARD reasons `"max_steps"`/`"wallclock"`.
- `(*Budget) Remaining() int` — current shared step balance (never negative).
- `(*Budget) BranchConsumed() int` — steps THIS branch consumed (the `<budget>` "used" count; no `MaxSteps()` getter by design).
- `(*Budget) Now() time.Time` — the run's single time source.
- `(*Budget) SetMaxSteps(n int32)` — overrides the shared counter (boot-time CLI flag).
- `(*Budget) SoftCapExceeded() bool` — non-terminal per-branch fair-share signal (D-12).
- `(*Budget) Child(fanout int) *Budget` — forks a PARALLEL branch: shares the same counter+deadline+clock, distinct dedup ring, passive soft cap (spawn-time snapshot, timing-dependent by design).
- `(*Budget) WithDeadline(parent) (ctx, cancel)` — context bounded by the wallclock deadline.
- `(*Budget) NodeTimeout() time.Duration` — optional per-node soft timeout.
- env consts (`envMaxSteps` … `envDedupResultCap`) and defaults (`defaultBudgetMaxSteps=25`, `defaultBudgetWallclockSec=300`, `defaultDedupWindow=3`, etc.).
- unexported: `errMalformed`, `resolveInt`, `int32Knob`, `envIntFailFast`, `envFloatFailFast`, `softCap`.

**budget_dedup.go**
- `dedupRing struct` + `resultTrack`/`fingerprint` types — per-branch ring of recent tool-call fingerprints plus latest-result-preview progress tracking (two-tier, veto-only result).
- `(*Budget) BeforeToolCall(name, argsCanonicalJSON) (dedup bool, reason string)` — PRE-execution gate: dedups on period-1/period-2/period-N repeat when no progress veto applies; exempt tools never dedup. Caller-canonicalizes contract (B2).
- `(*Budget) AfterToolResult(name, argsCanonicalJSON, resultPreview)` — records bounded result preview as a progress VETO; a changed preview resets repeats so volatile-result tools fail SAFE.
- `parseExemptTools(csv) map[string]struct{}` / `ExemptToolsFromEnv(extra ...string) map[string]struct{}` — build the `AURA_LOOP_DEDUP_EXEMPT_TOOLS` allowlist (the latter exported, composes env + caller extras).
- unexported ring logic: `ringCapacity`, `newDedupRing`, `computeFingerprint`, `push`, `countConsecutive`, `isPingPong`, `isRepeatedCycle`, `equalBlocks`, `stableBlock`, `containsEntry`.

**llm_agent.go**
- `LlmAgent struct` — the budget-gated tool-dispatch loop driving `llm.Client`; in-memory `history` with byte-stable `messages[0]`. Carries per-run recovery/completion/truncation counters, breaker, and optional reasoning classifier.
- `LlmAgentConfig struct` — constructor inputs (Name, Description, Client, LLM `llm.Config`, Registry, PreviewCap, RunDir, SessionID, Workspace, UserTurns, shared `Classifier`/`Embedder`/`ExampleStore`, shared `Breaker`, `HookManager`).
- `NewLlmAgent(cfg) *LlmAgent` — builds the agent; seeds `history` with the `systemMessage()` then user turns.
- `(*LlmAgent) Name()` / `Description()` / `OwnsBudget() bool` (true) / `SubAgents()` (nil leaf) / `FindAgent(name)`.
- `(*LlmAgent) Run(ic) iter.Seq2[*Event,error]` — the core loop: `agent.turn` span + hooks, per-iteration budget gate (recovery-or-finalize), prompt build with live `<budget>` tail-injection + adaptive reasoning tier, stream open w/ retry, `consume`, truncation classification, ask_user pause exclusivity, dispatch, terminal paths (text_response / content-stop / budget trip / empty-response recovery / infra error). Panic-recovering deferred.
- `(*LlmAgent) runTerminal(...)` — handles the `text_response` terminal call (parse error feedback, completion-gate veto, or final answer + `finalEvent`).
- `(*LlmAgent) appendSyntheticToolResults(calls, content)` — appends synthetic RoleTool results to keep the wire valid before retry/finalize.
- `toolRunResult struct` — per-call run record (ids, args, timing, preview, `tools.ToolResult`, err, `Mutating` flag).
- `hookToolRunResult(call, startedAt, res) toolRunResult` — builds a run result from a hook-supplied tool result.
- `(*LlmAgent) runTool(ctx, budget, call, startedAt) toolRunResult` — dispatches one tool call; injects tool-call ctx + swarm ctx + node timeout; unknown-tool/exec errors become model-visible previews, never panics; emits a `tool.execute` span.
- `(*LlmAgent) appendToolError(callID, err)` — appends a RoleTool error so the model self-corrects.
- consts `truncationNotice`, `terminalTool` (`"text_response"`).
- unexported: `resolveBreaker`.

**llm_agent_consume.go**
- `(*LlmAgent) consume(ch, ic, ...) (text, calls, finish, usage, stopped, streamErr)` — drains one provider stream, re-emitting chunk/tool-call/reasoning Events as they arrive (reasoning never enters accumulated text, #57); honors the iter.Seq2 yield-after-false drain-to-close contract.

**llm_agent_dispatch.go**
- `(*LlmAgent) dispatch(ic, ..., calls, usage, yield) (done bool, infraErr error)` — runs one turn's tool calls: partitions terminal `text_response` from runnable calls, runs `BeforeTool` hooks + dedup gate serially in order, executes runnable calls concurrently via `executeBatch`, then processes results serially (history append + `AfterToolResult` + result Event), arming the completion gate on a mutating turn.

**llm_agent_events.go**
- `(*LlmAgent) newEvent(...)` — stamps common trace identity + UTC timestamp.
- `chunkEvent`, `reasoningChunkEvent` (redacts CoT unless `ShowReasoning`), `discardStreamedEvent`, `toolCallEvent`, `toolStartEvent`, `toolResultEvent` (full `ToolInvocation` end payload + lifts `send_file` artifact onto `ArtifactDelta`), `toolPreviewEvent`, `finalEvent` (usage footer in StateDelta), `terminalBudgetEvent` (D-04 escalate + termination_reason/limit_hit).
- `metaArtifact(meta) (map[string]any, bool)`, `toolResultMetaMap(meta)`, `exitCodeFromMeta(meta)`, `usageStateDelta(usage)`, const `redactedReasoningDelta`.

**llm_agent_finalize.go**
- `(*LlmAgent) finalize(ic, ..., reason, yield)` — forced-finalization: issues ONE tool-free synthesis turn outside the budget gate and emits a non-empty `finalizeEvent` carrying the trip reason.
- `(*LlmAgent) maybeRecover(toolName) bool` — single counter-gated recovery decision for both trip sites: first trip appends one user-role nudge (tool/generic variant) and returns true; subsequent → false.
- `(*LlmAgent) maybeRecoverEmptyResponse() bool` — empty-completion variant, shares the recovery counter.
- `(*LlmAgent) synthesizeWithFallback(ic) (answer, usage)` — runs synthesis, retries once on empty/error, else returns the deterministic Italian `stubDigest()`.
- `(*LlmAgent) stubDigest()` — deterministic Italian fallback (lead-in + one bullet per RoleTool result, byte-capped); no LLM call.
- `(*LlmAgent) synthesize(ic) (answer, usage, err)` — the tool-free `ToolChoice="none"` synthesis call (never spends a step; bounded ctx).
- `(*LlmAgent) finalizeEvent(...)` — `finalEvent` augmented with the termination StateDelta keys.
- `toolNamesByCallID(history)`, `truncateBytes(s, n)`; consts `finalizeNudge`, recovery nudges, `stubLeadIn`, `stubBulletCap=500`, `stubDigestCap=8000`.

**llm_agent_completion.go**
- `(*LlmAgent) gateCompletion(ic, answer) (veto bool, feedback string)` — completion critic gate (amendment #54/D-43): vetoes ONCE on a side-effecting turn when the critic returns NOT_DONE; fails OPEN otherwise.
- `(*LlmAgent) runCompletionCritic(ic, answer) (done, reason, ok)` — issues the tool-free critic call (compact context, not the full prompt) and parses the verdict.
- `(*LlmAgent) criticModel()`, `completionCriticUser(answer)`, `sideEffectDigest()` (per-tool `name(args) → result` digest, tail-preferred, byte-bounded).
- `parseCriticVerdict(text) (done, reason, ok)` — DONE/NOT_DONE parse (NOT_DONE first, contains "DONE"); unparseable → fail-open.
- `truncateBytesKeepingTail`, `truncateTailBytes`, `extractReason`, `lastUserRequest(history)` (skips agent nudges), `isAgentNudge(content)`; consts `completionCriticSystem`, `completionVetoPrefix`, critic byte/token caps.

**llm_agent_pause.go**
- `(*LlmAgent) pauseCalls(ctx, calls) []pauseCall` — name-gated ask_user pause detection; ONLY `ask_user` pauses (amendment #51/D-40), returns the ask_user-only rewrite (intra-turn exclusivity, D-A1-07).
- `pauseCall struct`, `pauseToolCalls(pauses) []llm.ToolCall`.
- `(*LlmAgent) detectPause(ctx, call) (*tools.ErrAwaitingUserInput, bool)` — pre-executes the ask_user tool under the live ctx and detects the sentinel via `errors.As`.
- `(*LlmAgent) emitPauses(...)` — yields one pause Event per detected call (no RoleTool result; loop suspended).
- `(*LlmAgent) pauseEvent(...)` — builds the `Actions.AwaitingInput` Event.
- `pauseOptions(opts) []PauseOption`; var `askUserToolName` (read from the tool's Spec).

**llm_agent_reasoning.go**
- `(*LlmAgent) adaptiveReasoningTier(ctx) (prompt.ReasoningTier, bool)` — fast-path local embedding classifier (granite, ~10ms) replacing the per-turn LLM router; falls back to static-low or the LLM router round-trip.
- `(*LlmAgent) reasoningRouterTimeout()` — caps the router at ≤2s.
- `resolveClassifier(cfg) *prompt.ReasoningClassifier` — prefers the shared injected classifier, else builds one from the embedder.

**llm_agent_retry.go**
- `(*LlmAgent) execTool(ctx, tool, mutating, args) (tools.ToolResult, error)` — runs one tool, retrying a non-mutating TRANSIENT failure with linear backoff (mutating tools never retried — at-most-once side effects).
- `isTransientToolErr(err) bool` — type-based transient classification (`net.Error.Timeout`/`context.DeadlineExceeded`), never string-matching.
- consts `maxToolRetries=2`, var `toolRetryBaseDelay`.

**llm_agent_stream_retry.go**
- `(*LlmAgent) streamWithOpenRetry(ctx, req, requestID) (<-chan llm.Chunk, error)` — circuit-breaker-aware stream open with bounded retry + Retry-After-aware backoff.
- `retryableStreamOpenError(err) bool` — classifies retryable transport errors (HTTP 429/5xx, net timeouts, url errors, idle-timeout sentinel, typed network sentinels, last-resort substring table).
- `llmErrorKind(prefix, err) string` — bounded metric kind for an LLM error.
- `retryableNetworkText(s) bool`, `streamOpenRetryDelayFor(err)`; consts `streamOpenMaxAttempts=2`, delays.

**llm_agent_parallel.go**
- `(*LlmAgent) executeBatch(ctx, budget, calls, startedAt) []toolRunResult` — runs a batch of tool calls' `Execute()` (inline for one, concurrent for >1 with a semaphore limit), index-aligned results.
- `(*LlmAgent) runToolRecovering(...)` — panic-recovering wrapper around `runTool` (records to panicobs site `execute_batch`).
- `maxParallelTools() int` — reads `AURA_LOOP_MAX_PARALLEL_TOOLS` (default 4).

**llm_agent_truncation.go**
- `(*LlmAgent) classifyToolTruncation(finish) truncationAction` — handles `finish_reason="length"` tool-call turns: first truncation nudges, second finalizes (prevents the 203-turn thrash).
- `truncationAction` type + `truncationProceed`/`truncationContinue`/`truncationFinalize`; consts `maxTruncatedToolTurns=2`, `truncatedToolNudge`.

**llm_agent_args.go**
- `parseTextResponse(rawArgs) (string, error)` — validates the terminal-tool arguments (empty/malformed → model-visible error).
- `normalizeContentStopAnswer(raw)` / `parseTextResponsePayload(raw) (string, bool)` — unwrap a `text_response` payload emitted as plain content.
- `canonicalArgs(rawArgs) []byte` — canonicalizes tool args for the dedup fingerprint (B2; raw fallback on non-JSON).

**swarm_context.go**
- `swarmCtxKey struct{}` (private) + `SwarmContextValue struct` (Budget, Registry, Client, LLMCfg, ConvID) — carries parent deps to the swarm_spawn runner adapter.
- `WithSwarmContext(ctx, budget, registry, client, llmCfg, convID) context.Context` — injects the value (mirrors `tools.WithToolCallContext`).
- `SwarmContext(ctx) (SwarmContextValue, bool)` — reads it; ok=false on a non-swarm path.

**prompt.go**
- `SystemPrompt` const — Aura's canonical XML-tagged system prompt (byte-stable, cache-preserving; explains mechanisms without enumerating volatile tools).
- `systemMessage() string` — returns the byte-stable `messages[0]` content.

**trust.go**
- `renderToolResultForPrompt(toolName, res) string` — wraps untrusted tool output in a nonce `<tool_output trust="untrusted">` envelope (prompt-injection containment); trusted built-ins pass through.
- `trustedToolNames` map — explicit safe-built-in allowlist (everything else defaults UNTRUSTED, AG-052 fail-safe).
- unexported: `untrustedSource`, `wrapUntrustedToolOutput` (HTML-escape + NFKC normalize), `toolOutputNonce` (crypto/rand, panics on entropy failure).

**hooks.go**
- `FailPolicy` type + `FailClosed`/`FailOpen` consts + `(FailPolicy) String()` — per-hook fault policy.
- `HookTurn struct` — immutable turn identity into lifecycle hooks.
- `ModelHookResult`, `ToolHookResult`, `ToolResultHookResult` structs — hook short-circuit/rewrite payloads.
- `Hook interface` — `OnTurnStart`/`BeforeModel`/`BeforeTool`/`AfterTool`/`OnTurnEnd`.
- `HookManager struct` — composes hooks in registration order (first non-nil result wins).
- `NewHookManager(hooks ...Hook)` / `NewHookManagerWithPolicy(policy, hooks ...)` — constructors (default FailClosed).
- `(*HookManager) Register(h)` / `RegisterWithPolicy(h, policy)` — append non-nil hooks.
- `(*HookManager) OnTurnStart` / `BeforeModel` / `BeforeTool` / `AfterTool` / `OnTurnEnd` — the dispatch methods (nil-safe, panic-recovering, policy-gated).
- unexported: `policyAt`, `hookFault`, `recoverHook`, `(*LlmAgent) hookTurn(ic, reason)`.

**hooks_command.go**
- `CommandHookConfig struct` — trust-gated out-of-process hook config (Name, Command, Args, Env, ExpectedSHA256, Timeout).
- `CommandHookEvent` / `CommandHookDecision` structs — JSON stdin envelope / stdout response.
- `CommandHook struct` (implements `Hook`) + `NewCommandHook(cfg) (*CommandHook, error)` — constructs a hash-gated absolute-path local-command hook.
- `(*CommandHook) OnTurnStart`/`BeforeModel`/`BeforeTool`/`AfterTool`/`OnTurnEnd` — per-event execution with rewrite (bounded by caps), deny, and crashed-rewrite rejection (AG-030).
- unexported: `run` (hash-verify + exec + parse, returns crashed flag), `recordRewrite`, `rejectCrashedRewrite`, `verifyTrust`, `validateHookRequest`/`validateHookToolRewrite`/`validateHookToolResult` (AG-003 caps), `commandHookEnv`/`allowedCommandHookParentEnv` (secret-stripping env), `resolveHookCommand` (absolute-path requirement, AG-054), `fileSHA256`, decision parsers/helpers; consts `defaultCommandHookTimeout`, `maxHookRewrite*`.

**metrics.go**
- `agentMetrics struct` + `newAgentMetrics(reg, publishExpvar)` — dual expvar + Prometheus counters/histograms for budget steps, tool dispatch, stream open/retry, turn outcomes, LLM/tool errors, hook outcomes, token/cost totals, span export/entropy failures.
- package-level recorders: `recordBudgetConsumeStep`, `recordToolDispatch`, `recordLLMStreamOpen`, `recordLLMStreamRetry`, `recordToolDuration`, `recordTurnOutcome`, `recordLLMDuration`, `recordLLMError`, `recordToolError`, `recordHookOutcome`, `recordUsage`, `recordSpanExportFailure`, `recordSpanIDEntropyFailure`, `recordRecoveredPanic(site)` (delegates to panicobs).
- unexported helpers: expvar/prometheus constructors (`newExpvarInt/Float/Map`, `registerCounter/Histogram/CounterVec` with already-registered reuse), `metricLabel` (bounded sanitized label).

**tracing.go**
- `newTracerProvider(ctx, mode, endpoint)` / `NewTracerProvider(...) (TracerProvider, error)` — builds the real OTel exporter per `AURA_OTEL_EXPORTER` ∈ {otlp,stdout,none}; installs the global provider.
- `TracerProvider interface` — `Shutdown(ctx) error` (narrow binary-edge handle, hides the SDK type).
- `countingSpanExporter struct` + `ExportSpans` — wraps the exporter to count/log export failures.
- `mintSpanID() [8]byte` / `rootSpanIDs() (span, parent)` — crypto/rand 8-byte SpanID minting (resolves the Phase-2 deferral); zero-id fallback on entropy failure.
- `startLLMSpan(ctx)`, `startTurnSpan(ctx, requestID, threadID)`, `endTurnSpan(span, reason)`, `startToolSpan(ctx, name, mutating)`, `endToolSpan(span, errMsg)`, `setSpanAttrs(span, model, provider, requestID, usage)` — the `agent.turn`/`llm.request`/`tool.execute` span helpers (O-08 nesting; never stamps an api_key, D-28).
- consts `exporterNone`/`exporterStdout`/`exporterOTLP`, `tracerName`; var `spanIDReader`.

### `internal/agent/workflow` — deterministic + concurrent orchestrators (`SequentialAgent`, `LoopAgent`, `ParallelAgent`) composing the `agent.Agent` interface
Holds the three workflow agents (adapted from google/adk-go), each returning the `agent.Agent` interface from a factory constructor. They share one `*Budget` tree down the agent tree; `Sequential`/`Loop` share the dedup ring across sub-runs while `Parallel` forks a distinct ring per child. ~650 LOC across 4 files.

**workflow.go**
- `joinBranch(parent, child) string` — dot-joins branch labels (D-15); empty parent yields the bare child.
- `findInTree(self, subs, name) agent.Agent` — shared `FindAgent` contract: self-or-DFS-recurse, nil when absent (D-01).

**sequential.go**
- `SequentialAgent struct` + `NewSequential(name, subs ...agent.Agent) agent.Agent` — runs subs once each in declaration order.
- `(*SequentialAgent) Name()` / `Description()` / `Run(ic)` (each sub under `WithSubAgent`+dot-joined branch; early-return on a sub error or escalate Event, Req#4) / `SubAgents()` / `FindAgent(name)`.

**parallel.go**
- `ParallelAgent struct` + `NewParallel(name, subs ...agent.Agent) agent.Agent` — runs subs concurrently, fans Events into one channel, yields serially from the iterator frame.
- `result struct` — fan-in payload (event/err + per-Event `ack` backpressure channel).
- `(*ParallelAgent) Run(ic)` — errgroup fan-out; each child forks `ic.Budget.Child(len(subs))`; an escalate Event fires a captured cancel to stop siblings (D-03); a real child error surfaces once via the error slot (D-04); cancelled siblings drain returning nil (D-05).
- `runSub(ctx, ic, sub, results, done) error` — streams one child's Events with synchronous ack backpressure; multi-arm selects guarantee a goroutine-leak-free exit; panic-recovering (panicobs site `workflow_parallel`).
- `(*ParallelAgent) Name()` / `Description()` / `SubAgents()` / `FindAgent(name)`.

**loop.go**
- `LoopAgent struct` + `NewLoop(name, maxIter uint, subs ...agent.Agent) agent.Agent` — re-runs subs until maxIterations, a sub escalate, budget exhaustion, dedup loop, ctx cancel, or no-progress.
- `(*LoopAgent) Run(ic)` — drives iterations; for non-budget-owning children charges one step per consumed tool call (1:1 with step Events, WR-05) and one workflow step per empty pass; budget-owning children are observed without charging; emits explicit terminal Events; no-progress + iteration-limit guards.
- `(*LoopAgent) guardToolCall(...) (terminated, broke bool)` — per-tool-call dedup + budget gates, yields the consumed call as one step Event; skips dedup accounting for within-turn duplicate fingerprints (WR-02).
- `scopeToToolCall(ev, tc, last) *agent.Event` — narrows a multi-tool Event to a single-call step Event; escalate rides only the final per-call Event (WR-01).
- `(*LoopAgent) terminalEvent(ic, reason, stepsConsumed)` / `terminalEventKind(ic, kind, reason, stepsConsumed)` — explicit budget-exhausted/dedup/no_progress/iteration_limit termination Events (D-04 shape).
- `(*LoopAgent) Name()` / `Description()` / `SubAgents()` / `FindAgent(name)`.
- helpers `iterLabel(i)`, `toolCalls(ev)`, `resultPreview(ev)`, `canonArgs(arguments)`; const `defaultLoopMaxIterations=1000`.

### `internal/agent/panicobs` — bounded-cardinality recovered-panic observability
Single-file package providing low-cardinality expvar + Prometheus counters for where a panic was recovered, consumed by the agent/workflow/swarm recover sites. ~65 LOC.

**panicobs.go**
- site consts `SiteExecuteBatch`, `SiteLlmAgentRun`, `SiteWorkflowParallel`, `SiteSwarmWave`, `SiteShellBGReaper` (+ unexported `siteUnknown`) and `allowedSites` map — the bounded label set.
- `Record(site string)` — increments the recovered-panic counter (expvar + Prometheus), normalizing unknown sites.
- `Count(site string) int64` — reads the expvar-backed count for a site.
- unexported: `normalizeSite`; vars `expRecoveredPanics`, `promRecoveredPanics` (both named `aura_agent_panic_total`).

### `internal/agent/agenttest` — shared `Agent` mocks + a deterministic fake `llm.Client` for runtime tests
Test-support package (one-way import: imports `internal/agent`, never the reverse outside `_test.go`) supplying the canonical Agent fixtures the workflow/dry-run/Phase-3/9 tests reuse, and a network-free scripted `llm.Client`. Invariant: no mock forks a fresh budget — all consume the shared `ic.Budget`. ~380 LOC across 2 non-test files.

**mocks.go**
- compile-time `agent.Agent` assertions for all four mocks.
- `InfiniteToolCallAgent struct` — SC#2 budget-exhaustion fixture: emits the SAME tool call forever; only budget/consumer-break stops it.
- `EmitNThenEscalate struct` — emits N normal Events then one escalate Event (escalate-propagation fixture).
- `RecordingAgent struct` — records seen branches + emitted Events for order/label assertions; emits canned `Events`.
- `CountingAgent struct` — SC#3 shared-counter fixture: consumes `ic.Budget.ConsumeStep()` once per step, increments `Calls`, stops when refused.
- helpers `orDefault(v, def)`, `selfIfNamed(a, name)`.

**fakeclient.go**
- `FakeClient struct` (implements `llm.Client`) — deterministic scripted client: pops the next `FakeTurn` per `Stream` call, records each `Request` (with cloned Messages) for immutability/threading assertions; goleak-clean (pre-closed buffered channels).
- `FakeTurn struct` — one scripted response (`Chunks` or `Err`).
- `NewFakeClient(turns ...FakeTurn) *FakeClient`.
- `(*FakeClient) Stream(ctx, req)` / `CallCount() int` / `LastRequest() llm.Request`.
- builder helpers: `TextChunks(finish, parts...)`, `TextThenErr(err, parts...)`, `ToolCallTurn(calls...)`, `WithUsage(turn, u)`, `MakeToolCall(id, name, args)`.

---

## Tools & MCP

### `internal/agent/tools` — the agent's tool registry, ToolResult/spillover substrate, deferred-tool pattern, and every built-in LLM-facing tool

Defines the `Tool` interface the agent loop dispatches against, the `Spec`/`Registry`/`ToolResult` value types, and the **deferred-tool pattern**: tools with long descriptions or complex schemas set `Deferred = true` so the default LLM manifest shows only `Name` + `Summary`; the model loads the full spec on demand via the always-on `tool_search` hook (protects the prompt cache, scales to N tools at no per-turn cost). Large tool outputs are capped to a preview and spilled to a sidecar file (`NewResult`), then paged via `read_tool_output`. ~70 source files; this is the largest tools surface in the codebase.

**spec.go** — core types and registry
- `var ErrNoNonDeferredTool` — sentinel returned by `Registry.Validate` when every capability is deferred (mirrors Anthropic's "at least one tool must be non-deferred" 400).
- `type Spec struct {Name, Summary, Description string; Parameters json.RawMessage; Deferred, Mutating bool}` — LLM-visible tool metadata. `Mutating` is a runtime-only hint (never wire-encoded) that arms the completion-gate critic (amendment #54/D-43).
- `type TrustLevel string` + `const TrustUntrusted` — provenance classification for tool results.
- `type ToolResultProvenance struct {Source string; Trust TrustLevel}` — runtime-only result provenance.
- `type ToolResult struct {Preview, FullPath string; Bytes int; Truncated bool; Meta *ToolResultMeta; Provenance *ToolResultProvenance}` — value an Execute returns; `Preview` is threaded into history, full bytes spill to `FullPath`.
- `type ToolResultMeta map[string]any` — tool-specific structured audit fields.
- `type Tool interface {Spec() Spec; Execute(ctx, args json.RawMessage) (ToolResult, error)}` — the dispatch contract.
- `type Registry struct` + `NewRegistry()`, `(*Registry) Register(t Tool)` (panics on duplicate name — fail-loud wiring collision), `Get(name)`, `All()`, `Validate()` (fails closed when no non-deferred capability tool besides `tool_search`).

**registry.go**
- `Without(parent *Registry, names ...string) *Registry` — fresh registry copying parent minus the named tools, without mutating parent (strips `swarm_spawn` from swarm-worker / cron children; promoted out of `internal/swarm` to avoid an import cycle).

**result.go** — spillover substrate (D-25)
- `WithToolCallContext(ctx, sessionID, toolCallID, runDir, previewCap) context.Context` — agent injects per-Execute identity/cap.
- `NewResult(ctx, content) (ToolResult, error)` — the shared cap→preview→sidecar helper: small outputs preview-only; large ones truncate on a rune boundary, append a `read_tool_output` footer, write full bytes to `<run_dir>/conversations/<session>/<spill>.result`; sidecar write failure degrades clean.
- `NewResultReservingTail(ctx, body, footer)` — like `NewResult` but `footer` is always-visible (shell exit-code/stderr tail never sliced off).
- unexported: `validateID` (rejects ids outside `[a-zA-Z0-9_-]` before path join, T-03-07), `sidecarPath`, `truncatePreview` (UTF-8 safe), `writeSidecar` (0o600).

**manifest.go**
- `type ManifestEntry struct` + `(*Registry) Render() []ManifestEntry` (alphabetical-by-name — cache-stability load-bearing; deferred entries contribute only Name+Summary), `RenderToolDefs() []llm.ToolDef` (OpenAI-wire shape), `RenderText() string` (`[active]`/`[deferred]` listing for boot logs / `aura tools`).

**search.go** — **`tool_search`** (the deferred-spec discovery hook, NON-deferred)
- `type ToolSearch struct` — registry + `semindex.Embedder` + `semindex.Ranker` + BM25 index + learned boost.
- `(*ToolSearch) InvalidateIndex()` — signals the deferred set changed (MCP reconnect); embeds only the delta (D-03).
- `Spec()` — schema with `query` (`select:Name1,Name2` or free-text) + `max_results`.
- `Execute(ctx, raw)` — `select:` resolves by exact name (uncapped); free-text ranks semantically (embed sidecar is a hard dependency → explicit error if down, never a fake "no match").
- helpers `match`, `embedQuery`, `ensureBank` (lazy/incremental embedding bank over `searchDocument(spec)`).

**search_fusion.go** — guarded BM25 tiebreak over the embedding ranking
- `fusionEnabled()`, `guardedTiebreak(embRanked, bm25Ranked)` (embedding-primary; BM25 promotes only when its set is ≤5 AND the tool is in embedding's top-15), `stableSortScored` (brute-force, no ANN).

**search_learn.go** — stage-2 learned-centroid boost (D-07, two-tier oracle loop)
- `(*ToolSearch) EnableLearnedBoost(embed)`, `FoldLearned(examples)`, `RankForLearner(ctx, query) (top1, margin, ok)` (the `toolselectlearn.Ranker` seam: top-1 + top-2 cosine margin = the free-vs-escalate confidence signal).
- `type learnedBoost` + margin-gated `rerank` (strict no-op below threshold; anti-amplification floor).

**bm25.go** — in-process Okapi BM25 over deferred-tool search documents
- `searchDocument(s Spec) string` — flattens Name + Description + every property name into the BM25/embedding input text (D-02).
- `type bm25Index` + `newBM25Index`, `rank(query)`, `scoreDoc`; `tokenize`, `appendSchema`.

**action.go** — generic `action`-enum dispatcher (reused by `task`, `skill`)
- `type ActionRouter` + `NewActionRouter`, `Actions()` (sorted), `Dispatch(ctx, action, args)` (structured unknown-action error, never panic).

**text_response.go** — **`text_response`** (NON-deferred, terminal): emits the final user-visible reply and ends the turn.
**ask_user.go** — **`ask_user`** (NON-deferred HITL pause): `type ErrAwaitingUserInput` sentinel the dispatch loop intercepts (carries swarm-relay proxied ids, D-05); kinds `clarification|approval|choice`; validates 2-4 distinct options.
**current_time.go** — **`current_time`** (NON-deferred): the ONLY wall-clock read for the model (keeps the cached system prompt byte-stable, D-08).
**read_tool_output.go** — **`read_tool_output`** (NON-deferred): pages a BYTE range out of a sidecar by `tool_call_id`+`offset`+`limit`.
**evict.go** — `type SessionEvictor interface {Evict(sessionID)}` — implemented by tools holding per-session state; Runner calls it when a conversation ends (no leak in long `serve`, R-41).
**todo.go** — **`todo_write`** (NON-deferred working-memory scratchpad): session-scoped list, validates exactly-one `in_progress`, full-list replace; `Evict`.
**task.go** — **`task`** (NON-deferred scheduler verb): action enum schedule/list/cancel/run_now; gronx-validated cron; destructive payloads → `pending_approval`. Consumer-declared `taskStore` seam (no `internal/cron` import).
**web_search.go** — **`web_search`** (Deferred): delegates to the SSRF-hardened `internal/web`; a `*web.WebError` is sanitized to inline `{error,reason,message}`.
**web_fetch.go** — **`web_fetch`** (Deferred): per-conversation DNS-pin scope from ctx; large content spills; resolved IP/host/redirect chain never reaches the model.
**swarm_spawn.go** — **`swarm_spawn`** (Deferred fan-out): consumer-declared `swarmRunner` seam breaking the tools→swarm→agent cycle; enforces `AURA_SWARM_MAX_GOALS`; the Description is the load-bearing anti-over-spawn literal.
**skill.go / skill_read.go / skill_write.go** — **`skill`** (NON-deferred): action enum list/info/use/create/update/delete/save_snippet/restore/archive; Description IS the turn-stable skill manifest; snippet→host by-path invocation; create/update/delete gated via `scoring.Skill*` (in-box auto-activate per P5, gated fallback pauses via `ask_user`).
**send_file.go** — **`send_file`** (Deferred, NON-Mutating): channel-agnostic artifact delivery; gates 50 MiB; emits an artifact descriptor on `Meta` the loop lifts to `Actions.ArtifactDelta`; ASCII-folds caption (Bot-API non-ASCII 400 guard).
**document_search.go** — **`document_search`** (Deferred): searches Neo4j-indexed user docs (PDF/xlsx/DOCX) → cited chunks; stamps `Provenance{Trust:Untrusted}`.
**fs.go + fs_read/fs_write/fs_edit/fs_grep/fs_glob.go** — **`fs_read`/`fs_write`/`fs_edit`/`fs_grep`/`fs_glob`** (NON-deferred; write/edit Mutating): full-host filesystem access for the single trusted operator (amendment #50/D-15c), with a surgical exception fencing direct writes out of the skills library; `~` expansion; walk-budget caps (node cap 50000 / 5s deadline, B-16); binary-NUL rejection; RE2 grep; `**` glob.
**shell_exec.go (+ _env/_session/_unix/_windows) ** — **`shell_exec`** (Deferred, Mutating): the keystone full-terminal tool; in-process host shell (prefers Git Bash on Windows), CRLF normalization, destructive-approval gate, per-process-group kill, cwd-marker tracking, secret redaction, `[aura_shell {...}]` footer; bounded ring output buffer.
**shell_approval.go** — destructive-command one-shot approval ledger (`ShellApprovals`, sha256 digest, challenge/approve/consume).
**shell_bg.go** — background shell trio: **`shell_poll`** (Deferred) returns only NEW output + status, **`shell_kill`** (Deferred, Mutating) idempotent terminate; reaper prunes finished shells.

**Tool catalog** — every concrete LLM-facing tool (name · what it does · Deferred?):
- `tool_search` · loads full specs of deferred tools by `select:` or semantic free-text · **NOT Deferred** (always-on discovery hook)
- `text_response` · emits the final reply and ends the turn · **NOT Deferred**
- `ask_user` · pauses for a structured clarification/approval/choice (HITL) · **NOT Deferred**
- `current_time` · current RFC-3339 instant, optional IANA timezone · **NOT Deferred**
- `read_tool_output` · pages a byte range from a truncated output's sidecar · **NOT Deferred**
- `todo_write` · maintains a session-scoped multi-step todo list · **NOT Deferred**
- `task` · schedules/lists/cancels/runs background tasks & reminders · **NOT Deferred**
- `skill` · lists/inspects/applies/authors skills + snippet lifecycle · **NOT Deferred**
- `fs_read` · reads a file (optional line window) · **NOT Deferred**
- `fs_write` · writes/overwrites a file · **NOT Deferred** (Mutating)
- `fs_edit` · exact-string unique-match replace · **NOT Deferred** (Mutating)
- `fs_grep` · RE2 content search across a tree · **NOT Deferred**
- `fs_glob` · finds files by name glob (`**`) · **NOT Deferred**
- `web_search` · SearXNG web search → ranked {title,url,snippet} · **Deferred**
- `web_fetch` · fetches a public page → readable markdown (SSRF-hardened) · **Deferred**
- `document_search` · searches Neo4j-indexed user documents → cited chunks · **Deferred**
- `send_file` · delivers a host file to the user as an attachment · **Deferred** (NON-Mutating)
- `swarm_spawn` · runs ≥2 independent subtasks in parallel as worker agents · **Deferred**
- `shell_exec` · runs a host shell command (full terminal; background mode) · **Deferred** (Mutating)
- `shell_poll` · reads new output from a background shell_exec job · **Deferred**
- `shell_kill` · terminates a background shell_exec job · **Deferred** (Mutating)
- *(dynamic)* MCP-bridged tools namespaced `<namespace>__<tool>`, **Deferred by default** except the `memory` namespace.

### `internal/agent/mcptools` — bridges a generic MCP server's tools into the Aura registry (namespacing, trust framing, schema capping, reconnect)
Lists an MCP server's tools and adapts each to a `tools.Tool` whose `Execute` routes through `tools/call`. Model-facing names are namespaced `<namespace>__<tool>` so a mounted server can never shadow a built-in; server descriptions/schemas are trust-framed as untrusted data and byte-capped; results stamped `TrustUntrusted`. A `reconnectingServer` survives transport drops with backoff + a circuit breaker.

**bridge.go**
- `type Server interface {ListTools/CallTool}` — narrow MCP surface (`*mcp.Client` satisfies it).
- byte caps (`maxMCPDescriptionBytes=4096`, `maxMCPSummaryBytes=768`, `maxMCPSchemaBytes=16K`, `maxMCPSchemaProperties=128`, …).
- `type bridgedTool` + `Spec`, `refreshSpec` (atomic spec swap on reconnect), `Execute` (per-call timeout; tool-level failure → inline `error:` content, not a Go error).
- `Bridge(ctx, namespace, srv)` — lists + adapts; **Deferred by default** except the `memory` namespace; `Mount(ctx, reg, namespace, srv)` (all-or-nothing register + wires the `tool_search` refresh hook + duplicate-name disambiguation).
- schema hardening (`capSchemaDescriptions`, `countSchemaProperties`), trust framing (`frameMCPSummary`, `frameMCPDescription`).

**name.go** — `sanitizeName`, `hashSuffix` (deterministic sha256 disambiguator), `namespacedName` (caps + length-bounds preserving the `__` boundary).
**timeout.go** — `configuredMCPCallTimeout()` (`-1`=no timeout, `0`=default 60s, positive seconds).
**mount.go** — `MountServer` (stdio), `MountManagedServer` (stdio or streamable-HTTP, all-or-nothing closer).
**bridge_reconnect.go** — `type reconnectingServer` + reconnect-on-transport-error (CallTool does NOT replay — at-most-once), single-flight reconnect with backoff + breaker (`mcpReconnectBreakerAfter=3`, `mcpReconnectCooldown=30s`).

### `internal/mcp` — generic MCP (Model Context Protocol) client + protocol types (stdio JSON-RPC + Streamable-HTTP)
A reusable MCP client substrate: spawns any JSON-RPC-2.0-over-stdio server (or connects to a streamable-HTTP one), performs `initialize`, exposes `tools/list` + `tools/call`. Defines the durable managed-server registry (`ManagedConfig`) Aura uses to declare, trust-classify, and launch MCP servers.

**client.go** — stdio client: `Open(ctx, name, cfg)` (spawn + handshake; inherits only an allowlist of safe env keys, drops secrets), `ListTools`, `CallTool`, `Ping`, `Close`; `var ErrTransport` + `IsTransportError`; redacted bounded stderr tail.
**transport.go** — `type Transport interface` + `OpenServer` (dispatches streamable-HTTP vs stdio), `httpAuthFromEnv` (`MCP_BEARER_TOKEN`/`MCP_HEADER_*`).
**http_client.go** — Streamable-HTTP client (`OpenHTTP`, tracks `Mcp-Session-Id`, JSON-or-SSE decode, DELETE session teardown).
**tool_methods.go** — shared transport-agnostic `listToolsWith`/`callToolWith` (surfaces `isError=true` as a Go error).
**redact.go** — `RedactSecrets(s)` (masks token/secret/key env values + `Authorization: Bearer` before logging).
**managed_config.go** — durable registry: `type ManagedConfig`/`ManagedServer`/`ManagedTrust`/`ManagedRuntime`; trust classes (`TrustTrustedRecipe`/`TrustTrustedLocal`/`TrustSandboxedLocal`/`TrustRemoteHTTP`/`TrustBlocked`); `LoadManagedConfig`/`SaveManagedConfig` (`~/.aura/mcp/servers.json`, 0o600); profile + normalization helpers.

### `internal/mcp/manager` — managed MCP server lifecycle: launch resolution, trust gating, status snapshots, built-in recipe catalog, import/export
Resolves a `ManagedServer` into a concrete launch `ServerConfig` per runtime kind (local/docker/docker-gateway), gates on trust class, redacts credentials on export and preserves them on import, computes per-server status snapshots, ships a curated catalog of built-in server recipes.

**config.go** — `ExportProfile` (redacts secret env to `${KEY}` placeholders), `ImportProfile` (preserves existing real credentials unless overwrite), `RedactEnv`.
**runtime.go** — `RuntimeServers`/`RunnableManagedServers`/`RuntimeLaunchConfig` (blocks docker runtimes when `AURA_IN_CONTAINER=1`); builds `docker run -i --rm …` / `docker mcp gateway run --profile`; trust inference defaults `TrustBlocked`.
**status.go** — `type StatusSnapshot` + `SnapshotStatus(doc)` (startup-state + auth-status inference, sorted), `RedactSecrets`.
**catalog.go** — `BuiltInCatalog()`: **calculator** (uvx pinned-commit stdio), **calendar** (fixture), **mail** (vendored SMTP/IMAP), **whatsapp** (whatsmeow streamable-HTTP), **memory** (neo4j-labs agent-memory streamable-HTTP, 16-tool surface); loopback-port validation guards off-host retargeting.

---

## LLM & Reasoning Router

### `internal/llm` — provider-neutral streaming LLM contract + config/cost/breaker
Defines the wire-agnostic `Client` interface the agent loop targets (streaming `Chunk`s over a channel), the canonical conversation types (`Message`/`ToolCall`/`ToolDef`/`Usage`), the provider-neutral reasoning request hint, the resolved 4-tier `Config` loader, a model→capability lookup, a fallback price/cost table, and a shared circuit breaker. The interface never branches on provider; KV-cache discipline lives in the prompt builder. ~700 LOC across 5 files.

**client.go**
- role consts `RoleSystem/User/Assistant/Tool`; `type Message`, `type ToolCall` (`Arguments` stays a JSON string for downstream schema validation), `type ToolDef`, `type Usage struct{PromptTokens, CompletionTokens, CachedTokens int; Cost *float64}` (`CachedTokens`=cache READ count; `Cost` nil when provider sent none → caller falls back to price table, never reports $0).
- `type Chunk struct{Text, Reasoning string; ToolCall *ToolCall; FinishReason string; Usage *Usage; Err error}` — one streamed delta; `Reasoning` is stream-only CoT, never folded into content (#57).
- `type Client interface{ Stream(ctx, req Request) (<-chan Chunk, error) }` — the agent-loop target; consumers MUST drain.
- `type Request struct{Model; Messages; Tools; Temperature; MaxTokens; Reasoning; SessionID, ToolsCacheControl, ToolChoice string}` (`SessionID`=OpenRouter sticky-routing key; `ToolsCacheControl`=dormant Anthropic-direct cache marker; `ToolChoice` ""=auto, "none"=tool-free synthesis).
- `type ReasoningEffort` + consts `XHigh/High/Medium/Low/Minimal/None`; `type ReasoningConfig` + `Empty()`.

**models.go** — `var modelCapabilityTable`, `normalizeModelID` (strips OpenRouter `:`-suffix), `SupportsVision(model)` (true only for minimax-m3; conservatively false for unknown — gates the photo client cloud branch), `SupportsAudio(model)` (false; audio is sidecar-routed).
**config.go** — `type Config` (resolved LLM config; `ShowReasoning` master switch for live CoT; `ReasoningLearning` switch for the self-improvement loop, default OFF); `Load()` (locked 4-tier precedence: built-in < `.env` < `~/.aura/llm.json` < `AURA_LLM_*`; fail-fast on malformed numeric env; `ErrMissingAPIKey` on empty key), `LoadAllowEmptyKey()`. Default model `deepseek/deepseek-v4-flash:exacto`, context window 1M, completion gate on.
**prices.go** — `type Price`, `defaultPrices()` (deepseek-v4-flash:exacto $0.0983 in / $0.1966 out), `CostUSDValue`/`CostUSD` (D-18 precedence: provider cost → table → `(n/a,false)`; never a fabricated "$0").
**breaker.go** — `type Breaker` (consecutive-retryable-failure tracker + cooldown), `NewDefaultBreaker` (threshold 3 / 30s), `Allow()`/`Success()`/`Failure(err)`; `var ErrBreakerOpen`.

### `internal/llm/openai_compat` — handrolled OpenAI-compatible SSE streaming client
Implements `llm.Client` against an OpenRouter-shaped `/chat/completions` endpoint with no SDK (byte-level SSE framing, tool-call delta accumulation, ctx-cancel teardown). One goroutine parses the stream; the API key is written only onto the `Authorization` header at build time. Idle watchdog (B-08), bounded HTTP error capture, per-index tool-call accumulation. ~450 LOC across 6 files.

**client.go** — `type Client` + `New(cfg)` (no http total Timeout — total timeout rides the request ctx; `DisableKeepAlives` for goleak-order-independence); `Stream(ctx, req)` (opens POST under a cancellable child ctx so the idle watchdog can abort cleanly; `(nil, *HTTPError)` on non-2xx with ZERO retries; one parse goroutine emits chunks + trailing Usage + terminal `Err`); `buildWireRequest` (sends `provider.data_collection:"deny"`, drops tools array on `ToolChoice="none"`). `var ErrStreamIdleTimeout` (retryable).
**sse.go** — `parseSSE(r, emit)` (uses `bufio.Reader` not Scanner — survives >64KiB tool-arg lines; recognizes `[DONE]`; emits text + reasoning deltas immediately; accumulates tool-call deltas by index; accept-both `reasoning` vs `reasoning_content` for vLLM/DGX vs DeepSeek), `handleChunk`, `reasoningDelta`.
**accumulate.go** — `type accumulator` (merges streamed `tool_calls` deltas by index; args concatenate so a JSON value split across ≥3 chunks reassembles exactly; `finalize()` emits sorted-by-index calls).
**usage.go** — `type usageWire` → `Usage` (maps `cached_tokens` reads, discards `cache_write_tokens`) → `llm.Usage`.
**httperror.go** — `type HTTPError{StatusCode, RetryAfterSec, Body}` (Body is the response, never request, so it can't carry the API key; capped at 64KiB).
**stream_idle.go** — `type idleWatchdog` (reset on every byte incl. keep-alive ping so a long reasoning phase doesn't trip it, only a dead connection does) + `idleResettingReader`.

### `internal/agent/prompt` — wire-Request assembly chokepoint + adaptive-reasoning router/classifier
Owns the single `PromptBuilder` that assembles every `llm.Request` (KV-cache-safe, provider-aware `cache_control` seam), the prefix-hash fingerprint for the cache-stability gate, and the adaptive-reasoning machinery: the `ReasoningTier` policy, the local embedding `ReasoningClassifier` (granite sidecar + cosine argmax via `semindex`), and the LLM-oracle router prompt/parser. The reasoning router replaces the per-turn LLM round-trip (the adaptive-reasoning latency root cause) with a ~10ms local embed.

**hash.go** — `PrefixHash(msgs, indices)` — stable SHA-256 fingerprint over messages at given indices (via `canonicaljson.Marshal`); a CONTENT fingerprint for the cache-stability gate (not a MAC).
**builder.go** — `type PromptBuilder` (stateless chokepoint owning request SHAPE + the provider cache branch); `type Budget{Used, Remaining, Workspace, CurrentTime, Today}` (volatile hints rendered AFTER history so the cached prefix is never poisoned); `Build(...)` / `BuildWithReasoningTier(...)` (append to a COPY of history, never mutate caller slice or messages[0]).
**cache_anthropic.go** — `injectCacheControl(req, provider)` — dormant seam (no-op under OpenRouter; sets only `req.ToolsCacheControl` for anthropic).
**reasoning_policy.go** — `type ReasoningTier` + `None/Low/High`; `ApplyAdaptiveReasoning(req, provider, cfg, tier)` (sets ONLY reasoning effort, never `MaxTokens` — capping by tier once truncated tool-call JSON mid-call, the "203-turn disaster"; never forces `ToolChoice="none"`); `LastGenuineUserContent(history)` (newest real user request, skipping synthetic nudges).
**reasoning_router.go** — `const ReasoningRouterSystemPrompt` (the LLM "oracle" classification prompt, replies JSON `{"tier":…}`); `ParseReasoningRouterTier(raw)` (tolerates prose/fences).
**reasoning_classifier.go** — the local embedding classifier (adaptive-reasoning router core): `type Embedder = semindex.Embedder` (alias so `documents.EmbeddingClient` satisfies it with no adapter); `type ReasoningClassifier` maps a turn to a tier by cosine proximity to per-tier centroids (granite sidecar; math in `semindex.Classifier`); `Classify(ctx, userText) (tier, ok)` (greeting pre-filter → embed → argmax+margin → observe via learner; `("",false)` on any embedding failure so the caller falls back conservatively); `Refresh()` (re-folds newly stored examples); `var reasoningTierSeeds` (spike-052 variant B: 90%/92% @~10ms CPU).

### `internal/reasoningfifo` — rune-capped rolling CoT buffer
A fixed-capacity rune window for streamed chain-of-thought (tail-keep/front-evict on `[]rune` so eviction lands on a UTF-8 boundary). NOT concurrency-safe (sole caller is the single-goroutine Telegram status pane); a nil `*FIFO` is a valid no-op. ~66 LOC.
- `type FIFO` + `New(max)` (`max<=0` retains nothing — the "live CoT disabled" behavior), `Push(delta)`, `String()`, `Len()`, `Reset()` — all nil-safe.

### `internal/reasoninglearn` — async self-improvement worker for the reasoning classifier
The async learner (spike 053) that observes uncertain (low-margin) turns, labels them with an LLM oracle OFF the hot path, and saves the (embedding, tier) example so the classifier converges toward oracle accuracy without adding user-turn latency. Queue/dedup/margin/drop-on-full/goleak-clean mechanism delegated to `internal/activelearn`; this package supplies only reasoning-specific I/O. ~99 LOC.
- `type Oracle interface{Label(ctx, text) (tier, ok)}`, `type Saver interface{Save(ctx, text, vec, tier) error}`, `type Config`, `type Learner` (implements `prompt.Learner`, `Observe` never blocks), `New(cfg)`, `Close()` (goleak-clean); `reasoningOracle.LabelAndSave` adapts Oracle+Saver into the label-agnostic `activelearn.Oracle`.

### `internal/reasoningstore` — Neo4j persistence of oracle-labeled reasoning examples
Persists oracle-labeled reasoning-tier examples as `:ReasoningExample` nodes in Neo4j (via the mcp-neo4j-cypher `knowledge.Client`) and loads them for the classifier to fold into its centroids (self-improvement substrate, spike 053). Content hash is the MERGE key; no new migration. Implements both `prompt.ExampleStore` and the learner's `Saver`. ~122 LOC.
- `type GraphClient interface{Read/Write}` (`*knowledge.Client` satisfies it), `type Store`, `LoadExamples(ctx)`, `Save(ctx, text, vec, tier)` (`source:"oracle"`); `hashText`, `asFloats` (accepts APOC JSON-string form + raw `[]any`).

### `internal/reasoningtrace` — env-gated redacting JSONL reasoning trace
A diagnostic JSONL tracer for the reasoning/LLM-wire path, gated by `AURA_REASONING_TRACE` (off by default). Each `Record` writes one redacted row; secrets stripped by env-name heuristics, private fields summarized (not verbatim) unless `=full`, fields size-capped on rune boundaries, active file rotates to a single `.1` backup at a byte cap. Imported by the openai_compat client + SSE parser. ~236 LOC.
- `Enabled()`, `FullEnabled()`, `Path()`, `Record(stage, fields)` (no-op when disabled; mkdirs, rotates if oversized, appends to a 0600 file; all I/O failures warn, never panic), `RuneLen`; redaction helpers `redactValue`, `redactValueForKey`, `traceValueSummary` (JSON-hashes private values — never stores plaintext), `capTraceString`, `redactString` (masks env values whose NAME contains KEY/TOKEN/PASSWORD/SECRET).

---

## Learning Substrate, Scoring & Primitives

### `internal/semindex` — Aura's one reusable embedding-index core
A single lock-free pure-math layer (cosine/centroid/margin/bank ops over `[][]float64`) plus two typed wrappers — `Classifier` (Centroid mode: argmax over per-group means → best label + top-2 margin) and `Ranker` (PerItem mode: top-K cosine over individual docs). Both hold the `Embedder` seam + one `sync.RWMutex`; the math core stays pure. It powers BOTH the reasoning-tier classifier and tool_search semantic ranking; `prompt.Embedder` is a type alias of `semindex.Embedder` (no-adapter). Brute-force cosine only, no ANN/HNSW (immutable small banks make naive O(N) sub-millisecond). ~330 LOC.

**semindex.go** — `type Embedder interface{Embed(ctx, texts) ([][]float64, error)}` (matches `documents.EmbeddingClient.Embed` exactly so the granite sidecar satisfies it with no adapter), `type Item`, `type Scored`, `type Verdict{Label, Score, Margin float64; Ok bool}`.
**core.go** (lock-free pure math) — `Normalize(v)` (exported L2), unexported `l2normalize`, `cosine` (returns `-2.0` sentinel on length mismatch), `centroid`, `margin` (top-2 gap).
**classifier.go** (Centroid wrapper) — `type Classifier` + `NewClassifier(embed)`, `AddVecs(label, vecs...)`, `Add(ctx, label, texts...)`, `RankVecs(vec) Verdict` (argmax + top-2 margin; lazily memoizes centroids; deterministic label tie-break), `GroupCosine(label, vec)` (per-tool stage-2 boost lever), `Rank(ctx, text)`.
**ranker.go** (PerItem wrapper) — `type Ranker` + `NewRanker(embed)`, `AddVecs(items...)`, `Add(ctx, texts...)`, `Len()`, `RankVecs(vec, k) []Scored` (top-K cosine desc, ties by ascending insertion index — deterministic), `Rank(ctx, query, k) RankResult` (embedder error carried on `RankResult.Err`, never a silent empty ranking).

### `internal/activelearn` — label-agnostic async self-improvement mechanism
Extracted from the shipped reasoning learner (spike 053). Observes a consumer's uncertain (low-margin) turns, hands each OFF the hot path to a consumer-supplied `Oracle` that labels AND persists, then fires an optional refresh — so a classifier converges toward its oracle's accuracy without adding user-turn latency. The mechanism (bounded queue + sha256 content-hash dedup + `sync.Map` seen-set + margin gate + drop-on-full + one bounded goroutine + goleak-clean `Close`) is the genuinely shared part; the observation is OPAQUE (the core imports no label type and no consumer package). ~165 LOC.
- `type Oracle interface{LabelAndSave(ctx, text, vec) (saved bool)}` (`saved=true`→persisted, core keeps the hash; `false`→core removes the hash so a later observation can retry).
- `type Config{Oracle; Refresh func(); MarginFloor float64; Queue int}`, `type Learner` + `New(cfg)`, `Observe(text, vec, margin)` (NON-blocking; drops nil / `margin>=floor` / blank / already-seen / queue-full), `Close()` (goleak-clean); `hashText`.

### `internal/scoring` — pure Risk-Based governance tier scoring
PRD §Risk-Based Governance (amendment #41/D-11). Computes a qualitative `RiskTier` for scheduler tasks + skill mutations so consumers can render an advisory gate. Deliberately pure — no DB/IO/env; the alert threshold is a caller-supplied argument. ~149 LOC.
- `type RiskTier` + `Safe/Normal/Risky/Destructive`; `type TaskArgs`, `type SkillAction` + `SkillCreate/Update/Install/Delete`.
- `ComputeTaskTier(a)` (base + UP-only modifier bumps; destructive-keyword agent_job → Destructive), `ComputeSkillTier(action, body)` (Delete→Destructive else Risky), `GateRecommended(t)` (Risky|Destructive), `RequiresImmediateAlert(tier, threshold)`.

### `internal/toolselectlearn` — tool-selection active-learning loop (2nd activelearn consumer)
D-06/D-07, spike 057. Detects mis-routed turns cheaply (a shell/fs fallback tool was used, OR used-tool ≠ the ranker's top-1), labels the confident cases with the free ranker and escalates the low-margin tail to the existing DeepSeek router (two-tier oracle), then persists confirmed (query-embedding → tool) exemplars so the tool_search ranker's per-tool centroids self-improve. All async, off the hot path. ~390 LOC.

**detector.go** — `type Ranker interface{Rank(ctx, query) (top1, margin, ok)}` (the free semantic ranker seam); `fallbackTools` map (`shell_exec/poll/kill`, `fs_*`, `skill`, `text_response` — the "model improvised" signal); `isMisroute` (symmetric bare-name comparison; detector precision 0.78→0.88).
**learner.go** — `type Config{Embedder; Ranker; Teacher; Saver; Refresh; MarginFloor; Queue}`, `type Learner` + `New(cfg)` (nil `Embedder`/`Saver` → nil Learner), `Observe(request, usedTool)` (synchronous turn-path capture; NON-blocking; NO embed/network here — CR-01; hands a raw signal to the intake worker), `runIntake`/`detectAndEnqueue` (mis-route detection + exemplar embed on the worker), `Close()` (goleak-clean).
**oracle.go** — two-tier labeling: `type Teacher interface{Label(ctx, request) (tool, ok)}` (DeepSeek router as escalation oracle, kill-switch `AURA_TOOLSELECT_ORACLE`), `type Saver`, `type twoTierOracle` (Tier 1 FREE: confident ranker top-1 at margin ≥ 0.05; Tier 2 ESCALATION: low-margin tail → Teacher), `LabelAndSave`.

### `internal/toolselectstore` — Neo4j persistence for confirmed tool-selection exemplars
Persists oracle-confirmed (query-embedding → tool) examples as `:ToolSelectionExample` nodes in Neo4j and loads them for the tool_search ranker to fold into per-tool centroids (D-06/D-07). Rides the existing mcp-neo4j-cypher client; no new migration. ~157 LOC.
- `type GraphClient interface{Read/Write}`, `type LabeledVec{Tool, Vec, Query}`, `type Store` + `LoadExamples(ctx)`, `Save(ctx, query, tool, vec)` (MERGE on `sha256(query)` so re-labeling is idempotent and the latest oracle label wins; rejects empty tool/embedding, WR-04).

### `internal/toolinvocations` — append-only tool-invocation forensic ledger
Wraps the generated sqlc surface over `aura.tool_invocations` for an append-only, un-deletable (migration 0011 triggers reject DELETE) record of every tool start/end event, with a redaction chokepoint at the persistence boundary so secrets never land verbatim. ~370 LOC.

**store.go** — `EventStart`/`EventEnd`; `type Event` (one append-only fact; start/end correlated by ConversationID+RequestID+ToolCallID); `type Store` + `New(pool)`, `Insert(ctx, e)`, `ListByConversation(ctx, id)`; `toParams` (runs `Arguments` + `ResultPreview` through `RedactForLedger` BEFORE persistence; `ArgsBytes` records PRE-redaction forensic count; `clampInt32` so a >2GiB count never wraps); `eventFromRow` (corrupt `Meta` jsonb → `slog.Warn`, never silent drop, IN-02).
**redact.go** — `RedactForLedger(s, capBytes)` (UTF-8-boundary cap then redact every credential shape); `secretPatterns` table (authorization_header, bearer_token, openai_key, aws_key, json_credential, inline_credential — most-specific-first); `postgresTextSafe` (NUL→`[NUL]`), `capUTF8`.

### `internal/cachemetrics` — per-domain Store over `aura.cache_metrics` (Slice 4 KV)
A thin `Store{q}` wrapper over generated sqlc with pgtype boundary conversion and `%w` errors. Scope: one append-only INSERT per completed assistant turn + the two time-windowed reads behind `aura cache-stats --since=<window>`. ~280 LOC.
- `type Store` + `New(pool)`, `Insert(ctx, params)`, `type Metric` + `ListSince(ctx, since)`, `type Aggregate` + `AggregateSince(ctx, since)` (SQL-side sums; hit-rate ratio left to the caller so the divide-by-zero guard lives at the presentation boundary).
- **store_helpers.go** — `type MetricParams` + `NewInsertParams` (centralizes pgtype conversion); `numericFromFloat` (numeric(10,4) half-away-from-zero, rejects NaN/Inf/out-of-range — loud, never silent overflow); `anyInt64`/`anyNumericFloat` (coerce `coalesce(sum(...),0)` shape-variants; unmodeled shape is an ERROR not a fabricated 0, WR-02).

### `internal/boundedbuffer` — ring-trimmed `io.Writer` keeping the newest N bytes
A mutex-guarded `io.Writer` retaining only the newest `limit` bytes — a small primitive for capturing tail output (shell stdout/stderr) without unbounded growth. ~54 LOC.
- `DefaultLimit=4096`; `type Buffer` + `New(limit)` (≤0 → DefaultLimit), `Write(p)` (appends then trims to the newest `limit`; reports full input length), `String()`, `Len()`.

### `internal/canonicaljson` — deterministic serializer for Aura-internal hashing
A deterministic JSON serializer for internal hashing — NOT RFC-8785 (no cross-system crypto-signature consumer). Feeds the dedup fingerprint `sha256(name + canonical_json(args))` and reused by Phase 4 (conversation hash) + Phase 11 (skill content_hash). Three rules (D-08): sorted keys, numbers preserved as literal text via `json.Number` so `1 != 1.0`, strict-reject of un-canonicalizable input (NaN/Inf/func/chan). ~150 LOC.
- `Marshal(v)` (the public entry; non-nil error + nil bytes for un-canonicalizable input); unexported `normalize` (round-trips through `encoding/json` with `UseNumber`), `encode`, `encodeNumber` (emits literal number text; rejects non-finite), `encodeObject` (keys sorted by Go byte order — the one documented order making output hash-stable).

---

## Persistence & Knowledge

### `internal/db` — Postgres connectivity owner (pgxpool open, golang-migrate runner, role bootstrap, tx seam)
Owns Postgres for Aura: pool open with DSN redaction discipline, golang-migrate runner against embedded `migrations/*.sql`, two-role separation (`aura_app` runtime vs `aura_migrate` DDL), and the `WithTx` atomic-write seam. Every error wraps DSNs through `redactDSN` so `POSTGRES_PASSWORD` never leaks (T-1.05-01). ~540 LOC.

**config.go** — `type Config{URL, MigrateURL, BootstrapURL, Password; MaxConns, MinConns int32; MaxConnIdleTime}` — composed DSNs (D-07: runtime `aura_app` vs DDL `aura_migrate` vs superuser bootstrap) + pool tuning.
**db.go** — `Open(ctx, cfg)` (parse + tune + dial + ping; errors via `redactDSN`); `redactDSN` (masks the password, emits `username:***@`).
**ping.go** — `Ping(ctx, pool)` (`SELECT 1`, returns measured latency).
**status.go** — `type MigrationRow`, `Status(ctx, pool)` (reads golang-migrate tracker; missing table 42P01 → empty + nil, classified via `errors.As`+SQLSTATE handling pgx lazy-query).
**migrate.go** — `Migrate(ctx, migrateURL)` (one step at a time, returns count applied), `EnsureRoles(ctx, bootstrapURL, password)` (bootstraps `aura_app`+`aura_migrate` roles + `aura` schema + grants, errors scrubbed via `redactErrorPassword`); `migrationsFS embed.FS`.
**reset.go** — `Reset(ctx, migrateURL)` (dev-only Down-then-Up; CLI guards with `--yes`/`AURA_RESET_YES`).
**tx.go** — `WithTx(ctx, pool, fn func(*sqlc.Queries) error)` — the DRY atomic-write seam (panic→rollback+re-panic, err→rollback, nil→commit).
**migrate_steps.go** (`db_integration`) — `MigrateSteps(ctx, migrateURL, n)` (exactly n steps; schema round-trip test seam).
- 15 SQL migration pairs ship under `migrations/` (0001_init … 0015_document_ingest_jobs).

### `internal/db/sqlc` — sqlc-generated Postgres client (DO NOT hand-edit)
- Generated by sqlc v1.31.1. `Queries` (over a `DBTX` interface) is the client; `New(db)` constructs it; `WithTx(pgx.Tx)` rebinds to a tx; the `Querier` interface lists ~90 methods (`var _ Querier = (*Queries)(nil)`).
- One `<domain>.sql.go` per query file: knowledge_migrations, scheduler_tasks, tool_invocations, pending_notifications, skill_audit, telegram_accounts, telegram_setup_pending, capability_grants, conversations, conversation_turns, document_ingest_jobs, context_rot_events, paused_states, agent_job_runs, cache_metrics, identity.
- `models.go` row structs for the `aura.*` tables (`AuraIdentities`, `AuraConversations`, `AuraConversationTurns`, `AuraSchedulerTasks`, `AuraToolInvocations`, `AuraTelegramAccounts`, `AuraDocumentIngestJobs`, …). Domain projections live in the hand-written packages; this layer is pure pgtype-typed query wrappers. `SearchConversationTurns` is a LOCKED cross-slice FTS contract.

### `internal/knowledge` — Neo4j graph + vector substrate (MCP-subprocess Cypher client, driver-backed schema DDL, Cypher migrations, health probes)
The LLM-facing runtime interface to Neo4j is the `mcp-neo4j-cypher` subprocess over stdio JSON-RPC (no native Go driver for data ops — CLAUDE.md ban). A separate driver-backed `SchemaExecutor` runs schema DDL (which MCP cannot). Cypher migrations embedded + audited in Postgres `aura.knowledge_migrations`. D-06: a subprocess crash is unrecoverable (fail the Aura process). ~790 LOC.

**config.go** — `type Config{BoltURL, User, Password, Database, MCPBinary; ConnectTimeoutSec; EmbedURL; EmbedDimensions}`; `DefaultEmbedDimensions=384` (Granite / HNSW width).
**client.go** — `type Client` (wraps the `mcp-neo4j-cypher` subprocess); `Open(ctx, cfg)` (spawns `--transport stdio`, runs MCP handshake; spawn failure carries the pip-install hint); `Cypher(ctx, query, params, write)` (one read/write `tools/call`, serialized; failures wrap `crashHint` D-06 + redacted stderr), `Read`/`Write`, `Close`; `buildRequest` (keeps query/params structurally separate — no concatenation, T-1.07-01), `redactSecrets`, `decodeRows` (single MCP-envelope decode chokepoint).
**schema.go** — `type SchemaExecutor` (DDL via auto-commit driver sessions — the sanctioned native-driver path; MCP rejects DDL); `OpenSchema`, `Exec(ctx, stmt)` (`session.Run`, not `ExecuteWrite`), `Close`.
**migrate.go** — `Migrate(ctx, schema, pool)` (applies every embedded `.cypher` not yet in `aura.knowledge_migrations`, statement-by-statement; checksum drift on an applied version is a hard error); `cypherFS embed.FS`.
**status.go** — `type MigrationRow`, `Status(ctx, pool)` (reads applied Cypher migrations from Postgres — the source of truth; Neo4j Community has no tracker).
**reset.go** — `Reset(ctx, schema, pool)` (dev-only: DROP D-08 indexes/constraints + DETACH DELETE all nodes + clear audit + re-Migrate).
**ping.go** — `type PingResult`, `Ping(ctx, mcp, cfg)` (boot health: MCP/Neo4j liveness asserting kernel 5.26.x + embed-sidecar dimension self-test — the only place `AURA_EMBED_DIMENSIONS` becomes operational).
**probe.go** — `VerifyConnectivity(ctx, cfg)` (readiness probe for `/readyz`: native-driver dial, no version gate). Two Cypher migrations ship (`0001_init`, `0002_documents`).

### `internal/documents` — document ingestion domain (Postgres job state, Neo4j searchable graph, extractor/embedding sidecars)
Postgres owns durable job lifecycle, Neo4j owns the document/chunk graph + sparse FTS + vector embeddings, sidecars own file-format parsing + embedding generation. The `Service` coordinates extract → sparse-index → search → async-embed. ~1360 LOC.

**types.go** — `JobStatus` + `JobAccepted/Extracting/Searchable/Embedding/Complete/Failed/Refused/Canceled`; `IngestRequest`, `Job`, `Chunk`, `SearchRequest`, `SearchHit`, etc.
**service.go** — `type Service{Jobs; Extractor; Indexer; Searcher; Embedder; Clock; MaxBytes}` + interfaces `SparseIndexer`/`SearchBackend`/`EmbedQueue`/`Clock`; `IngestPath(ctx, req, path)` (stat + size-cap + type guard + content-hash + create job + extract → sparse-index → searchable → fire-and-forget embed; failures → `JobFailed`); `Search`/`GetJob`/`ListJobs`; `DefaultMaxIngestBytes=50MiB`; `ErrFileTooLarge`.
**store.go** — `JobStore interface` + `PostgresJobStore` (sqlc-backed CRUD + status/progress).
**indexer.go** — `Indexer` + `UpsertSparse` (MERGE `:Document`, batch `:Chunk` upserts via `HAS_CHUNK`), `UpsertEmbeddings` (set chunk `embedding` vectors).
**search.go** — `Searcher.Search` (sanitizes the query — strips Lucene operators — runs the `chunk_text` fulltext index, projects hits).
**extractor.go / extract_client.go** — `Extractor interface` + `ExtractClient` (streams the file as multipart to the markitdown sidecar `/extract`).
**embedder.go** — `EmbeddingClient.Embed` (POSTs to the OpenAI-compat `/v1/embeddings`, validates count + per-vector dimension).
**worker.go** — `EmbeddingWorker` + `Enqueue` (`WithoutCancel` goroutine), `Process` (batched embed + upsert, `JobEmbedding→JobComplete`; embed failure reverts to `JobSearchable`; `embedWithRetry` exponential backoff).
**ids.go** — `NormalizeText`, `ContentHashPath`/`ContentHashReader`, `DocumentID`/`ChunkID`/`ChunkHash`, `BuildExtractedDocument` — the stable-id + normalization + chunk-assembly toolkit.

### `internal/conversations` — multi-thread persistence + L1/L2/L2.5 context ladder + microcompact + sidecar spill + auto-title + boot GC
The per-domain `Store` over `aura.conversations`/`conversation_turns`/`context_rot_events` (copies the canonical Store shape). Houses the atomic per-turn append, byte-identical history reload, the deterministic context-management ladder, the offline tiktoken estimator, the auto-title worker body, and boot/periodic sidecar reconciliation. ~1900 LOC.

**store.go** — `type Store` + `Config` + `New`; `ConversationCleaner interface` (injected to avoid a sandbox import cycle); `Create`/`Get`/`List`/`UpdateStatus`/`Rename`/`SetTitleIfNull`/`CountTurns`/`LoadHistory` (raw, byte-identical Req#8)/`SearchConversationTurns` (wraps the LOCKED FTS query)/`Delete` (DB cascade then os.Root cascade); `ErrConversationNotFound`.
**store_append.go** — `AppendTurn(ctx, p)` (INSERT turn + UPDATE aggregates in a single `db.WithTx`, SC-2 atomicity; allocates seq under a row lock), `AppendAssistantTurnWithCacheMetric` (same tx + the `cache_metrics` row); `cleanupSidecarOnTxError`.
**store_helpers.go** — boundary/pure helpers: `conversationFromRow`, `turnToMessage`, `maybeSpill` (content over `turnCapBytes` → `<run_dir>/conversations/<id>/<seq>.content`), `validateID` (traversal guard), `repairToolMessagePairs` family (drops orphan tool turns / repairs dangling tool-call groups — crash recovery).
**context.go** — the L1/L2/L2.5 ladder: `ContextConfig`, `ErrContextWindowExceeded`, `LoadManagedHistory(ctx, id, cfg)` (Runner entry); `applyContextLadder` (L1 microcompact → L2 budget gate → L2.5 oldest-pair drop, writes one rot-event), `injectAlwaysBlock` (protected messages[1] block, D-07/Pitfall 3), `applyL1` (rewrite old tool turns to `read_tool_output` pointers, sidecar-backed only).
**tiktoken.go** — `InitEncoder()` (eager boot init of the cached cl100k_base encoder); vendored `cl100k_base.tiktoken` blob, no network (A6); `countTokens` (fast ~5-10% estimate for budget gating only).
**title.go** — `GenerateTitle(ctx, client, model, history)` (best-effort auto-title; errors → NULL title); `renderHistoryForTitle`, `sanitizeTitle`.
**orphan_scan.go** — `ScanOrphans(ctx, pool, p)` (boot reconciliation GC: removes orphan conversation dirs under an Lstat/no-follow guard, sweeps `tmp/*` > 24h, WARNs on oversized run dir — never purges).
**sweeper.go** — `Sweeper` + `NewRunDirSweeper` (periodic `ScanOrphans`; bounded join on Stop).

### `internal/identity` — single-user identity + capability_grants Store (Slice 1.7)
The canonical per-domain Store proven first (the shape conversations/askuser copy): SQLSTATE classification via `errors.As`+`pgErr.Code`, sentinel errors, pgtype boundary conversion. Scope: `local` identity CRUD + capability grant/revoke with wildcard-or-exact `HasCapability`. ~200 LOC.
- `type Store` + `New(pool)`, `ListIdentities`/`GetIdentityByName`/`GetIdentityByID`/`DeleteIdentity`/`HasCapability` (`*`-or-exact, matched in SQL)/`GrantCapability` (idempotent, rejects `*`/bad names pre-DB)/`RevokeCapability`; `Wildcard="*"`; sentinels `ErrWildcardManaged`/`ErrInvalidCapability`/`ErrIdentityNotFound`.

### `internal/profile` — per-identity Agent.md profile store (atomic disk writes + render/parse)
Stores each identity's profile (Agent.md + preferences.json + metadata.json + changelog.md) under `Root/<identity>/`, written atomically (temp-file + platform rename + fsync). Renders/parses the structured Agent.md, enforces a byte cap, guards identity names against traversal. ~590 LOC.
- **store.go** — `Preferences`/`Metadata`/`Profile`/`LoadedProfile`; `type Store` + `NewStore(root)` (empty → `DefaultRoot` `~/.aura/agents`), `WriteProfile`/`ReadProfile`; `profileDir` (identity grammar + containment), `atomicWrite` family.
- **store_fact.go** — `AddFact(identity, fact)` (read-modify-write into Agent.md's Identity section, de-duped, version-bumped).
- **render.go** — `type AgentContent` (8 sections), `RenderAgentMD`, `RenderContextBlock` (`<profile:Agent.md>…</profile:Agent.md>` for the protected messages[1] block), `AddFact` (pure); `MaxAgentMDBytes=32768`.
- **parser.go** — `ParseSections`, `FormatSectionTree` (ASCII tree for CLI). **atomic_posix.go/atomic_windows.go** — `replaceFile` (POSIX rename / Windows `MoveFileExW` REPLACE_EXISTING|WRITE_THROUGH).

### `internal/secret` — canonical secret-env-key predicate
ONE shared denylist so every env-filtering site (shell child-env, MCP child-env, MCP config export) redacts identically — preventing the B-09 divergence where a bare `*_KEY` leaked on one path. ~110 LOC.
- `IsSecretEnvKey(name)` (case-insensitive substring match against a broad marker list), `IsSecretEnvVar(name, value)` (avoids over-redacting harmless URLs), `ContainsCredentialURL(s)` (matches `scheme://user:pass@host`). "Never shrink these lists (re-opens B-09)."

---

## Capabilities (Swarm, Web, Skills, Scheduler, Onboarding, Eval)

### `internal/swarm` — ephemeral per-call fan-out coordinator (CAP-03)
Fans a set of goals out as budget-bounded, leak-safe `LlmAgent` workers in concurrency-capped waves, isolates per-child failures (a failed child becomes a `{failed}` report, siblings keep running — D-02), and collects an ordered `[]ChildReport` marshaled to JSON. Decoupled from the `swarm_spawn` tool via a ctx-injected `RunnerAdapter` seam. ~5 source files, ~350 LOC.

**report.go** (package doc + result model)
- `const StatusOK = "ok"`, `StatusFailed = "failed"`, `StatusNeedsUserInput = "needs_user_input"` — model-readable child statuses (D-15) the parent LLM reads off the collected report.
- `type ChildReport struct{GoalIndex int; ChildID string; Status, Summary, Error, Question string; Options []string; ToolCallID string}` — one worker's ordered slot; `needs_user_input` fields carry the worker's `ask_user` pause payload for parent proxying (D-04/D-05). `ChildID` is the flat `"w1".."wN"` id (no path separator — Pitfall 4).
- `dumpTranscript(runDir, convID, childID string, ev agent.Event) error` — appends one JSON line per Event to `<runDir>/<convID>/swarm/<childID>.jsonl` (D-18); best-effort (logs+swallows write errors, always returns nil).
- `marshalReports(reports []ChildReport) (string, error)` — compact-JSON serializer for the tool-result body.

**swarm.go** (the engine)
- `const budgetReserve = 3` — steps reserved for the parent's post-swarm synthesis (D-09/A3). `const swarmSpawnTool = "swarm_spawn"` — name dropped from every worker registry (flat v1: no nested swarms).
- `type RunConfig struct{ParentBudget *agent.Budget; ParentRegistry *tools.Registry; Client llm.Client; LLM llm.Config; Cfg config.Config; ConvID string; Depth int}` — one-spawn inputs.
- `Run(ctx, rc RunConfig, goals []string) (string, error)` — preflight-gates then fans goals into concurrency-capped waves (`rc.Cfg.MaxSwarmConcurrent`), returns the ordered reports JSON. A pre-flight rejection returns a model-readable `"error: ..."` string with no worker spawned; the Go error is reserved for marshal failure (D-15).
- `preflight(rc, goals) (string, bool)` — applies the three spawn-time rejections in order: depth (D-10), empty/zero goals (teaches the `{"goals":[...]}` arg shape), goals-cap (D-13), and budget snapshot (`Remaining() < len(goals)+budgetReserve`, D-09).
- `runWave(ctx, rc, goals, reports, start, end)` — runs `goals[start:end]` concurrently via `errgroup`; copies parallel.go's leak-safety verbatim but DIVERGES on errors: a child error is captured into its report slot and the goroutine returns nil so siblings are never cancelled (D-02). Recovers panics via `panicobs.Record(SiteSwarmWave)`.
- `panicChildReport(idx, recovered any) ChildReport` — turns a recovered worker panic into a `{failed, "panic: ..."}` report.
- `runChild(ctx, rc, budget, idx, goal) ChildReport` — builds one worker (`agent.NewLlmAgent`, registry minus `swarm_spawn` via `tools.Without`, FLAT `SessionID` `<conv>-swarm-w<n>`, the structured brief as `messages[1]`), drains its Event stream into a `ChildReport` (failure→failed, `ask_user`→needs_user_input, final content→summary), dumps each Event to the transcript. Never returns an error. Normalizes a deadline trip to a uniform `{failed,"timeout"}` (D-11) but preserves a worker that already streamed a terminal success (WR-01).
- `optionLabels(opts []agent.PauseOption) []string` — projects pause-option `Label`s onto the flat `[]string` the report carries.

**brief.go** (worker framing — D-06/D-07)
- `const workerOverlay` — the static "headless swarm worker" framing that rides in `messages[1]` (keeps `messages[0]` byte-identical for KV cache).
- `const briefObjective/briefOutput/briefTools/briefBoundaries` — the four load-bearing D-07 section-header literals.
- `structuredBrief(goal string) string` — builds the deterministic 4-part worker brief prefixed by the overlay; a pure function of `goal` (KV-cache friendly).

**swarm_depth.go** (depth guard — D-10)
- `const defaultMaxDepth = 2`.
- `maxDepth() int` — reads `AURA_SWARM_MAX_DEPTH`, falling back to the default on unset/empty/unparseable.
- `checkDepth(depth, max int) (string, bool)` — returns `("MAX_SPAWN_DEPTH=<max> exceeded", false)` when `depth >= max`.

**runner_adapter.go** (cycle-free tool seam)
- `type RunnerAdapter struct{Cfg config.Config; Depth int}` — the concrete `swarmRunner` the `swarm_spawn` tool delegates to; imports agent+config (the tools package imports neither).
- `NewRunnerAdapter(cfg config.Config) *RunnerAdapter` — builds an adapter with `Depth: 1`.
- `(*RunnerAdapter) Run(ctx, goals []string) (tools.ToolResult, error)` — resolves the per-invocation parent deps off the ctx (`agent.SwarmContext`), builds the `RunConfig`, calls the engine, wraps the report JSON via `tools.NewResult` (D-15 spillover) tagged `Source:"swarm", Trust:TrustUntrusted`. A missing swarm context is a model-readable inline error.

### `internal/web` — Phase-7 SSRF-hardened engine behind `web_search` + `web_fetch`
Owns the full request path so the two thin tool adapters stay free of cross-cutting concerns: SearXNG client, SSRF classifier, per-conversation DNS pin, pinned transport with manual redirect revalidation, readability→markdown extraction, TTL response cache, and a sanitized error enum. Fail-closed posture: no IP/host/header/body leaks to the model (D-26/27). ~11 source files.

**doc.go** — package overview only.

**client.go**
- `type Client struct{cfg *config.Config; transport *hardenedTransport; pin *dnsPin; cache *cache; hosts *hostThrottle}` — the shared engine owning the single hardened transport, DNS pin, response cache, host throttle.
- `NewClient(cfg *config.Config) *Client` — wires the hardened transport (`newGuard`+`newHardenedTransport`), DNS pin (`WebDNSPinTTLSec`), cache (`WebCachePersistent`), host throttle.
- `(*Client) userAgent() string` — the egress UA stamped on every request.

**searxng.go** (web_search)
- `type SearchParams struct{Query string; MaxResults int; Category, Language, TimeRange string; Domains []string; IncludeMetadata bool}` — validated model-supplied search request (hostnames only; category enum; time_range enum).
- `type Result struct{Title, URL, Snippet string; Metadata *ResultMetadata}` — model-visible hit; `Metadata` nil unless requested (D-07/D-08).
- `type ResultMetadata struct{Engine string; Score float64; Category string; PublishedAt *string; Thumbnail string}` — normalized second tier.
- `type searxResult`, `type searxResponse` — SearXNG's own JSON schema (`publishedDate` nullable).
- `var auraCategoryToSearXNG` (`""`/`general`/`news`), `var validTimeRanges` (day/week/month/year), `var errSearxUnreachable`.
- `(*Client) Search(ctx, params SearchParams) ([]Result, error)` — builds the `/search` JSON query, caches by a shape-discriminated key, runs under `WebSearchTimeoutSec` with one retry, post-filters by domain. Missing `SearxngURL`→`web_search_unavailable{searxng_not_configured}`; unreachable→`{searxng_unreachable}`; backend URL/body never leaked.
- `const searchCacheTTL = 2*time.Minute`; `searchCacheDiscriminator(values, params) string` — folds encoded query + `IncludeMetadata`+`MaxResults` into the cache key so different result shapes don't alias.
- `(*Client) buildQuery(params, cat) url.Values` — assembles q (+ `site:` rewrite OR-joined), format=json, category, optional language/time_range, pageno=1.
- `siteRewrite(domains) string` / `validHostname(d) string` — hostname-only `site:` clause, rejecting any entry with scheme/path/port (D-12).
- `(*Client) searxGet(ctx, values) (*searxResponse, error)` — GET with Aura UA, one retry on transient/408/429/5xx; `DisableKeepAlives` keeps goleak order-independent.
- `decodeSearx(resp) (out, err, retry)` — closes body, flags retryable status, decodes through `io.LimitReader(maxSearxBodyBytes=4MiB)`.
- `isRetryableStatus(code) bool` — 408/429/5xx.
- `(*Client) mapResults(resp, params) []Result` — normalizes results, attaches metadata only when requested, post-filters by domain suffix-match, clamps to `MaxResults`.
- `domainAllowed(rawURL, domains) bool` — exact/subdomain suffix match (D-13); empty set allows all; unparseable URL dropped.

**fetcher.go** (web_fetch)
- `const maxRedirectHops = 5`.
- `type Page struct{Title, URL, ContentMD string; Links []string; Warning string}` — model-visible fetch result; `Warning` only for soft `low_content`; `Links` are deduped normalized absolute URL strings (D-19).
- `var allowedSchemes` (http/https), `var allowedContentTypes` (text/html, application/xhtml+xml), `var errRetryable`.
- `(*Client) Fetch(ctx, convID, rawURL string) (Page, error)` — the full security state machine: parse+scheme allowlist → per-host throttle acquire (D-36) → per-hop SSRF validate+pin + manual redirect revalidation → Content-Type allowlist → size cap → readability→markdown; one retry on transient, none on SSRF/4xx/config; cache check first.
- `(*Client) fetchBody / doHops / resolveRedirect` — drive the manual redirect loop, re-entering `validateAndPin` for every hop BEFORE dialing the next target (a redirect to a blocked host is rejected at the hop, never dialed → `blocked_url{redirect_to_blocked_target}`).
- `gateAndRead(resp, capBytes) ([]byte, error)` — enforces Content-Type BEFORE reading, reads through `io.LimitReader(cap+1)` so a body filling the extra byte is `response_too_large` (Pitfall 6).
- `contentTypeAllowed(ct) bool`, `isRedirect(code) bool`, `classifyTransportErr(err) error`, `(*Client) classifyFetchErr(err) error`, `isNetTimeout(err) bool` — error/redirect classification helpers (SSRF blocks never retried; deadlines → `timeout`).
- `(*Client) fetchFromCache / fetchToCache` — marshalled-`Page` cache round-trip (D-31/D-33).

**ssrf.go** (the security gate)
- `var cgnatPrefix (100.64.0.0/10)`, `thisNetPrefix (0.0.0.0/8)`, `metadataV6Pfx (fd00:ec2::/32)`; `var hostnameBlocklist` — cloud-metadata/cluster-internal hostnames blocked before resolution (T-07-10).
- `classify(ip netip.Addr) (reason string, blocked bool)` — `Unmap()` first then maps an IP to a block class (loopback/link_local/private/multicast/unspecified/cgnat/this_network); the mutation-gate target.
- `type resolver interface{LookupNetIP(...)}` — injectable DNS seam.
- `type guard struct{res resolver; pin *dnsPin}`; `newGuard(res, pin, _ any) *guard`.
- `(*guard) validateAndPin(ctx, convID, host) (netip.Addr, string)` — hostname blocklist → live-pin reuse (anti-rebinding) → fresh resolve that classifies EVERY record and fails closed if ANY is blocked (D-24) → pins and returns the first public IP.

**dnspin.go**
- `type pinKey struct{conv, host string}`, `type pinEntry struct{ip netip.Addr; expires time.Time}`.
- `type dnsPin struct{mu sync.Mutex; m map[pinKey]pinEntry; ttl time.Duration; now func() time.Time}` — the per-(conversation,host) TTL pin closing the DNS-rebinding TOCTOU window (D-25).
- `newDNSPin(ttlSec int) *dnsPin`; `(*dnsPin) Pinned(conv, host) (netip.Addr, bool)` (expired entry is a miss); `(*dnsPin) Pin(conv, host, ip)` (called only after classifier approval).

**transport.go**
- `const dialConnectTimeout = 10*time.Second`; `type dialFunc`; `type convCtxKey`; `withConvID(ctx, convID) context.Context` / `convIDFrom(ctx) string` — carry the conversation id through the request ctx for pin scoping.
- `type hardenedTransport struct{client *http.Client; guard *guard; dial dialFunc; ua string}` — SSRF-hardened client.
- `newHardenedTransport(g, dial, ua) *hardenedTransport` — composes the client: `dialContext` (primary gate), `Control` recheck (defense-in-depth), `CheckRedirect` refusing auto-follow (`ErrUseLastResponse`), `DisableKeepAlives`.
- `(*hardenedTransport) dialContext(ctx, network, addr)` — runs `validateAndPin`, dials the PINNED IP string (never the hostname) so no second lookup can rebind (Pitfall 1 TOCTOU).
- `(*hardenedTransport) control(_, address, _)` — re-parses the post-resolution IP and rejects a blocked one fail-closed (T-07-12).

**html.go** (readability → markdown)
- `const lowContentRunes = 250`; `var citationAnchorRe / referencesHeadingRe / zeroPaddedReflistRe / converterCommentRe / wikiBoilerplateRe` — structural-markdown cleanup regexes (never NL prose).
- `ExtractMarkdown(body []byte, pageURL *url.URL) (title, markdown string, links []string, warning string, err error)` — runs `readability.FromReader` over already-fetched bytes (never self-fetches — T-07-22), converts the node tree to markdown, cleans citations, extracts deduped absolute links; thin text → `warning="low_content"` with nil error (D-22).
- `cleanMarkdown(md) string` / `referencesTailStart(md) int` — strip wiki boilerplate + inline citation anchors + truncate references/notes tail.
- `convertNode(node *html.Node) (string, error)` — `htmltomarkdown.ConvertNode` with a RenderHTML round-trip fallback.
- `extractLinks(root, pageURL) []string` — walks the readable tree for `<a href>`, resolves against pageURL, dedups, skips fragment-only anchors.

**cache.go**
- `const defaultCacheTTL = 5*time.Minute`; `type cacheEntry struct{Payload []byte; Expires time.Time}`.
- `type cache struct{mu sync.Mutex; m map[string]cacheEntry; defaultTTL time.Duration; persistent bool; dir string; now func() time.Time}` — in-memory TTL cache with optional disk tier (`AURA_WEB_CACHE_PERSISTENT`, D-31/D-32).
- `newCache(persistent bool, defaultTTL) *cache` — resolves the disk dir under `os.UserCacheDir`, falling back to memory-only on failure.
- `cacheKey(namespace, raw string) string` — SHA-256(namespace\x00raw) → filesystem-safe key.
- `(*cache) get / set / getDiskLocked / setDiskLocked` — TTL read/write with best-effort disk mirror; expired entries are misses.

**throttle.go**
- `const perHostLimit = 2`; `type hostThrottle struct{mu sync.Mutex; m map[string]chan struct{}}` — per-host in-flight fetch cap (D-36, not an RPM limiter).
- `newHostThrottle()`, `(*hostThrottle) sem(host) chan struct{}`, `(*hostThrottle) acquire(ctx, host) (release func(), ok bool)`.

**errors.go**
- `const CodeSearchUnavailable/BlockedURL/UnsupportedScheme/UnsupportedContent/ResponseTooLarge/Timeout/HTTPError/ExtractionFailed` — the D-38 stable model-visible `error` enum (a contract).
- `const ReasonSearxngNotConfigured/SearxngUnreachable/PrivateOrMetadata/RedirectToBlocked/InvalidTarget` — stable non-sensitive block-class reasons (never a concrete IP/host).
- `type WebError struct{Code, Reason, Message string; StatusCode int}` — the ONLY error shape the model sees; `MarshalJSON` emits `{error,reason?,message?,status_code?}` hand-rolled to omit zero values; `JSON()`, `Error()`.
- `type internalError struct{...; resolvedIP, host, redirectFrom string}` — RICH internal-only error (sensitive fields unexported); `Error()` includes them for logs/tests.
- `AsWebError(err) (*WebError, bool)` — unwraps to a `*WebError`, sanitizing an `*internalError` on the fly.
- `sanitize(e *internalError) *WebError` — the single chokepoint copying only safe fields (D-27).

### `internal/skills` — read+write+install+snippet+audit half of the skills system (Slice 7)
A multi-root loader scanning `SKILL.md` files (TTL-cached, symlink-stripped, structure-validated), a durable gate-aware `Writer` (pending→activate→materialize lifecycle, append-only PG audit ledger), executable-snippet save/use/sweep, and the always-on/manifest renderers. ~17 source files.

**loader.go**
- `const defaultCacheTTL = time.Second`, `defaultBodyCapBytes = 32768`.
- `type Skill struct{Name, Description string; Always bool; Type, Language, Body, Dir string}` — one loaded structurally-valid skill.
- `type Config struct{Roots []string; CacheTTL time.Duration; BodyCapBytes int; Blocklist []string}` — multi-root config; later-root-wins precedence; optional load-time injection blocklist (amendment #51/D-40).
- `type Loader struct{...; mu sync.Mutex; snapshot map[string]Skill; order []string; ...}` — TTL-cached, concurrency-safe, goroutine-free (lazy-on-read).
- `NewLoader(cfg Config) *Loader`; `(*Loader) List() []Skill` (stable name-sorted copy); `(*Loader) Get(name) (Skill, bool)`.
- `(*Loader) refreshLocked / scan / scanRoot / loadSkillDir` — re-scan roots past TTL, merge with later-root-wins, Lstat-no-follow symlink strip (Pitfall 4), parse+validate-structure+optional blocklist scan; invalid skills skip-logged, never fatal.
- `validateStructure(fm, dirName, body, bodyCap) error` — name present + grammar + name==dir + description present + type-in-enum + body within cap (deliberately blocklist-free, D-28).

**frontmatter.go**
- `type Frontmatter struct{Name, Description string; Always bool; Type string; License, Compatibility string; Metadata map[string]any; AllowedTools []string; Language string; InputsSchema map[string]any; OutputsDesc string; Deps, Tags []string; NeedsNetwork, NeedsWorkspace bool}` — parsed YAML header; snippet-docs fields inert on the read path.
- `const TypeInstruction = "instruction"`, `TypeSnippet = "snippet"`.
- `parseFrontmatter(raw []byte) (Frontmatter, string, error)` — CRLF-normalizes, splits the `---` fence, parses with goccy/go-yaml, defaults Type, NFKC-normalizes Name+Description.
- `splitFrontmatter / indexClosingFence` — fence extraction helpers.

**validator.go**
- `var skillNameRe = ^[a-z0-9-]{1,64}$`; `const maxSkillDescriptionLen = 1024`.
- `var ErrInvalidName / ErrBlocklisted / ErrInvalidStructure` — sentinel errors (callers classify without string-matching).
- `SanitizeName(name, dirName string) error` — the SINGLE name chokepoint (grammar + name==dir, D-30).
- `violatesBlocklist(body, blocklist) (matched string, pos int, ok bool)` — NFKC-normalize-then-fold-then-literal-substring match (Pitfall 2); returns the matched sequence + byte position for the D-27 operator gate.
- `ValidateForWrite(fm, body, blocklist, bodyCapBytes, allowBlocklisted) error` — pure write-boundary chokepoint: structure checks then the injection blocklist (unless the operator override `allowBlocklisted` is set). Model paths always pass false.
- `ValidateNameAgainstDir(fm, dirName) error` — installer's name+dir chokepoint.

**materialize.go**
- `Materialize(name, skillDir, exportDir string) error` — copies an ACTIVE skill into the export dir (the `/skills` ro-mount source, D-17), STRIPPING symlinks (manual `os.ReadDir`+`Lstat` recursion, not `WalkDir`).
- `copyTreeNoSymlinks(src, dst) error`, `copyRegularFile(src, dst) error`, `Dematerialize(name, exportDir) error` — recursive symlink-stripped copy + removal on archive/delete.

**contenthash.go**
- `HashSkillFiles(files map[string][]byte) string` — Aura's own canonical content hash (`sha256:` over byte-sorted length-prefixed (relPath,bytes) pairs, D-15/D-23); deliberately NOT upstream-interoperable.
- `HashSkillDir(dir string) (string, error)` / `collectFilesNoSymlinks(base, cur, out)` — symlink-stripped tree hash (installer's content pin).

**builtin.go**
- `//go:embed embed` `var builtinFS`; `const builtinRoot = "embed"`.
- `MaterializeBuiltins(dir string) error` — writes the embedded builtin skills (`skill-creator`, `find-skills-aura`) into the active root on first boot; fingerprint-idempotent (`writeIfChanged`, SHA-256 compared).
- `writeIfChanged(target, data) error`.

**messages.go**
- `const alwaysBlockHeader` — the frozen English header leading the `messages[1]` always-block.
- `RenderAlwaysBlock(loaded []Skill) (block string, present bool)` — concatenates `always:true` skill bodies in alphabetical order under the header; byte-stable across turns (CAP-04 KV-cache discipline); `present=false` when none.

**manifest.go**
- `const defaultManifestCapBytes = 8192`.
- `RenderManifest(skills []Skill, capBytes int) string` — alphabetically-sorted name+description block for the `skill` tool's Description (D-06); byte-stable; appends an overflow tail past the cap (D-09).
- `BM25Corpus(skills []Skill) []string` — spec-shaped `"name description"` docs in the same sort order, index-aligned for the overflow `list` ranker.

**writer.go**
- `type Writer struct{pool *pgxpool.Pool; pendingDir, activeDir, exportDir, archiveDir string; blocklist []string; bodyCapBytes int}` — durable, auditable, gate-aware write primitive (Slice 7c). `type WriterConfig`; `NewWriter(cfg) *Writer`.
- `const StatusPendingApproval = "pending_approval"`, `StatusActive = "active"`.
- `type AuditActor struct{ActorID, IdentityID string}`; `const ActorModel = "model"`.
- `(*Writer) WriteMutation(ctx, action scoring.SkillAction, fm, body, actor) (status string, err error)` — model-facing create/update/install/delete: computes tier+gate (`modelMutationBypassesGate` makes ordinary in-box create/update ungated, but `always:true` stays gated), validates at the write boundary, hashes, atomically writes `pending/<name>/` BEFORE the audit tx, records the D-29 pending tuple inside `db.WithTx`. Never self-activates.
- `modelMutationBypassesGate(action, fm, actor) bool` — true only for model-actor create/update of a non-`always` skill.
- `(*Writer) WriteInstallPending(ctx, fm, body, stagedDir, hash, actor) (string, error)` — promotes a staged installed tree into `pending/` + the install audit tuple.
- `(*Writer) WriteMutationByName(...)` / `WriteMutationCLI(...)` — string-keyed convenience wrappers (actor "model"/"cli").
- `(*Writer) writePending(name, fm, body) error` — atomic temp-dir+rename write of `pending/<name>/SKILL.md`.
- `skillFileBytes(fm, body) []byte` / `yamlScalar(s) string` / `auditActionFor(a scoring.SkillAction) AuditAction` — SKILL.md rendering + audit-action mapping helpers.

**writer_activate.go** (lifecycle transitions)
- `(*Writer) Activate(ctx, name, src ApprovalSource, pausedToken, actor) error` — resume/CLI activation: `pending→active` promote, materialize into the export dir, record the D-29 approved audit row inside `db.WithTx`. Never called by `WriteMutation` (T-11-04-E1).
- `(*Writer) Archive(ctx, name, src, actor) error` — de-materialize + `active→archived` + archive (or `auto_archive` for the TTL sweep) audit row.
- `(*Writer) Restore(ctx, name, src, actor) error` — inverse of Archive (`archived→active`, re-materialize, usage→active); audited as `activate`/`cli` (no `restore` action constant exists — load-bearing decision).
- `(*Writer) Delete(ctx, name, actor) (string, error)` — Destructive removal (de-materialize + remove active+pending dirs + delete audit row).
- `(*Writer) auditRejection / auditActivationLike` — D-29 tuple writers for human-declined and gate-taken rows.
- `(*Writer) SetAlways(ctx, name, always bool, actor) error` — operator-only re-flip of `always:true` on an active skill (rewrite+re-materialize+update audit).
- `promoteDir(src, dst) error` — `os.Rename` move with stale-dst replacement.

**snippet.go** (Sub-slice 7e executable snippets)
- `type SnippetLanguage string`; `const LangPython/LangShell/LangJS`; `type snippetMeta struct{ext, interpreter string}`; `var snippetMetaByLang`; `const inSandboxSkillsRoot = "/skills"`.
- `validSnippetLanguage(language) (SnippetLanguage, snippetMeta, error)` — normalizes aliases (py/python3/bash/javascript/node) and validates the enum.
- `SnippetCodeFile(name, lang) (string, error)`, `SnippetSandboxPath(name, lang) (string, error)`, `SnippetHostPath(name, lang, exportDir) (string, error)` — the deterministic `<name>.<ext>` file + in-sandbox `/skills/...` path + HOST export-dir path (D-01 host-primary).
- `SnippetInvocation(name, language) (sandboxPath, interpreter string, err error)` / `SnippetHostInvocation(name, language, exportDir) (hostPath, interpreter string, err error)` — resolve the by-path target + interpreter for `sandbox_exec` / host `shell_exec`.
- `type SnippetSaveResult struct{Status string; Tier scoring.RiskTier; Language SnippetLanguage; NeedsNetwork, NeedsWorkspace bool}`.
- `(*Writer) SaveSnippet(ctx, name, language, code string, fm, actor) (SnippetSaveResult, error)` — stages a `type:snippet` skill as pending (validates language+write boundary on the CODE, computes RISKY tier, atomically writes SKILL.md+`<name>.<ext>`, records the D-29 pending tuple). Never self-activates.
- `(*Writer) writePendingSnippet(...)` / `renderSnippetDocs(fm, meta) string` — atomic snippet write + generic docs frame (no baked execution-tier path).
- `type SnippetUse struct{Instructions, HostPath, SandboxPath, Interpreter string; Language SnippetLanguage}`.
- `(*Writer) UseSnippet(name string) (SnippetUse, error)` — resolves an ACTIVE snippet for the model (`SanitizeName` first), returns docs body + host/sandbox by-path targets + interpreter. Does not execute (D-04).

**snippet_usage.go** (TTL state)
- `type UsageSidecar struct{Status string; LastUsedAt time.Time; UseCount int}` — per-skill `.usage.json` live-state source (D-19), atomic-written.
- `(*Writer) usageSidecarPath / ReadUsage / StampUsage(name, now) / SetUsageStatus(name, status)` + `readUsageSidecar`, `writeUsageAtomic`, `writeUsageAtomicInDir`, `setUsageStatusInRoot` — atomic usage read/bump/status-set.
- `type SweepResult struct{Archived, Kept []string}`.
- `(*Writer) SweepExpiredSnippets(ctx, ttl, now, actor) (SweepResult, error)` — archives every active snippet whose last use (or SKILL.md mtime) is older than the TTL (`Archive(ApprovalAuto)` + usage→archived), best-effort per skill; `ttl<=0` disables.
- `(*Writer) snippetIsStale(name, cutoff) (stale, ok bool)` — staleness predicate (skips non-snippets / already-archived).

**audit_store.go** (append-only PG ledger)
- `type AuditAction string`; `const AuditCreate/Update/Delete/Install/Activate/Archive/AutoArchive/CleanupPending` — the eight audit actions (mirror the 0010 CHECK).
- `type ApprovalSource string`; `const ApprovalNone/AskUser/CLI/Auto` — the D-29 approval-source matrix.
- `var ErrAuditImmutable (42501)`, `ErrAuditIncoherent (23514)` — SQLSTATE-classified sentinels.
- `type AuditStore struct{pool; q}` (INSERT+SELECT only); `NewAuditStore(pool) *AuditStore`.
- `type AuditRow` / `type AuditInsert` — domain projection + insert payload; `(AuditInsert) toParams()` maps to sqlc with NULL boundary mapping.
- `InsertAuditTx(ctx, q *sqlc.Queries, in AuditInsert) error` — append one row through a tx-bound Queries (production write path).
- `type AuditFilter struct{SkillName string; Since time.Time; Limit int}`; `(*AuditStore) List(ctx, f) ([]AuditRow, error)` — newest-first read (CLI).
- `auditFromRow(r) AuditRow`, `classifyAuditErr(err) error` — projection + SQLSTATE classification.

**audit_store_integration.go** (`//go:build db_integration`)
- `(*AuditStore) InsertAudit(ctx, in) (AuditRow, error)` — non-tx insert helper for schema round-trip tests.

**resume.go**
- `type ResumeHandler struct{writer *Writer}` — the D-03 activation channel for a model-proposed mutation; `NewResumeHandler(w) *ResumeHandler`.
- `const ResumeAccept/Decline/Cancel` — mirror `askuser.Action` as plain strings (no askuser import).
- `(*ResumeHandler) Resume(ctx, action, name, pausedToken, actor) error` — accept→`Activate(ApprovalAskUser)`, decline/cancel→`DiscardPending`.
- `(*Writer) DiscardPending(ctx, name, src, pausedToken, actor) error` — removes the pending dir and records the ask_user/cli rejection audit row.

### `internal/skilladapters` — composition-root seam bridging `*skills.{Loader,Writer}` onto the tools-package interfaces
Keeps `internal/agent/tools` free of an `internal/skills` import while giving the prod root and the eval registry ONE shared adapter (IN-04). Single file.

**skilladapters.go**
- `var modelActor = skills.AuditActor{ActorID: "model"}` — labels every model-path write/save.
- `type Loader struct{loader *skills.Loader; manCap int; exportDir string}`; `NewLoader(loader, manCap, exportDir) *Loader`.
- `(*Loader) List() []tools.SkillMeta`, `Body(name) (string, bool)`, `ManifestDescription() string`, `Snippet(name) (instructions, hostPath, interpreter string, ok bool)` — project skills into the tool-local shapes; `Snippet` resolves an active snippet's HOST by-path invocation (D-01).
- `type Writer struct{w *skills.Writer}`; `NewWriter(w) *Writer`.
- `(*Writer) WriteMutation(ctx, action, name, description, body string, always bool) (string, error)` — maps the tool call onto the live Writer (actor "model").
- `(*Writer) SaveSnippet(ctx, name, language, code, description string, needsNetwork, needsWorkspace bool) (string, error)` — maps the ungated `save_snippet` call (validates+blocklists CODE, lands pending).
- `(*Writer) Restore(ctx, name) (string, error)` / `ArchiveSnippet(ctx, name) (string, error)` — map restore/archive onto the live Writer with the cli ApprovalSource.

### `internal/cron` — Phase-10 scheduler (tick loop + HA claim + crash recovery + store)
A long-lived tick loop selecting due tasks and claiming each under a held-conn session advisory lock (singleton across workers), with heartbeat liveness, boot orphan-scan + missed catch-up, a kind→handler dispatcher (no switch), a composite notifier with origin-channel precedence, and the sqlc store over `scheduler_tasks`+`agent_job_runs`+`pending_notifications`. ~11 source files.

**schedule.go** (DST-safe schedule engine)
- `const MinScheduleEveryMinutes = 5`; `type ScheduleKind string`; `const KindAt/KindEvery/KindCron`.
- `var ErrInvalidScheduleKind/ErrInvalidCronExpr/ErrEveryTooSmall/ErrMissingRunAt/ErrPastRunAt/ErrInvalidTimezone`.
- `type ScheduleSpec struct{Kind ScheduleKind; CronExpr string; EveryMinutes int; RunAt time.Time; TZ string}`.
- `ParseSchedule(kind, cronExpr string, everyMinutes int, runAt time.Time, tz string) (ScheduleSpec, error)` — validates grammar (gronx.IsValid for cron, floor for every, non-zero run_at for at, LoadLocation for tz); default tz `Europe/Rome`.
- `NextRunAt(spec, after time.Time) (time.Time, error)` — next fire strictly after `after`, in UTC; cron recomputes IN-ZONE (D-07, never a fixed offset).
- `FirstFire(spec, now) (time.Time, error)` — first fire for a fresh spec; rejects an unschedulable past `at` with `ErrPastRunAt` (the single gate both the LLM tool and CLI call).

**store.go**
- `var ErrTaskNotFound / ErrAlreadyRunning`; `type TaskKind string`; `const KindReminder/KindAgentJob/KindBackupPostgres/KindBackupNeo4j/KindSkillTTLSweep`.
- `type Store struct{pool; q}`; `New(pool) *Store`.
- `type Task struct{...}` / `type Run struct{...}` — domain projections of `scheduler_tasks` / `agent_job_runs`.
- `type CreateTaskParams struct{...}`; `(*Store) CreateTask(ctx, p) (Task, error)` — inserts at the initial Status in one INSERT (a gated `pending_approval` task is never momentarily active, WR-03).
- `(*Store) GetTask / ListActiveTasks / CancelTask / UpdateNextRunAt / InsertRun / Heartbeat` — task+run CRUD.
- `(*Store) CreateRunAndAdvance(ctx, taskID, stepBudget, next) (Run, error)` — atomic claim-then-reschedule (`db.WithTx`).
- `type CompleteRunParams struct{...}`; `(*Store) CompleteRun(ctx, p) error` — terminal write; a duplicate `completed_with_hash` (23505) is swallowed as `ErrAlreadyRunning` (SC#2 idempotency).
- `taskFromRow / runFromRow` + pgtype helpers (`newUUID/parseUUID/uuidOrNull/uuidString/text/int4OrNull/tsOrNull/payloadOrEmpty`) and `isUniqueViolation(err) bool` (SQLSTATE 23505).

**store_runs.go** (held-conn writers + recovery)
- `(*Store) insertRunOnConn(ctx, conn, taskID, stepBudget) (Run, error)` / `setMissedSinceOnConn(...)` — run-row writes on the advisory-lock conn.
- `(*Store) GetRun / DueTasks(ctx, limit) ([]Task, error)` — due-task batch pickup (limit clamped to [1,MaxInt32], WR-02; correctness held by the per-task advisory lock).
- `type StaleRun struct{RunID, TaskID string}`; `type PendingNotification struct{...; IdentityID string}`; `type InsertPendingNotificationParams struct{...; IdentityID string}`.
- `(*Store) ScanStaleRuns(ctx, staleSeconds float64) ([]StaleRun, error)` — boot orphan-scan input.
- `(*Store) InsertPendingNotification / SweepDueNotifications(ctx, attemptBound, limit) / MarkNotificationDelivered / MarkNotificationFailed / MarkUnknownRecovery` — durable notification queue + recovery transitions; `pendingNotificationFromRow`.

**scheduler.go** (the tick loop)
- `const defaultTickInterval=30s, defaultMaxConcurrentRuns=4, defaultHeartbeatInterval=30s, staleRecoverySeconds=90`.
- `type Dispatcher interface{Dispatch(ctx, task Task, c *Claim) error}`; `type notificationSweeper interface{sweepNotifications(ctx) error}`.
- `type Scheduler struct{Now func() time.Time; store; pool; dispatch; maxConcurrent; tickInterval; hbInterval; lastTickUnix atomic.Int64; reschedulesOnRecovery func(TaskKind) bool}`.
- `type SchedulerConfig struct{Dispatch; MaxConcurrent; TickInterval; Now; ReschedulesOnRecovery}`; `NewScheduler(pool, store, cfg) *Scheduler` (resolves cap/tick from cfg→`AURA_SCHEDULER_*` env→defaults); `envInt(key, def) int`.
- `(*Scheduler) Start(ctx) error` — boot recovery (orphan scan + missed catch-up + dispatch the collapsed missed fires) then the ticker loop until ctx cancel; graceful (joins in-flight runs, goleak-clean).
- `(*Scheduler) tick(ctx) error` — selects due tasks, claims+dispatches each under a `maxConcurrent`-bounded semaphore, sweeps notifications.
- `(*Scheduler) markTick / LastTick() time.Time`.
- `(*Scheduler) runOne(ctx, task)` — claims, reschedules-on-claim (singleton, SC#1), starts the heartbeat, dispatches; on a lost lock logs `skipped: previous run in progress` and reschedules.
- `(*Scheduler) runMissed(ctx, m MissedTask)` — fires one collapsed boot catch-up (D-18): same lifecycle as runOne but does NOT reschedule and threads `MissedSince`.
- `(*Scheduler) reschedule(ctx, task)` — advances `next_run_at` to the next fire.
- `(*Scheduler) DuringQuietHours(tz) bool` / `QuietHoursEnd(tz) (time.Time, bool)` — `AURA_SCHEDULER_QUIET_HOURS` window predicate (handles wrap-around) for the Notifier's deferral (D-23); `parseQuietWindow / parseHHMM / minuteOfDay` helpers.

**claim.go** (HA core)
- `taskHash(id string) int64` — FNV-1a 64 over the task UUID → advisory key.
- `type Claim struct{conn *pgxpool.Conn; RunID string; hash int64; MissedSince time.Time}`; `(*Claim) Conn() *pgxpool.Conn`.
- `(*Scheduler) claim(ctx, task) (*Claim, error)` — acquires a dedicated conn, `pg_try_advisory_lock`, opens the run row on the held conn; on a lost lock releases and returns `ErrAlreadyRunning` (D-04 singleton).
- `(*Claim) release(ctx)` — unlocks on the SAME held conn and returns it to the pool (Pitfall 1; nil-safe).

**heartbeat.go**
- `startHeartbeat(ctx, conn *pgxpool.Conn, runID string, interval) (stop func())` — liveness ticker bumping `last_heartbeat_at` on the run's held conn; `stop()` cancels and BLOCKS until exit (goleak-clean).

**recover.go** (boot reconciliation)
- `(*Scheduler) recoverOrphans(ctx) error` — marks runs with a stale heartbeat (>90s) as `unknown_recovery` (always returns nil — WARN-only degradation).
- `type MissedTask struct{Task Task; MissedSince time.Time}`.
- `(*Scheduler) catchUpMissed(ctx) ([]MissedTask, error)` — collapses every overdue recurring task to ONE catch-up fire, gated by the handler's `ReschedulesOnRecovery` (M-g); always advances the cadence, only re-fires if the handler reschedules.
- `(*Scheduler) firesOnRecovery(kind) bool` (nil lookup fails SAFE to "always fire"); `specFromTask(t Task) ScheduleSpec`.

**dispatch.go** (kind→handler routing + run lifecycle)
- `type HandlerMeta struct{Kind TaskKind; MaxDuration time.Duration; ReschedulesOnRecovery bool}`; `type Job struct{Payload []byte; StepBudget int; RunID string; MissedSince time.Time}`.
- `type Handler interface{Meta() HandlerMeta; Run(ctx, job Job) (summary string, err error)}` (cron-local, consumer-declared — avoids the cron↔handlers cycle).
- `type RunCompleter interface{CompleteRun(...)}`; `type PendingNotificationStore interface{...}` (both satisfied by `*Store`).
- `type DispatchDeps struct{Store RunCompleter; NotificationStore; Notifier; AlertThreshold scoring.RiskTier; QuietHours func(tz) bool; QuietHoursEnd func(tz) (time.Time, bool); ChannelDeliverer; PreferOriginChannel bool}`.
- `type Dispatch struct{deps; handlers map[TaskKind]Handler}` (satisfies `Dispatcher`); `NewDispatch(handlers, deps) *Dispatch`.
- `(*Dispatch) ReschedulesOnRecovery(kind) bool` — the handler-meta lookup seam the boot catch-up consults (unknown kind → false).
- `(*Dispatch) Dispatch(ctx, task, c *Claim) error` — runs the handler (missing-handler → terminal failed run, never silent), writes the terminal run state, notifies (on success AND failure).
- `const completeRunTimeout = 5s`; `(*Dispatch) complete(...)` — terminal write on a `context.WithoutCancel`+timeout ctx so a shutdown can't wedge or strand the run as 'running' (M-h).
- `(*Dispatch) notify(...)` — per-task route delivery (D-19/D-21); defers non-destructive notifications inside quiet hours (durable `notify_after`), prefers the origin channel (`deliverToOrigin`), persists a failed pending row on undelivered.
- `(*Dispatch) deferred / deferredUntil / insertPendingNotification / taskTier(scoring.ComputeTaskTier) / sweepNotifications / markSweptDelivered / markSweptFailed` — quiet-hours + notification-queue helpers; `sweepNotifications` re-routes due/failed rows through the same origin gate.
- `const pendingNotificationAttemptBound=3, pendingNotificationSweepLimit=50`.

**deliver.go** (origin-channel precedence — Phase 20 R4/R7)
- `type ChannelDeliverer interface{DeliverToIdentity(ctx, identityID, text string) (delivered bool, err error)}` — cron-local seam over `*channels.Registry`.
- `(*Dispatch) originGate(identityID, notifyRoute string) bool` — the SINGLE precedence source: off when the kill-switch is off / no deliverer / a deliberate `whatsapp`/`email` route / an un-owned ("" / "local") identity ("stdout" defers to origin).
- `(*Dispatch) deliverToOrigin(ctx, task, runID, text) (handled bool)` — prefers the origin channel for a live task; owns-but-failed queues a failed pending row and returns handled (no fallback double-delivery, Pitfall 3).
- `type sweepOutcome int`; `const sweepFallback/sweepDelivered/sweepKeep`; `(*Dispatch) deliverSweptRow(ctx, n PendingNotification) sweepOutcome` — same gate keyed on the row's identity snapshot.

**notify.go** (composite Notifier)
- `type NotifyRoute string`; `const RouteWhatsApp/RouteEmail/RouteStdout`; `const toolSendMessage/toolSendEmail`.
- `type SelfSendResolver interface{Resolve(bareName) (SelfSendTool, bool)}`; `type SelfSendTool interface{Send(ctx, args json.RawMessage) error}` — cron-local MCP self-send seam (avoids the tools import cycle).
- `type Notifier interface{Notify(ctx, route, recipient, text string) error}`.
- `type compositeNotifier struct{resolver; out io.Writer}`; `NewNotifier(resolver) Notifier` (nil resolver → stdout-only).
- `(*compositeNotifier) Notify / resolveRoute / sendViaMCP / stdout` — route precedence (per-task → `AURA_SCHEDULER_NOTIFY_DEFAULT` → stdout), MCP self-send with fail-soft stdout fallback surfacing the undelivered signal (D-22).
- `buildSend(route, recipient, text) (string, json.RawMessage)` — maps a route to its MCP tool bare name + arg JSON (recipient falls back to `AURA_SCHEDULER_NOTIFY_RECIPIENT`).

**tzdata.go** — `import _ "time/tzdata"` (embeds the IANA db for DST-safe in-zone recompute).

### `internal/cron/handlers` — per-`TaskKind` scheduler handlers (D-28, no central switch)
Each kind is one file with a `Handler` impl + `HandlerMeta`. The package is free of any `internal/cron` import (D-24): a handler receives a plain `Job` and returns a summary string; the cron dispatcher owns run completion/notification. ~5 source files.

**handler.go** (shared contracts + agent worker construction)
- `type TaskKind string`; `const KindReminder/KindAgentJob/KindBackupPostgres/KindBackupNeo4j/KindSkillTTLSweep` (mirror cron's as plain strings).
- `type HandlerMeta struct{Kind; MaxDuration; ReschedulesOnRecovery}`; `type Job struct{Payload []byte; StepBudget int; RunID string; MissedSince time.Time}`.
- `type Handler interface{Meta() HandlerMeta; Run(ctx, job Job) (summary string, err error)}`.
- `type AgentDeps struct{Client llm.Client; LLM llm.Config; Registry *tools.Registry; PreviewCap int; RunDir, Workspace string; MaxDuration time.Duration}` — shared runtime for an `agent_job`'s ephemeral LlmAgent.
- `childRegistry(parent) *tools.Registry` — full parent registry MINUS `swarm_spawn` (`tools.Without`, keeps `internal/swarm` out of the import graph, D-24; ask_user stays).
- `newAgentWorker(deps, runID, prior []llm.Message) *agent.LlmAgent` — constructs the ephemeral worker (mirrors `swarm.runChild`) with a FLAT `agent_job:<runID>` session and the workspace tail-hint (default process cwd).

**agentjob.go**
- `const agentJobMaxDuration=120s, autoRejectMarker, maxAutoRejects=8`.
- `type AgentJobHandler struct{Deps AgentDeps}`; `type agentJobPayload struct{Goal string}`.
- `(AgentJobHandler) Meta()` — `agent_job` reschedules on recovery, wall-bounded by `MaxDuration`.
- `(AgentJobHandler) Run(ctx, job Job) (string, error)` — constructs the ephemeral LlmAgent, drains its Event stream, returns the final assistant content as the audit summary; step budget INHERITED from the row (D-24); `ask_user` auto-rejected via inject-and-continue (D-25), bounded by `maxAutoRejects`, never blocks.
- `drain(ctx, worker, budget) (string, *agent.AwaitingInput, error)` — one LlmAgent invocation to completion or the first pause.
- `newJobBudget(stepBudget) (*agent.Budget, error)`, `assistantAskUserTurn(pause) llm.Message`, `askUserKind(kind) string`, `agentJobGoal(payload) string`, `appendLine(b, content)` — budget/wire-reconstruction/payload helpers.

**reminder.go**
- `const reminderMaxDuration=30s`; `type ReminderHandler struct{}`; `type reminderPayload struct{Text string}`.
- `(ReminderHandler) Meta()` — does NOT reschedule on recovery (D-18 collapses windows upstream).
- `(ReminderHandler) Run(_, job Job) (string, error)` — returns the verbatim payload text as the summary (no LLM/tools); empty payload degrades to a generic line; `reminderText(payload) string`.

**skill_ttl.go**
- `const skillTTLMaxDuration=2m`.
- `type SnippetSweeper interface{SweepExpiredSnippets(ctx, ttl, now, actorID string) (archived, kept []string, err error)}` — consumer-declared seam (the live `*skills.Writer` satisfies it).
- `type SkillTTLSweepHandler struct{Sweeper SnippetSweeper; TTL time.Duration; now func() time.Time}` (system-seeded, not model-schedulable).
- `(SkillTTLSweepHandler) Meta()` — no reschedule-on-recovery (idempotent re-evaluation).
- `(SkillTTLSweepHandler) Run(ctx, _ Job) (string, error)` — sweeps expired snippets and returns an `archived N / kept M` count summary; nil sweeper or `TTL<=0` is a no-op success.

**backup.go**
- `const backupRetentionPostgres=14d, backupRetentionNeo4j=7d, backupMaxDuration=30m, missedBackupAlertAfter=24h`; `const neo4jExportCypherAll` (the APOC export query).
- `type BackupVariant string`; `const BackupPostgres/BackupNeo4j`; `type pgDumper / neo4jDumper func`.
- `type BackupHandler struct{Variant BackupVariant; pgDumper; neo4jDumper}` — dumps a DB from inside the Aura box without a Docker socket (network `pg_dump` / APOC over Bolt) into `AURA_BACKUP_DIR`.
- `(BackupHandler) Meta()` — reschedules on recovery, generous I/O budget.
- `(BackupHandler) Run(ctx, job Job) (string, error)` — emits the missed-backup alert, dumps, verifies the host-visible artifact, sweeps retention, returns an operator summary.
- `(BackupHandler) dump(...)`; `type postgresDumpRequest struct{...}` with `argv()/validate()` + `postgresDumpRequestFromEnv/FromURL` (derives from `AURA_DB_MIGRATE_URL` or `POSTGRES_*`); `defaultPostgresDumper` (PGPASSWORD via env, never argv).
- `type neo4jDumpRequest struct{...}` + `neo4jDumpRequestFromEnv/validate`; `defaultNeo4jDumper` (APOC `cypher.all` over the neo4j-go-driver); `writeNeo4jCypherFile(dest, statements)` (atomic temp+rename).
- `(BackupHandler) dumpFilename/filePrefix/retention`, `envOr(key, fallback)`, `backupDir() (string, error)` (resolves `AURA_BACKUP_DIR`, expands `~`), `sweepRetention(dir, prefix, window) int`, `isBackupArtifact(name) bool`, `MissedBackupAlert(variant, missedSince, now) bool` (SC#3 alert past the 24h window).

### `internal/onboarding` — first-run profile interview (LoopAgent + state machine + LLM extractor)
A workflow `LoopAgent` driving a queued state-machine session through identity/work/projects/social/style/draft steps, an LLM-backed free-text answer extractor (fail-soft), and a draft renderer that emits an `Agent.md` + `profile.Preferences`. Note: the "injector / store / updater" the task referenced live in the sibling `internal/profile` package (`render.go` `RenderAgentMD`, `store.go`, `parser.go`, atomic writers) — outside this package's source. ~4 source files.

**interview.go** (LoopAgent adapter)
- `const InterviewStepAgentName = "InterviewStepAgent"`, `LoopName = "ProfileOnboardingLoop"`.
- `type InterviewStepAgent struct{session *Session}` — adapts session transitions to agent events; `NewInterviewStepAgent(session) *InterviewStepAgent`.
- `NewLoop(session *Session, maxIter uint) agent.Agent` — wraps the step agent in `workflow.NewLoop`.
- `(*InterviewStepAgent) Name() / Description() / SubAgents() / FindAgent(name)` — the `agent.Agent` surface (a leaf step).
- `(*InterviewStepAgent) Run(ic) iter.Seq2[*agent.Event, error]` — emits the next onboarding transition event (escalates on terminal).
- `transitionEvent(ic, author, out Transition) *agent.Event` — wraps a transition into an Event carrying `Content`, `Escalate=out.Terminal`, `StateDelta`.

**session.go** (the state machine)
- `type Intent string`; `const IntentAnswer/Confirm/Edit/Skip/Cancel/Restart`.
- `type Step string`; `const StepIdentity/Work/Projects/Social/Style/Draft`.
- `type Status string`; `const StatusActive/Draft/Completed/Skipped/Canceled`.
- `var ErrDraftRequired / ErrTerminal / ErrInvalidIntent`.
- `type Answers struct{Name, Role, Company, Location string; Expertise, Stack, Projects, Goals, Interests, People, Vetoes []string; Lang, Timezone, TonePreference, ResponseLength string; VoiceMode, CanProactiveMessage *bool; CustomInstructions string}` — structured profile facts.
- `type Input struct{Intent; Text string; Answers}`; `type Transition struct{Content string; StateDelta map[string]any; Terminal bool}`.
- `type Session struct{IdentityID, IdentityName string; Step; Status; Answers; DraftAgentMD string; Preferences profile.Preferences; pending []Input; prompted bool}`; `NewSession(identityID, identityName) *Session`.
- `(*Session) Apply(in Input) (Transition, error)` — applies one input (answer/confirm/edit/skip/cancel/restart) and returns the next transition; terminal sessions reject all but restart.
- `(*Session) Queue(inputs ...Input)` — appends inputs for the loop agent to consume in order; `nextTransition() (Transition, bool, error)` / `applyQueued()` drive prompt-then-apply ordering.
- `(*Session) applyAnswer / confirm / edit / replaceAnswers / restart / mergeAnswers` — step progression, draft confirm (emits `agent_md`+`preferences_json` StateDelta), edit (replace-then-redraft), progressive merge (`mergeStr/mergeSlice` dedup) vs replace (`replaceStr/replaceSlice`).
- `(*Session) refreshDraft() error` — calls `ExtractDraft` to refresh `DraftAgentMD`+`Preferences`.
- `(*Session) question / questionText(step) / currentPrompt / draft / terminal / state / preferencesJSON` — per-step English prompt text + StateDelta builders (channels override the user-facing text by `onboarding_step`).
- `boolValue(v *bool) bool`, `shouldApplyBeforePrompt(in Input) bool` (skip/cancel/restart apply before the prompt).

**extractor.go** (draft renderer)
- `const maxAnswerFieldBytes = 2048`.
- `type Draft struct{AgentMD string; Preferences profile.Preferences; PreferencesJSON string}`.
- `ExtractDraft(answers Answers) (Draft, error)` — cleans answers, builds `profile.Preferences`, renders a bounded `Agent.md` via `profile.RenderAgentMD` (8 sections), caps to `profile.MaxAgentMDBytes`.
- `cleanAnswers / cleanField / identityLines / joinRoleCompany / expertiseLines / projectGoalLines / styleLines / bulletLines / customInstructionLines / languagePreference / truncateBytes` — per-section line builders + byte-bounded truncation; `languagePreference` folds it/en variants.

**extractor_llm.go** (LLM answer extraction)
- `type AnswerExtractor interface{Extract(ctx, step Step, raw string) (Answers, error)}`.
- `type LLMAnswerExtractor struct{client llm.Client; model string}`; `NewLLMAnswerExtractor(client, model) *LLMAnswerExtractor`.
- `type extractDTO struct{...}` — the per-field JSON the model returns.
- `(*LLMAnswerExtractor) Extract(ctx, step, raw) (Answers, error)` — one-shot tool-free completion (temp 0, reasoning disabled, `ToolChoice:"none"`); never hard-errors — transport/parse failure falls back to storing the raw answer in the step's primary field.
- `extractJSON(s) string`, `fallbackAnswers(step, raw) Answers`, `extractSystemPrompt(step) string` — JSON-extraction + per-step extraction instruction (English; normalizes spellings, maps it/en synonyms).

### `internal/eval` — live operator-authorized CoT / tool-use / swarm / skills eval harness (build-tag gated)
A MANUAL paid gate (`//go:build cot_eval`, `OPENROUTER_API_KEY`-gated, never CI) that drives the real LlmAgent over the real openai_compat client and scores every AI-SPEC dimension plus CoT/guardrail/swarm/skills extensions. The harness entry points (`TestCoTEval`/`TestSwarmE2E`/`TestSkillsE2E`/etc.) live in `_test.go` files (excluded from this inventory); the non-test files hold the dataset, scoring predicates, judge, and capture. ~6 source files.

**doc.go** — package doc only (no build tag, so the default build is valid); documents the run invocation.

**dataset_cot_eval.go** (`cot_eval`)
- `type dimension string`; `const dimSecretRedaction/StreamingFidelity/ToolLoop/CostHonesty/CachePrefix/Budget/Cancellation/Reasoning/Guardrail/CacheRatio` (the AI-SPEC dimensions + advisory CoT/cache); the Phase-9 swarm dimensions `dimSwarmHardFloor/AutonomousParallel/SubAnswerCorrectness/AggregationQuality/NoOverSpawn`; the Phase-11 skills dimensions `dimSkillsHardFloor/CapabilityGapRecognition/SkillOutputQuality`.
- `const judgeSkillsGate = 0.90`, `judgeSwarmGate = 0.90` — the ≥90% judge means.
- `type scenario struct{id string; prompts []string; dimensions []dimension; expectedTool, ...; swarm *swarmExpect; noOverSpawn bool; skills *skillsExpect}` — one live dataset entry declaring the observable signals.
- `type swarmExpect struct{minWorkers int; expectFacts []string; mailQuery, mailToken, waToken, waChatSelf string; timingBudget float64}`.
- `intPtr(n) *int`.
- `scenarios() []scenario` — the 12 CoT scenarios (Italian prompts: arithmetic, reasoning, current_time tool, 2-turn memory, guardrail refusals, length truncation, budget trip, cancel mid-stream).
- `const swarmMailTagMarker/swarmWATagMarker`; `swarmScenarios(selfMail, selfPhone, waChatSelf string) []scenario` — the autonomous-swarm + no-over-spawn control scenarios (NATURAL prompts; the judge scores the fan-out choice against ground truth).

**scenarios_skills.go** (`cot_eval`)
- `type skillsExpect struct{installTargetRepo, installSelector, xlsxExt, readBackImport string; forbiddenWords []string; judgeBudget float64; maxSteadyStateCalls int; maxSteadyStateWallClock time.Duration}` — xlsx North-Star hard-floor ground-truth signals + 18-04 steady-state reuse thresholds.
- `skillsScenarios() []scenario` — the single xlsx North-Star scenario (natural Italian "make me an Excel of today's Yahoo Finance" with no "skill"/"install" word).
- `skillsSnippetReuseScenarios() []scenario` — the steady-state snippet-reuse scenario (same prompt, ≤6 dispatches / <40s gate over the `tool_invocations` ledger).

**capture_cot_eval.go** (`cot_eval || live_e2e`)
- `type eventKind string`; `const kindChunk/ToolCall/ToolResult/Final/Terminal`; `const askUserToolNameCapture = "ask_user"`.
- `type turnCapture struct{prose, rawProse, finish string; usage llm.Usage; eventKinds []eventKind; toolNames, toolArgs []string; toolCallMS []float64; toolResults []string; toolResultMS []float64; terminalReason string; terminated bool; firstByteMS, totalMS float64; runErr error; paused bool; awaitingInput *agent.AwaitingInput}` — the full observable record of one `LlmAgent.Run` (mirrors `cmd/aura/chat_render.go` without importing it).
- `captureTurn(run func() func(func(*agent.Event, error) bool)) *turnCapture` — drives one Run to completion/consumer-stop, recording prose, ordered Event kinds, tool calls+args+timings, tool results, finish reason, usage, latency, HITL pause.
- `flushRemainder / isToolResultPreview / isTerminalToolCall / usageFromStateDelta / anyInt / anyFloat` — chat_render.go mirror helpers (usage reconstruction tolerant of `json.Number`).

**scoring_cot_eval.go** (`cot_eval`)
- `const reportPath` (docs/, never /tmp); `type dimResult struct{pass, total int}`; `type scenarioMetrics struct{...}` — per-dimension and per-scenario accumulators.
- Scoring predicates: `secretLeaked(secret, c) bool`, `streamingClean(c) bool`, `cancelledOK(sc, _) bool`, `toolLoopOK(sc, c) bool`, `orderedToolFlow(kinds) bool`, `costHonest(cfg, c) bool`.
- Swarm hard-floor predicates: `countSwarmWorkers(c) int` (parses tool-result report arrays, takes the longest — never substring-scans, WR-02), `calledSwarmSpawn(c) bool`, `factsPresent(prose, facts) bool`, `timingOK(swarmMS, baselineMS, budget) bool`, `looksRefusal(prose) bool`, `hasTimestamp(s) bool`, `contains[T comparable](xs, v) bool`, `dimResultRecordAdvisory(...)`.
- `newBudget(t, sc) *agent.Budget`, `buildRegistry() *tools.Registry` (mirrors the prod tool set: text_response, tool_search, read_tool_output, current_time).
- Aggregation/report: `logMatrix / enforce / classOf / dimOrder / thresholdOf / writeReport` — matrix logging, threshold enforcement (secret_redaction 100% release-blocking; asserted dims 100%; reasoning+cache advisory), and the scored markdown report writer.

**judge_cot_eval.go** (`cot_eval`)
- `type judgeVerdict struct{Score int; Justification string; Refused bool}`.
- `const reasoningRubric / guardrailRubric` — the LLM-judge rubrics (1-5).
- `var swarmRubrics map[dimension]string` (the four D-22 swarm judge dimensions), `var skillsRubrics map[dimension]string` (the two D-35 skills dimensions).
- `runSkillsJudge(...) (scores, mean, pass, err)` / `runSwarmJudge(...)` — thin wrappers over the shared driver.
- `runRubricJudge(ctx, client, model, rubrics, gate, label, dims, question, answer, observed) (scores map[dimension]int, mean float64, pass bool, err error)` — the shared dual-gate driver: scores each dimension 1-5 against its rubric (feeding the judge the observed ground truth), returns per-dimension scores + the [0,1]-normalized mean + the gate verdict.
- `runJudge / runJudgeUser / parseVerdict` — single judge call (MaxTokens 2048 — DeepSeek burns reasoning budget) + tolerant JSON-verdict decode with a 1-5 range guard.

### `internal/runner` — orchestration layer (turn loop + resume + persistence + learners)
The `Runner` drives the agent turn-by-turn over a fresh per-round `LlmAgent` seeded with rehydrated history, persists each turn via the conversation store, is the SOLE writer of `paused_states`, resolves resumes as a fresh `agent.Run` over the rehydrated history (SC-4, never a silent re-run), owns the goleak-clean auto-title WaitGroup, and wires the reasoning-tier classifier + two async self-improvement learners. It is NOT an `agent.Agent` (AM-03). ~6 source files.

**interfaces.go** (consumer-side narrow seams — D-A2-02)
- `type ConversationStore interface{Create/Get/List/UpdateStatus/Rename/SetTitleIfNull/CountTurns/AppendTurn/AppendAssistantTurnWithCacheMetric/LoadHistory/LoadManagedHistory/SearchConversationTurns/Delete}` — satisfied implicitly by `*conversations.Store`.
- `type ContextBlockProvider func(ctx, owner identity.Identity) string` — renders identity-aware `messages[1]` context.
- `type PauseStore interface{Insert/GetByToken/ListPending/MarkResumed/MarkResumedBatch/AutoResolveForConversation}` (`*askuser.Store`; Runner is sole `Insert` caller, T-04-19).
- `type CacheMetricStore interface{Insert}`, `type ToolInvocationStore interface{Insert}`, `type IdentityStore interface{GetIdentityByName/GetIdentityByID}`.

**runner.go** (core construction + turn loop)
- `const defaultTitleTimeout=30s, defaultStopTimeout=10s, autoTitleMinSeq=3, localIdentityName="local"`; `var ErrThreadBusy`.
- `type threadLockHeldKey`; `WithThreadLockHeld(ctx) context.Context` / `threadLockHeld(ctx) bool` — let HTTP gateways reject busy threads without double-locking.
- `type Deps struct{Conv, Pause, Identity, CacheMetrics, ToolInvocations stores; Client llm.Client; Registry *tools.Registry; LLM llm.Config; Breaker *llm.Breaker; RunDir string; PreviewCap, EvictAfter int; Workspace string; TitleTimeout, StopTimeout time.Duration; ContextBlock ContextBlockProvider; AlwaysBlock func() string; ResumeHook ResumeHook; Embedder prompt.Embedder; ExampleStore prompt.ExampleStore; ReasoningSaver reasoninglearn.Saver; ToolSelectSaver toolselectlearn.Saver}` — all constructor inputs.
- `type toolSelectObserver interface{Observe(request, usedTool string); Close()}`; `type ResumeHook func(ctx, pending askuser.Pending, resp ResponseInput) error`.
- `type Runner struct{...; client; registry; cfg llm.Config; breaker; classifier *prompt.ReasoningClassifier; learner *reasoninglearn.Learner; toolSelectLearner toolSelectObserver; threadLocks sync.Map; wg sync.WaitGroup}`.
- `New(d Deps) *Runner` — applies timeout defaults, resolves the workspace (default cwd), builds the shared reasoning classifier ONCE, wires the granite embedder into `tool_search`, mints the shared breaker, attaches the reasoning + tool-select learners.
- `const embedHealthCheckTimeout=3s`; `wireToolSearchEmbedder(reg, embedder)` — sets `ToolSearch.Embed`, logs an unreachable embed sidecar at boot (never fatal — Open-Q #2).
- `(*Runner) CloseLearner()` — stops both async learners.
- `type ResponseInput struct{Action, Content string}` (accept|decline|cancel — the MCP three-action model).
- `(*Runner) NewConversation(ctx) / NewConversationWithID(ctx, conversationID) (string, error)` — create a conversation owned by the `local` identity.
- `(*Runner) EnsureConversation(ctx, convID) error` — lazy-create for channel-keyed conversations; a lost-create race is reconciled by SQLSTATE 23505 (`isUniqueViolation`), never by mere row presence (M-08).
- `(*Runner) Turn(ctx, convID, userMsg *string) iter.Seq2[*agent.Event, error]` — the sole loop-driver; takes the per-thread lock (unless already held), delegates to `turnLocked`.
- `(*Runner) TryLockThread(convID) (func(), bool)` / `lockForThread(convID) *sync.Mutex` — non-blocking per-conversation lock (HTTP 409).
- `(*Runner) turnLocked(...)` — persists the new user turn, short-circuits a fast-reply greeting, builds the context config + managed history, builds a fresh agent, drives one round persisting each Event, observes the pause Event(s), and flushes the combined pause assistant turn on EVERY return path (deferred `flushOnce`, WithoutCancel — fixes the infinite-pause-loop bug), then fires the auto-title worker.
- `(*Runner) appendUserTurn / contextConfig / renderContextBlock` — user-turn persistence + L1/L2/L2.5 context-config + identity/always-block rendering.
- `(*Runner) buildAgent(ctx, convID, history) (*agent.LlmAgent, agent.InvocationContext, context.CancelFunc, error)` — fresh per-round LlmAgent seeded with rehydrated history (`session_id==conversation_id`, shared classifier+breaker, per-turn budget+deadline).
- `stripLeadingSystem(history) []llm.Message` — drops a persisted leading system turn (KV-cache: the agent owns messages[0]).

**runner_resume.go** (resume + lifecycle)
- `(*Runner) maybeAutoTitle(turnCtx, convID, history)` — fires the best-effort auto-title worker at seq>=3 on an untitled conversation; outlives the turn ctx (`WithoutCancel`) but is bounded by `titleTimeout` and tracked by the WaitGroup; works on a defensive history snapshot (WR-03).
- `const declinedContent / cancelledContent` — RoleTool bodies injected on decline / cancel.
- `(*Runner) SubmitAnswer(ctx, token, resp) (int, error)` — resolves ONE pause (three-action): claims the pause via `MarkResumed` FIRST (M-02 — rejects a duplicate resume before injecting), injects the RoleTool answer, applies the resume hook; cancel routes to `cancelConversation`. Returns remaining-pending count.
- `(*Runner) SubmitAnswers(ctx, answers) (int, error)` — resolves MANY pauses atomically (`MarkResumedBatch`); a cancel short-circuits the whole conversation.
- `(*Runner) applyResumeHook / injectAnswer / cancelConversation / injectCancelledAnswers / PendingFor / remainingPending` — resume-hook dispatch, RoleTool injection keyed by the original tool_call_id (SC-4 wire-correctness), whole-turn cancel (answer every open ask_user call + auto-resolve rows), FIFO pending read.
- `toResumeAnswer(resp) askuser.ResumeAnswer`.
- `(*Runner) Stop(ctx, convID) error` — terminates the lifecycle: auto-resolves orphan pendings, evicts per-session tool state, joins the title WaitGroup with a bounded wait (goleak-clean).
- `(*Runner) evictSessionToolState(convID)` — ranges the registry calling `Evict(convID)` on every `tools.SessionEvictor` (todo_write list, shell cwd, approval ledger — audit R-41).
- `(*Runner) waitWorkers(timeout) bool` — bounded WaitGroup drain.

**runner_persist.go** (Event-sourced persistence)
- `type turnTracker struct{convID, userMsg string; paused bool; pauses []*agent.AwaitingInput; pendingToolCalls []llm.ToolCall; openToolCalls map[string]struct{}; toolSeq int}`; `(*turnTracker) nextToolInvocationSeq / addPendingToolCall`.
- `(*Runner) persistEvent(ctx, tr, ev) error` — routes a pause Event → `persistPause`, a tool-invocation Event → `persistToolTurn` + best-effort `persistToolInvocationLedger`, a final Event → `persistAssistantAnswer`.
- `(*Runner) persistToolTurn(ctx, tr, ti) error` — reconstructs the assistant `tool_calls` turn + the matching RoleTool result turn from start/end facts; on a tool-end fires the LIVE non-blocking `toolSelectLearner.Observe(userMsg, toolName)` capture (Open-Q #3).
- `(*Runner) flushToolCalls / persistToolInvocationLedger` — assistant-tool_calls turn write + append-only ledger insert (non-fatal, redacted at the store boundary).
- `timePtrValue / toolInvocationTimestamp` — time helpers.
- `(*Runner) persistAssistantAnswer(ctx, convID, ev) error` — persists the terminal assistant turn + per-turn usage + the `cache_metrics` row through the atomic assistant-write seam; cost via `llm.CostUSDValue` (wire cost → price-table fallback → 0.0).
- `(*Runner) cacheMetricParams / persistPause / flushPause / assistantAskUserToolCalls / pauseOptionsJSON` — cache-metric build, paused_states row write (SOLE writer), the single combined ask_user assistant turn flushed at round end (CR-02 wire-validity).
- `usageFromStateDelta / anyInt / anyFloat` — usage reconstruction (chat_render.go mirror, `json.Number`-tolerant, M-07).

**runner_fastpath.go**
- `const fastPathFinishReason = "fast_path"`.
- `fastReplyFor(content) (string, bool)` — canned Italian greeting reply (ciao/salve/buongiorno/...).
- `normalizeGreeting(content) string`, `fastReplyEvent(convID, answer) (*agent.Event, error)`, `fastReplyChunkEvent(final) *agent.Event` — build the zero-token fast-reply final + chunk Events.

**reasoning_learner.go**
- `type reasoningOracle struct{client llm.Client; model string}`; `(*reasoningOracle) Label(ctx, text) (prompt.ReasoningTier, bool)` — the teacher: runs the router prompt (MaxTokens 32, reasoning disabled, `ToolChoice:"none"`) and parses the tier; never touches the user hot path.
- `buildReasoningLearner(d Deps, classifier *prompt.ReasoningClassifier) *reasoninglearn.Learner` — wires the async self-improvement worker when `ReasoningLearning` is on and a Saver+classifier exist; attaches it to the classifier via `SetLearner`.

**toolselect_learner.go**
- `type toolSelectTeacher struct{client; model; registry}`; `(*toolSelectTeacher) Label(ctx, request) (string, bool)` — the DeepSeek escalation oracle ("which single tool should have handled this?"), validated against the live deferred-tool catalog so a hallucinated name is rejected (mirrors `reasoningOracle`'s shape).
- `toolSelectRouterPrompt(catalog) string`, `deferredToolNames(reg) []string` — teacher prompt + sorted deferred-tool catalog.
- `buildToolSelectLearner(d Deps) *toolselectlearn.Learner` — wires the tool-selection active-learning loop (D-06/D-07): locates `tool_search`, enables the learned-boost over the SAME granite embedder, wires the ranker detector + the DeepSeek teacher + the `toolselectstore` Saver onto the activelearn core with a `Refresh` re-folding the per-tool centroids from Neo4j. nil unless a Saver+Embedder are wired.
- `type toolSearchRanker struct{ts *tools.ToolSearch}`; `(toolSearchRanker) Rank(ctx, query) (string, float64, bool)` — adapts `tool_search.RankForLearner` (stage-1 description bank only, guard #4); `lookupToolSearch(reg) *tools.ToolSearch`.

---

## Transport, Channels & UX

### `internal/agui` — AG-UI protocol transport adapter (Slice 8), one-way bridge from Aura's agent Event stream onto the official AG-UI community Go SDK
Consumes Aura's in-process `iter.Seq2[*agent.Event, error]` and maps it onto AG-UI `events.Event`. The boundary is strictly one-way (agui imports agent; the runtime never imports agui, CI-enforced). The translator is a pure function (no I/O, no goroutines), property/golden-testable; transport (SSE HTTP server + in-process fanout) is layered on top. ~8 source files.

**types.go**
- `type ConversationStore interface { Get(ctx, conversationID) (conversations.Conversation, error); LoadHistory(ctx, conversationID) ([]llm.Message, error) }` — narrow conversation surface the server consumes (`*conversations.Store` satisfies it).
- `var ErrEmptyThreadID, ErrNoMessages` — Aura-semantic run-precondition sentinels (distinct from SDK JSON parse errors).
- `ValidateRunInput(in types.RunAgentInput) error` — checks non-empty threadId + at least one message; does not re-parse JSON or re-validate UUID shape.
- `type IDGenerator interface { NewMessageID(); NewReasoningID(); NewToolResultID(toolCallID) string }` — mints non-empty AG-UI ids (separate `msg-`, `rsn-`, `msg-tool-` prefixes so CoT id ≠ answer id).
- `NewIDGenerator() IDGenerator` — default uuid-v4 minter (`uuidIDGenerator`).

**server.go**
- `const maxRunBodyBytes = 1<<20` — POST body cap (DoS guard).
- `type ServerConfig struct { CORSPermissive bool; BufferCap int; HealthCheck func(ctx) error; HealthDetails func() map[string]any; ReadinessProbes []ReadinessProbe }` — server knobs resolved from `AURA_AGUI_*`.
- `type Runner interface { Turn(ctx, convID, *userMsg) iter.Seq2[*agent.Event,error]; SubmitAnswers(ctx, map[string]runner.ResponseInput) (int, error) }` — narrow agent-driver surface (`*runner.Runner` satisfies it).
- `type threadTryLocker interface { TryLockThread(threadID) (func(), bool) }` — optional per-thread lock to reject concurrent runs (409).
- `type Server struct{ run; conv; idgen; cfg }` — minimal AG-UI HTTP gateway.
- `NewServer(run Runner, conv ConversationStore, cfg ServerConfig) *Server` — build gateway with default IDGenerator.
- `(*Server).Mux() http.Handler` — registers `GET /healthz`, `/readyz`, `/debug/vars` (expvar), `/metrics` (promhttp), `POST /agent/run`, `GET /threads/{id}/messages`; wraps in `withCORS`.
- `(*Server).handleHealthz` — liveness JSON `{ok:true,...}` (503 on HealthCheck error, sanitized).
- `(*Server).withCORS(next) http.Handler` — sets `Access-Control-Allow-Origin:*` + answers preflight OPTIONS when CORSPermissive; pass-through otherwise (restrictive default).
- `(*Server).handleRun` — parses RunAgentInput, resolves thread to 404 (UUID-parse before store), tries thread-lock (409), applies `Resume[]`, drives `Turn`, streams translated SSE (reasoning redacted on the web path).
- `(*Server).handleMessages` — resolves thread (404), returns persisted history as a `MESSAGES_SNAPSHOT` JSON body.
- `(*Server).streamSSE(ctx, w, stream)` — producer goroutine ranges stream into a cap-N channel (drop-on-full), handler drains to the SDK SSE writer; goleak-clean on disconnect.
- `(*Server).pumpSend(...)` — delivers one event; lifecycle frames block-until-fit, non-lifecycle deltas dropped-with-WARN under backpressure; returns false only on ctx-cancel.
- `isLifecycleFrame(t events.EventType) bool` — protocol-boundary frames that must not be dropped (RUN/TEXT/TOOL/REASONING starts+ends, CUSTOM, STATE_SNAPSHOT).
- `(*Server).bufferCap() int` — resolves SSE channel cap (falls back to `fanoutBuffer`).
- `resumeAnswers(entries []types.ResumeEntry) map[string]runner.ResponseInput` — maps protocol-native resume onto accept/cancel.
- `payloadString(any) string` — renders a resume payload as answer content (string verbatim, else JSON).
- `lastUserMessage(msgs) (*string, error)` — extracts the final user message to drive the turn; rejects structured multimodal content.
- `projectMessages(hist) []events.Message` / `projectToolCalls(calls)` — projects persisted llm.Message/ToolCall onto SDK shapes for the snapshot.
- `var secretPattern, urlUserinfoPattern, tokenPattern` — redaction regexes (DB DSN, generic URL userinfo, bearer/api-key/token).
- `sanitizeErr(err) string` / `SanitizeString(msg) string` — strip credential-bearing substrings before they hit the wire (exported; reused by `obs` and telegram renderer).
- `redactEvent(ev) events.Event` — sanitizes a RUN_ERROR message in-flight, passes other events through.

**translator.go**
- `const ArtifactEventName = "aura.artifact"` — stable AG-UI CUSTOM-event name for delivered files (exported; telegram artifact consumer keys on it).
- `const redactedReasoningDelta = "[reasoning redacted]"` — placeholder substituted for CoT when `showReasoning` is false.
- `Translate(threadID, runID string, idgen IDGenerator, seq iter.Seq2[*agent.Event,error], showReasoning bool) iter.Seq2[events.Event, error]` — pure state machine: RUN_STARTED → coalesced text/reasoning/tool/artifact/state events → RUN_FINISHED(success/interrupt/error). Closes open runs on any family switch.
- `type textRunState` + `.content/.close` — coalesces chunk Events into one TEXT_MESSAGE_START/CONTENT*/END lifecycle.
- `type reasoningRunState` + `.content/.close` — coalesces reasoning into REASONING_START/MESSAGE_START/CONTENT*/MESSAGE_END/END with a separate `rsn-` id; emits reasoningtrace records; redacts or passes the real delta per `showReasoning`.
- `emitToolInvocation(yield, idgen, ti)` — maps a ToolInvocation onto TOOL_CALL_START(+ARGS)/END(+RESULT).
- `toolResultCallID(ev) (string, bool)` — detects a tool-result preview via the `tool_call_id` StateDelta marker.
- `stateDeltaOps(d) []events.JSONPatchOperation` — builds JSON-patch ops over SORTED keys (deterministic).
- `interruptFrom(ai) types.Interrupt` / `responseSchema(options) map[string]any` — map an AwaitingInput pause onto an AG-UI Interrupt + JSON-Schema response.

**fanout.go**
- `const fanoutBuffer = 64` — per-subscriber channel cap.
- `type Fanout struct{ source; mu; subs; started }` — distributes one translated stream to N in-process subscribers (drop-on-full, never blocks the Loop).
- `NewFanout(source) *Fanout` — wrap a translated stream.
- `(*Fanout).Subscribe() <-chan events.Event` — register a consumer (cap-64); panics if called after Run.
- `(*Fanout).Run(ctx)` — single producer goroutine fans each event to all subscribers, closes all channels on ctx-cancel/source-end; panics on a second Run.
- `send(ctx, sub, ev, idx) bool` — per-subscriber send (lifecycle blocks-until-fit, non-lifecycle dropped+WARN); records reasoningtrace.
- `eventJSONString(ev)`, `closeAll(subs)` — JSON trace helper + sole-sender channel close.

**client.go**
- `type Event = events.Event`, `type EventType = events.EventType` — re-exported SDK aliases keeping the external package out of call sites.
- `Subscribe(ctx, threadID, runID, idgen, turn) <-chan Event` — single-consumer convenience: composes `Translate` + a single-subscriber `Fanout` (reasoning redacted).

**metrics.go**
- `var promSSEDroppedTotal` (Prometheus counter `aura_agui_sse_dropped_total`) + `recordSSEDropped()` — counts dropped slow-client SSE events.

**readiness.go**
- `const readinessProbeTimeout = 3s` — per-probe bound.
- `type ReadinessProbe struct{ Name string; Check func(ctx) error }` — one named dependency check injected by the composition root.
- `(*Server).handleReadyz` — runs every probe under a bounded ctx; 200 only when all required deps are reachable, else 503 with per-dep status (ready when no probes configured).

### `internal/channels` — daemon channels framework: a `Channel` lifecycle interface + fail-soft `Registry` that `bootServe` mounts
A narrow `Channel` contract (Telegram is the first real one) plus a `Registry` that aggregates start/stop fail-soft and an optional `Deliverer` capability for identity-routed pushes.

**channel.go**
- `type Channel interface { Name() string; Start(ctx) error; Stop(ctx) error; IsHealthy() bool }` — narrow per-channel lifecycle; `Start` takes NO fanout subscriber (fanout is built per-turn inside the channel).

**deliver.go**
- `type Deliverer interface { Deliver(ctx, identityID, text) (delivered bool, err error) }` — optional push capability; tri-state contract `(false,nil)=not-my-user / (true,nil)=delivered / (false,err)=owns-but-failed`.

**deps.go**
- Package dependency anchor: blank-imports `gopkg.in/telebot.v4` to keep the amendment-#58 CI pin gate green (no executable code).

**registry.go**
- `type Registry struct{ channels; enabledOverride; mu; started }` — holds channels, aggregates lifecycle fail-soft.
- `NewRegistry() *Registry`, `(*Registry).Register(c Channel)` — map-backed register (last wins).
- `(*Registry).SetEnabledOverride(predicate func(name)(enabled,ok bool))` — flag-driven override of the env enable gate (`--no-telegram`/`--only=cli`).
- `(*Registry).StartAll(ctx) error` — starts every enabled channel; logs + `errors.Join`s failures (one failure never aborts siblings/daemon).
- `(*Registry).StopAll(ctx) error` — stops only channels actually started; idempotent.
- `(*Registry).DeliverToIdentity(ctx, identityID, text) (bool, error)` — fans a push to the owning channel in sorted-by-name order, honoring the tri-state Deliverer contract.
- `(*Registry).enabled(name) bool` / `envChannelEnabled(name) bool` — resolve enablement (override wins, else `AURA_CHANNEL_<NAME>_ENABLED`, default true).

### `internal/channels/telegram` — the Telegram main user-facing channel (telebot.v4): bot lifecycle, inbound dispatch, AG-UI render consumers, HITL, multimodal sidecars, onboarding, status pane, DB store
The largest channel package (~30 source files). Builds a fresh AG-UI `Fanout` per turn, three render consumers (status pane msg#1, content renderer msg#2, artifact sendDocument), full HITL over the Runner pause backend, four 9c multimodal sidecar HTTP clients (STT/TTS/vision/markitdown), `/start` onboarding + Agent.md profile onboarding, and a Postgres store for accounts + setup-pending tokens.

**bot.go**
- `const channelName = "telegram"` + `var _ channels.Channel = (*Telegram)(nil)`.
- `type turnDriver func(ctx, convID, *userMsg) iter.Seq2[*agent.Event,error]` — per-turn loop seam (`runner.Runner.Turn`).
- `type documentIngestor interface { IngestPath(...) (*documents.Job, error) }` — optional native document-ingestion seam.
- `type botSender interface { Send(...); Edit(...) }` — narrow telebot surface render consumers call.
- `type Deps struct{ ... }` — constructor inputs: Turn, Token, Store, Profile, AnswerExtractor, Multimodal, DocumentIngest, Search/Cost/Clear/Prices/Model command backends, Resume (HITL), throttles, ShowReasoning/ReasoningFIFORunes, Offline, and unexported test seams.
- `type Telegram struct{ deps; bot; wg; cmds; onboard; profile; voice; photo; docs; tts; pausePrompts; ... }` — the channel; builds dispatch instances once at Start.
- `NewChannel(d Deps) *Telegram`, `(*Telegram).Name()`.
- throttle accessors `statusThrottle/contentThrottle/chatRateLimit/reasoningFIFORunes`, `msOrDefault`, defaults consts (1500/500/1000ms, 4096 runes).
- `(*Telegram).Start(ctx) error` — constructs telebot bot (Offline uses `stopWaitPoller`), builds dispatch, registers handlers + bot menu, launches the polling goroutine (wg-tracked).
- `(*Telegram).registerBotCommands` / `botMenuCommands() []tele.Command` — sets the 12 Telegram slash-command menu entries.
- `(*Telegram).Stop(ctx) error` — graceful poller shutdown + drains async document goroutines + joins wg; idempotent.
- `(*Telegram).IsHealthy() bool`, `(*Telegram).sender(c) botSender`.
- `type stopWaitPoller` — zero-network poller for Offline unit tests.

**bot_dispatch.go**
- `const photoMIME = "image/jpeg"` + user-facing dispatch copy consts.
- `(*Telegram).buildDispatch()` — constructs cmds/onboard/profile/voice/photo/docs/tts once from Deps.
- `(*Telegram).registerHandlers(daemonCtx, bot)` — wires OnText, OnVoice/OnPhoto/OnDocument, the HITL/search/status-cancel/profile inline-button callbacks, OnCallback fallback, OnReply.
- `(*Telegram).onText(daemonCtx)` — command/HITL intercept: `/start <token>` onboarding → linked-account auth → `/cancel`-pause/`/onboard` → command dispatch → HITL text → profile onboarding → ordinary turn.
- `(*Telegram).handleStartPayload(...)` / `startMsgFromMessage` — consume a deep-link `/start <token>` before generic dispatch.
- `(*Telegram).onReply` — routes a ForceReply answer to HITL.
- `(*Telegram).onCallback(daemonCtx)` — resolves an inline-button HITL press through the Runner, disarms keyboard, renders next FIFO pause.
- `callbackToast/callbackToastText`, `(*Telegram).disarmCallbackKeyboard`, `(*Telegram).onCallbackFallback` — toast text + keyboard clear + ack a non-HITL callback.
- `(*Telegram).onVoice/onPhoto/onDocument(daemonCtx)` — getFile → STT transcribe / vision describe / markitdown convert (size-tiered) → drive a turn (typing pulse throughout); hard-fail copy + 😵 on STT failure.
- `(*Telegram).ingestTelegramDocument(...)` / `writeTelegramDocumentTemp(...)` — native document-ingestion path (temp file + `IngestPath`).
- `(*Telegram).runTurn/startTurn(...)` — drive ONE turn off the handler goroutine under a cancellable ctx registered with the command dispatcher (busy → reply); wg-tracked.
- `(*Telegram).sendBusy/reply/replyProfile/replyCommand` — best-effort user-facing sends.

**config.go**
- `type Config struct{ BotToken; StatusThrottleMS; ContentThrottleMS; ChatRateLimitMS; ReasoningFIFORunes }` — the channel's own config (TELEGRAM_BOT_TOKEN upstream naming + Aura-native throttles).
- `LoadConfig() Config` — reads env with silent-fallback defaults; `envIntDefault` local helper.

**commands.go**
- `type searchBackend/costBackend/clearBackend interface` — consumer-side seams over the locked CLI backends (FTS search, today usage, conversation hard-delete).
- `type commandDeps`, `type commands struct{ deps; cancels; searchPages }` + `newCommands`.
- `(*commands).registerTurn/unregisterTurn(chatID, cancel)` — per-chat in-flight-turn cancel registry (returns false if a turn is already running — prevents concurrent turns on one chat).
- `(*commands).dispatch / dispatchRich(ctx, chatID, text)` — intercept every slash-command BEFORE the LLM; handles `/start /help /cancel /cost /search /onboard /new /list /reset /clear /whoami /stop` + an unknown-command hint.
- `helpText` const, `(*commands).cancel/fireCancel/clear/cost/searchRich` — command bodies (`/cost` reuses `llm.CostUSD`; `/clear` hard-deletes via the backend).
- search pagination: `searchPage/renderSearchPage/paginateSearchLines/searchMarkup/searchCallbackData/searchCallbackCloseData/parseSearchCallback`.
- `splitCommand(s) (name, arg)` — split `/name@bot args`.
- `searchExcerpt/clampExcerpt` — verbatim port of the CLI excerpt window (cross-slice byte-identical `/search`).

**renderer.go**
- `const telegramTextCap=4096, telegramCaptionCap=1024, runErrorPrefix, canParseEntitiesMarker`.
- `type renderer struct{ bot; to; throttle; chatRate; buf; msg; plain; tableSent; textDone; ... }` — streams msg#2 (the answer) as Telegram HTML with plain-text fallback, markdown-table→PNG, throttle + per-chat rate limit, 4096 cap.
- `type botDeleter interface{ Delete(...) }` — optional delete of a superseded streamed message.
- `newRenderer(...)`, `(*renderer).consume(ctx, ch)` — drains TEXT_MESSAGE_*/RUN_FINISHED/RUN_ERROR.
- `(*renderer).finalText()` — accumulated answer (read for TTS-out).
- `(*renderer).flush/sendError/rateLimit/send/sendText/sendTextChunk/deliver/sendTable/deleteStreamed` — flush-on-throttle, sanitized RUN_ERROR notice, table-PNG-once-on-final, HTML→plain fallback, long-answer split.
- `tableCaption`, `stripProtocolToolBlocks` + `nextProtocolToolOpen/Close/firstIndexFrom/suppressPartialProtocolOpen` — strip leaked `<tool_call>`/`<tool_exec>` scaffolding from user-visible text.
- `sendOpts`, `isCantParseEntities(err)` (structured `*tele.Error` 400 match), `capRunes`, `splitTelegramText/splitBoundary` — send helpers.

**status_pane.go**
- glyph consts (🟡✅❌💭) + `statusCancelUnique/Data`, `hitlPauseToolName="ask_user"`, `redactedReasoningSentinel`.
- `type toolState`, `type statusPane struct{ bot; to; throttle; tools; byID; hidden; thinking; cost; showReasoning; fifo; started; ... }` — msg#1 edited-in-place lifecycle indicator (tool list, 💭 reasoning, cost footer), coalesced to the status throttle.
- `newStatusPane(...)`, `(*statusPane).consume/handle` — folds RUN_STARTED/TOOL_CALL_*/REASONING_*/STATE_DELTA/RUN_FINISHED/RUN_ERROR.
- `(*statusPane).startTool/finishTool/applyCost/render` — tool spinner→✅/❌, cost footer, coalesced edit.
- `text/reasoningSection/reasoningContent/elapsedText/baseText/costText/markup` — pane rendering with live-CoT FIFO window (tail-keep) gated by `showReasoning`, the "Annulla" inline button while in-flight.
- helpers `looksLikeToolError`, `safeReasoningState`, `collapseWhitespace`, `displayableReasoningDelta`, `capRunesTail`, `runeLen`.

**agui_subscriber.go** (per-turn AG-UI fanout consumer — the Phase-12 seam, MUST consume `internal/agui`)
- `type eventConsumer interface{ consume(ctx, ch) }`, `type consumerFactory func(bot, to)(status,content,artifact eventConsumer)`, `type finalTexter interface{ finalText() string }`.
- `(*Telegram).handleTurn(ctx, bot, chatID, *userMsg, inboundWasVoice)` — builds `Translate→NewFanout`, Subscribes status/content/artifact BEFORE `Run`, drains all three (renderer inline, pane + artifact on goroutines), then renders any pending pause + optional TTS-out. `userMsg==nil` drives a continuation (resume) turn.
- `(*Telegram).speakIfNeeded(...)` — synthesizes the final answer to a voice note when `ShouldSpeak` (after text render, never blocks it, ctx-aware).
- `(*Telegram).consumers(...)` — builds the three production consumers (or injected factory).
- `convID(chatID) string` + `var telegramChatNamespace` — deterministic chat→conversation UUIDv5.

**hitl.go** (render-only HITL over the Runner pause backend; channel never writes paused_states)
- `type resumeRunner interface{ PendingFor(...); SubmitAnswer(...) }`, `type resumeFunc`, consts `callbackUnique="aura_hitl"`, `callbackSep`, `callbackDataMaxBytes=64`.
- `type hitl struct{ runner; resume }`, `type callbackOutcome`, `newHitl`.
- `(*hitl).prompt(...)` — renders the first FIFO pause as approval/choice InlineKeyboard or clarification ForceReply; returns the sent message for later disarm.
- `(*hitl).send/handleCallback/handleCallbackResult/resolveChoiceValue/handleTextReply/submit/cancel` — parse token|action|value, submit via `SubmitAnswer` (accept/decline/cancel), drive a resume Turn only when remaining==0 and not a cancel; choice index→value resolution (64-byte callback_data guard).
- `approvalMarkup/choiceMarkup`, `type pauseOption`, `decodeOptions`, `callbackData`, `parseCallback` — inline keyboards + callback payload codec.

**bot_dispatch_hitl.go**
- `(*Telegram).hitlHandlesText/hitlHandlesReply(...)` — route a free-text/ForceReply answer to HITL when a pause is pending (notify on submit error, render next FIFO pause).
- `(*Telegram).cancelPendingPause(...)` (const `pauseCancelledMsg`) — `/cancel` resolves a pending ask_user pause (mirrors the "Annulla" button), disarms the tracked prompt.
- `notifyHitlSubmitError` (const `hitlSubmitErrorMsg`), `markHitlReplyHandled/takeHitlReplyHandled` — submit-error notice + reply-dedup tracking.
- `(*Telegram).promptPendingPause/trackPausePrompt/takePausePrompt` — render/track the FIFO pause prompt message.
- `(*Telegram).hitlFor(c, chatID) *hitl` — builds a HITL surface whose resume drives a continuation turn through this chat's fanout.

**bot_dispatch_auth.go**
- consts `activationRequiredMsg/Toast`; `type telegramSeenMarker interface`.
- `(*Telegram).profileForDispatch/accountsForDispatch` — resolve the profile-onboarding handler + account resolver (test override → Store).
- `telegramUserIDFromMessage/Callback` — extract the sender id.
- `(*Telegram).requireLinkedMessage/requireLinkedCallback/telegramUserIsLinked` — fail-closed inbound auth: only senders with a linked `telegram_accounts` row proceed (touches last_seen).

**bot_dispatch_callbacks.go**
- `(*Telegram).onStatusCancelCallback` — the status-pane "Annulla" button → cancel the in-flight turn + disarm.
- `(*Telegram).onSearchCallback` — `/search` pager prev/next/close inline buttons.
- `(*Telegram).onProfileCallback(daemonCtx)` — profile-onboarding confirm/edit/skip inline buttons.

**bot_dispatch_file.go**
- `downloadFile(filer botFiler, file *tele.File) ([]byte, error)` — pulls media bytes off the Telegram file server (bounded by the 20MB getFile ceiling).

**deliver.go**
- `var _ channels.Deliverer = (*Telegram)(nil)`, `type accountResolver interface{ GetAccountByIdentity(...) }`.
- `(*Telegram).Deliver(ctx, identityID, text) (bool, error)` — pushes to the 1:1 chat owned by an identity (Phase 20 R3), honoring the tri-state contract (no account/`local`/no-bot → not-my-user).
- `(*Telegram).deliverSender/accountResolver` — live bot/Store with test overrides.

**store.go** (DB seam over `aura.telegram_accounts` + `aura.telegram_setup_pending`)
- `var ErrTokenConsumed, ErrTokenNotFound, ErrAccountExists` — sentinel errors (SQLSTATE-classified, never message match).
- `type Store struct{ pool; q }` + `New(pool)`.
- `type Account, InsertPendingParams, ConsumeParams`.
- `(*Store).InsertPending` — mint an onboarding token (1h TTL, FK'd to identity).
- `(*Store).ConsumeOnboarding(ctx, p) (Account, error)` — single-use credential chokepoint: one `db.WithTx` marks consumed_at + INSERTs the account (23505→ErrAccountExists; spent/expired→ErrTokenConsumed; rolls back atomically).
- `(*Store).CleanupExpired`, `PendingConsumed`, `GetAccountByTelegramID`, `GetAccountByIdentity`, `TouchLastSeen`, `CountAccounts`, `ListAccounts` — GC, SSE-poll signal, account lookups (by telegram-id / identity), activity bump, status footer, listing.
- `accountFromRow/textOrNull/parseUUID/isUniqueViolation` — boundary + classification helpers.

**onboarding.go**
- `type onboardingStore interface{ ConsumeOnboarding(...) }`, `type startMsg`, `type onboarding struct{ store }` + `newOnboarding`.
- `(*onboarding).handleStart(ctx, m) (reply string, onboarded bool)` — resolves `/start`: bare → greeting; token → `ConsumeOnboarding`; renders consumed/expired/already-linked/success copies (Italian).
- `parseStartPayload(text) string` — extract the deep-link token.

**profile_onboarding.go** (Agent.md profile onboarding interview driven by `internal/onboarding` flow)
- consts `profileCallbackUnique/Prefix`, actions confirm/edit/skip.
- `type profileAccountResolver interface{ GetAccountByTelegramID(...) }`, `type profileOnboarding struct{ store *profile.Store; accounts; extractor; sessions }`, `type profileSession`, `type profileReply`.
- `newProfileOnboarding`, `maybeStart/restart` — start the interview when no profile exists / on `/onboard`.
- `handleText/handleCallback` — feed free-text answers (LLM `AnswerExtractor` with keyword fallback) into the flow; confirm writes the drafted Agent.md, skip writes a skipped marker, edit queues changes.
- `startLocked/deleteSession/runOne` — session lifecycle; `runOne` drives the `profileflow.NewLoop` through a 4-step `agent.Budget`.
- `writeCompleted/writeSkipped` — persist via `profile.Store.WriteProfile`.
- `replyFromEvent` — maps each onboarding step (Identity/Work/Projects/Social/Style/Draft) to an Italian prompt; renders the draft with confirm/edit/skip markup.
- `profileDraftMarkup/profileCallbackData/parseProfileCallbackData/identityLabel/answersFromText` — markup, callback codec, keyword answer parser.

**sidecar.go** (shared 9c multimodal config + HTTP client)
- `type MultimodalConfig struct{ VisionCloud; Model; MultimodalBaseURL/Model; FallbackModel; OpenRouterBaseURL/APIKey; STT*; TTS*; DocumentsBaseURL; RetryBackoff; TimeoutSec }` — telegram-package projection of central config; zero value = modality unconfigured.
- consts `defaultSidecarTimeoutSec=30`, `connectTimeoutSec=10`.
- `newSidecarHTTPClient()` — connect-timeout dialer, no client timeout (rides ctx), DisableKeepAlives (goleak).
- `(MultimodalConfig).requestTimeout/withTimeout` — per-request ctx deadline.
- `type sidecarStatusError struct{ endpoint; statusCode }` + `.Error()` — typed non-2xx error (no body, no secret leak).

**voice.go** (STT sidecar client — OGG/Opus → aura-stt, no ffmpeg)
- consts `transcribeFailMessage`, `hardFailReaction="😵"`, `var defaultRetryBackoffMS={1000,2000}`.
- `type botFiler interface{ File(...) }`, `type botReactor interface{ React(...) }`.
- `type voiceClient struct{ cfg; httpClient; backoff }` + `newVoiceClient`.
- `(*voiceClient).Transcribe(ctx, bot, _, voice) (string, error)` — download OGG, POST multipart to `/audio/transcriptions`, retry on transient failure.
- `(*voiceClient).HardFail` — apply the 😵 reaction; `download/postTranscription/sleep` — bytes + multipart POST (pins `language`) + ctx-aware backoff.

**photo.go** (vision sidecar client — single `AURA_VISION_CLOUD` branch, #60)
- `type photoClient struct{ cfg; httpClient }` + `newPhotoClient`.
- `type chatRequest/chatMessage/contentPart/imageURL/chatResponse` — minimal OpenAI image_url chat shapes.
- `(*photoClient).Describe(ctx, image, mimeType, caption) (string, error)` — downscale oversized photos, build base64 data-URL, POST to `/chat/completions`, return description/OCR.
- `(*photoClient).route() (baseURL, apiKey, model)` — the single config branch: local aura-ocr-vl (no key) vs OpenRouter (primary model when `llm.SupportsVision`, else fallback minimax-m3, so an image never hits a non-vision model).

**photo_resize.go**
- consts `visionMaxEdge=1024`, `visionJPEGQuality=85`.
- `downscaleForVision(raw) (out []byte, mime string)` — shrink an oversized image (CatmullRom) so the CPU vision sidecar stays fast; returns `("",original)` when already small or on any error (never errors).

**tts.go** (TTS sidecar client — reply text → aura-tts/Kokoro voice note)
- `ShouldSpeak(voiceMode, inboundWasVoice bool) bool` — speak when voice-mode pref OR the inbound was a voice note.
- `VoiceModePref(_) bool` — stub returning false (Slice 10 prefs not yet shipped).
- `type ttsClient struct{ cfg; httpClient }` + `newTTSClient`, `type ttsRequest`.
- `(*ttsClient).Speak(ctx, bot, to, text) (*tele.Message, error)` — sanitize speech, synthesize opus, sendVoice with an ASCII-safe caption (skips emoji-only replies).
- `(*ttsClient).synthesize` — POST to `/audio/speech` (response_format=opus).

**tts_sanitize.go**
- `var ttsCodeFence/ttsInlineCode/ttsMdLink/ttsHeading/ttsBlockquote/ttsListBullet/ttsEmphasis/ttsMultiSpace/ttsMultiNewline` — markdown-unwrap regexes.
- `sanitizeForSpeech(s) string` — strip markup + emoji/symbol runes (+ZWJ/variation selectors) so Kokoro voices clean prose; "" when nothing speakable.
- `stripNonSpeechRunes(s) string` — drop So/Sk category + emoji-glue runes.

**documents.go** (markitdown document-conversion sidecar, size-tiered)
- consts `asyncTierMinBytes=5MiB`, `refuseTierMinBytes=50MiB`, `documentRefuseMessage`.
- `type ConvertStatus` (`ConvertSync/Async/Refused`), `type ConvertResult`, `type documentAsyncCallback`.
- `type documentsClient struct{ cfg; httpClient; wg; stopOnce }` + `newDocumentsClient`.
- `(*documentsClient).Convert(ctx, payload, fileName, onAsync) (ConvertResult, error)` — ≤5MB sync, 5–50MB async on a wg-tracked goroutine (result via callback), >50MB refused.
- `(*documentsClient).Stop` — drains async goroutines (goleak, idempotent); `postConvert` — multipart POST to `/convert`, decode markdown.

**bot_typing.go**
- const `typingPulse=4s`, `type botNotifier interface{ Notify(...) }`.
- `keepWorking(ctx, c, action) (stop func())` / `pulseChatAction(ctx, n, to, action) (stop func())` — persistent "Aura sta scrivendo…" chat action refreshed under Telegram's ~5s expiry; goleak-safe stop.

**html.go**
- `RenderTelegramHTML(s) string` — convert model Markdown to Telegram-safe HTML via `gotg_md2html.MD2HTMLV2` (escapes raw HTML first).
- `normalizeTelegramBlockMarkdown/normalizeMarkdownHeading/stripClosingHeadingHashes/isMarkdownThematicBreak` — pre-normalize headings (→ bold) and thematic breaks.

**mdv2.go** (entity-aware MarkdownV2 escaper, locked in-tree by amendment #4)
- const `reservedOutsideEntity`.
- `EscapeMarkdownV2(s) string` — fence-aware escape of reserved chars OUTSIDE entities only (full set outside; only backtick/backslash inside code/pre); deterministically closes unterminated spans so the stream can't 400.
- `PlainTextFallback(original) string` — identity function documenting the renderer's "resend original, no ParseMode" fallback contract.

**tables.go** (markdown table → gridded PNG, deterministic, embedded fonts)
- `var errEmptyTable`, render consts (28px font, 72 DPI, padding, grid).
- `ParseMarkdownTable(s) (grid [][]string, ok bool)` — detect header+separator+data run, ignore surrounding prose.
- `splitRow/isSeparatorRow/normalizeColumns` — cell parsing + rectangularization.
- `RenderTablePNG(grid) ([]byte, error)` — pure transform to a gomono/gomonobold gridded PNG (byte-identical for the same grid; no CGO/fontconfig).
- `newFace/measure/fillRect` — font + draw helpers.
- `PreBlockTable(grid) string` — zero-dependency monospace ``` fenced fallback; `padRunes`.

**artifact.go** (artifact consumer — AG-UI CUSTOM event → sendDocument)
- `type artifact struct{ bot; to }` + `newArtifact` (implements `eventConsumer`).
- `(*artifact).consume/consumeEvent` — render every `agui.ArtifactEventName` CUSTOM event to a `sendDocument` (FromDisk path, ASCII-safe caption); ignore the rest.
- `artifactDescriptor(ev) (map[string]any, bool)`, `stringField` — extract the `{path,filename,caption}` descriptor.
- `asciiCaption(caption) string` / `foldLatinToASCII(r) byte` — fold accented Latin to ASCII (Pitfall 4: a non-ASCII caption byte 400s the Bot API).

### `internal/askuser` — per-domain `Store` over `aura.paused_states` (HITL pause persistence, PRD 1.5)
Copies the canonical sqlc Store pattern (SQLSTATE classification, sentinel errors, pgtype-at-boundary, `db.WithTx`). Owns FIFO pending listing + crash-recovery resume/batch + Loop.Stop auto-resolve; the Runner owns resume orchestration.

**store.go**
- action consts `ActionAccept/Decline/Cancel`, `autoTerminatedContent` marker.
- `var ErrPauseNotFound, ErrInvalidAnswer` — sentinels.
- `type Store struct{ pool; q }` + `New(pool)`.
- `type Pending` (pending-row projection), `type ResumeAnswer{Action,Content}` (resumed_answer jsonb), `type InsertParams` (plain fields incl. proxied swarm-relay ids), `type Record` (richer CLI projection with resolution state).
- `(*Store).Insert(ctx, p)` — persist one pending pause (jsonb options/context, proxied_* columns NULL for direct calls; child id stored verbatim text, not UUID).
- `(*Store).GetByToken` — fetch by token (missing→ErrPauseNotFound).
- `(*Store).ListPending(ctx, conversationID)` — still-pending pauses in total FIFO order (priority DESC, created_at ASC, token ASC).
- `(*Store).ListRecent(ctx, limit)` — most-recent rows (pending+resolved) for the CLI.
- `(*Store).MarkResumed(ctx, token, ans)` — resolve one (zero-rows→ErrPauseNotFound via `pool.Exec` RowsAffected) using `markResumedSQL`.
- `(*Store).MarkResumedBatch(ctx, answers)` — resolve many atomically (whole batch rolls back if any token is unknown/already-resumed).
- `(*Store).AutoResolveForConversation(ctx, conversationID)` — Loop.Stop helper: resolve all open pendings with the auto-terminated marker (one `db.WithTx`).
- `(*Store).CleanupResumedOlderThan(ctx, cutoff)` — GC resolved rows (the `paused-states purge` CLI).
- `fromRow/encodeAnswer/decodeResumedAnswer/parseUUID` — projection + answer codec (validates action) + UUID boundary.

### `internal/setup` — setup-wizard backend (loopback :9081 HTTP, Slice 9a, UX-03)
An isolated loopback HTTP server (distinct from the AG-UI :9080) that mints the single-use onboarding token Telegram's `/start` consumes and surfaces onboarding completion over SSE. Every route is gated by a one-time in-memory token; the bot token supplied to `/setup/token` is validated via telebot getMe and never logged. The HTML frontend is deferred (qr_svg stays empty; the live path is a terminal ASCII QR).

**types.go**
- `type TokenRequest{Token}`, `type TokenResponse{OK,BotUsername}`, `type OnboardLinkResponse{DeepLink,QRSVG}`, `type StatusResponse{BotConfigured,AccountCount,LastActivity}`, `type SetupEvent{Type,TelegramUserID,Username}` — the wire contracts.
- const `onboardingCompletedEvent` — the only SSE event type emitted.

**token.go**
- `type Token struct{ mu; value; valid }` — in-memory one-time setup credential.
- `NewToken(configured string, out io.Writer) *Token` — use operator value or generate a UUIDv4 and print `AURA_SETUP_TOKEN=<value>` once.
- `(*Token).Valid(presented) bool` — constant-time compare (timing-oracle safe); `(*Token).Invalidate()` — burn after onboarding (idempotent).

**qr.go**
- `qrSVG(deepLink) string` — deferred SVG body (returns "" — forward-compat; live QR is the terminal ASCII one).

**handlers.go**
- const `onboardingTTL=1h`.
- `(*Server).handleToken` (POST /setup/token) — validate bot token via BotProbe (getMe), record configured + username; never logs/echoes the token, non-leaky 400 on failure.
- `(*Server).handleOnboardLink` (POST /setup/onboard-link) — mint a single-use UUID, `InsertPending` (1h TTL), return `{deep_link, qr_svg:""}`, print a terminal ASCII QR (qrterminal); 409 if no bot configured.
- `(*Server).handleStatus` (GET /setup/status) — bot-configured + `CountAccounts` + server-clock last_activity.
- `(*Server).handleEvents` (GET /setup/events) — SSE completion stream: polls `consumed_at` every pollInterval; on consume burns the setup token then emits `onboarding_completed`; goleak-clean on ctx-cancel.
- `(*Server).pollCompletion`, `deepLink`, `writeJSON`, `writeSSE` — poll helper, t.me deep-link builder, JSON/SSE writers.

**server.go**
- consts `setupReadHeaderTimeout=10s`, `setupHeaderName="X-Aura-Setup-Token"`, `defaultPollInterval=2s`.
- `type Store interface{ InsertPending; PendingConsumed; CountAccounts }` — narrow DB seam (`*telegram.Store` satisfies it via a composition-root adapter), `type InsertPendingParams`.
- `type BotProbe func(ctx, token) (username, error)` — telebot getMe seam (must not log the token).
- `type Deps struct{ Store; Probe; Bind; Token; IdentityID; TokenOut; QROut; PollInterval }`, `type Server struct{ store; probe; token; identityID; pollInterval; qrOut; botUsername; botConfigured }`.
- `NewServer(deps) *Server` — defaults TokenOut/QROut to os.Stdout, PollInterval to 2s.
- `(*Server).Mux() http.Handler` — registers the 4 routes (+ `/setup` redirect), each wrapped by `requireSetupToken`.
- `(*Server).handleRoot` — redirect `/setup` → `/setup/status` (carrying ?token=).
- `(*Server).HTTPServer(bind) *http.Server` — loopback server with a slow-loris ReadHeaderTimeout.
- `(*Server).InvalidateToken()` — burn the one-time token.
- `(*Server).requireSetupToken(next) http.Handler` — mandatory gate on every route (`?token=` query or `X-Aura-Setup-Token` header, constant-time compare; 401 after invalidation).

### `internal/config` — thin root composite `Config` read by every cmd/aura subcommand
Per-subsystem configs (db/knowledge/llm/mcp) live in their owning packages; this only wires top-level fields, composes Postgres DSNs from `POSTGRES_*` primitives, and resolves ~60 `AURA_*`/upstream env knobs with silent-fallback (a typo falls back, never boots fatal — except required secrets and the LLM key).

**config.go**
- consts `defaultOtelExporter="otlp"`, `defaultOtelEndpoint="localhost:4317"`.
- `type Config struct{ DB; Neo4j; LLM; MCPServers; MCPPolicies; MCPServersErr; RunDir; ToolPreviewCap; Otel*; Conversation/Context/History tuning; Web* (search/fetch/cache/timeouts/UA); Swarm* (goals/concurrency/timeout); AgentJobMaxDurationSec; SchedulerPreferOriginChannel; Skill* (dir/caps/export/TTL/blocklist); AGUI* (bind/cors/buffer); ServeShutdownGraceSec; Setup* (bind/token); VisionCloud; Multimodal/STT/TTS/Documents sidecar knobs; Profile* }` — the whole root composite (each field documents its env var + decision id).
- `Load() (*Config, error)` — `loadBase` + `llm.Load` (LLM owns its 4-tier load order + fail-fast empty key).
- `LoadServe() (*Config, error)` — full daemon config but permits an empty LLM key (setup wizard/channels boot; turns fail closed at call time).
- `(*Config).Validate() error` — fail fast on empty required infra secrets (composed DB DSN / NEO4J_PASSWORD) with a named cause.
- `LoadDB() *Config` — non-LLM config only (DB-admin commands skip the LLM key).
- `loadBase() *Config` — godotenv best-effort + composes app/migrate/bootstrap DSNs from `POSTGRES_*` + role names, reads every non-LLM knob, loads MCP servers.
- `composeDSN(...)` — returns "" when password empty so callers fail-fast; URL-encodes the DSN.
- `loadMCPServers() (servers, policies, err)` — load managed config + runtime/runnable servers + env-override servers; `injectDefaultOnMemory` (in config_mcp.go) mounts the default-on memory recipe.
- `parseMCPServersJSON/validateMCPServers` — parse `AURA_MCP_SERVERS_JSON` (wrapped or direct, non-empty command).
- `envDefault/envIntDefault/envBoolDefault/envSliceDefault` — silent-fallback env readers.
- `defaultSkillInjectionBlocklist() []string` — built-in prompt-injection control-token blocklist across model families.
- `defaultRunDir/auraHomeDir/defaultSkillsDir/defaultSkillExportDir` — per-user path defaults.

**config_mcp.go**
- const `memoryRecipeName="memory"`.
- `injectDefaultOnMemory(policies, managed, envOverridden)` — adds the `memory` recipe out of the box (D-08) unless the operator overrode it via env, has any explicit `memory` entry (enabled/disabled/blocked/profile-excluded), or the catalog lacks it (D-09 respect-disable).

### `internal/obs` — process observability bootstrap: structured slog (redacted), OTel tracer provider, rate-limited OTel error handler
**init.go**
- consts `defaultService="aura"/Version="dev"/Exporter="otlp"/Endpoint="localhost:4317"`.
- `type Config struct{ Service; Version; OtelExporter; OtelEndpoint; LogWriter }` — process-level knobs (LogWriter injectable for tests).
- `type ShutdownFunc func(ctx) error` + `(ShutdownFunc).Shutdown(ctx)` — adapts Init's shutdown to `agent.TracerProvider`.
- `Init(ctx, cfg) (func(ctx) error, error)` — installs a JSON slog logger with redacting `ReplaceAttr` + service/version, installs the OTel error handler, wires the global tracer provider via `agent.NewTracerProvider`, returns its Shutdown.
- `redactAttr(groups, a) slog.Attr` — runs every string log attr through `agui.SanitizeString` so DSNs/tokens never leak to the sink.

**otel_error_handler.go**
- const `defaultOTelErrorWindow=1m`.
- `type rateLimitedOTelHandler struct{ logger; window; now; lastLog; suppressed; started }` — process-global `otel.ErrorHandler`.
- `newRateLimitedOTelHandler(logger, window, now)` — nil-safe defaults (logger→slog.Default at log time, now→time.Now, window→1m).
- `(*rateLimitedOTelHandler).Handle(err)` — logs the first error per window, increments a suppressed counter within the window, reports the suppressed count on the next emission (a broken exporter can't flood logs).
- `installOTelErrorHandler(logger)` — wires it as the global handler (covers every exporter mode).

### `cmd/aura` — the single `package main` CLI binary that is the user-facing entry point and composition root for every Aura surface
The top-level subcommand router lives in `main.go`; each `aura <command>` dispatches into a hand-rolled `run<Command>` switch tree in its own file (no cobra — repo convention). The package holds the composition roots that wire the live runtime (`bootChatEnv` for chat/shell/serve, `bootServe`), the operator/CI/maintenance smoke commands, and adapter types that bridge the cron/tools/setup/channels seams onto live Postgres + MCP + the agent registry.

**CLI commands**
- `aura version` — print build metadata (version/commit/date/go runtime)
- `aura tools` — print the tool manifest (active + deferred) including MCP-mounted tools
- `aura db migrate|ping|status|reset` — Postgres admin (reset gated by `--yes` + `AURA_RESET_YES=1`)
- `aura neo4j migrate|ping|status|reset|cypher {read|write} "QUERY" [--param k=v ...]` — Neo4j graph admin + raw Cypher escape hatch
- `aura identity list|get <name>|grant <name> <cap>|revoke <name> <cap>` — identity + capability_grants admin
- `aura paused-states list|purge --before <ISO8601> --confirm` — ask_user pause ledger inspect + purge
- `aura cache-stats --since=<dur>` — windowed `aura.cache_metrics` hit-rate table
- `aura cache-audit` — (hidden) KV-prefix invariant gate replaying 20 fixtures through the real runner
- `aura web doctor|tool <web_search|web_fetch> '<json>'` — SearXNG/web health + operator tool smoke
- `aura mcp recipes|install|add|profile {list|create|use|add|remove}|trust|status|logs|list|doctor|tools|enable|disable|remove` — MCP server lifecycle/governance
- `aura memory <verb>` — operator path into the agent-memory MCP sidecar (search/context/sessions/conversation/add-entity/add-fact/add-preference/store-message/get-entity/relationship/export/trace {start|step|complete|observations}/query)
- `aura swarm-demo [--max-steps N] [goals...]` — deterministic LLM-free swarm fan-out proof
- `aura task schedule|list|cancel|run_now|approve|runs|doctor` — scheduler triad CLI + approval gate + audit/health
- `aura skills list|info|create|update|delete|approve|always {on|off}|snippet {save|exec}|audit [purge]` — skills governance + executable snippets
- `aura shell` — primary interactive agent terminal (full host shell_exec + fs_* tools)
- `aura toolpipe` — (hidden) NON-LLM NDJSON tool-latency harness reading calls from stdin
- `aura config show|get <key>|set <key> <value>` — effective LLM config view + persist file-tier keys (api_key redacted/unsettable)
- `aura agent dry-run [--request-id][--max-steps][--max-wallclock-sec][--dedup-window]` — mock LoopAgent budget-termination proof, one Event per JSON line
- `aura profile show [--identity][--json]|add-fact [--identity] <fact>` — Agent.md profile inspect + fact append
- `aura docs ingest|search|status|list|bench` — document ingestion + retrieval + bench (JSON)
- `aura chat list|new|resume [<id>]|archive <id>|unarchive <id>|delete <id> --confirm|rename <id> <title>|search <query>` — persisted multi-thread conversation REPL (bare `aura chat` = new)
- `aura doctor` — full-stack dependency health checks (postgres/neo4j/embed/mcp binary/llm key) with sysexits codes
- `aura serve [--no-telegram | --only=cli]` — long-lived daemon: scheduler tick loop + AG-UI gateway + channels + setup wizard

**main.go**
- `main()` — godotenv load + the top-level `os.Args[1]` switch dispatching every subcommand; `usage()` on miss.
- `buildRegistry() *tools.Registry` / `buildBaseRegistry(cfg, ts)` / `buildBaseRegistryWithHandles(cfg, ts) (*tools.Registry, runtimeToolHandles)` — shared composition root registering the full built-in toolbelt (text_response, tool_search, read_tool_output, current_time, ask_user, task, todo, skill, web_search/web_fetch, document_search, shell_exec/poll/kill, fs_read/write/edit/grep/glob, send_file, swarm_spawn) and fail-closing via `reg.Validate()`.
- `type runtimeToolHandles{ BackgroundShells; ShellApprovals }`.
- `buildRegistryWithMCP(ctx, cfg, ts) (...)` — mounts managed/legacy MCP servers fail-soft (one dead server WARN-drops, boot continues).
- `closeMCPServers(closers) error`, `printTools()` — reverse-order MCP teardown; render the manifest text.

**version.go**
- `var version, commit, date` (ldflags-stamped); `runVersion()` / `versionString() string` / `buildInfo() (v,c,d)` / `orUnknown(s)` — render the version block preferring ldflags then `debug.ReadBuildInfo` VCS.

**db.go**
- `runDB(args)` — dispatch `db {migrate|ping|status|reset}` (LLM-free `config.LoadDB()`).
- `dbMigrate` (EnsureRoles + Migrate), `dbPing` (latency), `dbStatus` (version/dirty table), `dbReset(ctx,cfg,extra)` (destructive, double-gated).

**neo4j.go**
- `runNeo4j(args)` / `neo4jUsage()` — dispatch `neo4j {migrate|ping|status|reset|cypher}`.
- `openMCP(ctx,cfg) *knowledge.Client` / `openSchema(ctx,cfg) *knowledge.SchemaExecutor` — spawn mcp-neo4j-cypher / dial the driver for DDL.
- `neo4jMigrate/Ping/Status/Reset` + `neo4jCypher(ctx,cfg,args)` (raw read/write with `--param`, prints raw JSON), `parseParams/coerceParam` — param codec.

**identity.go**
- const `identityUsage`; `runIdentity(args)` — open aura_app pool + `identity.New`, dispatch `{list|get|grant|revoke}`.
- `identityList/Get/Grant/Revoke`, `identityNameCap(args)` — list/get table, grant/revoke by name.

**paused_states.go**
- const `pausedStatesUsage`; `runPausedStates(args)` — open pool + `askuser.New`, dispatch `{list|purge}`.
- `pausedStatesList` (recent 50 table), `pausedStatesPurge(args)` (`--before` + `--confirm` GC), `flagValue/truncate` helpers.

**cache.go**
- `runCacheStats(args)` — thin `os.Exit(cacheStatsMain(...))` wrapper.

**cache_stats.go**
- const `cacheStatsUsage`; `cacheStatsMain(ctx,args,out,errOut) int` — parse `--since`, open pool, window read, write per-turn + TOTAL table.
- `parseSince` (exit 64 on bad input before DB work), `writeCacheStats`, `sinceValue`, `hitRate(cached,prompt)` (div-by-zero "n/a" guard).

**web.go**
- `runWeb(args)` — dispatch `web {doctor|tool}` with sysexits codes.
- `runWebDoctor()` (SEARXNG_URL set/reachable/live-probe → 64/70/0), `runWebTool/runWebToolSearch/runWebToolFetch` (same `web.Client` the agent uses), `printWebError(tool,err)` (sanitized `*web.WebError` → exit 0; genuine fault → 71).

**exit_codes.go**
- consts `exitUnreachable=70, exitInfra=71, exitUsage=64` — shared sysexits-style codes.

**mcp.go**
- consts `mcpUsage`, `defaultWhatsAppBridgeURL`; `runMCP(args)` / `runMCPCommand(ctx,args,out)` — dispatch all `mcp` subverbs.
- `mcpRecipes` (catalog), `mcpInstall` (recipe → managed config + active profile), `mcpAdd` (`--env`/`--disabled`/`--trust` + `-- cmd`), `mcpList`, `mcpDoctor(ctx,args,out)` (start one/`--all` + list tools + whatsapp special-case), `mcpSetEnabled`, `mcpRemove`.
- `loadManagedMCPConfig()`, WhatsApp-bridge probes (`writeWhatsAppBridgeHealth/probeWhatsAppBridge*`, `var runWhatsAppBridgeWSLProbe`, `wslHTTPProbeScript`, `isWSLCommand/wslProbePrefixArgs/parseHTTPStatusLine`), `sortedManagedNames/renderMCPCommand/mcpBoolPtr/splitCommandParts/writef/writeln` — formatting + WSL probe helpers.

**mcp_profile.go**
- `mcpProfile(args,out)` — dispatch `mcp profile {list|create|use|add|remove}`.
- `mcpProfileList/Create/Use/Add/Remove`, `mcpTrust(args,out)` (mark `TrustTrustedLocal`), `ensureProfileMembership(doc,profile,server)`.

**mcp_tools.go**
- `mcpTools(ctx,args,out)` — open one server (managed/legacy) + list tools; `writeMCPTools`.
- `effectiveManagedMCPServer/effectiveMCPServer/openAndListMCPTools/openAndListManagedMCPTools` — resolve + open (HTTP or stdio runtime launch) + ListTools; `firstMCPDescriptionLine/toolCount`.

**mcp_status.go**
- `var mcpLookPath = exec.LookPath`; `mcpStatus(args,out)` (`SnapshotStatus` table/JSON), `mcpDoctorAll(ctx,out)` (per-server status+runtime+recipe checks), `mcpLogs(args,out)` (log-tail stub).
- `writeRuntimeCheck/writeRecipeChecks/writeMailChecks/envValue` — HTTP/PATH runtime checks + mail/calendar/whatsapp recipe health (secrets redacted).

**swarm_demo.go**
- `var demoGoals`, const `demoWorkerAnswer`; `runSwarmDemo(args)` (parse `--max-steps` + goals), `swarmDemo(w,goals,maxSteps)` — drive real `swarm.Run` over an `agenttest.FakeClient`, print ordered ChildReports as JSON.

**task.go**
- const `taskUsage`; `runTask(args)` — dispatch the scheduler triad CLI (LLM-free, sysexits); `openTaskPool`.
- `taskSchedule` (cron/at/every + `cron.ParseSchedule`/`FirstFire`/`scoring.ComputeTaskTier`, status-aware INSERT), `triadToSpec`, `taskList`, `taskCancel`, `taskRunNow`, `taskApprove`, `taskRuns` (job-runs ledger), `taskDoctor` (health counts).
- `requireID/payloadOrEmpty/defaultSchedulerTZ/nullableText/nullableInt/nullableTime/fmtTimePtr` — binding + formatting helpers.

**skills.go**
- const `skillsUsage`; `type skillsEnv{cfg,pool,w,audit}` + `.close()`; `newSkillWriter(cfg,pool) *skills.Writer`; `bootSkills(ctx)`.
- `runSkills(args)` — dispatch; `skillsList/Info`, `skillsWrite(action)` (create/update staged pending), `skillsDelete` (staged + archive), `skillsApprove` (CLI activation channel), `skillsAlways` (on/off), `skillsAudit` (+`purge` → `skillsAuditPurge`, which surfaces the append-only role permission-denied proof), `shortHash/hasFlag`.

**skills_snippet.go**
- const `snippetUsage`, `var snippetExecTimeout=120s`; `skillsSnippet(ctx,args)` — dispatch `snippet {save|exec}`.
- `skillsSnippetSave` (stage pending executable snippet via `Writer.SaveSnippet`), `skillsSnippetExec` (run an ACTIVE snippet via `Writer.UseSnippet` + `StampUsage`), `runSnippetProcess(...)` — exec the interpreter under a signal+timeout ctx.

**shell.go**
- `runShell(args)` — boot the persisted chat env (`bootChatNamed("aura shell")`), create a new conversation, run the REPL.

**toolpipe.go**
- `runToolPipe(_)` — NON-LLM NDJSON harness: build the real registry, execute `{"tool","args"}` lines via `WithToolCallContext`, print per-call ms + total.

**config.go**
- const `redactedAPIKey`; `runConfig(args)` / `configUsage()` — dispatch `config {show|get|set}`.
- `configShow` (effective llm.Config, api_key REDACTED), `configGet`, `configSet` (read-modify-write `~/.aura/llm.json`).
- `loadLLMConfigTolerant/isMissingAPIKey/getConfigKey/setConfigKey/setIntKey/jsonString/jsonNumber/jsonBool/userConfigFilePath/readConfigFile/writeConfigFile` — tolerant load + dotted-key get/set (api_key unsettable) + 0600/0700 file IO.

**agent.go**
- const `dryRunToolName="noop"`; `type dryRunConfig{requestID,maxSteps,maxWallclockSec,dedupWindow}`.
- `runAgent(args)` — dispatch `agent {dry-run}`; `parseDryRunArgs` (CLI>env>default), `dryRun(cfg,w)` — drive a mock `LoopAgent` over `InfiniteToolCallAgent`, one JSON Event per line; `resolveRequestID/buildBudget/overrideInt`.

**profile.go**
- const `profileUsage`; `runProfile(args)` — dispatch `profile {show|add-fact}`.
- `profileShow` (section tree or `--json`), `profileAddFact`, `profileFlagSet/profileStore/profileExit` — shared flag set + store builder + friendly error classification.

**memory.go**
- consts `memoryServerName="memory"`, `memoryUsage`; `runMemory(args)` / `runMemoryCommand(ctx,args,out)` — operator path into the agent-memory MCP sidecar.
- `memoryVerbToTool` (verb → `memory_*`/`graph_query` wire tool + args), per-verb arg builders (`memoryAddEntityArgs/AddFactArgs/AddPreferenceArgs/StoreMessageArgs/RelationshipArgs/TraceArgs`), `callMemoryTool(ctx,tool,args,out)` (open managed memory sidecar over streamable-HTTP, call the raw tool), `arg/argOr` positional helpers.

**docs.go**
- const `docsUsage`; `type docsCLIService interface`, `type docsServiceFactory`; `runDocs(args)`/`runDocsCommand(ctx,args,out,factory)` — dispatch `docs {ingest|search|status|list|bench}`.
- `docsIngest/Search/Status/List/Bench` (bench = ingest + 5× search → p95 + industrial score), `newDocsService(ctx)` (pool + knowledge MCP + `documents.Service`).
- `type runtimeDocumentIngestor` + `newRuntimeDocumentIngestor` + `.IngestPath` (wired with the async embedding queue — telegram/serve consume this), `type runtimeEmbeddingQueue` + `.Enqueue` (background `EmbeddingWorker.Process`), `type docsToolSearcher{cfg}` + `.Search` (per-call knowledge-MCP searcher).
- `documentsBaseURL/documentHTTPClient/writeJSON/percentile/industrialScore` — sidecar URL/client + output + bench helpers.

**chat.go**
- consts `exitCommand="/exit"`, `chatUsage`; `type chatEnv{cfg,pool,conv,pause,identity,run,client,reg,toolHandles,mcpClosers}` + `.close()` — shared composition root for chat/shell/serve (drains background shells + reasoning learner, closes MCP + pool).
- `runChat(args)` — dispatch the `chat` subverb group (bare = new REPL).
- `bootChat/bootChatNamed` (os.Exit-translating wrappers), `bootChatEnv` (error-returning root, `config.Load` fail-fast), `bootServeChatEnv` (keyless `config.LoadServe`), `bootChatEnvWithConfig(ctx, loadConfig)` (the real boot: validate, open pool, build Stores, orphan scan + tiktoken init, registry+MCP, optional reasoning/tool-select self-improvement stores gated by `AURA_LLM_REASONING_LEARNING`, build the Runner).
- `chatList/Search/SetStatus/Rename/Delete/New/Resume`, `mostRecentActive`, `runReplOrExit/runReplOrExitNamed` (wire OTel + two-stage Ctrl+C, run `chatLoop`), `type replDeps{in,out,errOut,run,cfg,convID,newTurnCtx}`.

**chat_repl.go**
- `newTracer(ctx,cfg)` — wire the OTel TracerProvider.
- `chatLoop(ctx,d)` (testable REPL core: read line, drive Turn, stream prose+footer, handle pauses, quit on EOF/`/exit`/2nd Ctrl+C, defers `Runner.Stop`), `runUserTurn/driveTurn/renderAndAnswerPauses/promptForPause`, `type pauseRenderOption` + `decodePauseOptions`, `isYes/parseChoice/trimLine`, `signalTurnCtx` (two-stage Ctrl+C), `printConversationList/printSearchResults`, `excerpt/clampExcerpt` (rune-safe FTS window).

**chat_render.go**
- `renderRunnerTurn(w,seq) (answer,finish,usage,paused,err)` — drive one Turn: stream clean prose, dim tool-activity, live CoT, skip tool-result previews, read usage off the final StateDelta, detect pause/discard.
- `costFooterFromFinish/flushRemainder/discardStreamed/isToolResultPreview/isTerminalToolCall/renderToolActivity/costFooter/usageFromStateDelta/anyInt/anyFloat`, `type iterSeq2` — footer (token+USD+latency via `llm.CostUSD`), tail flush, partial-render erase, tool-marker detection.

**chat_reasoning.go**
- const `cliReasoningWidth=72`; `type cliReasoning{fifo,active}` + `newCLIReasoning` + `.push(w,delta)` / `.clear(w)` — bounded in-place live CoT renderer over `reasoningfifo.FIFO` (dim rolling 💭 line, carriage-return redraw).

**llm_client.go**
- consts `llmNotConfiguredCode/Hint`; `type llmNotConfiguredClient`, `type llmNotConfiguredError`; `newLLMClient(cfg) llm.Client` — `openai_compat.New` when keyed, else the sentinel client whose `.Stream` always returns the not-configured JSON error.

**doctor.go**
- `type doctorProbe func(ctx,*config.Config)(string,error)`, `type doctorPostgresPool/doctorNeo4jClient interface`, `type doctorCheck{name,probe,failureCode}`.
- `var` overridable probe/IO seams (`doctorProbePostgres/Neo4j/Embed/MCPBinary`, `doctorLookupLLMKey`, `doctorLookPath`, `doctorHTTPClient`, `doctorOpenPostgres/Neo4j`).
- `runDoctor(args)` / `runDoctorWithConfig(ctx,out,cfg) int` — run every check, print PASS/FAIL, aggregate the first non-zero failure code, emit `status: OK|FAIL`.
- `doctorChecks()` (postgres/neo4j/embed/mcp-neo4j-cypher binary/llm_key), `defaultDoctorProbePostgres/Neo4j/Embed/MCPBinary`, `doctorProbeLLMKey` (presence-only OPENROUTER_API_KEY).

**cache_audit.go** (hidden — runtime KV-prefix invariant gate)
- consts `exitMutation=1, exitFixture=2, auditTurns=20, auditFixtureDir`; `type fixtureResponse/fixtureToolCall/fixtureTurn`.
- `runCacheAudit(args)` / `cacheAuditMain(ctx,_,out,errOut) int` — load + replay 20 fixtures through the real `runner.Turn`, assert messages[0]/[1] + skill-manifest streams are byte-stable across turns.
- `reportHashes/replayAudit/expectedAuditRequests/hashMessages0/hashMessages1/skillManifestHash/setupAuditSkills/drainTurn/decodeFixture/scriptTurns/toFakeTurn/loadFixtures/repoRoot` — hashing (`prompt.PrefixHash`), in-memory fake-store replay, deterministic skill/profile fixtures, strict fixture parse.

**serve.go**
- consts `aguiShutdownTimeout`, `aguiReadHeaderTimeout`, `serveUsage`; `type serveEnv{*chatEnv,store,scheduler,httpSrv,channels,setupSrv,sweeper}`.
- `runServe(args)` — parse `--no-telegram`/`--only=cli`, signal+work ctx split, boot, obs init, launch AG-UI HTTP goroutine + channels + setup + sweeper, run the scheduler, bounded drain on signal, graceful shutdown.
- `bootServe(ctx, channelOverride)` — build the daemon over the shared root: cron Store + channels Registry + dispatch + Scheduler + AG-UI server + sidecar sweeper + seed skill TTL sweep.
- `serveReadinessProbes(chat)` (PG ping + Neo4j dial `/readyz` probes), `seedSkillTTLSweep` (daily 03:00 snippet TTL cron), `var _ cron.ChannelDeliverer = (*channels.Registry)(nil)`, `buildDispatch(chat,store,reg)` (cron Dispatcher: per-TaskKind handlers + composite Notifier + quiet-hours + late-bound channel deliverer), `type handlerAdapter` + `.Meta()/.Run()` (bridge `handlers.Handler` onto `cron.Handler`, breaking the tools→cron→handlers cycle).

**serve_drain.go**
- `type drainResult int` (`drainResultDrained/TimedOut`) + `.String()`; `drainWithGrace(drain, grace) drainResult` (teardown goroutine waited up to grace; non-positive = single non-blocking poll); `drainShutdown(workCtx, env)` (stop channel subsystems + sweeper + bounded `httpSrv.Shutdown`).

**serve_channels.go**
- consts `localIdentityName="local"`, `setupShutdownTimeout`; `bootChannelsAndSetup(ctx, chat, override)` — build the Telegram channel over the shared root, register it, build the setup-wizard server.
- `multimodalConfig(cfg)` (map central config onto `telegram.MultimodalConfig`), `type todayCost{metrics}` + `newTodayCost` + `.TodayUsage` (daily cost backend for `/cost`), `clampInt64ToInt`, `ensuringTurn(run)` (lazily create a chat's conversation row on first inbound message), `buildSetupServer(ctx,chat)`, `type setupStoreAdapter` + `InsertPending/PendingConsumed/CountAccounts` (bridge `*telegram.Store` onto `setup.Store`), `resolveLocalIdentityID`, `telegramGetMeProbe` (validate bot token via live getMe, never logs it), `startChannelSubsystems/stopChannelSubsystems` (fail-soft lifecycle), `serveTelegramOverride(args)` (`--no-telegram`/`--only=cli` → registry enable override).

**serve_adapters.go**
- `newTaskTool(ts)` — build the non-deferred `task` tool (nil-safe store).
- `type selfSendResolver{reg}` (+ `var _ cron.SelfSendResolver`) + `newSelfSendResolver` + `.Resolve(bareName)` and `type selfSendTool{tool}` + `.Send(ctx,args)` — resolve + execute an MCP self-send tool off the registry.
- `type cronTaskStore{pool,store,conv}` + `newCronTaskStore` + `.CreateScheduledTask/ListScheduledTasks/CancelScheduledTask/RunScheduledTaskNow/ApproveScheduledTask` — adapt live PG + `cron.Store` onto the `tools.taskStore` seam (snapshots owning identity at schedule time, status-aware paths).
- `taskStorePool(ts)`, `newSkillTool(cfg,pool)` (non-deferred `skill` tool: materialize builtins, loader over `skillLoaderRoots`, gated Writer when a pool exists), `newSkillResumeHook(cfg,pool)` (decode `skill_approval` HITL context), `chainResumeHooks(hooks...)`, `newShellResumeHook(approvals)` (decode `shell_exec_approval` context), `skillLoaderRoots(cfg)`, `alwaysBlockProvider(cfg)` (per-turn messages[1] always-block, lazy TTL re-scan), `profileContextProvider(cfg)` (per-identity Agent.md context block), `type snippetSweeperAdapter{w}` + `.SweepExpiredSnippets(...)` (bridge `*skills.Writer` onto the `handlers.SnippetSweeper` seam).
