# Phase 37: Per-User Full-Capability Sandbox - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-06
**Phase:** 37-per-user-full-capability-sandbox
**Areas discussed:** Backend build-vs-adopt, Egress default & enforcement, Idle lifecycle, Sandbox-unavailable failure mode, Host→box bridging & tool routing, In-box privilege posture, Concurrency benchmark, Egress sidecar form, Image/dep strategy, Class-(c) scope, Pip/npm under egress, Internal-network boundary

> Overarching operator directives that steered the whole session: **"search for MCP, don't
> reinvent the wheel"**, **"how do senior devs implement an industrial sandbox — not a nuclear
> bomb, but useful"**, and **"Claude Code parity"** for the box.

---

## Backend build-vs-adopt

| Option | Description | Selected |
|--------|-------------|----------|
| Hybrid — own lifecycle, adopt egress | Bespoke DockerBackend (moby SDK) + E2B seam; adopt OpenSandbox egress sidecar | ✓ |
| Adopt OpenSandbox wholesale | Replace backend with OpenSandbox Go SDK + Python control-plane; loses E2B seam | |
| Stay fully bespoke | Everything per spike blueprint incl. bespoke CONNECT proxy | |

**User's choice:** Hybrid.
**Notes:** Research surfaced Alibaba OpenSandbox (Apache-2.0, Go SDK, single-host Docker, egress sidecar) which postdates the June-2026 spikes. agent-sandbox/agent-sandbox v0.7.0 re-verified against the real repo: still **K8s-only** → confirms the spike's "DGX tier only, Aura owns the Docker backend." Hybrid reuses the hard part (egress) without throwing away the validated DockerBackend or the E2B forward-bet.

---

## Egress default & enforcement (resolved over three passes)

| Option | Description | Selected |
|--------|-------------|----------|
| Profile-dependent (`none` prod / allow-public hardened) | First pass pick | (superseded) |
| Uniform allow-public + block RFC1918/metadata | | (superseded) |
| Uniform deny-all `--network none` | Pigment-style | |
| **Claude Code parity: full internet, block internal ranges only** | Final operator directive | ✓ |

**User's choice:** **"no deny nothing, claude code parity"** → full public internet under both strict profiles; the ONLY carve-out is the internal network (RFC1918 + `169.254.169.254` metadata + shared-services bridge) as a tenancy boundary.
**Notes:** The operator asked "what happens to pip and npm?" — which exposed that a literal `--network none` production default would break the hybrid uv-long-tail install strategy. Resolved by dropping the deny-by-default default entirely in favor of Claude-Code parity. **SBX-04's `--network none` default is amended** (enforcement-when-set preserved). Mechanism = OpenSandbox DNS+nftables sidecar, `--network container:<box>`, always-on to enforce the internal-block floor.

---

## Idle lifecycle (SBX-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Suspend-on-idle, transparent resume, never auto-delete data | | ✓ |
| Suspend-on-idle + scheduled volume GC | | |
| Stop-and-destroy on idle (no persistence) | Rejected — breaks SBX-03 | |

**User's choice:** Suspend-on-idle, transparent resume, never auto-delete; reaper reuses migration-0009 scheduler.

---

## Sandbox-unavailable failure mode (F-001 / GATE-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Fail-CLOSED hard-deny; every identity incl. CLI gets a box | | ✓ |
| Fail-closed + approval-gated retry | | |
| Fail-closed, but CLI/local stays on host | Weakens F-001 | |

**User's choice:** Fail-CLOSED hard-deny, never host fallback; both strict profiles + `local`/CLI get a box.

---

## Host→box bridging & tool routing (SBX-01 / SBX-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Materialize-in / copy-out; web tools host-side | | ✓ |
| Materialize-in; route web tools through box egress | | |
| Shared read-only skills volume + per-identity /workspace | | |

**User's choice:** Materialize-in/copy-out; **"we already have web tools, don't double-think"** → web_fetch/web_search stay host-side, reuse existing SSRF guard.
**Notes:** SBX-02 forbids host bind-mounts (tension with the old single-user compose bind), so per-identity skills/Agent.md/pyscripts are materialized into the named volume, not bound.

---

## In-box privilege posture

| Option | Description | Selected |
|--------|-------------|----------|
| Full-capability inside; isolation = boundary + gVisor + cgroups | | ✓ |
| Add in-box least-privilege hardening (Pigment distroless/non-root) | Breaks fat-box design | |

**User's choice:** Full-capability inside (fat box). Isolation from the boundary, not from crippling the inside.
**Notes:** Research (Pigment/Northflank) confirmed the "not a nuclear bomb" ladder = hardened runc + gVisor knob; microVM = DGX tier. Aura deliberately diverges from Pigment's distroless/non-root/ephemeral cell — the box is a persistent full personal terminal.

---

## Concurrency benchmark (SC-5)

| Option | Description | Selected |
|--------|-------------|----------|
| Concurrent-identity soak + cgroup caps, envelope-based bar | | ✓ |
| Light: 2–3 identity smoke + per-box cost | | |
| Heavy: full load/stress harness | | |

**User's choice:** Concurrent-identity soak on the real 32GB host; ~10–20 boxes; RAM envelope + Resolve/Resume p95 + cgroup caps prevent starvation.

---

## Egress sidecar form

| Option | Description | Selected |
|--------|-------------|----------|
| Per-box `--network container:` sidecar (OpenSandbox as-is) | | ✓ |
| Vendor just the nftables+DNS logic | | |

**User's choice:** Adopt OpenSandbox egress component as a per-box `--network container:` sidecar.

---

## Image/dep strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Hybrid: shared fat base + uv long-tail via warm cache | | ✓ |
| Bake-all (no on-demand) | | |
| Thin base + uv on-demand only | Anti-pattern (first-run latency) | |

**User's choice:** Hybrid — shared fat base image, uv long-tail via warm cache, per-identity = volume only.

---

## Class-(c) scope

| Option | Description | Selected |
|--------|-------------|----------|
| Keep deferred — Phase 37 = SBX-01..05 only | | ✓ |
| Pull class-(c) into Phase 37 | | |

**User's choice:** Keep deferred; hold scope tight.

---

## Claude's Discretion

- Exact cgroup cap values (starting points given, tune against the benchmark).
- Idle-TTL default within the ~30 min ballpark.
- Reference registry set for the opt-in tightened allowlist.

## Deferred Ideas

- Class-(c) per-user PIM/WhatsApp sidecar instances → own phase.
- Per-identity quotas → Phase 37/OPS.
- Firecracker/Kata microVM tier → DGX/cloud.
- K8s + agent-sandbox + gVisor-default → DGX-Spark tier.
- Tightened opt-in egress allowlist (registries-only) → ships as mechanism, not default.
