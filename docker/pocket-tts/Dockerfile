FROM ghcr.io/astral-sh/uv:debian

# curl is required by the Docker healthcheck (GET /health)
RUN apt-get update && apt-get install -y --no-install-recommends curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY pyproject.toml .

# Sync installs pocket-tts with quantize extras.
# torch is redirected to the CPU-only wheel index (pytorch-cpu in pyproject.toml)
# to avoid pulling ~1.5 GB CUDA wheels on this CPU-only mini-PC host.
RUN uv sync --no-dev

EXPOSE 8000

# NOTE: On first boot pocket-tts auto-downloads the italian_24l model and the
# giovanni voice from HuggingFace (~600 MB total). This takes 3-5 minutes on a
# cold cache. The pockettts-hf-cache named volume in compose.yaml preserves the
# download across restarts. Subsequent boots load from cache in ~10-30 s.
# The start_period: 300s healthcheck setting accounts for the initial download.
ENTRYPOINT ["uv", "run", "pocket-tts", "serve", "--host", "0.0.0.0", "--port", "8000", "--language", "italian_24l", "--quantize"]
