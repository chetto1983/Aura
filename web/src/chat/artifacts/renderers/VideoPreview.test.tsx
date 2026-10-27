import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import type { ReactNode } from 'react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so preview t() keys resolve
import { AssetSourceContext, type AssetSource } from './assetSourceContext';
import VideoPreview from './VideoPreview';

// VideoPreview (spec §5, R22/R24): the <video> element streams the asset through the
// tier's Range-capable /stream route — no fetch, no blob, no object URL — with native
// controls, click to play and never autoplay, and the shared error state when the
// browser cannot play it.

const props = { assetId: 'a/b?c', mimeType: 'video/mp4', fileName: 'sea.mp4' };

let fetchMock: ReturnType<typeof vi.fn>;
let createSpy: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
  createSpy = vi.fn(() => 'blob:never');
  URL.createObjectURL = createSpy as unknown as typeof URL.createObjectURL;
});

afterEach(() => {
  vi.unstubAllGlobals();
});

function videoOf(container: HTMLElement): HTMLVideoElement {
  const video = container.querySelector('video');
  if (video === null) throw new Error('expected a <video>');
  return video;
}

function renderIn(source: AssetSource | undefined, ui: ReactNode) {
  if (source === undefined) return render(ui);
  return render(<AssetSourceContext.Provider value={source}>{ui}</AssetSourceContext.Provider>);
}

describe('VideoPreview', () => {
  it('streams the identity-scoped route with an encoded id and fetches nothing', () => {
    const { container } = renderIn(undefined, <VideoPreview {...props} />);
    const video = videoOf(container);
    expect(video.getAttribute('src')).toBe('/api/assets/a%2Fb%3Fc/stream');
    expect(fetchMock).not.toHaveBeenCalled();
    expect(createSpy).not.toHaveBeenCalled();
  });

  it('streams whatever route a mounted share tier resolves', () => {
    const shareTier: AssetSource = {
      assetUrl: (id) => `/s/tok/asset/${encodeURIComponent(id)}`,
      streamUrl: (id) => `/s/tok/asset/${encodeURIComponent(id)}/stream`,
      credentials: 'omit',
    };
    const { container } = renderIn(shareTier, <VideoPreview {...props} />);
    expect(videoOf(container).getAttribute('src')).toBe('/s/tok/asset/a%2Fb%3Fc/stream');
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // The file manager serves its objects, Range included, at one route that names the object in
  // its query (FILE_MANAGER_SOURCE): refusing every query left its videos unplayable.
  it("streams the file manager's own route, and nothing else under its path", () => {
    const route = (extra: string): AssetSource => ({
      assetUrl: (id) => `/api/filemanager/direct?id=${encodeURIComponent(id)}${extra}`,
      streamUrl: (id) => `/api/filemanager/direct?id=${encodeURIComponent(id)}${extra}`,
      credentials: 'same-origin',
    });
    const file = { ...props, assetId: '/prove-craft/clip.mp4' };
    const { container } = renderIn(route(''), <VideoPreview {...file} />);
    expect(videoOf(container).getAttribute('src')).toBe(
      '/api/filemanager/direct?id=%2Fprove-craft%2Fclip.mp4',
    );

    for (const extra of ['&download=true', '#t=5', '&id=/other.mp4']) {
      const other = renderIn(route(extra), <VideoPreview {...file} />);
      expect(other.container.querySelector('video')).toBeNull();
      other.unmount();
    }
  });

  it('refuses a stream URL outside Aura', () => {
    const source: AssetSource = {
      assetUrl: () => '/safe/download',
      streamUrl: () => 'https://provider.example/clip.mp4',
      credentials: 'omit',
    };
    const { container } = renderIn(source, <VideoPreview {...props} />);
    expect(container.querySelector('video')).toBeNull();
    expect(screen.getByRole('alert')).toBeTruthy();
  });

  it('plays only on request, with native controls, inline, loading metadata first', () => {
    const { container } = renderIn(undefined, <VideoPreview {...props} />);
    const video = videoOf(container);
    expect(video.hasAttribute('controls')).toBe(true);
    expect(video.hasAttribute('playsinline')).toBe(true);
    expect(video.getAttribute('preload')).toBe('metadata');
    expect(video.hasAttribute('autoplay')).toBe(false);
    expect(video.autoplay).toBe(false);
    expect(video.getAttribute('aria-label')).toBe('sea.mp4');
    expect(screen.getByLabelText('sea.mp4')).toBe(video);
    expect(video.closest('[data-slot="video-player"]')).toBeTruthy();
    expect(video.className).toContain('max-h-[70vh]');
    expect(video.className).toContain('h-auto');
  });

  it('never autoplays when the preview is mounted again', () => {
    const { container, unmount } = renderIn(undefined, <VideoPreview {...props} />);
    unmount();
    const again = renderIn(undefined, <VideoPreview {...props} />);
    expect(container.querySelector('video')).toBeNull();
    expect(videoOf(again.container).hasAttribute('autoplay')).toBe(false);
  });

  it('shows the shared error state when the element cannot play the stream', () => {
    const { container } = renderIn(undefined, <VideoPreview {...props} />);
    fireEvent.error(videoOf(container));
    expect(screen.getByRole('alert').textContent).toContain("Couldn't load this preview.");
    expect(container.querySelector('video')).toBeNull();
  });

  it('forgets a previous asset error when a different asset is shown', () => {
    const { container, rerender } = renderIn(undefined, <VideoPreview {...props} />);
    fireEvent.error(videoOf(container));
    rerender(<VideoPreview {...props} assetId="next" />);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(videoOf(container).getAttribute('src')).toBe('/api/assets/next/stream');
  });

  it('keeps the card width in a message and takes the width its host asks for', () => {
    const inMessage = renderIn(undefined, <VideoPreview {...props} />);
    const card = inMessage.container.querySelector('[data-slot="video-player"]');
    expect(card?.classList.contains('max-w-xl')).toBe(true);
    inMessage.unmount();

    const { container } = renderIn(
      undefined,
      <VideoPreview {...props} className="w-fit max-w-full" />,
    );
    const player = container.querySelector('[data-slot="video-player"]');
    expect(player?.classList.contains('w-fit')).toBe(true);
    expect(player?.classList.contains('max-w-xl')).toBe(false);
  });
});
