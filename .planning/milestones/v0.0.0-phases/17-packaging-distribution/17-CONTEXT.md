# Phase 17: Packaging & Distribution - Context

**Gathered:** 2026-06-14
**Status:** Ready for planning

<domain>
## Phase Boundary

Ship Aura as **its own container plus its sidecars**, installable with **only Docker on the
host**. The Aura agent — model loop, full-terminal `shell_exec`, MCP subprocesses, skills
runtime — runs *inside* that container with the **same full power Claude Code has on the
operator's machine** (writable filesystem, root of its own box, full egress, self-extension).
The container earns its place through **packaging** (one image; the host stays clean) and a
**free, resettable host edge** (a container cannot touch the host outside its mounts) —
**not** through internal lockdown. This phase ships the artifacts that make the box real: a fat
Aura image, a hardened-as-a-boundary (not as a jail) `aura` compose service, host-coupling
removals (whatsapp sibling, no in-container Docker), a `curl|sh` installer, an appliance door, a
Caddy TLS front, and an `aura doctor` health check.

**This discussion was redirected into a 4-spike box-model investigation (spikes 059-062,
2026-06-14) before settling implementation decisions.** The spikes resolved a live conflict
between the locked SPEC and an audit commit. See `<decisions>` D-01..D-06 and `<canonical_refs>`.
</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**16 requirements are locked.** See `17-SPEC.md` for full requirements, boundaries, and
acceptance criteria. Downstream agents MUST read `17-SPEC.md` before planning or implementing.
Requirements are not duplicated here.

**In scope (from SPEC.md):**
- The `aura` container as the security **boundary** (host-controlled mounts, no Docker socket, env secrets) — **re-interpreted by this discussion: NOT internal lockdown** (see D-01/D-02).
- Host-coupling removals: whatsapp → sibling whatsmeow bridge; `RuntimeDocker`/gateway forbidden inside the box (clear in-container error).
- Fat multi-arch Aura image (`docker/aura/Dockerfile`): python/uvx + node/npx + pinned mcp-neo4j-cypher + pre-baked trusted recipes.
- `compose.yaml` additions: the `aura` service, one-shot `aura-migrate`, `aura-home` volume, bundled Caddy, whatsapp sibling, `restart: unless-stopped`.
- `scripts/install.sh` (Linux/macOS): HW preflight, best-effort Docker auto-install + guided fallback, idempotent secret-gen, compose up, summary.
- Appliance pre-seed door + systemd autostart; D-22 keyless-boot relaxation; `aura doctor`; Caddy TLS + shared token; ghcr.io publish; backup wiring + documented restore/update; README rewrite.

**Out of scope (from SPEC.md):**
- Egress allowlist / outbound firewall (declined — open NAT egress, documented residual).
- **Internal container lockdown** (`cap_drop: ALL`, read-only rootfs, forced non-root, seccomp tightening) — declined; the container is Aura's full computer, not a jail. **This phase additionally REVERTS an audit commit (`ec7fe2f6`) that violated this — see D-01.**
- Agent-behavior guardrail changes (injection blocklist, skills HITL) — untouched.
- The setup wizard UI (Phase 13); Telegram onboarding/channels (Phase 13/14); a bespoke Windows installer; private registry auth; in-band auto-update; k8s/Helm; real public TLS + multi-user RBAC; replacing the goreleaser host binary; a native Windows runtime.
</spec_lock>

<decisions>
## Implementation Decisions

### Box model — settled by spikes 059-062 (BINDING; supersedes the audit's hardening)

- **D-01 — Revert the audit jail (`ec7fe2f6`, 2026-06-12).** The audit added a **distroless** root
  `Dockerfile` + `cap_drop: ALL` + `read_only: true` + forced non-root `65532` on the `aura`
  compose service — **two days after** the SPEC (2026-06-10) locked "the container is Aura's
  computer, not a jail." Spike 059 proved this box **structurally cannot run `shell_exec`**
  (`exec "sh": not found`, exit 127), cannot self-extend (read-only), cannot spawn
  `mcp-neo4j-cypher` (no python), and runs non-root. **Phase 17 reverts it**: drop the distroless
  root `Dockerfile`, and remove `cap_drop`/`read_only`/`user 65532` from the `aura` service. The
  SPEC pre-authorizes this (the "accepted residual, operator's informed choice 2026-06-10").

