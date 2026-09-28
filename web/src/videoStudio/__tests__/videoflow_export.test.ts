import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { exportProject } from '../videoflow';
import {
  calls,
  decoded,
  installFakes,
  project,
  removeFakes,
  renderer,
  urls,
} from './videoflowFakes';

// What the export does with VideoFlow's browser renderer: the pre-decode, the local fonts, the
// renderer's lifetime and the abort. The compile it starts from is videoflow.test.ts's subject.

vi.mock('@videoflow/core', async () => ({
  default: (await import('./videoflowFakes')).FakeVideoFlow,
}));
vi.mock('@videoflow/renderer-browser', async () => ({
  default: (await import('./videoflowFakes')).FakeRenderer,
}));

beforeEach(installFakes);
afterEach(removeFakes);

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
