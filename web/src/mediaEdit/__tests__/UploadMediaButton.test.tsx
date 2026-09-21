import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { OpenEditorContext } from '../mediaEditorContext';
import { UploadMediaButton } from '../UploadMediaButton';

const { finalizeAsset, presignAsset } = vi.hoisted(() => ({
  presignAsset: vi.fn(),
  finalizeAsset: vi.fn(),
}));
const putWithProgress = vi.hoisted(() => vi.fn());

vi.mock('../../chat/attachments/api', () => ({ finalizeAsset, presignAsset }));
vi.mock('../../chat/attachments/upload', () => ({ putWithProgress }));

function mount(open = vi.fn()) {
  render(
    <OpenEditorContext.Provider value={open}>
      <UploadMediaButton />
    </OpenEditorContext.Provider>,
  );
  return open;
}

describe('UploadMediaButton', () => {
  beforeEach(() => {
    presignAsset.mockReset();
    finalizeAsset.mockReset();
    putWithProgress.mockReset();
  });

  it('uploads an image to the library and opens it in the photo editor', async () => {
    presignAsset.mockResolvedValue({
      asset: { id: 'uploaded-image' },
      upload: { upload_url: '/put', required_headers: {} },
    });
    putWithProgress.mockResolvedValue(undefined);
    finalizeAsset.mockResolvedValue({ id: 'uploaded-image' });
    const open = mount();

    fireEvent.change(screen.getByLabelText('Choose an image or video to edit'), {
      target: { files: [new File(['pixels'], 'photo.png', { type: 'image/png' })] },
    });

    await waitFor(() => {
      expect(presignAsset).toHaveBeenCalledWith({
        thread_id: '',
        scope: 'library',
        file_name: 'photo.png',
        mime_type: 'image/png',
        size_bytes: 6,
        modality_hint: 'image',
      });
    });
    expect(finalizeAsset).toHaveBeenCalledWith('uploaded-image');
    expect(open).toHaveBeenCalledWith({ assetId: 'uploaded-image', kind: 'image' });
  });

  it('opens a supported video in the video editor', async () => {
    presignAsset.mockResolvedValue({
      asset: { id: 'uploaded-video' },
      upload: { upload_url: '/put', required_headers: {} },
    });
    putWithProgress.mockResolvedValue(undefined);
    finalizeAsset.mockResolvedValue({ id: 'uploaded-video' });
    const open = mount();

    fireEvent.change(screen.getByLabelText('Choose an image or video to edit'), {
      target: { files: [new File(['clip'], 'clip.webm', { type: 'video/webm' })] },
    });

    await waitFor(() => {
      expect(open).toHaveBeenCalledWith({ assetId: 'uploaded-video', kind: 'video' });
    });
  });

  it('refuses a format neither editor supports before uploading it', async () => {
    mount();

    fireEvent.change(screen.getByLabelText('Choose an image or video to edit'), {
      target: { files: [new File(['gif'], 'loop.gif', { type: 'image/gif' })] },
    });

    expect((await screen.findByRole('alert')).textContent).toContain(
      'Choose a PNG, JPEG, WebP, MP4, or WebM file.',
    );
    expect(presignAsset).not.toHaveBeenCalled();
  });
});
