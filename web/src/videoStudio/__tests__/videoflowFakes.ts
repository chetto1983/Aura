import { vi } from 'vitest';
import type { VideoProject } from '../project';

// videoflowFakes.ts — VideoFlow's builder and browser renderer stood in for, shared by
// videoflow.test.ts (what the compile hands the builder) and the export tests (what an export does
// with the renderer). An export test mocks both packages with the classes below, and hands out the
// media cache through a getter, so the code under test always reads this test's own cache:
//
//   vi.mock('@videoflow/core', async () => {
//     const fakes = await import('./videoflowFakes');
//     return { default: fakes.FakeVideoFlow, get loadedMedia() { return fakes.media.cache; } };
//   });
//   vi.mock('@videoflow/renderer-browser', async () => ({
//     default: (await import('./videoflowFakes')).FakeRenderer,
//   }));
//
// The builder records every call it is given; the renderer records what the export asked of it.
// The media cache is NOT a stand-in: it is VideoFlow's own `MediaCache`, a fresh one per test, so
// a hit, a miss and a failed fetch behave exactly as they do under the real renderer.

const { MediaCache } = await vi.importActual<typeof import('@videoflow/core')>('@videoflow/core');

/** The page's `loadedMedia`, replaced by a fresh cache in `installFakes`. A module mock hands it
 *  out through a getter, so the code under test always reads the current one. */
export const media = { cache: new MediaCache() };

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

export interface FakeLayer {
  json: {
    type: string;
    settings: { source?: string; enabled?: boolean };
    properties: { mute?: boolean };
  };
  hasAudio: boolean;
  decodedBuffer: unknown;
  /** Its bytes arrive but it cannot read them — an HEVC clip, a corrupt still. */
  unreadable?: true;
}

/**
 * A layer over `source`, the way VideoFlow's runtime would hold it before `initLayers`. Whether it
 * carries sound follows from its type, as in VideoFlow: `RuntimeVideoLayer` and `RuntimeAudioLayer`
 * answer `hasAudio` true whatever the file holds, and every other layer false.
 */
export function layer(
  source: string,
  options: { type?: 'video' | 'image' | 'audio'; muted?: true; unreadable?: true } = {},
): FakeLayer {
  const type = options.type ?? 'video';
  return {
    json: { type, settings: { source }, properties: options.muted ? { mute: true } : {} },
    hasAudio: type !== 'image',
    decodedBuffer: null,
    ...(options.unreadable ? { unreadable: true } : {}),
  };
}

export const renderer = {
  instances: [] as FakeRenderer[],
  layers: [] as FakeLayer[],
  stockFontRequests: [] as string[],
  initCalls: 0,
  exportOptions: [] as { worker?: boolean; signal?: AbortSignal }[],
  // Each test decides how the export behaves: a Blob by default, a hang for the abort case.
  exportImpl: null as null | ((options: { signal?: AbortSignal }) => Promise<Blob>),
  /** What `renderAudio` answers: a mix by default, `null` for a project with no sound. */
  audio: {} as object | null,
};

export class FakeRenderer {
  json: unknown;
  layers: FakeLayer[] = [];
  loadedFonts: Record<string, string> = {};
  destroyed = 0;
  private readonly held: string[] = [];
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
  /** What BrowserRenderer.initLayers does (dist/BrowserRenderer.js:427-445): every layer takes its
   *  bytes from the media cache, and one that cannot load is disabled, never thrown. */
  async initLayers(): Promise<void> {
    renderer.initCalls += 1;
    this.layers = renderer.layers;
    await Promise.all(
      this.layers.map(async (held) => {
        const source = held.json.settings.source;
        if (source === undefined) return;
        try {
          await media.cache.acquire(source);
          this.held.push(source);
          if (held.unreadable) throw new Error('the layer could not read its bytes');
        } catch {
          held.json.settings.enabled = false;
        }
      }),
    );
  }
  exportVideo(options: { worker?: boolean; signal?: AbortSignal }): Promise<Blob> {
    renderer.exportOptions.push(options);
    if (renderer.exportImpl) return renderer.exportImpl(options);
    return Promise.resolve(new Blob(['mp4'], { type: 'video/mp4' }));
  }
  renderAudio(): Promise<object | null> {
    return Promise.resolve(renderer.audio);
  }
  destroy() {
    this.destroyed += 1;
    for (const source of this.held.splice(0)) media.cache.release(source);
  }
}

/** Every decode asked for, and the sources whose bytes the decoder refuses: a fetched body is its
 *  own URL (see `installFakes`), so the decoder knows which source it was handed. */
export const decoded = { calls: [] as ArrayBuffer[], refused: new Set<string>() };

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
  renderer.audio = {};
  decoded.calls.length = 0;
  decoded.refused.clear();
  media.cache = new MediaCache();

  // Each source's body is its own URL, so a decode can tell which source it was handed.
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string) => Promise.resolve(new Response(url))),
  );
  vi.stubGlobal(
    'OfflineAudioContext',
    class {
      decodeAudioData(bytes: ArrayBuffer) {
        decoded.calls.push(bytes);
        if (decoded.refused.has(new TextDecoder().decode(bytes))) {
          return Promise.reject(new DOMException('Unable to decode audio data', 'EncodingError'));
        }
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
