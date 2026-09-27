import { describe, expect, it } from 'vitest';
import { anchorAt, audioLength, audioWindow, freeAudioTrack, reanchorAudio } from '../audioLane';
import {
  moveClip,
  removeItem,
  removeRange,
  setClipPresentation,
  setJunctionTransition,
  splitAt,
  trimClip,
} from '../commands';
import type { AudioItem, VideoProject } from '../project';

// The lane rules, asked directly: where a sound sits, and where it goes when the video lane under
// it is trimmed, split or cut. The three spec rules are the cases — it never lengthens the film,
// it never disappears because of a video edit, and a sound extracted from a clip goes with it.

// Two 4 s clips of one 10 s source, and a music bed hung on the second one second into it.
function project(item: Partial<AudioItem> = {}): VideoProject {
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
      },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [],
    audio: [
      {
        id: 'lane-a',
        items: [
          {
            id: 'bed',
            sourceId: 'src-m',
            anchor: { clipId: 'clip-2', offset: 1 },
            sourceStart: 0,
            duration: 2,
            volume: 1,
            muted: false,
            ...item,
          },
        ],
      },
    ],
  };
}

/** The bed as the project holds it now; a fixture that lost it is a broken fixture. */
function bed(next: VideoProject): AudioItem {
  const found = next.audio?.[0]?.items.find((item) => item.id === 'bed');
  if (found === undefined) throw new Error('the bed is gone');
  return found;
}

describe('audioWindow', () => {
  it('starts where the anchor puts it and lasts its source length over its speed', () => {
    expect(audioWindow(project(), bed(project()))).toEqual({ start: 5, end: 7 });
    expect(audioLength({ ...bed(project()), speed: 2 })).toBe(1);
  });

  it('is cut at the project end and never lengthens the project', () => {
    const long = project({ duration: 8 });
    expect(audioWindow(long, bed(long))).toEqual({ start: 5, end: 8 });
  });

  it('reads the anchor offset through the clip speed, like an overlay', () => {
    const fast = { ...project(), video: project().video.map((clip) => ({ ...clip, speed: 2 })) };
    // clip-2 starts at 2 s at double speed; an offset of 1 source second is half a timeline second.
    expect(audioWindow(fast, bed(fast)).start).toBe(2.5);
  });

  it('is empty when its anchor names no clip', () => {
    const lost = project({ anchor: { clipId: 'gone', offset: 0 } });
    expect(audioWindow(lost, bed(lost))).toEqual({ start: 0, end: 0 });
  });
});

describe('anchorAt', () => {
  it('hangs a time on the clip under it', () => {
    expect(anchorAt(project(), 6)).toEqual({ clipId: 'clip-2', offset: 2 });
  });

  it('falls back to the last clip at its start past the last frame, and to nothing with no clip', () => {
    expect(anchorAt(project(), 99)).toEqual({ clipId: 'clip-2', offset: 0 });
    expect(anchorAt({ ...project(), video: [] }, 1)).toBeUndefined();
  });
});

describe('freeAudioTrack', () => {
  it('names a lane only where nothing covers the span, the item itself excepted', () => {
    expect(freeAudioTrack(project(), { start: 5.5, end: 6 })).toBeUndefined();
    expect(freeAudioTrack(project(), { start: 0, end: 5 })).toBe('lane-a');
    expect(freeAudioTrack(project(), { start: 5.5, end: 6 }, 'bed')).toBe('lane-a');
  });
});

