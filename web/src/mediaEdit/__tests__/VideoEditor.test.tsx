import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n/i18n';
import type { Asset } from '../../chat/attachments/types';

const media = vi.hoisted(() => ({ probeVideo: vi.fn() }));
vi.mock('../videoMedia', () => media);

const sources = vi.hoisted(() => ({ uploadSource: vi.fn() }));
vi.mock('../../videoStudio/VideoStudio_sources', async (original) => ({
  ...(await original<typeof import('../../videoStudio/VideoStudio_sources')>()),
  uploadSource: sources.uploadSource,
}));

const studio = vi.hoisted(() => ({ opened: [] as unknown[] }));
vi.mock('../../videoStudio/VideoStudio', () => ({
  default: ({ open, onClose }: { open: unknown; onClose: () => void }) => {
    studio.opened.push(open);
    return (
      <div role="dialog" aria-label="unified video editor">
        <button type="button" onClick={onClose}>
          close unified editor
        </button>
      </div>
    );
  },
}));

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
  studio.opened.length = 0;
  sources.uploadSource.mockResolvedValue('uploaded-clip');
  media.probeVideo.mockResolvedValue({
    duration: 10,
    width: 1280,
    height: 720,
    hasAudio: true,
    decodable: true,
  });
});

describe('VideoEditor', () => {
  it('opens a clip directly in the unified multi-track workspace', async () => {
    render(<VideoEditor asset={ASSET} source={new Blob()} onClose={vi.fn()} />);

    expect(await screen.findByRole('dialog', { name: 'unified video editor' })).toBeTruthy();
    expect(sources.uploadSource).not.toHaveBeenCalled();
    expect(studio.opened[0]).toMatchObject({
      kind: 'project',
      project: { sources: [{ assetId: 'asset-clip' }], video: [{ duration: 10 }] },
    });
  });

  it('gives a Garage clip a durable asset id before composing it', async () => {
    render(
      <VideoEditor
        asset={{ ...ASSET, id: 'garage:object-1' }}
        source={new Blob()}
        onClose={vi.fn()}
      />,
    );

    await screen.findByRole('dialog', { name: 'unified video editor' });
    expect(sources.uploadSource).toHaveBeenCalledOnce();
    expect(studio.opened[0]).toMatchObject({
      project: { sources: [{ assetId: 'uploaded-clip' }] },
    });
  });

  it('refuses a clip the composition cannot decode', async () => {
    media.probeVideo.mockResolvedValue({
      duration: 10,
      width: 1280,
      height: 720,
      hasAudio: true,
      decodable: false,
    });
    render(<VideoEditor asset={ASSET} source={new Blob()} onClose={vi.fn()} />);

    expect((await screen.findByRole('alert')).textContent).toBe(
      i18n.t('videoStudio.refusal.sourceUndecodable'),
    );
    expect(studio.opened).toEqual([]);
  });

  it('closes through the one workspace', async () => {
    const onClose = vi.fn();
    render(<VideoEditor asset={ASSET} source={new Blob()} onClose={onClose} />);
    fireEvent.click(await screen.findByRole('button', { name: 'close unified editor' }));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
