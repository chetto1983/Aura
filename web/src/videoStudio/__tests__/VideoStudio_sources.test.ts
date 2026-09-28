import { describe, expect, it, vi } from 'vitest';
import { CommandRefusal } from '../commands';
import { emptyProject, type VideoProject } from '../project';
import {
  openedProject,
  probeAsset,
  probeSource,
  projectFromClip,
  REFUSAL_MISSING_ASSET,
  REFUSAL_UNDECODABLE,
  REFUSAL_UNDECODABLE_SOUND,
  sourceEdit,
  uploadSource,
} from '../VideoStudio_sources';

// The async half of the workspace, tested without a renderer: what the probe refuses, what the
// first source is allowed to do to the project's frame, and which failures are allowed to be
// worded as "the asset is gone".

const media = vi.hoisted(() => ({ probeVideo: vi.fn(), probeAudio: vi.fn() }));
vi.mock('../../mediaEdit/videoMedia', () => media);

const store = vi.hoisted(() => ({ loadProject: vi.fn() }));
vi.mock('../projectStore', () => store);

const assets = vi.hoisted(() => ({
  presignAsset: vi.fn(() =>
    Promise.resolve({ asset: { id: 'x' }, upload: { upload_url: 'u', required_headers: {} } }),
  ),
  finalizeMediaAsset: vi.fn(() => Promise.resolve({ id: 'x' })),
}));
vi.mock('../../chat/attachments/api', () => assets);
vi.mock('../../chat/attachments/upload', () => ({ putWithProgress: () => Promise.resolve() }));

const SOURCE = {
  assetUrl: (assetId: string) => `/api/assets/${assetId}/download`,
  credentials: 'same-origin' as const,
};

const DEFAULT_SIZE = { width: 1920, height: 1080 };
const PORTRAIT = { kind: 'video' as const, duration: 6, width: 1080, height: 1920 };
const STILL = { kind: 'image' as const, duration: 0, width: 800, height: 600 };

/** A decoder that answers, or one that does not: `createImageBitmap` is the image's `canDecode`
 *  and jsdom has neither. */
function decodesImages(yes: boolean): void {
  vi.stubGlobal(
    'createImageBitmap',
    vi.fn(() =>
      yes
        ? Promise.resolve({ width: STILL.width, height: STILL.height, close: () => undefined })
        : Promise.reject(new Error('not an image this browser knows')),
    ),
  );
}

/** Answer every fetch with `status`, and a one-byte body so a 200 yields a blob. */
function serve(status: number): void {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response(status === 200 ? 'x' : null, { status }))),
  );
}

describe('probeSource', () => {
  it('refuses a file it cannot even read, by key', async () => {
    media.probeVideo.mockRejectedValue(new Error('no video track'));
    await expect(probeSource(new Blob())).rejects.toThrow(CommandRefusal);
    await expect(probeSource(new Blob())).rejects.toThrow(REFUSAL_UNDECODABLE);
  });

  it('refuses a file it can read but not decode, which is the commoner one', async () => {
    // The case the refusal sentence describes and the one a parse alone cannot see: an
    // MPEG-4 Part 2 clip parses, answers a display size and a duration, and has no decoder
    // in any browser. Accepting it puts a layer in the composition that exports black.
    media.probeVideo.mockResolvedValue({ ...PORTRAIT, hasAudio: true, decodable: false });
    await expect(probeSource(new Blob())).rejects.toThrow(REFUSAL_UNDECODABLE);
  });

  it('passes the three numbers through when it decodes', async () => {
    media.probeVideo.mockResolvedValue({ ...PORTRAIT, hasAudio: true, decodable: true });
    expect(await probeSource(new Blob())).toMatchObject(PORTRAIT);
  });
});

