# Phase 17: Packaging & Distribution — Research

**Researched:** 2026-06-14
**Domain:** Container packaging / Docker Compose distribution / shell-script installer + a small Go surface (`aura doctor`, D-22 keyless boot, in-container docker-runtime guard)
**Confidence:** HIGH (the technical investigation is settled by spikes 059–062 + 010; this RESEARCH consolidates + resolves the implementation HOW + builds the Validation Architecture)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (CONTEXT.md `<decisions>` — BINDING)

**Box model — settled by spikes 059–062 (supersedes the audit's hardening):**
- **D-01 — Revert the audit jail (`ec7fe2f6`, 2026-06-12).** Drop the distroless root `Dockerfile`; remove `cap_drop: ALL` + `read_only: true` + `user 65532` from the `aura` compose service. Spike 059 proved this box structurally cannot run `shell_exec` / self-extend / spawn `mcp-neo4j-cypher`. The SPEC pre-authorizes the revert.
- **D-02 — Baseline box = fat full-power writable container, default everywhere** (dev + appliance, amd64 + arm64). Writable rootfs, container root, full `shell_exec`/skills/MCP parity. The **only** structural invariants (proven sufficient on plain runc): **no host Docker socket** + `cpus`/`mem_limit`/`pids_limit` (mini-PC stability, NOT security).
- **D-03 — Optional strong-isolation tier = gVisor `runsc` via `compose.gvisor.yaml`** (`runtime: runsc`), **native-Linux/arm64 only**, **OFF on Docker-Desktop dev**. Transparent isolation (full parity inside), KVM-free, arm64-capable.
- **D-04 — PRD/SPEC AMENDMENT REQUIRED before `/gsd-execute-phase 17`** (see the dedicated gate section below).
- **D-05 — Docker Sandboxes (`sbx`) is NOT the appliance runtime** (arm64 = macOS-only; shape mismatch; perf; `sbx login`). Evaluated-and-deferred; optional x86_64-Linux dev wrapper only.
- **D-06 — Fat image base:** multi-stage `golang:1.26.4` builder → `debian:bookworm-slim` + python3 + `uv`/`uvx` (COPYed from `ghcr.io/astral-sh/uv`) + node/npx + pinned `mcp-neo4j-cypher==0.6.0` (`pip --break-system-packages`) + git + curl, `COPY --from=build /out/aura` as the **last** layer. Multi-arch via buildx amd64+arm64. Pre-bake the trusted recipes (uvx `calculator`, npx `mail`).

**Remaining HOW (captured at SPEC-default for the planner):**
- **D-07 — whatsapp sibling:** whatsmeow bridge as a compose sibling over streamable-HTTP (mirror `aura-agent-memory-mcp`); `catalog.go` recipe resolves to the sibling endpoint; fail-soft when down; image carries no `wsl.exe`. *Planner chooses the concrete bridge image — flag for a quick user confirm.*
- **D-08 — In-container docker-runtime guard:** `RuntimeDocker`/`RuntimeDockerGateway` detect in-box (`AURA_IN_CONTAINER=1` marker baked into the image is the cleanest signal) and return a clear actionable error. Lives in `internal/mcp/manager/runtime.go` dispatch.
- **D-09 — `aura doctor`:** must NOT use `docker compose ps` (no socket in-box). Direct probes: PG ping, Neo4j round-trip via `mcp-neo4j-cypher`, embed dimension match, `mcp-neo4j-cypher` spawn, LLM-key configured-or-not. One pass/fail line per check, non-zero exit on hard failure. Reuse the exit-code pattern of `aura web doctor`/`aura mcp doctor`.
- **D-10 — D-22 keyless boot:** relax `config.Load()`'s LLM fail-fast on the **serve path only** (`LoadDB()`/`db migrate` unchanged); an agent call without a key returns structured `llm_not_configured` (mirror the empty-`SEARXNG_URL` fail-soft pattern).
- **D-11 — Caddy:** internal-CA/self-signed TLS + a generated shared access token, fronting **only** the user-facing surface (wizard + AG-UI); everything else loopback-only. *Token enforcement mechanism (Caddy `forward_auth`/header check) = planner choice.*
- **D-12 — `scripts/install.sh`:** HW preflight (deploy target = mini-PC 16-core/32GB, **16 GB min** — warn below comfortable, abort below a hard floor); Docker check + best-effort auto-install + guided fallback + non-zero exit; idempotent secret-gen (`openssl rand`, `.env` chmod 600, **no** regen if `.env` exists); compose up; print wizard URL + token + next steps. Windows = documented Docker-Desktop + PowerShell secret-gen door. *Exact HW thresholds = planner choice.*
- **D-13 — compose + goreleaser + lifecycle:** de-hardened `aura` service + one-shot `aura-migrate` (gated `service_completed_successfully`) + `aura-home` named volume (`AURA_CONFIG_DIR`) + bundled Caddy + whatsapp sibling + `restart: unless-stopped`, **plus** optional `compose.gvisor.yaml`. `.goreleaser.yaml` adds buildx multi-arch + ghcr.io push pinned per tag (never `latest`); host binaries retained. systemd autostart unit.
- **D-14 — backup/restore/update:** enable Phase-10 scheduled backups to a host-visible `AURA_BACKUP_DIR`; documented one-command restore drill (`scripts/restore_drill.sh`); document `docker compose pull && up -d`.
- **D-15 — `internal/knowledge/client.go` is UNCHANGED** (SPEC constraint): same stdio spawn runs inside the image where python lives.

### Claude's Discretion (CONTEXT.md)
D-07/D-11/D-12 specifics (whatsapp bridge image, Caddy token enforcement mechanism, exact HW thresholds) are SPEC-default + planner choice. The operator explicitly skipped deep-diving these and can revisit at plan-review; surface any non-trivial fork in the plan for a quick confirm.

### Deferred Ideas (OUT OF SCOPE)
- Docker Sandboxes (sbx) as an optional x86_64-Linux dev/power-user wrapper — not the appliance runtime.
- docker-in-box without the host socket (Docker Sandboxes' best idea) — a future capability that could *lift SPEC Req 3*. Out of scope for Phase 17.
- Kata/Firecracker microVM tier — needs KVM + more RAM; gVisor is the better single-box fit now.
- **From SPEC out-of-scope:** egress allowlist; internal container lockdown; agent-behavior guardrail changes; the setup wizard UI (Phase 13); Telegram onboarding/channels; a bespoke Windows installer; private registry auth; in-band auto-update; k8s/Helm; real public TLS + multi-user RBAC; replacing the goreleaser host binary; a native Windows runtime.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OPS-01 | End-user packaging & distribution: fat Aura container image (`docker/aura/Dockerfile`), `compose.yaml` `aura` service + `aura-migrate` + `aura-home`, `scripts/install.sh`, appliance door, D-22 keyless boot, ghcr publish, backup wiring | This entire RESEARCH maps OPS-01 onto the 16 SPEC requirements (A.1–4 boundary, B.5–7 image, C.8–11 compose/lifecycle, D.12–16 distribution). Each requirement has a per-requirement HOW below with proven spike/decision references and concrete file:line touch points. |
</phase_requirements>

---

## Summary

Phase 17 ships Aura **as its own container plus sidecars**, installable with **only Docker on the host**. The agent runs *inside* a fat container with full Claude-Code parity (writable rootfs, container root, full egress, `shell_exec`, self-extension, MCP subprocess spawning). The container is a **packaging + resettable-host-edge boundary, NOT an internal jail**. Per spike 059, the only structural invariants are **no host Docker socket** + `cpus`/`mem_limit`/`pids_limit` (mini-PC stability). Capability stripping is *deliberately forbidden* — it would break the full-terminal/self-extension parity that is the entire point.

**This phase is unusual: the deep technical investigation is already done.** Four box-model spikes (059 parity/edge, 060 fat-image-base, 061 the isolation-tier decision matrix, 062 Docker Sandboxes) plus prior spike 010 (gVisor) settled every hard architectural question. The net-new research surface is *near zero*; the value of this document is **consolidation** (one planner-ready map of the 16 requirements to their proven HOW + file:line touch points), **landmine surfacing** (the backup `docker exec` paradox, cache-stable layer order, the gVisor `daemon.json` prerequisite, `docker history` secret leakage), and a **rigorous Validation Architecture** mapping each of the 16 acceptance criteria to a machine-checkable method (unit vs live-Docker-integration tier).

**Critical state-of-the-codebase fact:** the current `compose.yaml` `aura` service (lines 10–51) and the root `Dockerfile` **ARE the audit jail `ec7fe2f6`** that D-01 reverts. `compose.yaml:17-20` has `user: "65532:65532"`, `read_only: true`, `cap_drop: ALL`; `Dockerfile:12` uses `gcr.io/distroless/static-debian12:nonroot`. The revert is therefore an *edit of existing artifacts*, not a greenfield add. The SPEC Background (SPEC.md:16) still claims "no Dockerfile for the `aura` app" and "compose.yaml has no `aura` service" — both **now stale** (the audit added them); D-04's amendment corrects this.

**Primary recommendation:** Land the D-04 PRD/SPEC amendment commit FIRST (pre-execution gate). Then wave-order: (Wave 1) `docker/aura/Dockerfile` + D-01 revert of the root Dockerfile and the compose `aura` hardening; (Wave 2, parallel) the independent Go changes (D-08 in-container guard, D-10 keyless boot, D-09 `aura doctor`) and the compose topology (`aura-migrate`, `aura-home`, whatsapp sibling, Caddy); (Wave 3) goreleaser/ghcr publish (depends on the Dockerfile), `install.sh` (depends on a working compose), systemd autostart, backup wiring, README rewrite.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Aura agent runtime (loop, shell_exec, MCP, skills) | **Container (the box)** | — | The whole agent runs *inside* the fat container; the box is its computer (SPEC.md:10, D-02). |
| Host protection | **Container edge + mount set** | Optional gVisor `runsc` (D-03) | Structural confinement, never code guardrails (SPEC.md:166). No socket + invisible host fs come free with containerization. |
| Secret delivery | **Compose env / `env_file`** | `install.sh` secret-gen | Secrets reach the process only via env, never baked into an image layer (SPEC Req 2, `docker history` clean). |
| TLS + LAN auth | **Caddy reverse proxy** (front) | — | Only the user-facing surface (wizard, AG-UI) is LAN-exposed via Caddy; data/sidecars stay loopback (SPEC Req 11, D-11). |
| Persistence (config) | **`aura-home` named volume** → `AURA_CONFIG_DIR` | — | Operator-controlled mount; survives `down`/`up` (SPEC Req 2/8, D-13). |
| Persistence (data) | **Postgres + Neo4j named volumes** | host-visible `AURA_BACKUP_DIR` | Existing compose volumes; backups land on a host-visible dir (SPEC Req 16, D-14). |
| Container-needing MCP | **Compose sibling over streamable-HTTP** | — | Never punctures the box; mirrors `aura-agent-memory-mcp` (SPEC Req 3, compose.yaml:143-206). |
| MCP-in-image (`mcp-neo4j-cypher`) | **Inside the `aura` container** | — | The packaging-only win; `internal/knowledge/client.go` UNCHANGED (D-15). |
| Distribution image | **ghcr.io pinned per tag** | goreleaser host binary (retained) | Public, versioned, multi-arch (SPEC Req 14, D-13). |
| Boot lifecycle | **compose `restart` + systemd autostart unit** | — | Survives crashes (`unless-stopped`) and reboots (systemd) (SPEC Req 8/15). |

---

## Standard Stack

The "stack" here is the runtime base + the distribution toolchain — all **pinned and spike-verified**, not chosen anew.

### Core (image runtime — `docker/aura/Dockerfile`, D-06)
| Component | Version | Purpose | Why Standard |
|-----------|---------|---------|--------------|
| `golang` (build stage) | `1.26.4` | Compile the `aura` static binary | The project's Go line; the current root `Dockerfile:1` already uses `golang:1.26.4-alpine`. [VERIFIED: Dockerfile:1] |
| `debian:bookworm-slim` (runtime stage) | bookworm-slim | Fat runtime base | Same family as the markitdown sidecar (`python:3.12-slim`); spike 060 built it clean (73s cold). [VERIFIED: spike 060] |
| `python3` (Debian apt) | 3.11.2 | `mcp-neo4j-cypher` + uvx recipes | Resolved in spike 060 probe. PEP-668 externally-managed → `pip install --break-system-packages` (container IS the boundary). [VERIFIED: spike 060] |
| `node` (Debian apt) | v18.20.4 | `npx` recipes (mail) | apt v18 is fine for `npx`; pin NodeSource only if a recipe needs newer. [VERIFIED: spike 060] |
| `uv`/`uvx` | uv 0.11.21 | uvx recipes (calculator) + dep install | COPYed from `ghcr.io/astral-sh/uv` as a static binary (NOT pip-installed). [VERIFIED: spike 060 + sandbox-runtime.md] |
| `mcp-neo4j-cypher` | `==0.6.0` (exact pin) | Neo4j MCP subprocess | SPEC-pinned; spike 060 installed it clean with `--break-system-packages`. Confirmed **0.6.0 is current latest on PyPI**. [VERIFIED: PyPI + spike 060] |
| `git` + `curl` | apt | recipe git-fetch (calculator uses `git+https://`) + healthcheck/installer | calculator recipe = `uvx --from calculator-mcp-server@git+https://github.com/chetto1983/...` (catalog.go:53-54) needs git. [VERIFIED: catalog.go:53] |

### Supporting (distribution + lifecycle)
| Component | Version | Purpose | When to Use |
|-----------|---------|---------|-------------|
| `docker buildx` | current | Multi-arch (`linux/amd64`+`linux/arm64`) image build | SPEC multi-arch constraint (DGX = arm64). Wire into `.goreleaser.yaml` (D-13). [CITED: docker docs] |
| `caddy` (official image) | 2.x | TLS (internal CA / self-signed) + shared-token reverse proxy | Fronts wizard + AG-UI only (D-11). Caddy's `tls internal` issues a local-CA cert with zero config. [ASSUMED — planner verifies the exact directive] |
| `gVisor runsc` | latest release (e.g. `release-20260601.0`) | Optional appliance isolation tier | `compose.gvisor.yaml` `runtime: runsc`, native-Linux/arm64 only (D-03). runsc supports both amd64+arm64 (Linux 4.14.77+). [VERIFIED: spike 010 + gvisor.dev] |
| `goreleaser` v2 | current | Host-binary archives + the new ghcr multi-arch image | Existing `.goreleaser.yaml` already builds host binaries for linux/darwin/windows × amd64/arm64. [VERIFIED: .goreleaser.yaml] |
| `openssl rand` | system | Idempotent secret-gen in `install.sh` | `POSTGRES_PASSWORD`/`NEO4J_PASSWORD` + the Caddy access token; `.env` chmod 600, no regen if exists (D-12). |

### Alternatives Considered (settled by spikes — do NOT re-explore)
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Fat full-power box (D-02) | Distroless + `cap_drop`/`read_only`/non-root (audit `ec7fe2f6`) | **REJECTED — structurally breaks `shell_exec`/self-extend/MCP** (spike 059, exit 127). This is the jail D-01 reverts. |
| gVisor `runsc` appliance tier (D-03) | Kata/Firecracker microVM | Stronger isolation but needs KVM + more RAM/box; gVisor is KVM-free and arm64-capable (spike 061). Deferred. |
| gVisor `runsc` appliance tier (D-03) | Docker Sandboxes (`sbx`) | arm64 = macOS-only (DGX is arm64), wraps coding-agent CLIs not a compose stack, "crippling" perf, `sbx login` (spike 062). Deferred to dev/x86_64. |
| Compose sibling over streamable-HTTP (D-07) | Host Docker socket in the `aura` container | **REJECTED — punctures the box** (host-root escape). The lone load-bearing invariant (SPEC.md:167). |

**Installation (the image build, not a `npm install`):**
```bash
# Multi-arch build (the real docker/aura/Dockerfile)
docker buildx build --platform linux/amd64,linux/arm64 \
  -f docker/aura/Dockerfile -t ghcr.io/chetto1983/aura:<tag> --push .
```

**Version verification performed this session:**
- `mcp-neo4j-cypher==0.6.0` — **current latest on PyPI** (confirmed; one stale source said 0.5.2 but PyPI shows 0.6.0 wheels). [VERIFIED: PyPI]
- gVisor `runsc` — supports x86_64 **and** arm64, requires Linux 4.14.77+, registered via `/etc/docker/daemon.json` (`runsc install` command). [VERIFIED: gvisor.dev]
- `golang:1.26.4` — already in use at `Dockerfile:1`. [VERIFIED: codebase]

## Package Legitimacy Audit

This phase installs **no new Go module dependencies** (the Go surface — `aura doctor`, keyless boot, the in-container guard — uses only stdlib + existing internal packages). The "packages" are container base images + already-vendored recipe sources, all pinned and previously validated.

| Artifact | Registry | Source | Disposition |
|----------|----------|--------|-------------|
| `mcp-neo4j-cypher==0.6.0` | PyPI | neo4j-contrib/mcp-neo4j (official Neo4j Labs) | Approved — pinned, exact, spike-060-verified, current latest |
| `ghcr.io/astral-sh/uv` | ghcr | astral-sh/uv (official) | Approved — COPY static binary (sandbox-runtime.md established) |
| `debian:bookworm-slim` | Docker Hub | official | Approved |
| `golang:1.26.4` | Docker Hub | official | Approved — already in use |
| `caddy:2` | Docker Hub | official | Approved (planner confirms exact tag) |
| `calculator-mcp-server` (recipe, pre-bake) | git+https `chetto1983/calculator-mcp-server` | own fork (catalog.go:54) | Approved — already a shipped recipe |
| `github:martinzarfl/mail-mcp` (recipe, pre-bake) | npx github: | catalog.go:99 | Approved — already a shipped recipe |

*slopcheck was not run (no Python/npm package-name *discovery* happened — every artifact is an already-shipped, pinned recipe or an official base image). No `[SLOP]`/`[SUS]` risk: nothing was newly discovered from a non-authoritative source.*

---

## Architecture Patterns

### System Architecture Diagram (data flow through the box)

```
                         ┌──────────────────────────── HOST (Docker only) ────────────────────────────┐
   LAN user ──HTTPS+token─▶ Caddy (TLS internal-CA)  ──┐                                               │
                         │   ▲ fronts ONLY user-facing  │ loopback                                     │
                         │   │ (wizard :9081, AG-UI)    ▼                                               │
                         │  ┌──────────────── aura container (FAT, full-power, NO socket) ───────────┐ │
   operator ──shell/chat─┼─▶│  agent loop · shell_exec (full terminal) · skills self-extend         │ │
                         │  │  spawns: mcp-neo4j-cypher (stdio, in-image python) ───┐                │ │
                         │  │  guard: RuntimeDocker in-box → "deploy as sibling" err │                │ │
                         │  │  mounts: aura-home(rw,AURA_CONFIG_DIR) · runs(vol/tmpfs)│               │ │
                         │  │  limits: cpus/mem/pids (stability)  edge: host fs invisible            │ │
                         │  └────┬─────────────┬──────────────┬──────────────┬───────┴──────┐        │ │
                         │       │ pg DSN      │ bolt(via MCP)│ /v1/embeddings│ http/mcp     │ http   │ │
                         │   ┌───▼───┐    ┌────▼────┐    ┌────▼─────┐  ┌──────▼──────┐  ┌────▼──────┐ │ │
                         │   │postgres│   │ neo4j   │    │llama-embed│  │agent-memory │  │whatsapp   │ │ │
                         │   │(loopbk)│   │(loopbk) │    │ (loopbk)  │  │ -mcp(loopbk)│  │ sibling   │ │ │
                         │   └────────┘   └─────────┘    └───────────┘  └─────────────┘  │(loopbk)   │ │ │
                         │                                                               └───────────┘ │ │
                         │   aura-migrate (one-shot: db migrate && neo4j migrate, exit 0) ─gates──┘    │ │
                         │   backups → host-visible AURA_BACKUP_DIR (bind-mounted into pg/neo4j)        │
                         └─────────────────────────────────────────────────────────────────────────────┘
   Optional appliance tier: compose.gvisor.yaml adds `runtime: runsc` to the aura service (native-Linux/arm64 only)
```

### Recommended Project Structure (net-new + modified files)
```
docker/aura/Dockerfile          # NEW — fat multi-stage runtime (D-06); replaces root Dockerfile
docker/whatsapp/                # NEW — whatsmeow bridge sibling (D-07) [planner picks image basis]
Dockerfile                      # REMOVE (or repoint) — the distroless audit jail (D-01)
compose.yaml                    # MODIFY — de-harden aura svc; add aura-migrate, aura-home, caddy, whatsapp sibling
compose.gvisor.yaml             # NEW — optional `runtime: runsc` override (D-03)
caddy/Caddyfile                 # NEW — TLS internal-CA + shared-token front (D-11)
scripts/install.sh              # NEW — curl|sh installer (D-12)
scripts/aura.service            # NEW — systemd autostart unit (D-15/Req 15)
.goreleaser.yaml                # MODIFY — buildx multi-arch + ghcr push pinned per tag (D-13)
internal/mcp/manager/runtime.go # MODIFY — in-container docker-runtime guard (D-08)
internal/mcp/manager/catalog.go # MODIFY — whatsapp recipe → sibling endpoint, drop wsl.exe (D-07)
internal/config/config.go       # MODIFY — D-22 keyless boot serve-path relaxation (D-10)
internal/llm/config.go          # MODIFY (likely) — ErrMissingAPIKey deferral seam (D-10)
cmd/aura/doctor.go              # NEW — `aura doctor` aggregate (D-09)
cmd/aura/main.go                # MODIFY — wire `case "doctor"` (D-09)
README.md                       # MODIFY — end-user quick start (removes host pip install)
internal/knowledge/client.go    # UNCHANGED (D-15 — do not touch)
```

### Pattern 1: Cache-stable multi-stage layer ordering (D-06)
**What:** Heavy runtime layers (apt, uv COPY, pip mcp-neo4j-cypher, recipe pre-bake) sit BEFORE the tiny final `COPY --from=build /out/aura`. A code change re-runs only the last layer.
**When to use:** the `docker/aura/Dockerfile`. Mandatory — the SPEC warns cold multi-arch rebuild ~45–60 min.
**Evidence:** spike 060 measured cold 73s → warm **1.4s, 3 CACHED layers** for the single-arch base. The 45–60 min figure is the *multi-arch* (buildx ~2×) + recipe-bake cost, not the base.
```dockerfile
# Source: spike 060 README + docker/agent-memory/Dockerfile template (pinned-pip sidecar)
FROM golang:1.26.4 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aura ./cmd/aura

FROM debian:bookworm-slim
# --- HEAVY, cache-stable layers (never invalidated by a code change) ---
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 python3-pip nodejs npm git curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/
RUN pip install --no-cache-dir --break-system-packages mcp-neo4j-cypher==0.6.0
# Pre-bake trusted recipes for the offline appliance (SPEC Req 7) as their own warm layers
RUN uvx --from "calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git" calculator-mcp-server --help >/dev/null 2>&1 || true
RUN npx -y github:martinzarfl/mail-mcp --help >/dev/null 2>&1 || true
ENV AURA_IN_CONTAINER=1   # D-08 marker for the in-container docker-runtime guard
# --- TINY final layer ---
COPY --from=build /out/aura /usr/local/bin/aura
ENTRYPOINT ["aura"]
CMD ["serve"]
```
> NOTE: this is an *illustrative shape* derived from the spikes, not a verified build. The planner/executor must build it on both arches. The pre-bake `--help` warm-up may need a recipe-specific invocation to actually populate the uv/npx caches; the executor verifies with the egress-blocked acceptance probe (Req 7).

### Pattern 2: Compose sibling over streamable-HTTP (D-07, Req 3/4)
**What:** Any MCP that needs its own container becomes a compose sibling published loopback; Aura mounts it over streamable-HTTP — never spawning a sub-container.
**Evidence:** `aura-agent-memory-mcp` already does exactly this (compose.yaml:143-206 build-from-`docker/agent-memory`, published `127.0.0.1:8091`, mounted via URL). `RuntimeServers` already excludes streamable-HTTP servers from launch (`runtime.go:30-33`, `isStreamableHTTPServer`).
**When to use:** the whatsapp whatsmeow bridge (D-07) and any future container-needing MCP.

### Pattern 3: Fail-soft degrade for a downed sibling (D-07)
**What:** When the whatsapp sibling is down, Aura boots without crashing (no boot-time hard dependency on an optional MCP).
**Evidence:** the established "fail-soft MCP boot" pattern (memory `mcp_sidecar_lifecycle`; `RunnableManagedServers` skips disabled/blocked servers, runtime.go:49-78). The streamable-HTTP recipe with an unreachable URL must not fail the boot.

### Pattern 4: Empty-config fail-soft at call time (D-10, mirror SEARXNG_URL)
**What:** `aura serve` boots with an empty LLM key; the *agent call* fail-closes with a structured `llm_not_configured`, never a boot panic.
**Evidence:** the exact pattern exists for `SEARXNG_URL` — `config.go:268-270` sets it empty-by-default (D-05) and it surfaces as `web_search_unavailable{searxng_not_configured}` at call time, NOT a boot error (config_test.go:445-455). D-10 applies the same shape to the LLM key on the serve path only.

### Anti-Patterns to Avoid
- **Re-applying capability stripping** (`cap_drop: ALL`, `read_only: true`, forced non-root, seccomp tightening) on the `aura` service: spike 059 proved it breaks parity (exit 127). This is the audit jail D-01 explicitly reverts. **Do not "harden" the box.**
- **Mounting the host Docker socket** into the `aura` container to launch container-MCP: the lone forbidden invariant (SPEC.md:167). Use siblings (Pattern 2).
- **`docker compose ps` inside `aura doctor`**: there is no socket in-box (D-09). Use direct service probes.
- **Invalidating the heavy runtime layer on every code change**: put `COPY aura` LAST (Pattern 1).
- **Baking any secret into an image layer**: `docker history` must show zero secret values (Req 2). Secrets only via compose env / `env_file`.
- **Editing `internal/knowledge/client.go`**: it is UNCHANGED (D-15). The MCP-in-image win is purely that python now lives in the image PATH.

---

## Per-Requirement Implementation HOW

### A. The boundary

#### Req 1 — Full environment, not a jail; the box just stays a box (D-01/D-02)
**HOW:** De-harden the existing `aura` compose service. The current `compose.yaml:10-51` IS the audit jail: remove `user: "65532:65532"` (line 17), `read_only: true` (line 18), `cap_drop: ALL` (lines 19-20). Keep `mem_limit`/`cpus` (lines 23-24) and ADD `pids_limit`. Keep `security_opt: no-new-privileges` only if it does not block self-extension (spike 059 ran a plain fat box with NO security_opt — recommend dropping it too for true parity; planner confirms). Replace the writable-path strategy: the `read_only: true` + tmpfs split (lines 18, 44-45) becomes a plain writable rootfs.
**Files:** `compose.yaml` (the `aura` service block).
**Proven by:** spike 059 (parity probes all green on a plain fat box; the limits hold via cgroup-v2 `mem.max`/`pids.max`/`cpu.max`).
**Note:** SPEC Req 1 and the Boundaries §"In scope" bullet (SPEC.md:137) still *list* `cap_drop: ALL` / read-only-rootfs as in-scope — this is the **internal contradiction the D-04 amendment resolves** (the Requirements body + Constraints + Revision R6 all say the opposite; the spikes are authoritative). Flag prominently.

#### Req 2 — Host-controlled mounts; secrets via env (D-13)
**HOW:** Replace the current volume set (`aura-runs`/`aura-skills`/`aura-exported-skills`, compose.yaml:40-43) with a single `aura-home` named volume → `AURA_CONFIG_DIR` (holding `llm.json`, `mcp/servers.json`, `Agent.md`, skills, journals) rw, plus the run-artifact dir as a volume or tmpfs. No host bind of the repo, no host root, no docker socket. Secrets stay in compose `environment`/`env_file` (already the case — `OPENROUTER_API_KEY`/`POSTGRES_PASSWORD`/`NEO4J_PASSWORD` are env-injected, compose.yaml:30-37).
**Files:** `compose.yaml` volumes + the `aura` env block; set `AURA_CONFIG_DIR`.
**Validation:** `docker history <image>` shows no secret (secrets never reach a build layer); `aura-home` survives `down`/`up`.

#### Req 3 — No in-container Docker; container-MCP are siblings (D-08)
**HOW:** In `internal/mcp/manager/runtime.go`, the dispatch `RuntimeLaunchConfig` (runtime.go:83-99) switches on `runtimeKind`. Add an in-container check: when `os.Getenv("AURA_IN_CONTAINER") == "1"` (the marker baked at image build, Pattern 1) AND `runtimeKind(server)` is `RuntimeDocker`/`RuntimeDockerGateway`, return a clear actionable error *before* `dockerRuntimeConfig`/`dockerGatewayRuntimeConfig` build a `docker run` command line (runtime.go:101-147) — not the confusing `exec: "docker": not found`. Sibling MCP over streamable-HTTP already mount + list tools (runtime.go:30-33).
**Files:** `internal/mcp/manager/runtime.go` (the `RuntimeDocker`/`RuntimeDockerGateway` cases at runtime.go:89-92).
**Error text:** "docker runtime unavailable inside the container — deploy as a compose sibling and mount via URL" (SPEC Req 3 acceptance literal).

#### Req 4 — whatsapp via sibling bridge, not wsl.exe (D-07)
**HOW:** In `internal/mcp/manager/catalog.go`, the whatsapp recipe (catalog.go:113-129) currently spawns `Command: "wsl.exe"` with `Args: ["-e","bash","-lc","cd ~/whatsapp-mcp/... && uv run main.py"]`. Replace it with a streamable-HTTP recipe pointing at the sibling endpoint (mirror the `memory` recipe at catalog.go:130-139: `Type: StreamableHTTP`, a URL, no launch Command). Add a `whatsapp` compose sibling (whatsmeow bridge, published loopback). Update the `Summary` (catalog.go:115 still says "whatsmeow bridge in WSL, stdio via wsl.exe").
**Files:** `internal/mcp/manager/catalog.go` (whatsapp entry); `compose.yaml` (new sibling); a new `docker/whatsapp/` build context.
**PLANNER-CHOICE FLAG:** the concrete bridge image. CONTEXT D-07 basis = the existing chetto1983 whatsapp-mcp fork + `.planning/spikes/002-whatsapp-mcp-pairing/bridge-patch.diff`. **Surface for a quick user confirm** if the image choice is non-trivial.

### B. The image

#### Req 5 — Fat multi-arch image (D-06)
**HOW:** `docker/aura/Dockerfile` per Pattern 1 (golang:1.26.4 builder → debian:bookworm-slim + python3 + uv/uvx + node/npx + pinned mcp-neo4j-cypher==0.6.0 + git + curl → `COPY aura` last). Build for `linux/amd64`+`linux/arm64` via buildx. Template the runtime stage on `docker/agent-memory/Dockerfile` + `docker/markitdown/Dockerfile` (the canonical in-repo pinned-pip sidecars).
**Files:** `docker/aura/Dockerfile` (NEW); remove/repoint root `Dockerfile` (D-01).
**Proven by:** spike 060 (all four binaries resolve; 875 MB single-arch runtime; cache-stable).

#### Req 6 — MCP subprocess in-image, host clean; client.go UNCHANGED (D-15)
**HOW:** No code change. The same stdio spawn in `internal/knowledge/client.go:51-73` (`exec.Command(cfg.MCPBinary, args...)`) now resolves `mcp-neo4j-cypher` from the *in-image* PATH (where pip installed it) instead of the host. `cfg.MCPBinary` is operator-config (`AURA_MCP_NEO4J_CYPHER_BIN`); inside the image it defaults to the pip-installed `/usr/local/bin/mcp-neo4j-cypher`.
**Files:** NONE in `client.go` (constraint). Only the image (Req 5) makes this work.
**Validation:** in-container Neo4j round-trip returns the server version; on the host `command -v mcp-neo4j-cypher` returns non-zero.

#### Req 7 — Pre-baked recipes (offline appliance)
**HOW:** Bake the trusted recipe packages at build time so an air-gapped first boot needs zero network. Targets (from catalog.go): `calculator` = `uvx --from calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git` (catalog.go:51-58, needs git + uv cache warm); `mail` = `npx -y github:martinzarfl/mail-mcp` (catalog.go:96-99, needs npx cache warm). Add Dockerfile layers that pre-warm the uv + npx caches (Pattern 1).
**Files:** `docker/aura/Dockerfile` (recipe pre-bake layers).
**Landmine:** a bare `--help` warm-up may not fully populate the cache the way a real `tools/list` invocation does; the executor must verify with the egress-blocked acceptance probe and adjust the bake invocation if needed. **MEDIUM confidence on the exact pre-bake command** — the *approach* is settled (D-06), the precise warm-up invocation is an executor detail.

### C. Compose topology & lifecycle

#### Req 8 — Aura service + one-shot migrate + persistent home + restart (D-13)
**HOW:** Add the de-hardened `aura` service (Req 1/2) with `depends_on` healthchecks for pg/neo4j/embed (extend the current `depends_on: postgres` at compose.yaml:25-27 to include neo4j + aura-llama-embed, which already have healthchecks). Add a one-shot `aura-migrate` service running `aura db migrate && aura neo4j migrate` (the subcommands exist: `runDB`→`case "migrate"` db.go:30, `runNeo4j` exists per main.go:58) that exits 0; gate the `aura` service with `depends_on: { aura-migrate: { condition: service_completed_successfully } }`. The `aura-home` named volume + `restart: unless-stopped` (already set, compose.yaml:16) across long-lived services.
**Files:** `compose.yaml` (`aura` + new `aura-migrate` + volumes).
**Note:** `aura db migrate` uses `config.LoadDB()` (db.go:26) so it needs NO LLM key — the keyless-boot relaxation (Req 9) is independent and correct; migrate already works keyless.

#### Req 9 — D-22 keyless boot, fail-closed (D-10)
**HOW:** Currently `aura serve`/`chat` boot through `bootChatEnv` → `config.Load()` (chat.go:141-142), and `config.Load` composes `llm.Load()` which returns `ErrMissingAPIKey` on an empty key (config.go:173-176 → llm/config.go:208-209). `bootChat` translates that into `os.Exit(1)` (chat.go:125-128). For the serve path: relax so `aura serve` boots with an empty key (do NOT fail-fast), and an agent call without a key returns structured `{"error":"llm_not_configured","hint":...}`. Mirror the `SEARXNG_URL` fail-soft (config.go:268-270; config_test.go:445-455). `LoadDB()`/`db migrate` stay UNCHANGED (config.go:200-207).
**Files:** `internal/config/config.go` (a serve-path load variant or a flag that defers the key check); `internal/llm/config.go` (defer `ErrMissingAPIKey` from `Load`); `cmd/aura/serve.go`/`chat.go` (serve boots keyless; the agent-call site returns `llm_not_configured`).
**Design seam:** the cleanest shape is a `config.LoadServe()` (or a `Load` option) that returns a Config with an empty `LLM.APIKey` instead of erroring, and a call-time guard in the agent run path that emits `llm_not_configured`. The planner picks the exact seam; preserve the `chat` (interactive) fail-fast-with-friendly-line UX if desired, but `serve` MUST boot keyless.
**Validation:** `docker compose up` with no `OPENROUTER_API_KEY` reaches healthy; an agent call returns `llm_not_configured`; after the key is set, the same call answers.

#### Req 10 — `aura doctor` aggregate health check (D-09)
**HOW:** New `cmd/aura/doctor.go` with `runDoctor(args)`; wire `case "doctor": runDoctor(os.Args[2:])` into main.go:43-91 (and add to `usage()` main.go:95). Checks (one pass/fail line each, non-zero exit on any hard failure): (1) Postgres ping; (2) Neo4j round-trip via `mcp-neo4j-cypher` (reuse `knowledge.Open` + a `RETURN 1`); (3) embed dimension match (`AURA_EMBED_DIMENSIONS` vs the sidecar `/v1/embeddings`); (4) `mcp-neo4j-cypher` spawn; (5) LLM-key configured-or-not. **Must NOT use `docker compose ps`** (no socket in-box, D-09). Reuse the per-check + exit-code shape of `runWebDoctor` (web.go:42-65, distinct exit codes 64/70/0) and `mcpDoctorAll` (mcp_status.go:53-89).
**Files:** `cmd/aura/doctor.go` (NEW); `cmd/aura/main.go` (wire + usage); reuse `cmd/aura/exit_codes.go` (exitUnreachable=70, exitInfra=71, exitUsage=64).
**Validation:** healthy stack → exit 0 all-green; embed sidecar stopped → non-zero naming the failed check.

#### Req 11 — Caddy TLS + shared-token; data loopback (D-11)
**HOW:** Add a bundled `caddy` compose service terminating TLS (internal CA / self-signed via Caddy's `tls internal`) and enforcing a generated shared access token, fronting ONLY the user-facing surface: the setup wizard (`setupSrv` :9081, serve.go:65-67) and the AG-UI gateway (`httpSrv`, serve.go:60-61; when Phase 12 exists). Everything else (postgres 5432, neo4j 7687/7474, embed 8081, agent-memory-mcp 8091, multimodal sidecars, whatsapp bridge) stays loopback-only — they already bind `127.0.0.1` (compose.yaml:62,90-93,128,200,260,275,309,332). The `aura` AG-UI currently binds `127.0.0.1:9080` (compose.yaml:39) — Caddy fronts that for LAN.
**Files:** `caddy/Caddyfile` (NEW); `compose.yaml` (caddy service, the only LAN-exposed publish).
**PLANNER-CHOICE FLAG:** token enforcement mechanism (Caddy `forward_auth` vs a header/`@token` matcher) = planner choice (D-11). The access token generation lives in `install.sh` (D-12).
**Validation:** from another LAN host the wizard is HTTPS-reachable only with the token (no token → 401/403); a LAN connection to any data/sidecar port is refused.

### D. Distribution

#### Req 12 — curl|sh installer + HW preflight + idempotent secrets (D-12)
**HOW:** New `scripts/install.sh` (Linux + macOS), hosted in-repo raw URL: HW preflight (RAM/disk/CPU — deploy target mini-PC 16-core/32GB, **16 GB min**; warn below comfortable, abort below a hard floor) → Docker check → secret-gen (`openssl rand` for `POSTGRES_PASSWORD`/`NEO4J_PASSWORD` + the Caddy access token; `.env` chmod 600; **no** regen if `.env` exists) → fetch compose+`.env` → `docker compose up` → print wizard URL + access token + next steps.
**Files:** `scripts/install.sh` (NEW). Does not exist yet (verified).
**PLANNER-CHOICE FLAG:** exact HW thresholds (warn vs abort floors) = planner choice (D-12).
**Validation:** `curl -fsSL <url>/install.sh | sh` on a clean Linux host (no Python/Node/pip) → healthy stack + printed summary; `which python3 pip node` shows none; re-run leaves `.env` byte-identical; under-spec HW warns/aborts first.

#### Req 13 — Docker auto-install + guided fallback; Windows door (D-12)
**HOW:** In `install.sh`, if Docker is absent try the OS package path (`get.docker.com` on Linux, `brew --cask docker` on macOS); on failure print clear instructions + the official link and exit non-zero (no silent hang). Windows = a documented Docker-Desktop path in the README: install Docker Desktop, run a documented PowerShell secret-gen one-liner, then `docker compose up` against the shipped `compose.yaml`. NO bespoke Windows installer.
**Files:** `scripts/install.sh` (Docker handling); `README.md` (Windows PowerShell door).
**Validation:** Docker-less Linux → auto-install via `get.docker.com`; forced-failure → guided instructions + non-zero exit; the Windows steps bring the stack up and the PowerShell one-liner produces a `.env` with generated DB secrets.

#### Req 14 — Public ghcr image pinned per tag; host binary retained (D-13)
**HOW:** Extend `.goreleaser.yaml` to build + push the multi-arch image to `ghcr.io/chetto1983/aura:<tag>` (the org is `chetto1983`, .goreleaser.yaml:66) pinned per release tag, never `latest`. Keep the existing host-binary `builds`/`archives` (.goreleaser.yaml:12-44). `compose.yaml` references the pinned tag via `${AURA_IMAGE}` (compose.yaml:13 already parameterizes this; the appliance pins a real `ghcr.io/...:<tag>` instead of `aura:local`).
**Files:** `.goreleaser.yaml` (add a `dockers_v2`/buildx multi-arch block + ghcr push); `compose.yaml` (`AURA_IMAGE` default → pinned tag for the appliance; dev keeps `pull_policy: never` local build).
**Note:** goreleaser's Docker support historically built per-arch images + a manifest; the planner verifies the current goreleaser v2 multi-arch image directive. [ASSUMED — exact goreleaser directive needs verification at plan time]
**Validation:** a tagged release publishes a public multi-arch `ghcr.io/chetto1983/aura:<tag>` (amd64+arm64) AND host-binary archives; compose references the pinned tag.

#### Req 15 — Boot persistence + appliance autostart (D-13)
**HOW:** Ship a systemd unit (`scripts/aura.service`) that runs `docker compose up -d` on power-on (`WantedBy=multi-user.target`, `ExecStart=/usr/bin/docker compose -f /opt/aura/compose.yaml up -d`). `restart: unless-stopped` (compose.yaml:16) already covers crashes (Req 8).
**Files:** `scripts/aura.service` (NEW); install.sh optionally enables it on the appliance path.
**Validation:** after an appliance reboot the full stack is healthy with no human action.

#### Req 16 — Backup wiring + documented restore + documented update (D-14)
**HOW:** Enable the Phase-10 scheduled backups (the cron `backup_postgres`/`backup_neo4j` handlers, backup.go:36-69) by default to a host-visible `AURA_BACKUP_DIR`; document the one-command restore drill (`scripts/restore_drill.sh` exists); document `docker compose pull && docker compose up -d` (migrations via `aura-migrate`, volumes persist).
**Files:** `compose.yaml` (the `AURA_BACKUP_DIR` bind-mount into pg/neo4j containers + the `aura` env to schedule backups); `README.md` (restore + update docs).
**⚠️ LANDMINE (see Landmines §):** `BackupHandler.Run` shells out to `docker exec` (backup.go:41-53) — but the `aura` container has NO Docker socket (Req 1/3). The current backup design assumes the *host-run* Aura can reach the Docker daemon. Inside the box this `docker exec` will FAIL. **This is a genuine cross-requirement conflict the planner must resolve** (options below). MEDIUM confidence this needs a real design decision, not just wiring.

---

## Dependency / Wave Ordering Guidance

**Pre-execution gate (BLOCKING):** the **D-04 PRD/SPEC amendment commit lands FIRST**, before any code (PRD-first principle). See the dedicated gate section.

**Wave 1 — the box foundation (must land before the rest):**
- `docker/aura/Dockerfile` (Req 5, D-06) + remove/repoint root `Dockerfile` (D-01).
- De-harden the compose `aura` service (Req 1/2, D-01/D-02): the `aura` service must reference the new image and drop the jail directives.
- *Rationale:* the compose `aura` service and every downstream (installer, goreleaser, doctor live tests) depend on a buildable, runnable fat image.

**Wave 2 — parallelizable (independent of each other, depend only on Wave 1):**
- **Go branch (parallel):** D-08 in-container guard (`runtime.go`), D-10 keyless boot (`config.go`/`llm/config.go`/`serve.go`), D-09 `aura doctor` (`doctor.go`/`main.go`). These three are mutually independent Go edits — different files, no shared state. The `AURA_IN_CONTAINER` marker is set by the Wave-1 Dockerfile, so D-08's *test* depends on the image but the *code* does not.
- **Compose-topology branch (parallel):** `aura-migrate` one-shot + `aura-home` volume (Req 8), whatsapp sibling + catalog.go recipe rewrite (Req 4, D-07), Caddy front + Caddyfile (Req 11, D-11), `compose.gvisor.yaml` override (D-03).

**Wave 3 — depends on a working compose + image:**
- `.goreleaser.yaml` ghcr multi-arch push (Req 14) — **depends on** the Dockerfile (Wave 1).
- `scripts/install.sh` (Req 12/13) — **depends on** a working compose stack (Wave 1+2).
- `scripts/aura.service` systemd autostart (Req 15) — depends on the installer/compose.
- Backup wiring (Req 16, D-14) — depends on the compose `aura` env + the resolved `docker exec` landmine.
- README rewrite (end-user quick start) — depends on everything above being real.

**Phase-10 pre-baked recipes (Req 7)** ride on the Wave-1 Dockerfile (extra warm-cache layers); validation (egress-blocked) is a Wave-1/Wave-3 acceptance probe.

---

## Landmines & Gotchas

1. **The backup `docker exec` paradox (Req 16 vs Req 1/3) — HIGHEST PRIORITY.** `BackupHandler.Run` (backup.go:41-53) executes `docker exec aura-postgres pg_dump ...` and `docker exec aura-neo4j neo4j-admin dump ...`. The `aura` container has **no Docker socket** (the lone invariant). Inside the box this fails. The backup.go comment itself notes "docker is LookPath-gated and the socket is NEVER mounted (T-10-15)" — written for a *host-run* Aura. **Planner must resolve:** options (a) run the scheduled backup as a *separate compose service* with the socket (a deliberate, scoped exception, not the `aura` box) ; (b) replace `docker exec` with a network `pg_dump`/`cypher-shell` from inside the `aura` container (no socket needed — pg_dump over the DSN, neo4j-admin via a sidecar); (c) run backups from a host-side cron/systemd timer invoking `docker compose exec` (like `restore_drill.sh` already does, restore_drill.sh:41). Option (b) is the cleanest fit for "the box stays a box." **Surface this as an explicit plan decision.**

2. **Cache-stable layer order (Req 5).** `COPY aura` MUST be the last layer; the apt/uv/pip/recipe layers before it. Multi-arch cold rebuild ~45–60 min — a layer-order mistake re-runs the heavy layer on every code change. (spike 060: warm 1.4s with correct order.)

3. **The gVisor `daemon.json` prerequisite (D-03).** `compose.gvisor.yaml`'s `runtime: runsc` requires the runtime to be **pre-registered in `/etc/docker/daemon.json`** on the host (via `runsc install`) — compose cannot install it. The appliance install path (native-Linux/arm64) must register runsc before applying the override; Docker Desktop dev CANNOT (no runtime slot — spikes 010/059). The installer's HW/OS detection should only attempt the gVisor tier on native-Linux. [VERIFIED: gvisor.dev — runtime via daemon.json]

4. **`docker history` secret leakage (Req 2).** Any `ARG`/`ENV`/`RUN echo` of a secret persists in the image history. Secrets must reach the process ONLY via compose env/`env_file` at run time. The Dockerfile must carry zero secret values. The `AURA_IN_CONTAINER=1` marker (Pattern 1) is the only ENV baked, and it is not a secret.

5. **The `AURA_IN_CONTAINER=1` marker is load-bearing (D-08).** It is the cleanest in-box signal for the docker-runtime guard. Alternative signals (`/.dockerenv` presence) are less reliable (`/.dockerenv` exists in any container, including the dev host's). Bake the marker explicitly in the Dockerfile and gate the guard on it. The marker is NOT currently in the codebase (verified — only referenced in CONTEXT.md).

6. **Multi-arch buildx (DGX = arm64).** The image MUST build for both `linux/amd64` and `linux/arm64`. The acceptance probe (`command -v python3 && node && uvx && mcp-neo4j-cypher`) runs on both arches. apt python/node resolve on both; the uv binary COPY from `ghcr.io/astral-sh/uv` is multi-arch. Pip `mcp-neo4j-cypher==0.6.0` is a pure-Python wheel (arch-independent). No CGO in the Go build (CGO_ENABLED=0, Dockerfile:10) so the binary cross-compiles cleanly.

7. **Fail-soft whatsapp degrade (Req 4, D-07).** A streamable-HTTP whatsapp recipe with an unreachable sibling URL must NOT fail the boot. The `RunnableManagedServers` path already skips disabled/blocked servers (runtime.go:49-78); the planner must confirm an unreachable HTTP recipe degrades fail-soft rather than erroring at mount.

8. **SPEC internal contradiction on hardening (Req 1).** SPEC Req 1 *target* + the Boundaries §"In scope" first bullet (SPEC.md:137) still LIST `cap_drop: ALL`/read-only-rootfs/non-root as in-scope, while the SPEC Requirement body, Constraints (SPEC.md:166), Out-of-scope (SPEC.md:152), and Revision R6 (SPEC.md:229) all DECLINE them. The spikes (059/061) + CONTEXT D-01 are authoritative: **de-harden.** The D-04 amendment corrects the stale lines. Do not let a plan-checker "restore" the hardening citing SPEC.md:137.

9. **Idempotent secret-gen (Req 12).** `install.sh` must NOT regenerate `.env` if it exists (re-run → byte-identical secrets). Guard the `openssl rand` block on `[ ! -f .env ]`.

10. **Loopback vs LAN split is already 90% done.** Every sidecar already binds `127.0.0.1` (compose.yaml). The ONLY new LAN-exposed publish is Caddy. Do not accidentally publish the `aura` AG-UI port (`127.0.0.1:9080`, compose.yaml:39) on `0.0.0.0` — Caddy fronts it.

11. **`neo4j migrate` subcommand exists.** `aura-migrate` runs `aura db migrate && aura neo4j migrate`. `runDB`→`case "migrate"` (db.go:30) and `runNeo4j` (main.go:58) both exist. `db migrate` uses `LoadDB()` (no LLM key, db.go:26) — so `aura-migrate` boots keyless independently of D-10.

12. **The 17-RESEARCH stale-data warnings.** `.planning/REQUIREMENTS.md` CAP-01/CAP-02 (lines 32-33,117-118) still describe the **removed** `sandbox-agent` as the execution surface — these predate the sandbox removal (`c9e1124e`). They are NOT Phase-17 requirements (OPS-01 line 58/136 is current). The SPEC Background (SPEC.md:16) "no aura Dockerfile / no compose aura service" is stale (the audit added both). Treat SPEC.md Requirements + CONTEXT + spikes 059-062 as authoritative.

---

## Planner-Choice Flags (confirm with user at plan-review)

| Flag | Decision needed | CONTEXT basis | Risk if wrong |
|------|-----------------|---------------|---------------|
| **D-07 whatsapp bridge image** | The concrete whatsmeow bridge sibling image / build context | chetto1983 whatsapp-mcp fork + spike 002 `bridge-patch.diff` | A wrong image = no whatsapp; fail-soft means non-fatal, low blast radius |
| **D-11 Caddy token mechanism** | `forward_auth` vs header/`@token` matcher for the shared access token | SPEC-default; planner choice | Wrong mechanism = LAN exposure without auth (Req 11 acceptance fails) — verify the 401-without-token probe |
| **D-12 HW thresholds** | Exact warn/abort floors (the deploy target is 16-core/32GB, 16 GB min) | SPEC-default; planner choice | Too-strict abort = won't install on valid HW; too-loose = doomed boot |
| **Req 16 backup execution model** | Resolve the `docker exec` paradox (option a/b/c) | NOT in CONTEXT — a research-surfaced cross-requirement conflict | A box with a socket-less backup that silently no-ops = data loss risk. **Highest-priority confirm.** |
| **Req 14 goreleaser directive** | The exact goreleaser v2 multi-arch image push config | SPEC-default | Build-time only; verify with a snapshot build |

---

## D-04 Pre-Execution Amendment Gate (BLOCKING)

**A PRD/SPEC amendment commit MUST land before `/gsd-execute-phase 17`** (PRD-first principle; CONTEXT D-04). The amendment must:

1. **ADD the optional gVisor appliance tier** (`compose.gvisor.yaml`, `runtime: runsc`, native-Linux/arm64 only, OFF on Docker-Desktop dev). Frame it explicitly as a **transparent isolation boundary, NOT capability-stripping** — so it is consistent with the SPEC's "no internal lockdown" (the workload keeps full caps + full Linux; only the host gets thicker walls). gVisor is NOT in the locked SPEC; this is the load-bearing reason the amendment is required.

2. **RECORD the `ec7fe2f6` revert** (D-01) and **correct the stale SPEC Background** (SPEC.md:16 still claims "no Dockerfile for the `aura` app" + "compose.yaml has no `aura` service" — the audit added both 2026-06-12) AND **the stale SPEC Req 1/Boundaries hardening lines** (SPEC.md:137 lists `cap_drop: ALL`/read-only-rootfs as in-scope, contradicting the de-harden decision — Landmine #8).

3. **NOTE Docker Sandboxes (sbx) as evaluated-and-deferred** (D-05) + the docker-in-box-without-host-socket future capability that could lift SPEC Req 3 (Deferred).

This amendment is a documentation/PRD commit — it lands BEFORE the first code commit. The planner should make the amendment the explicit first task/wave-0 of the plan.

---

## Runtime State Inventory

> This is a packaging/refactor phase that *removes host coupling* and *reverts an audit commit* — so a runtime-state sweep applies.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| **Stored data** | Postgres `aura.*` + Neo4j graph live in named volumes (`aura-postgres`, `aura-neo4j`, compose.yaml:344-345). The `aura-home` volume is NEW (replaces `aura-runs`/`aura-skills`/`aura-exported-skills`, compose.yaml:40-43). | Data migration: NONE for pg/neo4j (volumes unchanged). The skills/runs volume rename to `aura-home`/`AURA_CONFIG_DIR` is a config-path change — existing dev `aura-skills`/`aura-runs` volumes are not auto-migrated. Document the dev→appliance volume mapping; the appliance is greenfield so no migration there. |
| **Live service config** | The whatsapp recipe currently points at a host WSL path (`~/whatsapp-mcp/...` via `wsl.exe`, catalog.go:120-123). No external SaaS dashboard config carries the old string. | Code edit only (catalog.go recipe → sibling URL). The host WSL whatsapp-mcp install becomes orphaned (no longer used) — note in README that the WSL whatsapp path is retired. |
| **OS-registered state** | NEW systemd unit (`scripts/aura.service`, Req 15) registers `docker compose up -d` at boot on the appliance. gVisor `runsc` registers in `/etc/docker/daemon.json` (Landmine #3) on the native-Linux appliance. | Registration is part of the appliance install (install.sh / appliance pre-seed). No pre-existing OS state to migrate. |
| **Secrets/env vars** | `OPENROUTER_API_KEY`/`POSTGRES_PASSWORD`/`NEO4J_PASSWORD`/`TELEGRAM_BOT_TOKEN` are env-injected (compose.yaml:30-37,59,78,158). NEW: a generated Caddy shared access token. `AURA_CONFIG_DIR` (new), `AURA_BACKUP_DIR` (existing, backup.go:181), `AURA_IN_CONTAINER` (new marker). | `install.sh` generates DB passwords + the Caddy token idempotently (no regen if `.env` exists). The new env vars are additive — document in the env catalog. No key rename breaks existing readers. |
| **Build artifacts / installed packages** | The host `pip install mcp-neo4j-cypher==0.6.0` (the undocumented host dependency, SPEC.md:20) becomes orphaned — it's now in the image. The root `Dockerfile` (distroless) is removed/repointed. | After the phase, the host no longer needs the pip-installed `mcp-neo4j-cypher` (Req 6 acceptance asserts `command -v mcp-neo4j-cypher` returns non-zero on the host). Document its removal in the README rewrite. |

**The canonical question — after every file is updated, what runtime systems still carry the old shape?** The host's WSL whatsapp-mcp install and the host pip `mcp-neo4j-cypher` become *orphaned but harmless* (no longer invoked); the README rewrite documents that the host can be cleaned. No live SaaS dashboard, no OS task description, and no secret-key *value* carries a renamed string — the only renames are env-var *additions* and the `aura-skills`→`aura-home` volume consolidation (dev-only; appliance is greenfield).

---

## Validation Architecture

> `workflow.nyquist_validation: true` (verified in `.planning/config.json:19`) — this section is MANDATORY.

The phase is **mostly Dockerfile/compose/shell-script/packaging** with a **small Go surface** (`aura doctor`, the D-10 config relaxation, the D-08 in-container guard). The validation strategy splits accordingly.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table-driven), per CLAUDE.md skills (`golang-testing`) |
| Config file | none (Go convention); integration tiers gated by build tags |
| Quick run command | `go test ./internal/mcp/manager/ ./internal/config/ ./internal/llm/ ./cmd/aura/` |
| Race run | `go test -race ./internal/mcp/manager/ ./internal/config/ ./cmd/aura/` |
| Full suite command | `make quality-full` (vet + build + lint + race + coverage gate, stack up) |
| Container acceptance | live Docker probes (no Go framework) — `docker run`/`docker compose` assertions in a smoke script |

### Go-surface validation (unit + integration, ≥85% owned-surface, no-skip-as-green)
| Target | Test type | Command | Coverage note |
|--------|-----------|---------|---------------|
| D-08 in-container guard (`runtime.go`) | unit (table-driven, `t.Setenv("AURA_IN_CONTAINER","1")`) | `go test ./internal/mcp/manager/` | Assert `RuntimeLaunchConfig` returns the "deploy as a compose sibling" error for `RuntimeDocker`/`RuntimeDockerGateway` when the marker is set, and the normal docker command line when unset. ≥85%. |
| D-10 keyless boot (`config.go`/`llm/config.go`) | unit | `go test ./internal/config/ ./internal/llm/` | Assert the serve-path load returns a Config with empty `LLM.APIKey` (no `ErrMissingAPIKey`) while `LoadDB()`/`Load()` (chat path) behavior is preserved; mirror the existing `TestSearxngURL*` (config_test.go:445). The agent-call site returns `llm_not_configured`. ≥85%. |
| D-09 `aura doctor` (`doctor.go`) | unit (per-check, seamed probes) + integration (live stack) | `go test ./cmd/aura/` + a `db_integration neo4j_integration`-tagged live test | Unit: each check's pass/fail + exit-code mapping with injected fakes (mirror web.go exit codes). Integration: real PG ping + Neo4j round-trip + embed dimension. The integration leg `t.Fatal`s under `$CI` when env unset (no-skip-as-green). ≥85% on the new file. |
| D-07 catalog whatsapp recipe (`catalog.go`) | unit | `go test ./internal/mcp/manager/` | Assert the whatsapp recipe resolves to a streamable-HTTP URL (no `wsl.exe` Command) and is fail-soft when unreachable. |

**No-skip-as-green discipline (CLAUDE.md):** CI exports the exact env the integration tests read (composed DSNs `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`, `mcp-neo4j-cypher` on PATH); skip-helpers `t.Fatal` under `$CI` when a var is unset. A sub-second "integration" runtime is a skip tell.

### Container / compose / installer validation (live Docker, integration tier)
These prove the packaging acceptance criteria — they require a LIVE Docker stack (cannot be unit-tested).

| What it proves | Machine-checkable method |
|----------------|--------------------------|
| Fat image bundles runtimes (Req 5) | `docker run --rm <image> sh -c "command -v python3 && command -v node && command -v uvx && command -v mcp-neo4j-cypher"` (BOTH arches) |
| Full parity inside (Req 1) | `docker run` then assert: write home, install a skill, spawn a subprocess all succeed (spike 059 probe shape) |
| No host Docker (Req 1) | in-container `ls /var/run/docker.sock` fails AND `docker ps` fails |
| Host fs invisible (Req 1) | in-container rootfs is the Debian image, not the host; `/host` absent (spike 059) |
| Limits in effect (Req 1) | `cat /sys/fs/cgroup/memory.max` = mem_limit; `pids.max` set; `cpu.max` quota set (spike 059) |
| No secret in layers (Req 2) | `docker history --no-trunc <image>` and grep for each secret value → zero hits |
| `aura-home` persists (Req 2/8) | write `llm.json`/`mcp/servers.json`/`Agent.md`, `docker compose down && up`, assert preserved; remove the mount → lost |
| In-container docker-MCP guard (Req 3) | configure a `runtime.kind=docker` MCP, launch in-box → assert the "deploy as a compose sibling" error string |
| Sibling MCP mounts (Req 3) | bring the streamable-HTTP sibling up → assert it mounts and lists tools |
| No wsl.exe (Req 4) | `docker run --rm <image> sh -c "command -v wsl.exe"` returns non-zero |
| whatsapp sibling resolves / fail-soft (Req 4) | sibling up → recipe resolves + lists tools; sibling down → `aura` boots without crash |
| MCP-in-image, host clean (Req 6) | in-container Neo4j round-trip returns the server version; host `command -v mcp-neo4j-cypher` non-zero |
| Pre-baked recipes offline (Req 7) | host egress blocked → install+invoke `calculator` (uvx) + `mail` (npx) in-container with no download |
| Migrate gating (Req 8) | `docker compose up` reaches healthy only after `aura-migrate` exits 0; `docker kill aura` → auto-restart |
| Keyless boot (Req 9) | `docker compose up` with no `OPENROUTER_API_KEY` → healthy; agent call returns `llm_not_configured`; set key → answers |
| `aura doctor` (Req 10) | healthy stack → exit 0 all-green; stop the embed sidecar → non-zero naming the failed check |
| Caddy token + loopback (Req 11) | from a 2nd LAN host: wizard HTTPS only with token (no token → 401/403); PG/Neo4j/embed/agent-memory/sidecar ports refused |
| Installer clean-host (Req 12) | `curl -fsSL <url>/install.sh \| sh` on a clean Linux host → healthy + summary; `which python3 pip node` → none; re-run → `.env` byte-identical; under-spec HW → warn/abort first |
| Docker auto-install / Windows door (Req 13) | Docker-less Linux → auto-install + continue; forced-fail → guided + non-zero exit; documented Windows steps → stack up + PowerShell `.env` |
| ghcr multi-arch (Req 14) | a tagged release → public `ghcr.io/chetto1883/aura:<tag>` pullable on amd64+arm64 AND host-binary archives; compose references the pinned tag (not `latest`) |
| Appliance autostart (Req 15) | reboot the appliance → full stack healthy, no human action (systemd unit fired) |
| Backup + restore + update (Req 16) | scheduled backup artifact appears in the host-visible dir; documented restore rebuilds the DB; `compose pull && up -d` to a newer tag runs migrations + preserves data |
| gVisor tier smoke (D-03, native-Linux only) | with `compose.gvisor.yaml` applied: `aura` runs under `runsc` (full parity inside); on Docker Desktop the override is NOT applied |

### Nyquist sampling intent (for the planner — Dimension 8)
- **Per task commit:** the Go quick-run (`go test ./internal/mcp/manager/ ./internal/config/ ./internal/llm/ ./cmd/aura/`) + `go vet` + `go build`.
- **Per wave merge:** the full Go suite with `-race` + the relevant live Docker acceptance probes for that wave's requirements.
- **Phase gate:** `make quality-full` green (≥85% owned-surface coverage) + the full 16-criterion live Docker acceptance matrix (both arches for image criteria) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/mcp/manager/runtime_guard_test.go` — D-08 in-container guard (covers Req 3)
- [ ] `internal/config/keyless_boot_test.go` (or extend `config_test.go`) — D-10 serve-path relaxation (covers Req 9)
- [ ] `cmd/aura/doctor_test.go` + a `db_integration neo4j_integration` live leg — D-09 (covers Req 10)
- [ ] `internal/mcp/manager/catalog_test.go` — whatsapp recipe → sibling URL (covers Req 4)
- [ ] `scripts/smoke_packaging.sh` (or a CI job) — the live Docker acceptance probes (covers Req 1/2/5/6/7/8/11) — the packaging criteria have NO Go test home; they need a smoke harness
- [ ] CI: a `packaging` job that builds the multi-arch image (buildx) and runs the smoke probes; the gVisor leg `t.Fatal`s/skips by OS (native-Linux only)

*(Existing infra: the `db_integration`/`neo4j_integration` tiers + composed-DSN env + `mcp-neo4j-cypher` on PATH are already wired — reuse them for the `aura doctor` live leg.)*

---

## Security Domain

> `security_enforcement` is not explicitly `false` in config → treat as enabled.

The security model here is **inverted from a typical phase**: the threat model is the *container edge*, not code guardrails. The SPEC is emphatic — **no host-protection guardrails in Go, no internal lockdown** (SPEC.md:166). This makes most ASVS input/auth categories "N/A by design" for the agent's own surface (the operator is the trusted principal, arbitrary code execution is the *purpose*), with the real control being structural confinement.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes (LAN surface only) | Caddy shared access token on the user-facing surface (D-11). Real multi-user auth is OUT (future milestone). |
| V3 Session Management | no | Single trusted operator; no session layer in scope. |
| V4 Access Control | yes (structural) | The container edge + mount set + no Docker socket. NOT code permission gates (deliberately — SPEC.md:166). gVisor `runsc` optional thicker walls (D-03). |
| V5 Input Validation | partial | The `aura doctor` checks read operator-config + service responses, not untrusted input. The in-container guard error (D-08) is a clear, non-injectable constant string. |
| V6 Cryptography | yes | Caddy TLS (internal CA / self-signed, D-11) — never hand-rolled. `openssl rand` for secret-gen (D-12). |
| Supply-chain | yes | Pinned base images + `mcp-neo4j-cypher==0.6.0` + ghcr pinned-per-tag (never `latest`) + `docker history` secret-leak check (Req 2/14). |

### Known Threat Patterns for the box

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Prompt-injected turn does `rm -rf` / fork bomb | Tampering / DoS | Bounded to the container + mounts; resettable; `cpus`/`mem`/`pids` limits (mini-PC stability that also caps a fork bomb). Open NAT egress = accepted residual (SPEC.md:37). |
| Malicious skill/MCP escapes to the host | Elevation of Privilege | No host Docker socket (the lone invariant); host fs outside mounts invisible; optional gVisor `runsc` syscall interception (D-03). |
| Secret leaked via `docker history` | Information Disclosure | Secrets only via compose env/`env_file`, never a build layer (Req 2). |
| Unauthenticated LAN access to the wizard/data | Information Disclosure / Tampering | Caddy TLS + shared token fronts ONLY the user-facing surface; all data/sidecar ports stay loopback (Req 11). |
| Slopsquatted recipe/base image | Tampering (supply-chain) | Pinned versions; recipes are own forks (chetto1983); ghcr pinned-per-tag. |
| Outbound data exfiltration | Information Disclosure | **Accepted residual, NOT mitigated** (egress allowlist declined 2026-06-10, SPEC.md:37/151). Documented, not pretended away. |

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Host-run Aura (`go run` + undocumented host `pip install mcp-neo4j-cypher`) | Fat container; host needs only Docker | This phase (Slice 14 / amendment #47) | The "single static binary" promise broke at MCP; the box absorbs every runtime. |
| In-loop micro-sandbox (`sandbox_exec` + rivetdev sandbox-agent) | Removed; the whole agent is in a box; `shell_exec` is the box's terminal | `c9e1124e` (sandbox removal) | The deleted sandbox + absence of code guardrails are CORRECT — host protection is the container's job. |
| Distroless + `cap_drop`/`read_only`/non-root (audit `ec7fe2f6`) | Fat full-power box; no capability stripping | This phase (D-01 revert) | The audit jail structurally broke `shell_exec`/self-extend/MCP (spike 059); reverted. |
| whatsapp via `wsl.exe` (host coupling) | whatsmeow sibling over streamable-HTTP | This phase (D-07) | Every MCP runs in the Linux container world; no Windows dependency. |

**Deprecated/outdated:**
- The root distroless `Dockerfile` — removed/repointed (D-01).
- `compose.yaml` `aura` service hardening (`user 65532`/`read_only`/`cap_drop`) — reverted (D-01).
- The host `pip install mcp-neo4j-cypher` README step — removed (now in-image, Req 6).
- The `sandbox-agent` references in `.planning/REQUIREMENTS.md` CAP-01/CAP-02 — stale (predate `c9e1124e`); NOT a Phase-17 input.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Caddy's `tls internal` + a header/`forward_auth` token directive satisfies Req 11 | Standard Stack / D-11 | Wrong directive = LAN exposure without auth; the 401-without-token probe catches it. Planner verifies the exact Caddyfile. |
| A2 | The recipe pre-bake `--help` warm-up populates the uv/npx caches enough for offline first-run | Req 7 | Egress-blocked acceptance probe catches an incomplete bake; executor adjusts the warm-up invocation. |
| A3 | goreleaser v2 has a current multi-arch image push directive for buildx + ghcr | Req 14 | Build-time only; a snapshot build verifies. Fallback: a separate buildx step in CI driven by the release workflow. |
| A4 | The whatsapp whatsmeow bridge sibling image can be built from the chetto1983 fork + spike 002 patch | Req 4 / D-07 | Fail-soft means a wrong image is non-fatal; planner-choice flag surfaces it. |
| A5 | The illustrative `docker/aura/Dockerfile` shape builds on both arches as written | Pattern 1 | The exact directives are an executor detail; spikes 059/060 prove the *approach*, not this literal file. |
| A6 | `security_opt: no-new-privileges` can be dropped for full parity without a regression | Req 1 | Spike 059 ran with no security_opt and full parity held; planner confirms whether to keep or drop it. |

**These assumptions need user/planner confirmation before becoming locked decisions.** A1/A4 map to the existing planner-choice flags; A2/A3/A5 are executor-resolved build details; A6 is a small compose-knob confirm.

## Open Questions (RESOLVED)

> All three resolved during planning (2026-06-14); each maps to a specific plan task. No plan changes pending — the resolutions are encoded in the plans and recorded here for traceability.

1. **The Req 16 backup execution model (the `docker exec` paradox).** — **RESOLVED → 17-08 Task 1** (`checkpoint:decision`, default **option (b)** in-box network `pg_dump`; the box stays socket-less).
   - What we know: `BackupHandler.Run` uses `docker exec` (backup.go:41-53); the `aura` box has no socket.
   - What's unclear: which of the three resolution options (separate socketed backup service / network `pg_dump` from in-box / host-side timer) the operator prefers.
   - Recommendation: **option (b)** — network `pg_dump`/sidecar dump from inside the box — best preserves "the box stays a box." Surface as the highest-priority plan decision.

2. **Caddy token enforcement mechanism (D-11 planner-choice).** — **RESOLVED → 17-06 Task 3** (header/`@token` matcher returning 401/403; verified by the live 401-without-token probe).
   - What we know: shared token + TLS internal CA, fronting wizard + AG-UI only.
   - What's unclear: `forward_auth` vs a header/`@token` matcher.
   - Recommendation: planner picks; verify with the 401-without-token live probe.

3. **`aura-skills`/`aura-runs` → `aura-home` dev volume migration.** — **RESOLVED → 17-06 Task 1** (`aura-home` is the greenfield appliance volume; dev volume mapping is a documented manual step, not auto-migrated).
   - What we know: the appliance is greenfield (no migration); dev has existing `aura-skills`/`aura-runs` volumes.
   - What's unclear: whether to auto-migrate dev volumes or document a manual mapping.
   - Recommendation: document a manual dev mapping; the appliance ships clean.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker + buildx | image build, all live acceptance | ✓ (Docker Desktop, dev) | — | — |
| `mcp-neo4j-cypher` | image runtime + doctor + client.go | ✓ (in-image after build) | 0.6.0 (pinned) | — |
| gVisor `runsc` | D-03 appliance tier | ✗ on Docker Desktop dev | — | Baseline plain box on dev; runsc only native-Linux/arm64 |
| native-Linux dockerd | gVisor tier, install.sh auto-Docker test | ✗ on the Windows dev host | — | WSL native dockerd / CI Linux / the appliance |
| `openssl` | install.sh secret-gen | ✓ (system) | — | — |
| `caddy` image | Req 11 | ✓ (pullable) | 2.x | — |

**Missing dependencies with no fallback:** none that block planning.
**Missing dependencies with fallback:**
- gVisor `runsc` is unavailable on the Windows/Docker-Desktop dev host (spikes 010/059) → the gVisor tier is validated on native-Linux/CI/the appliance only; dev runs the baseline plain box. The install.sh Docker auto-install and the multi-arch arm64 build are CI/native-Linux validated, not dev-host validated.

---

## Sources

### Primary (HIGH confidence)
- `.planning/phases/17-packaging-distribution/17-SPEC.md` — the 16 locked requirements, boundaries, acceptance criteria, the 2026-06-10 security-model revision.
- `.planning/phases/17-packaging-distribution/17-CONTEXT.md` — D-01…D-15 implementation decisions; the box-model settlement.
- `.planning/spikes/059-phase17-box-parity-edge/README.md` — on-host parity/edge evidence (distroless exit 127; fat box full parity; limits hold).
- `.planning/spikes/060-phase17-fat-image-base/README.md` — fat runtime base (all four binaries resolve; cache-stable 73s→1.4s; 875 MB).
- `.planning/spikes/061-phase17-isolation-tier/README.md` — the box-model decision matrix + the PRD-amendment call.
- `.planning/spikes/062-docker-sandboxes-sbx-fit/README.md` — Docker Sandboxes verdict (deferred; direction-signal).
- `.planning/spikes/010-sandbox-gvisor-runsc/README.md` — gVisor prior art (workload survives; Docker Desktop can't host runsc).
- `.claude/skills/spike-findings-Aura/references/sandbox-runtime.md` — hardening-tiers menu + compose-override-per-tier pattern.
- Codebase (file:line cited throughout): `compose.yaml`, `Dockerfile`, `internal/mcp/manager/runtime.go`/`catalog.go`, `internal/config/config.go`, `internal/llm/config.go`, `internal/knowledge/client.go`, `cmd/aura/{main,db,web,mcp_status,serve,chat}.go`, `cmd/aura/exit_codes.go`, `internal/cron/handlers/backup.go`, `scripts/restore_drill.sh`, `docker/agent-memory/Dockerfile`, `docker/markitdown/Dockerfile`, `.goreleaser.yaml`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`, `.planning/config.json`.

### Secondary (MEDIUM confidence — verified this session)
- [PyPI mcp-neo4j-cypher](https://pypi.org/project/mcp-neo4j-cypher/) — confirmed 0.6.0 is current latest.
- [gVisor install / runtime docs](https://gvisor.dev/docs/user_guide/install/) — runsc supports amd64+arm64, Linux 4.14.77+, registered via `/etc/docker/daemon.json`.
- [gVisor docker-compose tutorial](https://gvisor.dev/docs/tutorials/docker-compose/) — `runtime:` field requires daemon.json registration.

### Tertiary (LOW confidence — needs validation)
- The exact goreleaser v2 multi-arch image push directive (A3) — verify at plan time with a snapshot build.
- The exact Caddyfile `tls internal` + token directive (A1) — verify with the live 401-without-token probe.

## Metadata

**Confidence breakdown:**
- Standard stack: **HIGH** — every component is pinned + spike-verified (059/060) or confirmed against the live registry (mcp-neo4j-cypher 0.6.0).
- Architecture / box model: **HIGH** — settled by spikes 059–062 + 010; this is consolidation, not re-investigation.
- Per-requirement HOW: **HIGH** for the 12 settled requirements (file:line touch points confirmed in the codebase); **MEDIUM** for Req 7 pre-bake invocation, Req 14 goreleaser directive, Req 16 backup model.
- Landmines / validation: **HIGH** — the backup `docker exec` paradox and the gVisor daemon.json prerequisite are verified-real cross-requirement conflicts; the validation matrix maps all 16 criteria to machine-checkable methods.

**Research date:** 2026-06-14
**Valid until:** ~2026-07-14 (30 days — stable; the box model is locked, the only volatility is upstream base-image/goreleaser minor versions).

---

## RESEARCH COMPLETE

**Phase:** 17 - Packaging & Distribution
**Confidence:** HIGH

### Key Findings
- The technical investigation is **already done** (spikes 059–062 + 010). This RESEARCH is **consolidation + validation architecture**, not re-investigation — net-new research is near zero.
- The current `compose.yaml` `aura` service (lines 10–51) and root `Dockerfile` **ARE the audit jail `ec7fe2f6`** that D-01 reverts — the de-harden is an *edit*, not a greenfield add. Spike 059 proved that box breaks `shell_exec`/self-extend/MCP (exit 127).
- **A D-04 PRD/SPEC amendment is a BLOCKING pre-execution gate** — add the gVisor tier, record the revert, correct the stale SPEC Background + the SPEC.md:137 hardening contradiction.
- **Highest-priority surfaced conflict:** the Req 16 backup `docker exec` paradox — `BackupHandler.Run` (backup.go:41-53) needs a Docker socket the `aura` box deliberately lacks. Needs an explicit plan decision (recommend: network `pg_dump` from in-box).
- Wave order: D-04 amendment → Wave 1 (Dockerfile + de-harden compose) → Wave 2 (independent Go changes ∥ compose topology) → Wave 3 (goreleaser/installer/systemd/backup/README). `mcp-neo4j-cypher==0.6.0` confirmed current; gVisor needs host `daemon.json` registration (native-Linux/arm64 only).

### File Created
`.planning/phases/17-packaging-distribution/17-RESEARCH.md`

### Confidence Assessment
| Area | Level | Reason |
|------|-------|--------|
| Standard Stack | HIGH | Pinned + spike-verified (059/060) + registry-confirmed (mcp-neo4j-cypher 0.6.0) |
| Architecture | HIGH | Settled by spikes 059–062 + 010; consolidation not re-decision |
| Pitfalls | HIGH | Backup paradox + gVisor daemon.json prerequisite are verified-real; validation maps all 16 criteria |

### Open Questions
- The Req 16 backup execution model (the `docker exec` paradox) — recommend network `pg_dump` from in-box.
- Caddy token mechanism + exact HW thresholds + the whatsapp bridge image (the planner-choice flags).

### Ready for Planning
Research complete. The planner can create PLAN.md files — gating on the D-04 amendment as wave-0 and resolving the Req 16 backup model + the four planner-choice flags at plan-review.
