import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { exportProject, LOCAL_FONTS, toVideoJSON } from '../videoflow';
import type { VideoProject } from '../project';

// The shape we hand VideoFlow is the whole contract of this module, so the builder is mocked and
// the calls are recorded: what the adapter says to `addVideo` / `addText` / `addImage` is what
// spike 107 and 108 measured against, and it is what breaks if the adapter drifts.
const calls = vi.hoisted(() => ({
  videos: [] as { props: Record<string, unknown>; settings: Record<string, unknown> }[],
  texts: [] as { props: Record<string, unknown>; settings: Record<string, unknown> }[],
  images: [] as { props: Record<string, unknown>; settings: Record<string, unknown> }[],
  shapes: [] as { props: Record<string, unknown>; settings: Record<string, unknown> }[],
  waits: [] as unknown[],
  project: [] as unknown[],
  compiled: 0,
}));

vi.mock('@videoflow/core', () => ({
  default: class {
    constructor(settings: unknown) {
      calls.project.push(settings);
    }
    addVideo(props: Record<string, unknown>, settings: Record<string, unknown>) {
      calls.videos.push({ props, settings });
      return { animate: vi.fn() };
    }
    addText(props: Record<string, unknown>, settings: Record<string, unknown>) {
      calls.texts.push({ props, settings });
      return { animate: vi.fn() };
    }
    addImage(props: Record<string, unknown>, settings: Record<string, unknown>) {
      calls.images.push({ props, settings });
      return { animate: vi.fn() };
    }
    addShape(props: Record<string, unknown>, settings: Record<string, unknown>) {
      calls.shapes.push({ props, settings });
      return { animate: vi.fn() };
    }
    wait(time: unknown) {
      calls.waits.push(time);
    }
    compile() {
      calls.compiled += 1;
      return { layers: [], duration: 7 };
    }
  },
}));

interface FakeLayer {
  json: { settings: { source?: string } };
  hasAudio: boolean;
  decodedBuffer: unknown;
}

const renderer = vi.hoisted(() => ({
  instances: [] as {
    json: unknown;
    layers: FakeLayer[];
    loadedFonts: Record<string, string>;
    destroyed: number;
    loadFont: (name: string) => Promise<void>;
  }[],
  layers: [] as FakeLayer[],
  stockFontRequests: [] as string[],
  initCalls: 0,
  exportOptions: [] as { worker?: boolean; signal?: AbortSignal }[],
  // Each test decides how the export behaves: a Blob by default, a hang for the abort case.
  exportImpl: null as null | ((options: { signal?: AbortSignal }) => Promise<Blob>),
}));

vi.mock('@videoflow/renderer-browser', () => ({
  default: class {
    json: unknown;
    layers: FakeLayer[] = [];
    loadedFonts: Record<string, string> = {};
    destroyed = 0;
    constructor(json: unknown) {
      this.json = json;
      renderer.instances.push(this);
    }
    // VideoFlow's own loadFont fetches fonts.googleapis.com — 26 requests, measured in spike 107.
    // Anything that lands here is a request that left the origin.
    loadFont(name: string): Promise<void> {
      renderer.stockFontRequests.push(name);
      return Promise.resolve();
    }
    initLayers(): Promise<void> {
      renderer.initCalls += 1;
      this.layers = renderer.layers;
      return Promise.resolve();
    }
    exportVideo(options: { worker?: boolean; signal?: AbortSignal }): Promise<Blob> {
      renderer.exportOptions.push(options);
      if (renderer.exportImpl) return renderer.exportImpl(options);
      return Promise.resolve(new Blob(['mp4'], { type: 'video/mp4' }));
    }
    destroy() {
      this.destroyed += 1;
    }
  },
}));

const decoded = { calls: [] as ArrayBuffer[] };

function project(): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 30,
    sources: [
      {
        id: 'src-a',
        assetId: 'asset-a',
        kind: 'video',
        duration: 10,
        size: { width: 1920, height: 1080 },
      },
      {
        id: 'src-still',
        assetId: 'asset-still',
        kind: 'image',
        duration: 0,
        size: { width: 1920, height: 1080 },
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 3, sourceStart: 4, muted: true },
    ],
    overlays: [
      {
        id: 'lane-1',
        items: [
          {
            id: 'title',
            kind: 'text',
            anchor: { clipId: 'clip-2', offset: 0.5 },
            duration: 1,
            props: { text: 'AURA', fontFamily: 'Atkinson Hyperlegible Next' },
          },
        ],
      },
    ],
  };
}

