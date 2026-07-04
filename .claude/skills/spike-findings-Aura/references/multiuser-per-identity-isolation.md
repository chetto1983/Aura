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

**Go contract** — corrected against the REAL agent-sandbox source + a live kind run (**082**, supersedes 079's paper mapping):
```go
type SandboxSpec struct { IdentityID, Image, Workspace, RuntimeClass string; Egress EgressPolicy; Limits Resources }
type Sandbox interface {
    Resolve(ctx, id) (BoxHandle, error)  // get-or-create per identity ≈ direct Sandbox CREATE (NOT SandboxClaim — see below)
    Exec(ctx, h, cmd) (Result, error)
    Suspend(ctx, h) error                // idle: OperatingMode:Suspended → pod killed, box+volume RETAINED, resumable
    Resume(ctx, h) error
    Stop(ctx, h) error                   // ShutdownPolicy:Delete → destroy
}
```
Corrections from 082's live run (kind v0.32 + released `manifest.yaml`, create→Ready→exec→Suspend→Resume→Delete all green):
- **`Resolve` maps to direct idempotent `Sandbox` creation, NOT `SandboxClaim`.** `SandboxClaim.WarmPoolRef` is a *required* field → a Claim is a warm-pool checkout, not get-or-create. Warm pools are the latency tier Aura skips on one box, so Claim isn't the provisioning primitive.
- **Idle-stop is `OperatingMode: Suspended`** (retain box + PVC, fast resume), distinct from `ShutdownPolicy: Delete`. Add Suspend/Resume verbs — an idle identity's box should suspend, not be destroyed (pairs with 084's per-identity-sidecar RAM cost).
- **Egress is a first-class `NetworkPolicy` with a secure default** in the real `SandboxTemplateSpec` (allow public internet, block RFC1918 + cloud metadata server) — mirror that default, not just an allowlist toggle.
- **Backend seam = speak the E2B protocol, not vendored CRD structs** (answers 079's open question). The operator's `github.com/agent-sandbox/agent-sandbox` (v0.7.0) is a *different* project from `kubernetes-sigs/agent-sandbox` — an **E2B-protocol REST+MCP gateway built ON TOP** of the CRD one, with per-user token→sandbox scoping and a mountable `/mcp` server (createSandbox/getSandbox/listSandbox/deleteSandbox/sandboxExecutor). Make Aura's `Backend` speak E2B verbs: `DockerBackend` now; drop agent-sandbox/agent-sandbox in **unmodified** for the DGX tier (or even mount its `/mcp` like agent-memory `:8091`). Both upstreams are hard K8s-bound (client-go/knative, no Docker backend) → Aura owns the Docker backend regardless.

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
- **Live PUT/GET round-trip + cross-deny PROVEN (083, closes 080's open tier)** through Aura's real `internal/objectstore.S3Store` (path-style): A PUT+GET round-trips its bucket; B GET A / A GET B / B PUT→A all return **403 AccessDenied**. Bucket-per-identity + scoped key is storage-enforced.

### 3. Per-identity MCP (Phase 36 — there are THREE classes, NOT one)
Config rooting is per-identity (`~/.aura/mcp/{id}/servers.json`, shared catalog read-only + per-identity enable/trust, `mcptools.MountForIdentity(ctx)`, identity-keyed `mcp_audit`). But **isolation strategy depends on the server class — the live sidecars are not uniform** (operator caution: "we have calendar, agent-memory AND whatsapp"):

| Class | Live servers | State | Per-identity strategy |
|---|---|---|---|
| **(a) stdio / stateless local** | calculator-recipe, mail-mcp, ad-hoc tools | none / fs-only | **Run INSIDE the identity's box** → isolated for free. Cheap. |
| **(b) shared graph, scope-keyable** | **agent-memory** (`:8091`, 16 `memory__*`) | one Neo4j graph, multi-tenant-able | **ONE shared sidecar, called with the identity as a mandatory scope key** (`user_identifier`). The **fork `aura/provenance-safe-dedup`** enforces the key; do NOT trust upstream semantic dedup for isolation (032/034). **PROVEN live (083, closes 081's open tier):** A `memory_add_fact` → A recalls `fact_count 1`, B recalls `fact_count 0`. The scope is now a `(:User {identifier})-[:HAS_FACT\|HAS_ENTITY\|…]->` ownership edge + `_SCOPED` query variants (**shipped commit `9a4ca594`, 2026-07-03**) — this is the template for class (d). |
| **(c) per-user-ACCOUNT HTTP sidecar** | **calendar/PIM** (`aura-pim-mcp`, Google/Outlook **OAuth**), **whatsapp** (whatsmeow, one **paired phone number**) | one external account per instance | **A scope key is NOT enough** — the account IS the instance (no per-request identity param). **PROVEN live (084):** two `aura-pim-mcp` instances (own port + admin token + data volume) → cross-instance admin tokens 401, account/OAuth store filesystem-isolated, **per-instance data-protection key rings** (A's encrypted tokens undecryptable by B), ~33 MiB idle each. Per identity = a per-identity **instance** (own `~/.aura/pim/{id}`, admin token, OAuth client / WA pairing), lazy-started + idle-suspended (082 Suspend). N × {calendar, whatsapp} sidecars + N OAuth/pairing onboardings is the real Phase-36 cost. |
| **(d) shared graph, was UNSCOPED** | **documents** (`document_search` via `internal/knowledge`→mcp-neo4j-cypher, `:Document`/`:Chunk`) | one Neo4j graph, **identity-blind today** | **Was a LEAK (085): `:Chunk`/`:Document` carry no owner, `SearchRequest`/`IngestRequest` have no identity field, unscoped `Searcher.Search` returns ALL identities' chunks.** Fix = apply class-(b)'s pattern: `(:User {identifier})-[:HAS_DOCUMENT]->(:Document)` + a fail-closed `EXISTS{}` ownership filter on every retrieval query. Proven live (real Indexer/Searcher): B scoped search misses A's doc, A finds own. See `## Per-identity documents/retrieval`. |

So `MountForIdentity` resolves a *mix*: in-box stdio (a) + the shared agent-memory with the identity scope key (b) + that identity's own calendar/whatsapp sidecar instances (c). The **documents graph (d) is the same shared-graph-scope-key shape as (b)** but its scoping must be *built* (085). Never mount/return identity A's calendar/whatsapp/memory/documents for identity B.

### 4. Per-identity skills (Phase 36 — filesystem rooting + run in the box)
- `$AURA_SKILLS_DIR/{id}/` + `~/.aura/pyscripts/{id}/` + identity-keyed `skill_audit`/approval; built-ins shared read-only; **snippets execute inside the identity's box**. `newSkillToolForIdentity(ctx)`.
- Additive `*ForIdentity` methods (same shape as the F-028 conversation/approval owner-scoping), `local` fallback for CLI/no-principal.

### 5. Per-identity documents / retrieval (Phase 36 — the shared-graph plane that leaks today, 085)
The `internal/documents` pipeline (`:Document`/`:Chunk` written via `internal/knowledge`→mcp-neo4j-cypher, searched by `document_search`) is **identity-blind graph-side** — the leak (085) is real and reproduced through the production `Searcher`. Close it by mirroring the memory MCP's `:User`-ownership pattern (`9a4ca594`):
1. **Ingest:** add `IdentityID` to `documents.IngestRequest` → thread to `ExtractedDocument`; on upsert, `MERGE (u:User {identifier:$id}) MERGE (u)-[:HAS_DOCUMENT]->(d)` atomically with the `:Document` write.
2. **Retrieve:** add `IdentityID` to `documents.SearchRequest`; add `WHERE EXISTS { (:User {identifier:$identity_id})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }` to **every** retrieval query — `sparseSearchQuery`, `docScopedVectorSeedQuery` (the dense seed), the two-stage `Retrieve` seeds, and the graphrag 1-hop expand. **Empty identity fails closed** (returns nothing), never the global index.
3. **Unify:** the `:User {identifier}` is the **same** `identityctx` principal as box/bucket/memory — all four planes key on one identity (proven together in 083).
4. **Backfill:** existing chunks have no `:User` edge → invisible under fail-closed scoping. The migration must attach ownership edges from the Postgres catalog's identity→document_id map *before* flipping retrieval to scoped.

Do NOT rely on 077's catalog-injection for isolation — it makes the agent *usually* pass a `document_id`, but any unscoped/prompt-injected `document_search` bypasses it. Isolation is graph-side, fail-closed.

## What to Avoid

- **Garage key-prefix in a shared bucket** for isolation — Garage grants are **per-bucket, not per-prefix** (verified live); a shared bucket means any key with read sees every prefix. Use bucket-per-identity.
- **Kubernetes / k3s on the appliance** — agent-sandbox needs a control plane; its design center is multi-node + warm pools (idle compute for latency) = overkill on one 16-core box (062 + online research agree). Mirror the *pattern* over Docker.
- **Warm pools on one box** — trade idle resource for latency you don't need (078 idle box ≈ 1 MB; cold-start a non-issue at this scale).
- **Re-spiking the single-user box** — 059–062/008–010 already proved the box, host-edge, gVisor tier, egress. Don't repeat.
- **Trusting upstream agent-memory semantic dedup for per-identity isolation** (032/034) — pass an Aura-side identity scope key; upstream over-merges.
- **Driving Docker from Git-Bash** — MSYS path-mangling breaks `docker -v name:/path`; use the **PowerShell** docker CLI (or the Docker SDK in Go). The harness path-guard also blocks commands pairing `rm` with a `/workspace`-like token — keep cleanup (`docker stop`, `docker volume rm <names>`) free of absolute paths.

## Constraints

- Dev Docker is capped at **15.47 GiB** (WSL), not the host's 32 GB — irrelevant per operator (dev/server headroom), but the Phase-37 pre-merge benchmark must measure concurrent-identity box cost on the real host.
- Garage v2.0.0: admin via `/garage` (binary at root, not on `$PATH`); grants are per-bucket RW/owner. **Drive `docker exec aura-garage /garage …` under `MSYS_NO_PATHCONV=1`** (Git-Bash rewrites `/garage`→`C:/Program Files/Git/garage`; same for `--entrypoint /bin/sh`, 084).
- **All four session-21 open tiers are now CLOSED** (083/084/085): Garage PUT/GET round-trip, 2-identity memory recall, per-identity PIM instance model, and the document-ingest leak+fix. Nothing in the isolation model remains un-proven — what's left is Phase-36/37 build work.
- Live-run tooling: `kind` via `go install sigs.k8s.io/kind@latest`; per-identity Garage keys/PIM instances/boxes/graph nodes are all torn down on exit; `os.Exit` skips `defer` → cleanup must be called explicitly in Go harnesses.

## Origin

Synthesized from spikes: 078 (per-identity-box-multiplexing, VALIDATED live), 079 (agent-sandbox-api-contract, VALIDATED design), 080 (garage-per-identity-isolation, VALIDATED live), 081 (mcp-skills-per-identity-scoping, VALIDATED design), **082 (agent-sandbox-realsource-contract, VALIDATED — real source + live kind run, corrects 079), 083 (two-identity-e2e-tenancy, VALIDATED live — box+Garage+memory together, closes 080/081 tiers), 084 (per-identity-pim-sidecar, VALIDATED live — the 3rd MCP class), 085 (document-ingest-tenancy, VALIDATED live — leak + `:User`-ownership fix, the 4th plane)**.
Source READMEs + harnesses in: sources/078…081/, sources/082-agent-sandbox-realsource-contract/, sources/083-two-identity-e2e-tenancy/, sources/084-per-identity-pim-sidecar/, sources/085-document-ingest-tenancy/.
Wires into v2.0.0 REQUIREMENTS `SBX-*` / `MUSR-*` and ROADMAP Phases 36–37.
