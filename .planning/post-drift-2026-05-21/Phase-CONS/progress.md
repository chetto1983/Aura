# Phase-CONS Progress

**Role:** progress
**Status:** active after Phase-CTX closure
**Rule:** append one entry per atomic slice after verification. Do not mark a story complete from smoke output.

| Date | Slice | Status | Evidence | Notes |
| --- | --- | --- | --- | --- |
| 2026-05-24 | Planning repair | self-audited | Added missing `source.md`, `benchmark.md`, and `progress.md`; repaired `plan.md` to include CONS-02..13 and Wave B assistant-ui extension. | No runtime code changed. Next implementation slice: US-CONS-02. |

## Current Slice

**US-CONS-02:** collapse web session/state onto `agent.SessionStore` + `conversation.Context`.

Pre-edit map:

- `cmd/aura/web_chat.go`
- `cmd/aura/web_chat_executor.go`
- `cmd/aura/web_chat_test.go`
- `internal/agent/session.go`
- `internal/conversation/context.go`
- `internal/conversation/tool_compaction.go`
- `internal/channels/web/chat_service.go`
- `internal/channels/web/chat_service_test.go`

Dedicated QA target:

- `go test ./cmd/aura ./internal/channels/web -run "TestHubBackedWebChat|TestChatService" -count=1`
- new CONS-02 tests from `benchmark.md`
- `go vet ./...`
- `go build ./...`
- `golangci-lint run ./cmd/aura ./internal/channels/web --timeout=10m --new-from-rev=HEAD`
- `dupl -t 60 cmd/aura internal/channels/web`
- `git diff --check`
