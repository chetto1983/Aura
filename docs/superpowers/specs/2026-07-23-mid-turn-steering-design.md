# Mid-turn steering — design study (typing while the agent runs)

**Status:** DESIGN STUDY ONLY — no code, no PRD amendment yet. This document informs a
future PRD amendment (PRD-first discipline); a future session writes the amendment and the
implementation plan directly from it.
**Date:** 2026-07-23
**Scope studied:** `internal/agent` (loop seam), `internal/agui` (route + wire + 1.3B
registry), `internal/runner` (persistence seam), `internal/conversations` (seq/branch),
`internal/channels/telegram` (parity), `web/src/chat` (composer/reducer).
**Foundation:** fix-plan 1.3 Tier B run-detach infrastructure, ratified as PRD Amendment
#90 (prd.md:1683-1698) and shipped in `internal/agui/{runregistry,runsession,server_run_detach,server_run_resume}.go`.
**Related approved-but-unbuilt design:** durable swarm messaging
(`docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md`) — reconciled in §8.

---

## Executive summary

Today the cockpit composer is dead while the agent runs: the thread lock returns 409
`ErrThreadBusy` on a second `POST /agent/run` (internal/runner/runner.go:46,
internal/agui/server_run_detach.go:58-62), and the just-shipped 1.3B client explicitly
blocks send while `live_run_id` is set (web/src/chat/ExternalStoreChat.tsx:577,
:556-564). The operator wants Claude-Code-style **steering**: type a message mid-turn and
have it **injected into the running turn as a user message at the next round boundary** —
alongside the freshly appended tool results, before the next LLM call — instead of waiting
for turn end.

The study finds the seam is unusually clean. The agent loop already injects user-role
messages mid-run on three established paths (recovery nudge, empty-response nudge,
completion-gate feedback — internal/agent/llm_agent.go:331, :439 and
llm_agent_finalize.go), so a steer is a fourth, operator-sourced instance of an existing
in-loop pattern, not a new message discipline. The runner persists turns **incrementally
per round** (assistant tool_calls turn, then RoleTool result turns —
internal/runner/runner_persist.go:187-204, :144-151), so a steer user turn appended at the
drain point lands at exactly the right `seq` with no in-flight-assistant-row conflict. The
1.3B RunRegistry gives every live run an owner-scoped, addressable identity
(`run-<uuid>`), a mutation-route grammar to mirror (`POST /agent/runs/{runID}/cancel`,
internal/agui/idempotency_http.go:55), and a replay ring that makes the steer echo
resume-exact for free.

