import type { Page } from '@playwright/test';

// frames.ts — frames of an exported file, read at chosen instants. They are decoded in the page:
// Node has no WebCodecs, and a `<video>` fed the exported bytes is also the bluntest available
// proof that what came out is playable.

export interface Frame {
  /** Where the frame sits in the file: the one Chrome presented for the seek, within 0.1 s of the
   *  instant asked for. */
  readonly mediaTime: number;
  /** RGBA, row by row, `videoWidth × videoHeight × 4` bytes. */
  readonly rgba: Buffer;
}

/** The frames Chrome presents at `times`, in that order. */
export async function readFrames(
  page: Page,
  mp4: Buffer,
  times: readonly number[],
): Promise<readonly Frame[]> {
  const read = await page.evaluate(
    async ({ base64, times }) => {
      const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
      const url = URL.createObjectURL(new Blob([bytes], { type: 'video/mp4' }));
      const video = document.createElement('video');
      video.muted = true;
      video.preload = 'auto';
      video.src = url;
      try {
        await new Promise<void>((resolve, reject) => {
          video.onloadeddata = () => {
            resolve();
          };
          video.onerror = () => {
            reject(new Error('the exported file did not decode in the browser'));
          };
        });
        const canvas = new OffscreenCanvas(video.videoWidth, video.videoHeight);
        const context = canvas.getContext('2d', { willReadFrequently: true });
        if (context === null) throw new Error('no 2d context');
        const frames: { mediaTime: number; rgba: string }[] = [];
        for (const time of times) {
          // `seeked` can fire before the decoded frame reaches the compositor under CI load.
          // Read only after Chrome reports the requested frame as presented.
          const mediaTime = await new Promise<number>((resolve, reject) => {
            const timeout = window.setTimeout(() => {
              reject(new Error(`the exported video did not present frame at ${String(time)}s`));
            }, 10_000);
            const onFrame: VideoFrameRequestCallback = (_now, metadata) => {
              if (Math.abs(metadata.mediaTime - time) > 0.1) {
                video.requestVideoFrameCallback(onFrame);
                return;
              }
              window.clearTimeout(timeout);
              resolve(metadata.mediaTime);
            };
            video.requestVideoFrameCallback(onFrame);
            video.currentTime = time;
          });
          context.drawImage(video, 0, 0);
          const data = context.getImageData(0, 0, canvas.width, canvas.height).data;
          // A typed array does not cross `evaluate`; base64 does, built in 32 KiB slices so
          // `fromCharCode` is never handed more arguments than a call can take.
          let binary = '';
          for (let at = 0; at < data.length; at += 0x8000) {
            binary += String.fromCharCode(...data.subarray(at, at + 0x8000));
          }
          frames.push({ mediaTime, rgba: btoa(binary) });
        }
        return frames;
      } finally {
        URL.revokeObjectURL(url);
      }
    },
    { base64: mp4.toString('base64'), times: [...times] },
  );
  return read.map((frame) => ({
    mediaTime: frame.mediaTime,
    rgba: Buffer.from(frame.rgba, 'base64'),
  }));
}

/** How bright a frame is: the mean of R, G and B over every pixel, 0–255. */
export function meanLevel(frame: Frame): number {
  let sum = 0;
  for (let at = 0; at < frame.rgba.length; at += 4) {
    sum += (frame.rgba[at] ?? 0) + (frame.rgba[at + 1] ?? 0) + (frame.rgba[at + 2] ?? 0);
  }
  return sum / ((frame.rgba.length / 4) * 3);
}
