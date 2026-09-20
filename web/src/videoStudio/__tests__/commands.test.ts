import { describe, expect, it } from 'vitest';
import {
  addClip,
  addOverlay,
  CommandRefusal,
  freeOverlayTrack,
  moveClip,
  removeItem,
  removeRange,
  setMuted,
  setProperty,
  splitAt,
  trimClip,
} from '../commands';
import { clipStarts, projectDuration, type VideoProject } from '../project';

function project(): VideoProject {
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
        fps: 30,
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [
      {
        id: 'lane-1',
        items: [
          {
            id: 'title',
            kind: 'text',
            anchor: { clipId: 'clip-2', offset: 1 },
            duration: 1,
            props: {},
          },
        ],
      },
    ],
  };
}

/** A project whose source is a still, which has no length of its own to run past. */
function stills(): VideoProject {
  return {
    ...project(),
    sources: [
      {
        id: 'src-img',
        assetId: 'b',
        kind: 'image',
        duration: 0,
        size: { width: 8, height: 6 },
        fps: 0,
      },
    ],
    video: [{ id: 'still-1', sourceId: 'src-img', duration: 3, sourceStart: 0, muted: true }],
    overlays: [],
  };
}

/** The key a refusal carries, so a test can name the sentence the shell will show. */
function refusalKey(run: () => unknown): string {
  try {
    run();
  } catch (error) {
    if (error instanceof CommandRefusal) return error.reasonKey;
    throw error;
  }
  throw new Error('the command did not refuse');
}

describe('trimClip ripples', () => {
  it('shortens the clip and pulls everything after it back', () => {
    const next = trimClip(project(), { clipId: 'clip-1', start: 1, end: 4 });
    expect(next.video[0]?.duration).toBe(3);
    expect(next.video[0]?.sourceStart).toBe(1);
    expect(clipStarts(next)).toEqual([0, 3]);
    expect(projectDuration(next)).toBe(7);
  });

  it('refuses a trim past the source', () => {
    expect(() => trimClip(project(), { clipId: 'clip-2', start: 0, end: 9 })).toThrow(
      CommandRefusal,
    );
    expect(refusalKey(() => trimClip(project(), { clipId: 'clip-2', start: 0, end: 9 }))).toBe(
      'videoStudio.refusal.trimPastSource',
    );
  });

  it('refuses a trim with nothing left in it, or one starting before the source', () => {
    expect(refusalKey(() => trimClip(project(), { clipId: 'clip-1', start: 2, end: 2 }))).toBe(
      'videoStudio.refusal.trimPastSource',
    );
    expect(refusalKey(() => trimClip(project(), { clipId: 'clip-1', start: -1, end: 2 }))).toBe(
      'videoStudio.refusal.trimPastSource',
    );
  });

  it('hands material back, and the overlay stays over the content it was over', () => {
    const next = trimClip(project(), { clipId: 'clip-2', start: -4, end: 4 });
    expect(next.video[1]?.duration).toBe(8);
    expect(next.video[1]?.sourceStart).toBe(0);
    expect(projectDuration(next)).toBe(12);
    expect(next.overlays[0]?.items[0]?.anchor).toEqual({ clipId: 'clip-2', offset: 5 });
  });

  it('refuses when the clip names a source the project does not carry', () => {
    const orphaned = { ...project(), sources: [] };
    expect(refusalKey(() => trimClip(orphaned, { clipId: 'clip-1', start: 0, end: 2 }))).toBe(
      'videoStudio.refusal.sourceMissing',
    );
  });
});

describe('splitAt', () => {
  it('cuts one clip into two that together are the original', () => {
    const next = splitAt(project(), { time: 1.5 });
    expect(next.video).toHaveLength(3);
    expect(next.video[0]?.duration).toBe(1.5);
    expect(next.video[1]?.duration).toBe(2.5);
    expect(next.video[1]?.sourceStart).toBe(1.5);
    expect(projectDuration(next)).toBe(8);
  });

  it('refuses a split on a boundary, which would make an empty clip', () => {
    expect(() => splitAt(project(), { time: 4 })).toThrow(CommandRefusal);
    expect(refusalKey(() => splitAt(project(), { time: 4 }))).toBe(
      'videoStudio.refusal.splitOnBoundary',
    );
  });

  it('refuses a split past the end, and one a float away from a boundary', () => {
    expect(refusalKey(() => splitAt(project(), { time: 8 }))).toBe(
      'videoStudio.refusal.splitOnBoundary',
    );
    expect(refusalKey(() => splitAt(project(), { time: 3.9999999 }))).toBe(
      'videoStudio.refusal.splitOnBoundary',
    );
  });

  it('sends an overlay to the half that holds it', () => {
    const next = splitAt(project(), { time: 4.5 });
    expect(next.video.map((clip) => clip.id)).toEqual(['clip-1', 'clip-2', expect.any(String)]);
    expect(next.overlays[0]?.items[0]?.anchor).toEqual({
      clipId: next.video[2]?.id,
      offset: 0.5,
    });
  });

  it('leaves an overlay on the left half when that is where it sits', () => {
    const next = splitAt(project(), { time: 5.5 });
    expect(next.overlays[0]?.items[0]?.anchor).toEqual({ clipId: 'clip-2', offset: 1 });
  });
});

