import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DETAILS, renderDetail, renderDocuments, stubServer } from './documentsTestServer';
import i18n from '@/i18n/i18n';
import { mediaQueryList } from '@/test/mediaQuery';

beforeEach(async () => {
  await act(() => i18n.changeLanguage('en'));
});
afterEach(() => vi.unstubAllGlobals());
const invoice = DETAILS.d2;
const note = DETAILS.d3;
if (!invoice || !note) throw new Error('Missing document fixtures');
const path =
  '/documents/document/d2?from=%2Fdocuments%2Finbox&inbox=true&file_type=all&sort=file_name&limit=25&offset=0&display=cards';
function urlOf(input: RequestInfo | URL) {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}
function server() {
  const result = stubServer();
  const original = fetch;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(urlOf(input), location.origin);
      if (url.pathname === '/api/documents' && url.searchParams.get('inbox') === 'true') {
        return Promise.resolve(
          Response.json({ rows: [DETAILS.d2, DETAILS.d3], total: 2, facets: {} }),
        );
      }
      if (url.pathname === '/api/filemanager/direct') return Promise.resolve(new Response('pdf'));
      if (url.pathname.endsWith('/content'))
        return Promise.resolve(
          Response.json({
            passages: [{ ordinal: 0, text: 'Cedar indexed passage' }],
            offset: 0,
            has_more: false,
          }),
        );
      return original(input, init);
    }),
  );
  return result;
}
it('opens a direct link with content/history tabs and returns to its exact result context', async () => {
  server();
  renderDocuments(path);
  await screen.findByRole('heading', { name: invoice.file_name });
  fireEvent.mouseDown(screen.getByRole('tab', { name: 'Content' }), { button: 0, ctrlKey: false });
  expect(await screen.findByText('Cedar indexed passage')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Documents' }));
  await screen.findByRole('heading', { name: 'Inbox' });
  expect(screen.getByRole('combobox', { name: 'File type' }).textContent).toContain('All files');
});

it('navigates previous and next within current Inbox results without a suggestions query', async () => {
  server();
  renderDocuments(path);
  const next = await screen.findByRole('button', { name: 'Next document' });
  await waitFor(() => {
    expect(next).toHaveProperty('disabled', false);
  });
  fireEvent.click(next);
  expect(await screen.findByRole('heading', { name: note.file_name })).toBeTruthy();
  fireEvent.click(await screen.findByRole('button', { name: 'Previous document' }));
  expect(await screen.findByRole('heading', { name: invoice.file_name })).toBeTruthy();
  expect(
    vi.mocked(fetch).mock.calls.some(([url]) => urlOf(url).includes('needs_review=true&limit=2')),
  ).toBe(false);
});

it('keeps the native iframe mounted across tabs and cancellation dispatches no review', async () => {
  const { writes } = server();
  renderDocuments(path);
  const frame = (await screen.findAllByTitle(invoice.file_name)).find(
    (element) => element.tagName === 'IFRAME',
  );
  if (!(frame instanceof HTMLIFrameElement)) throw new Error('Missing native frame');
  void act(() =>
    window.dispatchEvent(
      new MessageEvent('message', {
        origin: location.origin,
        source: frame.contentWindow,
        data: { type: 'pdfcraft:dirty', dirty: true },
      }),
    ),
  );
  fireEvent.mouseDown(screen.getByRole('tab', { name: 'History' }), { button: 0, ctrlKey: false });
  expect(screen.getAllByTitle(invoice.file_name)).toContain(frame);
  fireEvent.click(screen.getByRole('button', { name: 'Reviewed, save and next' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Stay' }));
  expect(writes).toHaveLength(0);
});

it('shows failed labels and a retry instead of treating unavailable lists as empty', async () => {
  server();
  const original = fetch;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
      urlOf(input).includes('/labels?')
        ? Promise.resolve(new Response('', { status: 503 }))
        : original(input, init),
    ),
  );
  renderDocuments(path);
  expect(await screen.findByText('Could not load organization lists.')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Retry lists' })).toBeTruthy();
});

it('creates a searched label in context and preserves existing unsaved edits', async () => {
  const { writes } = server();
  renderDocuments(path);
  fireEvent.change(await screen.findByLabelText('Document date'), {
    target: { value: '2026-10-22' },
  });
  fireEvent.click(screen.getByRole('combobox', { name: 'Counterparty' }));
  expect(screen.getByRole('listbox', { name: 'Counterparty' })).toBeTruthy();
  fireEvent.change(screen.getByRole('combobox', { name: 'Search or create a label…' }), {
    target: { value: 'New vendor' },
  });
  fireEvent.click(screen.getByRole('option', { name: 'Create “New vendor”' }));
  await waitFor(() => {
    expect(writes).toHaveLength(1);
  });
  expect(screen.getByLabelText('Document date')).toHaveProperty('value', '2026-10-22');
  fireEvent.click(screen.getByRole('button', { name: 'Save details' }));
  await waitFor(() => {
    expect(writes).toHaveLength(2);
  });
  expect(writes[1]?.body).toEqual({
    set: [
      { field: 'counterparty', value_key: 'c-verdi' },
      { field: 'document_date', value_key: '2026-10-22' },
    ],
    remove: [],
  });
});

it('preserves untouched refreshed fields while saving only the intended edit', async () => {
  const { writes } = server();
  const { client } = renderDetail('d2');
  fireEvent.change(await screen.findByLabelText('Document date'), {
    target: { value: '2026-10-22' },
  });
  void act(() =>
    client.setQueryData(['document', 'documents-test', 'd2'], {
      ...DETAILS.d2,
      values: [
        ...invoice.values,
        {
          field: 'type',
          value_key: 't-contratto',
          accepted: true,
          set_by: 'operator',
          set_at: '2026-10-22',
        },
      ],
    }),
  );
  await waitFor(() => {
    expect(screen.getByRole('combobox', { name: 'Type' }).textContent).toBe('Contratto');
  });
  fireEvent.click(screen.getByRole('button', { name: 'Save details' }));
  await waitFor(() => {
    expect(writes).toHaveLength(1);
  });
  expect(writes[0]?.body).toEqual({
    set: [{ field: 'document_date', value_key: '2026-10-22' }],
    remove: [],
  });
});

it('aborts the metadata chain on identity change before dispatching review', async () => {
  server();
  const original = fetch;
  let resolve!: (response: Response) => void;
  let held: AbortSignal | undefined;
  const reviews: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'PATCH') {
        held = init.signal ?? undefined;
        return new Promise<Response>((done) => {
          resolve = done;
        });
      }
      if (urlOf(input).endsWith('/review')) reviews.push(urlOf(input));
      return original(input, init);
    }),
  );
  const { client } = renderDetail('d2');
  fireEvent.change(await screen.findByLabelText('Document date'), {
    target: { value: '2026-10-22' },
  });
  const review = screen.getByRole('button', { name: 'Reviewed, save and next' });
  await waitFor(() => {
    expect(review).toHaveProperty('disabled', false);
  });
  fireEvent.click(review);
  await waitFor(() => {
    expect(held).toBeDefined();
  });
  void act(() => client.setQueryData(['me'], { identity_id: 'other', capabilities: [] }));
  await waitFor(() => {
    expect(held?.aborted).toBe(true);
  });
  await act(async () => {
    resolve(Response.json({ ok: true }));
    await Promise.resolve();
  });
  expect(reviews).toEqual([]);
});

