// Our own Italian table, fully offline: the tabs and tools must read in Italian with no request
// leaving localhost.
import { chromium } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';
const browser = await chromium.launch({ channel: 'chrome' });
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
const offOrigin = [];
page.on('request', (r) => !/^(http:\/\/localhost|data:|blob:)/.test(r.url()) && offOrigin.push(r.url()));
await page.goto('http://localhost:5199/?tab=photo');
await page.waitForFunction(() => window.__spike?.photoSave);
await page.locator('select').last().selectOption('it-aura');
await page.waitForTimeout(2500);
await page.getByText('Filtri', { exact: true }).first().click();
await page.waitForTimeout(800);
await page.screenshot({ path: 'out/photos/chrome/ui-it-aura.png' });
console.log('Regola/Filtri/Salva visible:', await page.getByText('Regola', { exact: true }).count(), await page.getByText('Filtri', { exact: true }).count(), await page.getByText('Salva', { exact: true }).count());
console.log('off-origin:', offOrigin);
await browser.close();
