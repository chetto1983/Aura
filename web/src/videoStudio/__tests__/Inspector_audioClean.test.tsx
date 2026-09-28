import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { DuckingControls, NoiseReductionSwitch } from '../Inspector_audioClean';
import type { AudioItem, VideoProject } from '../project';
import { DuckingListeningContext, type DuckingListening } from '../VideoStudio_ducking';

// The analysing controls — Noise reduction and ducking — with the cleaning, the upload and the speech
// detector stood in for. What is judged is the ONE edit each makes — the analysis recorded and the
// setting changed together, one undo step — and that an analysis already made is never made again
// (Review Focus 3).

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

const hearing = vi.hoisted(() => ({
  detect: vi.fn<(url: string, signal?: AbortSignal) => Promise<(readonly [number, number])[]>>(),
}));
vi.mock('../audioSpeech', () => ({ detectSpeech: hearing.detect }));

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
      target={{ kind: 'sound', id: 'voice', sourceId: 'src-v', label: 'voice.mp3', on }}
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
  hearing.detect.mockReset();
  hearing.detect.mockResolvedValue([[1, 2]]);
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
    // Named after the sound, not after the project's internal id: the copy lands in the library.
    expect(cleaning.cleanedFile).toHaveBeenCalledWith(
      '/api/assets/v/download',
      'voice.mp3',
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
    // A clip carries no name of its own in the project: its copy is named after what it is.
    expect(cleaning.cleanedFile).toHaveBeenCalledWith(
      '/api/assets/a/download',
      'clip',
      expect.any(AbortSignal),
    );
  });
});

/** The ducking controls, under the workspace's own listening as `listening` describes it. */
function mountDucking(project: VideoProject, listening: Partial<DuckingListening> = {}) {
  const edits: Edit[] = [];
  const item = project.audio?.[0]?.items[0];
  if (item === undefined) throw new Error('fixture');
  const view = render(
    <DuckingListeningContext
      value={{
        listening: false,
        failure: undefined,
        refusal: undefined,
        retry: vi.fn(),
        ...listening,
      }}
    >
      <DuckingControls
        project={project}
        item={item}
        onCommand={(edit) => {
          edits.push(edit);
        }}
      />
    </DuckingListeningContext>,
  );
  return { ...view, edits, apply: (index = 0) => edits[index]?.(project) };
}

function heard(
  project: VideoProject,
  sourceId: string,
  speech: (readonly [number, number])[],
): VideoProject {
  return {
    ...project,
    sources: project.sources.map((source) =>
      source.id === sourceId ? { ...source, speech } : source,
    ),
  };
}

describe('DuckingControls', () => {
  it('listens to the clip’s sound, then records it and turns ducking on in one edit', async () => {
    const { edits, apply } = mountDucking(film());
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    expect((await screen.findByRole('status')).textContent).toBe('videoStudio.audio.listening');
    await waitFor(() => {
      expect(edits).toHaveLength(1);
    });
    expect(hearing.detect).toHaveBeenCalledWith('/api/assets/a/download', expect.any(AbortSignal));
    const next = apply();
    expect(next?.sources.find((source) => source.id === 'src-a')?.speech).toEqual([[1, 2]]);
    expect(next?.audio?.[0]?.items[0]?.ducking).toEqual({ amountDb: -12, ramp: 0.5 });
  });

  it('records no speech as heard, so a silent film is not listened to again', async () => {
    hearing.detect.mockResolvedValue([]);
    const { apply, edits } = mountDucking(film());
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    await waitFor(() => {
      expect(edits).toHaveLength(1);
    });
    expect(apply()?.sources.find((source) => source.id === 'src-a')?.speech).toEqual([]);
  });

  it('turns on at once when everything has been heard, and with nothing to listen to', () => {
    const muted: VideoProject = {
      ...film(),
      video: film().video.map((clip) => ({ ...clip, muted: true })),
    };
    const { apply } = mountDucking(muted);
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    expect(hearing.detect).not.toHaveBeenCalled();
    expect(apply()?.audio?.[0]?.items[0]?.ducking).toEqual({ amountDb: -12, ramp: 0.5 });
  });

  it('sets the amount and the softness, and turns off', () => {
    const on = heard(film({ ducking: { amountDb: -12, ramp: 0.5 } }), 'src-a', [[1, 2]]);
    const { apply } = mountDucking(on);
    fireEvent.keyDown(
      within(screen.getByLabelText('videoStudio.audio.duckAmount')).getByRole('slider'),
      { key: 'ArrowLeft' },
    );
    expect(apply(0)?.audio?.[0]?.items[0]?.ducking).toEqual({ amountDb: -13, ramp: 0.5 });
    fireEvent.keyDown(
      within(screen.getByLabelText('videoStudio.audio.duckSoftness')).getByRole('slider'),
      { key: 'ArrowRight' },
    );
    expect(apply(1)?.audio?.[0]?.items[0]?.ducking?.ramp).toBeCloseTo(0.6, 9);
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    expect(apply(2)?.audio?.[0]?.items[0]?.ducking).toBeUndefined();
  });

  // A sound added after ducking was turned on is listened to by itself (VideoStudio_ducking.ts),
  // not offered: the control says so, and offers a second try only when that listening failed.
  it('says on its control that ducking is listening by itself to a sound it has not heard', () => {
    const { edits } = mountDucking(film({ ducking: { amountDb: -12, ramp: 0.5 } }), {
      listening: true,
    });
    expect(screen.getByRole('status').textContent).toBe('videoStudio.audio.listening');
    expect(screen.queryByRole('button', { name: 'videoStudio.audio.listenAgain' })).toBeNull();
    expect(hearing.detect).not.toHaveBeenCalled();
    expect(edits).toHaveLength(0);
  });

  it('says why ducking could not hear a sound, and listens again when asked', () => {
    const retry = vi.fn();
    mountDucking(film({ ducking: { amountDb: -12, ramp: 0.5 } }), {
      failure: 'Unable to decode audio data',
      retry,
    });
    expect(screen.getByRole('alert').textContent).toBe(
      'videoStudio.audio.listenFailed Unable to decode audio data',
    );
    fireEvent.click(screen.getByRole('button', { name: 'videoStudio.audio.listenAgain' }));
    expect(retry).toHaveBeenCalledOnce();
  });

  it('says nothing of the listening while its own sound waits on nothing', () => {
    const on = heard(film({ ducking: { amountDb: -12, ramp: 0.5 } }), 'src-a', [[1, 2]]);
    mountDucking(on, { listening: true });
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('says why listening failed and leaves ducking off', async () => {
    hearing.detect.mockRejectedValue(new Error('Unable to decode audio data'));
    const { edits } = mountDucking(film());
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'videoStudio.audio.listenFailed Unable to decode audio data',
    );
    expect(edits).toHaveLength(0);
  });

  it('writes nothing when it goes away while listening', async () => {
    let signal: AbortSignal | undefined;
    hearing.detect.mockImplementation((_url, given) => {
      signal = given;
      return new Promise(() => undefined);
    });
    const { edits, unmount } = mountDucking(film());
    fireEvent.click(screen.getByRole('switch', { name: 'videoStudio.audio.ducking' }));
    await waitFor(() => {
      expect(signal).toBeDefined();
    });
    unmount();
    expect(signal?.aborted).toBe(true);
    expect(edits).toHaveLength(0);
  });
});
