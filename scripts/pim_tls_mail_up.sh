#!/usr/bin/env bash
# A fake mailbox over TLS and a PIM that trusts it, for web/e2e/mcp-cockpit-live.spec.ts.
#
# GreenMail serves IMAPS and SMTPS on loopback 993 and 465 with a throwaway self-signed
# certificate for 127.0.0.1, and its REST API at AURA_E2E_FAKE_MAIL_API (default
# http://127.0.0.1:18025), where the spec reads what arrived. Not 18080: that is where compose
# publishes SearXNG (AURA_SEARXNG_PORT).
# The mail ports are not a choice: the cockpit sends no security key for a domain discovery
# does not know, so the PIM takes it from the port, ssl on 993 and 465 and required STARTTLS on
# any other (spec amendment A2, ImapProviderInput.ForWrite in the PIM), and GreenMail offers
# implicit TLS only, never STARTTLS (measured 2026-11-23: no STARTTLS in CAPABILITY or EHLO).
#
# The PIM runs on the host network at 127.0.0.1:8093, issuer 127.0.0.1:9080, as in ci.yml's
# web-e2e job. It keeps MailKit's default certificate validation; SSL_CERT_FILE names a bundle of
# the image's own roots plus this certificate, which .NET on Linux reads through OpenSSL
# (learn.microsoft.com, "Cross-platform cryptography", trusted root locations on Linux).
# DOTNET_SYSTEM_NET_DISABLEIPV6 keeps .NET on IPv4 sockets: under Docker Desktop's host network
# its dual-stack HTTP client never reached Aura's issuer on 127.0.0.1 while curl did, and every
# token failed with IDX10500 (measured 2026-11-23, WSL with Docker Desktop).
# 127.0.0.1 is a private address, so only an identity holding identity.create may add the
# account (spec amendment A5): the spec signs in as the operator.
#
# Usage: bash scripts/pim_tls_mail_up.sh [down]
#   AURA_PIM_MCP_DEV_IMAGE picks the PIM image, e.g. a local build of a branch not yet published.
#   AURA_E2E_FAKE_MAIL_API moves GreenMail's API; the spec reads the same variable.
# The Aura under test needs AURA_PIM_MCP_URL=http://127.0.0.1:8093.
# Needs Docker and openssl, and loopback 993, 465, the API port and 8093 free: stop the compose PIM.
set -euo pipefail

PIM_IMAGE="${AURA_PIM_MCP_DEV_IMAGE:-ghcr.io/chetto1983/aura-pim-mcp:43c49b31c11eebfc041cdd5031cdc3cd45021f3a}"
MAIL_IMAGE="${PIM_E2E_MAIL_IMAGE:-greenmail/standalone:2.1.5}"
MAIL=aura-e2e-tls-mail
PIM=aura-e2e-tls-pim
DIR="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/aura-e2e-tls-mail"
KEYSTORE_PASSWORD=e2e-keystore-only
MAIL_API="${AURA_E2E_FAKE_MAIL_API:-http://127.0.0.1:18025}"

docker rm -f "$MAIL" "$PIM" >/dev/null 2>&1 || true
rm -rf "$DIR"
[ "${1:-}" = down ] && exit 0
# A failed bring-up leaves nothing running behind it.
trap 'docker rm -f "$MAIL" "$PIM" >/dev/null 2>&1 || true; rm -rf "$DIR"' ERR

mkdir -p "$DIR"
openssl req -x509 -newkey rsa:2048 -noenc -keyout "$DIR/mail.key" -out "$DIR/mail.crt" -days 2 \
  -subj '/CN=127.0.0.1' -addext 'subjectAltName=IP:127.0.0.1' >"$DIR/openssl.log" 2>&1
openssl pkcs12 -export -in "$DIR/mail.crt" -inkey "$DIR/mail.key" -out "$DIR/mail.p12" \
  -passout "pass:$KEYSTORE_PASSWORD"
docker run --rm --entrypoint cat "$PIM_IMAGE" /etc/ssl/certs/ca-certificates.crt >"$DIR/ca-bundle.pem"
cat "$DIR/mail.crt" >>"$DIR/ca-bundle.pem"
# Both containers run as non-root users that must read their file.
chmod 0644 "$DIR/mail.p12" "$DIR/ca-bundle.pem"

# GreenMail 2.1.5 reads one password for the keystore and the key (its Dockerfile sets only
# greenmail.tls.keystore.password). Without auth, any login works and a recipient's mailbox is
# created on delivery.
docker run -d --name "$MAIL" \
  -p 127.0.0.1:993:3993 -p 127.0.0.1:465:3465 -p "127.0.0.1:${MAIL_API##*:}:8080" \
  -v "$DIR/mail.p12:/run/aura-e2e/mail.p12:ro" \
  -e GREENMAIL_OPTS="-Dgreenmail.setup.test.all -Dgreenmail.hostname=0.0.0.0 -Dgreenmail.auth.disabled \
-Dgreenmail.tls.keystore.file=/run/aura-e2e/mail.p12 -Dgreenmail.tls.keystore.password=$KEYSTORE_PASSWORD" \
  "$MAIL_IMAGE" >/dev/null

# The PIM asks Aura's issuer for its metadata on the first request, not at boot, so Aura may
# start after it.
docker run -d --name "$PIM" --network host \
  -v "$DIR/ca-bundle.pem:/run/aura-e2e/ca-bundle.pem:ro" \
  -e SSL_CERT_FILE=/run/aura-e2e/ca-bundle.pem \
  -e DOTNET_SYSTEM_NET_DISABLEIPV6=1 \
  -e ASPNETCORE_URLS=http://127.0.0.1:8093 \
  -e CALENDAR_MCP_OAuth__Issuer=http://127.0.0.1:9080 \
  -e CALENDAR_MCP_OAuth__MetadataAddress=http://127.0.0.1:9080/.well-known/oauth-authorization-server \
  -e 'CALENDAR_MCP_OAuth__Resource=http://127.0.0.1:8093,http://127.0.0.1:8093/' \
  -e CALENDAR_MCP_OAuth__ToolsScope=mcp:tools \
  -e AURA_PIM_EMPTY_CONFIG_B64=eyJDYWxlbmRhck1jcCI6eyJBY2NvdW50cyI6W119fQ== \
  --entrypoint /bin/sh "$PIM_IMAGE" \
  -c 'test -s /app/data/appsettings.json || printf "%s" "$AURA_PIM_EMPTY_CONFIG_B64" | base64 -d > /app/data/appsettings.json; exec dotnet CalendarMcp.HttpServer.dll' >/dev/null

for endpoint in "$MAIL_API/api/service/readiness" http://127.0.0.1:8093/health; do
  curl --retry 30 --retry-delay 2 --retry-connrefused --retry-all-errors -fsS "$endpoint" >/dev/null
done
echo "==> GreenMail over TLS on 127.0.0.1:993/465 (API $MAIL_API), PIM $PIM_IMAGE on :8093 trusting it"
echo "==> run the spec with AURA_E2E_PIM_TLS_MAIL=1; tear down with: bash scripts/pim_tls_mail_up.sh down"
