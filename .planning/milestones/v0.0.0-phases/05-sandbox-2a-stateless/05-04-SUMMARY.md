---
phase: 05-sandbox-2a-stateless
plan: 04
subsystem: sandbox
tags: [sandbox, gate-3, escape-bench, sandboxescapebench, ci, dind, runsc, userns-remap, qemu, arm64, mutation, gvisor, no-skip-as-green]

# Dependency graph
requires:
  - phase: 05-sandbox-2a-stateless
    plan: 01
    provides: "PRD amendments — gVisor-primary D12 (#36), D-09 auto-start, D-20 build-time bake (#37)"
  - phase: 05-sandbox-2a-stateless
    plan: 02
    provides: "sidecar artifacts — Dockerfile/sidecar.py/seccomp.json + aura-sandbox compose service + gVisor overlay + make sandbox-up"
  - phase: 05-sandbox-2a-stateless
    plan: 03
    provides: "internal/sandbox/docker.go DockerRunner + execute tool + aura exec CLI + sandbox_integration tier"
provides:
  - "scripts/sandbox_escape_bench.sh — deterministic SandboxEscapeBench port: 14 runtime/kernel live-denominator probes + 4 config-regression assertions (must stay 0) + 4 explicit N/A kubernetes lines (auditable denominator, OQ1); emits escape-rate, asserts userns-remap live, runs the docker.go go-mutesting spot-check, writes both into docs/aura-quality-snapshot.md"
  - ".github/workflows/sandbox.yml — REQUIRED gating DinD job: runsc install + daemon.json (userns-remap+runsc) + QEMU arm64 + live sidecar + sandbox_integration tier + live bench + docker.go mutation (>=70%) + arm64 leg + 85% coverage fold (no-skip-as-green)"
  - "docs/aura-quality-snapshot.md — Sandbox escape-rate matrix row + Phase 5 detail section (escape rate / docker.go mutation / config-regressions=0 / QEMU-arm64 tracked obligation), live values CI-populated"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "deterministic-probe port of an LLM-driven CTF benchmark (no model driver): each escape technique is attempted directly against the live sidecar over the /exec/python wire and the boundary errno is the assertion — fast, repeatable, CI-gating (vs the D-03 scheduled/manual LLM red-team)"
    - "two-way scenario partition: config-regression (structurally-forbidden misconfigs, separate gate, must stay 0) vs live-denominator (runtime/kernel escapes, the escape-rate denominator) vs explicit N/A kubernetes (printed, never dropped — auditable per OQ1)"
    - "no-skip-as-green in a shell bench: a missing daemon/sidecar/go-mutesting prints a clear skip locally but exits non-zero under \\$CI so a skipped gate never reports falsely green"
    - "DinD: userns-remap on the daemon that runs the sidecar + runsc registered as a runtime; the workflow documents the --privileged-disables-seccomp + userns-on-inner-daemon traps inline"
    - "probe traverses the real boundary via curl from a sibling container sharing the sidecar's network namespace (--network container:aura-sandbox) rather than docker exec, so seccomp+net-none+read_only are all exercised"

key-files:
  created:
    - scripts/sandbox_escape_bench.sh
    - .github/workflows/sandbox.yml
    - .planning/phases/05-sandbox-2a-stateless/05-04-SUMMARY.md
  modified:
    - docs/aura-quality-snapshot.md

