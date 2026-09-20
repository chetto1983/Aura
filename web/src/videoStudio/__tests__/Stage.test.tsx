import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { VideoJSON } from '@videoflow/core';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { VideoProject } from '../project';
import { Stage } from '../Stage';

// The renderer is the one thing on this surface that is not ours, and jsdom has no media
// pipeline to give it, so it is mocked the way `videoflow.test.ts` mocks the core: a class that
// records what it was told. `loadVideo` also records whether `loadFont` was still the prototype's
// at that moment — `withLocalFonts` replaces it with an OWN property, so that flag is the
// observable form of "no renderer ever loads with the stock, off-origin font loader in place".
interface FakeRenderer {
  readonly loaded: unknown[];
  readonly seeks: number[];
  readonly overriddenAtLoad: boolean[];
  destroyed: number;
}

const dom = vi.hoisted(() => ({ instances: [] as FakeRenderer[] }));

vi.mock('@videoflow/renderer-dom', () => ({
  default: class {
    loadedFonts: Record<string, string> = {};
    loaded: unknown[] = [];
    seeks: number[] = [];
    overriddenAtLoad: boolean[] = [];
    destroyed = 0;
    host: HTMLElement;
    constructor(host: HTMLElement) {
      this.host = host;
      dom.instances.push(this);
    }
    loadFont(): Promise<void> {
      return Promise.resolve();
    }
    loadVideo(json: unknown): Promise<void> {
      this.loaded.push(json);
      this.overriddenAtLoad.push(Object.hasOwn(this, 'loadFont'));
      return Promise.resolve();
    }
    seek(frame: number): Promise<void> {
      this.seeks.push(frame);
      return Promise.resolve();
    }
    destroy(): void {
      this.destroyed += 1;
    }
  },
}));

// `videoflow.ts` imports BrowserRenderer for the export path. The stage never builds one, but the
// package reads a JSON module Node will not take without an import attribute, so the import has
// to be mocked away for this module graph to load at all.
vi.mock('@videoflow/renderer-browser', () => ({
  default: class {
    loadedFonts: Record<string, string> = {};
  },
}));

// Task 8 writes the sentences. Asserting on keys keeps these tests about what the surface emits
// rather than about copy that does not exist yet, and a literal that slipped into the component
// would fail here rather than ship untranslated.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
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
        fps: 25,
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    // Window [1, 3) of the project: clip-1 starts at 0 and the overlay hangs one second in.
    overlays: [
      {
        id: 'lane-1',
        items: [
          {
            id: 'title',
            kind: 'text',
            anchor: { clipId: 'clip-1', offset: 1 },
            duration: 2,
            props: { text: 'AURA', position: [0.5, 0.5] },
          },
        ],
      },
    ],
  };
}

/** jsdom lays nothing out and the suite's setup zeroes every rect, so the stage is given one. */
function measured(element: Element, width: number, height: number): void {
  element.getBoundingClientRect = () => ({
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: width,
    bottom: height,
    width,
    height,
    toJSON: () => ({}),
  });
}

interface Mounted {
  readonly commands: ((current: VideoProject) => VideoProject)[];
  readonly selections: string[];
  readonly rerender: (props: { time?: number; selectedId?: string }) => void;
}

function mount(current: VideoProject, selectedId?: string, time = 2): Mounted {
  const commands: ((project: VideoProject) => VideoProject)[] = [];
  const selections: string[] = [];
  const props = {
    project: current,
    time,
    selectedId,
    onSelect: (id: string) => selections.push(id),
    onCommand: (edit: (project: VideoProject) => VideoProject) => commands.push(edit),
  };
  const view = render(<Stage {...props} />);
  return {
    commands,
    selections,
    rerender: (next) => {
      view.rerender(<Stage {...props} {...next} />);
    },
  };
}

function renderer(): FakeRenderer {
  const instance = dom.instances[0];
  if (instance === undefined) throw new Error('no renderer was constructed');
  return instance;
}

beforeEach(() => {
  dom.instances.length = 0;
  HTMLElement.prototype.setPointerCapture = vi.fn();
});

