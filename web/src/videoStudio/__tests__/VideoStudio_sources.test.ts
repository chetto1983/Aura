import { describe, expect, it, vi } from 'vitest';
import { CommandRefusal } from '../commands';
import { emptyProject, type VideoProject } from '../project';
import {
  openedProject,
  probeSource,
  REFUSAL_MISSING_ASSET,
  REFUSAL_UNDECODABLE,
  sourceEdit,
} from '../VideoStudio_sources';

// The async half of the workspace, tested without a renderer: what the probe refuses, what the
// first source is allowed to do to the project's frame, and which failures are allowed to be
// worded as "the asset is gone".

const media = vi.hoisted(() => ({ probeVideo: vi.fn() }));
vi.mock('../../mediaEdit/videoMedia', () => media);

const store = vi.hoisted(() => ({ loadProject: vi.fn() }));
vi.mock('../projectStore', () => store);

const SOURCE = {
  assetUrl: (assetId: string) => `/api/assets/${assetId}/download`,
  credentials: 'same-origin' as const,
};

const DEFAULT_SIZE = { width: 1920, height: 1080 };
const PORTRAIT = { duration: 6, width: 1080, height: 1920 };

/** Answer every fetch with `status`, and a one-byte body so a 200 yields a blob. */
function serve(status: number): void {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response(status === 200 ? 'x' : null, { status }))),
  );
}

describe('probeSource', () => {
  it('refuses what the browser cannot decode, by key', async () => {
    media.probeVideo.mockRejectedValue(new Error('no video track'));
    await expect(probeSource(new Blob())).rejects.toThrow(CommandRefusal);
    await expect(probeSource(new Blob())).rejects.toThrow(REFUSAL_UNDECODABLE);
  });

  it('passes the three numbers through when it decodes', async () => {
    media.probeVideo.mockResolvedValue({ ...PORTRAIT, hasAudio: true });
    expect(await probeSource(new Blob())).toMatchObject(PORTRAIT);
  });
});

describe('sourceEdit and the project frame', () => {
  it('takes the first source frame when nothing has chosen one', () => {
    const framed = sourceEdit(PORTRAIT, 'asset-a')(emptyProject('', DEFAULT_SIZE, 30));
    // VideoFlow crops with `fit: cover`, so a portrait clip left in the 1920x1080 default would
    // lose its sides with nothing said.
    expect(framed.size).toEqual({ width: 1080, height: 1920 });
    expect(framed.video).toHaveLength(1);
  });

  it('leaves the frame alone for the SECOND source, whatever shape it is', () => {
    const first = sourceEdit(PORTRAIT, 'asset-a')(emptyProject('', DEFAULT_SIZE, 30));
    const second = sourceEdit({ duration: 3, width: 3840, height: 2160 }, 'asset-b')(first);
    expect(second.size).toEqual({ width: 1080, height: 1920 });
    expect(second.video).toHaveLength(2);
  });

  it('leaves the frame alone when the project already carries one of its own', () => {
    // What `projectFromClip` builds: a project whose frame came from the clip it was opened on.
    const chosen: VideoProject = emptyProject('', { width: 1280, height: 720 }, 30);
    const added = sourceEdit(PORTRAIT, 'asset-a')(chosen);
    expect(added.size).toEqual({ width: 1280, height: 720 });
  });

  it('leaves a DEFAULT-sized project alone once it already holds a source', () => {
    const held = emptyProject('', DEFAULT_SIZE, 30);
    const seeded = {
      ...held,
      sources: [
        {
          id: 'src-0',
          assetId: 'asset-0',
          kind: 'video' as const,
          duration: 4,
          size: DEFAULT_SIZE,
          fps: 30,
        },
      ],
    };
    expect(sourceEdit(PORTRAIT, 'asset-a')(seeded).size).toEqual(DEFAULT_SIZE);
  });
});

describe('openedProject', () => {
  it('hands back the project it was given, untouched', async () => {
    const given = emptyProject('held', DEFAULT_SIZE, 30);
    expect(await openedProject({ kind: 'project', project: given }, SOURCE)).toEqual({
      project: given,
      missing: [],
    });
  });

  it('builds a project from a seeded asset, through the same probe', async () => {
    serve(200);
    media.probeVideo.mockResolvedValue({ ...PORTRAIT, hasAudio: false });
    const opened = await openedProject(
      { kind: 'source', assetId: 'asset-a', name: '  a prompt  ' },
      SOURCE,
    );
    expect(opened.project.name).toBe('a prompt');
    expect(opened.project.size).toEqual({ width: 1080, height: 1920 });
    expect(opened.missing).toEqual([]);
  });

  it('calls a 404 on the seeded asset what it is: gone', async () => {
    serve(404);
    await expect(
      openedProject({ kind: 'source', assetId: 'asset-a', name: 'x' }, SOURCE),
    ).rejects.toThrow(REFUSAL_MISSING_ASSET);
  });

  it.each([401, 500])('does NOT call a %d a deletion', async (status) => {
    serve(status);
    const opening = openedProject({ kind: 'source', assetId: 'asset-a', name: 'x' }, SOURCE);
    await expect(opening).rejects.toThrow(new RegExp(String(status)));
    await expect(opening).rejects.not.toThrow(CommandRefusal);
  });

  it('reads a saved project through the store', async () => {
    const saved = emptyProject('saved', DEFAULT_SIZE, 30);
    store.loadProject.mockResolvedValue({ project: saved, missing: ['src-a'] });
    expect(await openedProject({ kind: 'saved', assetId: 'file-1' }, SOURCE)).toEqual({
      project: saved,
      missing: ['src-a'],
    });
  });
});
