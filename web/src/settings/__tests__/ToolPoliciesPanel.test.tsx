import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ToolPoliciesPanel } from '../ToolPoliciesPanel';

// ToolPoliciesPanel (prd.md §5, 2026-10-09). The suite pins what this surface can get wrong
// quietly: a policy shown under the wrong word, a set that sends a different subject than
// the one typed, and a failed save that looks like it worked.

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

interface Call {
  readonly url: string;
  readonly method: string;
  readonly body: unknown;
}

function stubFetch(opts: { readonly list: unknown; readonly writeStatus?: number }) {
  const calls: Call[] = [];
  const spy = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const method = init?.method ?? 'GET';
    const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined;
    calls.push({ url: urlOf(input), method, body });
    if (method === 'GET') {
      return Promise.resolve(new Response(JSON.stringify(opts.list), { status: 200 }));
    }
    const status = opts.writeStatus ?? 200;
    return Promise.resolve(new Response(JSON.stringify({ set: true, cleared: true }), { status }));
  });
  vi.stubGlobal('fetch', spy);
  return calls;
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ToolPoliciesPanel />
    </QueryClientProvider>,
  );
}

const POLICIES = [
  {
    tool: 'calendar',
    action: 'send_email',
    subject: 'calendar send_email',
    policy: 'deny',
    set_at: '2026-10-09T18:00:00Z',
  },
  {
    tool: 'shell_exec',
    action: '',
    subject: 'shell_exec',
    policy: 'ask',
    set_at: '2026-10-09T18:00:00Z',
  },
];

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('ToolPoliciesPanel', () => {
  it('lists each policy under its own word', async () => {
    stubFetch({ list: POLICIES });
    renderPanel();
    expect(await screen.findByText('calendar send_email')).toBeTruthy();
    const deny = screen.getByText('calendar send_email').closest('li');
    const ask = screen.getByText('shell_exec', { selector: 'span' }).closest('li');
    expect(deny?.textContent).toContain('Never run it');
    expect(ask?.textContent).toContain('Always ask me');
  });

  it('says what an empty list means', async () => {
    stubFetch({ list: [] });
    renderPanel();
    expect(await screen.findByText(/No tool policies/)).toBeTruthy();
  });

  it('sets the subject exactly as typed, trimmed', async () => {
    const calls = stubFetch({ list: [] });
    renderPanel();
    await screen.findByText(/No tool policies/);
    fireEvent.change(screen.getByLabelText('Tool'), { target: { value: '  calendar ' } });
    fireEvent.change(screen.getByLabelText('Action (optional)'), {
      target: { value: 'send_email' },
    });
    fireEvent.change(screen.getByLabelText('Policy'), { target: { value: 'deny' } });
    fireEvent.click(screen.getByRole('button', { name: 'Set policy' }));
    await waitFor(() => {
      expect(calls.some((c) => c.method === 'PUT')).toBe(true);
    });
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.url).toBe('/api/approvals/policies');
    expect(put?.body).toEqual({ tool: 'calendar', action: 'send_email', policy: 'deny' });
  });

  it('will not submit an empty tool', async () => {
    const calls = stubFetch({ list: [] });
    renderPanel();
    await screen.findByText(/No tool policies/);
    const button = screen.getByRole('button', { name: 'Set policy' }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    const form = button.closest('form');
    if (form === null) throw new Error('the submit button is outside its form');
    fireEvent.submit(form);
    expect(calls.filter((c) => c.method !== 'GET')).toHaveLength(0);
  });

  it('clears one policy by its two coordinates', async () => {
    const calls = stubFetch({ list: POLICIES });
    renderPanel();
    await screen.findByText('calendar send_email');
    const clear = screen.getByText('calendar send_email').closest('li')?.querySelector('button');
    if (clear == null) throw new Error('the policy row has no clear button');
    fireEvent.click(clear);
    await waitFor(() => {
      expect(calls.some((c) => c.method === 'POST')).toBe(true);
    });
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.url).toBe('/api/approvals/policies/clear');
    expect(post?.body).toEqual({ tool: 'calendar', action: 'send_email' });
  });

  it('shows a failed save instead of pretending it worked', async () => {
    stubFetch({ list: [], writeStatus: 500 });
    renderPanel();
    await screen.findByText(/No tool policies/);
    fireEvent.change(screen.getByLabelText('Tool'), { target: { value: 'shell_exec' } });
    fireEvent.click(screen.getByRole('button', { name: 'Set policy' }));
    expect(await screen.findByText(/Could not save that policy/)).toBeTruthy();
  });

  it('shows a failed load', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('nope', { status: 500 }))),
    );
    renderPanel();
    expect(await screen.findByText(/Could not load your tool policies/)).toBeTruthy();
  });
});
