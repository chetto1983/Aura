import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import '../../../i18n/i18n';
import type { Attachment, AttachmentStatus } from '@assistant-ui/core';
import { AttachmentChip } from '../AttachmentChip';

// Removal is AttachmentPrimitive.Remove now — it calls the adapter through the runtime
// context rather than an onRemove prop — so the primitive is stubbed to a plain button and
// what this test asserts is the chip's own job: the status word, its badge, and the
// accessible name of the remove control. The adapter's remove() is covered where it lives.
vi.mock('@assistant-ui/react', () => ({
  AttachmentPrimitive: {
    Root: ({ children, ...props }: { children: ReactNode }) => <span {...props}>{children}</span>,
    Remove: ({ render: node }: { render: ReactNode }) => <>{node}</>,
  },
}));

function attachment(status: AttachmentStatus, name = 'note.pdf'): Attachment {
  return {
    id: `asset-${status.type}`,
    type: 'document',
    name,
    contentType: 'application/pdf',
    file: new File(['x'], name, { type: 'application/pdf' }),
    status,
  } as Attachment;
}

describe('AttachmentChip', () => {
  it('shows the upload percentage while the adapter is still yielding progress', () => {
    render(
      <AttachmentChip
        attachment={attachment({ type: 'running', reason: 'uploading', progress: 0.42 })}
      />,
    );

    expect(screen.getByText('Uploading 42%')).toBeTruthy();
  });

  // A full bar is not a finished attachment: the adapter stays `running` while the server
  // indexes, and calling that "Uploading 100%" told the operator the wrong thing.
  it('reads a delivered-but-still-indexing attachment as processing', () => {
    render(
      <AttachmentChip
        attachment={attachment({ type: 'running', reason: 'uploading', progress: 1 })}
      />,
    );

    expect(screen.getByText('Processing')).toBeTruthy();
  });

  it('reads an uploaded-but-unsent attachment as ready', () => {
    render(
      <AttachmentChip
        attachment={attachment({ type: 'requires-action', reason: 'composer-send' })}
      />,
    );

    const badge = screen.getByText('Ready');
    expect(badge.getAttribute('data-slot')).toBe('badge');
  });

  it('reads a refused or failed upload as failed', () => {
    render(
      <AttachmentChip
        attachment={attachment({ type: 'incomplete', reason: 'error', message: 'refused' })}
      />,
    );

    expect(screen.getByText('Failed')).toBeTruthy();
  });

  it('names the file in the remove control so it is distinguishable among several', () => {
    render(<AttachmentChip attachment={attachment({ type: 'complete' }, 'verbale.pdf')} />);

    expect(screen.getByRole('button', { name: 'Remove verbale.pdf' })).toBeTruthy();
  });
});
