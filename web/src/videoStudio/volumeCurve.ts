// volumeCurve.ts — a layer's gain over time, as the keyframes VideoFlow's mixer can play.
//
// S1 measured the three rules this obeys (spikes/video-studio-audio/FINDINGS.md): keyframe times
// are absolute source seconds; the mixer never interpolates, it steps from one keyframe to the
// next (`setValueAtTime`); and before the first keyframe the gain is 1. So a curve starts with a
// keyframe at the layer's own sourceStart, and every ramp is sampled densely enough to sound
// continuous. Ducking multiplies in: the layer goes down under the other sounds' speech (`duckAt`).

import type { EnvelopePoint } from './project';
import { sourceSeconds } from './sourceClock';

/** Seconds between the samples of a ramp: 10 ms steps are finer than the ear hears as stairs. */
export const CURVE_STEP = 0.01;
/** Gains closer than this are one gain, and a keyframe for them is a keyframe for nothing. */
const SAME_GAIN = 1e-4;

export interface VolumeKeyframe {
  readonly time: number;
  readonly value: number;
}

export interface Ducking {
  /** Speech in the other sounds, in timeline seconds from the layer's start. */
  readonly windows: readonly (readonly [number, number])[];
  readonly amountDb: number;
  /** Seconds the gain takes to go down before a window and to come back after it. */
  readonly ramp: number;
}

export interface CurveInput {
  /** Where the layer starts in its source — the keyframes' zero, read off the layer itself. */
  readonly sourceStart: number;
  readonly speed: number;
  /** How long the layer plays, in timeline seconds. */
  readonly length: number;
  readonly volume: number;
  readonly fadeIn?: number | undefined;
  readonly fadeOut?: number | undefined;
  /** Source seconds from `sourceStart`, gains 0–1. */
  readonly envelope?: readonly EnvelopePoint[] | undefined;
  /** Where to lower the layer under other sounds' speech. */
  readonly duck?: Ducking | undefined;
}

/** The envelope's gain at `time`: linear between its points, held before the first and after the
 *  last, and 1 when there is none. */
export function envelopeAt(points: readonly EnvelopePoint[] | undefined, time: number): number {
  const first = points?.[0];
  if (points === undefined || first === undefined) return 1;
  let previous = first;
  for (const point of points) {
    if (point.time > time) {
      return point === previous
        ? point.gain
        : previous.gain +
            ((point.gain - previous.gain) * (time - previous.time)) / (point.time - previous.time);
    }
    previous = point;
  }
  return previous.gain;
}

/** The ducking gain `t` timeline seconds into the layer: the full `amountDb` inside a speech
 *  window, a linear ramp of `ramp` seconds down into it before and up out of it after, 1 elsewhere.
 *  Where two windows' ramps meet, the deeper one wins. */
export function duckAt(duck: Ducking | undefined, t: number): number {
  if (duck === undefined) return 1;
  let depth = 0;
  for (const [start, end] of duck.windows) {
    const outside = t < start ? start - t : t > end ? t - end : 0;
    depth = Math.max(depth, 1 - outside / duck.ramp);
  }
  return 1 - depth * (1 - 10 ** (duck.amountDb / 20));
}

/** The gain `t` timeline seconds into the layer. The fades are a MIN of two ramps, so fades that
 *  overlap on a short layer make one rise and one fall, never a double dip. */
export function gainAt(input: CurveInput, t: number): number {
  const fadeIn = input.fadeIn ?? 0;
  const fadeOut = input.fadeOut ?? 0;
  const fade = Math.min(
    1,
    fadeIn > 0 ? t / fadeIn : 1,
    fadeOut > 0 ? (input.length - t) / fadeOut : 1,
  );
  return (
    input.volume *
    Math.max(0, fade) *
    envelopeAt(input.envelope, t * input.speed) *
    duckAt(input.duck, t)
  );
}

/** The keyframes that play `gainAt` on VideoFlow's mixer, or `undefined` when the gain is 1
 *  throughout and the mixer's own default already says so. */
export function volumeKeyframes(input: CurveInput): readonly VolumeKeyframe[] | undefined {
  const times: number[] = [];
  for (let index = 0; index * CURVE_STEP < input.length; index += 1) {
    times.push(index * CURVE_STEP);
  }
  times.push(input.length);
  const frames: VolumeKeyframe[] = [];
  for (const t of times) {
    const value = gainAt(input, t);
    const last = frames.at(-1);
    if (last !== undefined && Math.abs(last.value - value) < SAME_GAIN) continue;
    frames.push({ time: sourceSeconds(input, t), value });
  }
  const only = frames.length === 1 ? frames[0] : undefined;
  return only !== undefined && Math.abs(only.value - 1) < SAME_GAIN ? undefined : frames;
}
