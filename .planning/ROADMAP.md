# Roadmap: Aura

## Milestones

- ✅ **v0.0.0 Substrate** — Phases 0–21 (shipped 2026-06-15) — full details in [`milestones/v0.0.0-ROADMAP.md`](milestones/v0.0.0-ROADMAP.md)
- ✅ **v1.0.0 Aura Deep Search Web Cockpit** — Phases 22–30 (shipped 2026-06-29) — full details in [`milestones/v1.0.0-ROADMAP.md`](milestones/v1.0.0-ROADMAP.md)
- 🔨 **v2.0.0 Industrial Hardening & Multi-User Production** — Phases 31–41 (in planning) — close the 51-finding security audit + the ~64-finding quality audit to an honest 10/10 via stabilization/cleanup + per-user full-capability sandbox + multi-user identity isolation + ToolGateway + observability/security/ops industrialization

## Phases

<details>
<summary>✅ v0.0.0 Substrate (Phases 0–21) — SHIPPED 2026-06-15</summary>

- [x] Phase 0: PRD Amendments (6/6) — 2026-05-29
- [x] Phase 1: Infra DB + Knowledge — 2026-05-30
- [x] Phase 2: Agent Cornerstone (8/8) — 2026-05-30
- [x] Phase 3: LLM Client + ToolResult (5/5) — 2026-05-30
- [x] Phase 4: HITL + Identity + Conversations (5/5) — 2026-05-30
- [x] Phase 6: KV Cache Builder (5/5) — 2026-06-02
- [x] Phase 7: Web Tools (4/4) — 2026-06-02
- [x] Phase 07.1: Agent-Loop Forced Finalization (INSERTED) — 2026-06-03
- [x] Phase 8: Sandbox via sandbox-agent (replaces bespoke 2a/2b) — 2026-06-03
- [x] Phase 08.1: Tool Search hardening — defer_loading parity (INSERTED) — 2026-06-03
- [x] Phase 08.2: Semantic tool_search + unified semindex (INSERTED) — 2026-06-05
- [x] Phase 9: Swarm (Minimal) (6/6) — 2026-06-04
- [x] Phase 10: Scheduler (6/6) — 2026-06-04
- [x] Phase 11: Skills (10/10) — 2026-06-06
- [x] Phase 12: AG-UI Gateway (6/6) — 2026-06-07
- [x] Phase 13: Channels + Telegram + Multimodal (10/10) — 2026-06-08
- [x] Phase 14: Onboarding + Agent.md (5/5) — 2026-06-14
- [x] Phase 15: Memory Subsystem (5/5) — 2026-06-12
- [x] Phase 16: MCP Sidecar Manager + Third-Party Trust (8/8) — 2026-06-04
- [x] Phase 17: Packaging & Distribution (8/8) — 2026-06-14
- [x] Phase 18: Slice 7e Executable Snippet Reuse — 2026-06-08
- [x] Phase 19: Audit Bug Resolution + E2E Live Test — 2026-06-10
- [x] Phase 20: Scheduler Hardening Full Implementation — 2026-06-11
- [x] Phase 21: Plugins — Hooks (Slice EXT-1) — 2026-06-15

> Phase 5 (bespoke Sandbox 2a) was superseded by the Phase 8 sandbox-agent pivot (D-15) and is not counted.

</details>

<details>
<summary>✅ v1.0.0 Aura Deep Search Web Cockpit (Phases 22–30) — SHIPPED 2026-06-29</summary>

Embedded Vite + React + assistant-ui operator cockpit over the AG-UI/SSE gateway, per `docs/design/aura-deep-search-figma/ux-spec.md`. Hardened the agent perimeter first, then built the research-locked industrial frontend foundation, the serve/auth/health web host, the Core-Value chat+approval loop, the typed-display spine + router, the read-only graph explorer, read-only governance + web onboarding, and finally the governance WRITE surfaces (MCP config + skills install/lifecycle, landing last after auth + approval center + read-only boards were proven). The `ui_control` operator-OS shell and scheduler write surfaces were deferred to a follow-up milestone.

