# ADR 0037 — Container-per-identity over Docker for the per-user full-capability sandbox

- **Status:** Accepted
- **Date:** 2026-07-07
- **Requirement:** SBX-05 (records decision D-15 + the research-surfaced residuals)
- **Phase:** 37 — per-user-full-capability-sandbox
- **Supersedes / relates to:** SBX-01..SBX-04, GATE-01, D-01/D-02, D-04/D-05/D-06 (egress),
  D-07 (egress sidecar), D-08 (suspend/resume lifecycle), D-12/D-13 (runtime + image),
  D-14 (concurrency envelope), audit findings F-001 (host shell/fs escape surface) and
  F-036 (egress enforcement), issue #934 (gVisor ⊥ nftables-nat).

---

## Context

Aura runs untrusted, per-identity agent workloads (shell, filesystem, Python/skill snippets)
that previously executed **directly on the host** — audit finding **F-001** (host shell/fs
escape surface) and **F-036** (unenforced egress). Phase 37 moves every such tool into a
**per-identity container** so a compromised or adversarial identity is contained to its own
box, its own workspace volume, and a tenancy-bounded network — never the host, never a
co-tenant.

The deployment target is the **mini-PC appliance**: a *single node* with ~32 GB RAM running
the Docker Engine, hosting one Aura daemon and N per-identity boxes concurrently. A separate,
*future* tier — the **DGX-Spark multi-node** cluster — has fundamentally different operational
economics (many nodes, a scheduler, warm pools, a security team that can run a hardened runtime
by default).

Two design forces shaped the decision:

1. **The isolation control must fit the appliance.** A single-node appliance cannot justify a
   Kubernetes control plane or a warm-pool sandbox controller — that is operational weight with
   no payoff at N≈10–20 boxes on one host (see `multiuser-per-identity-isolation.md`
   "What to Avoid": K8s on the appliance).
2. **The seam must not foreclose the cluster tier.** The isolation *interface* Aura codes
   against should be the same one a managed E2B / `agent-sandbox` gateway satisfies, so the DGX
   tier can drop a different backend behind it **without touching the tool layer** (the D-01
   forward-bet: the `Backend` E2B seam in `internal/sandbox/usersandbox/backend.go`).

---

## Decision (D-15)

**On the mini-PC appliance, Aura runs a container-per-identity sandbox *directly over the
Docker Engine API* (the `moby/moby/client` SDK) — not Kubernetes, and not an `agent-sandbox`
warm-pool `SandboxClaim`.**

Concretely:

- **Direct create, keyed on identity.** `DockerBackend.Resolve` is an idempotent
  get-or-create of `aura-box-<identityID>` (container + per-identity named volume) — a *direct*
  `ContainerCreate`, never a warm-pool checkout (D-02). The same identity always resolves to its
  one box; cross-identity workspace access is *storage-enforced* by separate named volumes
  (spike 078), never app-prefix scoping.
- **Full-capability box, contained by construction.** The box is the shared fat image
  (`docker/aura-sandbox/Dockerfile`, digest-pinned — D-13) running root+writable inside its own
  namespaces (D-12). Host re-exposure is **unrepresentable** in the type layer: `SandboxSpec`
  has no `Privileged` / host-network / bind-mount / device / docker-socket field, and the single
  private `toHostConfig` translator pins the dangerous moby `HostConfig` fields to safe constants
  (SBX-02). Containment is a *structural* property, not a validated one.
- **gVisor (`runsc`) is an opt-in per-box runtime, not the default.** `RuntimeClass` selects
  `runc` (zero value) or `runsc`; `runsc` is accepted **only under `server_production`**
  (`NewSandboxSpec`, D-12). The mini-PC default is `runc` — the namespace boundary plus the
  egress floor is the appliance posture; gVisor is available where an operator wants the extra
  syscall-interception boundary and can pay the performance cost (see `compose.gvisor.yaml`).
- **The config/seam mirrors the E2B / agent-sandbox template-and-claim shape (D-01 forward
  bet).** The `Backend` interface speaks the E2B verbs (Resolve / Exec / Suspend / Resume /
  Stop), and `SandboxSpec` is the "template" a caller fills. A managed **E2B or `agent-sandbox`
  gateway can implement this same interface unmodified** — `Resolve` maps to a `SandboxClaim`
  checkout, `ContainerID` carries the remote sandbox id — so migrating the *backend* on the DGX
  tier requires **no change to the tool layer or the router**.

