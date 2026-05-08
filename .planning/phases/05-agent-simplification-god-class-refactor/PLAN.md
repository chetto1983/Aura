# Agent Simplification And God Class Refactor Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `aura-implementation` before executing this plan. Use one small commit per task. Preserve user changes. Do not mutate `data/aura.db` from the host while the Compose `aura` service is running.

**Goal:** Simplify Aura's agent loop by removing ritual guardrails, then split the current conversation god class into small, testable orchestration components before adding bounded workspace file tools.

**Architecture:** Aura should behave more like Codex/Picobot: a compact agent loop, a plain tool registry, recoverable tool errors, and runtime safety enforced at tool boundaries. Prompt/profile policy should guide behavior but never deadlock tool use. The main refactor target is `internal/telegram/conversation.go`, currently 1565 lines and responsible for routing, prompt assembly, tool loop, tool execution, terminal handling, archive writes, telemetry, fallback formatting, and snapshot storage.

**Tech Stack:** Go, Telegram bot runtime, existing `internal/tools.Registry`, existing `internal/orchestration` package, SQLite-backed conversation archive, React dashboard settings.

---

## Current Findings

- `internal/telegram/conversation.go` is the primary god file at 1565 lines.
- `internal/orchestration/orchestration.go` is the secondary policy god file at 569 lines.
- The current code has overlapping enforcement layers: profile allowlist, hidden-tool hook, explicit hidden-tool check, skill preflight, terminal-tool policy, and profile loop caps.
- Conversation logs show real failures from this complexity:
  - repeated `search_memory` calls for simple recall;
  - max-loop fallback instead of a useful answer;
  - skill proposal approval confusion;
  - tool preflight deadlocks with nonexistent skill names.
- Picobot's shape is simpler: one registry, one loop, workspace-bounded tools, direct memory/source operations.
- Hermes' useful guardrails are loop hygiene, not ritual preflight: duplicate tool-call capping, JSON/tool recovery, bounded delegation, final no-tool summary on max iteration.

## Non-Negotiable Safety Rails To Keep

- Telegram/dashboard auth and allowlist.
- Dashboard bearer tokens and secret redaction.
- Admin gates for install/delete skill, settings mutation, restart, dashboard token, and any future destructive workspace action.
- Path containment for any workspace file tool.
- Deny `.env`, live DB/WAL/SHM, binaries, generated raw OCR artifacts, and broad recursive deletion.
- Docker-first DB rule: do not mutate `data/aura.db` from host while Aura Compose service is running.
- Timeouts, loop budgets, and compact logging.

## Target Shape

### Agent Loop Packages

Create or reshape these boundaries:

- `internal/agentloop`
  - Owns model/tool iteration.
  - Does not know Telegram.
  - Accepts messages, tool definitions, executor, loop options.
  - Returns final text plus telemetry.

- `internal/agentloop/tool_exec.go`
  - Executes one batch of tool calls.
  - Deduplicates identical calls.
  - Converts unavailable tools into recoverable tool results.
  - No skill preflight enforcement.

- `internal/agentloop/fallback.go`
  - Builds max-iteration finalization messages.
  - Produces no-tool final summary from gathered tool results.

- `internal/telegram/conversation.go`
  - Shrinks to Telegram transport orchestration only:
    - load context;
    - select prompt/toolset;
    - call `agentloop.Run`;
    - send/edit Telegram messages;
    - archive turn.

- `internal/orchestration`
  - Becomes prompt/toolset selection only.
  - No runtime blocking except toolset selection.

## Execution Log

