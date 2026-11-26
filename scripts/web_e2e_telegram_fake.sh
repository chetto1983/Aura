#!/usr/bin/env bash
# Makes api.telegram.org answer getMe locally for the web-e2e specs that provision a second
# identity (scripts/telegram_getme_fake.py says why). It creates a throwaway CA and a certificate
# for api.telegram.org signed by it, starts the fake on 127.0.0.1:443, resolves the name there
# through /etc/hosts, and writes a CA bundle (the system roots plus the throwaway CA) whose path it
# prints: aura serve reads it through SSL_CERT_FILE, so nothing in the product changes.
#
#   bundle="$(sudo bash scripts/web_e2e_telegram_fake.sh DIR)"
#
# Root is needed for port 443 and /etc/hosts. The CA lives one day and only in DIR.
set -euo pipefail

dir="${1:?usage: web_e2e_telegram_fake.sh DIR}"
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$dir"

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=Aura CI throwaway CA" \
  -addext "basicConstraints=critical,CA:TRUE" -addext "keyUsage=critical,keyCertSign" \
  -keyout "$dir/ca.key" -out "$dir/ca.crt" 2>/dev/null
openssl req -newkey rsa:2048 -nodes -subj "/CN=api.telegram.org" \
  -keyout "$dir/telegram.key" -out "$dir/telegram.csr" 2>/dev/null
printf 'subjectAltName=DNS:api.telegram.org\nextendedKeyUsage=serverAuth\n' >"$dir/telegram.ext"
openssl x509 -req -days 1 -in "$dir/telegram.csr" -CA "$dir/ca.crt" -CAkey "$dir/ca.key" \
  -CAcreateserial -extfile "$dir/telegram.ext" -out "$dir/telegram.crt" 2>/dev/null
cat /etc/ssl/certs/ca-certificates.crt "$dir/ca.crt" >"$dir/bundle.pem"

nohup python3 "$here/telegram_getme_fake.py" "$dir/telegram.crt" "$dir/telegram.key" \
  >"$dir/fake.log" 2>&1 &
grep -q ' api.telegram.org$' /etc/hosts || echo '127.0.0.1 api.telegram.org' >>/etc/hosts

for _ in $(seq 1 20); do
  if curl -fsS --cacert "$dir/bundle.pem" https://api.telegram.org/bot0:fake/getMe >/dev/null 2>&1; then
    echo "$dir/bundle.pem"
    exit 0
  fi
  sleep 0.5
done
echo "the getMe fake did not answer on api.telegram.org:443" >&2
cat "$dir/fake.log" >&2
exit 1
