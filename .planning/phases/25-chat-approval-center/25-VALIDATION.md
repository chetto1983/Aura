---
phase: 25
slug: chat-approval-center
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-17
---

# Phase 25 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (backend stores/adapters/SSE/migration) + Vitest 4 + @testing-library/react 16 + Playwright (web cockpit) + Stryker 9 (frontend mutation) |
| **Config file** | `Makefile` (quality/coverage/sqlc), `web/vitest.config.ts`, `web/stryker.conf.json`, `scripts/cache_invariant_audit.sh` |
| **Quick run command** | `go test ./internal/conversations/... ./internal/askuser/... ./internal/agui/...` and `cd web && npm run test` |
| **Full suite command** | `go test -tags 'db_integration neo4j_integration' ./internal/...` (stack up) + `cd web && npm run test` (coverage ≥85%) + `bash scripts/cache_invariant_audit.sh` |
| **Estimated runtime** | ~120 seconds (Go integration tier + web vitest); + Playwright E2E ~30s; + Stryker mutation ~minutes (phase gate only) |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test ./internal/<pkg>/ -race` for touched Go; `cd web && npm run lint && npm run typecheck && npm run test` for touched web
- **After every plan wave:** full `go test -tags 'db_integration neo4j_integration' ./internal/...` on the live stack + `cd web && npm run test` (coverage ≥85%) + `bash scripts/cache_invariant_audit.sh` (after any D-09 touch — waves 6/7)
- **Before `/gsd-verify-work`:** full suite green + Playwright E2E green
- **Phase gate:** `cd web && npm run mutation` (Stryker ≥70% killed) + Go mutation spot-check ≥70% on `store_branch.go`, `ListPendingAll`, the `/api/` adapters
- **Max feedback latency:** ~120 seconds (integration tier)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 25-01-01 | 01 | 1 | CHAT-02 | T-25-01/02/05 | uuid.Parse 404 guard; FTS parameterized; sanitizeErr; +GET /{id} aggregates + /{id}/rot-events (thin ListContextRotEvents, no new query) | go integration | `go test -tags db_integration ./internal/agui/ -run TestConversationsAPI -race` | ❌ W0 | ⬜ pending |
| 25-01-02 | 01 | 1 | CHAT-03 | T-25-03 | reasoning-on cockpit-scoped (server.go:214); Telegram posture provably config-driven (agui_subscriber.go Translate(..,ShowReasoning), NOT true) | go unit (golden) | `go test ./internal/agui/ -run 'TestTranslate|Reasoning' && go test ./internal/channels/telegram/ -run 'Reasoning|Subscriber'` | ✅ extend translator_reasoning_test.go + agui_subscriber_test.go | ⬜ pending |
| 25-01-03 | 01 | 1 | CHAT-02 | T-25-04/05 | RequireAuth inherited; no bare /api/; integrations unshadowed | go unit | `go test ./cmd/aura/ -run 'ServeWebui|Route|Mount'` | ❌ W0 | ⬜ pending |
| 25-02-01 | 02 | 2 | APRV-01 | T-25-10 | parameterized aggregate; token-ASC tiebreaker | go integration | `go test -tags db_integration ./internal/askuser/ -run TestListPendingAll -race` | ❌ W0 (extend store_test.go) | ⬜ pending |
| 25-02-02 | 02 | 2 | APRV-02/03 | T-25-06/08/09 | decline≠accept; SanitizeString; uuid 404 | go integration | `go test -tags db_integration ./internal/agui/ -run TestApprovalsAPI -race && go test -tags db_integration ./internal/runner/ -run TestResolve -race` | ❌ W0 / ✅ extend runner_resume_test.go | ⬜ pending |
| 25-02-03 | 02 | 2 | APRV-02 | T-25-07 | RequireCapability on mutating resolve | go unit | `go test ./cmd/aura/ -run 'ServeWebui|Approvals'` | ❌ W0 | ⬜ pending |
| 25-03-01 | 03 | 3 | CHAT-01 | T-25-SC | [BLOCKING] package legitimacy (npmjs/slopcheck -e npm) | manual checkpoint | npmjs.com verification | n/a | ⬜ pending |
| 25-03-02 | 03 | 3 | CHAT-01/03/04 | T-25-12 | tool_call_id marker ≠ prose (Pitfall 2); /0 guard | vitest (golden frames) | `cd web && npm run test -- sseAdapter` | ❌ W0 (golden-events.json fixture) | ⬜ pending |
| 25-03-03 | 03 | 3 | CHAT-01/03 | T-25-11 | no dangerouslySetInnerHTML; sanitized markdown | vitest + build | `cd web && npm run test -- ReasoningDrawer ToolActivityCard && npm run build` | ❌ W0 | ⬜ pending |
| 25-04-01 | 04 | 4 | CHAT-02 | T-25-14/15/16 | delete behind confirm; auto-escaped text; RequireAuth | vitest | `cd web && npm run test -- ConversationSidebar SearchPanel` | ❌ W0 | ⬜ pending |
| 25-04-02 | 04 | 4 | CHAT-04 | — | /0 cache guard; no $NaN; off aura.display namespace; gauge marker bound to GET /api/conversations/{id}/rot-events (plan 25-01 route, no invented route) | vitest | `cd web && npm run test -- RuntimeFooter usageFromStateDelta ContextBudgetGauge` | ❌ W0 | ⬜ pending |
| 25-05-01 | 05 | 5 | APRV-01/06 | T-25-18/20 | terminal state never silent; polite announce; auto-escaped | vitest | `cd web && npm run test -- ApprovalList` | ❌ W0 | ⬜ pending |
| 25-05-02 | 05 | 5 | APRV-02/03 | T-25-17/19/20 | decline≠accept footgun; RequireAuth+Capability inherited | vitest | `cd web && npm run test -- InlineApprovalCard` | ❌ W0 | ⬜ pending |
| 25-06-01 | 06 | 6 | CHAT-05 | — | PRD-first amendment recorded before code | grep gate | `grep -c CHAT-05 .planning/REQUIREMENTS.md .planning/ROADMAP.md` | n/a | ⬜ pending |
| 25-06-02 | 06 | 6 | CHAT-05 | T-25-22/23 | slot 0017 verified (latest shipped 0016; `ls migrations/ \| grep -c '^0017' == 0` pre-create); tx-safe ALTER; default backfill; no CONCURRENTLY-in-tx | go migration | `go test -tags db_integration ./internal/db/ -run TestMigrate -race` | ✅ extend migrate_test.go | ⬜ pending |
| 25-06-03 | 06 | 6 | CHAT-05 | T-25-21 | [BLOCKING] cache-invariant; protected head byte-identical | go integration + gate | `go test -tags db_integration ./internal/conversations/ -run TestBranch -race && bash scripts/cache_invariant_audit.sh` | ❌ W0 (store_branch_test.go) | ⬜ pending |
| 25-07-01 | 07 | 7 | CHAT-05 | T-25-24/25/26 | [BLOCKING] cache head unchanged on switch; RequireCapability | go integration + gate | `go test -tags db_integration ./internal/agui/ -run TestBranch -race && bash scripts/cache_invariant_audit.sh` | ❌ W0 | ⬜ pending |
| 25-07-02 | 07 | 7 | CHAT-05 | — | no feedback group (Phase-26 boundary held) | vitest | `cd web && npm run test -- BranchPicker` | ❌ W0 | ⬜ pending |
| 25-07-03 | 07 | 7 | CHAT-01 | — | [BLOCKING] no-skip-as-green: setup throws under CI when no live stack AND no golden-events.json; ≥1 token-chunk assertion counted; deterministic CI path = golden-events.json replay (no synthetic-only; no test.skip) | playwright E2E | `cd web && npx playwright test chat.spec.ts` | ❌ W0 (chat.spec.ts) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/agui/conversations_api_test.go` — CHAT-02 (`db_integration`), incl. GET /{id} aggregates + GET /{id}/rot-events (thin `ListContextRotEvents` read)
- [ ] `internal/agui/translator_reasoning_test.go` (extend) + `internal/channels/telegram/agui_subscriber_test.go` (extend) — CHAT-03 cockpit flip + Telegram config-driven `ShowReasoning` posture (machine-checkable, NOT a blanket true)
- [ ] `internal/agui/approvals_api_test.go` — APRV-01/02/03 + the decline bridge
- [ ] `internal/askuser/store_test.go` (extend) — `ListPendingAll` (APRV-01)
- [ ] `internal/conversations/store_branch_test.go` + `context_branch_test.go` — branch path walk + byte-identity (D-09)
- [ ] `internal/db/migrations/0017_conversation_turn_branches.{up,down}.sql` + `migrate_test.go` extension
- [ ] `cmd/aura/serve_webui_test.go` (extend) — mount + no-shadow + capability gate
- [ ] `web/src/chat/__tests__/sseAdapter.test.ts` — SSE frame → parts reducer (CHAT-01/03/04), driven by `internal/agui/testdata/golden-events.json`
- [ ] `web/src/chat/__tests__/RuntimeFooter.test.tsx` — usage parse + /0 guard (CHAT-04)
- [ ] `web/src/chat/__tests__/{ReasoningDrawer,ToolActivityCard,BranchPicker}.test.tsx`
- [ ] `web/src/conversations/__tests__/{ConversationSidebar,SearchPanel}.test.tsx`
- [ ] `web/src/approvals/__tests__/{ApprovalList,InlineApprovalCard}.test.tsx`
- [ ] `web/e2e/chat.spec.ts` — Playwright: prompt → stream → resolve inline card → resume; setup throws under CI when no live stack AND no golden-events.json; ≥1 token-chunk assertion; deterministic CI path = golden-events.json replay

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| assistant-ui package legitimacy | CHAT-01 | Supply-chain pre-install human gate (T-25-SC) | Verify the 3 deps on npmjs.com + slopcheck `--ecosystem npm`; type "approved" (plan 25-03 Task 1) |
| Live streamed turn token-by-token | CHAT-01 | Real LLM stream over a live `aura serve` | The Playwright E2E covers this against the live stack; a CI mock replays captured real frames (no synthetic-only) |

*Most behaviors are automated; the package-legitimacy gate is a deliberate blocking human checkpoint per the security threat model.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags (vitest `run`, not watch; playwright single-run)
- [x] Feedback latency < 120s (per-task quick run)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
