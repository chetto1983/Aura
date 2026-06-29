# webbuild stage (D-06): build the operator SPA with Node 24 in a discardable stage
# so NO Node is added to the runtime image for the SPA's sake. The build is fed into
# the go build stage below as a fresh, reproducible dist — the web-dist-freshness CI
# job proves the committed bytes equal this build, tying D-05 <-> D-06.
#
# Output path note: web/vite.config.ts sets build.outDir = ../internal/webui/dist (Go
# //go:embed is package-relative — 23-01/23-02). At WORKDIR /web, `..` is /, so
# `npm run build` writes to /internal/webui/dist (NOT /web/dist, which is never created).
FROM node:24-bookworm-slim AS webbuild
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# The web TypeScript build includes src/__tests__; sseAdapter tests import the
# captured AG-UI fixture from the repo root. Copy only that fixture into the path
# the relative import resolves to inside this stage.
COPY internal/agui/testdata/golden-events.json /internal/agui/testdata/golden-events.json
RUN npm run build

FROM dxflrs/garage:v2.0.0 AS garagebin

FROM golang:1.26.4-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Overwrite the committed embed source (internal/webui/dist) with the fresh webbuild
# output so the image's baked-in SPA is a reproducible build, not the committed bytes.
COPY --from=webbuild /internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aura ./cmd/aura

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        curl \
        git \
        gnupg \
        python3 \
        python3-pip \
        python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Node 24 LTS (Debian bookworm ships Node 18, too old). NodeSource nodejs bundles
# npm/npx. Runtime consumers: (1) JS skill snippets — internal/skills/snippet.go maps
# LangJS to the `node` interpreter (Slice 7e), and (2) the `npx skills find/add`
# self-extension loop the agent drives on the host terminal (find-skills-aura).
#
# D-06 note: this runtime Node-24 is ORTHOGONAL to the SPA. The operator SPA is built
# in the `webbuild` stage above and baked into the Go binary before runtime — no Node
# touches the SPA at runtime. Do not read this as a D-06 "no Node for the SPA" violation.
RUN curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

RUN install -d /usr/share/postgresql-common/pgdg \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
        | gpg --dearmor -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg \
    && echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
        > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends postgresql-client-18 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:0.11.21 /uv /uvx /usr/local/bin/

RUN pip install --no-cache-dir --break-system-packages mcp-neo4j-cypher==0.6.0

# calculator: warm uvx at a pinned commit (chetto1983 fork) so it runs with egress
# blocked — an unpinned HEAD re-fetches and fails offline (Phase 17 AC7).
RUN uvx --from "calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git@46a1e66709bc387e8c223f15ec25fb5ae3a1af08" \
        calculator-mcp-server --help >/dev/null

# mail recipe retired: the calendar PIM sidecar (forked calendar-mcp →
# chetto1983/aura-pim-mcp, a separate compose service) now provides email
# (get/search/send) over MCP, so the standalone vendored mail-mcp bin is no longer
# baked into this image.

ENV AURA_IN_CONTAINER=1

COPY --from=build /out/aura /usr/local/bin/aura
COPY --from=garagebin /garage /usr/local/bin/garage
COPY docker/aura/aura-garage-bootstrap.sh /usr/local/bin/aura-garage-bootstrap.sh
RUN chmod +x /usr/local/bin/aura-garage-bootstrap.sh

ENTRYPOINT ["aura"]
CMD ["serve"]
