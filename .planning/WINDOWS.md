---
schema_version: 1
open_count: 2
waived_count: 0
fixed_count: 0
total_count: 2
last_updated: 2026-08-31T19:29:22.115Z
---

# Broken Windows Ledger

> Cross-phase defect register. With `workflow.windows_enforce` enabled, `/gsd-ship` blocks while `open_count > 0`.
> Waive with `gsd-tools windows waive <id> "<reason>"` (reason required).
> Mark fixed with `gsd-tools windows fixed <id>`.

| id | phase | kind | file | line | description | status | reason | recorded_at | resolved_at |
|----|-------|------|------|------|-------------|--------|--------|-------------|-------------|
| 1 | 49 | deviation | internal/arcadedb/memory_conversation.go |  | Live ArcadeDB 26.8.1 rejected LIGHTWEIGHT UNIQUE IF NOT EXISTS edge DDL; replaced by regular edges with unique endpoint indexes | open |  | 2026-08-31T19:29:21.704Z |  |
| 2 | 49 | deviation | cmd/aura/serve.go |  | Task 2 RED commit 80a141ac6 inherited three concurrent pre-staged cmd/aura paths; ownership transferred in b14f03382 and history was not rewritten while sessions were active | open |  | 2026-08-31T19:29:22.115Z |  |

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
  }
]
````
