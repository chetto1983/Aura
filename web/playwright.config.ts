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

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: { baseURL: 'http://127.0.0.1:9080', trace: 'on-first-retry' },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: '../aura serve --only=cli',
    url: 'http://127.0.0.1:9080/healthz',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
    env: serveEnv,
  },
});
