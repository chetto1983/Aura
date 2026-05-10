# Runtime Answer Discipline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura answer naturally on Telegram while keeping shell/code/search tools internal by default, so tool results are evidence for synthesis and never the user-facing voice unless the user explicitly asks for raw output.

**Architecture:** No new runtime layer. Tighten the existing boundaries: `internal/tools.Registry` remains the tool catalog/search owner, `internal/agentloop` owns loop/finalization invariants, `internal/agentruntime` owns terminal/raw-output formatting, and `internal/telegram` owns channel-specific raw-output intent and smoke probes. The default hot surface keeps `tool_search`, but shell execution becomes discovered/admin-diagnostic rather than always visible in normal chat.

**Tech Stack:** Go, existing OpenAI-compatible function calling flow, existing Aura process sandbox in the Docker container, existing `cmd/debug_telegram_sandbox`, SQLite conversation archive for regression evidence.

---

## Status

Status: PLANNED  
Started: 2026-05-10  
Reason: live logs show Aura still leaks raw tool output and hidden-tool errors after the first natural-memory guardrail slice.

## Research Summary

### Online Sources

- OpenAI function calling describes the loop as: model requests a tool, the app executes it, the app sends tool output back, and the model produces the final response. Aura must therefore avoid treating tool output itself as the final response. Source: <https://developers.openai.com/api/docs/guides/function-calling>
- OpenAI allowed tools support restricting the model to a subset of available tools. Aura's `modelToolNames()` should use that as a product contract: only conversationally safe tools are hot-visible. Source: <https://developers.openai.com/api/docs/guides/function-calling>
- Anthropic tool use has the same client-tool pattern: execute client tools in the app, return `tool_result`, then let the model formulate the user answer. Source: <https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview>
- Anthropic tool definitions emphasize precise descriptions that say what a tool does, when to use it, and how it behaves. Aura's `execute_shell` description must explicitly say it is for explicit diagnostics/admin/developer requests, not conversational self-checks. Source: <https://platform.claude.com/docs/en/agents-and-tools/tool-use/define-tools>
- Anthropic's tool-writing guidance argues for intentionally defined tools, judicious context use, and eval-driven iteration. This maps directly to tool-output leak smokes and per-turn policy counters. Source: <https://www.anthropic.com/engineering/writing-tools-for-agents>
- Anthropic's agent architecture guidance favors simple composable patterns over framework sprawl. This supports fixing the existing loop/terminal/tool modules instead of adding `internal/toolsearch` or a second orchestrator. Source: <https://www.anthropic.com/engineering/building-effective-agents>

### Local Examples Checked

- `D:\tmp\hermes-agent\AGENTS.md`: skill commands are injected as user messages, not system prompt; tool schema descriptions must not mention unavailable tools; background terminal notifications have output verbosity modes. Lesson: tool output is an operational channel, not the assistant voice.
- `D:\tmp\hermes-agent\agent\tool_guardrails.py`: per-turn guardrails are side-effect-free and classify repeated failures/non-progress without exposing raw arguments. Lesson: Aura should add policy decisions to the existing loop, not scatter keyword patches.
- `D:\tmp\hermes-agent\model_tools.py`: dynamic tool definitions only reference actually available tools. Lesson: `execute_code`/`execute_shell` docs must not lure the model toward tools that are hidden or inappropriate.
- `D:\tmp\nanobot\nanobot\agent\runner.py` and `nanobot\utils\runtime.py`: tool results are appended back to the model; empty final responses get a finalization retry; repeated external/workspace failures become stop-retry guidance. Lesson: finalization recovery should be no-tools and user-facing.
- `D:\tmp\nanobot\nanobot\templates\SOUL.md` and `TOOLS.md`: persona and tool policy live in runtime overlays, not development `AGENTS.md`. Lesson: Aura needs default `SOUL.md`/`TOOLS.md` materialized or base-prompted, because `/workspace/AGENT.md` is intentionally not injected.
- `D:\tmp\picobot\internal\agent\loop.go`: Picobot falls back to `lastToolResult` when final content is empty. Lesson: do not copy this into Aura; it is the exact raw-output leak shape.
- `D:\tmp\picobot\internal\agent\tools\registry.go`: the simple registry boundary is good; the raw fallback is not. Lesson: keep registry ownership, tighten finalization.

## Live Regression Evidence

From Docker logs and archived Telegram turns:

- `Cosa sai di me?` found memory, then tried hidden `read_file`, then returned a technical hidden-tool message. This is a hidden-tool recovery bug.
- `Sei pienamente operativo...` and `Queste risposte non mi dicono niente` called `execute_shell` and returned raw workspace/runtime output. This is a shell visibility and terminal finalization bug.
- `Puoi anche scansionare le cartelle di rete?` called shell for a broad capability question and dumped `mount`/`df` output. This is a tool-intent policy bug.
- `search_memory` can still leak `Memory evidence...` and `Evidence envelope` if finalization fails or budget falls back. This is a final-answer sanitizer gap.
- `/workspace/AGENT.md` is not injected by design; `SOUL.md` and `TOOLS.md` are the overlay files, but the runtime workspace may not have them. This is a prompt/persona bootstrap gap.

## Non-Goals

- Do not reintroduce Pyodide.
- Do not create a new `internal/toolsearch` layer.
- Do not expose all MCP/raw provider tools in normal chat.
- Do not mutate the live container-owned SQLite DB from the host.
- Do not make `AGENT.md` or `AGENTS.md` part of the system prompt.
- Do not remove shell/code capability from Aura; make it intentional and synthesized.

## Target Invariants

- A user-facing final answer must not contain `Evidence envelope`, `Memory evidence for`, `exit_code`, `elapsed_ms`, `source_id`, `tokens_total`, `tool_calls`, raw JSON tool errors, mount tables, or command banners unless the user explicitly asks for raw output.
- `execute_shell` is not visible in the default normal chat surface. It is discovered through `tool_search` for explicit diagnostics/admin/developer work.
- `execute_code` may remain visible only if the prompt and terminal handler force natural synthesis; otherwise move it behind `tool_search` in the same slice.
- Hidden-tool rejection never becomes the final user answer when prior useful evidence exists.
- Broad capability questions are answered conversationally. Shell is allowed for explicit commands, diagnostics, tests, builds, logs, filesystem inspection, package installation, or user-requested raw output.
- Runtime persona/tool policy lives in `SOUL.md`/`TOOLS.md` or base prompt text, never in `AGENT.md`.

## File Structure

Modify:

- `internal/telegram/conversation.go` - shrink default hot tools, refine runtime prompt, wire final-answer guard.
- `internal/telegram/conversation_tool_exec.go` - classify raw-output intent and terminal handling metadata.
- `internal/telegram/conversation_terminal.go` - no-tool finalization for shell/code unless raw output was explicitly requested.
- `internal/agentruntime/terminal.go` - add raw-output detectors, safe fallback synthesis, and terminal formatter tests.
- `internal/agentloop/loop.go` - replace hidden-tool spiral final text with recoverable no-tool finalization.
- `internal/tools/exec.go` - tighten `execute_shell` description and category/search metadata.
- `cmd/debug_telegram_sandbox/main.go` - add gates for forbidden final fragments, forbidden tool calls, and raw-output intent.
- `scripts/test-agent-tool-search-smoke.ps1` - stop teaching shell discovery as a normal first smoke.
- `docs/implementation-tracker.md` - append each completed slice.

Create:

- `scripts/test-runtime-answer-discipline-smokes.ps1` - Docker-first regression matrix from the bad logs.

Possibly create:

- `internal/telegram/runtime_answer_policy.go` - small helpers only if keeping them in `conversation_tool_exec.go` makes that file noisy.
- `internal/telegram/runtime_answer_policy_test.go` - raw intent and shell eligibility tests.

## Implementation Tasks

### Task 1: Add Failing Regression Smokes First

**Files:**
- Modify: `cmd/debug_telegram_sandbox/main.go`
- Modify: `cmd/debug_telegram_sandbox/main_test.go`
- Create: `scripts/test-runtime-answer-discipline-smokes.ps1`

- [ ] **Step 1: Add final-fragment forbid gates**

Add CLI flags:

```go
forbidFinalFragments := flag.String("forbid-final-fragments", "", "comma-separated fragments that must not appear in final_text")
forbidTools := flag.String("forbid-tools", "", "comma-separated tool names that must not be called")
```

Validation shape:

```go
for _, fragment := range splitCSV(expectations.ForbiddenFinalFragments) {
	if strings.Contains(strings.ToLower(result.FinalText), strings.ToLower(fragment)) {
		return fmt.Errorf("final_text contains forbidden fragment %q", fragment)
	}
}
for _, name := range splitCSV(expectations.ForbiddenTools) {
	if slices.Contains(result.ToolCalls, name) {
		return fmt.Errorf("forbidden tool %q was called", name)
	}
}
```

Run:

```powershell
go test ./cmd/debug_telegram_sandbox -run "TestValidateDebugExpectations" -count=1
```

Expected before implementation: fail until the new fields and validation exist.

