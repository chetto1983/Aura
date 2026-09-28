import { describe, expect, it } from 'vitest';
import { CURVE_STEP, duckAt, gainAt, volumeKeyframes, type CurveInput } from '../volumeCurve';

// The curve, against the three rules S1 measured on VideoFlow's mixer: keyframe times are
// absolute source seconds, gains step between keyframes, and the gain is 1 before the first.

const flat: CurveInput = { sourceStart: 0, speed: 1, length: 4, volume: 1 };

function valueAt(frames: ReturnType<typeof volumeKeyframes>, time: number): number | undefined {
  return frames?.findLast((frame) => frame.time <= time + 1e-9)?.value;
}

describe('volumeKeyframes', () => {
  it('writes nothing for a layer at full volume with no fade', () => {
    expect(volumeKeyframes(flat)).toBeUndefined();
  });

  it('holds a constant volume with one keyframe at the layer own sourceStart', () => {
    expect(volumeKeyframes({ ...flat, sourceStart: 4.0001, volume: 0.5 })).toEqual([
      { time: 4.0001, value: 0.5 },
    ]);
  });

  it('samples a fade in from silence, every step, and holds once it is up', () => {
    const frames = volumeKeyframes({ ...flat, volume: 0.5, fadeIn: 2 });
    expect(frames?.[0]).toEqual({ time: 0, value: 0 });
    expect(valueAt(frames, 1)).toBeCloseTo(0.25, 3);
    expect(frames?.at(-1)?.value).toBeCloseTo(0.5, 6);
    expect(frames?.at(-1)?.time).toBeLessThanOrEqual(2 + 1e-9);
    expect((frames?.[1]?.time ?? 0) - (frames?.[0]?.time ?? 0)).toBeCloseTo(CURVE_STEP, 9);
  });

  it('speaks source seconds: a trimmed layer at double speed maps its ramp through both', () => {
    const frames = volumeKeyframes({ ...flat, sourceStart: 3, speed: 2, length: 2, fadeIn: 1 });
    // Half a timeline second into the layer is one source second past its sourceStart.
    expect(valueAt(frames, 4)).toBeCloseTo(0.5, 3);
    expect((frames?.[1]?.time ?? 0) - 3).toBeCloseTo(CURVE_STEP * 2, 9);
  });

  it('ends a fade out at silence on the layer last source instant', () => {
    const frames = volumeKeyframes({ ...flat, fadeOut: 1 });
    expect(frames?.at(-1)).toEqual({ time: 4, value: 0 });
    expect(valueAt(frames, 2.9)).toBe(1);
  });

  it('rises then falls once when the fades overlap, never dipping twice', () => {
    const values = (volumeKeyframes({ ...flat, fadeIn: 3, fadeOut: 3 }) ?? []).map(
      (frame) => frame.value,
    );
    const peak = values.indexOf(Math.max(...values));
    const rising = values
      .slice(0, peak + 1)
      .every((value, index, all) => index === 0 || value >= (all[index - 1] ?? 0));
    const falling = values
      .slice(peak)
      .every((value, index, all) => index === 0 || value <= (all[index - 1] ?? 0));
    expect(peak).toBeGreaterThan(0);
    expect(rising && falling).toBe(true);
  });
});

describe('gainAt', () => {
  it('follows the envelope linearly between its points and holds it past either end', () => {
    const input = {
      ...flat,
      envelope: [
        { time: 1, gain: 1 },
        { time: 3, gain: 0 },
      ],
    };
    expect(gainAt(input, 0)).toBe(1);
    expect(gainAt(input, 2)).toBeCloseTo(0.5, 9);
    expect(gainAt(input, 3.5)).toBe(0);
  });

  it('reads envelope points in source seconds, so a faster layer reaches them sooner', () => {
    const input = {
      ...flat,
      speed: 2,
      envelope: [
        { time: 0, gain: 1 },
        { time: 2, gain: 0 },
      ],
    };
    expect(gainAt(input, 0.5)).toBeCloseTo(0.5, 9);
  });

  it('multiplies volume, fade and envelope', () => {
    const input = { ...flat, volume: 2, fadeIn: 2, envelope: [{ time: 0, gain: 0.5 }] };
    expect(gainAt(input, 1)).toBeCloseTo(0.5, 9);
  });
});

describe('duckAt', () => {
  const duck = { windows: [[2, 4]] as const, amountDb: -12, ramp: 0.5 };
  const floor = 10 ** (-12 / 20);

  it('lowers by the amount inside a speech window, and not at all far from one', () => {
    expect(duckAt(duck, 3)).toBeCloseTo(floor, 9);
    expect(duckAt(duck, 2)).toBeCloseTo(floor, 9);
    expect(duckAt(duck, 1)).toBe(1);
    expect(duckAt(duck, 5)).toBe(1);
  });

  it('ramps down before the window and back up after it, over the softness', () => {
    expect(duckAt(duck, 1.75)).toBeCloseTo(1 - 0.5 * (1 - floor), 9);
    expect(duckAt(duck, 4.25)).toBeCloseTo(1 - 0.5 * (1 - floor), 9);
    expect(duckAt(duck, 1.5)).toBeCloseTo(1, 9);
    expect(duckAt(duck, 4.5)).toBeCloseTo(1, 9);
  });

  it('takes the deeper duck where two windows’ ramps meet', () => {
    const two = {
      windows: [
        [1, 2],
        [2.6, 3],
      ] as const,
      amountDb: -12,
      ramp: 0.5,
    };
    // 0.3 s after the first and 0.3 s before the second: each alone would be 40 % down.
    expect(duckAt(two, 2.3)).toBeCloseTo(1 - 0.4 * (1 - floor), 9);
  });

  it('does not duck without ducking, or with nothing to duck under', () => {
    expect(duckAt(undefined, 3)).toBe(1);
    expect(duckAt({ windows: [], amountDb: -24, ramp: 1 }, 3)).toBe(1);
  });

  it('multiplies into the gain the mixer plays', () => {
    const input = { sourceStart: 0, speed: 1, length: 6, volume: 0.5, duck };
    expect(gainAt(input, 3)).toBeCloseTo(0.5 * floor, 9);
    expect(gainAt(input, 0.5)).toBeCloseTo(0.5, 9);
  });
});
