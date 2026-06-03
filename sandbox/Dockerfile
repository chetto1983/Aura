# Aura sandbox 2a sidecar image (D-20 build-time curated bake; no runtime pip).
#
# Build context is the repo root: `docker build -f sandbox/Dockerfile sandbox`
# (compose `build: ./sandbox` sets the context to sandbox/, so COPY paths below
# are relative to sandbox/).
#
# python:3.12-slim, digest-pinned to the multi-arch manifest list resolved on
# 2026-06-01 via the Docker Hub registry API (RESEARCH § version-verification).
# The digest pins the *manifest list*, so the same line resolves the correct
# per-arch image on both linux/amd64 and linux/arm64.
FROM python:3.12-slim@sha256:090ba77e2958f6af52a5341f788b50b032dd4ca28377d2893dcf1ecbdfdfe203

# bash (for /exec/shell) + coreutils (user code expects the usual utilities).
# --no-install-recommends keeps the image lean; clean apt lists in the same
# layer so they are not baked into the read-only rootfs.
RUN apt-get update \
    && apt-get install -y --no-install-recommends bash coreutils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# D-20 BUILD-TIME bake: network is available during `docker build`, NOT at
# runtime. --require-hashes makes pip refuse any artifact whose sha256 is not
# in requirements.txt (supply-chain gate, D-20b). All wheels are manylinux/
# none-any, so no compiler/build-essential is needed. This is the ONLY pip
# invocation in the image and it runs as root at build time; the runtime
# container (network_mode:none + read_only) can never pip-install.
COPY requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir --require-hashes -r /app/requirements.txt

# Build FAILS here if any baked library is missing or unimportable — proves the
# curated set is actually usable before the image is published (D-20 / T-05-02-
# SECCOMP-FIT is then proven LIVE under the hardened runtime by 05-03).
RUN python3 -c "import numpy,pandas,scipy,sympy,matplotlib,PIL,bs4,lxml,yaml,dateutil,openpyxl; print('baked-ok')"

# Non-root runtime identity (uid:gid 65532:65532 — the conventional distroless
# "nonroot" id). Layered under daemon userns-remap (D-15) + cap_drop:ALL +
# no-new-privileges from compose.
RUN groupadd --gid 65532 nonroot \
    && useradd --uid 65532 --gid 65532 --no-create-home --shell /usr/sbin/nologin nonroot

# /workspace is the 2b session cwd (SESSION_DEFAULT_CWD in sidecar.py). The
# per-conv host workspace is bind-mounted here at runtime (08-10); this empty
# owned-by-65532 dir is the fallback mount-point so a session WITHOUT a bind-mount
# still has a valid writable cwd (defense-in-depth — the bind-mount overlays it when
# present). Created BEFORE the USER switch so chown succeeds as root.
RUN mkdir -p /workspace && chown 65532:65532 /workspace

COPY sidecar.py /app/sidecar.py

USER 65532:65532
EXPOSE 18901

# No HEALTHCHECK here: the image ships no curl, and the compose healthcheck
# uses a python3 urllib one-liner against /healthz instead (see compose.yaml).
# Everything below the USER switch performs NO pip — runtime stays pip-free.
ENTRYPOINT ["python3", "/app/sidecar.py"]
