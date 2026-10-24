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
# Two fixes from the weekly security scan (2026-10-24) wait on upstream, so they are made here:
# - Caddy v2.11.7 requires golang.org/x/net v0.59.0 (CVE-2026-78669, fixed in v0.60.0). xcaddy's
#   --replace writes a replace directive, its README's way to move a dependency that is not a
#   plugin. `go version -m` then shows v0.60.0 in the binary, and Trivy reports nothing for it.
# - caddy:2's Alpine base ships brotli-libs 1.2.0-r0 (CVE-2026-111497). Alpine has 1.2.1-r0.
# Drop each once upstream carries it. A replace pins a version, it does not raise a floor: once
# Caddy requires a newer x/net, this line would downgrade it.
FROM caddy:2-builder AS build
RUN xcaddy build --with github.com/caddy-dns/desec \
      --replace golang.org/x/net=golang.org/x/net@v0.60.0

FROM caddy:2
RUN apk add --no-cache --upgrade brotli-libs
COPY --from=build /usr/bin/caddy /usr/bin/caddy
