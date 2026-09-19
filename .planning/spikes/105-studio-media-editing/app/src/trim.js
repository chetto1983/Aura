// The one video operation the spike measures: cut [start, end] out of a clip (optionally rotating,
// flipping, cropping, resizing or muting it) entirely in the browser with Mediabunny.
import {
  ALL_FORMATS,
  BlobSource,
  BufferTarget,
  Conversion,
  Input,
  Mp4OutputFormat,
  Output,
  WebMOutputFormat,
  getEncodableAudioCodecs,
  getEncodableVideoCodecs,
} from 'mediabunny';
import { log } from './log.js';

// The three cut modes the spike compares:
//   exact  — transcode everything: frame-exact cut, slow, needs an encoder in this browser.
//   expand — copy packets, widening the cut back to the previous key frame: fast, may show
//            up to one GOP before the requested start.
//   shrink — copy packets, narrowing the cut to the next key frame: fast, may lose up to one
//            GOP after the requested start.
const COPY_BY_MODE = {
  exact: false,
  expand: { boundaryPolicy: 'expand' },
  shrink: { boundaryPolicy: 'shrink' },
};

export async function describe(blob) {
  const input = new Input({ source: new BlobSource(blob), formats: ALL_FORMATS });
  try {
    const tracks = await input.getTracks();
    const described = [];
    for (const t of tracks) {
      const row = { type: t.type, codec: t.codec, decodable: await t.canDecode(), first: await t.getFirstTimestamp() };
      if (t.type === 'video') Object.assign(row, { width: t.displayWidth, height: t.displayHeight, rotation: t.rotation });
      described.push(row);
    }
    return { mime: await input.getMimeType(), duration: await input.computeDuration(), tracks: described };
  } finally {
    input.dispose?.();
  }
}

export async function capabilities() {
  return {
    webCodecs: typeof VideoEncoder !== 'undefined',
    encodableVideo: await getEncodableVideoCodecs(),
    encodableAudio: await getEncodableAudioCodecs(),
  };
}

export async function trim(blob, { start, end, mode = 'exact', rotate, flip, crop, width, mute, onProgress } = {}) {
  const t0 = performance.now();
  const isWebm = blob.type === 'video/webm' || /\.webm$/i.test(blob.name ?? '');
  const input = new Input({ source: new BlobSource(blob), formats: ALL_FORMATS });
  const output = new Output({
    format: isWebm ? new WebMOutputFormat() : new Mp4OutputFormat({ fastStart: 'in-memory' }),
    target: new BufferTarget(),
  });
  const conversion = await Conversion.init({
    input,
    output,
    trim: { start, end },
    copy: COPY_BY_MODE[mode],
    video: { rotate, flip, crop, width },
    audio: mute ? { discard: true } : undefined,
    showWarnings: false,
  });
  const discarded = conversion.discardedTracks.map((d) => ({ type: d.track.type, codec: d.track.codec, reason: d.reason }));
  if (!conversion.isValid) {
    log('error', 'conversion invalid', { discarded });
    throw new Error(`conversion invalid: ${JSON.stringify(discarded)}`);
  }
  conversion.onProgress = (p) => onProgress?.(p);
  await conversion.execute();
  const ms = Math.round(performance.now() - t0);
  const out = new Blob([output.target.buffer], { type: output.format.mimeType });
  const described = await describe(out);
  input.dispose?.();
  const result = { mode, start, end, ms, inBytes: blob.size, outBytes: out.size, discarded, out: described };
  log('video', `trim ${mode} ${start}-${end}s in ${ms} ms`, result);
  return { blob: out, result };
}
