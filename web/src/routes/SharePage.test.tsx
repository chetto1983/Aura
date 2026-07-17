import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { renderAsync } from 'docx-preview';
import '../i18n/i18n';
import type { Snapshot } from '../chat/share/shareTypes';
import { SharePage } from './SharePage';

// SharePage.test.tsx — covers every <behavior> row in 37F-16-PLAN.md, with the
// security assertions (null-origin iframe, SVG download-only, XSS-as-escaped-text,
// the oracle-free 404/401) as the centrepiece, per the plan's own acceptance criteria.
//
// docx-preview/xlsx are MOCKED (mirrors renderers.test.tsx) purely to exercise every
// previewKind branch in renderArtifactKind's switch without loading the real heavy
// parsers — the security-critical kinds (html/svg/image) get full assertions above;
// these two are presence-only, proving the dispatch wires up, not re-testing 37B.
vi.mock('docx-preview', () => ({
  renderAsync: vi.fn(() => Promise.resolve({})),
}));
vi.mock('xlsx', () => ({
  read: vi.fn(() => ({ SheetNames: ['Sheet1'], Sheets: { Sheet1: {} } })),
  utils: { sheet_to_html: vi.fn(() => '<table></table>') },
}));

const baseSnapshot: Snapshot = {
  schema_version: 1,
  title: 'Weekend trip to Turin',
  model: 'deepseek-v4',
  created_at: '2026-07-10T09:00:00.000Z',
  snapshot_at: '2026-07-17T12:00:00.000Z',
  turns: [
    { seq: 1, role: 'user', text: 'Plan a weekend trip to Turin.' },
    { seq: 2, role: 'assistant', text: 'Here is an itinerary.', tool_names: ['web_search'] },
  ],
  artifacts: [],
};

interface FetchStub {
  readonly snapshot?: Snapshot;
  readonly dataStatus?: number;
  readonly dataThrows?: boolean;
  readonly dataPending?: boolean;
  readonly dataMalformed?: boolean;
  readonly assetText?: string;
}

interface Call {
  readonly url: string;
  readonly init: RequestInit;
}

/** One fetch mock routing on the URL shape: any `…/data` path resolves (or 404s/
 *  throws) the snapshot; anything else is treated as an asset byte fetch — the SAME
 *  dispatcher every 37B renderer's own hook (useAssetContent/useBlobPreview) calls
 *  through useAssetSource(). */
function stubFetch(opts: FetchStub = {}) {
  const calls: Call[] = [];
  const fn = vi.fn((url: string, init: RequestInit = {}) => {
    calls.push({ url, init });
    if (url.includes('/data')) {
      if (opts.dataThrows === true) return Promise.reject(new TypeError('network down'));
      if (opts.dataPending === true) return new Promise<Response>(() => undefined);
      if (opts.dataStatus !== undefined && opts.dataStatus !== 200) {
        return Promise.resolve({ ok: false, status: opts.dataStatus } as Response);
      }
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () =>
          opts.dataMalformed === true
            ? Promise.reject(new SyntaxError('Unexpected token'))
            : Promise.resolve(opts.snapshot ?? baseSnapshot),
      } as unknown as Response);
    }
    const text = opts.assetText ?? '<h1>hello</h1>';
    return Promise.resolve({
      ok: true,
      status: 200,
      text: () => Promise.resolve(text),
      blob: () => Promise.resolve(new Blob([text])),
      arrayBuffer: () => Promise.resolve(new ArrayBuffer(8)),
    } as unknown as Response);
  });
  vi.stubGlobal('fetch', fn);
  return { fn, calls };
}

