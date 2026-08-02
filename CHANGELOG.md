# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
once it reaches its first tagged release. The project is pre-1.0 and under active
development on the `tabula-rasa` branch; nothing is API-stable yet.

## [Unreleased]

### Added
- `aura version` command — prints build metadata (version/commit/date), stamped
  by goreleaser ldflags with a runtime/debug build-info fallback.
- Industrial quality gates: coverage floor (≥85% owned surface), `govulncheck`
  supply-chain scan, `dupl` and `gofmt` in golangci-lint, `make quality` /
  `quality-full` / `tools` targets.
- CI: CodeQL analysis, Dependabot (gomod + actions), goreleaser release pipeline.
- Repo governance: MIT `LICENSE`, `SECURITY.md`, `CONTRIBUTING.md`, `CODEOWNERS`,
  issue/PR templates, `.editorconfig`, `.gitattributes`, lefthook git hooks.
- Phase 1 (infra-db-knowledge): Postgres (sqlc + pgx + golang-migrate) and Neo4j
  (mcp-neo4j-cypher + Cypher migrations + embedding sidecar) with integration +
  smoke tiers.
- Phase 2 (agent-cornerstone): open `Agent` interface, Sequential/Loop/Parallel
  workflow agents, shared-atomic Budget tree with dedup, UUIDv7 `request_id`.

### Removed
- Neo4j, and the chunk-retrieval document plane built on it. The graph driver,
  the `neo4j` Compose service, `mcp-neo4j-cypher`, `internal/knowledge` with its
  Cypher migration sequence, the `docker/agent-memory` sidecar, the
  `neo4j_integration` build tag and the `make neo4j-*` targets are all gone.
  Long-term memory moved to ArcadeDB — one database per identity, server-enforced,
  bitemporal facts, served by Aura's own `cmd/arcadedb-mcp`. Documents moved to
  Postgres as one catalog row + digest per file: `document_search` says *which*
  file and `document_open` hands the agent the real file to compute on, because
  the passage pipeline it replaces scored 0% on every aggregate question at every
  k. The cockpit's graph explorer is schema-only as a result — the traversal
  compilers that drew the canvas did not survive the move.

### Changed
- WSL is now the full primary dev environment (CGO + make + native `-race` +
  container integration).

### Fixed
- `pingEmbed` HTTP idle-connection goroutine leak (order-dependent goleak).
- Line-ending normalization (LF) ends the Windows CRLF churn.

[Unreleased]: https://github.com/chetto1983/Aura/commits/tabula-rasa
