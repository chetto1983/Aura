// commands_audio.ts — the audio lanes' commands, on the contract commands.ts keeps: a pure function
// from project to project, arguments as JSON, a refusal the UI can translate, and a caller out of
// step with the model is loud. Sub-project 2 exposes each one to Aura as a tool with these
// arguments, so none of them touches the DOM or carries a sentence.

import { anchorAt, audioLaneIsBusy, audioLength, audioWindow, freeAudioTrack } from './audioLane';
import { CommandRefusal, EPSILON } from './commands';
import {
  audioTracks,
  projectDuration,
  sourceOf,
  type AudioDucking,
  type AudioItem,
  type AudioTrack,
  type EnvelopePoint,
  type OverlayAnchor,
  type VideoProject,
} from './project';

export const AUDIO_REFUSAL = {
  noClip: 'videoStudio.audio.refusal.noClip',
  pastEnd: 'videoStudio.audio.refusal.pastEnd',
  beforeStart: 'videoStudio.audio.refusal.beforeStart',
  overlap: 'videoStudio.audio.refusal.overlap',
  notSound: 'videoStudio.audio.refusal.notSound',
  fadesTooLong: 'videoStudio.audio.refusal.fadesTooLong',
  sourceMissing: 'videoStudio.refusal.sourceMissing',
  trimPastSource: 'videoStudio.refusal.trimPastSource',
  splitOnBoundary: 'videoStudio.refusal.splitOnBoundary',
} as const;

const MAX_FADE = 5;

interface Located {
  readonly track: AudioTrack;
  readonly item: AudioItem;
}

function locateAudio(project: VideoProject, itemId: string): Located {
  for (const track of audioTracks(project)) {
    const item = track.items.find((candidate) => candidate.id === itemId);
    if (item !== undefined) return { track, item };
  }
  throw new Error(`videoStudio: no audio item named ${itemId}`);
}

/** Where a sound starting at `time` hangs, or the refusal for a lane with no clip to hang on. */
function anchorFor(project: VideoProject, time: number): OverlayAnchor {
  const anchor = anchorAt(project, time);
  if (anchor === undefined) throw new CommandRefusal(AUDIO_REFUSAL.noClip);
  return anchor;
}

/** Set or drop one optional field: under exactOptionalPropertyTypes an absent value is a missing
 *  key, never `undefined`. */
function withField<K extends keyof AudioItem>(
  item: AudioItem,
  key: K,
  value: AudioItem[K] | undefined,
): AudioItem {
  const { [key]: _dropped, ...rest } = item;
  return (value === undefined ? rest : { ...rest, [key]: value }) as AudioItem;
}

/** Replace `itemId` with `items` (one, or two after a split) where it stood, or move it to the
 *  end of `toTrack`. */
function placeItems(
  project: VideoProject,
  itemId: string,
  items: readonly AudioItem[],
  toTrack?: string,
): VideoProject {
  return {
    ...project,
    audio: audioTracks(project).map((track) => {
      const kept = track.items.filter((candidate) => candidate.id !== itemId);
      if (toTrack !== undefined) {
        return { ...track, items: track.id === toTrack ? [...kept, ...items] : kept };
      }
      const index = track.items.findIndex((candidate) => candidate.id === itemId);
      if (index === -1) return track;
      return { ...track, items: [...kept.slice(0, index), ...items, ...kept.slice(index)] };
    }),
  };
}

/** A new sound joins the first lane free over its window; only one with nowhere to go opens a
 *  lane of its own. Shared by every command that creates a sound. */
function placeOnFreeLane(project: VideoProject, item: AudioItem): VideoProject {
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

function refuseOverlap(project: VideoProject, track: AudioTrack, item: AudioItem): void {
  if (audioLaneIsBusy(project, track, audioWindow(project, item), item.id)) {
    throw new CommandRefusal(AUDIO_REFUSAL.overlap);
  }
}

/** Fades that fit the sound: each at most five seconds, and together no longer than it. */
function fittedFades(item: AudioItem): AudioItem {
  const length = audioLength(item);
  const fadeIn = Math.min(item.fadeIn ?? 0, length, MAX_FADE);
  const fadeOut = Math.min(item.fadeOut ?? 0, length - fadeIn, MAX_FADE);
  return withField(
    withField(item, 'fadeIn', fadeIn > 0 ? fadeIn : undefined),
    'fadeOut',
    fadeOut > 0 ? fadeOut : undefined,
  );
}

/** The envelope over a stretch of the sound's source, re-based to that stretch's start. */
function envelopeWithin(
  points: readonly EnvelopePoint[] | undefined,
  from: number,
  to: number,
): readonly EnvelopePoint[] | undefined {
  const kept = (points ?? [])
    .filter((point) => point.time >= from - EPSILON && point.time <= to + EPSILON)
    .map((point) => ({ ...point, time: Math.max(0, point.time - from) }));
  return kept.length === 0 ? undefined : kept;
}

export interface AddAudioArgs {
  readonly sourceId: string;
  /** Project seconds where the sound starts. */
  readonly time: number;
  readonly label?: string | undefined;
}

/**
 * Put a sound on the first lane free over its window, hung on the clip under `time`. It plays its
 * whole source and the project's end cuts it. A video source lends its sound only when it has one:
 * a clip with no audio track has nothing to put on a lane.
 */
export function addAudio(project: VideoProject, args: AddAudioArgs): VideoProject {
  const source = sourceOf(project, args.sourceId);
  if (
    source === undefined ||
    source.kind === 'image' ||
    source.duration <= 0 ||
    (source.kind === 'video' && source.hasAudio === false)
  ) {
    throw new CommandRefusal(AUDIO_REFUSAL.notSound);
  }
  const anchor = anchorFor(project, args.time);
  if (args.time >= projectDuration(project) - EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.pastEnd);
  }
  return placeOnFreeLane(project, {
    id: crypto.randomUUID(),
    sourceId: source.id,
    anchor,
    sourceStart: 0,
    duration: source.duration,
    volume: 1,
    muted: false,
    ...(args.label === undefined ? {} : { label: args.label }),
  });
}

