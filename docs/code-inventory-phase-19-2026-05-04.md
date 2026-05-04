# Code Inventory And Reuse Map

Date: 2026-05-04

Scope: start phase 19 by inventorying Aura code, identifying real dead/legacy code, and mapping the reusable patterns from Picobot and Hermes Agent without reinventing the runtime.

Primary references:

- Aura tracker: `docs/implementation-tracker.md`
- Aura/Picobot/Hermes product inventory: `docs/aura-picobot-hermes-inventory-2026-05-03.md`
- LLM Wiki philosophy: `docs/llm-wiki.md`
- Picobot local reference: `D:\tmp\picobot`
- Hermes local reference: `D:\tmp\hermes-agent`

## Executive State

Aura should stay the product core. The codebase already has the durable second-brain shape:

- source inbox, OCR, ingest, wiki, graph, archive;
- review-gated wiki proposals;
- scheduler and `agent_job`;
- skills loader/admin surfaces;
- MCP client/tool registration;
- AuraBot swarm/runner/telemetry;
- React dashboard for operation and review.

The next work should not be "another dashboard" by default. Phase 19 should focus on:

1. procedural memory (`propose_skill_change`);
2. skill-backed agent jobs with toolset profiles;
3. graph-aware wiki/source operations;
4. cleanup of verified dead code and legacy paths only when migration risk is understood.

## Aura Code Inventory

Largest Go areas by size from `internal/`:

| Package | Shape | Phase 19 read |
| --- | --- | --- |
| `internal/api` | Largest surface; dashboard/auth/read-write endpoints. | Touch only when a review/install flow needs an API. Avoid dashboard-first slices. |
| `internal/tools` | LLM tool layer; file tools, wiki/source/search/scheduler. | Main entry for `propose_skill_change`, toolset metadata, and better memory tools. |
| `internal/conversation` | Telegram/LLM loop support, prompts, archive, summarizer. | Keep prompt context small; route procedural learning through tools. |
| `internal/scheduler` | SQLite jobs, recurrence, `agent_job`, maintenance. | Extend jobs with skills/toolsets/context_from/wake_if_changed. |
| `internal/telegram` | Bot delivery, setup, handlers. | Do not add logic here unless user-facing delivery is needed. |
| `internal/wiki` | Store/schema/parser/frontmatter/migration. | Keep `Body` field and `[[slug]]` links; legacy YAML support is migration code, not dead yet. |
| `internal/swarm` | Store/manager/planner/synthesis. | Add Hermes-like toolset profiles before expanding subagent power. |
| `internal/agent` | Bounded runner. | Keep; this is the right base for cron/swarm workers. |
| `internal/search` | Wiki search and embedding cache. | Legacy `.yaml` fallback remains until old wiki migration is retired intentionally. |

Command/debug harnesses are large but valuable:

- `cmd/debug_memory_quality`: canonical usefulness benchmark; keep live LLM path.
- `cmd/debug_swarm`: useful E2E swarm metrics; one stale hard-coded assignment helper was removed in this slice.
- `cmd/debug_ingest`, `cmd/debug_files`, `cmd/debug_tools`: keep as smoke harnesses.

## Dead Code And Legacy Findings

### Removed Now

- `cmd/debug_swarm/main.go`: removed unused `debugAssignments()`.
  - It was hard-coded pre-planner assignment data.
  - `cmd/debug_swarm` now builds plans through `swarm.BuildPlan`, so the helper was stale.

### Cleaned Now

Staticcheck-only hygiene:

- eliminated unused test assignments in `internal/api/settings_test.go`;
- escaped a literal control character in `internal/files/xlsx_test.go`;
- used the direct struct conversion in `internal/llm/openai.go`;
- normalized a capitalized error string in `internal/tools/ollama_web.go`;
- normalized capitalized debug harness errors in file/summarizer smoke commands.

### Keep For Now

- Wiki `.yaml` support in `internal/wiki`, `internal/search`, and `internal/telegram/setup.go`.
  - This is migration/backward compatibility, not safe dead code.
  - Remove only after an explicit check proves no production wiki has `.yaml` pages.
- `search_wiki`.
  - It is narrower than `search_memory`, but still intentionally exposed for wiki-only lookup and tests.
- Scheduler legacy migrations.
  - Existing `aura.db` files may need them.

### Investigate Later

- `web/node_modules/flatted/golang/pkg/flatted` is visible to `go list ./...`.
  - It should not become part of normal Go package inventory.
  - Do not delete `node_modules` blindly; prefer a repo-local mitigation after checking frontend install/build assumptions.
- Generated dashboard assets under `internal/api/dist`.
  - Leave untouched unless doing a frontend/build slice.

## Reuse Map: Picobot

