// videoflow.ts — the only file that composes VideoFlow. It turns the editor's project into a
// VideoJSON, exports it in the browser, and owns the four behaviours the spikes measured:
// fonts from our own origin, the cut nudge, mute where the mixer reads it, and one decode per
// source instead of one per layer.
//
// Everything here is the renderer's vocabulary; nothing of it leaks into the model. `project.ts`
// knows about lanes and clips, this file knows about layers and settings, and the translation
// happens once, here.

import VideoFlow from '@videoflow/core';
import type { VideoJSON } from '@videoflow/core';
import BrowserRenderer from '@videoflow/renderer-browser';
import type { AssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import {
  clipStarts,
  overlayWindow,
  projectDuration,
  sourceOf,
  type OverlayItem,
  type VideoItem,
  type VideoProject,
} from './project';

/** Where a clip's bytes come from: the cockpit's own asset route, never a foreign URL. */
export type MediaUrls = Pick<AssetSource, 'assetUrl'>;

/**
 * VideoFlow resolves every family through `renderer.loadFont(name)` — its own init calls it for the
 * hard-coded default 'Noto Sans', and every text layer calls it for its `fontFamily`. The stock
 * implementation fetches fonts.googleapis.com: 26 requests, measured in spike 107. There is no
 * config option, so the override below is the way, and these stylesheets are what it points at.
 */
export const LOCAL_FONTS: Readonly<Record<string, string>> = {
  'Noto Sans': '/fonts/noto-sans-alias.css',
  'Atkinson Hyperlegible Next': '/fonts/atkinson.css',
};

/**
 * The tenth of a millisecond that stops the renderer sampling a frame exactly on its own source
 * boundary, where it serves the PREVIOUS frame instead. Spike 108 §7 rendered one composition in
 * three configurations: without it 49 of 240 frames repeat at 30 fps over a 30 fps source, 26 of
 * 192 at 24 over 24, and 6 of 192 at 24 over 30; with it, 0 in all three.
 *
 * It goes on EVERY video clip, not only the trimmed ones, and it is not sized to a frame rate —
 * both are the spike's explicit rider. The layer that starts mid-timeline with `sourceStart` 0,
 * which is exactly what a cut produces, is the worst case measured (27 of 60 frames, 45 %), so
 * nudging only the clips with `sourceStart > 0` would leave the commonest gesture broken.
 */
const CUT_NUDGE = 1e-4;

/** The rate BrowserRenderer mixes at (`renderAudio`, `sampleRate: 48000`) — primed buffers match. */
const MIX_SAMPLE_RATE = 48000;

/** The part of a renderer the font override touches. Both renderers expose it; neither declares it
 *  in a shared interface, so this is the seam rather than an import of either class. */
export interface FontLoadingRenderer {
  loadFont(fontName: string): Promise<void>;
  loadedFonts: Record<string, string>;
}

/**
 * Point a renderer's font loading at our own origin. Replacing the one public method keeps the
 * whole pipeline — `document.fonts` plus the FontEmbedder's SVG inlining, which reads
 * `loadedFonts` — and a family we do not serve is left to the fallback stack rather than fetched.
 */
export function useLocalFonts<R extends FontLoadingRenderer>(renderer: R): R {
  renderer.loadFont = async (name: string): Promise<void> => {
    if (name in renderer.loadedFonts) return;
    const href = LOCAL_FONTS[name];
    if (href === undefined) return;
    renderer.loadedFonts[name] = href;
    if (!document.querySelector(`link[data-local-font="${name}"]`)) {
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = href;
      link.dataset.localFont = name;
      const loaded = new Promise<void>((resolve, reject) => {
        link.onload = () => {
          resolve();
        };
        link.onerror = () => {
          reject(new Error(`videoStudio: ${href} did not load`));
        };
      });
      document.head.appendChild(link);
      await loaded;
    }
    await document.fonts.load(`1em "${name}"`);
  };
  return renderer;
}

function addClip(
  flow: VideoFlow,
  project: VideoProject,
  urls: MediaUrls,
  clip: VideoItem,
  startTime: number,
): void {
  const source = sourceOf(project, clip.sourceId);
  if (source === undefined) {
    throw new Error(
      `videoStudio: clip ${clip.id} points at a source the project lost: ${clip.sourceId}`,
    );
  }
  const settings = {
    name: clip.id,
    source: urls.assetUrl(source.assetId),
    startTime,
    sourceDuration: clip.duration,
  };
  // A still has one frame and no audio: no source window to sample, so no nudge and no mute.
  if (source.kind === 'image') {
    flow.addImage({ fit: 'cover' }, settings);
    return;
  }
  flow.addVideo(
    // `muted` in a layer's SETTINGS is a no-op — the mixer reads `mute` in its PROPERTIES
    // (spike 108 §6: a clip carrying `settings.muted` played at full volume). Muting does not
    // save the decode either, which is one more reason the cache below belongs to us.
    { fit: 'cover', mute: clip.muted },
    { ...settings, sourceStart: clip.sourceStart + CUT_NUDGE },
  );
}

function addOverlay(
  flow: VideoFlow,
  project: VideoProject,
  urls: MediaUrls,
  item: OverlayItem,
): void {
  const span = overlayWindow(project, item.anchor, item.duration);
  const duration = span.end - span.start;
  // An overlay whose window collapsed — dragged past the end of the clip it hangs on — is a state
  // the model allows and has nothing to show, so it contributes no layer.
  if (duration <= 0) return;
  const settings = { name: item.id, startTime: span.start, sourceDuration: duration };
  if (item.kind === 'image') {
    const { assetId, ...props } = item.props;
    if (typeof assetId !== 'string') {
      throw new Error(`videoStudio: image overlay ${item.id} names no asset in its props`);
    }
    flow.addImage(props, { ...settings, source: urls.assetUrl(assetId) });
    return;
  }
  flow.addText(item.props, settings);
}

/**
 * The project as VideoFlow sees it: the video lane in order, then every overlay resolved against
 * the clip it hangs on rather than against the timeline — which is what makes an overlay ripple
 * with its clip instead of staying where it was authored.
 */
export async function toVideoJSON(project: VideoProject, urls: MediaUrls): Promise<VideoJSON> {
  const flow = new VideoFlow({
    name: project.name,
    width: project.size.width,
    height: project.size.height,
    fps: project.fps,
    backgroundColor: '#000000',
  });
  const starts = clipStarts(project);
  project.video.forEach((clip, index) => {
    addClip(flow, project, urls, clip, starts[index] ?? 0);
  });
  for (const track of project.overlays) {
    for (const item of track.items) addOverlay(flow, project, urls, item);
  }
  // No layer was added with `waitFor`, so the flow pointer is still at zero: this one wait is what
  // gives the compiled JSON the lane's own length rather than a float sum of layer ends.
  flow.wait(projectDuration(project));
  return flow.compile();
}

/** What the decode cache touches on a live renderer. `initLayers` and the layers' `decodedBuffer`
 *  are not in BrowserRenderer's public type — see `primeDecodedBuffers` for why we reach for them
 *  anyway, and spike 108 §8 for the measurement that says it is safe. */
interface PrimableLayer {
  readonly json: { readonly settings: { readonly source?: string } };
  readonly hasAudio: boolean;
  decodedBuffer: AudioBuffer | null;
}

interface PrimableRenderer {
  readonly layers: readonly PrimableLayer[];
  initLayers(): Promise<void>;
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
 */
async function primeDecodedBuffers(renderer: BrowserRenderer): Promise<void> {
  const primable = renderer as unknown as PrimableRenderer;
  await primable.initLayers();
  const audioCtx = new OfflineAudioContext(2, MIX_SAMPLE_RATE, MIX_SAMPLE_RATE);
  const bySource = new Map<string, Promise<AudioBuffer | null>>();
  for (const layer of primable.layers) {
    const source = layer.json.settings.source;
    if (source === undefined || !layer.hasAudio) continue;
    let decoding = bySource.get(source);
    if (decoding === undefined) {
      // `decodeAudioData` detaches its input, so the promise rather than the bytes is shared.
      // A source that will not decode — a clip with no audio track, which VideoFlow hands to the
      // decoder anyway because `RuntimeVideoLayer.hasAudio` is hard-coded true — resolves null and
      // is left to the mixer's own path, which catches the same failure. Failing the export here
      // would be stricter than VideoFlow is with itself.
      decoding = fetch(source)
        .then((response) => response.arrayBuffer())
        .then((bytes) => audioCtx.decodeAudioData(bytes))
        .catch(() => null);
      bySource.set(source, decoding);
    }
    const buffer = await decoding;
    if (buffer) layer.decodedBuffer = buffer;
  }
}

export interface ExportOptions {
  readonly onProgress?: (progress: number) => void;
  /** Aborting destroys the renderer. The previous cycle shipped an editor that kept transcoding
   *  after its dialog closed; this one cannot. */
  readonly signal?: AbortSignal;
}

/**
 * Render the project to an MP4 in the browser. The caller downloads the blob — nothing here
 * writes to the Studio library, which is cycle 2's door and does not exist yet.
 */
export async function exportProject(
  project: VideoProject,
  urls: MediaUrls,
  options: ExportOptions = {},
): Promise<Blob> {
  options.signal?.throwIfAborted();
  const json = await toVideoJSON(project, urls);
  // Not a React hook: `useLocalFonts` is VideoFlow's own name for the font seam (spike 107), and
  // this module is not part of any component tree.
  // eslint-disable-next-line react-hooks/rules-of-hooks
  const renderer = useLocalFonts(new BrowserRenderer(json));
  let closed = false;
  const close = (): void => {
    if (closed) return;
    closed = true;
    renderer.destroy();
  };
  options.signal?.addEventListener('abort', close, { once: true });
  try {
    await primeDecodedBuffers(renderer);
    return await renderer.exportVideo({
      worker: true,
      ...(options.onProgress ? { onProgress: options.onProgress } : {}),
      ...(options.signal ? { signal: options.signal } : {}),
    });
  } finally {
    options.signal?.removeEventListener('abort', close);
    close();
  }
}
