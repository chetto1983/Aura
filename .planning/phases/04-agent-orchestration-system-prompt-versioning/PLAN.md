# Aura v3.1 Agent Orchestration And System Prompt Versioning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `subagent-driven-development` (recommended) or `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Aura's Telegram agent reliably choose the right route for each turn: direct memory tools, read-only swarm, Python sandbox, typed file tools, or review-gated admin tools.

**Architecture:** Keep the main LLM as the orchestrator, but give it a versioned prompt, a small per-turn tool profile, deterministic lifecycle hooks, and auditable telemetry. Do not add a separate intent model in this milestone; use conservative deterministic routing plus evals that verify tool choice and tool boundaries.

**Tech Stack:** Go, SQLite-backed settings, existing `internal/orchestration`, `internal/telegram`, `internal/tools`, `internal/toolsets`, React dashboard settings, Docker release runtime, OpenTelemetry-compatible log fields.

---

## Summary

v3.1 is the bridge milestone between the clean v1.3 memory runtime and the v4.0 MCP/plugin marketplace.

The core product problem is not "add more tools." Aura already has memory, wiki, source, skills, swarm, sandbox, file generation, MCP, tasks, and admin tools. The problem is that exposing too much at once makes the model slower, less predictable, and more likely to skip the right preparation step, such as reading a skill or launching swarm for broad synthesis.

The target behavior is Claude Code style orchestration:

- The main model drives the loop.
- The runtime narrows the active tools before the model sees them.
- Prompt modules are versioned and hashable.
- Skills are read before capability-specific work.
- Swarm is the preferred read-only route for broad audits and "what is missing?" prompts.
- Python sandbox is the preferred route for computation, parser experiments, data transforms, charts, simulations, generated artifacts, and repeatable debug scripts.
- Admin/plugin/skill mutation stays review-gated.
- Each turn leaves enough telemetry to debug why the model picked a route.

## 2026 Research Basis

The plan reflects current agent engineering practices from OpenAI Agents SDK tracing/guardrails/sessions, Google ADK lifecycle callbacks, Anthropic Claude Code subagent/tool-boundary guidance, MCP quality/security guidance, OpenTelemetry GenAI conventions, OWASP agentic security work, and NIST agent standards work.

Key takeaways applied here:

- Keep tool surfaces small and profile-based; use progressive discovery for large future MCP catalogs.
- Treat model, tool, handoff, guardrail, and sandbox events as traceable lifecycle events.
- Use deterministic pre/post hooks for security and policy, not just prompt wording.
- Do not let subagents inherit the parent's full tool surface.
- Curate memory and retrieve compact evidence; do not bulk-load everything into the prompt.
- Validate orchestration with evals that assert tool calls, exposed tools, latency, cost, and safety boundaries, not only final answer text.
- MCP and future plugin tools must use least privilege, scoped credentials, review gates, and suspicious-tool-description checks.

References used for this plan:

- OpenAI Agents SDK tracing: https://openai.github.io/openai-agents-js/guides/tracing/
- OpenAI Agents SDK guardrails: https://openai.github.io/openai-agents-js/guides/guardrails/
- OpenAI Agents SDK sessions: https://openai.github.io/openai-agents-js/guides/sessions
- Google ADK callbacks: https://google.github.io/adk-docs/callbacks/
- Anthropic Claude Code subagents: https://docs.anthropic.com/en/docs/claude-code/sub-agents
- MCP quality clients: https://modelcontextprotocol.io/docs/develop/clients/building-quality-clients
- MCP security best practices: https://modelcontextprotocol.io/docs/tutorials/security/security_best_practices
- OpenTelemetry GenAI conventions: https://opentelemetry.io/docs/specs/semconv/gen-ai/
- OWASP Agentic Security Initiative: https://genai.owasp.org/initiatives/agentic-security-initiative/
- NIST AI Agent Standards Initiative: https://www.nist.gov/node/1906621

