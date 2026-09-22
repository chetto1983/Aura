// commands.ts — every edit is a command: a pure function from project to project, arguments as
// JSON. Three consumers share the shape, which is why none of them touches the DOM, mutates its
// input, or carries a sentence — the UI calls them, the history records immer patches around them,
// and in cycle 3 each one is a tool with the same arguments and the same validation.
//
// The lane is a sequence, so a trim, a split and a removal all work the same way: they replace one
// clip with the slices of it that survive, and every later clip moves because its start is what the
// clips before it leave. An overlay rides the content it sits on — it follows its slice, and it
// goes when that content goes, the way removing a clip removes the overlays anchored to it.

import {
  clipAt,
  clipTimelineDuration,
  overlayWindow,
  projectDuration,
  sourceOf,
  type OverlayAnchor,
  type OverlayItem,
  type OverlayTrack,
  type ClipEditProperties,
  type JunctionTransition,
  type ProjectSource,
  type VideoItem,
  type VideoProject,
} from './project';

/** A refusal the UI can translate: the key is an i18n key, never an English sentence. */
export class CommandRefusal extends Error {
  constructor(readonly reasonKey: string) {
    super(reasonKey);
    this.name = 'CommandRefusal';
  }
}

const REFUSAL = {
  sourceMissing: 'videoStudio.refusal.sourceMissing',
  trimPastSource: 'videoStudio.refusal.trimPastSource',
  splitOnBoundary: 'videoStudio.refusal.splitOnBoundary',
  emptyRange: 'videoStudio.refusal.emptyRange',
  overlayOverlap: 'videoStudio.refusal.overlayOverlap',
} as const;

// Handles, scrubs and agents all arrive with floats. Without a tolerance, dragging a handle to the
// very end of a source refuses itself on the last bit of a sum, and a cut placed on a boundary
// makes a clip a nanosecond long instead of refusing.
const EPSILON = 1e-6;

/** A surviving piece of a clip, in seconds measured from where that clip starts in its source. */
interface Slice {
  readonly from: number;
  readonly to: number;
}

/** Where a slice landed: the id it carries, over the span of the original clip it holds. */
interface Placement extends Slice {
  readonly id: string;
}

/**
 * A command that names a clip, a track or an item the project does not have is a caller out of
 * step with the model, not a decision the editor declines. The five refusal keys are the five
 * decisions; this is a bug in the caller, so it is loud rather than translated.
 */
function locateClip(
  project: VideoProject,
  clipId: string,
): { readonly index: number; readonly clip: VideoItem } {
  const index = project.video.findIndex((item) => item.id === clipId);
  const clip = project.video[index];
  if (clip === undefined) throw new Error(`videoStudio: no clip named ${clipId}`);
  return { index, clip };
}

function locateOverlay(
  project: VideoProject,
  itemId: string,
): { readonly track: OverlayTrack; readonly item: OverlayItem } {
  for (const track of project.overlays) {
    const item = track.items.find((candidate) => candidate.id === itemId);
    if (item !== undefined) return { track, item };
  }
  throw new Error(`videoStudio: no overlay item named ${itemId}`);
}

/** A clip whose source is not in the project cannot be measured, so it cannot be re-timed. */
function sourceForClip(project: VideoProject, clip: VideoItem): ProjectSource {
  const source = sourceOf(project, clip.sourceId);
  if (source === undefined) throw new CommandRefusal(REFUSAL.sourceMissing);
  return source;
}

/** An image has no length of its own: it lasts as long as the item asks, so nothing runs past it. */
function runsPastSource(source: ProjectSource, sourceEnd: number): boolean {
  return source.kind === 'video' && sourceEnd > source.duration + EPSILON;
}

/** Where an insertion lands: appended when no index is asked for, and never off either end. */
function insertionIndex(asked: number | undefined, length: number): number {
  return asked === undefined ? length : Math.max(0, Math.min(asked, length));
}

