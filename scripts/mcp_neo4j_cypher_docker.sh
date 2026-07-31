#!/usr/bin/env bash
# AURA_MCP_NEO4J_CYPHER_BIN shim: run the CONTAINERIZED mcp-neo4j-cypher instead of a
# host install (CLAUDE.md: never install MCP tooling on the host). internal/knowledge/
# client.go execs this with `--db-url bolt://127.0.0.1:7687 --transport stdio ...`; we
# forward every arg and pipe the JSON-RPC stdio through `docker run -i`. The hardened
# knowledge client supplies credentials through the upstream NEO4J_* environment contract,
# so forward each variable by NAME (`-e NAME`) rather than copying secret values into argv.
# `--network host` lets the container reach the stack's 127.0.0.1-published bolt port
# (WSL/Linux docker). The image is built by scripts/coverage_docker.sh.
set -euo pipefail
exec docker run --rm -i --network host \
  -e NEO4J_URL \
  -e NEO4J_USERNAME \
  -e NEO4J_PASSWORD \
  -e NEO4J_DATABASE \
  -e NEO4J_TRANSPORT \
  "${AURA_MCP_IMAGE:-aura-mcp-neo4j-cypher:local}" "$@"
