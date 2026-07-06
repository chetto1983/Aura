# Phase 37: Per-User Full-Capability Sandbox - Research

**Researched:** 2026-07-06
**Domain:** Per-identity Docker sandboxing (moby Go SDK), netfilter egress enforcement, container lifecycle on an existing scheduler, host-exposure unrepresentability
**Confidence:** HIGH (codebase seams + spike blueprint), MEDIUM (moby/moby/client v0.4.1 exact API surface — v0.x, verify at build time), MEDIUM (OpenSandbox-egress-as-Docker-sidecar adoption — K8s-centric upstream)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01 (Hybrid build-vs-adopt):** Keep the spike-locked **bespoke `DockerBackend`** over `moby/moby/client` (already in `go.mod` — promote indirect→direct) for box lifecycle (`Resolve`/`Exec`/`Suspend`/`Resume`/`Stop`). The `Backend` seam **speaks the E2B protocol** so `agent-sandbox/agent-sandbox` v0.7.0 drops in at the DGX tier unmodified. Package: `internal/sandbox/usersandbox`.
- **D-02 (Contract, corrected by spike 082):** `Resolve` = idempotent **direct `Sandbox` create** (NOT `SandboxClaim`); idle = `OperatingMode:Suspended` (retain box + volume); verbs `Resolve/Exec/Suspend/Resume/Stop`.
- **D-03 ("don't reinvent the wheel"):** Adopt **Alibaba OpenSandbox** Apache-2.0 **egress component** (DNS + nftables allowlist) for SBX-04 instead of the advisory ~80-LOC CONNECT proxy from spike 009. Search for an existing MCP/library before locking any bespoke mechanism.
- **D-04 (Default posture):** **Full public internet under BOTH strict profiles** — pip/npm/uv/npx/any API/any host all work (Claude-Code parity). Do not nanny outbound work.
- **D-05 (The only carve-out = tenancy boundary):** Egress default ruleset = **allow-public, DROP RFC1918 + `169.254.169.254` cloud-metadata + the shared-services Docker bridge**. Box A can't reach box B, `agent-memory :8091`, Garage, Postgres/Neo4j, or the host LAN. Shared services reached only via identity-scoped MCP brokering.
- **D-06 (SBX-04 AMENDMENT REQUIRED):** SBX-04's *"egress defaults to `--network none`"* is amended — default is **full-internet-minus-internal**. Core requirement preserved: a tightened allowlist, when set, is **enforced (nftables), not advisory**; `runtime: runsc` stays selectable under `server_production`. **PRD/REQUIREMENTS-amendment commit before implementation.**
- **D-07 (Egress sidecar form):** OpenSandbox egress runs as a **per-box sidecar sharing the box netns via `--network container:<box>`** (`CAP_NET_ADMIN` on the sidecar only). Always-on for any networked box (D-05 internal-block is the default floor).
- **D-08 (Lifecycle):** **Suspend-on-idle** — idle-TTL (~30 min config knob) → `OperatingMode:Suspended` (box killed, **named volume RETAINED**). Next tool call **auto-resumes transparently**. Volume never auto-deleted; destroy (`ShutdownPolicy:Delete`) only on explicit identity deprovision. The idle reaper **reuses the migration-0009 scheduler**, not a new goroutine.
- **D-09 (Fail-CLOSED):** If the box can't create/resume under a strict profile, the shell/fs call **fails CLOSED** — clear error `ToolResult` (same shape as approval-required deny), **NEVER a host fallback**. Both strict profiles route to a box. The `local`/no-principal CLI identity under strict **gets a `local`-id box too — never host**.
- **D-10 (Host→box bridging):** SBX-02 makes host bind-mounts unrepresentable → per-identity skills / `Agent.md` / pyscripts are **materialized into the box's named volume** at create/resume (docker cp / init step), never host-bound. `/workspace` = persistent per-identity named volume + a **tmpfs RW scratch**. Artifacts copied back out (docker cp) for Telegram `sendDocument`.
- **D-11 (Tool routing):** Tools routed **INTO the box**: `shell_exec`, `shell_bg`, `fs_read`, `fs_write`, **skill-snippet execution**. `web_fetch`/`web_search` **stay host-side** (Aura already SSRF-guards them; don't duplicate).
- **D-12 (Box posture):** Agent is **root inside with writable rootfs + full shell** (the fat box — `packaging-box.md`, REVERT `ec7fe2f6`; **NOT distroless/non-root/read-only-rootfs**). Isolation = the boundary: container namespaces + host-exposure flags unrepresentable + **gVisor `runsc` under `server_production`** + **cgroup v2 CPU/mem/pids caps** + **seccomp default**. (eBPF-per-tool-call explicitly skipped.)
- **D-13 (Image & deps):** **Hybrid** — **ONE shared fat base image** bakes runtimes + heavy common deps (`python3`/`node`/`go`/`uv`/`git`/`gh`/`jq` + Phase-5 heavy set); `uv` installs the long tail on-demand via a **shared warm-cache volume** (makes `deps:` frontmatter **load-bearing**). Per-identity = the volume only; image shared. Dockerfile in-repo, **digest-pinned**.
- **D-14 (Benchmark):** **Concurrent-identity soak on the REAL 32GB host** (dev WSL Docker capped at 15.47 GiB). Measure ~10–20 concurrent boxes: aggregate RAM within 32GB + headroom, **Resolve p95 < ~2s**, **Resume p95 < ~1s**, per-box cgroup caps (start **2 CPU / 2GB / 512 pids**) proven to prevent starvation. Gate-3 `VALIDATION.md` table. Pass = fits 32GB with headroom + no starvation (not a scale SLA).
- **D-15 (ADR):** Record **container-per-identity over Docker** for the mini-PC; **K8s + `agent-sandbox` + gVisor-as-default reserved for DGX-Spark**; sandbox config **mirrors the E2B / agent-sandbox template/claim shape** for forward compat.

### Claude's Discretion
- Exact cgroup cap values (D-14 gives starting points; tune against the benchmark).
- Idle-TTL default within the ~30 min ballpark (config knob).
- Exact registry set if an operator opts into a tightened allowlist — pypi/pythonhosted/npm/github/skills.sh reference set.

### Deferred Ideas (OUT OF SCOPE)
- Class-(c) per-user PIM/WhatsApp sidecar instances (own phase).
- Per-identity quotas (Phase 37/OPS, not this phase).
- Firecracker / Kata microVM tier (DGX/cloud only).
- K8s + `kubernetes-sigs/agent-sandbox` + gVisor-as-default (DGX multi-node).
- Tightened opt-in egress allowlist as *default* (mechanism ships D-06, but default is full-internet-minus-internal).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **SBX-01** | Under hardened/production, host shell/fs tools execute inside a per-identity full-capability sandbox (full shell/fs/network inside, named volume, per-user `/workspace`) routed by `SandboxRouter.Resolve(identityctx)`; agent experiences a full host, real host never exposed. | §Architecture Pattern 1 (Router seam) + Pattern 5 (tool routing); `RuntimeProfile.Strict()` gate (verified `config_runtimeprofile.go`); named-volume isolation proven live (spike 078). |
| **SBX-02** | Docker-socket mount, `--privileged`, `--network host`, host bind-mounts are **unrepresentable** in the sandbox config; a test asserts none can be set. | §Architecture Pattern 2 (unrepresentability translator) — Aura's spec type omits the fields; a private translator pins the dangerous moby `HostConfig` fields to safe constants; structural + behavioral test. |
| **SBX-03** | Per-identity lifecycle (create / idle-TTL stop / resume / scheduled-delete) with stable identity + persistent storage; cross-identity leakage impossible. | §Architecture Pattern 3 (idle-suspend on migration-0009 scheduler) + Pattern 4 (per-identity named volume); cross-deny proven live (spike 078/083). |
| **SBX-04** | Egress default `--network none` **AMENDED to full-internet-minus-internal (D-06)**; a tightened allowlist, when set, is enforced (nftables), not advisory — a configured allowlist can't reach a disallowed host; `runtime: runsc` selectable under `server_production`. | §Architecture Pattern 6 (OpenSandbox egress sidecar) + Pitfall 2 (gVisor⊥nat-redirect) + Pitfall 3 (dev vpnkit NAT is advisory-only). |
| **SBX-05** | ADR: container-per-identity over Docker for the mini-PC; K8s+agent-sandbox+gVisor-default for DGX; config mirrors the agent-sandbox template/claim shape. | §State of the Art (E2B backend seam, spike 082 corrections); D-15. |
</phase_requirements>

## Summary

Phase 37 resolves audit **F-001** (full-host shell/fs escape surface) and **F-036** (advisory egress) by *containment, not capability-stripping*: under the two strict `RuntimeProfile`s, the five host tools (`shell_exec`, `shell_bg`, `fs_read`, `fs_write`, skill-snippet exec) route into a **per-identity full-capability Docker box** instead of running `os/exec`/`os.ReadFile` directly on the host. The box is the fat image (root, writable rootfs, full shell — the current `docker/aura` Dockerfile is already fat; the `ec7fe2f6` distroless revert is done). Isolation is the *boundary*: a per-identity named `/workspace` volume, cgroup v2 caps, gVisor as an opt-in `server_production` runtime, and a set of host-exposure flags made structurally unrepresentable. The entire mechanism is a **no-op under `dev`/`local_trusted`** (mirroring how `gateway.Decide` short-circuits when `!profile.Strict()`), so the operator's daily full-host experience is unchanged.

Every load-bearing decision is already locked in CONTEXT.md and validated live by spikes 059–062 (single-user box, host edge, gVisor tier) and 078/082/083 (per-identity multiplexing, the corrected E2B contract, two-identity cross-deny). The spikes proved the *Docker model* over the CLI; the genuinely new build work is the **Go SDK binding** (`moby/moby/client` v0.4.1 — a v0.x module with an options-struct API that differs from every `docker/docker/client` example online) and the **egress sidecar adoption** (OpenSandbox is Apache-2.0 and netns-based, but its docs are K8s-centric and its DNS-redirect is gVisor-incompatible — a real tension with D-06/D-12).

**Primary recommendation:** Build `internal/sandbox/usersandbox` as an E2B-verb `Backend` interface with a `DockerBackend` impl over `moby/moby/client`; expose a narrow `SandboxSpec` type that *cannot express* privileged/host-network/bind-mount/socket (a single private translator pins those moby `HostConfig` fields to safe constants); interpose `SandboxRouter.Resolve(identityctx)` inside the 5 tools behind a `Strict()`-gated no-op; enforce the D-05 default-floor with a **filter-table** nftables ruleset (gVisor-compatible) and reserve the OpenSandbox DNS-redirect FQDN-allowlist for the runc-only tightened-opt-in mode; hook idle-suspend into the existing migration-0009 scheduler as a new `TaskKind` handler; and gate the 32GB soak on the real host.

## Architectural Responsibility Map

Multi-container phase — tiers are process/isolation boundaries, not web layers.

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Box lifecycle (`Resolve`/`Suspend`/`Resume`/`Stop`) | **Aura Daemon** (`internal/sandbox/usersandbox`) | Docker Engine | The daemon owns the Docker SDK client and speaks to `dockerd`; boxes never self-manage. |
| Command/file execution | **Per-Identity Box** | Aura Daemon (routes exec) | Full shell/fs runs *inside* the box; the daemon only `Exec`s + `docker cp`s. |
| Host-exposure unrepresentability | **Aura Daemon** (spec type + translator) | — | A dangerous flag must be impossible to set at the Go type layer, before it reaches `dockerd`. |
| Egress enforcement (default floor + FQDN allowlist) | **Egress Sidecar** (netns-shared) | Docker network topology | `CAP_NET_ADMIN` lives only on the sidecar; the box stays net-unprivileged. |
| Per-identity persistence | **Named Volume** (`aura-box-<id>`) | Docker Engine | Storage-/kernel-enforced isolation — not app-prefix scoping (spike non-negotiable). |
| Idle reaping / scheduled delete | **Aura Daemon** (migration-0009 scheduler) | Postgres (`scheduler_tasks`) | Reuse the existing tick loop; no new background goroutine (goleak discipline). |
| Policy decision (allow/deny/approve) | **Aura Daemon** (`gateway.Decide`) | — | Phase 35 GATE-01 already interposes above `tool.Execute`; sandbox routing is orthogonal and *inside* the tool. |
| Artifact delivery (box → Telegram) | **Aura Daemon** (`docker cp` out → `send_file`) | Per-Identity Box | Box writes to `/workspace`; daemon copies out; existing `send_file`/AG-UI artifact path unchanged. |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/moby/moby/client` | v0.4.1 → **promote indirect→direct** | Docker Engine API Go SDK: container create/exec/cp, volumes, image pull | `[VERIFIED: go.sum]` Already transitively present (via `testcontainers-go` + Authula). The officially-maintained successor to `docker/docker/client`, versioned independently. **D-01 locks it.** |
| `github.com/moby/moby/api` | v1.54.2 | API types: `container.Config`, `container.HostConfig`, `container.Resources`, `network.NetworkMode` | `[VERIFIED: go.sum]` The type package the client's options structs reference; already indirect. |
| OpenSandbox egress | main (Apache-2.0, built-from-source image) | FQDN egress allowlist sidecar (DNS proxy + nftables) — the D-06 tightened-opt-in enforcement | `[CITED: github.com/alibaba/OpenSandbox]` D-03 adopted. **Caveat: K8s-centric docs; DNS-redirect is gVisor-incompatible (Pitfall 2).** |
| gVisor `runsc` | (host-provisioned) | Optional `server_production` isolation runtime via existing `compose.gvisor.yaml` | `[VERIFIED: compose.gvisor.yaml]` Already in-repo; native-Linux/arm64 only, KVM-free. |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/testcontainers/testcontainers-go` | v0.42.0 (indirect) | Spin a real container/daemon in integration tests | `[VERIFIED: go.mod]` Already present; the SBX-03/04 `docker_integration`-tagged tests can use it or drive `dockerd` directly. |
| `internal/cron` (migration-0009 scheduler) | in-repo | Idle-suspend reaper + scheduled-delete as a new `TaskKind` handler | `[VERIFIED: internal/cron/*]` D-08 reuse; the `identity_purge` handler is the exact template. |
| `google/nftables` OR shell `nft` in a minimal sidecar | — | The D-05 default-floor filter ruleset (fallback if OpenSandbox-as-Docker-sidecar doesn't land cleanly) | `[ASSUMED]` Only if OpenSandbox adoption stalls; the filter-table floor is ~10 nft rules. |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `moby/moby/client` v0.4.1 | `github.com/docker/docker/client` (classic, stable, ubiquitously documented) | Classic SDK is API-stable and every online example targets it, but it'd be a *new* heavy direct dep and Moby is migrating away from it. **D-01 locks moby/moby/client**; keep classic as the escape hatch only if v0.x churn blocks the build (Pitfall 1). |
| OpenSandbox egress sidecar | Bespoke minimal `CAP_NET_ADMIN` sidecar running a static `nft` filter ruleset | Bespoke = fully in Aura's control + gVisor-compatible for the floor, but violates "don't reinvent the wheel" (D-03). **Recommendation: OpenSandbox for the FQDN allowlist (runc), bespoke filter-floor for the always-on internal-block** (works under runc *and* gVisor). |
| gVisor `runsc` | Kata Containers (`kata-qemu`) | Kata gives a full per-box kernel so the OpenSandbox nat-redirect works under it (Pitfall 2 workaround), but needs KVM + ~500ms boot + more RAM — over-engineering for the mini-PC (deferred, D-15). gVisor stays the opt-in tier. |
| Docker socket into the aura daemon | `tecnativa/docker-socket-proxy` (RO-scoped API) | Direct socket = simplest but broad; a socket-proxy narrows the daemon's Docker API surface. **Flag for the plan** (Open Question 5). |

**Installation:**
```bash
# Promote the two already-present modules from indirect to direct:
go get github.com/moby/moby/client@v0.4.1
go get github.com/moby/moby/api@v1.54.2
go mod tidy
# Verify the exact ContainerCreate options-struct shape BEFORE writing the backend (Pitfall 1):
go doc github.com/moby/moby/client.ContainerCreateOptions
go doc github.com/moby/moby/api/types/container.HostConfig
```

**Version verification (2026-07-06):**
```
github.com/moby/moby/client v0.4.1  — VERIFIED present in go.sum (hash-pinned h1:DMQgisVoMkmMs7fp3ROSdiBnoAu8+vo3GggFl06M/wY=)
github.com/moby/moby/api    v1.54.2 — VERIFIED present in go.sum (hash-pinned)
```
Both are the canonical `github.com/moby/moby` org (the upstream of Docker Engine). No new language-registry package is introduced by this phase.

## Package Legitimacy Audit

> This phase introduces **no new pip/npm/crates package** — it promotes two already-vendored Go modules and adopts one Apache-2.0 container image built from source. `slopcheck` is a pip/npm hallucination detector and does not apply to Go modules (integrity is enforced by `go.sum` hashes) or source-built images. The Go-native verification below stands in.

| Package | Registry | Age | Provenance | Verification | Disposition |
|---------|----------|-----|------------|--------------|-------------|
| `github.com/moby/moby/client` | Go proxy | mature (Moby project) | Official `github.com/moby/moby` org (Docker Engine upstream) | `go.sum` hash-pinned; `go mod why` → testcontainers-go + Authula | **Approved** (promote to direct) |
| `github.com/moby/moby/api` | Go proxy | mature | Same org | `go.sum` hash-pinned | **Approved** (already indirect; make direct if imported) |
| OpenSandbox egress image | source (github.com/alibaba/OpenSandbox) | active (Alibaba) | Apache-2.0, verified license | Built from a pinned commit; `docker build components/egress/`; image digest-pinned in compose | **Approved with caveat** — pin the source commit + image digest; see Pitfall 2 (gVisor) + Open Question 2 (Docker-standalone). |
| `agent-sandbox/agent-sandbox` v0.7.0 | source | active | Apache-2.0, E2B-compatible | **Not imported now** — DGX-tier forward-bet only (D-01/D-15). | Deferred (no dep added) |

**Packages removed due to slopcheck [SLOP] verdict:** none (slopcheck N/A for this ecosystem).
**Packages flagged as suspicious [SUS]:** none. The OpenSandbox *adoption* risk is integration-fit (K8s-centric, gVisor-incompatible DNS-redirect), not supply-chain — pin the commit + digest and gate behind the Pitfall-2 decision.

## Architecture Patterns

### System Architecture Diagram

```
                                  ┌─────────────────────── AURA DAEMON (host / aura service container) ───────────────────────┐
  model tool call                 │                                                                                           │
  shell_exec / fs_* / snippet ───▶│  execTool ──▶ gateway.Decide (GATE-01, Phase 35) ──allow──▶ tool.Execute                  │
                                  │                                                              │                            │
                                  │                                    RuntimeProfile.Strict()? ─┤                            │
                                  │                              no (dev/local_trusted)          │ yes (hardened/production)  │
                                  │                                    │                         ▼                            │
                                  │                              os/exec on HOST          SandboxRouter.Resolve(identityctx)  │
                                  │                              (unchanged, D-domain)           │  get-or-create by id       │
                                  │                                                              ▼                            │
                                  │                                              DockerBackend (moby/moby/client) ──┐         │
                                  │                                                 │ create/exec/cp/suspend/resume  │ Docker  │
                                  │   migration-0009 scheduler ──idle-TTL──▶ Suspend │                                │ socket  │
                                  │   (new TaskKind: sandbox_reap)      ──deprovision─▶ Stop(Delete)                  │         │
                                  └──────────────────────────────────────────────────┼────────────────────────────┼─────────┘
                                                                                      │                            ▼
                                                            ┌──── PER-IDENTITY BOX (fat image, root, gVisor optional) ────┐  Docker
                                                            │  shell / python / node / uv / git / gh   ◀── docker cp in ──┤  Engine
                                                            │  /workspace  = named volume aura-box-<id> (RETAINED)        │
                                                            │  /workspace/.scratch = tmpfs (ephemeral)                    │
                                                            │  /skills, Agent.md, pyscripts materialized in               │
                                                            │  net: shares netns with ▼                                   │
                                                            └───────────────┬─────────────────────────────────────────────┘
                                                                            │ --network container:<box>
                                                            ┌───────────────▼──── EGRESS SIDECAR (CAP_NET_ADMIN only) ────┐
                                                            │  nftables filter floor: DROP RFC1918 + 169.254.169.254 +    │──▶ PUBLIC
                                                            │  shared-services bridge; ACCEPT public internet (D-05)      │    INTERNET
                                                            │  [opt-in tightened: OpenSandbox DNS-proxy FQDN allowlist]   │  ✗ RFC1918
                                                            └─────────────────────────────────────────────────────────────┘  ✗ metadata
```
File-to-component mapping is in the Component Responsibilities of the Architectural Responsibility Map above; this diagram is data-flow only.

### Recommended Project Structure
```
internal/sandbox/usersandbox/
├── backend.go          # Backend interface (E2B verbs) + Sandbox/BoxHandle types
├── docker_backend.go   # DockerBackend over moby/moby/client (create/exec/cp/suspend/resume/stop)
├── spec.go             # SandboxSpec — the type that CANNOT express host-exposure flags (SBX-02)
├── translate.go        # private: SandboxSpec → moby container.Config/HostConfig, dangerous fields pinned
├── router.go           # SandboxRouter.Resolve(identityctx) — get-or-create, Strict() no-op, fail-CLOSED
├── egress.go           # egress sidecar spec (netns-share, CAP_NET_ADMIN, nft floor + optional FQDN)
├── reap.go             # idle-suspend TaskKind handler (wires into internal/cron)
├── materialize.go      # docker cp in (skills/Agent.md/pyscripts) + copy artifacts out (D-10)
└── *_test.go           # unit (translator unrepresentability) + docker_integration (lifecycle/leak/egress)
docker/aura-sandbox/
└── Dockerfile          # the shared fat box image (digest-pinned; derived from the packaging-box.md base)
```
Keep each file ≤600 LOC (CLAUDE.md NO GOD CLASS); `docker_backend.go` will be the largest — split by verb if it grows (`docker_backend_exec.go`, `docker_backend_lifecycle.go`).

### Pattern 1: The `SandboxRouter.Resolve(identityctx)` seam
**What:** A single get-or-create entry point the 5 tools call under strict profiles, returning a `BoxHandle` the tool `Exec`s against. Mirrors the additive `*ForIdentity` owner-scoping shape (Phase 36) with `local` fallback.
**When to use:** Inside each routed tool, *after* `gateway.Decide` allows, gated on `profile.Strict()`.
**Interface (E2B verbs, corrected by spike 082):**
```go
// Source: spike 082 (real agent-sandbox source + live kind run) + CONTEXT D-02
type Backend interface {
    Resolve(ctx context.Context, spec SandboxSpec) (BoxHandle, error) // idempotent direct create (NOT Claim)
    Exec(ctx context.Context, h BoxHandle, cmd ExecRequest) (ExecResult, error)
    Suspend(ctx context.Context, h BoxHandle) error                    // OperatingMode:Suspended — kill box, RETAIN volume
    Resume(ctx context.Context, h BoxHandle) error                     // transparent auto-resume
    Stop(ctx context.Context, h BoxHandle) error                       // ShutdownPolicy:Delete — destroy box+volume
}

type SandboxRouter struct {
    backend Backend
    profile config.RuntimeProfile
    // resolve is get-or-create keyed on identityctx.IdentityID(ctx); local fallback = the
    // seeded local id (mirror tools.localOwnerID "00000000-0000-0000-0000-000000000001").
}

// Route is the tool-facing call. A nil router OR a non-strict profile returns (nil,false)
// so the tool runs its existing host os/exec path unchanged (SC-4 dev no-op), EXACTLY like
// gateway.Decide's `if g == nil || !g.profile.Strict()` short-circuit.
func (r *SandboxRouter) Route(ctx context.Context) (BoxHandle, bool, error) {
    if r == nil || !r.profile.Strict() {
        return nil, false, nil // host-direct no-op
    }
    id := identityID(ctx) // identityctx.IdentityID(ctx) or localOwnerID
    h, err := r.backend.Resolve(ctx, r.specFor(id))
    if err != nil {
        return nil, true, err // fail-CLOSED (D-09) — the tool must NOT fall back to host
    }
    return h, true, nil
}
```
**Fail-CLOSED (D-09):** when `Route` returns `(_, true, err)`, the tool returns a deny-shaped `ToolResult` — mirror `gateway.gatewayApprovalRequiredResult` (a JSON `Preview` with an `error` field + guidance), *never* a host `os/exec`. The routed=true-but-errored branch is the containment invariant: strict + no box = no execution.

### Pattern 2: Host-exposure unrepresentability (SBX-02 — the crux)
**What:** The moby `container.HostConfig` *can* express every escape (`Privileged bool`, `NetworkMode("host")`, `Binds []string`, a `/var/run/docker.sock` mount). SBX-02 demands they be **unrepresentable**. The pattern: Aura's public `SandboxSpec` type simply **has no field** for them, and a single private translator builds the moby structs with the dangerous fields **pinned to safe constants**.
**When to use:** Always — this is the SBX-02 mechanism.
```go
// spec.go — the ONLY type callers construct. Note what is ABSENT: no Privileged, no
// NetworkMode, no Binds, no Devices, no CapAdd, no socket path. They cannot be set.
type SandboxSpec struct {
    IdentityID   string
    Image        string        // digest-pinned fat image
    WorkspaceVol string        // named volume aura-box-<id>
    Runtime      RuntimeClass  // enum {Runc, Runsc} — NOT a free string; Runsc only under server_production
    Egress       EgressPolicy  // {Floor, FQDNAllowlist []string} — never "host"/"none-with-holes"
    Limits       Resources     // {NanoCPUs, MemoryBytes, PidsLimit}
}

// translate.go — private. The dangerous moby fields are literals here and NOWHERE else.
func toHostConfig(s SandboxSpec) *container.HostConfig {
    return &container.HostConfig{
        // --- pinned safe, UNCONDITIONAL (SBX-02) ---
        Privileged:  false,
        NetworkMode: network.NetworkMode(""),   // never "host"; egress via the sidecar netns
        Binds:       nil,                         // no host bind-mounts — volumes only (D-10)
        // (no docker.sock: Mounts is built ONLY from the named volume + tmpfs below)
        CapDrop:     []string{},                  // keep default caps (D-12: not a jail)
        Mounts: []mount.Mount{
            {Type: mount.TypeVolume, Source: s.WorkspaceVol, Target: "/workspace"},
            {Type: mount.TypeTmpfs, Target: "/workspace/.scratch"},
        },
        Runtime: s.Runtime.dockerRuntime(),       // "" (runc) or "runsc"
        Resources: container.Resources{
            NanoCPUs:  s.Limits.NanoCPUs,
            Memory:    s.Limits.MemoryBytes,
            PidsLimit: &s.Limits.PidsLimit,
        },
    }
}
```
**Test (SBX-02, test-asserted):** two layers. (a) A *structural* test — reflect over `SandboxSpec` and assert no field named/typed for privileged/host-network/bind/socket exists (fails at compile if someone adds one, or use a golden field-list). (b) A *behavioral* test — for a matrix of adversarial `SandboxSpec` inputs, call `toHostConfig` and assert `!hc.Privileged && hc.NetworkMode != "host" && hc.Binds == nil && no mount targets the docker socket`. This is the SBX-02 acceptance test.

### Pattern 3: Idle-suspend + scheduled-delete on the migration-0009 scheduler (SBX-03, D-08)
**What:** No new goroutine (goleak discipline). Add a system-seeded `TaskKind` (e.g. `sandbox_reap`) whose handler sweeps boxes idle past the TTL and `Suspend`s them; deprovision routes to `Stop(Delete)`.
**When to use:** The idle reaper + the scheduled-delete leg.
**How:** Follow `internal/cron/handlers/identity_purge.go` verbatim — it is the exact template:
- Define `const KindSandboxReap TaskKind = "sandbox_reap"` in the handlers package.
- Handler struct holds a consumer-declared seam interface (`SandboxReaper{ SuspendIdle(ctx, now) (n int, err error) }`) satisfied by the live `usersandbox` router — so `handlers` does NOT import `usersandbox` (avoids the reverse cycle, same as `IdentityPurger`).
- `Meta()` → `{Kind: KindSandboxReap, MaxDuration: 5*time.Minute, ReschedulesOnRecovery: false}` (idempotent sweep).
- Seed it at the composition root like `identity_purge` (migration 0033 already widened the scheduler kind CHECK for Phase 36; **a new migration widens the `scheduler_tasks.kind` CHECK to include `sandbox_reap`** — the CHECK is `kind IN ('reminder','agent_job','backup_postgres','backup_neo4j', ...)`).
- **Auto-resume is NOT scheduled** — it's inline in `Route`: `Resolve` transparently `Resume`s a `Suspended` box on the next tool call (D-08). Idle-tracking = a `lastUsedAt` per box the router bumps on each `Exec`.

### Pattern 4: Per-identity named volume (storage-enforced isolation, SBX-03)
**What:** One named volume `aura-box-<identityID>` per identity, mounted at `/workspace`. Isolation is storage-/kernel-enforced (proven live spike 078: A's `/workspace/secret.txt` invisible+unreadable from B's box), never app-prefix scoping (the spike non-negotiable — "app-only prefix scoping is a silent hole").
**Lifecycle:** `Resolve` = `VolumeCreate` (idempotent by name) + `ContainerCreate`. `Suspend` = `ContainerStop` (volume untouched). `Resume` = `ContainerStart` (or re-create against the same volume). `Stop(Delete)` = `ContainerRemove` + `VolumeRemove` (only on explicit deprovision). **Never `--rm`** (Pitfall 5).

### Pattern 5: Routing the 5 tools into the box (SBX-01, D-11)
**What:** `shell_exec`, `shell_bg`, `fs_read`, `fs_write`, skill-snippet exec each gain a `Router *usersandbox.SandboxRouter` field (nil = host-direct). `web_fetch`/`web_search` do NOT (D-11 — already SSRF-guarded host-side).
**Per-tool mapping:**
- `shell_exec` → `router.Route(ctx)`; if routed, `backend.Exec(h, ExecRequest{Cmd: []string{"/bin/sh","-c", cmd}, Dir: workdir, Env: env})` instead of `exec.CommandContext`. The cwd-tracking marker wrap (`wrapForCwdTracking`) still applies — it's shell-level, runs the same inside the box.
- `shell_bg` → the background registry's `start` runs `Exec` in a detached box exec (streamed). **Open Question 4:** the current `BackgroundShells` holds a host `*exec.Cmd`; routing background jobs into a box exec-stream needs the registry to hold a box-exec handle instead of a `cmd`. Scope this carefully — it's the most invasive of the five.
- `fs_read` → `backend.Exec(h, cat/read)` or `CopyFromContainer` + parse; `fs_write` → `CopyToContainer` (tar stream) or `Exec` a write. The atomic-write helper is host-only today; inside the box a simple write is fine (the box IS the boundary).
- skill-snippet exec → already designed for this: `snippet.go` has `SnippetSandboxPath` (`/skills/<name>/<name>.<ext>`) + `SnippetInvocation` returning the in-box path + interpreter. `action=use` already returns both `HostPath` and `SandboxPath` (D-01 host-primary today); under strict, the tool uses `SandboxPath` + `backend.Exec`.
- `display/normalize.go` **already handles a `sandbox_exec` tool name** (returns `CodeInput`) — the preview surface is pre-wired.

### Pattern 6: Egress sidecar (SBX-04, D-05/D-07) — split floor vs allowlist
**What:** A per-box sidecar sharing the box netns via `--network container:<box>`, `CAP_NET_ADMIN` on the sidecar only. Two distinct enforcement jobs with different runtime compatibility:
1. **Default floor (D-05, always-on, both runc + gVisor):** a **filter-table** nftables ruleset — `DROP` to `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `169.254.169.254/32`, and the shared-services Docker bridge subnet; `ACCEPT` all else (public internet, D-04). The filter table is implemented by gVisor's netstack → **works under `runsc`**. This is the tenancy boundary that satisfies SBX-03/SBX-04-core.
2. **Tightened FQDN allowlist (D-06 opt-in, runc-only):** OpenSandbox egress full mode — a DNS proxy at `127.0.0.1:15353` with `iptables REDIRECT` in the **`nat` table** + dynamic nftables allow-sets. **gVisor does NOT implement the `nat` table (issue #934) → this mode is mutually exclusive with `runtime: runsc`** (Pitfall 2).
**OpenSandbox deployment facts** `[CITED: github.com/alibaba/OpenSandbox/docs/components/egress.md]`:
- Modes: `dns` (default) / `dns+nft` (recommended for strict). Config via env: `OPENSANDBOX_EGRESS_MODE`, `OPENSANDBOX_EGRESS_RULES` (JSON policy), `OPENSANDBOX_EGRESS_HTTP_ADDR` (`:18080`), `OPENSANDBOX_EGRESS_TOKEN`.
- Rules support domains, IPs, CIDRs, wildcards (`*.pypi.org`); static rules in `/var/egress/rules/` (`deny.always`/`allow.always`), hot-reloaded every minute, `deny.always` highest precedence.
- Startup auto-whitelists `127.0.0.1` + `/etc/resolv.conf` nameserver IPs.
- Runs under `opensandbox-supervisor` (crash recovery + backoff). Build: `docker build -t opensandbox/egress:local components/egress/`.
- **Not compatible with an existing service-mesh sidecar** (both rewrite outbound in the shared netns).

### Anti-Patterns to Avoid
- **Distroless / non-root / read-only-rootfs the box** (D-12): each individually breaks `shell_exec` parity (distroless has no shell → exit 127; read-only blocks self-extension; `USER 65532` forces non-root). The current `docker/aura` Dockerfile is already fat — keep that posture.
- **`docker run --rm` for a suspendable box** (Pitfall 5): `--rm` auto-removes on stop, destroying the box you meant to `Suspend`+retain.
- **Inferring egress posture from the dev stack** (`sandbox-runtime.md`): Docker Desktop/WSL vpnkit NATs the bridge regardless of `enable_ip_masquerade:false` → an allowlist is *advisory* on dev. "Probe, don't inherit" — the enforced tier is native-Linux-only.
- **Mounting the host Docker socket into the BOX** (SBX-02): the socket goes into the aura *daemon* (to spawn boxes), never into a per-identity box.
- **Trusting app-level `WHERE identity_id` for volume isolation**: use bucket/volume-per-identity (storage-enforced) — the spike non-negotiable.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Docker container lifecycle | Shelling `docker run`/`exec`/`cp` via `os/exec` | `moby/moby/client` Go SDK | The CLI path hits MSYS path-mangling on Windows (spike 078) + no structured errors; the SDK is D-01-locked. (Spikes proved the *model* via CLI; the SDK binding is the build task.) |
| Egress FQDN allowlist enforcement | The advisory ~80-LOC CONNECT proxy (spike 009) | OpenSandbox egress (DNS+nftables) | The proxy's `HTTPS_PROXY` is advisory — hostile code egresses around it. Enforcement needs to be the only route (nftables). D-03. |
| Idle reaper | A new `time.Ticker` goroutine | The migration-0009 scheduler (`sandbox_reap` TaskKind) | A new goroutine trips the goleak gate; the scheduler already owns claim/heartbeat/reschedule. D-08 + `identity_purge` template. |
| cgroup v2 CPU/mem/pids caps | Writing `cgroupfs` files manually | `container.HostConfig.Resources{NanoCPUs, Memory, PidsLimit}` | Docker sets the cgroup for you (proven spike 059: `cpu.max`/`memory.max`/`pids.max` reflect the limits). |
| Host-exposure safety | Runtime validation "reject if privileged==true" | A spec type that omits the field entirely (Pattern 2) | Validation can be bypassed/forgotten; unrepresentability is structural (SBX-02 wants "cannot be set", not "is checked"). |
| DGX/K8s backend | Vendoring `sigs.k8s.io/agent-sandbox` CRD structs | An E2B-protocol `Backend` seam | Spike 082: `agent-sandbox/agent-sandbox` v0.7.0 already exposes E2B verbs + a `/mcp` server; speak the protocol, drop it in unmodified. Both upstreams are hard K8s-bound → Aura owns Docker regardless. |

**Key insight:** In this domain the *boundary* is the security control, not the inside of the box. Every "harden the box internals" instinct (drop caps, read-only, non-root) is both wrong (breaks parity, D-12) and unnecessary (the namespace + volume + cgroup + egress-netns boundary already contains). Spend effort on making escapes *unrepresentable*, not on crippling capability.

## Runtime State Inventory

> This phase is part-refactor (routing 5 host tools into a box) + part-greenfield (the box runtime). The runtime-state audit:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| **Stored data** | Per-identity named volumes `aura-box-<id>` are **NEW** (no legacy data to migrate). Existing per-identity host dirs — `$AURA_SKILLS_DIR/{id}/`, `~/.aura/agents/<id>/` (Agent.md), `~/.aura/pyscripts/{id}/` — currently live on the host and are read by host tools. Under strict, these must be **materialized into the box volume** at create/resume (D-10, `docker cp`), not host-bound. | Code: `materialize.go` copies skills/Agent.md/pyscripts in. No data migration (greenfield volumes); the host dirs remain the source-of-truth the daemon copies from. |
| **Live service config** | `compose.yaml` — the **aura daemon needs Docker API access** to spawn boxes (host socket or socket-proxy). `scheduler_tasks.kind` CHECK constraint must widen for `sandbox_reap`. `compose.gvisor.yaml` exists (runsc tier). The egress sidecar image must be built + referenced. | Compose edit (daemon Docker access — Open Q5) + **new migration** (widen `kind` CHECK, mirror 0033) + build/pin the egress + sandbox images. |
| **OS-registered state** | None. No Task Scheduler / systemd / pm2 registrations embed sandbox state (the reaper lives in the DB scheduler, not the OS). | None — verified: idle-suspend is a `scheduler_tasks` row, not an OS timer. |
| **Secrets / env vars** | **New** `AURA_SANDBOX_*` knobs (image ref, idle-TTL, cgroup caps, egress mode/allowlist, runtime class). No existing secret is renamed. The box must **never** inherit host secrets (`sandbox-runtime.md`: "don't put secrets in the container env beyond what the run needs"; `mergeEnv` already strips secret-like vars via `secret.IsSecretEnvVar` — preserve that on the box exec path). | Code: catalog the new `AURA_SANDBOX_*` env (PROF env-catalog discipline); ensure box `Exec` env is scrubbed like host `mergeEnv`. |
| **Build artifacts / images** | The fat box image (`docker/aura-sandbox/Dockerfile` — NEW, or reuse `docker/aura`) must be built + **digest-pinned** (D-13). The shared `uv` warm-cache volume is new. | Build + digest-pin the sandbox image; create the warm-cache named volume. |

**The canonical question — after every file is updated, what runtime systems still hold old state?** For a rename this matters; here it's greenfield box state + one compose/migration change. Nothing is silently cached in an external live service (the boxes/volumes are created fresh by this phase's own code).

## Common Pitfalls

### Pitfall 1: `moby/moby/client` v0.4.1 is NOT the SDK your training data knows
**What goes wrong:** Every Docker-SDK Go example online uses `github.com/docker/docker/client` with `cli.ContainerCreate(ctx, &container.Config{}, &container.HostConfig{}, &network.NetworkingConfig{}, nil, name)`. The **new `moby/moby/client` v0.4.1 uses an options-struct API** — `ContainerCreate(ctx, options ContainerCreateOptions) (ContainerCreateResult, error)`, `ExecCreate(ctx, id, ExecCreateOptions)`, `CopyToContainer(ctx, id, CopyToContainerOptions)`. Writing the classic signature won't compile.
**Why it happens:** The client was extracted into an independently-versioned v0.x module; the API is mid-refactor and pre-1.0 (churn risk).
**How to avoid:** Before writing `docker_backend.go`, run `go doc github.com/moby/moby/client.ContainerCreateOptions` and `go doc github.com/moby/moby/api/types/container.CreateRequest` against the *vendored* v0.4.1 to get the exact field wiring (the client options likely embed `api/types/container.{Config,HostConfig}` + `network.NetworkingConfig`). The `container.HostConfig`/`Resources`/`NetworkMode` types are confirmed present (`NanoCPUs`, `Memory`, `PidsLimit *int64`, `NetworkMode("container:<id>")`/`("none")` with `IsHost()`/`IsContainer()` methods). **Escape hatch:** if v0.x API churn blocks the build, `docker/docker/client` (classic, stable) is the fallback — but D-01 prefers moby/moby/client.
**Warning signs:** Compile errors on `ContainerCreate` arg count; `undefined: types.ContainerCreateConfig`.

### Pitfall 2: gVisor (`runsc`) ⊥ the OpenSandbox DNS-redirect egress (issue #934)
**What goes wrong:** The OpenSandbox egress sidecar installs an `iptables REDIRECT` rule in the **`nat` table** to intercept DNS on port 53. **gVisor's netstack does not implement the `nat` table** (only `filter` + `mangle`) — the sidecar fails with `can't initialize iptables table 'nat': Table does not exist` (a long-standing gVisor limitation, gvisor#170). So D-06's *tightened FQDN allowlist* (OpenSandbox full mode) and D-12's *gVisor tier* **cannot both be on for the same box**.
**Why it happens:** gVisor's userspace kernel is a reduced netfilter surface; the DNS-redirect-via-nat pattern is fundamental to OpenSandbox's transparent proxying.
**How to avoid:** **Split the two egress jobs** (Pattern 6): (a) the always-on D-05 **default floor** uses only **filter-table** DROP rules → gVisor-compatible, satisfies the SBX-03/04 tenancy boundary under both runtimes; (b) the opt-in D-06 **FQDN allowlist** (OpenSandbox nat-redirect) is **runc-only** — document it as mutually exclusive with `runtime: runsc`. The realistic default posture (runc + filter-floor) needs neither the nat table nor gVisor simultaneously. (Kata Containers is the #934 workaround — full per-box kernel — but it's the deferred DGX tier, not the mini-PC.) **Surface this as a REQUIREMENTS/ADR note** so the operator's "gVisor under server_production" (D-06) and "adopt OpenSandbox egress" (D-03) don't silently collide.
**Warning signs:** `runsc` box + egress sidecar → sidecar CrashLoopBackOff with the `nat` table error.

### Pitfall 3: On Docker Desktop / WSL, egress enforcement is advisory (vpnkit NAT)
**What goes wrong:** vpnkit NATs the bridge regardless of `enable_ip_masquerade:false`, so a box egresses directly around any proxy/nftables rule (spike 009). An egress test that "passes" on dev proves nothing.
**Why it happens:** Docker Desktop's network stack is a dev artifact, never a design input (`sandbox-runtime.md`: "Probe, don't inherit").
**How to avoid:** The SBX-04 egress-DROP integration test **must run on native-Linux dockerd with a genuinely non-masquerading bridge** (CI Linux or the real 32GB host), and be `t.Fatal`-under-`$CI` (no-skip-as-green). Mark the dev-run of the egress test as informational-only.
**Warning signs:** Egress-block test green on Windows/WSL but the box can `curl` an RFC1918 host.

### Pitfall 4: The aura daemon needs Docker access — that's a new privileged surface
**What goes wrong:** To spawn per-identity boxes, the aura *daemon* must reach `dockerd` (host socket mount or TCP). Under `server_production` aura itself runs as a container, so this is the "sibling containers" pattern — and a mounted host socket gives the daemon broad Docker control (a real, if daemon-scoped, escalation surface).
**Why it happens:** Docker-direct (not K8s) means someone holds the socket; it's the daemon, never the box.
**How to avoid:** Mount the socket **only into the aura service**, never a box (SBX-02 keeps boxes socket-free). Consider a **socket-proxy** (`tecnativa/docker-socket-proxy`) restricting the daemon to the container/exec/volume/image API subset it needs. Document this in the SBX-05 ADR as the accepted residual (the daemon is trusted; the boxes are not). **Open Question 5.**
**Warning signs:** A box with `/var/run/docker.sock` present (SBX-02 test must catch this); the daemon granted more Docker API verbs than lifecycle needs.

### Pitfall 5: `--rm` destroys the box you meant to suspend
**What goes wrong:** Spikes 078/059 used `docker run -d --rm` (auto-remove on stop). D-08 requires `Suspend` = stop-but-retain. `--rm` + stop = the box (and its exec state) is gone; only the named volume survives, but the container object you'd `Resume` is deleted.
**Why it happens:** The spike ergonomics (ephemeral demo) contradict the production lifecycle (suspend/resume).
**How to avoid:** Create suspendable boxes **without `AutoRemove`** (moby `HostConfig.AutoRemove=false`). `Suspend` = `ContainerStop`; `Resume` = `ContainerStart` (same container, same volume). Only `Stop(Delete)` calls `ContainerRemove`. (Alternatively, if you re-create on resume, retain only the volume — but then "resume" loses in-container process/tmpfs state; retaining the stopped container is the D-08-faithful choice.)
**Warning signs:** `Resume` returns "no such container".

### Pitfall 6: Windows dev host ↔ Linux box path + exec translation
**What goes wrong:** The host tools resolve a Windows Git-Bash for `shell_exec` (`windowsShell()` prefers `C:\Program Files\Git\bin\bash.exe`) and use Windows cwd tracking (`pwd -W`). Inside a Linux box, paths are POSIX and `pwd -W` is meaningless. `docker cp` with a Windows source path + MSYS mangling breaks (`docker -v name:/path` path-guard, spike 078).
**Why it happens:** The routed path crosses an OS boundary the host-direct path never did.
**How to avoid:** In the box-exec branch, use `/bin/sh -c` + plain `pwd` (POSIX) — not the Windows shell resolution. Drive `docker cp`/exec through the **Go SDK** (tar streams via `CopyToContainer`), never a shelled `docker` command, sidestepping MSYS entirely. Materialize skills/Agent.md/pyscripts by tar-streaming bytes, not host path binds.
**Warning signs:** `pwd -W` marker garbage in box output; `docker cp` "cannot open" on a `C:\...` path.

### Pitfall 7: The Go SDK binding is UNPROVEN (spikes used the CLI)
**What goes wrong:** Spike 078's "How to Run" is `docker run`/`docker exec` PowerShell — the *model* is proven, but no spike exercised a single `moby/moby/client` Go call. Treating "the box works" as "the SDK code works" skips real risk (auth to `dockerd`, tar-stream `CopyToContainer`, exec attach/stream, `DEADLINE`/context cancellation on a long exec).
**How to avoid:** Wave 0 should stand up a `docker_integration`-tagged smoke that does the full SDK round-trip (pull → volume → create → exec → cp in → cp out → suspend → resume → stop) against a live `dockerd`, on native Linux, before building the tool routing on top. This is the SDK-binding proof the spikes never did.
**Warning signs:** First real `dockerd` call is inside a tool at runtime.

## Code Examples

### Exec into the box (replacing `exec.CommandContext`)
```go
// Source: moby/moby/client v0.4.1 options-struct API (verify exact field names via `go doc`)
func (b *DockerBackend) Exec(ctx context.Context, h BoxHandle, r ExecRequest) (ExecResult, error) {
    ec, err := b.cli.ExecCreate(ctx, h.ContainerID, /* ExecCreateOptions{
        Cmd: []string{"/bin/sh", "-c", r.Command}, WorkingDir: r.Dir, Env: scrubSecrets(r.Env),
        AttachStdout: true, AttachStderr: true } */)
    if err != nil { return ExecResult{}, fmt.Errorf("box exec create: %w", err) }
    att, err := b.cli.ExecAttach(ctx, ec.ID, /* ExecAttachOptions{} */)
    if err != nil { return ExecResult{}, fmt.Errorf("box exec attach: %w", err) }
    defer att.Close()
    // stdcopy.StdCopy(&outBuf, &errBuf, att.Reader) — demux the multiplexed stream
    // then ExecInspect for the exit code. Honor ctx cancellation (the WaitDelay analog).
    ...
}
```

### The cgroup caps + safe HostConfig (SBX-02 pinned fields)
```go
// Source: github.com/moby/moby/api/types/container HostConfig/Resources (verified fields)
hc := &container.HostConfig{
    Privileged: false,                         // pinned (SBX-02)
    Binds:      nil,                           // pinned — no host bind (SBX-02 / D-10)
    AutoRemove: false,                         // suspendable (Pitfall 5)
    Runtime:    runtimeClass,                  // "" or "runsc" (D-12, server_production only)
    Mounts: []mount.Mount{
        {Type: mount.TypeVolume, Source: "aura-box-" + id, Target: "/workspace"},
        {Type: mount.TypeTmpfs,  Target: "/workspace/.scratch"},
        {Type: mount.TypeVolume, Source: "aura-uv-cache", Target: "/root/.cache/uv"}, // shared warm cache (D-13)
    },
    Resources: container.Resources{
        NanoCPUs:  2_000_000_000, // 2 CPU (D-14 start)
        Memory:    2 << 30,       // 2 GiB
        PidsLimit: ptr(int64(512)),
    },
}
// NetworkMode is left default here; the egress sidecar joins THIS box's netns via
// NetworkMode("container:"+boxID) on ITS create (D-07). The box never gets "host".
```

### Idle-suspend reaper handler (mirrors identity_purge.go)
```go
// Source: internal/cron/handlers/identity_purge.go (verified template)
const KindSandboxReap TaskKind = "sandbox_reap"
type SandboxReaper interface { SuspendIdle(ctx context.Context, now time.Time) (int, error) }
type SandboxReapHandler struct{ Reaper SandboxReaper }
func (SandboxReapHandler) Meta() HandlerMeta {
    return HandlerMeta{Kind: KindSandboxReap, MaxDuration: 5*time.Minute, ReschedulesOnRecovery: false}
}
func (h SandboxReapHandler) Run(ctx context.Context, _ Job) (string, error) {
    if h.Reaper == nil { return "sandbox reap: disabled", nil }
    n, err := h.Reaper.SuspendIdle(ctx, time.Now().UTC())
    if err != nil { return "", fmt.Errorf("sandbox reap: %w", err) }
    return fmt.Sprintf("sandbox reap ok: suspended %d idle box(es)", n), nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `docker/docker/client` monolith SDK | `moby/moby/client` independently-versioned module (v0.x) | Moby module split, 2025–26 | Options-struct API; the SDK examples online are stale (Pitfall 1). |
| Vendor `sigs.k8s.io/agent-sandbox` CRD structs (spike 079 paper contract) | Speak the **E2B protocol** as the `Backend` seam; `agent-sandbox/agent-sandbox` v0.7.0 exposes it + a `/mcp` server | spike 082 correction (2026-07-04) | DGX drop-in is unmodified; no CRD translation layer. `Resolve`=direct create (not Claim); idle=`Suspended` (not delete). |
| Advisory `HTTPS_PROXY` CONNECT allowlist (spike 009) | Enforced nftables (OpenSandbox egress) — filter-floor always-on + optional DNS-redirect FQDN | D-03/D-06 | Enforcement is the only route; but nat-redirect ⊥ gVisor (Pitfall 2). |
| `--network none` default (SBX-04 literal) | full-internet-minus-internal (D-04/D-05) | Phase 37 CONTEXT amendment | Claude-Code parity; the carve-out is the tenancy boundary only. **REQUIREMENTS-amendment required.** |

**Deprecated/outdated:**
- The distroless box (`ec7fe2f6`): already reverted — the current `docker/aura` Dockerfile is fat (debian-slim, root, python3/node/uv/mcp-neo4j-cypher, no `USER`). Confirm no regression re-introduces it.
- Spike 079's `Router.Resolve ≈ SandboxClaim` mapping: **wrong** (`SandboxClaim.WarmPoolRef` is required) — superseded by 082's direct-create mapping (D-02).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | OpenSandbox's egress component runs cleanly as a **standalone Docker sidecar** (not just a K8s pod) with `--network container:<box>` + `--cap-add NET_ADMIN` + `OPENSANDBOX_EGRESS_RULES` | Standard Stack / Pattern 6 | If it's effectively K8s-bound, D-03 adoption stalls → fall back to a bespoke `nft` filter-floor sidecar (Open Q2). Medium — the floor is ~10 nft rules; the FQDN allowlist is the harder part to replace. |
| A2 | The `moby/moby/client` v0.4.1 `ContainerCreateOptions` embeds/references `api/types/container.{Config,HostConfig}` + `network.NetworkingConfig` | Pitfall 1 / Code Examples | If the wiring differs, the backend code needs reshaping — but `go doc` at build time resolves it deterministically. Low (verifiable). |
| A3 | A **filter-table** nftables ruleset (RFC1918/metadata/bridge DROP) works under gVisor `runsc` (only nat is missing) | Pitfall 2 / Pattern 6 | If gVisor's filter table is also incomplete for egress DROP, the always-on floor can't run under runsc → floor becomes runc-only too. Medium — verify on the real host during the benchmark. |
| A4 | A shared `uv` warm-cache **volume** across identities does not leak data between boxes (cache is content-addressed, read-mostly) | Code Examples (mounts) | If uv writes identity-identifying paths into the shared cache, it's a minor cross-identity signal (not a data leak of `/workspace`). Low — cache is package artifacts, but confirm no creds land there. |
| A5 | Routing `shell_bg` (background jobs) into a box exec-stream is feasible without redesigning `BackgroundShells` beyond swapping `*exec.Cmd` for a box-exec handle | Pattern 5 / Open Q4 | If box exec-attach can't cleanly detach+poll like a host process group, `shell_bg`-in-box is the riskiest of the five. Medium — scope it as its own plan slice. |

**If this table looks long:** these are the honest unknowns a spike-validated *model* still leaves at the *code* layer — flag each for the plan; none is a kill risk.

## Open Questions

1. **gVisor tier vs OpenSandbox FQDN allowlist — which wins under `server_production`?**
   - What we know: the nat-redirect FQDN mode ⊥ gVisor (Pitfall 2); the filter-floor works under both.
   - What's unclear: does any deployment actually want *both* runsc *and* a tightened FQDN allowlist simultaneously?
   - Recommendation: Ship the filter-floor as always-on (both runtimes); document FQDN-allowlist mode as runc-only; record the mutual-exclusion in the SBX-05 ADR. Let the operator pick per-deployment.

2. **Is OpenSandbox egress adoptable as a plain Docker sidecar (non-K8s)?**
   - What we know: it's netns-based, Apache-2.0, `docker build components/egress/` works, env-configured; but the docs assume pods/Service-CIDR.
   - What's unclear: whether it runs correctly with `--network container:<box>` outside K8s without the OpenSandbox control plane.
   - Recommendation: Wave-0 spike the OpenSandbox-egress-as-Docker-sidecar round-trip; keep a bespoke `nft` filter-floor sidecar as the proven fallback (honors D-03's "search first" while de-risking).

3. **E2B `envd` subset — exec + files only?**
   - What we know (spike 082): the E2B gateway's terminal/files endpoints talk to an in-box `envd` daemon. Aura's `DockerBackend` uses the Docker exec/cp API directly today, not `envd`.
   - What's unclear: whether adopting the E2B protocol *wholesale* (for DGX forward-compat) needs `envd` in the box now.
   - Recommendation: For Phase 37, `DockerBackend.Exec`/`cp` uses the Docker API (no `envd`); the E2B *verbs* are the Go interface shape (D-01), not the wire protocol. Defer `envd` to the DGX tier.

4. **`shell_bg` (background jobs) inside the box** (Pattern 5) — the current registry holds a host `*exec.Cmd` with a process group; a box exec-stream needs a different handle + poll/kill mapping to `ExecInspect`/box signal. Scope as its own plan slice; it's the most invasive of the five tools.

5. **How does the aura daemon reach `dockerd` under `server_production`?** (Pitfall 4) — host socket vs socket-proxy vs TCP-with-TLS. Recommendation: host socket into the aura service only, hardened via `tecnativa/docker-socket-proxy` to the lifecycle API subset; record as the ADR's accepted residual.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker Engine (dev, WSL) | All box lifecycle + `docker_integration` tests | ✓ | 29.6.1 (spike 082); **15.47 GiB cap** | — (dev cap is why D-14 benchmark needs the real host) |
| Native-Linux dockerd (non-masquerading bridge) | SBX-04 egress-DROP enforcement test (Pitfall 3) | CI Linux ✓ / dev ✗ | — | Egress test is informational-only on Docker Desktop |
| gVisor `runsc` | SBX-04 `runtime: runsc` selectability | ✗ on Docker Desktop; ✓ native-Linux/arm64 | — | runc baseline; runsc is the opt-in appliance tier |
| Real 32GB host | D-14 concurrent-identity soak (10–20 boxes, Resolve/Resume p95) | ✗ in dev (15.47 GiB WSL) | — | **No fallback — Gate-3 blocks on the real host** |
| `testcontainers-go` | integration test harness | ✓ (indirect) | v0.42.0 | Drive `dockerd` directly |
| OpenSandbox egress image | D-06 tightened FQDN allowlist | build-from-source | main (Apache-2.0) | Bespoke `nft` sidecar (Open Q2) |

**Missing dependencies with no fallback:**
- The **real 32GB host** for the D-14 soak benchmark (Gate-3). Dev WSL (15.47 GiB) cannot validate the 10–20-box RAM envelope + p95 targets — flag every D-14 metric as "real-host-only".

**Missing dependencies with fallback:**
- gVisor on dev → runc baseline (the box runs identically; only the host boundary differs).
- OpenSandbox-as-Docker-sidecar uncertainty → bespoke `nft` filter-floor sidecar.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` + `testify` (indirect) + `pgregory.net/rapid` (property-based) + `go.uber.org/goleak` (leak gate) |
| Config file | none (Go convention); build tags gate tiers |
| Quick run command | `go test ./internal/sandbox/usersandbox/` (unit — translator/router/spec, no Docker) |
| Full suite command | `go test -tags="docker_integration" -race ./internal/sandbox/...` (needs a live dockerd) + `make quality-full` |

**Proposed build tag:** `docker_integration` (new — mirrors the existing `db_integration`/`neo4j_integration` convention). The skip-helper must `t.Fatal` under `$CI` when `dockerd` is unreachable (no-skip-as-green, CLAUDE.md).

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SBX-01 | Under strict, `shell_exec`/`fs_*` route into the box; host FS unreachable | integration | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestRoute_StrictExecInBox` | ❌ Wave 0 |
| SBX-01 | Under dev/local_trusted, routing is a no-op (host-direct) | unit | `go test ./internal/sandbox/usersandbox/ -run TestRoute_DevNoOp` | ❌ Wave 0 |
| SBX-02 | Privileged/host-net/bind/socket unrepresentable — structural | unit | `go test ./internal/sandbox/usersandbox/ -run TestSpec_NoHostExposureFields` | ❌ Wave 0 |
| SBX-02 | Translator always emits safe HostConfig for adversarial specs | unit (table + rapid) | `go test ./internal/sandbox/usersandbox/ -run TestTranslate_PinsSafe` | ❌ Wave 0 |
| SBX-03 | Cross-identity `/workspace` leak impossible (A writes, B can't read) | integration | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestVolume_CrossIdentityDeny` | ❌ Wave 0 |
| SBX-03 | create→suspend→resume→delete lifecycle; volume retained on suspend, gone on delete | integration | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestLifecycle_SuspendResumeDelete` | ❌ Wave 0 |
| SBX-03 | Idle-TTL reaper suspends via the scheduler; next call auto-resumes | integration | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestReap_IdleSuspendAutoResume` | ❌ Wave 0 |
| SBX-04 | Default floor: box reaches public internet, CANNOT reach RFC1918/metadata/bridge | integration (native-Linux) | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestEgress_FloorDropsInternal` | ❌ Wave 0 (Pitfall 3: native-Linux only) |
| SBX-04 | Tightened allowlist: an allowed host resolves, a disallowed host is DROPPED | integration (native-Linux, runc) | `go test -tags=docker_integration ./internal/sandbox/usersandbox/ -run TestEgress_FQDNAllowlist` | ❌ Wave 0 |
| SBX-04 | `runtime: runsc` selectable under server_production (spec accepts it) | unit | `go test ./internal/sandbox/usersandbox/ -run TestSpec_RunscOnlyServerProduction` | ❌ Wave 0 |
| SBX-05 | ADR file exists with the required decisions | manual/doc | reviewer checks `docs/adr/` (or `.planning/`) | ❌ Wave 0 |
| GATE-01/D-09 | Strict + box-create failure → fail-CLOSED ToolResult, never host | unit | `go test ./internal/agent/tools/ -run TestShellExec_FailClosedNoHostFallback` | ❌ Wave 0 |
| — | No goroutine leak on box lifecycle / reaper | integration | `goleak` in `TestMain` of the package | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/sandbox/usersandbox/` (unit — sub-second, no Docker).
- **Per wave merge:** `go test -tags=docker_integration -race ./internal/sandbox/...` on a live dockerd (CI Linux).
- **Phase gate:** `make quality-full` green (owned-surface coverage ≥85%, CLAUDE.md floor; mutation ≥70% on `translate.go`/`router.go`/`egress.go` per REL-02) **+ the D-14 soak on the real 32GB host** before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/sandbox/usersandbox/spec_test.go` — SBX-02 structural + translator tests (no Docker; do first).
- [ ] `internal/sandbox/usersandbox/router_test.go` — Strict()-no-op + fail-CLOSED (D-09).
- [ ] `internal/sandbox/usersandbox/docker_backend_integration_test.go` — the full SDK round-trip smoke (Pitfall 7) — the earliest real-dockerd proof.
- [ ] `internal/sandbox/usersandbox/egress_integration_test.go` — floor DROP + FQDN (native-Linux, `t.Fatal` under `$CI`).
- [ ] `internal/cron/handlers/sandbox_reap_test.go` — reaper handler (mirror `identity_purge_test.go`).
- [ ] Build-tag skip-helper for `docker_integration` that `t.Fatal`s under `$CI` when dockerd is unreachable.
- [ ] New migration widening `scheduler_tasks.kind` CHECK for `sandbox_reap` (mirror 0033).
- [ ] The D-14 benchmark harness (`internal/sandbox/usersandbox/bench_soak_test.go` or a `cmd/` tool) — 10–20 boxes, Resolve/Resume p95, RAM envelope — **real-host-only**, documented in `VALIDATION.md` Manual-Only.

## Security Domain

> `security_enforcement` is not set to `false` in config → enabled. This phase IS a security boundary (F-001/F-036 remediation).

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Containment boundary documented in the SBX-05 ADR (trust: daemon trusted, box untrusted). |
| V4 Access Control | yes | Per-identity volume isolation (storage-enforced); `SandboxRouter` keys on `identityctx` with `local` fallback — never cross-identity. |
| V5 Input Validation | yes | `SandboxSpec` is the only constructible type; the translator pins dangerous fields (SBX-02 = validation-by-unrepresentability). |
| V6 Cryptography | no (n/a) | No new crypto; do NOT leak host secrets into the box env (reuse `secret.IsSecretEnvVar` scrub on the box exec path). |
| V10 Malicious Code / Sandboxing | **yes (core)** | The whole phase: namespace + cgroup + seccomp-default + gVisor-opt-in + egress-netns boundary. |
| V12 Files & Resources | yes | Named-volume `/workspace` + tmpfs scratch; no host bind (D-10); artifacts copied out via `docker cp` (no host path exposure). |
| V13 API / SSRF | yes | `web_fetch`/`web_search` stay host-side (already SSRF-guarded, D-11); the box's egress floor blocks the metadata server (169.254.169.254) — an SSRF-via-box mitigation. |

### Known Threat Patterns for the per-identity Docker sandbox
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Container escape via Docker socket mount | Elevation of Privilege | Socket unrepresentable in `SandboxSpec` (SBX-02); never mounted into a box. |
| `--privileged` / host-network breakout | Elevation of Privilege | Pinned `Privileged:false`, no `NetworkMode("host")` — structurally absent (SBX-02). |
| Cross-identity data read via shared volume/prefix | Information Disclosure | Bucket/volume-per-identity, storage-enforced (spike 078 cross-deny); never app-prefix scoping. |
| Lateral movement to shared services (Postgres/Neo4j/Garage/agent-memory) | Elevation / Info Disclosure | Egress floor DROPs RFC1918 + the shared bridge (D-05); shared services only via identity-scoped MCP brokering. |
| Cloud-metadata SSRF (169.254.169.254) | Information Disclosure | Egress floor DROPs the metadata IP unconditionally (D-05). |
| Resource-exhaustion starvation of co-tenants | Denial of Service | cgroup v2 caps (2 CPU / 2GB / 512 pids start; D-14 tunes) — proven to bound (spike 059). |
| Host secret exfiltration via box env | Information Disclosure | Scrub secret-like env on the box exec path (`secret.IsSecretEnvVar`, as `mergeEnv` already does host-side). |
| Fail-open to host on box failure | Elevation of Privilege | Fail-CLOSED (D-09): strict + no box = deny ToolResult, never host `os/exec`. |
| Egress bypass on dev NAT | (test integrity) | Enforcement test is native-Linux only (Pitfall 3); dev is advisory. |

## Sources

### Primary (HIGH confidence)
- Codebase (verified by direct read): `internal/config/config_runtimeprofile.go` (`Strict()`), `internal/identityctx/identityctx.go`, `internal/agent/tools/{shell_exec,shell_bg,shell_bg_owner,fs_read,fs_write}.go`, `internal/gateway/{decide,approve,gateway}.go`, `internal/agent/llm_agent_retry.go` (execTool seam), `internal/cron/{scheduler,dispatch}.go` + `internal/cron/handlers/identity_purge.go`, `internal/skills/snippet.go`, `internal/agent/display/normalize.go` (`sandbox_exec` pre-wired), `cmd/aura/main.go` (tool wiring), `docker/aura/Dockerfile` (fat, already reverted), `go.mod`/`go.sum`, `compose.gvisor.yaml`, `.planning/REQUIREMENTS.md` (SBX-01..05, GATE-01), `internal/db/migrations/0009_scheduler.up.sql`.
- Spike blueprint (validated live): `.claude/skills/spike-findings-Aura/references/{multiuser-per-identity-isolation,sandbox-runtime,packaging-box}.md`; `.planning/spikes/078-per-identity-box-multiplexing/README.md` (live cross-deny), `.planning/spikes/082-agent-sandbox-realsource-contract/README.md` (E2B contract corrections).
- `github.com/moby/moby/api/types/container` (v1.54.2) — HostConfig/Resources/NetworkMode fields verified via pkg.go.dev.

### Secondary (MEDIUM confidence)
- `github.com/alibaba/OpenSandbox` — egress component README + `docs/components/egress.md` (mode/config/nftables/DNS-proxy) + **issue #934** (gVisor nat-table incompatibility, root cause quoted). Apache-2.0 confirmed.
- `github.com/moby/moby/client@v0.4.1` pkg.go.dev — options-struct API signatures (exact field internals to be confirmed via `go doc` at build time — Pitfall 1 / A2).

### Tertiary (LOW confidence)
- OpenSandbox-as-standalone-Docker-sidecar feasibility (A1/Open Q2) — inferred from the netns-based design + `docker build` support; not empirically confirmed outside K8s.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — moby modules verified in go.sum; D-01 locks them; OpenSandbox Apache-2.0 + design cited.
- Architecture / seams: HIGH — every integration point read directly (Strict() gate, execTool, scheduler handler template, tool wiring, snippet sandbox-path).
- moby/moby/client v0.4.1 exact API: MEDIUM — options-struct shape confirmed; exact field wiring needs `go doc` at build (Pitfall 1).
- Egress (OpenSandbox adoption + gVisor tension): MEDIUM — the gVisor⊥nat incompatibility is HIGH (issue #934 quoted); the Docker-standalone adoptability is MEDIUM (A1).
- Pitfalls: HIGH — grounded in spikes (078 CLI-only, 009 vpnkit NAT, 059 caps) + verified upstream facts.

**Research date:** 2026-07-06
**Valid until:** ~2026-08-05 for moby/OpenSandbox (fast-moving v0.x + active repos — re-verify the moby client API and OpenSandbox egress docs at plan time); stable for the codebase seams (in-repo).
