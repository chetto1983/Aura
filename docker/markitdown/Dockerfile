# Aura markitdown-mcp sidecar — multi-stage slim build.
#
# Stage 1 (builder): python:3.13-slim-bookworm. Installs markitdown with
# explicit extras (docx,pdf,pptx,xls,xlsx,outlook) instead of [all]. Drops
# the heavy extras Aura never reaches for:
#   • azure-ai-documentintelligence + azure-identity (Azure cloud OCR API;
#     Aura uses Mistral OCR via internal/storage/sources/ocr — see CLAUDE.md
#     Source Ingestion Pipeline).
#   • youtube-transcript-api (Aura ingests files, not YouTube transcripts).
#   • speechrecognition (cloud STT; Aura uses the in-tree whisper.cpp
#     sidecar via internal/storage/sources/markitdown is doc-only).
# Also installs the Aura-local converter plugins from ./aura-plugins.
#
# Stage 2 (runtime): python:3.13-slim-bookworm + the prebuilt venv copy,
# plus ffmpeg + exiftool for the audio/image-metadata converters still in
# markitdown core. Strips __pycache__, *.pyi typing stubs, and test trees
# from the venv before copying.
#
# Expected total: ~700-800 MB extracted vs 1.47 GB on the single-stage
# [all]-extras build.

FROM python:3.13-slim-bookworm AS builder

ENV DEBIAN_FRONTEND=noninteractive \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# build-essential here covers the rare wheel that compiles from source on
# python:3.13 (older wheels often lag the latest CPython). The runtime
# stage drops these.
RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential \
    && rm -rf /var/lib/apt/lists/*

RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:${PATH}"

# Explicit-extras install — see header for which extras and why.
RUN pip install --no-cache-dir \
        'markitdown[docx,pdf,pptx,xls,xlsx,outlook]' \
        markitdown-mcp

COPY aura-plugins/ /tmp/aura-plugins/
RUN pip install --no-cache-dir --no-deps /tmp/aura-plugins \
    && rm -rf /tmp/aura-plugins

# Aggressive venv slimming — same pattern as docker/pocket-tts/Dockerfile.
# Excludes wheel-bundled */.libs/* (libgfortran / libopenblas alignment).
RUN set -eux; \
    find /opt/venv -depth -type d -name '__pycache__' -exec rm -rf {} +; \
    find /opt/venv -type f \( -name '*.pyc' -o -name '*.pyi' \) -delete; \
    find /opt/venv/lib -depth -type d \
        \( -name 'tests' -o -name 'test' -o -name 'docs' -o -name 'doc' -o -name 'man' \) \
        ! -path '*.libs*' -prune -exec rm -rf {} +; \
    find /opt/venv -path '*.dist-info/RECORD' -delete


FROM python:3.13-slim-bookworm AS runtime

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    MARKITDOWN_ENABLE_PLUGINS=True \
    PATH="/opt/venv/bin:${PATH}"

# ffmpeg (audio/video conversion) + exiftool (image/audio metadata) are
# called by markitdown core converters even with the slim extras list.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg exiftool \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /opt/venv /opt/venv

WORKDIR /workdir

# Drop privileges. nobody/nogroup ship with debian-slim.
USER nobody:nogroup

EXPOSE 3001

# Aura's wrapper keeps markitdown-mcp transport behavior while honoring
# MARKITDOWN_ENABLE_PLUGINS for local converters.
#
# Default to Streamable HTTP on all interfaces inside the container. The
# host-side port mapping in compose.yaml is bound to 127.0.0.1 so the
# sidecar is never reachable from the network - only the aura container
# (via the docker bridge) and the host loopback can reach it.
ENTRYPOINT ["aura-markitdown-mcp"]
CMD ["--http", "--host", "0.0.0.0", "--port", "3001"]
