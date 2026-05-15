# Phase 01C — Question Gate Scope (v2 — nanobot-aligned)

**Date:** 2026-05-15
**Status:** scope proposal v2, awaiting user confirmation before queue creation
**References:**
- PRD §5.2 + §5.2.5 (the contract)
- `D:/tmp/nanobot/nanobot/agent/tools/ask.py` (working reference implementation)
- LangGraph `interrupt()` pattern (industry SOTA 2025-2026)
- OpenAI Codex `approval_policy` system

---

## What changed between v1 and v2

**v1 proposed** a custom `<request_input/>` action grammar parsed from assistant content + a new `chat_questions` SQLite table + 4 new event types + a separate runtime pause state machine. ~5 slices, ~7.5h.

**v2 (this doc)** adopts the **nanobot pattern**: `ask_user` is a regular tool. The model emits a standard tool_call. The runtime pauses (sets `run.status = waiting_for_user` — that column exists post Phase 1A). User's next reply becomes a standard `tool_result` message. No new table, no new event types, no custom grammar. Channel rendering branches on capability (numbered text for Telegram v1; buttons for v2). ~3 slices, ~3-4h.

**Why the pivot:**
1. Working reference (nanobot ships this pattern in production Python code)
2. LangGraph's `interrupt()` + `Command(resume=...)` confirms the industry direction
3. Resume-via-tool_result is a STANDARD mechanism — no run-state machine to invent
4. The state Aura needs (pending question, options, expiry) lives in the EXISTING `run_events` ledger as a tool_call entry without a matching tool_result. PRD §5.2 already calls this "the durable causal record."
5. Channel adapters change shape (button list vs numbered text) but the agent-side protocol stays the same

## PRD §5.2.5 reconciliation

PRD warns: *"Aura must not expose a broad `ask_user` tool as an always-loaded escape hatch. That pattern risks making the model passive."*

**v2 response:** the tool is loaded, but the system prompt is **prescriptive** about when to use it:
- Cardinal cases that justify `ask_user` (the criteria from PRD §5.2.5 verbatim)
- Cardinal non-cases (clear instruction; resolvable by tools/context/wiki)
- Concrete emit examples + counter-examples

The prompt is the policy. No runtime gate enforces it; the model's training + the prompt criteria do. If production shows the model over-asks, tune the prompt. If under-asks, also tune the prompt. The prompt is the dial.

**Counterweight (optional, v2+):** if the model emits `ask_user` repeatedly in a single run without making progress (>3 questions in 5 iterations), the runtime can refuse the 4th and return a synthetic tool_result "you have asked 3+ times already; resolve with available tools and context first." This is a LIGHTWEIGHT loop-guard, not a policy gate. NOT in v1.

## v2 Scope (3 slices)

### Slice 1 — `ask_user` tool definition + agent loop pause/resume
- New tool in `internal/agent/tools/registry/ask_user.go` implementing `Tool` interface:
  - Name: `ask_user`
  - Description: PRD-aligned (when to use, when not to use)
  - Parameters: `question` (string, required), `options` (array of strings, optional 2-4), `kind` (enum `clarification` | `approval`, default `clarification`)