- [ ] **Step 2: Create the regression smoke script**

Create `scripts/test-runtime-answer-discipline-smokes.ps1` with these cases:

```powershell
Invoke-Smoke "memory natural answer" @(
  "-no-validate",
  "-prompt", "Cosa sai di me? Rispondi naturale, non stampare evidenze tecniche.",
  "-expect-tools", "search_memory",
  "-forbid-tools", "execute_shell,read_file",
  "-forbid-final-fragments", "Memory evidence for,Evidence envelope,source:,score=,tool_search",
  "-expect-llm-calls-max", "4",
  "-expect-tool-calls-max", "2",
  "-max-elapsed-ms", "60000"
)

Invoke-Smoke "capability question no shell" @(
  "-no-validate",
  "-prompt", "Puoi anche scansionare le cartelle di rete?",
  "-forbid-tools", "execute_shell,execute_code",
  "-forbid-final-fragments", "mount | grep,Filesystem,overlay,/var/lib,exit_code,elapsed_ms",
  "-expect-llm-calls-max", "2",
  "-expect-tool-calls-max", "0",
  "-max-elapsed-ms", "45000"
)

Invoke-Smoke "explicit raw command allowed" @(
  "-no-validate",
  "-prompt", "Esegui il comando pwd e mostrami l'output grezzo.",
  "-expect-tools", "tool_search,execute_shell",
  "-expect-execute-shell-calls-min", "1",
  "-expect-llm-calls-max", "4",
  "-max-elapsed-ms", "60000"
)
```

If `-expect-execute-shell-calls-min` does not exist, add it beside the existing max/min counters.

### Task 2: Remove `execute_shell` From The Hot Chat Surface

**Files:**
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/debug_smoke_test.go`
- Modify: `internal/tools/exec.go`
- Modify: `internal/tools/registry_test.go`

- [ ] **Step 1: Change `modelToolNames()`**

Target default:

```go
core := []string{"search_memory", "schedule_task", "tool_search", "execute_code"}
```

If Task 3 proves `execute_code` still leaks raw terminal output, reduce further:

```go
core := []string{"search_memory", "schedule_task", "tool_search"}
```

- [ ] **Step 2: Make shell discoverable only for explicit diagnostics**

Update `ExecuteShellTool.Description()` to include:

```text
Use only for explicit operator/developer diagnostics, commands, tests, builds, logs, filesystem inspection, package installation, or when the user asks to see exact raw command output. Do not use for ordinary conversation, broad capability questions, memory answers, or self-status unless the user asks to inspect the runtime/container.
```

- [ ] **Step 3: Keep registry search behavior deterministic**

Adjust `internal/tools/registry_test.go` so shell still ranks for queries like `run shell command`, but not for generic `sei operativo` or `cartelle di rete` if a negative test is practical.

Run:

```powershell
go test ./internal/tools ./internal/telegram -run "TestRegistrySearch|TestModelToolNames|TestDebugSmoke" -count=1
```

### Task 3: Replace Raw Terminal Return With No-Tool Synthesis

**Files:**
- Modify: `internal/agentruntime/terminal.go`
- Modify: `internal/agentruntime/terminal_test.go`
- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/conversation_terminal.go`
- Modify: `internal/telegram/debug_smoke_test.go`

- [ ] **Step 1: Add raw-output intent detection**

Implement a helper in Telegram or agentruntime:

```go
func UserRequestedRawOutput(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "output grezzo") ||
		strings.Contains(lower, "raw output") ||
		strings.Contains(lower, "mostrami l'output") ||
		strings.Contains(lower, "stampa l'output") ||
		strings.Contains(lower, "esegui il comando") ||
		strings.Contains(lower, "`")
}
```

Add table tests for:

- `Esegui pwd e mostrami l'output grezzo` => true.
- `Sei operativo?` => false.
- `Puoi scansionare cartelle di rete?` => false.
- `Fai diagnostica del container e riassumi` => false.

- [ ] **Step 2: Route `execute_shell` through no-tool finalization by default**

Replace this branch in `TerminalHandler`:

```go
if terminalTool == "execute_code" || terminalTool == "execute_shell" {
	response := formatTerminalExecuteCodeResult(lastToolResult)
	convCtx.AddAssistantMessage(response)
	return response, false, true
}
```

with:

```go
if terminalTool == "execute_shell" && !UserRequestedRawOutput(lastUserText) {
	response, delivered := b.finalizeTerminalToolWithNoToolLLM(ctx, c, convCtx, userID, placeholder, lastToolResult, &telegramStats)
	return response, delivered, true
}
if terminalTool == "execute_code" && !LooksLikePlainUserResult(lastToolResult) {
	response, delivered := b.finalizeTerminalToolWithNoToolLLM(ctx, c, convCtx, userID, placeholder, lastToolResult, &telegramStats)
	return response, delivered, true
}
```

