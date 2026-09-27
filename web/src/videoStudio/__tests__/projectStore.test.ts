import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Asset, PresignResponse } from '../../chat/attachments/types';
import type { VideoProject } from '../project';
import {
  lastSavedProject,
  loadProject,
  projectFileName,
  rememberSavedProject,
  saveProject,
} from '../projectStore';

// The project file is an ASSET, so the only thing worth testing here is the round trip through
// that route and what a load says about a source whose bytes are gone. The transport itself —
// the presigned PUT, the poll — is the attachments' own and tested there.

const api = vi.hoisted(() => ({
  presignAsset: vi.fn(),
  finalizeAsset: vi.fn(),
  getAsset: vi.fn(),
}));
const uploaded = vi.hoisted(() => ({ files: [] as File[] }));

vi.mock('../../chat/attachments/api', () => api);

vi.mock('../../chat/attachments/upload', async (original) => ({
  ...(await original<Record<string, unknown>>()),
  putWithProgress: (_url: string, file: File) => {
    uploaded.files.push(file);
    return Promise.resolve();
  },
}));

const SOURCE = {
  assetUrl: (assetId: string) => `/api/assets/${assetId}/download`,
  credentials: 'same-origin' as const,
};

function project(): VideoProject {
  return {
    id: 'project-1',
    name: 'A film',
    size: { width: 1920, height: 1080 },
    fps: 30,
    sources: [
      {
        id: 'src-a',
        assetId: 'asset-a',
        kind: 'video',
        duration: 12.5,
        size: { width: 1920, height: 1080 },
      },
      {
        id: 'src-b',
        assetId: 'asset-b',
        kind: 'image',
        duration: 0,
        size: { width: 800, height: 600 },
      },
    ],
    video: [
      {
        id: 'clip-1',
        sourceId: 'src-a',
        duration: 4,
        sourceStart: 1.25,
        muted: true,
        volume: 0.75,
        rotation: 90,
        fit: 'contain',
        speed: 1.5,
        transitionIn: 'blurResolve',
        transitionOut: 'fade',
        transitionInDuration: 0.8,
        transitionOutDuration: 1.2,
      },
      {
        id: 'clip-2',
        sourceId: 'src-b',
        duration: 3,
        sourceStart: 0,
        muted: false,
        junctionFromClipId: 'clip-1',
        junctionTransition: 'crossfade',
        junctionDuration: 0.7,
      },
    ],
    overlays: [
      {
        id: 'lane-1',
        items: [
          {
            id: 'title-1',
            kind: 'text',
            anchor: { clipId: 'clip-1', offset: 0.5 },
            duration: 2,
            props: { text: 'Ciao', color: '#FFFFFF', position: [0.5, 0.25] },
          },
        ],
      },
    ],
  };
}

function asset(id: string, status: Asset['status'] = 'complete'): Asset {
  return {
    id,
    status,
    modality: 'video',
    file_name: `${id}.mp4`,
    mime_type: 'video/mp4',
    declared_size_bytes: 1,
    size_bytes: 1,
  };
}

function presigned(id: string): PresignResponse {
  return {
    asset: asset(id, 'presigned'),
    upload: {
      upload_url: 'https://store.example/put',
      method: 'PUT',
      required_headers: { 'Content-Type': 'application/json' },
      expires_at: '2026-01-01T00:00:00Z',
    },
  };
}

/**
 * Answer the project file's GET with `body`, and every source's HEAD with `sourceStatus`. The
 * two are separate because they mean different things: the first is whether the project reads,
 * the second is whether one clip's bytes are still there.
 */
function serve(body: string, options: { ok?: boolean; sourceStatus?: number } = {}): void {
  const { ok = true, sourceStatus = 200 } = options;
  vi.stubGlobal(
    'fetch',
    vi.fn((_url: string, init?: RequestInit) =>
      Promise.resolve(
        init?.method === 'HEAD'
          ? new Response(null, { status: sourceStatus })
          : new Response(body, { status: ok ? 200 : 404 }),
      ),
    ),
  );
}

/** Every URL the stubbed fetch was called with, and how. */
function fetched(): readonly (readonly [string, RequestInit | undefined])[] {
  const stub = globalThis.fetch as unknown as { mock: { calls: [string, RequestInit?][] } };
  return stub.mock.calls.map(([url, init]) => [url, init] as const);
}

/** A `getAsset` that fails the way the route does when the row is not there: an Error carrying
 *  no status at all, which is exactly why the HEAD below exists. */
function metadataFails(forId: string): void {
  api.getAsset.mockImplementation((id: string) =>
    id === forId ? Promise.reject(new Error('asset not found')) : Promise.resolve(asset(id)),
  );
}

