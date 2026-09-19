import { describe, expect, it } from 'vitest';
import { CROP_PRESETS, moveRect, presetRect, rotateSize } from '../cropMath';

const HD = { width: 1280, height: 720 };

describe('rotateSize', () => {
  it('swaps the sides on a quarter turn only', () => {
    expect(rotateSize(HD, 0)).toEqual(HD);
    expect(rotateSize(HD, 180)).toEqual(HD);
    expect(rotateSize(HD, 90)).toEqual({ width: 720, height: 1280 });
    expect(rotateSize(HD, 270)).toEqual({ width: 720, height: 1280 });
  });
});

describe('presetRect', () => {
  it('fills an HD frame with the largest centred square', () => {
    expect(presetRect(HD, '1:1')).toEqual({ left: 280, top: 0, width: 720, height: 720 });
  });

  it('keeps even sides for the encoder', () => {
    expect(presetRect(HD, '9:16')).toEqual({ left: 438, top: 0, width: 404, height: 720 });
    expect(presetRect({ width: 1279, height: 719 }, 'original')).toEqual({
      left: 0,
      top: 0,
      width: 1278,
      height: 718,
    });
  });

  it('works in the rotated frame', () => {
    expect(presetRect(rotateSize(HD, 90), '9:16')).toEqual({
      left: 0,
      top: 0,
      width: 720,
      height: 1280,
    });
  });

  it('stays inside the frame for every preset and rotation', () => {
    for (const rotation of [0, 90, 180, 270] as const) {
      const frame = rotateSize({ width: 1918, height: 1080 }, rotation);
      for (const preset of CROP_PRESETS) {
        const rect = presetRect(frame, preset);
        expect(rect.left).toBeGreaterThanOrEqual(0);
        expect(rect.top).toBeGreaterThanOrEqual(0);
        expect(rect.left + rect.width).toBeLessThanOrEqual(frame.width);
        expect(rect.top + rect.height).toBeLessThanOrEqual(frame.height);
        expect(rect.width % 2).toBe(0);
        expect(rect.height % 2).toBe(0);
      }
    }
  });
});

describe('moveRect', () => {
  const square = presetRect(HD, '1:1');

  it('moves by whole pixels', () => {
    expect(moveRect(square, -100.4, 0, HD)).toEqual({ ...square, left: 180 });
  });

  it('never leaves the frame', () => {
    expect(moveRect(square, -1000, 50, HD)).toEqual({ ...square, left: 0, top: 0 });
    expect(moveRect(square, 1000, 0, HD)).toEqual({ ...square, left: 560 });
  });
});
