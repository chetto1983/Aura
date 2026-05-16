# Phase06 Benchmark

| Check | Command / Method | Threshold | Actual Result | Status |
| --- | --- | --- | --- | --- |
| Observation tests | selected tool supervisor tests | all classes represented | All 10→5 bucket mappings + tool_kind derivation covered in observation_test.go (US-J01, 73ddea04) | ✅ met |
| Retry budget tests | selected package tests | caps enforced | budget=2 + 3 recoverables → 3rd refused; budget=0 + 1 blocked → refused immediately; refusal row reason format verified (US-J05, 0ceb7133) | ✅ met |
| Unknown side-effect test | selected workflow tests | reconcile-first behavior | Deferred to Phase-K (durable workflow + idempotency out of scope for Alt-A storage-first slice) | ⏸ deferred (Phase-K) |
| Redaction tests | selected learning/log tests | no raw sensitive args | Record roundtrip test confirms arg values never appear in row, only hash + key names (US-J02, 36013353) | ✅ met |
| Full compile/vet/test | `go build ./...`; `go vet ./...`; `go test ./...` | green | All green across US-J01..J06 (latest fa7d4559) | ✅ met |