## Current Baseline

Some v3.1 pieces already exist and must be audited before new work:

- `internal/orchestration/orchestration.go` composes `aura-agent-v1`, selects profiles, and returns profile allowlists.
- `internal/orchestration/orchestration_test.go` covers prompt metadata and profile boundaries.
- `internal/telegram/conversation.go` builds the prompt per turn, filters tool definitions, rejects hidden tool calls, and logs turn telemetry.
- `cmd/debug_telegram_sandbox/main.go` reports prompt version, hash, modules, active profile, exposed tools, called tools, token usage, estimated context tokens, and cost.
- Dashboard settings already exposes the `agent` group for `AURA_PROMPT_VERSION`, `AURA_TOOL_PROFILE_MODE`, and `AURA_ORCHESTRATION_LOG_LEVEL`.

This milestone should harden and complete the orchestration surface, not duplicate it.

## User Decisions

- Use system prompt versioning.
- Do not build a separate intent-classifier milestone.
- Swarm and Python sandbox are first-class routes, not rare fallback tools.
- Do not focus only on DOCX; source extraction, coding, sandbox, PDFs, XLSX, swarm, and future MCP/plugin skills all need skill preflight.
- Docker is the release runtime.
- v4.0 MCP/plugin marketplace waits until this tool-profile layer is clear.

## File Map

- Modify: `internal/orchestration/orchestration.go`
  - Own prompt modules, profile definitions, profile selection, profile allowlists, and profile guard metadata.
- Modify: `internal/orchestration/orchestration_test.go`
  - Lock prompt hash behavior, module selection, profile selection, allowlist boundaries, skill preflight rules, and security guard metadata.
- Modify or create: `internal/orchestration/hooks.go`
  - Add deterministic lifecycle hook interfaces for profile selection, prompt composition, tool exposure, before-tool policy, after-tool telemetry, and final turn reporting.
- Modify or create: `internal/orchestration/hooks_test.go`
  - Verify hooks redact secrets, reject hidden tool calls, record policy decisions, and preserve stable trace fields.
- Modify: `internal/telegram/conversation.go`
  - Wire lifecycle hooks into the live Telegram loop and archive enough orchestration evidence for dashboard/debug review.
- Modify: `internal/telegram/debug_smoke.go`
  - Expose orchestration trace fields in synthetic Telegram runs.
- Modify: `cmd/debug_telegram_sandbox/main.go`
  - Add strict `-expect-profile`, `-expect-tools`, `-expect-no-tools`, `-expect-skill-read`, `-expect-swarm`, and `-expect-sandbox` checks.
- Modify: `internal/settings/catalog.go`, `internal/settings/applier.go`, and tests
  - Ensure all orchestration keys are typed, redacted where needed, dashboard-overridable, and persisted through SQLite.
- Modify: `web/src/components/SettingsPanel.tsx`
  - Keep orchestration settings editable with enum controls where applicable.
- Modify: `web/src/i18n/locales/en.json`, `web/src/i18n/locales/it.json`
  - Keep labels and hints clear for prompt versioning, profile mode, and orchestration logging.
- Modify: `web/e2e/settings.spec.ts`
  - Verify orchestration settings are visible, editable, and preserved.
- Modify or create: `cmd/debug_orchestration/main.go`
  - Add a non-Telegram harness for profile selection, prompt composition, exposed tools, and hook telemetry without sending Telegram messages.
- Modify: `.planning/ROADMAP.md`
  - Keep v3.1 before v4.0 and update success criteria if this plan changes scope.
- Modify: `.planning/STATE.md`
  - Record v3.1 as active when implementation starts and capture final validation evidence when it closes.

## Public Interfaces And Settings

Settings:

- `AURA_PROMPT_VERSION=aura-agent-v1`
- `AURA_TOOL_PROFILE_MODE=auto|default|memory|swarm_research|sandbox_compute|document|admin_review`
- `AURA_ORCHESTRATION_LOG_LEVEL=summary|debug`

