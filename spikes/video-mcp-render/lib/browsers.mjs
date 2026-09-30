// The three browsers the spikes compare, by the binary each one runs. renderer-server launches
// `channel: 'chrome'` unless VIDEOFLOW_CHROME_PATH names a binary (ServerRenderer.js:91-97), so a
// binary path is what lets the same render run on each of them.
import { existsSync, readdirSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { chromium } from 'playwright';

const CACHE = process.env.PLAYWRIGHT_BROWSERS_PATH ?? join(homedir(), '.cache/ms-playwright');

function shellPath() {
  const dir = readdirSync(CACHE).find((name) => name.startsWith('chromium_headless_shell-'));
  const path = dir && join(CACHE, dir, 'chrome-headless-shell-linux64/chrome-headless-shell');
  return path && existsSync(path) ? path : undefined;
}

export function browserPaths() {
  return {
    'headless-shell': shellPath(),
    chromium: existsSync(chromium.executablePath()) ? chromium.executablePath() : undefined,
    chrome: existsSync('/opt/google/chrome/chrome') ? '/opt/google/chrome/chrome' : undefined,
  };
}

/** renderer-server's own launch flags (ServerRenderer.js:98-129), reused for our export page so
 *  the two pipelines differ only in the page they run. */
export const RENDER_ARGS = [
  '--no-sandbox',
  '--js-flags=--max-old-space-size=4096',
  '--enable-blink-features=CanvasDrawElement',
  '--disable-frame-rate-limit',
  '--disable-gpu-vsync',
  '--no-zygote',
  '--disable-gpu',
  '--disable-dev-shm-usage',
  '--disable-background-timer-throttling',
  '--disable-renderer-backgrounding',
  '--disable-features=site-per-process',
  '--disable-extensions',
];

export function launch(executablePath) {
  return chromium.launch({ headless: true, executablePath, args: RENDER_ARGS });
}
