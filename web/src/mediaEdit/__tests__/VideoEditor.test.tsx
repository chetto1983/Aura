import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n';
import type { Asset } from '../../chat/attachments/types';

const media = vi.hoisted(() => ({
  probeVideo: vi.fn(),
  filmstrip: vi.fn(
    (_source: Blob, _count: number, _height: number, _signal?: AbortSignal) =>
      Promise.resolve([]) as Promise<CanvasImageSource[]>,
  ),
  exportVideo: vi.fn(),
}));
vi.mock('../videoMedia', () => media);
// The real hook always runs; `pending` withholds its URL to show what the editor draws before
// the URL exists, which the async probe otherwise hides.
const objectUrl = vi.hoisted(() => ({ pending: false }));
vi.mock('../useObjectUrl', async (importOriginal) => {
  const real = await importOriginal<typeof import('../useObjectUrl')>();
  return {
    useObjectUrl: (blob: Blob) => {
      const url = real.useObjectUrl(blob);
      return objectUrl.pending ? undefined : url;
    },
  };
});
const downloadBlob = vi.hoisted(() => vi.fn());
vi.mock('../download', () => ({ downloadBlob }));

const { default: VideoEditor } = await import('../VideoEditor');

const ASSET: Asset = {
  id: 'asset-clip',
  status: 'complete',
  modality: 'video',
  file_name: 'clip.mp4',
  mime_type: 'video/mp4',
  declared_size_bytes: 1000,
  size_bytes: 1000,
};

beforeEach(() => {
  objectUrl.pending = false;
  Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:source'), revokeObjectURL: vi.fn() });
  media.probeVideo.mockResolvedValue({ duration: 10, width: 1280, height: 720, hasAudio: true });
  media.exportVideo.mockResolvedValue({
    kind: 'done',
    blob: new Blob(['x'], { type: 'video/mp4' }),
  });
  downloadBlob.mockReset();
});

async function mount(onClose = vi.fn()) {
  const view = render(<VideoEditor asset={ASSET} source={new Blob()} onClose={onClose} />);
  await screen.findByRole('slider', { name: i18n.t('mediaEdit.video.startHandle') });
  return view;
}

/** An export that runs until its signal aborts, as exportVideo does; returns the signal it got. */
function pendingExport(): () => AbortSignal | undefined {
  let signal: AbortSignal | undefined;
  media.exportVideo.mockImplementation(
    (_s: Blob, _m: string, _e: unknown, _p: unknown, abort: AbortSignal) =>
      new Promise((resolve) => {
        signal = abort;
        abort.addEventListener('abort', () => {
          resolve({ kind: 'canceled' });
        });
      }),
  );
  return () => signal;
}

