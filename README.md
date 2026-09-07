<div align="center">

<img src="public/Logo.png" alt="Aura logo" width="160" height="160" />

# Aura

**A local-first, provider-neutral AI agent platform — in Go.**

An agent for ongoing work: tools, document retrieval, temporal memory, scheduled
jobs, and a web cockpit on infrastructure you control.

[![CI](https://github.com/chetto1983/Aura/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/chetto1983/Aura/actions/workflows/ci.yml)
[![CodeQL](https://github.com/chetto1983/Aura/actions/workflows/codeql.yml/badge.svg?branch=master)](https://github.com/chetto1983/Aura/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)

[What is Aura?](#what-is-aura) · [Features](#key-features) · [Architecture](#architecture-one-screen) · [Quick Start](#quick-start) · [Docs](#documentation) · [Development](#development)

</div>

---

## What is Aura?

Aura is a self-hosted AI agent. Its Go binary hosts the runtime, tools, CLI,
Telegram gateway, and embedded web cockpit. Docker Compose runs Postgres,
ArcadeDB, Garage, embedding, ingestion, and selected integrations alongside it.
The model is configurable; the default route uses OpenRouter. Local storage does
not make cloud inference offline: the selected provider receives the context sent
to that model. Local OpenAI-compatible endpoints are also supported.

> **Deployment:** Aura can run on hardware the operator owns or manages. Select
> capacity for the chosen model, workload, documents and backup retention.

<div align="center">

<img src="public/cockpit.png" alt="Aura operator cockpit — chat with reasoning, human-in-the-loop approval cards, and a live token/cost footer" width="820" />

<sub>The web cockpit (AG-UI/SSE): streaming chat with reasoning, human-in-the-loop approval &amp; input-required gates, and live token/cost accounting.</sub>

</div>

## At a glance

| | |
|---|---|
| **Language** | Go 1.26 |
| **Tests** | Unit, property, race, leak, mutation, live integration, and browser tests |
| **Test coverage** | Owned-surface aggregate **≥85%**, with package policies and separate live memory/sandbox coverage authorities |
| **CI** | build/vet/lint · CodeQL · `-race` + goleak · db/ArcadeDB/embed integration · MUSR two-identity E2E · web lint/test/mutation/Playwright · critical mutation ≥70% killed |
| **Persistence** | Postgres (sqlc, pgx) + ArcadeDB (graph memory, full-text + LSM vector index) + Garage (S3 object store) |
| **Default LLM** | DeepSeek-V4 via OpenRouter — provider-neutral; the active profile (provider, model, budgets) is hot-reloaded from the cockpit settings, no restart |
| **Distribution** | `edge` tracks master; `v1.0.2-rc1` is the latest tagged prerelease checked on 2026-09-07. See Releases for current availability |

## Key features

- **Streaming agent loop** with shared step/time budgets and repeated-call controls to bound work.
- **Deferred tools and `tool_search`** — discover tools and load their schemas when needed, including tools from mounted MCP servers.
- **Adaptive reasoning router** — selects reasoning effort using the configured classifier and the active model's supported capabilities.
- **Full host terminal + filesystem tools** — real operating power, with destructive-command approval gates and secret redaction.
- **Graph-native memory** — facts, sources and validity windows in ArcadeDB; temporal paths return supporting evidence. Postgres-authoritative conversations have a derived recall projection and managed context compaction.
- **Document retrieval** — indexed passages with source hashes and citations, plus access to the original file for calculations and whole-file tasks.
- **Self-extension** — author and run skills, use bundled memory/PIM/WhatsApp integrations, and connect additional MCP servers.
- **Scheduler and self wake-ups** — one `task` tool (`at | every | cron`) for reminders and `agent_job` runs, with job policy, operator controls and outcomes delivered to the owning conversation.
- **Per-identity sandbox** — a full-capability box per operator (gVisor `runsc` on native Linux), with deliverables handed back over the channel (`send_file`), never as a path.
- **Multi-channel** — CLI REPL, Telegram (voice/photo/docs/HITL), and a web cockpit over AG-UI/SSE with mid-turn steering, approvals, and live settings.

## Architecture (one screen)

```text
Transport & UX     cmd/aura (CLI) · channels (+telegram) · agui (SSE) · setup · askuser
Agent runtime      agent (LlmAgent, Budget, Events, hooks) · workflow (Seq/Par/Loop) · swarm
Tools & MCP        agent/tools (registry, deferred, tool_search, fs/shell/web/skill) · mcp (+bridge, manager)
Intelligence       llm (+openai_compat) · semindex (embed-index core) · reasoningtrace · scoring
Capabilities       web · skills · cron · onboarding · documents
Persistence        db (Postgres+sqlc) · arcadedb (memory + retrieval) · conversations · identity · objectstore · secret
Observability      obs · panicobs · reasoningtrace · toolinvocations · cachemetrics
```

## Documentation

| Doc | For |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | How the system is built — layers, turn lifecycle, invariants |
| [docs/TECHNICAL_OVERVIEW.md](docs/TECHNICAL_OVERVIEW.md) | CTO / due-diligence overview — problem, differentiators, maturity |
| [docs/CAPABILITIES.md](docs/CAPABILITIES.md) | Capability matrix — shipped / in-progress / roadmap |
| [docs/release-readiness.md](docs/release-readiness.md) | How a release is cut — the twelve-report exact-SHA gate, rollback rule, operational checks |
| [docs/BACKUP-RESTORE.md](docs/BACKUP-RESTORE.md) | Backup schedules, recovery procedures, live validation and scope |
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
> Check the
> [Releases page](https://github.com/chetto1983/Aura/releases) for the current tag
> (`v1.0.2-rc1` is the latest) and use it as `vX.Y.Z` below. Independently of
> releases, every master push publishes the moving `ghcr.io/chetto1983/aura:edge`
> image (plus an immutable `master-<sha>` tag) — the continuous-delivery channel a
> default install tracks.

### Linux or macOS

The interactive installer supports local installation or a Linux target over SSH:

```bash
npx create-aura-appliance
npx create-aura-appliance --mode remote
```

It requires Node.js **22.13 or newer** on the workstation. The target needs at least
**4 CPU cores, 14 GiB usable RAM, and 20 GiB free disk**; documents, models and backup
retention need additional capacity. The wizard detects NVIDIA on the target and
selects CUDA or CPU embeddings. See the [installer guide](packages/create-aura/README.md)
for supported targets and prerequisites. The npm installer carries its own payload.

The source-hosted installer remains available:

Install Docker, then run the installer. One command on a machine with Node 18+
(`npx` fetches the repo and runs `scripts/install.sh`):

```bash
sudo npx github:chetto1983/Aura -- --appliance
```

or the curl equivalent of the same script — use `master` to track the edge
channel, or a release tag `vX.Y.Z` to pin:

```bash
curl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/master/scripts/install.sh | sudo bash -s -- --appliance
```

The installer checks hardware, creates `.env` with generated `POSTGRES_PASSWORD`,
the three `ARCADEDB_*` secrets, and `AURA_ACCESS_TOKEN`, downloads the Compose/Caddy assets, and
starts the stack. Re-running it keeps an existing `.env` intact. A master/edge
install points `.env` at the `:edge` moving tags, and `--appliance` also enables
the `aura-image-update` systemd timer: from then on the machine re-pulls aura and
its MCP sidecars from GHCR on its own, migrations included, with no operator
involved. Without `--appliance` (no systemd units, no timer), the stack still
starts; updates stay manual.

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

An edge appliance installed with `--appliance` updates itself: the
`aura-image-update.timer` (5-minute cadence, flock-guarded) pulls the moving
tags and recreates only what changed, running migrations first. Watch it with
`journalctl -u aura-image-update.service -f`.

Manual update (pinned installs, or no systemd). Volumes persist, and the
`aura-migrate` one-shot runs the Postgres migrations before the Aura service
starts (ArcadeDB needs none — the MCP creates each identity's database on first
use):

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

**Memory is backed up automatically.** ArcadeDB loads
[`docker/arcadedb/backup.json`](docker/arcadedb/backup.json) and backs up every
database every 60 minutes, including newly-created identity databases. Archives
live in the separate `aura-arcadedb-backups` volume. The configuration sets
`maxFiles=60` and tiered hourly/daily/weekly/monthly retention of 24/7/4/6.

The database and backup volumes are separate, but both are on the same host by
default. Preserve off-host copies and the deployment configuration separately.
Garage objects, workspaces, and other runtime files need their own backup policy.

Run the restore drill against the current Compose stack:

```bash
set -a
. ./.env
set +a
scripts/restore_drill.sh
```

The drill tests four planes: Postgres, conversation sidecars, Garage and an
ArcadeDB database shaped like a tenant. It verifies restored checksums and cleans
up its disposable resources. All four passed on 2026-09-07. A separate restore
of an existing scheduled operator-memory archive recovered 93 entities, 75 facts
and 40 mentions, including a historical fact. This is a dated recovery check,
not a complete host-loss rehearsal or an RPO/RTO guarantee. See
[Backup and restore](docs/BACKUP-RESTORE.md) for scope and evidence.

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

To run a local source build instead of a published image, build
the image and point `.env` at it (the image builds
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
| `make restore-drill` | four-plane restore drill (Postgres, sidecars, Garage, ArcadeDB) |

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
