# Phase-O: User Memory Promotion — Benchmark

## Actuals

| Metric | Value |
|--------|-------|
| Stories shipped | 4 (US-O01..O04) |
| Commits | 4 atomic commits on master |
| Files added | 6 new files (triage.go, triage_test.go, triage_routing_test.go, user_memory_writer.go, user_memory_writer_test.go, recall_user_memory.go, recall_user_memory_test.go) |
| Migrations | 0 (no new tables; existing proposed_updates + compact_memory_documents schemas accept kind='user_memory') |
| Test cases added | 19 (7 triage + 4 routing + 4 writer + 4 recall) |
| go build/vet/test | ✅ green across all 4 commits |

## Capability gaps closed by Phase-O

| Gap | Status |
|-----|--------|
| KindUserMemory wired as first-class writer (Phase 7B US-L01 deferral) | ✅ Closed |
| User facts extractable from conversation candidates | ✅ Closed |
| Operator-gated approval before write | ✅ Closed (proposed_updates review flow) |
| User facts retrievable by LLM via recall_user_memory tool | ✅ Closed |

## Remaining deferrals (Phase-Q candidates)

- `memory.user.write` capability check (prd.md §5.7 line 714)
- Question gate when candidate category is ambiguous
- Importance scorer for user fact prioritization
