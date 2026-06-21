# Aura Industrial Audit

Audit date: 2026-06-21

Audited repository: `D:\Aura`

Reference context inspected: `D:\tmp` (`adk-go-study`, `agent-memory`, `agent-infra-sandbox`, `go-swarm`, and Aura smoke/log artifacts). These paths were used only as supporting context and were not treated as production source.

Subagent update: six read-only module explorers were spawned on 2026-06-21 for agent loop/runner, tools/security, persistence/memory, MCP/governance, AG-UI/Web/API, and infrastructure/operations. A seventh testing/CI explorer was attempted but the agent-thread limit was reached, so the testing/CI slice was covered locally.

## Scope

This audit reviewed Aura as an industrial agent-loop system: the LLM loop, tool/action execution, memory and context persistence, AG-UI serving path, MCP integration, shell/filesystem capabilities, configuration, secrets, observability, testing, deployment posture, and production-readiness gaps.

The audit was evidence-based and used source paths and line locations where possible. No production source code was modified. Only documentation files were created under `docs/audit`.

## How To Read This Audit

- Start with `executive-summary.md` for score, top risks, and immediate actions.
- Read `bug-report.md` for evidence-backed correctness, reliability, and security findings.
- Read `security-audit.md` for trust boundaries, prompt-injection surfaces, sandboxing, and secret-handling risks.
- Read `architecture-review.md` and `target-architecture.md` for current and proposed designs.
- Use `industrialization-roadmap.md` and `action-plan.md` as the engineering backlog.
- Use `risk-register.md` and `audit-index.json` for tracking and automation.
- Use `proposed-patches.md` for code-level remediation guidance. These patches are recommendations only and were not applied.
- Use `subagent-review.md` for the delegated module review summary and delta findings.

## Methodology Notes

Confirmed issues are labeled with evidence. Items that require live deployment validation, external threat modeling, or operational data are marked as `NEEDS CONFIRMATION` or `UNKNOWN`.

The repository contains meaningful production-oriented work already: bounded loop budgets, per-call LLM timeouts, bounded parallel tool execution, panic recovery in parallel tools, result sidecars with previewing, auth gates for AG-UI routes, MCP process timeouts, tool-result provenance tagging, and a nontrivial test suite. The gaps below are the remaining blockers for treating Aura as a serious industrial production system rather than a powerful local trusted-operator runtime.

## Production Readiness Snapshot

Production readiness score: **4.6 / 10**

Findings:

- P0: 0
- P1: 10
- P2: 28
- P3: 13
- Total: 51

## Most Important Findings

1. Shell and filesystem tools intentionally operate with full host privileges and no workspace capability boundary (`internal/agent/tools/shell_exec.go`, `internal/agent/tools/fs.go`).
2. Copying `.env.example` disables the default destructive shell approval gate because it sets `AURA_SHELL_DESTRUCTIVE_PATTERNS=` (`.env.example`, `internal/agent/tools/shell_exec_env.go`).
3. A terminal `text_response` can be emitted with runnable sibling tools, and the tools execute before the final response (`internal/agent/llm_agent_dispatch.go`, `internal/agent/llm_agent.go`).
4. Human-in-the-loop resume claim and answer append are not fully atomic across single and batch paths (`internal/runner/runner_resume.go`).
5. Conversation sidecar rehydration trusts a DB-stored path instead of reconstructing and fencing the path (`internal/conversations/store.go`, `internal/conversations/store_branch.go`).
6. Command hooks default to fail-open, which is unsafe for security gates (`internal/agent/hooks.go`, `internal/agent/hooks_command.go`).
7. Object-store and Garage credentials use static development defaults without production validation (`internal/config/config.go`, `compose.yaml`, `scripts/garage_bootstrap.sh`).
8. AG-UI listener failure can be logged while the daemon/container keeps running and the Compose healthcheck remains green (`cmd/aura/serve.go`, `compose.yaml`).
9. Managed MCP entries with both `url` and `command` can be classified as remote HTTP trust while opened as stdio command execution (`internal/mcp/manager/runtime.go`, `internal/mcp/managed_config.go`, `internal/agent/mcptools/mount.go`, `internal/mcp/transport.go`).
10. AG-UI conversation and approval APIs are not consistently scoped to authenticated identity (`internal/agui/conversations_api.go`, `internal/agui/approvals_api.go`, `internal/runner/runner_conversation.go`).
