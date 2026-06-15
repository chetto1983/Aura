---
status: complete
phase: 05-sandbox-2a-stateless
source: [05-01-SUMMARY.md, 05-02-SUMMARY.md, 05-03-SUMMARY.md, 05-04-SUMMARY.md]
started: 2026-06-02T07:27:37Z
updated: 2026-06-02T07:45:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Cold Start Smoke Test
expected: Kill any running aura-sandbox. Recreate fresh. The image builds (hash-pinned pip bake + import smoke), the container starts, and `127.0.0.1:18901/healthz` returns healthy. Boots clean, no errors.
result: pass
evidence: |
  `docker compose up -d --force-recreate aura-sandbox` → Health=healthy at t+5s,
  Restarts=0, logs clean, `curl /healthz` → {"status": "ok"}. The prior 15-min-old
  container was unhealthy (pids/thread exhaustion from escape-bench fork-bomb probes);
  the cold start cleared it — confirming the cold-start contract.
caveat: Started under runtime=runc + the full seccomp/caps/net-none/read-only/pids floor (the PRD "portable floor"). The gVisor `runsc` overlay (CAP-01 SC#5 default-on x86) is OMITTED here — runsc is not installable on this Docker Desktop/WSL2 host; gVisor is CI-DinD-gated by design (05-04). Not a code defect.

### 2. Run Python in Sandbox (happy path)
expected: `aura exec python "print(2+2)"` prints 4, exit 0 (CAP-01 SC#1).
result: pass
evidence: stdout `4`, rc=0.

### 3. Run Shell in Sandbox
expected: shell route runs bash -c, prints output, exit 0.
result: pass
evidence: `aura exec shell "echo hello-from-shell"` → `hello-from-shell`, rc=0.

### 4. Baked Packages Available (D-20)
expected: curated build-time-baked packages import and C-extensions run; no runtime pip.
result: pass
evidence: `import numpy, pandas` → `numpy 2.4.6 pandas 3.0.3`; numpy C-extension `arange(10).sum()` → 45, rc=0.

### 5. Network Isolation (CAP-01 SC#3)
expected: socket/connect denied (net-none + seccomp EPERM).
result: pass
evidence: `socket.create_connection(("1.1.1.1",80))` → `BLOCKED PermissionError [Errno 1] Operation not permitted`. No egress.

### 6. Privileged Syscall Isolation (CAP-01 SC#2)
expected: ptrace/unshare/mount denied; host-file read denied.
result: pass
evidence: ptrace → `-1 EPERM`; unshare(CLONE_NEWUSER) → `-1 EPERM`; `mount -t tmpfs` → denied (rc=32, cap_drop). Confirmed across the live sandbox_integration tier too.

### 7. Timeout Enforcement
expected: long-running code hits timeout → exit 124 + limit_hit timeout.
result: pass
evidence: `AURA_SANDBOX_TIMEOUT_SEC=2` + `time.sleep(5)` → `exit_code: 124`, `[limit: timeout]`, 2002 ms, rc=124.

### 8. Sandbox-Down Clear Error (D-09)
expected: sidecar unreachable + auto-start fails → clear error + exit 70.
result: pass
evidence: dead URL `127.0.0.1:19999` → `aura exec: sandbox POST /exec/python: ... sandbox sidecar unreachable (auto-start failed)`, rc=70. Best-effort auto-start never mounts the docker socket.

### 9. execute Tool Available to Agent
expected: execute registered as the first Deferred:true tool (name+summary in manifest, schema via tool_search); runs sandboxed code through the shared Runner.
result: pass
evidence: |
  `aura tools` → `[deferred] execute — Run a Python or shell snippet in an isolated
  network-less sandbox.`; registered in buildRegistry; tool_search keyword+select tests
  green; execute_test.go + registry tests green; the shared DockerRunner+FormatLean path
  is the same one proven live in tests 2–8. Live LLM-driven turn is the OPENROUTER-gated
  cot_eval harness (not a phase-5 CLI deliverable).

### 10. Escape Bench Gate (CAP-01 SC#4/SC#5)
expected: escape-rate < 5% over live-denominator probes, config-regressions = 0, mutation ≥ 70%.
result: pass
evidence: |
  scripts/sandbox_escape_bench.sh (WSL, live sidecar): 14/14 runtime/kernel probes
  PASS-contained → escape-rate 0.0%; 4/4 config-regressions = 0; docker.go go-mutesting
  100% killed (25/25); 4 N/A kubernetes lines printed. Final: "OK: escape-rate < 5%,
  config-regressions = 0, ... docker.go mutation recorded." Snapshot row updated.
caveat: userns-remap not live on the Docker Desktop dev daemon → bench prints WARN (CI-only hard assertion per Pitfall 3). The gVisor runsc leg + the QEMU-arm64 leg are CI-DinD-gated. These are infrastructure assertions, not escape failures; the security substance (seccomp+caps+net-none+read-only+pids) is fully enforced and verified.

## Summary

total: 10
passed: 10
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none — all tests passed]

## CI-gated obligations (NOT UAT failures)

These are environment/infrastructure items that cannot run on a Docker Desktop/WSL2 dev
host and are gated live in `.github/workflows/sandbox.yml` (05-04) + the Gate-3
human-verify checkpoint. The local UAT verifies the full security substance under the
runc+seccomp portable floor; these add defense-in-depth + the production runtime:

- **gVisor `runsc` default-on x86 (CAP-01 SC#5 / amendment #36):** runsc not installable on Docker Desktop; verified by the CI DinD job under runtime=runsc.
- **userns-remap live on the daemon (Pitfall 3):** dev-daemon opt-in; the CI DinD inner daemon.json enables it (bench hard-fails on it under $CI).
- **QEMU-arm64 leg + real-DGX arm64 confirmation (D-12):** necessary-not-sufficient QEMU run in CI; real arm64 kernel is a tracked pre-production obligation.

CAP-01 remains formally OPEN until these CI/production proofs are signed off (per 05-04).
