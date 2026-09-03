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

  // Rewritten deliberately, not repaired: this asserted a "Ready" badge, and a permanent
  // label on a finished upload is noise that teaches the reader to stop looking at the one
  // state that matters. The contract is now that a usable attachment says nothing and just
  // shows what it is.
  it('says nothing once the attachment is usable', () => {
    render(
      <AttachmentChip
        attachment={attachment({ type: 'requires-action', reason: 'composer-send' })}
      />,
    );

    expect(screen.queryByText('Ready')).toBeNull();
    expect(screen.queryByText('Processing')).toBeNull();
    expect(screen.getByText('note.pdf')).toBeTruthy();
  });

  // An image is shown, not named: the question a composer preview answers is "is that the
  // right screenshot?", which a filename answers badly.
  it('previews an image attachment instead of listing its name', () => {
    const file = new File(['x'], 'shot.png', { type: 'image/png' });
    render(
      <AttachmentChip
        attachment={{
          id: 'asset-image',
          type: 'image',
          name: 'shot.png',
          contentType: 'image/png',
          file,
          status: { type: 'requires-action', reason: 'composer-send' },
        }}
      />,
    );

    const image = screen.getByAltText('shot.png');
    expect(image.tagName).toBe('IMG');
    expect(image.getAttribute('src')).toContain('blob:');
    expect(screen.queryByText('shot.png')).toBeNull();
  });

  // A non-image keeps the name-and-badge chip: there is nothing to look at.
  it('keeps naming a document that has no preview', () => {
    render(
      <AttachmentChip
        attachment={attachment({ type: 'running', reason: 'uploading', progress: 1 })}
      />,
    );

    expect(screen.queryByAltText('note.pdf')).toBeNull();
    expect(screen.getByText('note.pdf')).toBeTruthy();
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