Debug commands:

- `go run ./cmd/debug_orchestration -prompt "facciamo il punto di tutta la pipeline"`
- `go run ./cmd/debug_telegram_sandbox -no-validate -expect-profile swarm_research -expect-tools run_aurabot_swarm,read_swarm_result -prompt "..."`
- `go run ./cmd/debug_telegram_sandbox -no-validate -expect-profile sandbox_compute -expect-sandbox -expect-tools execute_code -prompt "..."`

Telemetry fields:

- `prompt_version`
- `prompt_hash`
- `prompt_modules`
- `tool_profile`
- `tools_exposed`
- `tools_called`
- `hidden_tool_rejected`
- `skill_reads`
- `swarm_used`
- `sandbox_used`
- `tokens_prompt`
- `tokens_completion`
- `tokens_total`
- `estimated_context_tokens`
- `cost_usd`
- `latency_ms`
- `llm_calls`
- `tool_calls`
- `profile_select_reason`
- `policy_decisions`

## Tool Profiles

`default`

- Purpose: ordinary conversation and simple memory/task/dashboard work.
- Include: safe memory/source/wiki reads, basic wiki write only if already supported by existing review expectations, task tools, dashboard token, web search/fetch if configured.
- Exclude: sandbox, swarm mutation, skills admin, MCP admin, file generation unless the request clearly routes to another profile.

`memory`

- Purpose: source-backed or wiki-backed answers.
- Include: `search_memory`, `search_wiki`, `read_wiki`, `list_wiki`, `list_sources`, `read_source`, lint/read-only maintenance, review-gated proposal tools.
- Exclude: direct file generation, sandbox, skill/plugin mutation, task mutation unless explicitly needed.

`swarm_research`

- Purpose: broad synthesis, audits, planning, memory quality checks, "what is missing?", "facciamo il punto", pipeline reviews, repo-wide/document-wide analysis.
- Include: `run_aurabot_swarm`, `read_swarm_result`, `list_swarm_tasks`, read-only memory/source/wiki tools.
- Exclude: all mutation tools, typed file creation, sandbox, settings, skill/plugin admin.
- Rule: worker allowlists remain role-scoped and read-only; workers never inherit the parent tool profile.

`sandbox_compute`

- Purpose: calculations, data analysis, CSV/plots, parser experiments, source extraction experiments, repeatable debug scripts, generated computed artifacts.
- Include: `execute_code`, `list_tools`, `read_tool`, source reads, sandbox artifact persistence.
- Exclude: admin, settings, plugin/skill mutation, direct wiki mutation.
- Rule: deliverable files must stay under `/tmp/aura_out` and persist as source artifacts.

`document`

- Purpose: user-facing DOCX/XLSX/PDF/report generation.
- Include: `list_skills`, `read_skill`, memory/source/wiki reads, optional read-only swarm evidence, typed file tools `create_docx`, `create_xlsx`, `create_pdf`.
- Rule: use typed file tools for ordinary static documents; use sandbox only for computed artifacts.

`admin_review`

- Purpose: review queues, proposals, settings review, future plugin review.
- Include: proposal/review surfaces, skill read/catalog surfaces, task review as needed.
- Exclude: silent mutation and automatic enablement.

## Lifecycle Hooks

Add explicit orchestration hooks so safety and observability are not prompt-only:

- `BeforeProfileSelect`
  - Normalize the user text, availability, configured mode, and current settings.
  - Record why a profile was selected.
- `AfterProfileSelect`
  - Validate profile availability, fall back safely, and log fallback reason.
- `BeforePromptCompose`
  - Load overlays and skill manifest blocks.
  - Redact secrets from any logged prompt inputs.
- `AfterPromptCompose`
  - Record prompt version, modules, hash, and estimated prompt size.
