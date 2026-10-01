// videoflow_media.ts — the bytes an export plays: each source fetched once, and a source the
// renderer could not read stopping the export by name instead of becoming black or silence.
//
// VideoFlow's layers and its mixer all take their bytes from `loadedMedia`, one refcounted cache
// per page keyed by URL (core dist/MediaCache.js; renderer-browser dist/layers/RuntimeVideoLayer.js:63,
// RuntimeAudioLayer.js:31, RuntimeImageLayer.js:16, audio/mixer.js:69). Its `initLayers` catches
// a layer that fails to load, logs a warning and disables the layer (BrowserRenderer.js:427-445),
// and the export still resolves: measured in spikes/video-mcp-render/FINDINGS.md S1.4, a source
// behind a refused fetch came out as a black, silent 26,604 B MP4. The same run fetched every
// source twice, because the pre-decode fetched on its own instead of reading that cache.

import { loadedMedia } from '@videoflow/core';
import type BrowserRenderer from '@videoflow/renderer-browser';
import type { AssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import type { VideoProject } from './project';

/** Why a source stopped the export: its bytes never arrived, or they arrived and did not play. */
export type SourceFailure = 'unreachable' | 'undecodable';

/** A source the export could not play. The asset id names it; the export wrote nothing. A page
 *  that hands the failure on to another process catches it here and passes `{ assetId, failure }`
 *  as plain data: a class instance does not survive a structured clone. */
export class ExportSourceError extends Error {
  readonly assetId: string;
  readonly failure: SourceFailure;

  constructor(assetId: string, failure: SourceFailure) {
    super(`videoStudio: source ${assetId} is ${failure}`);
    this.name = 'ExportSourceError';
    this.assetId = assetId;
    this.failure = failure;
  }
}

/** The rate BrowserRenderer mixes at (`renderAudio`, `sampleRate: 48000`) — primed buffers match. */
const MIX_SAMPLE_RATE = 48000;

/** What the export touches on a live renderer. `initLayers` and the layers are not in
 *  BrowserRenderer's public type — see `primeDecodedBuffers` for why we reach for them anyway, and
 *  spike 108 §8 for the measurement that says it is safe. */
interface PrimableLayer {
  readonly json: {
    readonly type: string;
    readonly settings: { readonly source?: string; readonly enabled?: boolean };
    readonly properties?: { readonly mute?: unknown };
  };
  readonly hasAudio: boolean;
  decodedBuffer: AudioBuffer | null;
}

interface PrimableRenderer {
  readonly layers: readonly PrimableLayer[];
  initLayers(): Promise<void>;
}

/** Every asset the project plays, by the URL its layers fetch it from. */
function assetsByUrl(
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
): ReadonlyMap<string, string> {
  const ids = [
    ...project.sources.flatMap((source) =>
      source.denoisedAssetId === undefined
        ? [source.assetId]
        : [source.assetId, source.denoisedAssetId],
    ),
    ...project.overlays
      .flatMap((lane) => lane.items)
      .flatMap((item) => (typeof item.props.assetId === 'string' ? [item.props.assetId] : [])),
  ];
  return new Map(ids.map((id) => [urls.assetUrl(id), id]));
}

/** A source's bytes out of VideoFlow's cache, decoded. Its layers already hold them, so this is
 *  never a fetch; `decodeAudioData` detaches its input, and `arrayBuffer()` is a fresh copy. */
async function decodeHeld(
  source: string,
  audioCtx: OfflineAudioContext,
): Promise<AudioBuffer | null> {
  const entry = await loadedMedia.acquire(source);
  try {
    return await audioCtx.decodeAudioData(await entry.blob.arrayBuffer());
  } catch {
    return null;
  } finally {
    loadedMedia.release(source);
  }
}

/**
 * One decoded buffer per SOURCE, because VideoFlow decodes once per LAYER.
 *
 * Measured, spike 108: 3 layers over 2 sources cost 3 decodes; 16 layers over 1 source cost 16,
 * and every decode reads the WHOLE file whatever `sourceDuration` says — 8 layers of a 60 s clip
 * hold 175.8 MB of PCM alive and spend 1.85 s, 32.7 % of the render, decoding. A cut is the
 * commonest gesture in this editor, so the pathological case is the normal case.
 *
 * The seam is VideoFlow's own and needs no fork: `decodeLayerAudio` reads `layer.decodedBuffer`
 * off ANY layer although only `RuntimeAudioLayer` ever writes it, and `initLayers()` is idempotent
 * (`elementsSetup`), so the export re-entering it costs nothing. `scheduleBufferOnContext` still
 * applies `sourceStart` / `sourceDuration` / `speed` / `mute` per layer. Measured: 16 decodes → 1,
 * 8 → 1, decode time 1 847 → 234 ms on the 60 s case, audio identical sample for sample over
 * 2 880 512 samples. The render-time saving is the noisy figure (−22 % and −40 % in two runs); the
 * decode collapse is the exact one.
 *
 * A video that will not decode — a clip with no audio track, which VideoFlow hands to the decoder
 * anyway because `RuntimeVideoLayer.hasAudio` is hard-coded true — is left to the mixer's own
 * path, which catches the same failure. A SOUND that will not decode has nothing else to play:
 * it would export as silence, so it stops the export — unless the operator muted it, since the
 * mixer drops a muted layer whatever its bytes (`scheduleBufferOnContext` reads `mute` first,
 * audio/mixer.js:245-247).
 */
async function primeDecodedBuffers(
  layers: readonly PrimableLayer[],
  nameOf: (source: string) => string,
  signal: AbortSignal | undefined,
): Promise<void> {
  const audioCtx = new OfflineAudioContext(2, MIX_SAMPLE_RATE, MIX_SAMPLE_RATE);
  const bySource = new Map<string, Promise<AudioBuffer | null>>();
  for (const layer of layers) {
    const source = layer.json.settings.source;
    if (source === undefined || !layer.hasAudio) continue;
    let decoding = bySource.get(source);
    if (decoding === undefined) {
      // The promise rather than the buffer is shared, so two layers of one source wait on one
      // decode instead of racing to start two.
      decoding = decodeHeld(source, audioCtx);
      bySource.set(source, decoding);
    }
    const buffer = await decoding;
    // `decodeAudioData` has no cancellation of its own: a decode already running finishes, and
    // nothing after it is ever scheduled.
    signal?.throwIfAborted();
    if (buffer !== null) layer.decodedBuffer = buffer;
    else if (layer.json.type === 'audio' && layer.json.properties?.mute !== true)
      throw new ExportSourceError(nameOf(source), 'undecodable');
  }
}

/**
 * Load every layer's media through VideoFlow — one fetch per source, into its cache — refuse the
 * export if any layer could not load, and pre-decode the audio from the bytes already held.
 *
 * A layer VideoFlow disabled is told apart by the cache: `acquire` forgets a URL whose fetch
 * failed ("a failed fetch does not poison the URL forever"), so a source still in the cache is one
 * whose bytes arrived and could not be read. VideoFlow keeps the HTTP status in its console warning
 * only; the page never learns it, and neither does this error.
 */
async function loadSources(
  renderer: BrowserRenderer,
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
  signal?: AbortSignal,
): Promise<void> {
  const primable = renderer as unknown as PrimableRenderer;
  await primable.initLayers();
  signal?.throwIfAborted();
  const assets = assetsByUrl(project, urls);
  const nameOf = (source: string): string => assets.get(source) ?? source;
  const lost = primable.layers
    .map((layer) => layer.json.settings)
    .find((settings) => settings.enabled === false && settings.source !== undefined)?.source;
  if (lost !== undefined) {
    throw new ExportSourceError(
      nameOf(lost),
      loadedMedia.has(lost) ? 'undecodable' : 'unreachable',
    );
  }
  await primeDecodedBuffers(primable.layers, nameOf, signal);
}

/**
 * Load the renderer's media, run `render` on it, and destroy it exactly once, however the export
 * ends. The abort reaches `render` through a listener that exists only while it runs. While the
 * media loads there is none, on purpose: VideoFlow's fetches take no signal, so a renderer
 * destroyed under them would see its layers acquire their bytes afterwards and keep them pinned in
 * `loadedMedia`, the page-wide cache the Stage reads too. `loadSources` checks the signal after
 * every await instead, and the `finally` releases what the layers acquired once the loads settle.
 */
export async function renderLoaded<T>(
  renderer: BrowserRenderer,
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
  signal: AbortSignal | undefined,
  render: () => Promise<T>,
): Promise<T> {
  let closed = false;
  const close = (): void => {
    if (closed) return;
    closed = true;
    renderer.destroy();
  };
  try {
    await loadSources(renderer, project, urls, signal);
    signal?.addEventListener('abort', close, { once: true });
    return await render();
  } finally {
    signal?.removeEventListener('abort', close);
    close();
  }
}
