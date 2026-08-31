---
schema_version: 1
open_count: 9
waived_count: 0
fixed_count: 0
total_count: 9
last_updated: 2026-08-31T23:48:20.997Z
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
| 4 | 49 | stub | internal/arcadedb/memory_recall.go | 186 | Reserved reasoning mode returns reasoning_not_available until Plan 49-04 connects the explicit reasoning graph | open |  | 2026-08-31T23:03:29.191Z |  |
| 5 | 49 | deviation | internal/arcadedb/memory_recall_browse.go |  | Browse and cursor logic split from memory_recall.go to keep both production files under the 600-line cap | open |  | 2026-08-31T23:03:29.620Z |  |
| 6 | 49 | deviation | docs/arcadedb-mcp-live-tools.json |  | Generated MCP manifest and legacy fact-only recall fixture updated for the additive unified schema and native fusion flow | open |  | 2026-08-31T23:03:30.022Z |  |
| 7 | 49 | deviation | .planning/STATE.md |  | Sequential state handlers advanced Plan 49-03 but retained Plan 49-07 last-activity metadata; canonical and prose activity fields were synchronized manually | open |  | 2026-08-31T23:06:50.014Z |  |
| 8 | 49 | deviation | internal/arcadedb/memory_recall_exclusion.go |  | Plan 49-08 added the internal recall exclusion field/helper so validated host headers become pre-ranking negative filters across semantic and browse modes. | open |  | 2026-08-31T23:45:38.183Z |  |
| 9 | 49 | deviation | .planning/STATE.md |  | Plan 49-08 completed out of order; state handlers preserved current plan 4 but left Plan 49-03 prose activity/progress, so canonical and prose fields were synchronized without advancing. | open |  | 2026-08-31T23:48:20.997Z |  |

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
    "status": "open",
    "reason": "",
    "recorded_at": "2026-08-31T23:03:29.191Z",
    "resolved_at": null
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
  }
]
````
