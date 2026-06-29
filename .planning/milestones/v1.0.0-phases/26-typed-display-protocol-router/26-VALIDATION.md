---
phase: 26
slug: typed-display-protocol-router
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-18
---

# Phase 26 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `26-RESEARCH.md` §"Validation Architecture". Frontend gates: Vitest ≥85% coverage + Stryker ≥70% mutation (blocking CI). Backend gates: ≥85% owned-surface coverage + `cache_invariant_audit.sh` CI gate green.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (Go)** | stdlib `testing` + table/golden tests; `-race`; mutation via `go-mutesting` (WSL) |
| **Framework (Frontend)** | Vitest 4.1.9 + @testing-library/react 16; Stryker 9.6.1 mutation; Playwright e2e |
| **Config file** | `web/package.json` scripts (vitest/stryker); Go `go.mod` (no config) |
| **Quick run (Go)** | `go test ./internal/agent/display/ ./internal/agui/ ./internal/web/` |
| **Quick run (Frontend)** | `cd web && npm run test` (vitest run --coverage) |
| **Full suite (Go)** | `make quality-full` (vet+build+lint+race+vuln+coverage; stack up) |
| **Full suite (Frontend)** | `cd web && npm run test && npm run mutation && npm run lint && npm run typecheck` |
| **Cache-invariant gate** | `bash scripts/cache_invariant_audit.sh` (drives `aura cache-audit`, messages[0] hash) |
| **Contrast gate** | `cd web && npm run contrast` (`scripts/contrast-check.mjs`, AA pairs) |
| **Estimated runtime** | Go quick ~10–30s; Frontend quick ~20–40s; full matrix several min (stack up) |

---

## Sampling Rate

- **After every task commit:** the package's quick run (`go test ./internal/agent/display/` or `npm run test -- <component>`) + `go vet`/`go build` (Go) or `npm run lint`/`typecheck` (frontend).
- **After every plan wave:** full Go `make quality` + `cd web && npm run test && npm run mutation`; `bash scripts/cache_invariant_audit.sh`; `npm run contrast`.
- **Before `/gsd-verify-work`:** `make quality-full` green (≥85% owned-surface) + Vitest ≥85% + Stryker ≥70% killed + cache-invariant gate green + Playwright replay e2e green.
- **Max feedback latency:** ~40 seconds (frontend quick run).

---

## Per-Task Verification Map

