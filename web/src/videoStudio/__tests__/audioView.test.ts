import { describe, expect, it } from 'vitest';
import { envelopeFromView, envelopeView, peaksOf, sameView } from '../audioView';

// What the waveform shows, asked directly: the peaks of the stretch the lane displays, and the
// envelope in the plugin's terms and back.

describe('peaksOf', () => {
  it('takes the loudest sample of each bucket over the asked stretch only', () => {
    // 10 samples per second: second 1 is quiet, second 2 loud, and the bucket boundary splits them.
    const samples = Float32Array.from([
      0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0.1, -0.2, 0.1, 0, 0, 0, 0, 0, 0, 0, 0.9, -0.8, 0.5, 0, 0, 0, 0,
      0, 0, 0,
    ]);
    expect(Array.from(peaksOf(samples, 10, 1, 3, 2))).toEqual([
      expect.closeTo(0.2, 6),
      expect.closeTo(0.9, 6),
    ]);
  });

  it('answers zeros past the end of the samples, and nothing for no bucket', () => {
    expect(Array.from(peaksOf(Float32Array.from([0.5]), 10, 5, 6, 3))).toEqual([0, 0, 0]);
    expect(peaksOf(Float32Array.from([0.5]), 10, 0, 1, 0)).toHaveLength(0);
  });
});

describe('envelopeView', () => {
  it('always draws both edges, at the gain the envelope has there', () => {
    expect(envelopeView(undefined, 8)).toEqual([
      { time: 0, volume: 1 },
      { time: 8, volume: 1 },
    ]);
    expect(
      envelopeView(
        [
          { time: 2, gain: 0.5 },
          { time: 6, gain: 0.5 },
        ],
        8,
      ),
    ).toEqual([
      { time: 0, volume: 0.5 },
      { time: 2, volume: 0.5 },
      { time: 6, volume: 0.5 },
      { time: 8, volume: 0.5 },
    ]);
  });

  it('shows only the points inside the stretch the lane displays', () => {
    expect(
      envelopeView(
        [
          { time: 1, gain: 0 },
          { time: 9, gain: 0 },
        ],
        4,
      ).map((point) => point.time),
    ).toEqual([0, 1, 4]);
  });
});

describe('envelopeFromView', () => {
  it('replaces the shown points and keeps the ones past the stretch', () => {
    const next = envelopeFromView(
      [
        { time: 0, volume: 1 },
        { time: 3, volume: 0.2 },
        { time: 4, volume: 1 },
      ],
      [{ time: 9, gain: 0.4 }],
      4,
    );
    expect(next).toEqual([
      { time: 0, gain: 1 },
      { time: 3, gain: 0.2 },
      { time: 4, gain: 1 },
      { time: 9, gain: 0.4 },
    ]);
  });

  it('answers no envelope at all when every point is back at full gain', () => {
    expect(
      envelopeFromView(
        [
          { time: 0, volume: 1 },
          { time: 8, volume: 1 },
        ],
        undefined,
        8,
      ),
    ).toEqual([]);
  });

  it('sorts points the plugin reports out of order', () => {
    const next = envelopeFromView(
      [
        { time: 8, volume: 1 },
        { time: 0, volume: 1 },
        { time: 4, volume: 0 },
      ],
      undefined,
      8,
    );
    expect(next.map((point) => point.time)).toEqual([0, 4, 8]);
  });
});

describe('sameView', () => {
  it('compares time and volume whatever the order and whatever ids the plugin added', () => {
    const shown = [
      { id: 'b', time: 8, volume: 1 },
      { id: 'a', time: 0, volume: 0.5 },
    ];
    expect(
      sameView(shown, [
        { time: 0, volume: 0.5 },
        { time: 8, volume: 1 },
      ]),
    ).toBe(true);
    expect(sameView(shown, [{ time: 0, volume: 0.5 }])).toBe(false);
    expect(
      sameView(shown, [
        { time: 0, volume: 0.4 },
        { time: 8, volume: 1 },
      ]),
    ).toBe(false);
  });
});
