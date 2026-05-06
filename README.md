# Aura

Aura is a Go-based Telegram assistant with LLM integrations, local wiki storage, search, budget tracking, health endpoints, logging, and optional tracing.

## Install (end users)

**No Go, Node, or build tools required.** Download a pre-built binary for your OS and follow [INSTALL.md](INSTALL.md). Takes ~15 minutes including creating your Telegram bot.

| OS                    | Release archive                         | Extracted binary |
| --------------------- | --------------------------------------- | ---------------- |
| Windows               | `aura_<version>_windows_x86_64.zip`     | `aura.exe`       |
| macOS (Intel)         | `aura_<version>_darwin_x86_64.tar.gz`   | `aura`           |
| macOS (Apple Silicon) | `aura_<version>_darwin_arm64.tar.gz`    | `aura`           |
| Linux (x86_64)        | `aura_<version>_linux_x86_64.tar.gz`    | `aura`           |
| Linux (ARM64)         | `aura_<version>_linux_arm64.tar.gz`     | `aura`           |

## Build from source (developers)

- Go 1.25.5 or newer matching `go.mod`
- Node 20+ for the web dashboard
- A Telegram bot token
- At least one allowlisted Telegram user ID
- Optional OpenAI-compatible LLM and embedding API credentials

## Setup

Create a local environment file from the template:

```powershell
Copy-Item .env.example .env
```

Then fill in the required values:

- `TELEGRAM_TOKEN`
- `TELEGRAM_ALLOWLIST`

Optional LLM settings can point to any OpenAI-compatible API, including OpenAI, OpenRouter, Mistral, Groq, DeepSeek, Together, or local Ollama.

## Common Commands

```powershell
go test ./...
go build ./...
go run ./cmd/aura
go run ./cmd/debug_llm
```

The same commands are available through `make`:

```powershell
make test
make build
make run
make debug-llm
```

## Container Stack

For server-style installs, Aura ships a Docker Compose stack with Aura running
headless and SearXNG running beside it:

```powershell
New-Item -ItemType Directory -Force data,wiki,skills,garage | Out-Null
Copy-Item .env.example data/.env
docker compose up -d --build
```

Open <http://127.0.0.1:8080>. SearXNG is exposed on
<http://127.0.0.1:8088>. See [docs/container.md](docs/container.md).

## Runtime Data

The repo ignores generated runtime files such as `.env`, `aura.db`, and built binaries. Wiki raw data is also ignored by default, while schema and documentation files stay tracked.

## Health

Aura starts an HTTP health and observability server on `HTTP_PORT`, defaulting to `:8080`.
