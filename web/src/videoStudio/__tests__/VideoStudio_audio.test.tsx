import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n';
import type { VideoProject } from '../project';

// The workspace with sounds in it, on the real bundle like VideoStudio.test.tsx: a sound picked
// through the audio door lands on a lane at the playhead, a sound nobody can hang is refused before
// its bytes are sent, and Split with a sound selected cuts the sound, not the film.

vi.mock('@videoflow/renderer-dom', () => ({
  default: class {
    loadedFonts: Record<string, string> = {};
    loadFont(): Promise<void> {
      return Promise.resolve();
    }
    loadVideo(): Promise<void> {
      return Promise.resolve();
    }
    seek(): Promise<void> {
      return Promise.resolve();
    }
    destroy(): void {
      // Nothing to release: the renderer is a stand-in.
    }
  },
}));

vi.mock('@videoflow/renderer-browser', () => ({
  default: class {
    loadedFonts: Record<string, string> = {};
  },
}));

const media = vi.hoisted(() => ({ probeVideo: vi.fn(), probeAudio: vi.fn() }));
vi.mock('../../mediaEdit/videoMedia', () => media);

const assets = vi.hoisted(() => ({
  presignAsset: vi.fn(),
  finalizeAsset: vi.fn(),
  finalizeMediaAsset: vi.fn(),
}));
vi.mock('../../chat/attachments/api', () => assets);
vi.mock('../../chat/attachments/upload', () => ({ putWithProgress: () => Promise.resolve() }));

const speech = vi.hoisted(() => ({
  audio: vi.fn(() =>
    Promise.resolve({ blob: new Blob(['mp3'], { type: 'audio/mpeg' }), truncated: false }),
  ),
}));
vi.mock('../../chat/voice/voiceApi', () => ({ synthesizeSpeechAudio: speech.audio }));
vi.mock('../../chat/voice/useVoiceCapabilities', () => ({
  useVoiceCapabilities: () => ({ tts: true, stt: false }),
}));
// jsdom has no canvas and no microphone: the recorder is AudioRecorder.test.tsx's to judge.
vi.mock('../AudioRecorder', () => ({ AudioRecorder: () => null }));
// No Web Audio either: the speech detector is audioSpeech.test.ts's to judge.
const hearing = vi.hoisted(() => ({
  detect: vi.fn<(url: string, signal?: AbortSignal) => Promise<(readonly [number, number])[]>>(),
}));
vi.mock('../audioSpeech', () => ({ detectSpeech: hearing.detect }));
// The library's list is VideoStudio_library.test.tsx's to judge; here, what a pick does.
const library = vi.hoisted(() => ({
  audio: { id: 'lib-sound', file_name: 'river.wav', mime_type: 'audio/wav' },
  video: { id: 'lib-clip', file_name: 'beach.mp4', mime_type: 'video/mp4' },
}));
vi.mock('../VideoStudio_library', () => ({
  LibraryPicker: ({
    modalities,
    onPick,
  }: {
    modalities: readonly string[];
    onPick: (asset: typeof library.audio) => void;
  }) => (
    <button
      type="button"
      onClick={() => {
        onPick(modalities.includes('audio') ? library.audio : library.video);
      }}
    >
      {`library of ${modalities.join(' and ')}`}
    </button>
  ),
}));

const { default: VideoStudio } = await import('../VideoStudio');

function film(withSound = false): VideoProject {
  return {
    id: 'p',
    name: 'demo',
    size: { width: 1920, height: 1080 },
    fps: 25,
    sources: [
      {
        id: 'src-a',
        assetId: 'asset-a',
        kind: 'video',
        duration: 20,
        size: { width: 1920, height: 1080 },
      },
      {
        id: 'src-m',
        assetId: 'asset-m',
        kind: 'audio',
        duration: 8,
        size: { width: 0, height: 0 },
      },
    ],
    video: [
      { id: 'clip-1', sourceId: 'src-a', duration: 4, sourceStart: 0, muted: false },
      { id: 'clip-2', sourceId: 'src-a', duration: 4, sourceStart: 4, muted: false },
    ],
    overlays: [],
    ...(withSound
      ? {
          audio: [
            {
              id: 'lane',
              items: [
                {
                  id: 'bed',
                  sourceId: 'src-m',
                  anchor: { clipId: 'clip-1', offset: 0 },
                  sourceStart: 0,
                  duration: 8,
                  volume: 1,
                  muted: false,
                },
              ],
            },
          ],
        }
      : {}),
  };
}

function mount(project: VideoProject): void {
  render(<VideoStudio open={{ kind: 'project', project }} onClose={vi.fn()} />);
}

function sound(index: number): Promise<HTMLElement> {
  return screen.findByRole('button', { name: i18n.t('videoStudio.audio.item', { index }) });
}

