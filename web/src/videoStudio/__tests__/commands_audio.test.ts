import { describe, expect, it } from 'vitest';
import { audioWindow } from '../audioLane';
import { CommandRefusal } from '../commands';
import {
  addAudio,
  AUDIO_REFUSAL,
  moveAudio,
  recordAnalysis,
  setAudioProperties,
  setEnvelope,
  splitAudio,
  trimAudio,
} from '../commands_audio';
import { projectDuration, type AudioItem, type VideoProject } from '../project';

// The audio commands' contract, the one commands.ts keeps: a decision the editor declines is a
// refusal by key, a caller out of step with the model is loud, and nothing is mutated.

function project(items: AudioItem[] = []): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 30,
    sources: [
      {
        id: 'src-a',
        assetId: 'a',
        kind: 'video',
        duration: 10,
        size: { width: 1920, height: 1080 },
        hasAudio: true,
      },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
      { id: 'src-img', assetId: 'i', kind: 'image', duration: 0, size: { width: 8, height: 6 } },
      {
        id: 'src-mute',
        assetId: 's',
        kind: 'video',
        duration: 5,
        size: { width: 8, height: 6 },
        hasAudio: false,
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [],
    ...(items.length === 0 ? {} : { audio: [{ id: 'lane-a', items }] }),
  };
}

function item(over: Partial<AudioItem> = {}): AudioItem {
  return {
    id: 'bed',
    sourceId: 'src-m',
    anchor: { clipId: 'clip-1', offset: 0 },
    sourceStart: 0,
    duration: 4,
    volume: 1,
    muted: false,
    ...over,
  };
}

function refusalKey(run: () => unknown): string {
  try {
    run();
  } catch (error) {
    if (error instanceof CommandRefusal) return error.reasonKey;
    throw error;
  }
  throw new Error('the command did not refuse');
}

function sounds(next: VideoProject): readonly AudioItem[] {
  return next.audio?.flatMap((lane) => lane.items) ?? [];
}

function first(next: VideoProject): AudioItem {
  const found = sounds(next)[0];
  if (found === undefined) throw new Error('no sound on any lane');
  return found;
}

describe('addAudio', () => {
  it('opens the first lane with the whole sound hung on the clip under the time', () => {
    const next = addAudio(project(), { sourceId: 'src-m', time: 5, label: 'Bed' });
    expect(first(next)).toMatchObject({
      anchor: { clipId: 'clip-2', offset: 1 },
      duration: 8,
      volume: 1,
      muted: false,
      label: 'Bed',
    });
    expect(projectDuration(next)).toBe(8);
  });

  it('joins a lane free over its window and opens another where it is not', () => {
    const base = project([item({ duration: 2 })]);
    expect(addAudio(base, { sourceId: 'src-m', time: 3 }).audio).toHaveLength(1);
    expect(addAudio(base, { sourceId: 'src-m', time: 1 }).audio).toHaveLength(2);
  });

  it('plays the sound of a video source that has one', () => {
    expect(first(addAudio(project(), { sourceId: 'src-a', time: 0 })).duration).toBe(10);
  });

  it('refuses a still, a silent clip, a missing source, a lane with no clip and a time past the end', () => {
    expect(refusalKey(() => addAudio(project(), { sourceId: 'src-img', time: 0 }))).toBe(
      AUDIO_REFUSAL.notSound,
    );
    expect(refusalKey(() => addAudio(project(), { sourceId: 'src-mute', time: 0 }))).toBe(
      AUDIO_REFUSAL.notSound,
    );
    expect(refusalKey(() => addAudio(project(), { sourceId: 'nope', time: 0 }))).toBe(
      AUDIO_REFUSAL.notSound,
    );
    expect(
      refusalKey(() => addAudio({ ...project(), video: [] }, { sourceId: 'src-m', time: 0 })),
    ).toBe(AUDIO_REFUSAL.noClip);
    expect(refusalKey(() => addAudio(project(), { sourceId: 'src-m', time: 8 }))).toBe(
      AUDIO_REFUSAL.pastEnd,
    );
  });
});

