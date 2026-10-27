import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so the header's labels resolve
import { ChatPreviewModal } from './ChatPreviewModal';

vi.mock('./renderers/ImagePreview', () => ({ default: () => <div data-testid="r-image" /> }));

// A chat artifact's preview offers Edit: what the Craft app saves goes into the next message.

describe('ChatPreviewModal', () => {
  it('offers Edit on an image the agent delivered', async () => {
    render(
      <ChatPreviewModal
        active={{ id: 'a', file_name: 'generated.png', mime_type: 'image/png' }}
        onClose={vi.fn()}
      />,
    );
    expect(await screen.findByRole('button', { name: /^(Edit|Modifica)$/ })).toBeTruthy();
  });
});
