import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NoiseReductionSwitch } from '../Inspector_audioClean';
import type { AudioItem, VideoProject } from '../project';

// The Noise reduction switch, with the cleaning and the upload stood in for. What is judged is the
// ONE edit it makes — the cleaned copy recorded and the switch turned on together, one undo step —
// and that a copy already made is never made again (Review Focus 3).

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) =>
      values === undefined ? key : `${key} ${Object.values(values).join(' ')}`,
  }),
}));

const cleaning = vi.hoisted(() => ({
  cleanedFile: vi.fn<(url: string, name: string, signal?: AbortSignal) => Promise<File>>(),
  upload: vi.fn<(file: File) => Promise<string>>(),
  remove: vi.fn<(id: string) => Promise<unknown>>(),
}));
vi.mock('../audioClean', () => ({ cleanedFile: cleaning.cleanedFile }));
vi.mock('../VideoStudio_sources', () => ({ uploadSource: cleaning.upload }));
vi.mock('../../chat/attachments/api', () => ({ deleteAsset: cleaning.remove }));

function film(over: Partial<AudioItem> = {}, denoisedAssetId?: string): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 320, height: 180 },
    fps: 25,
    sources: [
      {
        id: 'src-a',
        assetId: 'a',
        kind: 'video',
        duration: 8,
        size: { width: 320, height: 180 },
        hasAudio: true,
      },
      {
        id: 'src-v',
        assetId: 'v',
        kind: 'audio',
        duration: 6,
        size: { width: 0, height: 0 },
        ...(denoisedAssetId === undefined ? {} : { denoisedAssetId }),
      },
    ],
    video: [{ id: 'clip-1', sourceId: 'src-a', duration: 8, sourceStart: 0, muted: false }],
    overlays: [],
    audio: [
      {
        id: 'lane',
        items: [
          {
            id: 'voice',
            sourceId: 'src-v',
            anchor: { clipId: 'clip-1', offset: 0 },
            sourceStart: 0,
            duration: 6,
            volume: 1,
            muted: false,
            ...over,
          },
        ],
      },
    ],
  };
}

type Edit = (current: VideoProject) => VideoProject;

function mount(project: VideoProject, on = false) {
  const edits: Edit[] = [];
  const onCommand = vi.fn((edit: Edit) => {
    edits.push(edit);
  });
  const view = render(
    <NoiseReductionSwitch
      project={project}
      target={{ kind: 'sound', id: 'voice', sourceId: 'src-v', on }}
      onCommand={onCommand}
    />,
  );
  const apply = (index = 0) => edits[index]?.(project);
  return { ...view, onCommand, apply };
}

function flip() {
  fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.denoise' }));
}

beforeEach(() => {
  cleaning.cleanedFile.mockReset();
  cleaning.cleanedFile.mockResolvedValue(
    new File(['ogg'], 'src-v.clean.ogg', { type: 'audio/ogg' }),
  );
  cleaning.upload.mockReset();
  cleaning.upload.mockResolvedValue('v-clean');
  cleaning.remove.mockReset();
  cleaning.remove.mockResolvedValue({});
});

describe('NoiseReductionSwitch', () => {
  it('cleans the source once, then records the copy and turns the switch on in one edit', async () => {
    const { onCommand, apply } = mount(film());
    flip();
    expect(await screen.findByRole('status')).toBeTruthy();
    await waitFor(() => {
      expect(onCommand).toHaveBeenCalledOnce();
    });
    expect(cleaning.cleanedFile).toHaveBeenCalledWith(
      '/api/assets/v/download',
      'src-v',
      expect.any(AbortSignal),
    );
    const next = apply();
    expect(next?.sources.find((source) => source.id === 'src-v')?.denoisedAssetId).toBe('v-clean');
    expect(next?.audio?.[0]?.items[0]?.denoise).toBe(true);
    await waitFor(() => {
      expect(screen.queryByRole('status')).toBeNull();
    });
  });

  it('turns back on from the copy already made, without cleaning again', () => {
    const { onCommand, apply } = mount(film({}, 'v-clean'));
    flip();
    expect(onCommand).toHaveBeenCalledOnce();
    expect(cleaning.cleanedFile).not.toHaveBeenCalled();
    expect(apply()?.audio?.[0]?.items[0]?.denoise).toBe(true);
  });

  it('turns off without cleaning anything', () => {
    const { apply } = mount(film({ denoise: true }, 'v-clean'), true);
    flip();
    expect(apply()?.audio?.[0]?.items[0]?.denoise).toBe(false);
    expect(cleaning.cleanedFile).not.toHaveBeenCalled();
  });

  it('says why a cleaning failed and leaves the project untouched', async () => {
    cleaning.cleanedFile.mockRejectedValue(new Error('Unable to decode audio data'));
    const { onCommand } = mount(film());
    flip();
    expect((await screen.findByRole('alert')).textContent).toBe(
      'videoStudio.audio.cleanFailed Unable to decode audio data',
    );
    expect(onCommand).not.toHaveBeenCalled();
    expect(cleaning.upload).not.toHaveBeenCalled();
  });

  it('writes nothing, and uploads nothing, when it goes away while cleaning', async () => {
    let signal: AbortSignal | undefined;
    let finish: (file: File) => void = () => undefined;
    cleaning.cleanedFile.mockImplementation(
      (_url, _name, given) =>
        new Promise((resolve) => {
          signal = given;
          finish = resolve;
        }),
    );
    const { onCommand, unmount } = mount(film());
    flip();
    await waitFor(() => {
      expect(signal).toBeDefined();
    });
    unmount();
    expect(signal?.aborted).toBe(true);
    await act(async () => {
      finish(new File(['ogg'], 'x.ogg', { type: 'audio/ogg' }));
      await Promise.resolve();
    });
    expect(cleaning.upload).not.toHaveBeenCalled();
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('deletes the copy it uploaded when it went away during the upload', async () => {
    let finish: (id: string) => void = () => undefined;
    cleaning.upload.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const { onCommand, unmount } = mount(film());
    flip();
    await waitFor(() => {
      expect(cleaning.upload).toHaveBeenCalledOnce();
    });
    unmount();
    await act(async () => {
      finish('v-clean');
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(cleaning.remove).toHaveBeenCalledWith('v-clean');
    });
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('offers nothing for a clip whose source has no sound', () => {
    const base = film();
    const silent: VideoProject = {
      ...base,
      sources: base.sources.map((source) =>
        source.id === 'src-a' ? { ...source, hasAudio: false } : source,
      ),
    };
    render(
      <NoiseReductionSwitch
        project={silent}
        target={{ kind: 'clip', id: 'clip-1', sourceId: 'src-a', on: false }}
        onCommand={vi.fn()}
      />,
    );
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it('turns a clip’s noise reduction on through the clip’s own edit', async () => {
    const edits: Edit[] = [];
    const base = film();
    render(
      <NoiseReductionSwitch
        project={base}
        target={{ kind: 'clip', id: 'clip-1', sourceId: 'src-a', on: false }}
        onCommand={(edit) => edits.push(edit)}
      />,
    );
    flip();
    await waitFor(() => {
      expect(edits).toHaveLength(1);
    });
    const next = edits[0]?.(base);
    expect(next?.video[0]?.denoise).toBe(true);
    expect(next?.sources[0]?.denoisedAssetId).toBe('v-clean');
  });
});
