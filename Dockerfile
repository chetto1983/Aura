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
    && apt-get install -y --no-install-recommends ca-certificates curl file git jq procps python3 python3-pip python3-venv ripgrep sqlite3 tzdata unzip wget xz-utils zip \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /data --shell /usr/sbin/nologin aura \
    && mkdir -p /data/logs /wiki /skills /app/runtime \
    && chown -R aura:aura /data /wiki /skills /app

# Python packages baked into the image so execute_code has the same library
# surface the old Pyodide bundle provided. Without these, the LLM naturally
# writes `import requests` / `import openpyxl` and crashes on stdlib-only.
RUN pip3 install --no-cache-dir --break-system-packages \
      requests beautifulsoup4 lxml pillow \
      numpy pandas pyarrow python-calamine openpyxl xlrd \
      pyyaml python-dateutil pytz regex

RUN wget -qO /tmp/mail-mcp.tar.xz "https://github.com/tecnologicachile/mail-mcp/releases/download/v${MAIL_MCP_VERSION}/mail-mcp-x86_64-unknown-linux-gnu.tar.xz" \
    && echo "${MAIL_MCP_SHA256}  /tmp/mail-mcp.tar.xz" | sha256sum -c - \
    && tar -xJf /tmp/mail-mcp.tar.xz -C /tmp \
    && install -m 0755 "/tmp/mail-mcp-x86_64-unknown-linux-gnu/mail-mcp" /usr/local/bin/mail-mcp \
    && test -x /usr/local/bin/mail-mcp \
    && rm -rf /tmp/mail-mcp.tar.xz /tmp/mail-mcp-x86_64-unknown-linux-gnu

COPY --from=mcp-node /usr/local/bin/node /usr/local/bin/node
COPY --from=mcp-node /usr/local/lib/node_modules /usr/local/lib/node_modules
# Recreate the npm/npx CLI symlinks the node image normally ships. The COPY
# above brings the bundled `npm` package over but not the /usr/local/bin
# entrypoints, so without these links `npx skills add ...` (used by the
# Skills installer) fails with "npx: not found".
RUN ln -sf /usr/local/lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
    && ln -sf /usr/local/lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
    && printf '%s\n' '#!/bin/sh' 'exec /usr/local/bin/node /usr/local/lib/node_modules/@executeautomation/database-server/dist/src/index.js "$@"' > /usr/local/bin/ea-database-server \
    && chmod 0755 /usr/local/bin/ea-database-server \
    && test -x /usr/local/bin/node \
    && test -x /usr/local/bin/ea-database-server \
    && /usr/local/bin/node /usr/local/bin/npx --version

COPY --from=build /out/aura /usr/local/bin/aura

USER aura
WORKDIR /app

ENV AURA_HEADLESS=true \
    AURA_ENV_PATH=/data/.env \
    HTTP_PORT=0.0.0.0:8080 \
    DB_PATH=/data/aura.db \
    LOG_DIR=/data/logs \
    HOME=/data \
    PATH=/data/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    PIP_CACHE_DIR=/data/.pip \
    PIP_USER=1 \
    PIP_BREAK_SYSTEM_PACKAGES=1 \
    NPM_CONFIG_CACHE=/data/.npm \
    WIKI_PATH=/wiki \
    SKILLS_PATH=/skills \
    SKILLS_INSTALL_PROJECT_DIR=/skills \
    MCP_SERVERS_PATH=/data/mcp.json \
    PROMPT_OVERLAY_PATH=/data \
    SANDBOX_ENABLED=true

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1

ENTRYPOINT ["aura"]
