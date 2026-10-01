import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { VideoProject } from '../project';
import { exportProject } from '../videoflow';
import { ExportSourceError } from '../videoflow_media';
import {
  calls,
  decoded,
  installFakes,
  layer,
  media,
  project,
  removeFakes,
  renderer,
  urls,
} from './videoflowFakes';

// What the export does with VideoFlow's browser renderer: the media it loads, the pre-decode, the
// local fonts, the renderer's lifetime and the abort. The compile it starts from is
// videoflow.test.ts's subject.

vi.mock('@videoflow/core', async () => {
  const fakes = await import('./videoflowFakes');
  return {
    default: fakes.FakeVideoFlow,
    get loadedMedia() {
      return fakes.media.cache;
    },
  };
});
vi.mock('@videoflow/renderer-browser', async () => ({
  default: (await import('./videoflowFakes')).FakeRenderer,
}));

beforeEach(installFakes);
afterEach(removeFakes);

const A = '/api/assets/asset-a/download';
const B = '/api/assets/asset-b/download';
const C = '/api/assets/asset-c/download';

/** How many times the page fetched each URL. */
function fetchesOf(url: string): number {
  const stub = globalThis.fetch as unknown as { mock: { calls: [string][] } };
  return stub.mock.calls.filter(([fetched]) => fetched === url).length;
}

/** The demo project with a sound of its own, `asset-b`, under the film. */
function withSound(): VideoProject {
  const base = project();
  return {
    ...base,
    sources: [
      ...base.sources,
      {
        id: 'src-b',
        assetId: 'asset-b',
        kind: 'audio',
        duration: 5,
        size: { width: 0, height: 0 },
      },
    ],
  };
}

/** A fetch that answers `status` for one URL and serves every other. */
function answering(url: string, status: number): void {
  vi.stubGlobal(
    'fetch',
    vi.fn((fetched: string) =>
      Promise.resolve(new Response(fetched, { status: fetched === url ? status : 200 })),
    ),
  );
}

