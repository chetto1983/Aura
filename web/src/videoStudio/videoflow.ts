// videoflow.ts — the only file that composes VideoFlow. It turns the editor's project into a
// VideoJSON and exports it in the browser, with the behaviours the spikes measured: fonts from our
// own origin, the cut nudge, and mute and volume where the mixer reads them. The audio half —
// sounds and every volume curve — is videoflow_audio.ts; every fade reaches the renderer through
// videoflow_keyframes.ts; the media an export reads, once per source and never silently lost, is
// videoflow_media.ts.
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
  clipTimelineDuration,
  junctionDurationAt,
  overlayWindow,
  projectDuration,
  sourceOf,
  type OverlayItem,
  type VideoItem,
  type VideoProject,
} from './project';
import { addAudioItems, playsCleaned, withVolumes } from './videoflow_audio';
import { withKeyframes } from './videoflow_keyframes';
import { renderLoaded } from './videoflow_media';
import { clipTransitions, type ClipTransitions } from './videoflow_transitions';

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

/** A clip's opacity: a value, or its fades as keyframes on the film's clock — half a second of
 *  what the viewer sees, whatever the clip's speed. `withKeyframes` carries them onto the source
 *  clock the renderer reads. */
function clipOpacity(clip: VideoItem): unknown {
  const value = clip.opacity ?? 1;
  const fadeIn = clip.fadeIn === true || clip.animation === 'fadeIn';
  const fadeOut = clip.fadeOut === true || clip.animation === 'fadeOut';
  if (!fadeIn && !fadeOut) return value;
  const length = clipTimelineDuration(clip);
  const edge = Math.min(0.5, length / 2);
  return [
    ...(fadeIn
      ? [
          { time: 0, value: 0 },
          { time: edge, value },
        ]
      : []),
    ...(fadeOut
      ? [
          { time: length - edge, value },
          { time: length, value: 0 },
        ]
      : []),
  ];
}

/** The part of a renderer the font override touches. Both renderers expose it; neither declares it
 *  in a shared interface, so this is the seam rather than an import of either class. */
export interface FontLoadingRenderer {
  loadFont(fontName: string): Promise<void>;
  loadedFonts: Record<string, string>;
}

/** The stylesheet a family is waiting on, so two layers asking for the same one share a single
 *  `<link>` and a single wait. The promise rides on the element rather than in a map of its own:
 *  when the link goes, the memory of it goes too, which is what makes a failure retryable. */
const STYLESHEET_LOADS = new WeakMap<HTMLLinkElement, Promise<void>>();

function linkStylesheet(name: string, href: string): Promise<void> {
  const existing = document.querySelector<HTMLLinkElement>(`link[data-local-font="${name}"]`);
  if (existing !== null) return STYLESHEET_LOADS.get(existing) ?? Promise.resolve();
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = href;
  link.dataset.localFont = name;
  const loading = new Promise<void>((resolve, reject) => {
    link.onload = () => {
      resolve();
    };
    link.onerror = () => {
      reject(new Error(`videoStudio: ${href} did not load`));
    };
  }).catch((failure: unknown) => {
    // A stylesheet that failed must not become permanent. Dropping the link drops the only record
    // of the attempt, so the next layer asking for this family gets a fresh one.
    link.remove();
    throw failure;
  });
  STYLESHEET_LOADS.set(link, loading);
  document.head.appendChild(link);
  return loading;
}

/**
 * Point a renderer's font loading at our own origin. Replacing the one public method keeps the
 * whole pipeline — `document.fonts` plus the FontEmbedder's SVG inlining, which reads
 * `loadedFonts` — and a family we do not serve is left to the fallback stack rather than fetched.
 *
 * Not a React hook, whatever the shape suggests: it takes a renderer and gives it back.
 */
