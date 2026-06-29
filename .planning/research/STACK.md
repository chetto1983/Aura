# Stack Research — v2.0.0 Industrial Hardening & Multi-User Production

**Domain:** Hardening/industrialization of an existing Go-native agentic substrate (single-binary + Docker-Compose, mini-PC deployment, DGX Spark appliance as long-term target)
**Researched:** 2026-06-29
**Confidence:** HIGH on versions/licenses (verified web + already-pinned go.mod); HIGH on the sandbox recommendation (quantified footprint + existing repo evidence)

> **Read this first.** This is a *subsequent* milestone. Most of the v2.0.0 stack is **already in `go.mod` / `compose.yaml`** — the work is mostly *wiring and policy*, not *adoption*. Concretely: Authula `v1.11.0`, OpenTelemetry Go SDK `v1.44.0` (trace + metric + OTLP/grpc + stdout exporters), `prometheus/client_golang v1.23.2`, `govulncheck` (CI `vuln` job), `testcontainers-go v0.42.0`, the Docker Go SDK (`moby/moby/client v0.4.1`), Garage S3 `v2.0.0`, AWS S3 SDK v2, and a ready-but-OFF `compose.gvisor.yaml` runsc overlay all exist today. The net-new *dependencies* are small; the net-new *engineering* (ToolGateway, per-user sandbox controller, owner-scoping, dashboards, SBOM, DR drill) is large. This document flags, per item, **ADOPT (new) vs WIRE (present) vs BUMP (present, newer available)**.

---

## TL;DR — The Sandbox Fork

**Recommendation: Option B (per-user full-capability pattern over Docker via the already-present Docker Go SDK), with Option C's `compose.gvisor.yaml` runsc overlay as the optional defense-in-depth tier on native-Linux/DGX appliances. Reject Option A (Kubernetes) for the mini-PC.**

Rationale in one paragraph: Option A (k3s/k0s + `kubernetes-sigs/agent-sandbox` or `agent-sandbox/agent-sandbox`) is the *correct* architecture for the DGX Spark appliance and the most direct literal match to the operator's reference repos — but the control plane alone costs **~0.6–1.2 GB RAM + a CPU core at idle** on a host already carrying ~6 GB of sidecars with a 16 GB floor, and it **breaks the single-binary + Docker-Compose deployment invariant** that every other Aura surface depends on. Option B reuses the Docker engine that already runs the entire stack, gives each identity a full-shell/full-fs/full-network container (the "agent still sees a full host" requirement is met inside the container), isolates users via per-identity named volumes + a per-user Docker network, and is driven by the Docker SDK *already in `go.mod`* — zero new daemon, zero new control plane, ~**+150–400 MB per *active* user container** (idle pool can be zero). Option C primitives (gVisor `runsc`, Sysbox) layer *under* Option B as the runtime, hardening the per-user containers without changing the controller. This keeps a single code path that scales from mini-PC (Docker + Option B) to DGX Spark (same controller, optionally K8s-backed later) and honors `feedback_aura_full_host_terminal_primary` + `feedback_no_atomic_bombs_minimal_industrial_shape`.

Detailed evidence in **§1**.

---

## Recommended Stack

### Core Technologies (the v2.0.0 additions/changes)

