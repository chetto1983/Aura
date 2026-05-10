<p align="center">
  <img src="Logo/logo.png" alt="Aura logo" width="112">
</p>

<h1 align="center">Aura</h1>

<p align="center">
  A private Telegram second brain that runs as a self-hosted Docker stack.
</p>

<p align="center">
  <a href="https://github.com/chetto1983/Aura/pkgs/container/aura"><img alt="Docker image" src="https://img.shields.io/badge/image-ghcr.io%2Fchetto1983%2Faura-2496ED"></a>
  <a href="docs/container.md"><img alt="Install path" src="https://img.shields.io/badge/install-Docker%20Compose-0B6B50"></a>
  <img alt="Go version" src="https://img.shields.io/badge/Go-1.26.2-00ADD8">
</p>

Aura is a personal assistant you own. It chats through your Telegram bot,
ingests files into a local source inbox, builds a Markdown wiki, searches memory
with embeddings, and gives you a dashboard for sources, tasks, settings,
backups, skills, and health.

The supported install path is Docker Compose. Release tags publish only the
container image at `ghcr.io/chetto1983/aura:<version>`.

## What Runs

The default stack starts:

- `aura`: the Telegram bot, memory engine, tools, and embedded dashboard.
- `aura-secrets`: sidecar that decrypts secrets for Aura at startup.
- `searxng`: local web search for the `web_search` tool.
- `qdrant`: vector-search sidecar; Aura keeps local search as fallback.
- `garage`: S3-compatible artifact and backup storage.
- `garage-webui`: optional Garage admin UI behind a Compose profile.

Primary user data stays in visible folders beside the Compose file:

- `data/`: `.env`, SQLite database, logs.
- `runtime-workspace/`: MCP config, prompt overlays, runtime workspace files.
- Docker volume `aura-wiki`: compiled memory pages and source evidence.
- Docker volume `aura-skills`: installed agent skills.
- `garage/`: Garage object storage data.

Code execution runs Python directly inside the Aura container.

## Quick Start

Prerequisites:

- Docker Desktop or Docker Engine with Docker Compose.
- A Telegram bot token from [@BotFather](https://t.me/BotFather).
- An OpenAI-compatible LLM endpoint, or a local Ollama-compatible endpoint.

```powershell
git clone https://github.com/chetto1983/Aura
cd Aura
New-Item -ItemType Directory -Force data,runtime-workspace,garage | Out-Null
Copy-Item .env.example data/.env
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:latest"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Open the setup wizard at `http://127.0.0.1:18080`.

## Update

```powershell
docker compose -f compose.yaml -f compose.image.yaml pull aura
docker compose -f compose.yaml -f compose.image.yaml up -d
```

Pin a release with `AURA_IMAGE`:

```powershell
$env:AURA_IMAGE = "ghcr.io/chetto1983/aura:v1.2.3"
docker compose -f compose.yaml -f compose.image.yaml up -d
```

## Develop

Build the local working tree instead of pulling GHCR:

```powershell
docker compose up -d --build
```

Run the test suite:

```powershell
docker compose --profile test run --rm test
```

Local Go commands:

```powershell
go test ./...
go build ./...
go run ./cmd/aura
```

## Release

```powershell
git tag v1.2.3
git push origin v1.2.3
```

The `Docker image` workflow publishes:

- `ghcr.io/chetto1983/aura:v1.2.3`
- `ghcr.io/chetto1983/aura:latest`
- a `sha-*` traceability tag

## Docs

- [Install guide](INSTALL.md)
- [Container stack](docs/container.md)
- [Product requirements](prd.md)
