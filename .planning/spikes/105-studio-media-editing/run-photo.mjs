// Drives the Photo lab: saves every sample through Filerobot's own save function, clicks a filter
// and the Save button like a user would, and records console errors and off-origin requests.
import { mkdirSync, writeFileSync } from 'node:fs';
import { chromium, firefox } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';

const channel = process.argv[2] ?? 'chrome';
const dir = `out/photos/${channel}`;
mkdirSync(dir, { recursive: true });
const browser = channel === 'firefox' ? await firefox.launch() : await chromium.launch({ channel });
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
page.setDefaultTimeout(120_000);
const consoleIssues = [];
const offOrigin = [];
page.on('console', (m) => ['error', 'warning'].includes(m.type()) && consoleIssues.push(`${m.type()}: ${m.text().slice(0, 240)}`));
page.on('response', (res) => res.status() >= 400 && consoleIssues.push(`http ${res.status()}: ${res.url()}`));
page.on('pageerror', (e) => consoleIssues.push(`pageerror: ${e.message.slice(0, 240)}`));
page.on('request', (r) => !r.url().startsWith('http://localhost') && !r.url().startsWith('data:') && !r.url().startsWith('blob:') && offOrigin.push(r.url()));

async function loadSample(name) {
  await page.evaluate((n) => window.__spike.photoLoad(n), name);
  // The editor swaps its canvas once the new source decodes; give it the time a 12 MP JPEG needs.
  await page.waitForTimeout(name.startsWith('photo') ? 3000 : 1500);
}

await page.goto('http://localhost:5199/?tab=photo');
await page.waitForFunction(() => window.__spike?.photoSave);
await page.waitForTimeout(2000);
await page.screenshot({ path: `${dir}/ui-initial.png` });

const saves = [];
for (const [name, ext, ratio] of [
  ['studio-1024.png', 'png', 1],
  ['studio-1024.webp', 'webp', 1],
  ['photo-12mp.jpg', 'jpeg', 1],
  ['photo-exif6.jpg', 'jpeg', 1],
  ['photo-12mp.jpg', 'jpeg', 4],
]) {
  await loadSample(name);
  try {
    const r = await page.evaluate(([n, e, p]) => window.__spike.photoSave(n.replace(/\.\w+$/, ''), e, p), [name, ext, ratio]);
    writeFileSync(`${dir}/${name.replace(/\.\w+$/, '')}-x${ratio}.${ext}`, Buffer.from(r.base64.split(',')[1], 'base64'));
    delete r.base64;
    saves.push({ name, ext, ratio, ...r });
    console.log('save', name, ext, `x${ratio}`, JSON.stringify(r));
  } catch (e) {
    saves.push({ name, ext, ratio, error: e.message.slice(0, 200) });
    console.log('save FAILED', name, ext, `x${ratio}`, e.message.slice(0, 200));
  }
}

// A user's path: pick a filter, then Save → the save modal → confirm.
await loadSample('studio-1024.png');
await page.getByText('Filters', { exact: true }).first().click();
await page.waitForTimeout(800);
await page.screenshot({ path: `${dir}/ui-filters.png` });
const logBefore = await page.evaluate(() => window.__spikeLog.events.length);
const filterNames = await page.locator('[class*="FilterItem"], [class*="filter-item"], .FIE_filters-item').allInnerTexts().catch(() => []);
console.log('filter items seen:', filterNames.slice(0, 12).join(' | '));
await page.getByText('Clarendon', { exact: true }).first().click().catch((e) => console.log('no Clarendon:', e.message.slice(0, 120)));
await page.waitForTimeout(500);
await page.getByRole('button', { name: 'Save', exact: true }).first().click();
await page.waitForTimeout(800);
await page.screenshot({ path: `${dir}/ui-save-modal.png` });
const modalSave = page.getByRole('button', { name: 'Save', exact: true });
if ((await modalSave.count()) > 1) await modalSave.last().click();
await page.waitForTimeout(1500);
const uiSave = await page.evaluate((n) => window.__spikeLog.events.slice(n).filter((e) => e.category === 'photo'), logBefore);
console.log('ui save events:', JSON.stringify(uiSave));
await page.screenshot({ path: `${dir}/ui-after-save.png` });

writeFileSync(`${dir}/report.json`, JSON.stringify({ userAgent: await page.evaluate(() => navigator.userAgent), saves, uiSave, consoleIssues, offOrigin }, null, 2));
console.log('console issues:', consoleIssues.length, '\n ', [...new Set(consoleIssues)].slice(0, 12).join('\n  '));
console.log('off-origin requests:', offOrigin);
await browser.close();