describe('Stage', () => {
  it('mounts one renderer on the host and loads the compiled project once', async () => {
    mount(project());

    await waitFor(() => {
      expect(renderer().loaded).toHaveLength(1);
    });
    expect(dom.instances).toHaveLength(1);
    const json = renderer().loaded[0] as VideoJSON;
    expect(json.duration).toBe(8);
    // A layer's settings bag is typed `[key: string]: any`, so the whole bag is matched rather
    // than one key read out of it.
    expect(json.layers.map((layer) => layer.settings)).toMatchObject([
      { name: 'clip-1' },
      { name: 'clip-2' },
      { name: 'title' },
    ]);
  });

  it('never loads a renderer that is still on the stock font loader', async () => {
    mount(project());

    await waitFor(() => {
      expect(renderer().overriddenAtLoad).toEqual([true]);
    });
  });

  it('seeks the frame the time prop names, and again when it changes', async () => {
    const view = mount(project(), undefined, 2);

    await waitFor(() => {
      expect(renderer().seeks).toEqual([50]);
    });
    view.rerender({ time: 3.5 });
    await waitFor(() => {
      expect(renderer().seeks).toEqual([50, 88]);
    });
  });

  it('destroys the renderer when it goes', async () => {
    const view = render(
      <Stage
        project={project()}
        time={0}
        selectedId={undefined}
        onSelect={() => undefined}
        onCommand={() => undefined}
      />,
    );
    await waitFor(() => {
      expect(renderer().loaded).toHaveLength(1);
    });

    view.unmount();

    expect(renderer().destroyed).toBe(1);
  });

  it('selects the clip under the playhead when the picture is clicked', () => {
    const view = mount(project(), undefined, 5);

    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.stage.picture' }));

    expect(view.selections).toEqual(['clip-2']);
  });

  it('boxes the selected overlay while it is on screen, and nothing else', () => {
    const view = mount(project(), 'title', 2);
    expect(screen.getByRole('button', { name: 'videoStudio.stage.selection' })).toBeTruthy();

    // A clip has no position of its own — its edits are the timeline's, not the stage's.
    view.rerender({ selectedId: 'clip-1' });
    expect(screen.queryByRole('button', { name: 'videoStudio.stage.selection' })).toBeNull();

    // Past the overlay's window there is nothing on the picture to box.
    view.rerender({ selectedId: 'title', time: 3.5 });
    expect(screen.queryByRole('button', { name: 'videoStudio.stage.selection' })).toBeNull();
  });

  it('keeps a drag in its own state and commits only the release', () => {
    const view = mount(project(), 'title', 2);
    const box = screen.getByRole('button', { name: 'videoStudio.stage.selection' });
    measured(screen.getByTestId('video-stage'), 200, 100);

    fireEvent.pointerDown(box, { pointerId: 1, clientX: 100, clientY: 50 });
    fireEvent.pointerMove(box, { pointerId: 1, clientX: 140, clientY: 60 });

    expect(view.commands).toHaveLength(0);
    expect(box.style.left).toBe('70%');
    expect(box.style.top).toBe('60%');

    fireEvent.pointerUp(box, { pointerId: 1 });

    expect(view.commands).toHaveLength(1);
    const edited = view.commands[0]?.(project());
    expect(edited?.overlays[0]?.items[0]?.props.position).toEqual([0.7, 0.6]);
  });

  it('clamps a drag that leaves the frame to the frame', () => {
    const view = mount(project(), 'title', 2);
    const box = screen.getByRole('button', { name: 'videoStudio.stage.selection' });
    measured(screen.getByTestId('video-stage'), 200, 100);

    fireEvent.pointerDown(box, { pointerId: 1, clientX: 100, clientY: 50 });
    fireEvent.pointerMove(box, { pointerId: 1, clientX: 900, clientY: -900 });
    fireEvent.pointerUp(box, { pointerId: 1 });

    const edited = view.commands[0]?.(project());
    expect(edited?.overlays[0]?.items[0]?.props.position).toEqual([1, 0]);
  });

  // A touch drag the browser takes away — a call arriving, a gesture the OS claims — never sends
  // pointerup. Left alone, the box would keep showing a move nobody made and hand it to whatever
  // release came next.
  it('drops a cancelled drag instead of keeping it on screen', () => {
    const view = mount(project(), 'title', 2);
    const box = screen.getByRole('button', { name: 'videoStudio.stage.selection' });
    measured(screen.getByTestId('video-stage'), 200, 100);

    fireEvent.pointerDown(box, { pointerId: 1, clientX: 100, clientY: 50 });
    fireEvent.pointerMove(box, { pointerId: 1, clientX: 140, clientY: 60 });
    expect(box.style.left).toBe('70%');

    fireEvent.pointerCancel(box, { pointerId: 1 });

    expect(view.commands).toHaveLength(0);
    expect(box.style.left).toBe('50%');

    // Nothing is held any more, so a move with no press behind it moves nothing and the release
    // after it commits nothing.
    fireEvent.pointerMove(box, { pointerId: 1, clientX: 180, clientY: 60 });
    fireEvent.pointerUp(box, { pointerId: 1 });

    expect(view.commands).toHaveLength(0);
    expect(box.style.left).toBe('50%');
  });

  it('lets go when the capture is taken away', () => {
    const view = mount(project(), 'title', 2);
    const box = screen.getByRole('button', { name: 'videoStudio.stage.selection' });
    measured(screen.getByTestId('video-stage'), 200, 100);

    fireEvent.pointerDown(box, { pointerId: 1, clientX: 100, clientY: 50 });
    fireEvent.pointerMove(box, { pointerId: 1, clientX: 140, clientY: 60 });
    fireEvent.lostPointerCapture(box, { pointerId: 1 });

    expect(view.commands).toHaveLength(0);
    expect(box.style.left).toBe('50%');
  });

  it('moves the box with the arrow keys, because a drag no mouse can make is unusable', () => {
    const view = mount(project(), 'title', 2);

    fireEvent.keyDown(screen.getByRole('button', { name: 'videoStudio.stage.selection' }), {
      key: 'ArrowRight',
    });

    const edited = view.commands[0]?.(project());
    expect(edited?.overlays[0]?.items[0]?.props.position).toEqual([0.51, 0.5]);
  });
});
