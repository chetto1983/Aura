---
phase: 37-per-user-full-capability-sandbox
plan: 08
subsystem: infra
tags: [sandbox, adr, docker, docker-socket-proxy, egress, gvisor, benchmark, soak, concurrency, compose, docker_integration]

# Dependency graph
requires:
  - phase: 37-per-user-full-capability-sandbox
    provides: "the SBX-04 egress-default amendment + gVisor⊥nat #934 note + AURA_SANDBOX_* config surface (37-01)"
  - phase: 37-per-user-full-capability-sandbox
    provides: "DockerBackend + Resolve/Suspend/Resume/Stop lifecycle over moby v0.4.1 (37-04)"
  - phase: 37-per-user-full-capability-sandbox
    provides: "SandboxRouter + buildSandboxRouter (client.FromEnv, Strict()-gated) + specFor (37-05)"
  - phase: 37-per-user-full-capability-sandbox
    provides: "EgressSidecar + ErrRunscFQDNMutualExclusion filter-floor/FQDN split (37-06)"
provides:
  - "docs/adr/0037-per-identity-docker-sandbox.md — the SBX-05 ADR: D-15 (container-per-identity over Docker on the mini-PC; K8s+agent-sandbox+gVisor-default reserved for DGX-Spark; E2B/agent-sandbox template-claim forward shape) + the three accepted residuals (daemon Docker socket/Open Q5, gVisor⊥nat FQDN/#934, D-06 egress amendment)"
  - "compose.yaml: profiles:[sandbox] tecnativa/docker-socket-proxy fronting /var/run/docker.sock (read-only into the proxy ONLY, never a box), aura DOCKER_HOST (Strict()-gated empty no-op default) + digest-pinnable AURA_SANDBOX_IMAGE/AURA_SANDBOX_EGRESS_IMAGE refs"
  - "internal/sandbox/usersandbox/bench_soak_test.go — the D-14 concurrency-soak harness (concurrent Resolve p95, concurrent Resume-from-suspend p95, aggregate-RAM-vs-32GB envelope, cgroup-cap starvation probe) gated on AURA_SANDBOX_SOAK_REALHOST=1"
  - "37-VALIDATION.md Manual-Only D-14 run instructions + Gate-3 results table"
affects: [37-verify, deployment, ops]

# Tech tracking
tech-stack:
  added: ["tecnativa/docker-socket-proxy:0.3.0 (compose, profiles:[sandbox] — narrowed Docker API for the daemon)"]
  patterns:
    - "profiles:[sandbox]-gated Docker-socket narrowing: raw socket read-only into a proxy only (never aura, never a box), aura reaches it via DOCKER_HOST=tcp://docker-socket-proxy:2375 (Open Q5)"
    - "real-host-only benchmark gate: pass/fail assertions run ONLY under an explicit AURA_SANDBOX_SOAK_REALHOST=1 opt-in; dev/CI SKIP with a real-host message (T-37-08-FALSEBENCH — no false green on the 15.47 GiB cap)"
    - "host-level RAM envelope via /proc/meminfo MemAvailable delta (the 32GB-fit question is a host-aggregate, not a per-container RSS sum)"
    - "detached-burner starvation probe: a co-tenant's exec p95 stays bounded while a hog box saturates its own cgroup CPU quota"

key-files:
  created:
    - docs/adr/0037-per-identity-docker-sandbox.md
    - internal/sandbox/usersandbox/bench_soak_test.go
  modified:
    - compose.yaml
    - .planning/phases/37-per-user-full-capability-sandbox/37-VALIDATION.md

key-decisions:
  - "Compose grants Docker API access via a profiles:[sandbox]-gated docker-socket-proxy (the T-37-08-SOCKET mitigate disposition / Open Q5 recommended narrowing), not a raw socket mount into aura — the raw socket is the documented fallback (ADR Residual A). Profile-gating + an empty DOCKER_HOST default keep the default dev stack unchanged."
  - "The soak harness measures the host-aggregate RAM envelope via /proc/meminfo MemAvailable delta (not a moby per-container stats stream) — the D-14 pass question is 'does the whole set fit 32GB with headroom', a host-level measurement, and it avoids depending on an uncertain moby stats JSON schema (NEVER-SUPPOSE)."
  - "The soak is a plain SKIP without AURA_SANDBOX_SOAK_REALHOST=1 (including under $CI) — a DELIBERATE exception to no-skip-as-green, because T-37-08-FALSEBENCH requires the 15.47 GiB dev/CI cap to never produce a pass/fail verdict for the 10–20-box envelope. The functional docker_integration tests keep their t.Fatal-under-CI gate; only this real-host bench is realhost-gated."
  - "Per-box caps + N + headroom are sourced from the same AURA_SANDBOX_* knobs the daemon reads (D-14 defaults 2 CPU/2GiB/512 pids), so the harness measures the DEPLOYED caps, not a fixture."

