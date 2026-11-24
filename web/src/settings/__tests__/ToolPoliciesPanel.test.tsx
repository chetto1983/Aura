import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { chooseOption } from '../../test/chooseOption';
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

// TOOLS is what GET /api/approvals/policies/tools answers: the agent's registry, MCP mounts
// included, with actions only on the multiplexed tools the gateway keys by action.
const TOOLS = [
  { name: 'board', summary: 'The work board.', actions: ['list', 'add', 'delete'] },
  { name: 'calendar__send_email', summary: 'Send an email.' },
  { name: 'shell_exec', summary: 'Run a shell command.' },
  { name: 'browser__agent_browser_open', summary: 'Open a page.' },
];

function stubFetch(opts: {
  readonly list: unknown;
  readonly writeStatus?: number;
  readonly toolsStatus?: number;
}) {
  const calls: Call[] = [];
  const spy = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const method = init?.method ?? 'GET';
    const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : undefined;
    const url = urlOf(input);
    calls.push({ url, method, body });
    if (method === 'GET' && url.endsWith('/tools')) {
      const status = opts.toolsStatus ?? 200;
      return Promise.resolve(new Response(JSON.stringify(TOOLS), { status }));
    }
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
    tool: 'board',
    action: 'delete',
    subject: 'board delete',
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

// pickTool opens the tool combobox and chooses one entry, the way an operator does.
async function pickTool(name: string) {
  fireEvent.click(await screen.findByRole('combobox', { name: 'Tool' }));
  fireEvent.click(await screen.findByRole('option', { name: new RegExp(`^${name}`) }));
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('ToolPoliciesPanel', () => {
  it('lists each policy under its own word', async () => {
    stubFetch({ list: POLICIES });
    renderPanel();
    expect(await screen.findByText('board delete')).toBeTruthy();
    const deny = screen.getByText('board delete').closest('li');
    const ask = screen.getByText('shell_exec', { selector: 'span' }).closest('li');
    expect(deny?.textContent).toContain('Never run it');
    expect(ask?.textContent).toContain('Always ask me');
  });

  it('says what an empty list means', async () => {
    stubFetch({ list: [] });
    renderPanel();
    expect(await screen.findByText(/No tool policies/)).toBeTruthy();
  });

  it('sets the subject picked from the agent tools, with its action', async () => {
    const calls = stubFetch({ list: [] });
    renderPanel();
    await screen.findByText(/No tool policies/);
    await pickTool('board');
    await chooseOption('Action (optional)', 'delete');
    await chooseOption('Policy', 'Never run it');
    fireEvent.click(screen.getByRole('button', { name: 'Set policy' }));
    await waitFor(() => {
      expect(calls.some((c) => c.method === 'PUT')).toBe(true);
    });
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.url).toBe('/api/approvals/policies');
    expect(put?.body).toEqual({ tool: 'board', action: 'delete', policy: 'deny' });
  });

  it('sets a PIM tool as a plain tool, with no action list', async () => {
    const calls = stubFetch({ list: [] });
    renderPanel();
    await screen.findByText(/No tool policies/);
    await pickTool('calendar__send_email');
    const action = screen.getByRole('combobox', { name: 'Action (optional)' });
    expect(action.hasAttribute('disabled')).toBe(true);
    expect(action.textContent).toContain('This tool has no actions');
    await chooseOption('Policy', 'Never run it');
    fireEvent.click(screen.getByRole('button', { name: 'Set policy' }));
    await waitFor(() => {
      expect(calls.some((c) => c.method === 'PUT')).toBe(true);
    });
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.body).toEqual({ tool: 'calendar__send_email', action: '', policy: 'deny' });
  });

  it('groups the tools under Aura and under each MCP server', async () => {
    stubFetch({ list: [] });
    renderPanel();
    await screen.findByText(/No tool policies/);
    fireEvent.click(await screen.findByRole('combobox', { name: 'Tool' }));
    const browser = screen.getByRole('group', { name: 'browser' });
    expect(within(browser).getByRole('option', { name: /agent_browser_open/ })).toBeTruthy();
    const aura = screen.getByRole('group', { name: 'Aura' });
    expect(within(aura).getByRole('option', { name: /shell_exec/ })).toBeTruthy();
    expect(within(aura).queryByRole('option', { name: /agent_browser_open/ })).toBeNull();
  });

  it('offers actions only for a tool the gateway keys by action', async () => {
    stubFetch({ list: [] });
    renderPanel();
    await screen.findByText(/No tool policies/);
    const action = () => screen.getByRole('combobox', { name: 'Action (optional)' });
    expect(action().hasAttribute('disabled')).toBe(true);
    await pickTool('board');
    expect(action().hasAttribute('disabled')).toBe(false);
    fireEvent.click(action());
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      'Every action',
      'list',
      'add',
      'delete',
    ]);
    fireEvent.click(screen.getByRole('option', { name: 'delete' }));
    await pickTool('shell_exec');
    expect(action().hasAttribute('disabled')).toBe(true);
    expect(action().textContent).toContain('This tool has no actions');
  });

  it('shows a tool list it could not load', async () => {
    stubFetch({ list: [], toolsStatus: 503 });
    renderPanel();
    expect(await screen.findByText(/Could not load the tools/)).toBeTruthy();
  });

  it('will not submit an empty tool', async () => {
    const calls = stubFetch({ list: [] });
    renderPanel();
    await screen.findByText(/No tool policies/);
    const button = screen.getByRole<HTMLButtonElement>('button', { name: 'Set policy' });
    expect(button.disabled).toBe(true);
    const form = button.closest('form');
    if (form === null) throw new Error('the submit button is outside its form');
    fireEvent.submit(form);
    expect(calls.filter((c) => c.method !== 'GET')).toHaveLength(0);
  });

  it('clears one policy by its two coordinates', async () => {
    const calls = stubFetch({ list: POLICIES });
    renderPanel();
    await screen.findByText('board delete');
    const clear = screen.getByText('board delete').closest('li')?.querySelector('button');
    if (clear == null) throw new Error('the policy row has no clear button');
    fireEvent.click(clear);
    await waitFor(() => {
      expect(calls.some((c) => c.method === 'POST')).toBe(true);
    });
    const post = calls.find((c) => c.method === 'POST');
    expect(post?.url).toBe('/api/approvals/policies/clear');
    expect(post?.body).toEqual({ tool: 'board', action: 'delete' });
  });

  it('shows a failed save instead of pretending it worked', async () => {
    stubFetch({ list: [], writeStatus: 500 });
    renderPanel();
    await screen.findByText(/No tool policies/);
    await pickTool('shell_exec');
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
