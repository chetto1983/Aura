# Aura WhatsApp MCP sibling. Source is vendored from chetto1983/whatsapp-mcp
# commit 7d6a06dcdce1f01dfb24f60e1030d5efba9f3b88 plus bridge-patch.diff,
# packed into whatsapp-mcp-src.tar.gz for deterministic builds.

FROM debian:bookworm-slim AS source

WORKDIR /src
COPY whatsapp-mcp-src.tar.gz ./
RUN tar -xzf whatsapp-mcp-src.tar.gz

FROM golang:bookworm AS bridge-build

RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src/whatsapp-bridge
COPY --from=source /src/whatsapp-bridge/go.mod /src/whatsapp-bridge/go.sum ./
RUN go mod download
COPY --from=source /src/whatsapp-bridge/ ./
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/whatsapp-bridge .

FROM python:3.11-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates ffmpeg \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir uv==0.5.31

WORKDIR /app
COPY --from=source /src/whatsapp-mcp-server/ ./whatsapp-mcp-server/
COPY --from=source /src/whatsapp-bridge/ ./whatsapp-bridge/
COPY --from=bridge-build /out/whatsapp-bridge /usr/local/bin/whatsapp-bridge
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

RUN cd /app/whatsapp-mcp-server \
    && uv sync --frozen --no-dev \
    && chmod +x /usr/local/bin/entrypoint.sh

ENV WHATSAPP_BRIDGE_PORT=8081
ENV WHATSAPP_API_BASE_URL=http://127.0.0.1:8081/api

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
