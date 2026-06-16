import { defineConfig, devices } from '@playwright/test';

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
    // Playwright's webServer does NOT inherit the runner env by default
    // (microsoft/playwright#19780). The 23-03 CI job wires the real
    // DB/Neo4j vars `aura serve` needs here explicitly.
    env: {},
  },
});