Keep direct formatting only for raw-output requests and simple scalar/code results.

- [ ] **Step 3: Harden terminal fallback**

Expand `LooksLikeInternalToolResult` to catch:

```go
"filesystem", "mounted on", "/var/lib/", "overlay", "tmpfs", "--- stderr ---",
"stdout:", "stderr:", "cmd:", "cwd:", "workspace_root", "top_dirs_in_workspace"
```

Fallback for shell should be natural:

```go
return "Ho controllato l'ambiente e ho una risposta, ma evito di incollarti l'output tecnico grezzo."
```

Run:

```powershell
go test ./internal/agentruntime ./internal/telegram -run "TestFormatTerminal|TestTerminalToolFallback|TestRawOutputIntent|TestDebugSmoke" -count=1
```

### Task 4: Hidden Tool Recovery Must Finalize, Not Scold

**Files:**
- Modify: `internal/agentloop/loop.go`
- Modify: `internal/agentloop/loop_test.go`

- [ ] **Step 1: Add a failing test for useful prior evidence**

Test shape:

```go
func TestRunHiddenToolRejectionFinalizesFromPriorEvidence(t *testing.T) {
	state := newFakeLoopState()
	client := newFakeLoopClient(
		responseWithTool("search_memory"),
		responseWithTool("read_file"),
		responseText("So una cosa utile, detta naturale."),
	)
	executor := agentloop.ToolExecutorFunc(func(ctx context.Context, calls []llm.ToolCall) agentloop.ExecutionSummary {
		if calls[0].Name == "search_memory" {
			state.AddToolResultMessage(calls[0].ID, "Memory evidence for ... useful fact")
			return agentloop.ExecutionSummary{LastResult: "Memory evidence for ... useful fact"}
		}
		return agentloop.ExecutionSummary{HiddenRejected: true}
	})
	result, _ := Run(ctx, client, executor, state, Options{AllowNoToolFinalization: true, SpiralBreakerEnabled: true})
	if strings.Contains(result.Text, "tool_search") || strings.Contains(result.Text, "not available") {
		t.Fatalf("technical hidden-tool answer leaked: %q", result.Text)
	}
}
```

- [ ] **Step 2: Replace immediate spiral-breaker final with no-tool recovery**

Current behavior returns:

```go
"A tool you requested is not available in this turn..."
```

New behavior:

```go
if execution.HiddenRejected && opts.SpiralBreakerEnabled {
	stats.SpiralBreakerFired = true
	if opts.AllowNoToolFinalization {
		if answer, ok := finalizeAnswerAfterBudget(ctx, client, state, opts, &stats); ok {
			state.AddAssistantMessage(answer)
			return Result{Text: answer, Stats: stats}, nil
		}
	}
	// Only now use a natural fallback, no tool names.
	answer := "Ho gia abbastanza contesto per rispondere senza aprire altri strumenti. Se vuoi, posso fare una verifica piu tecnica dopo."
}
```

Run:

```powershell
go test ./internal/agentloop -run "TestRunHiddenTool|TestRunSpiralBreaker|TestRunMaxIterationFinalizes" -count=1
```

### Task 5: Add Final-Answer Sanitizer As Last Gate

**Files:**
- Modify: `internal/agentruntime/terminal.go` or create `internal/agentruntime/final_answer.go`
- Modify: `internal/agentruntime/terminal_test.go`
- Modify: `internal/agentloop/loop.go`

- [ ] **Step 1: Add `LooksLikeUnsafeFinalAnswer`**

Rules:

```go
func LooksLikeUnsafeFinalAnswer(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"memory evidence for", "evidence envelope:", "exit_code:", "elapsed_ms",
		"source_id", "tokens_total", `"ok":false`, `"tool_calls"`,
		"workspace_root", "top_dirs_in_workspace", "filesystem", "/var/lib/",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Use it in all fallback paths**

Replace any `return lastToolResult` fallback in `agentloop.Run` and terminal fallback with safe text or no-tool finalization.

Required removals:

- `if response == "" { response = lastToolResult }`
- `if result := strings.TrimSpace(lastToolResult); result != "" { return result }`
- terminal fallback returning raw if not currently classified internal.

Run:

```powershell
go test ./internal/agentloop ./internal/agentruntime -run "Fallback|Unsafe|Budget|Terminal" -count=1
```

### Task 6: Materialize Runtime Persona And Tool Policy

**Files:**
- Modify: runtime bootstrap path in `internal/telegram/setup.go` or existing workspace bootstrap module.
- Modify: `docs/container.md`
- Test: relevant setup/bootstrap test.

- [ ] **Step 1: Confirm overlay behavior stays correct**

Invariant:

- `AGENT.md` and `AGENTS.md` are readable workspace docs, not system prompt overlays.
- `SOUL.md`, `USER.md`, and `TOOLS.md` are the only file overlays.

- [ ] **Step 2: Bootstrap missing `SOUL.md` and `TOOLS.md`**

If absent from `/workspace`, create minimal defaults:

`SOUL.md`:

```markdown
# Soul

