FROM golang:1.26.2-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG GIT_COMMIT=dev
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-X github.com/aura/aura/internal/release.Commit=${GIT_COMMIT} \
              -X github.com/aura/aura/internal/release.BuildDate=${BUILD_DATE}" \
    -o /out/aura ./cmd/aura

FROM node:22-bookworm-slim AS mcp-node

ARG DATABASE_MCP_VERSION=1.1.0

RUN npm install -g "@executeautomation/database-server@${DATABASE_MCP_VERSION}" \
    && npm cache clean --force \
    && test -x /usr/local/bin/ea-database-server

FROM debian:bookworm-slim

ARG MAIL_MCP_VERSION=0.4.5
ARG MAIL_MCP_SHA256=44f010966050b2391bcf88bdaf2e42e2396068ee16b8ba7fd3165c92388249bf

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      build-essential ca-certificates curl dnsutils ffmpeg file \
      git gnupg imagemagick iproute2 iputils-ping jq less \
      libcap2-bin lsof net-tools netcat-openbsd nmap \
      openssh-client procps python3 python3-pip python3-venv ripgrep \
      rsync socat sqlite3 strace tcpdump traceroute tree tzdata unzip vim \
      wget whois xz-utils yq zip \
    # 2026-05-22 slim pass dropped 9 redundant tools the LLM never actually
    # reaches for (bake-size win: ~250 MB). Each removed package has an
    # always-installed equivalent already in the apt list:
    #   bat        → cat / less
    #   fd-find    → find / ripgrep
    #   fzf        → no interactive use in container
    #   htop       → procps `top`
    #   httpie     → curl + jq
    #   mtr-tiny   → traceroute
    #   nano       → vim
    #   ncdu       → du / tree
    #   pandoc     → aura-markitdown sidecar
    # GitHub CLI (gh) lives in its own apt repo — pull keyring, register source,
    # install in one layer so the keyring doesn't leak into the final image
    # without being used.
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
       -o /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
       > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --home-dir /data --shell /usr/sbin/nologin aura \
    && mkdir -p /data/logs /wiki /skills /app/runtime \
    && chown -R aura:aura /data /wiki /skills /app \
    # Grant raw-socket + admin capabilities to nmap and tcpdump so the non-root
    # `aura` user can do SYN scans, OS detection, and packet capture without
    # sudo. compose.yaml grants NET_RAW + NET_ADMIN to the container; setcap
    # makes those caps inheritable by the binary's effective set even for a
    # uid 10001 process. Without this the LLM's `nmap -O` fails with
    # "requires root privileges" — verified on 2026-05-17 against live LAN.
    && setcap cap_net_raw,cap_net_admin+eip /usr/bin/nmap \
    && setcap cap_net_raw,cap_net_admin+eip /usr/bin/tcpdump

# Python packages baked into the image so execute_code has the same library
# surface the old Pyodide bundle provided. Without these, the LLM naturally
# writes `import requests` / `import openpyxl` and crashes on stdlib-only.
#
# Post-install strip removes __pycache__, *.pyi typing stubs, and test trees
# shipped inside numpy/pandas/pillow/matplotlib wheels (~120 MB savings).
# Does NOT strip wheel-bundled .libs/ (numpy.libs has libgfortran/libopenblas
# with section alignment that even `strip --strip-debug` breaks). PYTHONDONT-
# WRITEBYTECODE in env (set further down) keeps __pycache__ from being
# regenerated at runtime.
RUN pip3 install --no-cache-dir --break-system-packages \
      requests beautifulsoup4 lxml pillow \
      numpy pandas pyarrow python-calamine openpyxl xlrd \
      pyyaml python-dateutil pytz regex python-docx matplotlib \
    && find /usr/local/lib/python3*/dist-packages /usr/lib/python3/dist-packages \
            -depth -type d -name '__pycache__' -exec rm -rf {} + 2>/dev/null || true \
    && find /usr/local/lib/python3*/dist-packages /usr/lib/python3/dist-packages \
            -type f \( -name '*.pyc' -o -name '*.pyi' \) -delete 2>/dev/null || true \
    && find /usr/local/lib/python3*/dist-packages /usr/lib/python3/dist-packages \
            -depth -type d \( -name 'tests' -o -name 'test' -o -name 'docs' -o -name 'doc' \) \
            ! -path '*.libs*' -exec rm -rf {} + 2>/dev/null || true

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
    PYTHONDONTWRITEBYTECODE=1 \
    HTTP_PORT=0.0.0.0:8080 \
    DB_PATH=/data/aura.db \
    LOG_DIR=/data/logs \
    HOME=/data \
    # /data/.npm-global/bin first so `npm install -g foo` puts foo on PATH
    # without root. /data/.local/bin keeps pip --user installs reachable.
    PATH=/data/.npm-global/bin:/data/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    PIP_CACHE_DIR=/data/.pip \
    PIP_USER=1 \
    PIP_BREAK_SYSTEM_PACKAGES=1 \
    NPM_CONFIG_CACHE=/data/.npm \
    # Redirect npm's global prefix into /data so `npm install -g <pkg>` works
    # for the non-root aura user without touching /usr/local (root-owned).
    NPM_CONFIG_PREFIX=/data/.npm-global \
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
