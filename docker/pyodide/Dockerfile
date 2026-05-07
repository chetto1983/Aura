FROM node:22-bookworm-slim AS node

FROM pyodide/pyodide-env:latest@sha256:dbb8fbce6c98d17fd68e777b0cdfe731c286f7595105747d79ded1945917cf6c

WORKDIR /runtime

COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY . /runtime

ENV NODE_OPTIONS=--max-old-space-size=4096

EXPOSE 8787

HEALTHCHECK --interval=30s --timeout=10s --start-period=90s --retries=3 \
  CMD python3 -c "import json, urllib.request; body=json.loads(urllib.request.urlopen('http://127.0.0.1:8787/health', timeout=8).read().decode('utf-8')); raise SystemExit(0 if body.get('ok') else 1)"

CMD ["node", "/runtime/runner/aura-pyodide-server.mjs", "--runtime-dir", "/runtime", "--host", "0.0.0.0", "--port", "8787"]
