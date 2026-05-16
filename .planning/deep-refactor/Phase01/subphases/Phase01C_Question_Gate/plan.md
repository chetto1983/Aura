# Phase01C Plan - Add the Question Gate

Status: closed E2E on 2026-05-16 and CI-verified after push
(`ecb4cf3e`, GitHub Actions run `25958870299`).

## Goal

Support clarification and approval without making the agent ask instead of
think.

## Closed Slice

- SQLite `chat_questions` is the canonical pending/answered question state,
  linked to `run_events` through the question event id.
- `question_requested` records a durable waiting question with thread, channel,
  kind, prompt, options, blocking metadata, producer metadata, and expiry /
  fallback fields.
- `question_answered` records a correlated answer event and closes the matching
  question row.
- The agent loop treats `ask_user` as exclusive: if a model batches `ask_user`
  with other tools, only `ask_user` is retained/executed and the run pauses.
- The composed prompt carries the clarification/approval protocol and a fixture
  table covering ask and no-ask cases.
- Telegram replies route through the same durable question id. If process-local
  conversation state is gone after restart, the adapter falls back to the
  durable pending `chat_questions` row and records the answer before resuming.
- Late, duplicate, and wrong-channel answers are explicit errors and do not
  append misleading `question_answered` events.

## Source Files

- `D:/Aura/internal/db/migrations/migrations.go`
- `D:/Aura/internal/db/migrations/migrations_test.go`
- `D:/Aura/internal/storage/runs/questions.go`
- `D:/Aura/internal/storage/runs/store_test.go`
- `D:/Aura/internal/chat/types.go`
- `D:/Aura/internal/chat/hub.go`
- `D:/Aura/internal/chat/hub_test.go`
- `D:/Aura/internal/agent/loop.go`
- `D:/Aura/internal/agent/executor.go`
- `D:/Aura/internal/agent/runtime.go`
- `D:/Aura/internal/agent/pending_ask.go`
- `D:/Aura/internal/agent/ask_user_test.go`
- `D:/Aura/internal/agent/ask_user_promptfx_test.go`
- `D:/Aura/internal/conversation/system_prompt.go`
- `D:/Aura/internal/channels/telegram/ask_user_resume.go`
- `D:/Aura/internal/channels/telegram/ask_user_resume_test.go`
- `D:/Aura/internal/channels/telegram/invocation_builder.go`
- `D:/Aura/internal/channels/web/chat_service.go`
- `D:/Aura/internal/channels/web/chat_service_test.go`
- `D:/Aura/internal/agent/tools/registry/registry.go`
- `D:/Aura/internal/agent/tools/registry/registry_test.go`

## PRD Coverage

| PRD Item | Implementation | Benchmark | Status |
| --- | --- | --- | --- |
| Durable question state | `chat_questions` migration plus `internal/storage/runs/questions.go` | migration/store/hub tests in `benchmark.md` | closed |
| Question and approval events | `chat.Hub` persists `question_requested` and `question_answered` run events | hub tests assert event counts and causation id | closed |
| Gate before risky tools/memory | `ask_user` protocol plus runtime exclusive pause prevents later batched tools from executing before approval/clarification | agent loop and prompt contract tests | closed for Phase01C primitive |
| Structured request input | `ask_user(question, options, kind)` with bounded clarification/approval kinds | agent/tool/prompt tests | closed |
| Resume same or correlated run | answer runs append `question_answered` with `causation_id=<question_id>` and Telegram durable-pending resume works after store reopen | hub restart test and Telegram resume helper test | closed |
| Late/duplicate/wrong-channel states | store/hub reject non-waiting and channel-mismatched answers before event append | store and hub duplicate/wrong-channel tests | closed |
| Live web pipe question persistence | `/api/chat` supplies `ThreadID=web:<user>` so Hub can persist `chat_questions` in production | live `cmd/probe_chat` ask_user probe plus SQL checks on `chat_questions`, `run_events`, and `runs` | closed after falsification fix |
| ask_user sentinel observability | Registry treats `ErrAwaitingUserInput` as an expected pause, not a tool failure | registry log regression test and final production logs | closed after falsification fix |
| Pushed CI | commit `ecb4cf3e` on `origin/master` | GitHub Actions run `25958870299` | closed |

## Non-Goals Preserved

- Do not create a Telegram-only approval path.
- Do not store question state in cache.
- Do not make `ask_user` the default way to avoid reasoning; the prompt
  contract includes clear no-ask cases.
- Do not run broad Phase 5/6 tool-risk policy redesign inside Phase01C. The
  closed primitive is the durable question/answer gate and exclusive pause
  behavior; later tool consolidation can add richer per-tool policy on top.
