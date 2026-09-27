import type { Page } from '@playwright/test';

// audioMeasure.ts — how loud an exported file is, a tenth of a second at a time. The audio is
// decoded in the page, like the frames in video-studio.spec.ts: Node has no WebCodecs, and the
// browser's decoder is the one the operator's player uses.

export const WINDOW = 0.1;

/** Mean power (mean square over every channel) of each whole 100 ms window of the file's audio. */
export async function windowPowers(page: Page, bytes: Buffer): Promise<readonly number[]> {
  return page.evaluate(
    async ({ base64, window }) => {
      const data = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
      const decoded = await new OfflineAudioContext(1, 1, 48_000).decodeAudioData(data.buffer);
      const size = Math.round(decoded.sampleRate * window);
      const channels = Array.from({ length: decoded.numberOfChannels }, (_, index) =>
        decoded.getChannelData(index),
      );
      const powers: number[] = [];
      for (let start = 0; start + size <= decoded.length; start += size) {
        let sum = 0;
        for (const channel of channels) {
          for (let at = start; at < start + size; at += 1) sum += (channel[at] ?? 0) ** 2;
        }
        powers.push(sum / (size * channels.length));
      }
      return powers;
    },
    { base64: bytes.toString('base64'), window: WINDOW },
  );
}

/** The level, in dBFS, of the windows lying wholly inside [from, to). */
export function levelBetween(powers: readonly number[], from: number, to: number): number {
  const inside = powers.slice(Math.ceil(from / WINDOW - 1e-9), Math.floor(to / WINDOW + 1e-9));
  if (inside.length === 0) {
    throw new Error(`no whole window between ${String(from)} and ${String(to)} s`);
  }
  const mean = inside.reduce((sum, power) => sum + power, 0) / inside.length;
  return 10 * Math.log10(Math.max(mean, 1e-20));
}
