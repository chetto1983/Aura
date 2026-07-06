# docker/aura-egress/Dockerfile — the per-box EGRESS SIDECAR image (Phase 37, SBX-04,
# D-03/D-05/D-07). DockerBackend launches one of these beside every networked box, sharing
# the box network namespace via `--network container:<box>` with CAP_NET_ADMIN on the
# SIDECAR ONLY (the box stays net-unprivileged). It enforces the tenancy boundary at the
# network layer: full public internet (D-04) minus RFC1918 + the 169.254.169.254 metadata
# IP + the shared-services Docker bridge (D-05).
#
# Two enforcement jobs, split by runtime compatibility (37-RESEARCH Pattern 6 / Pitfall 2):
#
#   1. Always-on FILTER-TABLE floor (runc AND gVisor runsc): the entrypoint applies the
#      Go-generated nftables ruleset from $AURA_EGRESS_FLOOR_RULESET with `nft -f -`. The
#      filter table is implemented by gVisor's netstack, so the floor is identical under
#      both runtimes. This is the bespoke nft floor the research recommends as the primary
#      mechanism (and the A1 fallback for the OpenSandbox fit-risk).
#
#   2. Opt-in tightened FQDN allowlist (runc-ONLY): Alibaba OpenSandbox's egress component
#      (DNS proxy + iptables REDIRECT in the NAT table + dynamic nftables allow-sets). gVisor
#      does NOT implement the nat table (issue #934), so buildEgressSidecar refuses Runsc +
#      a configured allowlist BEFORE this image is ever launched in FQDN mode. When the
#      OpenSandbox egress binary is present at /usr/local/bin/opensandbox-egress AND
#      $OPENSANDBOX_EGRESS_MODE is set, the entrypoint execs it (foreground) after the floor
#      is applied. Build it from a PINNED OpenSandbox source commit into that path to enable
#      FQDN mode (RESEARCH Package Legitimacy — Approved-with-caveat: pin the source commit +
#      digest-pin the resulting image). The default build ships the floor only, so
#      `docker build -f docker/aura-egress/Dockerfile -t aura-egress:build .` is deterministic
#      and needs no network fetch of a third-party repo.
#
# Digest-pinned base (D-13), resolved 2026-07-06 from the live registry (same pin as
# docker/aura-sandbox/Dockerfile):
#   debian:bookworm-slim -> sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df
FROM debian:bookworm-slim@sha256:60eac759739651111db372c07be67863818726f754804b8707c90979bda511df

# nftables is the ONLY hard dependency of the floor. ca-certificates + curl are kept so a
# later OpenSandbox FQDN build layer (or a supervisor fetching allow/deny lists) has TLS.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        nftables \
        curl \
    && rm -rf /var/lib/apt/lists/*

# The entrypoint: apply the always-on filter-table floor, then either hand off to OpenSandbox
# (FQDN mode, runc-only) or block so the sidecar stays a tracked, always-present container the
# DockerBackend tears down with the box. Inlined here so the image is a single self-contained
# file (no sibling script in the plan's file set).
#
# The floor is applied to the SHARED box netns (this container joined it via
# `--network container:<box>`), so the box's own outbound traffic is filtered even though the
# box holds no NET_ADMIN. If $AURA_EGRESS_FLOOR_RULESET is unset the entrypoint fails closed —
# a sidecar with no floor would silently leave the tenancy boundary open.
RUN cat > /usr/local/bin/aura-egress-entrypoint <<'EOF' \
    && chmod +x /usr/local/bin/aura-egress-entrypoint
#!/bin/sh
set -eu

if [ -z "${AURA_EGRESS_FLOOR_RULESET:-}" ]; then
    echo "aura-egress: FATAL: AURA_EGRESS_FLOOR_RULESET is empty — refusing to run a sidecar with no tenancy floor" >&2
    exit 1
fi

# Apply the always-on filter-table floor (gVisor-safe; no nat).
printf '%s' "$AURA_EGRESS_FLOOR_RULESET" | nft -f -
echo "aura-egress: filter-table floor applied"

# Opt-in tightened FQDN allowlist (runc-only). Only runs when the OpenSandbox egress binary
# was baked in AND the mode env is set (buildEgressSidecar sets it only for a configured,
# non-runsc allowlist). OpenSandbox reads OPENSANDBOX_EGRESS_RULES from the environment.
if [ -n "${OPENSANDBOX_EGRESS_MODE:-}" ]; then
    if [ -x /usr/local/bin/opensandbox-egress ]; then
        echo "aura-egress: starting OpenSandbox egress (mode=${OPENSANDBOX_EGRESS_MODE})"
        exec /usr/local/bin/opensandbox-egress
    fi
    echo "aura-egress: FATAL: FQDN mode requested but /usr/local/bin/opensandbox-egress is not baked into this image — rebuild with the pinned OpenSandbox egress binary" >&2
    exit 1
fi

# Floor-only: keep the sidecar alive so it is tracked and torn down with the box.
echo "aura-egress: floor-only mode — holding netns"
exec sleep infinity
EOF

ENTRYPOINT ["/usr/local/bin/aura-egress-entrypoint"]
