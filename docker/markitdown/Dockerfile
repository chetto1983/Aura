# Aura markitdown-mcp sidecar.
#
# Installs markitdown + markitdown-mcp from PyPI. The [all] extra pulls in
# every converter (xlsx, docx, pptx, epub, html, pdf, image metadata,
# audio metadata). We do NOT install the LLM-based image caption path -
# Wave 2.9 ships text extraction only; Wave 2.9.5 layers OCR via Aura's
# OCRBackend abstraction without touching this container.
#
# Slim debian base so apt can pull ffmpeg + exiftool for audio/image
# metadata. Both are tiny and useful for Wave 2.9.5.
FROM python:3.13-slim-bookworm

ENV DEBIAN_FRONTEND=noninteractive \
    PIP_NO_CACHE_DIR=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    MARKITDOWN_ENABLE_PLUGINS=True

RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg exiftool \
    && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir 'markitdown[all]' markitdown-mcp

COPY aura-plugins/ /tmp/aura-plugins/
RUN pip install --no-cache-dir --no-deps /tmp/aura-plugins \
    && rm -rf /tmp/aura-plugins

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
