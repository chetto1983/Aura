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

</div>

---

## What is Aura?

Aura is a personal AI agent that runs on the user's own machine. A single Go binary
hosts the agent runtime, a broad tool surface (host shell + filesystem, web, documents,
scheduling, skills), multi-channel access (CLI, Telegram, AG-UI/SSE web), and a
Postgres + Neo4j memory — talking to a swappable LLM (DeepSeek-V4 over OpenRouter by
default) plus a few local CPU sidecars. It is built as a **product, not a prototype**.

> **Strategic context:** Aura is designed to ship as a **DGX Spark + software bundle** for
> SMBs that want a private, capable assistant on hardware they own.

## At a glance

| | |
|---|---|
| **Language** | Go 1.26 |
| **Size** | ~98k LOC non-test (`cmd` + `internal`, of which ~7k sqlc-generated) · 68 internal packages |
| **Tests** | ~143k LOC — table-driven · property-based · fuzz · `-race` · `goleak` · mutation |
| **Test coverage** | owned-surface floor **≥85% per package**, enforced in CI on every push |
| **CI** | green — build/vet/lint · CodeQL · integration (Postgres / Neo4j) |
| **Persistence** | Postgres (40 migrations, sqlc, pgx) + Neo4j (HNSW 1024-d + APOC + GDS) |
| **Default LLM** | DeepSeek-V4 via OpenRouter — provider-neutral, swap by config |
| **Status** | v0.0.0 substrate + v1.0.0 web cockpit shipped · v2.0.0 industrial hardening in progress (8/12 phases) |

## Key features

- **Streaming agent loop** with a shared *budget tree* (step + wall-clock caps) and a tool-loop *dedup ring* — bounded, predictable cost.
- **Deferred-tool pattern + semantic `tool_search`** — dozens of tools (incl. dynamic MCP tools) stay discoverable at near-zero per-turn token cost.
- **Adaptive reasoning router** — a local curated-seed embedding classifier picks reasoning effort in ~10 ms.
- **Full host terminal + filesystem tools** — real operating power, with destructive-command approval gates and secret redaction.
- **Graph-native memory** — documents become a searchable Neo4j graph (FTS + HNSW vectors); conversations persist with a context-management ladder.
- **Self-extension** — the agent authors and runs its own skills, and mounts MCP servers (calculator, calendar, whatsapp, memory).
- **Multi-channel** — CLI REPL, Telegram (voice/photo/docs/HITL), and a web cockpit over AG-UI/SSE.

## Architecture (one screen)

```text
Transport & UX     cmd/aura (CLI) · channels (+telegram) · agui (SSE) · setup · askuser
Agent runtime      agent (LlmAgent, Budget, Events, hooks) · workflow (Seq/Par/Loop) · swarm
Tools & MCP        agent/tools (registry, deferred, tool_search, fs/shell/web/skill) · mcp (+bridge, manager)
Intelligence       llm (+openai_compat) · semindex (embed-index core) · reasoningtrace · adaptive · scoring
Capabilities       web · skills · cron · onboarding · documents · eval
Persistence        db (Postgres+sqlc) · knowledge (Neo4j) · conversations · identity · profile · secret
Observability      obs · panicobs · reasoningtrace · toolinvocations · cachemetrics
```

## Documentation

| Doc | For |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | How the system is built — layers, turn lifecycle, invariants |
| [docs/TECHNICAL_OVERVIEW.md](docs/TECHNICAL_OVERVIEW.md) | CTO / due-diligence overview — problem, differentiators, maturity |
| [docs/CAPABILITIES.md](docs/CAPABILITIES.md) | Capability matrix — shipped / in-progress / roadmap |
| [.planning/codebase/](.planning/codebase/) | Package-level inventory + conventions + concerns — generated, refresh with `/gsd-map-codebase` |
| [CLAUDE.md](CLAUDE.md) · [prd.md](prd.md) | Engineering guidance · product requirements (source of truth) |

---

## Deployment (Docker Compose appliance)

Aura is a self-hosted agent runtime packaged as a Docker Compose appliance. The
default stack brings up Aura, Postgres, Neo4j, the local embedding sidecar, Caddy
TLS/token access, and optional MCP siblings.

## Quick Start

### Linux or macOS

Install Docker, then run the installer from a release tag:

```bash
curl -fsSL https://raw.githubusercontent.com/chetto1983/Aura/vX.Y.Z/scripts/install.sh | bash
```

The installer checks hardware, creates `.env` with generated `POSTGRES_PASSWORD`,
`NEO4J_PASSWORD`, and `AURA_ACCESS_TOKEN`, downloads the Compose/Caddy assets, and
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
NEO4J_USER=neo4j
NEO4J_PASSWORD=$(New-Hex)
AURA_NEO4J_DATABASE=neo4j
AURA_IMAGE=ghcr.io/chetto1983/aura:vX.Y.Z
AURA_ACCESS_TOKEN=$(New-Hex)
AURA_BACKUP_DIR=./backups
AURA_EMBED_IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda
AURA_EMBED_HF_REPO=Qwen/Qwen3-Embedding-0.6B-GGUF
AURA_EMBED_HF_FILE=Qwen3-Embedding-0.6B-Q8_0.gguf
AURA_EMBED_POOLING=last
AURA_EMBED_NGL=99
AURA_EMBED_DIMENSIONS=1024
AURA_RERANK_IMAGE=ghcr.io/ggml-org/llama.cpp:server-cuda
AURA_RERANK_NGL=99
OPENROUTER_API_KEY=
"@ | Set-Content -Path .env -Encoding ascii

