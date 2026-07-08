import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so the modal's t() keys resolve
import type { Asset } from '../attachments/types';
import { PreviewModal } from './PreviewModal';

// PreviewModal (WEBART-05 / D-09): the click-to-preview modal. The six renderers are MOCKED
// to marker divs so the test pins the previewKind→renderer dispatch table, the download-only
// card for svg/pptx/unknown, the 90vw×90vh sizing, the header download href, and onClose —
// without loading the real renderer chunks (and their heavy docx-preview/xlsx deps).

vi.mock('./renderers/ImagePreview', () => ({ default: () => <div data-testid="r-image" /> }));
vi.mock('./renderers/PdfPreview', () => ({ default: () => <div data-testid="r-pdf" /> }));
vi.mock('./renderers/TextPreview', () => ({ default: () => <div data-testid="r-text" /> }));
vi.mock('./renderers/HtmlPreview', () => ({ default: () => <div data-testid="r-html" /> }));
vi.mock('./renderers/DocxPreview', () => ({ default: () => <div data-testid="r-docx" /> }));
vi.mock('./renderers/XlsxPreview', () => ({ default: () => <div data-testid="r-xlsx" /> }));

const RENDERER_IDS = ['r-image', 'r-pdf', 'r-text', 'r-html', 'r-docx', 'r-xlsx'];

function asset(over: Partial<Asset>): Asset {
  return {
    id: 'a1',
    status: 'accepted',
    modality: 'document',
    file_name: 'report.pdf',
    mime_type: 'application/pdf',
    declared_size_bytes: 0,
    size_bytes: 0,
    ...over,
  };
}

function dialogContent(): HTMLElement {
  const node = document.querySelector('[data-slot="dialog-content"]');
  if (!(node instanceof HTMLElement)) throw new Error('dialog content not rendered');
  return node;
}

describe('PreviewModal dispatch (previewKind → renderer)', () => {
  const cases: readonly (readonly [string, string, string])[] = [
    ['image/png', 'cat.png', 'r-image'],
    ['application/pdf', 'doc.pdf', 'r-pdf'],
    ['text/plain', 'notes.txt', 'r-text'],
    ['text/html', 'page.html', 'r-html'],
    ['application/octet-stream', 'memo.docx', 'r-docx'],
    ['application/octet-stream', 'book.xlsx', 'r-xlsx'],
  ];

  it.each(cases)('mime=%s name=%s mounts the matching renderer', async (mime, name, testid) => {
    render(<PreviewModal active={asset({ mime_type: mime, file_name: name })} onClose={vi.fn()} />);
    expect(await screen.findByTestId(testid)).toBeTruthy();
  });

  const downloadOnly: readonly (readonly [string, string])[] = [
    ['image/svg+xml', 'logo.svg'],
    ['application/vnd.openxmlformats-officedocument.presentationml.presentation', 'deck.pptx'],
    ['application/x-unknown', 'blob.bin'],
  ];

  it.each(downloadOnly)(
    'mime=%s name=%s shows a download card, NOT a renderer',
    async (mime, name) => {
      render(
        <PreviewModal active={asset({ mime_type: mime, file_name: name })} onClose={vi.fn()} />,
      );
      await waitFor(() => {
        expect(screen.getByText(/download it to open/i)).toBeTruthy();
      });
      for (const id of RENDERER_IDS) {
        expect(screen.queryByTestId(id)).toBeNull();
      }
    },
  );
});

describe('PreviewModal chrome', () => {
  it('sizes the content ~90vw × 90vh', () => {
    render(<PreviewModal active={asset({})} onClose={vi.fn()} />);
    const cls = dialogContent().className;
    expect(cls).toContain('w-[90vw]');
    expect(cls).toContain('h-[90vh]');
  });

  it('header carries a same-origin download anchor to the asset route', () => {
    render(
      <PreviewModal active={asset({ id: 'xyz', file_name: 'report.pdf' })} onClose={vi.fn()} />,
    );
    const link = screen.getByRole('link', { name: /download report\.pdf/i });
    expect(link.getAttribute('href')).toBe('/api/assets/xyz/download');
    expect(link.getAttribute('download')).toBe('report.pdf');
  });

  it('is closed (no dialog content) when active is undefined', () => {
    render(<PreviewModal active={undefined} onClose={vi.fn()} />);
    expect(document.querySelector('[data-slot="dialog-content"]')).toBeNull();
  });

  it('calls onClose when the close button is clicked', () => {
    const onClose = vi.fn();
    render(<PreviewModal active={asset({})} onClose={onClose} />);
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
