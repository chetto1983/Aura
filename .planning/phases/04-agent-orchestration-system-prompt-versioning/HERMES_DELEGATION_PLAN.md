# Hermes-Style Delegation Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura's swarm route behave like a bounded delegation tool: fast, read-only, traceable, and unable to keep looping through unrelated parent tools after the swarm returns.

**Architecture:** Keep Aura's main LLM as the orchestrator, but make the runtime own delegation limits, child toolsets, finalization, token/cost aggregation, and stale worker hygiene. This follows the Hermes Agent pattern: filtered toolsets, a blocking delegation tool, isolated children, capped fan-out, deduped delegation calls, and runtime-owned budgets.

**Tech Stack:** Go, SQLite settings, existing Aura swarm tools, `internal/orchestration`, `internal/telegram`, `internal/swarmtools`, `cmd/debug_telegram_sandbox`, Docker release runtime.

---

## Summary

Aura's live v3.1 swarm smoke proved the right high-level route, but exposed the wrong runtime behavior:

- `swarm_research` selected correctly.
- `run_aurabot_swarm` was called.
- The parent LLM still continued direct reads and extra tool loops after the swarm result.
- Missing source artifacts caused repeated `read_source` failures.
- The turn hit max iteration behavior.
- Token and cost metrics were not reported.
- Total latency was too high for a closure gate.

The fix is not a prompt-only patch and not a separate intent model. Hermes Agent shows a better runtime shape:

- Toolsets are filtered before the model sees tools.
- Delegation is a blocking aggregate tool.
- Child agents receive isolated context and restricted toolsets.
- Depth, timeout, max children, iterations, and child roles are controlled by runtime config, not by model arguments.
- Delegation calls are capped and deduped before execution.
- Parent receives a compact child summary, not every child detail.
- Child cost and usage roll up into the parent turn.

This plan adapts that pattern to Aura.

## Current Evidence

Local and upstream Hermes research used for this plan:

- Hermes repo: https://github.com/NousResearch/hermes-agent
- Hermes `AIAgent.run_conversation()` keeps a synchronous tool loop with iteration budget.
- Hermes `model_tools.get_tool_definitions()` filters tools by enabled and disabled toolsets.
- Hermes `tools/delegate_tool.py` implements bounded child delegation with max depth, max children, child timeouts, blocklists, and aggregated results.
- Hermes caps and dedupes delegate calls before running them.
- Hermes warns that child reports are self-reports; callers should request verifiable handles when needed.

Aura-specific current files:

- `internal/orchestration/orchestration.go`
  - Owns profile selection and profile tool allowlists.
- `internal/telegram/conversation.go`
  - Owns the Telegram LLM loop, tool execution, and debug telemetry.
- `internal/swarmtools/tools.go`
  - Owns the `run_aurabot_swarm` tool implementation and result surface.
- `cmd/debug_telegram_sandbox/main.go`
  - Owns live Telegram-style smoke checks and expectation flags.
- `cmd/debug_orchestration/main.go`
  - Owns deterministic profile routing checks.

## User Decisions

- No separate intent model for this slice.
- Do not patch randomly; use a research-backed plan.
- Swarm remains useful but must become faster.
- The parent agent must not see too many confusing tools.
- Old worker aliases and stale slug references are a real bug to remove.
- Token, cost, latency, worker count, and quality metrics must be visible.
- Docker is the release runtime.

## Target Behavior

For broad prompts such as:

```text
facciamo il punto di tutta la pipeline Aura e dimmi cosa manca per chiudere v3.1
```

Aura should:

1. Select `swarm_research`.
2. Expose only delegation tools to the parent:

```text
run_aurabot_swarm
read_swarm_result
list_swarm_tasks
```

3. Run one bounded swarm.
4. Aggregate worker summaries, evidence handles, token usage, cost, worker failures, and elapsed time.
5. Finalize with either:
   - a direct terminal response from the swarm aggregate, or
   - one final no-tool LLM pass using only the aggregate.
