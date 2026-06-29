---
spike: 079
name: agent-sandbox-api-contract
type: standard
validates: "Given agent-sandbox/agent-sandbox + kubernetes-sigs/agent-sandbox, when their Sandbox/Template/Claim lifecycle is extracted, then Aura's internal/sandbox/usersandbox Go-over-Docker contract mirrors it so a future DGX K8s backend is a transport swap, not a redesign"
verdict: VALIDATED
related: [062, 078]
tags: [sandbox, agent-sandbox, k8s, api-contract, forward-compat, phase-37, v2.0.0]
---

# Spike 079: agent-sandbox API Contract Mapping

## What This Validates

The operator pointed to `github.com/agent-sandbox/agent-sandbox` as the per-identity sandbox model. This spike extracts the **lifecycle/API contract** Aura's `internal/sandbox/usersandbox` should mirror, so the mini-PC Docker-direct implementation (078) and a future DGX-Spark K8s backend share one interface — the migration becomes a transport swap, not a redesign.

## Research (cited)

Prior research already established the verdict; this spike formalizes the contract. Sources: `docs/research/senior-dev-agent-hardening-2026.md`, the Kubernetes.io agent-sandbox blog (2026-03-20), the Google OSS blog (2025-11), `kubernetes-sigs/agent-sandbox`, and prior spike 062 (Docker-Sandboxes/Firecracker = direction-signal, not the appliance runtime).

| Concern | agent-sandbox (K8s) | Aura mini-PC (Docker-direct, 078) |
|---|---|---|
| Unit | `Sandbox` CRD = one stateful pod, stable identity + persistent storage | one container per identity + per-identity named volume |
| Blueprint | `SandboxTemplate` (base image, resource limits, security policy) | a Go `SandboxSpec` (image, limits, runtimeClass, egress) |
| Provisioning | `SandboxClaim` (transactional request, claims from warm pool) | `Router.Resolve(identityID)` → get-or-create container |
| Cold-start mitigation | `SandboxWarmPool` (pre-warmed pods) | **skip on one box** (trades idle compute for latency; 078 idle cost is already ~0) |
| Isolation | `runtimeClassName`: gVisor / Kata | `runtime: runsc` via `compose.gvisor.yaml` (061), `server_production` only |

**Verdict basis:** K8s is **mandatory** for agent-sandbox itself (Sandbox/Template/Claim are CRDs requiring a control plane); its design center is multi-node / tens-of-thousands of sandboxes. On a single 16-core appliance that is overhead without payoff — confirmed by 062 and the online research. The *pattern* (declarative per-identity stateful box with pluggable isolation runtime) is what Aura adopts, over Docker.

## Proposed Aura Go contract (`internal/sandbox/usersandbox`)

```go
type SandboxSpec struct {
    IdentityID string        // identityctx principal — the tenant key
    Image      string        // fat box image (060), or per-profile override
    Workspace  string        // per-identity named-volume mount target
    RuntimeClass string      // "" (runc) | "runsc" (gVisor) — server_production policy
    Egress     EgressPolicy  // none (default) | allowlist (009)
    Limits     Resources     // cpu/mem/pids (059 edge already enforces)
    // host-exposure flags are NOT representable: no socket, no privileged, no host-net, no host bind (SBX-02)
}

type Sandbox interface {
    Resolve(ctx context.Context, id string) (BoxHandle, error) // get-or-create per identity (≈ SandboxClaim)
    Exec(ctx context.Context, h BoxHandle, cmd Command) (Result, error)
    Stop(ctx context.Context, h BoxHandle) error               // idle-TTL or explicit
    // backend is swappable: DockerBackend now, K8sBackend (agent-sandbox CRDs) for DGX
}
```

`SandboxSpec` field-maps onto `SandboxTemplate`; `Resolve` onto `SandboxClaim`; idle-TTL Stop onto scheduled-delete. A future `K8sBackend` translates the same spec into a `Sandbox` CRD — no caller change.

## Results

**VALIDATED ✓** (design): the contract above mirrors agent-sandbox's shape while running over Docker (078). K8s + `kubernetes-sigs/agent-sandbox` is documented as the **DGX multi-node tier** (DGX-01 in REQUIREMENTS Future). Warm pools intentionally omitted on one box.

**Signal for the build (Phase 37):** implement `usersandbox` with a `Backend` seam (Docker now), `SandboxSpec` mirroring `SandboxTemplate`, `Resolve` ≈ `SandboxClaim`. SBX-05 ADR records this Docker-now / K8s-DGX-later decision. Open: whether to vendor agent-sandbox's CRD types now for the forward-compat contract or define Aura-native structs and adapt later (lean Aura-native; adapt at DGX time).
