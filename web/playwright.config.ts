import { defineConfig, devices } from '@playwright/test';

/**
 * Phase 12 E2E config.
 *
 * Drives the dashboard at AURA_DASHBOARD_URL (default http://localhost:8081)
 * with a bearer token from AURA_E2E_TOKEN (mint one via the
 * `request_dashboard_token` Telegram tool, then export the token in your
 * shell or write it to web/.e2e-token).
 *
 * Run:
 *   cd web
 *   AURA_DASHBOARD_URL=http://localhost:8081 \
 *   AURA_E2E_TOKEN=<your-bearer-token> \
 *   npx playwright test
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: false, // dashboard reads shared SQLite; serialize for now
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: process.env.AURA_DASHBOARD_URL ?? 'http://localhost:8081',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
