<div align="center">

<img src="public/Logo.png" alt="Aura logo" width="160" height="160" />

# Aura

**A local-first, provider-neutral AI agent platform — in Go.**

One binary. Your hardware. Your data. A capable agent with a full terminal,
graph-backed memory, self-authored skills, and multi-channel access.

[![CI](https://github.com/chetto1983/Aura/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/chetto1983/Aura/actions/workflows/ci.yml)
[![CodeQL](https://github.com/chetto1983/Aura/actions/workflows/codeql.yml/badge.svg?branch=master)](https://github.com/chetto1983/Aura/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)

[What is Aura?](#what-is-aura) · [Features](#key-features) · [Architecture](#architecture-one-screen) · [Quick Start](#quick-start) · [Docs](#documentation) · [Development](#development)

</div>

---

## What is Aura?

Aura is a personal AI agent that runs on the user's own machine. A single Go binary
hosts the agent runtime, a broad tool surface (host shell + filesystem, web, documents,
scheduling, skills), multi-channel access (CLI, Telegram, AG-UI/SSE web), and a
Postgres + ArcadeDB memory — talking to a swappable LLM (DeepSeek-V4 over OpenRouter by
default) plus a few local CPU sidecars. It is built as a **product, not a prototype**.

> **Strategic context:** Aura is designed to ship as a **DGX Spark + software bundle** for
> SMBs that want a private, capable assistant on hardware they own.

<div align="center">

<img src="public/cockpit.png" alt="Aura operator cockpit — chat with reasoning, human-in-the-loop approval cards, and a live token/cost footer" width="820" />

<sub>The web cockpit (AG-UI/SSE): streaming chat with reasoning, human-in-the-loop approval &amp; input-required gates, and live token/cost accounting.</sub>

</div>

## At a glance

| | |
|---|---|
| **Language** | Go 1.26 |
| **Size** | ~98k LOC non-test (`cmd` + `internal`, of which ~7k sqlc-generated) · 68 internal packages |
| **Tests** | ~143k LOC — table-driven · property-based · fuzz · `-race` · `goleak` · mutation |
| **Test coverage** | owned-surface aggregate **≥85%** (87.0% measured 2026-08-30) plus a fail-closed per-package policy, enforced in CI on every push |
| **CI** | build/vet/lint · CodeQL · `-race` + goleak · db/ArcadeDB/embed integration · MUSR two-identity E2E · web lint/test/mutation/Playwright · critical mutation ≥70% killed |
| **Persistence** | Postgres (sqlc, pgx) + ArcadeDB (graph memory, full-text + LSM vector index) + Garage (S3 object store) |
| **Default LLM** | DeepSeek-V4 via OpenRouter — provider-neutral; the active profile (provider, model, budgets) is hot-reloaded from the cockpit settings, no restart |
| **Status** | v1.0.1 tagged 2026-06-20 · v2.0.0 industrial hardening shipped · v2.1.0 (Hermes/Claude-Code parity) in progress, 3/8 phases closed · `v1.0.2-rc1` being cut through the exact-SHA readiness gate |

## Key features

- **Streaming agent loop** with a shared *budget tree* (step + wall-clock caps) and a tool-loop *dedup ring* — bounded, predictable cost.
- **Deferred-tool pattern + semantic `tool_search`** — dozens of tools (incl. dynamic MCP tools) stay discoverable at near-zero per-turn token cost.
- **Adaptive reasoning router** — a local curated-seed embedding classifier picks reasoning effort in ~10 ms.
- **Full host terminal + filesystem tools** — real operating power, with destructive-command approval gates and secret redaction.
- **Graph-native memory** — bitemporal facts and entities live in an ArcadeDB graph (full-text + optional dense leg); conversations persist with a context-management ladder.
- **Self-extension** — the agent authors and runs its own skills, and mounts MCP servers (calculator, calendar, whatsapp, memory).
- **Scheduler and self wake-ups** — one `task` tool (`at | every | cron`) for reminders and `agent_job` runs: the agent can schedule itself to wake up later and act; every agent_job is approval-gated on the channel it was scheduled from.
- **Per-identity sandbox** — a full-capability box per operator (gVisor `runsc` on native Linux), with deliverables handed back over the channel (`send_file`), never as a path.
- **Multi-channel** — CLI REPL, Telegram (voice/photo/docs/HITL), and a web cockpit over AG-UI/SSE with mid-turn steering, approvals, and live settings.

## Architecture (one screen)

```text
Transport & UX     cmd/aura (CLI) · channels (+telegram) · agui (SSE) · setup · askuser
Agent runtime      agent (LlmAgent, Budget, Events, hooks) · workflow (Seq/Par/Loop) · swarm
Tools & MCP        agent/tools (registry, deferred, tool_search, fs/shell/web/skill) · mcp (+bridge, manager)
Intelligence       llm (+openai_compat) · semindex (embed-index core) · reasoningtrace · scoring
Capabilities       web · skills · cron · onboarding · documents
Persistence        db (Postgres+sqlc) · arcadedb (graph memory) · conversations · identity · profile · secret
Observability      obs · panicobs · reasoningtrace · toolinvocations · cachemetrics
```

## Documentation

| Doc | For |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | How the system is built — layers, turn lifecycle, invariants |
| [docs/TECHNICAL_OVERVIEW.md](docs/TECHNICAL_OVERVIEW.md) | CTO / due-diligence overview — problem, differentiators, maturity |
| [docs/CAPABILITIES.md](docs/CAPABILITIES.md) | Capability matrix — shipped / in-progress / roadmap |
| [docs/release-readiness.md](docs/release-readiness.md) | How a release is cut — the twelve-report exact-SHA gate, rollback rule, operational checks |
| [.planning/codebase/](.planning/codebase/) | Package-level inventory + conventions + concerns — generated, refresh with `/gsd-map-codebase` |
| [CLAUDE.md](CLAUDE.md) · [prd.md](prd.md) | Engineering guidance · product requirements (source of truth) |

---

## Deployment (Docker Compose appliance)

Aura is a self-hosted agent runtime packaged as a Docker Compose appliance. The
default stack brings up Aura, Postgres, ArcadeDB and its MCP, the local embedding sidecar, Caddy
TLS/token access, and optional MCP siblings.

## Quick Start

> **Releases.** `ghcr.io/chetto1983/aura:<tag>` and the binary archives are published by
> the `Release` workflow on a `v*` tag, and only after the exact-SHA *Production
> Readiness* check passed for that commit ([docs/release-readiness.md](docs/release-readiness.md)).
> Tags `v1.0.0`/`v1.0.1` exist, but every earlier appliance image was retired (PRD
> amendment #106.4) and no GitHub Release is currently published — check the
> [Releases page](https://github.com/chetto1983/Aura/releases) for the current tag
> (`v1.0.2-rc1` is the one being cut) and use it as `vX.Y.Z` below. While no image is
> on GHCR, build it from a checkout (see [Development](#development)) and set
> `AURA_IMAGE=aura:local`.

### Linux or macOS

Install Docker, then run the installer from a release tag — replace `vX.Y.Z` with the
tag you are installing:

```bash
curl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/vX.Y.Z/scripts/install.sh | bash
```

The installer checks hardware, creates `.env` with generated `POSTGRES_PASSWORD`,
the three `ARCADEDB_*` secrets, and `AURA_ACCESS_TOKEN`, downloads the Compose/Caddy assets, and
starts the stack. Re-running it keeps an existing `.env` intact.

Optional appliance mode installs a systemd unit under `/opt/aura`:

```bash
curl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/vX.Y.Z/scripts/install.sh | sudo bash -s -- --appliance
```

Add `--gvisor` on native Linux Docker hosts that should run Aura under `runsc`.
Docker Desktop is intentionally not supported for that isolation tier.

### Windows

Use Docker Desktop and the shipped Compose files. From PowerShell in the Aura
checkout or release directory:

```powershell
function New-Hex { -join ((1..32) | ForEach-Object { '{0:x2}' -f (Get-Random -Maximum 256) }) }
@"
POSTGRES_PASSWORD=$(New-Hex)
POSTGRES_IMAGE=postgres:18.4-alpine3.24
POSTGRES_USER=aura
POSTGRES_DB=aura
ARCADEDB_PASSWORD=$(New-Hex)
ARCADEDB_APP_PASSWORD=$(New-Hex)
AURA_ARCADEDB_TENANT_SECRET=$(New-Hex)
AURA_IMAGE=ghcr.io/chetto1983/aura:vX.Y.Z
AURA_ACCESS_TOKEN=$(New-Hex)
AURA_AUTHULA_SECRET=$(New-Hex)
SEARXNG_SECRET=$(New-Hex)
AURA_OBJECTSTORE_ACCESS_KEY=GK$((New-Hex).Substring(0,24))
AURA_OBJECTSTORE_SECRET_KEY=$(New-Hex)
GARAGE_RPC_SECRET=$(New-Hex)
AURA_GARAGE_ADMIN_TOKEN=$(New-Hex)
AURA_BACKUP_DIR=./backups
AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda
AURA_EMBED_MODEL_PATH=/root/.cache/llama.cpp/embeddinggemma-300M-Q8_0.gguf
AURA_EMBED_REVISION=0f741b5a6585bd53aeb15cd1372c56f2a0f65e12
AURA_EMBED_FINGERPRINT=b5ce9d77a3fc4b3b39ccb5643c36777911cc4eb46a66962eadfa3f5f60490d63
AURA_EMBED_NGL=99
AURA_EMBED_DIMENSIONS=768
OPENROUTER_API_KEY=
"@ | Set-Content -Path .env -Encoding ascii

docker run --rm --gpus all nvidia/cuda:12.8.0-base-ubuntu24.04 nvidia-smi
docker compose up -d
```

Aura's local embedding sidecar requires Docker GPU passthrough. Fix
Docker/NVIDIA before starting Aura if the `nvidia-smi` container check fails.

Set `OPENROUTER_API_KEY` before production use. For local development images,
replace `AURA_IMAGE` with `aura:local` after building the image.

Postgres 18 is the default Compose image for new installs. When upgrading an
existing Aura deployment from Postgres 17, migrate the data with `pg_dump` /
`pg_restore` or `pg_upgrade`; a Postgres 18 container cannot reuse a Postgres 17
data volume directly.

### Access

Aura publishes AG-UI and setup on loopback and Caddy on HTTPS:

```text
https://localhost/setup/?token=<AURA_ACCESS_TOKEN>
```

Caddy uses `tls internal`. Browsers on other LAN machines will warn until they
trust the local CA root from the `caddy-data` volume:

```bash
docker compose exec caddy cat /data/caddy/pki/authorities/local/root.crt > aura-caddy-root.crt
```

## Updates

Update the appliance with Compose. Volumes persist, and the `aura-migrate`
one-shot runs the Postgres migrations before the Aura service starts (ArcadeDB
needs none — the MCP creates each identity's database on first use):

```bash
docker compose pull
docker compose up -d
```

## Backup And Restore

Scheduled backups run inside the socketless Aura box. Postgres is dumped over the
Compose network with `pg_dump` into `AURA_BACKUP_DIR`:

```text
./backups/postgres-YYYYMMDDTHHMMSSZ.dump
```

**Memory is NOT backed up.** Memory lives in one ArcadeDB database per identity
(`internal/arcadedb/tenant.go`) and nothing dumps them. Snapshot the
`aura-arcadedb` volume out of band until that gap is closed.

Run the restore drill against the current Compose stack:

```bash
set -a
. ./.env
set +a
scripts/restore_drill.sh
```

The drill has three planes — Postgres into a temporary database, the conversation
sidecar archive, and Garage — each checksum-verified into a disposable target and
cleaned up. There is no memory plane; see the gap above.

Manual restore commands:

```bash
docker compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres \
  pg_restore -U "${POSTGRES_USER:-aura}" -d "${POSTGRES_DB:-aura}" \
  --clean --if-exists --no-owner --no-acl /backups/postgres-YYYYMMDDTHHMMSSZ.dump
```

Take a fresh backup before restoring over a live database.

## Optional WhatsApp MCP

The `whatsapp` service is an optional sibling mounted through Aura's MCP catalog.
It uses an unofficial whatsmeow-based client, so it carries WhatsApp Terms of Service and account-ban risk. First pairing is headless:

```bash
docker compose logs -f whatsapp
```

Scan the QR code shown in the logs. Aura boot never depends on this service.

## Retired Host Setup

The host needs no Python MCP runtime at all: memory is served by Aura's own
ArcadeDB MCP, a Go binary in the image. Old host-level Python installs and the
earlier WSL WhatsApp MCP install can be removed after migrating to the Compose
appliance.

## CLI

```text
aura serve                    run the long-lived agent runtime (channels, cockpit, scheduler)
aura shell | chat <sub>       interactive REPL / chat conversations against the agent loop
aura doctor | config <sub>    environment diagnostics / effective configuration
aura agent dry-run            drive a mock LoopAgent through the Budget tree
aura tools                    print the tool manifest
aura task <sub>               operator parity with the model-facing `task` tool:
                              schedule | list | cancel | run_now | approve | runs | doctor
aura mcp <sub>                managed MCP servers: install | add | list | doctor | tools | enable | disable | remove
aura memory <sub>             ArcadeDB memory administration
aura identity <sub>           identities, capability grants, operator break-glass recovery
aura gateway grants <sub>     AG-UI gateway approval grants
aura paused-states <sub>      HITL pauses
aura skills <sub> | pack <sub> skill lifecycle · packs: list | show | install | trust
aura retention <plan|apply>   retention sweep
aura db <sub>                 Postgres lifecycle: migrate | ping | status | reset
aura objectstore <sub>        Garage object-store administration
aura web <doctor|tool ...>    web tools (search/fetch) from the CLI
aura docs <sub>               document ingestion
aura version                  build metadata
```

## Development

For source builds, install Go from [`go.mod`](go.mod), Docker, and a POSIX shell.
Linux is the supported source-build and quality-gate runtime. On Windows, use WSL;
the native Windows Go binary is not a release target because Windows ACLs are not
represented by POSIX `FileMode` bits. Docker Desktop remains supported for running
the shipped Linux Compose appliance.

```bash
git clone https://github.com/chetto1983/Aura.git
cd Aura
cp .env.example .env
make tools
lefthook install
make db-migrate memory-up
go run ./cmd/aura version
go run ./cmd/aura agent dry-run --request-id auto
```

To run the Compose appliance from source while no image is published on GHCR, build
the image the release would have published and point `.env` at it (the image builds
`web/` in its own stage; the committed `internal/webui/dist` only feeds a host `go build`
and is refreshed from that stage, never from a host `vite build`):

```bash
docker build -f docker/aura/Dockerfile -t aura:local .
# then in .env:  AURA_IMAGE=aura:local
```

Quality gates:

```bash
make quality
make db-migrate memory-up
make quality-full
```

| Target | Does |
|--------|------|
| `make tools` | install the quality toolchain |
| `make lint` / `make vet` | lint and `go vet` |
| `make vuln` | `govulncheck` supply-chain scan |
| `make test-race` | `go test -race ./...` |
| `make coverage` | owned-surface coverage floor |
| `make restore-drill` | three-plane restore drill (Postgres, sidecars, Garage) |

## Project Layout

```text
cmd/aura/                CLI entry and subcommands
internal/agent/          Agent interface, Event, Budget tree, workflow agents
internal/db/             Postgres: pgx pool, golang-migrate, sqlc bindings
internal/config/         environment to typed config
internal/canonicaljson/  deterministic JSON for dedup fingerprints
scripts/                 install, smoke, restore drill, coverage, file-size cap
.planning/               GSD planning artifacts
```

## Scope

Aura is PRD-first. Persistence is Postgres plus an ArcadeDB graph, with graph access
through MCP for model-facing tools. The default packaged deployment keeps Aura
socketless: no Docker socket is mounted into the Aura container.

## Contributing And Security

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Report vulnerabilities privately per
[`SECURITY.md`](SECURITY.md), never in a public issue.

## License

[MIT](LICENSE) Copyright 2026 Davide Marchetto.