beforeEach(() => {
  uploaded.files = [];
  api.presignAsset.mockResolvedValue(presigned('file-1'));
  api.finalizeAsset.mockResolvedValue(asset('file-1', 'complete'));
  api.getAsset.mockImplementation((id: string) => Promise.resolve(asset(id)));
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('saveProject', () => {
  it('presigns a .json document, uploads it and finalizes', async () => {
    const id = await saveProject(project());
    expect(id).toBe('file-1');
    const request = api.presignAsset.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(request.mime_type).toBe('application/json');
    // A document, so the server files it under chat/ — media/ is for the sources.
    expect(request.modality_hint).toBe('document');
    expect(String(request.file_name)).toMatch(/\.json$/);
    expect(api.finalizeAsset).toHaveBeenCalledWith('file-1');
  });

  it('writes no frame rate onto a source, because nothing ever measured one', async () => {
    // `probeVideo` does not report a frame rate, so the field could only ever hold the PROJECT's
    // default while calling itself the clip's. Saved, it would become a fact cycle 2 reads.
    await saveProject(project());
    const [written] = uploaded.files;
    if (written === undefined) throw new Error('saveProject uploaded nothing');
    const saved = JSON.parse(await written.text()) as { sources: Record<string, unknown>[] };
    for (const source of saved.sources) expect(source).not.toHaveProperty('fps');
  });

  it('names the file after the project without letting its name reach the path', async () => {
    await saveProject({ ...project(), name: '../../etc/passwd' });
    const request = api.presignAsset.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(String(request.file_name)).not.toContain('/');
    expect(String(request.file_name)).not.toContain('..');
  });

  it('uses the name the caller resolved, so no English fallback is written here', () => {
    // What the workspace passes is t('videoStudio.untitled'); the store never writes that word.
    expect(projectFileName({ ...project(), name: '' }, 'mp4', 'Progetto senza nome')).toBe(
      'Progetto-senza-nome.mp4',
    );
  });

  it('falls back to the project id rather than to a word, when a name slugs to nothing', () => {
    expect(projectFileName({ ...project(), name: '' }, 'json', '?!?')).toBe('project-1.json');
  });
});

describe('loadProject', () => {
  it('round-trips a project exactly', async () => {
    const original = project();
    await saveProject(original);
    const [written] = uploaded.files;
    if (written === undefined) throw new Error('saveProject uploaded nothing');
    serve(await written.text());

    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.project).toEqual(original);
    expect(loaded.missing).toEqual([]);
  });

  it('still reads a file saved while sources carried an fps, and ignores it', async () => {
    const legacy = project();
    serve(
      JSON.stringify({
        ...legacy,
        sources: legacy.sources.map((source) => ({ ...source, fps: 30 })),
      }),
    );

    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.project.sources).toHaveLength(2);
    expect(loaded.project.video).toHaveLength(2);
  });

  it('reads through the identity-scoped route when it is given only an asset id', async () => {
    serve(JSON.stringify(project()));
    const loaded = await loadProject('file-1');
    expect(loaded.project.id).toBe('project-1');
    expect(fetched()[0]?.[0]).toBe('/api/assets/file-1/download');
  });

  it('marks a source the route answers 404 for, and still returns the project', async () => {
    serve(JSON.stringify(project()), { sourceStatus: 404 });
    metadataFails('asset-b');

    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.missing).toEqual(['src-b']);
    expect(loaded.project.video).toHaveLength(2);
  });

  it('does NOT call a 500 a deletion - it propagates as the failure it is', async () => {
    serve(JSON.stringify(project()), { sourceStatus: 500 });
    metadataFails('asset-b');

    await expect(loadProject('file-1', SOURCE)).rejects.toThrow(/500/);
  });

  it('does NOT call an expired session a deletion', async () => {
    serve(JSON.stringify(project()), { sourceStatus: 401 });
    metadataFails('asset-a');

    await expect(loadProject('file-1', SOURCE)).rejects.toThrow(/401/);
  });

  it('counts a row the sweeper marked terminal as gone, without asking for its bytes', async () => {
    serve(JSON.stringify(project()));
    api.getAsset.mockImplementation((id: string) =>
      Promise.resolve(asset(id, id === 'asset-a' ? 'deleted' : 'complete')),
    );

    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.missing).toEqual(['src-a']);
    // The metadata answered, so nothing asked for bytes.
    expect(fetched().filter(([, init]) => init?.method === 'HEAD')).toEqual([]);
  });

  it('refuses a body that is not a project', async () => {
    serve(JSON.stringify({ hello: 'world' }));
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow();
  });

  it.each([
    ['a source that is not an object', { sources: ['src-a'] }],
    ['a source naming no asset', { sources: [{ id: 'src-a', kind: 'video' }] }],
    ['a clip with no sourceStart', { video: [{ id: 'c', sourceId: 'src-a', duration: 1 }] }],
    [
      'a clip whose duration is a string',
      { video: [{ id: 'c', sourceId: 'src-a', duration: '4', sourceStart: 0, muted: false }] },
    ],
    ['a lane that is not a lane', { overlays: ['lane-1'] }],
    [
      'a clip naming a source the file does not hold',
      { video: [{ id: 'c', sourceId: 'src-gone', duration: 1, sourceStart: 0, muted: false }] },
    ],
    [
      'an overlay anchored to a clip the file does not hold',
      {
        overlays: [
          {
            id: 'lane-1',
            items: [
              {
                id: 'i',
                kind: 'text',
                duration: 1,
                anchor: { clipId: 'clip-gone', offset: 0 },
                props: {},
              },
            ],
          },
        ],
      },
    ],
    [
      'an overlay with no anchor',
      { overlays: [{ id: 'lane-1', items: [{ id: 'i', kind: 'text', duration: 1, props: {} }] }] },
    ],
    [
      'an overlay whose props are an array',
      {
        overlays: [
          {
            id: 'lane-1',
            items: [
              {
                id: 'i',
                kind: 'text',
                duration: 1,
                anchor: { clipId: 'c', offset: 0 },
                props: [],
              },
            ],
          },
        ],
      },
    ],
  ])('refuses a saved file carrying %s', async (_case, broken) => {
    serve(JSON.stringify({ ...project(), ...broken }));
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow(/not a project/);
  });

  it('refuses a read the route did not answer', async () => {
    serve('nope', { ok: false });
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow();
  });
});

