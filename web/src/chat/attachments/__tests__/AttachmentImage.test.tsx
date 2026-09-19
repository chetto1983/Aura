import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { LocalImagePreview } from '../AttachmentImage';

describe('LocalImagePreview', () => {
  it('draws no image until its object URL exists', async () => {
    const imagesWhenMinted: (string | null)[] = [];
    // jsdom has no object URLs; assign them rather than replacing the URL class jsdom uses.
    Object.assign(URL, {
      createObjectURL: vi.fn(() => {
        for (const image of document.querySelectorAll('img')) {
          imagesWhenMinted.push(image.getAttribute('src'));
        }
        return 'blob:preview';
      }),
      revokeObjectURL: vi.fn(),
    });
    render(<LocalImagePreview file={new File(['x'], 'beach.png', { type: 'image/png' })} />);
    expect(imagesWhenMinted).toEqual([]);
    const image = await screen.findByRole('img', { name: 'beach.png' });
    expect(image.getAttribute('src')).toBe('blob:preview');
  });
});