> Mapped to the 6 plans (Waves 1–4). Each row maps a task to its requirement, threat ref, and automated command. Derived from the Phase Requirements → Test Map below.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|--------|
| 26-01-T1 | 26-01 | 1 | DISP-01 | T-26-04 | Actions.Display omitempty identity round-trip | unit (Go) | `go test ./internal/agent/ -run TestActionsDisplayRoundTrip` | ⬜ |
| 26-01-T2 | 26-01 | 1 | DISP-01/04/05/SWARM-01 | T-26-01/02/03 | normalizer typed payload; default→false; safe reasons; stable RefIDs | unit/golden (Go) | `go test ./internal/agent/display/ -run TestNormalize` | ⬜ |
| 26-01-T3 | 26-01 | 1 | DISP-01 | T-26-04 | aura.display CUSTOM beside artifact | golden (Go) | `go test ./internal/agui/ -run TestTranslate` | ⬜ |
| 26-02-T1 | 26-02 | 1 | DISP-01/06 | T-26-07 | reopen rehydration (RequireAuth) | unit (Vitest) | `cd web && npm run test -- snapshotToMessages` | ⬜ |
| 26-02-T2 | 26-02 | 1 | DISP-02 | T-26-06 | reducer attaches by toolCallId; unknown CUSTOM no-op | unit (Vitest) | `cd web && npm run test -- sseAdapter` | ⬜ |
| 26-02-T3 | 26-02 | 1 | DISP-02/03/HARDEN-08 | T-26-05 | router default→raw card (never null); pagination | unit (Vitest) | `cd web && npm run test -- DisplayRouter DisplayPagination` | ⬜ |
| 26-03-T1 | 26-03 | 2 | DISP-01/05 | T-26-08 | volatile source list NOT in messages[0]; cache gate green | unit (Go) + CI gate | `go test ./internal/agent/prompt/ -run TestBudgetSources && bash scripts/cache_invariant_audit.sh` | ⬜ |
| 26-03-T2 | 26-03 | 2 | DISP-03/D-09 | T-26-09/10 | FetchImage SSRF guard + RequireAuth + image allowlist | integration (Go) | `go test ./internal/web/ -run TestFetchImage && go test ./internal/agui/ -run TestImageProxy` | ⬜ |
| 26-03-T3 | 26-03 | 2 | DISP-01/D-06 | T-26-11 | re-derive at projectMessages; replay==live | integration (Go) | `go test ./internal/agui/ -run TestProjectMessagesDisplay` | ⬜ |
| 26-04-T1 | 26-04 | 2 | DISP-02/03 | T-26-13 | table sort/filter/CSV/paginate; chart zero-dep SVG | unit (Vitest) | `cd web && npm run test -- TableDisplay ChartDisplay` | ⬜ |
| 26-04-T2 | 26-04 | 2 | DISP-04 | T-26-12 | all 8 web/errors.go codes→safe label; no SSRF internals | unit (Vitest) | `cd web && npm run test -- SystemEventCard` | ⬜ |
| 26-04-T3 | 26-04 | 2 | SWARM-01 | T-26-14 | ChildReport table + row expand; no mailbox theater | unit (Vitest) | `cd web && npm run test -- SwarmReportTable LocalArtifactDisplay` | ⬜ |
| 26-05-T1 | 26-05 | 3 | DISP-02/03 | T-26-SC | package legitimacy (blocking human) | checkpoint | npmjs.com review (radix-hover-card, shiki) | ⬜ |
| 26-05-T2 | 26-05 | 3 | DISP-03 | T-26-17/18 | rehypeCitations inline splice; CitationBubble focus+tap | unit (Vitest) | `cd web && npm run test -- rehypeCitations CitationBubble DocumentDisplay` | ⬜ |
| 26-05-T3 | 26-05 | 3 | DISP-02/D-09/D-10 | T-26-15/16 | image-proxy <img>; Shiki escaped spans (never exec) | unit (Vitest) + build | `cd web && npm run test -- WebResultDisplay CodeDisplay && npm run build` | ⬜ |
| 26-06-T1 | 26-06 | 4 | DISP-05 | T-26-19/20 | read-only Source Explorer (no PATCH/destructive) | unit (Vitest) | `cd web && npm run test -- SourceExplorerSheet` | ⬜ |
| 26-06-T2 | 26-06 | 4 | DISP-05/D-13 | T-26-21 | "Sources (N)" + citation click-through (one registry) | unit (Vitest) | `cd web && npm run test -- SourcesButton` | ⬜ |
| 26-06-T3 | 26-06 | 4 | DISP-01/06 | — | replay e2e; Stryker scope; dist rebuild | e2e + mutation | `cd web && npm run test:e2e -- replay && npm run mutation` | ⬜ |

### Phase Requirements → Test Map (from RESEARCH.md)

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DISP-01 | Normalizer maps each tool result → correct `Payload` shape | unit/golden (Go) | `go test ./internal/agent/display/ -run TestNormalize` | ❌ Wave 0 |
| DISP-01 | `Actions.Display` decode(encode)==identity | unit (Go) | `go test ./internal/agent/ -run TestActionsDisplayRoundTrip` | ❌ Wave 0 |
| DISP-01 | `Translate` emits `aura.display` CUSTOM beside artifact (golden) | golden (Go) | `go test ./internal/agui/ -run TestTranslate` (extend golden-events.json) | ✅ extend |
| DISP-01 | `messages[0]` byte-stable with source-list tail-inject | CI gate | `bash scripts/cache_invariant_audit.sh` | ✅ extend fixtures |
| DISP-01/05 | Volatile source list NOT in `messages[0]`; rides Budget copy | unit (Go) | `go test ./internal/agent/prompt/ -run TestBudgetSources` | ❌ Wave 0 |
| DISP-02 | `DisplayRouter` renders each type; `default:`→raw card (never null) | unit (Vitest) | `cd web && npm run test -- DisplayRouter` | ❌ Wave 0 |
| DISP-02 | Reducer attaches `aura.display` payload to tool part by `toolCallId` | unit (Vitest) | `npm run test -- sseAdapter` | ✅ extend |
| DISP-02/HARDEN-08 | Unknown-type/malformed payload renders escaped, never markdown | unit (Vitest, XSS assertion) | `npm run test -- DisplayRouter` | ❌ Wave 0 |
| DISP-03 | Pagination "X–Y of N" + prev/next; default 3/page | unit (Vitest) | `npm run test -- DisplayPagination` | ❌ Wave 0 |
| DISP-03 | Citation chip → hovercard (focus + tap, not hover-only) | unit (Vitest) + a11y | `npm run test -- CitationBubble`; `npm run lint` (jsx-a11y) | ❌ Wave 0 |
| DISP-03 | `rehypeCitations` splices inline at the `[n]` claim position | unit (Vitest) | `npm run test -- rehypeCitations` | ❌ Wave 0 |
| DISP-04 | Each `web/errors.go` code → safe label; no SSRF internals | unit (Vitest) + Go enum coverage | `npm run test -- SystemEventCard`; `go test ./internal/web/ -run TestSanitize` | ❌ Wave 0 / ✅ Go exists |
| DISP-05 | Source Explorer Table sort/search/paginate; Metadata/Config read-only | unit (Vitest) | `npm run test -- SourceExplorerSheet` | ❌ Wave 0 |
| DISP-05/D-09 | Image-proxy reuses SSRF guard; blocks private/metadata targets | integration (Go, `web_integration`) | `go test -tags web_integration ./internal/web/ -run TestFetchImage` | ❌ Wave 0 |
| SWARM-01 | `swarm_report` table over `[]ChildReport`; row expand shows summary/error/question | unit (Vitest) | `npm run test -- SwarmReportTable` | ❌ Wave 0 |
| D-06 | Reopened thread re-derives + renders displays identically to live | integration (Go snapshot) + Vitest | `go test ./internal/agui/ -run TestProjectMessagesDisplay`; `npm run test -- snapshotToMessages` | ❌ Wave 0 |
| D-06 | `GET /threads/{id}/messages` fetched on reopen | e2e (Playwright) | `npm run test:e2e -- replay` | ❌ Wave 0 |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