const urls = {
  assetUrl: (assetId: string) => `/api/assets/${encodeURIComponent(assetId)}/download`,
};

beforeEach(() => {
  calls.videos.length = 0;
  calls.texts.length = 0;
  calls.images.length = 0;
  calls.shapes.length = 0;
  calls.waits.length = 0;
  calls.project.length = 0;
  calls.compiled = 0;
  renderer.instances.length = 0;
  renderer.layers = [];
  renderer.stockFontRequests.length = 0;
  renderer.initCalls = 0;
  renderer.exportOptions.length = 0;
  renderer.exportImpl = null;
  decoded.calls.length = 0;

  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve({ arrayBuffer: () => Promise.resolve(new ArrayBuffer(8)) })),
  );
  vi.stubGlobal(
    'OfflineAudioContext',
    class {
      decodeAudioData(bytes: ArrayBuffer) {
        decoded.calls.push(bytes);
        return Promise.resolve({ id: decoded.calls.length });
      }
    },
  );
  Object.defineProperty(document, 'fonts', {
    configurable: true,
    value: { load: vi.fn(() => Promise.resolve([])) },
  });
  vi.spyOn(document.head, 'appendChild').mockImplementation(<T extends Node>(node: T): T => {
    Node.prototype.appendChild.call(document.head, node);
    queueMicrotask(() => node.dispatchEvent(new Event('load')));
    return node;
  });
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.head.querySelectorAll('link[data-local-font]').forEach((link) => {
    link.remove();
  });
});

