# syntax=docker/dockerfile:1.7
# Build context is the repo root: the server shares internal/arcadedb with Aura,
# so it builds from the same module rather than vendoring a second copy.
#
FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY cmd/arcadedb-mcp ./cmd/arcadedb-mcp
COPY internal/arcadedb ./internal/arcadedb

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
