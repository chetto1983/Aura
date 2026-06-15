# Phase 22: Agent Perimeter Hardening - Research

**Researched:** 2026-06-15
**Status:** Complete
**Scope:** Planning research for Phase 22, based on `22-SPEC.md`, `22-CONTEXT.md`, and `docs/audit/`.

## Research Question

What does the executor need to know to plan Phase 22 well?

Short answer: this phase should be planned as a risk-first remediation train over the `internal/agent` operational perimeter. The core loop is already well-tested; the missing work is failure-mode hardening around goroutine panic isolation, cross-goroutine state safety, MCP resilience, secret hygiene, observability, hooks, trust provenance, budget/wallclock bounds, tool memory safety, skill self-extension honesty, and a finding-coverage ledger.

## Authoritative Inputs

- `.planning/phases/22-bug-fix/22-SPEC.md` locks 12 HARDEN requirements and the risk-first wave hint.
- `.planning/phases/22-bug-fix/22-CONTEXT.md` refines the spec with implementation decisions D-00a through D-13.
- `docs/audit/bug-report.md` is the authoritative AG-001..AG-064 finding list.
- `docs/audit/proposed-patches.md` gives PP-1..PP-10 implementation guidance for the major clusters.
- `docs/audit/testing-strategy.md` names the missing failure-mode tests.
- `docs/audit/action-plan.md` maps the work into A1..A25 engineering tasks.
- `docs/audit/audit-index.json` and `docs/audit/risk-register.md` confirm risk ordering and current OPEN / TRACKED / NEEDS CONFIRMATION state.

## Codebase Facts

The audit anchors still exist in the current tree:

- Panic firewall seams:
  - `internal/agent/llm_agent_parallel.go` contains `executeBatch`.
  - `internal/agent/workflow/parallel.go` contains parallel child execution.
  - `internal/swarm/swarm.go` contains swarm wave execution.
  - `internal/agent/tools/shell_bg.go` contains the background-shell reaper path.
- Dedup:
  - `internal/agent/budget_dedup.go` contains `dedupRing`, `BeforeToolCall`, and `AfterToolResult`.
- MCP:
  - `internal/agent/mcptools/bridge_reconnect.go` contains `reconnectAfterTransport`.
  - `internal/agent/mcptools/bridge.go` parses per-call timeout and wraps MCP execution.
  - `internal/agent/mcptools/timeout.go` contains `configuredMCPCallTimeout`.
  - `internal/agent/mcptools/name.go` and `mount.go` are the namespace/dead-code seams.
- Secrets and hooks:
  - `internal/secret/envkey.go` contains `IsSecretEnvKey`.
  - `internal/agent/tools/shell_exec_env.go` contains shell output redaction.
  - `internal/agent/hooks_command.go` contains command-hook exec/env/rewrite behavior.
  - `internal/agent/hooks.go` contains hook-manager fan-out and in-process hook calls.
- Observability:
  - `internal/agent/metrics.go` is the existing metric substrate.
  - `internal/agent/tracing.go` contains `mintSpanID` and OTLP setup.
  - `internal/agent/llm_agent.go`, dispatch/finalize/completion files, and hook paths are the emission points.
- Reasoning router:
  - `internal/agent/llm_agent_reasoning.go` contains adaptive reasoning tier selection.
  - `internal/agent/prompt/reasoning_classifier.go` has classifier anchor-loading lock scope.
  - `internal/agent/prompt/reasoning_router.go` and tests are the router seam.
- Trust/provenance:
  - `internal/agent/trust.go` contains `untrustedSource`.
  - `internal/swarm/runner_adapter.go` projects child reports.
- Loop/budget/workflow:
  - `internal/agent/budget.go` contains `NewBudget` and `WithDeadline`.
  - `cmd/aura/agent.go` is the composition-root place to wire active wallclock context.
  - `internal/agent/workflow/workflow.go` contains `findInTree`.
  - `internal/agent/workflow/loop.go`, `parallel.go`, and `sequential.go` hold loop/parallel behavior.
- Tools:
  - `internal/agent/tools/fs_read.go`, `fs_write.go`, and `fs_edit.go` need size caps/atomicity.
  - `internal/agent/tools/shell_bg.go` needs session eviction and buffer pruning.
  - `internal/agent/tools/task.go`, `send_file.go`, `search.go`, `fs_grep.go`, `fs_glob.go`, `read_tool_output.go`, and `result.go` cover the remaining P2/P3 tool findings.
