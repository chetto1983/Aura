import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../../i18n/i18n';
import { AttachmentCard } from '../AttachmentCard';
import type { Asset } from '../types';

const readyAsset: Asset = {
  id: 'asset-1',
  status: 'searchable',
  modality: 'document',
  file_name: 'manual.pdf',
  mime_type: 'application/pdf',
  declared_size_bytes: 9,
  size_bytes: 9,
  summary: 'indexed',
};

describe('AttachmentCard actions', () => {
  it('shows promote and remove controls for ready assets', () => {
    const promote = vi.fn();

    render(<AttachmentCard asset={readyAsset} onPromote={promote} />);

    expect(screen.getByText('manual.pdf').closest('[data-slot="card"]')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Promote' }));

    expect(promote).toHaveBeenCalledWith('asset-1');
    // Deliberately absent: an attachment cannot be un-sent, so a remove control here
    // only deletes the bytes of a message that already happened, leaving a broken card
    // behind it (seen live 2026-09-03 -- the X left status `deleting` and a 404 image).
    expect(screen.queryByRole('button', { name: 'Remove manual.pdf' })).toBeNull();
  });

  it('shows retry only for failed assets', () => {
    const retry = vi.fn();

    render(
      <AttachmentCard
        asset={{ ...readyAsset, status: 'failed', error_message: 'processor failed' }}
        onRetry={retry}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(retry).toHaveBeenCalledWith('asset-1');
    expect(screen.queryByRole('button', { name: 'Promote' })).toBeNull();
  });
});

// The live defect, 2026-09-03: a screenshot the vision model read and described in full
// sat under a permanent "Elaborazione" badge. `processing` is where assets STOP on this
// deployment -- upload.ts records that nothing ever reaches `searchable` -- so treating it
// as unfinished meant the card could never stop claiming work was in progress.
describe('AttachmentCard status honesty', () => {
  const processing: Asset = {
    id: 'asset-2',
    status: 'processing',
    modality: 'image',
    file_name: 'screenshot.png',
    mime_type: 'image/png',
    declared_size_bytes: 12,
    size_bytes: 12,
  };

  it('stops calling a usable attachment unfinished', () => {
    render(<AttachmentCard asset={processing} />);

    expect(screen.queryByText('Processing')).toBeNull();
    expect(screen.getByText('screenshot.png')).toBeTruthy();
  });

  it('shows the image itself, addressed by asset id', () => {
    render(<AttachmentCard asset={processing} />);

    const image = screen.getByAltText('screenshot.png');
    expect(image.getAttribute('src')).toBe('/api/assets/asset-2/download');
  });

  it('still reports a refusal, which is the case worth a badge', () => {
    render(<AttachmentCard asset={{ ...processing, status: 'refused' }} />);

    expect(screen.getByText('Refused')).toBeTruthy();
    expect(screen.queryByAltText('screenshot.png')).toBeNull();
  });

  it('does not try to preview a document', () => {
    render(<AttachmentCard asset={{ ...processing, modality: 'document' }} />);

    expect(screen.queryByAltText('screenshot.png')).toBeNull();
  });
});
