# Phase 37: Per-User Full-Capability Sandbox - Context

**Gathered:** 2026-07-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Resolve audit finding **F-001** by containment: under the strict runtime profiles
(`single_user_hardened` / `server_production` — the ROADMAP's "hardened/production"),
host shell/filesystem tools execute **inside a per-identity full-capability Docker
sandbox** — full shell/fs/network *inside* the box, per-identity named `/workspace`
volume — routed by `SandboxRouter.Resolve(identityctx)`. The agent experiences a full
host; the **real host is never exposed**. Capability is never stripped (the full-terminal
home is preserved), it is contained.

**In scope:** `SBX-01..05` only — the box runtime (`internal/sandbox/usersandbox`),
its lifecycle, egress enforcement, host-exposure-flag unrepresentability, and the ADR.
Every hardening behavior is a **no-op under `dev` / `local_trusted`** — the operator's
daily full-host `shell_exec` experience is unchanged (CLAUDE.md locked-(e)).

**Out of scope (deferred — see `<deferred>`):** class-(c) per-user PIM/WhatsApp sidecar
instances; per-identity quotas; microVM/Firecracker isolation; K8s + agent-sandbox.

</domain>

<decisions>
## Implementation Decisions

### Backend & runtime seam
- **D-01 (Hybrid build-vs-adopt):** Keep the spike-locked **bespoke `DockerBackend`**
  over the `moby/moby/client` Go SDK (already in `go.mod` — promote indirect→direct) for
  box lifecycle (`Resolve`/`Exec`/`Suspend`/`Resume`/`Stop`), because spikes 078/082 proved
  it live. The `Backend` seam **speaks the E2B protocol** so `agent-sandbox/agent-sandbox`
  (v0.7.0, E2B-compatible, Go, mountable `/mcp`, Apache-2.0 — verified still **K8s-only**,
  so DGX-tier-only) drops in unmodified at the DGX tier with zero rework. Package:
  `internal/sandbox/usersandbox`.
- **D-02 (Contract, corrected by spike 082):** `Resolve` = idempotent **direct `Sandbox`
  create** (NOT `SandboxClaim` — Claim is a warm-pool checkout Aura doesn't need on one box);
  idle = `OperatingMode:Suspended` (retain box + volume); verbs `Resolve/Exec/Suspend/Resume/Stop`.
- **D-03 ("don't reinvent the wheel" — operator directive):** Adopt **Alibaba OpenSandbox**'s
  Apache-2.0 **egress component** (DNS + nftables allowlist) for SBX-04 enforcement instead of
  the advisory ~80-LOC CONNECT proxy from spike 009. Before locking any bespoke mechanism,
  search for an existing MCP server / maintained library first.

### Egress — Claude Code parity (operator directive: "no deny nothing, claude code parity")
- **D-04 (Default posture — SUPERSEDES the earlier profile-dependent draft):** **Full public
  internet under BOTH strict profiles** — `pip`/`npm`/`uv`/`npx skills add`/any API/any host
  all work, true Claude-Code parity. **Do not nanny the agent's outbound work.**
- **D-05 (The only carve-out = tenancy boundary, not a work restriction):** The egress sidecar's
  default ruleset is **allow-public, DROP RFC1918 private ranges + `169.254.169.254` cloud-metadata
  + the shared-services Docker bridge**. Box A cannot reach box B's internals, `agent-memory :8091`,
  Garage, Postgres/Neo4j, or the host LAN at the network layer (that would be a cross-identity leak
  undermining SBX-03). Shared services are reached only via Aura's **identity-scoped MCP brokering**,
  never raw network.
- **D-06 (SBX-04 AMENDMENT REQUIRED):** SBX-04's literal *"egress defaults to `--network none`"*
  is amended — default is **full-internet-minus-internal** (D-04/D-05). SBX-04's core requirement is
  **preserved**: the allowlist, *when an operator tightens it*, is **enforced (nftables), not advisory**;
  `runtime: runsc` (gVisor) stays selectable under `server_production`. A tightened allowlist (e.g.
  registries-only) remains an available opt-in for locked-down deployments. **PRD/REQUIREMENTS-amendment
  commit before implementation.**
- **D-07 (Egress sidecar form):** OpenSandbox egress runs as a **per-box sidecar sharing the box netns
  via `--network container:<box>`** (`CAP_NET_ADMIN` on the sidecar only; box stays net-unprivileged —
  standard Tailscale/Istio pattern). Because D-05's internal-block is the default floor, the sidecar is
  **always-on** for any networked box (not only when an allowlist is set).

### Lifecycle (SBX-03)
- **D-08:** **Suspend-on-idle** — an idle-TTL (default ~30 min, config knob) triggers
  `OperatingMode:Suspended`: box killed, **named volume RETAINED**. Next tool call **auto-resumes
  transparently** (the agent never sees a "box suspended" state). Volume/data is **never auto-deleted**;
  destroy (`ShutdownPolicy:Delete`) only on **explicit identity deprovision**. The idle reaper **reuses
  the existing migration-0009 scheduler**, not a new goroutine.

### Failure mode (F-001 / GATE-01 fail-CLOSED)
- **D-09:** If the box can't create/resume under a strict profile (Docker down, image pull fail, OOM),
  the shell/fs call **fails CLOSED** — a clear error `ToolResult` (same shape as the approval-required
  deny path), **NEVER a host fallback**. Both `single_user_hardened` AND `server_production` route to a
  box (containment even for one operator). The `local` / no-principal CLI identity under a strict profile
  **gets a `local`-id box too — never host**.

### Host→box bridging & tool routing (SBX-01 / SBX-02)
- **D-10:** SBX-02 makes host bind-mounts **unrepresentable**, so per-identity skills / `Agent.md` /
  pyscripts are **materialized into the box's named volume** at create/resume (docker cp / init step),
  **never host-bound**. `/workspace` = persistent per-identity named volume + a **tmpfs RW scratch** for
  ephemera. Artifacts written to `/workspace` are **copied back out** (docker cp) for Telegram
  `sendDocument` delivery.
- **D-11 (operator directive: "we already have web tools, don't double-think"):** Tools routed **INTO
  the box**: `shell_exec`, `shell_bg`, `fs_read`, `fs_write`, and **skill-snippet execution**.
  `web_fetch` / `web_search` **stay host-side** — Aura already SSRF-guards them; do **not** duplicate the
  guard or force box-network on for a web tool.

### Box posture (full box, contained boundary)
- **D-12:** The agent is **root inside with a writable rootfs + full shell** (the fat full-power box —
  `packaging-box.md`, REVERT `ec7fe2f6`; **the planner must NOT distroless / non-root / read-only-rootfs
  it** — that breaks the full-terminal-home design). Isolation comes from the **boundary**, not from
  crippling the inside: container namespaces + **host-exposure flags unrepresentable** (no docker-socket,
  no `--privileged`, no `--network host`, no host bind-mount — SBX-02, test-asserted) + **gVisor `runsc`
  under `server_production`** + **cgroup v2 CPU/mem/pids caps** (prevent cross-identity starvation) +
  kept **seccomp default** profile. (AgentCgroup-style eBPF-per-tool-call is research-grade — explicitly skipped.)

### Image & dependency strategy
- **D-13:** **Hybrid** — **ONE shared fat base image** bakes the runtimes + heavy common deps
  (`python3`/`node`/`go`/`uv`/`git`/`gh`/`jq` + the Phase-5 heavy set); `uv` installs the **long tail
  on-demand** via a **shared warm-cache volume** (makes `deps:` frontmatter **load-bearing**). Per-identity
  = the **volume only**; the image is shared across identities. Dockerfile in-repo, **digest-pinned**.
  (Pre-baking the common set avoids the first-run install latency + dependency-resolution failure mode the
  research warns against; D-04 full egress means uncached long-tail installs still just work.)

### Concurrency benchmark (Success Criterion 5, Gate-3)
- **D-14:** **Concurrent-identity soak on the REAL 32GB host** (dev WSL Docker is capped at 15.47 GiB —
  insufficient, per spike). Measure ~10–20 concurrent per-identity boxes: **aggregate RAM within the 32GB
  envelope + headroom**, **Resolve p95 < ~2s**, **Resume-from-suspend p95 < ~1s**, and per-box cgroup caps
  (starting point **2 CPU / 2GB / 512 pids**) proven to **prevent starvation**. Documented as the Gate-3
  `VALIDATION.md` table. Pass = fits 32GB with headroom + no starvation (not a scale SLA).

### ADR (SBX-05)
- **D-15:** Record: **container-per-identity over Docker** for the mini-PC; **K8s + `agent-sandbox` +
  gVisor-as-default reserved for the DGX-Spark multi-node tier**; the sandbox config **mirrors the E2B /
  agent-sandbox template/claim shape** for forward compatibility (the `Backend` E2B seam of D-01).

### Claude's Discretion
- Exact cgroup cap values (D-14 gives starting points; tune against the benchmark).
- Idle-TTL default value within the ~30 min ballpark (D-08 is a config knob).
- Exact registry set if an operator opts into a tightened allowlist (D-06) — pypi/pythonhosted/npm/github/skills.sh is the reference set.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & audit (truth-source)
- `.planning/REQUIREMENTS.md` — `SBX-01..05` (the locked acceptance), `GATE-01` (fail-CLOSED gateway), the "Locked decisions" header (F-001 via containment, no RBAC, no-op under dev/local_trusted).
- `.planning/ROADMAP.md` — Phase 37 entry (goal + 5 success criteria) and the Phase-37 = `SBX-01..05` scope line.
- `docs/audit/` — **F-001** (host shell/fs escape surface) and **F-036** (egress enforcement) findings.

### Spike blueprint (validated live — non-negotiables in each file's `## Requirements`)
- `.claude/skills/spike-findings-Aura/references/multiuser-per-identity-isolation.md` — the Phase-37 box contract (Go seam, corrected-by-082 verbs, E2B backend seam, the 4 MCP isolation classes, no-host-bind, `--network container:` egress).
- `.claude/skills/spike-findings-Aura/references/sandbox-runtime.md` — mounts, deps (bake/uv/hybrid), hardening tiers (token/egress-proxy/gVisor), the "npm/github/skills.sh/pypi on the allowlist" note, "probe don't inherit egress."
- `.claude/skills/spike-findings-Aura/references/packaging-box.md` — **fat full-power box, REVERT `ec7fe2f6`** (NOT distroless), gVisor `runsc` via `compose.gvisor.yaml` as the optional appliance tier.
- `.planning/spikes/MANIFEST.md` + spikes **078–085** (per-identity box, agent-sandbox contract, Garage, MCP/skills scoping, real-source correction, 2-identity E2E, PIM sidecar, document tenancy).

### Code (integration points — read before touching)
- `internal/config/config_runtimeprofile.go` — `RuntimeProfile` enum (`dev`, `local_trusted`, `single_user_hardened`, `server_production`) + `Strict()` gate; the routing switch keys on this.
- `internal/identityctx/identityctx.go` — `identityctx.IdentityID(ctx)`, `local` no-principal fallback (the `SandboxRouter.Resolve` key).
- `internal/agent/tools/shell_exec.go`, `shell_bg.go`, `shell_bg_owner.go`, `fs_read.go` — the host tools that must route into the box (currently `os/exec` direct on host).
- `compose.gvisor.yaml` — the existing `runtime: runsc` override for the gVisor tier.
- `go.mod` — `moby/moby/client v0.4.1` (present; promote to direct dep).

### External (forward-compat + adopted deps — record, don't vendor blindly)
- OpenSandbox — `https://github.com/alibaba/OpenSandbox` (Apache-2.0; egress component `components/egress`: DNS+nftables FQDN allowlist, CAP_NET_ADMIN sidecar). The D-03/D-07 adopted egress.
- agent-sandbox — `https://github.com/agent-sandbox/agent-sandbox` (v0.7.0, 2026-06-24; E2B-compatible, Go, `/mcp`; **K8s-only** → DGX tier, the D-01 forward-bet).
- Practitioner corroboration: Northflank (`northflank.com/blog/how-to-sandbox-ai-agents`), Pigment Engineering (`engineering.pigment.com/2026/06/10/sandbox-for-llm-generated-code-execution/`), Imbue (`imbue.com/blog/containers`).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`moby/moby/client`** — already in `go.mod` (indirect); the Docker SDK for the `DockerBackend`. No new heavy dep.
- **`compose.gvisor.yaml`** — the gVisor `runsc` tier already exists; reuse for the `server_production` isolation knob.
- **`identityctx` + the Phase-36 `*ForIdentity` pattern** — `SandboxRouter.Resolve(identityctx)` follows the same additive owner-scoping shape (`local` fallback for CLI/no-principal).
- **migration-0009 scheduler** — reuse for the idle-suspend reaper (D-08), no new background worker.
- **display `normalize.go` already handles a `sandbox_exec` tool name** — the display/preview surface is pre-wired for the routed exec.

### Established Patterns
- **`RuntimeProfile.Strict()`** gates all hardening; the box routing is a no-op under `dev`/`local_trusted` (D-domain), active under the two strict profiles.
- **Deferred-tool pattern** (CLAUDE.md) — any new sandbox tool spec follows `Deferred: true` big-tool convention.
- **Fail-CLOSED under strict, fail-OPEN under lenient** (GATE-01) — D-09 mirrors the existing policy-decision shape.

### Integration Points
- **`SandboxRouter.Resolve(identityctx)`** is the new seam: `shell_exec`/`shell_bg`/`fs_read`/`fs_write`/skill-snippet exec call it under strict profiles; it returns a `BoxHandle` (get-or-create), the tool `Exec`s inside the box, artifacts copied out.
- `web_fetch`/`web_search` bypass the router (host-side, D-11).

</code_context>

<specifics>
## Specific Ideas

- **"Claude Code parity"** is the operator's north star for the box egress (D-04): the agent's box should be as network-capable as Claude Code on a dev machine — full internet, don't nanny it. The only line is the multi-tenant internal network (D-05).
- **"Industrial, not a nuclear bomb, but useful"** — the operator's framing for isolation depth: hardened runc + gVisor knob, NOT Firecracker-microVM-per-identity maximalism; a persistent usable full box, NOT an ephemeral distroless code-cell.
- **"Don't reinvent the wheel / search for MCP first"** — standing directive for the phase: adopt maintained components (OpenSandbox egress, agent-sandbox at DGX) over bespoke Go where the fit is honest.

</specifics>

<deferred>
## Deferred Ideas

- **Class-(c) per-user PIM/WhatsApp sidecar instances** — per-identity calendar/whatsapp instances (own port/token/OAuth/pairing, idle-suspend with the box). Phase 36 CONTEXT D-05 marked "Phase 37+"; ROADMAP scopes Phase 37 = `SBX-01..05` only. **Own phase.**
- **Per-identity quotas** — Phase 37/OPS, not this phase.
- **Firecracker / Kata microVM tier** — the hardware-boundary isolation tier; DGX / cloud only, over-engineering for the mini-PC.
- **K8s + `kubernetes-sigs/agent-sandbox` + gVisor-as-default** — the DGX-Spark multi-node future tier (the D-01/D-15 forward-bet), not the appliance.
- **Tightened opt-in egress allowlist (registries-only, etc.)** — the mechanism ships (D-06) but is not the default; deployments that want it enable it.

None — discussion stayed within phase scope (the above are explicit forward-boundaries, not scope creep).

</deferred>

---

*Phase: 37-per-user-full-capability-sandbox*
*Context gathered: 2026-07-06*