describe('a sound follows the video lane', () => {
  it('rides the ripple when an earlier clip is trimmed', () => {
    const next = trimClip(project(), { clipId: 'clip-1', start: 1, end: 4 });
    expect(bed(next).anchor).toEqual({ clipId: 'clip-2', offset: 1 });
    expect(audioWindow(next, bed(next)).start).toBe(4);
  });

  it('follows the half of a split clip that holds its offset', () => {
    const next = splitAt(project(), { time: 4.5 });
    const right = next.video[2];
    expect(bed(next).anchor).toEqual({ clipId: right?.id, offset: 0.5 });
    expect(audioWindow(next, bed(next)).start).toBe(5);
  });

  it('keeps its project time when the content under its anchor is removed', () => {
    const next = removeRange(project(), { from: 4, to: 6 });
    // clip-2 lost its first two seconds; the sound started at 5 s and still does, on clip-2
    // (spec §Model: "re-anchors to whatever clip now covers the same project time").
    expect(bed(next).anchor).toEqual({ clipId: 'clip-2', offset: 1 });
    expect(audioWindow(next, bed(next)).start).toBeCloseTo(5, 6);
  });

  it('re-anchors instead of disappearing when its clip is removed', () => {
    const next = removeItem(project(), { itemId: 'clip-2' });
    expect(bed(next).anchor).toEqual({ clipId: 'clip-1', offset: 0 });
  });

  it('goes with the clip it was extracted from, when that clip is removed whole', () => {
    const next = removeItem(project({ extractedFrom: 'clip-2' }), { itemId: 'clip-2' });
    expect(next.audio?.[0]?.items).toEqual([]);
  });

  it('stays when the clip it was extracted from survives the edit', () => {
    const next = trimClip(project({ extractedFrom: 'clip-2' }), {
      clipId: 'clip-2',
      start: 2,
      end: 4,
    });
    expect(bed(next).extractedFrom).toBe('clip-2');
  });

  it('outlives either half of the clip it was extracted from once a split cut that clip in two', () => {
    const extracted = project({ extractedFrom: 'clip-2', anchor: { clipId: 'clip-2', offset: 0 } });
    const split = splitAt(extracted, { time: 6 });
    // The split keeps clip-2's id on the left half; removing it must not take the whole sound.
    const next = removeItem(split, { itemId: 'clip-2' });
    expect(bed(next).extractedFrom).toBeUndefined();
    expect(audioWindow(next, bed(next)).start).toBe(4);
  });

  it('empties the lanes when no clip is left to hang on', () => {
    const next = removeRange(project(), { from: 0, to: 8 });
    expect(next.audio?.[0]?.items).toEqual([]);
  });

  it('leaves a project saved before audio existed without an audio key', () => {
    const { audio: _audio, ...legacy } = project();
    expect(trimClip(legacy, { clipId: 'clip-1', start: 1, end: 4 }).audio).toBeUndefined();
    expect(removeItem(legacy, { itemId: 'clip-1' }).audio).toBeUndefined();
    expect(reanchorAudio(legacy, legacy, new Map())).toBeUndefined();
  });

  it('removes an audio item by id and nothing else', () => {
    const next = removeItem(project(), { itemId: 'bed' });
    expect(next.audio?.[0]?.items).toEqual([]);
    expect(next.video).toHaveLength(2);
  });
});

describe('no two sounds cover the same instant of one lane', () => {
  // `early` covers 0–3 on clip-1 and the bed 4–6 on clip-2: neighbours on one lane until the video
  // under them moves — a video edit carries sounds with their clips but never shortens one.
  function crowded(): VideoProject {
    const base = project({ anchor: { clipId: 'clip-2', offset: 0 } });
    const early: AudioItem = {
      ...bed(base),
      id: 'early',
      anchor: { clipId: 'clip-1', offset: 0 },
      duration: 3,
    };
    return { ...base, audio: [{ id: 'lane-a', items: [early, bed(base)] }] };
  }

  function lanes(next: VideoProject): string[][] {
    return (next.audio ?? []).map((lane) => lane.items.map((item) => item.id));
  }

  it.each([
    ['a trim', (p: VideoProject) => trimClip(p, { clipId: 'clip-1', start: 0, end: 2 })],
    ['a clip speed', (p: VideoProject) => setClipPresentation(p, { clipId: 'clip-1', speed: 2 })],
    [
      'a junction',
      (p: VideoProject) =>
        setJunctionTransition(p, {
          fromClipId: 'clip-1',
          toClipId: 'clip-2',
          transition: 'crossfade',
          duration: 1.5,
        }),
    ],
    ['a removal', (p: VideoProject) => removeItem(p, { itemId: 'clip-2' })],
  ])('moves the sound %s pushed onto its neighbour to a lane of its own', (_edit, edit) => {
    const next = edit(crowded());
    expect(lanes(next)).toEqual([['early'], ['bed']]);
    expect(next.audio?.[0]?.id).toBe('lane-a');
  });

  it('moves it to the first lane already free over its window rather than opening one', () => {
    const quiet: AudioItem = {
      ...bed(crowded()),
      id: 'quiet',
      anchor: { clipId: 'clip-1', offset: 0 },
      duration: 1,
    };
    const withSecondLane = {
      ...crowded(),
      audio: [...(crowded().audio ?? []), { id: 'lane-b', items: [quiet] }],
    };
    const next = trimClip(withSecondLane, { clipId: 'clip-1', start: 0, end: 2 });
    expect(lanes(next)).toEqual([['early'], ['quiet', 'bed']]);
  });

  it('moves the sound that collides after a reorder, and keeps the one that was there', () => {
    // The long sound on clip-2 runs past it; once clip-2 leads, it covers where clip-1's sound is.
    const base = crowded();
    const long: AudioItem = { ...bed(base), duration: 6 };
    const reordered = moveClip(
      {
        ...base,
        audio: [
          {
            id: 'lane-a',
            items: [long, { ...bed(base), id: 'short', anchor: { clipId: 'clip-1', offset: 0 } }],
          },
        ],
      },
      { clipId: 'clip-2', toIndex: 0 },
    );
    expect(lanes(reordered)).toEqual([['bed'], ['short']]);
  });

  it('leaves the project as it was when nothing collides', () => {
    const next = trimClip(crowded(), { clipId: 'clip-1', start: 0, end: 4 });
    expect(lanes(next)).toEqual([['early', 'bed']]);
  });
});
