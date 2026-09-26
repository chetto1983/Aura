#!/usr/bin/env bash
# Builds aura-sandbox:spike and aura-egress:spike from the repo Dockerfiles, adding only:
#   - an early layer trusting an HTTPS-intercepting egress proxy (CA_BUNDLE), with apt on https;
#   - (sandbox only) the verified agent-browser binary (AB_BIN) before WORKDIR /workspace.
# The repo Dockerfiles are never modified. Usage (from the repo root):
#   CA_BUNDLE=/root/.ccr/ca-bundle.crt AB_BIN=.../agent-browser-linux-x64 bash spikes/agent-browser-auth/build_images.sh
set -euo pipefail
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cp "$CA_BUNDLE" "$work/ccr-ca.crt"
cp "$AB_BIN" "$work/agent-browser"

python3 - "$work" <<'PY'
import sys
work = sys.argv[1]
ca = '''
COPY --from=spike ccr-ca.crt /usr/local/share/ca-certificates/ccr-ca.crt
RUN printf 'Acquire::https::CAInfo "/usr/local/share/ca-certificates/ccr-ca.crt";\\n' > /etc/apt/apt.conf.d/99ccr \\
    && sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources
'''
ca_env = '''ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt REQUESTS_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt \\
    PIP_CERT=/etc/ssl/certs/ca-certificates.crt NODE_EXTRA_CA_CERTS=/usr/local/share/ca-certificates/ccr-ca.crt
'''
ab = 'COPY --from=spike --chmod=0755 agent-browser /usr/local/bin/agent-browser\n'

def patch(src, dst, extra_head, tail):
    lines = open(src).read().split('\n')
    i = next(n for n, l in enumerate(lines) if l.startswith('FROM '))
    lines.insert(i + 1, ca + extra_head)
    if tail:
        j = max(n for n, l in enumerate(lines) if l.startswith('WORKDIR /workspace'))
        lines.insert(j, tail)
    open(dst, 'w').write('\n'.join(lines))

patch('docker/aura-sandbox/Dockerfile', f'{work}/Dockerfile.sandbox', ca_env, ab)
patch('docker/aura-egress/Dockerfile', f'{work}/Dockerfile.egress', '', '')
PY

proxy_args=()
for v in HTTPS_PROXY https_proxy HTTP_PROXY http_proxy; do proxy_args+=(--build-arg "$v=${HTTPS_PROXY:-}"); done
for img in egress sandbox; do
  docker build --network host --build-context "spike=$work" -f "$work/Dockerfile.$img" \
    "${proxy_args[@]}" --build-arg NO_PROXY=localhost,127.0.0.1 -t "aura-$img:spike" .
done
