---
phase: 5
slug: sandbox-2a-stateless
status: draft
nyquist_compliant: true
wave_0_complete: true
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
| **Framework** | Go `testing` (stdlib) + table-driven + `goleak` (repo mandate, `TestMain` in `internal/sandbox/docker_test.go`) + `golang-testing` skill patterns |
| **Build-tag tier** | `//go:build sandbox_integration` (real sidecar required; `t.Fatal` under `$CI` when env unset — no-skip-as-green) |
| **Config file** | none — Go modules; sidecar env via `AURA_SANDBOX_URL` / `AURA_SANDBOX_TIMEOUT_SEC` / `AURA_SANDBOX_RUNTIME` |
| **Quick run command** | `go test ./internal/sandbox/ ./internal/agent/tools/` (unit, no sidecar) |
| **Integration command** | `go test -tags sandbox_integration -race ./internal/sandbox/...` (sidecar up) |
| **Bench command** | `scripts/sandbox_escape_bench.sh` (deterministic 18-scenario port → escape-rate + docker.go mutation spot-check) |
| **Full suite command** | `make quality-full` (folds sandbox coverage into the 85% floor — CLAUDE.md) |
| **Estimated runtime** | ~5 s unit / ~60–120 s integration+bench (sidecar build + DinD) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/sandbox/ ./internal/agent/tools/` (unit, fast).
- **After every plan wave:** Run `go test -tags sandbox_integration -race ./internal/sandbox/...` + `scripts/sandbox_escape_bench.sh` (sidecar up via `make sandbox-up`).
- **Before `/gsd-verify-work` (Gate 3 DoD):** `make quality-full` green (≥85% combined coverage) AND escape-rate < 5% recorded in `docs/aura-quality-snapshot.md` AND deterministic-bench config-regressions at 0 AND the `internal/sandbox/docker.go` mutation spot-check ≥70% killed AND QEMU arm64 leg green (with the QEMU-divergence caveat noted).
- **Max feedback latency:** ~5 s (unit tier); integration tier gates each wave merge.

---

## Per-Task Verification Map

> Task IDs are assigned by the planner (PLAN.md). This draft binds each **requirement /
> success-criterion** to its automated signal so the planner can attach the matching
> `<acceptance_criteria>` per task. Update Task ID + Plan + Wave columns once PLAN.md exists.

| Req / SC | Behavior (ROADMAP SC) | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|----------|------------------------|------------|-----------------|-----------|-------------------|-------------|--------|
| CAP-01 / SC#1 | `aura exec python "print(2+2)"` → `4`, sidecar idle within timeout | — | positive control — allowlist not too tight | integration | `go test -tags sandbox_integration ./internal/sandbox/ -run TestRunner_PythonHappy` | ✅ 05-03 W3 | ⬜ pending |
| CAP-01 / SC#2 | `ctypes…ptrace(...)` → EPERM; `open('/proc/self/root/etc/shadow')` → ENOENT/EPERM | EoP / Info Disclosure | syscall + host-file boundary holds | integration (negative) | `... -run TestRunner_PtraceBlocked` / `TestRunner_ProcRootDenied` | ✅ 05-03 W3 | ⬜ pending |
| CAP-01 / SC#3 | `socket().connect(('1.1.1.1',80))` → EPERM; `unshare(CLONE_NEWNET)` → EPERM | Info Disclosure / EoP | net-none + unshare excluded | integration (negative) | `... -run TestRunner_SocketBlocked` / `TestRunner_UnshareBlocked` | ✅ 05-03 W3 | ⬜ pending |
| CAP-01 / SC#4 | `scripts/sandbox_escape_bench.sh` → escape-rate < 5% in quality-snapshot | all | 18-scenario denominator + config-regressions=0 | bench (deterministic) | `scripts/sandbox_escape_bench.sh && grep 'escape-rate' docs/aura-quality-snapshot.md` | ✅ 05-04 W4 | ⬜ pending |
| CAP-01 / SC#5 | compose `aura-sandbox`: `cap_drop:ALL`, `no-new-privileges`, `read_only`, `pids_limit:64`, userns-remap (daemon) all set; gVisor default-on x86 via `make sandbox-up` | EoP | hardening flag set complete + gVisor default-on | config-assertion | `scripts/sandbox_escape_bench.sh` config-regression checks (must stay 0) + `make -n sandbox-up` includes the gVisor overlay on x86 | ✅ 05-02/05-04 W2/W4 | ⬜ pending |
| D-16 / D-17 | limit_hit paths: timeout / oom / pids reported; lean preview shape; per-call timeout_sec delivered on the wire | DoS | resource caps enforced + reported | integration + unit | `TestRunner_TimeoutLimitHit`, `TestRunner_TimeoutClampedAndBodied`, `TestExecute_LeanPreview`, `TestExecute_TimeoutPassThrough` | ✅ 05-03 W3 | ⬜ pending |
| D-18 | `ErrSandboxUnreachable` after auto-start fails; `ErrSandboxProtocol` on malformed resp; `aura exec` exit 70 | — | env-fault → typed error (not result) | unit + cli | `TestDockerRunner_UnreachableSentinel`, `TestRunShell_MalformedProtocol`, `TestRunExec_Exit70` | ✅ 05-03 W3 | ⬜ pending |
| D-11 / D-12 | seccomp profile valid + loaded on both arches; arm64 under QEMU | EoP | multi-arch by-name profile loads | integration | QEMU leg: `docker buildx ... --platform linux/arm64` run of the negative tests | ✅ 05-04 W4 | ⬜ pending |
| Gate-3 mutation | `internal/sandbox/docker.go` go-mutesting ≥70% killed | — | the negative-test suite actually kills mutants | mutation (spot-check) | `go-mutesting internal/sandbox/docker.go` (GOFLAGS=-tags=sandbox_integration) ≥70%; recorded in quality-snapshot | ✅ 05-04 W4 | ⬜ pending |
| D-20 | curated baked package set imports + runs C-extensions under the hardened runtime (`import numpy,pandas`); build-time hash-pinned bake, no runtime pip | DoS (seccomp-fit) / Tampering (supply-chain) | batteries-included Python without weakening net-none/read-only floor | integration (positive) + build-smoke | `go test -tags sandbox_integration ./internal/sandbox/ -run TestRunner_BakedPackagesImport`; Dockerfile build-stage `import ...; print('baked-ok')` | ✅ 05-02/05-03 W2/W3 | ⬜ pending |

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

- [x] `internal/sandbox/docker_test.go` — `//go:build sandbox_integration` happy + negative + limit + sentinel tests + goleak `TestMain` (05-03 Task 1)
- [x] `internal/sandbox/errors.go` + unit tests for the two sentinels (`ErrSandboxUnreachable`, `ErrSandboxProtocol`) (05-03 Task 1)
- [x] `internal/agent/tools/execute_test.go` — lean-preview shaping (unit, fake `sandbox.Runner`) + deferred-spec + timeout-pass-through assertions (05-03 Task 2)
- [x] `cmd/aura/` exec CLI test (exit-code 70 path; reuse the re-exec subprocess pattern in `cmd/aura/agent_test.go`) (05-03 Task 3)
- [x] `scripts/sandbox_escape_bench.sh` — deterministic 18-scenario port (config-regression assertions + live escape-rate denominator) (05-04 Task 1)
- [x] CI DinD job: install `runsc`, write inner `daemon.json` (userns-remap + runsc), QEMU binfmt, export sandbox env (no-skip-as-green) (05-04 Task 2)