function withoutJunction(clip: VideoItem): VideoItem {
  const {
    junctionFromClipId: _from,
    junctionTransition: _transition,
    junctionDuration: _duration,
    ...plain
  } = clip;
  return plain;
}

function normalizeJunctions(video: readonly VideoItem[]): readonly VideoItem[] {
  return video.map((clip, index) =>
    index > 0 && clip.junctionFromClipId === video[index - 1]?.id ? clip : withoutJunction(clip),
  );
}

function withTrackItems(
  project: VideoProject,
  trackId: string,
  items: (current: readonly OverlayItem[]) => readonly OverlayItem[],
): VideoProject {
  return {
    ...project,
    overlays: project.overlays.map((lane) =>
      lane.id === trackId ? { ...lane, items: items(lane.items) } : lane,
    ),
  };
}

/**
 * Rebuild the lane. `slicesOf` answers, for each clip, which pieces of it survive — or null for a
 * clip this command does not touch, which then passes through with its overlays untouched. The
 * first slice keeps the clip's id and the rest get fresh ones, so a trim stays the same clip and a
 * split leaves the left half where it was.
 */
function resliceLane(
  project: VideoProject,
  slicesOf: (clip: VideoItem, start: number) => readonly Slice[] | null,
): VideoProject {
  const video: VideoItem[] = [];
  const placements = new Map<string, readonly Placement[]>();
  let at = 0;
  for (const clip of project.video) {
    const slices = slicesOf(clip, at);
    at += clipTimelineDuration(clip);
    if (slices === null) {
      video.push(clip);
      continue;
    }
    const placed = slices.map((slice, index) => ({
      ...slice,
      id: index === 0 ? clip.id : crypto.randomUUID(),
    }));
    placements.set(clip.id, placed);
    for (const place of placed) {
      video.push({
        ...clip,
        id: place.id,
        duration: place.to - place.from,
        sourceStart: clip.sourceStart + place.from,
      });
    }
  }
  return {
    ...project,
    video: normalizeJunctions(video),
    overlays: reanchor(project.overlays, placements),
  };
}

/** An overlay rides its content: it moves to the slice holding its offset, or goes with the rest. */
function reanchor(
  tracks: readonly OverlayTrack[],
  placements: ReadonlyMap<string, readonly Placement[]>,
): readonly OverlayTrack[] {
  return tracks.map((track) => ({
    ...track,
    items: track.items.flatMap((item) => {
      const placed = placements.get(item.anchor.clipId);
      if (placed === undefined) return [item];
      const place = placed.find(
        ({ from, to }) => item.anchor.offset >= from && item.anchor.offset < to,
      );
      if (place === undefined) return [];
      return [{ ...item, anchor: { clipId: place.id, offset: item.anchor.offset - place.from } }];
    }),
  }));
}

export interface AddClipArgs {
  readonly sourceId: string;
  readonly duration: number;
  readonly sourceStart?: number | undefined;
  readonly atIndex?: number | undefined;
}

/**
 * Append a clip, or insert it at `atIndex`. The duration is asked for rather than read off the
 * source because an image has none: it lasts as long as the clip says it does.
 */
export function addClip(project: VideoProject, args: AddClipArgs): VideoProject {
  const source = sourceOf(project, args.sourceId);
  if (source === undefined) throw new CommandRefusal(REFUSAL.sourceMissing);
  const sourceStart = args.sourceStart ?? 0;
  if (
    args.duration <= 0 ||
    sourceStart < 0 ||
    runsPastSource(source, sourceStart + args.duration)
  ) {
    throw new CommandRefusal(REFUSAL.trimPastSource);
  }
  const video = [...project.video];
  video.splice(insertionIndex(args.atIndex, video.length), 0, {
    id: crypto.randomUUID(),
    sourceId: source.id,
    duration: args.duration,
    sourceStart,
    muted: false,
  });
  return { ...project, video: normalizeJunctions(video) };
}

