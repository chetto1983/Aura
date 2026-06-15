# Aura `internal/agent` — Production-Readiness Audit (2026-06-15)

**Audited path:** `d:\Aura\internal\agent` (loop · tools · mcptools · workflow · prompt · hooks · tracing · budget)
**Reference paths (verified call-sites):** `internal/secret`, `internal/web`, `internal/swarm`, `internal/reasoningtrace`, `internal/db`.
**HEAD:** `136325dc` · **Branch:** `master` · **Cycle:** fresh independent pass (4 parallel deep-readers + manual core-loop read).

> **This cycle replaces the 12 canonical report files** with a fresh, independent, evidence-based audit. The prior multi-cycle audit (2026-06-10 → 06-13) is **preserved** in its dated cycle docs: [`reconciliation-2026-06-13.md`](reconciliation-2026-06-13.md), [`re-audit-2026-06-12.md`](re-audit-2026-06-12.md), [`deep-correctness-audit-2026-06-10.md`](deep-correctness-audit-2026-06-10.md), [`e2e-closure-2026-06-11.md`](e2e-closure-2026-06-11.md), [`T-01-test-apex.md`](T-01-test-apex.md), and the `*-validation-*.md` notes. Where this cycle's findings match a prior one, they are cross-referenced (e.g. *prior B-04*).

---

## TL;DR

Aura's agent runtime is a **verified-correct reasoning loop wrapped in an under-hardened operational perimeter.** The loop in isolation is excellent (~8.5/10): bounded recovery counters (no infinite loop), a non-empty terminal contract, KV-cache stable-prefix discipline, a shared-atomic budget that defeats the swarm fan-out bomb, strong SSRF hardening, a crypto-nonce untrusted-output envelope, and ~19.4k LOC of tests with `goleak` + property + fuzz coverage.

The **blended production-readiness score is 6.5/10**, held down by the perimeter:

- **1 P0** — no `recover()` in any spawned goroutine (verified repo-wide); one panicking tool call crashes the whole multi-channel `serve` daemon.
- **12 P1** — dedup concurrent-map-write race; command-hook TOCTOU + full-env secret exposure + no fail-soft; MCP reconnect livelock + `=0`-timeout hang; no capability gate on mutating MCP tools; reasoning-router latency cliff; plaintext PII in the reasoning trace; DB-password DSN leak into shell children; ungated skill self-activation; no SLO metrics; no structured logs.
- **26 P2 / 24 P3** — file-size caps, MCP name collisions, config validation, crash-resume, dead code, and assorted hardening.

If you read only one document, read [`executive-summary.md`](executive-summary.md).

---

## How to read these reports

| File | What it is | Read it when |
|---|---|---|
| [`executive-summary.md`](executive-summary.md) | Score, top-5 risks, immediate actions | You want the 2-minute verdict |
| [`architecture-review.md`](architecture-review.md) | Current design, loop analysis, weaknesses, target shape | You want to understand the system |
| [`bug-report.md`](bug-report.md) | Every finding (P0/P1 full format, P2/P3 tables), `AG-###` IDs | You're fixing things |
| [`security-audit.md`](security-audit.md) | Threat model, prompt-injection surface, subprocess/file/secret boundaries | Threat-model review |
| [`infrastructure-audit.md`](infrastructure-audit.md) | Logging, metrics, tracing, config, secrets, health, persistence, isolation | Ops/SRE readiness |
| [`testing-strategy.md`](testing-strategy.md) | Coverage reality, missing failure-mode tiers, the pyramid, CI | Test planning |
| [`industrialization-roadmap.md`](industrialization-roadmap.md) | Phases 0–5 with effort/impact/acceptance | Sequencing the work |
| [`action-plan.md`](action-plan.md) | Concrete backlog (A1–A25) by priority | Sprint planning |
| [`risk-register.md`](risk-register.md) | All risks: severity/probability/impact/status | Risk tracking |
| [`target-architecture.md`](target-architecture.md) | Proposed industrial-grade design (L1–L6 layers) | Long-horizon design |
| [`proposed-patches.md`](proposed-patches.md) | Patch-style recommendations (PP-1..PP-10, not applied) | Implementing fixes |
| [`22-finding-ledger.md`](22-finding-ledger.md) | **Phase-22 close-out:** every AG-001..064 with a constrained disposition (`fixed+test` / `accepted+rationale` / `confirmed+routed`), evidence, and commit | Verifying nothing was dropped |
| [`22-LIVE-SIGNOFF-2026-06-15.md`](22-LIVE-SIGNOFF-2026-06-15.md) | **Phase-22 close-out:** automated CI/coverage/mutation evidence + the operator live-stack sign-off runbook | Gate-3 close evidence |
| [`audit-index.json`](audit-index.json) | Machine-readable summary | Tooling/dashboards |

**Severity scale.** P0 = critical production blocker / data loss / security breach / unsafe execution / system-wide failure. P1 = serious reliability/correctness/security to fix before production. P2 = important maintainability/observability/architecture. P3 = improvement / cleanup / future hardening.

