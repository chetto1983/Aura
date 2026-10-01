import type { VideoJSON } from '@videoflow/core';
import { describe, expect, it } from 'vitest';
import { sourceClockOf, sourceSeconds } from '../sourceClock';

function layer(settings: Record<string, unknown>): VideoJSON['layers'][number] {
  return { settings, properties: {}, animations: [] } as unknown as VideoJSON['layers'][number];
}

describe('a compiled layer’s source clock', () => {
  it('reads where the layer starts in its source and how fast it plays', () => {
    expect(sourceClockOf(layer({ sourceStart: 30.0001, speed: 2 }))).toEqual({
      sourceStart: 30.0001,
      speed: 2,
    });
  });

  it('plays a layer without either at the start of its source, at 1×', () => {
    expect(sourceClockOf(layer({}))).toEqual({ sourceStart: 0, speed: 1 });
  });

  it('treats a speed of 0 as 1× and a reversed speed as its size', () => {
    expect(sourceClockOf(layer({ speed: 0 })).speed).toBe(1);
    expect(sourceClockOf(layer({ speed: -2 })).speed).toBe(2);
  });

  it('carries film seconds into source seconds', () => {
    expect(sourceSeconds({ sourceStart: 1, speed: 2 }, 0)).toBe(1);
    expect(sourceSeconds({ sourceStart: 1, speed: 2 }, 1.5)).toBe(4);
  });
});
