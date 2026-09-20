import { fireEvent, render, screen } from '@testing-library/react';
import type {
  DragEndEvent,
  ResizeEndEvent,
  Span,
  TimelineContextProps,
  useTimelineMonitor,
} from 'dnd-timeline';
import { describe, expect, it, vi } from 'vitest';
import { trimClip } from '../commands';
import type { VideoProject } from '../project';
import { Timeline } from '../Timeline';
import {
  insertIndexFor,
  rulerMarks,
  rulerStep,
  sourceEndOf,
  trimArgsFromSpan,
  zoomedRange,
} from '../timelineView';

// jsdom lays nothing out, so a real pointer drag through dnd-kit would measure zeros and assert
// jsdom's geometry rather than this component's. The library is kept real and only its two
// completion callbacks are intercepted, which is the seam a gesture actually arrives through: the
// tests below hand them the span dnd-timeline would have computed and read back the thunk.
const gestures = vi.hoisted(() => ({
  drag: undefined as ((event: DragEndEvent) => void) | undefined,
  resize: undefined as ((event: ResizeEndEvent) => void) | undefined,
}));

vi.mock('dnd-timeline', async (importOriginal) => {
  const actual = await importOriginal<typeof import('dnd-timeline')>();
  return {
    ...actual,
    TimelineContext: (props: TimelineContextProps) => {
      gestures.resize = props.onResizeEnd;
      return <actual.TimelineContext {...props} />;
    },
    useTimelineMonitor: (args: Parameters<typeof useTimelineMonitor>[0]) => {
      gestures.drag = args.onDragEnd;
      actual.useTimelineMonitor(args);
    },
  };
});

// Task 8 writes the sentences. Asserting on keys keeps these tests about what a gesture emits
// instead of about copy that does not exist yet, and a literal that slipped into the component
// would fail here rather than ship untranslated.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      values === undefined ? key : `${key} ${Object.values(values).join(' ')}`,
  }),
}));

function project(): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 25,
    sources: [
      {
        id: 'src-a',
        assetId: 'a',
        kind: 'video',
        duration: 20,
        size: { width: 1920, height: 1080 },
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
      { id: 'clip-3', sourceId: 'src-a', duration: 4, sourceStart: 8, muted: false },
    ],
    overlays: [
      {
        id: 'lane-1',
        items: [
          {
            id: 'title',
            kind: 'text',
            anchor: { clipId: 'clip-2', offset: 1 },
            duration: 2,
            props: { text: 'hi' },
          },
        ],
      },
    ],
  };
}

/**
 * The completion event dnd-timeline hands back: the item, and the span its own strategy computed
 * for the release. Nothing else of the event is read, so nothing else is built — a fake with a
 * pixel delta in it would only invite the adapter to go back to doing the arithmetic itself.
 */
function completed(id: string, span: Span | null) {
  const strategy = () => span;
  return {
    active: {
      id,
      data: {
        current: { span, getSpanFromDragEvent: strategy, getSpanFromResizeEvent: strategy },
      },
    },
  } as unknown as DragEndEvent & ResizeEndEvent;
}

/** One clip that plays its source to the last frame: the end handle has nowhere left to go. */
function lastFrames(): VideoProject {
  return {
    ...project(),
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 16, muted: false }],
    overlays: [],
  };
}

/** A still already longer than the hour the handle would otherwise offer. */
function twoHourStill(): VideoProject {
  return {
    ...project(),
    sources: [
      {
        id: 'img',
        assetId: 'a',
        kind: 'image',
        duration: 0,
        size: { width: 800, height: 600 },
      },
    ],
    video: [{ id: 'clip-1', sourceId: 'img', duration: 7200, sourceStart: 0, muted: false }],
    overlays: [],
  };
}

/** A fixture index that must exist; a miss is a broken fixture, not a case to handle. */
function never(): never {
  throw new Error('fixture has no clip at that index');
}

function mount({
  playhead = 0,
  selectedId,
  base = project(),
}: { playhead?: number; selectedId?: string; base?: VideoProject } = {}) {
  const onCommand = vi.fn();
  const onSelect = vi.fn();
  const onScrub = vi.fn();
  render(
    <Timeline
      project={base}
      selectedId={selectedId}
      playhead={playhead}
      onCommand={onCommand}
      onSelect={onSelect}
      onScrub={onScrub}
    />,
  );
  return { base, onCommand, onSelect, onScrub };
}

