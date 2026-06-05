---
spike: 010
name: sandbox-gvisor-runsc
type: standard
validates: "Given runsc installed, when the sandbox runs python/uv under gVisor, then isolation holds and the workload survives — OR the dev stack blocks it (documented)"
verdict: PARTIAL
related: [008, 009]
tags: [sandbox, gvisor, runsc, isolation, hardening, phase-8]
---

# Spike 010: sandbox-gvisor-runsc

## What This Validates

The strong-isolation tier of the A+C recommendation: run the sandbox under gVisor (`runsc`) so model-authored code hits a user-space kernel, not the host kernel. Phase 8 specced "gVisor-primary x86" (amendment #36) then lost the overlay in the sandbox-agent pivot. The real risk: does gVisor's syscall emulation break the python/uv workload?

## Results

**PARTIAL — gVisor runs the workload fine; it just can't ride the Docker Desktop dev stack.**

| Probe | Result |
|---|---|
| Docker Desktop `--runtime=runsc` | **`unknown or invalid runtime name: runsc`** — not installable on Docker Desktop |
| WSL `docker` daemon | = Docker Desktop's (runtimes: runc/nvidia only); no native dockerd |
| `runsc` install in WSL (release-20260601.0, sha512 OK) | clean |
| `runsc --network=none do echo` | `GVISOR-OK` |
| `runsc do python3` (tempfile + os.urandom + sha256 + cpu_count) | **`PYTHON-GVISOR-OK aaef074df115 cpus 8`** — syscall-heavy python survives gVisor |

## The boundary (load-bearing finding)

gVisor is a **native-Linux-only** tier on this project:
- **Docker Desktop (dev) CANNOT run it** — the daemon has no `runsc` runtime and can't be given one through Docker Desktop. Same class as the CAP-02 Docker Desktop blocker (memory `reference_sandbox_live_gate3_docker_desktop_blockers`).
- **Native Linux / WSL native dockerd / CI / the DGX prod box CAN** — `runsc` installs in seconds and the python workload runs clean under it (proven via `runsc do`, which sandboxes without needing dockerd at all).

So gVisor is correct as the **prod/CI isolation tier**, not a dev-stack default — exactly Phase-8 D-05/D-06's "gVisor primary x86 / hardened-container+seccomp portable floor / arm64 fallback" posture. The dev box keeps runc; CI + prod get `runsc`.

## Plan obligations

1. **Restore the Phase-8 gVisor overlay for the sandbox-agent image** — a `compose.gvisor.yaml` adding `runtime: runsc` to `aura-sandbox-agent`, applied on native-Linux/CI only (lost in the pivot; the Dockerfile survived but the runtime overlay didn't).
2. **The portable floor (runs everywhere incl. Docker Desktop) is the real Phase-11 dependency**: tightened seccomp + `no-new-privileges` (already set) + read-only rootfs + the token auth (008) + egress allowlist (009). gVisor is the prod bonus tier on top.
3. CI gates gVisor live (DinD with runsc, the Phase-8 sandbox.yml pattern); dev asserts the seccomp/auth/egress floor. No-skip-as-green: the gVisor leg `t.Fatal`s under `$CI` if runsc is absent.

This is a **Phase-8 sandbox-hardening regression**, broader than skills — track it against Phase 8, not inside Phase 11.