> Every `<verify>` below is `❌ Wave 0` (the test file does not yet exist) and is CREATED by the task that owns it (RED→GREEN within the same plan, per the TDD `<behavior>` blocks). No task ships without its `<automated>` verify.

- [ ] `internal/agent/display/*_test.go` — normalizer table/golden tests (26-01-T1/T2; DISP-01/02/04, SWARM-01)
- [ ] `internal/agent/prompt/budget_sources_test.go` + `cache_audit_sources_test.go` — source-list tail-inject + messages[0] stability (26-03-T1; DISP-01/05)
- [ ] `internal/agui/testdata/golden-events.json` — extend with an `aura.display` fixture (26-01-T3; DISP-01)
- [ ] `scripts/cache_invariant_audit.sh` fixtures — add a turn that emits a source list (26-03-T1; DISP-01)
- [ ] `internal/web/fetcher_image_test.go` + `internal/agui/image_proxy_test.go` — `FetchImage` SSRF + RequireAuth (26-03-T2; D-09)
- [ ] `internal/agui/server_display_test.go` — projectMessages re-derive parity (26-03-T3; D-06)
- [ ] `web/src/chat/displays/__tests__/*` — DisplayRouter, DisplayPagination, snapshotToMessages, TableDisplay, ChartDisplay, SystemEventCard, SwarmReportTable, LocalArtifactDisplay, rehypeCitations, CitationBubble, DocumentDisplay, WebResultDisplay, CodeDisplay, SourceExplorerSheet, SourcesButton (26-02..06; DISP-02..05, SWARM-01, D-06)
- [ ] `web/src/chat/__tests__/sseAdapter.test.ts` — extend with CUSTOM/aura.display frame (26-02-T2)
- [ ] Playwright `web/tests/e2e/replay.spec.ts` — reopen-thread rehydration (26-06-T3; D-06)
- [ ] `stryker.config.json` — ensure the new `web/src/chat/displays/` dir is in the mutation scope (26-06-T3)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Citation hovercard visual fidelity (premium bar) | DISP-03 | Visual/UX judgement not machine-checkable | Run `aura serve`, ask a web_search question, hover/focus a `[n]` chip, confirm type-icon + title + snippet preview, click → Source Explorer opens |
| Source Explorer Table/Metadata/Configuration read-only views | DISP-05 | Operator-flow judgement | Open Source Explorer via "Sources (N)"; confirm sort/search/paginate; confirm Metadata + Configuration panes are read-only (no PATCH/destructive controls) |
| `swarm_report` table — no inter-agent chat / mailbox theater | SWARM-01 | Negative visual assertion | Run a `swarm_spawn`, confirm summary table over ChildReport with row-expand; confirm NO mailbox/chat UI |
| Go mutation spot-check ≥70% on normalizer critical file(s) | DISP-01 | Run on WSL `go-mutesting` (not CI) | `go-mutesting ./internal/agent/display/...` on WSL; PASS=killed; record in VALIDATION Manual-Only |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 40s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
