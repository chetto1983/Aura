import { existsSync } from 'node:fs';
import { defineConfig, devices } from '@playwright/test';

// Playwright's webServer does NOT inherit the runner/process env by default
// (microsoft/playwright#19780). `aura serve` hard-requires a Postgres pool +
// config (db.Open is unconditional in bootServe), so the vars below must be
// forwarded explicitly into the webServer process from process.env. The 23-03
// web-e2e CI job provisions the stack (make db-up + migrate) and exports these;
// any unset var is dropped from the forwarded record (not passed as "undefined").
const SERVE_ENV_KEYS = [
  'AURA_DB_URL',
  'AURA_DB_MIGRATE_URL',
  'AURA_WEB_AUTH_PROVIDER',
  'AURA_AUTHULA_DATABASE_URL',
  'AURA_AUTHULA_SECRET',
  'AURA_AUTHULA_OPERATOR_IDENTITY',
  'AURA_AUTHULA_RATE_LIMIT_MAX',
  'OPENROUTER_API_KEY',
  'POSTGRES_USER',
  'POSTGRES_PASSWORD',
  'POSTGRES_DB',
  'POSTGRES_HOST',
  'POSTGRES_PORT',
  'POSTGRES_SSLMODE',
  'AURA_OBJECTSTORE_BACKEND',
  'AURA_OBJECTSTORE_ENDPOINT',
  'AURA_OBJECTSTORE_PUBLIC_ENDPOINT',
  'AURA_OBJECTSTORE_REGION',
  'AURA_OBJECTSTORE_BUCKET',
  'AURA_OBJECTSTORE_ACCESS_KEY',
  'AURA_OBJECTSTORE_SECRET_KEY',
  'AURA_OBJECTSTORE_PATH_STYLE',
] as const;

const serveEnv: Record<string, string> = {};
for (const key of SERVE_ENV_KEYS) {
  const value = process.env[key];
  if (value !== undefined) {
    serveEnv[key] = value;
  }
}

// SERVE_ORIGIN is the loopback origin `aura serve` listens on. It is the AG-UI
// gateway default (config.go: AGUIBind = envDefault("AURA_AGUI_BIND",
// "127.0.0.1:9080"), asserted in internal/config/config_test.go). The web-e2e job
// does NOT export AURA_AGUI_BIND, so this MUST track that default — single source
// for both baseURL and the webServer readiness URL so the coupling is visible. If
// the AGUIBind default ever moves, update this constant (or export AURA_AGUI_BIND
// in the job env and read it here). AURA_E2E_ORIGIN overrides it for a local run
// against an already-running serve on a non-default port (it also suppresses the
// managed webServer below so the existing instance is reused as-is).
const SERVE_ORIGIN = process.env.AURA_E2E_ORIGIN ?? 'http://127.0.0.1:9080';
const USE_EXTERNAL_SERVE = process.env.AURA_E2E_ORIGIN !== undefined;
const AURA_BINARY =
  process.platform === 'win32' && existsSync('../aura.exe') ? '..\\aura.exe' : '../aura';

// The cockpit session cookie is `__Host-aura_session; Secure`. WebKit (unlike Chromium,
// which whitelists 127.0.0.1) refuses to store a Secure cookie over plain http, so the
// mobile-safari (iPhone 13) profile can only authenticate against an HTTPS origin —
// exactly how a real device reaches the cockpit (caddy :443). When AURA_E2E_HTTPS_ORIGIN
// points at a local TLS terminator (web/e2e/https-proxy.mjs → the http serve), the
// mobile-safari project is enabled against it; otherwise it is omitted (so CI, which has
// no proxy, runs chromium + mobile-chrome only and stays green).
const HTTPS_ORIGIN = process.env.AURA_E2E_HTTPS_ORIGIN;
// CI runners provide branded Chrome and keep exercising that production-adjacent channel. Local
// WSL environments may only have Playwright's managed Chromium, so this explicit opt-in removes
// the channel override without changing the default/CI browser matrix.
const CHROMIUM_CHANNEL =
  process.env.AURA_E2E_USE_BUNDLED_CHROMIUM === 'true' ? {} : { channel: 'chrome' as const };

export default defineConfig({
  testDir: './e2e',
  snapshotPathTemplate: '{testDir}/__screenshots__/{testFilePath}/{arg}{ext}',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  // serviceWorkers: 'block' — the embedded cockpit registers a PWA service worker
  // (internal/webui/dist/sw.js); leaving it active makes the SW intercept fetches so
  // page.route() never sees them (the golden-replay E2E mocks /agent/run + /api/* at the
  // page-network layer). Blocking the SW keeps every request on the routable path; it does
  // not change the app under test (the SW is an offline-cache optimisation, not behaviour).
  use: {
    baseURL: SERVE_ORIGIN,
    trace: 'on-first-retry',
    // Evidence capture: a screenshot + a video are retained ONLY when a test fails, so
    // a green run leaves no artifacts but a regression is diagnosable (Phase 26 QA).
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    // Local appliance HTTPS uses Caddy's internal CA (and e2e may also presign uploads
    // to that HTTPS objectstore route), so every browser context must tolerate local
    // test certificates. Network/auth behavior is still exercised end to end.
    ignoreHTTPSErrors: true,
    serviceWorkers: 'block',
  },
  // Desktop chromium + two realistic mobile device profiles (Pixel 5 = mobile Chrome,
  // iPhone 13 = mobile WebKit). The Phase 26 typed-display surface (cards, citations,
  // Source Explorer sheet, pagination) is validated on all three so the cockpit holds
  // on a touch viewport, not just desktop.
  projects: [
    { name: 'chrome', use: { ...devices['Desktop Chrome'], ...CHROMIUM_CHANNEL } },
    { name: 'mobile-chrome', use: { ...devices['Pixel 5'], ...CHROMIUM_CHANNEL } },
    // mobile-safari is enabled only when an HTTPS origin is provided (the __Host-/Secure
    // cookie needs https for WebKit — see HTTPS_ORIGIN above). It overrides baseURL to the
    // TLS proxy and ignores the self-signed cert.
    ...(HTTPS_ORIGIN !== undefined
      ? [
          {
            name: 'mobile-safari',
            use: { ...devices['iPhone 13'], baseURL: HTTPS_ORIGIN },
          },
        ]
      : []),
  ],
  // When AURA_E2E_ORIGIN points at an already-running serve, do NOT manage a webServer
  // (the external instance is reused verbatim). Otherwise launch `aura serve` and gate on
  // /healthz (PG-only liveness; the e2e stack provisions Postgres only).
  ...(USE_EXTERNAL_SERVE
    ? {}
    : {
        webServer: {
          command: `${AURA_BINARY} serve --only=cli`,
          url: `${SERVE_ORIGIN}/healthz`,
          reuseExistingServer: !process.env.CI,
          timeout: 60_000,
          env: serveEnv,
        },
      }),
});