6. Never continue into direct parent `read_source`, `read_wiki`, `search_memory`, file generation, settings, or admin tools inside the same `swarm_research` turn.

## Runtime Policy

Runtime-owned defaults:

- `SWARM_RESEARCH_MAX_WORKERS=1`
- `SWARM_RESEARCH_TIMEOUT_MS=25000`
- `SWARM_RESEARCH_CHILD_MAX_ITERATIONS=3`
- `SWARM_RESEARCH_FINALIZATION=aggregate|no_tool_llm`
- `SWARM_RESEARCH_DEDUPE_WINDOW=turn`
- `SWARM_RESEARCH_MAX_RESULT_CHARS=12000`

Settings may be added to dashboard later if needed, but this slice should start with conservative defaults and environment overrides.

Model-owned arguments must not override hard runtime limits. If the model asks for 20 workers, deep recursion, or long timeouts, Aura clamps to runtime config and logs the clamp.

## File Map

- Modify: `internal/orchestration/orchestration.go`
  - Narrow `swarm_research` parent tool allowlist and add route metadata for terminal delegation.
- Modify: `internal/orchestration/orchestration_test.go`
  - Lock exact swarm parent tool surface and route metadata.
- Modify: `internal/swarmtools/tools.go`
  - Add bounded delegation config, stale alias hygiene, worker metrics, and aggregate result shape.
- Modify or create: `internal/swarmtools/delegation_policy.go`
  - Keep runtime clamps, worker role policy, and stale alias validation out of the large tool file if the existing layout allows it.
- Modify or create: `internal/swarmtools/delegation_policy_test.go`
  - Test clamp behavior, dedupe behavior, and stale alias rejection.
- Modify: `internal/telegram/conversation.go`
  - Add terminal delegation finalization after `run_aurabot_swarm`.
- Modify: `internal/telegram/debug_smoke.go`
  - Report delegation finalization, worker count, worker failures, aggregate token/cost, and slow threshold.
- Modify: `cmd/debug_telegram_sandbox/main.go`
  - Add strict expectations for terminal swarm behavior and token/cost presence.
- Modify: `cmd/debug_orchestration/main_test.go`
  - Add deterministic route tests for common Italian and English prompts.
- Modify: `.planning/phases/04-agent-orchestration-system-prompt-versioning/VALIDATION.md`
  - Record measured before/after evidence.

## Public Interfaces

No new user-facing API is required for the first hardening slice.

Optional environment settings:

```env
SWARM_RESEARCH_MAX_WORKERS=1
SWARM_RESEARCH_TIMEOUT_MS=25000
SWARM_RESEARCH_CHILD_MAX_ITERATIONS=3
SWARM_RESEARCH_FINALIZATION=aggregate
SWARM_RESEARCH_MAX_RESULT_CHARS=12000
```

Debug command extensions:

```powershell
go run ./cmd/debug_telegram_sandbox `
  -timeout 90s `
  -no-validate `
  -expect-profile swarm_research `
  -expect-tools run_aurabot_swarm `
  -expect-swarm `
  -expect-terminal-swarm `
  -expect-token-metrics `
  -max-elapsed-ms 30000 `
  -prompt "facciamo il punto di tutta la pipeline Aura e dimmi cosa manca per chiudere v3.1"
