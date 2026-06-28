---
spike: 062
name: docker-sandboxes-sbx-fit
type: standard
validates: "Given Docker Sandboxes (sbx, Firecracker microVM), when evaluated as Aura's Phase-17 box, then we learn whether it can host a persistent multi-service Linux appliance (compose-in-microVM, docker-in-VM no host socket) on the mini-PC/DGX target — or whether it is a dev tool + direction-signal only"
verdict: PARTIAL
related: [010, 059, 061]
tags: [phase-17, packaging, docker-sandboxes, sbx, microvm, firecracker, isolation]
---

# Spike 062: docker-sandboxes-sbx-fit

## What This Validates

Operator directive (2026-06-14): "look also docker.com/products/docker-sandboxes — make Aura
powerful." Docker Sandboxes (`sbx`) is Docker's GA, free-tier, **Firecracker microVM** runtime
for running coding agents (Claude Code/Codex/Gemini) unattended-but-safely. Each sandbox is an
own-kernel microVM with a **docker engine running inside** (so the agent gets docker *without*
the host socket), a host-side network proxy, and persistence across restarts. The question: is
this the Phase-17 box for Aura — or a dev tool + direction-signal?

## Research (docs + Docker blog + andrewlock deep-dive)

| Property | Finding | Aura fit |
|---|---|---|
| Isolation | Firecracker microVM, **own kernel** per sandbox (~125ms boot, <5MiB) | ✅ gold-standard host isolation |
| Docker-in-box | separate docker engine *inside* the VM → no host socket mount | ✅✅ solves SPEC "no host docker.sock" AND lifts SPEC Req 3 (docker-runtime MCP could run *inside*) |
| Full power inside | root, install pkgs, run services, unattended ("YOLO"/`--dangerously-skip-permissions`) | ✅ exactly "Aura's computer, not a jail" |
| Persistence | sandboxes persist after agent exits; pkgs/images/config kept across restarts | ✅ appliance-shaped |
| Network | host-side proxy blocks host-localhost, injects secrets, policy tiers (open/balanced/locked) | ✅ secrets never on disk/in-VM |
| **Host OS** | macOS (Apple silicon), Windows 11 (x86_64), **Ubuntu 24.04+ (x86_64)** | ⚠️ Linux OK *only x86_64* |
| **arm64** | **macOS Apple-silicon only; Windows + Linux are x86_64-only** | ❌ **the DGX appliance is arm64 → unsupported** |
| **Workload shape** | wraps a known **coding-agent CLI** (`sbx run claude`); arbitrary servers / a compose stack inside = "unclear"/off-label | ❌ Aura is a persistent multi-service stack (aura+pg+neo4j+embed+memory+searxng+multimodal), not a one-shot dev agent |
| Performance | andrewlock: "performance hit can be crippling, even for simple projects" | ⚠️ a 24/7 mini-PC on a tight CPU budget can't absorb a crippling microVM tax |
| Dependency | requires `sbx login` (Docker account); free tier + paid enterprise add-ons | ⚠️ third-party SaaS auth + licensing for a self-hosted/commercial DGX bundle |

## How to Run (NOT exercised — see verdict)

```
winget install -h Docker.sbx   # Windows ;  brew install docker/tap/sbx (mac) ;  apt-get install docker-sbx (Ubuntu x86_64)
sbx login ; sbx run claude
```
On-host check (2026-06-14, this AMD64 Windows host): winget package path is real; `sbx` not
installed. Deliberately **not** installed/logged-in — it wraps `claude`/`codex`, would not host
Aura's compose stack, and cannot change the two appliance-blocking facts below.

## Results

**PARTIAL — VALIDATED as a dev tool + the definitive direction-signal; INVALIDATED as Aura's appliance runtime.**

Two hard blockers for the appliance:
1. **arm64 gap.** `sbx` arm64 is macOS-only; Windows/Linux are x86_64-only. The headline commercial
   target — the **DGX Spark appliance — is arm64**. Docker Sandboxes cannot run the appliance there.
2. **Shape mismatch.** `sbx` is built to wrap a one-shot coding-agent CLI on a dev machine, not to
   host a persistent, multi-service product stack. Running Aura's whole compose stack inside one
   microVM's nested docker engine is off-label, perf-taxed, and `sbx login`-coupled.

**What it proves for Aura's box model (the real value):** the industry's "agent in a box" gold
standard is now **microVM (own kernel) + docker-in-VM-without-host-socket**. Aura should *aspire*
to that isolation grade where the host supports it, and must NOT cripple the box the other way
(the audit's distroless jail, spike 059). On the **x86_64 Linux mini-PC**, `sbx` is viable as an
*optional* power-user wrapper / dev-ergonomics tool; on **arm64 DGX** it is unavailable. Therefore
it is **not** the default Phase-17 box — but it ratifies the "powerful, full, isolated box"
direction and the docker-in-box-without-host-socket idea (a future capability that could lift
SPEC Req 3). The portable, arm64-capable isolation tier that *is* adoptable single-host is gVisor
`runsc` (spike 010) — see spike 061 for the decision.