export interface MoveAudioArgs {
  readonly itemId: string;
  /** Project seconds where the sound should now start. */
  readonly start: number;
  readonly trackId?: string | undefined;
}

/** A drag on the audio lane: the sound re-hangs on the clip under its new start. */
export function moveAudio(project: VideoProject, args: MoveAudioArgs): VideoProject {
  const { track, item } = locateAudio(project, args.itemId);
  if (args.start >= projectDuration(project) - EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.pastEnd);
  }
  const moved = { ...item, anchor: anchorFor(project, args.start) };
  const destination =
    args.trackId === undefined
      ? track
      : audioTracks(project).find((lane) => lane.id === args.trackId);
  if (destination === undefined) {
    throw new Error(`videoStudio: no audio track named ${String(args.trackId)}`);
  }
  refuseOverlap(project, destination, moved);
  return placeItems(
    project,
    item.id,
    [moved],
    destination.id === track.id ? undefined : destination.id,
  );
}

export interface TrimAudioArgs {
  readonly itemId: string;
  /** Source seconds from the sound's current sourceStart, as `trimClip` measures a clip. */
  readonly start: number;
  readonly end: number;
}

/**
 * Set which part of its source a sound plays. Trimming the head moves the sound's start by the
 * same material, so what is left stays under the picture it was under; handing material back
 * moves it earlier, and never before the film starts.
 */
export function trimAudio(project: VideoProject, args: TrimAudioArgs): VideoProject {
  const { track, item } = locateAudio(project, args.itemId);
  const source = sourceOf(project, item.sourceId);
  if (source === undefined) throw new CommandRefusal(AUDIO_REFUSAL.sourceMissing);
  const sourceStart = item.sourceStart + args.start;
  if (
    args.end - args.start <= EPSILON ||
    sourceStart < -EPSILON ||
    item.sourceStart + args.end > source.duration + EPSILON
  ) {
    throw new CommandRefusal(AUDIO_REFUSAL.trimPastSource);
  }
  const start = audioWindow(project, item).start + args.start / Math.abs(item.speed ?? 1);
  if (start < -EPSILON) throw new CommandRefusal(AUDIO_REFUSAL.beforeStart);
  if (start >= projectDuration(project) - EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.pastEnd);
  }
  const trimmed = fittedFades(
    withField(
      {
        ...item,
        anchor: anchorFor(project, start),
        sourceStart: Math.max(0, sourceStart),
        duration: args.end - args.start,
      },
      'envelope',
      envelopeWithin(item.envelope, args.start, args.end),
    ),
  );
  refuseOverlap(project, track, trimmed);
  return placeItems(project, item.id, [trimmed]);
}

export interface SplitAudioArgs {
  readonly itemId: string;
  /** Project seconds, strictly inside the sound. */
  readonly time: number;
}

/** Cut a sound in two at `time`. The left half keeps the fade in, the right the fade out, and each
 *  envelope point goes to the half it falls in. */
export function splitAudio(project: VideoProject, args: SplitAudioArgs): VideoProject {
  const { item } = locateAudio(project, args.itemId);
  const window = audioWindow(project, item);
  if (args.time <= window.start + EPSILON || args.time >= window.end - EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.splitOnBoundary);
  }
  const cut = (args.time - window.start) * Math.abs(item.speed ?? 1);
  const left = withField(
    withField({ ...item, duration: cut }, 'fadeOut', undefined),
    'envelope',
    envelopeWithin(item.envelope, 0, cut),
  );
  const right = withField(
    withField(
      {
        ...item,
        id: crypto.randomUUID(),
        anchor: anchorFor(project, args.time),
        sourceStart: item.sourceStart + cut,
        duration: item.duration - cut,
      },
      'fadeIn',
      undefined,
    ),
    'envelope',
    envelopeWithin(item.envelope, cut, item.duration),
  );
  return placeItems(project, item.id, [fittedFades(left), fittedFades(right)]);
}

