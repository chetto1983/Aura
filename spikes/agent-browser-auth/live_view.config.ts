// Playwright config for live_view.e2e.ts against an already running `aura serve`.
// Usage (from web/): npx playwright test -c ../spikes/agent-browser-auth/live_view.config.ts
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: 'live_view.e2e.ts',
  timeout: 120_000,
  reporter: 'list',
  use: {
    baseURL: process.env.AURA_E2E_ORIGIN ?? 'http://127.0.0.1:9080',
    viewport: { width: 1440, height: 900 },
    launchOptions: process.env.AURA_E2E_CHROMIUM ? { executablePath: process.env.AURA_E2E_CHROMIUM } : {},
    trace: 'retain-on-failure',
  },
});
