FROM golang:1.26.4-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aura ./cmd/aura

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        gnupg \
        nodejs \
        npm \
        python3 \
        python3-pip \
        python3-venv \
    && rm -rf /var/lib/apt/lists/*

RUN install -d /usr/share/postgresql-common/pgdg \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
        | gpg --dearmor -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg \
    && echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg] http://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
        > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends postgresql-client-17 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:0.11.21 /uv /uvx /usr/local/bin/

RUN pip install --no-cache-dir --break-system-packages mcp-neo4j-cypher==0.6.0

RUN uvx --from "calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git" \
        calculator-mcp-server --help >/dev/null \
    && npm cache add "github:martinzarfl/mail-mcp"

ENV AURA_IN_CONTAINER=1

COPY --from=build /out/aura /usr/local/bin/aura

ENTRYPOINT ["aura"]
CMD ["serve"]
