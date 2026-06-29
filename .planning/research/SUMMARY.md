# Project Research Summary

**Project:** Aura — v2.0.0 Industrial Hardening & Multi-User Production
**Domain:** Production-grade, multi-user, sandboxed agent runtime (hardening/industrialization of an existing system)
**Researched:** 2026-06-29
**Confidence:** HIGH

> Synthesized inline by the orchestrator after the four GSD researchers (STACK/FEATURES/ARCHITECTURE/PITFALLS, all opus) completed and converged. The online deep-research pass (`docs/research/senior-dev-agent-hardening-2026.md`, adversarially verified) independently corroborates the same calls. The GSD synthesizer subagent and the deep-research final-merge both hit a session/usage limit before writing, so this file was authored in the main loop from the four completed research artifacts + the online report.

## Executive Summary

This is **not greenfield** — it is the industrialization of a feature-complete Go single-binary agentic runtime (the v0.0.0 substrate + v1.0.0 cockpit) to close a 51-finding industrial audit (current 4.6/10) to an **honest 10/10**. The defining decision — how to resolve F-001 (shell/fs run with full host privileges) while keeping the full-host terminal as Aura's core surface — is settled by overwhelming convergence: give each identity a **per-user, full-capability, isolated sandbox** built **directly over the existing container runtime (Docker, via the Docker Go SDK already in `go.mod`)**, *not* over Kubernetes. The agent keeps a full shell/fs/network *inside*; the real host is never exposed; users are isolated. Capability is never stripped — it is contained.

Both the internal 4-way research and the online senior-dev evidence reject K8s (k3s/k0s + `kubernetes-sigs/agent-sandbox`) and microVMs/gVisor-as-default for the mini-PC: the K8s project is designed for multi-node, tens-of-thousands of parallel sandboxes, and its headline `SandboxWarmPool` trades idle compute for latency — counter-productive on one 16-core box. gVisor adds ~+10–125% on IO-heavy work (the core shell/build feature). The correct minimal-industrial form is container-per-active-identity over Docker, with **gVisor/Kata reserved as an opt-in `server_production`/DGX runtime tier** and the agent-sandbox CRD shape kept as a forward-compatible contract for the eventual DGX-Spark multi-node fleet.

The remaining hardening is mostly **wire/policy, not adopt** — Authula, OTel SDK, Prometheus client, govulncheck, testcontainers, Docker SDK, Garage S3, and a ready-but-off `compose.gvisor.yaml` are already present. The dominant risk is **half-done isolation** (some stores owner-scoped, others global → IDOR) and **"full-capability inside" silently becoming "full host"** via Docker-socket/`--privileged`/`--network host`/bind-mounts. An honest 10/10 is bounded by the *weakest* evidence (a real two-identity live E2E, a drilled DR restore), not the count of green checks.

## Key Findings

### Recommended Stack

Most of the v2.0.0 stack already exists in `go.mod`/`compose.yaml`; the work is overwhelmingly **WIRE/policy**, with two **BUMP**s and zero net-new heavyweight adoptions. Detail in [STACK.md](STACK.md).

**Core technologies:**
- **Per-user sandbox = Docker Go SDK** (`moby/moby/client`, already in `go.mod`): a new `internal/sandbox/usersandbox` Go controller spawns/pools a per-identity container — no new daemon, no K8s. *Why:* +0 idle, ~150–400 MB per active user; preserves the single-binary + Compose invariant.
- **gVisor `runsc`** via the existing-but-off `compose.gvisor.yaml`: an opt-in per-profile runtime for `server_production`/untrusted code — *not* the mini-PC default. *Why:* RuntimeClass-grade isolation only where the threat model demands it.
- **Authula v1.11.0 → v1.12.0** (Apache-2.0; capability-per-route, 2FA/TOTP, OAuth, PG; v1.12.0 adds a library-mode event bus useful for the audit ledger): cutover = flip default + bump + per-principal owner-scoping. *Why:* capability-per-route **without forcing RBAC** — exactly the chosen model.
- **OTel Go SDK** (traces already wired in `internal/obs`; **metrics NOT** — that path is the observability-phase work) + **Prometheus `client_golang`** + Grafana dashboards-as-JSON + alert-rule YAML in-repo, syntax-validated in CI.
- **Supply-chain:** `syft` + `cyclonedx-gomod` SBOM, `govulncheck` made blocking, SHA-pinned Actions.
- **Load/chaos/DR:** k6 + vegeta + toxiproxy; DR drill via testcontainers. ⚠️ **Neo4j 5.26 Community = offline backup only** (`neo4j-admin database dump/load`; online/differential is Enterprise) — caps DR RPO/RTO.

