---
spike: 009
name: sandbox-egress-allowlist
type: standard
validates: "Given a host forward-proxy with a hostname CONNECT allowlist, when the sandbox egresses via the proxy vs directly, then allowed hosts pass / denied hosts are 403'd — and the enforcement boundary is characterized honestly"
verdict: PARTIAL
related: [007, 008, 010]
tags: [sandbox, egress, network, proxy, hardening, phase-8, phase-11]
---

# Spike 009: sandbox-egress-allowlist

## What This Validates

Network egress control for model-authored skill code — the Phase-8 D-08 design ("host-side Go forward proxy + hostname-CONNECT allowlist + resolve-then-pin") that the sandbox-agent pivot dropped. Also resolves the spike-007 prod-parity obligation (on-demand `uv` deps need egress; egress must be controllable).

## How to Run

```bash
go build -o /d/tmp/spike009-proxy.exe ./.planning/spikes/009-sandbox-egress-allowlist/proxy
/d/tmp/spike009-proxy.exe &              # ~80-LOC CONNECT proxy, :18443, pypi allowlist
# drive sandbox /v1/processes/run with env https_proxy=http://host.docker.internal:18443
```

## Results

**PARTIAL — the mechanism is VALIDATED; enforcement is environment-dependent.**

| Test | Result |
|---|---|
| host → proxy → pypi.org (allowed) | **200** tunnels |
| host → proxy → github.com (denied) | **000** (403 at CONNECT) |
| sandbox WITH `HTTPS_PROXY` → pypi (allowed) | `HTTP/1.1 200 OK` |
| sandbox WITH `HTTPS_PROXY` → github (denied) | **`HTTP/1.1 403 Forbidden`** |
| sandbox DIRECT (no proxy env) → github | **`HTTP/2 200`** ← bypass |

Proxy decision log: `ALLOW pypi.org:443` / `DENY github.com:443`. The ~80-LOC Go CONNECT proxy (hostname-suffix allowlist) does exactly its job.

## The enforcement gap (the load-bearing finding)

`HTTP(S)_PROXY` env is **advisory** — it only redirects cooperating clients. Test C proves a process that ignores the env egresses **directly**, because **Docker Desktop's vpnkit NATs the `aura-sandbox-local` bridge to the internet regardless of `com.docker.network.bridge.enable_ip_masquerade: "false"`** (the same accidental-NAT that overturned spike-006). So on Docker Desktop the allowlist is decorative against hostile code.

**Enforcement requires the proxy to be the container's ONLY route out:**
- **Native-Linux dockerd + true non-masquerading bridge** (no vpnkit): the container has no NAT route → the *only* egress is the proxy you point it at → allowlist is **enforced**. This is the prod posture Phase-8 D-08 assumed and it holds there, NOT on Docker Desktop dev.
- Alternative (heavier, rejected for now — "no atomic bombs"): `internal: true` docker network + proxy as a second reachable NIC; or nftables egress rules (Phase-8 D-08 already rejected iptables as incompatible with `cap_drop: ALL`).

## Plan obligations

1. **Phase-8 regression**: the host forward-proxy + non-masquerading bridge egress control was specced (D-08) and lost in the sandbox-agent pivot. Restore it as the prod egress boundary; this ~80-LOC proxy is a working reference. Egress posture is the planner's explicit decision (don't ship relying on Docker Desktop's accidental NAT — it's a dev-only artifact).
2. **Phase-11 tie-in**: a snippet/skill with `needs_network: true` → its egress should route through the allowlisted proxy (per-skill or global allowlist); `needs_network: false` → no proxy env + (on prod) no route = truly offline. This makes `needs_network` a real RISKY-tier signal, not just docs.
3. Dev (Docker Desktop) keeps full egress — acknowledge it as advisory-only and gate live egress tests behind the native-Linux/CI tier (mirrors the CAP-02 Docker Desktop blocker pattern).
