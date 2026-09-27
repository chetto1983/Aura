import { writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const probe = await openProbe('s1.html');
try {
  await probe.page.waitForFunction(() => 's1Results' in window, null, { timeout: 120_000 });
  const results = await probe.page.evaluate(() => window.s1Results);
  writeFileSync(new URL('out/s1.json', import.meta.url), JSON.stringify(results, null, 2));
  for (const r of results) {
    console.log(`${r.name}: level ${r.firstSecondDb} dBFS, drop at timeline ${r.dropAtTimeline ?? 'none'} s`);
  }
} finally {
  await probe.close();
}
