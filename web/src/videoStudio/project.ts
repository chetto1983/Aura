// project.ts — the editor's whole state as data: sources by asset id, a video lane that is a
// sequence, and overlay lanes whose items hang off a clip rather than off the timeline.
//
// A clip has no start of its own. Its start is what the clips before it leave, which is what
// makes ripple the default instead of a feature: trim one and every later clip moves with it,
// and so do the overlays anchored to them.

export interface ProjectSize {
  readonly width: number;
  readonly height: number;
}

export interface ProjectSource {
  readonly id: string;
  readonly assetId: string;
  readonly kind: 'video' | 'image';
  readonly duration: number; // seconds; 0 for an image, which takes the duration its item asks for
  readonly size: ProjectSize;
  readonly hasAudio?: boolean;
}

export type ClipTransition =
  | 'none'
  | 'fade'
  | 'blurResolve'
  | 'zoom'
  | 'slideUp'
  | 'slideDown'
  | 'slideLeft'
  | 'slideRight'
  | 'overshootPop'
  | 'glitchResolve'
  | 'wipeReveal'
  | 'lightSweepReveal';

export interface VideoItem {
  readonly id: string;
  readonly sourceId: string;
  readonly duration: number;
  readonly sourceStart: number;
  readonly muted: boolean;
  readonly volume?: number;
  readonly rotation?: 0 | 90 | 180 | 270;
  readonly fit?: 'contain' | 'cover';
  readonly flipX?: boolean;
  readonly flipY?: boolean;
  readonly brightness?: number;
  readonly contrast?: number;
  readonly saturation?: number;
  readonly hue?: number;
  readonly blur?: number;
  readonly opacity?: number;
  readonly animation?: 'none' | 'fadeIn' | 'fadeOut';
  readonly fadeIn?: boolean;
  readonly fadeOut?: boolean;
  readonly speed?: number;
  readonly transitionIn?: ClipTransition;
  readonly transitionOut?: ClipTransition;
  readonly transitionInDuration?: number;
  readonly transitionOutDuration?: number;
}

export type ClipEditProperties = Omit<
  VideoItem,
  'id' | 'sourceId' | 'duration' | 'sourceStart' | 'muted'
>;

export interface OverlayAnchor {
  readonly clipId: string;
  readonly offset: number; // seconds after the clip starts
}

export interface OverlayItem {
  readonly id: string;
  readonly kind: 'text' | 'image';
  readonly anchor: OverlayAnchor;
  readonly duration: number;
  readonly props: Readonly<Record<string, unknown>>;
}

export interface OverlayTrack {
  readonly id: string;
  readonly items: readonly OverlayItem[];
}

export interface VideoProject {
  readonly id: string;
  readonly name: string;
  readonly size: ProjectSize;
  readonly fps: number;
  readonly sources: readonly ProjectSource[];
  readonly video: readonly VideoItem[];
  readonly overlays: readonly OverlayTrack[];
}

export function clipTimelineDuration(clip: VideoItem): number {
  return clip.duration / Math.abs(clip.speed ?? 1);
}

export function emptyProject(name: string, size: ProjectSize, fps: number): VideoProject {
  return { id: crypto.randomUUID(), name, size, fps, sources: [], video: [], overlays: [] };
}

/** Where each clip begins, in project time. The i-th start is the sum of the durations before it. */
export function clipStarts(project: VideoProject): number[] {
  const starts: number[] = [];
  let at = 0;
  for (const clip of project.video) {
    starts.push(at);
    at += clipTimelineDuration(clip);
  }
  return starts;
}

export function projectDuration(project: VideoProject): number {
  return project.video.reduce((total, clip) => total + clipTimelineDuration(clip), 0);
}

/** The clip covering `time`, start inclusive and end exclusive, or undefined past the end. */
export function clipAt(project: VideoProject, time: number): VideoItem | undefined {
  if (time < 0) return undefined;
  let end = 0;
  for (const clip of project.video) {
    end += clipTimelineDuration(clip);
    if (time < end) return clip;
  }
  return undefined;
}

export function clipStart(project: VideoProject, clipId: string): number | undefined {
  const index = project.video.findIndex((clip) => clip.id === clipId);
  return index === -1 ? undefined : clipStarts(project)[index];
}

/**
 * An overlay's window in project time. It never outlives the clip it hangs on, and it never comes
 * back inverted: an offset past the clip's end, or a negative duration, collapses it to an empty
 * window at the boundary. Overlap detection reads this, and `end < start` would silently read as
 * "no overlap".
 */
export function overlayWindow(
  project: VideoProject,
  anchor: OverlayAnchor,
  duration: number,
): { readonly start: number; readonly end: number } {
  const clip = project.video.find((item) => item.id === anchor.clipId);
  const base = clipStart(project, anchor.clipId);
  if (clip === undefined || base === undefined) return { start: 0, end: 0 };
  const speed = Math.abs(clip.speed ?? 1);
  const clipEnd = base + clipTimelineDuration(clip);
  const start = Math.min(base + Math.max(0, anchor.offset) / speed, clipEnd);
  return { start, end: Math.min(start + Math.max(0, duration), clipEnd) };
}

export function sourceOf(project: VideoProject, sourceId: string): ProjectSource | undefined {
  return project.sources.find((source) => source.id === sourceId);
}
