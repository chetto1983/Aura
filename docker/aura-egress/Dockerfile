# docker/aura-egress/Dockerfile — the per-box EGRESS SIDECAR image (Phase 37, SBX-04,
# D-03/D-05/D-07). DockerBackend launches one of these beside every networked box, sharing
# the box network namespace via `--network container:<box>` with CAP_NET_ADMIN on the
# SIDECAR ONLY (the box stays net-unprivileged). It enforces the tenancy boundary at the
# network layer: full public internet (D-04) minus RFC1918 + the 169.254.169.254 metadata
# IP + the shared-services Docker bridge (D-05).
#
# One enforcement job (37-RESEARCH Pattern 6): an always-on FILTER-TABLE floor. The
# entrypoint applies the Go-generated nftables ruleset from $AURA_EGRESS_FLOOR_RULESET with
# `nft -f -`; it never touches the nat table. An opt-in OpenSandbox FQDN allowlist mode was
# removed on 2026-11-25: no build ever baked its binary in, so asking for it only made the
# sidecar exit at boot.
#
# Digest-pinned base (D-13), the same pin as docker/aura-sandbox/Dockerfile. Dependabot
# refreshes it when bookworm-slim moves, before Hub can collect the superseded manifest.
FROM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc

# A cached RUN keeps the packages it installed: on 2026-10-26 aura-egress:edge still had the
# libssl3 of 2026-10-24, deb12u1, while bookworm had deb12u2 (CVE-2026-84782, issue #189). The
# builders pass scripts/apt_week.sh, so this layer runs again once a week, and apt-get upgrade
# also takes the fixes the base itself lacks.
ARG APT_WEEK=""

# nftables is the ONLY dependency of the floor; ca-certificates lets apt itself use https.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get upgrade -y \
    && apt-get install -y --no-install-recommends \
        nftables \
    && rm -rf /var/lib/apt/lists/*

# The entrypoint: apply the always-on filter-table floor, then block so the sidecar stays a
# tracked, always-present container the DockerBackend tears down with the box. Inlined here so the image is a single self-contained
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

# Apply the always-on filter-table floor (no nat).
printf '%s' "$AURA_EGRESS_FLOOR_RULESET" | nft -f -
echo "aura-egress: filter-table floor applied"

# Keep the sidecar alive so it is tracked and torn down with the box.
echo "aura-egress: floor applied — holding netns"
exec sleep infinity
EOF

ENTRYPOINT ["/usr/local/bin/aura-egress-entrypoint"]
