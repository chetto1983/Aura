import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n';
import type { Asset } from '../../chat/attachments/types';
import { StudioError } from '../../studio/studioApi';
import { FILEROBOT_IT } from '../filerobotSetup';

const PNG = 'data:image/png;base64,iVBORw0KGgo=';
const exportImage = vi.hoisted(() => vi.fn());
const seen = vi.hoisted(() => ({
  props: undefined as Record<string, unknown> | undefined,
  sources: [] as unknown[],
}));

vi.mock('react-filerobot-image-editor', () => ({
  TABS: {
    ADJUST: 'Adjust',
    FINETUNE: 'Finetune',
    FILTERS: 'Filters',
    ANNOTATE: 'Annotate',
    RESIZE: 'Resize',
  },
  TOOLS: { CROP: 'Crop' },
  default: function FakeFilerobot(props: {
    source: unknown;
    getCurrentImgDataFnRef: { current?: unknown };
    onModify: () => void;
  }) {
    seen.props = props;
    seen.sources.push(props.source);
    useEffect(() => {
      props.getCurrentImgDataFnRef.current = exportImage;
    });
    return (
      <button
        type="button"
        onClick={() => {
          props.onModify();
        }}
      >
        fake edit
      </button>
    );
  },
}));

const uploadStudioFrame = vi.hoisted(() => vi.fn());
vi.mock('../../studio/frameUpload', () => ({ uploadStudioFrame }));
const listStudioLibrary = vi.hoisted(() => vi.fn());
vi.mock('../../studio/studioApi', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../studio/studioApi')>()),
  listStudioLibrary,
}));
const downloadBlob = vi.hoisted(() => vi.fn());
vi.mock('../download', () => ({ downloadBlob }));

const { default: PhotoEditor } = await import('../PhotoEditor');

const ASSET: Asset = {
  id: 'asset-photo',
  status: 'complete',
  modality: 'image',
  file_name: 'beach.png',
  mime_type: 'image/png',
  declared_size_bytes: 10,
  size_bytes: 10,
};

function mount(onClose = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <PhotoEditor asset={ASSET} source={new Blob()} onClose={onClose} />
    </QueryClientProvider>,
  );
  return onClose;
}

beforeEach(() => {
  seen.sources = [];
  Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:photo'), revokeObjectURL: vi.fn() });
  exportImage.mockReset().mockReturnValue({
    imageData: { imageBase64: PNG, mimeType: 'image/png' },
    designState: {},
    hideLoadingSpinner: vi.fn(),
  });
  uploadStudioFrame.mockReset().mockResolvedValue({
    id: 'lib-1',
    file_name: 'beach-edited.png',
    mime_type: 'image/png',
  });
  listStudioLibrary.mockReset().mockResolvedValue([]);
  downloadBlob.mockReset();
});

describe('PhotoEditor', () => {
  it('runs Filerobot offline, at natural size, without its own Save button', async () => {
    mount();
    await screen.findByText('fake edit');
    expect(seen.props).toMatchObject({
      useBackendTranslations: false,
      savingPixelRatio: 1,
      removeSaveButton: true,
      source: 'blob:photo',
    });
  });

  it('opens Filerobot only once the photo has a URL', async () => {
    mount();
    await screen.findByText('fake edit');
    expect(seen.sources).not.toContain(undefined);
  });

  it('downloads the edited photo under the edited name', async () => {
    mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Download' }));
    await waitFor(() => {
      expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), 'beach-edited.png');
    });
    expect(exportImage).toHaveBeenCalledWith(
      { name: 'beach-edited', extension: 'png', quality: 0.92 },
      1,
    );
  });

  it('saves to the Studio library and says so', async () => {
    mount();
    const save = await screen.findByRole('button', { name: 'Save to library' });
    await waitFor(() => {
      expect(save).toHaveProperty('disabled', false);
    });
    fireEvent.click(save);
    expect(await screen.findByText('Saved to the Studio library')).toBeTruthy();
    const file = uploadStudioFrame.mock.calls[0]?.[0] as File;
    expect(file.name).toBe('beach-edited.png');
  });

  it('disables Save to library when the Studio is not active', async () => {
    listStudioLibrary.mockRejectedValue(new StudioError(503, '', 'studio unavailable'));
    mount();
    expect(
      await screen.findByText('The Studio is not active: download the photo instead.'),
    ).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Save to library' })).toHaveProperty(
      'disabled',
      true,
    );
    expect(screen.getByRole('button', { name: 'Download' })).toHaveProperty('disabled', false);
  });

  it('shows the server sentence on a failed upload and retries', async () => {
    uploadStudioFrame.mockRejectedValueOnce(new Error('image exceeds 26214400 bytes'));
    mount();
    const save = await screen.findByRole('button', { name: 'Save to library' });
    await waitFor(() => {
      expect(save).toHaveProperty('disabled', false);
    });
    fireEvent.click(save);
    expect((await screen.findByRole('alert')).textContent).toContain(
      'image exceeds 26214400 bytes',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('Saved to the Studio library')).toBeTruthy();
  });

  it('retries a failed download as a download, never as an upload', async () => {
    exportImage.mockImplementationOnce(() => {
      throw new Error('the canvas was lost');
    });
    mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Download' }));
    expect((await screen.findByRole('alert')).textContent).toContain('the canvas was lost');
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(downloadBlob).toHaveBeenCalledWith(expect.any(Blob), 'beach-edited.png');
    });
    expect(uploadStudioFrame).not.toHaveBeenCalled();
  });

  it('says so when Filerobot hands back no image', async () => {
    exportImage.mockReturnValue({ imageData: {}, designState: {}, hideLoadingSpinner: vi.fn() });
    mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Download' }));
    expect((await screen.findByRole('alert')).textContent).toContain(
      'the editor returned no image',
    );
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it('shows the upload progress', async () => {
    uploadStudioFrame.mockImplementation(
      (_file: File, onProgress: (progress: number) => void) =>
        new Promise(() => {
          onProgress(0.5);
        }),
    );
    mount();
    const save = await screen.findByRole('button', { name: 'Save to library' });
    await waitFor(() => {
      expect(save).toHaveProperty('disabled', false);
    });
    fireEvent.click(save);
    expect(await screen.findByText('Saving… 50%')).toBeTruthy();
    expect(save).toHaveProperty('disabled', true);
    expect(screen.getByRole('button', { name: 'Download' })).toHaveProperty('disabled', true);
  });

  it('forgets the saved notice at the next edit and asks again before closing', async () => {
    const onClose = mount();
    const save = await screen.findByRole('button', { name: 'Save to library' });
    await waitFor(() => {
      expect(save).toHaveProperty('disabled', false);
    });
    fireEvent.click(screen.getByText('fake edit'));
    fireEvent.click(save);
    await screen.findByText('Saved to the Studio library');
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByText('fake edit'));
    expect(screen.queryByText('Saved to the Studio library')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(await screen.findByRole('button', { name: 'Discard' })).toBeTruthy();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("hands Filerobot Aura's Italian table in Italian", async () => {
    await i18n.changeLanguage('it');
    try {
      mount();
      await screen.findByText('fake edit');
      expect(seen.props).toMatchObject({ language: 'it', translations: FILEROBOT_IT });
    } finally {
      await i18n.changeLanguage('en');
    }
  });

  it('asks before closing over unsaved edits', async () => {
    const onClose = mount();
    fireEvent.click(await screen.findByText('fake edit'));
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole('button', { name: 'Discard' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes at once when nothing changed', async () => {
    const onClose = mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
