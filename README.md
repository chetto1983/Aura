# Aura

Aura is a Go-based Telegram assistant with LLM integrations, local wiki storage, search, budget tracking, health endpoints, logging, and optional tracing.

## Install (end users)

**No Go, Node, or build tools required.** Download a pre-built binary for your OS and follow [INSTALL.md](INSTALL.md). Takes ~15 minutes including creating your Telegram bot.

| OS                    | Binary                     |
| --------------------- | -------------------------- |
| Windows               | `aura_windows_amd64.exe`   |
| macOS (Intel)         | `aura_darwin_amd64`        |
| macOS (Apple Silicon) | `aura_darwin_arm64`        |
| Linux (x86_64)        | `aura_linux_amd64`         |
| Linux (ARM64)         | `aura_linux_arm64`         |

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

Optional LLM settings can point to OpenAI-compatible APIs or local Ollama, depending on the provider path you want to use.

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

## Runtime Data

The repo ignores generated runtime files such as `.env`, `aura.db`, and built binaries. Wiki raw data is also ignored by default, while schema and documentation files stay tracked.

## Health

Aura starts an HTTP health and observability server on `HTTP_PORT`, defaulting to `:8080`.
