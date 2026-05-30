# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project aims to follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
once it reaches its first tagged release. The project is pre-1.0 and under active
development on the `tabula-rasa` branch; nothing is API-stable yet.

## [Unreleased]

### Added
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

### Changed
- WSL is now the full primary dev environment (CGO + make + native `-race` +
  container integration).

### Fixed
- `pingEmbed` HTTP idle-connection goroutine leak (order-dependent goleak).
- Line-ending normalization (LF) ends the Windows CRLF churn.

[Unreleased]: https://github.com/chetto1983/Aura/commits/tabula-rasa