**Finding IDs.** This cycle uses `AG-###`. Deep-reader sub-IDs (`LP-`, `F-`, `CMD-`, `CACHE-`, `REAS-`, `TRC-`, `MET-`, `EVT-`, `OBS-`, `WF-`, `DD-`, `BG-`, `SC-`, `TO-`, `ST-`, `AUR-`) are retained in `bug-report.md` for traceability. Prior-cycle `B-##/O-##/D-##/M-##/R-##` IDs are cross-referenced where they match.

**Threat-model calibration (important).** The runtime is designed (amendment #50 / D-15c) for a *single trusted operator on their own machine*: the host shell + filesystem **are** the capability — no sandbox, no path fence. Arbitrary `shell_exec`/`fs_write` is the **intended capability, not a finding**. Findings are graded within that model **and** the deployment reality that the same binary serves Telegram + AG-UI + scheduler from one daemon (which makes crash blast-radius and cross-turn isolation real). Several security findings (hook TOCTOU, capability gate, self-extension) are rated P1 today and **become P0 in any multi-tenant deployment** — see `security-audit.md` §7.

---

## Most important findings (2026-06-15)

| # | Sev | Finding | Files |
|---|---|---|---|
| AG-001 | **P0** | No `recover()` in any spawned goroutine → one panicking tool crashes the whole daemon | `llm_agent_parallel.go:43`, `workflow/parallel.go:95`, `swarm.go`, `shell_bg.go:174` |
| AG-002 | **P1** | `dedupRing` mutated lock-free; latent concurrent-map-write fatal (`recover()` can't catch) | `budget_dedup.go:122,170` |
| AG-003 | **P1** | Command-hook exec TOCTOU + full `os.Environ()` secrets to subprocess + unvalidated request rewrite | `hooks_command.go:182,183,114,206` |
| AG-005 | **P1** | MCP reconnect holds lock across spawn+handshake, ctx-coupled, no backoff/breaker → livelock | `mcptools/bridge_reconnect.go:97-155` |
| AG-006 | **P1** | `AURA_MCP_CALL_TIMEOUT_SEC=0` disables the per-call timeout → unbounded hang + held mutex | `mcptools/timeout.go:24`, `bridge.go:76` |
| AG-007 | **P1** | No capability gate on mutating MCP tools; reconnect can silently flip `Mutating` | `llm_agent_dispatch.go:100`, `bridge_reconnect.go:117` |
| AG-008 | **P1** | Reasoning-router LLM fallback adds ≤8s extra round-trip/turn when the embed sidecar degrades | `llm_agent_reasoning.go:50-90` |
| AG-009 | **P1** | Full prompts/history/PII logged to the reasoning trace; redaction is env-name-only | `reasoningtrace/*`, `llm_agent.go:290` |
| AG-010 | **P1** | DB password leaks into `shell_exec` children via DSN-shaped env vars | `shell_exec.go:424`, `secret/envkey.go:20` |
| AG-011 | **P1** | Skill self-activation ungated despite gated-looking spec (*prior B-04*) | `skill_write.go:164`, `skill.go:99` |
| AG-012 | **P1** | No latency/error/cost/outcome metrics — no SLOs (*prior O-02*) | `metrics.go:11-68` |
| AG-013 | **P1** | No structured logs; tracing silent-drop + `mintSpanID` panics (*prior O-01*) | `tracing.go:41-74,96-102` |

Full P2/P3 lists and the confirmed-good (no-action) strengths are in [`bug-report.md`](bug-report.md).

---

## Verified strengths (preserve as-is)

SSRF hardening (`internal/web`: pinned-IP dial, scheme allowlist, metadata-IP block, per-hop redirect re-validation, size cap) · `trust.go` crypto-nonce untrusted-output envelope · KV-cache stable-prefix discipline · shared-atomic budget defeating the `max_steps^depth` fan-out bomb · non-empty terminal contract (synthesis → Italian stub) · bounded recovery/completion/truncation counters (no infinite loop) · 105 test files / ~19.4k test LOC with `goleak`, property, and fuzz coverage.

## Recommended next 5 actions

1. **`recover()` in every spawned goroutine** + Runner backstop (AG-001 / PP-1).
2. **Mutex on `dedupRing`** (AG-002 / PP-2).
3. **MCP resilience:** single-flight reconnect off-lock + `WithoutCancel` + backoff/breaker; `=0`→default, boot-validate (AG-005/AG-006 / PP-3).
4. **Secret boundary:** DSN markers in `IsSecretEnvKey`; minimal-env command hooks (AG-010/AG-003 / PP-5/PP-4).
5. **Observability minimum:** `slog` + Prometheus turn-outcome/latency/error/token metrics; never panic in `mintSpanID` (AG-012/AG-013 / PP-6).

See [`industrialization-roadmap.md`](industrialization-roadmap.md) for the phased plan. The loop core needs no rework — the work is perimeter hardening.
