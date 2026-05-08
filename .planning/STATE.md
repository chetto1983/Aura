# State

Date: 2026-05-08

Active milestone: v3.2 Runtime Diet (active)

Last closed milestone: v3.1 Agent Runtime Stabilization

Current branch: `master`

## Current Truth

`master` is deployed to GitHub at `a5b3594` (`docs: clean planning state after simplification`). The simplification and Docker runtime cleanup branches have been fast-forward merged.

Aura's current runtime direction is no longer the old profile/preflight guardrail stack. The shipped shape is:

- compact agent loop in `internal/agentloop`;
- four runtime toolsets (`default`, `compute`, `document`, `admin`);
- bounded workspace file tools rooted at `/workspace` in Docker;
- legacy wiki/skill/proposal wrapper tools removed from the LLM surface/code;
- skills preserved as file-backed procedures under the runtime workspace;
- materialized wiki graph files plus compact Retrieval Capsule injection for memory/document turns;
- Docker image/context narrowed so `/app` no longer exposes the developer repository.

Recent verification evidence:

- Full Go verification passed during Phase 06/07 closure.
- Docker Compose rebuild passed.
- `/status` returned ok after the Docker runtime cleanup.
- Live workspace probe confirmed `/app` contains only `/app/runtime`; runtime files live under `/workspace`.
- Live wiki/graph swarm prompt passed under 30s after finalization latency repair.
- Live skill discovery and workspace listing smokes passed against the narrow runtime workspace.

## Active Blocker

The remaining product blocker is broader than the document route: Aura still spends too much context and too many tool calls before answering.

Evidence:

- the broad document prompt in `.planning/phases/04-agent-orchestration-system-prompt-versioning/VALIDATION.md` creates a DOCX, but expands evidence too much before `create_docx` (`loop_steps=5`, `elapsed_ms=62894`, `tokens_total=66119`);
- recent Docker logs still show repeated `read_file/search_files/list_files` loops on broad prompts (`elapsed_ms=54205`, `tool_calls=9`);
- the broad document prompt previously created a DOCX only after too much broad evidence expansion;
- source/archive recall still needs a durable compact-memory index beyond the current calibrated scan fallback.

Phase 08 slice 1 cut the first hot-path weight: the hardcoded fallback is gone, generic turns skip speculative Retrieval Capsule injection, skill manifests and swarm tools are explicit-only, and summarizer/nightly auto-improve default to off.

Phase 08 profile/preflight deletion pass cut the remaining taxonomy code instead of leaving compatibility switches: `ProfileCard`/capability policy, skill preflight policy, swarm routing prompt helper, `cmd/debug_orchestration`, `AURA_SKILL_PREFLIGHT`, `AURA_TOOL_PROFILE_MODE`, and legacy profile aliases are gone. Search now merges exact wiki slug/title/link hits, SQLite FTS, and vector results; Qdrant merges local hybrid evidence instead of hiding it.

Phase 08 Task 3/5/6 slice replaced the old Memory Pack code with `## Retrieval Capsule`, removed hot-path graph/log file reads, calibrated `search_memory` wiki/source/archive scores, added source/archive follow-up handles, narrowed document tool exposure to `search_memory` plus typed file tools, and removed toolset-specific step caps so the DB/dashboard `AgentLoopMaxSteps` value remains authoritative.

Added `scripts/capture-aura-health.ps1` so validation can save Docker status/logs, container health, dashboard conversations, Telegram bot health, and optional debug Telegram smokes into ignored `reports/health/<timestamp>/` bundles without copying the live SQLite DB.

Hermes-style tool-call examples are now attached through Aura's LangChain-style `ToolDefinition` contract rather than registry-side prompt patching. Tools can own `Definition()` with name, description, schema, and structured examples; legacy/dynamic tools get an adapter fallback so every exposed tool still has a concrete call shape. The live document smoke now passes under 15s and creates/sends a DOCX in one loop step with only `create_docx`; `hidden_tool_rejected=false`.

Latest architecture direction after reviewing `google/adk-go`: keep Aura-native code, but adopt ADK's clean split of `Runner`, `Agent`, `Session/Event`, dynamic `Toolset`, and before/after model/tool callbacks. The first slice is now in place: `internal/agentruntime` emits tools/stats/final events, Telegram uses `RuntimeToolset.Tools(ctx)` plus invocation-aware filters, and duplicate/document-route steering moved into orchestration callbacks.

Next implementation should keep following `.planning/phases/08-runtime-diet-embedding-retrieval/PLAN.md`: build the durable compact-memory index for source/archive/proposal facts, then finish the runner boundary by moving session persistence and terminal finalization behind event handling.

## Cleaned-Up Plan Map

- Phase 05 (`05-agent-simplification-god-class-refactor`): complete and merged. Treat as historical implementation record.
- Phase 06 (`06-fs-first-wiki-skills-agent`): complete. Remaining items are follow-ups, not blockers.
- Phase 07 (`07-runtime-workspace-bootstrap-graph-cache`): complete. Runtime workspace, graph cache, historical Memory Pack benchmark, and live benchmarks are done.
- Phase 08 (`08-runtime-diet-embedding-retrieval`): active. Supersedes the narrow document-route evidence capsule slice.
- Phase 04 (`04-agent-orchestration-system-prompt-versioning`): historical v3.1 orchestration plan. It contains pre-simplification tasks that are now superseded; use only `VALIDATION.md` for the active document-route blocker.
- v4.0 MCP marketplace: planned, blocked until Phase 08 closes the runtime diet gates.

## Next Slice

Recommended slice: Phase 08 compact-memory index.

Goal:

- index compact source/archive/proposal facts instead of relying on raw scans;
- feed source/archive/proposal evidence into the Retrieval Capsule path;
- keep raw source bodies and archive turns behind explicit handles;
- preserve the simple Runtime Diet hot path.

Suggested acceptance:

- `go test ./internal/search ./internal/tools ./internal/telegram -count=1`;
- source/archive recall returns compact facts before raw body scans;
- broad document prompt keeps the one-step typed artifact path when the capsule is sufficient.

## Deferred Follow-Ups

- Automatic wiki index/search/graph refresh after accepted workspace writes to wiki files.
- Skill manifest/cache refresh after accepted workspace writes to `SKILL.md`.
- Small review-gated Git toolset for Aura (`git_status`, `git_diff`, `git_log`, explicit-path `git_stage`, review-gated `git_commit`).
- Optional planning archive workflow after a real `.planning/MILESTONES.md` exists.
- v4.0 MCP marketplace after Phase 08 runtime diet closure.
