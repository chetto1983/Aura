import type { Span } from 'dnd-timeline';
import type { KeyboardEvent } from 'react';
import type { TrimClipArgs } from './commands';
import { sourceOf, type VideoItem, type VideoProject } from './project';

// timelineView.ts — the arithmetic between a gesture and the model: how much of the project a
// zoom level shows, where a mark or the playhead sits inside it, and what a drop or a dragged
// handle means in the numbers a command takes.
//
// It is all pure, which is the point: the two questions a timeline gets wrong — where a dropped
// clip lands in a sequence, and which zero a trim is measured from — are answered here, by a
// function a test can ask directly instead of through a widget.

/** The coarse-pointer floor from the previous cycle: items are this tall, handles this wide. */
export const TOUCH_FLOOR = 44;
/** Zoom stops here. Below a second on screen the ruler says nothing a frame number would not. */
export const MIN_VISIBLE = 1;
export const SIDEBAR_WIDTH = 120;
const MAX_MARKS = 10;
/** Steps a viewer reads without arithmetic: halves, seconds, the clock's own divisions. */
const STEPS = [0.5, 1, 2, 5, 10, 15, 30, 60, 120, 300] as const;
/** Past an hour on screen the marks stay ten minutes apart: nothing coarser still reads as time. */
const COARSEST_STEP = 600;
/**
 * How long a still keeps being offered more. This is the handle's announced range, not a rule:
 * `runsPastSource` exempts an image, so no command refuses a longer one, and a clip that already
 * runs past this keeps its own end as the bound rather than reporting a maximum below its value.
 */
const IMAGE_LIMIT = 3600;

export type TrimSpan = Omit<TrimClipArgs, 'clipId'>;

export function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

/** Slider values are read aloud: a millisecond is as fine as a spoken number gets. */
export function atMilli(value: number): number {
  return Math.round(value * 1000) / 1000;
}

/** The step whose marks fit the ruler at this zoom, coarsening until they do. */
export function rulerStep(visible: number): number {
  return STEPS.find((step) => visible / step <= MAX_MARKS) ?? COARSEST_STEP;
}

/** Every multiple of `step` inside the range. Counted from the first, so the last does not drift. */
export function rulerMarks(range: Span, step: number): number[] {
  const first = Math.ceil(range.start / step - 1e-9) * step;
  const marks: number[] = [];
  for (let index = 0; first + index * step <= range.end + 1e-9; index += 1) {
    marks.push(atMilli(first + index * step));
  }
  return marks;
}

/**
 * Widen or narrow what is on screen around its middle. `factor` 1 only re-clamps, which is how a
 * range held from before a project grew or shrank is brought back inside it.
 */
export function zoomedRange(range: Span, factor: number, total: number): Span {
  const fits = Math.max(total, MIN_VISIBLE);
  const span = clamp((range.end - range.start) * factor, MIN_VISIBLE, fits);
  const middle = (range.start + range.end) / 2;
  const start = clamp(middle - span / 2, 0, Math.max(0, fits - span));
  return { start, end: start + span };
}

/** Where a value sits inside the visible range, as the percentage a style can use. */
export function offsetOf(value: number, range: Span): string {
  const span = range.end - range.start;
  const ratio = span <= 0 ? 0 : (value - range.start) / span;
  return `${String(atMilli(clamp(ratio, 0, 1) * 100))}%`;
}

/**
 * Where a clip released at `time` lands in the sequence. The lane is measured WITHOUT the dragged
 * clip, because that is the lane the drop joins; a clip counts as passed once the drop is at or
 * beyond its middle, so nudging a clip a few pixels never reorders anything.
 */
export function insertIndexFor(project: VideoProject, clipId: string, time: number): number {
  let at = 0;
  let index = 0;
  for (const clip of project.video) {
    if (clip.id === clipId) continue;
    if (at + clip.duration / 2 <= time) index += 1;
    at += clip.duration;
  }
  return index;
}

/**
 * A span in project time, read as the trim it is. `trimClip` measures from where the clip starts
 * in its source, and a clip's start in project time IS that zero — so the conversion subtracts the
 * clip's start and nothing else. Adding `sourceStart` here would count it twice, once in the
 * argument and once in `resliceLane`.
 */
export function trimArgsFromSpan(start: number, span: Span): TrimSpan {
  return { start: span.start - start, end: span.end - start };
}

/**
 * How far out a clip's end handle may go. A video stops at its source; an image is measured
 * against nothing, so it stops at the hour the handle offers. Either way the bound is never below
 * the end the clip already has: a slider whose maximum sits under its own value says nothing true.
 */
export function sourceEndOf(project: VideoProject, clip: VideoItem): number {
  const source = sourceOf(project, clip.sourceId);
  const end = clip.sourceStart + clip.duration;
  return Math.max(source?.kind === 'video' ? source.duration : IMAGE_LIMIT, end);
}

interface StepKeys {
  readonly frame: number;
  readonly onStep: (delta: number) => void;
}

/** Arrows move a frame, shift-arrows a second — the cockpit's idiom, from mediaEdit/VideoTimeline. */
export function stepOnArrow({ frame, onStep }: StepKeys) {
  return (event: KeyboardEvent<HTMLElement>) => {
    const back = event.key === 'ArrowLeft' || event.key === 'ArrowDown';
    const forward = event.key === 'ArrowRight' || event.key === 'ArrowUp';
    if (!back && !forward) return;
    event.preventDefault();
    onStep((back ? -1 : 1) * (event.shiftKey ? 1 : frame));
  };
}
