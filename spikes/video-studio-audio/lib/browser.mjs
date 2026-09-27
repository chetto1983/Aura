// Starts the probe's Vite server and a Chromium on it. Every response the page receives is
// recorded with its size, so a probe can say exactly which bytes a package pulled in.
import { chromium, devices } from 'playwright';
import { createServer } from 'vite';
import { fileURLToPath } from 'node:url';

const ROOT = fileURLToPath(new URL('..', import.meta.url));

export async function openProbe(pagePath, { mobile = false } = {}) {
  const server = await createServer({ root: ROOT, configFile: `${ROOT}vite.config.ts` });
  await server.listen();
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext(
    mobile ? { ...devices['Pixel 5'] } : { viewport: { width: 1280, height: 800 } },
  );
  const page = await context.newPage();
  const assets = [];
  page.on('response', async (response) => {
    try {
      assets.push({ url: response.url(), bytes: (await response.body()).length });
    } catch {
      // A redirect or an aborted request has no body to count.
    }
  });
  page.on('console', (message) => console.log(`[page] ${message.text()}`));
  page.on('pageerror', (error) => console.log(`[page error] ${error.message}`));
  await page.goto(`http://127.0.0.1:5199/${pagePath}`);
  return {
    page,
    assets,
    close: async () => {
      await browser.close();
      await server.close();
    },
  };
}