export interface TrimClipArgs {
  readonly clipId: string;
  readonly start: number;
  readonly end: number;
}

/**
 * Set which part of its source a clip plays. `start` and `end` are seconds into that source,
 * measured from where the clip starts in it, so a trim can hand material back as well as take it
 * away and the refusal is against the source's own length rather than against what the clip shows
 * today. Everything after the clip moves, because the lane is a sequence.
 */
export function trimClip(project: VideoProject, args: TrimClipArgs): VideoProject {
  const { clip } = locateClip(project, args.clipId);
  const source = sourceForClip(project, clip);
  if (
    args.end - args.start <= 0 ||
    clip.sourceStart + args.start < 0 ||
    runsPastSource(source, clip.sourceStart + args.end)
  ) {
    throw new CommandRefusal(REFUSAL.trimPastSource);
  }
  return resliceLane(project, (item) =>
    item.id === clip.id ? [{ from: args.start, to: args.end }] : null,
  );
}

export interface SplitAtArgs {
  readonly time: number;
}

/**
 * Cut the clip under `time` in two. The right half plays on from where the left one stops, and
 * each overlay stays with the half that holds it.
 */
export function splitAt(project: VideoProject, args: SplitAtArgs): VideoProject {
  const target = clipAt(project, args.time);
  if (target === undefined) throw new CommandRefusal(REFUSAL.splitOnBoundary);
  // The offset is measured against the start the lane walk already knows; asking for that start a
  // second time would mean handling an answer that cannot be missing.
  return resliceLane(project, (clip, start) => {
    if (clip.id !== target.id) return null;
    const offset = (args.time - start) * Math.abs(clip.speed ?? 1);
    if (offset <= EPSILON || offset >= clip.duration - EPSILON) {
      throw new CommandRefusal(REFUSAL.splitOnBoundary);
    }
    return [
      { from: 0, to: offset },
      { from: offset, to: clip.duration },
    ];
  });
}

export interface RemoveRangeArgs {
  readonly from: number;
  readonly to: number;
}

/**
 * Take a stretch of project time out and close the lane over it. A clip the range covers whole
 * goes, one it covers at an edge is shortened, one it covers in the middle becomes two — and the
 * overlays over the removed content go with it, the way they go with a removed clip.
 */
export function removeRange(project: VideoProject, args: RemoveRangeArgs): VideoProject {
  const covered = Math.min(args.to, projectDuration(project)) - Math.max(args.from, 0);
  if (covered <= 0) throw new CommandRefusal(REFUSAL.emptyRange);
  return resliceLane(project, (clip, start) => {
    const speed = Math.abs(clip.speed ?? 1);
    const end = start + clipTimelineDuration(clip);
    const head = Math.max(args.from, start);
    const tail = Math.min(args.to, end);
    if (tail - head <= 0) return null;
    const slices: Slice[] = [];
    if (head > start) slices.push({ from: 0, to: (head - start) * speed });
    if (tail < end) slices.push({ from: (tail - start) * speed, to: clip.duration });
    return slices;
  });
}

export interface MoveClipArgs {
  readonly clipId: string;
  readonly toIndex: number;
}

/**
 * Put a clip at another index. The lane is a sequence, so a drop is an insertion and never an
 * overlap; overlays name their clip, so they travel with it untouched.
 */
export function moveClip(project: VideoProject, args: MoveClipArgs): VideoProject {
  const { index, clip } = locateClip(project, args.clipId);
  const video = [...project.video];
  video.splice(index, 1);
  video.splice(insertionIndex(args.toIndex, video.length), 0, clip);
  return { ...project, video: normalizeJunctions(video) };
}

export interface SetMutedArgs {
  readonly clipId: string;
  readonly muted: boolean;
}

