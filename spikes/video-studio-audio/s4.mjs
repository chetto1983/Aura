import { writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const probe = await openProbe('s4.html');
try {
  await probe.page.waitForFunction(() => 's4Results' in window, null, { timeout: 300_000 });
  const results = await probe.page.evaluate(() => window.s4Results);
  const heavy = probe.assets.filter((a) => /\.(wasm|onnx|mjs)(\?|$)/.test(a.url));
  const origins = [...new Set(probe.assets.map((a) => new URL(a.url).origin))];
  writeFileSync(new URL('out/s4.json', import.meta.url), JSON.stringify({ results, heavy, origins }, null, 2));
  console.table(results.map((r) => ({ ...r, errorsMs: JSON.stringify(r.errorsMs) })));
  console.table(heavy);
  console.log('origins:', origins);
} finally {
  await probe.close();
}