async function pickSound(): Promise<void> {
  fireEvent.change(await screen.findByLabelText(i18n.t('videoStudio.audio.pick')), {
    target: { files: [new File(['x'], 'bed.wav', { type: 'audio/wav' })] },
  });
}

beforeEach(() => {
  media.probeAudio.mockResolvedValue({ duration: 6, decodable: true });
  assets.presignAsset.mockResolvedValue({
    asset: { id: 'sound-asset' },
    upload: { upload_url: 'u', required_headers: {} },
  });
  assets.finalizeMediaAsset.mockResolvedValue({ id: 'sound-asset' });
  hearing.detect.mockResolvedValue([[1, 2]]);
});

afterEach(() => {
  vi.clearAllMocks();
  vi.unstubAllGlobals();
});

/** Every asset the editor reads answers as the asset route does for ALL assets: an
 *  application/octet-stream attachment (the D-10 stored-XSS guard), whatever it holds. */
function serveAsTheAssetRoute(): void {
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve(
        new Response('x', { headers: { 'Content-Type': 'application/octet-stream' } }),
      ),
    ),
  );
}

describe('VideoStudio, with sounds', () => {
  it('puts a picked sound on an audio lane at the playhead, named after its file, and selects it', async () => {
    mount(film());
    await pickSound();
    expect((await sound(1)).textContent).toContain('bed.wav');
    expect(assets.presignAsset).toHaveBeenCalledWith(
      expect.objectContaining({ modality_hint: 'audio' }),
    );
    expect(
      screen
        .getByRole('tab', { name: i18n.t('videoStudio.inspector.tabs.audio') })
        .getAttribute('aria-selected'),
    ).toBe('true');
  });

  it('refuses a sound before uploading it when there is no film to hang it on', async () => {
    mount({ ...film(), video: [] });
    await pickSound();
    expect((await screen.findByRole('alert')).textContent).toBe(
      i18n.t('videoStudio.audio.refusal.noClip'),
    );
    expect(assets.presignAsset).not.toHaveBeenCalled();
  });

  it('splits the selected sound at the playhead and leaves the clips alone', async () => {
    mount(film(true));
    fireEvent.pointerDown(await sound(1));
    const playhead = screen.getByRole('slider', { name: i18n.t('videoStudio.timeline.playhead') });
    fireEvent.keyDown(playhead, { key: 'ArrowRight', shiftKey: true });
    fireEvent.keyDown(playhead, { key: 'ArrowRight', shiftKey: true });
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.command.split') }));
    expect(await sound(2)).toBeTruthy();
    expect(
      screen.queryByRole('button', { name: i18n.t('videoStudio.timeline.clip', { index: 3 }) }),
    ).toBeNull();
  });

  it('opens the audio panel from the rail, whose Upload audio opens the picker', async () => {
    mount(film());
    const picker = await screen.findByLabelText(i18n.t('videoStudio.audio.pick'));
    const click = vi.spyOn(picker, 'click');
    // The first clip is selected on open, so the phone bar shows its tools, not its add actions:
    // the one Add audio on screen is the rail's.
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.audio.add') }));
    const panel = await screen.findByRole('dialog', {
      name: i18n.t('videoStudio.audio.panel.title'),
    });
    fireEvent.click(
      within(panel).getByRole('button', { name: i18n.t('videoStudio.audio.panel.upload') }),
    );
    expect(click).toHaveBeenCalledOnce();
    expect(
      screen.queryByRole('dialog', { name: i18n.t('videoStudio.audio.panel.title') }),
    ).toBeNull();
  });

  it('puts a sound picked from the library on a lane, read where it is stored, never uploaded', async () => {
    serveAsTheAssetRoute();
    mount(film());
    fireEvent.click(await screen.findByRole('button', { name: i18n.t('videoStudio.audio.add') }));
    const panel = await screen.findByRole('dialog', {
      name: i18n.t('videoStudio.audio.panel.title'),
    });
    fireEvent.click(within(panel).getByRole('button', { name: 'library of audio' }));
    expect((await sound(1)).textContent).toContain('river.wav');
    expect(fetch).toHaveBeenCalledWith('/api/assets/lib-sound/download', expect.anything());
    expect(assets.presignAsset).not.toHaveBeenCalled();
  });

  it('opens the clip panel from Add a clip: its upload opens the picker, a library clip comes next', async () => {
    serveAsTheAssetRoute();
    media.probeVideo.mockResolvedValue({
      duration: 5,
      width: 1920,
      height: 1080,
      hasAudio: false,
      decodable: true,
    });
    mount(film());
    const picker = await screen.findByLabelText(i18n.t('videoStudio.source.pick'));
    const click = vi.spyOn(picker, 'click');
    const openPanel = async () => {
      fireEvent.click(
        screen.getByRole('button', { name: i18n.t('videoStudio.command.addSource') }),
      );
      return screen.findByRole('dialog', { name: i18n.t('videoStudio.clipPanel.title') });
    };
    fireEvent.click(
      within(await openPanel()).getByRole('button', {
        name: i18n.t('videoStudio.clipPanel.upload'),
      }),
    );
    expect(click).toHaveBeenCalledOnce();
    fireEvent.click(
      within(await openPanel()).getByRole('button', { name: 'library of video and image' }),
    );
    expect(
      await screen.findByRole('button', {
        name: i18n.t('videoStudio.timeline.clip', { index: 3 }),
      }),
    ).toBeTruthy();
    expect(assets.presignAsset).not.toHaveBeenCalled();
  });

  it('puts a text read aloud at the playhead, named after the text', async () => {
    mount(film());
    fireEvent.click(await screen.findByRole('button', { name: i18n.t('videoStudio.audio.add') }));
    const panel = await screen.findByRole('dialog', {
      name: i18n.t('videoStudio.audio.panel.title'),
    });
    fireEvent.change(within(panel).getByLabelText(i18n.t('videoStudio.audio.panel.speech')), {
      target: { value: 'The river runs.' },
    });
    fireEvent.click(
      within(panel).getByRole('button', { name: i18n.t('videoStudio.audio.panel.speak') }),
    );
    expect((await sound(1)).textContent).toContain('The river runs.');
    expect(assets.presignAsset).toHaveBeenCalledWith(
      expect.objectContaining({ file_name: 'speech.mp3', mime_type: 'audio/mpeg' }),
    );
  });
});