**K8s + `agent-sandbox` + gVisor-as-default are reserved for the DGX-Spark multi-node tier.**
They are the *right* controls for a cluster (a scheduler, warm pools amortized across many
boxes, a hardened runtime run by default), and the wrong weight for a single appliance. This ADR
records that reservation explicitly so a future contributor does not "upgrade" the appliance to
K8s and does not flip gVisor on by default for the mini-PC.

---

## Consequences

**Positive**

- Zero control-plane weight on the appliance: the Docker Engine already present is the entire
  runtime substrate. No etcd, no scheduler, no CRD reconciler to operate at N≈10–20 boxes.
- Suspend/resume is cheap and lossless (D-08): a stop-retain keeps the container + volume, so an
  idle box costs no RAM yet resumes in-place on the next tool call (the D-14 Resume p95 < ~1 s
  target). Warm pools — the thing `agent-sandbox` buys — are unnecessary at appliance scale.
- The forward seam is real, not aspirational: the DGX tier swaps `DockerBackend` for an E2B/
  agent-sandbox gateway behind the *same* `Backend` interface, leaving the router (37-05) and the
  tool interposition untouched.

**Negative / costs (accepted)**

- The Aura daemon needs Docker API access to spawn boxes — a privileged surface (see Residual A).
- `runc` (the appliance default) is a weaker boundary than a microVM or gVisor-by-default; the
  compensating controls are the SBX-02 structural containment, the per-identity volume, and the
  always-on egress floor (SBX-04). gVisor is available opt-in for operators who want more.
- Concurrency does not scale past what one 32 GB host holds — this is a **single-appliance
  posture**, not a multi-tenant SaaS. The DGX tier is where horizontal scale lives. The D-14 soak
  proves the 10–20-box envelope fits 32 GB with headroom (Gate-3 evidence; see 37-VALIDATION.md).

---

## Accepted Residuals

These are the surfaces the research (37-RESEARCH.md) surfaced and this decision *knowingly
accepts*, each with its compensating control. They are recorded here so they are not
rediscovered as "bugs".

### Residual A — The daemon holds Docker API access (Pitfall 4 / Open Q5)

To spawn per-identity boxes the Aura daemon must reach `dockerd`. Mounting the raw
`/var/run/docker.sock` into the daemon container is *effectively host-root* (anyone who can talk
to the socket can start a privileged container).

- **Invariant (SBX-02):** the socket is granted to the **Aura daemon only — never to a box**. No
  per-identity `aura-box-<id>` container ever receives a socket mount; a box that could reach the
  daemon would defeat the entire containment model.
- **Recommended narrowing (Open Q5):** front the daemon's Docker access with
  **`tecnativa/docker-socket-proxy`**, exposing only the lifecycle subset the backend needs
  (`CONTAINERS`, `IMAGES`, `VOLUMES`, `NETWORKS`, `EXEC`, and `POST` writes) and denying the rest
  (no `swarm`, no `secrets`, no daemon `info`/`auth`). The daemon reaches the proxy over the
  internal compose network via `DOCKER_HOST=tcp://docker-socket-proxy:2375`; the raw socket is
  mounted **read-only into the proxy alone**. `compose.yaml` wires this behind the `sandbox`
  profile (see below).
- **No-op under dev/local_trusted:** sandbox routing is `Strict()`-gated — `buildSandboxRouter`
  returns a nil (host-direct) router under any non-strict profile and never constructs the Docker
  client. So the socket surface is *only* live under `single_user_hardened` /
  `server_production`; the default dev stack neither starts the proxy nor uses the socket.
- **Open question deliberately left open (Q5):** raw-socket-into-the-daemon vs socket-proxy vs a
  TCP+mTLS `dockerd` is an operator deployment choice. The proxy is the recommended default; the
  raw socket is the accepted fallback for a minimal single-user install. Neither weakens the
  never-into-a-box invariant.

### Residual B — gVisor ⊥ nftables-nat FQDN allowlist mutual-exclusion (Pitfall 2, issue #934)

The egress boundary (SBX-04, D-07) is a per-box sidecar that shares the box network namespace
(`--network container:<box>`) with `CAP_NET_ADMIN` on the **sidecar only**. It has two layers:

