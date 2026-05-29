#!/usr/bin/env bash
# ROADMAP Phase 1 SC#5: recall@5 = 5/5 + p95 vector-search latency <= 30ms on the
# deterministic Italian 5-doc fixture corpus.
#
# Delegates to the Go acceptance harness internal/knowledge/smoke_test.go (build
# tag `smoke`): it holds ONE persistent driver connection and reads the
# server-side query time (ResultSummary.ResultAvailableAfter) — the real HNSW
# search latency. The original bash+jq approach timed `aura neo4j cypher` per
# query, which measured Go/MCP process startup + bolt connect (~seconds), not the
# search, so p95 <= 30ms was unmeasurable. This wrapper needs no jq.
set -euo pipefail

# Resolve the password the live stack actually uses (authoritative: the running
# container's NEO4J_AUTH) unless the operator already exported NEO4J_PASSWORD.
if [[ -z "${NEO4J_PASSWORD:-}" ]] && command -v docker >/dev/null 2>&1; then
  NEO4J_PASSWORD=$(docker inspect aura-neo4j --format '{{range .Config.Env}}{{println .}}{{end}}' 2>/dev/null \
    | grep '^NEO4J_AUTH=' | sed 's|^NEO4J_AUTH=neo4j/||' | tr -d '\r' || true)
  export NEO4J_PASSWORD
fi

exec go test -tags smoke -run '^TestKnowledgeSmoke$' ./internal/knowledge/... -count=1 -v
