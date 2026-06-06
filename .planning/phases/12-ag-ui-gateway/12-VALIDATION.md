---
phase: 12
slug: ag-ui-gateway
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-06
---

# Phase 12 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) + rapid (property-based) + goleak |
| **Config file** | none — standard `go test`; tagged tiers per repo convention |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/agui/` |
| **Full suite command** | `go test -race ./... && go test -tags db_integration -race ./internal/agui/ ./internal/conversations/` (DSNs from POSTGRES_PASSWORD per repo convention) |
| **Estimated runtime** | ~30s quick / ~120s full (stack up) |

---

## Sampling Rate

- **After every task commit:** Run `go vet ./... && go build ./... && go test -race ./internal/agui/`
- **After every plan wave:** Run full suite command (incl. db_integration tier with stack up)
- **Before `/gsd-verify-work`:** Full suite green + live smoke (aura serve + curl SSE) + boundary gate + pin grep
- **Max feedback latency:** 120 seconds

---

## Per-Task Verification Map

*To be filled by the planner — one row per task. Seed mapping from RESEARCH.md §Validation Architecture:*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| SC-1 | TBD | TBD | UX-01 | — | loopback-only bind default | smoke | `./aura serve & curl -N -X POST http://127.0.0.1:9080/agent/run -d @testdata/run-input.json` → RUN_STARTED…RUN_FINISHED SSE frames | ❌ W0 | ⬜ pending |
| SC-2 | TBD | TBD | UX-01 | — | N/A | ci-gate | boundary script: `go list -deps ./internal/agent/ \| grep -q internal/agui && exit 1` (inverted) | ❌ W0 | ⬜ pending |
| SC-3 | TBD | TBD | UX-01 | — | thread 404 on unknown id | integration | `curl http://127.0.0.1:9080/threads/<id>/messages` → MESSAGES_SNAPSHOT JSON matching streamed turns (db_integration tier) | ❌ W0 | ⬜ pending |
| SC-4 | TBD | TBD | UX-01 | — | immutable dep pin | ci-gate | `grep -E 'github\.com/ag-ui-protocol/ag-ui/sdks/community/go v0\.0\.0-20260514093510-e9e910b230b9' go.mod` → exactly 1 match | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@e9e910b230b9329c905e31ca024b4114dedf7918` + transitive-sums `go get .../pkg/core/events@v0.0.0-20260514093510-e9e910b230b9` (spike 014 two-step)
- [ ] `internal/agui/testdata/` seeded from `.claude/skills/spike-findings-Aura/sources/015-agui-event-surface/golden-events.json` (21 golden wire shapes)
- [ ] Boundary-gate script + CI wiring (SC-2) — exists before any `internal/agui` code lands
- [ ] Pin-grep CI gate (SC-4)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Mutation spot-check ≥70% on translator.go | Gate 3 DoD | go-mutesting runs on WSL, manual per repo convention | WSL: `PATH=~/go/bin:$PATH go-mutesting ./internal/agui/translator.go` — PASS=killed, score=killed/total, autopsy survivors before chasing score |
| Live operator smoke (SC-1/SC-3 by hand) | UX-01 | Operator-observable per ROADMAP wording | `./aura serve` then curl per SC-1/SC-3 rows; inspect SSE frames visually (≥1 body print) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
