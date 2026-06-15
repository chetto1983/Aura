---
phase: 8
slug: sandbox-2b-session-bound
status: audited
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-03
audited: 2026-06-04
---

# Phase 8 — Validation Strategy

> Per-phase validation contract. **Re-targeted 2026-06-04**: the original map below validated the bespoke session-bound sandbox, which commit `0ebb3d81` (D-15 pivot) deleted (~7,300 LOC). Phase 8 was re-scoped in ROADMAP.md to "Sandbox via sandbox-agent (local container)" — this file now maps the 4 current success criteria. The historical bespoke map is preserved in git history of this file (`git log -p -- .planning/phases/08-sandbox-2b-session-bound/08-VALIDATION.md`).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Go 1.26), stdlib assertions |
| **Config file** | none — build-tag `sandbox_integration` gates the live tier |
| **Quick run command** | `go test ./internal/sandboxagent/ ./internal/agent/tools/ ./internal/scoring/` |
| **Full suite command** | `AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 go test -race -tags sandbox_integration ./internal/sandboxagent/ ./internal/agent/tools/ ./internal/scoring/` (stack: `make sandbox-up`) |
| **Estimated runtime** | <1s unit; ~2s live tier against a healthy container |

---

## Per-Criterion Verification Map (ROADMAP Phase 8 success criteria)

| Criterion | Requirement | Secure Behavior | Test Type | Automated Command | File | Status |
|-----------|-------------|-----------------|-----------|-------------------|------|--------|
| SC1 (mocked path) | CAP-01/CAP-02 | `sandbox_exec` → `sandboxagent.Client` → POST `/v1/processes/run` threads command/args/cwd/env/timeout/max-output; response decoded | unit | `go test -run 'TestSandboxExecRunsThroughSandboxAgent\|TestClientRun' ./internal/agent/tools/ ./internal/sandboxagent/` | sandbox_exec_test.go + client_test.go | ✅ green |
| SC1 (live eval) | CAP-01/CAP-02 | live container runs `python -c "print(40+2)"` → stdout `42`, exit_code 0, not timed out | integration (live) | `AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 go test -tags sandbox_integration -run TestSandboxAgentLive_PythonEval ./internal/sandboxagent/` | client_live_integration_test.go | ✅ green (live 2026-06-04, also `-race`) |
| SC1 (live persistence) | CAP-01/CAP-02 | file written under `/workspace` in one `Run` is readable in a separate later `Run` (named volume persists) | integration (live) | `AURA_SANDBOX_AGENT_URL=http://127.0.0.1:2468 go test -tags sandbox_integration -run TestSandboxAgentLive_WorkspacePersistence ./internal/sandboxagent/` | client_live_integration_test.go | ✅ green (live 2026-06-04, also `-race`; required the `/workspace` ownership fix below) |
| SC2 | CAP-01 | `sandbox_exec` registered **non-deferred** (model sees command/args schema — live 502 regression guard); result carries stdout/stderr/exit_code/timed_out/truncated/duration_ms | unit | `go test -run 'TestSandboxExecSpecIsNotDeferredAndLocal\|TestBuildChatRegistry_RegistersSandboxExec\|TestBuildRegistry_RegistersSandboxExec' ./internal/agent/tools/ ./cmd/aura/` | sandbox_exec_test.go + chat_test.go + registry_test.go | ✅ green |
| SC3 | CAP-01 | sandbox down / runner error → structured `{"error":"sandbox_unavailable","hint":"… make sandbox-up"}` inline result (model-visible, not loop-fatal); zero boot provision | unit | `go test -run 'TestSandboxExecUnavailableIsInlineResult\|TestSandboxExecRunnerErrorIsInlineResult' ./internal/agent/tools/` | sandbox_exec_test.go | ✅ green |
| SC4 | CAP-02 | bespoke surface deleted (`internal/sandbox/*`, migration 0008, `cmd/aura/{exec,sandbox,sandbox_proxy}.go`, `sandbox.yml`); zero `code-sandbox-mcp` refs in code; build/test/lint green | structural | `grep -ri 'code-sandbox-mcp' --include='*.go' .` (0 matches) + CI | — | ✅ verified 2026-06-04 |
| scoring (D-11, retained) | CAP-02 | empty=Safe, pypi-only=Safe, arbitrary=Risky; monotone modifiers | unit (rapid) | `go test ./internal/scoring/` | scoring_test.go | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Live Tier — CI posture

The `sandbox_integration` tier is **not wired into CI**: `compose.yaml` uses `pull_policy: never` for `aura-sandbox-agent:py3` (operator-preloaded local image, no online pull by design), so runners have no image. No-skip-as-green still holds — `sandboxOrSkip` in `client_live_integration_test.go` calls `t.Fatal` under `$CI` when `AURA_SANDBOX_AGENT_URL` is unset, so a misconfigured CI job that *claims* to run the tier fails instead of false-greening. Operator runbook: `make sandbox-up`, then the Full suite command above.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| SC1 agent-loop E2E (the *agent* chooses `sandbox_exec` for a compute task in `aura chat`) | CAP-01 | Tool-selection by the live LLM is non-deterministic; the wiring (registry registration + client + container) is covered by the automated rows above. | `make sandbox-up`, `aura chat`, ask "compute 40+2 in python" → verify a `sandbox_exec` tool call with `command:"python"`, `args:["-c", …]` and reply `42`. |

---

## Validation Audit 2026-06-04

| Metric | Count |
|--------|-------|
| Gaps found | 2 (SC1 live eval, SC1 live persistence — no live tier existed post-pivot) |
| Resolved | 2 (both authored + run green live, plain + `-race`) |
| Escalated | 0 |

**Implementation bug found and fixed during the audit:** the base image has no `/workspace`, so the named-volume mount created it `root:root 0755` — the sandbox process (uid 1001) got `PermissionError` on every write, making SC1's persistence clause false live. Fix: `docker/sandbox-agent/Dockerfile` now bakes `install -d -o sandbox -g sandbox /workspace` (Docker copies ownership into the volume at first initialization) + one-off `chown` of the existing `aura_aura-sandbox-agent` volume. Verified live: `drwxr-xr-x sandbox sandbox /workspace`, persistence test green.

---

## Validation Sign-Off

- [x] All 4 ROADMAP success criteria have automated verification (SC1 unit+live, SC2 unit, SC3 unit, SC4 structural+CI)
- [x] Live tier executed against the real container (not compile-checked): plain + `-race` green 2026-06-04
- [x] No-skip-as-green: gate helper `t.Fatal`s under `$CI`
- [x] No watch-mode flags
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** Nyquist audit complete 2026-06-04. All criteria automated and green; one impl bug (root-owned `/workspace`) surfaced by the new persistence test and fixed in the same audit.