```

Expected output must include:

```text
tool_profile=swarm_research
exposed_tools=run_aurabot_swarm,read_swarm_result,list_swarm_tasks
called_tools=run_aurabot_swarm
terminal_swarm=true
worker_count<=3
worker_failures=0
token_usage_reported=true
elapsed_ms<=30000
```

## Implementation Tasks

### Task 1: Lock Swarm Parent Tool Surface

**Files:**

- Modify: `internal/orchestration/orchestration.go`
- Modify: `internal/orchestration/orchestration_test.go`
- Modify: `cmd/debug_orchestration/main_test.go`

- [ ] Add a test named `TestSwarmResearchProfileExposesOnlyDelegationTools`.

```go
func TestSwarmResearchProfileExposesOnlyDelegationTools(t *testing.T) {
	got := ProfileToolNames(ProfileSwarmResearch)
	want := []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("swarm tools mismatch (-want +got):\n%s", diff)
	}
}
```

- [ ] If the project does not use `cmp`, use the existing local slice comparison helper instead of adding a dependency.
- [ ] Update `ProfileSwarmResearch` to expose only the three delegation tools.
- [ ] Add route tests for these prompts:

```text
facciamo il punto di tutta la pipeline
what is missing to close v3.1?
audit memory, embeddings, retrieval, and tool routing
```

- [ ] Run:

```powershell
go test ./internal/orchestration ./cmd/debug_orchestration -count=1
```

- [ ] Commit:

```powershell
git add internal/orchestration cmd/debug_orchestration
git commit -m "test: lock swarm delegation tool surface"
```

### Task 2: Add Delegation Policy And Runtime Clamps

**Files:**

- Create: `internal/swarmtools/delegation_policy.go`
- Create: `internal/swarmtools/delegation_policy_test.go`
- Modify: `internal/swarmtools/tools.go`

- [ ] Define `DelegationPolicy`.

```go
type DelegationPolicy struct {
	MaxWorkers         int
	Timeout           time.Duration
	ChildMaxIterations int
	MaxResultChars     int
	Finalization       string
}
```

- [ ] Add a `DefaultDelegationPolicy()` with conservative defaults.

```go
func DefaultDelegationPolicy() DelegationPolicy {
	return DelegationPolicy{
		MaxWorkers:          1,
		Timeout:             25 * time.Second,
		ChildMaxIterations:  3,
		MaxResultChars:      12000,
		Finalization:        "aggregate",
	}
}
```

- [ ] Add `Clamp()` that enforces lower and upper bounds.

```go
func (p DelegationPolicy) Clamp() DelegationPolicy {
	if p.MaxWorkers < 1 {
		p.MaxWorkers = 1
	}
	if p.MaxWorkers > 3 {
		p.MaxWorkers = 3
	}
	if p.Timeout <= 0 || p.Timeout > 30*time.Second {
		p.Timeout = 25 * time.Second
	}
	if p.ChildMaxIterations < 1 || p.ChildMaxIterations > 3 {
		p.ChildMaxIterations = 3
	}
	if p.MaxResultChars < 2000 || p.MaxResultChars > 12000 {
		p.MaxResultChars = 12000
	}
	if p.Finalization != "aggregate" && p.Finalization != "no_tool_llm" {
		p.Finalization = "aggregate"
	}
	return p
}
```

- [ ] Load environment overrides without letting model arguments override hard caps.
- [ ] Test that excessive values are clamped.
- [ ] Test that invalid finalization falls back to `aggregate`.
- [ ] Run:

```powershell
go test ./internal/swarmtools -count=1
```

- [ ] Commit:

```powershell
git add internal/swarmtools
git commit -m "feat: add bounded swarm delegation policy"
```

### Task 3: Reject Stale Worker Aliases

**Files:**

- Modify: `internal/swarmtools/delegation_policy.go`
- Modify: `internal/swarmtools/delegation_policy_test.go`
- Modify: worker catalog code in `internal/swarmtools` or the package that owns worker role resolution.

- [ ] Add a catalog-backed worker role validator.

```go
type WorkerCatalog interface {
	HasRole(role string) bool
}

func ValidateWorkerRoles(roles []string, catalog WorkerCatalog) error {
	for _, role := range roles {
		if strings.HasSuffix(role, ".yaml") {
			return fmt.Errorf("stale worker alias %q: yaml slugs are not valid worker roles", role)
		}
		if !catalog.HasRole(role) {
			return fmt.Errorf("unknown worker role %q", role)
		}
	}
	return nil
}
```

- [ ] Add tests for stale aliases such as `golem.yaml` and legacy role names found in logs.
- [ ] Add tests for valid current roles.
- [ ] Ensure rejected aliases produce a clear tool error and do not start a swarm.
- [ ] Run:

```powershell
go test ./internal/swarmtools -count=1
```

- [ ] Commit:

```powershell
git add internal/swarmtools
git commit -m "fix: reject stale swarm worker aliases"
```

### Task 4: Cap And Dedupe Swarm Calls Per Turn

**Files:**

- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/debug_smoke.go`
- Modify: `internal/telegram/debug_smoke_test.go`

