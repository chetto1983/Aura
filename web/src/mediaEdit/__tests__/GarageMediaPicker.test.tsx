import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { OpenEditorContext } from '../mediaEditorContext';
import { GarageMediaPicker } from '../GarageMediaPicker';

vi.mock('../../files/FilesWorkspace', () => ({
  default: ({
    onOpenFile,
  }: {
    onOpenFile: (file: { id: string; name: string; size: number; assetId?: string }) => void;
  }) => (
    <div>
      <button
        type="button"
        onClick={() => {
          onOpenFile({ id: '/media/clip.mp4', name: 'clip.mp4', size: 4 });
        }}
      >
        choose clip
      </button>
      <button
        type="button"
        onClick={() => {
          onOpenFile({ id: '/media/loop.gif', name: 'loop.gif', size: 3 });
        }}
      >
        choose gif
      </button>
      <button
        type="button"
        onClick={() => {
          onOpenFile({
            id: '/media/7d1c.mp4',
            name: 'videoplayback.mp4',
            size: 13,
            assetId: '7d1c',
          });
        }}
      >
        choose library clip
      </button>
    </div>
  ),
}));

function mount(open = vi.fn()) {
  render(
    <OpenEditorContext.Provider value={open}>
      <GarageMediaPicker />
    </OpenEditorContext.Provider>,
  );
  return open;
}

describe('GarageMediaPicker', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('opens the Garage library and hands the chosen video to the editor', async () => {
    const open = mount();

    fireEvent.click(screen.getByRole('button', { name: 'Open Garage library' }));
    expect(screen.getByRole('dialog', { name: 'Choose from Garage' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'choose clip' }));

    await waitFor(() => {
      expect(open).toHaveBeenCalledWith({
        garageObjectId: '/media/clip.mp4',
        fileName: 'clip.mp4',
        sizeBytes: 4,
        kind: 'video',
      });
    });
    expect(screen.queryByRole('dialog', { name: 'Choose from Garage' })).toBeNull();
  });

  it('keeps the library open when the selected format is not editable', async () => {
    const open = mount();
    fireEvent.click(screen.getByRole('button', { name: 'Open Garage library' }));
    fireEvent.click(screen.getByRole('button', { name: 'choose gif' }));

    expect((await screen.findByRole('alert')).textContent).toContain(
      'Choose a PNG, JPEG, WebP, MP4, or WebM file.',
    );
    expect(open).not.toHaveBeenCalled();
    expect(screen.getByRole('dialog', { name: 'Choose from Garage' })).toBeTruthy();
  });
});

describe('GarageMediaPicker on a library file', () => {
  // Opening a library file as a bucket object made the editor upload it as a new asset, and
  // that copy was itself a library file: every open added one more twin.
  it('opens a file that is already an asset as that asset, so nothing is uploaded again', async () => {
    const open = mount();
    fireEvent.click(screen.getByRole('button', { name: 'Open Garage library' }));
    fireEvent.click(screen.getByRole('button', { name: 'choose library clip' }));

    await waitFor(() => {
      expect(open).toHaveBeenCalledWith({ assetId: '7d1c', kind: 'video' });
    });
  });
});