export function setMuted(project: VideoProject, args: SetMutedArgs): VideoProject {
  const { clip } = locateClip(project, args.clipId);
  return {
    ...project,
    video: project.video.map((item) =>
      item.id === clip.id ? { ...item, muted: args.muted } : item,
    ),
  };
}

export interface SetClipPresentationArgs extends ClipEditProperties {
  readonly clipId: string;
}

export function setClipPresentation(
  project: VideoProject,
  args: SetClipPresentationArgs,
): VideoProject {
  locateClip(project, args.clipId);
  if (args.volume !== undefined && (args.volume < 0 || args.volume > 2)) {
    throw new Error('videoStudio: clip volume must be between 0 and 2');
  }
  if (args.opacity !== undefined && (args.opacity < 0 || args.opacity > 1)) {
    throw new Error('videoStudio: clip opacity must be between 0 and 1');
  }
  if (args.speed !== undefined && (args.speed < 0.25 || args.speed > 4)) {
    throw new Error('videoStudio: clip speed must be between 0.25 and 4');
  }
  for (const duration of [args.transitionInDuration, args.transitionOutDuration]) {
    if (duration !== undefined && duration <= 0) {
      throw new Error('videoStudio: transition duration must be positive');
    }
  }
  for (const amount of [args.brightness, args.contrast, args.saturation, args.blur]) {
    if (amount !== undefined && amount < 0) {
      throw new Error('videoStudio: visual adjustments cannot be negative');
    }
  }
  const { clipId, ...asked } = args;
  const changes = asked as Partial<VideoItem>;
  return {
    ...project,
    video: project.video.map((item) => (item.id === clipId ? { ...item, ...changes } : item)),
  };
}

export interface SetJunctionTransitionArgs {
  readonly fromClipId: string;
  readonly toClipId: string;
  readonly transition: JunctionTransition;
  readonly duration?: number;
}

export function setJunctionTransition(
  project: VideoProject,
  args: SetJunctionTransitionArgs,
): VideoProject {
  const toIndex = project.video.findIndex((clip) => clip.id === args.toClipId);
  const incoming = project.video[toIndex];
  const outgoing = project.video[toIndex - 1];
  if (incoming === undefined || outgoing?.id !== args.fromClipId) {
    throw new Error('videoStudio: transition clips must be adjacent');
  }
  if (args.transition === 'none') {
    return {
      ...project,
      video: project.video.map((clip) => (clip.id === incoming.id ? withoutJunction(clip) : clip)),
    };
  }
  const asked = args.duration ?? incoming.junctionDuration ?? 1;
  if (asked <= 0) throw new Error('videoStudio: junction duration must be positive');
  const duration = Math.min(
    asked,
    clipTimelineDuration(outgoing) / 2,
    clipTimelineDuration(incoming) / 2,
  );
  return {
    ...project,
    video: project.video.map((clip) =>
      clip.id === incoming.id
        ? {
            ...clip,
            junctionFromClipId: outgoing.id,
            junctionTransition: args.transition,
            junctionDuration: duration,
          }
        : clip,
    ),
  };
}

export function setFrameSize(
  project: VideoProject,
  size: { readonly width: number; readonly height: number },
): VideoProject {
  if (size.width <= 0 || size.height <= 0) {
    throw new Error('videoStudio: frame sides must be positive');
  }
  return { ...project, size: { width: Math.round(size.width), height: Math.round(size.height) } };
}

export interface AddOverlayArgs {
  readonly kind: OverlayItem['kind'];
  readonly anchor: OverlayAnchor;
  readonly duration: number;
  readonly props: Readonly<Record<string, unknown>>;
  readonly trackId?: string | undefined;
}

/** Whether anything already on this lane covers `span`. One rule, read from both sides: the
 *  command enforces it, and `freeOverlayTrack` asks it before choosing where to put an overlay. */
