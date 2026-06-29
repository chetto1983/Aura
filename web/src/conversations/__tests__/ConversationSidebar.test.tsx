import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import '../../i18n/i18n';
import { ConversationSidebar } from '../ConversationSidebar';
import type { Conversation } from '../useConversations';

function conv(over: Partial<Conversation> & Pick<Conversation, 'ID'>): Conversation {
  return {
    Title: '',
    TitleSet: false,
    IdentityID: 'op-1',
    Status: 'active',
    Model: 'deepseek-v4',
    TotalInputTokens: 0,
    TotalOutputTokens: 0,
    TotalCachedTokens: 0,
    TotalCostUSD: 0,
    CreatedAt: '2026-06-17T10:00:00Z',
    ...over,
  };
}

const ROWS: Conversation[] = [
  conv({ ID: 'c-recent', Title: 'Latest run', TitleSet: true }),
  conv({ ID: 'c-older', Title: 'Older run', TitleSet: true }),
];

const ARCHIVED: Conversation = conv({
  ID: 'c-arch',
  Title: 'Archived run',
  TitleSet: true,
  Status: 'archived',
});

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

interface FetchLog {
  readonly url: string;
  readonly method: string;
}

// A fetch double: list (recent-first as the server returns it) + 204 on mutations.
// Returns the included-archived list when ?archived=true.
function stubFetch(log: FetchLog[]) {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = urlOf(input);
    const method = init?.method ?? 'GET';
    log.push({ url, method });
    if (method === 'GET' && url.includes('/api/conversations')) {
      const body = url.includes('archived=true') ? [...ROWS, ARCHIVED] : ROWS;
      return Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
    }
    // rename/archive/unarchive/delete → 204 No Content.
    return Promise.resolve(new Response(null, { status: 204 }));
  });
}

function renderSidebar(props?: { activeId?: string; onSelect?: (id: string) => void }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
  return render(
    <ConversationSidebar
      activeId={props?.activeId ?? ''}
      onSelect={props?.onSelect ?? (() => undefined)}
    />,
    { wrapper: Wrapper },
  );
}