docker run --rm --gpus all nvidia/cuda:12.8.0-base-ubuntu24.04 nvidia-smi
docker compose up -d
```

Aura's local embedding and rerank sidecars require Docker GPU passthrough. Fix
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
one-shot runs Postgres and Neo4j migrations before the Aura service starts:

```bash
docker compose pull
docker compose up -d
```

## Backup And Restore

Scheduled backups run inside the socketless Aura box. Postgres is dumped over the
Compose network with `pg_dump`, and Neo4j is exported over Bolt with APOC into
`AURA_BACKUP_DIR`:

```text
./backups/postgres-YYYYMMDDTHHMMSSZ.dump
./backups/neo4j-YYYYMMDDTHHMMSSZ.cypher
```

Run the restore drill against the current Compose stack:

```bash
set -a
. ./.env
set +a
scripts/restore_drill.sh
```

The drill restores Postgres into a temporary `aura_restore_drill` database and
feeds the latest Neo4j `.cypher` backup through `cypher-shell`.

Manual restore commands:

```bash
docker compose exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres \
  pg_restore -U "${POSTGRES_USER:-aura}" -d "${POSTGRES_DB:-aura}" \
  --clean --if-exists --no-owner --no-acl /backups/postgres-YYYYMMDDTHHMMSSZ.dump

docker compose exec -T neo4j \
  cypher-shell -a bolt://neo4j:7687 -u "${NEO4J_USER:-neo4j}" -p "$NEO4J_PASSWORD" \
  -d "${AURA_NEO4J_DATABASE:-neo4j}" "MATCH (n) DETACH DELETE n;"

docker compose exec -T neo4j \
  cypher-shell -a bolt://neo4j:7687 -u "${NEO4J_USER:-neo4j}" -p "$NEO4J_PASSWORD" \
  -d "${AURA_NEO4J_DATABASE:-neo4j}" < ./backups/neo4j-YYYYMMDDTHHMMSSZ.cypher
```

Neo4j Community has a single writable database in this deployment, so the Neo4j
restore command rebuilds that graph. Take a fresh backup before restoring over a
live graph.

## Optional WhatsApp MCP

The `whatsapp` service is an optional sibling mounted through Aura's MCP catalog.
It uses an unofficial whatsmeow-based client, so it carries WhatsApp Terms of Service and account-ban risk. First pairing is headless:

```bash
docker compose logs -f whatsapp
```

Scan the QR code shown in the logs. Aura boot never depends on this service.

## Retired Host Setup

The host no longer needs a separate `pip install mcp-neo4j-cypher==0.6.0`; the
Neo4j MCP runtime is built into the Aura image. Old host-level Python installs and
the earlier WSL WhatsApp MCP install can be removed after migrating to the Compose
appliance.

## CLI

```text
aura serve              run the long-lived agent runtime
aura shell              interactive REPL against the agent loop
aura agent dry-run      drive a mock LoopAgent through the Budget tree
aura tools              print the tool manifest
aura mcp <sub>          managed MCP servers: install | add | list | doctor | tools | enable | disable | remove
aura db <sub>           Postgres lifecycle: migrate | ping | status | reset
aura neo4j <sub>        Neo4j lifecycle: migrate | ping | status | reset | cypher
aura version            build metadata
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
make neo4j-migrate
go run ./cmd/aura version
go run ./cmd/aura agent dry-run --request-id auto
```

Quality gates:

```bash
make quality
make neo4j-migrate
make quality-full
```

| Target | Does |
|--------|------|
| `make tools` | install the quality toolchain |
| `make lint` / `make vet` | lint and `go vet` |
| `make vuln` | `govulncheck` supply-chain scan |
| `make test-race` | `go test -race ./...` |
| `make coverage` | owned-surface coverage floor |
| `make smoke` | retrieval smoke |
| `make restore-drill` | Postgres plus Neo4j restore drill |

## Project Layout

```text
cmd/aura/                CLI entry and subcommands
internal/agent/          Agent interface, Event, Budget tree, workflow agents
internal/db/             Postgres: pgx pool, golang-migrate, sqlc bindings
internal/knowledge/      Neo4j MCP client, Cypher migrations, embedding ping
internal/config/         environment to typed config
internal/canonicaljson/  deterministic JSON for dedup fingerprints
scripts/                 install, smoke, restore drill, coverage, file-size cap
.planning/               GSD planning artifacts
```

## Scope

Aura is PRD-first. Persistence is Postgres plus Neo4j HNSW, with graph access
through MCP for model-facing tools. The default packaged deployment keeps Aura
socketless: no Docker socket is mounted into the Aura container.

## Contributing And Security

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Report vulnerabilities privately per
[`SECURITY.md`](SECURITY.md), never in a public issue.

## License

[MIT](LICENSE) Copyright 2026 Davide Marchetto.
