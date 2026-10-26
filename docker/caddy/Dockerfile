# A Caddy that can answer an ACME DNS-01 challenge.
#
# The official caddy image ships NO DNS provider modules — they are external Go
# modules compiled in at build time — so an appliance that wants a certificate a
# browser already trusts has to build its own. That is not a preference: HTTP-01
# cannot issue a wildcard, and without a wildcard the MCP Apps sandbox origin
# needs its own separately-provisioned name.
#
# DNS-01 also needs NOTHING inbound to be reachable at issuance time: the
# challenge is a TXT record, not a request. That is what lets this appliance hold
# a publicly-trusted certificate while publishing exactly one port.
#
# deSEC (desec.io) is the provider wired here: free, REST API, DNSSEC, open
# source, and — unlike DuckDNS, which has no wildcard at all — it answers the
# wildcard case that the two-origin design depends on.
#
# Caddy v2.11.7 requires golang.org/x/net v0.59.0 (CVE-2026-78669, fixed in v0.60.0; weekly
# security scan, 2026-10-24). xcaddy's --replace writes a replace directive, its README's way to
# move a dependency that is not a plugin. `go version -m` then shows v0.60.0 in the binary, and
# Trivy reports nothing for it. Drop it once upstream carries it: a replace pins a version, it
# does not raise a floor, so once Caddy requires a newer x/net this line would downgrade it.
FROM caddy:2-builder AS build
RUN xcaddy build --with github.com/caddy-dns/desec \
      --replace golang.org/x/net=golang.org/x/net@v0.60.0

FROM caddy:2
# caddy:2's Alpine base trails Alpine's own fixes: brotli-libs 1.2.0-r0 on 2026-10-24
# (CVE-2026-111497) and curl 8.22.0-r0 on 2026-10-26 (CVE-2026-92392, issue #189). apk upgrade
# takes every fix the base lacks, and the builders pass scripts/apt_week.sh, so it runs again
# once a week (https://docs.docker.com/reference/dockerfile/#impact-on-build-caching).
ARG APT_WEEK=""
RUN apk upgrade --no-cache
COPY --from=build /usr/bin/caddy /usr/bin/caddy
