import BrowserRenderer from '@videoflow/renderer-browser';
import { AudioBufferSource, BufferTarget, Output, WavOutputFormat } from 'mediabunny';
import type { VideoProject } from './project';
import { primeDecodedBuffers, toVideoJSON, type MediaUrls } from './videoflow';

/** The renderer found no sound layers; the UI translates this separately from a failed encode. */
export class NoProjectAudioError extends Error {}

/** Render the edited project's sound mix to a standalone 16-bit PCM WAVE file. */
export async function exportProjectAudio(
  project: VideoProject,
  urls: MediaUrls,
  signal?: AbortSignal,
): Promise<Blob> {
  signal?.throwIfAborted();
  const json = await toVideoJSON(project, urls);
  signal?.throwIfAborted();
  const renderer = new BrowserRenderer(json);
  let closed = false;
  const close = () => {
    if (closed) return;
    closed = true;
    renderer.destroy();
  };
  signal?.addEventListener('abort', close, { once: true });
  try {
    await primeDecodedBuffers(renderer, signal);
    signal?.throwIfAborted();
    const audio = await renderer.renderAudio();
    signal?.throwIfAborted();
    if (audio === null) throw new NoProjectAudioError();

    const target = new BufferTarget();
    const output = new Output({ format: new WavOutputFormat(), target });
    const source = new AudioBufferSource({ codec: 'pcm-s16' });
    output.addAudioTrack(source);
    let canceling: Promise<void> | undefined;
    const cancel = () => {
      canceling = output.cancel();
    };
    signal?.addEventListener('abort', cancel, { once: true });
    try {
      await output.start();
      signal?.throwIfAborted();
      await source.add(audio);
      signal?.throwIfAborted();
      await output.finalize();
      signal?.throwIfAborted();
      if (target.buffer === null) throw new Error('WAVE encoder produced no file');
      return new Blob([target.buffer], { type: 'audio/wav' });
    } finally {
      signal?.removeEventListener('abort', cancel);
      if (canceling !== undefined) await canceling;
      else if (output.state !== 'finalized' && output.state !== 'canceled') await output.cancel();
    }
  } finally {
    signal?.removeEventListener('abort', close);
    close();
  }
}
