// Q3: runs the scripted undo/redo session against the immer-patch history and the zundo history and
// writes out/history-report.json. Usage: node run-history.mjs
import { mkdirSync, writeFileSync } from 'node:fs';
import { chromium } from 'file:///D:/Repo/Aura/web/node_modules/playwright/index.mjs';

mkdirSync('out', { recursive: true });
const browser = await chromium.launch({ channel: 'chrome' });
const page = await browser.newPage();
const errors = [];
page.on('pageerror', (e) => errors.push(e.message));
await page.goto('http://localhost:5207/?tab=history');
await page.waitForFunction(() => window.__spike?.history);
const results = [];
for (const kind of ['immer', 'zundo']) {
  const r = await page.evaluate((k) => window.__spike.history(k), kind);
  results.push(r);
  console.log(`${kind}: ${r.pass ? 'PASS' : 'FAIL'} history=${r.historyChars} chars`, r.timings.map((t) => `${t.label} ${t.ms} ms`).join(', '));
  for (const c of r.checks.filter((c) => !c.ok)) console.log('  ✗', c.name, JSON.stringify(c.extra ?? ''));
}
writeFileSync('out/history-report.json', JSON.stringify({ results, errors }, null, 2));
console.log('page errors:', errors);
await browser.close();
