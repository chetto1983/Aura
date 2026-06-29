# Multi-User Per-Identity Isolation (v2.0.0 — Phases 36–37)

Implementation blueprint for making Aura multi-user by giving **each identity its own isolated full-capability sandbox** plus per-identity object-store and MCP/skills — identity isolation only, **no RBAC**, authz stays `capability_grants`-based. Synthesized from Session-21 spikes 078–081 (2026-06-29), building on the validated single-user box (059–062), MCP mounts (001/032/064), skills (003/005), and per-identity `Agent.md` (036–039).

## Requirements (non-negotiable)

- **Capability never stripped, host never exposed.** The agent keeps a full shell/fs/network *inside* a per-identity sandbox; the real host is never reachable. This resolves audit F-001 by containment, not by removing the full-host terminal surface.
- **No RBAC / OAuth / roles.** Multi-user = identity isolation (owner-scoped data + per-identity resources). Authz remains `capability_grants` per-route.
- **Per-identity isolation must be storage-/kernel-enforced, not app-enforced** wherever the substrate supports it (named volumes for fs; bucket-per-identity for object store). App-only prefix scoping is a silent hole.
- **Docker-direct, not Kubernetes**, on the appliance. K8s + `kubernetes-sigs/agent-sandbox` is the DGX-multi-node future tier only.
- RAM fit is **not** a gate in dev / on the real server (operator directive); idle per-identity boxes are ~1 MB anyway.

## How to Build It

### 1. Per-identity sandbox box (Phase 37 — `internal/sandbox/usersandbox`)
Spawn one container **per `identityctx.IdentityID`** over the Docker Go SDK (`moby/moby/client`, already in `go.mod`). Proven working (078, live on `aura:local`):

```
docker run -d --rm --network none -v <per-identity-volume>:/workspace --entrypoint sh <fat-image> -c '...'
```

- **Per-identity named volume** (not host bind) → data isolation is storage-enforced. Live proof: A's `/workspace/secret.txt` is invisible + unreadable from B's box.
- `--network none` default; egress via the 009 allowlist when enabled.
- Make the host-exposure flags **unrepresentable** in the box spec (SBX-02): no docker-socket mount, no `--privileged`, no `--network host`, no host bind-mount. (Live: box A has no `/var/run/docker.sock`.)
- Optional `runtime: runsc` (gVisor) under `server_production` via the existing `compose.gvisor.yaml` (061).

**Go contract** (mirror agent-sandbox so a future K8s backend is a transport swap — 079):
```go
type SandboxSpec struct { IdentityID, Image, Workspace, RuntimeClass string; Egress EgressPolicy; Limits Resources } // ≈ SandboxTemplate
type Sandbox interface {
    Resolve(ctx, id) (BoxHandle, error)  // get-or-create per identity ≈ SandboxClaim
    Exec(ctx, h, cmd) (Result, error)
    Stop(ctx, h) error                   // idle-TTL ≈ scheduled-delete
}
// Backend seam: DockerBackend now; K8sBackend (agent-sandbox CRDs) for DGX. Skip warm pools on one box.
```

### 2. Per-identity object store (Phase 36 — Garage)
**Bucket-per-identity + a scoped key** (proven 080, live):
```
garage bucket create aura-<identityID>
garage key create key-<identityID>
garage bucket allow --read --write --key key-<identityID> aura-<identityID>
```
- Mint the key during identity provisioning (alongside the Authula cutover, MUSR-06).
- `internal/objectstore` + `internal/assets` select bucket/key from `identityctx`; presign scoped to the identity's key.
- Removes the shared static `aura-assets-local` cred → satisfies audit F-007 under `server_production`.

### 3. Per-identity MCP (Phase 36 — there are THREE classes, NOT one)
Config rooting is per-identity (`~/.aura/mcp/{id}/servers.json`, shared catalog read-only + per-identity enable/trust, `mcptools.MountForIdentity(ctx)`, identity-keyed `mcp_audit`). But **isolation strategy depends on the server class — the live sidecars are not uniform** (operator caution: "we have calendar, agent-memory AND whatsapp"):

