import { act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { PAGE, renderDocuments, stubServer } from './documentsTestServer';
import i18n from '@/i18n/i18n';

afterEach(() => {
  vi.unstubAllGlobals();
});
it('keeps late content results out of a newer filename query and recovers from empty/error states', async () => {
  await act(() => i18n.changeLanguage('en'));
  stubServer();
  const original = fetch;
  let finish: (response: Response) => void = () => undefined;
  let requestSignal: AbortSignal | null | undefined;
  let retry = false;
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      const url = new URL(raw, 'http://test');
      if (url.pathname === '/api/documents' && url.searchParams.get('q') === 'old') {
        requestSignal = init?.signal;
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      }
      if (url.pathname === '/api/documents' && url.searchParams.get('q') === 'new') {
        return Promise.resolve(new Response(JSON.stringify({ rows: [], total: 0, facets: {} })));
      }
      if (url.pathname === '/api/documents' && url.searchParams.get('q') === 'retry' && !retry) {
        return Promise.resolve(new Response('{}', { status: 503 }));
      }
      return original(input, init);
    }),
  );
  renderDocuments('/documents/library?display=compact');
  const input = await screen.findByRole('searchbox', { name: 'Search documents' });
  fireEvent.change(input, { target: { value: 'old' } });
  await waitFor(() => {
    expect(requestSignal).toBeDefined();
  });
  fireEvent.change(input, { target: { value: 'new' } });
  expect(await screen.findByText('No documents match this view.')).toBeTruthy();
  await act(async () => {
    finish(new Response(JSON.stringify(PAGE)));
    await Promise.resolve();
  });
  expect(requestSignal?.aborted).toBe(true);
  expect(screen.queryByText('fattura_2026_044.pdf')).toBeNull();
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Search in' }), { key: 'ArrowDown' });
  fireEvent.click(await screen.findByRole('option', { name: 'Filename' }));
  fireEvent.change(input, { target: { value: 'retry' } });
  expect(await screen.findByRole('alert')).toBeTruthy();
  retry = true;
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  expect(await screen.findByText('fattura_2026_044.pdf')).toBeTruthy();
});

it('keeps custom equality/unassigned chips and confirms a custom bulk value on the displayed media filter', async () => {
  await act(() => i18n.changeLanguage('en'));
  const server = stubServer();
  renderDocuments(
    '/documents/library?file_type=video&display=compact&inbox=false&search_mode=content',
  );
  fireEvent.click(await screen.findByRole('button', { name: 'Custom filters' }));
  fireEvent.change(screen.getByRole('textbox', { name: 'Numero fattura' }), {
    target: { value: 'Cedar' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
  await waitFor(() => {
    expect(server.lists.at(-1)?.get('custom')).toBe(
      '[{"field":"custom:cf-numero","operator":"eq","value":"Cedar"}]',
    );
  });
  fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select fattura_2026_031.pdf' }));
  fireEvent.click(screen.getByRole('button', { name: 'select all 24 in the filter' }));
  fireEvent.click(screen.getByRole('button', { name: 'Edit custom fields' }));
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Stato: Edit custom fields' }), {
    key: 'ArrowDown',
  });
  fireEvent.click(await screen.findByRole('option', { name: 'Set value' }));
  fireEvent.keyDown(screen.getByRole('combobox', { name: 'Stato' }), { key: 'ArrowDown' });
  fireEvent.click(await screen.findByRole('option', { name: 'pagata' }));
  fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' });
  fireEvent.click(screen.getByRole('button', { name: 'Apply' }));
  const dialog = await screen.findByRole('dialog', { name: 'Confirm the change?' });
  expect(dialog.textContent).toContain('Stato');
  expect(dialog.textContent).toContain('pagata');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Confirm' }));
  await waitFor(() => {
    expect(server.posts).toHaveLength(1);
  });
  expect(server.posts[0]).toMatchObject({
    selection: {
      filter: {
        file_type: 'video',
        search_mode: 'content',
        custom: [{ field: 'custom:cf-numero', operator: 'eq', value: 'Cedar' }],
      },
    },
    diff: { set: [{ field: 'custom:cf-stato', value_key: 'pagata' }], remove: [] },
  });
});
