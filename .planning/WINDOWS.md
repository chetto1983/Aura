---
schema_version: 1
open_count: 24
waived_count: 0
fixed_count: 2
total_count: 26
last_updated: 2026-09-01T05:56:47.302Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 49 | deviation | internal/arcadedb/memory_conversation.go |  | Live ArcadeDB 26.8.1 rejected LIGHTWEIGHT UNIQUE IF NOT EXISTS edge DDL; replaced by regular edges with unique endpoint indexes | open |  | 2026-08-31T19:29:21.704Z |  |
| 2 | 49 | deviation | cmd/aura/serve.go |  | Task 2 RED commit 80a141ac6 inherited three concurrent pre-staged cmd/aura paths; ownership transferred in b14f03382 and history was not rewritten while sessions were active | open |  | 2026-08-31T19:29:22.115Z |  |
| 3 | 49 | deviation | cmd/aura/chat_memory_projection.go |  | Split composition and reconciliation coverage into focused files to satisfy the repository 600-line cap | open |  | 2026-08-31T22:02:02.530Z |  |
| 4 | 49 | stub | internal/arcadedb/memory_recall.go | 186 | Reserved reasoning mode returns reasoning_not_available until Plan 49-04 connects the explicit reasoning graph | fixed |  | 2026-08-31T23:03:29.191Z | 2026-09-01T00:33:13.455Z |
| 5 | 49 | deviation | internal/arcadedb/memory_recall_browse.go |  | Browse and cursor logic split from memory_recall.go to keep both production files under the 600-line cap | open |  | 2026-08-31T23:03:29.620Z |  |
| 6 | 49 | deviation | docs/arcadedb-mcp-live-tools.json |  | Generated MCP manifest and legacy fact-only recall fixture updated for the additive unified schema and native fusion flow | open |  | 2026-08-31T23:03:30.022Z |  |
| 7 | 49 | deviation | .planning/STATE.md |  | Sequential state handlers advanced Plan 49-03 but retained Plan 49-07 last-activity metadata; canonical and prose activity fields were synchronized manually | open |  | 2026-08-31T23:06:50.014Z |  |
| 8 | 49 | deviation | internal/arcadedb/memory_recall_exclusion.go |  | Plan 49-08 added the internal recall exclusion field/helper so validated host headers become pre-ranking negative filters across semantic and browse modes. | open |  | 2026-08-31T23:45:38.183Z |  |
| 9 | 49 | deviation | .planning/STATE.md |  | Plan 49-08 completed out of order; state handlers preserved current plan 4 but left Plan 49-03 prose activity/progress, so canonical and prose fields were synchronized without advancing. | open |  | 2026-08-31T23:48:20.997Z |  |
| 10 | 49 | deviation | internal/arcadedb/memory_reasoning_validate.go |  | Split reasoning validation and redaction from storage/search so both production files remain under the 600-line cap | open |  | 2026-09-01T00:33:13.852Z |  |
| 11 | 49 | deviation | docs/arcadedb-mcp-live-tools.json |  | Regenerated the canonical MCP manifest for the additive explicit reasoning selector and result schema | open |  | 2026-09-01T00:33:14.263Z |  |
| 12 | 49 | deviation | .planning/STATE.md |  | Plan 49-04 advanced sequentially to Plan 5, but update-progress left Plan 49-08 prose activity and 54/62 progress; canonical and prose fields were synchronized to 55/62 | open |  | 2026-09-01T00:34:17.388Z |  |
| 13 | 49 | deviation | internal/arcadedb/memory_recall_exclusion.go |  | Live indexed collection NOT IN filtering dropped eligible historical recall evidence; replaced with bounded scalar exclusions. | open |  | 2026-09-01T01:31:02.219Z |  |
| 14 | 49 | deviation | cmd/arcadedb-mcp/memory_live_integration_helpers_test.go |  | Shared live harness needed strict dependencies, per-call headers, tenant control, and MCP receiving middleware for complete proof. | open |  | 2026-09-01T01:31:02.607Z |  |
| 15 | 49 | deviation | scripts/agent_memory_eval_phase49.py |  | Phase 49 evaluator split module and calculated markers were required to preserve the 600-line cap and honest route evidence. | open |  | 2026-09-01T01:31:03.002Z |  |
| 16 | 49 | deviation | .planning/STATE.md |  | Out-of-order Plan 49-13 close required manual activity/progress synchronization while preserving current Plan 5. | open |  | 2026-09-01T01:31:03.436Z |  |
| 17 | 49 | stub | internal/runner/runner.go | 68 | ReasoningGraphSink is intentionally not injected at production boot in Plan 49-12; dependent Plan 49-09 owns the single boot sink and lifecycle composition. | open |  | 2026-09-01T02:10:48.333Z |  |
| 18 | 49 | deviation | internal/runner/runner_reasoning_persist.go |  | Plan 49-12 extended the existing authorization seam and ArcadeDB storage-boundary tests outside the literal file list so graph capture cannot diverge from display authorization and TOUCHED cannot target absent entities. | open |  | 2026-09-01T02:10:48.735Z |  |
| 19 | 49 | deviation | .planning/STATE.md |  | Plan 49-12 completed out of order; state.advance-plan moved the sequential pointer from still-incomplete Plan 49-05 to 49-06, so the pointer was restored to 49-05 while retaining the 57/62 completed count and Plan 49-12 activity/session metadata. | open |  | 2026-09-01T02:13:36.146Z |  |
| 20 | 49 | stub | internal/runner/runner_deps.go | 46 | Production boot does not yet inject MemoryCaptureSink; Plan 49-14 owns graph composition and live mid-task proof. | fixed |  | 2026-09-01T04:08:58.607Z | 2026-09-01T05:56:47.302Z |
| 21 | 49 | deviation | internal/runner/runner_persist.go | 151 | Rule 1: terminal flush now snapshots the runner-global accepted watermark so resumed turns drain pre-pause captures. | open |  | 2026-09-01T04:08:59.003Z |  |
| 22 | 49 | deviation | .planning/STATE.md |  | Plan 49-05 state handlers advanced to completed Plan 6 and left Plan 09 activity/progress prose; pointer and prose were synchronized to next incomplete Plan 10 and 59/62. | open |  | 2026-09-01T04:12:07.678Z |  |
| 23 | 49 | deviation | internal/arcadedb/memory_batch_store.go |  | Plan 49-14 normalized batch DATETIME parameters so capture-created facts are immediately recallable. | open |  | 2026-09-01T05:56:21.236Z |  |
| 24 | 49 | deviation | internal/runner/runner_memory_capture.go |  | Plan 49-14 added the required host-bound user_turn provenance reference. | open |  | 2026-09-01T05:56:21.636Z |  |
| 25 | 49 | deviation | cmd/aura/chat_boot_memory_capture_test.go |  | Plan 49-14 added omitted daemon-free composition and precision regression coverage. | open |  | 2026-09-01T05:56:22.032Z |  |
| 26 | 49 | deviation | .planning/STATE.md |  | Plan 49-14 restored the sequential pointer to incomplete Plan 49-11 after out-of-order close-out. | open |  | 2026-09-01T05:56:22.442Z |  |

