FROM golang:1.26.2-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/aura ./cmd/aura

FROM node:22-bookworm-slim AS mcp-node

ARG DATABASE_MCP_VERSION=1.1.0

RUN npm install -g "@executeautomation/database-server@${DATABASE_MCP_VERSION}" \
    && npm cache clean --force \
    && test -x /usr/local/bin/ea-database-server

FROM debian:bookworm-slim

ARG MAIL_MCP_VERSION=0.4.5
ARG MAIL_MCP_SHA256=44f010966050b2391bcf88bdaf2e42e2396068ee16b8ba7fd3165c92388249bf

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git tzdata wget xz-utils \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /data --shell /usr/sbin/nologin aura \
    && mkdir -p /data/logs /wiki /skills /app/runtime \
    && chown -R aura:aura /data /wiki /skills /app

RUN wget -qO /tmp/mail-mcp.tar.xz "https://github.com/tecnologicachile/mail-mcp/releases/download/v${MAIL_MCP_VERSION}/mail-mcp-x86_64-unknown-linux-gnu.tar.xz" \
    && echo "${MAIL_MCP_SHA256}  /tmp/mail-mcp.tar.xz" | sha256sum -c - \
    && tar -xJf /tmp/mail-mcp.tar.xz -C /tmp \
    && install -m 0755 "/tmp/mail-mcp-x86_64-unknown-linux-gnu/mail-mcp" /usr/local/bin/mail-mcp \
    && test -x /usr/local/bin/mail-mcp \
    && rm -rf /tmp/mail-mcp.tar.xz /tmp/mail-mcp-x86_64-unknown-linux-gnu

COPY --from=mcp-node /usr/local/bin/node /usr/local/bin/node
COPY --from=mcp-node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN printf '%s\n' '#!/bin/sh' 'exec /usr/local/bin/node /usr/local/lib/node_modules/@executeautomation/database-server/dist/src/index.js "$@"' > /usr/local/bin/ea-database-server \
    && chmod 0755 /usr/local/bin/ea-database-server \
    && test -x /usr/local/bin/node \
    && test -x /usr/local/bin/ea-database-server

COPY --from=build /out/aura /usr/local/bin/aura

USER aura
WORKDIR /app

ENV AURA_HEADLESS=true \
    AURA_ENV_PATH=/data/.env \
    HTTP_PORT=0.0.0.0:8080 \
    DB_PATH=/data/aura.db \
    LOG_DIR=/data/logs \
    HOME=/data \
    NPM_CONFIG_CACHE=/data/.npm \
    WIKI_PATH=/wiki \
    SKILLS_PATH=/skills \
    SKILLS_INSTALL_PROJECT_DIR=/skills \
    MCP_SERVERS_PATH=/data/mcp.json \
    PROMPT_OVERLAY_PATH=/data \
    SANDBOX_ENABLED=true \
    SANDBOX_RUNTIME_MODE=auto

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1

ENTRYPOINT ["aura"]
