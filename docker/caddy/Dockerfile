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
FROM caddy:2-builder AS build
RUN xcaddy build --with github.com/caddy-dns/desec

FROM caddy:2
COPY --from=build /usr/bin/caddy /usr/bin/caddy
