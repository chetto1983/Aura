# syntax=docker/dockerfile:1.7
# Build context is the repo root: the server shares internal/arcadedb with Aura,
# so it builds from the same module rather than vendoring a second copy.
#
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
# proxy.golang.org drops a stream mid-fetch often enough to redden a whole CI run on a
# build that has nothing wrong with it: measured 2026-09-01 on Actions run 33552817871,
# `stream error: stream ID 323; INTERNAL_ERROR; received from peer` on one module .mod
# file, which killed the Agent Memory job before a single test body ran. Three tries with
# a widening pause: a transport blip is ridden out, a module that genuinely is not there
# still stops the build on the third. Every branch is `if`-guarded because under `set -e`
# a bare `[ ... ] && ...` whose test is false exits the shell with the test's status.
RUN --mount=type=cache,target=/go/pkg/mod set -e; \
    for attempt in 1 2 3; do \
      if go mod download; then break; fi; \
      if [ "$attempt" = 3 ]; then echo "go mod download failed after 3 attempts" >&2; exit 1; fi; \
      echo "go mod download failed (attempt $attempt); retrying" >&2; \
      sleep $((attempt * 5)); \
    done

COPY cmd/arcadedb-mcp ./cmd/arcadedb-mcp
# The whole internal tree, not an enumerated subset. internal/arcadedb gained an
# import of internal/embeddings, which imports internal/config, and `go list -deps`
# now reports 19 internal packages behind this binary (db, llm, mcp, conversations,
# …). Listing them individually is what broke this build: the import landed and the
# COPY list did not follow. A subset here fails again on the next one.
COPY internal ./internal

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/arcadedb-mcp ./cmd/arcadedb-mcp

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 nonroot \
    && adduser -S -D -H -u 65532 -G nonroot nonroot

COPY --from=build /out/arcadedb-mcp /usr/local/bin/arcadedb-mcp

USER 65532:65532
EXPOSE 8096
ENTRYPOINT ["/usr/local/bin/arcadedb-mcp"]