it('still guards route navigation after a confirmed file-discard review fails', async () => {
  server();
  const original = fetch;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
      urlOf(input).endsWith('/review')
        ? Promise.resolve(new Response('', { status: 503 }))
        : original(input, init),
    ),
  );
  renderDocuments(path);
  const frame = await waitFor(() => {
    const found = document.querySelector('iframe');
    if (!found) throw new Error('Missing native frame');
    return found;
  });
  void act(() =>
    window.dispatchEvent(
      new MessageEvent('message', {
        origin: location.origin,
        source: frame.contentWindow,
        data: { type: 'pdfcraft:dirty', dirty: true },
      }),
    ),
  );
  const review = screen.getByRole('button', { name: 'Reviewed, save and next' });
  await waitFor(() => {
    expect(review).toHaveProperty('disabled', false);
  });
  fireEvent.click(review);
  fireEvent.click(
    within(await screen.findByRole('dialog')).getByRole('button', { name: 'Close without saving' }),
  );
  await screen.findByText(/The change did not go through/);
  fireEvent.click(screen.getByRole('link', { name: 'Overview' }));
  expect(await screen.findByRole('dialog')).toBeTruthy();
});

it('blocks browser Back with metadata edits and restores the same result after discard', async () => {
  server();
  const { router } = renderDocuments('/documents/inbox?file_type=all&display=cards');
  await screen.findByRole('heading', { name: 'Inbox' });
  await act(async () => {
    await router.navigate(path);
  });
  fireEvent.change(await screen.findByLabelText('Document date'), {
    target: { value: '2026-10-22' },
  });
  await act(async () => {
    await router.navigate(-1);
  });
  let dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Stay' }));
  expect(screen.getByLabelText('Document date')).toHaveProperty('value', '2026-10-22');
  await act(async () => {
    await router.navigate(-1);
  });
  dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Close without saving' }));
  await screen.findByRole('heading', { name: 'Inbox' });
  expect(router.state.location.search).toBe('?file_type=all&display=cards');
});

it.each([false, true])(
  'connects every detail tab to its named panel and retains the native frame (mobile=%s)',
  async (mobile) => {
    vi.stubGlobal('matchMedia', (query: string) => mediaQueryList(query, mobile));
    server();
    renderDocuments(path);
    await screen.findByRole('heading', { name: invoice.file_name });
    const frame = await waitFor(() => {
      const found = document.querySelector('iframe');
      if (!found) throw new Error('Missing native frame');
      return found;
    });
    const nativeWindow = frame.contentWindow;
    for (const trigger of screen.getAllByRole('tab')) {
      const panel = document.getElementById(trigger.getAttribute('aria-controls') ?? '');
      expect(panel?.getAttribute('role')).toBe('tabpanel');
      expect(panel?.getAttribute('aria-labelledby')).toBe(trigger.id);
    }
    act(() => {
      window.dispatchEvent(
        new MessageEvent('message', {
          source: nativeWindow,
          origin: location.origin,
          data: { type: 'pdfcraft:dirty', dirty: true },
        }),
      );
    });
    for (const name of [
      'Details',
      'Content',
      'History',
      ...(mobile ? ['Preview', 'Details', 'Preview'] : []),
    ]) {
      const trigger = screen.getByRole('tab', { name });
      fireEvent.mouseDown(trigger, { button: 0, ctrlKey: false });
      const panel = await screen.findByRole('tabpanel', { name });
      expect(trigger.getAttribute('aria-controls')).toBe(panel.id);
      expect(panel.getAttribute('aria-labelledby')).toBe(trigger.id);
      expect(trigger.getAttribute('aria-selected')).toBe('true');
      expect(document.querySelector('iframe')).toBe(frame);
      expect(frame.contentWindow).toBe(nativeWindow);
    }
    fireEvent.click(screen.getByRole('button', { name: 'Documents' }));
    expect(await screen.findByRole('dialog', { name: 'Close without saving?' })).toBeTruthy();
  },
);