- `BeforeExposeTools`
  - Convert profile allowlist into LLM tool definitions.
  - Record exact exposed tools.
- `BeforeToolCall`
  - Reject hidden tools with a fatal tool error.
  - Reject admin/plugin/skill mutation unless the profile and review gate allow it.
- `AfterToolCall`
  - Record tool name, duration, error class, and redacted result metadata.
- `AfterTurn`
  - Emit one compact summary log for dashboard/debug review.

## Implementation Tasks

### Task 1: Baseline Audit And Lock Existing Behavior

**Files:**

- Modify: `internal/orchestration/orchestration_test.go`
- Modify: `internal/telegram/debug_smoke_test.go`
- Modify: `cmd/debug_telegram_sandbox/main.go`

- [ ] Add tests that document the current baseline before changing behavior.
- [ ] Assert each profile exposes the exact expected tool set.
- [ ] Assert `swarm_research` excludes `write_wiki`, `create_docx`, `execute_code`, settings tools, and skill/plugin admin tools.
- [ ] Assert `sandbox_compute` includes `execute_code` and excludes wiki/admin mutation.
- [ ] Assert `document` includes skill read tools and typed file tools.
- [ ] Extend `debug_telegram_sandbox` validation flags for expected profile and forbidden tools.
- [ ] Run `go test ./internal/orchestration ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`.
- [ ] Commit: `test: lock orchestration baseline`.

### Task 2: Add Orchestration Hook Interfaces

**Files:**

- Create: `internal/orchestration/hooks.go`
- Create: `internal/orchestration/hooks_test.go`
- Modify: `internal/orchestration/orchestration.go`

- [ ] Add a `TraceEvent` type with stable fields for profile, prompt, tool, policy, token, cost, and latency data.
- [ ] Add hook interfaces for profile selection, prompt composition, tool exposure, tool call policy, tool result recording, and final turn summary.
- [ ] Provide a default no-op hook implementation so existing callers do not need custom hooks.
- [ ] Provide a structured logger hook that redacts API keys, bearer tokens, Telegram tokens, and source paths that contain secrets.
- [ ] Test redaction with strings containing `sk-`, `api-`, `TELEGRAM_TOKEN=`, `LLM_API_KEY=`, and bearer-token patterns.
- [ ] Run `go test ./internal/orchestration -count=1`.
- [ ] Commit: `feat: add orchestration lifecycle hooks`.

### Task 3: Harden Prompt Version Composition

**Files:**

- Modify: `internal/orchestration/orchestration.go`
- Modify: `internal/orchestration/orchestration_test.go`
- Modify: `internal/conversation/system_prompt_test.go`

- [ ] Keep `aura-agent-v1` as the default prompt version.
- [ ] Make module ordering deterministic: `base`, `runtime`, `security`, route modules, overlays, skills, proposals.
- [ ] Include prompt overlays in the hash but never log full overlay text at summary level.
- [ ] Add prompt module metadata for `hooks` and `tool_profiles` if hooks/profile guidance is present.
- [ ] Add tests proving the same input produces the same hash and changing overlay/profile/version changes the hash.
- [ ] Add tests proving prompt content gives direct guidance for swarm, sandbox, document, memory, and admin review profiles.
- [ ] Run `go test ./internal/orchestration ./internal/conversation -count=1`.
- [ ] Commit: `feat: harden versioned prompt composition`.

### Task 4: Replace Brittle Routing With Declarative Profile Cards

**Files:**

- Modify: `internal/orchestration/orchestration.go`
- Modify: `internal/orchestration/orchestration_test.go`

