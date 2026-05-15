# Phase03 Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Fixture diff | `git diff internal/channels/telegram/fixture/testdata/` after `go test -count=1 ./internal/channels/telegram/fixture/` | zero content diff | zero content diff (CRLF/LF warnings only on Windows) | met |
| Channel tests | `go test ./internal/chat ./internal/channels/...` | green | green (5 packages: chat, channels/cron, channels/silent, channels/telegram, channels/telegram/fixture, channels/web) | met |
| Telegram package tests | `go test ./internal/telegram/...` | green | green | met |
| Agent + chat package tests | `go test ./internal/agent/... ./internal/chat/...` | green | green | met |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | green | blocked by unrelated user WIP at internal/api/auth/store.go (new Authorizer.Authorize method not yet implemented by fakes) — Phase 3 scope packages green | scope met / out-of-scope blocker |
| Web API shape | not exercised | response shape unchanged | n/a — this arc did not touch /api/chat | not applicable |