describe('ConversationSidebar (CHAT-02 / D-07)', () => {
  let log: FetchLog[];
  beforeEach(() => {
    log = [];
    vi.stubGlobal('fetch', stubFetch(log));
    // jsdom HTMLDialogElement lacks showModal/close — polyfill for the confirm.
    if (typeof HTMLDialogElement !== 'undefined') {
      HTMLDialogElement.prototype.showModal = function show() {
        this.open = true;
      };
      HTMLDialogElement.prototype.close = function close() {
        this.open = false;
        this.dispatchEvent(new Event('close'));
      };
    }
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The listitem for a given conversation title (scoped action queries).
  function rowFor(title: string): HTMLElement {
    return screen.getByText(title).closest('li') as HTMLElement;
  }

  function openActions(title: string): HTMLElement {
    const row = rowFor(title);
    fireEvent.click(within(row).getByRole('button', { name: 'Conversation actions' }));
    return row;
  }

  it('renders the conversation rows recent-first over GET /api/conversations', async () => {
    renderSidebar();
    await waitFor(() => {
      expect(screen.getByText('Latest run')).toBeTruthy();
    });
    expect(screen.getByText('Latest run').closest('[data-slot="card"]')).toBeNull();
    expect(
      within(rowFor('Latest run')).getByRole('button', { name: 'Conversation actions' }),
    ).toBeTruthy();
    expect(within(rowFor('Latest run')).queryByRole('button', { name: 'Rename' })).toBeNull();
    // The store returns recent-first; the rendered order preserves it.
    const titles = screen.getAllByText(/run$/).map((el) => el.textContent);
    expect(titles).toEqual(['Latest run', 'Older run']);
    // Archived rows hidden until the toggle (no ?archived=true yet).
    expect(screen.queryByText('Archived run')).toBeNull();
  });

  it('marks the active row aria-current', async () => {
    renderSidebar({ activeId: 'c-older' });
    const row = await screen.findByRole('button', { name: 'Older run' });
    expect(row.getAttribute('aria-current')).toBe('true');
  });

  it('shows archived rows after the include-archived toggle', async () => {
    renderSidebar();
    await screen.findByText('Latest run');
    const checkbox = screen.getByRole('checkbox', { name: 'Show archived' });
    expect(checkbox.getAttribute('data-slot')).toBe('checkbox');
    fireEvent.click(checkbox);
    await waitFor(() => {
      expect(screen.getByText('Archived run')).toBeTruthy();
    });
    expect(log.some((c) => c.url.includes('archived=true'))).toBe(true);
  });

  it('selects a conversation when its row is clicked', async () => {
    const onSelect = vi.fn();
    renderSidebar({ onSelect });
    const row = await screen.findByRole('button', { name: 'Latest run' });
    fireEvent.click(row);
    expect(onSelect).toHaveBeenCalledWith('c-recent');
  });

  it('inline-renames a conversation → POST /rename', async () => {
    renderSidebar();
    await screen.findByText('Latest run');
    fireEvent.click(within(openActions('Latest run')).getByRole('menuitem', { name: 'Rename' }));
    const input = screen.getByRole('textbox', { name: 'Conversation title' });
    fireEvent.change(input, { target: { value: 'Renamed run' } });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
    await waitFor(() => {
      expect(log.some((c) => c.method === 'POST' && c.url.endsWith('/c-recent/rename'))).toBe(true);
    });
  });

  it('Escape cancels an inline rename without a POST', async () => {
    renderSidebar();
    await screen.findByText('Latest run');
    fireEvent.click(within(openActions('Latest run')).getByRole('menuitem', { name: 'Rename' }));
    const input = screen.getByRole('textbox', { name: 'Conversation title' });
    fireEvent.change(input, { target: { value: 'Discarded' } });
    fireEvent.keyDown(input, { key: 'Escape', code: 'Escape' });
    // Editing closed, the title row is back, no rename hit the wire.
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Latest run' })).toBeTruthy();
    });
    expect(log.some((c) => c.url.includes('/rename'))).toBe(false);
  });

  it('does not POST a rename when the draft is blank (commit no-op)', async () => {
    renderSidebar();
    await screen.findByText('Latest run');
    fireEvent.click(within(openActions('Latest run')).getByRole('menuitem', { name: 'Rename' }));
    const input = screen.getByRole('textbox', { name: 'Conversation title' });
    fireEvent.change(input, { target: { value: '   ' } });
    fireEvent.keyDown(input, { key: 'Enter', code: 'Enter' });
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Latest run' })).toBeTruthy();
    });
    expect(log.some((c) => c.url.includes('/rename'))).toBe(false);
  });

  it('archives via the reversible primary action → POST /archive', async () => {
    renderSidebar();
    await screen.findByText('Latest run');
    fireEvent.click(within(openActions('Latest run')).getByRole('menuitem', { name: 'Archive' }));
    await waitFor(() => {
      expect(log.some((c) => c.method === 'POST' && c.url.endsWith('/c-recent/archive'))).toBe(
        true,
      );
    });
  });

  it('unarchives an archived row → POST /unarchive', async () => {
    renderSidebar();
    await screen.findByText('Latest run');
    fireEvent.click(screen.getByRole('checkbox', { name: 'Show archived' }));
    await screen.findByText('Archived run');
    fireEvent.click(
      within(openActions('Archived run')).getByRole('menuitem', { name: 'Unarchive' }),
    );
    await waitFor(() => {
      expect(log.some((c) => c.method === 'POST' && c.url.endsWith('/c-arch/unarchive'))).toBe(
        true,
      );
    });
  });

  it('gates hard delete behind a confirm dialog: DELETE never fires before confirm; Esc cancels; confirm not default-focused', async () => {
    renderSidebar();
    await screen.findByText('Latest run');

    // "Delete permanently" opens the dialog — but does NOT fire DELETE.
    fireEvent.click(
      within(openActions('Latest run')).getByRole('menuitem', { name: 'Delete permanently' }),
    );
    const dialog = await screen.findByRole('dialog');
    expect(screen.getByText('Delete conversation?')).toBeTruthy();
    expect(log.some((c) => c.method === 'DELETE')).toBe(false);

    // Cancel ("Keep conversation") is the default focus, NOT the danger confirm.
    const cancel = within(dialog).getByRole('button', { name: 'Keep conversation' });
    expect(document.activeElement).toBe(cancel);

    // Esc cancels the dialog without deleting.
    fireEvent(dialog, new Event('cancel', { cancelable: true }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    expect(log.some((c) => c.method === 'DELETE')).toBe(false);
  });

  it('confirming the dialog fires DELETE /api/conversations/{id}', async () => {
    renderSidebar();
    await screen.findByText('Latest run');
    fireEvent.click(
      within(openActions('Latest run')).getByRole('menuitem', { name: 'Delete permanently' }),
    );
    const dialog = await screen.findByRole('dialog');
    // The confirm button inside the dialog (also labelled "Delete permanently").
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete permanently' }));
    await waitFor(() => {
      expect(
        log.some((c) => c.method === 'DELETE' && c.url.endsWith('/api/conversations/c-recent')),
      ).toBe(true);
    });
  });

  it('shows the empty state when there are no conversations', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('[]', { status: 200 }))),
    );
    renderSidebar();
    await waitFor(() => {
      expect(screen.getByText('Start a run')).toBeTruthy();
    });
  });

  it('shows the error state when the list fails to load', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new Error('down'))),
    );
    renderSidebar();
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeTruthy();
    });
  });
});