export function withLocalFonts<R extends FontLoadingRenderer>(renderer: R): R {
  renderer.loadFont = async (name: string): Promise<void> => {
    // `hasOwn`, not `in`: a family called `constructor` or `toString` would otherwise answer for
    // the prototype's and be handed a function as its stylesheet.
    if (Object.hasOwn(renderer.loadedFonts, name)) return;
    const href = Object.hasOwn(LOCAL_FONTS, name) ? LOCAL_FONTS[name] : undefined;
    if (href === undefined) return;
    try {
      await linkStylesheet(name, href);
    } catch {
      // Explicitly silenced, and it lands where a family we do not serve lands: the fallback
      // stack. Nothing is recorded, so the next layer asking for it tries the stylesheet again
      // instead of inheriting this failure — and failing a whole export over a font would be
      // harsher than VideoFlow is with its own.
      return;
    }
    // Recorded only once the face is really there: `loadedFonts` is what the FontEmbedder inlines
    // into every rasterized frame, so an entry pointing at a stylesheet that never loaded would
    // embed nothing and burn the fallback font into the video.
    renderer.loadedFonts[name] = href;
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
  transitions: ClipTransitions,
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
    speed: clip.speed ?? 1,
    ...transitions,
  };
  // A still has one frame and no audio: no source window to sample, so no nudge and no mute.
  if (source.kind === 'image') {
    flow.addImage({ fit: 'cover' }, settings);
    return;
  }
  flow.addVideo(
    // `muted` in a layer's SETTINGS is a no-op — the mixer reads `mute` in its PROPERTIES
    // (spike 108 §6: a clip carrying `settings.muted` played at full volume). Muting does not
    // save the decode either, which is one more reason the export decodes once per source
    // (videoflow_media.ts `primeDecodedBuffers`). A static `volume` here would be ignored as
    // well: the mixer reads it only from the compiled `animations` (S1), which `withVolumes`
    // writes. A cleaned clip is muted too: its sound plays from the cleaned copy's own layer
    // (videoflow_audio.ts).
    {
      fit: clip.fit ?? 'cover',
      mute: clip.muted || playsCleaned(project, clip),
      rotation: clip.rotation ?? 0,
      scale: [clip.flipX === true ? -1 : 1, clip.flipY === true ? -1 : 1],
      filterBrightness: clip.brightness ?? 1,
      filterContrast: clip.contrast ?? 1,
      filterSaturate: clip.saturation ?? 1,
      filterHueRotate: clip.hue ?? 0,
      filterBlur: clip.blur ?? 0,
      // Fades are keyframes, which the builder cannot take as a value: `withKeyframes` moves them
      // where the renderer reads them, and the cast is over the builder's scalar type.
      opacity: clipOpacity(clip) as number,
    },
    { ...settings, sourceStart: clip.sourceStart + CUT_NUDGE },
  );
}

function addJunctionWash(
  flow: VideoFlow,
  project: VideoProject,
  toIndex: number,
  startTime: number,
): void {
  const incoming = project.video[toIndex];
  const duration = junctionDurationAt(project, toIndex);
  if (
    incoming === undefined ||
    duration <= 0 ||
    (incoming.junctionTransition !== 'fadeBlack' && incoming.junctionTransition !== 'fadeWhite')
  ) {
    return;
  }
  const shortSide = Math.min(project.size.width, project.size.height);
  flow.addShape(
    {
      width: `${String((project.size.width / shortSide) * 100)}em`,
      height: `${String((project.size.height / shortSide) * 100)}em`,
      fill: incoming.junctionTransition === 'fadeWhite' ? '#ffffff' : '#000000',
      opacity: [
        { time: 0, value: 0 },
        { time: duration / 2, value: 1 },
        { time: duration, value: 0 },
      ] as unknown as number,
    },
    {
      name: `junction-${incoming.id}`,
      startTime,
      sourceDuration: duration,
      shapeType: 'rectangle',
    },
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
    addClip(flow, project, urls, clip, starts[index] ?? 0, clipTransitions(project, index));
  });
  for (let index = 1; index < project.video.length; index += 1) {
    addJunctionWash(flow, project, index, starts[index] ?? 0);
  }
  for (const track of project.overlays) {
    for (const item of track.items) addOverlay(flow, project, urls, item);
  }
  addAudioItems(flow, project, urls);
  // No layer was added with `waitFor`, so the flow pointer is still at zero: this one wait is what
  // gives the compiled JSON the lane's own length rather than a float sum of layer ends — and what
  // keeps a sound running past the last frame from lengthening the film.
  flow.wait(projectDuration(project));
  return withVolumes(project, withKeyframes(await flow.compile()));
}

export interface ExportOptions {
  readonly onProgress?: (progress: number) => void;
  /** Aborting stops the export and destroys the renderer: at once while it encodes, and as soon as
   *  its media has settled while that loads (videoflow_media.ts `renderLoaded`). The previous
   *  cycle shipped an editor that kept transcoding after its dialog closed; this one cannot. */
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
  const { signal } = options;
  signal?.throwIfAborted();
  const json = await toVideoJSON(project, urls);
  // Rechecked after every await, because the abort listener `renderLoaded` adds cannot cover what
  // happens before it exists: an abort landing while the project compiles would otherwise
  // construct a renderer that nothing ever destroys.
  signal?.throwIfAborted();
  const renderer = withLocalFonts(new BrowserRenderer(json));
  return await renderLoaded(renderer, project, urls, signal, () =>
    renderer.exportVideo({
      worker: true,
      ...(options.onProgress ? { onProgress: options.onProgress } : {}),
      ...(signal ? { signal } : {}),
    }),
  );
}
