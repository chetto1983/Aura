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
# The web TypeScript build includes src/__tests__, and two suites import a fixture
# from the repo root: sseAdapter the captured AG-UI stream, projectStore the Studio
# project the Go re-key check also reads. Copy only those fixtures into the paths
# the relative imports resolve to inside this stage.
COPY internal/agui/testdata/golden-events.json /internal/agui/testdata/golden-events.json
COPY internal/assets/testdata/studio-project.json /internal/assets/testdata/studio-project.json
RUN npm run build

# The Craft apps the cockpit frames to view and edit files, one folder each under
# /usr/share/aura/apps (docs/superpowers/specs/2026-10-19-craft-web-apps-design.md). GridCraft's,
# PdfCraft's, WordCraft's and DeckCraft's official web releases carry the `?host=parent` bridge and
# need no change, so their zips go in as they are, pinned by version and SHA-256: nothing is fetched
# at run time. The wasm and its glue are gzipped here because nothing in front of aura compresses: aura
# sends the .gz to a browser that accepts it (internal/staticapp).
FROM alpine:3.24 AS craft-official
ADD --checksum=sha256:48c5d1b101befebb3c5ba3b7a14914f01c3207934813ed07dc4251b7ae116a78 https://github.com/storytold/gridcraft/releases/download/v0.19.1/gridcraft-web-0.19.1.zip /tmp/gridcraft.zip
ADD --checksum=sha256:fe9708d46af191aebb56f70eb32cf8b112b0b9452d17ae6ba3a8c4a8665fe748 https://github.com/storytold/pdfcraft/releases/download/v0.21.1/pdfcraft-web-0.21.1.zip /tmp/pdfcraft.zip
ADD --checksum=sha256:3daa7c2d1f209a7eeb2527fcefa3fe31e875848fb1970d68f54edae55fe0bc9f https://github.com/storytold/wordcraft/releases/download/v0.18.1/wordcraft-web-0.18.1.zip /tmp/wordcraft.zip
ADD --checksum=sha256:3196e537fcfab538aaa2e88584bc74e29bb584459b916ab584ac22fbafe32db5 https://github.com/storytold/deckcraft/releases/download/v0.18.1/deckcraft-web-0.18.1.zip /tmp/deckcraft.zip
RUN set -eu; \
    mkdir /apps; \
    for app in gridcraft pdfcraft wordcraft deckcraft; do \
        unzip -q "/tmp/$app.zip" -d "/tmp/$app" && mv /tmp/$app/$app-web-* "/apps/$app"; \
        rm -f "/apps/$app/_headers" "/apps/$app/.htaccess"; \
        gzip -9 -k /apps/$app/*.wasm /apps/$app/*.js; \
    done

# Keep in lockstep with the garage service pin in compose.yaml — the bootstrap
# CLI talks native RPC to the server and Garage wants matched versions.
FROM dxflrs/garage:v2.4.1 AS garagebin

# The appliance payload -- compose files, the updater and its units, sidecar config -- travels
# INSIDE this image so an edge machine converges on it with no operator:
# deploy/aura-image-update.sh extracts it after every pull and installs whatever differs. The
# file list IS scripts/payload_manifest.txt and the copy is checked against it, so an image
# whose payload does not match the commit it was built from fails here instead of on a host.
FROM debian:bookworm-slim AS payload
RUN --mount=type=bind,target=/src \
    set -eu; \
    while read -r _ rel; do install -D -m 0644 "/src/$rel" "/payload/$rel"; done < /src/scripts/payload_manifest.txt; \
    install -m 0644 /src/scripts/payload_manifest.txt /payload/payload_manifest.txt; \
    cd /payload && sha256sum --check --quiet --strict payload_manifest.txt

FROM golang:1.27.4-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
# proxy.golang.org drops a stream mid-fetch often enough to redden a whole CI run on a
# build that has nothing wrong with it: measured 2026-09-01 on Actions run 33552817871,
# `stream error: stream ID 323; INTERNAL_ERROR; received from peer` on one module .mod
# file, which killed the Agent Memory job before a single test body ran. Three tries with
# a widening pause: a transport blip is ridden out, a module that genuinely is not there
# still stops the build on the third. Every branch is `if`-guarded because under `set -e`
# a bare `[ ... ] && ...` whose test is false exits the shell with the test's status.
RUN set -e; \
    for attempt in 1 2 3; do \
      if go mod download; then break; fi; \
      if [ "$attempt" = 3 ]; then echo "go mod download failed after 3 attempts" >&2; exit 1; fi; \
      echo "go mod download failed (attempt $attempt); retrying" >&2; \
      sleep $((attempt * 5)); \
    done

COPY cmd/aura ./cmd/aura
COPY internal ./internal
# Overwrite the committed embed source (internal/webui/dist) with the fresh webbuild
# output so the image's baked-in SPA is a reproducible build, not the committed bytes.
COPY --from=webbuild /internal/webui/dist ./internal/webui/dist
# VCS_REF stamps cmd/aura/version.go's `commit` so a running image's provenance is
# CHECKABLE (`aura version`) rather than asserted. `.dockerignore` excludes `.git`,
# so `debug.ReadBuildInfo()` has no vcs.revision to fall back to; goreleaser's
# `-X main.commit=` stamp only covers the release path, not this local build path
# (measured 2026-08-13, 45-08-PLAN.md Task 1 step 6). Unset VCS_REF leaves
# `commit: unknown` exactly as before this change — additive, not a behaviour change.
ARG VCS_REF=""
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.commit=${VCS_REF}" -o /out/aura ./cmd/aura

# The same digest as docker/aura-sandbox/Dockerfile's base; Dependabot moves both weekly.
FROM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc

# A cached RUN keeps the packages it installed for as long as the base digest and its own line
# stay the same, so a fix to a package the base does not carry never arrived: on 2026-10-24
# aura:edge had libexpat1 deb12u4 and libbrotli1 1.0.9-2+b6 while bookworm had deb12u5 and
# 1.0.9-2+deb12u1. A changed ARG re-runs every RUN after it
# (https://docs.docker.com/reference/dockerfile/#impact-on-build-caching), and the builders pass
# scripts/apt_week.sh, so apt runs once a week.
ARG APT_WEEK=""

# apt-get upgrade takes the fixes of the packages the base itself ships, which install leaves
# alone: aura-ingest's python base kept libssl3 deb12u1 on 2026-10-26 (issue #189).
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get upgrade -y \
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
# LangJS to the `node` interpreter (Slice 7e), (2) the `npx skills find/add` installer
# (internal/skills/installer.go), and (3) npm-prepared stdio MCP servers (see uv below).
#
# D-06 note: this runtime Node-24 is ORTHOGONAL to the SPA. The operator SPA is built
# in the `webbuild` stage above and baked into the Go binary before runtime — no Node
# touches the SPA at runtime. Do not read this as a D-06 "no Node for the SPA" violation.
#
# npm 11.21.1 replaces the npm NodeSource bundles (11.19.1): its own brace-expansion and undici
# carried fixable HIGHs (weekly security scan, 2026-10-24). docker/aura-sandbox/Dockerfile moves
# with it.
RUN curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && npm install -g npm@11.21.1 \
    && npm cache clean --force \
    && rm -rf /var/lib/apt/lists/*

RUN install -d /usr/share/postgresql-common/pgdg \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
        | gpg --dearmor -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg \
    && echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.gpg] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
        > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends postgresql-client-18 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=ghcr.io/astral-sh/uv:0.11.32 /uv /uvx /usr/local/bin/

# uv/uvx stay, with Node and python3 above, for the work Aura itself runs in this container:
# preparing a stdio MCP server's environment (cmd/aura/mcp_prepare.go, amendment #211) and
# running one whose runtime is not the box. The agent's own shell work runs in its box. What is
# gone is the calculator warm-up that used to live here. Warming a uv cache at build time cannot
# work for a recipe in THIS image: compose mounts a named volume over /root/.cache/uv, and a
# named volume is seeded once at creation and never refreshed, so every image built after
# the volume existed warmed a cache nobody could see. With UV_OFFLINE=1 on the mount there
# was no network to fall back to. See manager.BuiltInCatalog for the retirement.

# mail recipe retired: the calendar PIM sidecar (forked calendar-mcp →
# chetto1983/aura-pim-mcp, a separate compose service) now provides email
# (get/search/send) over MCP, so the standalone vendored mail-mcp bin is no longer
# baked into this image.

# No document toolchain here: shell tools never run in this container (there is no host
# execution path: internal/sandbox/usersandbox/router.go), and documents are the Craft MCP
# servers' work in the box. poppler-utils stays for the Go code that runs here (pdftotext in
# internal/pdftext, pdftoppm in cmd/aura-media-index), with the metric-compatible fonts it renders
# a PDF's unembedded fonts in.
RUN apt-get update \
    && apt-get install -y --no-install-recommends poppler-utils fonts-liberation \
    && rm -rf /var/lib/apt/lists/*

ENV AURA_IN_CONTAINER=1

COPY --from=payload /payload /usr/share/aura/payload
COPY --from=craft-official /apps /usr/share/aura/apps
# The two Craft apps compiled from Rust come prebuilt: docker/aura-craft-web, published by
# publish-craft-web.yml under the hash of its inputs. TestCraftWebImagePinMatchesItsInputs fails
# when docker/craft-web/ changes and this pin does not follow.
COPY --from=ghcr.io/chetto1983/aura-craft-web:ebf0b8add69cb12a@sha256:d975942ea74bdc8687ffeb76982131fecb017600a92f5402cef02bd12f722852 /apps /usr/share/aura/apps
COPY --from=build /out/aura /usr/local/bin/aura
COPY --from=garagebin /garage /usr/local/bin/garage
COPY docker/aura/aura-garage-bootstrap.sh /usr/local/bin/aura-garage-bootstrap.sh
COPY docker/aura/aura-tempo-blocks-repair.sh /usr/local/bin/aura-tempo-blocks-repair.sh
RUN chmod +x /usr/local/bin/aura-garage-bootstrap.sh /usr/local/bin/aura-tempo-blocks-repair.sh

ENTRYPOINT ["aura"]
CMD ["serve"]