- [x] Phase 22: Agent Perimeter Hardening (5/5) — 2026-06-15 (Gate-3 closed w/ operator live sign-off 2026-06-16)
- [x] Phase 23: Frontend Infrastructure & Industrial Foundation (3/3) — 2026-06-16
- [x] Phase 24: Web Foundation — Serve + Auth + Health (4/4) — 2026-06-16
- [x] Phase 25: Chat + Approval Center (7/7) — 2026-06-17
- [x] Phase 26: Typed-Display Protocol + Router (6/6) — 2026-06-19
- [x] Phase 27: Neo4j Graph Explorer (4/4) — 2026-06-19
- [x] Phase 28: Governance Boards + Web Onboarding (6/6) — 2026-06-20
- [x] Phase 29: Governance Write — MCP Configuration + Skills Install (5/5) — 2026-06-21
- [x] Phase 30: Retrieval & Memory Pipeline Hardening — Rerank + Full-Docs E2E (5/5) — 2026-06-28

> **Cockpit Overhaul (post-Phase-25, not a formal phase).** A premium-bar overhaul reworked the
> Phase-23/24/25 surfaces in place: a logo-matched **blue** design system (operator-accepted
> 2026-06-18, fonts + WCAG AA gate), a responsive shell (svh grid, drawers, edge-swipe,
> intent-restore, 380px floor), chat/footer enrichment, **Authula** embedded auth (flag-gated),
> an `aura.settings` settings page, and calendar/PIM + WhatsApp connect. Specs + adversarial
> validation + per-spec ledgers: `docs/cockpit-overhaul/` (`00-VALIDATION.md` = umbrella).
>
> **Closeout:** `override_closeout` — audit PASSED (56/56), 6 deferred-by-design verification items
> (GPU-host live tiers unrunnable on this 4GB-GPU host + live-CI-only tiers + Phase-25
> carried-forward UAT). See `.planning/STATE.md` → Deferred Items and `milestones/v1.0.0-MILESTONE-AUDIT.md`.

</details>

### 🔨 v2.0.0 Industrial Hardening & Multi-User Production (Phases 31–41) — IN PLANNING

**Goal:** Close the entire 2026-06-21 industrial audit (51 findings, F-001..F-052) **AND** the maintainability/architecture audit (`docs/audit/quality/`, ~64 findings) to an **honest 10/10** production-readiness (from 4.6/10) — via a per-user full-capability sandbox (Docker, resolving F-001 without stripping the full-host surface), multi-user identity isolation (no RBAC), Authula cutover, a central ToolGateway, observability/security/ops industrialization, and an upfront code-quality cleanup. Every hardening behavior is a **no-op under `dev`/`local_trusted`** — the operator's daily full-host experience is unchanged; hardening activates under `server_production`. Dependency rule: stabilize+cleanup → profiles + ledger → gateway → identity → sandbox → MCP → idempotency/obs → security → ops/eval. Requirements + traceability in [`REQUIREMENTS.md`](REQUIREMENTS.md); research in [`research/SUMMARY.md`](research/SUMMARY.md); quality audit in [`../docs/audit/quality/`](../../docs/audit/quality/README.md).

- [x] **Phase 31: Stabilization & CI Unblock** (Wave 0 — URGENT, prerequisite) — `QUAL-01`, `F-015`, `SEC-08` (quality audit + CI hygiene + critical CodeQL SSRF) (completed 2026-06-29)
  - Goal: clean tree + green CI so every commit passes hooks (drop the `--no-verify` workaround); remediate the critical CodeQL SSRF pulled forward from the security backlog.
  - Success: (1) no production/test file >600 LOC (`cmd/aura/serve_webui.go`, `web/src/__tests__/LoginPage.test.tsx` split); (2) `internal/webui/dist` rebuilt → `web-dist-freshness` green; (3) all Go CI jobs use `scripts/go_packages.sh`, no raw `./...` (F-015); (4) frontend branch-coverage gate restored ≥85%; (5) critical CodeQL `go/request-forgery` (SSRF) at `internal/mcp/http_client.go` remediated + alert resolved (SEC-08).
- [x] **Phase 32: Quality Cleanup — Dead Code + Shared Helpers** — `QUAL-02/03/05` (quality audit ~64 findings) (completed 2026-06-30)
  - Goal: kill cross-package duplication + dead code BEFORE feature phases build on them (so later work reuses clean shared packages).
  - Success: (1) dead exports/placeholders removed, each confirmed via `deadcode`/`knip`/repo-wide `rg`; (2) shared packages extracted — `internal/neostore`, `internal/envutil`, `internal/agentrender`, agent `CanonicalArgs`/`isTransientNetworkErr`, web single `getJSON`/shared `focusTrap` — with a parity test per extraction; (3) targeted test gaps closed (`web/throttle`, setup ordering, Telegram keyword fallback, `truncateTailBytes`).
