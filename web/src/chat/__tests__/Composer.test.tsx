import type { ButtonHTMLAttributes, HTMLAttributes, TextareaHTMLAttributes } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import { Composer } from '../Composer';
import type { AttachmentUploads } from '../attachments/useAttachmentUploads';

const setText = vi.fn();

vi.mock('@assistant-ui/react', () => ({
  useAui: () => ({ composer: () => ({ setText }) }),
  useAuiState: <T,>(selector: (state: { thread: { isRunning: boolean } }) => T): T =>
    selector({ thread: { isRunning: false } }),
  ComposerPrimitive: {
    Root: (props: HTMLAttributes<HTMLDivElement>) => <div {...props} />,
    Input: (props: TextareaHTMLAttributes<HTMLTextAreaElement>) => <textarea {...props} />,
    Cancel: (props: ButtonHTMLAttributes<HTMLButtonElement>) => <button type="button" {...props} />,
    Send: (props: ButtonHTMLAttributes<HTMLButtonElement>) => <button type="submit" {...props} />,
  },
}));

function uploads(overrides: Partial<AttachmentUploads> = {}): AttachmentUploads {
  return {
    items: [],
    readyAssetIds: [],
    hasBlockingUploads: false,
    addFiles: vi.fn(),
    remove: vi.fn(),
    clearReady: vi.fn(),
    ...overrides,
  };
}

describe('Composer attachments', () => {
  it('prefills a document draft prompt', () => {
    render(<Composer draftPrompt={{ text: 'Answer from Manual.pdf', nonce: 1 }} />);

    expect(setText).toHaveBeenCalledWith('Answer from Manual.pdf');
  });

  it('file input passes selected files to addFiles', () => {
    const addFiles = vi.fn();
    const { container } = render(<Composer uploads={uploads({ addFiles })} />);
    const input = container.querySelector('input[type="file"]');
    if (!(input instanceof HTMLInputElement)) throw new Error('expected hidden file input');
    const file = new File(['x'], 'manual.pdf', { type: 'application/pdf' });

    fireEvent.change(input, { target: { files: [file] } });

    expect(Array.from(addFiles.mock.calls[0]?.[0] as FileList | File[])).toEqual([file]);
  });

  it('paste passes clipboard files to addFiles', () => {
    const addFiles = vi.fn();
    const { container } = render(<Composer uploads={uploads({ addFiles })} />);
    const root = container.firstElementChild;
    if (root === null) throw new Error('expected composer root');
    const file = new File(['x'], 'manual.pdf', { type: 'application/pdf' });

    fireEvent.paste(root, { clipboardData: { files: [file] } });

    expect(Array.from(addFiles.mock.calls[0]?.[0] as FileList | File[])).toEqual([file]);
  });

  it('disables send while uploads are blocking', () => {
    render(<Composer uploads={uploads({ hasBlockingUploads: true })} />);

    const send = screen.getByLabelText('Send message');
    expect(send).toHaveProperty('disabled', true);
    expect(send.getAttribute('data-slot')).toBe('button');
  });
});
