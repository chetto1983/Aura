# Wave 3.0 — Chat Hub Slice 0: Single Agent Loop

> **Status: SHIPPED pre-Phase01 in the `internal/chathub/` package (now `internal/chat/`). Plan preserved as evidence; implementation evolved through US-A* arc.**

**Status:** plan (not yet implemented)
**Date drafted:** 2026-05-13
**Predecessor:** Wave 2.10.b (tool reconciler), Wave 2.7-fix (action-enum tools, commit `5313397d`)
**Source PRD:** [docs/chat-interface-prd.md](docs/chat-interface-prd.md), §2.1 + §2.4 + §11.4 + Slice 0
**Authoritative decision (§2.4):** Telegram becomes an adapter. `agent.Runner` no longer drives the interactive chat. A new channel-neutral runtime is extracted from `internal/telegram/conversation.go` + `agentruntime/` + `agentloop/`.

**Audit revision 2026-05-13:** consumer-map audit showed that `internal/agentruntime` ALREADY exposes a channel-neutral `Run(Invocation) → Result` with an `OnEvent func(Event)` callback (see [agentruntime/runner.go:81](internal/agentruntime/runner.go#L81)). The original plan in §3.1 proposed a brand-new `internal/agentcore/` package; the revised plan **extends agentruntime in place** instead of creating a parallel package. `internal/agentloop` stays — `internal/telegram/conversation.go` and `internal/agentruntime` both import its `Stats`, `ChatClient`, `ToolExecutor`, `PhantomToolGuard`, `WrapUntrustedToolResult`, so deleting it would force a fan-out we don't want for Slice 0. Smaller diff, lower risk.

---

## 1. Goal

Eliminate the structural confusion the user surfaced 2026-05-13:

- two parallel agent runners (`agent.Runner` vs `agentloop.Loop`) with overlapping responsibilities, distinct knobs, distinct phantom-guard wiring
- Telegram code path doing prompt assembly, context build, tool dispatch, progressive rendering all entangled in `internal/telegram/conversation.go`
- `/api/chat` and Telegram producing different stats, hitting different iteration caps, behaving differently for the same user intent
- knob taxonomy bloat: `AURABOT_MAX_ITERATIONS`, `AURA_AGENT_LOOP_MAX_STEPS`, dead `MAX_TOOL_ITERATIONS`, plus per-tool budgets

After Slice 0:

```
Telegram ─┐
Web/SSE  ─┤            Chat Hub                  ┌─ Telegram
Heartbeat┼─→ Inbound → Normalize → Agent Loop → Outbound ─┼─ Web/SSE
Cron     ─┘            (one runtime,             └─ Heartbeat
                       channel-neutral)
```

**Telegram becomes a thin wrapper.** Web/`/api/chat` becomes another wrapper. Both go through the same `Hub.Receive(InboundMessage) → emit(OutboundEvent)` contract.

---

## 2. Scope decision

### In scope (Slice 0)

1. **New package `internal/chathub/`**: contracts (InboundMessage, OutboundEvent, Principal, Run), state machine (queued/running/waiting_for_user/cancelling/cancelled/completed/failed), event types canonical list.
2. **New package `internal/agentcore/`** (or `internal/agent/runtime/`): the channel-neutral runtime. Extracts prompt assembly, context build, tool dispatch, budget guard from `internal/telegram/conversation.go`. Produces `OutboundEvent` stream via callback, NOT a one-shot result.
3. **Refactor `agent.Runner` to wrap `agentcore`**: keep `agent.Runner` as a compat surface for swarm/background/cron paths that don't need streaming. Internally it calls `agentcore.Run` and synthesizes a one-shot result from the event stream.
4. **Refactor `agentloop.Loop` similarly** OR delete if `agentcore` covers its surface. Decision: investigate during plan; default = delete `agentloop` if it has no consumers `agentcore` can't serve.
5. **Telegram adapter**: `internal/telegram/conversation.go` shrinks to "convert tele.Context → InboundMessage; consume OutboundEvent → progressive Telegram edits". All prompt/context/tool logic moves out.
6. **Web `/api/chat` adapter**: existing `handleChat` rewired to construct an InboundMessage and feed it into the Hub.
7. **Knob consolidation**: one config field `AGENT_MAX_ITERATIONS` (env `AURA_AGENT_MAX_ITERATIONS`). `AURABOT_MAX_ITERATIONS` + `AURA_AGENT_LOOP_MAX_STEPS` deprecated, mapped to the new key during migration; dead `MAX_TOOL_ITERATIONS` row deleted by migration.
8. **Tests**: golden tests proving Telegram + web + heartbeat fixture produce identical agent input for the same prompt, and identical event sequence for the same model reply.

### Out of scope (defer to later slices)

| Item | Slice | Note |
|------|-------|------|
| Persistent `chat_threads` / `chat_messages` tables | 3 | PRD Slice 3 — needs schema migration + sidebar UI |
| SSE streaming endpoint | 4 | PRD Slice 4 — requires event-emit refactor (this slice) as prerequisite |
| Attachments in chat composer | 5 | PRD Slice 5 |
| Question cards / starter / clarification | 6 | PRD Slice 6 |
| `chat_principals` + `chat_channel_accounts` | 3 | needs schema migration |
| React `/chat` route | 1 | PRD Slice 1 — UI shell |
| Removing `agent.Runner` entirely | future | swarm/cron still use it — keep as compat wrapper for now |

**Honest statement**: Slice 0 alone doesn't ship a single user-visible feature. It's pure architectural extraction. The win is enabling every subsequent slice (streaming, attachments, questions) without re-engaging the two-runner confusion. PRD §2.5 explicitly calls this out: "il PRD non è implementabile in modo sicuro se la prima fase prova a costruire la UI sopra /api/chat e poi 'aggiungere' il Chat Hub. Il primo slice deve estrarre il runtime channel-neutral".

---

## 3. Architecture

### 3.1 Package layout post-Slice-0

```
internal/
  chathub/                      NEW
    types.go                    InboundMessage, OutboundEvent, Principal, Run, state machine
    hub.go                      Hub interface + concrete *Hub (registry of inbound + outbound adapters)
    state.go                    Run lifecycle, cancel registry, idempotency
    adapters/
      telegram_inbound.go       tele.Context → InboundMessage
      telegram_outbound.go      OutboundEvent → tele.Bot.Send/Edit (progressive)
      web_inbound.go            chatRequest → InboundMessage
      web_outbound_buffered.go  OutboundEvent → one-shot ChatReply (compat /api/chat)
      web_outbound_sse.go       OutboundEvent → SSE frames (deferred to slice 4)
      heartbeat_inbound.go      scheduled task firing → InboundMessage (silent mode)
      cron_inbound.go           cron job → InboundMessage (silent mode)

  agentcore/                    NEW (moved from telegram/conversation.go + agentruntime/ + agentloop/)
    runtime.go                  Run(ctx, input AgentInput, emit EmitFn) error
    context.go                  buildContext: system prompt, history, overlay, memory hints
    tools.go                    tool dispatch, parallel/sequential, schema-validation, action-error path
    budget.go                   per-turn iteration cap, tool result cap, microcompaction
    phantom_guard.go            (moved unchanged from agentloop/)
    types.go                    AgentInput, EmitFn, Stats

  agent/                        REFACTORED (smaller)
    runner.go                   one-shot wrapper over agentcore for swarm/cron/background
    runner_test.go              proves it produces same result as Telegram adapter for same input

  telegram/                     SHRINKS substantially
    bot.go                      bootstrap, allowlist, doc handler
    conversation.go             SHRINKS: only "tele.Context → Hub.Receive(InboundMessage)" + "OutboundEvent → progressive edit"
    setup.go                    builds Hub + adapters
    documents.go                unchanged (file upload via Telegram)
    streaming.go                only the Telegram-specific progressive-edit logic (rate limit, edit retry)

  agentloop/                    DELETED if no consumers remain
                                OR shrinks to phantom_guard.go + helpers if agentcore can't absorb everything
```

### 3.2 Core contracts

```go
// internal/chathub/types.go
package chathub

type InboundMessage struct {
    ID          string
    Channel     string          // telegram | web | heartbeat | cron
    PrincipalID string          // channel-neutral identity
    ThreadID    string          // empty for one-shot channels (heartbeat first turn)
    Text        string
    Attachments []AttachmentRef
    Question    *QuestionAnswer // when answering a previously-asked question
    Locale      string
    TimeZone    string
    Mode        DeliveryMode    // streaming | deferred | silent | notification
    CreatedAt   time.Time
    ChannelData map[string]any  // adapter-specific metadata; the Agent Loop MUST NOT read this
}

type OutboundEvent struct {
    ID        string
    RunID     string
    ThreadID  string
    Channel   string
    Type      EventType         // run_started | message_created | message_delta | tool_start | tool_end | question_requested | message_done | usage | done | error | cancelled
    Seq       int64             // monotonic per RunID
    MessageID string            // empty until message_created
    Content   string            // delta payload for streaming
    Payload   map[string]any    // typed per Type
    CreatedAt time.Time
}

type Run struct {
    ID            string
    ThreadID      string
    PrincipalID   string
    Status        RunStatus     // queued | running | waiting_for_user | cancelling | cancelled | completed | failed
    Model         string
    StartedAt     time.Time
    CompletedAt   *time.Time
    LastError     string
}

// internal/agentcore/types.go
package agentcore

type AgentInput struct {
    Message      InboundMessage
    Conversation *conversation.Context
    Tools        *tools.Registry
    SystemPrompt string
    Model        string
    Limits       Limits
}

type EmitFn func(event chathub.OutboundEvent) error

// Run is the single channel-neutral entry point. Calls emit() for every
// delta, tool event, question, error, usage, done. Returns when the run
// reaches a terminal state. The caller (adapter) decides what to do with
// each event.
func Run(ctx context.Context, in AgentInput, emit EmitFn) (Stats, error)
```

### 3.3 Migration shim — keeping `agent.Runner` working

`agent.Runner.Run()` becomes:

```go
func (r *Runner) Run(ctx context.Context, task Task) (Result, error) {
    var result Result
    var content strings.Builder
    emit := func(ev chathub.OutboundEvent) error {
        switch ev.Type {
        case chathub.EventMessageDelta:
            content.WriteString(ev.Content)
        case chathub.EventMessageDone:
            result.Content = content.String()
        case chathub.EventToolStart:
            result.ToolCalls++
        // ...
        }
        return nil
    }
    stats, err := agentcore.Run(ctx, agentInputFromTask(task), emit)
    result.Stats = stats
    return result, err
}
```

Swarm, cron, background paths keep using `agent.Runner` and don't see the refactor. The Telegram and web adapters skip the wrapper and use `agentcore.Run` directly with their own emit.

---

## 4. Knob consolidation

| Today | After Slice 0 |
|-------|--------------|
| `AURABOT_MAX_ITERATIONS` (env, settings) | DEPRECATED, mapped to new `AGENT_MAX_ITERATIONS` |
| `AURA_AGENT_LOOP_MAX_STEPS` (env, settings) | DEPRECATED, mapped to new `AGENT_MAX_ITERATIONS` |
| `MAX_TOOL_ITERATIONS` (settings, orphan) | DELETED by migration |
| Per-tool `MaxCallsPerTool` (in code) | UNCHANGED — orthogonal concern, valid |
| `AURABOT_TIMEOUT_SEC` | UNCHANGED — orthogonal |

Migration step:
- Read SQLite `settings` for `AURABOT_MAX_ITERATIONS`, `AURA_AGENT_LOOP_MAX_STEPS`, `MAX_TOOL_ITERATIONS`
- Pick MAX(values) → write as `AGENT_MAX_ITERATIONS`
- Delete the three old rows
- Log the consolidation choice

---

## 5. Slice 0 step-by-step

### Step 1 — Plan + contracts (DAY 1, ~3h)
- Write `internal/chathub/types.go` with InboundMessage, OutboundEvent, Run, state machine constants
- Write `internal/agentcore/types.go` with AgentInput, EmitFn, Stats
- Write golden table of all current "what does the agent do for input X" cases that must still work post-refactor (read from `conversations` SQLite for the last 100 Telegram turns + last 20 chat-cli calls)

### Step 2 — Extract agentcore (DAY 1-2, ~6h)
- Move prompt building from `telegram/conversation.go` to `agentcore/context.go`
- Move tool dispatch loop to `agentcore/tools.go`
- Move budget/microcompaction to `agentcore/budget.go`
- Move phantom guard to `agentcore/phantom_guard.go` (or leave in agentloop and import — decide based on consumer count)
- `agentcore.Run` becomes the single function that owns the loop
- Add `EmitFn` so callers see every step (no more one-shot return)
- Tests: feed in golden inputs from Step 1, assert event sequences match what Telegram produced before

### Step 3 — Adapters (DAY 2-3, ~5h)
- `chathub/adapters/telegram_inbound.go`: extract from `telegram/conversation.go`
- `chathub/adapters/telegram_outbound.go`: extract progressive-edit logic from `telegram/streaming.go`
- `chathub/adapters/web_inbound.go` + `web_outbound_buffered.go`: extract from `api/chat.go`
- `Hub.Receive` routes inbound to `agentcore.Run` and forwards events to the matching outbound adapter

### Step 4 — Wire it together (DAY 3, ~4h)
- `telegram/setup.go` builds the Hub at startup, registers Telegram adapters
- `api/router.go` constructs InboundMessage from the chat request and calls `Hub.Receive`
- `agent.Runner` becomes the buffered-event shim described in §3.3
- Run the existing test suite (48 packages) — every existing test should pass without touching its assertions (the contract is preserved)

### Step 5 — Knob consolidation (DAY 4, ~2h)
- Add `AGENT_MAX_ITERATIONS` to config
- Migration to consolidate three legacy keys
- Deprecation warnings in logs when old env vars are set

### Step 6 — Live verify (DAY 4, ~3h)
- Telegram bot still answers; behavior unchanged
- `/api/chat` still works; stats are stable
- Heartbeat task fires → silent inbound message → run completes → wiki updated. (Heartbeat doesn't exist yet — only stub the inbound channel; full heartbeat is post-Slice 0.)
- Pipe a real chat through and compare event sequence with pre-Slice-0 baseline

### Step 7 — Cleanup (DAY 4-5, ~3h)
- Delete `internal/agentloop/` if no consumers remain
- Delete dead `MAX_TOOL_ITERATIONS` SQLite row
- Update `docs/llm-wiki.md` and CLAUDE.md architecture section
- Commit + memory

**Total estimated effort: 5 working days.** Pessimistic — could be 7 if Telegram's progressive-edit has hidden coupling.

---

## 6. Risks + mitigations

| Risk | Mitigation |
|------|------------|
| Behavior drift between old Telegram path and new chathub path | Golden test in Step 1 — record current behavior on 100 real Telegram turns + 20 chat-cli calls, replay through chathub, diff outputs |
| Progressive Telegram edit (rate-limited, retryable) is tightly coupled to the conversation flow | Extract first to `telegram/outbound_edit.go` (still in telegram package), THEN move to chathub adapter. Two-step move reduces coupling-risk surface |
| Swarm uses `agent.Runner` heavily | Keep `agent.Runner` API stable; only its internals change to wrap `agentcore`. Swarm code untouched |
| Settings migration corrupts existing installs | Migration is additive (writes new key, doesn't delete until next release). Three-rev deprecation cycle for old keys |
| New abstractions encourage premature generalization | PRD §20: "Se il Chat Hub diventa troppo astratto troppo presto, può rallentare la feature. Il contratto deve restare piccolo." Hard cap: max 5 types in chathub/types.go, no factory pattern, no plugin system |
| The refactor breaks the wave-2.10.b reconciler / wave-2.10 install / wave-2.9 markitdown wiring | All those touch the registry/setup, not the agent loop. Risk is low but explicit cross-check in Step 6 |

---

## 7. Done criteria

Wave 3.0 Slice 0 ships when **all** of:

1. `internal/chathub/` package exists with the 5 types listed in §3.2 + Hub interface + concrete implementation + ≥10 unit tests
2. `internal/agentcore/` package exists; `agentcore.Run` is the single entry point; ≥15 unit tests cover prompt/context/tool-dispatch/budget paths
3. `agent.Runner.Run` internally calls `agentcore.Run`; same external API; existing `agent_test.go` passes without modification
4. `agentloop/` is either deleted OR shrunk to phantom-guard + helpers only
5. `internal/telegram/conversation.go` is ≤200 LOC (was much larger); it imports chathub and only adapts tele.Context ↔ InboundMessage/OutboundEvent
6. `api/chat.go` constructs an InboundMessage and calls `Hub.Receive`; the existing ChatReply response is built from buffered outbound events
7. `AGENT_MAX_ITERATIONS` is the single iteration knob; migration applied; deprecation warnings logged
8. `cmd/probe_chat` passes against the new path with identical results to the prior baseline (golden table from Step 1)
9. Telegram bot live test: send "Ciao" → reply within ≤3s, no behavior regression
10. `/api/chat` live test: same simple prompt → same reply shape, stable stats
11. `go test ./...` 50+ packages green (new chathub + agentcore = +2)
12. `docs/wave-3-chathub-slice0.md` (this file) updated to "Status: shipped" + commit hash
13. Updated `docs/chat-interface-prd.md` Slice 0 status: done

---

## 8. Open scope questions (close before Step 1)

1. **Is `internal/agentloop` deletable?** Need consumer-count audit. If `mcp`, `aurabot`, `swarm` use it, keep it as a wrapper. If only `agent.Runner` uses it, delete.

2. **Do we extract `phantom_guard` to `agentcore` or leave in `agentloop`?** The guard is generic (tool-name vs called-this-turn). Moving to `agentcore` is cleaner but creates a small breakage risk for any external test that imports it. **Reco:** move to `agentcore` since `agentloop` is being deleted anyway.

3. **Do we keep `agent.Runner` or absorb it into chathub?** Keeping it lets swarm/cron stay unchanged. Absorbing it would force a bigger refactor. **Reco:** keep `agent.Runner` as a buffered-event shim; revisit removal in a future slice.

4. **Heartbeat + Cron inbound — stub or real?** PRD says these are channels too, but Slice 0 only needs the *contract* not the implementations. **Reco:** stub adapters that return "not implemented" → keeps the type system happy and surface area visible without committing to behavior in this slice.

5. **Knob migration: aggressive or conservative?** Aggressive = delete old keys + env vars in one release. Conservative = log deprecation warnings + remove in 2-3 releases. **Reco:** conservative — operator's `compose.yaml` may pin the old env vars; respect it for one release.

---

## 9. What this unlocks

After Slice 0 lands:

- **Slice 1 (UI shell)**: React `/chat` route can be built against `Hub.Receive` directly — no special `/api/chat` v2 endpoint needed.
- **Slice 2 (non-streaming web chat)**: web inbound adapter already exists; just hook up the UI.
- **Slice 3 (persistence)**: `chat_threads` / `chat_messages` tables live in chathub; Telegram and web both populate them through the same code.
- **Slice 4 (streaming)**: swap the `web_outbound_buffered.go` for `web_outbound_sse.go`. Agent emits the same events; only the outbound adapter format changes.
- **Slice 5 (attachments)**: `AttachmentResolver` is a chathub concern; works the same for Telegram documents and web uploads.
- **Slice 6 (questions)**: `question_requested` is already in the EventType list; UI + backend question state machine layers on top.

Slice 0 is the keystone. Without it, every subsequent slice has to re-fight the runner duplication and the Telegram coupling.

---

## 10. Decision request

Before kicking off Step 1 I need confirmation on:

1. **Confirm scope** — §2 in/out matches what you want?
2. **Effort + timing** — ~5 working days for Slice 0. Acceptable, or split it into 0.a (chathub contracts + agentcore extraction) + 0.b (Telegram migration + knob consolidation)?
3. **Open scope questions** — §8 points 1-5. Use my recos as defaults?
4. **Order vs. other waves** — Slice 0 vs. Wave 2.10.c (MCP reload) vs. Wave 2.9.5 (OCRBackend) vs. Wave 2.11 (React wizard). Slice 0 unblocks Wave 2.11. Mio voto: Slice 0 first, then 2.11 on top of it (using the chathub contracts), then 2.10.c / 2.9.5.