describe('VideoEditor', () => {
  it('draws the preview only once the clip has a URL', async () => {
    objectUrl.pending = true;
    await mount();
    expect(screen.getByRole('status').textContent).toBe('Opening the file…');
    expect(document.querySelector('video')).toBeNull();
  });

  it('saves the whole clip untouched by default and downloads it', async () => {
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), 'clip-edited.mp4');
    });
    expect(media.exportVideo).toHaveBeenCalledWith(
      expect.any(Blob),
      'video/mp4',
      { start: 0, end: 10, rotation: 0, mute: false },
      expect.any(Function),
      expect.any(AbortSignal),
    );
  });

  it('sends the typed range', async () => {
    await mount();
    const start = screen.getByLabelText('Start');
    fireEvent.change(start, { target: { value: '00:02.5' } });
    fireEvent.blur(start);
    const end = screen.getByLabelText('End');
    fireEvent.change(end, { target: { value: '6' } });
    fireEvent.blur(end);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({ start: 2.5, end: 6 });
    });
  });

  it('exports the tenth a typed bound shows, not the digits past it', async () => {
    await mount();
    const start = screen.getByLabelText('Start');
    fireEvent.change(start, { target: { value: '3.25' } });
    fireEvent.blur(start);
    expect(screen.getByLabelText('Start')).toHaveProperty('value', '00:03.3');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.lastCall?.[2]).toMatchObject({ start: 3.3, end: 10 });
    });
  });

  it('crops to a centred square in the rotated frame', async () => {
    await mount();
    fireEvent.click(screen.getByRole('radio', { name: 'Crop' }));
    fireEvent.click(screen.getByRole('button', { name: '1:1' }));
    expect(screen.getByText('720 × 720')).toBeTruthy();
    fireEvent.click(screen.getByRole('radio', { name: 'Rotate' }));
    fireEvent.click(screen.getByRole('button', { name: 'Rotate 90° right' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({
        rotation: 90,
        crop: { left: 0, top: 280, width: 720, height: 720 },
      });
    });
  });

  it('turns left from zero to 270', async () => {
    await mount();
    fireEvent.click(screen.getByRole('radio', { name: 'Rotate' }));
    fireEvent.click(screen.getByRole('button', { name: 'Rotate 90° left' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({ rotation: 270 });
    });
  });

  it('cannot mute a silent clip', async () => {
    media.probeVideo.mockResolvedValue({ duration: 10, width: 1280, height: 720, hasAudio: false });
    await mount();
    fireEvent.click(screen.getByRole('radio', { name: 'Audio' }));
    expect(screen.getByRole('switch', { name: 'Remove audio' })).toHaveProperty('disabled', true);
    expect(screen.getByText('This clip has no audio')).toBeTruthy();
  });

  // The select's own class reached only the <select>; its wrapper kept drawing the chevron next
  // to the wide-screen tool row (seen in every desktop screenshot of the live check, 2026-09-19).
  it('hides the narrow-screen tool list with its chevron, not just the select', async () => {
    await mount();
    const select = screen.getByRole('combobox', { name: 'Tools' });
    const wrapper = select.closest('[data-slot="native-select-wrapper"]');
    const hidden = document.querySelector('.sm\\:hidden');

    expect(wrapper).toBeTruthy();
    expect(hidden?.contains(wrapper as Node)).toBe(true);
  });

  it('drops the audio when asked, from the narrow-screen tool list too', async () => {
    await mount();
    fireEvent.change(screen.getByRole('combobox', { name: 'Tools' }), {
      target: { value: 'audio' },
    });
    fireEvent.click(screen.getByRole('switch', { name: 'Remove audio' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({ mute: true });
    });
  });

  it('puts every edit back on Reset', async () => {
    await mount();
    fireEvent.keyDown(screen.getByRole('slider', { name: 'End of the selection' }), {
      key: 'ArrowLeft',
      shiftKey: true,
    });
    fireEvent.click(screen.getByRole('radio', { name: 'Crop' }));
    fireEvent.click(screen.getByRole('button', { name: '9:16' }));
    fireEvent.click(screen.getByRole('radio', { name: 'Rotate' }));
    fireEvent.click(screen.getByRole('button', { name: 'Rotate 90° right' }));
    fireEvent.click(screen.getByRole('radio', { name: 'Audio' }));
    fireEvent.click(screen.getByRole('switch', { name: 'Remove audio' }));
    fireEvent.click(screen.getByRole('button', { name: 'Reset' }));
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(media.exportVideo.mock.calls[0]?.[2]).toEqual({
        start: 0,
        end: 10,
        rotation: 0,
        mute: false,
      });
    });
  });

  it('plays the selection and stops at its end', async () => {
    const play = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
    const pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockReturnValue(undefined);
    await mount();
    const start = screen.getByLabelText('Start');
    fireEvent.change(start, { target: { value: '3' } });
    fireEvent.blur(start);
    fireEvent.click(screen.getByRole('button', { name: 'Play the selection' }));
    const video = document.querySelector('video');
    if (video === null) throw new Error('the preview has no video element');
    expect(video.currentTime).toBe(3);
    expect(play).toHaveBeenCalledTimes(1);
    video.currentTime = 9.9;
    fireEvent.timeUpdate(video);
    expect(pause).not.toHaveBeenCalled();
    video.currentTime = 10;
    fireEvent.timeUpdate(video);
    expect(pause).toHaveBeenCalledTimes(1);
    play.mockRestore();
    pause.mockRestore();
  });

  it('names the track the browser cannot process and downloads nothing', async () => {
    media.exportVideo.mockResolvedValue({
      kind: 'blocked',
      tracks: [{ type: 'video', codec: 'hevc', reason: 'undecodable_source_codec' }],
    });
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'This browser cannot process the video track (hevc). Try Chrome or Edge.',
    );
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('words a track without a declared codec in the operator language', async () => {
    media.exportVideo.mockResolvedValue({
      kind: 'blocked',
      tracks: [{ type: 'audio', codec: null, reason: 'undecodable_source_codec' }],
    });
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'This browser cannot process the audio track (unknown codec). Try Chrome or Edge.',
    );
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('shows why an export failed and saves again afterwards', async () => {
    media.exportVideo.mockRejectedValueOnce(new Error('encoder closed'));
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect((await screen.findByRole('alert')).textContent).toBe(
      'The export failed: encoder closed',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(downloadBlob).toHaveBeenCalledTimes(1);
    });
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it.each([
    ['en', 'The export produced an empty file.'],
    ['it', "L'esportazione ha prodotto un file vuoto."],
  ])('words an empty export in %s and downloads nothing', async (language, sentence) => {
    media.exportVideo.mockResolvedValue({ kind: 'empty' });
    await i18n.changeLanguage(language);
    try {
      await mount();
      fireEvent.click(screen.getByRole('button', { name: i18n.t('mediaEdit.video.save') }));
      expect((await screen.findByRole('alert')).textContent).toBe(sentence);
      expect(downloadBlob).not.toHaveBeenCalled();
    } finally {
      await i18n.changeLanguage('en');
    }
  });

  it('says so when the clip cannot be opened', async () => {
    media.probeVideo.mockRejectedValue(new Error('the file has no video track'));
    render(<VideoEditor asset={ASSET} source={new Blob()} onClose={vi.fn()} />);
    expect((await screen.findByRole('alert')).textContent).toBe('The file could not be opened.');
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
  });

  it('keeps the timeline when the filmstrip cannot be drawn', async () => {
    media.filmstrip.mockRejectedValueOnce(new Error('decoder closed'));
    await mount();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByRole('button', { name: 'Save' })).toHaveProperty('disabled', false);
  });

  it('draws the export progress as a bar a screen reader can read', async () => {
    let report: (fraction: number) => void = () => undefined;
    media.exportVideo.mockImplementation(
      (_s: Blob, _m: string, _e: unknown, onProgress: (fraction: number) => void) => {
        report = onProgress;
        return new Promise(() => undefined);
      },
    );
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    const bar = await screen.findByRole('progressbar', { name: 'Export progress' });
    expect(bar.getAttribute('aria-valuemin')).toBe('0');
    expect(bar.getAttribute('aria-valuemax')).toBe('100');
    expect(bar.getAttribute('aria-valuenow')).toBe('0');
    act(() => {
      report(0.424);
    });
    expect(bar.getAttribute('aria-valuenow')).toBe('42');
    expect(bar.querySelector<HTMLElement>('[data-progress-fill]')?.style.width).toBe('42%');
  });

  it('cancels a running export', async () => {
    const signal = pendingExport();
    await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(signal()?.aborted).toBe(true);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).toHaveProperty('disabled', false);
    });
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('stops a running export when the editor is closed', async () => {
    const signal = pendingExport();
    const onClose = vi.fn();
    await mount(onClose);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByRole('button', { name: 'Cancel' });
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(signal()?.aborted).toBe(true);
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();
    });
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('stops a running export when the editor goes away', async () => {
    const signal = pendingExport();
    const { unmount } = await mount();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByRole('button', { name: 'Cancel' });
    unmount();
    expect(signal()?.aborted).toBe(true);
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('stops reading the clip when the editor goes away', () => {
    media.probeVideo.mockReturnValueOnce(new Promise(() => undefined));
    media.filmstrip.mockReturnValueOnce(new Promise(() => undefined));
    const { unmount } = render(<VideoEditor asset={ASSET} source={new Blob()} onClose={vi.fn()} />);
    const probing = media.probeVideo.mock.lastCall?.[1] as AbortSignal | undefined;
    const drawing = media.filmstrip.mock.lastCall?.[3];
    expect(probing?.aborted).toBe(false);
    expect(drawing?.aborted).toBe(false);
    unmount();
    expect(probing?.aborted).toBe(true);
    expect(drawing?.aborted).toBe(true);
  });

  it('says so when the clip refuses to play, but not when a pause interrupts it', async () => {
    const play = vi
      .spyOn(HTMLMediaElement.prototype, 'play')
      .mockRejectedValueOnce(new DOMException('interrupted by pause()', 'AbortError'))
      .mockRejectedValueOnce(new DOMException('autoplay blocked', 'NotAllowedError'));
    await mount();
    const button = screen.getByRole('button', { name: 'Play the selection' });
    fireEvent.click(button);
    await waitFor(() => {
      expect(play).toHaveBeenCalledTimes(1);
    });
    expect(screen.queryByRole('alert')).toBeNull();
    fireEvent.click(button);
    expect((await screen.findByRole('alert')).textContent).toBe('The clip could not play.');
    play.mockRestore();
  });

  describe('on a clip shorter than the shortest cut', () => {
    beforeEach(() => {
      media.probeVideo.mockResolvedValue({ duration: 0.05, width: 64, height: 64, hasAudio: true });
    });

    function type(label: string, value: string) {
      const field = screen.getByLabelText(label);
      fireEvent.change(field, { target: { value } });
      fireEvent.blur(field);
    }

    it('never lets a typed start go below zero', async () => {
      await mount();
      type('Start', '1');
      fireEvent.click(screen.getByRole('button', { name: 'Save' }));
      await waitFor(() => {
        expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({ start: 0, end: 0.05 });
      });
    });

    it('never lets a typed end pass the end of the clip', async () => {
      await mount();
      type('End', '5');
      type('Start', '1');
      fireEvent.click(screen.getByRole('button', { name: 'Save' }));
      await waitFor(() => {
        expect(media.exportVideo.mock.calls[0]?.[2]).toMatchObject({ start: 0, end: 0.05 });
      });
    });
  });
});
