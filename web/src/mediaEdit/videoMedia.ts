import {
  ALL_FORMATS,
  BlobSource,
  BufferTarget,
  CanvasSink,
  Conversion,
  Input,
  Mp4OutputFormat,
  Output,
  WebMOutputFormat,
} from 'mediabunny';
import {
  blockingDiscards,
  conversionOptions,
  videoContainer,
  type BlockedTrack,
  type VideoEdit,
} from './editRules';

// videoMedia.ts — the only file that runs Mediabunny. Source and output both live in memory
// (BlobSource + BufferTarget), which is why the editor asks before opening a very large clip.

export interface VideoInfo {
  readonly duration: number;
  /** Display size: the file's own rotation already applied. */
  readonly width: number;
  readonly height: number;
  readonly hasAudio: boolean;
  /** Whether this browser holds a decoder for the video track. Parsing the container is not
   *  the same question: an MPEG-4 Part 2 clip answers a size and a duration and still has no
   *  decoder anywhere, and a caller that composes it gets black frames rather than an error. */
  readonly decodable: boolean;
}

/** `empty`: the conversion ran and wrote nothing. A kind rather than an Error, so the editor
 *  words it in the operator's language instead of showing this file's English. */
export type ExportResult =
  | { readonly kind: 'done'; readonly blob: Blob }
  | { readonly kind: 'blocked'; readonly tracks: readonly BlockedTrack[] }
  | { readonly kind: 'empty' }
  | { readonly kind: 'canceled' };

const CANCELED: ExportResult = { kind: 'canceled' };

function openInput(source: Blob): Input {
  return new Input({ source: new BlobSource(source), formats: ALL_FORMATS });
}

function abortError(): DOMException {
  return new DOMException('The read was aborted.', 'AbortError');
}

/** Runs `read` on the file, then lets go of it. An abort disposes the Input at once, which makes
 *  Mediabunny cancel the read or decode it has in flight, and rejects with an AbortError without
 *  waiting for that read to notice: a step such as canDecode() is not tied to the Input at all. */
async function reading<T>(
  source: Blob,
  signal: AbortSignal | undefined,
  read: (input: Input) => Promise<T>,
): Promise<T> {
  if (signal?.aborted) throw abortError();
  const input = openInput(source);
  let stop: () => void = () => undefined;
  const stopped = new Promise<never>((_resolve, reject) => {
    stop = () => {
      input.dispose();
      reject(abortError());
    };
  });
  signal?.addEventListener('abort', stop, { once: true });
  try {
    return await Promise.race([read(input), stopped]);
  } finally {
    signal?.removeEventListener('abort', stop);
    input.dispose();
  }
}

export function probeVideo(source: Blob, signal?: AbortSignal): Promise<VideoInfo> {
  return reading(source, signal, async (input) => {
    const video = await input.getPrimaryVideoTrack();
    if (video === null) throw new Error('the file has no video track');
    const audio = await input.getPrimaryAudioTrack();
    return {
      duration: await input.computeDuration(),
      width: await video.getDisplayWidth(),
      height: await video.getDisplayHeight(),
      hasAudio: audio !== null,
      decodable: await video.canDecode(),
    };
  });
}

/** `count` frames `height` pixels tall, one from the middle of each equal slot of the clip. An
 *  undecodable track yields none: the timeline still works, without pictures. */
export function filmstrip(
  source: Blob,
  count: number,
  height: number,
  signal?: AbortSignal,
): Promise<CanvasImageSource[]> {
  return reading(source, signal, async (input) => {
    const video = await input.getPrimaryVideoTrack();
    if (video === null || !(await video.canDecode())) return [];
    const slot = (await input.computeDuration()) / count;
    const stamps = Array.from({ length: count }, (_, index) => (index + 0.5) * slot);
    const sink = new CanvasSink(video, { height });
    const frames: CanvasImageSource[] = [];
    for await (const wrapped of sink.canvasesAtTimestamps(stamps)) {
      if (wrapped !== null) frames.push(wrapped.canvas);
    }
    return frames;
  });
}

export async function exportVideo(
  source: Blob,
  mimeType: string,
  edit: VideoEdit,
  onProgress: (fraction: number) => void,
  signal: AbortSignal,
): Promise<ExportResult> {
  // A call, not the property: TypeScript narrows `signal.aborted` after the first check and
  // cannot see an await flip it.
  const aborted = () => signal.aborted;
  if (aborted()) return CANCELED;
  const input = openInput(source);
  const output = new Output({
    format:
      videoContainer(mimeType) === 'webm'
        ? new WebMOutputFormat()
        : new Mp4OutputFormat({ fastStart: 'in-memory' }),
    target: new BufferTarget(),
  });
  try {
    const conversion = await Conversion.init({
      input,
      output,
      ...conversionOptions(edit),
      showWarnings: false,
    });
    // cancel() releases the output asynchronously. It runs once, and every canceled return awaits
    // that same promise, so the input is disposed only after the output has let go.
    let cancellation: Promise<void> | undefined;
    const cancel = () => (cancellation ??= conversion.cancel());
    const onAbort = () => {
      void cancel();
    };
    // An abort during init reached no listener: stop here rather than run the whole export.
    if (aborted()) {
      await cancel();
      return CANCELED;
    }
    const blocked = blockingDiscards(conversion.discardedTracks);
    if (!conversion.isValid || blocked.length > 0) return { kind: 'blocked', tracks: blocked };
    conversion.onProgress = (fraction) => {
      onProgress(fraction);
    };
    signal.addEventListener('abort', onAbort, { once: true });
    try {
      await conversion.execute();
    } catch (error) {
      if (!aborted()) throw error;
    } finally {
      signal.removeEventListener('abort', onAbort);
    }
    if (aborted()) {
      await cancel();
      return CANCELED;
    }
    const buffer = output.target.buffer;
    if (buffer === null) return { kind: 'empty' };
    return { kind: 'done', blob: new Blob([buffer], { type: output.format.mimeType }) };
  } finally {
    input.dispose();
  }
}
