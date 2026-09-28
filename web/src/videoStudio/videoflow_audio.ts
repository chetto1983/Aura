// videoflow_audio.ts — the audio half of the compile: one AudioLayer per sound, and every audible
// layer's volume written where VideoFlow's mixer reads it.
//
// That place is the compiled JSON's `animations` and nowhere else (S1): a static `volume` in a
// layer's properties never reaches the mix. So the volumes are written AFTER `compile()`, onto the
// layers it returns, each against that layer's own `sourceStart`, `speed` and `sourceDuration` —
// the numbers the mixer maps keyframes through, cut nudge included.
//
// A clip or sound with noise reduction on plays its cleaned copy (`denoisedAssetId`) once the copy
// exists; a clip does so through a sound layer of its own under its muted picture.

import type VideoFlow from '@videoflow/core';
import type { VideoJSON } from '@videoflow/core';
import type { AssetSource } from '../chat/artifacts/renderers/assetSourceContext';
import { audioWindow } from './audioLane';
import { audioTracks, clipStarts, sourceOf, type VideoItem, type VideoProject } from './project';
import { volumeKeyframes, type CurveInput } from './volumeCurve';

type Loudness = Pick<CurveInput, 'volume' | 'fadeIn' | 'fadeOut' | 'envelope'>;

/** The name of the sound layer a cleaned clip plays through; the clip's own layer keeps its id. */
const CLEANED = '#clean';

/** The cleaned copy a clip or a sound plays instead of its source: only once noise reduction is on
 *  AND its cleaning has finished — until then it plays what it always played. */
function cleanedAsset(
  project: VideoProject,
  sourceId: string,
  denoise: boolean | undefined,
): string | undefined {
  return denoise === true ? sourceOf(project, sourceId)?.denoisedAssetId : undefined;
}

/** Whether a clip's sound comes from its cleaned copy. VideoFlow's video layer can only play its
 *  own file's audio, so a cleaned clip is its muted picture plus a sound layer of its own. */
export function playsCleaned(project: VideoProject, clip: VideoItem): boolean {
  const source = sourceOf(project, clip.sourceId);
  return (
    !clip.muted &&
    source?.kind === 'video' &&
    source.hasAudio !== false &&
    cleanedAsset(project, clip.sourceId, clip.denoise) !== undefined
  );
}

/** Every sound as a VideoFlow AudioLayer, at the window its anchor gives it. */
export function addAudioItems(
  flow: VideoFlow,
  project: VideoProject,
  urls: Pick<AssetSource, 'assetUrl'>,
): void {
  for (const item of audioTracks(project).flatMap((track) => track.items)) {
    const window = audioWindow(project, item);
    const length = window.end - window.start;
    // A sound with no window — its anchor gone, or pushed to the film's last instant — has
    // nothing to play, so it contributes no layer.
    if (length <= 0) continue;
    const source = sourceOf(project, item.sourceId);
    if (source === undefined) {
      throw new Error(
        `videoStudio: sound ${item.id} points at a source the project lost: ${item.sourceId}`,
      );
    }
    const speed = item.speed ?? 1;
    flow.addAudio(
      { mute: item.muted },
      {
        name: item.id,
        source: urls.assetUrl(cleanedAsset(project, item.sourceId, item.denoise) ?? source.assetId),
        startTime: window.start,
        sourceStart: item.sourceStart,
        sourceDuration: length * Math.abs(speed),
        speed,
      },
    );
  }
  const starts = clipStarts(project);
  project.video.forEach((clip, index) => {
    const cleaned = playsCleaned(project, clip)
      ? cleanedAsset(project, clip.sourceId, clip.denoise)
      : undefined;
    if (cleaned === undefined) return;
    // No cut nudge: it keeps the PICTURE off a frame boundary, and this layer has none. The
    // cleaned copy is already trimmed of RNNoise's delay (audioClean.ts), so it sits on the picture.
    flow.addAudio(
      { mute: false },
      {
        name: `${clip.id}${CLEANED}`,
        source: urls.assetUrl(cleaned),
        startTime: starts[index] ?? 0,
        sourceStart: clip.sourceStart,
        sourceDuration: clip.duration,
        speed: clip.speed ?? 1,
      },
    );
  });
}

/** What each audible layer should sound like, by the name the compile gave it. */
function loudnessByLayer(project: VideoProject): ReadonlyMap<string, Loudness> {
  const byName = new Map<string, Loudness>();
  for (const clip of project.video) {
    const source = sourceOf(project, clip.sourceId);
    if (!clip.muted && source?.kind === 'video' && source.hasAudio !== false) {
      byName.set(playsCleaned(project, clip) ? `${clip.id}${CLEANED}` : clip.id, {
        volume: clip.volume ?? 1,
      });
    }
  }
  for (const item of audioTracks(project).flatMap((track) => track.items)) {
    if (item.muted) continue;
    byName.set(item.id, {
      volume: item.volume,
      fadeIn: item.fadeIn,
      fadeOut: item.fadeOut,
      envelope: item.envelope,
    });
  }
  return byName;
}

/** The compiled JSON with every audible layer's volume curve in its `animations`. */
export function withVolumes(project: VideoProject, json: VideoJSON): VideoJSON {
  const loudness = loudnessByLayer(project);
  return {
    ...json,
    layers: json.layers.map((layer) => {
      const name: unknown = layer.settings.name;
      const wanted = typeof name === 'string' ? loudness.get(name) : undefined;
      if (wanted === undefined) return layer;
      const speed = Math.abs(Number(layer.settings.speed ?? 1)) || 1;
      const keyframes = volumeKeyframes({
        ...wanted,
        sourceStart: layer.settings.sourceStart ?? 0,
        speed,
        length: layer.settings.sourceDuration / speed,
      });
      if (keyframes === undefined) return layer;
      return {
        ...layer,
        animations: [
          ...layer.animations.filter((animation) => animation.property !== 'volume'),
          { property: 'volume', keyframes: [...keyframes] },
        ],
      };
    }),
  };
}