patterns-established:
  - "ADR home established at docs/adr/ (first entry, 0037) — Context/Decision/Consequences/Residuals/Alternatives/Forward-path, the SBX-05 + OPS-06 deployment-decision record."
  - "Docker-socket narrowing as compose infra (socket-proxy), gated so it is opt-in for strict-profile production only."

requirements-completed: [SBX-05]

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "The SBX-05 ADR records D-15 (container-per-identity over Docker on the mini-PC; K8s+agent-sandbox+gVisor-default reserved for the DGX-Spark tier; the Backend/SandboxSpec mirror the E2B/agent-sandbox template-claim shape) and all three accepted residuals (daemon Docker socket/Open Q5, gVisor⊥nat FQDN/#934, D-06 egress amendment)."
    requirement: "SBX-05"
    verification:
      - kind: other
        ref: "test -f docs/adr/0037-per-identity-docker-sandbox.md && grep -qi 'container-per-identity' && grep -qi 'DGX' && grep -qi 'socket' && grep -qi 'E2B' && grep -qi '934|gVisor'"
        status: pass
    human_judgment: true
    rationale: "grep proves the required terms + all three residuals are present, but a reviewer should confirm the ADR faithfully captures the D-15 intent + the DGX-tier reservation as a deployment-posture decision (a planning/architecture adequacy call, not a machine assertion)."
  - id: D2
    description: "compose.yaml gives the aura daemon Docker API access to spawn boxes (docker-socket-proxy fronting the socket, DOCKER_HOST wiring) and references the digest-pinnable sandbox + egress images; the raw socket is granted to the proxy ONLY, never a box (SBX-02); the change is a Strict()-gated no-op under dev/local_trusted."
    requirement: "SBX-05"
    verification:
      - kind: integration
        ref: "docker compose --env-file <populated> -f compose.yaml config -q (EXIT=0) + no box service definition mounts /var/run/docker.sock"
        status: pass
    human_judgment: false
  - id: D3
    description: "The D-14 concurrency-soak harness compiles + runs, measures Resolve p95 / Resume p95 / aggregate RAM / a starvation verdict, and without AURA_SANDBOX_SOAK_REALHOST SKIPS with the real-32GB-host-only message (no false pass on the 15.47 GiB WSL/CI cap)."
    requirement: "SBX-05"
    verification:
      - kind: integration
        ref: "internal/sandbox/usersandbox/bench_soak_test.go#TestSoak_ConcurrentIdentities (go vet -tags docker_integration + go test -run TestSoak → SKIP with the real-host message)"
        status: pass
    human_judgment: false
  - id: D4
    description: "The D-14 soak PASS/FAIL envelope (10–20 boxes fit 32GB + headroom, Resolve p95 <~2s, Resume p95 <~1s, no co-tenant starvation) on the real 32GB host — the Gate-3 evidence recorded in 37-VALIDATION.md Manual-Only."
    requirement: "SBX-05"
    verification:
      - kind: manual_procedural
        ref: "37-VALIDATION.md Manual-Only D-14 run instructions + results table (AURA_SANDBOX_SOAK_REALHOST=1 on the 32GB host)"
        status: unknown
    human_judgment: true
    rationale: "The pass verdict requires the real 32GB host; dev WSL (15.47 GiB) cannot validate the envelope (T-37-08-FALSEBENCH). The harness + instructions are shipped; the operator runs it on the appliance at phase validation and fills the Gate-3 table."

# Metrics
duration: ~35 min
completed: 2026-07-06
status: complete
---

# Phase 37 Plan 08: SBX-05 ADR + D-14 concurrency-soak Summary