function renderShare(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/s/:token" element={<SharePage tier="public" />} />
        <Route path="/shared/:id" element={<SharePage tier="internal" />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('SharePage — public tier (/s/:token)', () => {
  it('fetches GET /s/{token}/data with credentials: omit', async () => {
    const { calls } = stubFetch();
    renderShare('/s/tok-123');
    await waitFor(() => {
      expect(screen.getByText('Weekend trip to Turin')).toBeTruthy();
    });
    const first = calls[0];
    if (!first) throw new Error('expected a fetch call');
    expect(first.url).toBe('/s/tok-123/data');
    expect(first.init.credentials).toBe('omit');
  });

  it('percent-encodes the token — never interpolated raw', async () => {
    const { calls } = stubFetch();
    renderShare(`/s/${encodeURIComponent('a b/c')}`);
    await waitFor(() => {
      expect(calls.length).toBeGreaterThan(0);
    });
    expect(calls[0]?.url).toBe(`/s/${encodeURIComponent('a b/c')}/data`);
  });

  it('renders the title, snapshot date, and turns in order with tool provenance', async () => {
    stubFetch();
    renderShare('/s/tok-123');
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Weekend trip to Turin' })).toBeTruthy();
    });
    const turns = screen.getAllByRole('article');
    expect(turns).toHaveLength(2);
    expect(turns[0]?.textContent).toContain('Plan a weekend trip to Turin.');
    expect(turns[1]?.textContent).toContain('Here is an itinerary.');
    expect(turns[1]?.textContent).toContain('web_search');
  });

  it('escapes an XSS payload in turn text — no element is created from it', async () => {
    stubFetch({
      snapshot: {
        ...baseSnapshot,
        turns: [{ seq: 1, role: 'assistant', text: '<img src=x onerror=alert(1)>' }],
      },
    });
    const { container } = renderShare('/s/tok-123');
    await waitFor(() => {
      expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeTruthy();
    });
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('script')).toBeNull();
  });

  it('renders an HTML artifact in a null-origin iframe fed via srcDoc', async () => {
    stubFetch({
      snapshot: {
        ...baseSnapshot,
        artifacts: [
          { asset_id: 'a1', filename: 'itinerary.html', mime_type: 'text/html', size_bytes: 128 },
        ],
      },
      assetText: '<h1>Turin</h1>',
    });
    const { container } = renderShare('/s/tok-123');
    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const frame = container.querySelector('iframe');
    if (!frame) throw new Error('expected an iframe');
    expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
    expect(frame.getAttribute('sandbox')).not.toContain('allow-same-origin');
    expect(frame.getAttribute('srcdoc')).toBe('<h1>Turin</h1>');
    expect(frame.getAttribute('src')).toBeNull();
  });

  it('renders an image artifact through the token-scoped provider (useBlobPreview fix)', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:mock');
    URL.revokeObjectURL = vi.fn();
    const { calls } = stubFetch({
      snapshot: {
        ...baseSnapshot,
        artifacts: [
          { asset_id: 'a2', filename: 'photo.png', mime_type: 'image/png', size_bytes: 900 },
        ],
      },
    });
    const { container } = renderShare('/s/tok-123');
    await waitFor(() => {
      expect(container.querySelector('img[alt="photo.png"]')).not.toBeNull();
    });
    const assetCall = calls.find((c) => c.url.includes('/asset/'));
    if (!assetCall) throw new Error('expected an asset fetch');
    expect(assetCall.url).toBe('/s/tok-123/asset/a2');
    expect(assetCall.init.credentials).toBe('omit');
  });

  it('renders an SVG artifact as download-only — never inline (previewKind gate)', async () => {
    stubFetch({
      snapshot: {
        ...baseSnapshot,
        artifacts: [
          { asset_id: 'a3', filename: 'logo.svg', mime_type: 'image/svg+xml', size_bytes: 300 },
        ],
      },
    });
    const { container } = renderShare('/s/tok-123');
    await waitFor(() => {
      expect(screen.getByText('logo.svg')).toBeTruthy();
    });
    const preview = container.querySelector('[data-artifact-preview]');
    expect(preview?.querySelector('svg')).toBeNull();
    expect(preview?.querySelector('img')).toBeNull();
  });

  it('shows a role="status" while loading', () => {
    stubFetch({ dataPending: true });
    renderShare('/s/tok-123');
    expect(screen.getByRole('status')).toBeTruthy();
  });

  it('shows a role="alert" on a network error (not the not-found body)', async () => {
    stubFetch({ dataThrows: true });
    renderShare('/s/tok-123');
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });

  it('shows a role="alert" on a malformed (ok but unparsable) response body', async () => {
    stubFetch({ dataMalformed: true });
    renderShare('/s/tok-123');
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });

  it('unknown, expired, and revoked tokens all render the IDENTICAL not-found body', async () => {
    const bodies: string[] = [];
    for (const token of ['unknown-tok', 'expired-tok', 'revoked-tok']) {
      stubFetch({ dataStatus: 404 });
      const { container, unmount } = renderShare(`/s/${token}`);
      await waitFor(() => {
        expect(container.textContent).not.toBe('');
      });
      bodies.push(container.innerHTML);
      unmount();
    }
    expect(bodies[0]).toBe(bodies[1]);
    expect(bodies[1]).toBe(bodies[2]);
  });

  it('renders no message input, no re-run action, and no duplicate-into-my-account affordance', async () => {
    stubFetch();
    renderShare('/s/tok-123');
    await waitFor(() => {
      expect(screen.getByRole('heading')).toBeTruthy();
    });
    expect(screen.queryByRole('textbox')).toBeNull();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('renders a PDF artifact via a native <iframe src>', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:mock-pdf');
    URL.revokeObjectURL = vi.fn();
    stubFetch({
      snapshot: {
        ...baseSnapshot,
        artifacts: [
          { asset_id: 'a4', filename: 'report.pdf', mime_type: 'application/pdf', size_bytes: 500 },
        ],
      },
    });
    const { container } = renderShare('/s/tok-123');
    await waitFor(() => {
      expect(container.querySelector('iframe[src="blob:mock-pdf"]')).not.toBeNull();
    });
  });

  it('renders a text artifact as escaped monospace text', async () => {
    stubFetch({
      snapshot: {
        ...baseSnapshot,
        artifacts: [
          { asset_id: 'a5', filename: 'notes.txt', mime_type: 'text/plain', size_bytes: 40 },
        ],
      },
      assetText: 'plain notes',
    });
    const { container } = renderShare('/s/tok-123');
    await waitFor(() => {
      expect(container.querySelector('pre')).not.toBeNull();
    });
    expect(container.querySelector('pre')?.textContent).toBe('plain notes');
  });

  it('renders a docx artifact through docx-preview.renderAsync', async () => {
    stubFetch({
      snapshot: {
        ...baseSnapshot,
        artifacts: [
          {
            asset_id: 'a6',
            filename: 'memo.docx',
            mime_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
            size_bytes: 700,
          },
        ],
      },
    });
    renderShare('/s/tok-123');
    await waitFor(() => {
      expect(vi.mocked(renderAsync)).toHaveBeenCalled();
    });
  });

  it('renders an xlsx artifact through SheetJS in an inert-sandbox iframe', async () => {
    stubFetch({
      snapshot: {
        ...baseSnapshot,
        artifacts: [
          {
            asset_id: 'a7',
            filename: 'book.xlsx',
            mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
            size_bytes: 800,
          },
        ],
      },
    });
    const { container } = renderShare('/s/tok-123');
    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
  });

  it('treats a missing route param as the not-found state (defensive fallback)', () => {
    stubFetch();
    render(
      <MemoryRouter initialEntries={['/s/tok-999']}>
        <Routes>
          {/* Deliberately mismatched: this route only ever supplies :token, so an
              "internal" tier reading params.id sees an empty routeKey — proving the
              guard holds even if a future caller wires the tier prop wrong. */}
          <Route path="/s/:token" element={<SharePage tier="internal" />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText(/expired|revoked|never existed/i)).toBeTruthy();
  });
});