const MUSIC_SOURCE = {
  id: 'src-music',
  assetId: 'asset-music',
  kind: 'audio',
  duration: 8,
  size: { width: 0, height: 0 },
  speech: [[1, 2.5]],
};

function audioItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 'bed-1',
    sourceId: 'src-music',
    anchor: { clipId: 'clip-1', offset: 0 },
    sourceStart: 0,
    duration: 6,
    volume: 0.8,
    muted: false,
    fadeIn: 1,
    fadeOut: 2,
    envelope: [
      { time: 0, gain: 1 },
      { time: 3, gain: 0.4 },
    ],
    ducking: { amountDb: -12, ramp: 0.4 },
    denoise: true,
    label: 'Bed',
    ...overrides,
  };
}

function withAudio(item: Record<string, unknown>, sources: unknown[] = [MUSIC_SOURCE]) {
  const base = project();
  return {
    ...base,
    sources: [...base.sources, ...sources],
    audio: [{ id: 'audio-1', items: [item] }],
  };
}

describe('loadProject with audio lanes', () => {
  it('round-trips a project that carries an audio lane', async () => {
    const original = withAudio(audioItem());
    serve(JSON.stringify(original));
    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.project).toEqual(original);
  });

  it('still reads a project saved before audio existed', async () => {
    const legacy = project();
    serve(JSON.stringify(legacy));
    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.project.audio).toBeUndefined();
    expect(loaded.project.video).toEqual(legacy.video);
  });

  it.each([
    [
      'an audio item naming a source the file does not hold',
      audioItem({ sourceId: 'src-gone' }),
      undefined,
    ],
    ['an audio item playing a still image', audioItem({ sourceId: 'src-b' }), undefined],
    [
      'an audio item anchored to a clip the file does not hold',
      audioItem({ anchor: { clipId: 'clip-gone', offset: 0 } }),
      undefined,
    ],
    [
      'an audio item extracted from a clip the file does not hold',
      audioItem({ extractedFrom: 'clip-gone' }),
      undefined,
    ],
    ['an audio volume JSON reads as Infinity', '__INFINITE_VOLUME__', undefined],
    ['an envelope point with no gain', audioItem({ envelope: [{ time: 0 }] }), undefined],
    ['ducking that is not an object', audioItem({ ducking: -12 }), undefined],
    ['a speech window that runs backwards', audioItem(), [{ ...MUSIC_SOURCE, speech: [[3, 1]] }]],
    ['a speech window that is not a pair', audioItem(), [{ ...MUSIC_SOURCE, speech: [[1]] }]],
  ])('refuses a saved file carrying %s', async (_case, item, sources) => {
    const body =
      item === '__INFINITE_VOLUME__'
        ? JSON.stringify(withAudio(audioItem())).replace('"volume":0.8', '"volume":1e999')
        : JSON.stringify(withAudio(item as Record<string, unknown>, sources));
    serve(body);
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow(/not a project/);
  });

  it('refuses a clip on the video lane that plays an audio source', async () => {
    const base = withAudio(audioItem());
    const broken = { ...base, video: [{ ...base.video[0], sourceId: 'src-music' }] };
    serve(JSON.stringify(broken));
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow(/not a project/);
  });
});

describe('the last project saved here', () => {
  it('is remembered across a reload and read back', () => {
    rememberSavedProject('file-9');
    expect(lastSavedProject()).toBe('file-9');
  });

  it('is simply absent when the browser refuses storage', () => {
    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('denied');
      },
      setItem: () => {
        throw new Error('denied');
      },
    });
    // Neither call throws: a browser without storage is one where the entrance does not appear.
    expect(() => {
      rememberSavedProject('file-9');
    }).not.toThrow();
    expect(lastSavedProject()).toBeUndefined();
  });
});
