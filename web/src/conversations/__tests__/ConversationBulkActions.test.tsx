import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ConversationSidebar } from '../ConversationSidebar';
import type { Conversation } from '../useConversations';

const row = (ID: string, Status = 'active'): Conversation => ({
  ID,
  Title: ID,
  TitleSet: true,
  Status,
  IdentityID: 'owner',
  Model: 'model',
  TotalInputTokens: 0,
  TotalOutputTokens: 0,
  TotalCachedTokens: 0,
  TotalCostUSD: 0,
  CreatedAt: new Date().toISOString(),
});

describe('bulk conversation actions', () => {
  let rows: Conversation[];
  let requests: { id: string; action: string }[];
  let failures: Set<string>;
  let hold: Promise<void> | undefined;

  beforeEach(() => {
    rows = [row('First'), row('Second'), row('Hidden', 'archived')];
    requests = [];
    failures = new Set();
    hold = undefined;
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: string, init?: RequestInit) => {
        if (!init?.method || init.method === 'GET') {
          return new Response(
            JSON.stringify(
              input.includes('archived=true') ? rows : rows.filter((r) => r.Status !== 'archived'),
            ),
          );
        }
        const [, id = '', action = 'delete'] =
          /conversations\/([^/]+)(?:\/(archive|unarchive))?$/.exec(input) ?? [];
        requests.push({ id, action });
        await hold;
        if (failures.has(id)) return new Response('denied', { status: 403 });
        rows =
          action !== 'delete'
            ? rows.map((r) =>
                r.ID === id ? { ...r, Status: action === 'archive' ? 'archived' : 'active' } : r,
              )
            : rows.filter((r) => r.ID !== id);
        return new Response(null, { status: 204 });
      }),
    );
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function mount(onDeleted = vi.fn()) {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const onSelect = vi.fn();
    render(
      <QueryClientProvider client={client}>
        <ConversationSidebar activeId="First" onSelect={onSelect} onDeleted={onDeleted} />
      </QueryClientProvider>,
    );
    return { onSelect, onDeleted };
  }

  async function selectMode() {
    fireEvent.click(await screen.findByRole('button', { name: 'Select conversations' }));
  }

  function check(name: string) {
    fireEvent.click(screen.getByRole('checkbox', { name: `Select ${name}` }));
  }

  it('archives only selected rows without changing the open chat', async () => {
    const { onSelect, onDeleted } = mount();
    await selectMode();
    check('Second');
    expect(screen.getByText('1 selected')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Archive' }));
    expect(await screen.findByText('1 conversation archived.')).toBeTruthy();
    expect(requests).toEqual([{ id: 'Second', action: 'archive' }]);
    expect(onSelect).not.toHaveBeenCalled();
    expect(onDeleted).not.toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Second' })).toBeNull();
  });

  it('selects listed rows only and clears selection when the archive filter changes', async () => {
    mount();
    await selectMode();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all listed conversations' }));
    expect(screen.getByText('2 selected')).toBeTruthy();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Show archived' }));
    await screen.findByRole('button', { name: 'Hidden' });
    await selectMode();
    expect(screen.getByText('0 selected')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Delete' }).hasAttribute('disabled')).toBe(true);
    expect(requests).toEqual([]);
  });

  it('restores archived rows in a mixed selection and keeps the other selections', async () => {
    mount();
    await screen.findByRole('button', { name: 'Select conversations' });
    fireEvent.click(screen.getByRole('checkbox', { name: 'Show archived' }));
    await screen.findByRole('button', { name: 'Hidden' });
    await selectMode();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all listed conversations' }));
    fireEvent.click(screen.getByRole('button', { name: 'Unarchive' }));
    await screen.findByText('1 conversation restored.');
    expect(requests).toEqual([{ id: 'Hidden', action: 'unarchive' }]);
    expect(screen.getByText('2 selected')).toBeTruthy();
    expect(rows.every((r) => r.Status === 'active')).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Archive' }));
    await screen.findByText('2 conversations archived.');
    expect(requests).toEqual([
      { id: 'Hidden', action: 'unarchive' },
      { id: 'First', action: 'archive' },
      { id: 'Second', action: 'archive' },
    ]);
    expect(rows.find((r) => r.ID === 'Hidden')?.Status).toBe('active');
  });

  it('requires one explicit confirmation and reports only successful deletions', async () => {
    const { onDeleted } = mount();
    await selectMode();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all listed conversations' }));
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    let dialog = screen.getByRole('dialog', { name: 'Delete 2 conversations?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep conversations' }));
    expect(requests).toEqual([]);
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    dialog = screen.getByRole('dialog', { name: 'Delete 2 conversations?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete permanently' }));
    await screen.findByText('2 conversations deleted.');
    expect(requests).toEqual([
      { id: 'First', action: 'delete' },
      { id: 'Second', action: 'delete' },
    ]);
    expect(onDeleted.mock.calls).toEqual([['First'], ['Second']]);
    expect(rows.map((r) => r.ID)).toEqual(['Hidden']);
  });

  it('keeps failures selected and retries them without repeating successful actions', async () => {
    failures.add('Second');
    const { onDeleted } = mount();
    await selectMode();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all listed conversations' }));
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete permanently' }),
    );
    expect((await screen.findByRole('alert')).textContent).toContain(
      '1 failed and remains selected',
    );
    expect(onDeleted.mock.calls).toEqual([['First']]);
    expect(
      screen.getByRole('checkbox', { name: 'Select Second' }).getAttribute('aria-checked'),
    ).toBe('true');
    failures.clear();
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    fireEvent.click(
      within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete permanently' }),
    );
    await waitFor(() => {
      expect(onDeleted.mock.calls).toEqual([['First'], ['Second']]);
    });
    expect(requests.map((r) => r.id)).toEqual(['First', 'Second', 'Second']);
  });

  it('keeps labels and selections independent when desktop and mobile sidebars coexist', async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <div data-testid="desktop">
          <ConversationSidebar activeId="" onSelect={() => undefined} />
        </div>
        <div data-testid="mobile">
          <ConversationSidebar activeId="" onSelect={() => undefined} />
        </div>
      </QueryClientProvider>,
    );
    const buttons = await screen.findAllByRole('button', { name: 'Select conversations' });
    buttons.forEach((button) => {
      fireEvent.click(button);
    });
    const filters = screen.getAllByRole('checkbox', { name: 'Show archived' });
    expect(filters).toHaveLength(2);
    expect(filters[0]?.id).not.toBe(filters[1]?.id);
    const desktop = within(screen.getByTestId('desktop'));
    const mobile = within(screen.getByTestId('mobile'));
    fireEvent.click(mobile.getByText('First'));
    expect(
      mobile.getByRole('checkbox', { name: 'Select First' }).getAttribute('aria-checked'),
    ).toBe('true');
    expect(
      desktop.getByRole('checkbox', { name: 'Select First' }).getAttribute('aria-checked'),
    ).toBe('false');
    const all = screen.getAllByRole('checkbox', { name: 'Select all listed conversations' });
    expect(all[0]?.id).not.toBe(all[1]?.id);
  });

  it('prevents duplicate submissions and selection changes during a batch', async () => {
    let release: () => void = () => undefined;
    hold = new Promise<void>((resolve) => {
      release = resolve;
    });
    mount();
    await selectMode();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all listed conversations' }));
    const archive = screen.getByRole('button', { name: 'Archive' });
    fireEvent.click(archive);
    await screen.findByText('Updating conversations…');
    fireEvent.click(archive);
    expect(screen.getByRole('button', { name: 'Cancel selection' }).hasAttribute('disabled')).toBe(
      true,
    );
    expect(screen.getByRole('checkbox', { name: 'Select First' }).hasAttribute('disabled')).toBe(
      true,
    );
    expect(requests).toHaveLength(1);
    release();
    await screen.findByText('2 conversations archived.');
    expect(requests).toHaveLength(2);
  });
});