**Recommended semantics in one sentence:** a steer is additive user input, queued FIFO
per run and injected as one plain `user` message per steer at the top of the agent loop
(before the budget gate, after the previous round's tool results), never interrupting a
tool mid-execution, never extending the Budget, echoed on the wire as a custom AG-UI
frame, persisted at drain time, and — if the run ends before injection — bounced back to
the client to re-send as a normal new turn.

---

## 0. Ground truth — what the code does today

Claims below are load-bearing for the design; each is cited.

| # | Fact | Where |
|---|---|---|
| G1 | The agent loop is a single `for {}` in `Run`: budget gate → build request → LLM call → (terminal \| pause \| dispatch tools) → loop. "the next LLM call sees the appended RoleTool results." | internal/agent/llm_agent.go:230-482 (gate :251, buildRequest :302, dispatch :471, loop comment :481) |
| G2 | History appends happen ONLY at the tail: assistant answers (:334, :442, :470), user-role nudges/feedback (:331, :439), pause rewrite (:465), RoleTool results (llm_agent_dispatch.go:133). `messages[0..2]` prefix is never touched mid-run (builder re-derives it per call; tail-inject copies only — llm_agent.go:273-294). | internal/agent/llm_agent.go, llm_agent_dispatch.go |
| G3 | The budget bounds are steps + wallclock (`AURA_LOOP_MAX_STEPS` default 25, `AURA_LOOP_MAX_WALLCLOCK_SEC` default 300); `ConsumeStep` gates each round; recovery gets ONE gate bypass. | internal/agent/budget.go:29-41; llm_agent.go:248-259 |
| G4 | An `ask_user` pause TERMINATES the run: the loop rewrites history to the pause calls and returns (llm_agent.go:464-468); the translator yields `RUN_FINISHED(interrupt)` and stops (internal/agui/translator.go:79-86); the RunSession goes terminal and the thread lock is released (Amendment #90 point 5). Resume is a NEW `POST /agent/run` with `Resume[]` → `SubmitAnswers` (internal/runner/runner_resume.go:107-145). | as cited |
| G5 | The runner persists turns incrementally DURING the round: one assistant tool_calls turn per batch (runner_persist.go:187-204), one RoleTool turn per result (:144-151), the final assistant answer as its own row (:277-307), driven by `persistEvent` observing agent Events (:82-119). There is no single in-flight assistant row being built. | internal/runner/runner_persist.go |
| G6 | `AppendTurn` allocates `seq` as MAX(seq)+1 under a per-conversation row lock inside one tx (internal/conversations/store_append.go:65-108, :204-220). `AppendTurnParams` has NO branch field (:34-54); `branch_id` defaults to the canonical all-zero branch (migration 0017), and branch forks write pointers separately (store_branch.go:191-238). Rehydration repairs dangling tool-call groups with synthetic RoleTool markers (store_helpers.go:221-247). | as cited |
| G7 | The detached path (flag `AURA_AGUI_RUN_DETACH`, internal/config/config_agui_run.go:23) runs the turn on a `context.WithoutCancel` producer; ctx VALUES ride across the detach (principal, idempotency op, reasoning override, lock-held mark — server_run_detach.go:32-41). The RunSession ring stores post-redaction frames; replay is byte-identical (runsession.go:114-140, :152-183). | internal/agui/server_run_detach.go |
| G8 | Run-scoped routes resolve through the §3.2 owner-scoped 404 ladder (`resolveRunSession`, server_run_resume.go:27-47); cancel is a governed mutation (`agent_run_cancel`, Idempotency-Key required — idempotency_http.go:55) answering 202. | internal/agui/server_run_resume.go:94-103 |
| G9 | The cockpit composer is disabled while `live_run_id` is set (`sendBlocked`, ExternalStoreChat.tsx:577) with an explanatory hint (:556-564); Stop = abort + `POST cancel` (:246-260; sseResume.ts:396-402). | web/src/chat |
| G10 | Telegram serializes turns per chat via `registerTurn`; a message arriving mid-turn gets the busy reply "⏳ Sto ancora elaborando… /cancel" (bot_dispatch_turn.go:94-101; bot_dispatch.go:54). Telegram rides `Fanout` (subscribe-before-run), NOT the RunRegistry (runsession.go:12-20 package doc). | internal/channels/telegram |
| G11 | Values placed on the request ctx reach the agent: precedent for value-keyed seams is established (`tools.WithRequestID` llm_agent.go:184, `runner.WithReasoningOverride` runner.go:531, `gateway.WithResponder` runner.go:526, `runner.WithThreadLockHeld` runner.go:53). Import direction: `agui` → `runner` → `agent`; `agent` imports neither. | as cited |
| G12 | AG-UI protocol: input reaches the agent ONLY via `RunAgentInput` at run start (vendored SDK `types.RunAgentInput` — threadID, messages, resume entries); the event stream is strictly agent→client; there is NO mid-run client→server input event in the spec. The cancel route is already an Aura extension outside the protocol; steer would be the same class. | vendored `ag-ui-protocol/ag-ui` Go SDK; https://docs.ag-ui.com/concepts/events |

---

## 1. Industry survey — how mature agents take input mid-turn

| System | Behavior | Grammar |
|---|---|---|
| **Claude Code (CLI/TUI)** | A message typed while Claude works is delivered **between tool calls** — injected into the running turn as a user message alongside the next tool result. The community explicitly asks for the opposite (true end-of-turn deferral) because the queue "flushes at the next LLM pause, not at true end-of-turn" — i.e. inject-at-step-boundary IS the shipped default. Esc = interrupt-now escalation. | queue → inject at round boundary |
| **Claude Desktop (Code window)** | Holds the typed message and delivers it only after the whole turn completes; the CLI parity gap is a filed feature request. | queue → new turn after |
| **OpenAI Codex CLI** | While a tool call runs the UI states: *"Messages to be submitted after next tool call (press esc to interrupt and send immediately)"* — queue-then-inject at the next tool-call boundary, with Esc as the interrupt-now escalation. | queue → inject at round boundary, Esc = interrupt |
| **Cursor** | Queued messages are held and processed **after** the current run completes (default); a "send after current message" option injects earlier and has a history of bugs where it interrupts the agent; `/multitask` spawns subagents instead of queueing. | queue → new turn after (opt-in earlier injection) |
| **Devin** | Follow-up instructions can be sent to running sessions; Devin "adapts, revises its plan, and keeps moving" — injection semantics are not publicly specified at the step level. | inject (opaque granularity) |
| **Manus** | No documented mid-task injection contract found; cloud tasks continue in background; intervention is via new instructions/skill slash-commands, granularity undocumented. | undocumented |
| **AG-UI protocol** | No input-while-running event exists; client input is only `RunAgentInput` at run start / a new run with `Resume[]`. Steering is necessarily a **transport extension**, exactly like Aura's cancel route already is. | n/a — extension required |

Sources:
- [Claude Code #63190 — Deferred Messages: queue for end of turn](https://github.com/anthropics/claude-code/issues/63190)
- [Claude Code #49373 — queue flushes at next LLM pause, not true end-of-turn](https://github.com/anthropics/claude-code/issues/49373)
- [Claude Code #77537 — first-class steering queue UI](https://github.com/anthropics/claude-code/issues/77537)
- [Claude Code #69124 — Codex-style live steering request](https://github.com/anthropics/claude-code/issues/69124)
- [Claude Code #71726 — Desktop parity: inject queued messages mid-task between tool calls](https://github.com/anthropics/claude-code/issues/71726)
- [Claude Code #30492 — real-time steering priority channel](https://github.com/anthropics/claude-code/issues/30492)
- [hapi #888 — deliver queued messages at step boundaries (harness parity)](https://github.com/tiann/hapi/issues/888)
- [openai/codex #17095 — "Messages to be submitted after next tool call (press esc to interrupt and send immediately)"](https://github.com/openai/codex/issues/17095)
- [Cursor forum — Queue Agent Messages](https://forum.cursor.com/t/queue-agent-messages/110883), [Queued messages interrupt agent](https://forum.cursor.com/t/queued-messages-interrupt-agent/140944)
- [Devin docs — advanced capabilities (follow-ups to running sessions)](https://docs.devin.ai/work-with-devin/advanced-capabilities)
- [AG-UI events reference (no mid-run input event)](https://docs.ag-ui.com/concepts/events), [CopilotKit — the AG-UI event types](https://www.copilotkit.ai/blog/master-the-17-ag-ui-event-types-for-building-agents-the-right-way)

**The common grammar.** Three distinct verbs recur across every mature system:

1. **Steer** (add context / redirect softly): queue the text, inject it as a plain user
   message at the next step/round boundary. Never kills a tool in flight. This is Claude
   Code CLI's and Codex's default.
2. **Interrupt-now** (stop and redirect): abort the current step, then inject. Codex Esc,
   Claude Code Esc. Strictly stronger; loses in-flight tool work.
3. **Queue-for-after** (new turn later): hold until the turn completes, then run as a
   fresh turn. Cursor default, Claude Desktop.

"Stop and redirect" is therefore a *different verb* from "add context" — every system
that conflated them (Cursor's early "send after current message") produced bug reports
about steers derailing the agent. Aura already has verb 3 today (wait for the turn, then
send) and verb 2's destructive half (cancel). This design adds verb 1 and keeps the ladder
explicit.

---

## 2. Semantics

**Definition.** A **steer** is additive operator input for a specific live run, injected
into the in-flight model history as one or more plain `user` messages at the next **round
boundary** — the point where the previous round's tool results have been appended and the
next LLM call has not yet been built (G1/G2). A steer:

- **never interrupts a tool mid-execution** — the executing batch (`executeBatch`,
  llm_agent_dispatch.go:109) always completes and its results are appended first;
- **never extends the Budget** — `AURA_LOOP_MAX_STEPS`/`AURA_LOOP_MAX_WALLCLOCK_SEC`
  (G3) stay the outer bounds. A steer arriving near exhaustion may only shape the
  finalize synthesis (its text is in history when `finalize` runs). Rationale: a steer
  that refilled the budget would be an operator-triggered unbounded run — the same class
  of hazard the 1.3B wallclock cap exists to prevent (runregistry.go:24). If real usage
  shows steers starving, a bounded `AURA_STEER_STEP_BONUS` knob is a one-line follow-up —
  default 0 in any case;
- **queues FIFO, bounded** — multiple steers accumulate in arrival order and ALL drain at
  the next boundary, each becoming its own `user` message (preserves operator intent
  granularity; matches Claude Code's queue flush). The queue is capped
  (`AURA_AGUI_RUN_STEER_MAX`, default 8) and each steer size-capped
  (`AURA_AGUI_RUN_STEER_MAX_BYTES`, default 16384);
- **is not a cancel** — "actually, do X instead" is a steer (the model re-plans from the
  new instruction); "stop everything" is the existing cancel route.

**The escalation ladder** (operator-facing):

| Rung | Verb | Mechanism |
|---|---|---|
| 1 | steer (add context / soft redirect) | this design: inject at next round boundary |
| 2 | steer + skip remaining tools | **does not exist as a distinct mechanism at the chosen seam** — and the study recommends NOT inventing one. At the drain point there is nothing left to skip: the round's batch has already completed (results appended), and no future tools are queued anywhere — the model *decides* the next batch after reading the steer. "Skip remaining" would require ctx-cancelling an executing batch, which is cancel-lite with partial-side-effect semantics (a half-run `shell_exec` is worse than either a finished one or none). The honest ladder is 1 → 3. |
| 3 | cancel (+ optionally re-send as a new turn) | existing `POST /agent/runs/{runID}/cancel` (server_run_resume.go:94-103) + normal send |

**Steer during an ask_user pause is not a steer.** A pause terminates the run (G4): the
RunSession is terminal, the thread lock is free, `live_run_id` clears, and the composer is
already unlocked today. There is no live run to steer. The two affordances in that state
are the existing ones: **answer** the pause (`Resume[]` → `SubmitAnswers`,
runner_resume.go:107) or **send a new message** (a normal turn; rehydration repairs the
dangling ask_user tool_call with synthetic markers — store_helpers.go:221-247, so this is
already well-defined). The steer route answers 404/410 for a paused (terminal) run by
construction of the resolution ladder — no special-casing needed. The UI must simply keep
the two states visually distinct: "answer requested" (pause card) vs "steering available"
(live run).

**Model framing: plain user message, no `[steering]` prefix.** Recommended and justified:

- Claude Code delivers mid-turn input as plain user messages; models are trained on that
  shape (survey §1).
- The loop already injects unannotated user-role text mid-run (recovery nudge, completion
  feedback — G2); a novel annotation dialect would be a fourth framing style with no
  precedent in the codebase.
- Position carries the meaning: a user message sitting after tool results mid-task is
  self-evidently mid-task input.
- Persistence honesty: the persisted turn is byte-identical to what the model saw — no
  visible/model split to maintain (contrast the `modelUserMsg` machinery the attachment
  path needs, server_run.go:70-88).

---

## 3. The loop seam (`internal/agent/llm_agent.go`)

**Exact drain point: the top of the `for` loop (llm_agent.go:230), BEFORE the budget gate
(:251).** One block, placed as the first statement of the loop body:

```go
for {
    a.drainSteers(ic, spanID, parentSpanID, yield)   // NEW — no-op when no inbox/no steers
    // 1. Budget gate BEFORE each LLM call — unchanged (llm_agent.go:251)
    recoveryTurn := skipBudgetGate
    ...
```

Why this exact point and not elsewhere:

- **After dispatch, before the next call** — dispatch returns to the loop head
  (llm_agent.go:481 "the next LLM call sees the appended RoleTool results"), so drained
  steers append *immediately after* the round's RoleTool messages: the model sees
  `[...tool results][steer user msg(s)]` — byte-parity with the Claude Code shape
  ("delivered alongside the next tool result").
- **Before `ConsumeStep`** — the steer round is a normal budgeted step (no gate bypass,
  no new `skipBudgetGate`-style flag), and a steer that arrives just as the budget trips
  is still in history when `finalize` synthesizes (:257), so the final answer can at
  least acknowledge it.
- **Covers the recovery turn too** — the recovery iteration re-enters the loop head, so a
  steer queued during a budget-trip recovery is drained normally.
- **Not inside dispatch** — injecting between sequential results of one batch would
  interleave a user message inside an assistant tool_call/result group, breaking the
  wire-validity invariant the pause machinery works hard to protect (CR-02,
  runner_persist.go:22-28).

**Drain mechanics.** `drainSteers`:

1. `inbox := steer.FromContext(ic.Ctx)` — nil on every non-steer path (CLI, Telegram,
   flag-off web): the function returns immediately. Zero regression by construction.
2. `items := inbox.Drain()` — non-blocking swap-and-return of the FIFO slice. The loop
   **never waits** for steers; a steer only rides if it arrived before the boundary. No
   goroutines added to the agent.
3. Per item, in order:
   `a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: s.Text})`
   — the same tail-append discipline as every existing injection site (G2), then
   `yield(a.steerEvent(ic, spanID, parentSpanID, s), nil)` — a new Event carrying
   `Actions.Steer = &SteerInjected{ID, Text}`. The yield-after-false guard applies
   exactly as for tool events (if the consumer stopped, return without further yields —
   the iter.Seq2 contract, llm_agent.go:375-383).

The Event is the single channel that makes persistence (runner) and wire echo
(translator) fall out of existing seams — mirroring how tool turns already work
(persistEvent observing `Actions.ToolInvocation`, runner_persist.go:96-110).

**How the inbox reaches the agent without import cycles.** New leaf package
`internal/steer` (types + ctx key only, imports `context`/`sync`): `agent` imports it for
`FromContext`; `agui` imports it to construct and attach. `runner` needs no change — the
inbox rides the ctx **as a value**, which survives both the detach
(`context.WithoutCancel` preserves values, server_run_detach.go:32-41 / design §1.4) and
the runner's ctx plumbing into `InvocationContext.Ctx` (runner.go:522-553). This is the
established seam pattern G11 (`WithRequestID`, `WithReasoningOverride`, `WithResponder`,
`WithThreadLockHeld`). A mutable inbox as a ctx value is unusual Go, but the precedent is
already set by `gateway.WithResponder` (a live responder handle) — and the alternative
(threading a field through `runner.Deps`/`Turn`'s signature into `LlmAgentConfig`)
touches three packages for the same result.

**Lifecycle.** The inbox is created per run by `handleRunDetached`, held on the
`RunSession` (the steer route finds it by runID), attached to the detached ctx before
`s.run.Turn(dctx, …)` (server_run_detach.go:105), and closed by `RunSession.finish()`
(runsession.go:198-216) so a post-terminal `Push` fails cleanly (→ §4 races).

---

## 4. API shape

### 4.1 Route

```
POST /agent/runs/{runID}/steer          — mutation, mirrors cancel exactly
Body: {"text": "<operator input>"}      — non-empty, ≤ AURA_AGUI_RUN_STEER_MAX_BYTES
```

- **Resolution:** the same §3.2 owner-scoped ladder as resume/cancel, reused verbatim
  (`resolveRunSession`, server_run_resume.go:27-47): nil registry → 404; malformed runID
  → 404; unknown/reaped → 404; foreign identity → 404. Zero new auth wiring — the route
  mounts inside the agui mux behind the parent `RequireAuth` + `withCORS` like its
  siblings (server_run_resume.go:12-16).
- **Mutation inventory:** `"POST /agent/runs/{runID}/steer": httpMutationMeta("agent_run_steer")`
  in `httpMutationRoutes` (idempotency_http.go:49-114). Idempotency-Key required; the 202
  JSON body is small and bounded, so unlike `agent_run` (terminally indeterminate,
  idempotency_http.go:219-231) the steer operation **completes normally** with a replay
  envelope — a double-click or client retry replays the same 202 instead of double-queuing.
- **Success:** `202 {"status":"queued", "steer_id":"steer-<uuid>", "queued": <n>}` —
  202 because injection is asynchronous (the loop drains at its own boundary), the same
  async-unwind rationale as cancel's 202 (server_run_resume.go:87-103). `steer_id` is the
  correlation key for the wire echo (§5); `queued` is the current queue depth for the UI
  indicator.
- **Refusals:**
  - empty/oversized text → `400`;
  - queue full (≥ `AURA_AGUI_RUN_STEER_MAX`) → `429 + Retry-After` (a politeness bound,
    same posture as the max-live 503, server_run_detach.go:29-30);
  - run **terminal** (still lingering in the registry) → **`410 Gone`**
    `{"error":"run already finished"}`.

### 4.2 The terminal race — recommendation: 410, client re-sends as a normal turn

Two distinct races exist and get one coherent answer — **the server never converts a
steer into a new turn; the client does**:

1. **Steer POST lands after terminal** (run finished between the operator's keystroke and
   the POST): `410 Gone`. The client falls back to a normal `POST /agent/run` send with
   the same text (the composer still owns it). Justification: (a) the server
   auto-starting a turn would need to mint a turn-scoped Idempotency-Key, pick effort/
   skill/attachment context, and drive `Runner.Turn` from a place that today never
   originates turns — the runner's callers are the gateway handlers and channels, and
   keeping turn origination client-owned preserves that invariant (runner.go:155-161);
   (b) 410 already means "the thing you addressed is gone, recovery is a different
   request" in this surface's grammar (replay-gap 410, server_run_resume.go:77-80);
   (c) the fallback is one client-side branch — the text is in hand, `installMutationIdempotency`
   mints the key, and the turn runs with full normal semantics (attachments, effort,
   busy-lock). Cursor's default IS this behavior; we get it as the degraded mode.
2. **Steer accepted (202) but the run reaches terminal before the next boundary**
   (undrained): the steer was never injected, never persisted, and must not be silently
   lost. Contract: injection is **confirmed only by the `aura.steer` echo frame** (§5)
   carrying the `steer_id`. The client tracks acked-but-unechoed steer_ids; when
   `RUN_FINISHED`/`RUN_ERROR` folds with steers still unechoed, it automatically re-sends
   each as a normal new-turn message, FIFO (Claude Code's queue-flush-as-next-turn
   parity). Server side, `RunSession.finish()` drops the inbox — undrained steers leave
   no server-side residue, so the client re-send cannot double-apply.

### 4.3 Flag and knobs

Steering ships behind its own default-off flag, requiring detach:

| Knob | Default | Meaning |
|---|---|---|
| `AURA_AGUI_RUN_STEER` | `false` | Mounts the steer inbox + route. Requires `AURA_AGUI_RUN_DETACH=true` (without a RunSession there is no addressable run). Off ⇒ route 404s (nil-registry rung), no inbox on ctx, agent drain is a structural no-op. |
| `AURA_AGUI_RUN_STEER_MAX` | `8` | FIFO cap per run; over → 429. |
| `AURA_AGUI_RUN_STEER_MAX_BYTES` | `16384` | Per-steer text cap; over → 400. |

Same registration pattern as the five `AURA_AGUI_RUN_*` knobs (config_agui_run.go:23-40,
config_knobs.go:112; env catalog prd.md:5338-5342). Default flips to true only after the
live E2E >9.8, its own amendment line — the 1.3B rollout convention (Amendment #90
point 10).

---

## 5. Wire events

**No new AG-UI protocol event exists to reuse** (G12) — the echo is an Aura extension,
and the translator already has the exact extension grammar for this: named CUSTOM events
(`aura.artifact` translator.go:19, `aura.display` :26).

**Recommendation: one CUSTOM frame `aura.steer`**, emitted by a new translator branch on
`ev.Actions.Steer` (closing any open text/reasoning run first, exactly like the artifact
branch — translator.go:133-140):

```
CUSTOM {name: "aura.steer", value: {steer_id, text}}
```

- **All viewers see it**: the frame flows agent Event → `Translate` → `runProducer` →
  `RunSession.append` → every subscriber + the replay ring (server_run_detach.go:134-147),
  so the original stream, a resumed stream, and a reload-attach replay all render the
  steer at its true position between `TOOL_CALL_RESULT` frames — byte-identical (the §6.3
  redaction-at-insert property; steer text is operator-authored and passes redaction
  unchanged).
- **Injection confirmation**: the presence of the echo is the client's "my steer was
  consumed" signal (§4.2 race 2).
- **Channel-agnostic**: Telegram's subscriber ignores unknown custom events by existing
  convention (D-06, translator.go:13-19), so nothing leaks there.

**The rejected alternative — `TEXT_MESSAGE_START(role="user")` + CONTENT + END** — is
protocol-native (the SDK supports role on start events, translator.go:238) and would be
legible to a hypothetical generic AG-UI client. Rejected for slice 1 because: (a) the
translator's `textRunState` machine is single-assistant-run-scoped
(translator.go:226-259) and would need surgery to open/close a user-role run mid-stream;
(b) the cockpit reducer folds the whole stream into ONE `AssistantTurnState`
(sseResume.ts:143-165 `pumpBody` → `reduceFrame`) — a mid-stream user message would force
a message-splitting rework of the ExternalStore fold, a strictly larger client change
than rendering a steer chip from a custom frame; (c) the cockpit is the only AG-UI
consumer today. Revisit if a second AG-UI client ever materializes.

---

## 6. Persistence model

**The steer user turn is persisted at drain time, through the same seam as tool turns.**
`persistEvent` gains one branch: `ev.Actions.Steer != nil` →
`r.Conv.AppendTurn(ctx, {ConversationID, Role: llm.RoleUser, Content: s.Text})` — beside
the existing `AwaitingInput`/`ToolInvocation` branches (runner_persist.go:93-111).

Why this is exactly right, given G5/G6:

- **Seq placement.** Turns are persisted incrementally as the round runs (assistant
  tool_calls → RoleTool results → …), each allocating MAX(seq)+1 under the conversation
  row lock (store_append.go:204-220). The steer Event is yielded by the agent at the
  drain point — after the round's tool-result Events, before the next round's tool
  events — so the runner appends it at a `seq` that **mirrors the in-memory history
  position by construction**. There is no "in-flight assistant turn" to interleave
  around: the final answer becomes its own later row (`persistAssistantAnswer`,
  runner_persist.go:277-307). Persisted order == model-visible order == wire order.
- **Branch implications: none new.** `AppendTurnParams` carries no branch field (G6); a
  steer turn lands with the same default `branch_id` discipline as the tool-result turns
  around it, whatever the run (linear Turn or a `TurnBranch` re-run,
  runner.go:329-331) — it inherits the surrounding turns' behavior identically because it
  uses the identical write path. A future branch fork above the steer treats it like any
  other body turn (path-fold at read time, store_branch.go).
- **Reload/resume exactness.** After reload, `MESSAGES_SNAPSHOT` shows the persisted
  user turn between the tool turns (handleMessages, server.go:449-497); a 1.3B
  resume/attach replays the `aura.steer` frame at its ring seq. Both views place the
  steer at the same logical point. One honest presentational nuance remains: live/replay
  renders a steer **chip** inside the assistant flow, the snapshot fold renders a
  **user turn** — §7 recommends the snapshot fold also render mid-tool-sequence user
  turns as steer chips so the two views converge (pure client-side mapping; turns
  between an assistant tool_calls turn and the final answer are structurally
  identifiable in the snapshot).
- **Undrained steers are never persisted** — correct, because the model never saw them
  (§4.2); the client re-send persists them as the next turn's user message via the
  normal `appendUserTurn` path (runner.go:367, :500-504).
- **Failure mode:** if `AppendTurn` fails, the turn errors out through the existing
  persist-error path (runner.go:460-462) — same severity as a tool-turn persist failure,
  no new class.

---

## 7. UI + channels

### 7.1 Cockpit composer (web/src/chat)

The 1.3B "disabled while live" state (`sendBlocked`, ExternalStoreChat.tsx:577 +
hint :556-564) becomes the **steer state**:

- **Composer enabled during live runs**; the send affordance switches to a visually
  distinct **steer variant** (icon + accent — per §Frontend_aesthetics keep it a
  deliberate design moment, not a grayed clone; the existing hint row becomes "steering
  the live run…"). Attachments, skill-pin, and effort controls are **disabled** in steer
  mode — a steer is text-only (those are turn-scoped inputs; the 400-on-payload keeps the
  server honest).
- Send while live → `POST /agent/runs/{activeRunId}/steer` (the runId is already tracked:
  `activeRunIdRef`, ExternalStoreChat.tsx:103-104, fed by `onRunId` — sseResume.ts:39,
  :156). Optimistically render the steer chip as pending; confirm on the `aura.steer`
  echo; on `RUN_FINISHED` with unechoed steers, auto re-send as normal turns (§4.2).
- **Queued-steer indicator**: pending chips + the `queued` count from the 202; a pending
  steer can be locally withdrawn only before POST (no server-side unqueue in slice 1 —
  see Open Questions).
- **Stop** keeps its meaning (abort + cancel POST, :246-260) and sits beside the steer
  affordance — the ladder made visible: steer field + Stop button.
- Reducer: `reduceFrame` handles `aura.steer` by appending a `steer` part to the
  assistant turn state (the same additive-part pattern as `aura.display`); the split-
  replay idempotence property test extends to steer frames (design 1.3B §4.1 vitest
  property).

### 7.2 Telegram parity (note only — out of scope for slice 1)

Telegram's mid-turn inbound today gets the busy reply (G10). The seam built here is
channel-agnostic by construction: the inbox rides the **turn ctx**, not the HTTP layer —
`startTurn` (bot_dispatch_turn.go:94-121) could mint an inbox, stash it per chat beside
the `registerTurn` cancel, put it on `turnCtx`, and route mid-turn texts into it instead
of the busy reply (with the echo rendered into the status pane). Single-operator reality
makes this genuinely useful (Telegram is the away-from-desk channel), but it needs its own
UX decisions (what does "queued" look like in chat? does /cancel drop the queue?) —
deferred, seam ready. The busy reply stays until then.

### 7.3 CLI

No steer. The REPL is synchronous and its stdin IS the turn boundary; `FromContext`
returns nil and the drain no-ops (G11 nil-safety).

---

## 8. Reconciliation with the durable swarm-messaging design (approved, UNBUILT)

The 2026-06-29 durable swarm messaging design
(`docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md`, amended
2026-07-04) is ratified-but-unimplemented: `internal/swarm/messaging/` does not exist and
no swarm migration is on disk (verified 2026-07-23 — `internal/swarm/` contains only the
flat in-memory engine). It designs a Postgres-backed message substrate whose vocabulary
overlaps steering:

- task status `waiting_input` — A2A `input-required`: non-terminal, never consumes
  `attempt_count`, "woken transactionally by the arriving reply" (spec :64-66, :303-307);
- an append-only `swarm_messages` log with `direction` ∈ {request, response, system},
  `kind` ∈ {task, reply, status, error}, `correlation_id`/`causation_id`, and a
  per-scope idempotency key (spec :309-338).

### 8.1 Where should the steer queue live? — Recommendation: in-memory, RunSession-adjacent

**Recommended for the first slice: the in-memory inbox on the RunSession** (§3), NOT the
durable substrate. Honest reasoning under the "minimal industrial form" rule:

- **Posture match.** The whole 1.3B foundation the steer rides on is deliberately
  in-memory and migration-free: daemon restart loses the RunRegistry and the client
  degrades to the snapshot (Amendment #90 point 10, runregistry.go:10-17). A steer's
  lifetime is strictly shorter than its run's — a queue that outlives the run has no
  meaning (an undrained steer at terminal is *defined* to bounce back to the client,
  §4.2). Durability would buy exactly nothing for the defined semantics: after a crash
  there is no run to inject into, and the recovery is the same client re-send.
- **Scope containment.** Riding `swarm_messages` would pull an entire unbuilt schema
  (4 tables, leases, fencing, retry machinery) into a feature that needs a bounded
  `[]Steer` and a mutex. That is the "atomic bomb" anti-pattern by name.
- **The one real durability argument** — an operator steers from Telegram while the
  daemon restarts — is void: the run itself dies with the daemon, and its turn's
  persistence (conversation_turns) is already the durable truth.

**The door-open seam (no-rework path to the durable substrate):** `internal/steer`
defines the inbox as a narrow interface, not a struct:

```go
type Steer struct { ID, Text string; CreatedAt time.Time }
type Inbox interface {
    Push(Steer) error          // ErrFull / ErrClosed
    Drain() []Steer            // non-blocking FIFO swap
    Close() []Steer            // terminal: returns the undrained residue for accounting
}
```

The slice-1 implementation is the mutex+slice. If/when `internal/swarm/messaging` lands,
a store-backed implementation satisfies the same interface (Push = idempotent
`swarm_messages` insert with `kind='steer'`; Drain = claim-and-mark within the run's
task), and neither the agent seam (`FromContext(...).Drain()`) nor the route handler
changes. The interface — not the storage — is the contract the PRD amendment ratifies.

### 8.2 Vocabulary map

| Steering concept | Swarm-messaging concept | Fit |
|---|---|---|
| ask_user pause (run terminal, waiting for `Resume[]`) | task `waiting_input` — non-terminal pause, "woken transactionally by the arriving reply", zero attempts consumed | **Same grammar, different lifecycle carrier.** Aura's pause terminates the *run* but not the *conversation-level work*; A2A's `waiting_input` keeps the *task* row alive. If the substrate lands, an Aura pause maps naturally onto `waiting_input` and `SubmitAnswers` onto the transactional wake. |
| steer while tools execute (this design) | **no analog — explicitly.** The swarm design's tasks are either `running` (no input path exists into a running task) or `waiting_input` (work stopped). Input-into-actively-running-work is a grammar the durable design does not have and would need this design's round-boundary semantics grafted on (a worker draining a steer kind between rounds). | gap, stated honestly |
| steer message | `swarm_messages` row: `direction='request'`, new `kind='steer'`, `causation_id` = the message that started the run's task | clean extension |
| steer_id | `swarm_messages.id` + the route's Idempotency-Key ↔ the store's idempotency scope | aligns |

### 8.3 Naming/API choices to align now (avoid future renames)

1. **`steer` is the kind name** — use it verbatim everywhere now (route segment
   `/steer`, normalizer `agent_run_steer`, event `aura.steer`, future
   `swarm_messages.kind='steer'`). The swarm spec's kind set {task, reply, status,
   error} gains `steer` when it lands, not a synonym (`nudge`, `hint`, `input`).
2. **`steer_id` shape `steer-<uuid>`** — mirrors `run-<uuid>` so the §3.2 shape-check
   rung generalizes; a future durable row adopts the uuid part as its PK.
3. **Direction vocabulary** — a steer is a `request` (operator → agent), never a
   `system` message: the framing decision in §2 (plain user message) and the durable
   `direction='request'` agree by construction.
4. **Causation** — the 202 response and echo carry `steer_id` only; if the substrate
   lands, `causation_id` points at the run's originating message — the in-memory `Steer`
   struct deliberately carries no causation field so nothing is invented pre-substrate.

---

## 9. Risks & non-goals

**Risks**

1. **KV-cache invariant — honest answer: NOT broken.** The §Cross-cutting invariant
   (amendment #16/#29, prd.md:1709-1720) protects the three-segment cacheable **prefix**
   (`messages[0]` byte-identical, `[1]` per-identity, `[2]` cached-TTL); conversation
   turns live in the dynamic tail `[3..N]` (prd.md:1718). A steer is a tail append at the
   drain point — structurally identical to the RoleTool appends beside it (G2) — and
   `messages[0..2]` are rebuilt untouched by the builder each call (llm_agent.go:273-302).
   Provider prefix caching still hits everything **before** the injection point; the only
   "cost" is that the tail after the steer differs from a hypothetical steer-less turn —
   which is true of any user input ever. The cache_invariant_audit gate is unaffected.
   One real obligation: the steer must NEVER be routed into `messages[1]`/`[2]` (it is
   per-turn data; landing it in a prefix segment WOULD break the invariant — the
   amendment must pin the tail-only rule).
2. **Prompt-injection surface: unchanged in kind.** A steer is owner-scoped operator
   input on an authenticated mutation route (§4.1 ladder + Idempotency-Key), exactly as
   trusted as the `POST /agent/run` user message the same operator already sends —
   MUSR-01/D-06 owner-scoping means a foreign principal cannot even learn the run exists
   (server_run_resume.go:27-47). No tool output, no third-party content enters through
   this path. The gateway/PEP posture is untouched: a steer that talks the model into a
   mutating tool still crosses the same `gateway.Decide` gate (llm_agent.go:60-62).
3. **Model derailment** — the Claude Code community's own complaint (#63190: steers
   flushing mid-task "derail whatever Claude was doing"). Mitigations: steers are
   operator-initiated (single-operator cockpit, not automation), FIFO-bounded, and the
   escalation ladder keeps "wait for the turn" (compose-and-hold is still possible —
   just don't press steer) and cancel available. Accepting this residual risk IS the
   feature.
4. **Dedup/budget interaction** — an injected steer can make the model re-issue a
   previously-deduped call ("try X again"); the dedup ring
   (`Budget.BeforeToolCall`, llm_agent_dispatch.go:78) will veto an exact repeat within
   its window. Acceptable: the model can vary arguments; the veto is per-fingerprint. A
   steer does not reset the ring (no new bypass surface).
5. **Undrained-steer UX** — a steer during the run's final LLM call never injects and
   bounces to a new turn (§4.2). The user may perceive "it ignored me until the next
   turn". The queued-chip → posted-as-new-turn transition must be visually explicit.
6. **iter.Seq2 discipline** — the drain adds yields inside `Run`; every yield honors the
   stop-contract (G1's consumer-stopped guard). Property/race tests must cover a
   consumer stopping ON a steer echo.

**Non-goals (ratify verbatim)**

- **No interrupt-now rung** (Codex Esc): cancel is the only hard stop; no mid-batch
  tool abort.
- **No steer on the flag-off/request-scoped path**: without a RunSession there is no
  addressable run (`AURA_AGUI_RUN_STEER` requires `AURA_AGUI_RUN_DETACH`).
- **No Telegram/CLI steering in slice 1** (§7.2/§7.3 — Telegram seam-ready, deferred).
- **No swarm/child-agent steering**: the inbox attaches to the ROOT run's ctx; child
  branches (`swarm_spawn` workers) do not drain it — steering a subtree is a future
  design on top of the durable substrate (§8.2's stated gap).
- **No budget extension per steer** (§2), no server-side steer→new-turn conversion
  (§4.2), no durable steer queue (§8.1), no steer editing/unqueue API (see OQ-2).
- **No protocol claim**: `aura.steer` is an Aura extension; upstreaming an
  input-while-running event to AG-UI is not this design's job.

---

## 10. Implementation sizing (SDD tasks, dependency order)

| Task | Content | Reuses 1.3B verbatim | Est. LOC (impl + tests) | Risk |
|---|---|---|---|---|
| **ST-01** `internal/steer` package | `Steer`, `Inbox` interface + mutex impl (Push/Drain/Close, caps), ctx key (`WithInbox`/`FromContext`) | — (new leaf pkg, zero deps) | ~90 + ~150 | low |
| **ST-02** agent drain seam | `Actions.Steer` field on `agent.Event` (event.go), `drainSteers` at loop top (llm_agent.go:230), `steerEvent` constructor; agenttest scripted-client coverage incl. consumer-stop-on-echo + race | loop patterns G1/G2 | ~60 + ~200 | med (iter.Seq2 discipline) |
| **ST-03** runner persistence | `persistEvent` steer branch → `AppendTurn(RoleUser)` (runner_persist.go:93-111); seq-order integration test (steer between tool turns) | AppendTurn path G6 | ~20 + ~120 | low |
| **ST-04** translator echo | `aura.steer` CUSTOM branch (+ closeRuns), golden-frame test | custom-event grammar (translator.go:19-36, :133-140); redact-at-insert §6.3 | ~25 + ~80 | low |
| **ST-05** gateway route + session wiring | `RunSession` inbox field (created in `handleRunDetached`, ctx-attached before `Turn`, closed in `finish`), `handleRunSteer` (ladder + caps + 202/400/410/429), `httpMutationRoutes` entry + coverage-test row | `resolveRunSession` (server_run_resume.go:27-47) called verbatim; `httpMutationMeta` pattern; session lifecycle (runsession.go:198-216) | ~120 + ~250 | med |
| **ST-06** config knobs | `AURA_AGUI_RUN_STEER`, `_STEER_MAX`, `_STEER_MAX_BYTES` (config_agui_run.go + catalog + knob tests) | knob pattern (config_agui_run.go:23-40) | ~30 + ~60 | low |
| **ST-07** web client | Composer steer mode (replaces `sendBlocked`), steer POST + pending chips + echo confirmation + undrained→new-turn fallback, `reduceFrame` `aura.steer` part, snapshot-fold chip convergence (§6), vitest incl. split-replay property with steer frames | `activeRunIdRef`/`onRunId` (ExternalStoreChat.tsx:103, sseResume.ts:156), `installMutationIdempotency`, reducer part pattern | ~280 + ~350 | **high** (UI + fold states) |
| **ST-08** live E2E + flip | Cockpit driver: steer during a long tool-calling run → assert injected user turn seq + echo + final answer honors it; steer-after-terminal → 410 → auto new turn; queue-cap 429; owner-scope 404. Score >9.8, quality-snapshot re-attestation, then the default-flip amendment line | 1.3B E2E harness (RS-08) | scenario code | med |

Dependencies: ST-01 → ST-02 → {ST-03, ST-04} → ST-05 → ST-07 → ST-08; ST-06 parallel
anytime. Total new non-test code ≈ **625 LOC** across 6 packages — comparable to the 1.3B
Tier B itself. No migration (§8.1), no `llm.Message` change, no runner signature change.

---

## 11. PRD amendment checklist

A future amendment (one commit, before implementation) must ratify, verbatim:

1. **Steering semantics**: with `AURA_AGUI_RUN_STEER=true` (requires
   `AURA_AGUI_RUN_DETACH=true`), operator input may be injected into a live run as plain
   `user` messages at the next agent round boundary (after the in-flight tool batch's
   results, before the next LLM call); tools are never interrupted; the Budget
   (`AURA_LOOP_*`) is never extended; steers queue FIFO per run, bounded.
2. **New route `POST /agent/runs/{runID}/steer`** — authenticated, owner-scoped 404
   ladder shared with resume/cancel; body `{"text"}`; added to `httpMutationRoutes` as
   `agent_run_steer` (Idempotency-Key required, replayable 202); refusals 400
   (empty/oversize), 429+Retry-After (queue full), **410 Gone** (terminal run).
3. **Terminal-race contract**: the server never converts a steer into a new turn; a 410
   (or a steer left undrained at `RUN_FINISHED`, detected by a missing echo) is re-sent
   by the client as a normal `POST /agent/run` turn.
4. **Wire echo**: injection is confirmed by a new CUSTOM AG-UI event `aura.steer`
   `{steer_id, text}` emitted at the injection point, buffered in the RunSession ring
   like every frame (resume/replay-exact, redaction-at-insert unchanged).
5. **Persistence**: the steer is persisted as a `user` conversation turn at drain time
   through the standard `AppendTurn` seam (seq under the conversation row lock,
   default branch discipline identical to adjacent tool turns); an undrained steer is
   never persisted. Persisted order == model-visible order == wire order.
6. **Cache invariant addendum**: steers are tail-only (`messages[3..N]`); they MUST
   never be routed into the `[0]`/`[1]`/`[2]` cacheable prefix segments (amendment
   #16/#29 unchanged and reaffirmed).
7. **Pause distinction**: a run paused on `ask_user` is terminal and NOT steerable
   (route 404/410 by construction); the affordances there remain answer (`Resume[]`)
   or a normal new message.
8. **Escalation ladder ratified**: steer (additive) → cancel (existing route). No
   interrupt-now rung, no skip-remaining-tools mechanism.
9. **Steer inbox contract**: the queue is in-memory on the RunSession behind the
   `internal/steer.Inbox` interface (`Push/Drain/Close`); a future durable
   swarm-messaging implementation may replace the storage behind the same interface,
   with `kind='steer'`, `direction='request'` naming pre-aligned (this document §8.3).
   Daemon restart loses queued steers — client re-send is the recovery (1.3B posture).
10. **Env catalog additions**: `AURA_AGUI_RUN_STEER` (default **false**),
    `AURA_AGUI_RUN_STEER_MAX` (8), `AURA_AGUI_RUN_STEER_MAX_BYTES` (16384).
11. **Non-goals ratified**: no Telegram/CLI steering (seam noted for a future slice),
    no swarm/child-agent steering, no budget extension, no durable steer queue, no
    steer un-queue API, `aura.steer` is an Aura extension (no AG-UI protocol claim).
12. **UI contract**: the composer is steer-enabled during live runs (supersedes the
    1.3B "disabled while `live_run_id`" hint of RS-07 §4.2), with a distinct steer send
    variant, pending/confirmed steer indicators, and automatic new-turn fallback.
    Default-flip to `true` only after live cockpit E2E >9.8, in its own amendment line.

---

## 12. Open questions (operator-level, max 3) — RESOLVED 2026-07-23: "Claude Code parity"

> **Operator ratification (2026-07-23):** the target grammar is **Claude Code parity** —
> steer = plain user input queued FIFO and delivered alongside the next tool result,
> nothing more. Applied to the three questions:
> **OQ-1 → NO withdrawal affordance** in slice 1 (CC parity: you send another message,
> you don't recall one). **OQ-2 → cockpit first**; Telegram steering waits for real
> cockpit usage (second slice, same amendment family). **OQ-3 → NO budget bonus knob**;
> "cancel + re-send with a bigger budget" is the sanctioned answer (CC does not extend
> budgets on steer).


1. **Steer withdrawal**: once queued server-side (202), a steer cannot be recalled in
   slice 1 — is a "remove queued steer" affordance (DELETE route + UI) worth its surface
   before first real usage, or do we ship without and observe? (Claude Code shipped years
   without it; the request exists — issue #77537.)
2. **Telegram slice timing**: the seam is channel-agnostic from day one (§7.2) — should
   Telegram steering (replace the busy-reply with queue-and-echo) ride the SAME
   amendment as a second slice, or wait for real cockpit usage data first?
3. **Budget starvation in practice**: if live usage shows steers regularly arriving with
   <3 steps of budget left (steer text present but unactionable), do we want the bounded
   `AURA_STEER_STEP_BONUS` knob (default 0) in a follow-up amendment, or is "cancel +
   re-send with a bigger budget" the sanctioned answer?
