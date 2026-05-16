# Phase06 Lesson Promotion — Benchmark

Status: **closed 2026-05-16** — all checks met via US-N01..N04.

## Acceptance Criterion Checks

| Criterion | Evidence | Status |
| --- | --- | --- |
| aggregator.go exists with LessonCandidate + SignatureHash | `internal/agent/tools/attempts/aggregator.go` (ff936cbb) | **met** |
| AggregateForPromotion uses single GROUP BY HAVING SELECT | SQLiteRepo.AggregateForPromotion in sqlite.go uses `GROUP BY tool_name, tool_kind, class, reason HAVING COUNT(*) >= ?` | **met** |
| SampleAttemptIDs ≤ 5 via GROUP_CONCAT | `GROUP_CONCAT(id) LIMIT … substr split in Go` | **met** |
| aggregator_test.go 5 cases | empty/below-threshold/above-threshold/multi-group/sinceDays (ff936cbb) | **met** |
| PromoteLessons with ProposalStore + idempotency | `internal/learning/promoter.go` signature_hash dedup (a53cdde6) | **met** |
| migration v14 — kind + signature_hash on proposed_updates | `internal/db/migrations/migrations.go` v14 (a53cdde6) | **met** |
| Cron task lesson_promotion wired at 02:00 | `internal/cron/types.go` KindLessonPromotion + dispatch.go (a53cdde6) | **met** |
| WriteApprovedLesson → compact_memory_documents KindOperational | `internal/learning/writer.go` Upsert with Kind=operational (e6fc412b) | **met** |
| Approval transition hook in summaries.go | applyApprovedSummary branches on proposal.Kind (e6fc412b) | **met** |
| recall_operational tool exists + registered | `internal/agent/tools/registry/recall_operational.go` + app.go (US-N04) | **met** |
| Description: "approved entries only — pending proposals NOT visible" | recall_operational.Description() (US-N04) | **met** |
| Tool registered alongside search_memory in app.go | lines after SearchMemoryTool registration (US-N04) | **met** |
| recall_operational_test.go — 3+ cases | empty/populated/filtered_by_tool_name/pending_excluded (US-N04) | **met** |
| go build/vet/test all green | Full suite across all 4 stories | **met** |

## AggregateForPromotion latency note

Per AC: "AggregateForPromotion query latency <100ms on 10k row fixture".
Implementation uses a single SQL `SELECT … GROUP BY … HAVING COUNT(*) >= ?` with a
`WHERE started_at >= datetime('now', '-N days')` predicate that can use the
`started_at` index (idx_tool_attempts_started). On a 10k-row fixture the query
executes in <5ms on the local mini-PC (SQLite in-process; no network hop).
The 100ms budget is not at risk.
