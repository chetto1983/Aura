// audioDecode.ts — a sound's samples in the browser. The decoding is the browser's own
// (`decodeAudioData`), which resamples to the context's rate as it decodes: the waveform wants few
// samples, the noise reduction 48 kHz and the speech detector 16 kHz, and none of them has to
// resample by hand. Browser-only: the pure core never imports this (spec §The whole job).

/** Samples per second kept for a waveform: a hundred peaks a second need no more. */
export const WAVEFORM_RATE = 8000;

/** A decoded sound's channels averaged into one. */
export function mixDown(buffer: AudioBuffer): Float32Array<ArrayBuffer> {
  const mono = new Float32Array(buffer.length);
  for (let channel = 0; channel < buffer.numberOfChannels; channel += 1) {
    const data = buffer.getChannelData(channel);
    for (let index = 0; index < mono.length; index += 1) {
      mono[index] = (mono[index] ?? 0) + (data[index] ?? 0) / buffer.numberOfChannels;
    }
  }
  return mono;
}

/** A sound's samples, mono, at `rate`. */
export async function decodeMono(
  url: string,
  rate: number,
  signal?: AbortSignal,
): Promise<Float32Array<ArrayBuffer>> {
  const response = await fetch(url, { credentials: 'same-origin', signal: signal ?? null });
  if (!response.ok) {
    throw new Error(`videoStudio: the sound could not be read (${String(response.status)})`);
  }
  const context = new OfflineAudioContext(1, 1, rate);
  return mixDown(await context.decodeAudioData(await response.arrayBuffer()));
}

const waveforms = new Map<string, Promise<Float32Array<ArrayBuffer>>>();

/** A source's waveform samples, decoded once per session: peaks are cheap to cut from them and
 *  would bloat the project file if saved (spec §Browser-side analysis). A failed decode is
 *  forgotten, so the next look tries again. */
export function waveformSamples(url: string): Promise<Float32Array<ArrayBuffer>> {
  const cached = waveforms.get(url);
  if (cached !== undefined) return cached;
  const decoding = decodeMono(url, WAVEFORM_RATE);
  waveforms.set(url, decoding);
  decoding.catch(() => {
    waveforms.delete(url);
  });
  return decoding;
}
