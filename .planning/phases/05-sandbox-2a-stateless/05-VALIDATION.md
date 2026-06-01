---
phase: 5
slug: sandbox-2a-stateless
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-01
---

# Phase 5 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from
> `05-RESEARCH.md` § Validation Architecture. This phase's correctness signals are
> **security boundaries**: each negative test asserts a syscall/escape that MUST fail
> with EPERM/ENOENT. A passing positive test alone is insufficient — the negatives are
> the load-bearing signal. A PASS that does NOT trigger the boundary is a false-green.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (stdlib) + table-driven + `goleak` (repo mandate) + `golang-testing` skill patterns |
| **Build-tag tier** | `//go:build sandbox_integration` (real sidecar required; `t.Fatal` under `$CI` when env unset — no-skip-as-green) |
| **Config file** | none — Go modules; sidecar env via `AURA_SANDBOX_URL` / `AURA_SANDBOX_TIMEOUT_SEC` / `AURA_SANDBOX_RUNTIME` |
| **Quick run command** | `go test ./internal/sandbox/ ./internal/agent/tools/` (unit, no sidecar) |
| **Integration command** | `go test -tags sandbox_integration -race ./internal/sandbox/...` (sidecar up) |
| **Bench command** | `scripts/sandbox_escape_bench.sh` (deterministic 18-scenario port → escape-rate) |
| **Full suite command** | `make quality-full` (folds sandbox coverage into the 85% floor — CLAUDE.md) |
| **Estimated runtime** | ~5 s unit / ~60–120 s integration+bench (sidecar build + DinD) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/sandbox/ ./internal/agent/tools/` (unit, fast).
- **After every plan wave:** Run `go test -tags sandbox_integration -race ./internal/sandbox/...` + `scripts/sandbox_escape_bench.sh` (sidecar up).
- **Before `/gsd-verify-work` (Gate 3 DoD):** `make quality-full` green (≥85% combined coverage) AND escape-rate < 5% recorded in `docs/aura-quality-snapshot.md` AND deterministic-bench config-regressions at 0 AND QEMU arm64 leg green (with the QEMU-divergence caveat noted).
- **Max feedback latency:** ~5 s (unit tier); integration tier gates each wave merge.

---

## Per-Task Verification Map

> Task IDs are assigned by the planner (PLAN.md). This draft binds each **requirement /
> success-criterion** to its automated signal so the planner can attach the matching
> `<acceptance_criteria>` per task. Update Task ID + Plan + Wave columns once PLAN.md exists.

| Req / SC | Behavior (ROADMAP SC) | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|----------|------------------------|------------|-----------------|-----------|-------------------|-------------|--------|
| CAP-01 / SC#1 | `aura exec python "print(2+2)"` → `4`, sidecar idle within timeout | — | positive control — allowlist not too tight | integration | `go test -tags sandbox_integration ./internal/sandbox/ -run TestRunner_PythonHappy` | ❌ W0 | ⬜ pending |
| CAP-01 / SC#2 | `ctypes…ptrace(...)` → EPERM; `open('/proc/self/root/etc/shadow')` → ENOENT/EPERM | EoP / Info Disclosure | syscall + host-file boundary holds | integration (negative) | `... -run TestRunner_PtraceBlocked` / `TestRunner_ProcRootDenied` | ❌ W0 | ⬜ pending |
| CAP-01 / SC#3 | `socket().connect(('1.1.1.1',80))` → EPERM; `unshare(CLONE_NEWNET)` → EPERM | Info Disclosure / EoP | net-none + unshare excluded | integration (negative) | `... -run TestRunner_SocketBlocked` / `TestRunner_UnshareBlocked` | ❌ W0 | ⬜ pending |
| CAP-01 / SC#4 | `scripts/sandbox_escape_bench.sh` → escape-rate < 5% in quality-snapshot | all | 18-scenario denominator + config-regressions=0 | bench (deterministic) | `scripts/sandbox_escape_bench.sh && grep 'escape-rate' docs/aura-quality-snapshot.md` | ❌ W0 | ⬜ pending |
| CAP-01 / SC#5 | compose `aura-sandbox`: `cap_drop:ALL`, `no-new-privileges`, `read_only`, `pids_limit:64`, userns-remap (daemon) all set | EoP | hardening flag set complete | config-assertion | `scripts/sandbox_escape_bench.sh` config-regression checks (must stay 0) | ❌ W0 | ⬜ pending |
| D-16 / D-17 | limit_hit paths: timeout / oom / pids reported; lean preview shape | DoS | resource caps enforced + reported | integration + unit | `TestRunner_TimeoutLimitHit`, `TestExecute_LeanPreview` (unit, fake runner) | ❌ W0 | ⬜ pending |
| D-18 | `ErrSandboxUnreachable` after auto-start fails; `ErrSandboxProtocol` on malformed resp; `aura exec` exit 70 | — | env-fault → typed error (not result) | unit + cli | `TestDockerRunner_UnreachableSentinel`, `TestRunShell_MalformedProtocol`, `TestCLIExec_Exit70` | ❌ W0 | ⬜ pending |
| D-11 / D-12 | seccomp profile valid + loaded on both arches; arm64 under QEMU | EoP | multi-arch by-name profile loads | integration | QEMU leg: `docker buildx ... --platform linux/arm64` run of the negative tests | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

### Negative-Test Inventory (the load-bearing signals)

Each MUST fail with the expected errno (a PASS that does NOT trigger the boundary is a false-green):

- `ptrace` → EPERM (allowlist excludes ptrace)
- `socket` / `connect` → EPERM (net-none + socket syscalls excluded)
- `unshare(CLONE_NEWNET)` → EPERM (allowlist excludes unshare; blocks in-container userns)
- `open('/proc/self/root/etc/shadow')` → ENOENT/EPERM (read-only rootfs + no host mount)
- `mount(...)` → EPERM (excluded)
- **positive control:** `print(2+2)` → `4`, `echo hello` → `hello` (proves the allowlist is not too tight)
- **limit controls:** infinite loop → `limit_hit:"timeout"`; large alloc → `limit_hit:"oom"`; fork bomb → `limit_hit:"pids"`

---

## Wave 0 Requirements

- [ ] `internal/sandbox/docker_test.go` — `//go:build sandbox_integration` happy + negative + limit + sentinel tests
- [ ] `internal/sandbox/errors.go` + unit tests for the two sentinels (`ErrSandboxUnreachable`, `ErrSandboxProtocol`)
- [ ] `internal/agent/tools/execute_test.go` — lean-preview shaping (unit, fake `sandbox.Runner`) + deferred-spec assertions
- [ ] `cmd/aura/` exec CLI test (exit-code 70 path; reuse the re-exec subprocess pattern in `cmd/aura/agent_test.go`)
- [ ] `scripts/sandbox_escape_bench.sh` — deterministic 18-scenario port (config-regression assertions + live escape-rate denominator)
- [ ] CI DinD job: install `runsc`, write inner `daemon.json` (userns-remap + runsc), QEMU binfmt, export sandbox env (no-skip-as-green)
- [ ] Mutation spot-check (≥70% killed) target file: `internal/sandbox/docker.go` (CLAUDE.md Gate-3)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real-DGX arm64 seccomp confirmation | D-12 | QEMU syscall emulation can diverge from a real arm64 kernel's seccomp behavior — only a real DGX kernel confirms | Before any production arm64 deployment, run the negative-test integration tier on real arm64 hardware; record result + date in `docs/aura-quality-snapshot.md`. Tracked obligation, not a per-merge gate. |
| LLM-driven SandboxEscapeBench red-team | D-03 | Cost (~$1/attempt) + non-determinism — separately tagged, scheduled/manual, NOT per-merge | Run the real Inspect AI harness + a frontier model out-of-band; feed the true capability number into the quality-snapshot when run. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (every ❌ W0 row above has a created test file)
- [ ] No watch-mode flags
- [ ] Feedback latency < 5 s (unit tier)
- [ ] Negative-test inventory: every boundary asserts the expected errno (no false-greens)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
