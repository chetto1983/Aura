import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { renderAsync } from 'docx-preview';
import readXlsxFile from 'read-excel-file/universal';
import '../../../i18n/i18n'; // side-effect: initialise i18next so preview t() keys resolve
import ImagePreview from './ImagePreview';
import PdfPreview from './PdfPreview';
import TextPreview from './TextPreview';
import HtmlPreview from './HtmlPreview';
import { AssetSourceContext, type AssetSource } from './assetSourceContext';
import DocxPreview from './DocxPreview';
import XlsxPreview from './XlsxPreview';

// The six lazy per-MIME renderers (D-07/D-08/D-09). docx-preview and read-excel-file are the
// heavy deps confined to their own chunks — here they are MOCKED (vi.mock, hoisted above the
// imports) so the suite asserts the wrapper calls them with the right args without loading
// the real libraries (Pitfall 4, which would drag the 85% aggregate). The security-critical
// surface is pinned directly: HtmlPreview's null-origin sandbox and XlsxPreview's empty
// sandbox + escaped sheet name + escaped cell values (the escaping is OURS now — the library
// returns raw values, unlike SheetJS's sheet_to_html).
vi.mock('docx-preview', () => ({
  renderAsync: vi.fn(() => Promise.resolve({})),
}));
vi.mock('read-excel-file/universal', () => ({
  default: vi.fn(() =>
    Promise.resolve([
      {
        sheet: 'Q1 <Ledger>',
        data: [
          ['<img src=x onerror=alert(1)>', 42, null],
          [new Date('2026-02-03T00:00:00.000Z'), new Date('2026-02-03T04:05:06.000Z'), true],
        ],
      },
    ]),
  ),
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
  vi.mocked(readXlsxFile).mockClear();
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
  // These assertions were rewritten when the rendered lane moved from srcdoc to the sealed
  // src= document: the OLD test pinned `srcdoc == the fetched bytes`, which is exactly the
  // property the change removes (a srcdoc document inherits the embedder's CSP and can carry
  // no policy of its own). The null-origin invariant it also pinned is preserved verbatim
  // below, and the srcdoc lane still has a test of its own — on the tier that still uses it.
  it('rendered lane frames the SEALED document: src=/render, never srcdoc, null origin', async () => {
    stubFetch({ text: '<h1>hello</h1>' });
    const { container } = render(<HtmlPreview {...props} fileName="page.html" />);

    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const frame = iframeOf(container);
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame.getAttribute('sandbox')).not.toContain('allow-same-origin');
    expect(frame.getAttribute('src')).toBe('/api/assets/asset-1/render');
    expect(frame.getAttribute('srcdoc')).toBeNull();
  });

  it('does not fetch the body for the rendered lane — the frame does', () => {
    stubFetch({ text: '<h1>hello</h1>' });
    render(<HtmlPreview {...props} fileName="page.html" />);
    expect(vi.mocked(fetch)).not.toHaveBeenCalled();
  });

  it('percent-encodes the asset id in the render URL', async () => {
    stubFetch({ text: 'x' });
    const { container } = render(<HtmlPreview {...props} assetId="a/b?c" fileName="page.html" />);
    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    expect(iframeOf(container).getAttribute('src')).toBe('/api/assets/a%2Fb%3Fc/render');
  });

  it('source tab shows the markup ESCAPED, never parsed', async () => {
    stubFetch({ text: '<script>alert(1)</script>' });
    const { container } = render(<HtmlPreview {...props} fileName="page.html" />);
    fireEvent.click(screen.getByRole('tab', { name: /source|sorgente/i }));

    await waitFor(() => {
      expect(container.querySelector('pre')).not.toBeNull();
    });
    const pre = container.querySelector('pre');
    expect(pre?.textContent).toBe('<script>alert(1)</script>');
    // Escaped, so the markup never became a node of ours.
    expect(pre?.querySelector('script')).toBeNull();
  });

  it('shows the error state when the SOURCE fetch fails', async () => {
    stubFetch({ ok: false, status: 500 });
    render(<HtmlPreview {...props} fileName="page.html" />);
    fireEvent.click(screen.getByRole('tab', { name: /source|sorgente/i }));
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });

  it('falls back to the null-origin srcdoc lane when the tier has no render route', async () => {
    stubFetch({ text: '<h1>shared</h1>' });
    const shareTier: AssetSource = {
      assetUrl: (id) => `/s/tok/asset/${id}`,
      credentials: 'omit',
    };
    const { container } = render(
      <AssetSourceContext.Provider value={shareTier}>
        <HtmlPreview {...props} fileName="page.html" />
      </AssetSourceContext.Provider>,
    );

    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const frame = iframeOf(container);
    expect(frame.getAttribute('srcdoc')).toBe('<h1>shared</h1>');
    expect(frame.getAttribute('src')).toBeNull();
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
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
  it('parses with read-excel-file and renders an EMPTY-sandbox iframe with everything escaped', async () => {
    stubFetch({ buffer: new ArrayBuffer(16) });
    const { container } = render(<XlsxPreview {...props} fileName="book.xlsx" />);

    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const frame = iframeOf(container);
    expect(vi.mocked(readXlsxFile)).toHaveBeenCalledTimes(1);
    expect(frame.getAttribute('sandbox')).toBe('');
    const srcdoc = frame.getAttribute('srcdoc') ?? '';
    // Sheet NAME and cell VALUES are escaped by us — a hostile cell must never survive as
    // markup in the srcDoc, only as entities.
    expect(srcdoc).toContain('Q1 &lt;Ledger&gt;');
    expect(srcdoc).toContain('&lt;img src=x onerror=alert(1)&gt;');
    expect(srcdoc).not.toContain('<img');
    expect(srcdoc).toContain('<td>42</td>');
    expect(srcdoc).toContain('<td></td>'); // null cell → empty
    expect(srcdoc).toContain('<td>2026-02-03</td>'); // midnight-UTC date → date only
    expect(srcdoc).toContain('<td>2026-02-03T04:05:06.000Z</td>'); // timed date → full ISO
    expect(srcdoc).toContain('<td>true</td>');
    expect(srcdoc).toContain('<table>');
  });

  it('shows the loading state while the fetch is in flight', () => {
    stubFetch({ pending: true });
    render(<XlsxPreview {...props} fileName="book.xlsx" />);
    expect(screen.getByRole('status')).toBeTruthy();
  });

  it('emits a section without a table for a sheet with zero rows', async () => {
    stubFetch({ buffer: new ArrayBuffer(16) });
    vi.mocked(readXlsxFile).mockResolvedValueOnce([{ sheet: 'Ghost', data: [] }]);
    const { container } = render(<XlsxPreview {...props} fileName="book.xlsx" />);
    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const srcdoc = iframeOf(container).getAttribute('srcdoc') ?? '';
    expect(srcdoc).toContain('<h3>Ghost</h3>');
    expect(srcdoc).not.toContain('<table>');
  });

  it('shows the error state when parsing rejects', async () => {
    stubFetch({ buffer: new ArrayBuffer(16) });
    vi.mocked(readXlsxFile).mockRejectedValueOnce(new Error('corrupt workbook'));
    render(<XlsxPreview {...props} fileName="book.xlsx" />);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });
});