describe('toVideoJSON', () => {
  it('places every clip at the start its lane gives it, with the cut nudge', async () => {
    await toVideoJSON(project(), urls);

    expect(calls.videos).toHaveLength(2);
    expect(calls.videos[0]).toMatchObject({
      settings: { startTime: 0, sourceStart: 0.0001, sourceDuration: 4 },
    });
    expect(calls.videos[1]).toMatchObject({
      settings: { startTime: 4, sourceStart: 4.0001, sourceDuration: 3 },
    });
    expect(calls.compiled).toBe(1);
  });

  it('carries mute per clip, in the properties the mixer reads', async () => {
    await toVideoJSON(project(), urls);

    expect(calls.videos[0]?.props.mute).toBe(false);
    expect(calls.videos[1]?.props.mute).toBe(true);
    // `muted` in a layer's SETTINGS is a no-op (spike 108 §6): writing it would read as a mute
    // that never happens.
    expect(calls.videos[1]?.settings).not.toHaveProperty('muted');
  });

  it('carries the selected clip rotation, fit and volume into VideoFlow', async () => {
    const base = project();
    const first = base.video[0];
    const second = base.video[1];
    if (first === undefined || second === undefined) throw new Error('project fixture lost a clip');
    const styled: VideoProject = {
      ...base,
      video: [
        {
          ...first,
          rotation: 90,
          fit: 'contain',
          volume: 0.4,
          flipX: true,
          brightness: 1.2,
          contrast: 0.8,
          saturation: 1.3,
          hue: 20,
          blur: 0.5,
          animation: 'fadeIn',
          speed: 2,
          transitionIn: 'blurResolve',
          transitionOut: 'glitchResolve',
        },
        second,
      ],
    };
    await toVideoJSON(styled, urls);
    expect(calls.videos[0]?.props).toMatchObject({
      rotation: 90,
      fit: 'contain',
      volume: 0.4,
      scale: [-1, 1],
      filterBrightness: 1.2,
      filterContrast: 0.8,
      filterSaturate: 1.3,
      filterHueRotate: 20,
      filterBlur: 0.5,
      opacity: [
        { time: 0, value: 0 },
        { time: 0.5, value: 1 },
      ],
    });
    expect(calls.videos[0]?.settings).toMatchObject({
      speed: 2,
      sourceDuration: 4,
      transitionIn: { transition: 'blurResolve', duration: 1 },
      transitionOut: { transition: 'glitchResolve', duration: 1 },
    });
  });

  it('overlaps adjacent clips and applies one transition to both edges', async () => {
    const base = project();
    const first = base.video[0];
    const second = base.video[1];
    if (first === undefined || second === undefined) throw new Error('project fixture lost a clip');
    const transitioned: VideoProject = {
      ...base,
      video: [
        first,
        {
          ...second,
          junctionFromClipId: 'clip-1',
          junctionTransition: 'crossfade',
          junctionDuration: 1,
        },
      ],
    };
    await toVideoJSON(transitioned, urls);

    expect(calls.videos[0]?.settings).toMatchObject({
      startTime: 0,
      transitionOut: { transition: 'fade', duration: 1 },
    });
    expect(calls.videos[1]?.settings).toMatchObject({
      startTime: 3,
      transitionIn: { transition: 'fade', duration: 1 },
    });
    expect(calls.waits).toEqual([6]);
  });

  it('uses VideoFlow shape layers for fade-to-white without covering overlays', async () => {
    const base = project();
    const first = base.video[0];
    const second = base.video[1];
    if (first === undefined || second === undefined) throw new Error('project fixture lost a clip');
    await toVideoJSON(
      {
        ...base,
        video: [
          first,
          {
            ...second,
            junctionFromClipId: 'clip-1',
            junctionTransition: 'fadeWhite',
            junctionDuration: 1,
          },
        ],
      },
      urls,
    );

    expect(calls.shapes).toHaveLength(1);
    expect(calls.shapes[0]).toMatchObject({
      props: {
        fill: '#ffffff',
        opacity: [
          { time: 0, value: 0 },
          { time: 0.5, value: 1 },
          { time: 1, value: 0 },
        ],
      },
      settings: { startTime: 3, sourceDuration: 1, shapeType: 'rectangle' },
    });
    expect(calls.texts[0]?.settings).toMatchObject({ startTime: 3.5 });
  });

  it('resolves an overlay against its clip, not against the timeline', async () => {
    await toVideoJSON(project(), urls);

    expect(calls.texts).toHaveLength(1);
    expect(calls.texts[0]).toMatchObject({
      props: { text: 'AURA' },
      settings: { startTime: 4.5, sourceDuration: 1 },
    });
  });

  it('clips an overlay to the end of the clip it hangs on', async () => {
    const base = project();
    const overrun: VideoProject = {
      ...base,
      overlays: [
        {
          id: 'lane-1',
          items: [
            {
              id: 'title',
              kind: 'text',
              anchor: { clipId: 'clip-2', offset: 2 },
              duration: 10,
              props: { text: 'AURA' },
            },
          ],
        },
      ],
    };
    await toVideoJSON(overrun, urls);

    expect(calls.texts[0]?.settings).toMatchObject({ startTime: 6, sourceDuration: 1 });
  });

  it('resolves every source through the cockpit asset route, and nothing off-origin', async () => {
    await toVideoJSON(project(), urls);

    const sources = [...calls.videos, ...calls.images].map((call) => call.settings.source);
    expect(sources).toEqual(['/api/assets/asset-a/download', '/api/assets/asset-a/download']);
    for (const href of Object.values(LOCAL_FONTS)) expect(href.startsWith('/')).toBe(true);
  });

  it('gives a still in the video lane an image layer, with no source window to nudge', async () => {
    const base = project();
    const withStill: VideoProject = {
      ...base,
      video: [
        ...base.video,
        { id: 'clip-3', sourceId: 'src-still', duration: 2, sourceStart: 0, muted: true },
      ],
    };
    await toVideoJSON(withStill, urls);

    expect(calls.videos).toHaveLength(2);
    expect(calls.images).toHaveLength(1);
    expect(calls.images[0]?.settings).toMatchObject({
      source: '/api/assets/asset-still/download',
      startTime: 7,
      sourceDuration: 2,
    });
    expect(calls.images[0]?.settings).not.toHaveProperty('sourceStart');
  });

  it('refuses a clip whose source the project has lost', async () => {
    const base = project();
    const orphan: VideoProject = {
      ...base,
      video: [{ id: 'clip-1', sourceId: 'gone', duration: 4, sourceStart: 0, muted: false }],
    };

    await expect(toVideoJSON(orphan, urls)).rejects.toThrow(/gone/);
  });
});

