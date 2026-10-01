import BrowserRenderer from '@videoflow/renderer-browser';
import { AudioBufferSource, BufferTarget, Output, WavOutputFormat } from 'mediabunny';
import type { VideoProject } from './project';
import { toVideoJSON, type MediaUrls } from './videoflow';
import { renderLoaded } from './videoflow_media';

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
  return await renderLoaded(renderer, project, urls, signal, async () => {
    const audio = await renderer.renderAudio();
    signal?.throwIfAborted();
    if (audio === null) throw new NoProjectAudioError();
    return await writeWave(audio, signal);
  });
}

/** The mix as a WAVE file. Closing the editor cancels the encoder, so no half file is left open. */
async function writeWave(audio: AudioBuffer, signal: AbortSignal | undefined): Promise<Blob> {
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
}
