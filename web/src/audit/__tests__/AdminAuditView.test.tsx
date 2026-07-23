import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { AdminAuditView } from '../AdminAuditView';

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

const ROSTER = {
  identities: [
    { id: 'id-a', name: 'Alice', kind: 'user', capabilities: [] },
    { id: 'id-b', name: 'Bob', kind: 'user', capabilities: [] },
  ],
};

// Build 25 tool events so the page-size threshold (hasNextPage) is exercised.
function fullPage() {
  const events = Array.from({ length: 25 }, (_unused, i) => ({
    source: 'tool',
    action: 'end',
    target: `tool-${String(i)}`,
    detail: 'ok',
    created_at: '2026-07-05T12:00:00Z',
  }));
  return { events, limit: 25, offset: 0 };
}

function stub(auditByOffset: (offset: number) => unknown) {
  const spy = vi.fn((input: RequestInfo | URL) => {
    const url = urlOf(input);
    if (url.includes('/api/admin/audit')) {
      const offset = Number(new URL(url, 'http://x').searchParams.get('offset') ?? '0');
      return Promise.resolve(new Response(JSON.stringify(auditByOffset(offset)), { status: 200 }));
    }
    return Promise.resolve(new Response(JSON.stringify(ROSTER), { status: 200 }));
  });
  vi.stubGlobal('fetch', spy);
  return spy;
}

function renderView() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <AdminAuditView />
    </QueryClientProvider>,
  );
}

describe('AdminAuditView', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('renders the activity feed for the default identity with source badges', async () => {
    stub(() => ({
      events: [
        {
          source: 'mcp',
          action: 'install',
          target: 'aura-memory',
          detail: '',
          created_at: '2026-07-05T10:00:00Z',
        },
        {
          source: 'skill',
          action: 'create',
          target: 'my-skill',
          detail: 'model',
          created_at: '2026-07-05T09:00:00Z',
        },
      ],
      limit: 25,
      offset: 0,
    }));
    renderView();
    expect(await screen.findByText('install')).toBeTruthy();
    expect(screen.getByText('MCP')).toBeTruthy();
    expect(screen.getByText('Skill')).toBeTruthy();
    expect(screen.getByText('aura-memory')).toBeTruthy();
  });

  it('merges tool start/end pairs into one compact row with outcome chip + duration', async () => {
    stub(() => ({
      events: [
        {
          source: 'tool',
          action: 'end',
          target: 'shell_exec',
          detail: 'ok',
          created_at: '2026-07-23T13:45:01.280Z',
        },
        {
          source: 'tool',
          action: 'start',
          target: 'shell_exec',
          detail: '',
          created_at: '2026-07-23T13:45:00.780Z',
        },
        {
          source: 'tool',
          action: 'start',
          target: 'stuck_tool',
          detail: '',
          created_at: '2026-07-23T13:40:00.000Z',
        },
      ],
      limit: 25,
      offset: 0,
    }));
    renderView();

    // ONE merged row for the pair (no separate start/end action rows).
    const merged = await screen.findAllByTestId('audit-invocation');
    expect(merged).toHaveLength(2);
    expect(screen.queryByText('end')).toBeNull();
    expect(screen.queryByText('start')).toBeNull();
    expect(screen.getByText('shell_exec')).toBeTruthy();
    expect(screen.getByText('ok')).toBeTruthy();
    expect(screen.getByTestId('audit-duration').textContent).toBe('0.5 s');
    // The orphan start renders as its own running-state row.
    expect(screen.getByText('stuck_tool')).toBeTruthy();
    expect(screen.getByText('running')).toBeTruthy();
  });

  it('shows the empty state when an identity has no activity', async () => {
    stub(() => ({ events: [], limit: 25, offset: 0 }));
    renderView();
    expect(await screen.findByText('No recorded activity for this identity yet.')).toBeTruthy();
  });

  it('pages forward with Next and requests the next offset', async () => {
    const spy = stub((offset) => (offset === 0 ? fullPage() : { events: [], limit: 25, offset }));
    renderView();
    await screen.findByText('tool-0'); // first page loaded (unique target)

    fireEvent.click(screen.getByRole('button', { name: 'Next' }));

    await waitFor(() => {
      const requestedOffsets = spy.mock.calls
        .map((c) => urlOf(c[0]))
        .filter((u) => u.includes('/api/admin/audit'))
        .map((u) => new URL(u, 'http://x').searchParams.get('offset'));
      expect(requestedOffsets).toContain('25');
    });
  });

  it('refetches the current page on Refresh', async () => {
    const spy = stub(() => ({ events: [], limit: 25, offset: 0 }));
    renderView();
    await screen.findByText('No recorded activity for this identity yet.');
    const before = spy.mock.calls.filter((c) => urlOf(c[0]).includes('/api/admin/audit')).length;

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    await waitFor(() => {
      const after = spy.mock.calls.filter((c) => urlOf(c[0]).includes('/api/admin/audit')).length;
      expect(after).toBeGreaterThan(before);
    });
  });
});