key-decisions:
  - "Scenario count reconciliation (RESEARCH OQ1 / Pitfall 6): the 18 conceptual scenarios are realized as 14 runtime/kernel live-denominator probes + 4 config-regression assertions on the Docker/kernel surface; the reference repo's 4 Kubernetes-layer scenarios are recorded as explicit N/A lines (no orchestrator in Aura's single-container deployment) so the escape-rate denominator is auditable and neither inflated nor deflated."
  - "Config-regressions are a SEPARATE gate, not in the escape-rate denominator (they are structurally forbidden so they would always be 0 and would artificially deflate the rate). escape-rate = escapes / applicable live-denominator scenarios (CAP-01 SC#4); config-regressions must stay 0 (CAP-01 SC#5)."
  - "The bench posts probes via a curl sibling container joined to the sidecar's netns (--network container:aura-sandbox) instead of `docker exec`, so the probe hits the real /exec/python wire and traverses seccomp + net-none + read_only exactly as model code would. docker exec would bypass the entrypoint hardening path."
  - "go-mutesting install path pinned to the SAME canonical module the Makefile tools target uses (github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest) — no new/alternative package introduced (package-legitimacy)."
  - "The escape-rate, the docker.go mutation score, and the userns-remap-live confirmation are LIVE numbers produced ONLY by the gating DinD run; the authoring environment has no Docker daemon, so the quality-snapshot live cells are seeded CI-populated/pending and the bench's write_quality_snapshot step replaces them in CI. They are NOT fabricated — they are exactly what the human-verify checkpoint signs off."

requirements-completed: []

# Metrics
duration: ~18min
completed: 2026-06-01
---

# Phase 5 Plan 04: Gate-3 Evidence Layer (Escape Bench + Gating DinD CI) Summary

**The Gate-3 evidence layer for CAP-01: a deterministic SandboxEscapeBench port (`scripts/sandbox_escape_bench.sh`) that probes 14 runtime/kernel escape techniques against the live sidecar (the escape-rate denominator), asserts 4 structurally-forbidden misconfigs stay at 0 (config-regression), prints 4 explicit N/A kubernetes lines so the denominator is auditable (OQ1), asserts userns-remap is live, runs the `internal/sandbox/docker.go` go-mutesting spot-check, and writes the escape-rate + mutation score + QEMU-arm64 caveat into `docs/aura-quality-snapshot.md`; plus the REQUIRED gating DinD workflow (`.github/workflows/sandbox.yml`) that installs runsc, writes the inner `daemon.json` (userns-remap + runsc), exports the composed sandbox env (no-skip-as-green), and runs the integration tier + live bench + docker.go mutation (>=70%) + QEMU arm64 leg, gating the merge.**

## Performance

- **Duration:** ~18 min
- **Completed:** 2026-06-01
- **Tasks:** 2 implementable (committed) + 1 human-verify checkpoint (awaiting sign-off)
- **Files created:** 2 / modified: 1

## Accomplishments

