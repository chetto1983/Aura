# syntax=docker/dockerfile:1.7
# Aura pocket-tts (Wave 3 Italian TTS) sidecar — multi-stage ultra-slim build.
#
# Stage 1 (builder): ghcr.io/astral-sh/uv:debian installs pocket-tts[quantize]
# into /app/.venv with --no-bytecode (skips __pycache__ generation entirely).
# torch is pinned to the CPU wheel index via pyproject.toml.
#
# Stage 2 (slimmer): runs `strip --strip-unneeded` on every .so in the venv,
# drops Python stdlib trees the TTS server never imports (idlelib, tkinter,
# turtle, ensurepip, lib2to3), purges test trees from torch/scipy/numpy
# wheels, and removes typing stubs. Cumulative ~500-900 MB win vs unstripped.
#
# Stage 3 (runtime): python:3.12-slim-bookworm + the pre-stripped venv only.
# No build libs, no VCS, no uv binary. Healthcheck via Python urllib so we
# don't carry curl either.
#
# Expected total: ~2.5-3 GB compressed vs 9.5 GB on the single-stage v0.3.2.

FROM python:3.12-slim-bookworm AS builder

# Bring uv (just the binary) onto a python:3.12 base so the venv targets 3.12,
# matching the runtime stage. uv:debian ships 3.13 by default which then
# mismatches the runtime python and breaks site-packages discovery.
COPY --from=ghcr.io/astral-sh/uv:debian /usr/local/bin/uv /usr/local/bin/uv

ENV UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=never \
    UV_NO_CACHE=1 \
    UV_COMPILE_BYTECODE=0 \
    UV_PYTHON=python3.12

WORKDIR /app

COPY pyproject.toml .

# uv sync installs pocket-tts[quantize] + transitive deps (torch CPU, scipy,
# numpy, sentencepiece, fastapi, uvicorn, torchao, ...) into /app/.venv.
#
# CRITICAL: do NOT pass --no-install-project. The pyproject's
# [tool.uv.sources] mapping (`torch = [{ index = "pytorch-cpu" }]`) is only
# honored when the project itself is being installed; --no-install-project
# silently drops it and uv falls back to PyPI default, which delivers the
# CUDA torch wheel (~1 GB of nvidia-* libs) instead of the CPU build.
RUN uv sync --no-dev


FROM debian:bookworm-slim AS slimmer

# binutils provides `strip` for shrinking the torch/scipy native libraries.
RUN apt-get update \
    && apt-get install -y --no-install-recommends binutils \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/.venv /app/.venv

# Aggressive venv slimming. Order matters: strip native libs first (biggest
# win), then purge stdlib + wheel ride-alongs.
RUN set -eux; \
    # 1. Strip debug symbols from torch / scipy native libs — torch ships
    #    ~250 MB of libtorch_*.so debug info that pip never strips.
    #
    #    EXCLUDE wheel-bundled .libs/ directories (numpy/.libs, scipy/.libs):
    #    those carry pre-built BLAS shared libs (libscipy_openblas64_*.so,
    #    libgfortran*.so, etc.) with section alignment requirements that even
    #    `strip --strip-debug` violates. Stripping them produces "ELF load
    #    command address/offset not page-aligned" at import time.
    find /app/.venv -type f \( -name '*.so' -o -name '*.so.*' \) \
        ! -path '*.libs/*' \
        -exec strip --strip-unneeded {} + 2>/dev/null || true; \
    # 2. Drop bytecode + typing stubs (uv --no-bytecode should already prevent
    #    __pycache__; this is belt-and-suspenders + catches .pyi).
    find /app/.venv -depth -type d -name '__pycache__' -exec rm -rf {} +; \
    find /app/.venv -type f \( -name '*.pyc' -o -name '*.pyi' \) -delete; \
    # 3. Test trees shipped inside torch/scipy/numpy/transformers wheels.
    find /app/.venv/lib -type d \( -name 'tests' -o -name 'test' \) -prune -exec rm -rf {} +; \
    # 4. Python stdlib trees this TTS server never imports. The venv shares
    #    Python stdlib with the base image so these may not exist — `-prune`
    #    + `|| true` keeps the layer green either way.
    python_lib=$(find /app/.venv/lib -maxdepth 1 -type d -name 'python3.*' | head -n 1); \
    if [ -n "$python_lib" ]; then \
        for dir in idlelib tkinter turtle ensurepip lib2to3; do \
            rm -rf "$python_lib/$dir" || true; \
        done; \
    fi; \
    # 5. dist-info RECORD checksums + metadata pyc — keep top-level metadata
    #    (pip needs WHEEL + METADATA for `pip show`) but drop the noisy bits.
    find /app/.venv -path '*.dist-info/RECORD' -delete; \
    # 6. doc/man/locale residue from a handful of wheels.
    find /app/.venv -depth -type d \( -name 'docs' -o -name 'doc' -o -name 'man' \) -prune -exec rm -rf {} + ;


FROM python:3.12-slim-bookworm AS runtime

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PATH="/app/.venv/bin:${PATH}"

WORKDIR /app

COPY --from=slimmer /app/.venv /app/.venv

EXPOSE 8000

# Healthcheck via Python urllib — keeps curl off the runtime image.
HEALTHCHECK --interval=30s --timeout=10s --start-period=300s --retries=3 \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://localhost:8000/health',timeout=5).status==200 else 1)" || exit 1

# NOTE: On first boot pocket-tts auto-downloads the italian_24l model and the
# giovanni voice from HuggingFace (~600 MB total). This takes 3-5 minutes on a
# cold cache. The pockettts-hf-cache named volume in compose.yaml preserves the
# download across restarts. Subsequent boots load from cache in ~10-30 s.
ENTRYPOINT ["pocket-tts", "serve", "--host", "0.0.0.0", "--port", "8000", "--language", "italian_24l", "--quantize"]
