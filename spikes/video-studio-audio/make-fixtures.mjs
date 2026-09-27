import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { openProbe } from './lib/browser.mjs';

const OUT = new URL('../../web/e2e/fixtures/video-studio/audio/', import.meta.url);
const phrases = [1, 2, 3].map((i) =>
  readFileSync(new URL(`out/phrase-${i}.bin`, import.meta.url)).toString('base64'),
);
const probe = await openProbe('fixtures.html');
try {
  await probe.page.waitForFunction(() => 'buildFixtures' in window);
  const built = await probe.page.evaluate((p) => window.buildFixtures(p), phrases);
  mkdirSync(OUT, { recursive: true });
  writeFileSync(new URL('speech.wav', OUT), Buffer.from(built.speech, 'base64'));
  writeFileSync(new URL('speech-noisy.wav', OUT), Buffer.from(built.noisy, 'base64'));
  writeFileSync(new URL('speech.truth.json', OUT), `${JSON.stringify(built.truth, null, 2)}\n`);
  console.log(JSON.stringify(built.truth));
} finally {
  await probe.close();
}
