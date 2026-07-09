import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { renderAsync } from 'docx-preview';
import { read } from 'xlsx';
import '../../../i18n/i18n'; // side-effect: initialise i18next so preview t() keys resolve
import ImagePreview from './ImagePreview';
import PdfPreview from './PdfPreview';
import TextPreview from './TextPreview';
import HtmlPreview from './HtmlPreview';
import DocxPreview from './DocxPreview';
import XlsxPreview from './XlsxPreview';

// The six lazy per-MIME renderers (D-07/D-08/D-09). docx-preview and xlsx are the heavy
// deps confined to their own chunks — here they are MOCKED (vi.mock, hoisted above the
// imports) so the suite asserts the wrapper calls them with the right args without loading
// the real libraries (Pitfall 4, which would drag the 85% aggregate). The security-critical
// surface is pinned directly: HtmlPreview's null-origin sandbox and XlsxPreview's empty
// sandbox + escaped sheet name.
vi.mock('docx-preview', () => ({
  renderAsync: vi.fn(() => Promise.resolve({})),
}));
vi.mock('xlsx', () => ({
  read: vi.fn(() => ({ SheetNames: ['Q1 <Ledger>'], Sheets: { 'Q1 <Ledger>': {} } })),
  utils: { sheet_to_html: vi.fn(() => '<table><tr><td>42</td></tr></table>') },
}));

interface FetchShape {
  readonly ok?: boolean;
  readonly status?: number;
  readonly text?: string;
  readonly blob?: Blob;
  readonly buffer?: ArrayBuffer;
  readonly pending?: boolean;
}

function stubFetch(shape: FetchShape): void {
  const res = {
    ok: shape.ok ?? true,
    status: shape.status ?? 200,
    text: () => Promise.resolve(shape.text ?? ''),
    blob: () => Promise.resolve(shape.blob ?? new Blob(['docx-bytes'])),
    arrayBuffer: () => Promise.resolve(shape.buffer ?? new ArrayBuffer(8)),
  } as unknown as Response;
  vi.stubGlobal(
    'fetch',
    vi.fn(() =>
      shape.pending === true ? new Promise<Response>(() => undefined) : Promise.resolve(res),
    ),
  );
}

function iframeOf(container: HTMLElement): HTMLIFrameElement {
  const frame = container.querySelector('iframe');
  if (frame === null) throw new Error('expected an <iframe>');
  return frame;
}

const props = { assetId: 'asset-1', mimeType: 'application/octet-stream', fileName: 'report' };

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => 'blob:mock');
  URL.revokeObjectURL = vi.fn();
  vi.mocked(renderAsync).mockClear();
  vi.mocked(read).mockClear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ImagePreview', () => {
  it('renders an <img> from the relabelled object URL', async () => {
    stubFetch({ blob: new Blob(['img']) });
    const { container } = render(
      <ImagePreview {...props} mimeType="image/png" fileName="cat.png" />,
    );

    await waitFor(() => {
      expect(container.querySelector('img')).not.toBeNull();
    });
    const img = container.querySelector('img');
    expect(img?.getAttribute('src')).toBe('blob:mock');
    expect(img?.getAttribute('alt')).toBe('cat.png');
  });

  it('shows the loading state while the fetch is in flight', () => {
    stubFetch({ pending: true });
    render(<ImagePreview {...props} mimeType="image/png" />);
    expect(screen.getByRole('status')).toBeTruthy();
  });

  it('shows the error state on a non-ok response', async () => {
    stubFetch({ ok: false, status: 404 });
    render(<ImagePreview {...props} mimeType="image/png" />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });
});