- 2026-05-08 process correction: active execution must explicitly use `using-superpowers`, `aura-implementation`, and `executing-plans`; use `subagent-driven-development` when dispatching independent implementation/review tasks. `D:\Aura\AGENTS.md` is the project agent contract, while this plan is the slice execution contract. Each slice must start with the Aura Ralph status check, update this plan before/after work, verify, update the tracker when behavior changes, and commit atomically by explicit paths.
- 2026-05-08: Persisted the mandatory skill-driven work protocol into `AGENTS.md` so future sessions remember it before reading phase-specific plans.
- 2026-05-08: Merged `codex/v31-closure-gate` into `master` via fast-forward at `62d0d9a`, then created `codex/simplify-agent-god-classes`.
- 2026-05-08: Focused baseline passed: `go test ./internal/orchestration ./internal/telegram ./internal/api ./internal/settings ./internal/config`.
- 2026-05-08: Commit `b0fb0cc` removed required skill preflight. `AURA_SKILL_PREFLIGHT` now defaults to `off`, settings expose only `off|advisory`, and tool execution no longer blocks on `read_skill`.
- 2026-05-08: Commit `05ad47d` defanged `swarm_research`: it no longer requires swarm availability, no longer hides direct wiki/source/memory reads, and no longer declares `run_aurabot_swarm` as terminal.
- 2026-05-08: Commit `e1e4536` made hidden/unavailable tool calls recoverable instead of fatal.
- 2026-05-08: Removed remaining dead terminal-swarm helpers and tests from `internal/telegram/conversation.go` and `internal/telegram/debug_smoke_test.go`; verified with `go test ./internal/telegram`.
- 2026-05-08: Replaced swarm-specific duplicate capping with generic duplicate tool-call capping keyed by tool name plus canonical JSON arguments; removed terminal-swarm telemetry from Telegram/debug sandbox. Verified with `go test ./internal/telegram`, `go test ./cmd/debug_telegram_sandbox`, and `go test ./internal/orchestration ./internal/telegram ./internal/api ./internal/settings ./internal/config ./cmd/debug_telegram_sandbox`.
- 2026-05-08: Moved generic duplicate tool-call dedupe out of `internal/telegram/conversation.go` into `internal/agentloop/dedupe.go` with focused canonical-argument tests. Verified with `go test ./internal/agentloop`, `go test ./internal/telegram`, and `go test ./internal/agentloop ./internal/orchestration ./internal/telegram ./internal/api ./internal/settings ./internal/config ./cmd/debug_telegram_sandbox`.
- 2026-05-08: Split terminal-tool finalization and formatting helpers out of `internal/telegram/conversation.go` into `internal/telegram/conversation_format.go`; `conversation.go` is down to 1077 lines. Verified with `go test ./internal/telegram` and `go test ./internal/agentloop ./internal/orchestration ./internal/telegram ./internal/api ./internal/settings ./internal/config ./cmd/debug_telegram_sandbox`.

## Task 0: Baseline And Branch

**Files:**
- Modify: none.

- [x] Step 1: Check working tree.

Run:

```powershell
git status --short -uall
```

Expected: either clean, or only unrelated user files that must not be touched.

- [x] Step 2: Create branch.

Run:

```powershell
git switch -c codex/simplify-agent-god-classes
```

Expected: branch created.

- [x] Step 3: Run focused baseline.

Run:

```powershell
go test ./internal/orchestration ./internal/telegram ./internal/api ./internal/settings ./internal/config
```

Expected: pass before refactor.

- [ ] Step 4: Run full baseline.

Run:

```powershell
go test ./...
```

Expected: pass or document existing failures before edits.

Status: deferred. Focused baseline passed before edits; full `go test ./...` has not yet been run in this cleanup branch.

## Task 1: Remove Required Skill Preflight