**The SBX-05 ADR (0037) recording container-per-identity-over-Docker for the mini-PC — with K8s + agent-sandbox + gVisor-as-default reserved for the DGX-Spark tier and the E2B/agent-sandbox template-claim forward shape — plus its three accepted residuals (the daemon's Docker-socket surface narrowed by a docker-socket-proxy, the gVisor⊥nat FQDN mutual-exclusion #934, and the D-06 egress amendment); compose wiring that fronts `/var/run/docker.sock` with a `profiles:[sandbox]` socket-proxy (never into a box) and references the digest-pinnable sandbox + egress images; and the D-14 concurrency-soak harness (concurrent Resolve/Resume p95, aggregate-RAM-vs-32GB envelope, cgroup-cap starvation probe) gated real-32GB-host-only so the 15.47 GiB dev/CI cap can never false-pass.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-07-06
- **Tasks:** 2
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- **The SBX-05 ADR is the phase's forward-compat contract, grounded in what shipped.** `docs/adr/0037-per-identity-docker-sandbox.md` (the first entry under a new `docs/adr/` home) records D-15: Aura runs a **container-per-identity sandbox directly over the Docker Engine API** on the mini-PC — an idempotent `ContainerCreate` keyed on identity, NOT a Kubernetes deployment and NOT an `agent-sandbox` warm-pool `SandboxClaim`. **K8s + `agent-sandbox` + gVisor-as-default are explicitly reserved for the DGX-Spark multi-node tier**, and the `Backend`/`SandboxSpec` seam **mirrors the E2B / agent-sandbox template-and-claim shape** so the DGX tier swaps the backend behind the same interface unmodified (the D-01 forward-bet). The ADR is written from the 37-01/37-04/37-06 SUMMARYs (image/config posture, DockerBackend lifecycle, egress split), not speculation.
- **All three research residuals are recorded, each with its compensating control.** (A) the daemon holds Docker API access — narrowed via `tecnativa/docker-socket-proxy` to the lifecycle subset, **never mounted into a box** (SBX-02, Open Q5); (B) the **gVisor⊥nftables-nat FQDN mutual-exclusion** — the filter-floor is always-on under both runtimes, the FQDN allowlist is `runc`-only and refused with `ErrRunscFQDNMutualExclusion` under `runsc` (#934); (C) the **D-06 full-internet-minus-internal egress default** (cross-ref SBX-04). Plus a Consequences table, an Alternatives-Considered table (K8s/gVisor-default/Firecracker/no-isolation/warm-pool — each with a verdict), and the DGX forward-path.
- **The aura daemon gets production Docker access without holding raw root-equivalent socket.** `compose.yaml` adds a `profiles:[sandbox]`-gated `docker-socket-proxy` that mounts `/var/run/docker.sock` **read-only into the proxy alone** and exposes only `CONTAINERS/IMAGES/VOLUMES/NETWORKS/EXEC/POST`, internal-network only (no host publish). The `aura` service gets `DOCKER_HOST` (empty default) + digest-pinnable `AURA_SANDBOX_IMAGE`/`AURA_SANDBOX_EGRESS_IMAGE` refs. Because `buildSandboxRouter` is `Strict()`-gated (it returns a nil host-direct router and never builds the Docker client under a non-strict profile — 37-05), the whole surface is a **genuine no-op under dev/local_trusted**: the default `docker compose up` neither starts the proxy nor uses the socket. `docker compose config` validates green.
- **The D-14 concurrency envelope has a real harness with a false-green-proof gate.** `bench_soak_test.go` (`docker_integration`) resolves N (10–20) per-identity boxes **concurrently** (Resolve p95), suspends then resumes them concurrently (Resume-from-suspend p95), samples the **aggregate RAM footprint against the 32 GB envelope** via `/proc/meminfo`, and runs a **starvation probe** (a box saturating its own cgroup CPU quota must not push a co-tenant's exec p95 past the bound). The pass/fail assertions run **only** with `AURA_SANDBOX_SOAK_REALHOST=1`; without it (dev WSL / ordinary CI) it SKIPS with a real-host-only message — the 15.47 GiB cap can never produce a false verdict for the 10–20-box envelope (T-37-08-FALSEBENCH).
- **The Gate-3 evidence slot is ready to fill.** `37-VALIDATION.md` Manual-Only now carries the exact D-14 run command (env knobs + the `-run TestSoak` invocation) and a results table for the 32 GB-host run, plus the SBX-05 Per-Task rows backfilled (ADR present; soak harness present).

## Task Commits

Each task was committed atomically (real pre-commit hooks — vet + gofmt + file-size, no `--no-verify`):

1. **Task 1: SBX-05 ADR 0037 + compose daemon-Docker access (Open Q5)** — `7082a95b` (docs)
2. **Task 2: D-14 concurrency-soak harness + Gate-3 Manual-Only table** — `a4a1de08` (test)

## Files Created/Modified

- `docs/adr/0037-per-identity-docker-sandbox.md` (new) — the SBX-05 ADR (D-15 + 3 residuals + alternatives + DGX forward-path).
- `internal/sandbox/usersandbox/bench_soak_test.go` (new) — the D-14 soak harness (`docker_integration`, real-host-gated).
- `compose.yaml` (mod) — `docker-socket-proxy` service (`profiles:[sandbox]`) + aura `DOCKER_HOST`/`AURA_SANDBOX_IMAGE`/`AURA_SANDBOX_EGRESS_IMAGE`.
- `.planning/phases/37-per-user-full-capability-sandbox/37-VALIDATION.md` (mod) — D-14 Manual-Only run instructions + Gate-3 results table + SBX-05 row backfill.

## Decisions Made

- **Socket-proxy over a raw socket mount into aura** — the T-37-08-SOCKET `mitigate` disposition / Open Q5 recommended narrowing (raw socket is the documented Residual-A fallback). Profile-gated so the dev stack is untouched.
- **Host `/proc/meminfo` for the RAM envelope** — the 32GB-fit question is a host-aggregate; avoids an uncertain moby stats JSON schema (NEVER-SUPPOSE).
- **Real-host-only SKIP (a deliberate no-skip-as-green exception)** — T-37-08-FALSEBENCH requires the dev/CI cap to never yield a verdict; only this bench is realhost-gated (the functional `docker_integration` tests keep their `t.Fatal`-under-CI gate).
- **Caps/N/headroom from the deployed AURA_SANDBOX_* knobs** — the harness measures the real D-14 caps, not a fixture.

## Deviations from Plan

None — plan executed exactly as written. The compose wiring chose the socket-proxy (the plan's explicitly-offered "or wire the socket-proxy" option and the threat register's `mitigate` disposition) over the raw-socket alternative; both were sanctioned by the plan, so this is a design selection within scope, not a deviation.

## Known Stubs

None. `AURA_SANDBOX_EGRESS_IMAGE` is a forward reference (the composition-root `WithEgress` wiring is a later plan's concern — it is NOT read by the current binary), documented as such in the compose comment and the ADR; it is an operator digest-pin point, not a stub that blocks SBX-05's goal (the ADR + the daemon Docker access + the D-14 harness are all delivered).

## Threat Flags

None — no new trust-boundary surface beyond the plan's `<threat_model>`. The compose change implements exactly the daemon→dockerd narrowing (T-37-08-SOCKET) the register assigns `mitigate`; the harness implements the T-37-08-STARVE (starvation probe) and T-37-08-FALSEBENCH (realhost gate) mitigations. No new endpoint, auth path, or schema change.

## Issues Encountered

- **The live D-14 soak run is deferred to the real 32GB host (by design).** dockerd is unreachable in this Windows worktree (npipe is not stdlib-dialable) and the dev cap is 15.47 GiB, so `TestSoak` SKIPs locally with the real-host-only message. The harness compiles under `-tags docker_integration`, vets clean, and the SKIP is verified; the PASS/FAIL envelope run is the Gate-3 Manual-Only item on the appliance.

## User Setup Required

None — no external service configuration. To run the D-14 soak an operator sets `AURA_SANDBOX_SOAK_REALHOST=1` (+ optional `AURA_SANDBOX_TEST_IMAGE`/`AURA_SANDBOX_SOAK_N`) on the 32GB host per the 37-VALIDATION.md instructions. To enable the narrowed production Docker access, an operator runs `docker compose --profile sandbox up -d` under a strict profile and sets `AURA_SANDBOX_DOCKER_HOST=tcp://docker-socket-proxy:2375`.

## Next Phase Readiness

- **SBX-05 is closed:** the ADR records the decision + forward-compat shape + residuals, the aura daemon can spawn boxes in production (socket-proxy, never a box), and the D-14 concurrency benchmark exists with its real-32GB-host run as the recorded Gate-3 evidence.
- **Phase verification / Gate-3:** the D-14 soak on the 32GB host is the one remaining Manual-Only fill (harness + instructions shipped); the egress DROP + gVisor `runsc` smoke remain the other native-Linux Manual-Only items from 37-06.
- Blockers: none.

## Self-Check: PASSED

- Created files exist: `docs/adr/0037-per-identity-docker-sandbox.md`, `internal/sandbox/usersandbox/bench_soak_test.go` — both FOUND. Modified: `compose.yaml`, `37-VALIDATION.md` — both present.
- Task commits exist: `7082a95b`, `a4a1de08` — both FOUND in `git log`.
- Acceptance re-run green: ADR greps (container-per-identity / DGX / socket / E2B / #934|gVisor / egress-residual) all pass; `docker compose --env-file <populated> -f compose.yaml config -q` EXIT=0 with the raw socket mounted read-only into the proxy ONLY (no box service mounts it); `go vet -tags docker_integration ./internal/sandbox/usersandbox/` clean; `go build ./...` green; `go test -tags docker_integration -run TestSoak` SKIPs with the real-host-only message; untagged package unit tests green. No file > 600 LOC.

---
*Phase: 37-per-user-full-capability-sandbox*
*Completed: 2026-07-06*
