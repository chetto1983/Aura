import { vi } from 'vitest';
import type { VideoProject } from '../project';

// videoflowFakes.ts — VideoFlow's builder and browser renderer stood in for, shared by
// videoflow.test.ts (what the compile hands the builder) and videoflow_export.test.ts (what the
// export does with the renderer). Each file mocks both packages with the classes below:
//
//   vi.mock('@videoflow/core', async () => ({ default: (await import('./videoflowFakes')).FakeVideoFlow }));
//
// The builder records every call it is given; the renderer records what the export asked of it.

interface Call {
  props: Record<string, unknown>;
  settings: Record<string, unknown>;
}

export const calls = {
  videos: [] as Call[],
  texts: [] as Call[],
  images: [] as Call[],
  shapes: [] as Call[],
  audios: [] as Call[],
  waits: [] as unknown[],
  project: [] as unknown[],
  compiled: 0,
};

export class FakeVideoFlow {
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
  addAudio(props: Record<string, unknown>, settings: Record<string, unknown>) {
    calls.audios.push({ props, settings });
    return { animate: vi.fn() };
  }
  wait(time: unknown) {
    calls.waits.push(time);
  }
  compile() {
    calls.compiled += 1;
    return { layers: [], duration: 7 };
  }
}

interface FakeLayer {
  json: { settings: { source?: string } };
  hasAudio: boolean;
  decodedBuffer: unknown;
}

export const renderer = {
  instances: [] as FakeRenderer[],
  layers: [] as FakeLayer[],
  stockFontRequests: [] as string[],
  initCalls: 0,
  exportOptions: [] as { worker?: boolean; signal?: AbortSignal }[],
  // Each test decides how the export behaves: a Blob by default, a hang for the abort case.
  exportImpl: null as null | ((options: { signal?: AbortSignal }) => Promise<Blob>),
};

export class FakeRenderer {
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
}

export const decoded = { calls: [] as ArrayBuffer[] };

export function project(): VideoProject {
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

export const urls = {
  assetUrl: (assetId: string) => `/api/assets/${encodeURIComponent(assetId)}/download`,
};

/** Empties every record and stands in the browser pieces the adapter reaches: fetch, the
 *  decoder, the font set and the stylesheet links it waits on. For a `beforeEach`. */
export function installFakes(): void {
  calls.videos.length = 0;
  calls.texts.length = 0;
  calls.images.length = 0;
  calls.shapes.length = 0;
  calls.audios.length = 0;
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
}

/** Undoes `installFakes`. For an `afterEach`. */
export function removeFakes(): void {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.head.querySelectorAll('link[data-local-font]').forEach((link) => {
    link.remove();
  });
}
