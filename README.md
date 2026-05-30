# Aura

[![CI](https://github.com/chetto1983/Aura/actions/workflows/ci.yml/badge.svg?branch=tabula-rasa)](https://github.com/chetto1983/Aura/actions/workflows/ci.yml)
[![CodeQL](https://github.com/chetto1983/Aura/actions/workflows/codeql.yml/badge.svg?branch=tabula-rasa)](https://github.com/chetto1983/Aura/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go)](go.mod)

Self-hosted agent runtime. Tabula-rasa rewrite (2026-05-27).

## Scope

Four concentric components, nothing else:

1. **Agent loop** — streaming LLM, deferred-tool dispatch, bounded iterations.
2. **KV cache** — provider-aware prompt caching (DeepSeek auto, Anthropic ephemeral). Stable-prefix discipline; zero `messages[0]` mutation.
3. **Sandbox** — Python + shell execution in container-isolated workers (seccomp, ulimit, net-deny by default).
4. **Swarm** — parallel agents in a controlled loop with peer-to-peer talk (tier model, `MAX_SPAWN_DEPTH=3`, shared bus + DM-by-ID).

Persistence: Postgres (`aura.*` schema) + Neo4j via `mcp-neo4j-cypher` (MCP stdio) — the model talks to the graph through MCP exclusively, no native Go adapter.

## Quick start

**Prerequisites:** Go (see [`go.mod`](go.mod)), Docker, a POSIX shell. On Windows, **WSL** is recommended — it runs the whole stack and test matrix natively (see [development](#development)).

```bash
git clone https://github.com/chetto1983/Aura.git && cd Aura
cp .env.example .env             # set POSTGRES_PASSWORD / NEO4J_PASSWORD
make tools && lefthook install   # quality toolchain + git hooks (one-time)

make neo4j-migrate               # up: Postgres + Neo4j + embed sidecar, run migrations
go run ./cmd/aura version        # build metadata
go run ./cmd/aura agent dry-run --request-id auto   # stream Events through the Budget tree
```

### CLI

```
aura serve              run the long-lived agent runtime (production default)
aura shell              interactive REPL against the agent loop
aura agent dry-run      drive a mock LoopAgent through the Budget tree (one Event per JSON line)
aura tools              print the tool manifest (active + deferred)
aura db <sub>           Postgres lifecycle:  migrate | ping | status | reset
aura neo4j <sub>        Neo4j lifecycle:     migrate | ping | status | reset | cypher
aura version            build metadata (version, commit, date)
```

(`serve` / `shell` are stubbed until their slices land — see [status](#status).)

## Development

The repo ships an industrial quality gate. The same checks run in CI and locally:

```bash
make quality        # vet · build · file-size(≤600 LOC) · lint(+dupl,gofmt) · test-race · vuln  — no containers
make neo4j-migrate  # stack up
make quality-full   # quality + coverage floor (owned surface ≥ 85%)
```

| Target | Does |
|--------|------|
| `make tools` | install the toolchain (golangci-lint, govulncheck, dupl, lefthook, go-mutesting, …) |
| `make lint` / `make vet` | golangci-lint (errcheck, staticcheck, gosec, revive, dupl, gofmt) / `go vet` |
| `make vuln` | `govulncheck` supply-chain scan |
| `make test-race` | `go test -race ./...` |
| `make coverage` | owned-surface coverage floor ≥ 85% (needs the stack up) |
| `make smoke` | Italian retrieval smoke — recall@5 = 5/5, p95 reported |
| `make restore-drill` | `pg_dump` → `pg_restore` under 90s |

Git hooks (via [lefthook](https://lefthook.dev)) run gofmt/vet/file-size on commit and lint/build on push. **WSL** is a full dev environment (gcc + make + CGO + the Docker stack via `127.0.0.1`); see [`CLAUDE.md`](CLAUDE.md) §Quality tooling & gates for the cross-environment matrix and toolchain locations.

## Project layout

```
cmd/aura/                CLI entry + subcommands (db, neo4j, agent, version)
internal/agent/          Agent interface, Event, Budget tree, workflow agents (Sequential/Loop/Parallel)
internal/db/             Postgres: pgx pool, golang-migrate, sqlc bindings, redaction
internal/knowledge/      Neo4j: MCP client, Cypher migrations, embedding ping, smoke
internal/config/         env → typed config
internal/canonicaljson/  deterministic JSON (RFC-8785-like) for dedup fingerprints
scripts/                 smoke, restore drill, coverage gate, file-size cap
.planning/               GSD planning artifacts (PRD-derived; not part of code review)
```

## Architecture & process

Aura is built **PRD-first**: [`prd.md`](prd.md) is the source of truth and [`CLAUDE.md`](CLAUDE.md) the working contract. Work proceeds in numbered slices through a phased GSD workflow (spec → discuss → plan → execute → verify → review). Persistence is Postgres `aura.*` + Neo4j HNSW (768d cosine).

## What's deliberately not here

- Telegram bot (optional plugin binary, separate concern), web dashboard, setup wizard/tray
- Wiki `.md` filesystem with git tracking; FTS5 / Qdrant / in-memory graph index
- OCR / markitdown / whisper ingestion (return later as Neo4j-MCP-mediated tools)

History of the prior implementation is preserved at git tag `pre-rewrite-2026-05-27`.

## Status

Bootstrap. Phase 1 (Postgres + Neo4j infra) and Phase 2 (agent cornerstone: `Agent` interface + workflow agents + Budget tree) are in. See [`CHANGELOG.md`](CHANGELOG.md).

## Contributing & security

See [`CONTRIBUTING.md`](CONTRIBUTING.md). Report vulnerabilities privately per [`SECURITY.md`](SECURITY.md) — never in a public issue.

## License

[MIT](LICENSE) © 2026 Davide Marchetto.
