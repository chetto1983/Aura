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
COPY web/patches ./patches
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
# /usr/share/aura/apps (docs/superpowers/specs/2026-10-19-craft-web-apps-design.md). GridCraft's
# official web release carries the `?host=parent` bridge and needs no change, so its zip goes in as
# it is, pinned by version and SHA-256: nothing is fetched at run time. The wasm and its glue are
# gzipped here because nothing in front of aura compresses: aura sends the .gz to a browser that
# accepts it (internal/staticapp).
FROM alpine:3.22 AS craft-official
ADD --checksum=sha256:532966caa70bbd346bcd3c48a827d9d0118f83e50be7a0ca43fdb28bd48f1dd4 https://github.com/storytold/gridcraft/releases/download/v0.8.0/gridcraft-web-0.8.0.zip /tmp/gridcraft.zip
RUN set -eu; \
    mkdir /apps; \
    unzip -q /tmp/gridcraft.zip -d /tmp && mv /tmp/gridcraft-web-0.8.0 /apps/gridcraft; \
    rm -f /apps/gridcraft/_headers /apps/gridcraft/.htaccess; \
    gzip -9 -k /apps/gridcraft/*.wasm /apps/gridcraft/*.js

# The other five need a change: WordCraft, DeckCraft, PhotoCraft and FilmCraft have no bridge
# upstream, and PdfCraft and PhotoCraft draw their web interface in English whatever the browser's
# language. docker/craft-web builds each from its release tag, checked against the tag's commit,
# with the patch beside the script (that change, and the ArtCraft marks a modified build may not
# carry). WebAssembly is the same on every platform, so it builds once, on the builder's. The tools
# are release binaries pinned by SHA-256; trunk runs offline, so it fetches none of its own.
FROM --platform=$BUILDPLATFORM rust:1.97.0-bookworm@sha256:8fa55b2f3ddf97471ab6a767bfa3f37e6bad0986ba823e75fea57e2a2a5c3073 AS craft-web
ARG BUILDARCH
RUN set -eu; \
    case "$BUILDARCH" in \
        amd64) arch=x86_64; trunk=f2b4680cd239693a646a2795e4633c625328d7b2a044fbe749fa3a2fe9e7036b; \
               bindgen=82d12bb940e2d4e72e0d5605387fc1b8ca179044e012b620f0ce4e7440e8320e; \
               binaryen=e959f2170af4c20c552e9de3a0253704d6a9d2766e8fdb88e4d6ac4bae9388fe ;; \
        arm64) arch=aarch64; trunk=b1d8e60e454f7fc182d9a4d95d1506ffbae947d8ba90f8f6f02da93b60f980f9; \
               bindgen=2ed4351c35dd9440308bbb02767d47ea278efe851a52465300f3c94f5b6c2a87; \
               binaryen=4b6bd61ba6cd3b18c993b4657d93426c782f9b91b74be0d38018cd8be1319376 ;; \
        *) echo "craft-web: unsupported build arch ${BUILDARCH}" >&2; exit 1 ;; \
    esac; \
    tool() { \
        curl -fsSL -o /tmp/tool.tgz "$1"; echo "$2  /tmp/tool.tgz" | sha256sum -c -; \
        mkdir /tmp/tool && tar -xzf /tmp/tool.tgz -C /tmp/tool; \
        install -m 0755 "/tmp/tool/$3" /usr/local/bin/; rm -rf /tmp/tool /tmp/tool.tgz; \
    }; \
    tool "https://github.com/trunk-rs/trunk/releases/download/v0.21.14/trunk-$arch-unknown-linux-gnu.tar.gz" "$trunk" trunk; \
    tool "https://github.com/wasm-bindgen/wasm-bindgen/releases/download/0.2.129/wasm-bindgen-0.2.129-$arch-unknown-linux-musl.tar.gz" "$bindgen" "wasm-bindgen-0.2.129-$arch-unknown-linux-musl/wasm-bindgen"; \
    tool "https://github.com/WebAssembly/binaryen/releases/download/version_123/binaryen-version_123-$arch-linux.tar.gz" "$binaryen" binaryen-version_123/bin/wasm-opt; \
    rustup target add wasm32-unknown-unknown
COPY docker/craft-web/ /craft-web/
RUN /craft-web/build.sh pdfcraft v0.9.0 645016addcfb9078c557f0189559ab47cdcee057 /apps
RUN /craft-web/build.sh wordcraft v0.8.0 f2e15cf8f899a79899a2d9595ca4ce7df1e5ea22 /apps
RUN /craft-web/build.sh deckcraft v0.8.0 22db9261f9d1a214c296fd390173130c45d76d66 /apps
RUN /craft-web/build.sh photocraft v0.10.0 8407d9ccb125af52944fca9d228383459f94aaef /apps
RUN /craft-web/build.sh filmcraft v0.9.0 4a69a038d32761f604b3c7e4845b21fb40b3cbe4 /apps

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

FROM golang:1.27.3-alpine AS build

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

COPY --from=ghcr.io/astral-sh/uv:0.11.32 /uv /uvx /usr/local/bin/

# uv/uvx above stay: the agent reaches them from shell_exec for Python work. What is gone
# is the calculator warm-up that used to live here. Warming a uv cache at build time cannot
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
COPY --from=craft-web /apps /usr/share/aura/apps
COPY --from=build /out/aura /usr/local/bin/aura
COPY --from=garagebin /garage /usr/local/bin/garage
COPY docker/aura/aura-garage-bootstrap.sh /usr/local/bin/aura-garage-bootstrap.sh
COPY docker/aura/aura-tempo-blocks-repair.sh /usr/local/bin/aura-tempo-blocks-repair.sh
RUN chmod +x /usr/local/bin/aura-garage-bootstrap.sh /usr/local/bin/aura-tempo-blocks-repair.sh

ENTRYPOINT ["aura"]
CMD ["serve"]
