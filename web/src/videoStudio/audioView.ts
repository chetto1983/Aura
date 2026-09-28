// audioView.ts — what a sound's waveform shows: the peaks of the stretch of its source the lane
// displays, and its envelope in the Envelope plugin's terms and back. Pure, so the lane and its
// tests share one definition.

import type { EnvelopePoint } from './project';
import { envelopeAt } from './volumeCurve';

/** A point as wavesurfer's Envelope plugin holds it: seconds into the waveform, gain 0–1. */
interface ViewPoint {
  readonly time: number;
  readonly volume: number;
}

/** Closer than this, two times or two gains are the same one. */
const SAME = 1e-6;

/** The loudest sample in each of `count` equal buckets of `samples` between `from` and `to`
 *  seconds; a bucket past the end of the samples is silent. */
export function peaksOf(
  samples: Float32Array,
  rate: number,
  from: number,
  to: number,
  count: number,
): Float32Array {
  const peaks = new Float32Array(Math.max(0, count));
  const first = from * rate;
  const size = ((to - from) * rate) / Math.max(1, peaks.length);
  for (let bucket = 0; bucket < peaks.length; bucket += 1) {
    const end = Math.min(samples.length, Math.ceil(first + (bucket + 1) * size));
    let peak = 0;
    for (let index = Math.max(0, Math.floor(first + bucket * size)); index < end; index += 1) {
      peak = Math.max(peak, Math.abs(samples[index] ?? 0));
    }
    peaks[bucket] = peak;
  }
  return peaks;
}

/**
 * The envelope as the plugin draws it over `visible` source seconds. Both edges are always there:
 * without them the plugin's line starts and ends at volume 0 and draws a fade-in and a fade-out the
 * mix does not do (S2). Points past the stretch — a sound the film's end cuts short — stay hidden.
 */
export function envelopeView(
  envelope: readonly EnvelopePoint[] | undefined,
  visible: number,
): ViewPoint[] {
  const inside = (envelope ?? [])
    .filter((point) => point.time > SAME && point.time < visible - SAME)
    .map((point) => ({ time: point.time, volume: point.gain }));
  return [
    { time: 0, volume: envelopeAt(envelope, 0) },
    ...inside,
    { time: visible, volume: envelopeAt(envelope, visible) },
  ];
}

/** The item's envelope after the plugin moved its points: the shown stretch replaced, the points
 *  past it kept, and an envelope back at full gain everywhere dropped altogether. */
export function envelopeFromView(
  points: readonly ViewPoint[],
  envelope: readonly EnvelopePoint[] | undefined,
  visible: number,
): EnvelopePoint[] {
  const hidden = (envelope ?? []).filter((point) => point.time > visible + SAME);
  const next = [...points.map((point) => ({ time: point.time, gain: point.volume })), ...hidden];
  next.sort((a, b) => a.time - b.time);
  return next.every((point) => Math.abs(point.gain - 1) < SAME) ? [] : next;
}

/** Whether the plugin already shows these points, whatever their order and whatever ids it added:
 *  its own edit echoing back must not reset a drag under the pointer. */
export function sameView(shown: readonly ViewPoint[], wanted: readonly ViewPoint[]): boolean {
  const byTime = (points: readonly ViewPoint[]) => [...points].sort((a, b) => a.time - b.time);
  const a = byTime(shown);
  const b = byTime(wanted);
  return (
    a.length === b.length &&
    a.every((point, index) => {
      const other = b[index];
      return (
        other !== undefined &&
        Math.abs(point.time - other.time) < SAME &&
        Math.abs(point.volume - other.volume) < SAME
      );
    })
  );
}