| Class | Live servers | State | Per-identity strategy |
|---|---|---|---|
| **(a) stdio / stateless local** | calculator-recipe, mail-mcp, ad-hoc tools | none / fs-only | **Run INSIDE the identity's box** → isolated for free. Cheap. |
| **(b) shared graph, scope-keyable** | **agent-memory** (`:8091`, 16 `memory__*`) | one Neo4j graph, multi-tenant-able | **ONE shared sidecar, called with the identity as a mandatory scope key** (`source_id`/`run_id`/identity). 032/034: upstream semantic dedup over-merges → the **fork `aura/provenance-safe-dedup`** must enforce the identity key; do NOT trust upstream dedup for isolation. Cheapest correct option; needs a 2-identity recall test. |
| **(c) per-user-ACCOUNT HTTP sidecar** | **calendar/PIM** (`aura-pim-mcp`, Google/Outlook **OAuth**), **whatsapp** (whatsmeow, one **paired phone number**) | one external account per instance | **A scope key is NOT enough** — these authenticate to a single real account. Per identity needs **either a per-identity sidecar instance** (each with that identity's own OAuth tokens / WhatsApp pairing, mounted only for that identity) **or** an upstream multi-account mode (the forks don't have one today). Default: per-identity instance, lazy-started, mounted via that identity's `servers.json`. **Resource + UX cost is real** (N identities × {calendar, whatsapp} sidecars + N OAuth/pairing onboardings) → a Phase-36 design decision, flagged below. |

So `MountForIdentity` resolves a *mix*: in-box stdio (a) + the shared agent-memory with the identity scope key (b) + that identity's own calendar/whatsapp sidecar instances (c). Never mount identity A's calendar/whatsapp/memory for identity B.

### 4. Per-identity skills (Phase 36 — filesystem rooting + run in the box)
- `$AURA_SKILLS_DIR/{id}/` + `~/.aura/pyscripts/{id}/` + identity-keyed `skill_audit`/approval; built-ins shared read-only; **snippets execute inside the identity's box**. `newSkillToolForIdentity(ctx)`.
- Additive `*ForIdentity` methods (same shape as the F-028 conversation/approval owner-scoping), `local` fallback for CLI/no-principal.

## What to Avoid

- **Garage key-prefix in a shared bucket** for isolation — Garage grants are **per-bucket, not per-prefix** (verified live); a shared bucket means any key with read sees every prefix. Use bucket-per-identity.
- **Kubernetes / k3s on the appliance** — agent-sandbox needs a control plane; its design center is multi-node + warm pools (idle compute for latency) = overkill on one 16-core box (062 + online research agree). Mirror the *pattern* over Docker.
- **Warm pools on one box** — trade idle resource for latency you don't need (078 idle box ≈ 1 MB; cold-start a non-issue at this scale).
- **Re-spiking the single-user box** — 059–062/008–010 already proved the box, host-edge, gVisor tier, egress. Don't repeat.
- **Trusting upstream agent-memory semantic dedup for per-identity isolation** (032/034) — pass an Aura-side identity scope key; upstream over-merges.
- **Driving Docker from Git-Bash** — MSYS path-mangling breaks `docker -v name:/path`; use the **PowerShell** docker CLI (or the Docker SDK in Go). The harness path-guard also blocks commands pairing `rm` with a `/workspace`-like token — keep cleanup (`docker stop`, `docker volume rm <names>`) free of absolute paths.

## Constraints

- Dev Docker is capped at **15.47 GiB** (WSL), not the host's 32 GB — irrelevant per operator (dev/server headroom), but the Phase-37 pre-merge benchmark must measure concurrent-identity box cost on the real host.
- Garage v2.0.0: admin via `/garage` (binary at root, not on `$PATH`); grants are per-bucket RW/owner.
- Next-tier impl proofs (not yet run): a live S3 PUT-as-A / GET-denied-as-B round-trip (080) and a 2-identity memory-recall isolation test (081).

## Origin

Synthesized from spikes: 078 (per-identity-box-multiplexing, VALIDATED live), 079 (agent-sandbox-api-contract, VALIDATED design), 080 (garage-per-identity-isolation, VALIDATED live), 081 (mcp-skills-per-identity-scoping, VALIDATED design).
Source READMEs in: sources/078-per-identity-box-multiplexing/, sources/079-agent-sandbox-api-contract/, sources/080-garage-per-identity-isolation/, sources/081-mcp-skills-per-identity-scoping/.
Wires into v2.0.0 REQUIREMENTS `SBX-*` / `MUSR-*` and ROADMAP Phases 36–37.
