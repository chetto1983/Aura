---
phase: 38
slug: mcp-governance-hardening
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-18
---

# Phase 38 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `38-RESEARCH.md` §Validation Architecture. The Per-Task map below is populated by the planner/executor.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | none — standard `go test` + build tags (`db_integration`) |
| **Quick run command** | `go test ./internal/mcp/... ./cmd/aura/...` |
| **Full suite command** | `go test -race -tags db_integration ./internal/mcp/... ./internal/config/... ./cmd/aura/...` |
| **Estimated runtime** | ~60 seconds (unit); +DB for `db_integration` |

---

## Sampling Rate

- **After every task commit:** Run `go vet ./... && go build ./... && go test ./internal/<pkg>/`
- **After every plan wave:** Run `go test -race ./internal/mcp/... ./cmd/aura/...`
- **Before `/gsd-verify-work`:** Full suite must be green (incl. `db_integration` for the CLI audit write)
- **Max feedback latency:** 60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 38-01-01 | 01 | 1 | MCPH-01 | T-38-01 / — | Mixed url+command with no explicit type → Classify returns error; stdio Open never called | unit | `go test ./internal/mcp/ -run TestClassify` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*
*Planner populates the remaining rows from PLAN.md tasks — one row per task, mapping to MCPH-01..09.*

---

## Wave 0 Requirements

- [ ] `internal/mcp/classify_test.go` — table-driven cases for MCPH-01/02 (mixed transport, empty remote trust)
- [ ] Open-spy / test seam proving stdio `Open` is never reached on a blocked/mixed entry (SC1)
- [ ] `go test` framework already present — no install needed

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Shutdown leaves no child processes (process-tree kill) | MCPH-06 | Full process-tree reap is OS-observable; daemon-free unit tests cover the pure `SysProcAttr` wiring + kill-signal selection, but end-to-end "no orphaned grandchild" is verified manually / under a tagged integration run | Start a stdio MCP server that forks a child, shut the registry down, assert no orphaned PID in the process group |

*Mutation spot-check ≥70% on the classifier (`classify.go`) and bounded stdio reader per CLAUDE.md Gate 3.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
