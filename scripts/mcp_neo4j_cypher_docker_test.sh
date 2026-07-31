#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
shim="scripts/mcp_neo4j_cypher_docker.sh"

for name in NEO4J_URL NEO4J_USERNAME NEO4J_PASSWORD NEO4J_DATABASE NEO4J_TRANSPORT; do
  if ! grep -Eq -- "-e[[:space:]]+${name}([[:space:]\"']|$)" "$shim"; then
    echo "mcp-neo4j-cypher-docker-test: $name is not forwarded by name" >&2
    exit 1
  fi
  if grep -Eq -- "-e[[:space:]]+${name}=" "$shim"; then
    echo "mcp-neo4j-cypher-docker-test: $name value appears in docker argv" >&2
    exit 1
  fi
done

bash -n "$shim"
echo "mcp-neo4j-cypher-docker-test: pass"
