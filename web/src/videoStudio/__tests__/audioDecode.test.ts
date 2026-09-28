import { afterEach, describe, expect, it, vi } from 'vitest';
import { decodeMono, mixDown, waveformSamples, WAVEFORM_RATE } from '../audioDecode';

// The browser's decoder, stood in for: jsdom has no Web Audio, so an OfflineAudioContext that
// answers a two-channel buffer is enough to judge the mixing, the rate asked and the cache.

function buffer(channels: number[][]): AudioBuffer {
  return {
    length: channels[0]?.length ?? 0,
    numberOfChannels: channels.length,
    getChannelData: (index: number) => Float32Array.from(channels[index] ?? []),
  } as unknown as AudioBuffer;
}

const decoded = vi.hoisted(() => ({ rates: [] as number[] }));

function stubDecoder(answer: AudioBuffer) {
  vi.stubGlobal(
    'OfflineAudioContext',
    class {
      constructor(_channels: number, _length: number, rate: number) {
        decoded.rates.push(rate);
      }
      decodeAudioData(): Promise<AudioBuffer> {
        return Promise.resolve(answer);
      }
    },
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  decoded.rates.length = 0;
});

describe('mixDown', () => {
  it('averages the channels into one', () => {
    expect(
      Array.from(
        mixDown(
          buffer([
            [1, 0],
            [0, 1],
          ]),
        ),
      ),
    ).toEqual([0.5, 0.5]);
    expect(Array.from(mixDown(buffer([[0.25, -0.25]])))).toEqual([0.25, -0.25]);
  });
});

describe('decodeMono', () => {
  it('decodes at the rate asked and answers one channel', async () => {
    stubDecoder(
      buffer([
        [0.2, 0.4],
        [0.2, 0.4],
      ]),
    );
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response(new Uint8Array([1, 2])))),
    );
    const samples = await decodeMono('/a.wav', 16000);
    expect(decoded.rates).toEqual([16000]);
    expect(Array.from(samples)).toEqual([expect.closeTo(0.2, 6), expect.closeTo(0.4, 6)]);
  });

  it('is loud about a sound the server would not give', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('', { status: 404 }))),
    );
    await expect(decodeMono('/gone.wav', 8000)).rejects.toThrow(/404/);
  });
});

describe('waveformSamples', () => {
  it('decodes a source once per session at the waveform rate', async () => {
    stubDecoder(buffer([[0.5]]));
    const fetcher = vi.fn(() => Promise.resolve(new Response(new Uint8Array([1]))));
    vi.stubGlobal('fetch', fetcher);
    await waveformSamples('/once.wav');
    await waveformSamples('/once.wav');
    expect(fetcher).toHaveBeenCalledOnce();
    expect(decoded.rates).toEqual([WAVEFORM_RATE]);
  });

  it('forgets a failed decode, so the next look tries again', async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(new Response('', { status: 500 }))
      .mockResolvedValueOnce(new Response(new Uint8Array([1])));
    vi.stubGlobal('fetch', fetcher);
    stubDecoder(buffer([[0.5]]));
    await expect(waveformSamples('/retry.wav')).rejects.toThrow(/500/);
    await expect(waveformSamples('/retry.wav')).resolves.toHaveLength(1);
  });
});
