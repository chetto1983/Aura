// What does Filerobot's own Italian look like, where does it come from, and does the editor fit a
// phone? Toggles the Scaleflex translation backend with language=it and records the request.
import { writeFileSync } from 'node:fs';
import { chromium } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';
const browser = await chromium.launch({ channel: 'chrome' });
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
const calls = [];
page.on('response', async (r) => {
  if (r.url().includes('ultrafast.io')) calls.push({ url: r.url(), status: r.status(), body: (await r.text().catch(() => '')).slice(0, 400000) });
});
await page.goto('http://localhost:5199/?tab=photo');
await page.waitForFunction(() => window.__spike?.photoSave);
await page.locator('select').last().selectOption('it');
await page.getByLabel('traduzioni dal server Scaleflex').check();
await page.waitForTimeout(4000);
await page.screenshot({ path: 'out/photos/chrome/ui-it-backend.png' });
for (const c of calls) {
  let keys = null, itKeys = null;
  try { const j = JSON.parse(c.body); keys = Object.keys(j); itKeys = j.it ? Object.keys(j.it).length : null; } catch {}
  console.log(c.status, c.url, 'top-level keys:', keys?.slice(0, 12), 'it keys:', itKeys);
}
writeFileSync('out/photos/chrome/it-backend.json', JSON.stringify(calls.map((c) => ({ url: c.url, status: c.status })), null, 2));
const labels = await page.locator('.FIE_tabs, [class*="Tabs"]').first().innerText().catch(() => '');
console.log('tab labels:', labels.replace(/\n+/g, ' | '));
// Phone.
const phone = await browser.newPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2 });
await phone.goto('http://localhost:5199/?tab=photo');
await phone.waitForFunction(() => window.__spike?.photoSave);
await phone.waitForTimeout(2500);
await phone.screenshot({ path: 'out/photos/chrome/ui-phone.png' });
await browser.close();
