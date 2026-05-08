# State

Date: 2026-05-08

Active milestone: v3.1 Agent Runtime Stabilization (active)

Last closed milestone: v1.3 Memory Consolidation And Quality

Current branch: `master`

## Current Truth

`master` is deployed to GitHub at `4b66a98` (`docker: narrow Aura runtime context`). The simplification branch has been fast-forward merged.

Aura's current runtime direction is no longer the old profile/preflight guardrail stack. The shipped shape is:

- compact agent loop in `internal/agentloop`;
- four runtime toolsets (`default`, `compute`, `document`, `admin`);
- bounded workspace file tools rooted at `/workspace` in Docker;
- legacy wiki/skill/proposal wrapper tools removed from the LLM surface/code;
- skills preserved as file-backed procedures under the runtime workspace;
- materialized wiki graph files plus compact Memory Pack injection;
- Docker image/context narrowed so `/app` no longer exposes the developer repository.

Recent verification evidence:

- Full Go verification passed during Phase 06/07 closure.
- Docker Compose rebuild passed.
- `/status` returned ok after the Docker runtime cleanup.
- Live workspace probe confirmed `/app` contains only `/app/runtime`; runtime files live under `/workspace`.
- Live wiki/graph swarm prompt passed under 30s after finalization latency repair.
- Live skill discovery and workspace listing smokes passed against the narrow runtime workspace.

## Active Blocker

v3.1 remains open for one real product blocker: the live document route is functional but not fast or reliable enough.

Measured failure in `.planning/phases/04-agent-orchestration-system-prompt-versioning/VALIDATION.md`:

- broad prompt: "crea un breve documento Word con il riepilogo dei documenti e delle note disponibili in Aura";
- creates and delivers a DOCX, but expands evidence too much before `create_docx`;
- observed `loop_steps=5`, `elapsed_ms=62894`, `tokens_total=66119`;
- additional runs sometimes produced malformed `create_docx` JSON after context bloat.

Next implementation slice should add a runtime-owned compact evidence packet for document generation, then rerun the strict document live smoke.

## Cleaned-Up Plan Map

- Phase 05 (`05-agent-simplification-god-class-refactor`): complete and merged. Treat as historical implementation record.
- Phase 06 (`06-fs-first-wiki-skills-agent`): complete. Remaining items are follow-ups, not blockers.
- Phase 07 (`07-runtime-workspace-bootstrap-graph-cache`): complete. Runtime workspace, graph cache, Memory Pack, and live benchmarks are done.
- Phase 04 (`04-agent-orchestration-system-prompt-versioning`): historical v3.1 orchestration plan. It contains pre-simplification tasks that are now superseded; use only `VALIDATION.md` for the active document-route blocker.
- v4.0 MCP marketplace: planned, blocked until the document route passes the v3.1 closure gate.

## Next Slice

Recommended slice: `document-route-fast-evidence-capsule`.

Goal:

- provide one compact `document_summary_evidence` style helper or equivalent runtime packet;
- force document generation to follow `compact evidence -> create_docx`;
- harden JSON/tool argument reliability for typed file generation;
- prove the live document prompt stays under 30s and within the loop budget.

Suggested acceptance:

- `go test ./internal/tools ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`;
- `docker compose config --quiet`;
- `docker compose up -d --build aura`;
- `/status` ok;
- live document smoke with DB-selected model:
  - expected toolset/profile: document;
  - `create_docx` called;
  - no repeated broad memory/source loops;
  - elapsed <= 30000 ms;
  - loop steps <= 4;
  - token/cost metrics present.

## Deferred Follow-Ups

- Automatic wiki index/search/graph refresh after accepted workspace writes to wiki files.
- Skill manifest/cache refresh after accepted workspace writes to `SKILL.md`.
- Small review-gated Git toolset for Aura (`git_status`, `git_diff`, `git_log`, explicit-path `git_stage`, review-gated `git_commit`).
- Optional planning archive workflow after a real `.planning/MILESTONES.md` exists.
- v4.0 MCP marketplace after v3.1 document route closure.

