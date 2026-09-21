import { ALL_FORMATS, BlobSource, Input } from 'mediabunny';

export interface VideoInfo {
  readonly duration: number;
  readonly width: number;
  readonly height: number;
  readonly hasAudio: boolean;
  readonly decodable: boolean;
}

function abortError(): DOMException {
  return new DOMException('The read was aborted.', 'AbortError');
}

export function probeVideo(source: Blob, signal?: AbortSignal): Promise<VideoInfo> {
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
  const reading = (async () => {
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
  })();
  return Promise.race([reading, stopped]).finally(() => {
    signal?.removeEventListener('abort', stop);
    input.dispose();
  });
}
