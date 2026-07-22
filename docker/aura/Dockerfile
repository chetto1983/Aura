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

# Keep in lockstep with the garage service pin in compose.yaml — the bootstrap
# CLI talks native RPC to the server and Garage wants matched versions.
FROM dxflrs/garage:v2.3.0 AS garagebin

FROM golang:1.26.5-alpine AS build

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

RUN pip install --no-cache-dir --break-system-packages mcp-neo4j-cypher==0.6.0 fastmcp==2.13.3

# calculator: warm uvx at a pinned commit (chetto1983 fork) so it runs with egress
# blocked — an unpinned HEAD re-fetches and fails offline (Phase 17 AC7).
RUN uvx --from "calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git@46a1e66709bc387e8c223f15ec25fb5ae3a1af08" \
        -- calculator-mcp-server --help >/dev/null

# mail recipe retired: the calendar PIM sidecar (forked calendar-mcp →
# chetto1983/aura-pim-mcp, a separate compose service) now provides email
# (get/search/send) over MCP, so the standalone vendored mail-mcp bin is no longer
# baked into this image.

# Amendment #88 Task 2: pre-bake the document/data toolchain so document tasks
# stop reinstalling docx/pandoc on every turn. pandoc converts between document
# formats; file/xxd are used for content sniffing/hex inspection of workspace
# artifacts. Dedicated layer (not folded into the earlier locked apt layers above)
# so this stays independently cacheable and easy to diff/revert.
RUN apt-get update \
    && apt-get install -y --no-install-recommends pandoc file xxd \
    && rm -rf /var/lib/apt/lists/*

# NodeSource Node 24 (installed above) puts global npm modules under
# /usr/lib/node_modules; NODE_PATH must point there so `require('docx')` resolves
# from any CWD (the agent runs JS skill snippets from $AURA_WORKSPACE_DIR, not
# /usr/lib/node_modules).
RUN npm install -g docx
ENV NODE_PATH=/usr/lib/node_modules

# python-docx (import name `docx`), openpyxl and pandas: pre-baked so document/data
# skill snippets don't pip install on every invocation. --break-system-packages is
# required on this Debian image (PEP 668 externally-managed env), matching the
# mcp-neo4j-cypher/fastmcp install above.
RUN pip install --no-cache-dir --break-system-packages python-docx openpyxl pandas

ENV AURA_IN_CONTAINER=1

COPY --from=build /out/aura /usr/local/bin/aura
COPY --from=garagebin /garage /usr/local/bin/garage
COPY docker/aura/aura-garage-bootstrap.sh /usr/local/bin/aura-garage-bootstrap.sh
RUN chmod +x /usr/local/bin/aura-garage-bootstrap.sh

ENTRYPOINT ["aura"]
CMD ["serve"]