describe('exportProject', () => {
  it('decodes each source once, however many clips were cut from it', async () => {
    renderer.layers = [
      {
        json: { settings: { source: '/api/assets/asset-a/download' } },
        hasAudio: true,
        decodedBuffer: null,
      },
      {
        json: { settings: { source: '/api/assets/asset-a/download' } },
        hasAudio: true,
        decodedBuffer: null,
      },
      {
        json: { settings: { source: '/api/assets/asset-b/download' } },
        hasAudio: true,
        decodedBuffer: null,
      },
      {
        json: { settings: { source: '/api/assets/asset-c/download' } },
        hasAudio: false,
        decodedBuffer: null,
      },
    ];

    const blob = await exportProject(project(), urls);

    expect(blob.type).toBe('video/mp4');
    expect(decoded.calls).toHaveLength(2);
    expect(renderer.layers[0]?.decodedBuffer).toBe(renderer.layers[1]?.decodedBuffer);
    expect(renderer.layers[2]?.decodedBuffer).not.toBe(renderer.layers[0]?.decodedBuffer);
    expect(renderer.layers[3]?.decodedBuffer).toBeNull();
    expect(renderer.exportOptions[0]).toMatchObject({ worker: true });
  });

  it('leaves a source it cannot decode to the mixer, rather than failing the export', async () => {
    // A clip with no audio track still reaches the decoder — VideoFlow hard-codes
    // `RuntimeVideoLayer.hasAudio` true — and the mixer catches that failure itself.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('no audio track'))),
    );
    renderer.layers = [
      {
        json: { settings: { source: '/api/assets/asset-a/download' } },
        hasAudio: true,
        decodedBuffer: null,
      },
    ];

    const blob = await exportProject(project(), urls);

    expect(blob.type).toBe('video/mp4');
    expect(renderer.layers[0]?.decodedBuffer).toBeNull();
  });

  it('hands the renderer the local fonts before it renders a single frame', async () => {
    await exportProject(project(), urls);

    const instance = renderer.instances[0];
    await instance?.loadFont('Atkinson Hyperlegible Next');
    expect(renderer.stockFontRequests).toEqual([]);
    expect(instance?.loadedFonts['Atkinson Hyperlegible Next']).toBe('/fonts/atkinson.css');
  });

  it('destroys the renderer when the export finishes', async () => {
    await exportProject(project(), urls);

    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('destroys the renderer once when the export throws', async () => {
    renderer.exportImpl = () => Promise.reject(new Error('encoder gone'));

    await expect(exportProject(project(), urls)).rejects.toThrow('encoder gone');
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('destroys the renderer the moment its editor closes', async () => {
    const controller = new AbortController();
    renderer.exportImpl = (options) =>
      new Promise((_resolve, reject) => {
        options.signal?.addEventListener(
          'abort',
          () => {
            reject(new Error('aborted'));
          },
          { once: true },
        );
      });

    const running = exportProject(project(), urls, { signal: controller.signal });
    await vi.waitFor(() => {
      expect(renderer.exportOptions).toHaveLength(1);
    });
    controller.abort();

    await expect(running).rejects.toThrow();
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('constructs no renderer when the abort lands while the project is being compiled', async () => {
    const controller = new AbortController();

    const running = exportProject(project(), urls, { signal: controller.signal });
    // Synchronously after the call: `exportProject` is suspended inside `toVideoJSON`, past the
    // first check and before any listener could exist.
    controller.abort();

    await expect(running).rejects.toThrow();
    expect(renderer.instances).toHaveLength(0);
  });

  it('stops the pre-decode when the abort lands before the export starts', async () => {
    const controller = new AbortController();
    const started: AbortSignal[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: string, init?: { signal?: AbortSignal }) => {
        if (init?.signal) started.push(init.signal);
        return new Promise((_resolve, reject) => {
          init?.signal?.addEventListener(
            'abort',
            () => {
              reject(new DOMException('aborted', 'AbortError'));
            },
            { once: true },
          );
        });
      }),
    );
    renderer.layers = [
      {
        json: { settings: { source: '/api/assets/asset-a/download' } },
        hasAudio: true,
        decodedBuffer: null,
      },
    ];

    const running = exportProject(project(), urls, { signal: controller.signal });
    await vi.waitFor(() => {
      expect(started).toHaveLength(1);
    });
    controller.abort();

    await expect(running).rejects.toThrow();
    // The fetch was cancellable, the decode never ran, and the export was never asked for.
    expect(started[0]).toBe(controller.signal);
    expect(decoded.calls).toHaveLength(0);
    expect(renderer.exportOptions).toHaveLength(0);
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('renders nothing at all when its signal is already aborted', async () => {
    const controller = new AbortController();
    controller.abort();

    await expect(exportProject(project(), urls, { signal: controller.signal })).rejects.toThrow();
    expect(renderer.instances).toHaveLength(0);
    expect(calls.compiled).toBe(0);
  });

  it('reports progress the way its caller asked to hear it', async () => {
    const onProgress = vi.fn();
    renderer.exportImpl = (options) => {
      (options as { onProgress?: (p: number) => void }).onProgress?.(0.5);
      return Promise.resolve(new Blob(['mp4'], { type: 'video/mp4' }));
    };

    await exportProject(project(), urls, { onProgress });

    expect(onProgress).toHaveBeenCalledWith(0.5);
  });
});
