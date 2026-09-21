import type { ConversionOptions, DiscardedTrack } from 'mediabunny';
import type { CropRect, Rotation } from './cropMath';

// editRules.ts — what can be edited, what the result is called, and what Mediabunny is asked
// to do. Pure, so every rule is tested without a browser codec.

export type EditKind = 'image' | 'video';
export type ImageExtension = 'png' | 'jpeg' | 'webp';
export type VideoContainer = 'mp4' | 'webm';

// Filerobot reads png/jpeg/webp (its SUPPORTED_IMAGE_TYPES); the asset allowlist stores MP4 and
// WebM video (internal/assets/limits.go). SVG, GIF and QuickTime are not offered: an SVG does not
// rasterise safely, a GIF would lose its frames, and the server refuses .mov.
const EDITABLE: Readonly<Record<string, EditKind>> = {
  'image/png': 'image',
  'image/jpeg': 'image',
  'image/webp': 'image',
  'video/mp4': 'video',
  'video/webm': 'video',
};

const EDITABLE_EXTENSIONS: Readonly<Record<string, EditKind>> = {
  png: 'image',
  jpg: 'image',
  jpeg: 'image',
  webp: 'image',
  mp4: 'video',
  webm: 'video',
};

/** The media type without parameters, lower-cased ("video/webm; codecs=…" → "video/webm"). */
function essence(mimeType: string): string {
  return (mimeType.split(';')[0] ?? '').trim().toLowerCase();
}

export function editableKind(mimeType: string): EditKind | undefined {
  return EDITABLE[essence(mimeType)];
}

export function editableKindForFileName(fileName: string): EditKind | undefined {
  const dot = fileName.lastIndexOf('.');
  return dot < 0 ? undefined : EDITABLE_EXTENSIONS[fileName.slice(dot + 1).toLowerCase()];
}

export function imageExtension(mimeType: string): ImageExtension {
  const type = essence(mimeType);
  if (type === 'image/jpeg') return 'jpeg';
  if (type === 'image/webp') return 'webp';
  return 'png';
}

export function videoContainer(mimeType: string): VideoContainer {
  return essence(mimeType) === 'video/webm' ? 'webm' : 'mp4';
}

export function editedBase(fileName: string, suffix: string): string {
  const dot = fileName.lastIndexOf('.');
  const base = dot > 0 ? fileName.slice(0, dot) : fileName;
  return `${base}-${suffix}`;
}

export function editedName(fileName: string, suffix: string, extension: string): string {
  return `${editedBase(fileName, suffix)}.${extension}`;
}

export interface VideoEdit {
  readonly start: number;
  readonly end: number;
  readonly rotation: Rotation;
  readonly crop?: CropRect;
  readonly mute: boolean;
}

/** The shortest cut in seconds: one step of the tenth-of-a-second timeline and time fields. */
export const MIN_SPAN = 0.1;

/** The length the trim works on: a duration that is not a positive number leaves nothing. */
export function trimLength(duration: number): number {
  return Number.isFinite(duration) && duration > 0 ? duration : 0;
}

export interface TrimRange {
  readonly start: number;
  readonly end: number;
}

/** The range once the operator types `value` into Start or End: inside the clip, and at least
 *  the shortest cut from the other bound — MIN_SPAN, or the whole clip when it is shorter, the
 *  rule VideoTimeline's handles follow. */
export function typedRange(
  range: TrimRange,
  edge: 'start' | 'end',
  value: number,
  duration: number,
): TrimRange {
  const length = trimLength(duration);
  const span = Math.min(MIN_SPAN, length);
  if (edge === 'start') {
    return { start: Math.min(Math.max(0, value), Math.max(0, range.end - span)), end: range.end };
  }
  const endMin = Math.min(range.start + span, length);
  return { start: range.start, end: Math.max(Math.min(length, value), endMin) };
}

export type EditConversion = Pick<ConversionOptions, 'trim' | 'copy' | 'video' | 'audio'>;

/** Always the `expand` copy path: Mediabunny copies what it can (a trim, a mute) and transcodes
 *  only what it must (a crop, WebM). On MP4 the copy keeps the preceding key frame behind an
 *  edit list, so playback starts exactly on the requested frame (spike 105). */
export function conversionOptions(edit: VideoEdit): EditConversion {
  const options: EditConversion = {
    trim: { start: edit.start, end: edit.end },
    copy: { boundaryPolicy: 'expand' },
    video:
      edit.crop === undefined
        ? { rotate: edit.rotation }
        : { rotate: edit.rotation, crop: edit.crop },
  };
  return edit.mute ? { ...options, audio: { discard: true } } : options;
}

export interface BlockedTrack {
  readonly type: 'video' | 'audio';
  /** `null` when the track declares no codec; the UI words that in the operator's language. */
  readonly codec: string | null;
  readonly reason: DiscardedTrack['reason'];
}

/** The tracks this browser dropped on its own. `conversion.isValid` is not enough: spike 105 saw
 *  Firefox drop an HEVC video track and still return a valid, audio-only file. */
export function blockingDiscards(discarded: readonly DiscardedTrack[]): BlockedTrack[] {
  const blocked: BlockedTrack[] = [];
  for (const { track, reason } of discarded) {
    if (reason === 'discarded_by_user') continue;
    if (track.type !== 'video' && track.type !== 'audio') continue;
    // oxlint-disable-next-line typescript/no-deprecated -- getCodec() is async; this guard runs synchronously right after Conversion.init and only names the codec for the operator.
    blocked.push({ type: track.type, codec: track.codec, reason });
  }
  return blocked;
}
