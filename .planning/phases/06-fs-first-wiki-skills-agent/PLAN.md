# Phase 06 - FS-First Wiki, Skills, and Agent Simplification

Status: DONE
Started: 2026-05-08

## Goal

Replace Aura's legacy semantic LLM tools for wiki, skills, proposals, and logs with the bounded workspace file tools:

- `list_files`
- `read_file`
- `search_files`
- `write_file`
- `apply_patch`

Aura should behave more like Codex and Picobot: the agent reads and edits a bounded project workspace directly, while Aura keeps safety, indexing, audit, ingestion, dashboard, Telegram, auth, scheduler, and source/OCR capabilities as product infrastructure.

## Context

User direction:

- Guardrails that only compensate for missing filesystem access are now legacy.
- The new workspace tools are bounded and deny dangerous paths, so they can replace `write_wiki`, `read_wiki`, `list_wiki`, `lint_wiki`, `append_log`, `rebuild_index`, `list_skills`, `read_skill`, and proposal tools.
- Use the LLM Wiki pattern from `docs/llm-wiki.md`: the wiki is markdown-first, linkable, inspectable, and maintained like a small codebase.
- Keep the active phase plan updated continuously.

References:

- `docs/llm-wiki.md`
- `D:\tmp\picobot`
- `https://github.com/nousresearch/hermes-agent`
- Codex-like prompt style: concise tool policy, file-first edits, explicit verification.

## Non-Goals

- Do not remove source ingestion, OCR, scheduler, auth, dashboard, Telegram, MCP, web search, file generation, or `search_memory`.
- Do not mutate live SQLite database files directly.
- Do not delete dashboard/admin skill catalog infrastructure until replacement surfaces are verified.
- Do not remove the skills system. Preserve `internal/skills`, SKILL.md parsing/loading, multi-root skill discovery, dashboard/admin skill install/delete, and the prompt skill manifest. This phase removes only the legacy LLM wrapper tools for reading/proposing skills; skills remain first-class filesystem knowledge.
- Do not remove historical docs through this phase unless they block the simplification.

## Target Design

The LLM sees a small, strong tool surface:

- Workspace files for wiki, skills, prompts, docs, and local code.
- `search_memory` as a fast semantic accelerator, never the only source of truth.
- Source/OCR/ingest tools for external material capture.
- Scheduler/task/admin/auth/dashboard tools where they are product features.
- Web/search/fetch/code execution tools where explicitly configured.

The LLM no longer sees separate semantic write/proposal tools for wiki and skills. Wiki and skill changes are ordinary markdown edits under bounded workspace paths.

## Work Items

### 0. Baseline and Plan

Status: IN_PROGRESS

- Run Ralph status.
- Write this phase plan.
- Record first deletion scope before touching code.

### 1. Prompt and Skill Contract

Status: DONE

- Rewrite `internal/conversation/system_prompt.go` around filesystem-first behavior.
- Remove prompt instructions for:
  - `write_wiki`
  - `read_wiki`
  - `list_wiki`
  - `lint_wiki`
  - `append_log`
  - `rebuild_index`
  - `list_skills`
  - `read_skill`
  - `propose_wiki_change`
  - `propose_skill_change`
- Update skill manifest prompt blocks so skills are inspected via file tools when relevant.
- Keep progressive disclosure as prompt guidance, not as a ritualized tool dependency.

### 2. Workspace Tools Become the Default Agent Filesystem

Status: DONE

- Make bounded workspace tools available by default for the main Aura runtime.
- Keep deny rules for secrets, git internals, raw wiki data, live DB files, and binary writes.
- Ensure Docker still uses `/app`, while local/dev can use the project root.
- Update config docs and smoke tests.

### 3. Remove Legacy Tools from Main Runtime Surface

Status: DONE

Progress:

- Removed skill/wiki/proposal wrapper registration from the main Telegram runtime.
- Replaced orchestration allowlists and capability maps with workspace file tools.
- Removed the wiki proposal prompt module from composed prompts.
- Converted scheduler agent-job, swarm, and debug orchestration toolsets away from `read_wiki`, `list_wiki`, `read_skill`, and proposal wrappers.

- Stop registering legacy wiki/skill/proposal tools in `internal/telegram/setup.go`.
- Replace orchestration allowlists and capability mapping with workspace file tools.
- Keep product tools that are not filesystem surrogates.
- Update debug orchestration expectations.

### 4. Delete Legacy Tool Implementations

Status: IN_PROGRESS

