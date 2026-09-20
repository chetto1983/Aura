import { describe, expect, it } from 'vitest';
import { createHistory } from '../history';
import {
  addClip,
  CommandRefusal,
  removeItem,
  setMuted,
  setProperty,
  splitAt,
  trimClip,
} from '../commands';
import { projectDuration, type VideoProject } from '../project';

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

/** A trim a source cannot cover: the refusal every "nothing happened" test leans on. */
function trimPastSource(current: VideoProject): VideoProject {
  return trimClip(current, { clipId: 'clip-1', start: 0, end: 12 });
}

/**
 * No command drops an overlay property today — `setProperty` only writes one. The history takes
 * any pure edit, so the test writes the one that shrinks an object rather than growing it.
 */
function dropProp(current: VideoProject, itemId: string, key: string): VideoProject {
  return {
    ...current,
    overlays: current.overlays.map((lane) => ({
      ...lane,
      items: lane.items.map((item) =>
        item.id === itemId
          ? {
              ...item,
              props: Object.fromEntries(
                Object.entries(item.props).filter(([name]) => name !== key),
              ),
            }
          : item,
      ),
    })),
  };
}

describe('history', () => {
  it('undoes one command and redoes it', () => {
    const history = createHistory(project());
    history.apply((p) => trimClip(p, { clipId: 'clip-1', start: 0, end: 2 }));
    expect(projectDuration(history.current)).toBe(6);
    expect(projectDuration(history.undo())).toBe(8);
    expect(projectDuration(history.redo())).toBe(6);
  });

  it('keeps a gesture to one step, however many commands it ran', () => {
    const history = createHistory(project());
    history.transaction((p) => {
      let next = p;
      for (let end = 4; end > 2; end -= 0.1)
        next = trimClip(next, { clipId: 'clip-1', start: 0, end });
      return next;
    });
    history.undo();
    expect(projectDuration(history.current)).toBe(8);
  });

  it('drops the redo branch once a new command lands', () => {
    const history = createHistory(project());
    history.apply((p) => setMuted(p, { clipId: 'clip-1', muted: true }));
    history.undo();
    history.apply((p) => setMuted(p, { clipId: 'clip-2', muted: true }));
    expect(history.canRedo).toBe(false);
  });

  it('records one step for a gesture that ran several different commands', () => {
    const start = project();
    const history = createHistory(start);
    history.transaction((p) =>
      setMuted(splitAt(trimClip(p, { clipId: 'clip-1', start: 0, end: 3 }), { time: 1 }), {
        clipId: 'clip-1',
        muted: true,
      }),
    );
    expect(history.current.video).toHaveLength(3);
    history.undo();
    expect(history.current).toEqual(start);
    expect(history.canUndo).toBe(false);
  });

  it('leaves both stacks alone when a command refuses', () => {
    const history = createHistory(project());
    const before = history.current;
    expect(() => history.apply(trimPastSource)).toThrow(CommandRefusal);
    expect(history.current).toBe(before);
    expect(history.canUndo).toBe(false);
    expect(history.canRedo).toBe(false);
  });

  it('keeps the redo branch when a command refuses', () => {
    const history = createHistory(project());
    history.apply((p) => setMuted(p, { clipId: 'clip-1', muted: true }));
    history.undo();
    expect(() => history.apply(trimPastSource)).toThrow(CommandRefusal);
    expect(history.canRedo).toBe(true);
    expect(projectDuration(history.redo())).toBe(8);
    expect(history.current.video[0]?.muted).toBe(true);
  });

  it('records nothing for an edit that changes nothing', () => {
    const start = project();
    const history = createHistory(start);
    history.apply((p) => setMuted(p, { clipId: 'clip-1', muted: false }));
    expect(history.current).toBe(start);
    expect(history.canUndo).toBe(false);
  });

  it('does nothing when there is nothing to undo or redo', () => {
    const start = project();
    const history = createHistory(start);
    expect(history.undo()).toBe(start);
    expect(history.redo()).toBe(start);
    expect(history.canUndo).toBe(false);
    expect(history.canRedo).toBe(false);
  });

  it('shares the structure a step did not change', () => {
    const start = project();
    const history = createHistory(start);
    history.apply((p) => trimClip(p, { clipId: 'clip-1', start: 0, end: 2 }));
    expect(history.current.overlays).toBe(start.overlays);
    expect(history.undo().overlays).toBe(start.overlays);
  });

  it('undoes an added property, a dropped one, an insertion and a removal', () => {
    const start = project();
    const history = createHistory(start);
    history.apply((p) => setProperty(p, { itemId: 'title', key: 'text', value: 'hello' }));
    history.apply((p) => dropProp(p, 'title', 'text'));
    history.apply((p) => addClip(p, { sourceId: 'src-a', duration: 3 }));
    history.apply((p) => removeItem(p, { itemId: 'clip-2' }));
    expect(history.current.video).toHaveLength(2);
    for (let step = 0; step < 4; step += 1) history.undo();
    expect(history.current).toEqual(start);
    expect(history.canUndo).toBe(false);
  });
});