Aura risponde in modo naturale, diretto e umano. Usa i tool come strumenti interni: il risultato tecnico serve per capire, non per essere incollato all'utente. Quando non serve un tool, risponde direttamente.
```

`TOOLS.md`:

```markdown
# Tool Policy

Usa shell e codice solo per richieste esplicite di diagnostica, comandi, file, build, test, installazioni o verifiche tecniche. Dopo un tool, sintetizza sempre in linguaggio naturale salvo richiesta esplicita di output grezzo.
```

Run:

```powershell
go test ./internal/telegram -run "TestPromptOverlay|TestRuntimeWorkspace" -count=1
```

### Task 7: Update Smokes And Commit

**Files:**
- Modify: `scripts/test-agent-tool-search-smoke.ps1`
- Create: `scripts/test-runtime-answer-discipline-smokes.ps1`
- Modify: `docs/implementation-tracker.md`

- [ ] **Step 1: Stop normalizing shell discovery in the old smoke**

Replace the first smoke prompt:

```powershell
"Use tool_search to discover what shell and Python execution tools are available..."
```

with a code-only orchestration smoke. Put shell raw-output behavior only in the new explicit raw command smoke.

- [ ] **Step 2: Run verification**

Minimum:

```powershell
go test ./internal/agentruntime ./internal/agentloop ./internal/tools ./internal/telegram ./cmd/debug_telegram_sandbox -count=1
go test ./...
go build ./...
go vet ./...
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\test-runtime-answer-discipline-smokes.ps1
```

Docker:

```powershell
docker compose up -d --build aura
docker compose logs --tail 180 aura
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\test-runtime-answer-discipline-smokes.ps1
```

- [ ] **Step 3: Commit one slice**

```powershell
git add .planning/phases/11-runtime-answer-discipline/PLAN.md internal/agentruntime internal/agentloop internal/telegram internal/tools cmd/debug_telegram_sandbox scripts/test-runtime-answer-discipline-smokes.ps1 scripts/test-agent-tool-search-smoke.ps1 docs/implementation-tracker.md
git commit -m "slice runtime: enforce natural answer discipline"
```

## Acceptance Matrix

| Scenario | Expected tools | Forbidden final text | Expected behavior |
| --- | --- | --- | --- |
| `Cosa sai di me?` | `search_memory` max 2 | `Memory evidence`, `Evidence envelope`, `score`, `source:` | natural summary |
| `scenario test gestione richieste offerta pms` | `search_memory` max 2 | raw envelope/JSON | natural business answer |
| `Puoi scansionare cartelle di rete?` | none | `mount`, `df`, `Filesystem`, `/var/lib` | explains capability and asks for target if needed |
| `Sei operativo?` | none or `search_memory` only | `cwd`, `workspace_root`, `exit_code` | concise status in human language |
| `Esegui pwd e mostrami output grezzo` | `tool_search`, `execute_shell` | none | raw output allowed |
| hidden `read_file` after memory | no user-facing tool error | `tool_search`, `not available in this turn` | no-tool natural finalization |

## Execution Order

1. Task 1: regression smokes and debug gates.
2. Task 2: remove hot shell exposure.
3. Task 3: force terminal synthesis.
4. Task 4: hidden-tool recovery.
5. Task 5: final-answer sanitizer.
6. Task 6: runtime overlays.
7. Task 7: Docker smokes, tracker, commit.

## Self-Review

- The plan fixes the failures visible in the logs instead of declaring the runtime ready.
- It keeps `tool_search` inside `internal/tools.Registry`.
- It does not inject `AGENT.md` into the system prompt.
- It does not add a new orchestration layer.
- It preserves container shell/code autonomy for explicit technical work.
- It adds acceptance tests that fail on the exact bad behaviors: raw envelopes, raw shell dumps, hidden-tool scolding, and accidental shell use for broad conversational prompts.
