---
phase: 27
slug: neo4j-graph-explorer
status: ready
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-19
updated: 2026-06-19
---

# Phase 27 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Per-task map populated by the planner from RESEARCH.md §"Validation Architecture"
> (GRAPH-01..04 → unit / integration (db_integration + neo4j_integration) / a11y / e2e tiers).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `go test` + table tests (backend) + Vitest 4 (frontend unit) + Playwright 1.61 (e2e) + Stryker 9 (mutation) |
| **Config file** | `web/vitest.config.ts`, `web/playwright.config.ts`, `web/stryker.config.json`; Go std tooling |
| **Quick run command** | `go test ./internal/knowledge/... ./internal/agui/...` + `cd web && npm run test -- src/graph` |
| **Full suite command** | `go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/... ./internal/agui/...` (WSL, live stack) + `cd web && npm run test -- --coverage && npm run test:e2e -- graph && npx stryker run` |
| **Estimated runtime** | unit (Go) ~8s · unit (Vitest graph) ~6s · Go neo4j_integration ~25–40s (live stack) · Playwright graph (3 device profiles) ~60–90s · Stryker (src/graph) ~3–5 min |

---

## Sampling Rate

- **After every task commit:** Run the quick run command for the touched surface (Go: `go test ./internal/knowledge/... ./internal/agui/...`; frontend: `cd web && npm run test -- src/graph`).
- **After every plan wave:** Run the full suite command (live Neo4j stack up via `make neo4j-migrate`).
- **Before `/gsd-verify-work`:** Full suite green + `make coverage` (owned-surface ≥85%, `internal/knowledge` + `internal/agui` included) + Vitest ≥85% on `src/graph` + Stryker ≥70% killed on `src/graph` + Playwright graph e2e green.
- **Max feedback latency:** quick run < 15s per surface; full integration tier < 2 min.

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 27-01-01 | 01 | 1 | GRAPH-01, GRAPH-04 | T-27-01 / T-27-02 | flat `{nodes,edges,paths,schema,query}` contract struct + `GraphReader` read-only seam; intent→**parameterized** Cypher (values ride param map, caps clamped, labels bound as `WHERE l IN $labels` data — never interpolated) | unit | `go test ./internal/knowledge/ -run 'TestCompile|TestContract' -x` | ❌ W0 `graphview_test.go` | ⬜ pending |
| 27-01-02 | 01 | 1 | GRAPH-04 | T-27-01 / T-27-02 | `assertReadOnly` write-verb guard rejects `CREATE/MERGE/SET/DELETE/REMOVE/DROP/FOREACH` + `CALL{…write}`; strips string literals first (verbs in data must NOT trip); mutation spot-check on the guard + cap-clamp | unit | `go test ./internal/knowledge/ -run 'TestAssertReadOnly|TestCap' -x` | ❌ W0 `graphview_test.go` | ⬜ pending |
| 27-01-03 | 01 | 1 | GRAPH-01 | T-27-04 / T-27-05 | row map → contract normalization (labels via `apoc.convert.toJson`→`json.Unmarshal`, edge endpoints attach via `elementId(startNode/endNode)`, `Entity.type` as 2nd dimension); schema endpoint + default-seed compile; **live mcp serialization shape pinned** + footprint probe (resolves A1/A3) | unit + neo4j_integration | `go test ./internal/knowledge/ -run TestNormalize -x` + `go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/ -run TestGraphViewLive` | ❌ W0 `graphview_test.go` + `graphview_integration_test.go` | ⬜ pending |
| 27-02-01 | 02 | 2 | GRAPH-01, GRAPH-04 | T-27-03 / T-27-05 | thin handlers `handleGraphSchema`/`handleGraphQuery` over a narrow `GraphView` seam; `MaxBytesReader` body-cap; `op` enum + caps-clamp + label/rel-type validation against live schema set; `sanitizeErr`/`SanitizeString` on every wire error | unit (httptest) | `go test ./internal/agui/ -run TestGraph -x` | ❌ W0 `graph_api_test.go` | ⬜ pending |
| 27-02-02 | 02 | 2 | GRAPH-01 | T-27-03 | `registerGraphRoutes` + `SetGraphView` setter (off constructor, 503-until-wired); `/api/graph/schema` + `/api/graph/query` mount as **specific siblings** under the `/api/` carve-out behind `RequireAuth` (no `RequireCapability`, no bare `/api/`); boot-time `knowledge.Client` appended to reverse-close teardown | unit (httptest) + behavior | `go test ./internal/agui/ -run 'TestGraph|TestMux' -x` + `go vet ./cmd/aura/... && go build ./...` | ❌ W0 `graph_api_test.go` | ⬜ pending |
| 27-03-01 | 03 | 2 | GRAPH-02 (pkgs) | T-27-SC | npm install of the 4 MIT graph packages at pinned versions; re-verify `npm view <pkg> version` + empty `scripts.postinstall` before lockfile commit (legitimacy gate) | checkpoint:human-verify | `cd web && for p in sigma graphology @react-sigma/core graphology-layout-forceatlas2; do npm view "$p" version; done` | ❌ W0 | ⬜ pending |
| 27-03-02 | 03 | 2 | GRAPH-02, GRAPH-04 | T-27-04 | pure `graphIntent.ts` (intent reducer, label/edge filter predicates, **schema-driven** label-family color mapper: known family→brand token, unmapped→`hash(label)→ramp` deterministic, `Entity.type` 2nd dimension) + `graphApi.ts` typed fetch + contract `types.ts`; NO `sigma` import (jsdom-safe) | unit (Vitest) | `cd web && npm run test -- src/graph -t 'color|filter|intent'` | ❌ W0 `web/src/graph/__tests__/graphIntent.test.ts` | ⬜ pending |
| 27-04-01 | 04 | 3 | GRAPH-02 | T-27-04 | three-pane `GraphExplorer` shell + `SigmaCanvas` (SigmaContainer + useLoadGraph/useRegisterEvents/useSigma, ForceAtlas2 position-cache, `key={sigmaKey}` resize-remount + ErrorBoundary, node fill=label-family / size=degree / caption); `SeedFilterPanel` (seed + filters + read-only Cypher preview); label-family ramp added to `contrast-check.mjs` | unit (mocked renderer) + e2e | `cd web && npm run test -- src/graph` + `npm run test:e2e -- graph.spec` + `node web/scripts/contrast-check.mjs` | ❌ W0 `web/e2e/graph.spec.ts` | ⬜ pending |
| 27-04-02 | 04 | 3 | GRAPH-03, GRAPH-04 | T-27-04 | `NodeInspector` (label/props/degree/neighbors/citations + pin-path/open-source via `openSources()`/show-Cypher; NO add-note) + `PathStrip` (D-10 mirror + a11y parallel node/edge list, `role="list"`, keyboard traversal, focus-return); **tap/focus opens inspector (hover never the only path)**; `surface==='graph'` lazy swap in `AppShell` | a11y/e2e + unit | `cd web && npm run test:e2e -- graph-a11y.spec` (axe 0 WCAG-AA violations + keyboard/tap) + `npm run test -- src/graph -t inspector` | ❌ W0 `web/e2e/graph-a11y.spec.ts` | ⬜ pending |
| 27-04-03 | 04 | 3 | GRAPH-02, GRAPH-03 | — | i18n `graph.*` keys (en+it) via new `resources.graph.ts` spread into `resources.ts`; extend Stryker `mutate` to `src/graph/**`; rebuild + commit `internal/webui/dist` (CI freshness gate); empty/loading/error/populated states | unit + build | `cd web && npm run build && npx stryker run` (≥70% killed on src/graph) + verify a distinct Sigma lazy chunk in Vite output | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/knowledge/graphview_test.go` — intent→Cypher (params not interpolated, caps clamped), row→contract (labels via `apoc.convert.toJson`, edge endpoints), `assertReadOnly` (each verb present/absent, verbs in string literals, verbs in `CALL{}`) (GRAPH-01/04)
- [ ] `internal/knowledge/graphview_integration_test.go` (`//go:build neo4j_integration && db_integration`) — pin the live mcp-neo4j-cypher serialization shape (Open Question A1) + `:Conversation`/`:Message` footprint probe (Open Question A3; `session_id == ThreadID`, else schema-overview fallback)
- [ ] `internal/agui/graph_api_test.go` — httptest over a fake `GraphView` (route registration, 400 bad-intent, 401 unauth, contract JSON shape)
- [ ] `web/src/graph/__tests__/graphIntent.test.ts` — filter/color/intent/normalization logic with NO WebGL (jsdom)
- [ ] Mock module for `@react-sigma/core` (or the whole `SigmaCanvas`) so component tests run in jsdom (Pitfall 4)
- [ ] `web/e2e/graph.spec.ts` + `web/e2e/graph-a11y.spec.ts` — render + keyboard/tap/axe across desktop + mobile profiles (GRAPH-02/03)
- [ ] Stryker `mutate` scope extension to include `web/src/graph/**`

*All Wave-0 scaffolds are created inside the plan task that first needs them (per-task `<read_first>`/`<acceptance_criteria>` reference the file), satisfying the Nyquist rule.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Visual legibility of dense-graph default (evidence paths, not hairball) | GRAPH-04 | Subjective visual quality on live data | Open Graph mode on a conversation with a real memory footprint; confirm filtered evidence paths (caps applied, `graph.cap.notice` shown on overflow), not a hairball; expand a node and confirm it grows intentionally |
| Mutation spot-check on the write-verb guard + cap-clamp + intent compiler | GRAPH-04 | go-mutesting runs on WSL only (go1.26 fork) | WSL: `GOFLAGS=-tags=db_integration go-mutesting ./internal/knowledge/graphview*.go` — record killed/total in the phase VALIDATION Manual-Only table; `PASS`=killed |

*Prefer automated where feasible; the dense-graph legibility check is the one irreducibly-visual gate.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 2 min (full integration tier)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
