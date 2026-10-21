import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n'; // side-effect: initialise i18next so the modal's t() keys resolve
import type { Asset } from '../attachments/types';
import { PreviewModal } from './PreviewModal';
import { AssetSourceContext, IDENTITY_SCOPED } from './renderers/assetSourceContext';

// PreviewModal (WEBART-05 / D-09): the click-to-preview modal. The renderers are MOCKED to
// marker divs (a Craft app's names its app) so the test pins the previewKind→renderer dispatch
// table, the download-only card for svg/ppt/unknown, the 90vw×90vh sizing, the header download
// href, and onClose — without loading the real renderer chunks (and native app assets).

vi.mock('./renderers/ImagePreview', () => ({ default: () => <div data-testid="r-image" /> }));
vi.mock('./renderers/TextPreview', () => ({ default: () => <div data-testid="r-text" /> }));
vi.mock('./renderers/HtmlPreview', () => ({ default: () => <div data-testid="r-html" /> }));
// The Craft app's marker can report an edit and a save to its host, as the real frame does.
vi.mock('./renderers/CraftFrame', async () => {
  const { useContext } = await import('react');
  const { PreviewCloseGuardContext } = await import('./renderers/previewCloseGuardContext');
  return {
    default: function CraftApp({ app }: { app: string }) {
      const guard = useContext(PreviewCloseGuardContext);
      return (
        <div data-testid={`r-${app}`} data-held={String(guard?.held)}>
          <button type="button" onClick={() => guard?.setUnsaved(true)}>
            edit in {app}
          </button>
          <button type="button" onClick={() => guard?.setUnsaved(false)}>
            save in {app}
          </button>
        </div>
      );
    },
  };
});
vi.mock('./renderers/VideoPreview', () => ({ default: () => <div data-testid="r-video" /> }));

const RENDERER_IDS = [
  'r-image',
  'r-pdfcraft',
  'r-text',
  'r-html',
  'r-wordcraft',
  'r-gridcraft',
  'r-deckcraft',
  'r-video',
];

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
    ['application/pdf', 'doc.pdf', 'r-pdfcraft'],
    ['text/plain', 'notes.txt', 'r-text'],
    ['text/html', 'page.html', 'r-html'],
    ['application/octet-stream', 'memo.docx', 'r-wordcraft'],
    ['application/octet-stream', 'book.xlsx', 'r-gridcraft'],
    ['application/octet-stream', 'deck.pptx', 'r-deckcraft'],
    ['video/mp4', 'sea.mp4', 'r-video'],
    ['video/webm', 'sea.webm', 'r-video'],
  ];

  it.each(cases)('mime=%s name=%s mounts the matching renderer', async (mime, name, testid) => {
    render(<PreviewModal active={asset({ mime_type: mime, file_name: name })} onClose={vi.fn()} />);
    expect(await screen.findByTestId(testid)).toBeTruthy();
  });

  const downloadOnly: readonly (readonly [string, string])[] = [
    ['image/svg+xml', 'logo.svg'],
    ['application/vnd.ms-powerpoint', 'deck.ppt'],
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

// Where the source can store a save, a document, image or clip keeps its viewer and Edit opens it in
// its own Craft app.
describe('PreviewModal edit (a source that can save)', () => {
  const editable: readonly (readonly [string, string, string, string])[] = [
    ['image/png', 'cat.png', 'r-image', 'r-photocraft'],
    ['video/mp4', 'sea.mp4', 'r-video', 'r-filmcraft'],
  ];

  it.each(editable)(
    'mime=%s name=%s shows %s, and Edit opens %s',
    async (mime, name, viewer, app) => {
      render(
        <AssetSourceContext.Provider
          value={{ ...IDENTITY_SCOPED, save: vi.fn(() => Promise.resolve()) }}
        >
          <PreviewModal active={asset({ mime_type: mime, file_name: name })} onClose={vi.fn()} />
        </AssetSourceContext.Provider>,
      );
      expect(await screen.findByTestId(viewer)).toBeTruthy();
      fireEvent.click(screen.getByRole('button', { name: /^(Edit|Modifica)$/ }));
      expect(await screen.findByTestId(app)).toBeTruthy();
      expect(screen.queryByTestId(viewer)).toBeNull();
    },
  );

  // Floating over the viewer, Edit covered the PDF viewer's own toolbar: it belongs in the header,
  // beside the download and drawn the same way.
  it('puts Edit in the header beside the download, in the same style', async () => {
    render(
      <AssetSourceContext.Provider
        value={{ ...IDENTITY_SCOPED, save: vi.fn(() => Promise.resolve()) }}
      >
        <PreviewModal
          active={asset({ mime_type: 'image/png', file_name: 'photo.png' })}
          onClose={vi.fn()}
        />
      </AssetSourceContext.Provider>,
    );
    const edit = await screen.findByRole('button', { name: /^(Edit|Modifica)$/ });
    const download = screen.getByRole('link', { name: /download photo\.png|scarica photo\.png/i });
    const header = download.closest('[data-slot="preview-header"]');
    expect(header).not.toBeNull();
    expect(header?.contains(edit)).toBe(true);
    expect(edit.className).toBe(download.className);
  });
});

// An edit a Craft app has not saved lives only in the app: Esc, a click outside or ✕ dropped it.
describe('PreviewModal close (a preview with unsaved changes)', () => {
  const book = asset({ mime_type: 'application/octet-stream', file_name: 'book.xlsx' });

  it('holds the first close while the preview has unsaved changes, and closes on the second', async () => {
    const onClose = vi.fn();
    render(<PreviewModal active={book} onClose={onClose} />);
    const app = await screen.findByTestId('r-gridcraft');
    expect(app.dataset.held).toBe('false');
    fireEvent.click(screen.getByRole('button', { name: 'edit in gridcraft' }));
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
    expect(app.dataset.held).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes at once once the changes are saved', async () => {
    const onClose = vi.fn();
    render(<PreviewModal active={book} onClose={onClose} />);
    await screen.findByTestId('r-gridcraft');
    fireEvent.click(screen.getByRole('button', { name: 'edit in gridcraft' }));
    fireEvent.click(screen.getByRole('button', { name: 'save in gridcraft' }));
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
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
