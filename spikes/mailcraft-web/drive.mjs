// Drives MailCraft's web build framed by the spike host page in headless Chromium and reports
// what the page logged, the policy violations and the requests it made. The JMAP calls themselves
// are in the server's log (-log).
//
//   node spikes/mailcraft-web/drive.mjs --out <dir> --steps 'wait:15000,shot:inbox,click:0.45:0.3,wait:3000,shot:open'
//
// Steps: wait:<ms>, shot:<name>, click:<x>:<y> and dblclick:<x>:<y> (fractions of the frame),
// key:<chord>, type:<text>.
import { createRequire } from 'node:module';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { parseArgs } from 'node:util';

const require = createRequire(import.meta.url);
const { chromium } = require('playwright');

const { values: args } = parseArgs({
  options: {
    steps: { type: 'string', default: 'wait:15000,shot:inbox' },
    out: { type: 'string' },
    base: { type: 'string', default: 'http://127.0.0.1:8790' },
    locale: { type: 'string', default: 'it-IT' },
  },
});
mkdirSync(args.out, { recursive: true });

const browser = await chromium.launch({
  executablePath: '/opt/pw-browsers/chromium',
  headless: true,
  args: ['--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'],
});
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, locale: args.locale });
await context.addInitScript(() => {
  document.addEventListener('securitypolicyviolation', (e) => console.log(`CSP ${e.violatedDirective} ${e.blockedURI}`));
});
const page = await context.newPage();
const consoleLines = [];
const requests = [];
page.on('console', (m) => consoleLines.push(`[${m.type()}] ${m.text()}`.slice(0, 2000)));
page.on('pageerror', (e) => consoleLines.push(`[pageerror] ${e.message}`.slice(0, 2000)));
page.on('response', (r) => requests.push(`${r.status()} ${r.request().method()} ${r.url().replace(args.base, '')}`));

await page.goto(`${args.base}/`);
const box = async () => page.locator('#app').boundingBox();
const report = { steps: [] };
try {
  for (const step of args.steps.split(',').filter(Boolean)) {
    const [kind, ...rest] = step.split(':');
    if (kind === 'wait') await page.waitForTimeout(Number(rest[0]));
    else if (kind === 'shot') await page.screenshot({ path: join(args.out, `${rest[0]}.png`) });
    else if (kind === 'click' || kind === 'dblclick') {
      const b = await box();
      const x = b.x + b.width * Number(rest[0]);
      const y = b.y + b.height * Number(rest[1]);
      await (kind === 'click' ? page.mouse.click(x, y) : page.mouse.dblclick(x, y));
    } else if (kind === 'key') await page.keyboard.press(rest.join(':'));
    else if (kind === 'type') await page.keyboard.type(rest.join(':'), { delay: 30 });
    else throw new Error(`unknown step ${step}`);
    report.steps.push(step);
  }
} catch (e) {
  report.failure = String(e);
}
const frame = page.frames().find((f) => f.url().includes('/apps/mailcraft/'));
report.hook = frame ? await frame.evaluate(() => Boolean(window.mailcraftJmap && window.mailcraftJmap.accounts)) : null;
report.console = consoleLines;
report.requests = requests;
writeFileSync(join(args.out, 'report.json'), JSON.stringify(report, null, 2));
console.log(JSON.stringify({ failure: report.failure, hook: report.hook, steps: report.steps.length }, null, 2));
await browser.close();