- [ ] Add per-turn detection for repeated `run_aurabot_swarm` calls.
- [ ] Permit only one `run_aurabot_swarm` call in a `swarm_research` turn.
- [ ] If the model asks for duplicate swarm calls, return a fatal tool error:

```text
run_aurabot_swarm already completed for this turn; use the existing aggregate result.
```

- [ ] Add a fake model test that emits two swarm tool calls.
- [ ] Verify the second call is rejected and telemetry records `duplicate_swarm_rejected=true`.
- [ ] Run:

```powershell
go test ./internal/telegram -count=1
```

- [ ] Commit:

```powershell
git add internal/telegram
git commit -m "feat: cap duplicate swarm calls per turn"
```

### Task 5: Add Terminal Swarm Finalization

**Files:**

- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/debug_smoke.go`
- Modify: `internal/telegram/debug_smoke_test.go`
- Modify: `cmd/debug_telegram_sandbox/main.go`

- [ ] After a successful `run_aurabot_swarm` in `swarm_research`, stop the ordinary tool loop.
- [ ] For `SWARM_RESEARCH_FINALIZATION=aggregate`, format the aggregate result directly as the final assistant answer.
- [ ] For `SWARM_RESEARCH_FINALIZATION=no_tool_llm`, make exactly one final LLM call with no tools and only the aggregate evidence.
- [ ] Add telemetry:

```text
terminal_swarm=true
swarm_finalization=aggregate|no_tool_llm
post_swarm_tool_calls=0
```

- [ ] Add `-expect-terminal-swarm` to `cmd/debug_telegram_sandbox`.
- [ ] Add `-max-elapsed-ms` to fail slow common prompts.
- [ ] Add tests that no direct `read_source`, `read_wiki`, `search_memory`, or `write_wiki` happens after terminal swarm.
- [ ] Run:

```powershell
go test ./internal/telegram ./cmd/debug_telegram_sandbox -count=1
```

- [ ] Commit:

```powershell
git add internal/telegram cmd/debug_telegram_sandbox
git commit -m "feat: finalize swarm turns without extra parent tools"
```

### Task 6: Aggregate Worker Metrics Into Parent Turn

**Files:**

- Modify: `internal/swarmtools/tools.go`
- Modify: `internal/telegram/debug_smoke.go`
- Modify: `cmd/debug_telegram_sandbox/main.go`
- Modify: LLM usage normalization code if token data is currently lost.

- [ ] Extend swarm aggregate JSON with:

```json
{
  "worker_count": 3,
  "worker_failures": 0,
  "tokens_prompt": 0,
  "tokens_completion": 0,
  "tokens_total": 0,
  "cost_usd": 0,
  "elapsed_ms": 0,
  "evidence_handles": []
}
```

- [ ] Roll child token/cost usage into parent debug smoke totals.
- [ ] Preserve `token_usage_reported=true` when any child reports usage.
- [ ] If the provider does not report usage, record `token_usage_reported=false` and an explicit reason.
- [ ] Add tests using fake child results with usage data.
- [ ] Run:

```powershell
go test ./internal/swarmtools ./internal/telegram ./cmd/debug_telegram_sandbox -count=1
```

- [ ] Commit:

```powershell
git add internal/swarmtools internal/telegram cmd/debug_telegram_sandbox
git commit -m "feat: aggregate swarm token and cost metrics"
```

### Task 7: Add Live Docker Swarm Closure Smoke

**Files:**

- Modify: `.planning/phases/04-agent-orchestration-system-prompt-versioning/VALIDATION.md`
- Modify: `cmd/debug_telegram_sandbox/main.go` if the live gate needs one missing flag.

- [ ] Confirm Docker is up:

```powershell
docker compose ps
```

- [ ] Run a deterministic route probe:

```powershell
go run ./cmd/debug_orchestration -prompt "facciamo il punto di tutta la pipeline Aura e dimmi cosa manca per chiudere v3.1"
```

- [ ] Run live Telegram-style swarm smoke with DB-selected model:

```powershell
$env:AURA_ENV_PATH='data\.env'
go run ./cmd/debug_telegram_sandbox `
  -timeout 90s `
  -no-validate `
  -expect-profile swarm_research `
  -expect-tools run_aurabot_swarm `
  -expect-swarm `
  -expect-terminal-swarm `
  -expect-token-metrics `
  -max-elapsed-ms 30000 `
  -prompt "facciamo il punto di tutta la pipeline Aura e dimmi cosa manca per chiudere v3.1"
```

