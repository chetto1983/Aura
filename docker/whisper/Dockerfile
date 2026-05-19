# aura-whisper — wraps whisper.cpp whisper-server binary for audio transcription.
#
# Multi-stage build:
#   builder  — debian:bookworm-slim + cmake + g++; compiles whisper-server from
#              whisper.cpp v1.8.4 (CMake output: build/bin/whisper-server).
#              See memory reference_whisper_cpp_binary_path — v1.8.4+ uses CMake;
#              binaries land at build/bin/, NOT the legacy /main location.
#   runtime  — debian:bookworm-slim + libgomp1 + curl only; final image <200 MB
#
# Model file (ggml-small.bin, 466 MB) is mounted at runtime from ./data via
# compose.yaml volumes — NOT baked into the image.
# aura-init-models (Wave 2.10 + US-MM-INIT00) fetches ggml-small.bin to ./data/
# before this service starts; compose.yaml depends_on enforces ordering.

FROM debian:bookworm-slim AS builder

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       git build-essential cmake libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN git clone --depth 1 --branch v1.8.4 \
    https://github.com/ggerganov/whisper.cpp.git /opt/whisper.cpp

WORKDIR /opt/whisper.cpp

# Build whisper-server only (skips tests + other examples for faster build).
# CMake resolves whisper-server's dependencies (whisper, ggml, common) automatically.
RUN cmake -B build \
        -DCMAKE_BUILD_TYPE=Release \
        -DWHISPER_BUILD_TESTS=OFF \
    && cmake --build build --target whisper-server -j$(nproc)

# ── runtime stage ─────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

# libgomp1  — OpenMP parallel processing (whisper.cpp uses this for threading)
# curl      — used by the compose.yaml healthcheck (GET http://localhost:8082/)
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       libgomp1 curl \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /opt/whisper.cpp/build/bin/whisper-server /usr/local/bin/whisper-server
RUN chmod 755 /usr/local/bin/whisper-server

# Drop to nobody — port 8082 is non-privileged so no CAP_NET_BIND_SERVICE needed.
USER nobody:nogroup

EXPOSE 8082

# --host 0.0.0.0   → reachable from aura container via docker bridge
# --port 8082      → matches compose.yaml port mapping
# -m /models/ggml-small.bin → GGML model mounted read-only from ./data
# -t 4             → 4 threads (memory feedback_minipc_cpu_budget: ≤4 threads)
ENTRYPOINT ["/usr/local/bin/whisper-server", \
            "--host", "0.0.0.0", \
            "--port", "8082", \
            "-m", "/models/ggml-small.bin", \
            "-t", "4"]