- [ ] Define profile cards in one table: profile name, purpose, positive cues, negative cues, required availability, allowed tools, denied tools, and selection reason.
- [ ] Keep deterministic cue matching conservative; do not try to parse every possible user intent with regex.
- [ ] Prefer `swarm_research` for broad read-only synthesis and audits when swarm is available.
- [ ] Prefer `sandbox_compute` for computation/artifact/debug-script prompts when sandbox is available.
- [ ] Prefer `document` for explicit file/report generation.
- [ ] Prefer `memory` for source-backed or wiki-backed questions.
- [ ] Fall back to `default` when confidence is unclear.
- [ ] Return `profile_select_reason` for telemetry and debug output.
- [ ] Add Italian and English routing test prompts for common Aura usage.
- [ ] Run `go test ./internal/orchestration -count=1`.
- [ ] Commit: `feat: make tool profile routing declarative`.

### Task 5: Enforce Filtered Tools In Telegram

**Files:**

- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/bot_test.go`
- Modify: `internal/telegram/debug_smoke.go`
- Modify: `internal/telegram/debug_smoke_test.go`

- [ ] Wire hooks into the Telegram turn lifecycle.
- [ ] Ensure `DefinitionsFor(toolAllowlist)` is the only source of exposed tool definitions.
- [ ] Keep unexpected hidden tool calls fatal and visible to the model.
- [ ] Record `hidden_tool_rejected=true` when a hidden tool call is attempted.
- [ ] Archive tool profile, exposed tools, called tools, prompt version, prompt hash, and route reason with the turn where possible.
- [ ] Add tests with a fake model attempting a hidden tool call.
- [ ] Run `go test ./internal/telegram -count=1`.
- [ ] Commit: `feat: enforce telegram tool profile boundary`.

### Task 6: Strengthen Skill Preflight

**Files:**

- Modify: `internal/orchestration/orchestration.go`
- Modify: `internal/orchestration/orchestration_test.go`
- Modify: `internal/skills/*_test.go` if skill prompt block tests need updates.

- [ ] Represent skill preflight as profile policy, not only prose.
- [ ] Require skill preflight guidance for document, sandbox/source extraction, PDF, XLSX, DOCX, coding/debug, and future MCP/plugin capabilities.
- [ ] Add tests that `document` prompts require `list_skills` and `read_skill` before typed file tools.
- [ ] Add tests that `sandbox_compute` prompts require skill inspection before parser/source-extraction/coding workflows when skills are available.
- [ ] Add debug output for `skill_reads`.
- [ ] Run `go test ./internal/orchestration ./internal/skills ./cmd/debug_telegram_sandbox -count=1`.
- [ ] Commit: `feat: require skill preflight by profile`.

### Task 7: Make Swarm Fast And Predictable

**Files:**

- Modify: `internal/conversation/swarm_prompt.go`
- Modify: `internal/swarmtools/tools.go`
- Modify: `cmd/debug_swarm/main.go`
- Modify: relevant swarm tests under `internal/swarmtools` and `cmd/debug_swarm`.

- [ ] Add a fast swarm mode for read-only synthesis with bounded worker count, bounded context, and short per-worker timeout.
- [ ] Ensure worker allowlists are role-based and do not inherit the parent profile.
- [ ] Add stale alias hygiene: reject old worker slugs such as legacy `.yaml` aliases unless they are explicitly registered in the current catalog.
- [ ] Add model-speed knobs where supported by the configured provider, such as lower max tokens, lower reasoning effort, or provider-specific "thinking off" options when safe.
- [ ] Record swarm run latency, worker count, model, token usage, and failed worker count.
- [ ] Add tests that broad prompts choose swarm and that swarm tools remain read-only.
- [ ] Run `go test ./internal/swarmtools ./internal/conversation ./cmd/debug_swarm -count=1`.
- [ ] Commit: `feat: bound and trace swarm research route`.

### Task 8: Make Sandbox A First-Class Compute Route

**Files:**

- Modify: `internal/orchestration/orchestration.go`
- Modify: `internal/tools/exec.go`
- Modify: `cmd/debug_sandbox/main.go`
- Modify: `cmd/debug_telegram_sandbox/main.go`
- Modify: sandbox and tool tests under `internal/sandbox` and `internal/tools`.

- [ ] Ensure `sandbox_compute` exposes `execute_code` only when the runtime is healthy.
- [ ] Keep all generated deliverables under `/tmp/aura_out`.
- [ ] Persist sandbox artifacts as source artifacts with metadata linking them to the user turn.
- [ ] Add route guidance for parser experiments, transformations, charts, simulations, and repeatable debug scripts.
- [ ] Add debug smoke prompts for CSV plus chart artifact generation.
- [ ] Run `go test ./internal/sandbox ./internal/tools ./cmd/debug_sandbox ./cmd/debug_telegram_sandbox -count=1`.
- [ ] Commit: `feat: promote sandbox compute route`.

### Task 9: Add OpenTelemetry-Compatible Orchestration Telemetry

**Files:**

- Modify: `internal/telegram/conversation.go`
- Modify: `internal/telegram/debug_smoke.go`
- Modify: `cmd/debug_telegram_sandbox/main.go`
- Modify: `internal/llm/*` if token/cost reporting needs normalization.

- [ ] Emit one summary event per turn with OpenTelemetry-compatible GenAI naming where practical.
- [ ] Include prompt version/hash/modules, profile, route reason, exposed tools, called tools, skill reads, swarm/sandbox use, token usage, estimated context tokens, cost, LLM calls, tool calls, and elapsed time.
- [ ] Include debug-level per-tool duration and error class.
- [ ] Redact secrets from telemetry.
- [ ] Add tests proving token/cost metrics survive through debug smoke.
- [ ] Run `go test ./internal/telegram ./internal/llm ./cmd/debug_telegram_sandbox -count=1`.
- [ ] Commit: `feat: trace orchestration turns`.

### Task 10: Dashboard And Settings Polish

**Files:**

- Modify: `internal/settings/catalog.go`
- Modify: `internal/settings/applier.go`
- Modify: `internal/settings/*_test.go`
- Modify: `web/src/components/SettingsPanel.tsx`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/it.json`
- Modify: `web/e2e/settings.spec.ts`

- [ ] Ensure `AURA_TOOL_PROFILE_MODE` is an enum combo box with all valid values.
- [ ] Ensure `AURA_ORCHESTRATION_LOG_LEVEL` is an enum combo box with `summary` and `debug`.
- [ ] Ensure `AURA_PROMPT_VERSION` is editable but defaults to `aura-agent-v1`.
- [ ] Ensure settings saved in SQLite override environment values in live debug harnesses.
- [ ] Add Playwright checks for edit/save/reload of orchestration settings.
- [ ] Run `npm --prefix web run i18n:check`.
- [ ] Run `npm --prefix web run build`.
- [ ] Run settings E2E against the Docker dashboard.
- [ ] Commit: `feat: polish orchestration settings`.

### Task 11: Add Orchestration Evals

**Files:**

- Create or modify: `cmd/debug_orchestration/main.go`
- Create or modify: `cmd/debug_orchestration/main_test.go`
- Modify: `cmd/debug_telegram_sandbox/main.go`
- Modify: `internal/orchestration/orchestration_test.go`

- [ ] Add deterministic evals for profile selection without a live model.
- [ ] Add live Telegram-style smoke prompts for:
  - broad pipeline review, expecting `swarm_research` and `run_aurabot_swarm`;
  - computed CSV/chart, expecting `sandbox_compute` and `execute_code`;
  - repo summary document, expecting `document`, skill read, optional swarm evidence, and typed file creation;
  - ordinary memory answer, expecting `memory` or `default` without file/admin/sandbox tools.
- [ ] Add safety prompts that attempt hidden/admin/plugin tools and expect rejection.
- [ ] Record latency, token, and cost metrics for every live eval.
- [ ] Keep live evals small enough for the Docker release gate.
- [ ] Run `go test ./cmd/debug_orchestration ./cmd/debug_telegram_sandbox ./internal/orchestration -count=1`.
- [ ] Commit: `test: add orchestration route evals`.

### Task 12: Docker Release Gate And Planning Closeout

**Files:**

- Modify: `.planning/phases/04-agent-orchestration-system-prompt-versioning/VALIDATION.md`
- Modify: `.planning/ROADMAP.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/implementation-tracker.md`

- [ ] Run `go test ./internal/conversation ./internal/telegram ./internal/tools ./internal/toolsets ./internal/settings ./internal/api ./internal/orchestration -count=1`.
- [ ] Run `go test ./cmd/debug_telegram_sandbox ./cmd/debug_files ./cmd/debug_orchestration -count=1`.
- [ ] Run `go test ./... -count=1`.
- [ ] Run `npm --prefix web run i18n:check`.
- [ ] Run `npm --prefix web run build`.
- [ ] Run `docker compose config --quiet`.
- [ ] Run `docker compose up -d --build aura`.
- [ ] Confirm `/status` is healthy on the configured dashboard port.
- [ ] Run live debug prompts through `cmd/debug_telegram_sandbox` using the configured DB model, not a random provider swap.
- [ ] Confirm release metrics: no hidden tools exposed, expected tools called, token/cost metrics present, and no slow common prompts over the accepted threshold.
- [ ] Write `VALIDATION.md` with exact commands, model, profile results, tools called, token/cost metrics, and failures if any.
- [ ] Mark v3.1 closed only if all strict gates pass.
- [ ] Commit: `docs: close v3.1 orchestration validation`.

## Test Plan

Unit:

- `go test ./internal/orchestration -count=1`
- `go test ./internal/telegram -count=1`
- `go test ./internal/settings -count=1`
- `go test ./internal/tools ./internal/toolsets ./internal/swarmtools ./internal/sandbox -count=1`

Integration:

- `go test ./cmd/debug_orchestration ./cmd/debug_telegram_sandbox ./cmd/debug_files -count=1`
- `go test ./internal/conversation ./internal/api -count=1`

Frontend:

- `npm --prefix web run i18n:check`
- `npm --prefix web run build`
- settings Playwright E2E against Docker dashboard

Docker:

- `docker compose config --quiet`
- `docker compose up -d --build aura`
- `Invoke-RestMethod http://127.0.0.1:18080/status`

Live smoke:

- Pipeline review prompt must choose `swarm_research`, expose read-only swarm tools, and call `run_aurabot_swarm`.
- Computed CSV/chart prompt must choose `sandbox_compute`, call `execute_code`, persist artifacts, and report token/cost metrics.
- Repo summary document prompt must choose `document`, read a matching skill, gather evidence, and use a typed file tool unless the output is computed.
- Ordinary memory prompt must not expose sandbox, file generation, or admin tools.
- Hidden tool attack prompt must return a fatal hidden-tool error and log `hidden_tool_rejected=true`.

## Release Gate

v3.1 closes only when:

- Prompt composition is versioned, deterministic, and hashable.
- Telegram exposes only the active profile's tools.
- Hidden tool calls are rejected.
- Skill preflight is visible in prompts and telemetry.
- Swarm and sandbox routes are chosen by common live prompts.
- Debug commands report prompt version, profile, exposed tools, called tools, tokens, estimated context, cost, and latency.
- Dashboard settings exposes and preserves orchestration keys.
- Docker runtime passes the full test gate.
- `.planning` contains validation evidence with exact commands and measured results.

## Assumptions And Non-Goals

- No separate intent model in v3.1.
- No MCP marketplace implementation in v3.1.
- No silent skill/plugin/admin mutation.
- No manual wiki cleanup as part of this phase.
- Existing tool names stay stable.
- Profile routing starts conservative; ambiguous prompts fall back to `default`.
- Prompt overlays remain operator-editable and included in the prompt hash.
- Future MCP tools will be exposed through the same profile and review-gated policy layer.
