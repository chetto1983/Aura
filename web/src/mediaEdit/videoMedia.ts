import { ALL_FORMATS, BlobSource, Input } from 'mediabunny';

export interface VideoInfo {
  readonly duration: number;
  readonly width: number;
  readonly height: number;
  readonly hasAudio: boolean;
  readonly decodable: boolean;
}

export interface AudioInfo {
  readonly duration: number;
  readonly decodable: boolean;
}

function abortError(): DOMException {
  return new DOMException('The read was aborted.', 'AbortError');
}

/** Open the bytes, run `read` over them, and dispose of them whatever happens — at once when
 *  `signal` aborts, rather than after a read that may never return. */
function readInput<T>(
  source: Blob,
  read: (input: Input) => Promise<T>,
  signal?: AbortSignal,
): Promise<T> {
  if (signal?.aborted) return Promise.reject(abortError());
  const input = new Input({ source: new BlobSource(source), formats: ALL_FORMATS });
  let stop: () => void = () => undefined;
  const stopped = new Promise<never>((_resolve, reject) => {
    stop = () => {
      input.dispose();
      reject(abortError());
    };
  });
  signal?.addEventListener('abort', stop, { once: true });
  return Promise.race([read(input), stopped]).finally(() => {
    signal?.removeEventListener('abort', stop);
    input.dispose();
  });
}

export function probeVideo(source: Blob, signal?: AbortSignal): Promise<VideoInfo> {
  return readInput(
    source,
    async (input) => {
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
    },
    signal,
  );
}

/** A sound's length, and whether this browser can decode it: the audio lane's door. */
export function probeAudio(source: Blob, signal?: AbortSignal): Promise<AudioInfo> {
  return readInput(
    source,
    async (input) => {
      const audio = await input.getPrimaryAudioTrack();
      if (audio === null) throw new Error('the file has no audio track');
      return { duration: await input.computeDuration(), decodable: await audio.canDecode() };
    },
    signal,
  );
}
