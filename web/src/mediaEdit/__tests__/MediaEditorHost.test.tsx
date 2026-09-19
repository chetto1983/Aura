import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import type { Asset } from '../../chat/attachments/types';

const getAsset = vi.hoisted(() => vi.fn());
vi.mock('../../chat/attachments/api', () => ({ getAsset }));
vi.mock('../PhotoEditor', () => ({
  default: ({ asset, source }: { asset: Asset; source: Blob }) => (
    <p>
      photo {asset.file_name} {String(source.size)}
    </p>
  ),
}));
vi.mock('../VideoEditor', () => ({
  default: ({ asset }: { asset: Asset }) => <p>video {asset.file_name}</p>,
}));

const { default: MediaEditorHost } = await import('../MediaEditorHost');

function asset(over: Partial<Asset>): Asset {
  return {
    id: 'a1',
    status: 'complete',
    modality: 'image',
    file_name: 'beach.png',
    mime_type: 'image/png',
    declared_size_bytes: 3,
    size_bytes: 3,
    ...over,
  };
}

function mount(kind: 'image' | 'video' = 'image') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <MediaEditorHost assetId="a1" kind={kind} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
}

// A string body, not `new Blob(['abc'])`: under jsdom the Blob is jsdom's, which Node's Response
// does not recognise and serialises as "[object Blob]".
beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response('abc'))),
  );
});

describe('MediaEditorHost', () => {
  it('loads the bytes and opens the photo editor', async () => {
    getAsset.mockResolvedValue(asset({}));
    mount();
    expect(await screen.findByText('photo beach.png 3')).toBeTruthy();
  });

  it('opens the clip editor for a video', async () => {
    getAsset.mockResolvedValue(
      asset({ modality: 'video', file_name: 'clip.mp4', mime_type: 'video/mp4' }),
    );
    mount('video');
    expect(await screen.findByText('video clip.mp4')).toBeTruthy();
  });

  it('says so when the format cannot be edited', async () => {
    getAsset.mockResolvedValue(asset({ mime_type: 'image/gif', file_name: 'loop.gif' }));
    mount();
    expect(await screen.findByText('This format cannot be edited here.')).toBeTruthy();
  });

  it('says so when the file cannot be read', async () => {
    getAsset.mockRejectedValue(new Error('HTTP 404'));
    mount();
    expect(await screen.findByText('The file could not be opened.')).toBeTruthy();
  });

  it('asks before loading a very large clip', async () => {
    getAsset.mockResolvedValue(
      asset({
        modality: 'video',
        file_name: 'long.mp4',
        mime_type: 'video/mp4',
        size_bytes: 600 * 1024 * 1024,
      }),
    );
    mount('video');
    fireEvent.click(await screen.findByRole('button', { name: 'Open anyway' }));
    expect(await screen.findByText('video long.mp4')).toBeTruthy();
  });
});