- **Task 1 — escape bench + quality-snapshot seed (`c3d90b0`):** `scripts/sandbox_escape_bench.sh` is a deterministic CI-gating port of the SandboxEscapeBench (arXiv 2603.02277). It copies the `llm_smoke.sh` header discipline (shebang, `set -euo pipefail`, git-root cd) but with an explicit WHY block stating it is CI-gating, not manual, and is no-skip-as-green. It partitions the scenarios three ways per RESEARCH Pitfall 6 / OQ1: 14 **live-denominator** runtime/kernel probes (ptrace, unshare-netns, unshare-userns, mount, socket egress, /proc/self/root host-file read, read-only-rootfs write, process_vm_readv, bpf, userfaultfd, kexec_load, setuid(0), fork-bomb/pids, cgroup release_agent) — each posted to the live `/exec/python` wire, counted as an escape only if the technique succeeds; 4 **config-regression** assertions (docker socket bind mount, --privileged, writable host bind mount, excess capabilities) inspected against the running container + compose.yaml and required to stay 0; and 4 explicit **N/A kubernetes** lines (kubelet/RBAC, service-account escalation, CRI-O/containerd-shim, hostPath/privileged DaemonSet) so the count is auditable and never silently dropped. It prints a per-scenario `<id> <name> <category> <verdict>` table, computes `escape-rate = escapes / applicable live-denominator`, FAILS on escape-rate >= 5% OR any config-regression > 0, asserts userns-remap is live in the daemon (Pitfall 3; hard-fail under `$CI`), runs the `internal/sandbox/docker.go` go-mutesting spot-check (>=70% killed, `GOFLAGS=-tags=sandbox_integration`, no-skip-as-green under `$CI`), and writes the escape-rate + docker.go mutation score + the QEMU-arm64 tracked-obligation note into `docs/aura-quality-snapshot.md` (matrix row + a new Phase 5 detail section). The snapshot was seeded with the CI-populated structure (live cells marked CI-populated/pending since no Docker daemon is available to produce them here).
- **Task 2 — gating DinD CI workflow (`c807553`):** `.github/workflows/sandbox.yml` is a REQUIRED gating job (matches the `ci.yml` checkout/setup-go conventions, `paths:` filtered to the sandbox surface) that installs gVisor `runsc` from the Google GPG-signed apt repo, writes the `daemon.json` `{userns-remap:default, no-new-privileges:true, runtimes.runsc}` and restarts dockerd asserting userns-remap is live (Pitfall 3), sets up QEMU arm64 binfmt + buildx (Pitfall 4), builds + starts `aura-sandbox` under the gVisor overlay and asserts the `runsc` runtime is actually intercepting, exports `AURA_SANDBOX_URL`/`_TIMEOUT_SEC`/`_RUNTIME` + `CI=true` so the integration tier does NOT skip (no-skip-as-green), runs `go test -tags sandbox_integration -race ./internal/sandbox/...` (the negative tier), runs `scripts/sandbox_escape_bench.sh` LIVE (escape-rate gate, not a parse-check), installs + runs the `internal/sandbox/docker.go` go-mutesting spot-check (>=70% killed, same `avito-tech` path as the Makefile), runs the arm64 sidecar build + a ptrace-EPERM probe under QEMU (necessary-not-sufficient, D-12), and folds sandbox coverage into the 85% floor via `coverage_gate.sh`. The top-of-file comment documents Pitfall 2 (inner sidecar keeps seccomp despite the --privileged outer DinD) and Pitfall 3 (userns-remap on the inner daemon, outer --userns=host).

## Task Commits

1. **Task 1: deterministic escape bench + quality-snapshot seed** — `c3d90b0` (feat)
2. **Task 2: gating sandbox DinD CI workflow** — `c807553` (feat)
3. **Task 3: Gate-3 human-verify checkpoint** — awaiting human sign-off (NOT committed; this plan is non-autonomous)

## Files Created/Modified

- `scripts/sandbox_escape_bench.sh` (~280 LOC) — deterministic 18-scenario port + mutation spot-check + snapshot writer
- `.github/workflows/sandbox.yml` (~200 LOC) — gating DinD job
- `docs/aura-quality-snapshot.md` (modified) — escape-rate matrix row + Phase 5 detail section

## Decisions Made

See the `key-decisions` frontmatter. Highlights: the 18 conceptual scenarios are realized as 14 live-denominator probes + 4 config-regression assertions + 4 explicit N/A kubernetes lines (auditable denominator, OQ1); config-regressions are a separate gate, NOT in the escape-rate denominator; probes traverse the real boundary via a curl sibling container joined to the sidecar netns (not `docker exec`); go-mutesting uses the Makefile's canonical `avito-tech` module path; the live numbers are produced only by the gating DinD run (seeded CI-populated/pending here, not fabricated).

## Deviations from Plan

**None functional — both implementable tasks executed as written.** One environment-driven adaptation, by design for this wave:

