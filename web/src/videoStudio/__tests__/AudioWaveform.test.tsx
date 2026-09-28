import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createRef, useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AudioWaveform, type WaveformHandle } from '../AudioWaveform';
import { setEnvelope } from '../commands_audio';
import { createHistory, type History } from '../history';
import type { EnvelopePoint, VideoProject } from '../project';

// The waveform with wavesurfer stood in for: a fake that records what it was created with, and a
// fake Envelope plugin that reports its points the way the installed one does
// (wavesurfer.js 8.0.1, dist/plugins/envelope.esm.js): addPoint, a drag and setPoints alike end in
// ONE `points-change`, 200 ms after the last change, and destroying the waveform cancels it. The
// rules under test are S2's.

interface FakePoint {
  readonly time: number;
  readonly volume: number;
}

const fakes = vi.hoisted(() => {
  /** The plugin's debounce before it reports its points. */
  const reportDelay = 200;
  class FakeEnvelope {
    points: FakePoint[];
    setCalls = 0;
    added: FakePoint[] = [];
    private listener: ((points: FakePoint[]) => void) | undefined;
    private pending: ReturnType<typeof setTimeout> | undefined;
    constructor(points: FakePoint[]) {
      this.points = points;
    }
    private changed() {
      clearTimeout(this.pending);
      this.pending = setTimeout(() => {
        this.listener?.(this.points);
      }, reportDelay);
    }
    addPoint(point: FakePoint) {
      this.added.push(point);
      this.points = [...this.points, point].sort((a, b) => a.time - b.time);
      this.changed();
    }
    on(_event: string, listener: (points: FakePoint[]) => void) {
      this.listener = listener;
      return () => undefined;
    }
    getPoints() {
      return this.points;
    }
    setPoints(points: FakePoint[]) {
      this.points = points;
      this.setCalls += 1;
      this.changed();
    }
    /** The operator drags its points there: shown at once, reported after the debounce. */
    drag(points: FakePoint[]) {
      this.points = points;
      this.changed();
    }
    emit(points: FakePoint[]) {
      this.listener?.(points);
    }
    destroy() {
      clearTimeout(this.pending);
    }
  }
  return {
    FakeEnvelope,
    reportDelay,
    created: [] as { options: Record<string, unknown>; destroyed: boolean }[],
    envelopes: [] as InstanceType<typeof FakeEnvelope>[],
    width: 300,
  };
});

vi.mock('wavesurfer.js', () => ({
  default: {
    create: (options: Record<string, unknown>) => {
      const instance = {
        options,
        destroyed: false,
        // As WaveSurfer.destroy does, it takes its plugins down with it.
        destroy: () => {
          instance.destroyed = true;
          for (const plugin of options.plugins as { destroy: () => void }[]) plugin.destroy();
        },
      };
      fakes.created.push(instance);
      return instance;
    },
  },
}));

vi.mock('wavesurfer.js/plugins/envelope', () => ({
  default: {
    create: (options: { points: FakePoint[] }) => {
      const plugin = new fakes.FakeEnvelope(options.points);
      fakes.envelopes.push(plugin);
      return plugin;
    },
  },
}));

const decode = vi.hoisted(() => ({ fail: false }));
vi.mock('../audioDecode', () => ({
  WAVEFORM_RATE: 100,
  waveformSamples: () =>
    decode.fail
      ? Promise.reject(new Error('undecodable'))
      : Promise.resolve(new Float32Array(1000).fill(0.5)),
}));

