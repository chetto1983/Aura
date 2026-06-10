# Phase 17: Packaging & Distribution — Specification

**Created:** 2026-06-05
**Rewritten:** 2026-06-10 (re-centered on the container-as-security-boundary model after the sandbox-agent removal, `c9e1124e`)
**Ambiguity score:** 0.13 (gate: ≤ 0.20)
**Requirements:** 16 locked

## Goal

Aura ships as **its own container plus its sidecars**, installable with **only Docker on the host**. The Aura agent — model loop, `shell_exec` full terminal, MCP subprocesses, skills runtime — runs *inside* that container with the **same full power Claude Code has on the operator's machine**: a writable filesystem, root of its own box, full egress, self-extension. The container is **Aura's computer, not a jail**. It earns its place two ways — **packaging** (one image; the host stays clean) and a **free, natural host edge** (a container simply cannot touch the host outside its mounts) — *not* through internal lockdown. There are **no host-protection guardrails in the Go code and no capability stripping inside the box**: the only thing "controlled" is what the host chooses to mount, plus the one invariant that keeps the box a box (no host Docker socket).

The end-user flow collapses from today's developer dance (git clone → `make tools` → `go run` + an undocumented host `pip install mcp-neo4j-cypher`) to a single `curl|sh` (Linux/macOS), a documented Docker-Desktop compose path (Windows), or a pre-imaged appliance.

## Background

Aura distributes today as a goreleaser static binary + `compose.yaml` running sidecars (postgres, neo4j, llama-embed, agent-memory-mcp, searxng, the multimodal STT/TTS/OCR/markitdown set). There is **no Dockerfile for the `aura` app itself** — it runs via `go run`/the goreleaser binary on the host.

Two facts make a host-run Aura the wrong shape:

