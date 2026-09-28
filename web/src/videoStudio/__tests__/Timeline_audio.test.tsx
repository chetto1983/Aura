import { fireEvent, render, screen } from '@testing-library/react';
import type {
  DragEndEvent,
  ResizeEndEvent,
  Span,
  TimelineContextProps,
  useTimelineMonitor,
} from 'dnd-timeline';
import { describe, expect, it, vi } from 'vitest';
import type { AudioItem, VideoProject } from '../project';
import { Timeline } from '../Timeline';

// The audio lanes, through the same seam Timeline.test.tsx uses: the library is real and only its
// two completion callbacks are intercepted, handed the span dnd-timeline would have computed.
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

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      values === undefined ? key : `${key} ${Object.values(values).join(' ')}`,
  }),
}));

// The waveform is AudioWaveform.test.tsx's to judge; here only what the lane hands it.
const waves = vi.hoisted(() => ({ props: [] as Record<string, unknown>[] }));
vi.mock('../AudioWaveform', () => ({
  AudioWaveform: (props: Record<string, unknown>) => {
    waves.props.push(props);
    return <div data-testid="sound-waveform" />;
  },
}));

function sound(id: string, offset: number, over: Partial<AudioItem> = {}): AudioItem {
  return {
    id,
    sourceId: 'src-m',
    anchor: { clipId: 'clip-1', offset },
    sourceStart: 1,
    duration: 2,
    volume: 1,
    muted: false,
    ...over,
  };
}

function project(): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 25,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 20, size: { width: 320, height: 180 } },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 6, size: { width: 0, height: 0 } },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    overlays: [],
    audio: [
      { id: 'lane-a', items: [sound('bed', 0, { label: 'bed.wav' }), sound('tail', 2)] },
      { id: 'lane-b', items: [sound('voice', 4)] },
    ],
  };
}

function released(id: string, span: Span, extra: Record<string, unknown> = {}) {
  const strategy = () => span;
  return {
    active: {
      id,
      data: {
        current: { span, getSpanFromDragEvent: strategy, getSpanFromResizeEvent: strategy },
      },
    },
    ...extra,
  } as unknown as DragEndEvent & ResizeEndEvent;
}

function mount(selectedId?: string) {
  const onCommand = vi.fn();
  const onSelect = vi.fn();
  render(
    <Timeline
      project={project()}
      selectedId={selectedId}
      playhead={0}
      onCommand={onCommand}
      onSelect={onSelect}
      onScrub={vi.fn()}
    />,
  );
  const applied = () => {
    expect(onCommand).toHaveBeenCalledTimes(1);
    const edit = onCommand.mock.calls[0]?.[0] as (current: VideoProject) => VideoProject;
    return edit(project());
  };
  return { onSelect, applied };
}

function soundIn(next: VideoProject, id: string): AudioItem | undefined {
  return next.audio?.flatMap((lane) => lane.items).find((item) => item.id === id);
}

describe('Timeline, on the audio lanes', () => {
  it('draws one lane per audio track under the video lane, numbering sounds across lanes', () => {
    mount();
    const lanes = screen.getAllByRole('group').map((lane) => lane.getAttribute('aria-label'));
    expect(lanes.slice(-3)).toEqual([
      'videoStudio.timeline.videoLane',
      'videoStudio.audio.lane 1',
      'videoStudio.audio.lane 2',
    ]);
    expect(screen.getByRole('button', { name: 'videoStudio.audio.item 3' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'videoStudio.audio.item 1' }).textContent).toContain(
      'bed.wav',
    );
  });

  it('selects a sound on press', () => {
    const { onSelect } = mount();
    fireEvent.pointerDown(screen.getByRole('button', { name: 'videoStudio.audio.item 2' }));
    expect(onSelect).toHaveBeenCalledWith('tail');
  });

  it('drops a dragged sound where it is let go, onto the sound lane under it', () => {
    const { applied } = mount();
    gestures.drag?.(released('voice', { start: 6, end: 8 }, { over: { id: 'lane-a' } }));
    const next = applied();
    expect(next.audio?.[0]?.items.map((item) => item.id)).toContain('voice');
    expect(soundIn(next, 'voice')?.anchor).toEqual({ clipId: 'clip-1', offset: 6 });
  });

  it('keeps a sound on its own lane when it is let go over the video lane', () => {
    const { applied } = mount();
    gestures.drag?.(released('voice', { start: 5, end: 7 }, { over: { id: 'video' } }));
    expect(applied().audio?.[1]?.items[0]?.anchor.offset).toBe(5);
  });

  it('trims only the head when the start edge is dragged, keeping the end exact', () => {
    const { applied } = mount();
    gestures.resize?.(released('voice', { start: 4.5, end: 6 }, { direction: 'start' }));
    expect(soundIn(applied(), 'voice')).toMatchObject({ sourceStart: 1.5, duration: 1.5 });
  });

  it('trims only the tail when the end edge is dragged, keeping the start exact', () => {
    const { applied } = mount();
    gestures.resize?.(released('voice', { start: 4, end: 5.5 }, { direction: 'end' }));
    expect(soundIn(applied(), 'voice')).toMatchObject({ sourceStart: 1, duration: 1.5 });
  });

  it('steps a sound handle by a frame from the keyboard', () => {
    const { applied } = mount('voice');
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.audio.trimEnd 3' }), {
      key: 'ArrowLeft',
    });
    expect(soundIn(applied(), 'voice')?.duration).toBeCloseTo(2 - 1 / 25, 9);
  });

  it('draws each sound’s waveform over what the lane shows, and commits its envelope', () => {
    waves.props.length = 0;
    const { applied } = mount('bed');
    const bed = waves.props.findLast((props) => props.selected === true);
    // bed: 2 source seconds from second 1 at speed 1, all of it inside the 8 s film.
    expect(bed).toMatchObject({ assetId: 'm', sourceStart: 1, visible: 2 });
    expect(waves.props.filter((props) => props.selected === false)).not.toHaveLength(0);
    if (bed === undefined) throw new Error('the selected sound drew no waveform');
    (bed.onEnvelope as (points: { time: number; gain: number }[]) => void)([
      { time: 1, gain: 0.25 },
    ]);
    expect(soundIn(applied(), 'bed')?.envelope).toEqual([{ time: 1, gain: 0.25 }]);
  });

  it('steps the start handle too, measured from where the sound starts in its source', () => {
    const { applied } = mount('voice');
    fireEvent.keyDown(screen.getByRole('slider', { name: 'videoStudio.audio.trimStart 3' }), {
      key: 'ArrowRight',
    });
    expect(soundIn(applied(), 'voice')?.sourceStart).toBeCloseTo(1 + 1 / 25, 9);
  });
});
