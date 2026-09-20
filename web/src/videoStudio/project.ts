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
  readonly fps: number;
}

export interface VideoItem {
  readonly id: string;
  readonly sourceId: string;
  readonly duration: number;
  readonly sourceStart: number;
  readonly muted: boolean;
}

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

export function emptyProject(name: string, size: ProjectSize, fps: number): VideoProject {
  return { id: crypto.randomUUID(), name, size, fps, sources: [], video: [], overlays: [] };
}

/** Where each clip begins, in project time. The i-th start is the sum of the durations before it. */
export function clipStarts(project: VideoProject): number[] {
  const starts: number[] = [];
  let at = 0;
  for (const clip of project.video) {
    starts.push(at);
    at += clip.duration;
  }
  return starts;
}

export function projectDuration(project: VideoProject): number {
  return project.video.reduce((total, clip) => total + clip.duration, 0);
}

/** The clip covering `time`, start inclusive and end exclusive, or undefined past the end. */
export function clipAt(project: VideoProject, time: number): VideoItem | undefined {
  if (time < 0) return undefined;
  let end = 0;
  for (const clip of project.video) {
    end += clip.duration;
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
  const clipEnd = base + clip.duration;
  const start = Math.min(base + Math.max(0, anchor.offset), clipEnd);
  return { start, end: Math.min(start + Math.max(0, duration), clipEnd) };
}

export function sourceOf(project: VideoProject, sourceId: string): ProjectSource | undefined {
  return project.sources.find((source) => source.id === sourceId);
}