export interface SetAudioPropertiesArgs {
  readonly itemId: string;
  readonly volume?: number;
  readonly muted?: boolean;
  readonly fadeIn?: number;
  readonly fadeOut?: number;
  readonly speed?: number;
  readonly denoise?: boolean;
  /** `null` turns ducking off. */
  readonly ducking?: AudioDucking | null;
}

/** Values no control can produce: a caller out of step with the model, not a decision. */
function checkRanges(args: SetAudioPropertiesArgs): void {
  if (args.volume !== undefined && (args.volume < 0 || args.volume > 2)) {
    throw new Error('videoStudio: audio volume must be between 0 and 2');
  }
  if (args.speed !== undefined && (args.speed < 0.25 || args.speed > 4)) {
    throw new Error('videoStudio: audio speed must be between 0.25 and 4');
  }
  for (const fade of [args.fadeIn, args.fadeOut]) {
    if (fade !== undefined && (fade < 0 || fade > MAX_FADE)) {
      throw new Error('videoStudio: a fade must be between 0 and 5 seconds');
    }
  }
  const ducking = args.ducking;
  if (
    ducking !== undefined &&
    ducking !== null &&
    (ducking.amountDb < -24 || ducking.amountDb > -3 || ducking.ramp < 0.1 || ducking.ramp > 2)
  ) {
    throw new Error('videoStudio: ducking must lower by 3–24 dB over 0.1–2 seconds');
  }
}

/** The inspector's Audio and Speed tabs. Fades that together outlast the sound are a choice the
 *  operator can see and change, so they are refused rather than clamped. */
export function setAudioProperties(
  project: VideoProject,
  args: SetAudioPropertiesArgs,
): VideoProject {
  const { track, item } = locateAudio(project, args.itemId);
  checkRanges(args);
  const { itemId: _itemId, ducking, ...scalars } = args;
  const next =
    ducking === undefined
      ? { ...item, ...scalars }
      : withField({ ...item, ...scalars }, 'ducking', ducking ?? undefined);
  if ((next.fadeIn ?? 0) + (next.fadeOut ?? 0) > audioLength(next) + EPSILON) {
    throw new CommandRefusal(AUDIO_REFUSAL.fadesTooLong);
  }
  refuseOverlap(project, track, next);
  return placeItems(project, item.id, [next]);
}

export interface SetEnvelopeArgs {
  readonly itemId: string;
  readonly points: readonly EnvelopePoint[];
}

/** Replace the envelope: sorted, inside the sound, gains 0–1 — the envelope only attenuates, and
 *  boosting is the volume's job. An empty list removes it. */
export function setEnvelope(project: VideoProject, args: SetEnvelopeArgs): VideoProject {
  const { item } = locateAudio(project, args.itemId);
  const points = args.points
    .filter((point) => Number.isFinite(point.time) && Number.isFinite(point.gain))
    .map((point) => ({
      time: Math.min(Math.max(point.time, 0), item.duration),
      gain: Math.min(Math.max(point.gain, 0), 1),
    }))
    .sort((a, b) => a.time - b.time);
  return placeItems(project, item.id, [
    withField(item, 'envelope', points.length === 0 ? undefined : points),
  ]);
}

export interface RecordAnalysisArgs {
  readonly sourceId: string;
  readonly speech?: readonly (readonly [number, number])[];
  readonly denoisedAssetId?: string;
}

/** Overlapping windows merged, clamped to the source, and only the ones with length kept. */
function cleanSpeech(
  windows: readonly (readonly [number, number])[],
  duration: number,
): readonly (readonly [number, number])[] {
  const sorted = windows
    .filter(([from, to]) => Number.isFinite(from) && Number.isFinite(to))
    .map(([from, to]): [number, number] => [Math.max(0, from), Math.min(duration, to)])
    .filter(([from, to]) => to - from > EPSILON)
    .sort((a, b) => a[0] - b[0]);
  const merged: [number, number][] = [];
  for (const window of sorted) {
    const last = merged.at(-1);
    if (last !== undefined && window[0] <= last[1]) last[1] = Math.max(last[1], window[1]);
    else merged.push(window);
  }
  return merged;
}

/** The only door by which browser-side analysis enters the project (spec §Commands). */
export function recordAnalysis(project: VideoProject, args: RecordAnalysisArgs): VideoProject {
  const source = sourceOf(project, args.sourceId);
  if (source === undefined) throw new CommandRefusal(AUDIO_REFUSAL.sourceMissing);
  const recorded = {
    ...source,
    ...(args.speech === undefined ? {} : { speech: cleanSpeech(args.speech, source.duration) }),
    ...(args.denoisedAssetId === undefined ? {} : { denoisedAssetId: args.denoisedAssetId }),
  };
  return {
    ...project,
    sources: project.sources.map((candidate) =>
      candidate.id === source.id ? recorded : candidate,
    ),
  };
}