/** The film with its clips muted and its bed ducking: nothing it goes under yet. */
function duckingFilm(): VideoProject {
  const base = film(true);
  return {
    ...base,
    video: base.video.map((clip) => ({ ...clip, muted: true })),
    audio: (base.audio ?? []).map((lane) => ({
      ...lane,
      items: lane.items.map((item) => ({ ...item, ducking: { amountDb: -12, ramp: 0.5 } })),
    })),
  };
}

function exportButton(): HTMLElement {
  return screen.getByRole('button', { name: i18n.t('videoStudio.export.action') });
}

describe('VideoStudio, ducking by itself', () => {
  it('listens to a sound added under a ducking bed, says so wherever the operator is, and holds the export until it has heard it', async () => {
    let answer: (speech: (readonly [number, number])[]) => void = () => undefined;
    hearing.detect.mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = resolve;
        }),
    );
    mount(duckingFilm());
    await pickSound();
    await sound(2);
    // The new sound is selected, and it does not duck: the status line is where this is said.
    expect(await screen.findByText(i18n.t('videoStudio.audio.listening'))).toBeTruthy();
    expect(hearing.detect).toHaveBeenCalledWith(
      '/api/assets/sound-asset/download',
      expect.any(AbortSignal),
    );
    expect(exportButton()).toHaveProperty('disabled', true);
    expect(exportButton().getAttribute('title')).toBe(i18n.t('videoStudio.audio.exportListening'));
    await act(async () => {
      answer([[1, 2]]);
      await Promise.resolve();
    });
    await waitFor(() => {
      expect(screen.queryByText(i18n.t('videoStudio.audio.listening'))).toBeNull();
    });
    expect(exportButton()).toHaveProperty('disabled', false);
  });

  it('keeps what it heard out of the undo: one undo takes the sound back, and a redo brings it back heard', async () => {
    mount(duckingFilm());
    await pickSound();
    await sound(2);
    await waitFor(() => {
      expect(exportButton()).toHaveProperty('disabled', false);
    });
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.command.undo') }));
    await waitFor(() => {
      expect(
        screen.queryByRole('button', { name: i18n.t('videoStudio.audio.item', { index: 2 }) }),
      ).toBeNull();
    });
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.command.redo') }));
    expect(await sound(2)).toBeTruthy();
    await waitFor(() => {
      expect(exportButton()).toHaveProperty('disabled', false);
    });
    expect(hearing.detect).toHaveBeenCalledOnce();
  });

  it('says why it could not hear a sound, holds the export, and tries again from the bed’s control', async () => {
    hearing.detect.mockRejectedValueOnce(new Error('Unable to decode audio data'));
    mount(duckingFilm());
    await pickSound();
    await sound(2);
    const failed = i18n.t('videoStudio.audio.listenFailed', {
      reason: 'Unable to decode audio data',
    });
    expect(await screen.findByText(failed)).toBeTruthy();
    expect(exportButton().getAttribute('title')).toBe(
      i18n.t('videoStudio.audio.exportUnheard', { reason: 'Unable to decode audio data' }),
    );
    fireEvent.pointerDown(await sound(1));
    fireEvent.click(
      await screen.findByRole('button', { name: i18n.t('videoStudio.audio.listenAgain') }),
    );
    await waitFor(() => {
      expect(exportButton()).toHaveProperty('disabled', false);
    });
    expect(screen.queryByText(failed)).toBeNull();
    expect(hearing.detect).toHaveBeenCalledTimes(2);
  });
});
