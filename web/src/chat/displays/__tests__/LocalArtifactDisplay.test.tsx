import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so t() resolves keys
import { LocalArtifactDisplay } from '../LocalArtifactDisplay';
import type { DisplayArtifact, DisplayPayload } from '../types';

// LocalArtifactDisplay (37A-04): an authenticated download button when the
// descriptor carries an asset_id, else a render-only filename + size + "delivery
// unavailable" card. A raw host/container path is NEVER rendered in either branch.
// Delivered images and MP4/WebM clips preview inline (spec section 5); SVG never does.

const HOST_PATH = '/run/out/report.csv';

function payload(artifact: DisplayArtifact): DisplayPayload {
  return { type: 'local_artifact', tool_call_id: 'call-1', artifact };
}

describe('LocalArtifactDisplay', () => {
  it('renders an authenticated same-origin download anchor when asset_id is present', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'report.csv', size_bytes: 2048, asset_id: 'asset-123' })}
      />,
    );
    const link = screen.getByRole('link', { name: /report\.csv/i });
    expect(link.getAttribute('href')).toBe('/api/assets/asset-123/download');
    expect(link.getAttribute('download')).toBe('report.csv');
    expect(screen.getByText('report.csv')).toBeTruthy();
    expect(screen.getByText('2.0 KB')).toBeTruthy();
  });

  it('never renders a raw host/container path even if one leaks onto the artifact (asset_id present)', () => {
    // Defense-in-depth: the reducer already omits path, but the card itself must
    // not render it even if a path field rides alongside asset_id.
    const { container } = render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'report.csv', asset_id: 'asset-123', path: HOST_PATH })}
      />,
    );
    expect(screen.queryByText(HOST_PATH)).toBeNull();
    expect(container.textContent).not.toContain(HOST_PATH);
    expect(screen.queryByText('Path')).toBeNull();
  });

  it('degrades to filename + size + a "delivery unavailable" note when asset_id is absent', () => {
    render(
      <LocalArtifactDisplay payload={payload({ filename: 'report.csv', size_bytes: 2048 })} />,
    );
    expect(screen.getByText('report.csv')).toBeTruthy();
    expect(screen.getByText('2.0 KB')).toBeTruthy();
    expect(screen.getByText('Delivery unavailable')).toBeTruthy();
    // No download affordance on the degraded card.
    expect(screen.queryByRole('link')).toBeNull();
  });

  it('never renders a raw host/container path on the degraded card (asset_id absent, path present)', () => {
    const { container } = render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'report.csv', size_bytes: 2048, path: HOST_PATH })}
      />,
    );
    expect(screen.queryByText(HOST_PATH)).toBeNull();
    expect(container.textContent).not.toContain(HOST_PATH);
    expect(screen.queryByText('Path')).toBeNull();
    expect(screen.getByText('Delivery unavailable')).toBeTruthy();
  });

  it('formats the byte size in KB for a 2048-byte file', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'a.bin', size_bytes: 2048, asset_id: 'a' })}
      />,
    );
    expect(screen.getByText('2.0 KB')).toBeTruthy();
  });

  it('formats a sub-KB size in bytes', () => {
    render(<LocalArtifactDisplay payload={payload({ filename: 'tiny', size_bytes: 512 })} />);
    expect(screen.getByText('512 B')).toBeTruthy();
  });

  it('formats an MB-scale size', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'big', size_bytes: 5 * 1024 * 1024, asset_id: 'a' })}
      />,
    );
    expect(screen.getByText('5.0 MB')).toBeTruthy();
  });

  it('formats a GB-scale size', () => {
    render(
      <LocalArtifactDisplay
        payload={payload({ filename: 'huge', size_bytes: 3 * 1024 * 1024 * 1024, asset_id: 'a' })}
      />,
    );
    expect(screen.getByText('3.0 GB')).toBeTruthy();
  });

  describe('inline media previews', () => {
    afterEach(() => {
      vi.unstubAllGlobals();
    });

    function stubAssetBytes(): ReturnType<typeof vi.fn> {
      URL.createObjectURL = vi.fn(() => 'blob:inline');
      URL.revokeObjectURL = vi.fn();
      const fetchMock = vi.fn(() =>
        Promise.resolve({
          ok: true,
          status: 200,
          blob: () => Promise.resolve(new Blob(['bytes'], { type: 'image/png' })),
        } as unknown as Response),
      );
      vi.stubGlobal('fetch', fetchMock);
      return fetchMock;
    }

    it('previews a delivered image with its download action on the asset route', async () => {
      stubAssetBytes();
      render(
        <LocalArtifactDisplay
          payload={payload({
            filename: 'lake.png',
            mime_type: 'image/png',
            asset_id: 'img-1',
            path: HOST_PATH,
          })}
        />,
      );
      const img = await screen.findByRole('img', { name: 'lake.png' });
      expect(img.getAttribute('src')).toBe('blob:inline');
      expect(screen.getByRole('link', { name: 'Download lake.png' }).getAttribute('href')).toBe(
        '/api/assets/img-1/download',
      );
      expect(document.body.innerHTML).not.toContain(HOST_PATH);
    });

    it('streams a delivered MP4 inline with a download link and never fetches it', async () => {
      const fetchMock = stubAssetBytes();
      const { container } = render(
        <LocalArtifactDisplay
          payload={payload({
            filename: 'sea.mp4',
            mime_type: 'video/mp4',
            asset_id: 'vid-1',
            size_bytes: 2048,
            path: HOST_PATH,
          })}
        />,
      );
      const video = await screen.findByLabelText('sea.mp4');
      expect(video.tagName).toBe('VIDEO');
      expect(video.getAttribute('src')).toBe('/api/assets/vid-1/stream');
      expect(video.hasAttribute('autoplay')).toBe(false);
      expect(screen.getByRole('link', { name: /sea\.mp4/i }).getAttribute('href')).toBe(
        '/api/assets/vid-1/download',
      );
      expect(fetchMock).not.toHaveBeenCalled();
      expect(container.innerHTML).not.toContain(HOST_PATH);
    });

    it('keeps an SVG as a download card: no <img>, no <video>, download link kept', () => {
      const fetchMock = stubAssetBytes();
      const { container } = render(
        <LocalArtifactDisplay
          payload={payload({ filename: 'logo.svg', mime_type: 'image/svg+xml', asset_id: 'svg-1' })}
        />,
      );
      expect(container.querySelector('img')).toBeNull();
      expect(container.querySelector('video')).toBeNull();
      expect(screen.getByRole('link', { name: /logo\.svg/i }).getAttribute('href')).toBe(
        '/api/assets/svg-1/download',
      );
      expect(fetchMock).not.toHaveBeenCalled();
    });

    it('keeps an undelivered image on the degraded card', () => {
      const fetchMock = stubAssetBytes();
      const { container } = render(
        <LocalArtifactDisplay
          payload={payload({ filename: 'lake.png', mime_type: 'image/png' })}
        />,
      );
      expect(container.querySelector('img')).toBeNull();
      expect(screen.getByText('Delivery unavailable')).toBeTruthy();
      expect(fetchMock).not.toHaveBeenCalled();
    });
  });

  it('falls to a safe name when the filename is missing', () => {
    render(
      <LocalArtifactDisplay
        payload={{ type: 'local_artifact', tool_call_id: 'call-x' } as DisplayPayload}
      />,
    );
    expect(screen.getByText('Untitled file')).toBeTruthy();
  });
});
