---
phase: 31
slug: stabilization-ci-unblock
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-29
---

# Phase 31 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `31-RESEARCH.md` § Validation Architecture. The per-task map is
> populated by the planner against PLAN.md task IDs.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (backend) · `vitest` (frontend) · CI shell gates (`scripts/*.sh`, `.github/workflows/ci.yml`) |
| **Config file** | `.golangci.yml`, `web/vitest.config.ts`, `.github/workflows/ci.yml`, `scripts/go_packages.sh`, `scripts/check-file-size.sh` |
| **Quick run command** | `bash scripts/check-file-size.sh` (C1) · `go test ./internal/mcp/...` (C5) |
| **Full suite command** | CI matrix (`make quality` + the `ci.yml` jobs) · `cd web && npm run test -- --coverage` (C4) · `git diff --exit-code internal/webui/dist/` (C2) |
| **Estimated runtime** | file-size ~35s · frontend coverage ~30–60s · CI matrix several min |

---

## Sampling Rate

- **After every task commit:** Run the criterion's quick command (file-size hook / targeted `go test` / dist diff / vitest).
- **After every plan wave:** Run the affected CI gate(s) locally (WSL stack up).
- **Before `/gsd-verify-work`:** All 5 success criteria green; CodeQL re-scan shows `go/request-forgery` closed (C5).
- **Max feedback latency:** ~60 seconds for the unit/gate tiers.

---

## Per-Task Verification Map

> Criterion→validation is fixed (below, from RESEARCH §Validation Architecture); the planner
> binds each to concrete PLAN.md task IDs.

| Criterion | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|-----------|-------------|------------|-----------------|-----------|-------------------|--------|
| C1 file-size ≤600 | QUAL-01 | — | N/A | CI-gate (verify-only) | `bash scripts/check-file-size.sh` (exit 0) | ⬜ pending |
| C2 dist freshness | QUAL-01 | — | N/A | CI-gate (verify-only) | `cd web && npm ci && npm run build` → `git diff --exit-code internal/webui/dist/` | ⬜ pending |
| C3 F-015 CI hygiene | F-015 / SEC-07 | — | N/A | CI-gate | grep-lint finds zero raw `./...` in `.github/workflows/`; lint self-test passes | ⬜ pending |
| C4 frontend ≥85% | QUAL-01 | — | N/A | unit (vitest) | `cd web && npm run test -- --coverage` (4 thresholds ≥85; branches binding ~85.45%) | ⬜ pending |
| C5 SSRF guard | SEC-08 | T-31-SSRF | Outbound MCP HTTP to metadata/(enforced) private ranges denied; loopback permitted under dev | unit + CodeQL | `go test ./internal/mcp/...` (table-driven allow/deny) + CodeQL re-scan alert closed | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements (go test, vitest, CI gates, CodeQL all already wired). The only new test surface is C5's table-driven SSRF guard unit test (`internal/mcp/*_test.go`) and the C3 CI-lint negative self-test (mirroring `scripts/quality_snapshot_gate_test.sh`).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| CodeQL `go/request-forgery` alert resolved | SEC-08 | CodeQL re-scan runs in CI/GitHub, not a local unit gate | **RESOLVED 2026-06-29 (dismissed as false-positive).** Post-push CodeQL re-scan (commit `c93e28fb`) kept the alert OPEN at `http_client.go:229` — CodeQL cannot model the transport-level dialContext SSRF guard as a dataflow sanitizer (the URL string still flows to `client.Do`). This is the RESEARCH A1 MEDIUM-confidence outcome. Resolved by dismissing alert #18 as `false positive` with justification, consistent with the project's identical already-dismissed `internal/web/fetcher_image.go:52` (same classify+pin+dial transport defense). The runtime SSRF protection is complete + tested (metadata/private denied, loopback permitted under dev; `-race` + golangci-lint clean) — the static alert is the false positive, not the defense. |
| Mutation ≥70% on `internal/mcp/ssrf.go` (`classify` is the gate target) | SEC-08 | `go-mutesting` runs only in WSL (go1.26 fork); not a hosted-CI gate | In WSL: `cd ~/Aura-or-mount && export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH" && go-mutesting ./internal/mcp/ssrf.go --min-msi=70` (or the repo's `make`-wrapped mutation target). `PASS`=killed, `FAIL`=survived; score = killed/total must be ≥0.70. Document survivors per the mutation-autopsy convention. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
