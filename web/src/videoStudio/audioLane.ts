// audioLane.ts — where a sound sits in project time, and how it follows the video lane.
//
// A sound hangs off a clip like an overlay and rides the ripple with it, but it may run past that
// clip — a music bed covers many — and it is cut at the project's end, never lengthening the film.
// The rule that differs from an overlay's is the one that matters: a video edit never makes a
// sound disappear. When the content under its anchor goes, it keeps its project time on whatever
// clip covers that time now, or on the last clip (spec §Model). And a lane never plays two sounds
// at one instant: one a video edit crowds out moves to a free lane.

import {
  audioTracks,
  clipAt,
  clipStart,
  projectDuration,
  type AudioItem,
  type AudioTrack,
  type OverlayAnchor,
  type VideoItem,
  type VideoProject,
} from './project';

/** A surviving piece of a resliced clip, in source seconds from where that clip started, and the
 *  id it carries after the edit. `commands.ts` builds these; the overlay and audio lanes read them. */
export interface Placement {
  readonly id: string;
  readonly from: number;
  readonly to: number;
}

interface Span {
  readonly start: number;
  readonly end: number;
}

export function audioLength(item: AudioItem): number {
  return item.duration / Math.abs(item.speed ?? 1);
}

/** The sound's window in project time: from its anchor, never past the project's end, and empty
 *  when the anchor names no clip. */
export function audioWindow(project: VideoProject, item: AudioItem): Span {
  const clip = project.video.find((candidate) => candidate.id === item.anchor.clipId);
  const base = clipStart(project, item.anchor.clipId);
  if (clip === undefined || base === undefined) return { start: 0, end: 0 };
  const total = projectDuration(project);
  const start = Math.min(base + Math.max(0, item.anchor.offset) / Math.abs(clip.speed ?? 1), total);
  return { start, end: Math.min(start + audioLength(item), total) };
}

/** Where a sound starting at `time` hangs: the clip under it, or — past the last frame — the last
 *  clip at its start. Nothing only when there is no clip at all. */
export function anchorAt(project: VideoProject, time: number): OverlayAnchor | undefined {
  const last = project.video.at(-1);
  return last === undefined ? undefined : anchorOn(project, time, last);
}

/** `anchorAt` for a lane known to hold `last`, so the answer cannot be missing. */
function anchorOn(project: VideoProject, time: number, last: VideoItem): OverlayAnchor {
  const at = Math.max(0, time);
  const clip = clipAt(project, at);
  if (clip === undefined) return { clipId: last.id, offset: 0 };
  const start = clipStart(project, clip.id) ?? 0;
  return { clipId: clip.id, offset: (at - start) * Math.abs(clip.speed ?? 1) };
}

export function findAudioItem(project: VideoProject, itemId: string): AudioItem | undefined {
  return audioTracks(project)
    .flatMap((track) => track.items)
    .find((item) => item.id === itemId);
}

/** Whether anything else on this lane covers `span`: the rule the commands enforce and the lane
 *  picker asks, from both sides, as for overlays. */
export function audioLaneIsBusy(
  project: VideoProject,
  track: AudioTrack,
  span: Span,
  exceptId?: string,
): boolean {
  return track.items.some((other) => {
    if (other.id === exceptId) return false;
    const window = audioWindow(project, other);
    return span.start < window.end && window.start < span.end;
  });
}

/** The first lane free over `span`, or `undefined` — which is what opens a new one. */
export function freeAudioTrack(
  project: VideoProject,
  span: Span,
  exceptId?: string,
): string | undefined {
  return audioTracks(project).find((track) => !audioLaneIsBusy(project, track, span, exceptId))?.id;
}

/** A sound joins the first lane free over its window; only one with nowhere to go opens a lane
 *  of its own. Every new sound and every sound a video edit crowded out lands this way. */
export function placeOnFreeLane(project: VideoProject, item: AudioItem): VideoProject {
  const trackId = freeAudioTrack(project, audioWindow(project, item));
  const tracks = audioTracks(project);
  return {
    ...project,
    audio:
      trackId === undefined
        ? [...tracks, { id: crypto.randomUUID(), items: [item] }]
        : tracks.map((track) =>
            track.id === trackId ? { ...track, items: [...track.items, item] } : track,
          ),
  };
}

/**
 * Lanes where no two sounds cover the same instant, again. A video edit carries sounds with their
 * clips but never shortens one, so a trim, a faster clip, a junction, a reorder or a removal can
 * push a sound onto its neighbour. The sound that was on the lane first keeps its place; the one
 * that now collides moves to the first lane free over its window, or opens one.
 */
export function settleAudio(project: VideoProject): VideoProject {
  if (project.audio === undefined) return project;
  const crowded: AudioItem[] = [];
  const kept = project.audio.map((track) => {
    const items: AudioItem[] = [];
    for (const item of track.items) {
      const collides = audioLaneIsBusy(project, { ...track, items }, audioWindow(project, item));
      (collides ? crowded : items).push(item);
    }
    return { ...track, items };
  });
  if (crowded.length === 0) return project;
  return crowded.reduce(placeOnFreeLane, { ...project, audio: kept });
}

/**
 * Re-hang every sound after the video lane was resliced. A sound whose clip was not touched is
 * left alone and rides the ripple. One whose clip survives follows the slice holding its offset.
 * One whose anchor content went keeps its project time on whatever clip covers it now — except a
 * sound extracted from a clip that went whole, which goes with it. A split cuts that link: once
 * the clip is two, neither half is the clip the sound came from, and deleting the half that kept
 * the id must not take the sound from under the other. With no clip left there is nothing to hang
 * on, and a sound anchored to nothing would make the saved file unloadable, so the lanes empty. A
 * project saved before audio existed stays without the key.
 */
export function reanchorAudio(
  before: VideoProject,
  after: VideoProject,
  placements: ReadonlyMap<string, readonly Placement[]>,
): readonly AudioTrack[] | undefined {
  if (before.audio === undefined) return undefined;
  const last = after.video.at(-1);
  const pieces = (clipId: string | undefined) =>
    clipId === undefined ? undefined : placements.get(clipId)?.length;
  return before.audio.map((track) => ({
    ...track,
    items: track.items.flatMap((item): AudioItem[] => {
      if (last === undefined || pieces(item.extractedFrom) === 0) return [];
      const { extractedFrom: _cut, ...free } = item;
      const own = (pieces(item.extractedFrom) ?? 1) > 1 ? free : item;
      const placed = placements.get(own.anchor.clipId);
      if (placed === undefined) return [own];
      const place = placed.find(
        ({ from, to }) => own.anchor.offset >= from && own.anchor.offset < to,
      );
      const anchor =
        place === undefined
          ? anchorOn(after, audioWindow(before, own).start, last)
          : { clipId: place.id, offset: own.anchor.offset - place.from };
      return [{ ...own, anchor }];
    }),
  }));
}

/** The project without the sound `itemId`, or `undefined` when no lane holds it. */
export function withoutAudioItem(project: VideoProject, itemId: string): VideoProject | undefined {
  if (findAudioItem(project, itemId) === undefined) return undefined;
  return {
    ...project,
    audio: audioTracks(project).map((track) => ({
      ...track,
      items: track.items.filter((item) => item.id !== itemId),
    })),
  };
}
