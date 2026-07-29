# Aura Agent Memory MCP and Context Engine Audit

This directory contains the industrial, read-only audit of Aura's Agent Memory
MCP and context engine plus the separately executed P0 remediation record. The
audited implementation at the original cutoff was **not ready for
production-scale multi-user autonomous workloads**.

- Original audit recommendation: **NO-GO**
- Original overall readiness score: **3.4 / 10**
- Post-remediation P0 status: **0 open, 2 remediated and live-verified**
- Scoped P0 remediation score: **10.0 / 10**
- Findings: **30** total — 2 remediated P0, 16 P1, 11 P2, 1 INFO
- Assessment cutoff: 2026-07-29
- Revision: `5c151cf9541ff9946057361f97e757677d82cac8`
- Branch: `master`
- Audited state: the current, pre-existing dirty working tree, not only `HEAD`

At the original cutoff, the two P0 findings were directly reachable cross-user
disclosure paths in the vendored memory service: unauthenticated global MCP
resources/tools and cross-principal short-term session claiming. Both are now
closed by authenticated server-derived tenant scope and composite
tenant/session ownership. The remediation was exercised through adversarial
live tests and a real OpenRouter-backed `LlmAgent`; see the
[P0 remediation evidence](12-p0-remediation-evidence.md).

The overall recommendation remains **NO-GO** because closing only the P0s does
not resolve the 16 P1 findings, including exact-request context correctness,
typed mutation success, and verified deprovision erasure.

## Report index

1. [Executive summary](00-executive-summary.md)
2. [Scope, methodology, and evidence](01-scope-methodology-and-evidence.md)
3. [Current-state architecture](02-current-state-architecture.md)
4. [Agent Memory and MCP audit](03-agent-memory-mcp-audit.md)
5. [Context engine audit](04-context-engine-audit.md)
6. [Concurrency, reliability, and performance](05-concurrency-reliability-performance.md)
7. [Security and privacy](06-security-and-privacy.md)
8. [Testing, observability, and operability](07-testing-observability-operability.md)
9. [Findings register](08-findings-register.md)
10. [Target architecture](09-target-architecture.md)
11. [Prioritized roadmap](10-prioritized-roadmap.md)
12. [Q&A, open questions, and required evidence](11-open-questions-and-required-evidence.md)
13. [P0 remediation evidence](12-p0-remediation-evidence.md)
14. [Machine-readable audit index](audit-index.json)

## Reading guidance

The [findings register](08-findings-register.md) is authoritative for severity,
status, evidence, remediation, and acceptance criteria. Domain reports explain
the traced behavior and cross-component consequences without duplicating every
finding field.

All repository evidence is expressed as a repository-relative path and a
symbol or narrow line location. Line numbers describe the audited working tree
at the cutoff and can move as the concurrent user-owned changes evolve.

## Integrity note

The repository was already heavily modified before the audit. The initial
baseline contained 246 status entries. While the audit was in progress,
user-owned files continued changing independently; a later pre-output
checkpoint contained 248 entries and the final filtered check contained 253.
Audit agents performed read-only inspection, and the only audit writes are the
documents in this directory. This concurrent activity prevents claiming an
atomic working-tree snapshot; it does not change the directly traced call-path
conclusions identified in the reports.
