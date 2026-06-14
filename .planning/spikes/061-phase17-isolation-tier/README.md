---
spike: 061
name: phase17-isolation-tier
type: standard
validates: "Given plain container vs gVisor runsc vs Kata/Firecracker vs Docker Sandboxes sbx vs k8s platforms, when weighed against the SPEC hard constraints (single-host, Docker-only, no-k8s, mini-PC, arm64 DGX, no host docker socket), then the Phase-17 box model + the compose.gvisor.yaml wiring + the PRD-amendment call are decided"
verdict: VALIDATED
related: [010, 059, 060, 062]
tags: [phase-17, packaging, isolation, gvisor, microvm, box-model, decision, prd-amendment]
---

# Spike 061: phase17-isolation-tier (decision)

## What This Validates

The capstone decision the operator redirected discuss-phase 17 toward: **which isolation model
should the Phase-17 box adopt**, and **does it force a PRD-amendment** to the SPEC's container
model? Synthesizes spikes 059 (fat-box parity + host-edge), 060 (fat image base), 010 (gVisor
runsc, prior art), 062 (Docker Sandboxes sbx), and the k8s landscape (agent-sandbox /
agent-infra-sandbox / Alibaba OpenSandbox).

## The hard constraints (from 17-SPEC.md, non-negotiable)

Single-host · **Docker-only host** · **k8s/Helm OUT of scope** · mini-PC 16-core/32GB (tight CPU
budget) · **DGX appliance is arm64** · **host Docker socket never mounted** · full `shell_exec` +
skills self-extension + `mcp-neo4j-cypher` parity (the box is Aura's computer, not a jail).

## Decision matrix

| Model | Host isolation | Full parity inside | Docker-only single-host | arm64 (DGX) | Docker-Desktop dev | mini-PC cost | Verdict |
|---|---|---|---|---|---|---|---|
| **Distroless+cap_drop+read_only+nonroot** (audit `ec7fe2f6`) | medium | **❌ breaks shell_exec/self-extend/MCP** (059) | ✅ | ✅ | ✅ | light | **❌ REVERT** |
| **Plain fat container** (SPEC "not a jail") | namespaces only | ✅ (059) | ✅ | ✅ | ✅ | light | **✅ BASELINE — default everywhere** |
| **gVisor `runsc`** (compose.gvisor.yaml) | **strong** (userspace kernel, syscall intercept) | ✅ workload survives (010) | ✅ Docker-runtime drop-in, **no KVM** | ✅ runsc builds arm64 | ❌ can't run on Docker Desktop (010/059) | low-moderate | **✅ OPTIONAL APPLIANCE TIER** |
| Kata / Firecracker microVM | strongest (own kernel) | ✅ | needs a runtime + **KVM** | arm64 via KVM | ❌ | higher (per-VM RAM) | ⚠️ heavier than gVisor for one box |
| **Docker Sandboxes `sbx`** (Firecracker) | strongest + docker-in-VM | ✅ | ✅ but **x86_64-only** | **❌ arm64 = macOS-only** | ⚠️ | "crippling" perf + `sbx login` | ❌ appliance (062); ✅ dev/x86_64 option |
| agent-sandbox / OpenSandbox | strong | ✅ | **❌ k8s** | varies | ❌ | cluster | ❌ OUT (SPEC: no k8s) |

## Results

**VALIDATED — Phase-17 box model = fat full-power baseline + optional gVisor appliance tier; the audit lockdown is reverted; a small PRD-amendment is required.**

### The model
1. **Baseline box (default — dev *and* appliance, x86_64 *and* arm64):** the **fat, writable,
   full-power container** — root-of-its-box, `shell_exec` + self-extension + `mcp-neo4j-cypher`
   parity, **no** `cap_drop`/`read_only`/distroless/forced-non-root. The portable host edge is
   free: no host docker socket + host fs invisible + `cpus`/`mem`/`pids` limits (all proven on
   plain runc, spike 059). This **is** the SPEC's "Aura's computer, not a jail."
2. **Optional strong-isolation tier (appliance, native-Linux incl. arm64):** a
   **`compose.gvisor.yaml`** override adding `runtime: runsc` to the `aura` service. gVisor is the
   *only* strong-isolation option that is simultaneously a Docker-runtime drop-in (Docker-only
   host ✓), **transparent to the workload** (full parity inside — Aura still sees a full Linux,
   spike 010), arm64-capable, and **KVM-free**. It gives "thick walls without a jail." **Not**
   applied on Docker-Desktop dev (can't host runsc — spikes 010/059); the dev box runs the plain
   baseline.
3. **Docker Sandboxes / microVM:** documented as the **direction-signal** and an *optional*
   x86_64-Linux power-user / dev wrapper — **not** the appliance runtime (arm64 gap + coding-agent
   shape + perf + `sbx login`, spike 062). Its best idea — **docker-in-box without the host
   socket** — is a future capability that could safely lift SPEC Req 3 (docker-runtime MCP inside
   the box) without ever mounting the host socket.

### Revert obligation
**`ec7fe2f6` (2026-06-12) must be reverted** for the `aura` service: drop the distroless root
`Dockerfile`, and remove `cap_drop: ALL` + `read_only: true` + `user 65532` from the compose
service. It was added two days *after* the SPEC locked "not a jail," imports the deleted per-call
sandbox's ephemeral-box posture onto the stateful self-modifying whole-agent box, and structurally
breaks Aura's primary execution surface (spike 059). The SPEC already pre-authorizes this (the
"accepted residual, operator's informed choice 2026-06-10").

### PRD-amendment call — YES (small)
A SPEC/PRD amendment is required before implementation because gVisor was not in the locked SPEC:
- **Add** the optional **gVisor appliance-isolation tier** (`compose.gvisor.yaml`, `runtime: runsc`,
  native-Linux/arm64, off on dev). Frame it explicitly as a *transparent isolation boundary*, NOT
  capability-stripping — so it is consistent with the SPEC's "no internal lockdown" (the workload
  keeps full caps + full Linux; only the host gets thicker walls).
- **Note** the revert of `ec7fe2f6` and correct the SPEC Background (which still claims "no aura
  Dockerfile / no aura compose service").
- **Record** Docker Sandboxes (sbx) as evaluated-and-deferred (arm64 + shape), plus the
  docker-in-box-without-host-socket future capability.

These decisions + the amendment flag feed directly back into **17-CONTEXT.md** (paused discuss-phase 17).