- Remove obsolete LLM wrapper code once no runtime path references it:
  - `internal/tools/wiki.go`
  - wiki maintenance LLM wrappers
  - wiki proposal LLM wrappers
  - skill proposal LLM wrappers
  - skill read/list/search LLM wrappers
- Preserve underlying domain services that are still used by dashboard, ingestion, indexing, scheduler, and search.
- Remove or rewrite tests that only validate deleted wrappers.

Current blockers before physical deletion:

- `cmd/debug_tools` still smokes old wiki wrapper tools.
- `cmd/debug_memory_quality` still uses `propose_wiki_change` as an evaluation signal.
- `cmd/debug_ingest` still registers wiki maintenance wrappers for legacy smoke coverage.
- `cmd/debug_files` and `cmd/debug_sandbox` still have skill-read smoke paths that should become direct file-tool or loader checks.

Current slice started 2026-05-08:

- Use sub-agents for parallel audit/implementation support.
- Converted remaining debug smoke commands away from legacy wrappers.
- Deleted obsolete wrapper files/tests after direct references are gone.
- Keep only domain services used by dashboard, ingest, scheduler, search, and skill admin.
- Skills are explicitly preserved as file-backed procedures; only `list_skills`, `read_skill`, `search_skill_catalog`, and `propose_skill_change` wrapper tools are removed from the LLM tool surface/code.
- Deleted:
  - `internal/tools/wiki.go`
  - `internal/tools/wiki_proposal.go`
  - `internal/tools/wiki_maintenance.go`
  - `internal/tools/skills.go`
  - `internal/tools/skill_proposal.go`
  - wrapper-only tests for those files
- Replaced active `read_skill` telemetry with `read_file` over `SKILL.md` paths.
- Retained only intentional legacy strings for forbidden-tool drift detection and historical review-queue compatibility.

### 5. Wiki/Skill File Invariants

Status: PENDING

- Add post-write or follow-up validation for markdown wiki and skill paths.
- For wiki writes:
  - keep markdown/frontmatter valid
  - preserve `[[slug]]` links
  - update or rebuild index/search state through domain services
  - append human-readable audit/log entries where appropriate
- For skill writes:
  - validate `SKILL.md` shape
  - refresh manifest/catalog cache where needed

### 6. Debug, Scheduler, and Swarm Cleanup

Status: IN_PROGRESS

Progress:

- Debug tools, ingest smoke, file smoke, sandbox smoke, memory-quality live/hermetic evaluation, Telegram sandbox debug, and swarm tests now exercise workspace file tools.
- Scheduler agent jobs now use file read/search toolsets for memory and skill-backed jobs.
- Agent job prompts ask for recommended file patches instead of review-queue proposal tool calls.
- Swarm role presets now inspect memory/wiki/skills through workspace file reads.
- `cmd/debug_agent_jobs` now registers workspace file tools and reads the monitored wiki page with `read_file`.

- Update debug commands to smoke-test workspace file workflows.
- Convert scheduled agent job toolsets away from wiki/proposal wrappers.
- Update swarm planner/tool preferences to use file tools.
- Keep compatibility only where explicitly needed for old stored jobs.

### 7. Verification and Commit

Status: IN_PROGRESS

Verification:

- `go test ./internal/conversation ./internal/skills ./internal/config ./internal/api ./internal/orchestration ./cmd/debug_orchestration`
- `go test ./cmd/debug_agent_jobs ./internal/swarmtools ./internal/swarm ./internal/toolsets ./internal/scheduler ./internal/telegram ./internal/tools`
- `go test ./...`
- `go test ./cmd/debug_files ./cmd/debug_ingest ./cmd/debug_memory_quality ./cmd/debug_sandbox ./cmd/debug_swarm ./cmd/debug_telegram_sandbox ./cmd/debug_tools ./internal/agentloop ./internal/api ./internal/llm ./internal/scheduler ./internal/telegram ./internal/tools ./internal/toolsets ./internal/swarmtools`

- Run focused tests after each slice.
- Run `go test ./...` or the project verify script before commit.
- Update tracker docs.
- Commit only intentional files.

## First Slice Scope

Do now:

- Create this plan.
- Rewrite the prompt contract to filesystem-first.
- Enable bounded workspace tools as the default local agent filesystem.
- Remove legacy wiki/skill/proposal wrappers from the main Telegram runtime surface.
- Update orchestration allowlists/capabilities to prefer workspace tools.

Defer until references are mapped cleanly:

- Physical deletion of wrapper files.
- Scheduler/swarm compatibility migration.
- Wiki post-write indexing hooks.
