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
    const remove = vi.fn();

    render(<AttachmentCard asset={readyAsset} onPromote={promote} onRemove={remove} />);

    expect(screen.getByText('manual.pdf').closest('[data-slot="card"]')).not.toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Promote' }));
    fireEvent.click(screen.getByRole('button', { name: 'Remove manual.pdf' }));

    expect(promote).toHaveBeenCalledWith('asset-1');
    expect(remove).toHaveBeenCalledWith('asset-1');
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