Useful patterns:

- `D:\tmp\picobot\internal\agent\tools\skill.go`
  - Skill manager with sandboxed filesystem access via `os.Root`.
  - Direct `create_skill`, `list_skills`, `read_skill`, `delete_skill`.
  - Aura translation: keep containment ideas, but expose `propose_skill_change`, not direct model mutation.
- `D:\tmp\picobot\internal\agent\tools\write_memory.go`
  - Rejects heartbeat/status/no-op content before memory writes.
  - Aura translation: add pollution guardrails to proposal/memory quality flows, especially scheduled jobs.
- `D:\tmp\picobot\internal\heartbeat\service.go`
  - Background routine injects work into the agent loop.
  - Aura translation: use scheduler/watchers with wake-if-changed and propose-only writes; do not store heartbeat noise.

Do not copy:

- Picobot's direct flat `MEMORY.md` model. Aura's source -> wiki -> review queue stack is better.
- Picobot `spawn` pattern: local inspection shows it is not the reference to copy for Aura delegation.

## Reuse Map: Hermes Agent

Useful patterns:

- `D:\tmp\hermes-agent\toolsets.py`
  - Named toolsets grouped by domain (`web`, `skills`, `memory`, `delegation`, etc.).
  - Aura translation: introduce Aura toolset profiles for `agent_job` and swarm roles, instead of hand-maintaining allowlists everywhere.
- `D:\tmp\hermes-agent\tools\delegate_tool.py`
  - Isolated subagent contexts, restricted toolsets, blocked recursive/dangerous tools, active subagent registry, batch parallel mode.
  - Aura translation: keep Aura `internal/swarm`, but add blocked tool policy and clearer toolset profiles before increasing power.
- `D:\tmp\hermes-agent\cron\jobs.py`
  - Skill-backed cron jobs, persisted outputs, canonical multi-skill normalization, secure job paths.
  - Aura translation: extend `agent_job` with `skills`, `toolsets`, `context_from`, `wake_if_changed`, and persisted last outputs/metrics.
- `D:\tmp\hermes-agent\tools\skills_hub.py`
  - Skill source adapters, provenance, quarantine, lock/audit behavior, path normalization.
  - Aura translation: review-gated skill drafts with smoke prompts, provenance, install audit, and quarantine on failed smoke test.

Do not copy:

- Hermes as a full runtime replacement.
- Broad exec/filesystem authority without Aura's review/admin gates.
- Direct scheduled mutation of durable memory.

## Phase 19 Recommendation

### 19a: Code Inventory And Low-Risk Cleanup

Status: in progress in this slice.

Acceptance:

- inventory document created;
- staticcheck no longer reports the low-risk dead/hygiene items touched here;
- `go test ./...` / `go build ./...` / `go vet ./...` pass before commit.

### 19b: Procedural Memory Proposals

Implement `propose_skill_change` as the first real phase 19 feature.

Minimum shape:

- proposal kind: create/update/delete skill;
- draft `SKILL.md` with trigger rules, allowed tools, examples, and smoke prompt;
- provenance: source/wiki/conversation/tool/job/swarm evidence refs;
- review queue entry, not direct install;
- install only after approval and smoke test.

References to reuse:

- Picobot skill manager containment;
- Hermes skills hub provenance/quarantine;
- Aura existing summaries/proposals store and skills loader/admin install.

### 19c: Toolset Profiles For Jobs And Swarm

Extract current scattered allowlists into named Aura toolsets.

Minimum profiles:

- `memory_read`: `search_memory`, wiki/source read/lint tools;
- `wiki_review`: proposal tools, wiki read/lint, no direct write by default;
- `skills_read`: list/read/search skill catalog;
- `web_research`: web tools only when explicitly allowed;
- `scheduler_safe`: schedule/list/cancel/run-now with non-recursive guard in agent jobs.

References to reuse:

- Hermes `toolsets.py`;
- Hermes delegate blocked tools;
- existing Aura `internal/swarm/plan.go` and `internal/scheduler/agent_job.go`.

### 19d: Skill-Backed Agent Jobs

Extend scheduled routines with procedural memory and cheaper context.

Minimum fields:

- `skills`;
- `enabled_toolsets`;
- `context_from`;
- `wake_if_changed`;
- `last_output_ref` and metrics.

Default policy:

- fresh context per run;
- propose-only durable writes;
- schedule-mutating tools disabled unless explicitly allowed;
- no heartbeat/no-op memory proposals.

## Cleanup Policy

Remove code only when all are true:

1. usage search proves no runtime/test path;
2. the replacement path is already shipped;
3. migration/backward compatibility is not required;
4. targeted tests plus full Go verification pass.

Everything else goes into "investigate later", not "delete because old".
