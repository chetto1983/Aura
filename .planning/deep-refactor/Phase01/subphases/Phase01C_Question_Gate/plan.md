# Phase01C Plan - Add the Question Gate

Status: self-audited scaffold. Not verified.

## Goal

Support clarification and approval without making the agent ask instead of
think.

## Scope

- Durable `chat_questions` or equivalent question state linked to run events.
- Events for question/approval requested and answered.
- `QuestionGate` before risky tool execution and durable memory writes.
- Narrow structured `request_input` action.
- Channel replies routed back to the same question id.

## Non-Goals

- Do not add a broad always-loaded question tool.
- Do not create a Telegram-only approval path.
- Do not make clear instructions ask needless questions.

## PRD Coverage

| PRD Item | Plan Location | Benchmark Location | Source Evidence | Status |
| --- | --- | --- | --- | --- |
| Durable question state | this file | `benchmark.md` | `source.md` | planned |
| Question and approval events | this file | `benchmark.md` | `source.md` | planned |
| Gate before risky tools/memory | this file | `benchmark.md` | `source.md` | planned |
| Structured request input | this file | `benchmark.md` | `source.md` | planned |
| Resume same or correlated run | this file | `benchmark.md` | `source.md` | planned |

## Implementation Gates

- Clear instructions do not ask needless questions.
- Missing required slots produce one scoped question.
- Risky irreversible tool calls produce approval.
- Question state survives process restart.