> **Wave-4 gate (NOT a Wave-0 deliverable):** the mutation spot-check measures a score against
> `internal/sandbox/docker.go`, which is implemented in Wave 3 (05-03 Task 1) — it cannot exist at
> Wave 0. The *test infrastructure* it scores (the negative-test tier) is the Wave-0 artifact above;
> the score itself is a post-Wave-3 Gate-3 obligation, recorded at Wave 4:
- [ ] Mutation spot-check (≥70% killed) target file: `internal/sandbox/docker.go` (CLAUDE.md Gate-3) — **Wave-4 gate** (05-04 Task 1 records the score in `docs/aura-quality-snapshot.md`; 05-04 Task 2 gates it in CI)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `internal/sandbox/docker.go` mutation spot-check ≥70% killed | Gate-3 (CLAUDE.md) | `go-mutesting` is WSL/CI-only (only fork supporting go1.26) + container-gated (needs `GOFLAGS=-tags=sandbox_integration` + the sandbox env) → not a per-commit unit gate, run at wave/Gate-3 | Run `go-mutesting internal/sandbox/docker.go` with `GOFLAGS=-tags=sandbox_integration` + the sandbox DSN/env exported (WSL); assert killed/total ≥70% (PASS=killed, FAIL=survived); record value + date in `docs/aura-quality-snapshot.md` alongside the db.go/budget.go rows. Gated live in CI (05-04 Task 2). |
| Real-DGX arm64 seccomp confirmation | D-12 | QEMU syscall emulation can diverge from a real arm64 kernel's seccomp behavior — only a real DGX kernel confirms | Before any production arm64 deployment, run the negative-test integration tier on real arm64 hardware; record result + date in `docs/aura-quality-snapshot.md`. Tracked obligation, not a per-merge gate. |
| LLM-driven SandboxEscapeBench red-team | D-03 | Cost (~$1/attempt) + non-determinism — separately tagged, scheduled/manual, NOT per-merge | Run the real Inspect AI harness + a frontier model out-of-band; feed the true capability number into the quality-snapshot when run. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (every row above maps to a created test/bench/CI/mutation artifact in a plan)
- [x] No watch-mode flags
- [x] Feedback latency < 5 s (unit tier)
- [x] Negative-test inventory: every boundary asserts the expected errno (no false-greens)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
