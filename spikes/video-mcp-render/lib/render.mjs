// The two pipelines S1 compares, run on the same browser binary and the same compiled project.
//   (a) renderer-server as shipped: ServerRenderer.renderVideo on the JSON our toVideoJSON made.
//   (b) our export page: the Studio's exportProject (withLocalFonts + primeDecodedBuffers +
//       exportVideo({worker: true})) bundled unchanged and driven by Playwright.
// Both record what the page asked the network for and which font faces it ended up with.
import { ServerRenderer } from '@videoflow/renderer-server';
import { chromium } from 'playwright';
import { launch } from './browsers.mjs';


/** Console lines, from both Node and the page, collected while `run` runs. */
export async function capturing(run) {
  const lines = [];
  const saved = {};
  for (const level of ['log', 'info', 'warn', 'error']) {
    saved[level] = console[level];
    console[level] = (...args) => {
      lines.push(`${level}: ${args.map(String).join(' ')}`.slice(0, 400));
    };
  }
  try {
    return { value: await run(lines), lines };
  } catch (error) {
    error.logs = lines.slice(-40);
    throw error;
  } finally {
    Object.assign(console, saved);
  }
}

function fontsInPage() {
  return {
    faces: [...document.fonts].map((f) => `${f.family.replace(/"/g, '')}/${f.style}/${f.status}`),
    atkinsonLoaded: [...document.fonts].some((f) => f.family.replace(/"/g, '') === 'Atkinson Hyperlegible Next' && f.status === 'loaded'),
  };
}

// renderer-server opens its page inside renderVideo(); the only seam that sees every request from
// the first one is the browser it launches, which is the `chromium` both of us import.
const requests = [];
const originalLaunch = chromium.launch.bind(chromium);
chromium.launch = async (options) => {
  const browser = await originalLaunch(options);
  const newContext = browser.newContext.bind(browser);
  browser.newContext = async (contextOptions) => {
    const context = await newContext(contextOptions);
    context.on('request', (request) => requests.push(request.url()));
    context.on('requestfailed', (request) => requests.push(`FAILED ${request.url()} ${request.failure()?.errorText ?? ''}`));
    return context;
  };
  return browser;
};

/** (a) renderer-server. `browserPath` undefined means its own default, `channel: 'chrome'`. */
export async function renderServer(json, { output, browserPath, onProgress, ffmpeg = false, elementCapture }) {
  if (browserPath === undefined) delete process.env.VIDEOFLOW_CHROME_PATH;
  else process.env.VIDEOFLOW_CHROME_PATH = browserPath;
  requests.length = 0;
  const renderer = new ServerRenderer(structuredClone(json));
  const { value, lines } = await capturing(async () => {
    try {
      const started = performance.now();
      await renderer.renderVideo({ outputType: 'file', output, verbose: true, onProgress, ffmpeg, elementCapture });
      const seconds = (performance.now() - started) / 1000;
      return { seconds, fonts: await renderer.page.evaluate(fontsInPage) };
    } finally {
      await renderer.cleanup();
    }
  });
  return { ...value, requests: [...requests], logs: lines.filter((l) => !/Encoding \d+%/.test(l)) };
}

/** (b) our page. Media is fetched by the page itself, from `urls`. */
export async function renderPage({ server, projectFile, upload, browserPath, size, mediaQuery = '?X-Amz-Signature=spike', mediaOrigin }) {
  const browser = await launch(browserPath);
  const logs = [];
  const external = [];
  try {
    const context = await browser.newContext({ viewport: size });
    const page = await context.newPage();
    page.on('console', (m) => logs.push(`${m.type()}: ${m.text()}`.slice(0, 400)));
    page.on('pageerror', (e) => logs.push(`pageerror: ${e.message}`.slice(0, 400)));
    page.on('request', (r) => {
      if (!r.url().startsWith(server.origin)) external.push(r.url());
    });
    page.on('requestfailed', (r) => external.push(`FAILED ${r.url()} ${r.failure()?.errorText ?? ''}`));
    await page.goto(`${server.origin}/dist/studio.html`);
    await page.waitForFunction(() => 'studio' in window);
    const result = await page.evaluate(
      async ({ projectFile, upload, mediaQuery, mediaOrigin }) => {
        const { project, missing } = await window.studio.loadProject(projectFile, {
          assetUrl: (id) => `/media/${encodeURIComponent(id)}`,
          credentials: 'same-origin',
        });
        const urls = { assetUrl: (id) => `${mediaOrigin}/media/${encodeURIComponent(id)}${mediaQuery}` };
        const started = performance.now();
        const blob = await window.studio.exportProject(project, urls);
        const seconds = (performance.now() - started) / 1000;
        const response = await fetch(`/upload/${upload}`, { method: 'POST', body: blob });
        return { missing, seconds, bytes: blob.size, uploaded: response.status };
      },
      { projectFile, upload, mediaQuery, mediaOrigin: mediaOrigin ?? server.origin },
    );
    return { ...result, fonts: await page.evaluate(fontsInPage), requests: external, logs, version: browser.version() };
  } finally {
    await browser.close();
  }
}

/** Our toVideoJSON, run in the page on the parsed project: the JSON (a) renders. */
export async function compileInPage({ server, projectFile, browserPath, mediaQuery = '?X-Amz-Signature=spike' }) {
  const browser = await launch(browserPath);
  try {
    const page = await browser.newPage();
    await page.goto(`${server.origin}/dist/studio.html`);
    await page.waitForFunction(() => 'studio' in window);
    return await page.evaluate(
      async ({ projectFile, origin, mediaQuery }) => {
        const { project, missing } = await window.studio.loadProject(projectFile, {
          assetUrl: (id) => `/media/${encodeURIComponent(id)}`,
          credentials: 'same-origin',
        });
        if (missing.length > 0) throw new Error(`missing sources: ${missing.join(', ')}`);
        return window.studio.toVideoJSON(project, {
          assetUrl: (id) => `${origin}/media/${encodeURIComponent(id)}${mediaQuery}`,
        });
      },
      { projectFile, origin: server.origin, mediaQuery },
    );
  } finally {
    await browser.close();
  }
}