- Skill reconcile:
  - `internal/agent/tools/skill.go` still has `skillParamsSchema` and `skillParamsSchemaHonest`.
  - `internal/agent/tools/skill_write.go` contains the activation/pause-sentinel seam.

## Existing Test Substrate

The project already has useful local test harnesses:

- `internal/agent/main_test.go`, `internal/agent/tools/main_test.go`, `internal/agent/workflow/workflow_test.go`, `internal/agent/mcptools/main_test.go`, and `internal/swarm/main_test.go` install package-level `goleak`.
- `internal/agent/llm_agent_parallel_test.go`, `workflow/parallel_test.go`, and `internal/swarm/*_test.go` are the natural panic-firewall targets.
- `internal/agent/budget_dedup_test.go` and `budget_test.go` are the natural dedup/budget targets.
- `internal/agent/mcptools/bridge_*_test.go` already use `stubOpenMCPClient` and fake clients; extend that instead of adding live dependencies.
- `internal/agent/hooks_command_test.go`, `hooks_command.go`, and `llm_agent_hooks_test.go` cover hook behavior.
- `internal/agent/tools/shell_exec_mergeenvcap_test.go`, `shell_exec_env.go`, and `internal/secret/envkey_test.go` are the secret-boundary targets.
- `internal/reasoningtrace/*_test.go` covers trace rows and redaction behavior.
- `internal/agent/tracing_test.go` and `tracing_spans_test.go` are the span/entropy targets.
- `internal/agent/prompt/reasoning_*_test.go` and `llm_agent_reasoning_test.go` are the router fallback targets.
- `internal/agent/tools/fs_*_test.go`, `shell_bg_test.go`, `send_file_test.go`, and `search_mount_test.go` cover tool hardening.
- `scripts/coverage_gate.sh`, `cache_invariant_audit.sh`, and project CI commands form the phase-close bar.

## Planning Implications

### Wave 1 - Crash Class

Do AG-001 and AG-002 first. A panic firewall does not save concurrent-map-write fatals, so the plan should land both before other work. Tests must be `-race` and `goleak` guarded:

- Panicking fake tool through `executeBatch`.
- Panicking workflow child through `parallel.Run`.
- Panicking swarm child through `swarm.runWave`.
- Panicking `shell_bg` reaper path.
- Concurrent `BeforeToolCall` / `AfterToolResult` hammer.
- Multi-tool dispatch with more than one parallel call.

### Wave 2 - Secrets and Observability

Do secret boundary and telemetry before larger reliability work so later failures are visible and credential-safe:

- Expand secret key/value classification for DSN/URL/URI/CONN/PWD/COOKIE/SESSION/JWT shapes without over-redacting harmless URLs.
- Strip hook child env to a minimal allowlist.
- Stop writing full `history` to reasoning trace by default; cap fields.
- Add turn outcome, LLM latency, LLM error, tool error, token, hook, panic, and span-export-failure metrics in `internal/agent/metrics.go`.
- Add `slog` at turn/LLM/tool/hook boundaries.
- Make `mintSpanID` fail soft.

### Wave 3 - MCP, Router, and Active Bounds

MCP resilience and active wallclock should land together because unbounded MCP calls are one of the main reasons the wallclock must be a real context deadline:

- Reconnect outside `s.mu`, single-flight, with `context.WithoutCancel` and a dedicated reconnect timeout.
- Add backoff and a breaker: context decisions say 3 failures, 30s cooldown, 500ms to 30s exponential backoff, 10s reconnect timeout.
- Change `AURA_MCP_CALL_TIMEOUT_SEC=0` to default timeout and `-1` to explicit infinite.
- Resolve timeout once at mount/boot rather than per call.
- Warn on reconnect `Mutating` flips and required-arg changes; mark after-send failures non-retryable.
- Bound the reasoning-router path while preserving D-07: router stays on by default, first embed outage may attempt once, then breaker opens or static `ReasoningTierLow` keeps turns fast.
- Validate `NewBudget` options and thread `Budget.WithDeadline` from `cmd/aura/agent.go`.