- **The always-on filter-table floor** (`table ip aura_egress`, `policy accept` + explicit
  `drop` for RFC1918 + the `169.254.169.254` metadata IP + the shared-services bridge). It is
  **filter-table only, no nat**, so it is byte-identical under `runc` and gVisor `runsc` — it is
  the tenancy boundary under *both* runtimes.
- **The opt-in FQDN allowlist** (OpenSandbox, DNS + nftables **nat** redirect) for operators who
  want to tighten egress to a named host set. Because it uses the **nat table**, it is
  **`runc`-only**: gVisor's `runsc` netstack does not run the host nat redirect the allowlist
  needs (issue **#934**). `buildEgressSidecar` therefore **refuses `Runsc` + a non-empty
  `FQDNAllowlist`** with `ErrRunscFQDNMutualExclusion` *before launch*, rather than silently
  shipping an unenforced allowlist.
- **Consequence recorded:** you may have gVisor *or* an FQDN allowlist on a box, not both. The
  filter-floor (the load-bearing tenancy control) is available under either. An operator who
  needs both a hardened runtime and a named-host allowlist is on the DGX tier's problem, not the
  appliance's.

### Residual C — Egress default is full-internet-minus-internal (D-04/D-05/D-06)

SBX-04 amended the original literal `--network none` default to **full public internet minus the
tenancy boundary**: the box reaches the public internet (so `uv`/`npm`/`gh` "just work" and the
`deps:` frontmatter is load-bearing — D-13), while the floor **drops** RFC1918, the cloud
metadata IP, and the shared-services bridge. This ADR records that the *default* posture is
permissive-outbound-with-a-floor, not deny-all; the tightened FQDN allowlist (Residual B) is the
opt-in stricter tier. See SBX-04 and 37-01-SUMMARY.md for the amendment commit (the PRD-first
Gate-1 record).

---

## Alternatives Considered

| Alternative | Verdict | Why |
|---|---|---|
| **Kubernetes + `agent-sandbox` on the appliance** | Rejected for the mini-PC; **reserved for DGX-Spark** | A control plane + warm-pool controller is operational weight with no payoff at one node / N≈10–20 boxes. It is the *right* tool for the multi-node cluster tier, where a scheduler and pooled warm sandboxes amortize across many boxes. |
| **gVisor `runsc` as the default runtime** | Rejected as default; **opt-in per box, DGX-default** | Performance cost on every box, and the nat mutual-exclusion (Residual B) would silently disable the FQDN allowlist tier. Kept opt-in under `server_production` (`compose.gvisor.yaml`); default-on is a DGX-tier posture. |
| **Firecracker / Kata microVMs** | Rejected | Stronger isolation, but a heavier per-box cost and a second runtime to operate on the appliance; the `runc` + structural-containment + egress-floor posture meets F-001/F-036 at appliance scale. Revisit at the cluster tier. |
| **No container isolation / app-prefix scoping in the host process** | Rejected | This *is* F-001 — a shared host process with prefix-scoped paths is escapable and does not contain a hostile identity. The whole phase exists to retire it. |
| **`agent-sandbox` `SandboxClaim` warm-pool checkout on the appliance** | Rejected on the appliance | Warm pools optimize cold-start at scale; on one host the suspend/resume stop-retain (D-08) already gives fast in-place resume without a pool. The `Backend` seam keeps the claim model available for the DGX tier unchanged (D-01/D-02). |

---

## Forward path (DGX-Spark tier)

When Aura moves to the DGX-Spark multi-node tier, the migration is a **backend swap behind the
`Backend` interface**, not a rewrite:

1. Implement `Backend` over an E2B / `agent-sandbox` gateway: `Resolve` → a `SandboxClaim`
   checkout against a warm pool; `Exec`/`Suspend`/`Resume`/`Stop` → the gateway verbs; the
   remote sandbox id lives in `BoxHandle.ContainerID`.
2. Turn gVisor on by default at that tier (a cluster can absorb the perf cost and standardize the
   runtime).
3. The router (37-05), the tool interposition (37-07), `SandboxSpec`, and the egress-policy shape
   are **unchanged** — the appliance's Docker backend and the cluster's gateway are
   interchangeable by construction.

This is exactly the D-01 forward-bet the seam was designed for: pick the appliance-appropriate
runtime now, without foreclosing the cluster-appropriate one later.
