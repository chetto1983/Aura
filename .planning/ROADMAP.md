# Roadmap: Aura

## Milestones

- ✅ **v0.0.0 Substrate** — Phases 0–21 (shipped 2026-06-15) — full details in [`milestones/v0.0.0-ROADMAP.md`](milestones/v0.0.0-ROADMAP.md)
- ✅ **v1.0.0 Aura Deep Search Web Cockpit** — Phases 22–30 (shipped 2026-06-29) — full details in [`milestones/v1.0.0-ROADMAP.md`](milestones/v1.0.0-ROADMAP.md)
- 🔨 **v2.0.0 Industrial Hardening & Multi-User Production** — Phases 31–42 (in planning) — close the 51-finding security audit + the ~64-finding quality audit to an honest 10/10 via stabilization/cleanup + per-user full-capability sandbox + multi-user identity isolation + ToolGateway + observability/security/ops industrialization

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

### 🔨 v2.0.0 Industrial Hardening & Multi-User Production (Phases 31–42) — IN PLANNING

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
- [x] **Phase 36: Multi-User Identity Isolation + Authula Cutover** — `MUSR-01..06`, `QUAL`(Authula DSN test) (F-012/028/032/039/050)
  - Goal: owner-scope every user-facing store/API/job to the authenticated principal; cut over to Authula (no RBAC). Includes per-identity isolation for MCP config, Garage object-store, and skills dirs (see spike `.planning/spikes/`).
  - Success: (1) two-identity live E2E — B cannot list/get/delete/archive/resolve A's data (404/403), B-created chat owned by B and runs; (2) session B cannot poll/kill session A's shell, jobs expire by TTL; (3) conversation delete evicts all session tool state; (4) Authula default with provisioning + break-glass, no token in URLs.
