import { writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const probe = await openProbe('s3.html');
try {
  await probe.page.waitForFunction(() => 's3Results' in window, null, { timeout: 300_000 });
  const results = await probe.page.evaluate(() => window.s3Results);
  const base = results.find((r) => r.name === 'none');
  const rows = results.map((r) => ({
    suppressor: r.name,
    'noise floor drop (dB)': (base.noisy.gapDb - r.noisy.gapDb).toFixed(1),
    'speech loss on noisy (dB)': (base.noisy.speechDb - r.noisy.speechDb).toFixed(1),
    'speech loss on clean (dB)': (base.clean.speechDb - r.clean.speechDb).toFixed(1),
    '× real time': r.noisy.realtime,
    'latency on clean (ms)': r.clean.latencyMs,
  }));
  const wasm = probe.assets.filter((a) => a.url.endsWith('.wasm'));
  writeFileSync(new URL('out/s3.json', import.meta.url), JSON.stringify({ results, wasm }, null, 2));
  console.table(rows);
  console.table(wasm);
} finally {
  await probe.close();
}
