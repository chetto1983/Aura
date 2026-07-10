---
phase: 37E
slug: composer-model-reasoning-effort-selector-inserted
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-10
---

# Phase 37E — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `37E-RESEARCH.md` §Validation Architecture (Pass 1 + Pass 2 capability-detection additions).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (backend, incl. `-race`) · `vitest` (web unit/component) · `Playwright` (web e2e) |
| **Config file** | `go.mod` · `web/package.json` · `web/playwright.config.ts` (all existing — no new framework) |
| **Quick run command** | `go test ./internal/llm/... ./internal/agent/... ./internal/agui/...` · `cd web && pnpm vitest run` |
| **Full suite command** | `make quality` (vet+build+lint+race+vuln) then `make coverage` (db+neo4j tags) · `cd web && pnpm test:e2e` |
| **Estimated runtime** | backend ~60–120s · web unit ~20s · e2e ~1–3 min |

**Coverage-gate rule (CLAUDE.md, MANDATORY):** the CI gate runs `db_integration neo4j_integration` ONLY. Every capability-detection unit test (OpenRouter models parse, TTL cache, dynamic-400 path, llama.cpp `/props` parse, the level→wire builder branch) MUST be **daemon-free / live-free pure `go test`** (injected `http.RoundTripper` + captured fixtures; fake `ReasoningCapabilitySource`) or it contributes ZERO to the ≥85% owned-surface floor.

---

## Sampling Rate

- **After every task commit:** Run the package-scoped quick command for the touched package.
- **After every plan wave:** Run the full suite command.
- **Before `/gsd-verify-work`:** Full `make quality` + web e2e green.
- **Max feedback latency:** ~120 seconds (package-scoped).

---

## Per-Task Verification Map

> Expanded by the planner per PLAN.md task. Anchors from RESEARCH.md §Validation Architecture:

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 37E-0-* | 00 | 0 | — | — | captured fixtures land (`testdata/openrouter_models.json`, `testdata/llamacpp_props.json`) | fixture | `test -f internal/llm/testdata/openrouter_models.json` | ❌ W0 | ⬜ pending |
| 37E-*-* | * | * | WEBMODEL-01 | — | selector renders only model-advertised levels; persists per-conversation; restores on reopen | e2e/vitest | `cd web && pnpm test:e2e -g reasoning-effort` | ❌ W0 | ⬜ pending |
| 37E-*-* | * | * | WEBMODEL-02 | T-37E-enum / T-37E-cap | `/agent/run` maps symbol→ReasoningConfig; non-enum → 400; level not in model `supported_efforts` → 400; absent/`auto` → adaptive default | unit | `go test ./internal/agui/ -run Reasoning` | ❌ W0 | ⬜ pending |
| 37E-*-* | * | * | WEBMODEL-03 | T-37E-cap | no bypass: override validated against detected capability, never adds a model; llama.cpp wire branch is a pure builder test | unit | `go test ./internal/llm/... -run Wire` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/llm/testdata/openrouter_models.json` — captured real `/models` shape (per-model `reasoning.supported_efforts` / `default_effort` / `mandatory`) for the models-capability parse test
- [ ] `internal/llm/testdata/llamacpp_props.json` — captured llama-server `/props` shape for the local-capability parse test
- [ ] Fake `ReasoningCapabilitySource` (in-memory) for handler/endpoint tests — no live OpenRouter, no daemon
- [ ] Web: Playwright spec skeleton for the effort selector (render / persist / restore) + vitest component stub

*Existing Go + vitest + Playwright infrastructure otherwise covers phase requirements.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Graduated-effort **fidelity** on a real backend (does output actually deepen low→max) | WEBMODEL-03 (D-09) | Advertised `supported_efforts` ≠ guaranteed output differentiation; DeepSeek collapses to on/off; only a budget-capable local llama.cpp shows real gradation. CI must NOT depend on a live model. | Run `scripts/deepseek_reasoning_probe.py` (cloud on/off) + a llama.cpp `thinking_budget_tokens` sweep on the spike-095 local model; confirm off vs on reliably differs and local budgets scale monotonically. Assert only on/off in CI. |
| Live capability fetch against real OpenRouter | WEBMODEL-01 | External dependency; CI uses fixtures. | With a real key, hit `/api/composer/reasoning-capabilities` and confirm the active model's advertised levels render. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (fixtures + fake source)
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