1. **Live numbers are CI-populated, not produced here.** The Docker daemon is NOT available in this execution environment (no `/var/run/docker.sock`), and the CI-only tooling (runsc, QEMU, go-mutesting, a live DinD) is absent. Per the wave scope, the bench + workflow are authored-to-spec and statically validated (`bash -n`, YAML parse, all grep gates, `go build ./...` green); the escape-rate, the userns-remap-live confirmation, and the docker.go mutation score are produced by the gating DinD run — which is exactly what the human-verify checkpoint (Task 3) signs off. The quality-snapshot live cells are seeded with explicit `CI-populated (pending)` markers (the bench's `write_quality_snapshot` step replaces them in CI). No live numbers were fabricated.

## Known Stubs

None. The `CI-populated (pending)` cells in `docs/aura-quality-snapshot.md` are not stubs — they are the structure the live gating DinD run populates via the bench's `write_quality_snapshot` step (the snapshot's own CI-gate contract treats the row as populated by the owner phase's first shippable PR). The 4 N/A kubernetes scenarios are explicit, auditable exclusions (OQ1), not stubs.

## Threat Flags

None. No new security-relevant surface beyond the plan's `<threat_model>`: the bench probes the existing sidecar boundary (05-02/05-03) and the workflow installs OS/container-level tooling from approved sources (runsc Google apt GPG-signed, qemu-user-static, go-mutesting via the Makefile's pinned module path). No new network endpoint, auth path, or schema change.

## Verification

### Statically verified in this environment (Docker daemon NOT available)

- `bash -n scripts/sandbox_escape_bench.sh` — PASS (syntax clean).
- Task 1 `<verify>` grep gate — PASS: `set -euo pipefail`, `escape-rate`, `N/A|kubernetes`, `config-regression`, `userns-remap`, `mutest|mutation`, `docker.go` all present in the script; `escape rate` + `mutation` present in `docs/aura-quality-snapshot.md`.
- `python3 -c "yaml.safe_load(...)"` on `.github/workflows/sandbox.yml` — PASS (valid YAML).
- Task 2 `<verify>` grep gate — PASS: `runsc`, `userns-remap`, `sandbox_integration`, `sandbox_escape_bench.sh`, `linux/arm64|qemu`, `AURA_SANDBOX_URL`, `mutest|mutation` all present.
- `go build ./...` — PASS (still green; no Go files touched this plan).
- File-size cap (CLAUDE.md <=600 LOC): bench ~280, workflow ~200 — both well under.
- `shellcheck` was NOT available in this environment; `bash -n` is the syntax signal. The script avoids common shellcheck pitfalls (quoted expansions, `|| true` on grep counts, `set -euo pipefail`).

### DEFERRED to the gating CI DinD run (05-04 Task 3 human-verify) — Docker-gated, CANNOT run here

The Docker daemon + runsc + QEMU + go-mutesting + a live DinD are not available in this environment, so these are authored-to-spec but NOT live-verified here — they are the explicit content of the human-verify checkpoint:

- The LIVE escape-rate (`scripts/sandbox_escape_bench.sh` run against the running sidecar) — escape-rate < 5%, config-regressions = 0, the per-scenario table with N/A kubernetes lines, and the quality-snapshot row populated with today's date + the measured value.
- userns-remap LIVE in the inner DinD daemon (`docker info | grep userns`) — Pitfall 3, the single most likely CI-setup failure.
- The `sandbox_integration` negative tier (ptrace/socket/unshare/proc-root → EPERM/ENOENT through the runner) running green under the live boundary.
- The `internal/sandbox/docker.go` go-mutesting spot-check >= 70% killed, recorded in the quality-snapshot.
- The QEMU arm64 leg (multi-arch seccomp profile loads; ptrace denied) — necessary-not-sufficient, the real-DGX arm64 confirmation remains a tracked obligation (D-12).
- `make quality-full` combined coverage >= 85% with sandbox folded in.

**CAP-01 is NOT marked complete** — its success criteria now have the full evidence layer authored, but the live boundary proof + the Gate-3 numbers are produced by the gating DinD run and signed off at the human-verify checkpoint (verifier's call after sign-off).

## Self-Check: PASSED

- `scripts/sandbox_escape_bench.sh`, `.github/workflows/sandbox.yml` — present on disk; `docs/aura-quality-snapshot.md` modified (escape-rate row + Phase 5 detail section).
- Commits `c3d90b0`, `c807553` — present in git log.
- `go build ./...` — green.

---
*Phase: 05-sandbox-2a-stateless*
*Completed (implementable tasks): 2026-06-01 — Task 3 human-verify checkpoint pending*