beforeEach(() => {
  fakes.created.length = 0;
  fakes.envelopes.length = 0;
  fakes.width = 300;
  decode.fail = false;
  vi.stubGlobal(
    'ResizeObserver',
    class {
      private readonly callback: ResizeObserverCallback;
      constructor(callback: ResizeObserverCallback) {
        this.callback = callback;
      }
      observe() {
        this.callback(
          [{ contentRect: { width: fakes.width } } as ResizeObserverEntry],
          this as unknown as ResizeObserver,
        );
      }
      disconnect() {
        // Nothing to release.
      }
    },
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

type Props = Parameters<typeof AudioWaveform>[0];

/** The plugin's report of whatever it was last told has had time to arrive. */
async function afterTheReport() {
  await act(() => new Promise((resolve) => setTimeout(resolve, fakes.reportDelay + 50)));
}

/** A film whose one sound, 4 s long, carries `envelope`. */
function filmWithEnvelope(envelope: readonly EnvelopePoint[]): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 30,
    sources: [
      { id: 'src-a', assetId: 'a', kind: 'video', duration: 10, size: { width: 8, height: 6 } },
      { id: 'src-m', assetId: 'm', kind: 'audio', duration: 8, size: { width: 0, height: 0 } },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    overlays: [],
    audio: [
      {
        id: 'lane-a',
        items: [
          {
            id: 'bed',
            sourceId: 'src-m',
            anchor: { clipId: 'clip-1', offset: 0 },
            sourceStart: 0,
            duration: 4,
            volume: 1,
            muted: false,
            envelope,
          },
        ],
      },
    ],
  };
}

/** The waveform as the editor drives it: every reported envelope is an edit on `history`. */
function EditedThroughHistory({ history }: { readonly history: History }) {
  const [film, setFilm] = useState(history.current);
  return (
    <>
      <button
        type="button"
        onClick={() => {
          setFilm(history.undo());
        }}
      >
        undo
      </button>
      <AudioWaveform
        assetId="m"
        sourceStart={0}
        visible={4}
        envelope={film.audio?.[0]?.items[0]?.envelope}
        selected
        onEnvelope={(points) => {
          setFilm(history.apply((project) => setEnvelope(project, { itemId: 'bed', points })));
        }}
      />
    </>
  );
}

function mount(props: Partial<Props> = {}) {
  const onEnvelope = vi.fn();
  const all: Props = {
    assetId: 'm',
    sourceStart: 1,
    visible: 4,
    envelope: undefined,
    selected: false,
    onEnvelope,
    ...props,
  };
  const view = render(<AudioWaveform {...all} />);
  return {
    ...view,
    onEnvelope,
    rerenderWith: (next: Partial<Props>) => {
      view.rerender(<AudioWaveform {...all} {...next} />);
    },
  };
}

describe('AudioWaveform', () => {
  it('draws nothing until its host has a width', async () => {
    fakes.width = 0;
    mount();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(fakes.created).toHaveLength(0);
  });

  it('draws the peaks of the stretch the lane shows, and no envelope when not selected', async () => {
    mount();
    await waitFor(() => {
      expect(fakes.created).toHaveLength(1);
    });
    const options = fakes.created[0]?.options;
    expect(options?.duration).toBe(4);
    const peaks = options?.peaks as Float32Array[] | undefined;
    expect(peaks?.[0]).toHaveLength(400);
    expect(options?.plugins).toEqual([]);
  });

  it('shows the envelope with both edges while selected, and commits what the plugin reports', async () => {
    const envelope: EnvelopePoint[] = [{ time: 2, gain: 0.5 }];
    const view = mount({ selected: true, envelope });
    await waitFor(() => {
      expect(fakes.envelopes).toHaveLength(1);
    });
    const plugin = fakes.envelopes[0];
    expect(plugin?.points.map((point) => point.time)).toEqual([0, 2, 4]);
    plugin?.emit([
      { time: 0, volume: 1 },
      { time: 2, volume: 0 },
      { time: 4, volume: 1 },
    ]);
    expect(view.onEnvelope).toHaveBeenCalledWith([
      { time: 0, gain: 1 },
      { time: 2, gain: 0 },
      { time: 4, gain: 1 },
    ]);
  });

  it('leaves its own drag alone when it echoes back, and redraws an envelope changed elsewhere', async () => {
    const view = mount({ selected: true, envelope: [{ time: 2, gain: 0.5 }] });
    await waitFor(() => {
      expect(fakes.envelopes).toHaveLength(1);
    });
    const plugin = fakes.envelopes[0];
    // The operator pulls the middle point down: the plugin already shows it...
    plugin?.setPoints([
      { time: 0, volume: 0.5 },
      { time: 2, volume: 0 },
      { time: 4, volume: 0.5 },
    ]);
    const before = plugin?.setCalls ?? 0;
    // ...and the edit it reported comes back as the new envelope: nothing to redraw under the pointer.
    view.rerenderWith({
      envelope: [
        { time: 0, gain: 0.5 },
        { time: 2, gain: 0 },
        { time: 4, gain: 0.5 },
      ],
    });
    expect(plugin?.setCalls).toBe(before);
    // An undo brings the old envelope back: redrawn.
    view.rerenderWith({ envelope: [{ time: 2, gain: 0.5 }] });
    expect(plugin?.setCalls).toBe(before + 1);
    expect(plugin?.points.map((point) => point.volume)).toEqual([0.5, 0.5, 0.5]);
    // And no envelope at all: flat at full gain.
    view.rerenderWith({ envelope: undefined });
    expect(plugin?.points).toEqual([
      { time: 0, volume: 1 },
      { time: 4, volume: 1 },
    ]);
  });

  it('commits nothing when an undo redraws the envelope, so the redo survives', async () => {
    // A trimmed sound keeps its inner point but no edges: the view the plugin draws adds them.
    const trimmed: EnvelopePoint[] = [{ time: 1, gain: 0.5 }];
    const history = createHistory(filmWithEnvelope(trimmed));
    render(<EditedThroughHistory history={history} />);
    await waitFor(() => {
      expect(fakes.envelopes).toHaveLength(1);
    });
    const plugin = fakes.envelopes[0];
    plugin?.drag([
      { time: 0, volume: 0.5 },
      { time: 1, volume: 0 },
      { time: 4, volume: 0.5 },
    ]);
    await afterTheReport();
    expect(history.canUndo).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: 'undo' }));
    await afterTheReport();

    expect(history.current.audio?.[0]?.items[0]?.envelope).toEqual(trimmed);
    expect(history.canRedo).toBe(true);
  });

  it('keeps a press on an envelope point from starting the item drag, and lets any other through', () => {
    // The guard is on the host itself: no waveform needs drawing to judge it.
    fakes.width = 0;
    const outer = vi.fn();
    const { getByTestId } = render(
      <div onPointerDown={outer}>
        <AudioWaveform
          assetId="m"
          sourceStart={0}
          visible={4}
          envelope={undefined}
          selected
          onEnvelope={vi.fn()}
        />
      </div>,
    );
    const host = getByTestId('sound-waveform');
    const point = document.createElementNS('http://www.w3.org/2000/svg', 'ellipse');
    host.appendChild(point);
    fireEvent.pointerDown(point);
    expect(outer).not.toHaveBeenCalled();
    fireEvent.pointerDown(host);
    expect(outer).toHaveBeenCalledOnce();
  });

  it('adds an envelope point where a double-click on its sound landed, at that height', async () => {
    const handle = createRef<WaveformHandle>();
    const view = mount({ selected: true, ref: handle });
    await waitFor(() => {
      expect(fakes.envelopes).toHaveLength(1);
    });
    vi.spyOn(view.getByTestId('sound-waveform'), 'getBoundingClientRect').mockReturnValue(
      DOMRect.fromRect({ x: 100, y: 20, width: 400, height: 40 }),
    );
    // Three quarters across the 4 s the lane shows, a quarter of the way down.
    handle.current?.addPointAt(400, 30);
    expect(fakes.envelopes[0]?.added).toEqual([{ time: 3, volume: 0.75 }]);
  });

  it('adds no point while it shows no envelope', async () => {
    const handle = createRef<WaveformHandle>();
    mount({ ref: handle });
    await waitFor(() => {
      expect(fakes.created).toHaveLength(1);
    });
    handle.current?.addPointAt(400, 30);
    expect(fakes.envelopes).toHaveLength(0);
  });

  it('draws no waveform for a source the browser cannot decode, and says nothing', async () => {
    decode.fail = true;
    const { container } = mount();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(fakes.created).toHaveLength(0);
    expect(container.querySelector('[role="alert"]')).toBeNull();
  });

  it('tears the waveform down when it goes', async () => {
    const view = mount();
    await waitFor(() => {
      expect(fakes.created).toHaveLength(1);
    });
    view.unmount();
    expect(fakes.created[0]?.destroyed).toBe(true);
  });
});