function laneIsBusy(
  project: VideoProject,
  track: OverlayTrack,
  span: { readonly start: number; readonly end: number },
): boolean {
  return track.items.some((other) => {
    const window = overlayWindow(project, other.anchor, other.duration);
    return span.start < window.end && window.start < span.end;
  });
}

/**
 * The lane an overlay can join, or `undefined` when every lane is busy over its window — which is
 * what a caller passes as `trackId` to `addOverlay`, and `undefined` is exactly the value that
 * opens a new one.
 *
 * This is the spec's "a third title needs a third lane, which the editor adds ON DEMAND". Adding a
 * lane per overlay instead would be a lane per title, and two titles that never meet would sit on
 * two lanes for no reason an operator could name.
 */
export function freeOverlayTrack(
  project: VideoProject,
  anchor: OverlayAnchor,
  duration: number,
): string | undefined {
  // `overlayWindow` clamps the anchor exactly as `addOverlay` does before storing it, so the lane
  // is judged against the window the overlay will really occupy.
  const span = overlayWindow(project, anchor, duration);
  return project.overlays.find((lane) => !laneIsBusy(project, lane, span))?.id;
}

/**
 * Hang a text or an image on a clip. Without a `trackId` it opens a lane of its own, which is how
 * a third title gets a third lane; with one it has to fit, because two overlays over the same
 * instant of one lane would be a silent pick between them.
 */
export function addOverlay(project: VideoProject, args: AddOverlayArgs): VideoProject {
  const { clip } = locateClip(project, args.anchor.clipId);
  const anchor: OverlayAnchor = {
    clipId: clip.id,
    offset: Math.max(0, Math.min(args.anchor.offset, clip.duration)),
  };
  const span = overlayWindow(project, anchor, args.duration);
  if (span.end - span.start <= 0) throw new CommandRefusal(REFUSAL.emptyRange);
  const item: OverlayItem = {
    id: crypto.randomUUID(),
    kind: args.kind,
    anchor,
    duration: args.duration,
    props: args.props,
  };
  if (args.trackId === undefined) {
    return {
      ...project,
      overlays: [...project.overlays, { id: crypto.randomUUID(), items: [item] }],
    };
  }
  const track = project.overlays.find((lane) => lane.id === args.trackId);
  if (track === undefined) throw new Error(`videoStudio: no overlay track named ${args.trackId}`);
  if (laneIsBusy(project, track, span)) throw new CommandRefusal(REFUSAL.overlayOverlap);
  return withTrackItems(project, track.id, (items) => [...items, item]);
}

export interface SetPropertyArgs {
  readonly itemId: string;
  readonly key: string;
  readonly value: unknown;
}

/** Change one of an overlay's properties. A clip has none: its edits are commands of their own. */
export function setProperty(project: VideoProject, args: SetPropertyArgs): VideoProject {
  const { track, item } = locateOverlay(project, args.itemId);
  const updated: OverlayItem = { ...item, props: { ...item.props, [args.key]: args.value } };
  return withTrackItems(project, track.id, (items) =>
    items.map((candidate) => (candidate.id === item.id ? updated : candidate)),
  );
}

export interface RemoveItemArgs {
  readonly itemId: string;
}

/**
 * Remove a clip or an overlay. A clip takes the overlays anchored to it with it — they have
 * nothing left to hang on — which is what the shell warns about before asking for this.
 */
export function removeItem(project: VideoProject, args: RemoveItemArgs): VideoProject {
  const clip = project.video.find((item) => item.id === args.itemId);
  if (clip !== undefined) {
    return {
      ...project,
      video: normalizeJunctions(project.video.filter((item) => item.id !== clip.id)),
      overlays: project.overlays.map((lane) => ({
        ...lane,
        items: lane.items.filter((item) => item.anchor.clipId !== clip.id),
      })),
    };
  }
  const { track, item } = locateOverlay(project, args.itemId);
  return withTrackItems(project, track.id, (items) =>
    items.filter((candidate) => candidate.id !== item.id),
  );
}
