# Phase 2: Agent Cornerstone - Context

**Gathered:** 2026-05-29
**Status:** Ready for planning
**Confidence:** Design validated to ≥95% via 5 parallel scout agents (online + curated D:/tmp sources). Aggregate ~95.4 after fixes adopted.

<domain>
## Phase Boundary

Replace the Phase-1 `Loop` skeleton (`internal/agent/loop.go`, 132 LOC concrete struct) with the **unified `Agent` interface + workflow agents** (Sequential/Loop/Parallel), pattern **stolen-not-imported** from `google/adk-go` v1.4.0 (Apache 2.0). Wire the 3-cap **Budget tree** (steps + wallclock + dedup) with **child-inherits-parent's-remaining** semantics via a shared atomic counter. Ship `aura agent dry-run` CLI smoke to demonstrate SC#2/3/4.

**This is the substrate cornerstone** — every later phase (3 LLM, 4 HITL/Conv/Id, 5 Sandbox, 6 KV, 7 Web, 8 Sandbox-2b, 9 Swarm, 10 Scheduler, 11 Skills, 12 AG-UI, 13 Channels, 14 Onboarding, 15 Memory) implements or consumes this interface. Decisions here are deliberately over-validated.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**8 requirements are locked.** See `02-SPEC.md` for full requirements, boundaries, and acceptance criteria (ambiguity 0.105, gate ≤0.20).

Downstream agents MUST read `02-SPEC.md` before planning or implementing. Requirements are not duplicated here — but **this CONTEXT amends several SPEC details** (see `### PRD-Amendments Required` below). Where this CONTEXT and SPEC conflict, the amendments in this CONTEXT win (they are research-backed corrections discovered during discuss-phase).

**In scope (from SPEC.md):** `internal/agent/{agent,event,budget}.go`; `internal/agent/workflow/{sequential,loop,parallel}.go` + `workflow_test.go`; `budget_test.go`; `event_test.go`; `cmd/aura/agent.go` (`aura agent dry-run`) + `main.go` diff (drop `chat`/`stubClient`, add `agent`); `cmd/aura/agent_test.go`; `scripts/loop_budget_smoke.sh`; delete `internal/agent/loop.go`; new env vars in `.env.example`.

**Out of scope (from SPEC.md):** `LlmAgent` (Phase 3); AG-UI SSE transport mapping (Phase 12 — but Event shape is forward-compat here); `tools.Registry`↔Agent wiring (Phase 3); conversation persistence (Phase 4); OTel transitive dep (shape-compat only, no import); `aura chat` restore (Phase 3); swarm semantics (Phase 9); `ask_user` resume (Phase 4); skills (Phase 11); memory (Phase 15).

</spec_lock>

<decisions>
## Implementation Decisions

> Methodology: 4 discussion rounds + a 5-agent parallel scout pass (3 online, 1 curated-source deep-scan of `D:/tmp`, 1 OTel/AG-UI). The user explicitly required ≥95% confidence on this cornerstone. Each decision below carries its validation source.

