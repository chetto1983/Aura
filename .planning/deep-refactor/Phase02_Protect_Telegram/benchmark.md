# Phase02 Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Telegram fixture tests | `go test ./internal/channels/telegram/fixture/` | simple/CoT/tool/entity/fallback cases stable | 4/4 PASS (0.39s) | met |
| Snapshot comparison | `git diff internal/channels/telegram/fixture/testdata/` after `go test -count=1` | zero diff for previously-captured cases | zero diff (only CRLF/LF warnings) | met |
| Narrow package tests | `go test ./internal/channels/telegram ./internal/agent ./internal/chat` | green | all pass | met |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | green | all pass | met |
| Fallback path observability | snapshot inspection of `fallback_entity_edit_to_plain_text.json` | ≥1 `failed:true` entity call + ≥1 follow-up call with no entities | 1 failed + 1 plain-text retry + 1 final entity-edit success | met |
