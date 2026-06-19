// https-proxy.mjs — a tiny TLS terminator for the Playwright WebKit (mobile-safari)
// E2E project. The cockpit session cookie is `__Host-aura_session; Secure` (a
// deliberate CWE-614/__Host- hardening). Chromium whitelists 127.0.0.1 as a secure
// context so it stores that cookie over plain http; WebKit does NOT, so an iOS-Safari
// device profile can never authenticate against `aura serve` over http. Real devices
// reach the cockpit over HTTPS (caddy :443) where the cookie works — this proxy
// reproduces that leg locally: it presents a self-signed cert to the browser and
// forwards every request to the loopback `aura serve` (http). The browser↔proxy hop is
// https (cookie honoured); the proxy↔serve hop is http on localhost (the serve only
// reads the cookie value, never the TLS state).
//
// Not wired into CI: the mobile-safari project is only added when AURA_E2E_HTTPS_ORIGIN
// is set (see playwright.config.ts). Env contract:
//   PROXY_TLS_KEY / PROXY_TLS_CERT  — PEM paths
//   PROXY_TLS_PFX                  — optional PFX path (Windows-friendly fallback)
//   PROXY_TARGET_PORT (default 9091) — the http `aura serve` port
//   PROXY_LISTEN_PORT (default 9443) — the https port the browser hits

import http from 'node:http';
import https from 'node:https';
import { readFileSync } from 'node:fs';

const TARGET_HOST = '127.0.0.1';
const TARGET_PORT = Number(process.env.PROXY_TARGET_PORT ?? 9091);
const LISTEN_PORT = Number(process.env.PROXY_LISTEN_PORT ?? 9443);
const targetHostHeader = `${TARGET_HOST}:${String(TARGET_PORT)}`;
const targetOrigin = `http://${targetHostHeader}`;

const tlsOptions =
  process.env.PROXY_TLS_PFX !== undefined
    ? { pfx: readFileSync(process.env.PROXY_TLS_PFX), passphrase: process.env.PROXY_TLS_PFX_PASSPHRASE ?? '' }
    : {
        key: readFileSync(process.env.PROXY_TLS_KEY ?? ''),
        cert: readFileSync(process.env.PROXY_TLS_CERT ?? ''),
      };

const server = https.createServer(tlsOptions, (req, res) => {
  // Rewrite Host/Origin/Referer to the target so any server-side same-origin check
  // sees a self-consistent http://127.0.0.1:<port> request. The browser keeps managing
  // cookies by the proxy origin (https://127.0.0.1:LISTEN_PORT) independently.
  const headers = { ...req.headers, host: targetHostHeader };
  if (headers.origin !== undefined) headers.origin = targetOrigin;
  if (typeof headers.referer === 'string') {
    headers.referer = headers.referer.replace(/^https:\/\/[^/]+/, targetOrigin);
  }

  const proxyReq = http.request(
    { host: TARGET_HOST, port: TARGET_PORT, method: req.method, path: req.url, headers },
    (proxyRes) => {
      res.writeHead(proxyRes.statusCode ?? 502, proxyRes.headers);
      proxyRes.pipe(res);
    },
  );
  proxyReq.on('error', (err) => {
    res.writeHead(502, { 'content-type': 'text/plain' });
    res.end(`proxy error: ${String(err)}`);
  });
  req.pipe(proxyReq);
});

server.listen(LISTEN_PORT, TARGET_HOST, () => {
  process.stdout.write(`https-proxy: listening on https://${TARGET_HOST}:${String(LISTEN_PORT)} -> ${targetOrigin}\n`);
});
