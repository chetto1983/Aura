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

- [ ] **Phase 31: Stabilization & CI Unblock** (Wave 0 — URGENT, prerequisite) — `QUAL-01` (quality audit + F-015)
  - Goal: clean tree + green CI so every commit passes hooks (drop the `--no-verify` workaround). Coordinate with the in-flight Codex session on the over-cap files.
  - Success: (1) no production/test file >600 LOC (`cmd/aura/serve_webui.go`, `web/src/__tests__/LoginPage.test.tsx` split); (2) `internal/webui/dist` rebuilt → `web-dist-freshness` green; (3) all Go CI jobs use `scripts/go_packages.sh`, no raw `./...` (F-015); (4) frontend branch-coverage gate restored ≥85%.
- [ ] **Phase 32: Quality Cleanup — Dead Code + Shared Helpers** — `QUAL-02/03/05` (quality audit ~64 findings)
  - Goal: kill cross-package duplication + dead code BEFORE feature phases build on them (so later work reuses clean shared packages).
  - Success: (1) dead exports/placeholders removed, each confirmed via `deadcode`/`knip`/repo-wide `rg`; (2) shared packages extracted — `internal/neostore`, `internal/envutil`, `internal/agentrender`, agent `CanonicalArgs`/`isTransientNetworkErr`, web single `getJSON`/shared `focusTrap` — with a parity test per extraction; (3) targeted test gaps closed (`web/throttle`, setup ordering, Telegram keyword fallback, `truncateTailBytes`).
- [ ] **Phase 33: Runtime Profiles + Config Validation** (keystone) — `PROF-01..06`, `QUAL-04`(env catalog) (F-002/007/016/018/026/041)
  - Goal: 4 validated profiles (`dev`/`local_trusted`/`single_user_hardened`/`server_production`) in `internal/config`; production fails fast on unsafe defaults; all hot-path `AURA_*` knobs catalogued.
  - Success: (1) `aura config validate --profile server_production` exits non-zero listing every unmet requirement; (2) copying `.env.example`→`.env` keeps the destructive-shell gate active; (3) invalid env fails-fast under production, warns under dev; (4) `dev`/`local_trusted` preserve today's full-host behavior unchanged.
- [ ] **Phase 34: Agent-Loop Correctness + Durable Ledger** — `LOOP-01..11`, `QUAL-04`(double-Validate/pool-leak, int32 guard) (F-003/004/005/009/010/029/030/031/040/045/048)
  - Goal: terminal-response exclusivity, atomic HITL resume/pause, fenced sidecars, durable ledger state machine (migration 0025).
  - Success: (1) `text_response` + mutating sibling never executes the sibling; (2) duplicate single/batch resume → exactly one answer/pause, append-failure leaves a repairable state; (3) outside-root/traversal/symlink sidecar reads rejected; (4) mutating tool that panics post-side-effect still arms the completion gate.
- [ ] **Phase 35: ToolGateway + Policy Engine** — `GATE-01..04` (F-001 gateway, F-006/011/020)
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

## Progress

| Milestone | Phases | Plans | Status | Completed |
| --------- | ------ | ----- | ------ | --------- |
| v0.0.0 Substrate | 0–21 (24 phases) | 144/144 | ✅ Shipped | 2026-06-15 |
| v1.0.0 Aura Deep Search Web Cockpit | 22–30 (9 phases) | 45/45 | ✅ Shipped | 2026-06-29 |
| v2.0.0 Industrial Hardening & Multi-User Production | 31–41 (11 phases) | 0/— | 🔨 In planning | — |