### Expected Features

Six capability classes (detail + table-stakes/differentiator/anti-feature split in [FEATURES.md](FEATURES.md)). All 51 findings (F-001..F-052; F-044 intentionally absent) map across these.

**Must have (table stakes for 10/10):**
- **Runtime profiles** (`dev`/`local_trusted`/`single_user_hardened`/`server_production`) validating *effective behavior* — the cheapest keystone, gates ~7 findings.
- **ToolGateway** — one in-process `Decide → allow/deny/approve` seam, fail-CLOSED for mutating tools, durable mutating-only ledger reservation with idempotency.
- **Per-user sandbox lifecycle** — create/stop/resume/scheduled-delete + idle-TTL + persistent per-user `/workspace` + stable identity; full capability inside, isolation outside.
- **Multi-user identity isolation** — owner-scoped stores + per-principal API filtering + session-bound background jobs + two-identity live E2E.
- **Agent-loop correctness** — terminal `text_response` exclusivity; HITL resume/pause atomicity.
- **Production observability** — OTel metric path, alert pack, honest `/readyz`, retention/cleanup.

**Should have (competitive):** capability-eval suite (golden + adversarial prompt-injection + chaos-cancellation, CI report); secret-redaction in tool output/traces; idempotency keys.

**Defer (anti-features — explicitly do NOT build):** RBAC/roles, OAuth multi-tenant, OPA/Rego/Cedar policy engines, full K8s + agent-sandbox CRDs (DGX-fleet future only), gVisor/Kata as default, warm pools on one box, external gateway process, bundled Grafana/Prometheus stack.

### Architecture Approach

Every capability slots into existing seams; new vs modified detail in [ARCHITECTURE.md](ARCHITECTURE.md). Crucially, **every hardening step is a no-op in `dev`/`local_trusted`** (gateway fail-open, host-direct executor) — the operator's daily full-host experience is unchanged; hardening activates only under `server_production`.