describe('exportProject', () => {
  it('fetches each source once and decodes it once, however many clips were cut from it', async () => {
    renderer.layers = [layer(A), layer(A), layer(B), layer(C, { type: 'image' })];

    const blob = await exportProject(project(), urls);

    expect(blob.type).toBe('video/mp4');
    // S1.4 measured two GETs per source: VideoFlow's own, then the pre-decode's.
    expect([fetchesOf(A), fetchesOf(B), fetchesOf(C)]).toEqual([1, 1, 1]);
    expect(decoded.calls).toHaveLength(2);
    expect(renderer.layers[0]?.decodedBuffer).toBe(renderer.layers[1]?.decodedBuffer);
    expect(renderer.layers[2]?.decodedBuffer).not.toBe(renderer.layers[0]?.decodedBuffer);
    expect(renderer.layers[3]?.decodedBuffer).toBeNull();
    expect(renderer.exportOptions[0]).toMatchObject({ worker: true });
  });

  it('stops on a source the server refuses, naming its asset, and exports nothing', async () => {
    answering(A, 404);
    renderer.layers = [layer(A)];

    const failure = exportProject(project(), urls);

    await expect(failure).rejects.toBeInstanceOf(ExportSourceError);
    // The name and the message are what survive where the class does not: a log line, or a page
    // handing the failure on as plain data.
    await expect(failure).rejects.toMatchObject({
      name: 'ExportSourceError',
      message: 'videoStudio: source asset-a is unreachable',
      assetId: 'asset-a',
      failure: 'unreachable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
    expect(renderer.instances[0]?.destroyed).toBe(1);
  });

  it('stops on a source whose body fails halfway, as one that never arrived', async () => {
    // The server answered 200 and the stream broke: VideoFlow's cache drops the entry when the
    // body rejects (core dist/MediaCache.js `fetchAndStore`, then `acquire`), so it reads as lost.
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            new ReadableStream({
              start: (stream) => {
                stream.error(new TypeError('network error'));
              },
            }),
          ),
        ),
      ),
    );
    renderer.layers = [layer(A)];

    await expect(exportProject(project(), urls)).rejects.toMatchObject({
      assetId: 'asset-a',
      failure: 'unreachable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it('stops on a source the page cannot reach at all: a CORS refusal has no status', async () => {
    // S1.4's case: the media origin sent no CORS header, and the export resolved with black.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('Failed to fetch'))),
    );
    renderer.layers = [layer(A)];

    await expect(exportProject(project(), urls)).rejects.toMatchObject({
      assetId: 'asset-a',
      failure: 'unreachable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it.each([
    ['the cleaned copy of a clip', 'asset-a-clean'],
    ['a picture laid over the film', 'asset-logo'],
  ])('names %s by its own asset when its bytes are gone', async (_what, assetId) => {
    const url = `/api/assets/${assetId}/download`;
    answering(url, 404);
    renderer.layers = [layer(A), layer(url)];
    const base = project();

    await expect(
      exportProject(
        {
          ...base,
          sources: base.sources.map((source) =>
            source.id === 'src-a' ? { ...source, denoisedAssetId: 'asset-a-clean' } : source,
          ),
          overlays: [
            ...base.overlays,
            {
              id: 'lane-2',
              items: [
                {
                  id: 'logo',
                  kind: 'image',
                  anchor: { clipId: 'clip-1', offset: 0 },
                  duration: 1,
                  props: { assetId: 'asset-logo' },
                },
              ],
            },
          ],
        },
        urls,
      ),
    ).rejects.toMatchObject({ assetId, failure: 'unreachable' });
  });

  it('stops on a source whose bytes arrived but that the renderer could not read', async () => {
    renderer.layers = [
      layer(A),
      layer('/api/assets/asset-still/download', { type: 'image', unreadable: true }),
    ];

    await expect(exportProject(project(), urls)).rejects.toMatchObject({
      assetId: 'asset-still',
      failure: 'undecodable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it('stops on a sound whose bytes will not decode, rather than exporting its silence', async () => {
    decoded.refused.add(B);
    renderer.layers = [layer(A), layer(B, { type: 'audio' })];

    await expect(exportProject(withSound(), urls)).rejects.toMatchObject({
      assetId: 'asset-b',
      failure: 'undecodable',
    });
    expect(renderer.exportOptions).toHaveLength(0);
  });

  it('exports past a muted sound whose bytes will not decode: the mixer never plays it', async () => {
    decoded.refused.add(B);
    renderer.layers = [layer(A), layer(B, { type: 'audio', muted: true })];

    const blob = await exportProject(withSound(), urls);

    expect(blob.type).toBe('video/mp4');
    expect(renderer.layers[1]?.decodedBuffer).toBeNull();
  });

  it('leaves a video whose sound will not decode to the mixer, rather than failing the export', async () => {
    // A clip with no audio track still reaches the decoder — VideoFlow hard-codes
    // `RuntimeVideoLayer.hasAudio` true — and the mixer catches that failure itself.
    decoded.refused.add(A);
    renderer.layers = [layer(A)];

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

  it('decodes nothing and exports nothing when the abort lands while the media loads', async () => {
    const controller = new AbortController();
    let arrive: (() => void) | undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn(
        (url: string) =>
          new Promise<Response>((resolve) => {
            arrive = () => {
              resolve(new Response(url));
            };
          }),
      ),
    );
    renderer.layers = [layer(A)];

    const running = exportProject(project(), urls, { signal: controller.signal });
    await vi.waitFor(() => {
      expect(arrive).toBeDefined();
    });
    controller.abort();
    // VideoFlow's fetch takes no signal; its bytes land after the editor has closed.
    arrive?.();

    await expect(running).rejects.toThrow();
    expect(decoded.calls).toHaveLength(0);
    expect(renderer.exportOptions).toHaveLength(0);
    expect(renderer.instances[0]?.destroyed).toBe(1);
    // What the layer acquired after the close is released too: the cache is the page's, and the
    // Stage reads it. One acquire now is the only reference left, so the count reads 1.
    const entry = await media.cache.acquire(A);
    expect(entry.refCount).toBe(1);
    media.cache.release(A);
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
