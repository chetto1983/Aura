import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import '../../../i18n/i18n';
import { AssetSourceContext, type AssetSource } from './assetSourceContext';
import { ToolImageGallery, TrustedImageGallery } from './TrustedImageGallery';
import { groupTrustedImages, type GalleryCandidate, type GalleryItem } from './galleryGrouping';

vi.mock('../useBlobPreview', () => ({
  useBlobPreview: (assetId: string) => ({ url: `blob:${assetId}` }),
}));

const first: GalleryCandidate = {
  turnId: 'turn-1',
  assetId: 'image-1',
  src: 'blob:one',
  width: 400,
  height: 300,
  alt: 'One',
};
const second: GalleryCandidate = {
  turnId: 'turn-1',
  assetId: 'image-2',
  src: 'blob:two',
  width: 200,
  height: 500,
  alt: 'Two',
};

describe('trusted image gallery', () => {
  it('groups two fully decoded images from one assistant turn in source order', () => {
    expect(groupTrustedImages([first, second])).toEqual([
      [
        { assetId: 'image-1', src: 'blob:one', width: 400, height: 300, alt: 'One' },
        { assetId: 'image-2', src: 'blob:two', width: 200, height: 500, alt: 'Two' },
      ],
    ]);
  });

  it('waits for real image dimensions before replacing individual artifacts with a gallery', () => {
    const { container } = render(
      <TrustedImageGallery
        turnId="turn-1"
        artifacts={[
          { filename: 'One', mime_type: 'image/png', asset_id: 'image-1' },
          { filename: 'Two', mime_type: 'image/png', asset_id: 'image-2' },
        ]}
      />,
    );
    expect(screen.getByRole('status')).toBeTruthy();
    const probes = container.querySelectorAll('img[aria-hidden="true"]');
    expect(probes).toHaveLength(2);
    Object.defineProperty(probes[0], 'naturalWidth', { value: 400 });
    Object.defineProperty(probes[0], 'naturalHeight', { value: 300 });
    Object.defineProperty(probes[1], 'naturalWidth', { value: 200 });
    Object.defineProperty(probes[1], 'naturalHeight', { value: 500 });
    fireEvent.load(probes[0] as HTMLImageElement);
    fireEvent.load(probes[1] as HTMLImageElement);
    expect(container.querySelector('[data-slot="image-gallery"]')).toBeTruthy();
  });

  it('keeps single, cross-turn, missing-ID, zero-sized and excessive images separate', () => {
    expect(groupTrustedImages([first])).toEqual([]);
    expect(groupTrustedImages([first, { ...second, turnId: 'turn-2' }])).toEqual([]);
    expect(groupTrustedImages([first, { ...second, assetId: '' }])).toEqual([]);
    expect(groupTrustedImages([first, { ...second, width: 0 }])).toEqual([]);
    expect(groupTrustedImages([first, { ...second, height: Number.NaN }])).toEqual([]);
    expect(groupTrustedImages([first, { ...second, width: 50000 }])).toEqual([]);
  });

  it('opens only the selected lightbox, closes it with Escape, and downloads via the share tier', () => {
    const source: AssetSource = {
      assetUrl: (id) => `/s/tok/asset/${encodeURIComponent(id)}`,
      streamUrl: (id) => `/s/tok/asset/${encodeURIComponent(id)}/stream`,
      credentials: 'omit',
    };
    const images: readonly GalleryItem[] = [first, second];
    render(
      <AssetSourceContext.Provider value={source}>
        <ToolImageGallery id="turn-1" images={images} />
      </AssetSourceContext.Provider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Open Two' }));
    const dialog = screen.getByRole('dialog', { name: 'Two' });
    const download = screen.getByRole('link', { name: 'Download Two' });
    expect(download.getAttribute('href')).toBe('/s/tok/asset/image-2');
    expect(dialog.querySelector('img')?.getAttribute('src')).toBe('blob:two');
    fireEvent.keyDown(document.activeElement ?? dialog, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.getByRole('button', { name: 'Open One' })).toBeTruthy();
  });
});