- [x] **Phase 33: Runtime Profiles + Config Validation** (keystone) — `PROF-01..06`, `QUAL-04`(env catalog) (F-002/007/016/018/026/041)
  - Goal: 4 validated profiles (`dev`/`local_trusted`/`single_user_hardened`/`server_production`) in `internal/config`; production fails fast on unsafe defaults; all hot-path `AURA_*` knobs catalogued.
  - Success: (1) `aura config validate --profile server_production` exits non-zero listing every unmet requirement; (2) copying `.env.example`→`.env` keeps the destructive-shell gate active; (3) invalid env fails-fast under production, warns under dev; (4) `dev`/`local_trusted` preserve today's full-host behavior unchanged.
- [x] **Phase 34: Agent-Loop Correctness + Durable Ledger** — `LOOP-01..11`, `QUAL-04`(double-Validate/pool-leak, int32 guard) (F-003/004/005/009/010/029/030/031/040/045/048) (completed 2026-07-03)
  - Goal: terminal-response exclusivity, atomic HITL resume/pause (single cross-store transaction), fenced sidecars, crash-orphan reconciliation.
  - Success: (1) `text_response` + mutating sibling never executes the sibling; (2) duplicate single/batch resume → exactly one answer/pause, append-failure leaves a repairable state; (3) outside-root/traversal/symlink sidecar reads rejected; (4) mutating tool that panics post-side-effect still arms the completion gate.