describe('removeRange', () => {
  it('cuts a hole out of the middle and closes it', () => {
    const next = removeRange(project(), { from: 1, to: 3 });
    expect(projectDuration(next)).toBe(6);
    expect(clipStarts(next)).toEqual([0, 1, 2]);
  });

  it('refuses a range with nothing in it, backwards or off the end', () => {
    expect(refusalKey(() => removeRange(project(), { from: 3, to: 3 }))).toBe(
      'videoStudio.refusal.emptyRange',
    );
    expect(refusalKey(() => removeRange(project(), { from: 5, to: 2 }))).toBe(
      'videoStudio.refusal.emptyRange',
    );
    expect(refusalKey(() => removeRange(project(), { from: 20, to: 30 }))).toBe(
      'videoStudio.refusal.emptyRange',
    );
  });

  it('drops a clip it covers whole, and the overlays that hung on it', () => {
    const next = removeRange(project(), { from: 4, to: 8 });
    expect(next.video.map((clip) => clip.id)).toEqual(['clip-1']);
    expect(next.overlays[0]?.items).toEqual([]);
    expect(projectDuration(next)).toBe(4);
  });

  it('shortens a clip at its head and keeps the overlay over the same content', () => {
    const next = removeRange(project(), { from: 4, to: 4.5 });
    expect(next.video[1]).toEqual({
      id: 'clip-2',
      sourceId: 'src-a',
      duration: 3.5,
      sourceStart: 4.5,
      muted: false,
    });
    expect(next.overlays[0]?.items[0]?.anchor).toEqual({ clipId: 'clip-2', offset: 0.5 });
  });

  it('shortens a clip at its tail', () => {
    const next = removeRange(project(), { from: 2, to: 4 });
    expect(next.video.map((clip) => clip.duration)).toEqual([2, 4]);
    expect(next.video[0]?.sourceStart).toBe(0);
  });

  it('takes an overlay with the content it removed', () => {
    const next = removeRange(project(), { from: 4.5, to: 5.5 });
    expect(next.video).toHaveLength(3);
    expect(next.overlays[0]?.items).toEqual([]);
  });
});

describe('moveClip inserts, never overlaps', () => {
  it('puts the clip at the index asked for and keeps the lane abutting', () => {
    const next = moveClip(project(), { clipId: 'clip-2', toIndex: 0 });
    expect(next.video.map((clip) => clip.id)).toEqual(['clip-2', 'clip-1']);
    expect(clipStarts(next)).toEqual([0, 4]);
  });

  it('carries the overlays anchored to the moved clip', () => {
    const next = moveClip(project(), { clipId: 'clip-2', toIndex: 0 });
    expect(next.overlays[0]?.items[0]?.anchor.clipId).toBe('clip-2');
  });

  it('lands at the end rather than off it', () => {
    const next = moveClip(project(), { clipId: 'clip-1', toIndex: 99 });
    expect(next.video.map((clip) => clip.id)).toEqual(['clip-2', 'clip-1']);
  });

  // An id the project does not have is a caller out of step with the model, not a decision the
  // editor declines — so it is loud rather than one of the five translated refusals.
  it('is loud about a clip the project does not have', () => {
    expect(() => moveClip(project(), { clipId: 'nope', toIndex: 0 })).toThrow(/no clip named nope/);
  });
});

describe('setMuted', () => {
  it('mutes one clip and leaves the rest alone', () => {
    const next = setMuted(project(), { clipId: 'clip-1', muted: true });
    expect(next.video[0]?.muted).toBe(true);
    expect(next.video[1]?.muted).toBe(false);
  });
});

