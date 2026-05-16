# Phase04 Source Audit

| Source | Decision Supported | Adopt | Reject / Avoid | Status |
| --- | --- | --- | --- | --- |
| `D:/Aura/prd.md` Phase 4 | Runtime consolidation target | One canonical runtime | Duplicate production loop | read |
| `D:/Aura/.planning/aura-deep-refactor-decisions.json` ADR-003/009/010 | Agent owns loop/context | Agent-owned context bundle | Channel-owned loop behavior | read |
| `D:/Aura/internal/agent/` | Current central package | Preserve existing exported contracts | New god file | read during Phase-G closure |
| `D:/Aura/internal/agentloop/`, `internal/agentruntime/` | Duplicate/runtime paths | Merge only behind tests | Blind deletion | read during Phase-G closure |
| `D:/tmp/codex.md`, `D:/tmp/picobot`, `D:/tmp/nanobot` | External/example loop patterns | Single flat loop + context builder lessons | Importing foreign architecture wholesale | read for Phase-G planning |

## Missing Source Questions

None for the closed Runner-removal arc. Prompt snapshot/eval follow-up still
needs a fresh source map if selected.