### Wave 4 - Hooks, Provenance, Tools, Workflow Hardening

After the crash/secret/MCP/observability base is in place, land broad P2 hardening:

- Hook `fail_open` / `fail_closed`, absolute hook paths, `rewrite` requires exit 0, in-process hook recover.
- Default unknown tool output to untrusted and propagate untrusted provenance through swarm child reports.
- `findInTree` visited/depth guard.
- Dedup `results` pruning and period-3+ cycle detection.
- `BackgroundShells` session eviction and poll/kill pruning.
- `AURA_FS_MAX_READ_BYTES` cap for read/write/edit with stat-then-reject and paging hint.
- Atomic file writes/edits where appropriate.
- `send_file` fail-closed with empty root in non-CLI contexts.
- MCP schema size/property caps, namespace collision validation, stale embedding invalidation on description hash changes.
- Cache-prefix drift metric after `BeforeModel` hook rewrites.
- Classifier cold-start lock narrowing.

### Wave 5 - Skill Reconcile, Ledger, CI, Live Sign-Off

Close with hygiene, ledger, and verification:

- Delete duplicate/dead `skillParamsSchema`, reconcile documented skill activation behavior, restore alert or document trust boundary honestly.
- Confirm NEEDS-CONFIRMATION findings: AG-028, AG-034, AG-041, AG-043.
- Produce `docs/audit/22-finding-ledger.md` mapping every AG-### to fixed+test, accepted+rationale, or confirmed+routed.
- Produce `docs/audit/22-LIVE-SIGNOFF-<date>.md` with full live stack evidence.
- Run coverage floor, full CI, `cache_invariant_audit.sh`, and mutation spot-checks on `llm_agent_parallel.go`, `budget_dedup.go`, and `mcptools/bridge_reconnect.go`.

## Sharp Edges

- Do not implement SPEC R6 as "router default off"; D-07 says router stays on by default and is bounded.
- Do not chase full multi-tenant security in this phase. Only land the single-operator slices called out in `22-CONTEXT.md`.
- Do not rely on recovered panics for AG-002; concurrent map writes are fatal.
- Do not hold mutexes across process spawn, MCP handshake, network calls, hook execution, or delivery/LLM calls.
- Do not add live external dependencies to unit tests; use existing fake/stub seams.
- Do not greenwash tests with skips. CI-missing live env should remain explicit, and deterministic fakes should cover the hardening claims.
- Keep each touched file under 600 LOC; split/refactor on touch if needed.
- Keep atomic commits one finding or one tight cluster at a time, with AG-### in messages.

## Verification Architecture

Per wave:

- Focused tests for the touched package.
- `go test -race` for concurrency packages.
- `goleak`-guarded tests for goroutine paths.
- `go vet` and `golangci-lint` for touched subtree.

Phase close:

- `go test ./...`
- `go test -race ./internal/agent/... ./internal/swarm/...`
- `go vet ./...`
- `golangci-lint run ./...`
- `govulncheck ./...`
- `bash scripts/cache_invariant_audit.sh`
- `bash scripts/coverage_gate.sh` or `make coverage` with >=85% owned-surface.
- Mutation spot-check >=70% killed on `internal/agent/llm_agent_parallel.go`, `internal/agent/budget_dedup.go`, and `internal/agent/mcptools/bridge_reconnect.go`.
- Live stack sign-off: `aura serve` with PG + Neo4j + MCP + embed sidecar + SearXNG bridge; host `aura chat` tool trace; `/metrics` scrape; CDP Telegram round-trip; GLM OCR/full tool surface exercised.

## Recommended Plan Split

Create five executable plan files:

1. `22-01-PLAN.md` - crash firewall and dedup race, wave 1.
2. `22-02-PLAN.md` - secret boundary and observability, wave 2.
3. `22-03-PLAN.md` - MCP resilience, reasoning-router bounds, active budget/wallclock, wave 3.
4. `22-04-PLAN.md` - hooks, provenance, tools, workflow hardening, wave 4.
5. `22-05-PLAN.md` - skill reconcile, ledger, confirmation/routing, CI/mutation/live sign-off, wave 5.

This split matches SPEC's risk-first hint and keeps each executor context coherent while still covering every HARDEN requirement.

## RESEARCH COMPLETE