- [x] **Phase 35: ToolGateway + Policy Engine** — `GATE-01..04` (F-001 gateway, F-006/011/020) (executed 7/7; gap-closure 35-07 CLOSED the BLOCKING code-review Critical CR-01 by porting shell_exec's ApproveChallenge challenge/question binding into GatewayApprovals — the resume hook now records an approval ONLY when the gateway issued a server-side challenge for the authenticated (conv,tool,args_sha256) AND the operator-visible question matches, closing the confused-deputy bypass; WR-01/02/03 + IN-01/02 folded, unit -race + live db_integration green, 2026-07-04; re-verification PASSED 11/11 must-haves + code-review re-run CLEAN — CR-01 confused-deputy bypass closed, an in-cycle fix (63922e54) closed a residual Approve/pending-clear warning; GATE-01..04 all flipped [x]) (completed 2026-07-04)
  - Goal: one in-process policy decision on every tool call; fail-closed for mutating tools; durable reservation.
  - Success: (1) no tool executes without a recorded policy decision; (2) a timing-out/crashing command hook denies under hardened/production; (3) a mutating tool is blocked when ledger reservation fails in production; (4) gateway is a no-op (fail-open, host-direct) under dev/local_trusted.
- [ ] **Phase 36: Multi-User Identity Isolation + Authula Cutover** — `MUSR-01..06`, `QUAL`(Authula DSN test) (F-012/028/032/039/050)
  - Goal: owner-scope every user-facing store/API/job to the authenticated principal; cut over to Authula (no RBAC). Includes per-identity isolation for MCP config, Garage object-store, and skills dirs (see spike `.planning/spikes/`).
  - Success: (1) two-identity live E2E — B cannot list/get/delete/archive/resolve A's data (404/403), B-created chat owned by B and runs; (2) session B cannot poll/kill session A's shell, jobs expire by TTL; (3) conversation delete evicts all session tool state; (4) Authula default with provisioning + break-glass, no token in URLs.
- [ ] **Phase 37: Per-User Full-Capability Sandbox** — `SBX-01..05` (F-001 sandbox, F-036)
  - Goal: resolve F-001 — host shell/fs run inside a per-identity full-capability Docker sandbox under hardened/production; host never exposed.
  - Success: (1) under `server_production` shell/fs target the per-identity sandbox, real host filesystem unreachable; (2) Docker-socket/`--privileged`/`--network host`/bind-mounts unrepresentable (test-asserted); (3) cross-identity leakage impossible + idle-TTL lifecycle works; (4) configured egress allowlist cannot reach a disallowed host (default `--network none`); (5) ADR records container-per-identity (K8s/gVisor-default → DGX) + pre-merge concurrency benchmark on 32GB.
- [ ] **Phase 38: MCP Governance Hardening** — `MCPH-01..09`, `QUAL-03`(trust-norm unify) (F-013/014/027/033/034/035/037/038/046)
  - Goal: one canonical transport classifier + explicit remote trust + bounded MCP lifecycle + audited CLI writes.
  - Success: (1) mixed url+command / empty-remote-trust blocked and never call stdio open; (2) hung mount drops within deadline, oversized stdio frame aborts without large alloc, shutdown leaves no child processes; (3) CLI mutations append `mcp_audit` (or production-disallowed), empty trust body → 400; (4) dead HTTP MCP endpoint reports OK=false.
- [ ] **Phase 39: Idempotency + Observability Pack** — `OBS-01..06` (F-008/017/020/023/024/049)
  - Goal: idempotent mutating tools + production observability surface (migration 0026).
  - Success: (1) `/readyz` fails on unhealthy DB/listener/migration/scheduler, Compose healthcheck probes `/readyz`; (2) OTel metric path emits LLM/tool/MCP/DB/scheduler metrics, alert YAML + Grafana JSON validate in CI; (3) sidecar/trace cleanup works with retention + dry-run + active-conversation exclusion; (4) learning stores enforce a bounded retention cap.
- [ ] **Phase 40: Security & Supply-Chain Pack** — `SEC-01..07`, `QUAL`(decode-body unify) (F-019-sec/021/022/047/051/052)
  - Goal: close security + supply-chain findings; prove prompt-injection denial under production.
  - Success: (1) injected shell/file/network/MCP requests DENIED under `server_production` (regression suite); (2) secret-like values redacted before persistence, permissive CORS refused when auth disabled (except dev); (3) CI publishes SBOM, govulncheck blocks high-severity, all Actions SHA-pinned; (4) privileged JSON routes reject trailing/unknown-field/empty/wrong-content-type bodies.
- [ ] **Phase 41: Production Ops + Capability-Eval + Honest 10/10 Closeout** — `OPS-01..06`, `REL-01..03` (F-019/025/042/043 + evidence bar)
  - Goal: drilled backup/DR, ops-lifecycle hardening, capability-eval + load/chaos harness, honest-10/10 evidence bundle.
  - Success: (1) drilled DR restore with measured RPO/RTO (Neo4j-Community offline-dump caveat documented); (2) scheduler drain + systemd stop budget prove no partial-backup promotion on SIGTERM/kill; (3) load + chaos harness runs in CI (no-skip-as-green) + capability-eval pass/fail report; (4) ADRs + release-readiness checklist + production-readiness evidence bundle → defensible 10/10.

> **Host-constrained / deferred-tier flag:** Phase 41's load/chaos + DR drills and any gVisor/`server_production` live tiers may carry the same NO-SKIP-AS-GREEN "deferred verification tier" pattern as v1.0.0's 6 deferred items, pending an adequate host (DGX Spark). Decided at the Phase-41/closeout boundary.
>
> **Quality audit now fully phased:** the 4-slice maintainability audit (`docs/audit/quality/`) is taken into the roadmap as **Phase 31** (Wave 0 stabilization) + **Phase 32** (dead-code + shared-helper extraction), with correctness/security-overlapping items distributed into Phases 33 (env catalog), 34 (pool-leak/int32), 36 (Authula DSN), 38 (trust-norm/F-027), and 40 (decode-body/F-052). Nothing left as "refactor-on-touch only".
>
> **Spike (parallel, during Codex wait):** `.planning/spikes/` — agent-sandbox feasibility (single-host Docker-direct vs K8s) + per-identity multi-user isolation model for MCP / Garage / Skills, de-risking Phases 36–37.

### Phase Details

> Per-phase detail sections (GSD `roadmap.get-phase` format). Added incrementally as each phase is planned. Phases 32–41 retain the summary-checklist entry above until their `/gsd-plan-phase` run lands their detail section here.

#### Phase 31: Stabilization & CI Unblock

**Goal:** Clean working tree + green CI so every commit passes the pre-commit hooks and CI gates without the `--no-verify` workaround, and remediate the one critical CodeQL SSRF pulled forward from the security backlog.

**Requirements:** QUAL-01, F-015, SEC-08

**Success Criteria**:

1. No tracked production or test source file exceeds the 600-LOC cap (`cmd/aura/serve_webui.go` and `web/src/__tests__/LoginPage.test.tsx` split); `scripts/check-file-size.sh` is green whole-tree.
2. `internal/webui/dist` is rebuilt from `web/` and committed so the `web-dist-freshness` CI job is green (no drift between source and committed bundle).
3. Every Go build/test/vulnerability CI job sources its package list from `scripts/go_packages.sh` (no raw `./...`), and a CI lint rejects raw `go test ./...` / `govulncheck ./...` (F-015).
4. The frontend coverage gate is restored to ≥85% on all four vitest thresholds and the suite actually passes at that floor.
5. The critical CodeQL `go/request-forgery` (SSRF) finding at `internal/mcp/http_client.go` is remediated (outbound request target validated against an allow-list / SSRF guard) and the CodeQL alert resolves to fixed (SEC-08).

**Validation:** `scripts/check-file-size.sh` exit 0; `web-dist-freshness` job green; a CI grep-lint finds zero raw `./...` in `.github/workflows/`; `vitest run --coverage` meets the four 85% thresholds; a CodeQL re-scan shows the `go/request-forgery` alert closed.

**Plans:** 3/3 plans complete
**Wave 1**

- [x] 31-01-PLAN.md — Verify the QUAL-01 baseline green (C1 file-size, C2 dist freshness, C4 frontend coverage)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 31-02-PLAN.md — F-015 CI hygiene: 4 Go jobs source scripts/go_packages.sh + a no-raw-`./...` lint with negative self-test (C3)
- [x] 31-03-PLAN.md — SEC-08 SSRF guard: MCP-local guardEndpoint + enforce-only hardened transport, CodeQL go/request-forgery → fixed (C5)

#### Phase 32: Quality Cleanup — Dead Code + Shared Helpers

**Goal:** Kill cross-package duplication + dead code BEFORE feature phases build on them, so later work reuses clean shared packages.

**Requirements:** QUAL-02, QUAL-03, QUAL-05

**Success Criteria**:

1. Dead exports/placeholders removed (`assets.Status{Created,Embedding,Canceled}`, sidecar-only `AURA_MEMORY_EMBED_*` keys, `agui.indexByte`/`stringList`, redundant telebot blank import, redundant `RequestID` re-stamp), each confirmed via `deadcode`/`knip`/repo-wide `rg`.
2. Shared packages extracted — `internal/neostore`, `internal/envutil`, `internal/agentrender`, agent `CanonicalArgs`/`isTransientNetworkErr`, web single `getJSON`/shared `focusTrap` — each with a parity test.
3. Targeted test gaps closed (`web/throttle`, setup `InvalidateToken`-before-SSE ordering, Telegram `answersFromText` keyword fallback, `truncateTailBytes`, Authula `ensureAuthulaSearchPath` DSN parsing).

**Plans:** 10/10 plans complete

**Wave 1** *(QUAL-02 dead-code clean-slate)*

- [x] 32-01-PLAN.md — assets.Status{Created,Embedding,Canceled} keep/kill operator escalation (D-02/D-04)
- [x] 32-02-PLAN.md — Go stdlib swaps + keeps: RequestID load-bearing test, agui indexByte/stringList, truncateRunes decision
- [x] 32-03-PLAN.md — Go redundant-code removal: telebot blank import + discarded Build() restructure
- [x] 32-04-PLAN.md — AURA_MEMORY_EMBED_* full-stack removal (Go + web + i18n + compose/.env doc + dist)

**Wave 2** *(QUAL-03 test-first shared-helper extractions; blocked on Wave 1)*

- [x] 32-05-PLAN.md — leaf extractions: internal/neostore + internal/db numeric + internal/envutil (+ D-13 coverage-gate)
- [x] 32-06-PLAN.md — agent extractions: canonicaljson.CanonicalArgs + shared isTransientNetworkErr (asymmetric)
- [x] 32-07-PLAN.md — internal/agentrender render primitives (+ documented eval json.Number fix)
- [x] 32-08-PLAN.md — web dedup: single getJSON + canonical focusTrap + skeleton unification + dist rebuild

**Wave 3** *(QUAL-05 targeted test gaps; blocked on Wave 2)*

- [x] 32-09-PLAN.md — test gaps: web/throttle, setup SSE ordering, Authula ensureAuthulaSearchPath DSN
- [x] 32-10-PLAN.md — test gaps: Telegram answersFromText fallback, truncateTailBytes UTF-8, memory_integration CI verify+doc

#### Phase 33: Runtime Profiles + Config Validation

**Goal:** 4 validated profiles (`dev`/`local_trusted`/`single_user_hardened`/`server_production`) in `internal/config`; production fails fast on unsafe defaults; all hot-path `AURA_*` knobs catalogued.

**Requirements:** PROF-01, PROF-02, PROF-03, PROF-04, PROF-05, PROF-06, QUAL-04

**Success Criteria**:

1. `aura config validate --profile server_production` exits non-zero listing every unmet requirement.
2. Copying `.env.example`→`.env` keeps the destructive-shell gate active.
3. Invalid env fails-fast under production, warns under dev.
4. `dev`/`local_trusted` preserve today's full-host behavior unchanged.

**Plans:** 5/5 plans complete
**Wave 1**

- [x] 33-01-PLAN.md — Foundation: split Validate() out of config.go (LOC unblock) + RuntimeProfile enum + Config.Profile/ObjectStoreReplicationFactor/GarageRPCSecret fields (wave 1) → 33-01-SUMMARY.md
- [x] 33-02-PLAN.md — F-002/D-12 destructive-shell semantics flip (empty→defaults, only `off` disables) + truth table + .env.example (wave 1) → 33-02-SUMMARY.md

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 33-03-PLAN.md — KnobSpec registry (single source of truth, Tier A+B) + generic kind-driven reparse pass + rapid PBT invariants (wave 2) → 33-03-SUMMARY.md

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 33-04-PLAN.md — ValidateProfile aggregator + bespoke profile gates + profile-aware Validate() + boot warn-diagnostic (wave 3) → 33-04-SUMMARY.md

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 33-05-PLAN.md — `aura config validate [--profile] [--json]` CLI subcommand + exit-code/knob-name e2e test (wave 4) → 33-05-SUMMARY.md

#### Phase 34: Agent-Loop Correctness + Durable Ledger

**Goal:** Terminal-response exclusivity, atomic HITL resume/pause (single cross-store transaction), fenced sidecars, crash-orphan reconciliation.

**Requirements:** LOOP-01, LOOP-02, LOOP-03, LOOP-04, LOOP-05, LOOP-06, LOOP-07, LOOP-08, LOOP-09, LOOP-10, LOOP-11, QUAL-04

**Success Criteria**:

1. `text_response` + a mutating sibling never executes the sibling.
2. Duplicate single/batch resume → exactly one answer/pause; an append-failure leaves a repairable state.
3. Outside-root/traversal/symlink sidecar reads are rejected.
4. A mutating tool that panics post-side-effect still arms the completion gate.

**Plans:** 6/6 plans complete

Plans:
**Wave 1**

- [x] 34-01-PLAN.md — sqlc regen (MarkPausedStateResumed :execrows + ListSpilledSeqsForConversation) + ROADMAP D-07 goal reconciliation [wave 1]
- [x] 34-02-PLAN.md — terminal text_response exclusivity (LOOP-01) + mutating-panic regression test (LOOP-08) [wave 1]
- [x] 34-03-PLAN.md — send_file deterministic reject (LOOP-06), fs_write/fs_edit mode preservation (LOOP-07), boot pool-leak/double-Validate (QUAL-04) [wave 1]

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 34-04-PLAN.md — os.Root sidecar fence (LOOP-05), crash-orphan .content GC (LOOP-09), spilled-search assertion (LOOP-10) [wave 2]
- [x] 34-05-PLAN.md — HITL store tx-seams: askuser MarkResumed/Insert Tx + conversations AppendTurnTx + int32 guard (LOOP-02/03 seams, QUAL-04a) [wave 2] → 34-05-SUMMARY.md

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 34-06-PLAN.md — ResumeCommitter + atomic single/batch resume + atomic pause-flush + Stop goroutine-leak fix (LOOP-02/03/04/11) [wave 3] → 34-06-SUMMARY.md

#### Phase 35: ToolGateway + Policy Engine

**Goal:** One in-process policy decision on every tool call; fail-closed for mutating tools; durable reservation.

**Requirements:** GATE-01, GATE-02, GATE-03, GATE-04

**Success Criteria**:

1. No tool executes without a recorded policy decision.
2. A timing-out/crashing command hook denies under hardened/production.
3. A mutating tool is blocked when ledger reservation fails in production.
4. The gateway is a no-op (fail-open, host-direct) under dev/local_trusted.

**Plans:** 7/7 plans complete

**Wave 1** *(parallel — DB-free foundation)*

- [x] 35-01-PLAN.md — D-02 classifier + Mutating floor (skill/task/swarm_spawn) + boot-guard [GATE-01, GATE-03]
- [x] 35-02-PLAN.md — D-04 GATE-02 command-hook fail-closed verify + test strengthening [GATE-02]

**Wave 2** *(blocked on 35-01)*

- [x] 35-03-PLAN.md — Gateway Decide PEP + profile branch + 3-root injection + read-only decision-fact + approve routing (D-01e/D-03) [GATE-01, GATE-03]

**Wave 3** *(blocked on 35-03)*

- [x] 35-04-PLAN.md — Durable reservation + idempotency: :execrows + replay + Store.Reserve (D-01a/b/c) [GATE-03, GATE-04]

**Wave 4** *(blocked on 35-04)*

- [x] 35-05-PLAN.md — Crash-orphan reconciler: append-only end{indeterminate}, never re-invoke (D-01d) [GATE-03]

**Wave 5** *(gap closure — blocked on 35-03/35-04)*

- [x] 35-06-PLAN.md — Gap: interactive `approve` pause/resume UX — GatewayApprovals cross-turn ledger + shell_exec-style approval-required ToolResult surfaced through execTool (NO pre-dispatch intercept — the real call+args stay in history) + newGatewayResumeHook re-enters Decide (D-03 point 2); proven by TestGatewayApprovalRoundTrip + live db_integration — 2026-07-04. ⚠ code-review CR-01: the resume path is NOT consent-bound (dropped shell_exec's ApproveChallenge question-match) → confused-deputy; GATE-01 held open pending a follow-up gap-closure (35-REVIEW.md)

**Wave 6** *(gap closure — CR-01 consent-binding; standalone, the code it fixes is already shipped)*

- [x] 35-07-PLAN.md — Gap: ported shell_exec's ApproveChallenge challenge/question binding into GatewayApprovals + routeApprove records the server-generated question keyed on the authenticated (conv,tool,args_sha256) + newGatewayResumeHook verifies existence+question-match on the authenticated pending.ConversationID (CR-01 confused-deputy / informed-consent fix); deny-before-Consume reorder & refuse-record under server_production (WR-01); authenticated conversation id (WR-02); adversarial mismatched-question test (WR-03); folded single-fp (IN-02) & removed dead Peek (IN-01) — 2026-07-04. Unit -race (gateway/agent/cmd-aura/runner) + live db_integration reserve/idempotency tier green; gateway coverage 89.4% (>85%); the 6 prohibition files byte-unchanged; cooperative TestGatewayApprovalRoundTrip + TestAskUserOnlyPauseConstraint still green. CR-01 consent property holds end-to-end; GATE-01/GATE-02 flip pending phase re-verification + code-review [GATE-01]

#### Phase 36: Multi-User Identity Isolation + Authula Cutover

**Goal:** Owner-scope every user-facing store/API/job to the authenticated principal; cut over to Authula (no RBAC). Includes per-identity isolation for MCP config, Garage object-store, and skills dirs (see spike `.planning/spikes/`).

**Requirements:** MUSR-01, MUSR-02, MUSR-03, MUSR-04, MUSR-05, MUSR-06

**Success Criteria**:

1. Two-identity live E2E — B cannot list/get/delete/archive/resolve A's data (404/403); a B-created chat is owned by B and runs.
2. Session B cannot poll/kill session A's shell; jobs expire by TTL.
3. Conversation delete evicts all session tool state.
4. Authula is the default with provisioning + break-glass; no token in URLs.

#### Phase 37: Per-User Full-Capability Sandbox

**Goal:** Resolve F-001 — host shell/fs run inside a per-identity full-capability Docker sandbox under hardened/production; the host is never exposed.

**Requirements:** SBX-01, SBX-02, SBX-03, SBX-04, SBX-05

**Success Criteria**:

1. Under `server_production`, shell/fs target the per-identity sandbox and the real host filesystem is unreachable.
2. Docker-socket/`--privileged`/`--network host`/bind-mounts are unrepresentable (test-asserted).
3. Cross-identity leakage is impossible and the idle-TTL lifecycle works.
4. A configured egress allowlist cannot reach a disallowed host (default `--network none`).
5. An ADR records container-per-identity (K8s/gVisor-default → DGX) + a pre-merge concurrency benchmark on 32GB.

#### Phase 38: MCP Governance Hardening

**Goal:** One canonical transport classifier + explicit remote trust + bounded MCP lifecycle + audited CLI writes.

**Requirements:** MCPH-01, MCPH-02, MCPH-03, MCPH-04, MCPH-05, MCPH-06, MCPH-07, MCPH-08, MCPH-09

**Success Criteria**:

1. Mixed url+command / empty-remote-trust is blocked and never calls stdio open.
2. A hung mount drops within deadline; an oversized stdio frame aborts without large alloc; shutdown leaves no child processes.
3. CLI mutations append `mcp_audit` (or are production-disallowed); an empty trust body → 400.
4. A dead HTTP MCP endpoint reports OK=false.

#### Phase 39: Idempotency + Observability Pack

**Goal:** Idempotent mutating tools + a production observability surface (migration 0026).

**Requirements:** OBS-01, OBS-02, OBS-03, OBS-04, OBS-05, OBS-06

**Success Criteria**:

1. `/readyz` fails on unhealthy DB/listener/migration/scheduler; the Compose healthcheck probes `/readyz`.
2. The OTel metric path emits LLM/tool/MCP/DB/scheduler metrics; alert YAML + Grafana JSON validate in CI.
3. Sidecar/trace cleanup works with retention + dry-run + active-conversation exclusion.
4. Learning stores enforce a bounded retention cap.

#### Phase 40: Security & Supply-Chain Pack

**Goal:** Close security + supply-chain findings; prove prompt-injection denial under production.

**Requirements:** SEC-01, SEC-02, SEC-03, SEC-04, SEC-05, SEC-06, SEC-09

**Success Criteria**:

1. Injected shell/file/network/MCP requests are DENIED under `server_production` (regression suite).
2. Secret-like values are redacted before persistence; permissive CORS is refused when auth is disabled (except dev).
3. CI publishes an SBOM, `govulncheck` blocks high-severity, all Actions are SHA-pinned.
4. Privileged JSON routes reject trailing/unknown-field/empty/wrong-content-type bodies.
5. The high CodeQL `go/weak-sensitive-data-hashing` finding at `internal/agui/recovery_hash.go` is remediated with a strong salted KDF and the alert resolves (SEC-09).

#### Phase 41: Production Ops + Capability-Eval + Honest 10/10 Closeout

**Goal:** Drilled backup/DR, ops-lifecycle hardening, capability-eval + load/chaos harness, honest-10/10 evidence bundle.

**Requirements:** OPS-01, OPS-02, OPS-03, OPS-04, OPS-05, OPS-06, REL-01, REL-02, REL-03

**Success Criteria**:

1. A drilled DR restore with measured RPO/RTO (Neo4j-Community offline-dump caveat documented).
2. Scheduler drain + systemd stop budget prove no partial-backup promotion on SIGTERM/kill.
3. A load + chaos harness runs in CI (no-skip-as-green) + a capability-eval pass/fail report.
4. ADRs + a release-readiness checklist + a production-readiness evidence bundle → a defensible 10/10.

## Progress

| Milestone | Phases | Plans | Status | Completed |
| --------- | ------ | ----- | ------ | --------- |
| v0.0.0 Substrate | 0–21 (24 phases) | 144/144 | ✅ Shipped | 2026-06-15 |
| v1.0.0 Aura Deep Search Web Cockpit | 22–30 (9 phases) | 45/45 | ✅ Shipped | 2026-06-29 |
| v2.0.0 Industrial Hardening & Multi-User Production | 31–41 (11 phases) | 0/— | 🔨 In planning | — |