| Technology | Version | ADOPT/WIRE/BUMP | Purpose | Why Recommended |
|------------|---------|-----------------|---------|-----------------|
| **Docker Engine Go SDK** (`github.com/moby/moby/client`) | `v0.4.1` (present, indirect) | **WIRE** (promote to direct) | Per-user sandbox controller: create/start/exec/stop per-identity containers | Already pulled in transitively (testcontainers). Drives Option B with no new daemon. Container lifecycle, exec, volume + network mgmt are all first-class. |
| **gVisor `runsc`** | latest weekly (`release-2026062x.0`-class; near-weekly tags) | **WIRE** (`compose.gvisor.yaml` exists) | Optional kernel-isolation runtime under the per-user containers | `compose.gvisor.yaml` already registers `runtime: runsc`. Best-in-class container isolation w/o a VM; ~10–30% I/O/syscall overhead, ~0% on CPU-bound. Native Docker/containerd integration. Apache-2.0. |
| **Sysbox** (`nestybox/sysbox`) | `v0.7.0` (Mar 2026) | ADOPT (alternative to runsc) | Rootless/Docker-in-container runtime for the per-user containers, near-runc perf | Near-zero overhead (emulation only on privileged syscalls); lets a sandboxed container run nested Docker/systemd if a skill needs it. Apache-2.0. Pick **runsc for max isolation**, **sysbox for max compatibility/perf**. |
| **Authula** (`github.com/Authula/authula`) | `v1.11.0` present → **BUMP `v1.12.0`** (Jun 2026) | **WIRE + BUMP** | Multi-tenant auth: capability-per-route, 2FA/TOTP, OAuth, Postgres | Already in `go.mod` and the *default* `AURA_WEB_AUTH_PROVIDER`. v1.12.0 adds an event bus usable in Library Mode (useful for audit-ledger hooks). Capability-per-route **without** RBAC — exactly the constraint. Apache-2.0. |
| **OpenTelemetry Go SDK** (`go.opentelemetry.io/otel/{trace,metric,sdk}`) | `v1.44.0` (present) | **WIRE** (metrics path) | Traces (wired) + **metrics** (not yet wired) per target-architecture observability model | SDK is in `go.mod`; `internal/obs` + `internal/agent/tracing.go` already install the tracer provider. v2.0.0 adds the **metric** SDK path (spans exist, metrics don't yet flow through OTel). Apache-2.0. |
| **prometheus/client_golang** | `v1.23.2` (present) | **WIRE** (expand) | Native `/metrics` exposition + the v2.0.0 alert-driving counters/histograms | Already wired (`internal/agent/metrics.go`). v2.0.0 adds the audit/finding metrics (loop error rate, tool-timeout rate, pause/resume failures, ledger states, readiness). BSD-3-Clause. |
| **OpenTelemetry Collector (contrib)** | `v0.154.0` (Jun 2026) | ADOPT (optional sidecar/host) | Trace pipeline + Prometheus scrape + export; single egress point | Only needed when an external trace backend is wired. For single-binary mini-PC, Prometheus scrape of Aura's `/metrics` is often enough; Collector is the DGX/server-profile add. Apache-2.0. |

### Supporting Libraries & Tools

| Library / Tool | Version | ADOPT/WIRE | Purpose | When to Use |
|---------|---------|-----------|---------|-------------|
| **syft** (`anchore/syft`) | `v1.44.0+` (2026) | ADOPT (CI tool) | SBOM generation (source + compiled single binary), SPDX/CycloneDX | The de-facto Go SBOM tool; understands Go's static-link build. Run in CI on the `aura` binary + image. Apache-2.0. |
| **cyclonedx-gomod** (`CycloneDX/cyclonedx-gomod`) | latest (CycloneDX spec 1.6; needs Go ≥1.25) | ADOPT (CI tool) | Go-module-native CycloneDX SBOM (`mod`, `bin`, `app` modes) | Complements syft with module-accurate provenance; `app` mode SBOMs the single binary. Use syft as canonical + cyclonedx-gomod for cross-check. Apache-2.0. |
| **govulncheck** (`golang.org/x/vuln`) | latest (already CI `vuln` job) | **WIRE** (gate severity) | Reachability-aware Go CVE scan | Already run; v2.0.0 makes it a *blocking* gate (high-severity → fail unless waived). BSD-3-Clause. |
| **dependency-review-action** + **action SHA-pinning** | GH Action; pin via `ratchet` / `pin-github-action` | ADOPT (CI) | Block vulnerable dep PRs; pin all third-party Actions to commit SHA | Closes F-051/F-052 (supply-chain). `ratchet`/`pin-github-action` resolve `@vN` → `@<sha>`. MIT/Apache-2.0. |
| **k6** (`grafana/k6`) | `v2.1.0` (Jun 2026) | ADOPT (test harness) | HTTP/SSE load + scenario-based load profiles; fault injection via `xk6-disruptor` | Modern, Go-core, JS scenarios. Drives AG-UI/SSE + Telegram-webhook load. Defines supported concurrency + degradation (F-018). AGPL-3.0 (tool only; not linked into Aura). |
| **vegeta** (`tsenart/vegeta`) | `v12.x` (Go lib + CLI) | ADOPT (test harness) | Constant-rate HTTP load, embeddable as a Go library in `internal/eval`-style tiers | Simpler than k6 for CI smoke + p95/p99 latency assertions; composes with `jq`. MIT. |
| **toxiproxy** (`Shopify/toxiproxy/v2`) | `v2.12.0` (Mar 2025) | ADOPT (chaos) | TCP fault injection (latency, slow-close, partition) in front of PG/Neo4j/MCP sidecars | Go client + server are the community standard; drives chaos AC (DB outage, MCP timeout storm, object-store outage) F-035. MIT. |
| **ghz** (`bojand/ghz`) | `v0.121.0` | ADOPT *only if* gRPC | gRPC load/benchmark | Aura's transports are SSE/HTTP today; OTLP/gRPC export is the only gRPC surface. Use **only** if a gRPC API is added — otherwise skip (no scope creep). MIT. |
| **testcontainers-go** | `v0.42.0` (present) | **WIRE** (reuse) | Spin PG17 + Neo4j 5.26 in load/chaos/DR integration tiers | Already in `go.mod` (db_integration). Reuse for the DR restore-drill harness. MIT. |

### Development / Ops Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| **gosec** (`securego/gosec`) | Go SAST beyond linters | Pair with the existing `golangci-lint` v2.12.2 + `govulncheck`. Add to the security-regression CI (F-047). |
| **pg_dump / pg_restore (PG17 client)** | Postgres DR | Online, consistent logical backup. Already have `AURA_BACKUP_DIR` + `/backups` mount + role separation. DR drill restores into a throwaway testcontainer and asserts row counts (RPO/RTO). |
| **neo4j-admin database dump/load (5.26)** | Neo4j DR | **⚠ Community = OFFLINE only.** `neo4j-admin database backup` (online/differential) is Enterprise-only. See **§Constraints/Flags**. |
| **mc / aws-cli (S3)** | Garage object-store DR | Bucket snapshot + restore drill; AWS S3 SDK v2 already in `go.mod` for app-side, CLI for ops drill. |

---

## §1 — Per-User Full-Capability Sandbox: Evidence & Recommendation

The DEFINING fork. The requirement (from PROJECT.md / Key Decisions): **each identity drives a full-capability isolated sandbox — the agent still sees a full host (shell/fs/network), the real host is never exposed, users are isolated.** This resolves F-001/R-001 *without removing capability* (honors `feedback_aura_full_host_terminal_primary`).

### Current baseline (what exists today)

- `internal/agent/tools/shell_exec.go` runs the command **in-process, with the Aura process's own privileges, no sandbox hop, no path fence** (amendment #50 / D-15c). This is literally F-001.
- The rivetdev/sandbox-agent HTTP runner (`:2468`, `make sandbox-up`) is the documented **deliberate-escalation** path (memory `project_sandbox_pivot_to_code_sandbox_mcp`), not the primary surface. There is **no `tools.SandboxExec` in the current Go tree** (grep-confirmed) — the live tool is the host-direct `shell_exec`.
- `compose.gvisor.yaml` **already exists**: a one-line `runtime: runsc` overlay, OFF by default, intended for native-Linux/arm64 appliances. The repo already frames isolation as "performance cost, not capability stripping."
- The Docker Go SDK (`moby/moby/client v0.4.1`) and `testcontainers-go v0.42.0` are already in `go.mod`.

### Option A — Kubernetes (k3s/k0s) + agent-sandbox CRDs

| Attribute | `kubernetes-sigs/agent-sandbox` | `agent-sandbox/agent-sandbox` |
|---|---|---|
| Version | **v0.5.0** (Jun 24 2026) | **v0.7.0** (Jun 24 2026) |
| License | Apache-2.0 | Apache-2.0 |
| Languages | Go (controller + `clients/go/sandbox`) | Go 52% / TS 45% |
| Model | CRDs: `Sandbox`, `SandboxTemplate`, `SandboxClaim`, `SandboxWarmPool` | "AIO Sandbox" image, multi-tenant per-agent/per-user |
| Isolation | gVisor + Kata (via runtimeClass) | container isolation; E2B-compatible |
| K8s req | ≥1.26 | ≥1.26 |
| MCP | (controller-level) | **MCP server at `/mcp`, SSE/Streamable-HTTP** |
| E2B | — | **fully E2B protocol + SDK compatible** |
| Go client | `go get sigs.k8s.io/agent-sandbox/clients/go/sandbox@latest` | Python SDK shown; Go client not documented |
| Maturity | SIG-Apps, 3k★, 13 releases | v0.7.0, active |

**Mini-PC footprint (the decider):**
- **k3s** server node ≈ **1.2 GB RAM** in a small cluster (official profiling); single-node min 512 MB–1 GB + 1 CPU. Bundles ingress + LB (extra overhead).
- **k0s** single-node min **1 GB RAM + 1 vCPU**; "comfortable" 2 GB + 2 vCPU. Slightly leaner than k3s (pick-your-components), embedded etcd.
- Realistic **idle control-plane cost: ~0.6–1.2 GB RAM + ~0.5–1.0 CPU core** before a single sandbox pod runs.
- Host budget today: **~5.7–6.2 GB idle, ~7 GB peak**, on a **16 GB floor**. Adding a K8s control plane pushes idle to ~6.5–7.5 GB and erodes the headroom Slice 13 (vLLM, +5–7 GB, deferred) will need on the DGX path.

**Verdict on A:** Architecturally ideal **for DGX Spark** (where full K8s + gVisor/Kata runtimeClasses + warm pools shine, and `kubernetes-sigs/agent-sandbox` is the literal match). **Rejected for the mini-PC**: it violates the single-binary + Docker-Compose invariant, adds a second orchestrator to operate/back-up/monitor, and costs ~1 GB + a core at idle for a 1–few-user deployment. *Keep A as the documented DGX-appliance evolution path, not the v2.0.0 mini-PC mechanism.*

### Option B — Per-user full-capability pattern over Docker (RECOMMENDED)

Implement Aura's own thin Go controller (e.g. `internal/sandbox/usersandbox`) over the **Docker Engine the stack already runs**, using the Docker Go SDK already in `go.mod`:

- **One container per active identity**, image = a full Linux userland (the existing AIO-style image or a Debian/Alpine base with the skill toolchain). Inside: full shell, full fs, full network — the agent's "full host" is real, just *not the real host*.
- **Isolation:** per-identity **named volume** (`aura-sbx-<identityID>`) mounted at the workspace; per-identity **Docker network** (or `network_mode: none` + explicit egress allowlist proxy, reusing the egress-allowlist pattern already in `.planning/spikes/009-sandbox-egress-allowlist/`); `pids_limit`, `mem_limit`, `cpus`, `read_only` root + tmpfs, dropped caps as policy dials.
- **Lifecycle:** create-on-first-tool-use, idle-TTL reap (mirrors the background-shell TTL the audit already wants), explicit `docker stop` on session end. Idle pool can be **zero containers** → **zero idle RAM cost**.
- **`shell_exec` becomes a ToolGateway-routed `docker exec`** into the caller's identity container instead of an in-process host spawn. The host shell stays available only in `dev`/`local_trusted` profiles (per the runtime-profile table in target-architecture.md).
- **E2B/MCP:** not required for B — Aura already speaks its own tool protocol; the controller is internal. (If you later want the E2B wire, the AIO image from `agent-sandbox/agent-sandbox` is drop-in as the container image, since it's E2B+MCP-compatible — a clean upgrade path that does **not** require K8s.)

**Footprint:** **+0 idle** (no control plane, no idle pool), **~+150–400 MB per *active* user container** depending on base image + workload. On a 1–few-user mini-PC this is strictly cheaper than A and bounded by concurrency, not by a standing orchestrator.

**Why B wins:** reuses the engine that already runs *everything* (the Compose stack), no new daemon/control-plane to operate-back-up-monitor, single code path mini-PC→DGX, satisfies "agent sees a full host / real host never exposed / users isolated," and the Docker SDK + egress-allowlist pattern + gvisor overlay are **already in the repo**. Lowest blast radius for the deployment invariant.

### Option C — Lower-level primitives (the runtime under B)

These are **not a competing controller** — they are the *runtime* the Option-B containers run on, and the defense-in-depth dial:

| Primitive | Version | Role under B | Overhead | License |
|---|---|---|---|---|
| **gVisor `runsc`** | weekly (`release-2026062x.0`) | `runtime: runsc` per container → user-space kernel, strongest non-VM isolation | ~10–30% I/O/syscall, ~0% CPU-bound | Apache-2.0 |
| **Sysbox** | v0.7.0 | rootless + nested-Docker-capable runtime, near-runc perf | near-zero (emulation only on privileged syscalls) | Apache-2.0 |
| **Firecracker / microVM** | n/a | Hardware-VM isolation | higher (VM boot, mem) | Apache-2.0 |
| **landlock + seccomp + namespaces (Go)** | stdlib + `landlock-lsm/go-landlock` | In-process fence *without* a container | minimal | BSD-2/MIT |

**Verdict on C:** Use **gVisor `runsc` as the per-container runtime in `single_user_hardened`/`server_production`** (the `compose.gvisor.yaml` mechanism, applied to the per-user containers, native-Linux/DGX only — keep OFF on Docker Desktop dev). **Reject Firecracker/microVM** for the mini-PC (overkill, breaks Compose simplicity). **Reject pure landlock/seccomp-without-container** as the *primary* mechanism: it does not give the agent a "full host" view and complicates the full-shell requirement — but landlock/seccomp are fine as *additional* hardening inside the container. Sysbox is the runtime to pick when a skill needs Docker-in-the-sandbox.

### Sandbox decision summary

```
v2.0.0 mini-PC:    Aura Go controller (Docker SDK)  ->  per-identity container (runc by default)
                                                         |- named volume (per-user fs isolation)
                                                         |- per-user network / egress-allowlist proxy
                                                         |- policy: pids/mem/cpu/caps via ToolGateway
hardened/server:   + runtime: runsc (gVisor)  [compose.gvisor.yaml mechanism, native-Linux]
DGX Spark (later): same controller, optionally backed by k8s-sigs/agent-sandbox CRDs + warm pools
```

---

## Installation (new dependencies only)

```bash
# Go deps — promote present-indirect to direct + bump auth (rest already in go.mod)
go get github.com/moby/moby/client@v0.4.1            # WIRE: promote to direct (per-user sandbox controller)
go get github.com/Authula/authula@v1.12.0            # BUMP from v1.11.0 (event bus in Library Mode)
go get github.com/landlock-lsm/go-landlock@latest    # optional in-container hardening (Linux)

# CI / ops tools (NOT app deps — installed in CI runners / appliance image)
go install github.com/anchore/syft/cmd/syft@latest                 # SBOM (binary + source)
go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest
go install github.com/Shopify/toxiproxy/v2/cmd/toxiproxy-server@latest
go install github.com/Shopify/toxiproxy/v2/cmd/toxiproxy-cli@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
# k6 + vegeta installed as standalone binaries (release archives), not go-get into the module

# Host runtime (native-Linux / DGX appliance only — NOT Docker Desktop dev):
sudo runsc install && sudo systemctl reload docker    # gVisor; compose.gvisor.yaml then applies
# (alternative) install nestybox/sysbox v0.7.0 per its deb / k8s installer
```

OTel metrics SDK, Prometheus client, OTLP exporters, testcontainers, AWS S3 SDK, govulncheck: **already present — no install, just wire.**

---

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| Option B (Docker SDK controller) | Option A (k3s/k0s + agent-sandbox CRDs) | **DGX Spark appliance** or a true multi-tenant SaaS with warm-pool scale — there K8s + `kubernetes-sigs/agent-sandbox` is correct. Not the mini-PC. |
| Option B controller | rivetdev/sandbox-agent as the *primary* path | Keep rivetdev as the **deliberate-escalation** runner it already is; B subsumes it for the per-user default. Use rivetdev if you specifically want its E2B-on-cloud (Daytona/Vercel) deploy targets. |
| gVisor `runsc` | Sysbox | When a sandboxed skill must run **nested Docker/systemd** or you need near-runc perf over max isolation. |
| Authula | Keep HMAC passphrase cookie | `dev`/`local_trusted` single-operator only. Multi-user production **requires** Authula (per-route capability + per-principal identity). |
| Prometheus scrape (single-binary) | OTel Collector sidecar | Add the Collector only when exporting traces/metrics to an **external** backend (server_production/DGX). Mini-PC: scrape Aura's `/metrics` directly. |
| k6 | vegeta (Go lib) | Use vegeta for **in-CI Go-embedded** smoke + p95 assertions; k6 for richer multi-stage scenario load. Run both (they don't conflict). |
| syft (canonical SBOM) | cyclonedx-gomod | Use cyclonedx-gomod for module-accurate cross-check / `app`-mode binary SBOM; syft stays canonical. |

---

## What NOT to Use (scope guards)

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **Full Kubernetes / k3s / k0s on the mini-PC** | ~0.6–1.2 GB + a CPU core idle control plane; breaks single-binary + Docker-Compose invariant; second orchestrator to operate | Option B Docker-SDK controller; reserve K8s for the DGX appliance |
| **RBAC frameworks** (Casbin, OPA-as-RBAC, role tables) | Explicitly out of scope — authz stays `capability_grants` + Authula per-route capability | Authula capability-per-route (no roles) |
| **New LLM providers / GPU-gated stacks** (vLLM/LMCache, new rerankers) | Slice 13 is deferred; GPU work is out of scope | Existing OpenRouter/DeepSeek-V4 path unchanged |
| **OAuth multi-provider login, SaaS multi-tenant** | Out of scope (RBAC/OAuth = post-v2.0.0); v2.0.0 is identity isolation only | Authula email+password + capability; identity-scoped store/API |
| **Firecracker/microVM on mini-PC** | VM boot + mem overhead, breaks Compose simplicity | gVisor `runsc` (already wired) for the isolation dial |
| **ghz** (unless gRPC API added) | No gRPC serving surface today (only OTLP export) | k6/vegeta for the HTTP/SSE surfaces |
| **Neo4j Enterprise online backup** | Community is offline-dump only; do not assume online/differential backup exists | Scheduled offline `neo4j-admin database dump` + restore drill (see Flags) |
| **A second secrets system / vault** | Scope creep; `.env` + profile validation is the minimal industrial shape | Profile validation that *rejects* default secrets in `server_production` (F-002/F-007) |

---

## Stack Patterns by Variant (runtime profiles)

Maps to target-architecture.md's profile table. The sandbox + auth + observability dials change per profile:

**If `dev`:**
- Sandbox = `host_direct` (in-process `shell_exec`); auth = none/HMAC; OTel exporter = `stdout`/`none`. Today's behavior, explicitly labeled dev.

**If `local_trusted`:**
- Sandbox = host-direct with approvals; auth = HMAC or Authula; OTel = optional. The single-operator mini-PC default that exists now.

**If `single_user_hardened`:**
- Sandbox = **per-user Docker container (Option B), runtime `runsc`** (native-Linux); auth = Authula; metrics + `/readyz` on; secrets strict. Host shell disabled except via explicit grant.

**If `server_production`:**
- Sandbox = **per-user container mandatory**, host shell **disabled**, egress-allowlisted; auth = Authula required + per-principal owner-scoping enforced; OTel + Prometheus + alert rules + readiness mandatory; **fail-fast on default secrets / default object-store creds / listener bind failure** (F-002/F-007/F-008/F-016). SBOM + govulncheck gates blocking.

---

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| Go `1.26.4` | cyclonedx-gomod (needs Go ≥1.25) | OK |
| OTel SDK `v1.44.0` (trace) | OTel metric `v1.44.0` | Same release train — wire the metric reader/exporter at the version already in `go.mod` (no bump needed) |
| Authula `v1.12.0` | pgx `v5.9.2` / PG17 | Authula supports Postgres backend; shares the `aura.*` DB or its own schema; v1.12 event bus -> audit-ledger hook |
| Docker SDK `moby/moby/client v0.4.1` | Docker Engine on host / Compose | Same engine the stack runs; no Docker Desktop on the prod path (Linux only per constraints) |
| gVisor `runsc` | Linux ≥4.14.77, native Linux/arm64 | `compose.gvisor.yaml` already gates this to native-Linux appliances; OFF on Docker Desktop |
| testcontainers-go `v0.42.0` | PG17 + Neo4j 5.26 modules `v0.42.0` | Already pinned matching; reuse for DR + chaos tiers |
| Neo4j `5.26.26-community` | `neo4j-admin database dump/load` | **Offline-only** in Community (Flag below) |

---

## Constraints / Flags for the Roadmapper

1. **Single-binary + Docker-Compose invariant.** Option A (K8s) violates it; **Option B preserves it.** This is the load-bearing reason for the B recommendation. Any roadmap phase that proposes K8s on the mini-PC contradicts PROJECT.md constraints.
2. **Mini-PC RAM headroom is tight.** Idle ~5.7–6.2 GB on a 16 GB floor; Slice 13 (deferred) wants +5–7 GB. The per-user sandbox must be **idle-zero** (Option B with no warm pool) — do not stand up an idle orchestrator or warm pool on this host.
3. **⚠ Neo4j Community = offline backup only.** `neo4j-admin database backup` (online/differential) is **Enterprise-only**. The DR drill (F-042) must use scheduled **offline `neo4j-admin database dump`** (brief downtime / snapshot window) + `database load` restore, OR budget Neo4j Enterprise on the DGX appliance. **Flag this in the DR-harness phase** — it affects RPO/RTO targets.
4. **Default secrets in compose.yaml are intentional dev defaults** (`GARAGE_RPC_SECRET`, `AURA_OBJECTSTORE_ACCESS_KEY/SECRET_KEY`, PIM admin token). The profile-validation work (F-002/F-007) must **reject these in `server_production`** — they exist in the repo today as the F-007 finding.
5. **Authula is already the compose default** (`AURA_WEB_AUTH_PROVIDER: authula`) but go.mod pins v1.11.0 while v1.12.0 ships an event bus for Library Mode. The cutover is a *flip + bump + per-principal owner-scoping*, not an adoption. Confirm the existing HMAC↔Authula boundary converges on one principal/capability model (it already does per PROJECT.md).
6. **OTel traces are wired; OTel metrics are not.** `internal/obs` installs the tracer provider; `internal/agent/metrics.go` uses Prometheus directly. The observability-pack phase (F-023/F-024) wires the OTel **metric** SDK path and the audit's required identifiers (`run_id`, `tool_invocation_id`, `actor_id`, `runtime_profile`, `policy_decision_id`, …) as span attrs + metric labels.
7. **gVisor overlay exists but is OFF and native-Linux-only.** Do not enable on Docker Desktop dev; it's the hardened/DGX dial. The per-user-container work should make `runtime: runsc` a per-profile policy, not a global flag.
8. **No GPU, no RBAC, no new LLM providers** in v2.0.0 — confirmed against the "DO NOT add" list. ghz only if a gRPC API materializes (it shouldn't).

---

## Sources

- `D:\Aura\go.mod` — current pins: Authula v1.11.0, OTel v1.44.0, prometheus/client_golang v1.23.2, moby/moby/client v0.4.1, testcontainers-go v0.42.0, aws-sdk-go-v2/service/s3, neo4j-go-driver v5.28.4 (HIGH — ground truth)
- `D:\Aura\compose.yaml` + `D:\Aura\compose.gvisor.yaml` — existing sidecar footprint, Garage v2.0.0, runsc overlay (HIGH — ground truth)
- `D:\Aura\internal\agent\tools\shell_exec.go`, `internal\obs\init.go` — F-001 in-process host shell + tracer-only OTel wiring (HIGH — ground truth)
- github.com/kubernetes-sigs/agent-sandbox — v0.5.0, Apache-2.0, CRDs, Go client `sigs.k8s.io/agent-sandbox/clients/go/sandbox`, gVisor/Kata, K8s ≥1.26 (HIGH)
- github.com/agent-sandbox/agent-sandbox — v0.7.0, Apache-2.0, E2B+MCP-compatible AIO Sandbox, per-user isolation (HIGH)
- github.com/rivet-dev/sandbox-agent — E2B-compatible, ~15MB static binary, MCP server, per-user/per-agent isolation (MEDIUM — web)
- portainer.io / k3s docs / siderolabs — k3s ~1.2 GB server-node RAM, k0s 1 GB min / 2 GB comfortable; control-plane idle cost (MEDIUM — multiple sources agree)
- gvisor.dev/docs/architecture_guide/performance + pistack.xyz runtime comparison — runsc ~10–30% I/O overhead / ~0% CPU; Sysbox near-runc (MEDIUM — official + corroborating)
- nestybox/sysbox releases — v0.7.0 (Mar 2026), Apache-2.0 (HIGH)
- github.com/Authula/authula + /tags — v1.12.0 (Jun 20 2026) latest, Apache-2.0, capability-per-route (NOT RBAC), 2FA/TOTP, OAuth, PG/MySQL/SQLite, Library Mode + event bus (HIGH)
- open-telemetry/opentelemetry-go — v1.44.0 latest stable, Apache-2.0 (HIGH — matches go.mod)
- prometheus/client_golang releases — v1.23.2 latest, BSD-3-Clause (HIGH — matches go.mod)
- open-telemetry/opentelemetry-collector-releases — v0.154.0 (Jun 2026), Apache-2.0 (MEDIUM — web)
- anchore syft v1.44.0+ / CycloneDX cyclonedx-gomod (spec 1.6, Go ≥1.25) — Apache-2.0 (MEDIUM)
- grafana/k6 v2.1.0 (Jun 2026, AGPL-3.0) / Shopify/toxiproxy v2.12.0 (MIT) / tsenart/vegeta (MIT) / bojand/ghz v0.121.0 (MIT) (MEDIUM — multiple sources)
- neo4j.com/docs/operations-manual backup-restore — Community = offline dump/load only; online/differential = Enterprise (HIGH — official)

---
*Stack research for: v2.0.0 Industrial Hardening & Multi-User Production (subsequent milestone — wiring/policy over an existing single-binary Go substrate)*
*Researched: 2026-06-29*
