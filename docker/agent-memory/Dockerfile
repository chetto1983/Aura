# Aura agent-memory MCP sidecar — the Neo4j Labs `neo4j-agent-memory` engine
# (3-tier POLE+O memory over Neo4j, streamable-HTTP MCP) built from Aura's
# vendored, modified fork instead of the hand-built `:spike-fixed` image.
#
# Fork baseline: https://github.com/chetto1983/agent-memory at
# c1c2d6585fea4f2e562c17ac78d4c528e40e0f62. The image revision label records
# the exact Aura revision containing the vendored source and later modifications.
#
# WHY vendor + install from src/, NEVER `pip install neo4j-agent-memory` from PyPI
# (Pitfall 5): the provenance-safe-dedup fix (commit c1c2d65 — +165 lines in
# memory/long_term.py + 3 schema indexes, the `_deduplication_scope` logic) is
# FORK-INTERNAL and not upstreamed. A PyPI install would silently drop the fix and
# cross-run entity over-merge would return (spike 034 INVALIDATED state). Installing
# `-e .` from the vendored fork is also strictly safer than a registry name
# resolution (T-15-04-SC): the top-level source is pinned to a known commit.
#
# Per CLAUDE.md "Preserve Docker build cache": do NOT prune builder cache.
#
# Base + install shape mirror docker/markitdown/Dockerfile (the canonical in-repo
# pinned-pip sidecar) and the fork's own deploy/cloudrun/Dockerfile. The compose
# `command:` already drives the full `neo4j-agent-memory mcp serve …` invocation
# (--profile extended --session-strategy persistent --embedding-dimensions
# $AURA_EMBED_DIMENSIONS --no-auto-preferences) — the width is NOT restated here, since
# the "384" this line used to claim had been false since the sidecar moved to 768.
# This image deliberately carries no CMD — it only needs
# the `neo4j-agent-memory` entry point installed.
FROM python:3.11-slim

ARG AURA_AGENT_MEMORY_VENDOR_REV
RUN test -n "$AURA_AGENT_MEMORY_VENDOR_REV"

LABEL org.opencontainers.image.source="https://github.com/chetto1983/aura" \
    org.opencontainers.image.revision="$AURA_AGENT_MEMORY_VENDOR_REV" \
    org.opencontainers.image.licenses="Apache-2.0" \
    io.aura.agent-memory.upstream.source="https://github.com/neo4j-labs/agent-memory" \
    io.aura.agent-memory.upstream.fork="https://github.com/chetto1983/agent-memory" \
    io.aura.agent-memory.upstream.revision="c1c2d6585fea4f2e562c17ac78d4c528e40e0f62"

# gcc is needed for any transitive deps that build native extensions; the fork
# pins requires-python >=3.10 and upstream/cloudrun use python:3.11-slim.
RUN apt-get update && apt-get install -y --no-install-recommends gcc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Preserve the upstream license and Aura attribution in the distributed image.
COPY LICENSE NOTICE /usr/share/licenses/agent-memory/

# Copy package metadata first for layer caching, then the vendored package source.
COPY pyproject.toml README.md README-pypi.md constraints.txt ./
COPY src/ ./src/

# Install the vendored fork with the MCP server + Google + OpenAI-compat provider
# extras (the embedder + OpenRouter LLM both speak the OpenAI wire shape).
# constraints.txt pins every transitive dep (pip freeze of the validated build):
# pyproject.toml uses >= floors, so without -c every rebuild floats the dependency
# set and the build is reproducible in name only (WR-04). Regenerate with:
#   docker run --rm --entrypoint pip aura-agent-memory-mcp:local freeze --exclude-editable
RUN pip install --no-cache-dir -c constraints.txt -e ".[mcp,google,openai]"

# Entity resolution's fuzzy leg. `rapidfuzz` is a soft dependency the library
# swallows when absent — `CompositeResolver.__init__` does `except Exception: pass`
# and `_check_for_duplicates` does `except ImportError: pass` — so without it the
# documented Exact -> Fuzzy -> Semantic chain silently runs as Exact -> Semantic and
# a single cosine threshold becomes the only identity discriminator. Cheap, pure
# Python-ext, no model weights: unlike the spaCy/GLiNER/GLiREL extraction stages it
# costs nothing to carry.
#
# The extraction stages are deliberately NOT installed. They only ever run inside
# `short_term.add_message(extract_entities=...)`, and Aura drives this sidecar as
# long-term memory only — `add_fact`/`add_preference` write directly and never
# extract. Carrying them cost 20GB (torch + three model sets) to serve a code path
# that is never reached. Measured 2026-07-28 before removal, if that decision is ever
# revisited: the full pipeline was 3.5x slower than the LLM stage alone (45.9s vs
# 13.0s on the same sentence) and produced one extra entity — a duplicate, the same
# name under two types, which `match_same_type_only` dedup can never reconcile.
# Re-adding them also means re-adding `it_core_news_sm` plus the WikiNER rows in
# SpacyEntityExtractor.DEFAULT_TYPE_MAPPING (without them every non-English PERSON
# lands as OBJECT), and pinning back to huggingface_hub<1.0 + transformers<5.0
# because glirel 1.2.1 is abandoned and still expects the pre-1.0 ModelHubMixin.
RUN pip install --no-cache-dir "rapidfuzz>=3.0.0"

EXPOSE 8080
