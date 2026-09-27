import { fireEvent, render, screen } from '@testing-library/react';
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
});

afterEach(() => {
  vi.clearAllMocks();
});

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

  it('offers Add audio on the rail, which opens the audio picker', async () => {
    mount(film());
    const picker = await screen.findByLabelText(i18n.t('videoStudio.audio.pick'));
    const click = vi.spyOn(picker, 'click');
    // The first clip is selected on open, so the phone bar shows its tools, not its add actions:
    // the one Add audio on screen is the rail's.
    fireEvent.click(screen.getByRole('button', { name: i18n.t('videoStudio.audio.add') }));
    expect(click).toHaveBeenCalledOnce();
  });
});
