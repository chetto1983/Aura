---
phase: 9
slug: swarm-minimal
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-04
---

# Phase 9 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + pgregory.net/rapid (property) + goleak + race detector |
| **Config file** | none — existing tag-matrix conventions (`db_integration`, `neo4j_integration`, `cot_eval`, …) |
| **Quick run command** | `go vet ./... && go build ./... && go test ./internal/swarm/ ./internal/agent/tools/ ./internal/agent/mcptools/` |
| **Full suite command** | `go test -race ./...` (WSL primary; Windows needs `BASH_ENV=~/.aura-toolchain.sh`) |
| **Estimated runtime** | ~60-120 seconds (unit+race, no containers) |

---

## Sampling Rate

- **After every task commit:** Run the quick run command (vet + build + touched-package tests)
- **After every plan wave:** Run `go test -race ./...`
- **Before `/gsd-verify-work`:** Full suite green + live `cot_eval` tier executed by operator (not CI)
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

*To be filled by the planner from RESEARCH.md §Validation Architecture — every PLAN task maps a re-specced ROADMAP success criterion or D-25 property to a tier below.*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (planner fills) | | | CAP-03 | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

Tier mapping locked by RESEARCH.md §Validation Architecture:
- **SC#1 (3-child wall-clock < 1.5×, race+goleak clean):** unit/fixture tier with `agenttest.FakeClient`, `go test -race` + goleak — CI.
- **SC#2 (re-specced D-10: worker lacks `swarm_spawn`; depth code-guard error):** unit — registry-exclusion assertion + synthetic depth ≥ cap → PRD error literal — CI.
- **SC#3 (re-specced D-04: 5 children pause → 5 `needs_user_input` report entries; goroutine-leak clean):** unit + goleak — CI.
- **SC#4 (depth-2 tree total steps ≤ parent remaining):** unit reusing `Budget.Child()` proven harness — CI.
- **SC#5 (D-22 live E2E mail+WhatsApp, dual gate ground-truth + judge ≥90%):** `cot_eval` build tag, OPENROUTER-gated, operator-run — Manual-Only table below.
- **D-25 properties (report length/order, tree budget, goleak, per-child isolation):** rapid property tier — CI.

---

## Wave 0 Requirements

- [ ] `internal/swarm/` package scaffold — currently EMPTY (greenfield, verified)
- [ ] PRD amendment commit (D-23 doc-only plan 09-01) BEFORE any code

*Existing infrastructure (rapid, goleak, race, agenttest.FakeClient, cot_eval harness) covers all phase tiers — no framework install needed.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| D-22 live swarm E2E (natural prompt, no "swarm" mention) with mail+WhatsApp MCP mounts, dual gate (ground-truth read-back + judge ≥90%) | CAP-03 / SC#5 | Needs OPENROUTER_API_KEY + live mail/WhatsApp accounts + WSL whatsmeow bridge (REST :8080) — operator-run by design, NOT CI (no-skip-as-green: tier is operator-tier, CI gates stay on unit/property/race/goleak) | Bring up bridge (fork chetto1983/whatsapp-mcp @ 6de1dcd), health-check REST :8080, source .env, run `go test -tags cot_eval -run TestSwarm ./internal/eval/`; record numbers in docs/aura-quality-snapshot.md |
| Fail-soft boot check: dead MCP server must not kill `aura chat` | D-21 | Requires deliberately broken server entry in managed config | Add bogus server via `aura mcp add`, run `aura chat`, observe WARN-and-drop not exit(1) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