/** The thunk the gesture emitted, applied to the project it was emitted against. */
function applied(onCommand: ReturnType<typeof vi.fn>, base: VideoProject): VideoProject {
  expect(onCommand).toHaveBeenCalledTimes(1);
  const thunk = onCommand.mock.calls[0]?.[0] as (current: VideoProject) => VideoProject;
  return thunk(base);
}

function marks(): number[] {
  return screen
    .getAllByTestId('timeline-mark')
    .map((mark) => Number(mark.getAttribute('data-time')));
}

describe('insertIndexFor — a drop is a place in the sequence', () => {
  it('counts the clips whose middle the drop has passed', () => {
    // Without clip-1 the lane is clip-2 (0-4, middle 2) then clip-3 (4-8, middle 6).
    expect(insertIndexFor(project(), 'clip-1', 0)).toBe(0);
    expect(insertIndexFor(project(), 'clip-1', 1.9)).toBe(0);
    expect(insertIndexFor(project(), 'clip-1', 3)).toBe(1);
    expect(insertIndexFor(project(), 'clip-1', 7)).toBe(2);
    expect(insertIndexFor(project(), 'clip-1', 100)).toBe(2);
  });

  it('measures against the lane the dragged clip has left, not the one on screen', () => {
    // Without clip-3 the lane is clip-1 (0-4) then clip-2 (4-8): a drop at 5 lands between them.
    expect(insertIndexFor(project(), 'clip-3', 5)).toBe(1);
    expect(insertIndexFor(project(), 'clip-3', 0)).toBe(0);
  });
});

describe('trimArgsFromSpan — the clip is its own frame of reference', () => {
  it('measures the span from where the clip starts, never from the source zero', () => {
    // clip-2 starts at 4 in project time and at 4 in its source. A span of 5..8 takes one second
    // off its head; handing over 5 as the start would take five.
    expect(trimArgsFromSpan(4, { start: 5, end: 8 })).toEqual({ start: 1, end: 4 });
  });

  it('round-trips through trimClip without double-counting sourceStart', () => {
    const next = trimClip(project(), {
      clipId: 'clip-2',
      ...trimArgsFromSpan(4, { start: 5, end: 8 }),
    });
    expect(next.video[1]?.sourceStart).toBe(5);
    expect(next.video[1]?.duration).toBe(3);
  });

  it('hands material back when the span grows past the clip', () => {
    expect(trimArgsFromSpan(4, { start: 2, end: 8 })).toEqual({ start: -2, end: 4 });
    const next = trimClip(project(), {
      clipId: 'clip-2',
      ...trimArgsFromSpan(4, { start: 2, end: 8 }),
    });
    expect(next.video[1]?.sourceStart).toBe(2);
    expect(next.video[1]?.duration).toBe(6);
  });
});

describe('zoom', () => {
  it('halves and doubles around the middle of what is on screen', () => {
    expect(zoomedRange({ start: 0, end: 12 }, 0.5, 12)).toEqual({ start: 3, end: 9 });
    expect(zoomedRange({ start: 3, end: 9 }, 2, 12)).toEqual({ start: 0, end: 12 });
  });

  it('never shows less than a second, and never more than the project', () => {
    expect(zoomedRange({ start: 5.5, end: 6.5 }, 0.5, 12)).toEqual({ start: 5.5, end: 6.5 });
    expect(zoomedRange({ start: 0, end: 12 }, 2, 12)).toEqual({ start: 0, end: 12 });
  });

  it('picks the step that keeps the ruler readable', () => {
    expect(rulerStep(12)).toBe(2);
    expect(rulerStep(6)).toBe(1);
    expect(rulerStep(1)).toBe(0.5);
    expect(rulerStep(3600)).toBe(600);
  });

  it('puts the marks on the step, inside the range', () => {
    expect(rulerMarks({ start: 0, end: 12 }, 2)).toEqual([0, 2, 4, 6, 8, 10, 12]);
    expect(rulerMarks({ start: 5.5, end: 6.5 }, 0.5)).toEqual([5.5, 6, 6.5]);
  });
});

