# Agent Architecture Audit

Audit date: 2026-06-16

Audited path: `D:\Aura\internal\agent`

Supporting reference paths sampled: `D:\tmp\adk-go-study`, `D:\tmp\codex`

Known deployment context added after the initial audit: Aura runs in a container. The execution findings should therefore be read as full access to the Aura runtime container and its mounted resources, not necessarily direct access to the physical host. The remaining production risk depends on container hardening, mounted volumes, injected secrets, network policy, user namespace, and whether privileged mounts such as the Docker socket are present.

This directory contains a production-readiness audit of Aura's agent runtime. The audit focuses on the agent loop, tool execution, memory and context management, reliability, observability, security, testing, maintainability, and the path from a trusted local assistant to an industrial-grade production system.

## How To Read This Audit

- Start with `executive-summary.md` for the production-readiness score and highest-risk decisions.
- Read `bug-report.md` for evidence-backed findings with file paths, locations, risks, fixes, and suggested tests.
- Read `security-audit.md` before exposing the agent to remote users, shared infrastructure, or sensitive workspaces.
- Use `industrialization-roadmap.md` and `action-plan.md` as the engineering backlog.
- Use `target-architecture.md` as the proposed future design.
- Use `audit-index.json` for machine-readable tracking.
- Use `proposed-patches.md` for code-level patch recommendations. No production source code was modified.

## High-Level Assessment

Aura has a stronger-than-average local agent core: it has a bounded agent loop, retry classification, circuit-breaker behavior for LLM stream opens, untrusted tool-output wrapping, SSRF controls for web fetches, context compaction, tool-call deduplication, event persistence, OpenTelemetry hooks, and a useful amount of regression coverage.

The current architecture is still not production-ready for an industrial, remote, multi-user, or sensitive-runtime deployment. The biggest reason is trust boundary design: native shell and filesystem tools intentionally run with full access inside the Aura container and any mounted resources, and model-authored non-`always` skill create/update operations are intentionally auto-activated. Those may be acceptable in a single trusted local-operator product running in a hardened disposable container, but they are production blockers for shared infrastructure unless enforced capability profiles are added.

## Most Important Findings

- P1: Shell and native filesystem tools have full container/runtime access without a workspace jail or enforceable capability profile.
- P1: Model-authored skill create/update can auto-activate for non-`always` skills.
- P1: `FSWrite` uses direct `os.WriteFile` instead of the existing atomic-write helper.
- P1: Background shell jobs are process-scoped, detached from the request context, and have no TTL or ownership check.
- P1: The agent loop relies on callers to derive deadline contexts from budgets, while per-node timeout defaults to disabled.
- P1: A model can emit `text_response` and other tool calls in one batch; Aura executes the non-terminal tools before finalizing.

## Current Verification

Scoped tests were run successfully:

```text
go test ./internal/agent/...
```

Result: all packages under `internal/agent/...` passed in the current checkout.
