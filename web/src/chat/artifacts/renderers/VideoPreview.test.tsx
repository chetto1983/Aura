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
});