describe('Timeline lanes', () => {
  it('draws the video lane and one row per overlay track', () => {
    mount();
    expect(screen.getByText('videoStudio.timeline.videoLane')).toBeTruthy();
    expect(screen.getByText('videoStudio.timeline.overlayLane 1')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /videoStudio\.timeline\.clip/ })).toHaveLength(3);
    expect(screen.getByRole('button', { name: 'videoStudio.timeline.overlayText 1' })).toBeTruthy();
  });

  it('selects on the PRESS, not only on the click', () => {
    // Measured in Chrome, 2026-09-20: a bare click on a clip selected nothing. dnd-kit's
    // PointerSensor has no activation distance here, so `handleStart` runs on pointerdown and
    // adds a capturing document `click` listener that calls `stopPropagation`
    // (@dnd-kit/core core.esm.js:1504) — the button's own onClick never fires. jsdom cannot
    // reproduce that (it lays nothing out, so the sensor never engages), so this test holds the
    // contract the browser needs instead of the failure: a press selects.
    const { onSelect } = mount();
    fireEvent.pointerDown(screen.getByRole('button', { name: 'videoStudio.timeline.clip 2' }));
    expect(onSelect).toHaveBeenCalledWith('clip-2');
    fireEvent.pointerDown(
      screen.getByRole('button', { name: 'videoStudio.timeline.overlayText 1' }),
    );
    expect(onSelect).toHaveBeenLastCalledWith('title');
  });

  it('tells the shell what was clicked, and asks for no command', () => {
    const { onSelect, onCommand } = mount();
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.timeline.clip 2' }));
    expect(onSelect).toHaveBeenCalledWith('clip-2');
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.timeline.overlayText 1' }));
    expect(onSelect).toHaveBeenLastCalledWith('title');
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('marks the selected clip and keeps the others plain', () => {
    mount({ selectedId: 'clip-2' });
    const clips = screen.getAllByRole('button', { name: /videoStudio\.timeline\.clip/ });
    expect(clips.map((clip) => clip.getAttribute('aria-current'))).toEqual([null, 'true', null]);
  });

  it('never lets two trim handles claim the same band at a clip boundary', () => {
    mount();
    // A handle that straddles its clip's edge reaches 22 px into the neighbour, and where two
    // clips abut both bands coincide exactly — the one painted later takes all 44 px and the
    // other clip's handle cannot be pressed at all. At an INTERIOR edge each handle stays inside
    // the clip that owns it, which is also the side of the boundary dnd-timeline reads the press
    // against. An edge with no neighbour still straddles: there is nothing there to take.
    const handle = (name: string) => screen.getByRole('slider', { name });
    const straddles = (element: HTMLElement) => element.style.transform !== '';

    expect(straddles(handle('videoStudio.timeline.trimEnd 1'))).toBe(false);
    expect(straddles(handle('videoStudio.timeline.trimStart 2'))).toBe(false);
    expect(straddles(handle('videoStudio.timeline.trimEnd 2'))).toBe(false);
    expect(straddles(handle('videoStudio.timeline.trimStart 3'))).toBe(false);
    // The lane's own two ends, where a handle has the whole 44 px to itself.
    expect(straddles(handle('videoStudio.timeline.trimStart 1'))).toBe(true);
    expect(straddles(handle('videoStudio.timeline.trimEnd 3'))).toBe(true);
  });

  it('gives every item and handle the 44 px floor', () => {
    mount();
    const clip = screen.getByRole('button', { name: 'videoStudio.timeline.clip 1' });
    expect(clip.style.minHeight).toBe('44px');
    expect(clip.hasAttribute('data-required-touch-target')).toBe(true);
    const handle = screen.getByRole('slider', { name: 'videoStudio.timeline.trimStart 1' });
    expect(handle.style.minWidth).toBe('44px');
    expect(handle.hasAttribute('data-required-touch-target')).toBe(true);
    // An interior handle cannot have it, and does not claim it: two clips share one boundary, so
    // each owns half of it. The keyboard and the inspector reach both ends whatever the width.
    const interior = screen.getByRole('slider', { name: 'videoStudio.timeline.trimStart 2' });
    expect(interior.style.minWidth).toBe('22px');
    expect(interior.hasAttribute('data-required-touch-target')).toBe(false);
  });
});

