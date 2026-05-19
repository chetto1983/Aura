# aura-piper — thin Python HTTP wrapper around piper-tts for Italian TTS synthesis.
#
# Loads it_IT-paola-medium.onnx once at startup (amortizes the ~670 ms cold-start
# measured during the 2026-05-19 spike) and serves HTTP synthesis requests.
#
# POST /v1/synthesize  JSON {text, voice} → audio/wav (22 kHz mono PCM s16le)
# GET  /health         → 200 once voice loaded, 503 while loading
#
# Voice files (ONNX + .json) are mounted at runtime from ./data/piper via
# compose.yaml volumes — NOT baked into the image.
# aura-init-models (US-MM-INIT00) fetches them before this service starts.
#
# piper-tts uses onnxruntime (ONNX runtime), NOT torch.
# Final image target: <500 MB (piper-tts + onnxruntime + piper-phonemize).
FROM python:3.11-slim-bookworm

ENV DEBIAN_FRONTEND=noninteractive \
    PIP_NO_CACHE_DIR=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    # Redirect home/cache writes to /tmp (read_only fs, tmpfs at /tmp in compose).
    HOME=/tmp \
    XDG_CACHE_HOME=/tmp/.cache

# libgomp1 — OpenMP threading used by onnxruntime (piper inference backend).
RUN apt-get update \
    && apt-get install -y --no-install-recommends libgomp1 \
    && rm -rf /var/lib/apt/lists/*

# piper-tts uses onnxruntime, NOT torch.
# --extra-index-url for CPU-only PyTorch wheel selection in case any transitive
# dep resolves torch (memory feedback_pip_torch_cuda_default_on_linux).
RUN pip install --no-cache-dir \
    --extra-index-url https://download.pytorch.org/whl/cpu \
    piper-tts

COPY server.py /app/server.py
WORKDIR /app

# Drop privileges — port 8083 is non-privileged, no capability needed.
USER nobody:nogroup

EXPOSE 8083

ENTRYPOINT ["python", "/app/server.py"]
