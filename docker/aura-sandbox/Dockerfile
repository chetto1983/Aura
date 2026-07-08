# docker/aura-sandbox/Dockerfile — the shared FAT full-capability per-identity box
# image (Phase 37, D-12/D-13). SandboxRouter.Resolve boots one container-per-identity
# from this image under the strict runtime profiles. The box runs the MODEL's shell
# (shell_exec / shell_bg / fs_read / fs_write / skill-snippet exec), NOT the aura
# daemon — so, unlike docker/aura/Dockerfile, it deliberately does NOT COPY the aura
# binary. Isolation is the BOUNDARY (per-identity named volume + cgroup caps + egress
# netns + host-exposure flags made unrepresentable in SandboxSpec), never a crippled
# inside.
#
# Posture (D-12, REVERT ec7fe2f6 — packaging-box.md): FAT, writable rootfs, container
# ROOT (NO `USER` directive), full shell + full runtime. MUST NOT be distroless /
# non-root / read-only-rootfs — each individually breaks shell_exec parity (distroless
# has no shell → exit 127; read-only blocks self-extension; USER 65532 forces non-root).
#
# Digest-pinned (D-13) so the shared box image is reproducible. Both the debian base and
# the astral uv image are @sha256-pinned. Resolved 2026-07-06 from the live registry:
#   debian:bookworm-slim         -> sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df
#   ghcr.io/astral-sh/uv:0.11.21 -> sha256:ff07b86af50d4d9391d9daf4ff89ce427bc544f9aae87057e69a1cc0aa369946
FROM debian:bookworm-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df

# --- heavy runtime layer (cache-stable: ordered FIRST so a later tweak re-runs only the
# small trailing layers, never this ~apt/node/uv layer). D-13 bakes the common runtimes;
# uv installs the long tail on-demand into the shared warm-cache volume at /root/.cache/uv
# (mounted by the usersandbox translator), which makes skill `deps:` frontmatter load-bearing.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        curl \
        git \
        gnupg \
        jq \
        python3 \
        python3-pip \
        python3-venv \
    && rm -rf /var/lib/apt/lists/*

# GitHub CLI (`gh`) from the official cli.github.com apt repo (D-13 bakes gh so the box's
# shell has first-class GitHub parity). signed-by keyring, mirroring the pgdg repo posture
# in docker/aura/Dockerfile.
RUN install -d -m 0755 /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# Node 24 LTS (Debian bookworm ships Node 18, too old). NodeSource bundles npm/npx — the
# box's JS skill snippets (internal/skills/snippet.go LangJS -> node) and the
# `npx skills find/add` self-extension loop both need it. Mirrors docker/aura/Dockerfile.
RUN curl -fsSL https://deb.nodesource.com/setup_24.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*

# uv/uvx as static binaries from the DIGEST-PINNED astral image (sandbox-runtime finding:
# COPY the binaries, never pip-install uv). uv drives the on-demand long-tail dep installs
# into the shared warm-cache volume (D-13).
COPY --from=ghcr.io/astral-sh/uv@sha256:ff07b86af50d4d9391d9daf4ff89ce427bc544f9aae87057e69a1cc0aa369946 /uv /uvx /usr/local/bin/

# Pinned MCP server (parity with docker/aura). Debian python is PEP-668 externally-managed
# -> --break-system-packages (the container IS the boundary; no venv needed at image level).
RUN pip install --no-cache-dir --break-system-packages mcp-neo4j-cypher==0.6.0

# Mount points (documented — the ACTUAL mounts are set by the usersandbox translator, not
# baked here; D-10/D-13):
#   /workspace          -> the per-identity named volume aura-box-<id> (persistent, RETAINED on suspend)
#   /workspace/.scratch -> a tmpfs RW scratch (ephemeral)
#   /root/.cache/uv     -> the shared uv warm-cache volume aura-uv-cache (cross-identity, content-addressed)
#   /root/.npm          -> the shared npm warm-cache volume aura-npm-cache (cross-identity)
#   /root/.cache/pip    -> the shared pip warm-cache volume aura-pip-cache (cross-identity)
# Pre-create the dirs so a first exec before any mount still has a writable tree.
RUN mkdir -p /workspace /root/.cache/uv /root/.npm /root/.cache/pip

WORKDIR /workspace

# NO `USER` directive (root inside — D-12 parity). NO aura binary and NO privilege-dropping
# entrypoint: the box is driven by `docker exec` (usersandbox.DockerBackend), so the default
# command is just a keep-alive PID the daemon execs into. AutoRemove=false is set by the
# translator (Pitfall 5: --rm would destroy a suspendable box), not here.
CMD ["sleep", "infinity"]