**Major components:**
1. **ToolGateway** — injected interface at the existing `LlmAgent.runTool → execTool → tool.Execute` seam; table-driven over `internal/scoring` risk tiers; coexists with HookManager + completion-gate + `toolinvocations` ledger. No tool changes.
2. **`internal/sandbox/usersandbox` + `SandboxRouter.Resolve(ctx)`** — one `Executor` seam mapping `identityctx.IdentityID(ctx)` → the user's sandbox endpoint; host shell/fs tools target the per-identity sandbox under hardened profiles, the host directly under local.
3. **Owner-scoped stores** — additive `*ForIdentity` methods on `conversations`/`askuser`/approvals (non-breaking); `agui` APIs filter by `identityctx` principal; `NewConversation` inherits identity (fixes F-028); session eviction on delete (F-039).
4. **Runtime profiles in `internal/config`** (NOT `internal/profile` — that's Agent.md identity; name collision) — gates feature wiring at boot; `aura config validate --profile server_production` fails on unmet requirements.
5. **Observability** — OTel spans wrap LLM/tool/MCP/DB/scheduler; `/readyz` reflects listener+DB+migration+scheduler state.

**Migrations:** 0025 (durable ledger states), 0026 (observability/retention), 0027 (per-user sandbox) + optional owner-scope indexes.

### Critical Pitfalls

Top items from [PITFALLS.md](PITFALLS.md) (13 total, each mapped to F-###/R-### + owning phase):

1. **"Full-capability inside" silently becoming "full host"** — no Docker-socket mount, no `--privileged`, no `--network host`, no host bind-mounts. Build a *synthetic* full host (named volumes only); make those flags **unrepresentable** in the sandbox struct. (#1 escape vector.)
2. **Half-done multi-user isolation / IDOR** — thread `identityctx` at the **store layer**, not just handlers; treat the owner-scoped-surface list as an enumerated deliverable; random unguessable background-shell IDs (F-032). Two-identity live E2E is the only proof.
3. **ToolGateway over-engineering** — the LINE: one in-process function reusing existing risk tiers, fail-CLOSED for mutating tools, mutating-only ledger. OPA/Rego/RBAC/external policy fabric is over the line. (Bottleneck fear is empirically false: ~8.3 ms median.)
4. **Profiles that lie** — validate *effective* behavior, not env-var presence (F-002 empty-override trap, F-016 silent fallback). Keep `dev` frictionless.
5. **Dishonest 10/10** — no-skip-as-green every tier; mutation ≥70% on gateway/isolation/profile files; prompt-injection asserting **denial** under `server_production`; a **drilled** DR restore with measured RPO/RTO; an honest `/readyz`.

## Implications for Roadmap

Dependency-ordered build sequence (hard rule: ToolGateway + profiles **before** sandbox enforcement; durable ledger **before** idempotency). Maps onto the audit's 6-phase `industrialization-roadmap.md`. Phases continue at **31+**. (Phase numbers below are the researcher's suggested grouping — the roadmapper finalizes.)

### Phase 31: Runtime Profiles + Config Validation (keystone)
**Rationale:** Cheapest, gates the production behavior of ~7 findings; unblocks "server_production denies/sandboxes by default" everywhere downstream.
**Delivers:** 4 profiles in `internal/config`, `aura config validate --profile`, effective-behavior validation (default secrets, listener health, CORS, run-dir absolute, env-parse fail-fast).
**Addresses:** F-002, F-007, F-008, F-016, F-017, F-018, F-022, F-026, F-041. **Avoids:** profiles-that-lie pitfall.

### Phase 32: Agent-Loop Correctness + Durable Ledger
**Rationale:** P1 correctness; the ledger state machine (migration 0025) must exist before the gateway and idempotency.
**Delivers:** terminal `text_response` exclusivity (F-003), HITL single+batch resume atomicity (F-004/F-029), pause-flush durability (F-030), mutating-classification preserved on panic (F-031), durable started/succeeded/failed ledger.
**Addresses:** F-003/F-004/F-005/F-011/F-020/F-029/F-030/F-031.

### Phase 33: ToolGateway + Policy Engine
**Rationale:** Single enforcement point; fail-CLOSED for mutating tools (reverses F-006). Must precede sandbox enforcement.
**Delivers:** in-process gateway at the `execTool` seam, table-driven policy over `internal/scoring`, command-hook fail-closed default, sidecar path fencing (F-005).
**Addresses:** F-001 (partial), F-006, F-011, F-020.

### Phase 34: Multi-User Identity Isolation + Authula Cutover (parallelizable with 33)
**Rationale:** The "multi-user production" deliverable; plumbing already exists, stores ignore it.
**Delivers:** `*ForIdentity` store methods, per-principal API filtering, `NewConversation` identity inheritance, session eviction on delete (F-039), random session-bound background-shell IDs (F-032), Authula default flip (provisioning + break-glass first), two-identity live E2E.
**Addresses:** F-028/R-022, F-032, F-039, F-050. **NO RBAC.**

### Phase 35: Per-User Full-Capability Sandbox
**Rationale:** The F-001 resolution; depends on gateway + profiles + isolation.
**Delivers:** `internal/sandbox/usersandbox` Docker controller + `SandboxRouter`, per-identity container (synthetic full host, named volumes, no socket/privileged/host-net), idle-TTL lifecycle, `runtime: runsc` as a `server_production` policy, egress default `--network none`. Migration 0027.
**Addresses:** F-001/R-001, F-012, F-036 (minimal). **Research-lock container-per-identity + ADR rejecting K8s/gVisor-default for v2.**

### Phase 36: MCP Governance Hardening
**Delivers:** canonical transport classifier (F-027), explicit remote trust (F-013), per-server mount timeout (F-033), stdio frame cap (F-034), process-tree teardown (F-035-MCP/close), audited CLI writes (F-037), non-empty trust body (F-038), real HTTP probe (F-046), legacy-env production-gate (F-014).

### Phase 37: Idempotency + Observability Pack
**Rationale:** Idempotency needs the durable ledger; observability extends existing OTel/`/readyz`.
**Delivers:** idempotency keys for mutating tools; OTel **metric** path; Prometheus alert YAML + Grafana JSON in-repo (CI-validated); honest `/readyz`; sidecar/trace retention + cleanup command. Migration 0026.
**Addresses:** F-020, F-023, F-024, F-048, F-049.

### Phase 38: Security & Supply-Chain Pack
**Delivers:** secret redaction in tool output/traces (F-019/F-021), encrypted-trace option, profile-gated CORS allowlists (F-022), non-loopback console guard (F-047), SBOM (syft/cyclonedx-gomod), blocking govulncheck, SHA-pinned Actions (F-051), strict JSON decoding (F-052), CI `./...` filter (F-015).

### Phase 39: Production Ops — Backup/DR, Scale, Capability-Eval
**Rationale:** Literal-10/10 closer; HIGH cost + host-constrained.
**Delivers:** drilled backup/restore with measured RPO/RTO (Neo4j-Community offline-dump caveat baked in), scheduler drain split (F-042) + systemd stop budget (F-043), load (k6/vegeta) + chaos (toxiproxy) harness, capability-eval suite (golden + prompt-injection-denial + chaos), ADRs (F-025), release-readiness checklist, runner waiter-goroutine fix (F-045).
**Addresses:** F-018/F-019/F-025/F-035/F-042/F-043/F-045 + the honest-10/10 evidence bar.

### Phase Ordering Rationale
- Profiles first because they gate every other phase's production behavior at zero cost.
- Ledger before gateway before idempotency before sandbox (each is a hard dependency of the next).
- Identity isolation parallelizes with the gateway (different seams).
- Ops/eval last because it verifies the whole; eval goldens are authored *alongside* each fix and aggregated here.

### Research Flags
- **Phase 35 (sandbox):** needs a pre-merge **measured benchmark** — concurrent-identity ceiling on the 32 GB host before sandbox + LLM sidecars contend; egress enforcement (per-user proxy vs nftables OUTPUT — Slice-2b iptables mechanism exists) is a phase-level design choice.
- **Phase 39 (DR/load/chaos):** HIGH cost + host-constrained (4 GB-GPU mini-PC) — may carry the same **"deferred verification tier"** pattern as v1.0.0's 6 deferred items; flag for closeout policy. Neo4j-Community offline-only backup shapes RPO/RTO targets.
- **Phase 34 (Authula cutover):** auth migration risk — ship provisioning + break-glass *before* flipping the default; re-evaluate CSRF + cookie formats.
- Phases 32/33/36/37/38 are standard patterns against the known codebase — light per-phase research only.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Versions cross-checked vs pinned `go.mod` + live web; sandbox footprint quantified |
| Features | HIGH | Capability-class shapes + audit-finding mapping; minimal-form lines explicit |
| Architecture | HIGH | Integration points verified against the real tree (`internal/runner`, `internal/agent`, `internal/agui`, `internal/config`) |
| Pitfalls | HIGH | Grounded in bug-report/risk-register/security-audit + confirmed code (F-028/F-032/F-039/F-006/F-011) |
| Online corroboration | HIGH | Independent deep-research reaches the same sandbox + authz verdicts (`docs/research/senior-dev-agent-hardening-2026.md`) |

**Overall confidence:** HIGH

### Gaps to Address
- **Concurrent-identity sandbox ceiling on 32 GB** — resolve via a Phase-35 pre-merge benchmark, not at research time.
- **Egress enforcement mechanism** (proxy vs nftables) — Phase-35 design decision.
- **DR/load/chaos executability** on the 4 GB-GPU host — decide deferred-tier vs full at the Phase-39/closeout boundary.

## Sources

### Primary (HIGH confidence)
- `go.mod` / `compose.yaml` / `compose.gvisor.yaml` + live source tree — ground-truth for what already exists.
- `docs/audit/*` (2026-06-21 industrial audit: bug-report, security-audit, risk-register, action-plan, industrialization-roadmap, target-architecture) — the 51-finding ledger.
- kubernetes-sigs/agent-sandbox + Kubernetes.io blog (2026-03-20) + Google OSS blog (2025-11) — K8s agent-sandbox CRD model, gVisor/Kata RuntimeClass, WarmPool cold-start.
- Authula docs (v1.11→v1.12), OTel GenAI semantic conventions, Neo4j 5.26 operations manual (Community offline-backup limitation).

### Secondary (MEDIUM confidence)
- Northflank / AgentMarketCap / Spheron (2026) — E2B/Daytona/Modal/Fly/Firecracker cold-start datapoints; "K8s is overkill on a single host" senior consensus.
- Microsoft Agent Governance Toolkit, IBM mcp-context-forge — policy-gateway + fail-fast secret-validator patterns.

### Full detail
- `.planning/research/{STACK,FEATURES,ARCHITECTURE,PITFALLS}.md`
- `docs/research/senior-dev-agent-hardening-2026.md` (+ tool-policy-gateway deep-dive + 2 claim-verification files)

---
*Research completed: 2026-06-29*
*Ready for roadmap: yes*