describe('moveAudio', () => {
  it('re-hangs the sound on the clip under its new start', () => {
    const next = moveAudio(project([item()]), { itemId: 'bed', start: 6 });
    expect(first(next).anchor).toEqual({ clipId: 'clip-2', offset: 2 });
  });

  it('refuses a drop over another sound on the lane, and one at or past the end', () => {
    const base = project([
      item({ duration: 2 }),
      item({ id: 'voice', anchor: { clipId: 'clip-2', offset: 0 }, duration: 2 }),
    ]);
    expect(refusalKey(() => moveAudio(base, { itemId: 'bed', start: 3.5 }))).toBe(
      AUDIO_REFUSAL.overlap,
    );
    expect(refusalKey(() => moveAudio(base, { itemId: 'bed', start: 8 }))).toBe(
      AUDIO_REFUSAL.pastEnd,
    );
  });

  it('moves to another lane when asked, and is loud about a lane that is not there', () => {
    const base = {
      ...project(),
      audio: [
        { id: 'lane-a', items: [item()] },
        { id: 'lane-b', items: [] },
      ],
    };
    const next = moveAudio(base, { itemId: 'bed', start: 0, trackId: 'lane-b' });
    expect(next.audio?.map((lane) => lane.items.length)).toEqual([0, 1]);
    expect(() => moveAudio(base, { itemId: 'bed', start: 0, trackId: 'nope' })).toThrow(
      /no audio track/,
    );
  });

  it('refuses a move when there is no clip left to hang on', () => {
    const base = { ...project([item()]), video: [] };
    expect(refusalKey(() => moveAudio(base, { itemId: 'bed', start: -1 }))).toBe(
      AUDIO_REFUSAL.noClip,
    );
  });
});

describe('trimAudio', () => {
  it('trims the head and keeps the sound aligned with the picture', () => {
    const next = trimAudio(project([item()]), { itemId: 'bed', start: 1, end: 4 });
    expect(first(next)).toMatchObject({ sourceStart: 1, duration: 3 });
    expect(audioWindow(next, first(next)).start).toBe(1);
  });

  it('keeps envelope points on their material and drops the ones trimmed away', () => {
    const base = project([
      item({
        envelope: [
          { time: 0.5, gain: 1 },
          { time: 2, gain: 0 },
        ],
      }),
    ]);
    expect(first(trimAudio(base, { itemId: 'bed', start: 1, end: 4 })).envelope).toEqual([
      { time: 1, gain: 0 },
    ]);
  });

  it('clamps fades that no longer fit', () => {
    const trimmed = first(
      trimAudio(project([item({ fadeIn: 2, fadeOut: 2 })]), { itemId: 'bed', start: 0, end: 3 }),
    );
    expect((trimmed.fadeIn ?? 0) + (trimmed.fadeOut ?? 0)).toBeLessThanOrEqual(3);
  });

  it('refuses a trim past the source, an empty one, and one that would start before the film', () => {
    const base = project([item()]);
    expect(refusalKey(() => trimAudio(base, { itemId: 'bed', start: 0, end: 9 }))).toBe(
      AUDIO_REFUSAL.trimPastSource,
    );
    expect(refusalKey(() => trimAudio(base, { itemId: 'bed', start: 2, end: 2 }))).toBe(
      AUDIO_REFUSAL.trimPastSource,
    );
    const late = project([item({ sourceStart: 2 })]);
    expect(refusalKey(() => trimAudio(late, { itemId: 'bed', start: -1, end: 4 }))).toBe(
      AUDIO_REFUSAL.beforeStart,
    );
  });

  it('is loud about a sound the project does not have', () => {
    expect(() => trimAudio(project([item()]), { itemId: 'nope', start: 0, end: 1 })).toThrow(
      /no audio item/,
    );
  });
});

describe('splitAudio', () => {
  it('cuts the sound in two at the time, each half keeping its own fade and envelope', () => {
    const base = project([
      item({
        fadeIn: 1,
        fadeOut: 1,
        envelope: [
          { time: 1, gain: 1 },
          { time: 3, gain: 0.5 },
        ],
      }),
    ]);
    const [left, right] = sounds(splitAudio(base, { itemId: 'bed', time: 2 }));
    expect(left).toMatchObject({
      id: 'bed',
      duration: 2,
      fadeIn: 1,
      envelope: [{ time: 1, gain: 1 }],
    });
    expect(left?.fadeOut).toBeUndefined();
    expect(right).toMatchObject({
      sourceStart: 2,
      duration: 2,
      fadeOut: 1,
      anchor: { clipId: 'clip-1', offset: 2 },
      envelope: [{ time: 1, gain: 0.5 }],
    });
    expect(right?.fadeIn).toBeUndefined();
    expect(right?.id).not.toBe('bed');
  });

  it('refuses a cut on an edge of the sound', () => {
    expect(refusalKey(() => splitAudio(project([item()]), { itemId: 'bed', time: 0 }))).toBe(
      AUDIO_REFUSAL.splitOnBoundary,
    );
    expect(refusalKey(() => splitAudio(project([item()]), { itemId: 'bed', time: 4 }))).toBe(
      AUDIO_REFUSAL.splitOnBoundary,
    );
  });
});

