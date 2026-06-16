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
  'OPENROUTER_API_KEY',
  'NEO4J_PASSWORD',
  'POSTGRES_USER',
  'POSTGRES_PASSWORD',
  'POSTGRES_DB',
  'POSTGRES_HOST',
  'POSTGRES_PORT',
  'POSTGRES_SSLMODE',
  'AURA_NEO4J_BOLT_URL',
  'AURA_NEO4J_DATABASE',
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
// in the job env and read it here).
const SERVE_ORIGIN = 'http://127.0.0.1:9080';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: { baseURL: SERVE_ORIGIN, trace: 'on-first-retry' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: '../aura serve --only=cli',
    // /healthz (not /readyz) is the readiness gate: PG-only liveness, no Neo4j —
    // the e2e stack provisions Postgres only. A boot failure surfaces here as the
    // webServer never reaching ready within `timeout`.
    url: `${SERVE_ORIGIN}/healthz`,
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    env: serveEnv,
  },
});