````json
[
  {
    "id": 1,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/arcadedb/memory_conversation.go",
    "line": null,
    "description": "Live ArcadeDB 26.8.1 rejected LIGHTWEIGHT UNIQUE IF NOT EXISTS edge DDL; replaced by regular edges with unique endpoint indexes",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T19:29:21.704Z",
    "resolved_at": null
  },
  {
    "id": 2,
    "kind": "deviation",
    "phase": "49",
    "file": "cmd/aura/serve.go",
    "line": null,
    "description": "Task 2 RED commit 80a141ac6 inherited three concurrent pre-staged cmd/aura paths; ownership transferred in b14f03382 and history was not rewritten while sessions were active",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T19:29:22.115Z",
    "resolved_at": null
  },
  {
    "id": 3,
    "kind": "deviation",
    "phase": "49",
    "file": "cmd/aura/chat_memory_projection.go",
    "line": null,
    "description": "Split composition and reconciliation coverage into focused files to satisfy the repository 600-line cap",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T22:02:02.530Z",
    "resolved_at": null
  },
  {
    "id": 4,
    "kind": "stub",
    "phase": "49",
    "file": "internal/arcadedb/memory_recall.go",
    "line": 186,
    "description": "Reserved reasoning mode returns reasoning_not_available until Plan 49-04 connects the explicit reasoning graph",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-08-31T23:03:29.191Z",
    "resolved_at": "2026-09-01T00:33:13.455Z"
  },
  {
    "id": 5,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/arcadedb/memory_recall_browse.go",
    "line": null,
    "description": "Browse and cursor logic split from memory_recall.go to keep both production files under the 600-line cap",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T23:03:29.620Z",
    "resolved_at": null
  },
  {
    "id": 6,
    "kind": "deviation",
    "phase": "49",
    "file": "docs/arcadedb-mcp-live-tools.json",
    "line": null,
    "description": "Generated MCP manifest and legacy fact-only recall fixture updated for the additive unified schema and native fusion flow",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T23:03:30.022Z",
    "resolved_at": null
  },
  {
    "id": 7,
    "kind": "deviation",
    "phase": "49",
    "file": ".planning/STATE.md",
    "line": null,
    "description": "Sequential state handlers advanced Plan 49-03 but retained Plan 49-07 last-activity metadata; canonical and prose activity fields were synchronized manually",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T23:06:50.014Z",
    "resolved_at": null
  },
  {
    "id": 8,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/arcadedb/memory_recall_exclusion.go",
    "line": null,
    "description": "Plan 49-08 added the internal recall exclusion field/helper so validated host headers become pre-ranking negative filters across semantic and browse modes.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T23:45:38.183Z",
    "resolved_at": null
  },
  {
    "id": 9,
    "kind": "deviation",
    "phase": "49",
    "file": ".planning/STATE.md",
    "line": null,
    "description": "Plan 49-08 completed out of order; state handlers preserved current plan 4 but left Plan 49-03 prose activity/progress, so canonical and prose fields were synchronized without advancing.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T23:48:20.997Z",
    "resolved_at": null
  },
  {
    "id": 10,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/arcadedb/memory_reasoning_validate.go",
    "line": null,
    "description": "Split reasoning validation and redaction from storage/search so both production files remain under the 600-line cap",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T00:33:13.852Z",
    "resolved_at": null
  },
  {
    "id": 11,
    "kind": "deviation",
    "phase": "49",
    "file": "docs/arcadedb-mcp-live-tools.json",
    "line": null,
    "description": "Regenerated the canonical MCP manifest for the additive explicit reasoning selector and result schema",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T00:33:14.263Z",
    "resolved_at": null
  },
  {
    "id": 12,
    "kind": "deviation",
    "phase": "49",
    "file": ".planning/STATE.md",
    "line": null,
    "description": "Plan 49-04 advanced sequentially to Plan 5, but update-progress left Plan 49-08 prose activity and 54/62 progress; canonical and prose fields were synchronized to 55/62",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T00:34:17.388Z",
    "resolved_at": null
  },
  {
    "id": 13,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/arcadedb/memory_recall_exclusion.go",
    "line": null,
    "description": "Live indexed collection NOT IN filtering dropped eligible historical recall evidence; replaced with bounded scalar exclusions.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T01:31:02.219Z",
    "resolved_at": null
  },
  {
    "id": 14,
    "kind": "deviation",
    "phase": "49",
    "file": "cmd/arcadedb-mcp/memory_live_integration_helpers_test.go",
    "line": null,
    "description": "Shared live harness needed strict dependencies, per-call headers, tenant control, and MCP receiving middleware for complete proof.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T01:31:02.607Z",
    "resolved_at": null
  },
  {
    "id": 15,
    "kind": "deviation",
    "phase": "49",
    "file": "scripts/agent_memory_eval_phase49.py",
    "line": null,
    "description": "Phase 49 evaluator split module and calculated markers were required to preserve the 600-line cap and honest route evidence.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T01:31:03.002Z",
    "resolved_at": null
  },
  {
    "id": 16,
    "kind": "deviation",
    "phase": "49",
    "file": ".planning/STATE.md",
    "line": null,
    "description": "Out-of-order Plan 49-13 close required manual activity/progress synchronization while preserving current Plan 5.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T01:31:03.436Z",
    "resolved_at": null
  },
  {
    "id": 17,
    "kind": "stub",
    "phase": "49",
    "file": "internal/runner/runner.go",
    "line": 68,
    "description": "ReasoningGraphSink is intentionally not injected at production boot in Plan 49-12; dependent Plan 49-09 owns the single boot sink and lifecycle composition.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T02:10:48.333Z",
    "resolved_at": null
  },
  {
    "id": 18,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/runner/runner_reasoning_persist.go",
    "line": null,
    "description": "Plan 49-12 extended the existing authorization seam and ArcadeDB storage-boundary tests outside the literal file list so graph capture cannot diverge from display authorization and TOUCHED cannot target absent entities.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T02:10:48.735Z",
    "resolved_at": null
  },
  {
    "id": 19,
    "kind": "deviation",
    "phase": "49",
    "file": ".planning/STATE.md",
    "line": null,
    "description": "Plan 49-12 completed out of order; state.advance-plan moved the sequential pointer from still-incomplete Plan 49-05 to 49-06, so the pointer was restored to 49-05 while retaining the 57/62 completed count and Plan 49-12 activity/session metadata.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T02:13:36.146Z",
    "resolved_at": null
  },
  {
    "id": 20,
    "kind": "stub",
    "phase": "49",
    "file": "internal/runner/runner_deps.go",
    "line": 46,
    "description": "Production boot does not yet inject MemoryCaptureSink; Plan 49-14 owns graph composition and live mid-task proof.",
    "status": "fixed",
    "reason": "",
    "recorded_at": "2026-09-01T04:08:58.607Z",
    "resolved_at": "2026-09-01T05:56:47.302Z"
  },
  {
    "id": 21,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/runner/runner_persist.go",
    "line": 151,
    "description": "Rule 1: terminal flush now snapshots the runner-global accepted watermark so resumed turns drain pre-pause captures.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T04:08:59.003Z",
    "resolved_at": null
  },
  {
    "id": 22,
    "kind": "deviation",
    "phase": "49",
    "file": ".planning/STATE.md",
    "line": null,
    "description": "Plan 49-05 state handlers advanced to completed Plan 6 and left Plan 09 activity/progress prose; pointer and prose were synchronized to next incomplete Plan 10 and 59/62.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T04:12:07.678Z",
    "resolved_at": null
  },
  {
    "id": 23,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/arcadedb/memory_batch_store.go",
    "line": null,
    "description": "Plan 49-14 normalized batch DATETIME parameters so capture-created facts are immediately recallable.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T05:56:21.236Z",
    "resolved_at": null
  },
  {
    "id": 24,
    "kind": "deviation",
    "phase": "49",
    "file": "internal/runner/runner_memory_capture.go",
    "line": null,
    "description": "Plan 49-14 added the required host-bound user_turn provenance reference.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T05:56:21.636Z",
    "resolved_at": null
  },
  {
    "id": 25,
    "kind": "deviation",
    "phase": "49",
    "file": "cmd/aura/chat_boot_memory_capture_test.go",
    "line": null,
    "description": "Plan 49-14 added omitted daemon-free composition and precision regression coverage.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T05:56:22.032Z",
    "resolved_at": null
  },
  {
    "id": 26,
    "kind": "deviation",
    "phase": "49",
    "file": ".planning/STATE.md",
    "line": null,
    "description": "Plan 49-14 restored the sequential pointer to incomplete Plan 49-11 after out-of-order close-out.",
    "status": "open",
    "reason": "",
    "recorded_at": "2026-09-01T05:56:22.442Z",
    "resolved_at": null
  }
]
````
