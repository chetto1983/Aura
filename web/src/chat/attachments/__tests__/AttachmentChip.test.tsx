import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../../i18n/i18n';
import { AttachmentChip } from '../AttachmentChip';
import type { UploadItem } from '../types';

function uploadItem(status: UploadItem['status'], progress = 0): UploadItem {
  return {
    localId: `local-${status}`,
    file: new File(['x'], `${status}.pdf`, { type: 'application/pdf' }),
    progress,
    status,
  };
}

describe('AttachmentChip', () => {
  it('renders status labels and removes by local id', () => {
    const remove = vi.fn();

    render(
      <div>
        <AttachmentChip item={uploadItem('queued')} onRemove={remove} />
        <AttachmentChip item={uploadItem('uploading', 0.42)} onRemove={remove} />
        <AttachmentChip item={uploadItem('processing')} onRemove={remove} />
        <AttachmentChip item={uploadItem('ready')} onRemove={remove} />
        <AttachmentChip item={uploadItem('failed')} onRemove={remove} />
        <AttachmentChip item={uploadItem('refused')} onRemove={remove} />
      </div>,
    );

    expect(screen.getAllByText('Processing')).toHaveLength(2);
    expect(screen.getByText('Uploading 42%')).toBeTruthy();
    expect(screen.getByText('Ready')).toBeTruthy();
    expect(screen.getByText('Failed')).toBeTruthy();
    expect(screen.getByText('Refused')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Remove ready.pdf' }));

    expect(remove).toHaveBeenCalledWith('local-ready');
  });
});
