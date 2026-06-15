# Slice 0.7 Execution Summary (Plan 03 — Phase 1 close)

**Commit:** `e0122d0c` — `slice 0.7: neo4j community + mcp-neo4j-cypher + cypher migrations` (33 files, +2312/-28).
**Executed:** 2026-05-29, interactive-inline (non-autonomous plan with interleaved human gates).

## Artifacts created (LOC actual)

| File | LOC | Notes |
|---|---|---|
| `internal/knowledge/client.go` | ~290 | MCP stdio JSON-RPC client + `initialize` handshake + `decodeRows` + `redactSecrets` + `safeBuffer` |
| `internal/knowledge/schema.go` | ~70 | **NEW (deviation)** driver-backed auto-commit DDL executor |
| `internal/knowledge/migrate.go` | ~155 | Postgres-audited Cypher runner (sqlc), `loadMigrations(fs.FS)` injectable |
| `internal/knowledge/ping.go` | ~115 | `pingMCP` (dbms.components) + `pingEmbed` (Pattern 5 dim self-test) |
| `internal/knowledge/{config,status,reset}.go` | ~22/40/40 | |
| `internal/knowledge/migrations/0001_init.cypher` | ~23 | D-08: chunk_id constraint + HNSW 768d M=32 ef=200 cosine + fulltext |
| `internal/knowledge/{client_unit,client_paths,client,smoke}_test.go` | ~160/350/300/210 | B1 split: unit (no tag) + integration (`neo4j_integration && db_integration`) + smoke (`smoke`) |
| `cmd/aura/neo4j.go` | ~205 | `runNeo4j` dispatch: migrate\|ping\|status\|reset\|cypher |
| `scripts/neo4j_smoke.sh` | ~25 | thin wrapper -> Go acceptance harness |
| 5 IT fixtures + queries.txt | ~30 each | amatriciana, duomo, panda, nome-della-rosa, espresso |
| compose.yaml / Makefile / .env.example | extended | neo4j + aura-llama-embed services + neo4j-*/smoke targets + keys |

## Test results

- `golangci-lint` (v2.12.2, default + all tags): **0 issues**.
- unit `-race ./...`: PASS (config, db, knowledge). Integration (`neo4j_integration db_integration`, race): all PASS. Smoke (`smoke`): PASS.
- **Coverage internal/knowledge: 94.1%** (PRD Gate 3: 75 unit / 60 integration — exceeded). Remaining ~6% is provably-unreachable defensive code (json.Marshal of fixed structs, embed.FS errors on compile-time-embedded files, OS pipe-creation failures); user accepted 94.1% over coverage-theater mock-injection.
- **`make smoke`: recall@5 = 5/5, p95 = 1 ms** (ROADMAP SC#5). Note: p95 measured via persistent driver `ResultSummary.ResultAvailableAfter` (server-side HNSW latency), with a 5-query warm-up to exclude cold-start. `TestPingEmbed_Live` depends on first-boot HF model download (the embed sidecar reached healthy in ~1 min on this host).

## T-1.07-SC license evidence (blocking-human APPROVED)

- `gh api repos/neo4j-contrib/mcp-neo4j` -> `.license.spdx_id = "MIT"`, `.license.name = "MIT License"`.
- `LICENSE.txt` first line: `MIT License`. `pip show mcp-neo4j-cypher` -> `0.6.0`.
- MIT is permissive (OWASP-acceptable) -> approved.

## Wave 0 MCP JSON-RPC envelope (Assumption A10)

```
initialize -> serverInfo {"name":"mcp-neo4j-cypher","version":"1.26.0"}
tools/list -> read_neo4j_cypher / write_neo4j_cypher / get_neo4j_schema (args: {query, params})
tools/call read_neo4j_cypher "RETURN 1 AS one" ->
  {"result":{"content":[{"type":"text","text":"[{\"one\": 1}]"}],"isError":false}}
```
The server **requires** the `initialize` + `notifications/initialized` handshake before `tools/call` (added to `client.go`). `decodeRows` unwraps `content[0].text` as the JSON row array — matched, no decoder change.

## D-05 + Pattern 5 amendments (line-level)

- `prd.md:182` — `aura knowledge ping ... /health returns {"dim":<int>}` -> `aura neo4j ping ... /v1/embeddings round-trip returns 768d (Pattern 5 dim probe)`.
- `.planning/ROADMAP.md:61` — Phase 1 SC#4 `aura knowledge ping` -> `aura neo4j ping` + Pattern 5 correction.
- D-02 carry-forward: `prd.md` `sandbox/compose.yaml` -> `compose.yaml` (x7); `grep -c 'sandbox/compose.yaml' prd.md` = 0.

## Deviations from 03-PLAN.md

1. **Schema migrations via neo4j-go-driver/v5 (auto-commit), not MCP.** Architectural gap found at Gate C: mcp-neo4j-cypher v1.26.0 wraps writes in an explicit managed tx, and Neo4j forbids schema DDL inside an explicit tx ("Only write queries are allowed for write-query"). Resolution = CLAUDE.md-sanctioned "Go driver native (fallback se mcp-neo4j-cypher non sufficiente)"; MCP stays the LLM data/query runtime. Surfaced to operator + chosen after online research (golang-migrate neo4j driver + Neo4j-Migrations both use the driver auto-commit). migrate.go/reset.go/neo4j.go + integration tests switched from MCP to SchemaExecutor.
2. **smoke harness rewritten in Go** (build tag `smoke`): bash+jq timed `aura neo4j cypher` per query (process+MCP-spawn+bolt connect ~seconds) -> p95<=30ms unmeasurable; also jq was missing. Go harness uses a persistent connection + server-side timing.
3. **config Load tests in `internal/config/config_test.go`** (not `internal/knowledge`) — config imports knowledge, so a knowledge test calling Load would be an import cycle.
4. **mcp-neo4j-cypher version**: pip metadata reports 0.6.0 but the running server reports 1.26.0 (serverInfo). License (MIT) holds either way.
5. **Infra fixes (this session):** `.claude/settings.json` hooks `/opt/node22/bin/node` -> `node` (broken Linux path); compose neo4j healthcheck reads the password via `AURA_NEO4J_HC_PW` (NEO4J_AUTH is consumed by the entrypoint, not left in the shell env for cypher-shell); `.env.example` comments moved to own lines (godotenv does not reliably strip inline comments); `go env -w CC` + `~/.aura-toolchain.sh` BASH_ENV PATH-front fix so `-race`/cgo works (HMITool binutils 2.21 was shadowing w64devkit); jq installed to `~/go/bin`.

## Phase 1 closure

All 5 ROADMAP Phase 1 Success Criteria green:
- SC#1 idempotent `aura db migrate` ✓ (Slice 0.5)
- SC#2 role separation ✓ (Slice 0.5)
- SC#3 restore drill < 90s ✓ (Slice 0.5)
- SC#4 `aura neo4j ping` -> Neo4j 5.26.26 + sidecar dim 768 (Pattern 5) ✓ (Slice 0.7)
- SC#5 Italian smoke recall@5 = 5/5 + p95 = 1 ms ✓ (Slice 0.7)

**Next:** Phase 2 (Agent Cornerstone) per ROADMAP execution order.
