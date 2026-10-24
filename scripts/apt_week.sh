#!/usr/bin/env bash
set -euo pipefail

# The value every builder passes as ARG APT_WEEK to docker/aura and docker/aura-ingest. A changed
# ARG re-runs every RUN after it, so the first build of each ISO week reinstalls what apt has that
# week and the rest of the week reads the cache. The builders must agree: ci.yml reads the cache
# publish-aura-edge.yml writes, and a different value would miss all of it.
date -u +%G-W%V
