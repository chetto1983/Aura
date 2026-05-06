FROM golang:1.26.2-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/aura ./cmd/aura

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata wget \
    && adduser -D -H -u 10001 aura \
    && mkdir -p /data/logs /wiki /skills /app \
    && chown -R aura:aura /data /wiki /skills /app

COPY --from=build /out/aura /usr/local/bin/aura

USER aura
WORKDIR /app

ENV AURA_HEADLESS=true \
    AURA_ENV_PATH=/data/.env \
    HTTP_PORT=0.0.0.0:8080 \
    DB_PATH=/data/aura.db \
    LOG_DIR=/data/logs \
    WIKI_PATH=/wiki \
    SKILLS_PATH=/skills \
    MCP_SERVERS_PATH=/data/mcp.json \
    PROMPT_OVERLAY_PATH=/data \
    SANDBOX_ENABLED=false

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1

ENTRYPOINT ["aura"]