describe('moving a clip', () => {
  it('reads the span the library computed and inserts at the place it names', () => {
    const { base, onCommand } = mount();
    // clip-1 released with its head at 7: past the middle of both clips it left behind.
    gestures.drag?.(completed('clip-1', { start: 7, end: 11 }));
    expect(applied(onCommand, base).video.map((clip) => clip.id)).toEqual([
      'clip-2',
      'clip-3',
      'clip-1',
    ]);
  });

  it('inserts before a clip when the drop stops short of its middle', () => {
    const { base, onCommand } = mount();
    gestures.drag?.(completed('clip-3', { start: 3, end: 7 }));
    expect(applied(onCommand, base).video.map((clip) => clip.id)).toEqual([
      'clip-1',
      'clip-3',
      'clip-2',
    ]);
  });

  it('asks for nothing when the strategy has no span for the release', () => {
    const { onCommand } = mount();
    gestures.drag?.(completed('clip-1', null));
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('emits moveClip for the place Alt+Arrow asks for', () => {
    const { base, onCommand } = mount();
    fireEvent.keyDown(screen.getByRole('button', { name: 'videoStudio.timeline.clip 1' }), {
      key: 'ArrowRight',
      altKey: true,
    });
    expect(applied(onCommand, base).video.map((clip) => clip.id)).toEqual([
      'clip-2',
      'clip-1',
      'clip-3',
    ]);
    expect(base.video.map((clip) => clip.id)).toEqual(['clip-1', 'clip-2', 'clip-3']);
  });

  it('leaves a plain arrow to navigation, and says which keys do move a clip', () => {
    const { onCommand } = mount();
    const clip = screen.getByRole('button', { name: 'videoStudio.timeline.clip 1' });
    fireEvent.keyDown(clip, { key: 'ArrowRight' });
    fireEvent.keyDown(clip, { key: 'ArrowLeft' });
    expect(onCommand).not.toHaveBeenCalled();
    expect(clip.getAttribute('aria-keyshortcuts')).toBe('Alt+ArrowLeft Alt+ArrowRight');
  });

  it('asks for nothing when the clip is already at the end it is pushed against', () => {
    const { onCommand } = mount();
    fireEvent.keyDown(screen.getByRole('button', { name: 'videoStudio.timeline.clip 1' }), {
      key: 'ArrowLeft',
      altKey: true,
    });
    expect(onCommand).not.toHaveBeenCalled();
  });
});

describe('trimming a clip', () => {
  it('reads the released span as a trim of the clip it belongs to', () => {
    const { base, onCommand } = mount();
    // clip-2 starts at 4 in project time; a handle left at 5 takes a second off its head.
    gestures.resize?.(completed('clip-2', { start: 5, end: 8 }));
    const next = applied(onCommand, base);
    expect(next.video[1]?.sourceStart).toBe(5);
    expect(next.video[1]?.duration).toBe(3);
  });

  it('asks for nothing for a release on something that is not a clip', () => {
    const { onCommand } = mount();
    gestures.resize?.(completed('title', { start: 5, end: 8 }));
    gestures.resize?.(completed('clip-2', null));
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('moves the start handle a frame and measures it from the clip, not the source', () => {
    const { base, onCommand } = mount();
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.timeline.trimStart 2' }), {
      key: 'ArrowRight',
    });
    const next = applied(onCommand, base);
    expect(next.video[1]?.sourceStart).toBeCloseTo(4.04, 6);
    expect(next.video[1]?.duration).toBeCloseTo(3.96, 6);
  });

  it('moves a whole second with shift, and hands material back going the other way', () => {
    const { base, onCommand } = mount();
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.timeline.trimStart 2' }), {
      key: 'ArrowLeft',
      shiftKey: true,
    });
    const next = applied(onCommand, base);
    expect(next.video[1]?.sourceStart).toBeCloseTo(3, 6);
    expect(next.video[1]?.duration).toBeCloseTo(5, 6);
  });

  it('moves the end handle without touching where the clip starts', () => {
    const { base, onCommand } = mount();
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.timeline.trimEnd 2' }), {
      key: 'ArrowLeft',
    });
    const next = applied(onCommand, base);
    expect(next.video[1]?.sourceStart).toBe(4);
    expect(next.video[1]?.duration).toBeCloseTo(3.96, 6);
  });

  it('asks for nothing at the source start rather than a trim the command refuses', () => {
    const { onCommand } = mount();
    // clip-1 plays its source from zero: there is nothing before it to hand back.
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.timeline.trimStart 1' }), {
      key: 'ArrowLeft',
      shiftKey: true,
    });
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('asks for nothing at the source end either', () => {
    const { onCommand } = mount({ base: lastFrames() });
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.timeline.trimEnd 1' }), {
      key: 'ArrowRight',
    });
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('says where each handle sits in the source it plays', () => {
    mount();
    const start = screen.getByRole('slider', { name: 'videoStudio.timeline.trimStart 2' });
    const end = screen.getByRole('slider', { name: 'videoStudio.timeline.trimEnd 2' });
    expect(start.getAttribute('aria-valuenow')).toBe('4');
    expect(end.getAttribute('aria-valuenow')).toBe('8');
    expect(end.getAttribute('aria-valuemax')).toBe('20');
  });

  it('never announces a maximum below the end the clip already has', () => {
    // An image is measured against no source, so its handle keeps offering more — but a still
    // already running two hours reports two hours, not the hour the handle would have offered.
    const still = twoHourStill();
    expect(sourceEndOf(still, still.video[0] ?? never())).toBe(7200);
    const clip = project();
    expect(sourceEndOf(clip, clip.video[0] ?? never())).toBe(20);
  });

  it('writes nothing while the pointer is still down', () => {
    const { onCommand } = mount();
    const handle = screen.getByRole('slider', { name: 'videoStudio.timeline.trimStart 2' });
    fireEvent.pointerDown(handle, { clientX: 10 });
    fireEvent.pointerMove(window, { clientX: 60 });
    expect(onCommand).not.toHaveBeenCalled();
  });
});

