import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Asset, PresignResponse } from '../../chat/attachments/types';
import type { VideoProject } from '../project';
import { loadProject, saveProject } from '../projectStore';

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
        fps: 30,
      },
      {
        id: 'src-b',
        assetId: 'asset-b',
        kind: 'image',
        duration: 0,
        size: { width: 800, height: 600 },
        fps: 30,
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 1.25, muted: true },
      { id: 'clip-2', sourceId: 'src-b', duration: 3, sourceStart: 0, muted: false },
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

/** Answer the next GET of the project file with `body`. */
function serve(body: string, ok = true): void {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response(body, { status: ok ? 200 : 404 }))),
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

  it('names the file after the project without letting its name reach the path', async () => {
    await saveProject({ ...project(), name: '../../etc/passwd' });
    const request = api.presignAsset.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(String(request.file_name)).not.toContain('/');
    expect(String(request.file_name)).not.toContain('..');
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

  it('marks a source whose asset is gone and still returns the project', async () => {
    serve(JSON.stringify(project()));
    api.getAsset.mockImplementation((id: string) =>
      id === 'asset-b' ? Promise.reject(new Error('HTTP 404')) : Promise.resolve(asset(id)),
    );

    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.missing).toEqual(['src-b']);
    expect(loaded.project.video).toHaveLength(2);
  });

  it('counts an asset the sweeper deleted as gone, not as present', async () => {
    serve(JSON.stringify(project()));
    api.getAsset.mockImplementation((id: string) =>
      Promise.resolve(asset(id, id === 'asset-a' ? 'deleted' : 'complete')),
    );

    const loaded = await loadProject('file-1', SOURCE);
    expect(loaded.missing).toEqual(['src-a']);
  });

  it('refuses a body that is not a project', async () => {
    serve(JSON.stringify({ hello: 'world' }));
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow();
  });

  it('refuses a read the route did not answer', async () => {
    serve('nope', false);
    await expect(loadProject('file-1', SOURCE)).rejects.toThrow();
  });
});
