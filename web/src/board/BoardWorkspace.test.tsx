import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import BoardWorkspace from './BoardWorkspace';
import i18n from '@/i18n/i18n';

// BoardWorkspace mounts the real SVAR Kanban against a stubbed server. What it pins is what the
// widget alone would get wrong: a delete with no confirmation, a refused write that looks
// stored, a column edit that never reaches the server, and a view that filters nothing.

interface Call {
  readonly url: string;
  readonly method: string;
  readonly body: unknown;
}

const COLUMNS = [
  { id: 'todo', label: 'To do' },
  { id: 'doing', label: 'Doing', cardLimit: 5 },
  { id: 'done', label: 'Done' },
];

const CARDS = [
  {
    id: 'c-1',
    label: 'Call the supplier',
    description: '',
    column: 'todo',
    priority: 2,
    tags: ['ops'],
    conversation_id: 'conv-1',
    task_id: 'task-1',
    source: 'chat',
    updated_by: 'agent',
    updated_at: '2026-10-09T10:00:00Z',
  },
  {
    id: 'c-2',
    label: 'Pay the invoice',
    description: '',
    column: 'doing',
    priority: 3,
    tags: [],
    source: 'cockpit',
    updated_by: 'operator',
    updated_at: '2026-10-09T10:00:00Z',
  },
];

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function stubServer(
  opts: {
    readonly writeStatus?: number;
    readonly writeBody?: unknown;
    readonly views?: unknown;
  } = {},
) {
  const calls: Call[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = urlOf(input);
      const method = init?.method ?? 'GET';
      const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined;
      calls.push({ url, method, body });
      if (method === 'GET') {
        const read = url.endsWith('/cards')
          ? CARDS
          : url.endsWith('/views')
            ? (opts.views ?? [])
            : url.includes('/governance/scheduler')
              ? { tasks: [{ ID: 'task-1', Status: 'active' }] }
              : { name: 'main', columns: COLUMNS };
        return Promise.resolve(new Response(JSON.stringify(read), { status: 200 }));
      }
      const written = opts.writeBody ?? { id: 'c-new', columns: COLUMNS };
      return Promise.resolve(
        new Response(JSON.stringify(written), { status: opts.writeStatus ?? 200 }),
      );
    }),
  );
  return calls;
}

function renderBoard(onDiscuss = vi.fn()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(
    <QueryClientProvider client={client}>
      <BoardWorkspace onDiscuss={onDiscuss} />
    </QueryClientProvider>,
  );
  return { ...view, onDiscuss };
}