describe('addClip', () => {
  it('appends by default', () => {
    const next = addClip(project(), { sourceId: 'src-a', duration: 2, sourceStart: 6 });
    expect(next.video).toHaveLength(3);
    expect(next.video[2]?.sourceStart).toBe(6);
    expect(next.video[2]?.muted).toBe(false);
    expect(projectDuration(next)).toBe(10);
  });

  it('inserts at the index asked for, and at neither end past it', () => {
    const first = addClip(project(), { sourceId: 'src-a', duration: 1, atIndex: 0 });
    expect(clipStarts(first)).toEqual([0, 1, 5]);
    const last = addClip(project(), { sourceId: 'src-a', duration: 1, atIndex: 99 });
    expect(last.video[2]?.duration).toBe(1);
    const before = addClip(project(), { sourceId: 'src-a', duration: 1, atIndex: -3 });
    expect(before.video[0]?.duration).toBe(1);
  });

  it('refuses a source the project does not carry', () => {
    expect(refusalKey(() => addClip(project(), { sourceId: 'src-gone', duration: 1 }))).toBe(
      'videoStudio.refusal.sourceMissing',
    );
  });

  it('refuses a clip that runs past its source, or holds nothing', () => {
    expect(
      refusalKey(() => addClip(project(), { sourceId: 'src-a', duration: 5, sourceStart: 6 })),
    ).toBe('videoStudio.refusal.trimPastSource');
    expect(refusalKey(() => addClip(project(), { sourceId: 'src-a', duration: 0 }))).toBe(
      'videoStudio.refusal.trimPastSource',
    );
    expect(
      refusalKey(() => addClip(project(), { sourceId: 'src-a', duration: 1, sourceStart: -1 })),
    ).toBe('videoStudio.refusal.trimPastSource');
  });

  it('lets a still last as long as the clip asks', () => {
    const next = addClip(stills(), { sourceId: 'src-img', duration: 30 });
    expect(projectDuration(next)).toBe(33);
  });
});

describe('addOverlay', () => {
  it('opens a lane of its own when no track is named', () => {
    const next = addOverlay(project(), {
      kind: 'text',
      anchor: { clipId: 'clip-1', offset: 0 },
      duration: 1,
      props: { text: 'hi' },
    });
    expect(next.overlays).toHaveLength(2);
    expect(next.overlays[1]?.items[0]?.kind).toBe('text');
    expect(next.overlays[1]?.items[0]?.props).toEqual({ text: 'hi' });
  });

  it('refuses a second overlay over the same instant of one lane', () => {
    expect(
      refusalKey(() =>
        addOverlay(project(), {
          trackId: 'lane-1',
          kind: 'text',
          anchor: { clipId: 'clip-2', offset: 1.5 },
          duration: 1,
          props: {},
        }),
      ),
    ).toBe('videoStudio.refusal.overlayOverlap');
  });

  it('takes one that starts where the last one ends', () => {
    const next = addOverlay(project(), {
      trackId: 'lane-1',
      kind: 'text',
      anchor: { clipId: 'clip-2', offset: 2 },
      duration: 1,
      props: {},
    });
    expect(next.overlays[0]?.items).toHaveLength(2);
  });

  it('pulls an offset before the clip back to its start', () => {
    const next = addOverlay(project(), {
      trackId: 'lane-1',
      kind: 'image',
      anchor: { clipId: 'clip-1', offset: -5 },
      duration: 1,
      props: {},
    });
    expect(next.overlays[0]?.items[1]?.anchor).toEqual({ clipId: 'clip-1', offset: 0 });
  });

  it('refuses an overlay that would cover no instant at all', () => {
    expect(
      refusalKey(() =>
        addOverlay(project(), {
          kind: 'text',
          anchor: { clipId: 'clip-1', offset: 0 },
          duration: 0,
          props: {},
        }),
      ),
    ).toBe('videoStudio.refusal.emptyRange');
    expect(
      refusalKey(() =>
        addOverlay(project(), {
          kind: 'text',
          anchor: { clipId: 'clip-1', offset: 9 },
          duration: 1,
          props: {},
        }),
      ),
    ).toBe('videoStudio.refusal.emptyRange');
  });

  it('is loud about a lane the project does not have', () => {
    expect(() =>
      addOverlay(project(), {
        trackId: 'lane-9',
        kind: 'text',
        anchor: { clipId: 'clip-1', offset: 0 },
        duration: 1,
        props: {},
      }),
    ).toThrow(/no overlay track named lane-9/);
  });
});