1. **The "single static binary" promise breaks the moment MCP is needed.** `mcp-neo4j-cypher` is a Python subprocess spawned from the **host** PATH ([internal/knowledge/client.go:50-72](../../../internal/knowledge/client.go#L50-L72)), needing an undocumented `pip install mcp-neo4j-cypher==0.6.0`; generic MCP recipes want host `uvx`/`npx`; the whatsapp recipe shells out to `wsl.exe` ([internal/mcp/manager/catalog.go](../../../internal/mcp/manager/catalog.go)). The host accretes runtimes.

2. **`shell_exec` is a deliberate full-host terminal with no fence** ([internal/agent/tools/shell_exec.go:16-21](../../../internal/agent/tools/shell_exec.go#L16-L21)) — Claude-Code parity, "the operator's own privileges … no sandbox hop and no path fence." The in-loop micro-sandbox (`sandbox_exec` + the rivetdev sandbox-agent service) was **removed entirely** (`c9e1124e`, "host shell_exec is the execution surface") as a toy: a per-call container hop that protected nothing the real boundary should own.

The resolution is to put **the whole agent in a box**. When Aura runs inside a confined container, `shell_exec` is just that container's terminal; its blast radius is the container and its mounts, not the host. The deleted in-loop sandbox and the absence of code guardrails are therefore *correct* — host protection is the container's job.

### Security model — the container is Aura's computer, not a jail

**Trusted principal:** the operator. A single-user self-hosted appliance; the operator running arbitrary code through Aura is the *purpose*, never the threat.

**Adversary in scope:** a prompt-injected turn, a malicious or compromised skill/MCP package, or a model error that tries to act beyond the operator's intent.

**What the boundary defends (structural, not code):** the host OS, the host filesystem outside the explicit mounts, other host processes and containers, and any host service not deliberately published. Anything the agent does — `rm -rf`, a fork bomb, a poisoned skill — is bounded to the container and its mounts, and is **resettable**: the operator can throw the box away and recreate it. This is free with containerization; it needs no internal lockdown.

**Aura is not jailed inside its own box.** It runs with full power there — writable rootfs, root of its container, full egress, the right to install skills and spawn subprocesses — exactly the parity the operator has with Claude Code on the host. Capability stripping (`cap_drop: ALL`, read-only rootfs, forced non-root, seccomp tightening) is **deliberately not applied**: it would cripple the full-terminal and self-extension parity that is the entire point — "you have no restriction, why should Aura?" The resource limits that remain (`cpus`/`mem`/`pids`) are **mini-PC stability budget, not a security fence**. The single line held is that the host **Docker socket is never mounted** — not to restrict what Aura can do (it loses no current capability), but so the box stays a box for the turns Aura runs *unattended* (cron, swarm, a Telegram message written by someone other than the operator).

**What the boundary does NOT defend (accepted residual, operator's informed choice 2026-06-10):**
- **Outbound data exfiltration.** Egress is open NAT (Aura needs OpenRouter, Telegram, and package registries for `npx skills`/`uv`). An egress allowlist was considered and **declined** — "no stupid restriction." A determined injected turn can send what it can read to anywhere. This is documented, not pretended away.
- **Confidentiality of the mounted home volume and the databases.** The agent legitimately reads/writes them; the box does not wall the agent off from its own state.

**Consequence for the codebase:** Aura carries **no host-protection guardrails** — no command sanitizers, no path fences, no permission gates guarding `shell_exec`/`fs_*`. Confinement is the container. (Orthogonal, non-host-security mechanisms are out of tension with this and out of scope here: the skill-injection control-token blocklist protects the *model's* prompt integrity, and the skills self-modification HITL flow is a *product* approval, not a host fence. Neither is a "code guardrail" in the sense this model rejects.)

This phase ships the artifacts that make the box real: a fat Aura image, an `aura` compose service hardened as a boundary, the host-coupling removals (whatsapp sibling, no in-container Docker), a `curl|sh` installer, an appliance door, a Caddy TLS front for the user-facing surface, and an `aura doctor` health check — registered PRD-first as Slice 14 / amendment #47 (commit `47f46f72`).

## Requirements

### A. The boundary

1. **Aura's container is a full environment, not a jail; the box just stays a box**: full power inside, the host only outside.
   - Current: Aura runs on the host with the operator's full privileges; `shell_exec` has no fence by design.
   - Target: the `aura` compose service runs Aura with **full power inside its container** — writable rootfs, its own root, the right to install skills and spawn subprocesses (Claude-Code parity). **No capability stripping** (`cap_drop: ALL`, read-only rootfs, forced non-root, seccomp tightening) is applied — it would break full-terminal/self-extension parity. The **only** structural invariants: the host **Docker socket is never mounted** (the box is not dissolved into the host), and `cpus`/`mem_limit`/`pids_limit` are set as **mini-PC stability budget** (not security). The natural container edge — Aura cannot reach the host filesystem outside its mounts (Req 2) — comes free.
   - Acceptance: inside the running `aura` container Aura can write its home, install a skill, and spawn a subprocess (full parity); `ls /var/run/docker.sock` fails and `docker ps` fails (no socket/daemon); the host filesystem outside the declared mounts is not visible from the container; the `cpus`/`mem`/`pids` limits are in effect.

2. **Host-controlled mount model; secrets via env, never baked**: the host owns exactly what the box can touch.
   - Current: a host-run Aura sees the whole host filesystem; secrets sit in `.env`/`~/.aura` on the host.
   - Target: the **only** mounts are operator-controlled: a named `aura-home` volume → the in-container config root (`AURA_CONFIG_DIR`, holding `llm.json`, `mcp/servers.json`, `Agent.md`, skills, agent journals) rw; the run-artifact dir as a volume or tmpfs; **no host bind of the repo, no host root, no docker socket**. Secrets (`OPENROUTER_API_KEY`, `POSTGRES_PASSWORD`, `NEO4J_PASSWORD`, `TELEGRAM_BOT_TOKEN`) are injected via compose `env_file`/environment and held only in the process — **never** copied into an image layer.
   - Acceptance: the container's writable paths are exactly the declared volumes + tmpfs; `docker history <image>` shows no secret value in any layer; removing the `aura-home` mount loses config across `down`/`up` while keeping it preserves `llm.json`/`mcp/servers.json`/`Agent.md`.

3. **No in-container Docker; container-needing MCP are siblings**: the boundary is never punctured to launch more containers.
   - Current: `RuntimeDocker`/`RuntimeDockerGateway` build `docker run …` / `docker mcp gateway run …` command lines ([internal/mcp/manager/runtime.go:101-147](../../../internal/mcp/manager/runtime.go#L101-L147)), which inside a container would require mounting the host Docker socket — a host-root escape that destroys the boundary. No default recipe uses these kinds.
   - Target: the `aura` container has no Docker access; a docker-runtime MCP server is **not launchable from inside the box** and fails with a clear, actionable error. Any MCP that genuinely needs its own container is deployed as a **compose sibling** and mounted by Aura over **streamable-HTTP** (the existing transport, as `aura-agent-memory-mcp` already is, [compose.yaml:100-160](../../../compose.yaml#L100)).
   - Acceptance: configuring a `runtime.kind=docker` MCP server and launching it from inside the `aura` container returns an explicit "docker runtime unavailable inside the container — deploy as a compose sibling and mount via URL" error (not a confusing `exec: "docker": not found`); the streamable-HTTP sibling equivalent mounts and lists its tools.

4. **No host coupling: whatsapp via a sibling bridge, not `wsl.exe`**: every MCP runs in the Linux container world.
   - Current: the whatsapp recipe spawns `wsl.exe -e bash -lc "… uv run main.py"` ([internal/mcp/manager/catalog.go](../../../internal/mcp/manager/catalog.go)) — impossible inside a Linux container.
   - Target: whatsapp is delivered as a **sibling whatsmeow bridge** compose service; the recipe resolves to that sibling's endpoint, mounted like any other sibling. The `aura` image carries **no** `wsl.exe`/Windows dependency.
   - Acceptance: `command -v wsl.exe` inside the `aura` image returns non-zero; with the whatsapp sibling up, the whatsapp recipe resolves to the sibling and its tools list; with it down, Aura degrades fail-soft (no boot crash).

### B. The image

5. **Fat multi-arch image**: a single Aura container bundles every runtime so the host needs only Docker.
   - Current: no Dockerfile for the `aura` app; python/node/uvx/mcp-neo4j-cypher must exist on the host PATH.
   - Target: `docker/aura/Dockerfile`, multi-stage (golang builder → slim runtime with `python3` + `uv`/`uvx` + `node` + `npx` + pinned `mcp-neo4j-cypher==0.6.0` + `git` + `curl` → `COPY aura`), built for `linux/amd64` + `linux/arm64`.
   - Acceptance: `docker run --rm <image> aura version` prints build metadata on both arches; `docker run --rm <image> sh -c "command -v python3 && command -v node && command -v uvx && command -v mcp-neo4j-cypher"` resolves all four.

6. **MCP subprocess in-image, host clean**: `mcp-neo4j-cypher` spawns inside the `aura` container, code unchanged.
   - Current: `internal/knowledge/client.go` spawns it from the host PATH.
   - Target: the same stdio spawn runs inside the image where Python lives; **[internal/knowledge/client.go](../../../internal/knowledge/client.go) is unchanged** by this phase.
   - Acceptance: with the stack up, an in-container Neo4j round-trip returns the server version; on the host, `command -v mcp-neo4j-cypher` returns non-zero and no Aura-installed Python exists.

7. **Pre-baked recipes (offline appliance)**: trusted recipe packages are baked at build time.
   - Current: `uvx`/`npx` fetch recipe packages (calculator, mail) from the network on first use.
   - Target: the trusted recipe packages are baked into the image so a first-boot/air-gapped appliance runs them with zero network fetch.
   - Acceptance: with host egress blocked, installing+invoking the `calculator` (uvx) and `mail` (npx) recipes from inside the running `aura` container succeeds with no outbound download.

### C. Compose topology & lifecycle

8. **Aura compose service + one-shot migrate + persistent home + restart**: Aura is a first-class, ordered, durable service.
   - Current: `compose.yaml` has no `aura` service; migrations run via the host binary; no restart policy.
   - Target: `compose.yaml` gains `aura` (the hardened service of Req 1-2, `depends_on` healthchecks for pg/neo4j/embed, joined to the sibling network) + a one-shot `aura-migrate` (`db migrate && neo4j migrate`, exit 0) gated by `depends_on: { aura-migrate: { condition: service_completed_successfully } }`; the `aura-home` named volume; `restart: unless-stopped` across the long-lived services.
   - Acceptance: `docker compose up` brings the stack healthy only after `aura-migrate` exits 0; writing config then `docker compose down && up` preserves it via `aura-home`; killing the `aura` container causes Docker to restart it.

9. **D-22 keyless boot, fail-closed**: `aura serve` boots without an LLM key; agent calls fail closed.
   - Current: `config.Load()` fail-fasts on empty `OPENROUTER_API_KEY`.
   - Target: `aura serve` boots with an empty key (relax the fail-fast on the serve path only; `db migrate`/`LoadDB()` unchanged); an agent call without a key returns a structured `{"error":"llm_not_configured","hint":…}` (mirroring the empty-`SEARXNG_URL` pattern).
   - Acceptance: `docker compose up` with no `OPENROUTER_API_KEY` reaches healthy; an agent invocation returns `llm_not_configured`; after the key is set (installer flag OR `aura config`/wizard) the same invocation answers for real.

10. **`aura doctor` aggregate health check**: one command verifies the whole install.
    - Current: only scoped `aura web doctor` / `aura mcp doctor` exist.
    - Target: `aura doctor` checks all compose services healthy, Postgres + Neo4j reachable, `mcp-neo4j-cypher` spawns in-container, embed dimension matches `AURA_EMBED_DIMENSIONS`, and LLM-key configured-or-not — one pass/fail line per check, non-zero exit on any hard failure.
    - Acceptance: `aura doctor` on a healthy stack exits 0 all-green; with the embed sidecar stopped it exits non-zero naming the failed check.

11. **Caddy TLS + shared-token auth for the user-facing surface; data loopback**: LAN exposure is gated; the boundary services are never on the LAN.
    - Current: all services bind 127.0.0.1; no auth on any user-facing surface.
    - Target: a bundled Caddy reverse proxy terminates TLS (internal CA / self-signed) and enforces a generated shared access token, fronting **only** the user-facing surface (setup wizard; AG-UI gateway when Phase 12 exists; any LAN-exposed UI). Postgres, Neo4j, embed, agent-memory-mcp, the multimodal sidecars, and the whatsapp bridge remain loopback-only and are never LAN-reachable.
    - Acceptance: from another LAN host the wizard is reachable over HTTPS only with the token (no token → 401/403); a LAN connection to Postgres (5432), Neo4j (7687), embed (8081), agent-memory-mcp (8091), or any sidecar is refused/unreachable.

### D. Distribution

12. **curl|sh installer (Linux/macOS) + HW preflight + idempotent secrets**: one command installs the whole stack.
    - Current: no installer; the README is a developer flow; `.env.example` requires hand-set DB passwords.
    - Target: `scripts/install.sh` (hosted in-repo, raw URL) runs on Linux + macOS: RAM/disk/CPU preflight (warn or abort before a doomed boot) → Docker check → secret-gen (`openssl rand` for `POSTGRES_PASSWORD`/`NEO4J_PASSWORD`, `.env` chmod 600, **no** regenerate if `.env` exists) → fetch compose+`.env` → `docker compose up` → print the wizard URL, access token, and next steps.
    - Acceptance: `curl -fsSL <url>/install.sh | sh` on a clean Linux host with no Python/Node/pip yields a healthy stack + the printed summary, and `which python3 pip node` on the host afterward shows none were installed; re-running leaves existing `.env` secrets byte-identical; under-spec hardware prints a clear warn/abort before bringing the stack up.

13. **Best-effort Docker auto-install + guided fallback; Windows documented door**: missing Docker is handled per-OS.
    - Current: no Docker handling; Windows is dev-only.
    - Target: if Docker is absent the installer tries the package path (`get.docker.com` on Linux, `brew --cask docker` on macOS); on failure it prints clear instructions + the official link and exits non-zero (no silent hang). Windows is a documented Docker-Desktop path: install Docker Desktop, run a documented PowerShell secret-gen step, then `docker compose up` against the shipped `compose.yaml` — **no bespoke Windows installer**.
    - Acceptance: on a Docker-less Linux host the installer auto-installs via `get.docker.com` and continues; with the auto path forced to fail it prints guided instructions and exits non-zero; following the documented Windows steps on a Docker-Desktop host brings the stack up, and the PowerShell one-liner produces a `.env` with generated DB secrets (user never hand-types them).

14. **Public ghcr image pinned per tag; host binary retained**: distribution is public and versioned.
    - Current: goreleaser publishes host binaries only.
    - Target: `.goreleaser.yaml` builds+pushes the multi-arch image to `ghcr.io` pinned per release tag; the existing host-binary artifacts remain for dev; `compose.yaml` references the pinned tag, never `latest`.
    - Acceptance: a tagged release produces a publicly-pullable `ghcr.io/<org>/aura:<tag>` (amd64+arm64) AND the host-binary archives; `compose.yaml` references the pinned tag.

15. **Boot persistence + appliance autostart**: the stack survives reboot turnkey.
    - Current: no restart policy; manual `compose up`.
    - Target: the appliance ships a systemd unit that runs `docker compose up -d` on power-on (`restart: unless-stopped` already covers crashes, Req 8).
    - Acceptance: after an appliance reboot the full stack is healthy with no human action.

16. **Backup wiring + documented restore + documented update**: data durability and lifecycle.
    - Current: Phase 10 has `backup_postgres`/`backup_neo4j` handlers + `scripts/restore_drill.sh`, but packaging wires nothing; no documented update.
    - Target: the appliance enables the Phase-10 scheduled backups by default to a host-visible `AURA_BACKUP_DIR`, with a documented one-command restore drill; the README documents `docker compose pull && docker compose up -d` (migrations via `aura-migrate`, volumes persist).
    - Acceptance: on the appliance a backup artifact appears in the host-visible dir on schedule and the documented restore command rebuilds the DB; `compose pull && up -d` to a newer pinned tag runs new migrations and preserves data.

## Boundaries

**In scope:**
- The hardened `aura` container as the security boundary: non-root, no Docker socket, `no-new-privileges`, `cap_drop: ALL`, read-only rootfs + tmpfs, pid/mem/cpu limits, host-controlled named-volume mounts, secrets via env.
- Host-coupling removals: whatsapp → sibling whatsmeow bridge; `RuntimeDocker`/gateway forbidden inside the box (siblings + streamable-HTTP instead) with a clear in-container error.
- Fat multi-arch Aura image (`docker/aura/Dockerfile`): python/uvx + node/npx + pinned mcp-neo4j-cypher + pre-baked trusted recipes.
- `compose.yaml` additions: hardened `aura` service, one-shot `aura-migrate`, `aura-home` volume, bundled Caddy reverse proxy, whatsapp sibling, `restart: unless-stopped`.
- `scripts/install.sh` self-host door (Linux/macOS): HW preflight, best-effort Docker auto-install + guided fallback, idempotent secret-gen, compose up, post-install summary.
- Appliance pre-seed door + systemd autostart unit.
- D-22 keyless-boot relaxation (config + serve path) with fail-closed agent-call behavior.
- `aura doctor` aggregate health check.
- Caddy TLS (internal CA) + shared-token auth fronting the user-facing surface; data + sidecar services stay loopback.
- Public ghcr.io image publish via goreleaser, pinned per tag; host binary retained.
- Backup wiring to a host-visible dir + documented restore drill; documented `compose pull` update path.
- README rewrite to the end-user quick start (removes the undocumented host `pip install mcp-neo4j-cypher`).

**Out of scope:**
- **An egress allowlist / outbound firewall** — declined 2026-06-10 ("no stupid restriction"). Open NAT egress; exfiltration is an accepted residual, not mitigated here.
- **Internal container lockdown** — `cap_drop: ALL`, read-only root filesystem, forced privilege reduction, seccomp tightening: declined 2026-06-10 ("you have no restriction, why should Aura?"). The container is Aura's full computer, parity with Claude Code on the host — not a jail. Host protection is the container edge + mount set, nothing inside.
- **Removing or adding agent-behavior guardrails** — the skill-injection blocklist (model-prompt integrity) and the skills self-modification HITL gate (product approval) are not host-security guardrails and are not touched by this phase.
- The setup wizard web UI itself — Phase 13 (Slice 9a); this phase depends on it for the deferred API-key door but does not build it.
- Telegram onboarding / channels — Phase 13/14.
- A bespoke Windows installer script — Windows is the documented Docker-Desktop + compose path only.
- Private/licensed registry auth — the image is public; commercial gating is out-of-band (Andrea's commercial track).
- In-band auto-update / `aura update` helper — updates are documented `docker compose pull && up`.
- k8s/Helm orchestration — Docker Compose is the unit.
- Real public-domain TLS (Let's Encrypt/ACME) and full multi-user auth/RBAC — Caddy uses an internal CA + a shared token; real auth and per-identity `capability_grants` enforcement stay a future milestone.
- Replacing the goreleaser host binary — it remains for development.
- A native Windows runtime for Aura — Aura always runs in a Linux container; Windows only hosts Docker Desktop.

## Constraints

- **The container is Aura's computer; the code carries no host-protection guardrails and the box carries no internal lockdown.** No command sanitizers, path fences, or permission gates in `shell_exec`/`fs_*`; no `cap_drop: ALL` / read-only rootfs / forced non-root inside the box (they would break full-terminal/self-extension parity). Aura has full power *inside*; the host is protected only by the container edge + the mount set.
- **No Docker socket in the `aura` container by default.** The one load-bearing invariant: not mounting the socket keeps the box a box (it costs Aura no current capability). A container-management capability, if ever wanted, is wired deliberately (siblings, or an opt-in socket), never a default jail-break.
- **Resource limits are stability, not security.** `cpus`/`mem`/`pids` follow the mini-PC budget; they are not a confinement control.
- **Host requirement = Docker only.** No Python/Node/pip/PATH setup on the host for any in-scope path. (Windows additionally needs Docker Desktop.)
- **Data + sidecar services never bind the LAN.** Postgres, Neo4j, embed, agent-memory-mcp, the multimodal sidecars, and the whatsapp bridge stay loopback-only; only Caddy-fronted user-facing endpoints are LAN-exposed.
- **Multi-arch.** The image must build for `linux/amd64` and `linux/arm64` (the DGX appliance is arm64).
- **`internal/knowledge/client.go` unchanged.** The MCP-in-image win is packaging-only.
- **Image must be cache-stable to build.** Layer ordering keeps the heavy runtime layer cached (cold rebuild ~45–60 min; do not invalidate it on every code change).
- **Pinned versions.** `mcp-neo4j-cypher==0.6.0`; the ghcr image referenced in compose is pinned per release tag, never `latest`.
- **Idempotent installer.** Re-running `install.sh` must not regenerate existing secrets or duplicate state.
- **File-size + quality gates** per CLAUDE.md apply to any new Go (`aura doctor`, the config relaxation, the in-container docker-runtime guard): ≤600 LOC/file, vet/build/test/-race green, ≥85% owned-surface coverage.

## Acceptance Criteria

- [ ] Inside the running `aura` container: Aura has full power (writes its home, installs a skill, spawns a subprocess); no `/var/run/docker.sock` and `docker ps` fails; the host filesystem outside the declared mounts is not visible; `cpus`/`mem`/`pids` limits are in effect.
- [ ] `docker history <image>` exposes no secret value in any layer; secrets reach the process only via env; `aura-home` preserves `llm.json`/`mcp/servers.json`/`Agent.md` across `down`/`up`.
- [ ] A `runtime.kind=docker` MCP launched from inside the `aura` container returns a clear "deploy as a compose sibling" error; the streamable-HTTP sibling equivalent mounts and lists tools.
- [ ] `command -v wsl.exe` inside the image returns non-zero; with the whatsapp sibling up, the recipe resolves to it and lists tools; with it down, Aura boots fail-soft.
- [ ] `docker run --rm <image> sh -c "command -v python3 && command -v node && command -v uvx && command -v mcp-neo4j-cypher"` resolves all four (amd64 + arm64); the final stage is non-root.
- [ ] With the stack up, an in-container Neo4j round-trip succeeds AND the host has no `mcp-neo4j-cypher` and no Aura-installed Python.
- [ ] With host egress blocked, the pre-baked `calculator` (uvx) and `mail` (npx) recipes run from inside the container with no download.
- [ ] `docker compose up` brings the stack healthy only after `aura-migrate` exits 0; a killed `aura` container is auto-restarted.
- [ ] `docker compose up` with no `OPENROUTER_API_KEY` boots `aura serve`; an agent call returns `llm_not_configured`; after the key is set, chat works.
- [ ] `aura doctor` exits 0 all-green on a healthy stack and non-zero naming the failed check when a service is down.
- [ ] From a LAN host: the wizard is HTTPS-reachable only with the access token (no token → denied); Postgres/Neo4j/embed/agent-memory/sidecars are refused/unreachable.
- [ ] `curl -fsSL <url>/install.sh | sh` on a clean Linux host yields a healthy stack + a printed summary (wizard URL, access token, next steps) with zero host Python/Node/pip; re-running is idempotent (secrets unchanged); under-spec hardware warns/aborts first.
- [ ] Missing Docker → installer auto-installs via the OS package path, and on forced-failure falls back to guided instructions + non-zero exit (no hang); the documented Windows steps bring the stack up.
- [ ] A tagged release publishes a public multi-arch `ghcr.io/<org>/aura:<tag>` AND host binary archives; compose references the pinned tag.
- [ ] After an appliance reboot the stack is healthy with no human action.
- [ ] A scheduled backup artifact appears in the host-visible backup dir and the documented restore command rebuilds the DB; `compose pull && up -d` to a newer tag runs migrations and preserves data.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.90  | 0.75 | ✓      | Single outcome: Aura-in-a-box, container = boundary, only-Docker install |
| Boundary Clarity   | 0.90  | 0.70 | ✓      | Explicit out-of-scope incl. egress allowlist declined, behavior guardrails untouched, wizard/Windows-installer/registry/auto-update/k8s/ACME/RBAC |
| Constraint Clarity | 0.85  | 0.65 | ✓      | No-host-guardrails, no-docker-socket invariant, Docker-only host, data-loopback, multi-arch, client.go unchanged, cache-stable layers |
| Acceptance Criteria| 0.86  | 0.70 | ✓      | 16 pass/fail criteria, boundary-hardening front-loaded       |
| **Ambiguity**      | 0.13  | ≤0.20| ✓      | 16 decisions locked; security model re-centered 2026-06-10   |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective      | Question summary                                  | Decision locked                                                        |
|-------|------------------|---------------------------------------------------|-----------------------------------------------------------------------|
| 1     | Researcher/Boundary | Docker-missing behavior? installer OS scope?    | Auto-install Docker; OS = Windows + macOS + Linux (incl arm64)        |
| 2     | Failure Analyst  | Reconcile auto-install across OSes? Windows door? | Best-effort auto + guided fallback (get.docker.com/brew); Windows = documented Docker Desktop + compose |
| 3     | Seed Closer      | Offline recipes? aura doctor? update path?        | Pre-bake common recipes; `aura doctor` in scope; documented `compose pull` |
| 4     | Seed Closer      | Registry access? boot persistence? network bind?  | Public ghcr image; restart + appliance systemd autostart; LAN+TLS     |
| 5     | Failure Analyst  | TLS approach? exposed-surface auth? phase split?  | Bundled Caddy (internal CA); shared access token; keep LAN+TLS in P17 |
| 6     | Seed Closer      | Backup/restore in scope? HW preflight?            | Wire P10 backups + documented restore; installer HW preflight warn/abort |

**Revision — 2026-06-10 (re-center on the container-as-boundary model):**

| # | Decision point | Locked |
|---|----------------|--------|
| R1 | What is the security model? | The **Aura container is the boundary**. Host protection is structural (confinement), not behavioral (code guardrails). `shell_exec` is a full in-box terminal; the deleted sandbox-agent (`c9e1124e`) and the absence of code guardrails are correct. |
| R2 | Egress posture? | **Open NAT egress, no allowlist** ("no stupid restriction"). Outbound exfiltration is an accepted, documented residual — explicitly outside the threat model. |
| R3 | Docker-in-Aura? | **No Docker socket in the `aura` container, ever.** `RuntimeDocker`/gateway MCP are not launchable in-box; container-needing MCP become compose siblings over streamable-HTTP. |
| R4 | whatsapp host coupling? | **Sibling whatsmeow bridge**; the image carries no `wsl.exe`. |
| R5 | Scope breadth of the rewrite? | **Full distribution scope retained**, re-centered on the boundary model; behavior guardrails (injection blocklist, skills HITL) untouched as non-host-security concerns. |
| R6 | Why should Aura have restrictions Claude Code doesn't? | **It shouldn't.** The container is Aura's full computer, not a jail — no `cap_drop`/read-only-rootfs/forced-non-root/seccomp tightening. Full parity inside (writable fs, own root, full egress, self-extension). The container is kept for **packaging** + a **resettable host edge** for unattended/external-input turns; the lone invariant is no host Docker socket; resource limits are mini-PC stability, not security. |

---

*Phase: 17-packaging-distribution*
*Spec created: 2026-06-05; rewritten 2026-06-10*
*Next step: /gsd-discuss-phase 17 — implementation decisions (image base/version + layer order, the read-only-rootfs/tmpfs split, cap_drop add-backs if any, the in-container docker-runtime guard, whatsapp sibling image, Caddy config, doctor checks, systemd unit)*
