// sourceClock.ts — the one place a layer's source clock is read and applied.
//
// VideoFlow's mixer and renderer both look keyframes up in absolute source seconds (S1;
// RuntimeBaseLayer.js `sourceTimeAtFrame`). A layer's fade and its volume curve are written on the
// film's clock, so both carry their times through the same two numbers of the compiled layer:
// where it starts in its source (cut nudge included) and how fast it plays.

import type { VideoJSON } from '@videoflow/core';

export interface SourceClock {
  readonly sourceStart: number;
  readonly speed: number;
}

/** A compiled layer's clock. A speed of 0 or a missing one is 1×; a reversed one runs at its size. */
export function sourceClockOf(layer: VideoJSON['layers'][number]): SourceClock {
  return {
    sourceStart: layer.settings.sourceStart ?? 0,
    speed: Math.abs(Number(layer.settings.speed ?? 1)) || 1,
  };
}

/** `seconds` of film since the layer came on screen, as absolute seconds of its source. */
export function sourceSeconds(clock: SourceClock, seconds: number): number {
  return clock.sourceStart + seconds * clock.speed;
}