- [x] **Phase 37: Per-User Full-Capability Sandbox** — `SBX-01..05` (F-001 sandbox, F-036) (executed; 37-10 closed the SBX-04 composition-root BLOCKER; all 5 SC mechanism-verified; **LIVE-VERIFIED on native-Linux dockerd 2026-07-08 (casaserver)**: SC#1/SC#3 full usersandbox docker_integration suite `ok 79s`, SC#4 egress DROP proven both backend-floor (`TestEgress_FloorDropsInternal`) AND composition-root (`TestBuildSandboxRouter_LaunchesEgressFloor`); operator force-close 2026-07-08. Only genuinely-blocked tiers remain: D-14 32GB soak (impossible on 8GB), gVisor runsc (needs daemon restart), native `-race` (no gcc; green on WSL), FQDN-allowlist image — tracked as REL-03 must-runs) (completed 2026-07-08)
  - Goal: resolve F-001 — host shell/fs run inside a per-identity full-capability Docker sandbox under hardened/production; host never exposed.
  - Success: (1) under `server_production` shell/fs target the per-identity sandbox, real host filesystem unreachable; (2) Docker-socket/`--privileged`/`--network host`/bind-mounts unrepresentable (test-asserted); (3) cross-identity leakage impossible + idle-TTL lifecycle works; (4) configured egress allowlist cannot reach a disallowed host (default egress = full-internet-minus-internal per D-06, not `--network none`); (5) ADR records container-per-identity (K8s/gVisor-default → DGX) + pre-merge concurrency benchmark on 32GB.
- [X] **Phase 37A: Web Artifact Delivery Lane** — `WEBART-01..04` (product gap; depends on Phase 37 / 37-07)
  - Goal: agent-generated files (`send_file`) reach the web cockpit as an authenticated same-origin download (Garage-backed identity-scoped asset), never a raw container/host path.
  - Success: (1) `send_file` stores bytes in the identity's Garage store + creates a thread-scoped owned `assets.Asset`, and the `aura.artifact` event carries `asset_id`/`filename`/`size_bytes`/`mime_type`; (2) `GET /api/assets/{id}/download` streams from Garage with `Content-Disposition: attachment`, ownership-checked, `RequireAuth`-gated, non-owner → 404/403; (3) web chat consumes `aura.artifact` and renders a download button, no raw path in the browser; (4) Telegram delivery unregressed, CLI/no-identity degrades to today's host-path behavior.
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
- [ ] **Phase 42: Industrial Conversation Compaction** — `IC-01..14`
  - Goal: provider-portable context lifecycle with semantic-first compaction, immutable evidence, branch-aware recovery, typed artifacts, separated durable memory, operations surfaces, and measured rollout.
  - Success: exact fail-closed budgets; L2.4-before-L2.5; durable claim/CAS checkpoints and bounded recovery; safe typed summaries/artifacts/memory; all surfaces; 500+200 evaluation corpus and staged rollback-gated rollout.

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

**Plans:** 17/18 plans executed

**Wave 1** *(foundation — parallel)*

- [x] 36-01-PLAN.md — Break-glass CLI (`aura identity recover`) + local admin-cap seed (MUSR-06 shipped first)
- [x] 36-02-PLAN.md — Isolation schema migrations (paused_states.identity_id, saga journal, soft-delete, object-store key table, audit indexes) + AURA_MUSR_ISOLATION flag + RBAC-03 amendment note
- [x] 36-03-PLAN.md — Background jobs owner-binding + 1h TTL reaper (MUSR-03/04)
- [x] 36-07-PLAN.md — Per-identity MCP config + skills/pyscripts filesystem rooting (D-20/D-21)

**Wave 2** *(kernel + isolation planes — blocked on Wave 1)*

- [x] 36-04-PLAN.md — WithIdentityTx RLS carrier + ENABLE-RLS migration + owner-scoped conversations/approvals (404/403) + MUSR-02 owner + RLS backstop
- [x] 36-05-PLAN.md — Documents plane fail-closed (six flag-gated scoped EXISTS queries + HAS_DOCUMENT ingest edge; spike-085 fix) — documents plane of MUSR-01 closed; live neo4j_integration tier pending WSL/CI
- [x] 36-06-PLAN.md — Garage Admin API v2 client + bucket-per-identity + per-identity credential resolver — admin API enabled internal-only; stdlib garageadmin client (idempotent create/delete legs); AES-GCM encrypt-at-rest resolver, fail-closed miss; live garage_integration + db_integration tiers pending WSL/CI
- [x] 36-10-PLAN.md — Frontend capability-gating + admin grant/revoke control + admin audit UI + audit API + dist rebuild (D-03/D-26/D-28)

**Wave 3** *(sagas + lifecycle — blocked on Wave 2)*

- [x] 36-08-PLAN.md — Provisioning saga (Garage/FS legs + journal) + first-login (D-15) + de-provisioning saga + LinkUser
- [x] 36-09-PLAN.md — Runner conversation-delete lifecycle + composite (identity,session) keying (MUSR-05/D-23)

**Wave 4** *(routing + token gate — blocked on Wave 3)*

- [x] 36-11-PLAN.md — Telegram multi-user routing + no-long-lived-token-in-URL static gate (MUSR-06) — per-user turn identityctx scoping at the single startTurn choke point (fresh/async-doc/HITL-resume all covered); reject-unlinked→web-linking documented; `scripts/check-no-url-tokens.sh` static gate (self-tested, CI-wireable, ≤1h ?start=/setup ?token= carve-out); -race + two-identity live E2E pending WSL/CI

**Wave 5** *(acceptance gate — blocked on Wave 4)*

- [x] 36-12-PLAN.md — Documents backfill + flag-flip rollout runbook + CI (Garage+Authula) + two-identity live cross-deny E2E (D-29 acceptance gate) — idempotent Op1/Op2 owner-edge backfill (operator=UUID …001); deploy(off)→backfill→verify→flip(on) reversible runbook; musr-e2e CI job (composed DSNs + AURA_GARAGE_ADMIN_* + AURA_MUSR_ISOLATION=true, five tags, no-skip-as-green); five-tag E2E (HTTP 404/store-403/RLS/approvals/docs/Garage+resolver/MUSR-02/MUSR-06); + fixed the D-09 branch-route isolation hole; MUSR-01/02/06 [x]; live tiers compile-clean, pending WSL/CI

**Gap Closure** *(VERIFICATION gaps_found + REVIEW 1 Critical/3 High — planned 2026-07-06; 6 plans, 3 waves)*

- [x] 36-13-PLAN.md — CI correctness + static gates: version-anchor the migration-0026 reversibility test (VERIF-1) + wire check-no-url-tokens.sh into CI (VERIF-6). Wave 1. — done 2026-07-06: positioned the ±1 straddle at v26 (`stepDownToV26 := 26 - head` reverses 0027..HEAD, then -1/+1 isolates 0026's OWN down/up so caps drop then restore while `*` survives); LIVE db_integration `TestMigration0026LocalAdminCapsRoundTrip` PASS (1.04s, head≥32) — the confirmed-broken run-28753262579 test now green; added the blocking "No long-lived token in URLs (MUSR-06)" step after the file-size cap in the build-and-lint job (gate + --self-test exit 0, ci.yml valid YAML). Commits `653dfdd3` (test) + `9796c326` (chore). Test-only + workflow-only; no production code, no new deps.
- [x] 36-14-PLAN.md — Daemon provisioning/de-provisioning wiring + migration 0033 (scheduler kind CHECK admits 'identity_purge') + deactivation auth-gate (VERIF-3/HI-01 + HI-02). Wave 1.
- [x] 36-15-PLAN.md — Per-identity object-store consumption on the asset path (VERIF-4/HI-01). Wave 2 (depends 36-14). — done 2026-07-06: routed the asset Service (Presign/Finalize/IngestTelegramFile/Delete/hashAndSniff) + the audio/document/image processors through `objectstore.IdentityStore.Resolve(ctx)` via a new `resolveObjects` seam with explicit local/`ErrNoRows`→shared fallback (never a foreign bucket, F-007); `buildAssetService` wires the resolver + a caching per-identity `S3Store` factory (sync.Map/access-key) when pool+`AURA_AUTHULA_SECRET` present — `NewIdentityStore` now has a non-test asset-path consumer. Commits `406a1e75`+`248e5676`. `go build`/`vet` clean; tagged compile green; untagged assets+cmd/aura green; WSL `-race` exit 0. DEVIATIONS: (Rule 1) processor passes the REAL shared bucket to `resolveObjects` not `asset.ObjectBucket` (the literal pseudocode would 403 every per-identity read via the global key); (Rule 2) service stamps the owner into ctx (background durable-worker path carries no principal); (Rule 2) added untagged `object_resolver_unit_test.go`. Live `garage_integration && db_integration` cross-deny test CI-gated at 36-18 (admin :3903 unreachable, curl exit 7). MUSR-01 stays `[ ]` (phase-spanning; closes at 36-18).
- [x] 36-16-PLAN.md — Documents default-closed + config-validate + local-fallback (CR-01/VERIF-5 + ME-01/LO-03; ME-02/LO-01 recorded). Wave 2 (depends 36-14).
- [x] 36-17-PLAN.md — Telegram fail-closed scoping + shell admin-cap wiring + blank-principal regression (HI-03 + VERIF-7 + LO-02). Wave 2 (depends 36-14).
- [x] 36-18-PLAN.md — Push + full CI matrix green + live-stack acceptance + AURA_MUSR_ISOLATION rollout decision (VERIF-2 + human_verification #1/#2). Wave 3 terminal, autonomous:false. **DONE 2026-07-06:** CI run 28799334452 = 20/20 green on HEAD 207200c8 (musr-e2e ran live 268s -race); rollout activated (0-doc backfill → flag true).

#### Phase 37: Per-User Full-Capability Sandbox

**Goal:** Resolve F-001 — host shell/fs run inside a per-identity full-capability Docker sandbox under hardened/production; the host is never exposed.

**Requirements:** SBX-01, SBX-02, SBX-03, SBX-04, SBX-05

**Success Criteria**:

1. Under `server_production`, shell/fs target the per-identity sandbox and the real host filesystem is unreachable.
2. Docker-socket/`--privileged`/`--network host`/bind-mounts are unrepresentable (test-asserted).
3. Cross-identity leakage is impossible and the idle-TTL lifecycle works.
4. A configured egress allowlist cannot reach a disallowed host; the default egress posture is full public internet minus the tenancy boundary (DROP RFC1918 + `169.254.169.254` cloud-metadata + the shared-services Docker bridge), not `--network none` (SBX-04 amended per D-06).
5. An ADR records container-per-identity (K8s/gVisor-default → DGX) + a pre-merge concurrency benchmark on 32GB.

**Plans:** 10/10 plans complete

**Wave 1**

- [x] 37-01-PLAN.md — Foundation & Gate-1: SBX-04 egress amendment (D-06) + fat box image (D-12/D-13) + moby dep promotion
- [x] 37-02-PLAN.md — SBX-02 unrepresentability: SandboxSpec + translator + Backend E2B contract + docker_integration skip-helper
- [x] 37-03-PLAN.md — Idle-suspend reaper scaffold: sandbox_reap handler + migration 0034 (exact identity_purge/0033 template)

**Wave 2** *(blocked on 37-01, 37-02)*

- [x] 37-04-PLAN.md — DockerBackend over moby v0.4.1 + named-volume lifecycle + materialize + cross-identity deny (SBX-03)

**Wave 3** *(blocked on 37-04 / 37-03)*

- [x] 37-05-PLAN.md — SandboxRouter (Strict no-op + fail-CLOSED) + reap impl + reaper serve-wiring (SBX-01/SBX-03/GATE-01)
- [x] 37-06-PLAN.md — Egress sidecar: filter-table floor + OpenSandbox FQDN allowlist + native-Linux enforcement (SBX-04)

**Wave 4** *(blocked on 37-05 / 37-06)*

- [x] 37-07-PLAN.md — Route shell_exec/fs_read/fs_write/skill into the box, fail-CLOSED (not web_*) (SBX-01)
- [x] 37-08-PLAN.md — SBX-05 ADR + compose daemon-Docker access (Open Q5) + D-14 32GB concurrency benchmark

**Wave 5** *(blocked on 37-07)*

- [x] 37-09-PLAN.md — shell_bg background routing into the box (Open Q4, most invasive)

**Wave 6** *(gap closure — verification BLOCKER)*

- [x] 37-10-PLAN.md — Wire the always-on egress floor into `buildSandboxRouter` (SBX-04): `AURA_SANDBOX_EGRESS_IMAGE` → `cfg.Sandbox.EgressImage` → `usersandbox.WithEgress`, non-empty default (floor-on, SC#4), fail-CLOSED when the image is absent; docker-free wiring guard + composition-root live-DROP re-test (WSL/CI)

#### Phase 37A: Web Artifact Delivery Lane

**Goal:** Agent-generated files delivered by `send_file` reach the web cockpit as an authenticated same-origin download backed by a Garage object + identity-scoped `assets.Asset` — never a raw container/host path. Closes the gap where the web chat drops `aura.artifact` events (Telegram already consumes them via `internal/channels/telegram/artifact.go`).

**Requirements:** WEBART-01, WEBART-02, WEBART-03, WEBART-04

**Depends on:** Phase 37 (plan 37-07 stages box→host artifacts via `CopyArtifactsOut`; the resolved host path is this lane's Garage-ingest input — sequence after 37-07).

**Success Criteria**:

1. `send_file` stores bytes in the authenticated identity's Garage store (per-identity `AssetKey`) and creates a thread-scoped owned `assets.Asset` (mirrors `assets.Service.IngestTelegramFile`); no raw path is the delivery handle.
2. The `aura.artifact` event carries `asset_id` + `filename` + `size_bytes` + `mime_type` on the existing `/agent/run` SSE stream; Telegram delivery is unregressed.
3. `GET /api/assets/{id}/download` streams from Garage with `Content-Disposition: attachment`, enforces identity ownership (`GetForIdentity`), inherits `RequireAuth`; a non-owner → 404/403; no unauthenticated download surface added.
4. The web chat consumes `aura.artifact` in `sseAdapter.ts` and renders an authenticated download button (`/api/assets/{id}/download`); the browser never receives a raw container/host path. CLI / no-identity degrades to today's host-path behavior.

**Design forks for discuss-phase:** (a) `assets.source_kind` CHECK allows only `web|telegram|cli` → add an `agent` value (migration) or reuse `cli`; (b) thread-id reaches the tool ctx only via `agent.SwarmContext(ctx).ConvID` (a smell for a non-swarm concern) → consider a dedicated `threadctx`; (c) download-button UI: reuse the existing `local_artifact` display card (already renders + carries `size_bytes`) vs a new dedicated file part on the delivery event.

**Plans:** 4/4 plans executed

**Wave 1**

- [x] 37A-01-PLAN.md — Backend foundation: migration 0035 (+agent source_kind) + SourceAgent/AgentIngestRequest, IngestAgentFile + OpenForIdentity service methods, RFC-6266 contentDisposition helper

**Wave 2** *(parallel — both depend on 37A-01, zero file overlap)*

- [x] 37A-02-PLAN.md — Ingest lane: AssetDeliverer seam + ctx-aware emitDelivery descriptor enrichment (both tails) + degrade matrix + composition-root wiring + Telegram non-regression
- [x] 37A-03-PLAN.md — Download route: GET /api/assets/{id}/download (owner-scoped stream, attachment + octet-stream + nosniff, 404 non-owner, ctx-scoped io.Copy + goleak)

**Wave 3** *(depends on 37A-02 + 37A-03)*

- [x] 37A-04-PLAN.md — Web consume: sseAdapter aura.artifact → local_artifact card by tool_call_id + LocalArtifactDisplay download button + internal/webui/dist rebuild

#### Phase 37B: Web Artifact Sidebar (INSERTED)

**Goal:** Gli artifact prodotti in un thread sono aggregati in un pannello laterale destro "Artefatti" nel cockpit web (parità con Telegram + l'UI di Claude): elenco dei file del thread con download per-file e "Scarica tutto", anteprime, e nessun path host/container mai esposto al browser. Costruito sopra l'`asset_id` + `GET /api/assets/{id}/download` di Phase 37A. Prima delle tre aree di parità cockpit-web emerse dall'audit voice/artifact/skill (le altre due — voice web TTS/STT cloud-only, composer skill-picker "/" — sono Phase 37C/37D).

**Requirements:** WEBART-05, WEBART-06, WEBART-07, WEBART-08

**Depends on:** Phase 37A (WEBART-01..04 forniscono l'`asset_id` sull'evento `aura.artifact` + l'endpoint autenticato `GET /api/assets/{id}/download` identity-scoped; questo pannello aggrega quegli `asset_id` e ne fa lo streaming — nessuna nuova sorgente di verità).

**Success Criteria**:

1. Un pannello laterale destro "Artefatti" nella shell chat (`AppShell` `ResizablePanelGroup`) elenca ogni asset consegnato via `aura.artifact` nel thread attivo (filename + size + mime + icona), ordinato per recenza; empty-state graceful; su mobile/tablet collassa in drawer/overlay come la navigation, senza rompere il layout.
2. Ogni riga ha un download che colpisce `GET /api/assets/{id}/download` (identity-scoped, `Content-Disposition: attachment`); un "Scarica tutto" scarica in sequenza gli asset del thread. Nessun path host/container raggiunge mai il browser.
3. Il pannello riusa `GET /api/assets?thread_id=` + gli eventi live `aura.artifact` da `sseAdapter` (merge, non un nuovo store); l'ownership è garantita da `GetForIdentity` — un non-owner → 404/403, nessuna superficie non autenticata aggiunta.
4. Parità non regressiva: la chip inline `local_artifact` continua a renderizzare; CLI / no-identity degrada al comportamento host-path odierno. Test: unit React (render pannello + download-all) + e2e Playwright (artifact compare nel pannello + download) + coverage web ≥85%.

**Design forks for discuss-phase:** (a) sorgente del pannello — solo `GET /api/assets?thread_id=` (refetch) vs. merge con lo stream live `aura.artifact` (immediato ma stateful); (b) posizione — terzo `ResizablePanel` persistente a destra su desktop che diventa drawer su mobile, vs. overlay on-demand toggolato dall'header; (c) "Scarica tutto" — N richieste sequenziali client-side (preferito, YAGNI) vs. un nuovo endpoint zip server-side (evitare finché non c'è evidenza di necessità).

**PRD-first:** richiede PRD-amendment prima del codice (nuovo requirement group WEBART-05..08 + la superficie sidebar non è documentata nel PRD) — vedi §Q&A revision protocol.

**Plans:** 8/8 plans complete

**Wave 1** *(PRD-first gate — blocks all code, D-19)*

- [x] 37B-01-PLAN.md — PRD-amendment: WEBART-05..08 group + Artefatti sidebar surface + preview deps (docx-preview Apache-2.0, SheetJS CE via CDN) + null-origin HTML sandbox policy + D-14/D-15 persistence

**Wave 2** *(parallel — both depend on 37B-01, zero file overlap)*

- [x] 37B-02-PLAN.md — Supply-chain: legitimacy checkpoint + install docx-preview/jszip + xlsx from CDN (CVE-safe) + widen Asset.source_kind union to add 'agent'
- [x] 37B-03-PLAN.md — Pure foundation: artifactMeta (previewKind SVG-gated + category label/icon + shared formatSize) + downloadAll (sequential/throttled) + artifacts.* i18n (en+it) + Stryker targets

**Wave 3** *(parallel — depend on foundation, zero file overlap)*

- [x] 37B-04-PLAN.md — Preview: useBlobPreview + PreviewModal (Radix 90vw/90vh dispatch) + 6 lazy renderers (image/pdf/text/html/docx/xlsx; null-origin HTML sandbox; SVG/pptx download-only)
- [x] 37B-05-PLAN.md — Live-merge producer + D-15 fix: onArtifact signal through streamSSE pump + split-fold rehydration (agent→assistant turns) + assistant-side download chip

**Wave 4** *(depends on 37B-02/03/04)*

- [x] 37B-06-PLAN.md — Panel: useThreadArtifacts (agent-filtered newest-first) + ArtifactRow (download + preview + degraded) + ArtifactsPanel (header, Scarica tutto, empty-state, lazy PreviewModal)

**Wave 5** *(depends on 37B-05/06)*

- [x] 37B-07-PLAN.md — AppShell integration: third ResizablePanel (dynamic panelIds, no key bump) + header toggle + mobile right Drawer + onArtifact handler (invalidate + one-time auto-open)

**Wave 6** *(depends on 37B-07)*

- [x] 37B-08-PLAN.md — Gate: Playwright e2e (artifact in panel + download) + full coverage ≥85% + Stryker ≥70% on pure modules + internal/webui/dist rebuild

#### Phase 37C: Web Voice Lane (INSERTED)

**Goal:** Parità voce con Telegram nel cockpit web: (a) **output vocale** — la risposta dell'agente è riproducibile come audio (pulsante speaker per messaggio + preferenza "voice mode" auto-speak); (b) **input vocale** — il Mic del Composer diventa dettatura in-place (transcript nel box input, editabile, non un attachment). Cloud-only via OpenRouter (`AURA_STT_CLOUD_MODEL`/`AURA_TTS_MODEL`), nessun sidecar locale (vincolo RAM). Riusa `multimodal.TTSClient`/`STTClient` già completi. Seconda delle aree di parità cockpit-web dall'audit voice/artifact/skill.

**Requirements:** WEBVOICE-01, WEBVOICE-02, WEBVOICE-03, WEBVOICE-04

**Depends on:** Phase 36 (identity/auth), il client `internal/multimodal` (già shippato), la asset/STT pipeline (già shippata).

**Success Criteria**:

1. Ogni messaggio assistant ha un pulsante speaker che sintetizza il testo via un nuovo endpoint autenticato `POST /api/tts` (identity-scoped, streaming opus/mp3) sopra `multimodal.TTSClient`, riprodotto da un `<audio>` in-page; una preferenza per-conversazione "voice mode" abilita l'auto-speak (parità `ShouldSpeak`).
2. Il Mic del Composer produce una **dettatura**: registra → trascrive via l'STT esistente → inserisce il transcript nel box input (editabile prima dell'invio), invece di allegare una nota vocale. Fallback: su errore, ripiega sul comportamento attachment odierno.
3. Cloud-only: TTS/STT girano su OpenRouter senza sidecar locale; con i model cloud non configurati la UI degrada (speaker nascosto / mic in modalità attachment) senza errori.
4. Nessuna regressione dell'attachment audio; `RequireAuth` sull'endpoint TTS; unit React (speaker + dettatura) + e2e; coverage ≥85% web / owned-surface Go.

**Design forks for discuss-phase:** (a) trasporto TTS — nuovo `POST /api/tts` che risponde audio vs. evento SSE `aura.audio` sul run stream; (b) auto-speak — pref per-conversazione (settings) vs. toggle effimero nell'header chat; (c) dettatura — Web Speech API browser (gratis, qualità variabile) vs. il pipeline STT server (coerente con Telegram, preferito).

**PRD-first:** richiede PRD-amendment (WEBVOICE-01..04 + superficie voce web non documentata).

**Plans:** 6/6 plans complete

Plans:
**Wave 1**

- [x] 37C-01-PLAN.md — [W1] PRD-amendment (WEBVOICE-01..04 + web voice surface: routes, adapters, mp3-vs-opus, AURA_TTS_MAX_CHARS) — blocking pre-code gate (D-14)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 37C-02-PLAN.md — [W2] Backend foundation: AURA_TTS_MAX_CHARS knob (default 4096) + exported ;codecs=-safe assets.AudioFormat

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 37C-03-PLAN.md — [W3] Voice API: POST /api/tts + POST /api/stt + GET /api/voice/capabilities handlers + SetVoice + mp3 web TTSClient wiring (daemon-free tests)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 37C-04-PLAN.md — [W4] Web output lane: useVoiceCapabilities + speechAdapter + VoiceModeProvider/auto-speak + caps-gated Speak/StopSpeaking control (a699fc38c/833d72a74/0244648ea, 42 web tests, tsc+eslint+i18n-parity clean)

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 37C-05-PLAN.md — [W5] Web input lane: dictationAdapter (onSpeech isFinal) + Composer dictation-primary (attachment fallback kept) + runtime adapters wiring

**Wave 6** *(blocked on Wave 5 completion)*

- [x] 37C-06-PLAN.md — [W6] Terminal gate: Playwright voice.spec.ts (speaker+dictation+degrade) + coverage/Stryker ≥85%/≥70% + internal/webui/dist rebuild

#### Phase 37D: Composer Skill & Command Picker (INSERTED)

**Goal:** Un menu slash "/" nel Composer (parità col picker skill/comandi di Claude) che elenca le skill disponibili all'identità + comandi rapidi, filtrabili da tastiera, per invocare/allegare una skill inline nel turn — invece di gestirle solo nella board Governance admin. Terza area di parità cockpit-web dall'audit.

**Requirements:** WEBSKILL-01, WEBSKILL-02, WEBSKILL-03

**Depends on:** Phase 28 (governance skills API), il registry skill (già shippato).

**Success Criteria**:

1. Digitando "/" a inizio riga nel Composer si apre un menu che elenca le skill disponibili all'identità autenticata (via la governance skills API, identity-scoped), con filtro incrementale (↑/↓/Enter/Esc) e descrizione per riga.
2. Selezionando una voce, la skill è iniettata nel turn secondo il contratto runtime esistente; nessuna nuova sorgente di verità sulle skill (riusa l'API governance).
3. Accessibile (ARIA combobox/listbox), preserva paste/drop/Enter-invio del Composer, degrada a no-op se la skills API è vuota/non raggiungibile; unit + e2e; coverage ≥85%.

**Design forks for discuss-phase:** (a) sorgente lista — `GET /api/governance/skills` (esiste) vs. un endpoint per-identity più snello; (b) semantica selezione — allega la skill come contesto del turn vs. la invoca come tool esplicito; (c) ambito — solo skill vs. skill + comandi rapidi (new-chat, clear).

**PRD-first:** richiede PRD-amendment (WEBSKILL-01..03).

**Plans:** 5/5 plans complete

Plans:
**Wave 1**

- [x] 37D-01-PLAN.md — [W1] PRD-amendment gate (#81): WEBSKILL-01..03 + GET /api/composer/skills + aura.skill envelope + Mechanism A (blocks all code)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 37D-02-PLAN.md — [W2] Backend: GET /api/composer/skills (RequireAuth-only, global snapshot) + pinned-skill wire path (aura.skill decode + Mechanism-A prepend + SkillBody seam + divergence guard)
- [x] 37D-03-PLAN.md — [W2] Frontend picker foundation: skills client + pure combobox model + ARIA listbox (SkillPicker) + removable pill (SkillPill) + en/it i18n

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 37D-04-PLAN.md — [W3] Composer integration: / trigger + combobox keys/ARIA + pinned pill + quick actions (add-files/new-chat/clear) + skill carried on send

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 37D-05-PLAN.md — [W4] Terminal gate: Playwright composer-skills e2e (8/8, aura.skill wire proof + new-chat/clear) + internal/webui/dist rebuild + coverage ≥85 (web vitest 92.6% + owned-surface Go 85.5%, internal/agui 86.8%)

#### Phase 37E: Composer Model & Reasoning-Effort Selector (INSERTED)

**Goal:** Un selettore di reasoning-effort ("thinking") per-turno nel Composer (parità col controllo `off · low · mid · high · extra · max` di Claude più l'`auto` adattivo di Aura), che lascia all'utente scegliere quanto "pensa" l'agente sul prossimo turno. **Effort-only (D-01): il Composer NON espone un picker di modello** — la selezione del modello resta operator-scoped nella pagina Settings (`AURA_LLM_MODEL`, già shippata). I livelli mostrati sono **auto-detettati per modello attivo** (D-13), mai una lista hard-coded né un placebo (D-12); la scelta è persistita per-conversazione (`aura.conversations.metadata` jsonb, nessuna migration — D-06) e ripristinata alla riapertura. Quarta area di parità cockpit-web dall'audit voice/artifact/skill; scope ridotto da "modello + effort" a effort-only via l'amendment PRD-first di Wave 1 (37E-01, D-11).

**Requirements:** WEBMODEL-01, WEBMODEL-02, WEBMODEL-03

**Plans:** 7/7 plans complete

Plans:
**Wave 1**

- [x] 37E-01-PLAN.md — Wave 1: PRD-amendment gate (drop model-selector, 7-level capability-gated, delete stale "no Max", llama.cpp + capability requirements)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 37E-02-PLAN.md — Wave 2: llm effort engine (ReasoningEffortMax, neutral ReasoningTarget, AURA_LLM_PROVIDER, llama.cpp wire branch)
- [x] 37E-03-PLAN.md — Wave 2: per-conversation persistence (metadata jsonb, no migration; owner-scoped update + read projection)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 37E-04-PLAN.md — Wave 3: override seam (ApplyFixedReasoning, BuildWithReasoningOverride, ctx WithReasoningOverride, skip-when-fixed)
- [x] 37E-05-PLAN.md — Wave 3: capability-detection subsystem (OpenRouter /models client + TTL cache, llama.cpp /props source, fixtures)

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 37E-06-PLAN.md — Wave 4: two-stage /agent/run validation + GET /api/composer/reasoning-capabilities + composition wiring

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 37E-07-PLAN.md — Wave 5: Composer capability-aware selector UI + hooks + en/it i18n + vitest/Playwright e2e

**Depends on:** Phase 37D (il Composer e il suo contratto di invio del turn), il contratto reasoning provider-neutral di `internal/llm` (`ReasoningConfig`/`ReasoningEffort`), la sorgente di capability per-modello (OpenRouter `/models` `supported_efforts` per il cloud, llama.cpp `/props` + ops-contract per il locale), le settings (default effort `auto` odierno).

**Success Criteria**:

1. Il Composer espone un selettore reasoning-effort i cui livelli sono **auto-detettati per modello attivo** (nessuna lista hard-coded, nessun placebo — D-12/D-13), dal set `auto·off·low·mid·high·extra·max`; la scelta è persistita per-conversazione (`conversations.metadata` jsonb, no migration — D-06) e ripristinata alla riapertura del thread.
2. `/agent/run` accetta un override simbolico opzionale `effort`, validato server-side in **due stadi** (enum sintattico + capability contro i `supported_efforts` del modello attivo): un valore non-enum o non-advertised → 400; assente/`auto` → il default adattivo odierno, nessuna regressione. L'effort ha effetto su **OpenRouter E llama.cpp** (D-08).
3. Nessun bypass della governance — il client invia un **simbolo**, mai un `ReasoningConfig`/budget grezzo; il server possiede la mappa simbolo→config e il capability-gate. Ogni livello mappa su un **vero knob wire spike-validato** (D-12), nessun campo fabbricato. Unit + e2e; coverage ≥85%. Caveat di fedeltà onesto (D-09): supporto advertised ≠ gradazione garantita — la fedeltà graduata è reale sui modelli locali budget-capable, mentre il DeepSeek-V4-Flash di default collassa low..max a on/off.

**Design forks (RESOLVED da 37E-CONTEXT + 37E-01 amendment):** (a) **selezione modello** — FUORI SCOPE (effort-only, D-01): niente endpoint di lista-modelli, niente override di modello; (b) **store per-conversazione** — RISOLTO a `aura.conversations.metadata` jsonb (la colonna esiste già in migration 0005, **nessuna migration** — D-06); (c) **semantica effort** — RISOLTA alla scala a 7 livelli capability-gated `auto·off·low·mid·high·extra·max` mappata su knob wire reali per entrambi i backend (OpenRouter `reasoning:{effort}` immutato; llama.cpp `chat_template_kwargs:{enable_thinking:false}` / `thinking_budget_tokens:512/2048/8192/16384/-1`, spike 095), con auto-detection della capability per-modello (D-13) e validazione a due stadi (D-05).

**PRD-first:** richiede il PRD-amendment (WEBMODEL-01..03 effort-only + i requisiti effort-enum a due stadi + capability-auto-detection + real-knobs-only, D-11/D-12/D-13) prima del codice; **nessuna nuova migration** (persistenza sulla colonna `conversations.metadata` jsonb esistente, D-06).

#### Phase 37F: Conversation & Artifact Sharing / Export (INSERTED)

**Goal:** Condivisione/export di una conversazione o di un artifact (parità con "Condividi" + link di Claude), rispettando l'isolamento identità di Aura: export file o link condiviso autenticato, MAI una superficie pubblica non autenticata by-default.

**Requirements:** WEBSHARE-01, WEBSHARE-02, WEBSHARE-03, WEBSHARE-04

**Depends on:** Phase 36 (identity isolation), Phase 37A/37B (asset/download lane).

**Success Criteria**:

1. Da una conversazione l'owner può generare un export (Markdown/JSON del thread) scaricato via endpoint identity-scoped (`GetForIdentity`, `Content-Disposition: attachment`).
2. Condivisione: un link è o (a) revocabile + capability-gated verso identità Aura, o (b) — SE si sceglie il pubblico — un token opaco a scadenza esplicitamente opt-in con avviso, mai default; l'owner può revocare.
3. Nessun path host/container e nessun dato di un'altra identità raggiungono un destinatario; l'atto di condivisione è audited.
4. Unit + e2e + un test di cross-identity deny sul link condiviso; coverage ≥85%.

**Design forks for discuss-phase:** (a) scope — solo export file (semplice, nessuna nuova superficie auth) vs. link interno-identità vs. link pubblico a token (max parità, max rischio); (b) granularità — intera conversazione vs. singolo artifact/messaggio; (c) storage link — riusa `assets`/Garage vs. una nuova tabella `shared_links`. **Nota sicurezza:** un link pubblico è un buco potenziale nell'isolamento MUSR — default fail-closed, opt-in esplicito, revoca obbligatoria (probabile ADR).

**PRD-first:** richiede PRD-amendment (WEBSHARE-01..04) + probabile ADR (condivisione vs. isolamento identità).

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

#### Phase 42: Industrial Conversation Compaction

**Goal:** Deliver the complete provider-portable context lifecycle from the approved Section 17 design: compact before destructive loss, preserve immutable canonical evidence and semantic recent continuity, support distributed recursive checkpoints and bounded recovery, keep typed artifacts and durable memory governed separately, and enable only through numerical safety, quality, privacy, and rollback gates.

**Requirements:** IC-01..14 (see [`42-SPEC.md`](phases/42-llm-conversation-compaction/42-SPEC.md) and [`42-TRACEABILITY.md`](phases/42-llm-conversation-compaction/42-TRACEABILITY.md))

**Depends on:** shipped conversation/context, provider, assets, identity/privacy, database, AG-UI, and web foundations. Every slice is additive, backwards-readable, activation-disabled, and rollback-compatible.

**Success Criteria**:

1. Exact provider/model budgeting and semantic-unit selection trigger proactive L2.4 before any allowed L2.5 event and preserve a bounded atomic recent tail.
2. Durable claim/infer/finalize, immutable branch generations, CAS active pointer, deterministic reconstruction, last-known-good recovery, preview/restore, and quarantine pass independent-process tests.
3. Structured non-authoritative summaries, typed content parts, recursive rebase, and separately governed durable memory pass authority, artifact, security, privacy, deletion, and rollback gates.
4. CLI, REPL, Telegram, AG-UI, and accessible web surfaces share one coordinator; the 500+200 corpus meets every Section 17.13 threshold before deterministic staged activation.

**Plans:** 2/10 plans executed

Plans:

- [x] 42-01-PLAN.md — [wave 1 / slice 1] provider capabilities, exact budgets, semantic units, recent tail, L1 contracts, redacted shadow telemetry
- [x] 42-02-PLAN.md — [wave 2 / slice 1] additive schema, distributed claims, immutable manifests, CAS pointer, deterministic reconstruction and bounded recovery
- [ ] 42-03-PLAN.md — [wave 3 / slice 2] structured summarizer, authority ledger, adversarial validator, manual coordinator, preview and restore
- [ ] 42-04-PLAN.md — [wave 4 / slice 3] typed content parts, artifact durability/reachability, provider projection and typed L1
- [ ] 42-05-PLAN.md — [wave 5 / slices 4-5] atomic L2.4-before-L2.5 ladder, bounded overflow, recursive rebase, corruption recovery and canary controls
- [ ] 42-06-PLAN.md — [wave 6 / slice 6] separate durable-memory privacy lifecycle and security review
- [ ] 42-07-PLAN.md — [wave 7 / slice 7] common CLI/REPL/Telegram/AG-UI surfaces and accessible web recovery UX
- [ ] 42-08-PLAN.md — [wave 7 / rollout persistence] additive schema, sqlc queries, durable scoped state, immutable evidence, CAS and atomic LKG rollback
- [ ] 42-09-PLAN.md — [wave 8 / rollout control] evaluator-to-store/controller-to-effective-config-to-coordinator wiring and distributed rollback fences
- [ ] 42-10-PLAN.md — [wave 9 / terminal gate] 500+200 corpus, blocking CI matrix, operations runbook and acceptance evidence

## Progress

| Milestone | Phases | Plans | Status | Completed |
| --------- | ------ | ----- | ------ | --------- |
| v0.0.0 Substrate | 0–21 (24 phases) | 144/144 | ✅ Shipped | 2026-06-15 |
| v1.0.0 Aura Deep Search Web Cockpit | 22–30 (9 phases) | 45/45 | ✅ Shipped | 2026-06-29 |
| v2.0.0 Industrial Hardening & Multi-User Production | 31–42 (13 phases, incl. 37A) | 0/— | 🔨 In planning | — |

### Phase 43: Operator break-glass recovery and forgot-password E2E

**Goal:** Add a host-only **break-glass operator recovery** path so a missing/wiped `aura.identity_recovery` row can never permanently lock the operator out of the cockpit: an `aura` CLI subcommand (running on the host = admin proof) that resets the operator password (reusing Authula's argon2 `PasswordService.Hash`), invalidates existing sessions, and re-seeds a missing `identity_recovery` row — the credential sourced from a prompt/env the operator supplies, never logged. Plus **end-to-end coverage of the forgot-password flow**: happy path (recovery configured → `/start` → security question → code [Telegram delivery mocked] → `/verify` → set new password → login) and the **deny path** (recovery row missing → generic denial, no factor leak), with backend unit/integration coverage of the new command and the deny branch.

**Root cause (why this phase exists):** the 37D-05 coverage-gate DB-wipe footgun left the live operator (`dvdmarchetto@gmail.com`) with `identity_auth_links` + `telegram_accounts` rows but **no `identity_recovery` row** (recovery_rows=0). `LookupRecoveryByEmail` INNER-JOINs all three tables, so it returns zero rows → `ErrPasswordResetDenied` → forgot-password silently sends no code, and there is no offline recovery path → **permanent lockout**. This phase closes that class of footgun.
**Requirements**: R1–R6 (locked in 43-SPEC.md — break-glass command, operator guard, password sourcing, recovery re-seed, forgot-password E2E, backend coverage)
**Depends on:** Phase 36 (Multi-User Identity Isolation + Authula cutover — owns `authula.*`, `aura.identity_recovery`, the forgot-password flow, and `PasswordService`)
**Plans:** 4/4 plans complete

Plans:
**Wave 1**

- [x] 43-01-PLAN.md — [wave 1] `internal/breakglass` pure logic: operator guard (`selectSoleOperator`, R2/D-11 active/deactivated rule) + password/Q&A sourcing (`Sourcer`, R3/D-03) + unit tests
- [x] 43-04-PLAN.md — [wave 1] Playwright `password-reset.spec.ts` happy + deny (mocked, generic-no-factor denial, R5/D-10)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 43-02-PLAN.md — [wave 2] offline Authula setter + `RecoverOperator` orchestrator (re-seed + neutral audit, D-01/D-02/D-04/D-06) + throwaway-DB `db_integration` test (R1/R4/R6, D-07/D-08) + `coverage_docker.sh` secret export (DC-1)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 43-03-PLAN.md — [wave 3] `cmd/aura` glue: `recover-operator` subcommand + `identity.go` dispatch (D-05) + `golang.org/x/term` direct promotion (R1/R3/R4)
