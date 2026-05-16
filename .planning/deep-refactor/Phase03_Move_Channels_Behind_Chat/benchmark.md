# Phase03 Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Fixture diff | `git diff internal/channels/telegram/fixture/testdata/` after `go test -count=1 ./internal/channels/telegram/fixture/` | zero content diff | zero content diff (CRLF/LF warnings only on Windows) | met |
| Channel tests | `go test ./internal/chat ./internal/channels/...` | green | green (5 packages: chat, channels/cron, channels/silent, channels/telegram, channels/telegram/fixture, channels/web) | met |
| Telegram package tests | `go test ./internal/telegram/...` | green | green | met |
| Agent + chat package tests | `go test ./internal/agent/... ./internal/chat/...` | green | green | met |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | green | Later repo-wide gates and CI passed on current `master`; the original unrelated auth fake blocker is gone. Phase01C CI run `25958870299` passed `Go test + Phase 2 guards`. | met |
| Web API shape | Phase01B/Phase01C later repair evidence | response shape unchanged while `/api/chat` routes through Hub | Phase01B web Hub live markers and Phase01C web question probe passed | met later |
