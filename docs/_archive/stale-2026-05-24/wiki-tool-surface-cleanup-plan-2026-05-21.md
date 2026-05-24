# Wiki Tool Surface Cleanup Plan - 2026-05-21

Role: phase-plan

## Goal

Make Aura's wiki and tool guidance harder to confuse by removing stale,
redundant, or retired tool names from active LLM-facing surfaces, while keeping
the underlying helper tools available where unified dispatcher tools still call
them internally.

This slice does not mutate runtime wiki data, source archives, Qdrant, SQLite,
or logs. It only updates code, defaults, fixtures, generated guidance, and
tests.

## Evidence

- `D:/Aura/prd.md`: wiki is graph-controlled memory, source archives are
  evidence, and tool reduction should preserve durable auditability.
- `D:/Aura/internal/agent/tools/registry`: production already exposes
  dispatcher tools such as `file`, `source`, `web`, `task`, and `doc`; many
  flat helper names remain as implementation details.
- `D:/tmp/graphify/AGENTS.md`: mature graph workflows prefer querying the
  existing graph/index first, then reading bounded evidence instead of raw
  dumps.
- `D:/tmp/graphify/ARCHITECTURE.md`: graphify keeps the pipeline staged,
  validates schema before graph build, and treats generated graph/wiki outputs
  as derived artifacts.
- `D:/tmp/openhuman/AGENTS.md`: OpenHuman keeps architecture docs, runtime
  capability catalogs, and validation gates close to changes; stale skill
  runtime assumptions are called out explicitly after runtime removal.

## RFC

Option A: delete all flat helper tools now.

- Rejected for this slice.
- Reason: dispatcher tools still delegate to helper structs. Deleting helpers
  mixes surface cleanup with internal architecture surgery and risks changing
  behavior.

Option B: hide retired helper names from active prompts, defaults, fixtures, and
generated wiki/source guidance, but keep helpers as internal implementation.

- Chosen.
- Reason: lowest-risk path. It removes model confusion without changing storage
  behavior, tool execution semantics, or source archive data.

Option C: add compatibility aliases for every retired name.

- Rejected for now.
- Reason: aliases would preserve the ambiguity this cleanup is meant to remove.
  Historical traces can still display old names as data.

Option D: run a wiki data cleanup immediately.

- Deferred.
- Reason: repo rules require preserving user data. Runtime wiki mutations need a
  dry-run report and explicit evidence before any write.

## Improvement Map

| Area | Current issue | Action | Status |
| --- | --- | --- | --- |
| Runtime prompt | System and skill prompts mention `read_file` / `schedule_task` style names. | Use dispatcher examples: `file(action=...)`, `task(action=...)`. | Done in runtime-guidance slice. |
| Workspace hints | Directory/read recovery hints mention flat file helpers. | Point model to `file(action=\"list\")` and `file(action=\"read\")`. | Done in runtime-guidance slice. |
| Wiki/source hints | Wiki/source guidance points to `read_source`. | Use `source(action=\"read\", source_id=...)`. | Done in runtime-guidance slice; ingest pipeline pending. |
| Subagent write proposals | Default allowlist includes retired or nonexistent helper names. | Allow canonical tools only: `web`, memory recall/search, `wiki_subgraph`, and proposal tooling. | Done in runtime-guidance slice. |
| Defaults | `internal/config/defaults/AGENT.md` and `TOOLS.md` still advertise mixed flat/unified surfaces. | Rewrite active defaults around canonical dispatchers. | Subagent Task A complete, pending parent review. |
| Debug harnesses | `cmd/debug_tools` and `cmd/debug_ingest` register flat helper tools and request retired names. | Register unified tools and update scenarios. | Subagent Task A complete, pending parent review. |
| Retrieval fixture | The legacy fixture expects flat tool names for file/source/web/task/doc actions. | Update expected tool sequence to canonical dispatcher names. | Subagent Task A complete, pending parent review. |
| Workspace helper factory | `NewWorkspaceFileTools` keeps debug-only flat registration alive. | Remove only if no non-test callers remain after debug harness conversion. | Subagent Task A complete, pending parent review. |
| Generated ingest pages | Source ingest page body and OCR recovery errors mention `ingest_source`, `read_source`, `ocr_source`. | Emit canonical `source(action=...)` instructions. | Subagent Task B in progress. |
| Probe cases | Some probe prompts still ask for `ingest_source` / `ocr_source`. | Convert probes to `source(action=\"reprocess\")` once source dispatcher probe expectations are confirmed. | Follow-up unless Task C flags as must-fix. |
| Historical docs/tests | Many old names are valid as history, telemetry data, or helper unit tests. | Do not mass-edit. Categorize and leave unless active prompt/default/fixture. | Q&A categorization pending. |
| Wiki data hygiene | Actual wiki pages may contain stale tool instructions. | Produce a dry-run source/wiki lint report first; no write without explicit review. | Deferred. |

## Atomic Commit Plan

1. `docs(wiki): map tool surface cleanup plan`
   - Add this plan and evidence map only.
   - Verification: document review plus `git diff --check`.

2. `chore(tools): canonicalize runtime guidance`
   - Runtime prompt, skill loader text, workspace hints, source archive hints,
     subagent allowlist, and focused tests.
   - Verification:
     `go test ./internal/conversation ./internal/skills ./internal/agent/tools/registry ./internal/wiki ./internal/storage/memoryindex/audit ./internal/swarm ./internal/chat`

3. `chore(tools): canonicalize debug defaults and fixtures`
   - Defaults, debug harnesses, retrieval fixture, and debug-only helper
     registration cleanup.
   - Verification:
     `go test ./internal/agent/tools/registry ./cmd/debug_tools ./cmd/debug_ingest`

4. `fix(wiki): canonicalize ingest source guidance`
   - Generated ingest wiki/source text and ingest tests.
   - Verification:
     `go test ./internal/storage/sources/ingest`

5. `test(wiki): targeted cleanup Q&A`
   - No broad rerun by default.
   - Run targeted stale-name scan over active surfaces.
   - Run targeted package tests for changed areas.
   - Run `git diff --check`.
   - Escalate to broader `go test ./internal/agent/...` or `go build ./...`
     only if review or compile boundaries justify it.

## Q&A Questions

Q1: Are retired names still shown to the model in active prompts/defaults?

- Answer by scanning active prompt/default/fixture/generated-output files only.
- Do not count historical docs, helper implementation names, or telemetry test
  data as failures.

Q2: Did the unified dispatcher behavior change?

- Answer with focused package tests around registry, conversation prompt, skill
  loader, source ingest, swarm, and chat surfaces.

Q3: Did we mutate user knowledge or source data?

- Answer with git status: only source/config/docs/test files should be changed.
  Runtime wiki/source/archive/database/log files must remain untouched.

Q4: Are any follow-up improvements intentionally left out?

- Expected follow-ups: probe prompt conversion if still stale, wiki data dry-run
  lint, and deeper helper deletion only after dispatcher internals are mapped.

## Out Of Scope

- No runtime wiki rewrite.
- No source archive reprocessing.
- No Ralph queue story completion.
- No full repository refactor.
- No branch, PR, or push.
