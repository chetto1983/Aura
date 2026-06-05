# Phase 17: Packaging & Distribution — Specification

**Created:** 2026-06-05
**Ambiguity score:** 0.138 (gate: ≤ 0.20)
**Requirements:** 14 locked

## Goal

An end user installs and runs Aura with **only Docker on the host** — one `curl|sh` command (Linux/macOS) or a pre-imaged appliance — instead of today's developer flow (git clone → `make tools` → `go run` + an undocumented host `pip install mcp-neo4j-cypher`). The host requirement for MCP, Python, Node, and PATH surgery collapses to Docker alone.

## Background

Aura distributes today as a goreleaser static binary + `compose.yaml` running 6 sidecars (postgres, neo4j, llama-embed, sandbox-agent, searxng). There is **no Dockerfile for the `aura` app itself** — it runs via `go run`/the goreleaser binary. The "single static binary" promise breaks the moment MCP is needed: `mcp-neo4j-cypher` is a Python subprocess spawned from the **host** PATH ([internal/knowledge/client.go:49-71](../../../internal/knowledge/client.go#L49-L71), requiring an undocumented `pip install mcp-neo4j-cypher==0.6.0`), and generic MCP recipes want host `uvx`/`npx`. `config.Load()` ([internal/config](../../../internal/config/config.go)) fail-fasts on an empty `OPENROUTER_API_KEY`. `AURA_CONFIG_DIR=~/.aura` is the config root. The setup wizard (Phase 13) is not built. This phase ships the missing artifacts: a fat Aura image, an `aura` compose service, a `curl|sh` installer, an appliance door, a bundled Caddy TLS proxy, and an `aura doctor` health-check — registered PRD-first as Slice 14 / amendment #47 (commit `47f46f72`).

## Requirements

1. **Fat multi-arch image**: A single Aura container image bundles all runtimes so the host needs only Docker.
   - Current: No Dockerfile for the `aura` app; MCP runtimes (python/node) must exist on the host PATH.
   - Target: `docker/aura/Dockerfile` (multi-stage: golang builder → slim runtime with python3 + `uv`/`uvx` + node + `npx` + pinned `mcp-neo4j-cypher==0.6.0` → COPY `aura`), built for `linux/amd64` + `linux/arm64`.
   - Acceptance: `docker run --rm <image> aura version` prints build metadata on both amd64 and arm64; `docker run --rm <image> sh -c "command -v python3 && command -v node && command -v uvx && command -v mcp-neo4j-cypher"` resolves all four inside the image.

2. **MCP subprocess in-image, host clean**: `mcp-neo4j-cypher` spawns inside the `aura` container, unchanged code.
   - Current: `internal/knowledge/client.go` spawns it from the host PATH.
   - Target: The same stdio-spawn runs inside the image where Python lives; `internal/knowledge/client.go` is unchanged by this phase.
   - Acceptance: With the stack up, an in-container Neo4j round-trip succeeds (e.g. `aura neo4j ping`-equivalent returns the server version); on the HOST, `command -v mcp-neo4j-cypher` returns non-zero (not installed) and there is no host Python dependency.

3. **Aura compose service + one-shot migrate + persistent home**: Aura runs as a compose service with ordered migration and durable config.
   - Current: `compose.yaml` has no `aura` service; migrations run via the host binary.
   - Target: `compose.yaml` gains `aura` (`depends_on` healthchecks for pg/neo4j/embed) + a one-shot `aura-migrate` service (`db migrate && neo4j migrate`, exit 0) gated by `depends_on: { aura-migrate: { condition: service_completed_successfully } }`; a named `aura-home` volume mounts `~/.aura`.
   - Acceptance: `docker compose up` brings the stack up only after `aura-migrate` exits 0; writing `llm.json`/`mcp/servers.json`/an `Agent.md` then `docker compose down && up` preserves them via `aura-home`.

4. **curl|sh installer (Linux/macOS)**: One command installs the whole stack.
   - Current: No installer; the README is a developer flow.
   - Target: `scripts/install.sh` (hosted in-repo, raw URL) runs on Linux + macOS: HW preflight → Docker check → secret-gen → fetch compose+`.env` → `docker compose up` → print the wizard URL, access token, and next steps.
   - Acceptance: `curl -fsSL <url>/install.sh | sh` on a clean Linux host with no Python/Node/pip results in the full stack healthy and the post-install summary printed; `which python3 pip node` on the host afterward shows none were installed by Aura.

5. **Best-effort Docker auto-install, fall back to guided**: Missing Docker is handled per-OS.
   - Current: No Docker handling.
   - Target: If Docker is absent, the installer tries the package-manager auto path (`get.docker.com` on Linux, `brew --cask docker` on macOS, `winget install Docker.DockerDesktop` on Windows-documented-path); on absence/failure of the tool it falls back to printing clear install instructions + the official link and exits non-zero.
   - Acceptance: On a Linux host without Docker, the installer auto-installs via `get.docker.com` and continues; with the auto path forced to fail (no `get.docker.com` reachable / no brew/winget), it prints guided instructions and exits non-zero (no silent hang).

6. **Windows documented-compose door**: Windows is a supported target via Docker Desktop + documented compose.
   - Current: Windows is dev-only, no documented install.
   - Target: README documents the Windows path: install Docker Desktop, run a documented PowerShell secret-gen step, then `docker compose up` against the shipped `compose.yaml`. No bespoke Windows installer script.
   - Acceptance: Following the documented Windows steps on a Docker-Desktop host brings the stack up; the documented PowerShell one-liner produces a chmod-equivalent `.env` with generated `POSTGRES_PASSWORD`/`NEO4J_PASSWORD` (user never hand-types DB secrets).

7. **Auto-generated secrets, idempotent**: DB/Neo4j passwords are generated, never hand-edited.
   - Current: `.env.example` requires the user to set `POSTGRES_PASSWORD`/`NEO4J_PASSWORD` by hand.
   - Target: `install.sh` generates `POSTGRES_PASSWORD`/`NEO4J_PASSWORD` (`openssl rand`), writes `.env` chmod 600, and does NOT regenerate if `.env` already exists.
   - Acceptance: First install creates a chmod-600 `.env` with random secrets; re-running the installer leaves the existing secrets byte-identical (idempotent).

8. **D-22 keyless-boot relaxation**: `aura serve` boots without an LLM key; agent calls fail closed.
   - Current: `config.Load()` fail-fasts on empty `OPENROUTER_API_KEY`.
   - Target: `aura serve` boots with an empty key (relax the fail-fast on the serve path; `aura db migrate`/`LoadDB()` unchanged); an agent call without a configured key returns a structured `{"error":"llm_not_configured","hint":...}` result (fail-closed, mirroring empty `SEARXNG_URL`).
   - Acceptance: `docker compose up` with no `OPENROUTER_API_KEY` set → `aura serve` reaches healthy (no abort); an agent invocation returns `llm_not_configured`; after the key is set (installer flag OR `aura config`/wizard) the same invocation produces a real answer.

9. **Pre-baked recipes (offline appliance)**: Common MCP recipe packages are baked at build time.
   - Current: `uvx`/`npx` would fetch recipe packages (calculator, mail) from the network on first use.
   - Target: The trusted recipe packages are baked into the image at build time so a first-boot/air-gapped appliance runs them with zero network fetch.
   - Acceptance: With host network egress blocked, installing+invoking the `calculator` (uvx) and `mail` (npx) recipes from inside the running `aura` container succeeds without any outbound package download.

10. **`aura doctor` aggregate health-check**: One command verifies the whole install.
    - Current: Only scoped `aura web doctor` / `aura mcp doctor` exist.
    - Target: `aura doctor` checks: all compose services healthy, Postgres + Neo4j reachable, `mcp-neo4j-cypher` spawns, embed dimension matches `AURA_EMBED_DIMENSIONS`, and LLM key configured-or-not — printing a pass/fail line per check and a non-zero exit on any hard failure.
    - Acceptance: `aura doctor` on a healthy stack exits 0 with all checks green; with the embed sidecar stopped it exits non-zero and names the failed check.

11. **Caddy TLS + shared-token auth for user-facing surface; data loopback**: LAN exposure is gated.
    - Current: All services bind 127.0.0.1; the AG-UI gateway has no auth.
    - Target: A bundled Caddy reverse-proxy service terminates TLS (internal CA / self-signed) and enforces a generated shared access token, fronting ONLY the user-facing surface (setup wizard; AG-UI gateway when Phase 12 exists). Postgres, Neo4j, embed, and sandbox-agent remain loopback-only and are never reachable from the LAN.
    - Acceptance: From another host on the LAN, the wizard is reachable over HTTPS only with the access token (no token → 401/403); a LAN connection attempt to Postgres (5432), Neo4j (7687), embed (8081), or sandbox-agent (2468) is refused/unreachable.

12. **Public ghcr image pinned per tag; host binary retained**: Distribution is public and versioned.
    - Current: goreleaser publishes host binaries only.
    - Target: `.goreleaser.yaml` builds+pushes the multi-arch image to `ghcr.io` pinned per release tag; the existing goreleaser host-binary artifacts remain for dev.
    - Acceptance: A tagged release produces a publicly-pullable `ghcr.io/<org>/aura:<tag>` (amd64+arm64) AND the host binary archives; `compose.yaml` references the pinned tag (not `latest`).

13. **Boot persistence + appliance autostart**: The stack survives reboot turnkey.
    - Current: No restart policy; manual `compose up`.
    - Target: compose services set `restart: unless-stopped`; the appliance ships a systemd unit that runs `docker compose up -d` on power-on.
    - Acceptance: After a host reboot on the appliance, the full stack is healthy with no human action; killing the `aura` container causes Docker to restart it.

14. **Backup wiring + documented restore; documented update; HW preflight**: Data durability and lifecycle.
    - Current: Phase 10 has `backup_postgres`/`backup_neo4j` handlers + `scripts/restore_drill.sh`, but packaging wires nothing; no install preflight; no documented update.
    - Target: The appliance enables the Phase-10 scheduled backups by default to a host-visible `AURA_BACKUP_DIR`, with a documented one-command restore drill; the README documents the update path `docker compose pull && docker compose up -d` (migrations via `aura-migrate`, volumes persist); `install.sh` runs a RAM/disk/CPU preflight that warns or aborts before a doomed boot.
    - Acceptance: On the appliance, a backup artifact appears in the host-visible backup dir on schedule and the documented restore command rebuilds the DB from it; `docker compose pull && up -d` to a newer pinned tag runs new migrations and preserves data; the installer on under-spec hardware prints a clear warn/abort message before bringing the stack up.

## Boundaries

**In scope:**
- Fat multi-arch Aura image (`docker/aura/Dockerfile`) with python/uvx + node/npx + pinned mcp-neo4j-cypher + pre-baked trusted recipes.
- `compose.yaml` additions: `aura` service, one-shot `aura-migrate`, `aura-home` volume, bundled Caddy reverse proxy, `restart: unless-stopped`.
- `scripts/install.sh` self-host door (Linux/macOS): HW preflight, best-effort Docker auto-install + guided fallback, secret-gen, compose up, post-install summary.
- Appliance pre-seed door + systemd autostart unit.
- D-22 keyless-boot relaxation (config + serve path) with fail-closed agent-call behavior.
- `aura doctor` aggregate health-check command.
- Caddy TLS (internal CA) + shared-token auth fronting the user-facing surface; data services stay loopback.
- Public ghcr.io image publish via goreleaser, pinned per tag; host binary retained.
- Backup wiring to a host-visible dir + documented restore drill; documented `compose pull` update path.
- README rewrite to the end-user quick start (removes the undocumented host `pip install mcp-neo4j-cypher`).

**Out of scope:**
- The setup wizard web UI itself — that is Phase 13 (Slice 9a); this phase depends on it for the deferred API-key door but does not build it.
- Telegram onboarding / channels — Phase 13/14.
- A bespoke Windows installer script — Windows is the documented Docker-Desktop + compose path only.
- Private/licensed registry auth — the image is public; commercial gating is handled out-of-band (Andrea's commercial track).
- In-band auto-update / `aura update` helper — updates are documented `docker compose pull && up`.
- k8s/Helm orchestration — Docker Compose is the unit.
- Real public-domain TLS (Let's Encrypt/ACME) and full multi-user auth/RBAC — Caddy uses an internal CA + a shared token; real auth stays a future milestone.
- Replacing the goreleaser host binary — it remains for development.
- A native Windows runtime for Aura — Aura always runs in a Linux container; Windows only hosts Docker Desktop (consistent with the existing Out-of-Scope decision).

## Constraints

- **Host requirement = Docker only.** No Python/Node/pip/PATH setup on the host for any in-scope path. (Windows additionally needs Docker Desktop.)
- **Data services never bind the LAN.** Postgres, Neo4j, embed sidecar, and sandbox-agent stay loopback-only; only Caddy-fronted user-facing endpoints are LAN-exposed.
- **Multi-arch.** The image must build for `linux/amd64` and `linux/arm64` (the DGX appliance is arm64).
- **`internal/knowledge/client.go` unchanged.** The MCP-in-image win is packaging-only; no change to the MCP client layer.
- **Image must be cache-stable to build.** Layer ordering keeps the heavy runtime layer cached (cold rebuild ~45–60 min; do not invalidate it on every code change).
- **Pinned versions.** `mcp-neo4j-cypher==0.6.0`; the ghcr image referenced in compose is pinned per release tag, never `latest`.
- **Idempotent installer.** Re-running `install.sh` must not regenerate existing secrets or duplicate state.
- **File-size + quality gates** per CLAUDE.md apply to any new Go (e.g. `aura doctor`, the config relaxation): ≤600 LOC/file, vet/build/test/-race green, ≥85% owned-surface coverage.

## Acceptance Criteria

- [ ] `docker run --rm <image> sh -c "command -v python3 && command -v node && command -v uvx && command -v mcp-neo4j-cypher"` resolves all four inside the image (amd64 + arm64).
- [ ] With the stack up, an in-container Neo4j round-trip succeeds AND the host has no `mcp-neo4j-cypher` on PATH and no Aura-installed Python.
- [ ] `docker compose up` brings the stack healthy only after the one-shot `aura-migrate` exits 0; `aura-home` survives `down`/`up`.
- [ ] `curl -fsSL <url>/install.sh | sh` on a clean Linux host yields a healthy stack + a printed summary (wizard URL, access token, next steps), with zero host Python/Node/pip.
- [ ] Missing Docker → installer auto-installs via the OS package path, and on forced-failure falls back to guided instructions + non-zero exit (no hang).
- [ ] Re-running `install.sh` is idempotent: existing `.env` secrets unchanged.
- [ ] `docker compose up` with no `OPENROUTER_API_KEY` boots `aura serve`; an agent call returns `llm_not_configured`; after key set, chat works.
- [ ] With host egress blocked, the pre-baked `calculator` (uvx) and `mail` (npx) recipes run from inside the container without any package download.
- [ ] `aura doctor` exits 0 on a healthy stack (all checks green) and non-zero naming the failed check when a service is down.
- [ ] From a LAN host: the wizard is reachable over HTTPS only with the access token (no token → denied); Postgres/Neo4j/embed/sandbox ports are refused/unreachable from the LAN.
- [ ] A tagged release publishes a public multi-arch `ghcr.io/<org>/aura:<tag>` AND host binary archives; compose references the pinned tag.
- [ ] After an appliance reboot the stack is healthy with no human action; a killed `aura` container is auto-restarted.
- [ ] A scheduled backup artifact appears in the host-visible backup dir and the documented restore command rebuilds the DB; `compose pull && up -d` to a newer tag runs migrations and preserves data.
- [ ] `install.sh` on under-spec hardware prints a warn/abort message before bringing the stack up.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.88  | 0.75 | ✓      | Single outcome: install with only-Docker, two doors          |
| Boundary Clarity   | 0.88  | 0.70 | ✓      | Explicit out-of-scope: wizard, Windows-installer, licensed registry, auto-update, k8s, ACME/RBAC |
| Constraint Clarity | 0.82  | 0.65 | ✓      | Docker-only host, data-loopback, multi-arch, client.go unchanged, cache-stable layers |
| Acceptance Criteria| 0.85  | 0.70 | ✓      | 14 pass/fail criteria                                        |
| **Ambiguity**      | 0.138 | ≤0.20| ✓      | 14 decisions locked across 6 interview rounds                |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective      | Question summary                                  | Decision locked                                                        |
|-------|------------------|---------------------------------------------------|-----------------------------------------------------------------------|
| 1     | Researcher/Boundary | Docker-missing behavior? installer OS scope?    | Auto-install Docker; OS = Windows + macOS + Linux (incl arm64)        |
| 2     | Failure Analyst  | Reconcile auto-install across OSes? Windows door? | Best-effort auto + guided fallback (get.docker.com/brew/winget); Windows = documented Docker Desktop + compose |
| 3     | Seed Closer      | Offline recipes? aura doctor? update path?        | Pre-bake common recipes; `aura doctor` in scope; documented `compose pull` |
| 4     | Seed Closer      | Registry access? boot persistence? network bind?  | Public ghcr image; restart + appliance systemd autostart; LAN+TLS     |
| 5     | Failure Analyst  | TLS approach? exposed-surface auth? phase split?  | Bundled Caddy (internal CA); shared access token; keep LAN+TLS in P17  |
| 6     | Seed Closer      | Backup/restore in scope? HW preflight?            | Wire P10 backups + documented restore; installer HW preflight warn/abort |

---

*Phase: 17-packaging-distribution*
*Spec created: 2026-06-05*
*Next step: /gsd-discuss-phase 17 — implementation decisions (image base/version, layer order, install.sh internals, Caddy config, doctor checks, systemd unit)*
