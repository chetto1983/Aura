# Phase-Q: User Memory Write Guards — Benchmark

## Actuals

| Metric | Value |
|--------|-------|
| Stories shipped | 4 (US-Q01..Q04) |
| Commits | 4 atomic commits on master |
| Files added | 3 new files (`question_gate.go`, `question_gate_test.go`, `write_guards_integration_test.go`) |
| Files modified | 3 (`user_memory_writer.go`, `user_memory_writer_test.go`, applier path) |
| Migrations | 1 (v16: `actor_id TEXT NOT NULL DEFAULT ''` on `proposed_updates`) |
| Test cases added | 14 (4 writer + 5 question-gate + 2 integration scenarios × multiple assertions) |
| go build/vet/test | ✅ green across all 4 commits |

## Performance actuals

| Boundary | Latency | Notes |
|----------|---------|-------|
| Authorize call overhead | < 1 ms | In-process capability lookup, no DB round-trip |
| Question gate roundtrip | LLM-RTT bound | Gate emits event; answer arrives on next Telegram message (human-paced) |
| ShouldGateUserMemoryWrite | < 1 µs | Pure float comparison, no I/O |

## Capability gaps closed by Phase-Q

| Gap | Status |
|-----|--------|
| `memory.user.write` capability check at WriteApprovedUserFact boundary | ✅ Closed |
| Ambiguity question gate for Score < 0.7 user_memory candidates | ✅ Closed |
| Integration test covering both gates end-to-end (no live LLM) | ✅ Closed |
| prd.md §5.7 'write policy' fully wired | ✅ Closed |

## Phase 9 partial-close scope coverage

Phase-O + Phase-Q together cover:
- Preference/fact/person/todo extraction and routing (Phase-O US-O01..O02)
- WriteApprovedUserFact writer + recall_user_memory tool (Phase-O US-O03..O04)
- Capability check + question gate (Phase-Q US-Q01..Q02)
- Integration test coverage (Phase-Q US-Q03)

Remaining Phase 9 scope (documentation + invariant hardening, deferred to future pass):
- Clarify `memory` versus `storage` docs
- Conversion fixtures for important source types
- Harden SQLite concurrency (WAL/busy-timeout/retry)