describe('setAudioProperties', () => {
  it('sets volume, mute, fades, speed, denoise and ducking, and null turns ducking off', () => {
    const next = setAudioProperties(project([item()]), {
      itemId: 'bed',
      volume: 0.5,
      muted: true,
      fadeIn: 1,
      fadeOut: 1,
      speed: 2,
      denoise: true,
      ducking: { amountDb: -12, ramp: 0.5 },
    });
    expect(first(next)).toMatchObject({
      volume: 0.5,
      muted: true,
      fadeIn: 1,
      fadeOut: 1,
      speed: 2,
      denoise: true,
      ducking: { amountDb: -12, ramp: 0.5 },
    });
    expect(first(setAudioProperties(next, { itemId: 'bed', ducking: null })).ducking).toBe(
      undefined,
    );
  });

  it('refuses fades longer than the sound, and a speed that runs it into its neighbour', () => {
    expect(
      refusalKey(() =>
        setAudioProperties(project([item()]), { itemId: 'bed', fadeIn: 3, fadeOut: 2 }),
      ),
    ).toBe(AUDIO_REFUSAL.fadesTooLong);
    const crowded = project([
      item({ duration: 2 }),
      item({ id: 'voice', anchor: { clipId: 'clip-1', offset: 2 }, duration: 2 }),
    ]);
    expect(refusalKey(() => setAudioProperties(crowded, { itemId: 'bed', speed: 0.5 }))).toBe(
      AUDIO_REFUSAL.overlap,
    );
  });

  it('is loud about values no control can produce', () => {
    const base = project([item()]);
    expect(() => setAudioProperties(base, { itemId: 'bed', volume: 3 })).toThrow(/volume/);
    expect(() => setAudioProperties(base, { itemId: 'bed', speed: 9 })).toThrow(/speed/);
    expect(() => setAudioProperties(base, { itemId: 'bed', fadeIn: 6 })).toThrow(/fade/);
    expect(() =>
      setAudioProperties(base, { itemId: 'bed', ducking: { amountDb: 0, ramp: 1 } }),
    ).toThrow(/ducking/);
    expect(() => setAudioProperties(base, { itemId: 'nope', volume: 1 })).toThrow(/no audio item/);
  });
});

describe('setEnvelope', () => {
  it('sorts the points, clamps them to the sound and to 0–1, and an empty list removes the envelope', () => {
    const next = setEnvelope(project([item()]), {
      itemId: 'bed',
      points: [
        { time: 3, gain: 2 },
        { time: -1, gain: 0.5 },
        { time: Number.NaN, gain: 1 },
      ],
    });
    expect(first(next).envelope).toEqual([
      { time: 0, gain: 0.5 },
      { time: 3, gain: 1 },
    ]);
    expect(first(setEnvelope(next, { itemId: 'bed', points: [] })).envelope).toBeUndefined();
  });
});

describe('recordAnalysis', () => {
  it('writes merged, sorted speech windows inside the source, and the denoised asset', () => {
    const next = recordAnalysis(project(), {
      sourceId: 'src-m',
      speech: [
        [5, 9],
        [1, 2],
        [1.5, 3],
      ],
      denoisedAssetId: 'clean',
    });
    expect(next.sources.find((source) => source.id === 'src-m')).toMatchObject({
      speech: [
        [1, 3],
        [5, 8],
      ],
      denoisedAssetId: 'clean',
    });
  });

  it('refuses a source the project does not hold', () => {
    expect(
      refusalKey(() => recordAnalysis(project(), { sourceId: 'nope', denoisedAssetId: 'x' })),
    ).toBe(AUDIO_REFUSAL.sourceMissing);
  });
});
