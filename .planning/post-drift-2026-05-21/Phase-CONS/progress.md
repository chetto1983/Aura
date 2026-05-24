# Phase-CONS Progress

**Role:** progress
**Status:** active after Phase-CTX closure
**Rule:** append one entry per atomic slice after verification. Do not mark a story complete from smoke output.

| Date | Slice | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| 2026-05-24 | Planning repair | self-audited | Added missing `source.md`, `benchmark.md`, and `progress.md`; repaired `plan.md` to include CONS-02..13 and Wave B assistant-ui extension. | No runtime code changed. Next implementation slice: US-CONS-02. |
| 2026-05-24 | US-CONS-02 | passed | `go test ./cmd/aura -run "TestHubBackedWebChatUsesSessionStoreContext|TestHubBackedWebChatSessionStoreScopesThreads|TestHubBackedWebChatCompactsToolResultsBeforeNextTurn|TestWebToolExecutorCarriesVisibleToolContext" -count=1`; `go test ./cmd/aura ./internal/channels/web -run "TestHubBackedWebChat|TestChatService|TestWebToolExecutor|TestExtractLastTextResponseArg" -count=1`; `go test ./internal/channels/web/... ./cmd/aura/... -count=1`; `go test ./internal/agent ./internal/conversation ./internal/chat -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/channels/web --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/web_chat.go cmd/aura/web_chat_test.go internal/channels/web`; `rg -n "webChatSessions|webAgentState|trimWebMessages|webChatIdleTTL|webChatMaxMessages" cmd/aura internal`; `go test ./... -count=1`. | Web now uses `agent.SessionStore` + `conversation.Context`; removed web bespoke session/state helpers. `dupl -t 60 cmd/aura internal/channels/web` still reports a pre-existing clone in `cmd/aura/bootstrap_defaults_test.go`, outside touched files. `bash scripts/check-file-size.sh` could not run here because `/bin/bash` is unavailable; direct LOC check: `cmd/aura/web_chat.go` is 390 LOC and test files are script-exempt. |
| 2026-05-24 | US-CONS-03 | passed | `go test ./internal/agent -run "TestExecuteToolCalls(SuccessPath|ErrorPropagation|SummaryAggregation|UsesConversationIDOption|AppliesToolTimeout|RecordsToolAttempt)$" -count=1`; `go test ./cmd/aura -run "TestHubBackedWebChat(RecordsToolAttempts|TerminatesOnTextResponseTool|UsesSessionStoreContext|SessionStoreScopesThreads|CompactsToolResultsBeforeNextTurn)$|TestExtractLastTextResponseArg" -count=1`; `go test ./internal/agent/... ./cmd/aura/... -count=1`; `go vet ./...`; `go build ./...`; `golangci-lint run ./cmd/aura ./internal/agent --timeout=10m --new-from-rev=HEAD`; `dupl -t 60 cmd/aura/web_chat.go cmd/aura/web_chat_test.go internal/agent/exec_helpers.go internal/agent/exec_helpers_test.go internal/agent/runtask.go internal/agent/runtask_helpers.go`; `rg -n "webToolExecutor|web_chat_executor|TestWebToolExecutor|cleanWebToolList|cleanToolList\(" cmd/aura internal/agent`; `git diff --check`; `go test ./... -count=1`. | Web now dispatches tools through `agent.ExecuteToolCalls`; deleted `cmd/aura/web_chat_executor.go`. Ground truth: web probe captured allowed tool names, `userID=alice`, `conversationID=web:alice:default`, non-empty `runID`, and SQLite `tool_attempts` row. Self-audited slice QA PASS; negative check confirmed stale executor symbols are absent. |

## Current Slice

**US-CONS-04:** promote `agentcore.Builder` to own full `agent.Invocation` construction.

Pre-edit map:

- `internal/channels/telegram/invocation_builder.go`
- `cmd/aura/web_chat.go`
- `internal/agentcore/` (planned)
- `internal/agent/`
- `internal/chat/`
- `internal/channels/web/`

Dedicated QA target:

- feature-flagged parity tests from `benchmark.md`
- legacy vs `AURA_AGENTCORE_BUILDER=true` transcript comparison
- `go vet ./...`
- `go build ./...`
- `golangci-lint run <touched packages> --timeout=10m --new-from-rev=HEAD`
- `dupl -t 60 internal/agentcore internal/channels cmd/aura/web_chat*.go`
- `git diff --check`