describe('the playhead', () => {
  it('sits where the time says', () => {
    mount({ playhead: 3 });
    expect(screen.getByTestId('timeline-playhead').style.left).toBe('25%');
    expect(
      screen
        .getByRole('slider', { name: 'videoStudio.timeline.playhead' })
        .getAttribute('aria-valuenow'),
    ).toBe('3');
  });

  it('scrubs a frame with an arrow and a second with shift, and commands nothing', () => {
    const { onScrub, onCommand } = mount({ playhead: 3 });
    const slider = screen.getByRole('slider', { name: 'videoStudio.timeline.playhead' });
    fireEvent.keyDown(slider, { key: 'ArrowRight' });
    expect(onScrub).toHaveBeenLastCalledWith(3.04);
    fireEvent.keyDown(slider, { key: 'ArrowLeft', shiftKey: true });
    expect(onScrub).toHaveBeenLastCalledWith(2);
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('follows the pointer across the ruler without ever passing the ends', () => {
    const { onScrub } = mount();
    const slider = screen.getByRole('slider', { name: 'videoStudio.timeline.playhead' });
    // jsdom lays nothing out; the ruler is given the box a browser would measure.
    Object.defineProperty(slider, 'getBoundingClientRect', {
      value: () => ({ left: 100, width: 600, top: 0, right: 700, bottom: 0, height: 0 }),
    });
    slider.setPointerCapture = () => undefined;
    slider.hasPointerCapture = () => true;
    fireEvent.pointerDown(slider, { clientX: 400, pointerId: 1 });
    expect(onScrub).toHaveBeenLastCalledWith(6);
    fireEvent.pointerMove(slider, { clientX: 1000, pointerId: 1 });
    expect(onScrub).toHaveBeenLastCalledWith(12);
  });
});

describe('the zoom buttons', () => {
  it('changes the ruler step and keeps a second on screen at the floor', () => {
    mount({ playhead: 6 });
    expect(marks()).toEqual([0, 2, 4, 6, 8, 10, 12]);
    const zoomIn = screen.getByRole('button', { name: 'videoStudio.timeline.zoomIn' });
    fireEvent.click(zoomIn);
    expect(marks()).toEqual([3, 4, 5, 6, 7, 8, 9]);
    for (let press = 0; press < 4; press += 1) fireEvent.click(zoomIn);
    expect(marks()).toEqual([5.5, 6, 6.5]);
  });

  it('comes back out to the whole project and no further', () => {
    mount({ playhead: 6 });
    const zoomIn = screen.getByRole('button', { name: 'videoStudio.timeline.zoomIn' });
    fireEvent.click(zoomIn);
    const zoomOut = screen.getByRole('button', { name: 'videoStudio.timeline.zoomOut' });
    fireEvent.click(zoomOut);
    expect(marks()).toEqual([0, 2, 4, 6, 8, 10, 12]);
    fireEvent.click(zoomOut);
    expect(marks()).toEqual([0, 2, 4, 6, 8, 10, 12]);
  });
});