- [ ] Required pass conditions:

```text
profile=swarm_research
called_tools contains only run_aurabot_swarm
terminal_swarm=true
post_swarm_tool_calls=0
elapsed_ms <= 30000
token_usage_reported=true
worker_count <= 3
worker_failures = 0
```

- [ ] Save exact command, model, elapsed time, worker count, called tools, token usage, cost, and failures into `VALIDATION.md`.
- [ ] If the gate fails, keep v3.1 open and record the blocker instead of promoting v4.0.
- [ ] Commit:

```powershell
git add .planning/phases/04-agent-orchestration-system-prompt-versioning/VALIDATION.md
git commit -m "docs: record swarm delegation validation"
```

## Test Plan

Unit:

```powershell
go test ./internal/orchestration -count=1
go test ./internal/swarmtools -count=1
go test ./internal/telegram -count=1
```

Integration:

```powershell
go test ./cmd/debug_orchestration ./cmd/debug_telegram_sandbox -count=1
go test ./internal/conversation ./internal/tools ./internal/toolsets -count=1
```

Docker smoke:

```powershell
docker compose config --quiet
docker compose up -d --build aura
Invoke-RestMethod http://127.0.0.1:18080/status
```

Live route gate:

```powershell
$env:AURA_ENV_PATH='data\.env'
go run ./cmd/debug_telegram_sandbox -timeout 90s -no-validate -expect-profile swarm_research -expect-tools run_aurabot_swarm -expect-swarm -expect-terminal-swarm -expect-token-metrics -max-elapsed-ms 30000 -prompt "facciamo il punto di tutta la pipeline Aura e dimmi cosa manca per chiudere v3.1"
```

## Release Gate

This sub-plan is complete only when:

- `swarm_research` parent tools are exactly `run_aurabot_swarm`, `read_swarm_result`, and `list_swarm_tasks`.
- `run_aurabot_swarm` is bounded by runtime-owned worker, timeout, iteration, and result-size limits.
- Stale worker aliases are rejected before any child starts.
- Duplicate swarm calls in one turn are rejected.
- A successful swarm turn finalizes without post-swarm parent tool calls.
- Worker token/cost/latency metrics roll up into the parent debug report.
- The live Docker swarm prompt completes under 30 seconds.
- `VALIDATION.md` records exact measured evidence.

## Assumptions And Non-Goals

- No separate intent model.
- No v4.0 MCP/plugin marketplace work in this sub-plan.
- No manual wiki cleanup.
- No provider-specific model swapping during validation; use the configured DB model.
- No silent admin/plugin/skill mutation.
- No unbounded child recursion.
- No exposing all read tools to the parent in `swarm_research`.

## Self-Review

- Spec coverage: covers Hermes-style tool filtering, blocking delegation, isolated/capped children, finalization, metrics, stale alias hygiene, and live Docker validation.
- Placeholder scan: no TBD/TODO/later placeholders.
- Type consistency: `DelegationPolicy`, terminal swarm flags, and metric field names are used consistently across tasks.