### Interface shape (Public API)
- **D-01 — OPEN interface (diverge from adk).** `Agent` exposes only `Name() string`, `Description() string`, `Run(InvocationContext) iter.Seq2[*Event, error]`, `SubAgents() []Agent`, `FindAgent(name string) Agent`. **No unexported sealing method.** adk-go SEALS its interface via `internal() *agent` (`adk-go agent/agent.go:51`) so only its constructors can implement it — but adk's own doc says *"in future releases we will allow just implementing this interface"* (heading toward open). Aura's premise ("ogni Phase successiva implementa questa interface", SPEC L9) **requires** open: Phase 3 `LlmAgent`, Phase 9 swarm implement `Agent` directly. *Validated: scout-3 (DIVERGENT-BUT-DEFENSIBLE, upstream is moving this way).*
- **D-02 — Workflow structs EXPORTED + constructor returns interface.** `type SequentialAgent struct`, `LoopAgent`, `ParallelAgent` are **exported**; constructors `NewSequential(name string, subs ...Agent) Agent` / `NewLoop(name string, maxIter uint, subs ...Agent) Agent` / `NewParallel(name string, subs ...Agent) Agent` **return the `Agent` interface** (factory pattern, like `net.Pipe()`/`context.WithCancel()` — composes cleanly for nesting). Compile-asserts `var _ agent.Agent = (*workflow.SequentialAgent)(nil)` (etc.) reference the exported structs as SPEC Acceptance L129 wants. **Resolves SPEC self-contradiction** (Req#4 said unexported `sequentialAgent`). **Typed-nil guard:** never `return (*SequentialAgent)(nil)` on an error path — return an explicit nil interface or `(nil, err)`. *Validated: scout-3 (stdlib factory precedent), scout-4 (ADK exported-event pattern).*

### Escalation, budget signalling, error slot
- **D-03 — Escalate cancels siblings via captured `context.CancelFunc`, NOT a sentinel error.** ParallelAgent's coordinator captures a `cancel` from `errgroup.WithContext`; on a child Event with `Actions.Escalate=true`, it calls `cancel()`. Escalation is a control signal, not a failure → a fake error would pollute `errgroup.Wait()`. *Validated: scout-3 (this is the legitimate captured-cancelfunc pattern; passing cancelfunc across API boundaries is the anti-pattern, capturing in coordinator scope is fine).*
- **D-04 — Budget exhaustion = Event-only; `ErrBudgetExhausted` sentinel exported for programmatic consumers.** Termination is signalled by an explicit Event (`Actions.Escalate=true` + `StateDelta{termination_reason, limit_hit, steps_consumed}`), **never** through the `error` slot. Additionally export a sentinel `agent.ErrBudgetExhausted` for Phase 3/9 callers that inspect outside the Event stream. Machine-readable stop-reason validated by nanobot `stop_reason="max_iterations"` (`nanobot agent/runner.py:107-119`) and adk `EventActions.Escalate`.
- **D-05 — Cancelled siblings drain `(nil, nil)`, not `ctx.Err()`.** When escalate cancels siblings, they exit cleanly without yielding `context.Canceled` (intentional cancellation ≠ error noise). adk yields `ctx.Err()` (`parallelagent/agent.go:142`); Aura diverges for cleaner operator output. The `error` slot of `iter.Seq2` carries only *real* failures (LLM/tool errors).

### Env / flag precedence
- **D-06 — CLI flag sentinel `-1` + fallthrough to env; `NewBudgetFromEnv` fail-fast.** `aura agent dry-run` flags (`--max-steps`/`--max-wallclock-sec`/`--dedup-window`) default to `-1` ("unset") → fall through to `NewBudgetFromEnv()`; a non-`-1` value overrides. Precedence: **CLI flag > env > builtin default (25/300/3)**. `NewBudgetFromEnv() (*Budget, error)` **fail-fast** with exact message on malformed env (e.g. `AURA_LOOP_MAX_STEPS=abc`), consistent with Phase 1 boot fail-fast (D-06 prior phase). Avoids the cobra default-masks-env footgun. *Validated: sharp-edges (sentinel beats opaque flag.Changed for explicitness).*

### Reusable code placement
- **D-07 — Mock agents in shared `internal/agent/agenttest/` package.** `InfiniteToolCallAgent`, `EmitNThenEscalate`, `RecordingAgent` live in a reusable pkg, not inline in `workflow_test.go`. Phase 3 (LlmAgent) and Phase 9 (swarm) reuse them — zero mock duplication. *CLAUDE.md "reusable code" rule.*
- **D-08 — Canonical JSON in shared `internal/canonicaljson/`, RESCOPED as internal deterministic serializer (NOT RFC-8785).** Export `Marshal(any) ([]byte, error)`. Consumed by `budget.go` now, Phase 4 (conversation hash) + Phase 11 (skill content_hash) later. **Critical rescope (scout-2):** do NOT chase RFC-8785 compliance — it exists for cross-system cryptographic signatures Aura doesn't need, and its float-canonicalization layer is a determinism minefield (`1` vs `1.0`, sci-notation, UTF-16 vs UTF-8 key order, 286k+ oracle vectors per `json-canon`). Requirements collapse to: (a) sort map keys by **one documented order** (Go byte order is fine — no cross-impl consumer); (b) decode args with `json.Decoder.UseNumber()` so numbers stay literal text and **never round-trip through `float64`**; (c) **strict-reject** un-canonicalizable input (never silently coerce → no silent hash collisions). Fuzz test asserts `canon(x)==canon(decode(encode(x)))` and `1`≠`1.0` per documented rule.

### Budget / context derivation (the cornerstone mechanic)
- **D-09 — TWO context derivations.** Sequential/Loop → `ctx.WithSubAgent(sub)` shares the budget **as-is including the SAME dedup ring** (one logical reasoning thread iterating — LoopAgent MUST see cross-iteration repeats for the dedup termination, SC#5). Parallel → `Budget.Child()` forks a **DISTINCT dedup ring** (concurrent siblings isolated, no cross-branch false-positives) **while sharing the same `*atomic.Int32` step counter**. Two derivations, not one-with-a-boolean-flag (opaque-bool footgun). *Validated: scout-1 (Anthropic isolated-context-per-subagent under orchestrator-bounded-effort = exactly this split), scout-4 (adk fresh ctx+branch per parallel sub).*
- **D-10 — Shared-atomic-remaining is the correct frontier pattern.** Single `*atomic.Int32` shared by pointer across the whole tree → depth-3 spawn (Root→3 Parallel→each 3 = 9 leaf) consumes ≤25 total, NOT 25³. **Directly mirrored by AWS Strands Swarm** (`max_iterations` = "total across all agents", single shared counter) and OpenAI Agents SDK (run context carries shared usage across handoffs). Aura is **ahead of** LangGraph (no shared budget across subgraphs — open request #1260), Google ADK (`LoopAgent.max_iterations` does not propagate to sub-agents), and nanobot (fresh-per-child = depth³ trap). *Validated: scout-1 (88→95 after D-12 soft cap), scout-4 (ADK shared-InvocationID, 95% twin).*
- **D-11 — `ConsumeStep` = atomic decrement-then-check-then-restore.** `new := steps.Add(-1); if new < 0 { steps.Add(1); return false, "max_steps" }`. Prevents the check-then-decrement TOCTOU where N concurrent goroutines all pass `>0` then all decrement (logical over-spend beyond cap). Race-test: N goroutines vs counter of 1 → exactly one `ok=true`. *Validated: scout-1 Risk-2.*
- **D-12 — Per-branch SOFT CAP at `Budget.Child()` (prevents sibling starvation).** `softCap = ceil(remaining / fanout)` (tunable via `AURA_LOOP_BRANCH_SOFT_FRACTION`): throttles a branch to its fair share but allows borrowing from the shared pool only after siblings complete. Hard total bound preserved; ~30 LOC. Closes the one gap where the flat counter (Strands ships it flat) allows a greedy branch to eat 20/25 steps and starve siblings → deep-narrow result on a breadth task. *Validated: scout-1 Risk-1 (the 88→95 lever; academic frontier DOVA uses proportional allocation for exactly this).*
- **D-13 — Wallclock via `context.WithDeadline` threaded into leaf calls; optional `AURA_LOOP_NODE_TIMEOUT_SEC`.** The 300s `deadlineWallclock` only stops *new* `ConsumeStep`; an in-flight LLM/tool call can overrun. Derive the agent-tree `context.Context` from `context.WithDeadline(parent, deadlineWallclock)` so cancellation propagates end-to-end (golang-context rule). Optional per-node soft timeout (Strands `node_timeout` parity) so one hung tool can't silently eat the whole budget.

### Author / Branch / IDs (Event shape)
- **D-14 — Author set EXPLICITLY per workflow agent + optional `event.SetAuthorIfEmpty(name)` helper.** Aura's open interface has no base-struct embed hook, so adk's auto-fill-in-base-`Run` (`agent/agent.go:198`) doesn't apply. Each workflow agent sets `Author` explicitly; a small helper covers LLM-emitted events. nanobot is explicit too. *Validated: scout-3/scout-4.*
- **D-15 — Branch = dot-join `<branch>.<childName>`; LoopAgent adds `.iter-<N>`.** Parallel join per adk (`parallelagent/agent.go:77-80`); LoopAgent appends `.iter-<N>` per iteration (diverges from adk which doesn't, matches SPEC example `root.iter-2.worker-3`, useful for Phase-9 swarm trace correlation). **Branch is a LABEL only** — hierarchy is reconstructed from `SpanID`/`ParentSpanID`, never by parsing Branch. Escape or reserve the `.` separator if agent names can contain dots.
- **D-16 — Trace IDs: 16-byte UUIDv7 TraceID, 8-byte crypto/rand SpanID (OTel-correct).** **CRITICAL fix (scout-5):** `RequestID`/`TraceID` = `uuid.UUID` (UUIDv7, 16 bytes — fits OTel/W3C TraceID exactly). `SpanID`/`ParentSpanID` = **8 random bytes from `crypto/rand`** (NOT UUIDv7) — OTel/W3C SpanID is **8 bytes**, a 16-byte UUIDv7 would force lossy truncation later and silently break historical trace correlation. 8 random bytes = exactly OTel's `RandomIDGenerator`. This makes the "OTel-compatible no-dep" claim **TRUE**. UUIDv7 via `github.com/google/uuid` v1.6.0 (`NewV7` is monotonic). *Validated: scout-5 (the single biggest score lever, 78→95).*
- **D-17 — Event forward-compat IDs for AG-UI: add `MessageID` + surface `ToolCallID` + add `ThreadID`.** AG-UI is a stream of ~17-20 fine-grained events keyed by `message_id`/`tool_call_id`, with `thread_id`/`run_id` on lifecycle events and `STATE_DELTA` as RFC-6902 JSON Patch. Add `MessageID` (UUIDv7) to `LLMResponse`, surface the provider `ToolCallID` on `ToolCall`, add `ThreadID` to `Event` (`RequestID`≈`run_id`, `ThreadID`≈conversation thread from Slice 1.8). Then Phase-12 is a **fan-out adapter, not a refactor** (the gateway explodes one Event into START/CONTENT/END sub-events and translates `StateDelta` map→JSON-Patch). *Validated: scout-5 Risk-2.*

### Dedup / loop-guard (TWO-TIER — reversed from initial choice)
- **D-18 — TWO-TIER fingerprint (primary name+args, result as VETO only).** **Reverses the earlier `(name,args,result)`-in-hash choice** after scout-2 showed it is **fail-open**: any tool returning a volatile field (timestamp, page-token, request-id) never produces a repeating triple → the loop is never detected. Correct design: **primary fingerprint `sha256(name + canonical_json(args))`** — fires *before* re-executing (blocks side-effects), matches OpenFang/Hermes#481/Claude-Code#4277/Gemini. Bounded **result-preview used ONLY as a progress VETO** (args repeat but result changed → progress, suppress dedup — the zeroclaw#2152 insight) → volatile results fail *safe* (look like progress) instead of fail-open. *Validated: scout-2 (the 86→95 lever).*
- **D-19 — `AURA_LOOP_DEDUP_EXEMPT_TOOLS` allowlist** for genuinely poll-shaped tools (OpenFang + OpenClaw both ship this; standard pagination/polling escape hatch).
- **D-20 — Window=3, period-1 + period-2 (ping-pong), ring ≥ max(WINDOW,4).** Threshold 3 consecutive identical = industry consensus (Hermes#481, zeroclaw prod). Detect period-1 (A-A-A) AND period-2 (A-B-A-B) — exactly OpenClaw's shipped `genericRepeat`+`pingPong` tier. Period-k/general cycles are universally Phase-2/future (not a gap). Ring needs ≥4 slots for ping-pong while WINDOW=3 governs period-1. *Validated: scout-2 (ALIGNED, multi-source consensus).*

### Quality discipline (Go idioms — implementation gates)
- **D-21 — Property-based testing via `pgregory.net/rapid`** on `budget.go`: (a) total steps consumed ≤ max_steps initial; (b) escalate always yielded before return. PLUS a JSON round-trip property test on `Event` (`UseNumber()` + `SetEscapeHTML(false)` + canonical `time.Time` format) — byte-identical guarantee scoped to **Aura-internal symmetric encode only**, not a wire-level claim.
- **D-22 — `iter.Seq2` discipline:** every yield is `if !yield(ev, err) { return }` (no bare `yield(...)` — panics if called after returning false); producer goroutine/channel cleanup via `defer` *inside the iterator body* (runs on early return); no yielding from spawned goroutines (ParallelAgent fans-in to a channel and yields serially from the iterator frame).
- **D-23 — goleak-passing cancel/drain (the highest-leverage test):** in ParallelAgent every channel op (results send, ack send, ack recv) is a 2-arm `select` with `case <-ctx.Done(): return nil`; coordinator runs `defer cancel()`; guard the spawn loop with `if ctx.Err() != nil { break }` (Go #61611). Dedicated test: consumer breaks after 1 event with 3 slow children → `goleak.VerifyNone`. `goleak.VerifyTestMain(m)` in `workflow_test.go` (SC#1).
- **D-24 — `InvocationContext` single-Run-scope invariant:** `Ctx context.Context` as a **named field** (not embedded — embedding invites passing it *as* a ctx and storing it). Doc comment: "InvocationContext is single-Run-scoped; never store on a long-lived struct, never cache, never share across invocations." `WithContext`/`WithSubAgent` always return a **copy**, never mutate. Optional lint guard that no long-lived service struct holds an `InvocationContext` field. *Go team discourages ctx-in-struct; adk does it but flags it as debt — this invariant keeps it defensible.*

### PRD-Amendments Required (planner MUST emit before/with the slice commit)
- **A1 — SPEC Req#4:** workflow structs are **EXPORTED** (`SequentialAgent`), not unexported `sequentialAgent`. (D-02)
- **A2 — SPEC Req#3 dedup (L40):** TWO-TIER fingerprint — primary `sha256(name+canonical_args)` + result-as-veto, NOT `(name,args,result)` in the hash; + period-2 ping-pong + `AURA_LOOP_DEDUP_EXEMPT_TOOLS`. (D-18/19/20)
- **A3 — SPEC Constraint (L112):** canonical JSON lives in `internal/canonicaljson/` (not private in `budget.go`), rescoped as an **internal deterministic serializer** — drop the "RFC-8785" compliance framing. (D-08)
- **A4 — SPEC Req#1/#2:** `SpanID`/`ParentSpanID` are **8 bytes** (crypto/rand), not UUIDv7; add `MessageID`/`ToolCallID`/`ThreadID` to Event/LLMResponse for AG-UI forward-compat. (D-16/17)
- **A5 — SPEC Req#3/#6 Budget:** add per-branch soft cap (D-12), `ConsumeStep` decrement-then-check-then-restore (D-11), wallclock via `context.WithDeadline` + optional `AURA_LOOP_NODE_TIMEOUT_SEC` (D-13).
- **A6 — SPEC Acceptance:** `github.com/google/uuid` is **NOT** already in the dependency chain. Verified `go.mod`/`go.sum` 2026-05-29: absent. It is a genuine **new direct dep** (SPEC's "already in chain via golang-migrate v4" is wrong). `golang.org/x/sync` (errgroup) IS already present (indirect) — no new dep for ParallelAgent.
- **A7 — New env vars** (beyond the 3 in SPEC) to `.env.example` + PRD env catalog: `AURA_LOOP_DEDUP_EXEMPT_TOOLS` (CSV), `AURA_LOOP_BRANCH_SOFT_FRACTION`, `AURA_LOOP_NODE_TIMEOUT_SEC` (optional), `AURA_LOOP_DEDUP_RESULT_CAP` (result-preview byte cap, or reuse `Config.ToolPreviewCap`).

### Claude's Discretion (defaulted, planner-overridable)
- Result-preview byte cap default: 1–4 KB (no canonical industry number; matters only for the Tier-B veto, so non-critical). Reuse `Config.ToolPreviewCap` if it exists.
- `AURA_LOOP_BRANCH_SOFT_FRACTION` exact default and whether it's a fraction vs `ceil(remaining/fanout)` — planner picks; both preserve the hard total bound.
- Go version stays 1.26.3 (go.mod verified) — `iter.Seq2` GA since 1.23.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Locked requirements (read first)
- `.planning/phases/02-agent-cornerstone/02-SPEC.md` — 8 locked requirements, boundaries, acceptance criteria. **MUST read** — but apply the A1–A7 amendments in this CONTEXT where they conflict.

### Pattern source (stolen-not-imported, Apache 2.0)
- `D:/tmp/adk-go-study/agent/agent.go` — `Agent` interface (L43-52, note the `internal()` SEAL at L51 Aura removes), constructor pattern, `getAuthorForEvent` author auto-fill (L237-243), base-`Run` wrapper (L162-215).
- `D:/tmp/adk-go-study/agent/context.go` — `InvocationContext` (adk models it as an interface; Aura uses a struct).
- `D:/tmp/adk-go-study/agent/workflowagents/sequentialagent/agent.go` — Sequential `Run` core (L79-90).
- `D:/tmp/adk-go-study/agent/workflowagents/loopagent/agent.go` — Loop `Run` + escalate check (L75-105, escalate at L88; no budget — Aura adds it).
- `D:/tmp/adk-go-study/agent/workflowagents/parallelagent/agent.go` — errgroup + resultsChan + per-event ackChan + doneChan backpressure (L67-164) — **Aura steals this verbatim** for ParallelAgent (SPEC Req#6).
  - NOTE: `adk-go-study` is a shallow clone of `main` (commit 2472d61), not the v1.4.0 tag. Pattern is stable; cite by structure not line if drift.

### Curated industrial cross-references (D:/tmp — read first per project discipline)
- `D:/tmp/picobot/internal/agent/loop.go:236-281` — minimalist Go agent loop: single `maxIterations`, no budget/dedup/subagents. Confirms Aura's machinery is justified only by swarm capability, not over-engineering.
- `D:/tmp/nanobot/nanobot/agent/subagent.py:140-183` — `SubagentManager.spawn` (FRESH max_iterations per child = the depth³ trap Aura avoids); `:99-102` `max_concurrent_subagents`.
- `D:/tmp/nanobot/nanobot/agent/runner.py:107-119` — `stop_reason` string enum (validates Aura's `termination_reason`/`limit_hit`).
- `D:/tmp/nanobot/nanobot/channels/manager.py:260-286` — SHA1 fingerprint dedup keyed by origin message.

### Project planning (this repo)
- `.planning/ROADMAP.md` Phase 2 (L65-75) — goal + 4 Success Criteria.
- `.planning/REQUIREMENTS.md` INFRA-03 — slice-mapped acceptance (amendments #1/#9/#15/#19).
- `.planning/PROJECT.md` — substrate identity, Go 1.25+ constraint, key decisions.
- `.planning/phases/01-infra-db-knowledge/01-CONTEXT.md` — prior-phase decision style + boot fail-fast discipline (D-06/D-07 there).
- `.planning/codebase/STRUCTURE.md` / `CONVENTIONS.md` / `TESTING.md` — target layout, naming, test discipline.
- `CLAUDE.md` §Behavioral rules (no god class ≤600 LOC, refactor on touch, reusable code), §Tool design (deferred-tool), §Post-edit validation (Gate 2), §Coverage floor 85%, §No skip-as-green in CI, §Env vars (`AURA_<DOMAIN>_<UNIT>`).

### External validation sources (scout research, 2026-05-29)
- Budget: https://strandsagents.com/docs/user-guide/concepts/multi-agent/swarm/ (shared total counter — exact twin); https://github.com/strands-agents/sdk-python/blob/main/src/strands/multiagent/swarm.py ; https://openai.github.io/openai-agents-python/running_agents/ (shared run usage); https://github.com/langchain-ai/langgraph/discussions/1260 (no shared budget — open request); https://www.anthropic.com/engineering/built-multi-agent-research-system (isolated context + orchestrator bound); https://arxiv.org/abs/2603.13327 (DOVA proportional allocation).
- Dedup: https://github.com/NousResearch/hermes-agent/issues/481 (SHA-256 name+args, threshold 3); https://docs.openclaw.ai/tools/loop-detection (genericRepeat+pingPong, result only in post-compaction guard); https://github.com/anthropics/claude-code/issues/4277 (hash name+args); https://github.com/zeroclaw-labs/zeroclaw/issues/2152 (result-as-veto for progress); https://www.rfc-editor.org/rfc/rfc8785.html + https://github.com/golang/go/issues/6384 (why NOT to chase RFC-8785).
- Go idioms: https://go.dev/blog/context-and-structs ; https://pkg.go.dev/iter ; https://pkg.go.dev/golang.org/x/sync/errgroup ; https://github.com/golang/go/issues/61611 ; https://dev.to/gabrielanhaia/gos-range-over-func-4-footguns-the-compiler-wont-warn-you-about-5akf .
- OTel/AG-UI: https://www.w3.org/TR/trace-context/ (TraceID 16B, SpanID 8B); https://opentelemetry.io/docs/specs/otel/trace/api/ ; https://docs.ag-ui.com/concepts/events ; https://github.com/ag-ui-protocol/ag-ui ; https://github.com/google/uuid/blob/master/version7.go (NewV7 monotonic).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/llm/client.go` (78 LOC) — `Client` interface + `Message`/`ToolCall`/`Chunk`/`Request`/`ToolDef`. DROP-IN COMPATIBLE; Aura's `Event.LLMResponse.ToolCalls` reuses `llm.ToolCall` (surface its `ID` as `ToolCallID` per D-17). Not rewritten in Phase 2 (Phase 3 owns LLM wire).
- `internal/agent/tools/{spec,manifest,search,text_response}.go` — `Registry` + Tool interface. DROP-IN COMPATIBLE; NOT wired to Agent in Phase 2 (Phase 3 via LlmAgent).
- `cmd/aura/main.go` subcommand dispatcher (L27-46) — integration point: remove `case "chat"`+`chatOnce`+`stubClient` (L30-35, 64-94), add `case "agent"`→`runAgent`.

### Established Patterns
- Module path `github.com/chetto1983/aura`; new packages under `internal/`.
- `go.mod`: **go 1.26.3** ✓ (iter.Seq2 ready); `golang.org/x/sync v0.18.0` present (errgroup ✓); `go.uber.org/goleak v1.3.0` present ✓; **`github.com/google/uuid` ABSENT** → new direct dep (A6).
- Phase-1 test discipline (`internal/db/db_test.go`): `goleak.VerifyTestMain` in `TestMain`, build-tag integration, race detector.
- `scripts/check-file-size.sh` exists (≤600 LOC enforcement).

### Integration Points
- `cmd/aura/main.go` dispatcher — add `agent` subcommand.
- `cmd/aura/agent.go` (new) — `aura agent dry-run` builds mock `LoopAgent[InfiniteToolCallAgent]` (from `agenttest`), wires Budget from flags (D-06), iterates `agent.Run(InvocationContext)`, prints each Event as one JSON line.
- `internal/canonicaljson/` (new) — consumed by `budget.go`; reused Phase 4/11.
- `internal/agent/agenttest/` (new) — mocks reused Phase 3/9.
- `.env.example` — add the SPEC 3 + A7 env vars.

</code_context>

<specifics>
## Specific Ideas

- **Smoke (`scripts/loop_budget_smoke.sh`):** mock sub emits the same tool call forever; `aura agent dry-run --max-steps 25` → assert exactly 26 Event JSON lines (25 step + 1 budget-exhausted), grep `"limit_hit":"max_steps"`, and the final Event has `"author":"<loop_name>"` + `StateDelta{termination_reason:"budget_exhausted", limit_hit:"max_steps", steps_consumed:25}`.
- **SC#3 depth test:** Root spawns 3 ParallelAgent each spawning 3 = 9 leaf, single shared `*atomic.Int32` from 25 → total ≤25 (with D-12 soft cap, fairly distributed). Add the goleak break-early test (D-23) and the tool-wrapped-sub-agent shared-counter test (scout-1 rec #4: if a sub-agent is ever exposed as a tool, it MUST thread the shared counter — else reintroduces the depth³ trap).
- **Attribution comment** (SPEC Constraint): `// Pattern derivato da google/adk-go v1.4.0 agent/workflowagents/loopagent/agent.go (Apache 2.0). Adattato per Aura con SC#2 budget exhaustion + SC#3 child-inherits-remaining + SC#4 UUIDv7 OTel-compat.`

</specifics>

<deferred>
## Deferred Ideas

- **Period-k / general cycle detection** in dedup (A-B-C-A-B-C) — universally Phase-2/future across all surveyed frameworks; window=3 + period-1 + period-2 is the shipped tier. Logged so it isn't lost.
- **Per-tool volatile-field strip list** for result fingerprinting — only needed if D-18's result-as-veto proves insufficient; the veto approach avoids it for now.
- **Proportional/weighted sub-budget allocation** (DOVA-style) — the academic frontier beyond D-12's soft cap. Revisit if breadth-vs-depth tasks degrade in Phase 9 swarm.
- **Real OTel integration** (`go.opentelemetry.io/otel/trace` import) — Phase 2 ships OTel-compatible *shape* only (D-16 makes it genuinely drop-in). The import lands in a future observability slice.
- **AG-UI SSE transport** (Event fan-out → ~17 event types, StateDelta→JSON-Patch) — Phase 12; D-17 makes it an adapter not a refactor.
- **`tools.Registry`↔Agent wiring + real `LlmAgent`** — Phase 3.
- **Swarm semantics** (DM-by-ID, tier-mapped models, `MAX_SPAWN_DEPTH=2`) — Phase 9 reuses ParallelAgent.

</deferred>

---

*Phase: 2-Agent-Cornerstone*
*Context gathered: 2026-05-29*