describe('PdfPreview', () => {
  it('renders a native <iframe src> from the object URL', async () => {
    stubFetch({ blob: new Blob(['pdf']) });
    const { container } = render(<PdfPreview {...props} mimeType="application/pdf" />);

    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    expect(iframeOf(container).getAttribute('src')).toBe('blob:mock');
  });

  it('shows the error state on a non-ok response', async () => {
    stubFetch({ ok: false, status: 404 });
    render(<PdfPreview {...props} mimeType="application/pdf" />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });
});

describe('TextPreview', () => {
  it('renders fetched text React-escaped inside a <pre> (no HTML interpretation)', async () => {
    stubFetch({ text: 'line1\n<script>alert(1)</script>' });
    const { container } = render(<TextPreview {...props} />);

    await waitFor(() => {
      expect(container.querySelector('pre')).not.toBeNull();
    });
    const pre = container.querySelector('pre');
    // React escapes the bytes: the literal text is present, no real <script> element exists.
    expect(pre?.textContent).toContain('<script>alert(1)</script>');
    expect(container.querySelector('script')).toBeNull();
  });

  it('shows the error state on a non-ok response', async () => {
    stubFetch({ ok: false, status: 500 });
    render(<TextPreview {...props} />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });
});

describe('HtmlPreview (WEBART-07 / T-37B-08)', () => {
  it('renders a NULL-ORIGIN iframe: sandbox=allow-scripts, no allow-same-origin, srcDoc not src', async () => {
    stubFetch({ text: '<h1>hello</h1>' });
    const { container } = render(<HtmlPreview {...props} fileName="page.html" />);

    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const frame = iframeOf(container);
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame.getAttribute('sandbox')).not.toContain('allow-same-origin');
    expect(frame.getAttribute('srcdoc')).toBe('<h1>hello</h1>');
    expect(frame.getAttribute('src')).toBeNull();
  });

  it('shows the error state when the fetch fails', async () => {
    stubFetch({ ok: false, status: 500 });
    render(<HtmlPreview {...props} fileName="page.html" />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });
});

describe('DocxPreview (D-08 / T-37B-12)', () => {
  it('calls docx-preview.renderAsync with the fetched Blob and the aura-docx className', async () => {
    stubFetch({ blob: new Blob(['docx']) });
    render(<DocxPreview {...props} fileName="memo.docx" />);

    await waitFor(() => {
      expect(vi.mocked(renderAsync)).toHaveBeenCalledTimes(1);
    });
    expect(vi.mocked(renderAsync)).toHaveBeenCalledWith(
      expect.any(Blob),
      expect.any(HTMLElement),
      expect.anything(),
      expect.objectContaining({
        className: 'aura-docx',
        inWrapper: true,
        ignoreLastRenderedPageBreak: true,
      }),
    );
  });

  it('shows the error state when renderAsync rejects', async () => {
    stubFetch({ blob: new Blob(['docx']) });
    vi.mocked(renderAsync).mockRejectedValueOnce(new Error('corrupt docx'));
    render(<DocxPreview {...props} fileName="memo.docx" />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });

  it('shows the error state when the fetch fails', async () => {
    stubFetch({ ok: false, status: 404 });
    render(<DocxPreview {...props} fileName="memo.docx" />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
    expect(vi.mocked(renderAsync)).not.toHaveBeenCalled();
  });
});

describe('XlsxPreview (D-08 / T-37B-09)', () => {
  it('parses with SheetJS and renders an EMPTY-sandbox iframe with the escaped sheet name', async () => {
    stubFetch({ buffer: new ArrayBuffer(16) });
    const { container } = render(<XlsxPreview {...props} fileName="book.xlsx" />);

    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const frame = iframeOf(container);
    expect(vi.mocked(read)).toHaveBeenCalledTimes(1);
    expect(frame.getAttribute('sandbox')).toBe('');
    const srcdoc = frame.getAttribute('srcdoc') ?? '';
    // Sheet NAME is escaped by us (< and > → entities); the cell table is included verbatim.
    expect(srcdoc).toContain('Q1 &lt;Ledger&gt;');
    expect(srcdoc).toContain('<table>');
  });

  it('shows the loading state while the fetch is in flight', () => {
    stubFetch({ pending: true });
    render(<XlsxPreview {...props} fileName="book.xlsx" />);
    expect(screen.getByRole('status')).toBeTruthy();
  });

  it('emits an empty table for a sheet name with no matching worksheet', async () => {
    stubFetch({ buffer: new ArrayBuffer(16) });
    // SheetNames lists a name absent from Sheets → the `sheet ? … : ''` false branch.
    vi.mocked(read).mockReturnValueOnce({ SheetNames: ['Ghost'], Sheets: {} });
    const { container } = render(<XlsxPreview {...props} fileName="book.xlsx" />);
    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const srcdoc = iframeOf(container).getAttribute('srcdoc') ?? '';
    expect(srcdoc).toContain('<h3>Ghost</h3>');
    expect(srcdoc).not.toContain('<table>');
  });

  it('shows the error state when SheetJS parsing throws', async () => {
    stubFetch({ buffer: new ArrayBuffer(16) });
    vi.mocked(read).mockImplementationOnce(() => {
      throw new Error('corrupt workbook');
    });
    render(<XlsxPreview {...props} fileName="book.xlsx" />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });
});
