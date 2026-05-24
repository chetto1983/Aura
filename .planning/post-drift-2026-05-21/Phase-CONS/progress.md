# Phase-CONS Progress

**Role:** progress
**Status:** active after Phase-CTX closure
**Rule:** append one entry per atomic slice after verification. Do not mark a story complete from smoke output.

| Date | Slice | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| 2026-05-24 | Planning repair | self-audited | Added missing `source.md`, `benchmark.md`, and `progress.md`; repaired `plan.md` to include CONS-02..13 and Wave B assistant-ui extension. | No runtime code changed. Next implementation slice: US-CONS-02. |
| 2026-05-24 | US-CONS-02 | passed | `go test ./cmd/aura -run "TestHubBackedWebChatUsesSessionStoreContext|TestHubBackedWebChatSessionStoreScopesThreads|TestHubBackedWebChatCompactsToolResultsBeforeNextTurn|TestWebToolExecutorCarriesVisibleToolContext" -count=1`; `go test ./cmd/aura ./internal/channels/web -run "TestHubBackedWebChat|TestChatService|TestWebToolExecutor|TestExtractLastTextResponseArg" -count=1`; `go test ./internal/channels/web/... ./cmd/aura/... -count=1`; `go test ./internal/agent ./internal/conversation ./internal/chat -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/channels/web --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/web_chat.go cmd/aura/web_chat_test.go internal/channels/web`; `rg -n "webChatSessions|webAgentState|trimWebMessages|webChatIdleTTL|webChatMaxMessages" cmd/aura internal`; `go test ./... -count=1`. | Web now uses `agent.SessionStore` + `conversation.Context`; removed web bespoke session/state helpers. `dupl -t 60 cmd/aura internal/channels/web` still reports a pre-existing clone in `cmd/aura/bootstrap_defaults_test.go`, outside touched files. `bash scripts/check-file-size.sh` could not run here because `/bin/bash` is unavailable; direct LOC check: `cmd/aura/web_chat.go` is 390 LOC and test files are script-exempt. |
| 2026-05-24 | US-CONS-03 | passed | `go test ./internal/agent -run "TestExecuteToolCalls(SuccessPath|ErrorPropagation|SummaryAggregation|UsesConversationIDOption|AppliesToolTimeout|RecordsToolAttempt)$" -count=1`; `go test ./cmd/aura -run "TestHubBackedWebChat(RecordsToolAttempts|TerminatesOnTextResponseTool|UsesSessionStoreContext|SessionStoreScopesThreads|CompactsToolResultsBeforeNextTurn)$|TestExtractLastTextResponseArg" -count=1`; `go test ./internal/agent/... ./cmd/aura/... -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/agent --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/web_chat.go cmd/aura/web_chat_test.go internal/agent/exec_helpers.go internal/agent/exec_helpers_test.go internal/agent/runtask.go internal/agent/runtask_helpers.go`; `rg -n "webToolExecutor|web_chat_executor|TestWebToolExecutor|cleanWebToolList|cleanToolList\(" cmd/aura internal/agent`; `git diff --check`; `go test ./... -count=1`. | Web now dispatches tools through `agent.ExecuteToolCalls`; deleted `cmd/aura/web_chat_executor.go`. Ground truth: web probe captured allowed tool names, `userID=alice`, `conversationID=web:alice:default`, non-empty `runID`, and SQLite `tool_attempts` row. Self-audited slice QA PASS; negative check confirmed stale executor symbols are absent. |
| 2026-05-24 | US-CONS-04A | passed | `go test ./cmd/aura -run "TestHubBackedWebChat|TestExtractLastTextResponseArg" -count=1`; `go test ./internal/agent -run "TestRunTask|TestRuntime|TestToolsProvider" -count=1`; `go test ./internal/agentcore -count=1`; `go test ./cmd/aura -run "TestHubBackedWebChat|TestExtractLastTextResponseArg" -count=1`; `go test ./internal/agentcore ./cmd/aura ./internal/agent ./internal/chat -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./internal/agentcore ./cmd/aura --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 internal/agentcore cmd/aura/web_chat.go cmd/aura/web_chat_test.go`; `git diff --check`; `go test ./... -count=1`. | Introduced `internal/agentcore.Builder` and wired web invocation assembly through it. US-CONS-04 remains open: Telegram builder wiring, feature flag/parity probe, and final story closure are still pending. |
| 2026-05-24 | US-CONS-04B | passed | `go test ./internal/channels/telegram ./internal/channels/telegram/fixture -count=1`; `go test ./internal/agentcore -count=1`; `go test ./internal/channels/telegram/... ./internal/agentcore ./internal/chat ./cmd/aura -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./internal/channels/telegram ./internal/agentcore --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 internal/channels/telegram/invocation_builder.go internal/agentcore`; `git diff --check`; `go test ./... -count=1`. | Telegram invocation assembly now also uses `internal/agentcore.Builder`; Telegram-only rendering, status pane, archive, prompt hot reload, ask_user, and soft budget hooks remain in the adapter. US-CONS-04 remains open for the explicit feature-flag/parity closure. |

## Current Slice

**US-CONS-04C:** add the explicit `AURA_AGENTCORE_BUILDER` parity gate and close US-CONS-04 only after the fixture/byte-parity evidence is recorded.

Pre-edit map:

- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/channels/telegram/fixture/`
- `internal/channels/telegram/invocation_builder.go`
- `cmd/aura/web_chat.go`
- `internal/agentcore/`
- `internal/agent/`
- `internal/chat/`
- `internal/channels/web/`

Dedicated QA target:

- config/env tests for `AURA_AGENTCORE_BUILDER`
- Telegram fixture/byte-parity tests from `benchmark.md`
- parity evidence that both transports still build invocations through `agentcore.Builder`
- `go vet ./...`
- `go build ./...`
- `golangci-lint run <touched packages> --timeout=10m --new-from-rev=HEAD`
- `dupl -t 60 internal/agentcore internal/channels cmd/aura/web_chat*.go`
- `git diff --check`