- **D-02 — Baseline box = fat full-power writable container, default everywhere** (dev + appliance,
  x86_64 + arm64). Writable rootfs, container root, `shell_exec` + skills self-extension +
  `mcp-neo4j-cypher` parity. The **only** structural invariants (proven sufficient on plain runc,
  spike 059): **no host Docker socket** + `cpus`/`mem_limit`/`pids_limit` (mini-PC stability, not
  security). The free host edge — host fs outside declared mounts invisible — comes free. This IS
  the SPEC "not a jail" model.

- **D-03 — Optional strong-isolation appliance tier = gVisor `runsc` via `compose.gvisor.yaml`**
  (`runtime: runsc` on the `aura` service), applied **native-Linux/arm64 only**, **OFF on
  Docker-Desktop dev** (can't host runsc — spikes 010/059). gVisor is the only strong-isolation
  option that is simultaneously a Docker-runtime drop-in (Docker-only host ✓), **transparent to
  the workload** (full parity inside — Aura still sees a full Linux, spike 010 proved python
  survives), arm64-capable, and **KVM-free**. "Thick walls without a jail." **This is the menu
  already established by amendment #50 (dev full-trust; prod hardening tier).**

- **D-04 — PRD/SPEC AMENDMENT REQUIRED before `/gsd-execute-phase 17`.** gVisor is not in the
  locked SPEC. Amend to (a) **add** the optional gVisor appliance tier, framed as a *transparent
  isolation boundary* NOT capability-stripping (so it is consistent with "no internal lockdown");
  (b) **record** the `ec7fe2f6` revert and correct the SPEC Background (which still claims "no aura
  Dockerfile / no aura compose service" — both now stale); (c) note Docker Sandboxes as
  evaluated-and-deferred. Per the PRD-first principle, this amendment commit lands **before** code.

- **D-05 — Docker Sandboxes (`sbx`) is NOT the appliance runtime** (spike 062). It is the
  Firecracker-microVM **gold standard + direction-signal**, but: arm64 = macOS-only (the **DGX
  appliance is arm64** → unsupported), wraps coding-agent CLIs not a persistent compose stack, perf
  "crippling" on a CPU-budgeted mini-PC, and needs `sbx login`. Keep it as an **optional x86_64-Linux
  dev/power-user wrapper** + a future-capability reference (see Deferred).

- **D-06 — Fat image base** (spike 060): multi-stage `golang:1.26.4` builder → `debian:bookworm-slim`
  runtime with python3 + `uv`/`uvx` (COPYed from `ghcr.io/astral-sh/uv`) + node/npx + pinned
  `mcp-neo4j-cypher==0.6.0` (`pip --break-system-packages`) + git + curl, then `COPY --from=build
  /out/aura` as the **last** layer (cache-stable: 73s cold → 1.4s warm). Multi-arch via `buildx`
  amd64+arm64. Pre-bake the trusted recipes (uvx `calculator`, npx `mail`) as their own warm layers
  for the offline appliance (SPEC Req 7).

### Remaining HOW areas — captured at SPEC-default for the planner (user did not deep-dive)

- **D-07 — whatsapp sibling:** a whatsmeow bridge as a **compose sibling over streamable-HTTP**
  (mirror `aura-agent-memory-mcp`); the `catalog.go` recipe resolves to the sibling endpoint;
  fail-soft when down; the image carries **no `wsl.exe`**. Basis: the existing chetto1983
  whatsapp-mcp fork + `002-whatsapp-mcp-pairing/bridge-patch.diff` (spike 002). *Planner chooses the
  concrete bridge image — flag for a quick user confirm if non-trivial.*
- **D-08 — In-container docker-runtime guard:** `RuntimeDocker`/`RuntimeDockerGateway` detect
  in-box (an `AURA_IN_CONTAINER=1` marker baked into the image is the cleanest signal) and return a
  clear actionable error ("docker runtime unavailable inside the container — deploy as a compose
  sibling and mount via URL"), not a confusing `exec: "docker": not found`. Lives in
  `internal/mcp/manager/runtime.go` dispatch.
- **D-09 — `aura doctor`:** must NOT use `docker compose ps` (no socket in-box). Direct probes
  instead: PG ping, Neo4j round-trip via `mcp-neo4j-cypher`, embed dimension match
  (`AURA_EMBED_DIMENSIONS` vs sidecar `/v1/embeddings`), `mcp-neo4j-cypher` spawn, LLM-key
  configured-or-not. One pass/fail line per check, non-zero exit on any hard failure. Reuse the
  exit-code pattern of `aura web doctor` / `aura mcp doctor` (`cmd/aura`).
- **D-10 — D-22 keyless boot:** relax `config.Load()`'s LLM fail-fast on the **serve path only**
  (`LoadDB()`/`db migrate` unchanged); an agent call without a key returns structured
  `llm_not_configured` (mirror the empty-`SEARXNG_URL` fail-soft pattern in `internal/config`).
- **D-11 — Caddy:** internal-CA/self-signed TLS + a generated shared access token, fronting **only**
  the user-facing surface (wizard + AG-UI); Postgres/Neo4j/embed/agent-memory/sidecars/whatsapp stay
  loopback-only. Token enforcement mechanism (Caddy `forward_auth`/header check) = planner choice.
- **D-12 — `scripts/install.sh`:** HW preflight (RAM/disk/CPU; the deploy target is mini-PC
  16-core/32GB, **16 GB min** — warn below comfortable, abort below a hard floor); Docker check +
  best-effort auto-install (`get.docker.com` / `brew --cask docker`) + guided fallback + non-zero
  exit (no hang); idempotent secret-gen (`openssl rand`, `.env` chmod 600, **no** regen if `.env`
  exists); compose up; print wizard URL + token + next steps. Windows = documented Docker-Desktop +
  PowerShell secret-gen door (no bespoke installer).
- **D-13 — compose + goreleaser + lifecycle:** `aura` service (de-hardened per D-01/D-02) +
  one-shot `aura-migrate` (gated `service_completed_successfully`) + `aura-home` named volume
  (`AURA_CONFIG_DIR`) + bundled Caddy + whatsapp sibling + `restart: unless-stopped`, **plus** the
  optional `compose.gvisor.yaml` override (D-03). `.goreleaser.yaml` adds `buildx` multi-arch +
  ghcr.io push pinned per tag (never `latest`); host binaries retained. systemd autostart unit.
- **D-14 — backup/restore/update:** enable the Phase-10 scheduled backups to a host-visible
  `AURA_BACKUP_DIR` by default; documented one-command restore drill (`scripts/restore_drill.sh`);
  document `docker compose pull && up -d` (migrations via `aura-migrate`, volumes persist).
- **D-15 — `internal/knowledge/client.go` is UNCHANGED** (SPEC constraint): the same stdio spawn
  runs inside the image where python lives; the MCP-in-image win is packaging-only.

### Claude's Discretion
- D-07/D-11/D-12 specifics (whatsapp bridge image, Caddy token enforcement mechanism, exact HW
  thresholds) are SPEC-default + planner choice. The operator explicitly chose to skip deep-diving
  these and can revisit at plan-review; surface any non-trivial fork in the plan for a quick confirm.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase spec + the box-model spikes (READ FIRST)
- `.planning/phases/17-packaging-distribution/17-SPEC.md` — the 16 locked requirements, boundaries, acceptance criteria. **MUST read before planning.**
- `.planning/spikes/061-phase17-isolation-tier/README.md` — the box-model **decision matrix** + the PRD-amendment call (capstone).
- `.planning/spikes/059-phase17-box-parity-edge/README.md` — on-host evidence: distroless breaks `shell_exec`; the fat box delivers parity; the SPEC "free edge" holds on runc.
- `.planning/spikes/060-phase17-fat-image-base/README.md` — fat runtime base resolves SPEC Req 5; size + cache-stability.
- `.planning/spikes/062-docker-sandboxes-sbx-fit/README.md` — Docker Sandboxes verdict (microVM direction-signal; not the appliance runtime).
- `.planning/spikes/010-sandbox-gvisor-runsc/README.md` — prior art: gVisor workload validated, Docker Desktop can't host runsc; native-Linux/CI/prod tier.
- `.planning/spikes/MANIFEST.md` §Session-15 requirements — the binding box-model requirements.
- `.claude/skills/spike-findings-Aura/references/sandbox-runtime.md` — the established hardening-tiers menu (token/egress/gVisor) + compose-override-per-tier pattern.

### Codebase touch points (from the Phase-17 scout)
- `compose.yaml` — the `aura` service to de-harden (lines 10-51) + the sibling/loopback topology to mirror.
- `Dockerfile` (root, distroless) — to be **removed/replaced** by `docker/aura/Dockerfile` (D-01/D-06).
- `.goreleaser.yaml` — add multi-arch docker build + ghcr.io push (D-13).
- `internal/mcp/manager/runtime.go` (RuntimeDocker/Gateway) + `catalog.go` (whatsapp `wsl.exe` recipe) — the in-container guard (D-08) + whatsapp sibling (D-07).
- `internal/config/` (`config.go` LLM fail-fast, `LoadDB`, the empty-`SEARXNG_URL` pattern) — D-22 keyless boot (D-10).
- `cmd/aura/` (`web doctor` `web.go`, `mcp doctor` `mcp.go`/`mcp_status.go`, `main.go` subcommand wiring) — the `aura doctor` aggregate (D-09).
- `internal/knowledge/client.go` — UNCHANGED (D-15); confirms `mcp-neo4j-cypher` stdio spawn.
- `internal/cron/handlers/backup.go` + `scripts/restore_drill.sh` (`AURA_BACKUP_DIR`) — backup wiring (D-14).

### External (the box-model landscape)
- <https://www.docker.com/products/docker-sandboxes/> + <https://docs.docker.com/ai/sandboxes/get-started/> — Docker Sandboxes (sbx) capabilities + OS/arch support (D-05).
- <https://github.com/agent-sandbox/agent-sandbox> + <https://github.com/kubernetes-sigs/agent-sandbox> + <https://github.com/alibaba/OpenSandbox> — k8s/microVM agent-sandbox landscape (OUT per SPEC; direction-signal only).
- git `ec7fe2f6` ("fix(audit): close P1 items") + `c9e1124e` ("remove the container sandbox") — the audit jail to revert + the sandbox-removal that triggered the SPEC rewrite.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **compose.yaml topology** already loopback-binds every service (127.0.0.1) and uses the
  streamable-HTTP sibling pattern (`aura-agent-memory-mcp` build-from-`docker/agent-memory`) — the
  exact shape for the whatsapp sibling (D-07) and the Caddy "only user-facing is LAN" split (D-11).
- **`docker/agent-memory/` + `docker/markitdown/`** are working in-repo sidecar Dockerfiles
  (python:3.11/3.12-slim, pinned deps) — the template for `docker/aura/Dockerfile`'s runtime stage.
- **`aura web doctor` / `aura mcp doctor`** already implement the per-check + exit-code shape `aura
  doctor` aggregates (D-09).
- **Phase-10 `backup_postgres`/`backup_neo4j` handlers + `scripts/restore_drill.sh`** exist; Phase 17
  only wires/schedules them to a host-visible dir (D-14).

### Established Patterns
- **Fail-soft MCP boot** (memory `mcp_sidecar_lifecycle`) — the whatsapp sibling degrade path (D-07).
- **Compose-override-per-tier** (`compose.<tier>.yaml`; spikes 005/006/010) — the gVisor tier (D-03).
- **No host-protection guardrails in Go code** (SPEC constraint + memory `aura_full_host_terminal_primary`) — D-01/D-02 keep the code guardrail-free; confinement is the container edge.

### Integration Points
- The de-hardened `aura` service `depends_on` pg/neo4j/embed healthchecks + the new `aura-migrate`
  one-shot; joins the existing sibling network; mounts `aura-home` → `AURA_CONFIG_DIR`.
- Caddy fronts wizard + AG-UI (`aura serve` `POST /agent/run`, Phase 12); everything else loopback.

</code_context>

<specifics>
## Specific Ideas

- Operator directive (2026-06-14): **"make Aura powerful"** — the box must be Aura's *full computer*,
  not crippled. This is why the audit's distroless jail is reverted (D-01) and the model is
  full-power-by-default (D-02), with isolation added *transparently* (gVisor, D-03) rather than by
  stripping the agent's capabilities.
- The microVM "agent in a box" shape (own kernel, docker-in-VM **without the host socket**) seen in
  Docker Sandboxes is the aspirational direction; gVisor is the pragmatic, arm64-capable, Docker-only
  realization of it for Phase 17.

</specifics>

<deferred>
## Deferred Ideas

- **Docker Sandboxes (sbx) as an optional x86_64-Linux dev/power-user wrapper** — not the appliance
  runtime (arm64 + shape), but a great way to run the Aura *dev loop* safely. Revisit when an x86_64
  appliance SKU or a dev-ergonomics push warrants it.
- **docker-in-box without the host socket** (Docker Sandboxes' best idea) — a future capability that
  could *lift SPEC Req 3* (let Aura run a docker-runtime MCP **inside** its own box) without ever
  mounting the host socket. Out of scope for Phase 17; note for a future milestone.
- **Kata/Firecracker microVM tier** — stronger than gVisor but needs KVM + more RAM per box; gVisor
  is the better single-box fit now. Reconsider for a high-isolation appliance SKU on x86_64+KVM.

### Reviewed Todos (not folded)
None — no matching pending todos for Phase 17.

</deferred>

---

*Phase: 17-packaging-distribution*
*Context gathered: 2026-06-14*
