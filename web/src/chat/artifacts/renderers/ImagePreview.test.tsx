import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import '../../../i18n/i18n'; // side-effect: initialise i18next so preview t() keys resolve
import ImagePreview from './ImagePreview';

// ImagePreview: the preview modal's image sits centred in the space under the header, framed
// at its own size, so neither a grey band nor the left edge decides where it lands.

const props = { assetId: 'img/1', mimeType: 'image/png', fileName: 'boat.png' };

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => 'blob:boat');
  URL.revokeObjectURL = vi.fn();
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      Promise.resolve({
        ok: true,
        status: 200,
        blob: () => Promise.resolve(new Blob(['pixels'], { type: 'image/png' })),
      } as unknown as Response),
    ),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ImagePreview', () => {
  it('centres the image in the preview, framed at its own width', async () => {
    const { container } = render(<ImagePreview {...props} />);
    await waitFor(() => {
      expect(container.querySelector('img')).not.toBeNull();
    });
    const stage = container.querySelector('[data-slot="media-stage"]');
    const frame = container.querySelector('[data-slot="image"]');
    expect(stage?.classList.contains('justify-center')).toBe(true);
    expect(stage?.classList.contains('items-center')).toBe(true);
    expect(stage?.contains(frame)).toBe(true);
    expect(frame?.classList.contains('w-fit')).toBe(true);
    expect(frame?.classList.contains('w-full')).toBe(false);
  });
});