describe('probeSource, on a still', () => {
  it('measures it with a real decode, and gives it no length of its own', async () => {
    decodesImages(true);
    expect(await probeSource(new File([], 'a.png', { type: 'image/png' }))).toEqual(STILL);
  });

  it('refuses a still this browser cannot decode, the way it refuses a clip', async () => {
    // `createImageBitmap` IS the decode: a file that reaches VideoFlow undecodable becomes a
    // layer it disables, which is the same black picture an undecodable clip produces.
    decodesImages(false);
    await expect(probeSource(new File([], 'a.png', { type: 'image/png' }))).rejects.toThrow(
      REFUSAL_UNDECODABLE,
    );
  });

  it('never sends a still to the video probe', async () => {
    decodesImages(true);
    media.probeVideo.mockRejectedValue(new Error('no video track'));
    await expect(probeSource(new File([], 'a.png', { type: 'image/png' }))).resolves.toEqual(STILL);
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
    const second = sourceEdit(
      { kind: 'video', duration: 3, width: 3840, height: 2160 },
      'asset-b',
    )(first);
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

describe('sourceEdit, on a still', () => {
  it('gives the clip a length to be seen for and the source none of its own', () => {
    const added = sourceEdit(STILL, 'asset-i')(emptyProject('', DEFAULT_SIZE, 30));
    // A still has no duration to read, so the model's zero stays zero on the SOURCE and the
    // five seconds the editor chose sit on the ITEM, where a trim can change them.
    expect(added.sources[0]).toMatchObject({ kind: 'image', duration: 0 });
    expect(added.video[0]?.duration).toBe(5);
  });

  it('takes its frame the way a clip does when nothing has chosen one', () => {
    expect(sourceEdit(STILL, 'asset-i')(emptyProject('', DEFAULT_SIZE, 30)).size).toEqual({
      width: 800,
      height: 600,
    });
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
    media.probeVideo.mockResolvedValue({ ...PORTRAIT, hasAudio: false, decodable: true });
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

describe('projectFromClip', () => {
  const RANGE = { start: 1, end: 3 };

  it('carries the trim across when the clip decodes', () => {
    const project = projectFromClip(
      'clip.mp4',
      'asset-a',
      { ...PORTRAIT, hasAudio: true, decodable: true },
      RANGE,
    );
    expect(project.video).toHaveLength(1);
    expect(project.video[0]).toMatchObject({ sourceStart: 1, duration: 2 });
    expect(project.size).toEqual({ width: 1080, height: 1920 });
  });

  it('refuses a clip this browser cannot decode, the way the other two doors do', () => {
    // The quick editor WELCOMES an undecodable file — a copy-trim never decodes a frame — so it
    // is the one door that can hand a composition a source nobody can play. VideoFlow answers
    // such a layer by disabling it and exporting black, which is what the refusal exists for.
    expect(() =>
      projectFromClip(
        'clip.mp4',
        'asset-a',
        { ...PORTRAIT, hasAudio: true, decodable: false },
        RANGE,
      ),
    ).toThrow(REFUSAL_UNDECODABLE);
  });
});

const SOUND = { kind: 'audio' as const, duration: 6, width: 0, height: 0 };

describe('a sound at the door', () => {
  it('is probed as a sound when the bytes say they are one, and has no frame', async () => {
    media.probeVideo.mockClear();
    media.probeAudio.mockResolvedValue({ duration: 6, decodable: true });
    await expect(probeSource(new Blob(['x'], { type: 'audio/wav' }))).resolves.toEqual(SOUND);
    expect(media.probeVideo).not.toHaveBeenCalled();
  });

  it('is refused in its own words when this browser cannot decode it or read it', async () => {
    media.probeAudio.mockResolvedValue({ duration: 6, decodable: false });
    await expect(probeSource(new Blob(['x'], { type: 'audio/ogg' }))).rejects.toThrow(
      REFUSAL_UNDECODABLE_SOUND,
    );
    media.probeAudio.mockRejectedValue(new Error('no audio track'));
    await expect(probeSource(new Blob(['x'], { type: 'audio/ogg' }))).rejects.toThrow(
      REFUSAL_UNDECODABLE_SOUND,
    );
  });

  it('goes on a lane at the time asked, named after its file, and never re-frames the project', () => {
    const film: VideoProject = {
      ...emptyProject('film', DEFAULT_SIZE, 30),
      sources: [
        {
          id: 'src-a',
          assetId: 'a',
          kind: 'video',
          duration: 8,
          size: { width: 640, height: 360 },
        },
      ],
      video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    };
    const next = sourceEdit(SOUND, 'sound-asset', { time: 2, label: 'bed.wav' })(film);
    expect(next.size).toEqual(DEFAULT_SIZE);
    expect(next.sources.at(-1)).toMatchObject({
      assetId: 'sound-asset',
      kind: 'audio',
      duration: 6,
    });
    expect(next.audio?.[0]?.items[0]).toMatchObject({
      anchor: { clipId: 'clip-1', offset: 2 },
      label: 'bed.wav',
    });
    expect(next.video).toHaveLength(1);
  });

  it('is refused, not parked, when there is no film to hang it on', () => {
    expect(() =>
      sourceEdit(SOUND, 'sound-asset', { time: 0 })(emptyProject('empty', DEFAULT_SIZE, 30)),
    ).toThrow('videoStudio.audio.refusal.noClip');
  });

  it('is filed as a sound', async () => {
    await uploadSource(new File(['x'], 'bed.wav', { type: 'audio/wav' }));
    expect(assets.presignAsset).toHaveBeenCalledWith(
      expect.objectContaining({ modality_hint: 'audio' }),
    );
  });
});

describe('probeAsset', () => {
  it('reads a library asset through the probe a picked file goes through, typed by its route', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('x', { headers: { 'Content-Type': 'audio/wav' } }))),
    );
    media.probeAudio.mockResolvedValue({ duration: 4, decodable: true });
    expect(await probeAsset('bed', SOURCE)).toEqual({
      kind: 'audio',
      duration: 4,
      width: 0,
      height: 0,
    });
    expect(fetch).toHaveBeenCalledWith('/api/assets/bed/download', {
      credentials: 'same-origin',
    });
  });

  it('calls a library asset deleted since the list was read what it is: gone', async () => {
    serve(404);
    await expect(probeAsset('bed', SOURCE)).rejects.toThrow(REFUSAL_MISSING_ASSET);
  });
});