describe('setProperty', () => {
  it('changes one property and leaves the others standing', () => {
    const once = setProperty(project(), { itemId: 'title', key: 'text', value: 'ciao' });
    const twice = setProperty(once, { itemId: 'title', key: 'size', value: 48 });
    expect(twice.overlays[0]?.items[0]?.props).toEqual({ text: 'ciao', size: 48 });
  });

  it('is loud about an id that is not an overlay', () => {
    expect(() => setProperty(project(), { itemId: 'clip-1', key: 'text', value: 'x' })).toThrow(
      /no overlay item named clip-1/,
    );
  });

  it('leaves the other lanes and the other overlays where they were', () => {
    const second = addOverlay(project(), {
      kind: 'image',
      anchor: { clipId: 'clip-1', offset: 0 },
      duration: 1,
      props: { assetId: 'b' },
    });
    const crowded = addOverlay(second, {
      trackId: 'lane-1',
      kind: 'text',
      anchor: { clipId: 'clip-2', offset: 3 },
      duration: 1,
      props: { text: 'end' },
    });
    const next = setProperty(crowded, { itemId: 'title', key: 'text', value: 'ciao' });
    expect(next.overlays[0]?.items[0]?.props).toEqual({ text: 'ciao' });
    expect(next.overlays[0]?.items[1]?.props).toEqual({ text: 'end' });
    expect(next.overlays[1]).toEqual(crowded.overlays[1]);
  });
});

describe('removeItem', () => {
  it('takes the overlays anchored to a clip with the clip', () => {
    const next = removeItem(project(), { itemId: 'clip-2' });
    expect(next.video.map((clip) => clip.id)).toEqual(['clip-1']);
    expect(next.overlays[0]?.items).toEqual([]);
  });

  it('removes one overlay and nothing else', () => {
    const next = removeItem(project(), { itemId: 'title' });
    expect(next.overlays[0]?.items).toEqual([]);
    expect(next.video).toHaveLength(2);
  });
});

describe('every command is pure', () => {
  it('never edits the project it was given', () => {
    const before = project();
    const snapshot = structuredClone(before);
    trimClip(before, { clipId: 'clip-1', start: 1, end: 4 });
    splitAt(before, { time: 1.5 });
    removeRange(before, { from: 1, to: 3 });
    moveClip(before, { clipId: 'clip-2', toIndex: 0 });
    setMuted(before, { clipId: 'clip-1', muted: true });
    addClip(before, { sourceId: 'src-a', duration: 1 });
    addOverlay(before, {
      kind: 'text',
      anchor: { clipId: 'clip-1', offset: 0 },
      duration: 1,
      props: {},
    });
    setProperty(before, { itemId: 'title', key: 'text', value: 'x' });
    removeItem(before, { itemId: 'title' });
    expect(before).toEqual(snapshot);
  });
});

describe('freeOverlayTrack', () => {
  // The lane an overlay JOINS, which is what makes the spec's third lane arrive on demand
  // instead of once per title. `lane-1` is busy over 5s..6s — its title hangs on clip-2, which
  // starts at 4 — and free everywhere else.
  it('names the first lane whose window is free', () => {
    expect(freeOverlayTrack(project(), { clipId: 'clip-1', offset: 0 }, 2)).toBe('lane-1');
  });

  it('names nothing when every lane is busy over that window', () => {
    expect(freeOverlayTrack(project(), { clipId: 'clip-2', offset: 0.5 }, 1)).toBeUndefined();
  });

  it('names nothing when there is no lane at all', () => {
    expect(
      freeOverlayTrack({ ...project(), overlays: [] }, { clipId: 'clip-1', offset: 0 }, 1),
    ).toBeUndefined();
  });

  it('passes a busy lane by for the next free one', () => {
    const two = addOverlay(project(), {
      kind: 'text',
      anchor: { clipId: 'clip-1', offset: 0 },
      duration: 1,
      props: {},
    });
    // 5s..5.5s sits inside lane-1's title and outside the new lane's, so the answer is the
    // SECOND lane — the picker walks past a busy one rather than stopping at it.
    expect(freeOverlayTrack(two, { clipId: 'clip-2', offset: 1 }, 0.5)).toBe(two.overlays[1]?.id);
  });
});
