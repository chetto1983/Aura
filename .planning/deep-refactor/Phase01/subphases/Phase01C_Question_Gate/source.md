# Phase01C Source Audit

Status: source-audited and closed on 2026-05-16.

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` Phase 1C | Question gate requirements and closure gates | Durable questions, scoped ask/approval, resume with parent correlation, restart survival | Smoke-only completion or Telegram-only state | read/adopted |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-005 / ADR-022 | Questions as backend primitives | Shared clarification/approval state, narrow request-input action, runtime validation around asking | A broad always-loaded escape hatch that makes the model ask instead of think | read/adopted |
| `D:/Aura/docs/chat-interface-prd.md` question contract | Event and table shape | `question_requested -> chat_questions(waiting) -> question_answered(answered)` | UI-only pending-question state | read/adopted |
| `D:/Aura/internal/storage/runs/questions.go` | Canonical question storage | SQLite question lifecycle with explicit not-found, non-waiting, and channel-mismatch errors | Cache-backed or in-memory-only question status | implemented |
| `D:/Aura/internal/chat/hub.go` | Channel-neutral event persistence | Hub records question request/answer events and validates answer state before appending answer evidence | Appending answer events for duplicate/late/wrong-channel replies | implemented |
| `D:/Aura/internal/agent/loop.go` and `D:/Aura/internal/agent/executor.go` | Runtime pause semantics | `ask_user` is exclusive and pauses the loop without adding a tool result until the user answers | Executing later batched tools after an ask/approval request | implemented |
| `D:/Aura/internal/conversation/system_prompt.go` and `D:/Aura/internal/agent/ask_user_promptfx_test.go` | Ask/no-ask behavior | Prompt contract with cardinal ask cases, no-ask cases, and approval options | Prompt-only behavior without executable regression fixtures | implemented |
| `D:/Aura/internal/channels/telegram/invocation_builder.go` | Channel reply routing | Use durable pending question lookup when process-local waiting state is gone | Relying only on in-memory `ThreadRunStatus` after restart | implemented |
| `D:/Aura/internal/channels/web/chat_service.go` | Live `/api/chat` question persistence | Derive a stable non-empty web thread id from the authenticated user before dispatching to Hub | Treating a successful web reply or `question_requested` event as proof when `chat_questions` failed to persist | implemented after falsification |
| `D:/Aura/internal/agent/tools/registry/registry.go` | Operational log truth for ask_user | Log the `ErrAwaitingUserInput` sentinel as expected awaiting input | Warning-level `tool failed` logs for a successful pause | implemented after falsification |

## Adopted Boundaries

- Canonical store: main SQLite run/event database through `internal/storage/runs`.
- Channel adapters translate replies into `chat.QuestionAnswer`; they do not own
  question truth.
- The agent loop owns exclusive ask semantics; the Hub owns durable event/state
  persistence.
- Web `/api/chat` is a channel too: it must provide stable thread identity before
  using the same canonical question store.
- Container verification uses the test-profile image for deterministic
  question lifecycle evidence and the production `aura` image for boot/health
  evidence.

## Remaining Risk

- Phase01C closes the durable question primitive and runtime exclusive pause.
  More granular per-tool approval policy for every `DestructiveHint` tool can
  be layered in Phase05/Phase06 tool consolidation without changing the
  `chat_questions` contract.
- There is no scripted external Telegram chat fixture in Phase01C. Telegram is
  covered by adapter/resume/fixture tests plus live container startup evidence;
  a future UAT fixture can drive a real Telegram account end to end.
