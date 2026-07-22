# Containerized mcp-neo4j-cypher — the LLM graph interface (stdio JSON-RPC subprocess
# spawned per graph-client by internal/knowledge/client.go). Built so the coverage /
# neo4j_integration tiers can run with the MCP binary in a container instead of a host
# install (scripts/coverage_docker.sh + the wrapper shim). Pinned to 0.6.0 for parity
# with CI (.github/workflows/ci.yml installs the same version via pipx) and CLAUDE.md.
# FastMCP is a direct runtime dependency with a wide upstream range; pin the exact
# handshake-tested release so rebuilding this image cannot silently change stdio framing.
FROM python:3.12-slim
RUN pip install --no-cache-dir mcp-neo4j-cypher==0.6.0 fastmcp==2.13.3
ENTRYPOINT ["mcp-neo4j-cypher"]