describe('SharePage — internal tier (/shared/:id)', () => {
  it('fetches GET /api/shares/{id}/data with credentials: same-origin', async () => {
    const { calls } = stubFetch();
    renderShare('/shared/share-42');
    await waitFor(() => {
      expect(screen.getByText('Weekend trip to Turin')).toBeTruthy();
    });
    expect(calls[0]?.url).toBe('/api/shares/share-42/data');
    expect(calls[0]?.init.credentials).toBe('same-origin');
  });

  it('resolves an artifact via /api/shares/{id}/asset/{assetId} with credentials: same-origin', async () => {
    const { calls } = stubFetch({
      snapshot: {
        ...baseSnapshot,
        artifacts: [
          { asset_id: 'a1', filename: 'notes.html', mime_type: 'text/html', size_bytes: 64 },
        ],
      },
      assetText: '<p>notes</p>',
    });
    const { container } = renderShare('/shared/share-42');
    await waitFor(() => {
      expect(container.querySelector('iframe')).not.toBeNull();
    });
    const assetCall = calls.find((c) => c.url.includes('/asset/'));
    if (!assetCall) throw new Error('expected an asset fetch');
    expect(assetCall.url).toBe('/api/shares/share-42/asset/a1');
    expect(assetCall.init.credentials).toBe('same-origin');
  });

  it('renders the identical snapshot tree as the public tier (title + turns)', async () => {
    stubFetch();
    renderShare('/shared/share-42');
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Weekend trip to Turin' })).toBeTruthy();
    });
    expect(screen.getAllByRole('article')).toHaveLength(2);
  });

  it('an anonymous 401 renders the SAME not-found body as a public unknown-token 404', async () => {
    stubFetch({ dataStatus: 404 });
    const publicRender = renderShare('/s/unknown-tok');
    await waitFor(() => {
      expect(publicRender.container.textContent).not.toBe('');
    });
    const publicBody = publicRender.container.innerHTML;
    publicRender.unmount();

    stubFetch({ dataStatus: 401 });
    const internalRender = renderShare('/shared/foreign-id');
    await waitFor(() => {
      expect(internalRender.container.textContent).not.toBe('');
    });
    const internalBody = internalRender.container.innerHTML;

    expect(internalBody).toBe(publicBody);
  });
});