**Files:**
- Modify: `internal/orchestration/skill_policy.go`
- Modify: `internal/orchestration/skill_policy_test.go`
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/debug_smoke_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/settings/applier.go`
- Modify: `internal/settings/defaults.go`
- Modify: `internal/settings/*_test.go`
- Modify: `internal/api/settings.go`
- Modify: `internal/api/settings_test.go`
- Modify: `.env.example`
- Modify: `compose.yaml`

- [x] Step 1: Change config semantics.

Remove `required` from user-facing settings. Keep only:

```go
const DefaultSkillPreflight = "off"
```

Allowed values should be:

```go
case "advisory", "off":
```

Invalid values should normalize to `off`.

- [x] Step 2: Remove runtime blocking.

Delete this behavior from the live loop:

```go
if decision := b.skillPreflightDecision(...); decision.Required && !decision.Satisfied {
    // block tool execution
}
```

The replacement is no runtime block. Optional advisory can be prompt-only.

- [x] Step 3: Rewrite tests.

Delete tests whose only assertion is that a tool is blocked until `read_skill` is called.

Keep tests that verify:

- skill tools can still be listed/read;
- prompts can mention relevant skills;
- no live tool call is blocked by missing skill reads.

- [x] Step 4: Verify.

Run:

```powershell
go test ./internal/orchestration ./internal/telegram ./internal/config ./internal/settings ./internal/api
```

Expected: pass.

- [x] Step 5: Commit.

Run:

```powershell
git add internal/orchestration/skill_policy.go internal/orchestration/skill_policy_test.go internal/telegram/conversation.go internal/telegram/debug_smoke_test.go internal/config/config.go internal/config/config_test.go internal/settings/applier.go internal/settings/defaults.go internal/settings/applier_test.go internal/settings/defaults_test.go internal/api/settings.go internal/api/settings_test.go .env.example compose.yaml
git commit -m "simplify: remove required skill preflight"
```

## Task 2: Collapse Agent Profiles Into Toolsets

**Files:**
- Modify: `internal/orchestration/orchestration.go`
- Modify: `internal/orchestration/capabilities.go`
- Modify: `internal/orchestration/loop_policy.go`
- Modify: `internal/orchestration/*_test.go`
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/debug_smoke_test.go`
- Modify: `internal/api/settings.go`
- Modify: `internal/api/settings_test.go`

- [ ] Step 1: Replace profile taxonomy with toolsets.

Target toolsets:

```go
const (
    ToolsetDefault = "default"
    ToolsetCompute = "compute"
    ToolsetDocument = "document"
    ToolsetAdmin = "admin"
)
```

Meaning:

- `default`: chat, memory, wiki, source, search, web, proposal, skills read.
- `compute`: default + sandbox compute when available.
- `document`: default + typed file generation tools.
- `admin`: default + admin-only mutation tools.

- [ ] Step 2: Remove `swarm_research` as a profile.

Keep `run_aurabot_swarm` as a normal tool in `default` when available. It must not force terminal execution or hide memory/source/wiki tools.

- [ ] Step 3: Remove `memory` and `admin_review` as separate cages.

Memory is part of default. Admin review is a tool permission concern, not a separate mental mode.

- [ ] Step 4: Keep routing as a hint only.

Routing can select a default toolset, but it must not make later tool use impossible if the tool is safe and exposed.

- [ ] Step 5: Verify.

Run:

```powershell
go test ./internal/orchestration ./internal/telegram ./internal/api
```

Expected: pass.

- [ ] Step 6: Commit.

Run:

```powershell
git add internal/orchestration internal/telegram/conversation.go internal/telegram/debug_smoke_test.go internal/api/settings.go internal/api/settings_test.go
git commit -m "simplify: collapse agent profiles into toolsets"
```

## Task 3: Extract Agent Loop From Telegram Conversation

**Files:**
- Create: `internal/agentloop/loop.go`
- Create: `internal/agentloop/executor.go`
- Create: `internal/agentloop/dedupe.go`
- Create: `internal/agentloop/fallback.go`
- Create: `internal/agentloop/loop_test.go`
- Modify: `internal/telegram/conversation.go`

- [ ] Step 1: Define loop inputs.

Create `internal/agentloop/loop.go` with these types:

```go
package agentloop

import (
    "context"

    "aura/internal/llm"
)

type ChatClient interface {
    Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (*llm.Response, error)
}

type ToolExecutor interface {
    ExecuteToolCalls(ctx context.Context, calls []llm.ToolCall) ExecutionSummary
}

type Options struct {
    MaxIterations int
    AllowFinalNoToolSummary bool
}

type Result struct {
    Text string
    Stats Stats
}

type Stats struct {
    LLMCalls int
    ToolCalls int
    DuplicateToolCalls int
    MaxIterationsHit bool
}
```

- [ ] Step 2: Move the model/tool iteration.

Move the core loop from `Bot.runToolCallingLoop` into:

```go
func Run(ctx context.Context, client ChatClient, executor ToolExecutor, messages []llm.Message, tools []llm.ToolDefinition, opts Options) (Result, error)
```

Keep Telegram progress messages outside `agentloop`.

- [x] Step 3: Add generic duplicate tool-call dedupe.

Create `internal/agentloop/dedupe.go`:

```go
func DedupeToolCalls(calls []llm.ToolCall) (kept []llm.ToolCall, duplicates []llm.ToolCall)
```

The key is tool name plus canonical JSON args.

- [ ] Step 4: Add tests.

Test cases:

- no tool calls returns assistant text;
- one tool call appends tool result and continues;
- duplicate identical tool call executes once;
- max iteration produces final no-tool summary when enabled;
- unavailable tool result is recoverable.

- [ ] Step 5: Verify.

Run:

```powershell
go test ./internal/agentloop ./internal/telegram
```

Expected: pass.

- [ ] Step 6: Commit.

Run:

```powershell
git add internal/agentloop internal/telegram/conversation.go
git commit -m "refactor: extract agent loop from Telegram conversation"
```

## Task 4: Make Unavailable Tools Recoverable

**Files:**
- Modify: `internal/orchestration/hooks.go`
- Modify: `internal/orchestration/hooks_test.go`
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/agentloop/executor.go`
- Modify: `internal/agentloop/loop_test.go`

- [ ] Step 1: Remove fatal hidden-tool behavior.

Stop treating hidden/unavailable tool calls as fatal user-facing errors.

Replacement tool result:

```text
Tool unavailable in this runtime. Choose another available tool or answer from current context.
```

- [ ] Step 2: Keep registry truth.

If a tool is not registered, `Registry.Execute` can still return an error. The loop formats it as a recoverable tool result.

- [ ] Step 3: Verify.

Run:

```powershell
go test ./internal/orchestration ./internal/agentloop ./internal/telegram
```

Expected: pass.

- [ ] Step 4: Commit.

Run:

```powershell
git add internal/orchestration/hooks.go internal/orchestration/hooks_test.go internal/agentloop internal/telegram/conversation.go
git commit -m "simplify: make unavailable tools recoverable"
```

## Task 5: Remove Terminal Swarm Routing

**Files:**
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/swarmtools/tools.go`
- Modify: `internal/telegram/debug_smoke_test.go`
- Modify: `internal/orchestration/loop_policy.go`

- [ ] Step 1: Delete auto terminal swarm branch.

Remove behavior equivalent to:

```go
if profile == swarm_research && run_aurabot_swarm exposed {
    return runTerminalSwarm(...)
}
```

- [ ] Step 2: Treat swarm as normal tool.

`run_aurabot_swarm` may still be read-only internally, but the parent loop may continue with other safe tools afterward.

- [ ] Step 3: Remove swarm-specific duplicate cap.

The generic dedupe from Task 3 replaces `capDuplicateSwarmCalls`.

- [ ] Step 4: Verify.

Run:

```powershell
go test ./internal/telegram ./internal/swarmtools ./internal/orchestration
```

Expected: pass.

- [ ] Step 5: Commit.

Run:

```powershell
git add internal/telegram/conversation.go internal/telegram/debug_smoke_test.go internal/swarmtools/tools.go internal/orchestration/loop_policy.go
git commit -m "simplify: remove terminal swarm routing"
```

## Task 6: Shrink `conversation.go`

**Files:**
- Create: `internal/telegram/conversation_archive.go`
- Create: `internal/telegram/conversation_snapshot.go`
- Create: `internal/telegram/conversation_format.go`
- Create: `internal/telegram/conversation_context.go`
- Modify: `internal/telegram/conversation.go`
- Move tests from `debug_smoke_test.go` where useful into focused files.

- [ ] Step 1: Move archive helpers.

Move these functions/types:

- `archiveTurnInput`
- `archiveConversationTurns`
- `archiveAppenderForTurn`

Target: `conversation_archive.go`.

- [ ] Step 2: Move snapshot helpers.

Move:

- `orchestrationSnapshot`
- `storeOrchestrationSnapshot`
- `loadOrchestrationSnapshot`
- `pruneOrchestrationSnapshots`

Target: `conversation_snapshot.go`.

- [ ] Step 3: Move formatting helpers.

Move:

- `looksLikeToolCallMarkup`
- `looksLikeInternalToolResult`
- terminal/swarm formatting helpers still remaining after simplification
- `toolActivityMessage`
- `artifactNamesFromSandboxResult`

Target: `conversation_format.go`.

- [ ] Step 4: Move context/search helpers.

Move:

- `runSpeculativeSearch`
- `speculativeSearchTimeout`
- `latestUserMessage`
- small context-loading helpers introduced during refactor

Target: `conversation_context.go`.

- [ ] Step 5: Keep `conversation.go` below 500 lines.

`conversation.go` should contain:

- `handleConversation`;
- Telegram progress lifecycle;
- call into orchestration/toolset selection;
- call into `agentloop.Run`;
- final send/archive.

- [ ] Step 6: Verify.

Run:

```powershell
go test ./internal/telegram
```

Expected: pass.

- [ ] Step 7: Commit.

Run:

```powershell
git add internal/telegram/conversation.go internal/telegram/conversation_archive.go internal/telegram/conversation_snapshot.go internal/telegram/conversation_format.go internal/telegram/conversation_context.go internal/telegram/*_test.go
git commit -m "refactor: split Telegram conversation god file"
```

## Task 7: Fix Skill Approval Semantics

**Files:**
- Modify: `internal/api/summaries.go`
- Modify: `internal/api/summaries_test.go`
- Modify: `internal/api/types.go`
- Modify: `internal/skills/admin.go`
- Modify: `internal/skills/admin_test.go`
- Modify: `internal/tools/skill_proposal.go`
- Modify: `web/src/components/SummariesPanel.tsx`
- Modify: `web/src/types/api.ts`

- [ ] Step 1: Choose final product semantics.

Recommended semantics:

- Wiki proposal approval applies wiki mutation.
- Skill proposal approval installs/updates/deletes the local skill only when the authenticated dashboard user is admin and skill admin is enabled.
- If admin install is unavailable, the status must be `reviewed`, not `approved`.

- [ ] Step 2: Implement local skill proposal installer.

Add an interface that can apply already-validated `SkillProposal` content to the primary skills root.

Rules:

- create/update writes `<skillsRoot>/<name>/SKILL.md`;
- delete removes that skill directory only if it is inside the skills root;
- atomic write for create/update;
- no catalog `npx` dependency for local proposals.

- [ ] Step 3: Update API response copy.

Dashboard must never imply a skill is available unless `list_skills` would see it.

- [ ] Step 4: Verify.

Run:

```powershell
go test ./internal/api ./internal/skills ./internal/tools
npm --prefix web run build
```

Expected: pass.

- [ ] Step 5: Commit.

Run:

```powershell
git add internal/api/summaries.go internal/api/summaries_test.go internal/api/types.go internal/skills/admin.go internal/skills/admin_test.go internal/tools/skill_proposal.go web/src/components/SummariesPanel.tsx web/src/types/api.ts internal/api/dist
git commit -m "fix: make skill approval apply or say reviewed"
```

## Task 8: Add Bounded Workspace File Tools

**Files:**
- Create: `internal/workspace/root.go`
- Create: `internal/workspace/root_test.go`
- Create: `internal/tools/workspace_files.go`
- Create: `internal/tools/workspace_files_test.go`
- Modify: `internal/telegram/bot.go`
- Modify: `internal/orchestration/orchestration.go`
- Modify: `.env.example`
- Modify: `compose.yaml`

- [ ] Step 1: Add workspace root config.

Add config:

```text
AURA_WORKSPACE_TOOLS=disabled
AURA_WORKSPACE_ROOT=/app
```

For local development, root can be `D:\Aura`. In Docker, root is `/app`.

- [ ] Step 2: Implement path containment.

`internal/workspace.Root` must provide:

```go
Resolve(rel string) (string, error)
Read(rel string, maxBytes int) ([]byte, error)
WriteAtomic(rel string, content []byte) error
Search(pattern string, globs []string, limit int) ([]SearchMatch, error)
```

Hard deny:

- `.env`
- `data/aura.db`
- `data/aura.db-wal`
- `data/aura.db-shm`
- `.git/`
- `wiki/raw/`
- executable/binary extensions unless read-only and small

- [ ] Step 3: Add tools.

Expose:

- `list_files`
- `read_file`
- `search_files`
- `write_file`
- `apply_patch`

Do not expose broad shell yet.

- [ ] Step 4: Register only when enabled.

Default remains disabled until manual config enables it.

- [ ] Step 5: Verify.

Run:

```powershell
go test ./internal/workspace ./internal/tools ./internal/telegram ./internal/orchestration
```

Expected: pass.

- [ ] Step 6: Commit.

Run:

```powershell
git add internal/workspace internal/tools/workspace_files.go internal/tools/workspace_files_test.go internal/telegram/bot.go internal/orchestration/orchestration.go .env.example compose.yaml
git commit -m "feat: add bounded workspace file tools"
```

## Task 9: Final Verification And Tracker Update

**Files:**
- Modify: `docs/implementation-tracker.md`
- Modify: `.planning/STATE.md`

- [ ] Step 1: Run Go verification.

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1
```

Expected: fmt, test, build, vet pass.

- [ ] Step 2: Run frontend verification if dashboard changed.

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-web.ps1
```

Expected: i18n/build pass.

- [ ] Step 3: Run focused live/debug smoke.

Run:

```powershell
go run ./cmd/debug_orchestration
go run ./cmd/debug_tools
```

Expected: no required skill preflight, no swarm terminal cage, normal tools available.

- [ ] Step 4: Update tracker.

Record:

- removed preflight required;
- collapsed profiles;
- extracted agent loop;
- split conversation god file;
- fixed skill approval semantics;
- added workspace file tools if completed;
- verification commands and results.

- [ ] Step 5: Commit docs.

Run:

```powershell
git add docs/implementation-tracker.md .planning/STATE.md .planning/phases/05-agent-simplification-god-class-refactor/PLAN.md
git commit -m "docs: record agent simplification plan and results"
```

## Execution Notes

- Do not start with workspace file tools. First remove the policy maze.
- Do not refactor dashboard god components in this phase unless a backend API change forces it.
- Do not rewrite `internal/tools/files.go` yet; document/file generation is large but bounded and not the cause of agent confusion.
- Prefer moving code before changing behavior when splitting `conversation.go`.
- Keep each commit shippable and testable.

## Deferred Follow-Ups

- Split large dashboard components:
  - `web/src/components/SourceInbox.tsx`
  - `web/src/components/TasksPanel.tsx`
  - `web/src/components/SettingsPanel.tsx`
- Split `internal/wiki/memory_hygiene.go`.
- Split `internal/tools/files.go` into `xlsx`, `docx`, and `pdf` tool files.
- Consider replacing speculative per-turn search with explicit model-driven search only.