- `Tool.Execute()` returns a SENTINEL error type `tools.ErrAwaitingUserInput` carrying the question + options
- `internal/agent/loop.go` detects this sentinel error from executor return:
  - Set `Run.Status = "waiting_for_user"` (column already exists)
  - Set `Stats.StopReason = "waiting_for_user"`
  - Return from runLoop without further iterations
  - The tool_call entry is preserved in `run_events` (no matching tool_result yet — that's the pending state)
- Channel-neutral helper `agent.PendingAskUserCall(events []runs.Event) (toolCallID, options []string, kind string, ok bool)` walks the event log and returns the open ask_user call if present (mirrors nanobot's `pending_ask_user_id`)
- Tests: tool definition shape, pause behavior on Execute, helper detects pending vs resolved, helper returns nothing when ask_user not present

### Slice 2 — System prompt teaches when to use `ask_user`
- Add "Clarification and Approval Protocol" section to the canonical system prompt (in `internal/agent/promptplan.go` or `internal/conversation/system_prompt.go`):
  - **Cardinal cases (DO emit):** missing required slot, ambiguous viable interpretations, irreversible destructive action (delete/forget/overwrite), permission escalation, durable user-memory write without explicit intent, 3+ recoverable tool failures
  - **Cardinal non-cases (DO NOT emit):** clear instruction, low-risk reversible default, can be resolved by reading wiki/context, can be resolved by a search tool
  - **Approval vs clarification:**
    - `clarification` = generated 2-4 context-specific options + optional free-text
    - `approval` = canonical options `approve_once|approve_session|approve_persist|deny|cancel`
  - **Concrete examples** (verbatim, copy-paste-ready for the LLM): 1 clarification (missing slot), 1 approval (irreversible delete), 2 counter-examples (clear instruction, ambiguity resolvable by tool)
- Eval fixture in `internal/agent/ask_user_promptfx_test.go`: a list of (user_prompt, expected_emit, rationale) pairs documenting the contract. Not LLM-runtime in CI; serves as the contract for future eval slices.

### Slice 3 — Telegram channel rendering + reply routing
- `internal/channels/telegram` outbound: when the agent loop emits an event indicating `ask_user` was executed (or pre-emission, when streaming `tool_start`), render the question:
  - With options: `"❓ {question}\n\n1. {opt_a}\n2. {opt_b}\n3. {opt_c}\n\n(reply with number, or text for free input)"`
  - Without options: `"❓ {question}\n\n(reply with text)"`
- `internal/channels/telegram` inbound: when chat.Hub reports `run.Status == "waiting_for_user"` for the active run, parse the user reply:
  - Numeric `1..N` → select option N (1-indexed) → tool_result message `{tool_call_id: <pending>, name: "ask_user", content: <option_text>}` injected into next agent loop call
  - Free-text → tool_result message with the text as content
  - Out-of-range number → emit a clarification "please reply with 1..N or free text" (synthetic message, doesn't consume the pending question)
- Run resumes on the next `chat.Hub.Receive` for the same thread: loop sees the resolved tool_call (now has a tool_result) and continues from there
- Tests: numbered reply, free-text reply, out-of-range rejection, run state transition

## E2E rationale (Telegram numbered-text NOT inline keyboard)

`cmd/probe_chat` can automate text replies (`"2"`, `"the workspace one"`) over the HTTP API. Inline keyboard taps would require a Telegram client emulator — much heavier. Numbered text gives us full E2E coverage from the existing probe harness.

For v2 (post Phase 1C): add `tele.ReplyMarkup` inline keyboard rendering as an alternate channel format when not under test. The agent protocol stays the same.

## Effort

| Slice | LOC est. | Effort | Risk |
|---|---|---|---|
| 1 (ask_user tool + loop pause) | ~200 | 2h | Low (additive, no existing behavior changes) |
| 2 (system prompt section + fixture) | ~150 (prose + fixture) | 1h | Low |
| 3 (Telegram rendering + reply routing) | ~180 | 1.5h | Med (inbound state-machine) |
| **Total** | **~530** | **~4.5h** | |

E2E probe coverage is the natural next slice but can also be folded into Slice 3 by extending an existing `cmd/probe_chat` case rather than a new one.

## What "DONE" looks like

After 3 slices:
- A user asks "delete source X" → LLM emits `ask_user{kind:approval, options:[approve_once, deny, cancel]}` → Telegram shows numbered text → user replies "3" → tool_result "cancel" injected → LLM cancels the deletion
- A user gives an ambiguous instruction → LLM emits `ask_user{kind:clarification, options:[a,b,c]}` → user picks → LLM proceeds
- The pending state survives process restart (the run_events ledger has the unresolved tool_call; on resume, the loader sees `run.Status == waiting_for_user` and the next inbound is treated as the tool_result)
- The system prompt's cardinal cases + examples are documented and have an eval fixture for future tuning

Phase 1 closes when this ships.

## Out of scope for v1 (DEFER)

- Web dashboard question card UI (Phase 3 / channel work)
- Telegram inline keyboard (`tele.ReplyMarkup`) — text-numbered enough for v1 E2E
- Multi-select clarifications
- `secret_input` kind (no buttons, redacted answer)
- Repeat-ask loop-guard (>3 questions in 5 iterations → synthetic deny)
- Question expiry / timeout policy (relying on natural inbound timeout)

## Approval required

User: confirm v2 scope above OR redirect. Once confirmed I open `prd.json` as Phase-D queue with 3 slices and launch Ralph.
