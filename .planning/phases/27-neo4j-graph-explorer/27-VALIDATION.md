---
phase: 27
slug: neo4j-graph-explorer
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-19
---

# Phase 27 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Detailed per-task map is populated by the planner from RESEARCH.md §"Validation Architecture"
> (GRAPH-01..04 → unit / integration (db_integration + neo4j_integration) / a11y / e2e tiers).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `go test` (backend) + Vitest (frontend) + Playwright (e2e) |
| **Config file** | `web/vitest.config.ts`, `web/playwright.config.ts`; Go std tooling |
| **Quick run command** | `go test ./internal/knowledge/... ./internal/agui/...` + `cd web && npm run test` |
| **Full suite command** | `go test -tags 'db_integration neo4j_integration' ./...` + `cd web && npm run test && npm run test:e2e` |
| **Estimated runtime** | ~TBD seconds (planner to refine) |

---

## Sampling Rate

- **After every task commit:** Run the quick run command for the touched surface
- **After every plan wave:** Run the full suite command
- **Before `/gsd-verify-work`:** Full suite must be green (live Neo4j stack up)
- **Max feedback latency:** TBD seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 27-XX-XX | XX | X | GRAPH-01 | T-27-XX / — | normalizer maps MCP rows → `{nodes,edges,paths,schema,query}`; explicit-field projection (never `RETURN n`) | unit/integration | `{command}` | ❌ W0 | ⬜ pending |
| 27-XX-XX | XX | X | GRAPH-02 | — | WebGL canvas renders evidence paths + label-family color + path strip | unit/e2e | `{command}` | ❌ W0 | ⬜ pending |
| 27-XX-XX | XX | X | GRAPH-03 | — | node inspector via tap/focus (hover never the only access path) | a11y/e2e | `{command}` | ❌ W0 | ⬜ pending |
| 27-XX-XX | XX | X | GRAPH-04 | T-27-XX | structured intents only; parameterized read-Cypher; write-verb guard rejects CREATE/MERGE/SET/DELETE/DROP/REMOVE; dense→filtered evidence paths | unit/integration | `{command}` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/knowledge/graphview_test.go` + `graphview_integration_test.go` — pin the live
      mcp-neo4j-cypher serialization shape (Open Question A1) + normalizer contract for GRAPH-01
- [ ] `:Conversation`/`:Message` footprint probe (Open Question A3) — confirm the loop populates
      `session_id == ThreadID`, or rely on the schema-overview fallback for the default seed
- [ ] Frontend test seam mocking the Sigma/WebGL renderer so Vitest can unit-test the
      intent→Cypher compiler + normalizer + filter logic without a real WebGL context
- [ ] Playwright e2e scaffold for the Graph mode-swap workspace (desktop + mobile, keyboard/tap)

*Planner refines from RESEARCH.md §"Validation Architecture".*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Visual legibility of dense-graph default (evidence paths, not hairball) | GRAPH-04 | Subjective visual quality on live data | Open Graph mode on a conversation with a real memory footprint; confirm filtered evidence paths, not a hairball |

*Planner/executor refine; prefer automated where feasible.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < TBDs
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
