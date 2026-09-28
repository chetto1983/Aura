import { describe, expect, it } from 'vitest';
import { speechFromFrames } from '../speechWindows';

// S4's smoothing of the VAD's 30 ms verdicts: a pause under 300 ms joins its neighbours, a window
// under 250 ms is dropped.

const FRAME = 0.03;

function frames(pattern: string): boolean[] {
  return Array.from(pattern, (mark) => mark === '#');
}

describe('speechFromFrames', () => {
  it('joins consecutive voiced frames into one window, in seconds', () => {
    const found = speechFromFrames(frames('..##########..'), FRAME);
    expect(found).toHaveLength(1);
    expect(found[0]?.[0]).toBeCloseTo(0.06, 9);
    expect(found[0]?.[1]).toBeCloseTo(0.36, 9);
  });

  it('bridges a pause shorter than 300 ms, and keeps one of 300 ms apart', () => {
    // Nine silent frames are 270 ms; ten are 300 ms.
    expect(
      speechFromFrames(frames(`${'#'.repeat(10)}${'.'.repeat(9)}${'#'.repeat(10)}`), FRAME),
    ).toHaveLength(1);
    expect(
      speechFromFrames(frames(`${'#'.repeat(10)}${'.'.repeat(10)}${'#'.repeat(10)}`), FRAME),
    ).toHaveLength(2);
  });

  it('drops a window shorter than 250 ms, and keeps one of 270 ms', () => {
    expect(speechFromFrames(frames('..########..'), FRAME)).toEqual([]);
    expect(speechFromFrames(frames('..#########..'), FRAME)).toHaveLength(1);
  });

  it('answers no speech for no frames and for silence', () => {
    expect(speechFromFrames([], FRAME)).toEqual([]);
    expect(speechFromFrames(frames('..........'), FRAME)).toEqual([]);
  });
});