async function openCardMenu(label: string) {
  const card = (await screen.findByText(label)).closest('[data-kanban-card-id]');
  const trigger = card?.querySelector('[data-action="menu"]');
  if (!(trigger instanceof HTMLElement)) throw new Error(`no menu on ${label}`);
  fireEvent.click(trigger);
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('BoardWorkspace', () => {
  it('shows the columns and the cards the server holds', async () => {
    await act(() => i18n.changeLanguage('en'));
    stubServer();
    renderBoard();
    expect(await screen.findByText('Call the supplier')).toBeTruthy();
    expect(screen.getByText('Pay the invoice')).toBeTruthy();
    expect(screen.getByText('Doing')).toBeTruthy();
    expect(screen.getByRole('region', { name: 'Kanban board' })).toBeTruthy();
  });

  it('speaks the cockpit language in the widget chrome', async () => {
    await act(() => i18n.changeLanguage('it'));
    stubServer();
    renderBoard();
    expect(await screen.findByRole('region', { name: 'Bacheca Kanban' })).toBeTruthy();
    expect(screen.getAllByText('Aura in chat').length).toBeGreaterThan(0);
    await act(() => i18n.changeLanguage('en'));
  });

  it('filters the board by a source, and the whole board comes back with All cards', async () => {
    await act(() => i18n.changeLanguage('en'));
    stubServer();
    renderBoard();
    await screen.findByText('Call the supplier');
    fireEvent.change(screen.getByRole('combobox', { name: 'Source' }), {
      target: { value: 'cockpit' },
    });
    await waitFor(() => {
      expect(screen.queryByText('Call the supplier')).toBeNull();
    });
    expect(screen.getByText('Pay the invoice')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'All cards' }));
    expect(await screen.findByText('Call the supplier')).toBeTruthy();
  });

  it('asks before a delete, and deletes only once confirmed', async () => {
    await act(() => i18n.changeLanguage('en'));
    const calls = stubServer();
    renderBoard();
    await openCardMenu('Pay the invoice');
    fireEvent.click(await screen.findByText('Delete card'));
    const dialog = await screen.findByRole('dialog', { name: 'Delete this card?' });
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false);
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }));
    await waitFor(() => {
      expect(calls.some((c) => c.method === 'DELETE' && c.url === '/api/board/cards/c-2')).toBe(
        true,
      );
    });
    await waitFor(() => {
      expect(screen.queryByText('Pay the invoice')).toBeNull();
    });
  });

  it('says why a write was refused and reloads what the server holds', async () => {
    await act(() => i18n.changeLanguage('en'));
    const calls = stubServer({
      writeStatus: 404,
      writeBody: { error: 'not_found', message: 'gone' },
    });
    renderBoard();
    await openCardMenu('Call the supplier');
    fireEvent.click(await screen.findByText('Duplicate card'));
    expect(await screen.findByRole('alert')).toHaveProperty(
      'textContent',
      'That card is no longer on the board.',
    );
    await waitFor(() => {
      expect(
        calls.filter((c) => c.method === 'GET' && c.url === '/api/board/cards').length,
      ).toBeGreaterThan(1);
    });
  });

  it('saves the column array the operator edited', async () => {
    await act(() => i18n.changeLanguage('en'));
    const calls = stubServer();
    renderBoard();
    await screen.findByText('Call the supplier');
    fireEvent.click(screen.getByRole('button', { name: 'Columns' }));
    const dialog = await screen.findByRole('dialog', { name: 'Board columns' });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Name of column 3' }), {
      target: { value: 'Shipped' },
    });
    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Card limit of column 1' }), {
      target: { value: '7' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save columns' }));
    await waitFor(() => {
      const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/board/columns');
      expect(put?.body).toEqual([
        { id: 'todo', label: 'To do', cardLimit: 7 },
        { id: 'doing', label: 'Doing', cardLimit: 5 },
        { id: 'done', label: 'Shipped' },
      ]);
    });
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Board columns' })).toBeNull();
    });
  });

  it('opens the card in the editor, and discusses it where it was discussed', async () => {
    await act(() => i18n.changeLanguage('en'));
    stubServer();
    const { onDiscuss } = renderBoard();
    expect(await screen.findByText('Task: active')).toBeTruthy();
    fireEvent.click(await screen.findByText('Call the supplier'));
    fireEvent.click(await screen.findByText('Discuss in chat'));
    expect(onDiscuss).toHaveBeenCalledWith('conv-1', 'About the board card "Call the supplier": ');
    await waitFor(() => {
      expect(screen.queryByText('Discuss in chat')).toBeNull();
    });
    fireEvent.click(screen.getByText('Pay the invoice'));
    fireEvent.click(await screen.findByText('Discuss in chat'));
    expect(onDiscuss).toHaveBeenLastCalledWith('', 'About the board card "Pay the invoice": ');
  });

  it('saves an edited card through the provider, and deletes from the editor only once confirmed', async () => {
    await act(() => i18n.changeLanguage('en'));
    const calls = stubServer();
    renderBoard();
    fireEvent.click(await screen.findByText('Pay the invoice'));
    const title = await screen.findByDisplayValue('Pay the invoice');
    fireEvent.input(title, { target: { value: 'Pay both invoices' } });
    fireEvent.click(screen.getByText('Save'));
    await waitFor(
      () => {
        const put = calls.find((c) => c.method === 'PUT' && c.url === '/api/board/cards/c-2');
        expect((put?.body as { label?: string } | undefined)?.label).toBe('Pay both invoices');
      },
      { timeout: 3000 },
    );
    fireEvent.click(await screen.findByText('Pay both invoices'));
    fireEvent.click(await screen.findByText('Delete'));
    expect(await screen.findByRole('dialog', { name: 'Delete this card?' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Keep it' }));
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false);
  });

  it('gives a card added from a column a title the server accepts', async () => {
    await act(() => i18n.changeLanguage('en'));
    const calls = stubServer();
    renderBoard();
    await screen.findByText('Call the supplier');
    fireEvent.click(screen.getByRole('button', { name: 'Add card to To do' }));
    await waitFor(() => {
      const post = calls.find((c) => c.method === 'POST' && c.url === '/api/board/cards');
      expect(post?.body).toMatchObject({ label: 'New card', column: 'todo' });
    });
  });

  it('saves the filters on screen as a view, and pins and deletes saved ones', async () => {
    await act(() => i18n.changeLanguage('en'));
    const calls = stubServer({
      views: [{ id: 'v-1', name: 'urgent', filters: { priority: 3 }, pinned: false }],
      writeBody: { id: 'v-2', name: 'mine', filters: { source: 'cockpit' }, pinned: false },
    });
    renderBoard();
    fireEvent.click(await screen.findByRole('button', { name: 'urgent' }));
    await waitFor(() => {
      expect(screen.queryByText('Call the supplier')).toBeNull();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Pin urgent' }));
    fireEvent.change(screen.getByRole('combobox', { name: 'Source' }), {
      target: { value: 'cockpit' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save as view' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'View name' }), {
      target: { value: 'mine' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    fireEvent.click(screen.getByRole('button', { name: 'Delete the view urgent' }));
    await waitFor(() => {
      const writes = calls.filter(
        (c) => c.url.startsWith('/api/board/views') && c.method !== 'GET',
      );
      expect(writes.map((c) => [c.method, c.url, c.body])).toEqual([
        ['PUT', '/api/board/views', { name: 'urgent', filters: { priority: 3 }, pinned: true }],
        [
          'PUT',
          '/api/board/views',
          { name: 'mine', filters: { priority: 3, source: 'cockpit' }, pinned: false },
        ],
        ['DELETE', '/api/board/views/v-1', undefined],
      ]);
    });
  });

  it('keeps the column editor open on a refused save and says why', async () => {
    await act(() => i18n.changeLanguage('en'));
    stubServer({ writeStatus: 409, writeBody: { error: 'column_in_use', message: 'doing' } });
    renderBoard();
    await screen.findByText('Call the supplier');
    fireEvent.click(screen.getByRole('button', { name: 'Columns' }));
    const dialog = await screen.findByRole('dialog', { name: 'Board columns' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Remove column 2' }));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Save columns' }));
    expect(await within(dialog).findByRole('alert')).toHaveProperty(
      'textContent',
      'A column that still holds cards cannot be removed: move them first.',
    );
  });
});
