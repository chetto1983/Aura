// Which wait lets a worklet suppressor finish its WASM init before an offline render outruns it.
import { openProbe } from './lib/browser.mjs';

for (const mode of ['prestart', 'suspend']) {
  for (const wait of [0, 50, 500]) {
    const probe = await openProbe(`s3.html?only=rnnoise&mode=${mode}&wait=${wait}`);
    try {
      await probe.page.waitForFunction(() => 's3Results' in window, null, { timeout: 120_000 });
      const results = await probe.page.evaluate(() => window.s3Results);
      const [base, rnnoise] = results;
      console.log(
        `${mode} wait=${wait}ms: floor drop ${(base.noisy.gapDb - rnnoise.noisy.gapDb).toFixed(1)} dB, ` +
          `speech loss ${(base.noisy.speechDb - rnnoise.noisy.speechDb).toFixed(1)} dB`,
      );
    } finally {
      await probe.close();
    }
  }
}
